package nomad

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

type Client struct {
	cfg        Config
	httpClient *http.Client
	baseURL    *url.URL
}

func NewClient(cfg Config, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
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

func (c *Client) ListJobs(ctx context.Context, prefix string) ([]JobListItem, error) {
	var out []JobListItem
	query := url.Values{}
	if prefix != "" {
		query.Set("prefix", prefix)
	}
	return out, c.do(ctx, http.MethodGet, "/v1/jobs", query, nil, &out)
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

func (c *Client) Nodes(ctx context.Context) ([]NodeListItem, error) {
	var out []NodeListItem
	return out, c.do(ctx, http.MethodGet, "/v1/nodes", nil, nil, &out)
}

func (c *Client) Deployments(ctx context.Context) ([]Deployment, error) {
	var out []Deployment
	return out, c.do(ctx, http.MethodGet, "/v1/deployments", nil, nil, &out)
}

func (c *Client) Evaluations(ctx context.Context) ([]Evaluation, error) {
	var out []Evaluation
	return out, c.do(ctx, http.MethodGet, "/v1/evaluations", nil, nil, &out)
}

func (c *Client) Services(ctx context.Context) ([]ServiceRegistration, error) {
	var out []ServiceRegistration
	return out, c.do(ctx, http.MethodGet, "/v1/services", nil, nil, &out)
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
	reqURL := *c.baseURL
	reqURL.Path = path.Join(c.baseURL.Path, endpoint)
	values := reqURL.Query()
	if c.cfg.Namespace != "" {
		values.Set("namespace", c.cfg.Namespace)
	}
	if c.cfg.Region != "" {
		values.Set("region", c.cfg.Region)
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
	if c.cfg.Token != "" {
		req.Header.Set("X-Nomad-Token", c.cfg.Token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
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

func decodeError(status int, raw []byte) error {
	var body struct {
		Errors []string `json:"Errors"`
		Error  string   `json:"Error"`
	}
	if err := json.Unmarshal(raw, &body); err == nil {
		switch {
		case len(body.Errors) > 0:
			return fmt.Errorf("nomad api error %d: %s", status, strings.Join(body.Errors, "; "))
		case body.Error != "":
			return fmt.Errorf("nomad api error %d: %s", status, body.Error)
		}
	}
	msg := strings.TrimSpace(string(raw))
	if msg == "" {
		return fmt.Errorf("nomad api error %d", status)
	}
	return errors.New("nomad api error " + strconv.Itoa(status) + ": " + msg)
}
