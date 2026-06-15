package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"panel/internal/linux"
	"panel/internal/remoteops"
)

type HTTPClient struct {
	client *http.Client
}

func NewHTTPClient(tlsAssets *TLSAssets, timeout time.Duration) (*HTTPClient, error) {
	tlsConfig, err := tlsAssets.ClientTLSConfig()
	if err != nil {
		return nil, err
	}
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &HTTPClient{client: &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}}, nil
}

func (c *HTTPClient) Health(ctx context.Context, baseURL string) (HealthResponse, error) {
	var out HealthResponse
	err := c.get(ctx, baseURL, "/v1/health", nil, &out)
	return out, err
}

func (c *HTTPClient) OSRelease(ctx context.Context, baseURL string) (linux.OSRelease, error) {
	var out OSReleaseResponse
	if err := c.get(ctx, baseURL, "/v1/system/os-release", nil, &out); err != nil {
		return linux.OSRelease{}, err
	}
	return out.OSRelease, nil
}

func (c *HTTPClient) SystemTraits(ctx context.Context, baseURL string) (map[string]string, error) {
	var out SystemTraitsResponse
	if err := c.get(ctx, baseURL, "/v1/system/traits", nil, &out); err != nil {
		return nil, err
	}
	if out.Traits == nil {
		out.Traits = map[string]string{}
	}
	return out.Traits, nil
}

func (c *HTTPClient) MetricsSnapshot(ctx context.Context, baseURL string, serverID string) (linux.MetricsSnapshot, error) {
	var out MetricsSnapshotResponse
	query := url.Values{}
	query.Set("serverId", serverID)
	if err := c.get(ctx, baseURL, "/v1/metrics/snapshot", query, &out); err != nil {
		return linux.MetricsSnapshot{}, err
	}
	return SnapshotFromResponse(out), nil
}

func (c *HTTPClient) UFWStatus(ctx context.Context, baseURL string) (remoteops.UFWStatus, error) {
	var out UFWStatusResponse
	if err := c.get(ctx, baseURL, "/v1/ufw/status", nil, &out); err != nil {
		return remoteops.UFWStatus{}, err
	}
	return UFWStatusFromResponse(out), nil
}

func (c *HTTPClient) RuntimeDeploy(ctx context.Context, baseURL string, req RuntimeDeployRequest) (RuntimeInstanceResponse, error) {
	var out RuntimeInstanceResponse
	err := c.post(ctx, baseURL, "/v1/runtime/applications/deploy", req, &out)
	return out, err
}

func (c *HTTPClient) RuntimeStop(ctx context.Context, baseURL string, req RuntimeStopRequest) (RuntimeInstanceResponse, error) {
	var out RuntimeInstanceResponse
	err := c.post(ctx, baseURL, "/v1/runtime/applications/stop", req, &out)
	return out, err
}

func (c *HTTPClient) RuntimeRestart(ctx context.Context, baseURL string, req RuntimeRestartRequest) (RuntimeInstanceResponse, error) {
	var out RuntimeInstanceResponse
	err := c.post(ctx, baseURL, "/v1/runtime/applications/restart", req, &out)
	return out, err
}

func (c *HTTPClient) RuntimeStatus(ctx context.Context, baseURL, instanceID string) (RuntimeStatusResponse, error) {
	var out RuntimeStatusResponse
	err := c.get(ctx, baseURL, "/v1/runtime/applications/"+url.PathEscape(instanceID)+"/status", nil, &out)
	return out, err
}

func (c *HTTPClient) RuntimeLogs(ctx context.Context, baseURL, instanceID string, tail int) (RuntimeLogsResponse, error) {
	var out RuntimeLogsResponse
	query := url.Values{}
	if tail > 0 {
		query.Set("tail", fmt.Sprintf("%d", tail))
	}
	err := c.get(ctx, baseURL, "/v1/runtime/applications/"+url.PathEscape(instanceID)+"/logs", query, &out)
	return out, err
}

func (c *HTTPClient) get(ctx context.Context, baseURL, path string, query url.Values, out any) error {
	u, err := url.Parse(strings.TrimRight(baseURL, "/") + path)
	if err != nil {
		return err
	}
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var er ErrorResponse
		_ = json.NewDecoder(res.Body).Decode(&er)
		if er.Error == "" {
			er.Error = res.Status
		}
		return fmt.Errorf("agent request failed: %s", er.Error)
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func (c *HTTPClient) post(ctx context.Context, baseURL, path string, in, out any) error {
	u, err := url.Parse(strings.TrimRight(baseURL, "/") + path)
	if err != nil {
		return err
	}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(in); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var er ErrorResponse
		_ = json.NewDecoder(res.Body).Decode(&er)
		if er.Error == "" {
			er.Error = res.Status
		}
		return fmt.Errorf("agent request failed: %s", er.Error)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}
