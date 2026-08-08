package dns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	panelerr "panel/internal/platform/errors"
)

type CloudflareProvider struct {
	apiToken   string
	httpClient *http.Client
	baseURL    string
}

type cloudflareEnvelope[T any] struct {
	Success    bool                  `json:"success"`
	Result     T                     `json:"result"`
	Errors     []cloudflareAPIError  `json:"errors"`
	ResultInfo *cloudflareResultInfo `json:"result_info,omitempty"`
}

type cloudflareAPIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cloudflareResultInfo struct {
	Page       int `json:"page"`
	TotalPages int `json:"total_pages"`
}

type cloudflareZone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type cloudflareRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
	Comment string `json:"comment"`
}

func NewCloudflareProvider(apiToken string, httpClient *http.Client) *CloudflareProvider {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &CloudflareProvider{apiToken: apiToken, httpClient: httpClient, baseURL: "https://api.cloudflare.com/client/v4"}
}

func (p *CloudflareProvider) ListRecords(ctx context.Context, zone string) ([]Record, error) {
	zoneID, err := p.zoneID(ctx, zone)
	if err != nil {
		return nil, err
	}
	out := []Record{}
	for page := 1; ; page++ {
		query := url.Values{}
		query.Set("page", fmt.Sprintf("%d", page))
		query.Set("per_page", "5000")
		var envelope cloudflareEnvelope[[]cloudflareRecord]
		endpoint := "/zones/" + zoneID + "/dns_records?" + query.Encode()
		if err := p.do(ctx, http.MethodGet, endpoint, nil, &envelope); err != nil {
			return nil, err
		}
		for _, record := range envelope.Result {
			out = append(out, cloudflareRecordToRecord(record))
		}
		if envelope.ResultInfo == nil || envelope.ResultInfo.TotalPages <= page {
			break
		}
	}
	return out, nil
}

func (p *CloudflareProvider) CreateRecord(ctx context.Context, zone string, record RecordInput) (Record, error) {
	zoneID, err := p.zoneID(ctx, zone)
	if err != nil {
		return Record{}, err
	}
	body := cloudflareRecordBody(zone, record)
	var envelope cloudflareEnvelope[cloudflareRecord]
	if err := p.do(ctx, http.MethodPost, "/zones/"+zoneID+"/dns_records", body, &envelope); err != nil {
		return Record{}, err
	}
	return cloudflareRecordToRecord(envelope.Result), nil
}

func (p *CloudflareProvider) UpdateRecord(ctx context.Context, zone string, id string, record RecordInput) (Record, error) {
	zoneID, err := p.zoneID(ctx, zone)
	if err != nil {
		return Record{}, err
	}
	body := cloudflareRecordBody(zone, record)
	var envelope cloudflareEnvelope[cloudflareRecord]
	if err := p.do(ctx, http.MethodPut, "/zones/"+zoneID+"/dns_records/"+url.PathEscape(id), body, &envelope); err != nil {
		return Record{}, err
	}
	return cloudflareRecordToRecord(envelope.Result), nil
}

func (p *CloudflareProvider) DeleteRecord(ctx context.Context, zone string, id string) error {
	zoneID, err := p.zoneID(ctx, zone)
	if err != nil {
		return err
	}
	var envelope cloudflareEnvelope[map[string]string]
	return p.do(ctx, http.MethodDelete, "/zones/"+zoneID+"/dns_records/"+url.PathEscape(id), nil, &envelope)
}

func (p *CloudflareProvider) Present(ctx context.Context, domain, token, value string) error {
	zone, err := p.findZone(ctx, domain)
	if err != nil {
		return err
	}
	_, err = p.CreateRecord(ctx, zone.Name, RecordInput{
		Name:  "_acme-challenge." + strings.TrimPrefix(domain, "*."),
		Type:  "TXT",
		Value: value,
	})
	return err
}

func (p *CloudflareProvider) CleanUp(ctx context.Context, domain, token, value string) error {
	zone, err := p.findZone(ctx, domain)
	if err != nil {
		return err
	}
	records, err := p.ListRecords(ctx, zone.Name)
	if err != nil {
		return err
	}
	name := "_acme-challenge." + strings.TrimPrefix(domain, "*.")
	for _, record := range records {
		if record.Type == "TXT" && strings.EqualFold(record.Name, name) && record.Value == value {
			return p.DeleteRecord(ctx, zone.Name, record.ID)
		}
	}
	return nil
}

