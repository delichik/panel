package nomad

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"

	"panel/internal/panelerr"
)

type Client struct {
	cfg            Config
	httpClient     *http.Client
	mu             sync.RWMutex
	baseURL        *url.URL
	configProvider func(Config) Config
}

func NewClient(cfg Config, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = defaultHTTPClient(cfg)
	}
	if cfg.Address == "" {
		cfg.Address = "http://127.0.0.1:4646"
	}
	baseURL, err := url.Parse(strings.TrimRight(cfg.Address, "/"))
	if err != nil {
		baseURL = &url.URL{Scheme: "http", Host: "127.0.0.1:4646"}
	}
	return &Client{cfg: cfg, httpClient: httpClient, baseURL: baseURL}
}

func (c *Client) SetConfigProvider(provider func(Config) Config) {
	c.mu.Lock()
	c.configProvider = provider
	c.mu.Unlock()
}

func defaultHTTPClient(cfg Config) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.TLS != nil {
		tlsConfig, err := newClientTLSConfig(*cfg.TLS)
		if err == nil {
			transport.TLSClientConfig = tlsConfig
		}
	}
	return &http.Client{Transport: transport}
}

func newClientTLSConfig(cfg TLSConfig) (*tls.Config, error) {
	assets := &TLSAssets{
		CAPath:         cfg.CAFile,
		ClientCertPath: cfg.CertFile,
		ClientKeyPath:  cfg.KeyFile,
	}
	assets.CAPEM, _ = os.ReadFile(cfg.CAFile)
	if len(assets.CAPEM) == 0 {
		return nil, os.ErrNotExist
	}
	tlsConfig, err := assets.ClientTLSConfig()
	if err != nil {
		return nil, err
	}
	if !cfg.SkipVerifyHostname {
		tlsConfig.InsecureSkipVerify = false
		tlsConfig.VerifyPeerCertificate = nil
	}
	return tlsConfig, nil
}

func (c *Client) SetAddress(address string) {
	baseURL, err := url.Parse(strings.TrimRight(address, "/"))
	if err != nil || baseURL.Host == "" {
		return
	}
	c.mu.Lock()
	c.cfg.Address = address
	c.baseURL = baseURL
	c.mu.Unlock()
}

func (c *Client) Status(ctx context.Context) (StatusResponse, error) {
	var leader string
	if err := c.do(ctx, http.MethodGet, "/v1/status/leader", nil, nil, &leader); err != nil {
		return StatusResponse{Connected: false}, err
	}
	return StatusResponse{Connected: true, Leader: leader}, nil
}

func (c *Client) ReadJob(ctx context.Context, id string) (Job, error) {
	var out Job
	return out, c.do(ctx, http.MethodGet, "/v1/job/"+url.PathEscape(id), nil, nil, &out)
}

func (c *Client) ValidateJob(ctx context.Context, job Job) (ValidateResponse, error) {
	var out ValidateResponse
	return out, c.do(ctx, http.MethodPost, "/v1/validate/job", nil, jobPayload(job), &out)
}

func (c *Client) PlanJob(ctx context.Context, id string, job Job) (PlanResponse, error) {
	var out PlanResponse
	return out, c.do(ctx, http.MethodPost, "/v1/job/"+url.PathEscape(id)+"/plan", nil, jobPayload(job), &out)
}

func (c *Client) RegisterJob(ctx context.Context, id string, job Job) (RegisterResponse, error) {
	var out RegisterResponse
	return out, c.do(ctx, http.MethodPost, "/v1/job/"+url.PathEscape(id), nil, jobPayload(job), &out)
}

func (c *Client) StopJob(ctx context.Context, id string, purge bool) (StopResponse, error) {
	var out StopResponse
	query := url.Values{}
	if purge {
		query.Set("purge", "true")
	}
	return out, c.do(ctx, http.MethodDelete, "/v1/job/"+url.PathEscape(id), query, nil, &out)
}

func (c *Client) JobAllocations(ctx context.Context, id string) ([]AllocationListItem, error) {
	var out []AllocationListItem
	return out, c.do(ctx, http.MethodGet, "/v1/job/"+url.PathEscape(id)+"/allocations", nil, nil, &out)
}

func (c *Client) JobDeployment(ctx context.Context, id string) (Deployment, error) {
	var out Deployment
	return out, c.do(ctx, http.MethodGet, "/v1/job/"+url.PathEscape(id)+"/deployment", nil, nil, &out)
}

