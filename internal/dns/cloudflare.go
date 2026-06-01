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

	"panel/internal/panelerr"
)

type CloudflareProvider struct {
	apiToken   string
	accountID  string
	httpClient *http.Client
	baseURL    string
}

type cloudflareEnvelope[T any] struct {
	Success bool `json:"success"`
	Result  T    `json:"result"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
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
}

func NewCloudflareProviderWithToken(apiToken string, httpClient *http.Client) *CloudflareProvider {
	return NewCloudflareProvider(apiToken, "", httpClient)
}

func NewCloudflareProvider(apiToken, accountID string, httpClient *http.Client) *CloudflareProvider {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &CloudflareProvider{apiToken: apiToken, accountID: strings.TrimSpace(accountID), httpClient: httpClient, baseURL: "https://api.cloudflare.com/client/v4"}
}

func (p *CloudflareProvider) ListRecords(ctx context.Context, zone string) ([]Record, error) {
	zoneID, err := p.zoneID(ctx, zone)
	if err != nil {
		return nil, err
	}
	var envelope cloudflareEnvelope[[]cloudflareRecord]
	if err := p.do(ctx, http.MethodGet, "/zones/"+zoneID+"/dns_records", nil, &envelope); err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(envelope.Result))
	for _, record := range envelope.Result {
		out = append(out, Record{ID: record.ID, Name: record.Name, Type: record.Type, Value: record.Content})
	}
	return out, nil
}

func (p *CloudflareProvider) CreateRecord(ctx context.Context, zone string, record RecordInput) (Record, error) {
	zoneID, err := p.zoneID(ctx, zone)
	if err != nil {
		return Record{}, err
	}
	body := map[string]any{"type": record.Type, "name": record.Name, "content": record.Value, "ttl": 120}
	var envelope cloudflareEnvelope[cloudflareRecord]
	if err := p.do(ctx, http.MethodPost, "/zones/"+zoneID+"/dns_records", body, &envelope); err != nil {
		return Record{}, err
	}
	return Record{ID: envelope.Result.ID, Name: envelope.Result.Name, Type: envelope.Result.Type, Value: envelope.Result.Content}, nil
}

func (p *CloudflareProvider) UpdateRecord(ctx context.Context, zone string, id string, record RecordInput) (Record, error) {
	zoneID, err := p.zoneID(ctx, zone)
	if err != nil {
		return Record{}, err
	}
	body := map[string]any{"type": record.Type, "name": record.Name, "content": record.Value, "ttl": 120}
	var envelope cloudflareEnvelope[cloudflareRecord]
	if err := p.do(ctx, http.MethodPut, "/zones/"+zoneID+"/dns_records/"+url.PathEscape(id), body, &envelope); err != nil {
		return Record{}, err
	}
	return Record{ID: envelope.Result.ID, Name: envelope.Result.Name, Type: envelope.Result.Type, Value: envelope.Result.Content}, nil
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
	if p.accountID != "" {
		query.Set("account.id", p.accountID)
	}
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
		return panelerr.BadGateway("cloudflare_api_error", fmt.Sprintf("Cloudflare API error %d: %s", resp.StatusCode, strings.TrimSpace(string(raw))))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return err
	}
	if failed := cloudflareError(out); failed != "" {
		return panelerr.BadGateway("cloudflare_api_error", failed)
	}
	return nil
}

func cloudflareError(out any) string {
	raw, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	var envelope struct {
		Success bool `json:"success"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Success {
		return ""
	}
	if len(envelope.Errors) == 0 {
		return "Cloudflare API request failed"
	}
	messages := make([]string, 0, len(envelope.Errors))
	for _, item := range envelope.Errors {
		messages = append(messages, item.Message)
	}
	return strings.Join(messages, "; ")
}