func (p *CloudflareProvider) findZone(ctx context.Context, domain string) (cloudflareZone, error) {
	labels := strings.Split(strings.TrimPrefix(strings.TrimSuffix(domain, "."), "*."), ".")
	for i := 0; i < len(labels)-1; i++ {
		name := strings.Join(labels[i:], ".")
		zones, err := p.zones(ctx, name)
		if err != nil {
			return cloudflareZone{}, err
		}
		for _, zone := range zones {
			if strings.EqualFold(zone.Name, name) {
				return zone, nil
			}
		}
	}
	return cloudflareZone{}, panelerr.BadGateway("cloudflare_zone_not_found", "Cloudflare zone not found for "+domain)
}

func (p *CloudflareProvider) zoneID(ctx context.Context, zone string) (string, error) {
	zones, err := p.zones(ctx, zone)
	if err != nil {
		return "", err
	}
	for _, item := range zones {
		if strings.EqualFold(item.Name, zone) {
			return item.ID, nil
		}
	}
	return "", panelerr.BadGateway("cloudflare_zone_not_found", "Cloudflare zone not found for "+zone)
}

func (p *CloudflareProvider) zones(ctx context.Context, name string) ([]cloudflareZone, error) {
	var envelope cloudflareEnvelope[[]cloudflareZone]
	query := url.Values{}
	query.Set("name", name)
	endpoint := "/zones?" + query.Encode()
	if err := p.do(ctx, http.MethodGet, endpoint, nil, &envelope); err != nil {
		return nil, err
	}
	return envelope.Result, nil
}

func (p *CloudflareProvider) do(ctx context.Context, method, endpoint string, body any, out any) error {
	if p.apiToken == "" {
		return panelerr.Validation("cloudflare_api_token_required", "Cloudflare API token is required")
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.baseURL+endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiToken)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return panelerr.BadGateway("cloudflare_unreachable", "Cloudflare API unreachable: "+err.Error())
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if cloudflareErrorCode(raw) == 81054 {
			return panelerr.Conflict("dns_record_cname_exists", "A CNAME record with that host already exists")
		}
		message := cloudflareErrorMessage(raw)
		if message == "" {
			message = strings.TrimSpace(string(raw))
		}
		return panelerr.BadGateway("cloudflare_api_error", fmt.Sprintf("Cloudflare API error %d: %s", resp.StatusCode, message))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return panelerr.BadGateway("cloudflare_invalid_response", "Cloudflare API returned a non-JSON response")
	}
	if failed := cloudflareError(out); failed != "" {
		return panelerr.BadGateway("cloudflare_api_error", failed)
	}
	return nil
}

func cloudflareRecordToRecord(record cloudflareRecord) Record {
	return Record{ID: record.ID, Name: record.Name, Type: record.Type, Value: record.Content, TTL: record.TTL, Proxied: record.Proxied, Comment: record.Comment}
}

func cloudflareRecordBody(zone string, record RecordInput) map[string]any {
	ttl := record.TTL
	if ttl <= 0 {
		ttl = 120
	}
	body := map[string]any{"type": record.Type, "name": cloudflareRecordName(zone, record.Name), "content": record.Value, "ttl": ttl}
	if record.Proxied != nil {
		body["proxied"] = *record.Proxied
	}
	if strings.TrimSpace(record.Comment) != "" {
		body["comment"] = strings.TrimSpace(record.Comment)
	}
	return body
}

func cloudflareRecordName(zone, name string) string {
	zone = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(zone), "."))
	name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
	if name == "@" || name == zone {
		return zone
	}
	if strings.HasSuffix(name, "."+zone) {
		return name
	}
	return name + "." + zone
}

func cloudflareError(out any) string {
	raw, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return cloudflareErrorMessage(raw)
}

func cloudflareErrorCode(raw []byte) int {
	var envelope struct {
		Success bool                 `json:"success"`
		Errors  []cloudflareAPIError `json:"errors"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Success || len(envelope.Errors) == 0 {
		return 0
	}
	return envelope.Errors[0].Code
}

func cloudflareErrorMessage(raw []byte) string {
	var envelope struct {
		Success bool                 `json:"success"`
		Errors  []cloudflareAPIError `json:"errors"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Success {
		return ""
	}
	if len(envelope.Errors) == 0 {
		return "Cloudflare API request failed"
	}
	messages := make([]string, 0, len(envelope.Errors))
	for _, item := range envelope.Errors {
		message := strings.TrimSpace(item.Message)
		if item.Code != 0 {
			message = fmt.Sprintf("%d: %s", item.Code, message)
		}
		messages = append(messages, message)
	}
	return strings.Join(messages, "; ")
}