func (c *Client) JobEvaluations(ctx context.Context, id string) ([]Evaluation, error) {
	var out []Evaluation
	return out, c.do(ctx, http.MethodGet, "/v1/job/"+url.PathEscape(id)+"/evaluations", nil, nil, &out)
}

func (c *Client) Evaluation(ctx context.Context, id string) (Evaluation, error) {
	var out Evaluation
	return out, c.do(ctx, http.MethodGet, "/v1/evaluation/"+url.PathEscape(id), nil, nil, &out)
}

func (c *Client) Nodes(ctx context.Context) ([]NodeListItem, error) {
	var out []NodeListItem
	if err := c.do(ctx, http.MethodGet, "/v1/nodes", nil, nil, &out); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].ID == "" || out[i].Meta != nil {
			continue
		}
		var detail NodeListItem
		if err := c.do(ctx, http.MethodGet, "/v1/node/"+url.PathEscape(out[i].ID), nil, nil, &detail); err != nil {
			return nil, err
		}
		out[i].Meta = detail.Meta
	}
	return out, nil
}

func (c *Client) PurgeNode(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/v1/node/"+url.PathEscape(id)+"/purge", nil, nil, nil)
}

func (c *Client) RestartAllocation(ctx context.Context, allocID, task string) error {
	body := map[string]string{}
	if task != "" {
		body["TaskName"] = task
	}
	return c.do(ctx, http.MethodPost, "/v1/client/allocation/"+url.PathEscape(allocID)+"/restart", nil, body, nil)
}

func (c *Client) AllocationLogs(ctx context.Context, allocID, task, logType string, tail int) (string, error) {
	query := url.Values{}
	query.Set("task", task)
	query.Set("type", logType)
	query.Set("plain", "true")
	if tail > 0 {
		query.Set("origin", "end")
		query.Set("offset", strconv.Itoa(tail))
	}
	var out string
	return out, c.do(ctx, http.MethodGet, "/v1/client/fs/logs/"+url.PathEscape(allocID), query, nil, &out)
}

func jobPayload(job Job) map[string]Job {
	return map[string]Job{"Job": job}
}

func (c *Client) do(ctx context.Context, method, endpoint string, query url.Values, body any, out any) error {
	c.mu.RLock()
	reqURL := *c.baseURL
	cfg := c.cfg
	provider := c.configProvider
	c.mu.RUnlock()
	if provider != nil {
		cfg = provider(cfg)
	}
	reqURL.Path = path.Join(reqURL.Path, endpoint)
	values := reqURL.Query()
	if cfg.Namespace != "" {
		values.Set("namespace", cfg.Namespace)
	}
	if cfg.Region != "" {
		values.Set("region", cfg.Region)
	}
	for key, vals := range query {
		for _, val := range vals {
			values.Add(key, val)
		}
	}
	reqURL.RawQuery = values.Encode()

	var bodyReader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), bodyReader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if cfg.Token != "" {
		req.Header.Set("X-Nomad-Token", cfg.Token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return panelerr.BadGateway("nomad_unreachable", nomadUnreachableMessage(reqURL.Host))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decodeError(resp.StatusCode, raw)
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if str, ok := out.(*string); ok {
		*str = string(raw)
		return nil
	}
	return json.Unmarshal(raw, out)
}

func nomadUnreachableMessage(host string) string {
	if strings.TrimSpace(host) == "" {
		return "Panel cannot connect to the Nomad control plane. Check that a Nomad server has been bootstrapped and is reachable from Panel."
	}
	return "Panel cannot connect to the Nomad control plane at " + host + ". Check that the Nomad server is running and reachable from Panel."
}

func decodeError(status int, raw []byte) error {
	var body struct {
		Errors []string `json:"Errors"`
		Error  string   `json:"Error"`
	}
	if err := json.Unmarshal(raw, &body); err == nil {
		switch {
		case len(body.Errors) > 0:
			return panelerr.BadGateway("nomad_api_error", fmt.Sprintf("Nomad API error %d: %s", status, strings.Join(body.Errors, "; ")))
		case body.Error != "":
			return panelerr.BadGateway("nomad_api_error", fmt.Sprintf("Nomad API error %d: %s", status, body.Error))
		}
	}
	msg := strings.TrimSpace(string(raw))
	if msg == "" {
		return panelerr.BadGateway("nomad_api_error", fmt.Sprintf("Nomad API error %d", status))
	}
	return panelerr.BadGateway("nomad_api_error", "Nomad API error "+strconv.Itoa(status)+": "+msg)
}
