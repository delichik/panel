package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"panel/internal/linux"
	"panel/internal/remoteops"
)

type HTTPClient struct {
	mu      sync.RWMutex
	client  *http.Client
	timeout time.Duration
}

func NewHTTPClient(tlsAssets *TLSAssets, timeout time.Duration) (*HTTPClient, error) {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	client := &HTTPClient{timeout: timeout}
	if err := client.ReloadTLSAssets(tlsAssets); err != nil {
		return nil, err
	}
	return client, nil
}

func (c *HTTPClient) ReloadTLSAssets(tlsAssets *TLSAssets) error {
	tlsConfig, err := tlsAssets.ClientTLSConfig()
	if err != nil {
		return err
	}
	next := &http.Client{
		Timeout: c.timeout,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}
	c.mu.Lock()
	previous := c.client
	c.client = next
	c.mu.Unlock()
	if previous != nil {
		if transport, ok := previous.Transport.(*http.Transport); ok {
			transport.CloseIdleConnections()
		}
	}
	return nil
}

func (c *HTTPClient) do(req *http.Request) (*http.Response, error) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()
	if client == nil {
		return nil, fmt.Errorf("agent http client is not configured")
	}
	return client.Do(req)
}

func (c *HTTPClient) Health(ctx context.Context, baseURL string) (HealthResponse, error) {
	var out HealthResponse
	res, err := c.getResponse(ctx, baseURL, "/v1/health", nil)
	if err != nil {
		return out, err
	}
	defer res.Body.Close()
	if err := decodeAgentResponse(res, &out); err != nil {
		return HealthResponse{}, err
	}
	if res.TLS != nil && len(res.TLS.PeerCertificates) > 0 {
		cert := res.TLS.PeerCertificates[0]
		sum := sha256.Sum256(cert.Raw)
		out.Certificate = &CertificateInfo{
			Fingerprint: fmt.Sprintf("%X", sum[:]),
			CommonName:  cert.Subject.CommonName,
			NotBefore:   cert.NotBefore,
			NotAfter:    cert.NotAfter,
		}
	}
	return out, nil
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

func (c *HTTPClient) RuntimeStatus(ctx context.Context, baseURL, instanceID, containerName string) (RuntimeStatusResponse, error) {
	var out RuntimeStatusResponse
	query := url.Values{}
	if strings.TrimSpace(containerName) != "" {
		query.Set("containerName", containerName)
	}
	err := c.get(ctx, baseURL, "/v1/runtime/applications/"+url.PathEscape(instanceID)+"/status", query, &out)
	return out, err
}

func (c *HTTPClient) RuntimeLogs(ctx context.Context, baseURL, instanceID, containerName string, tail int) (RuntimeLogsResponse, error) {
	var out RuntimeLogsResponse
	query := url.Values{}
	if strings.TrimSpace(containerName) != "" {
		query.Set("containerName", containerName)
	}
	if tail > 0 {
		query.Set("tail", fmt.Sprintf("%d", tail))
	}
	err := c.get(ctx, baseURL, "/v1/runtime/applications/"+url.PathEscape(instanceID)+"/logs", query, &out)
	return out, err
}

func (c *HTTPClient) DockerContainers(ctx context.Context, baseURL string) ([]DockerContainer, error) {
	var out DockerContainersResponse
	err := c.get(ctx, baseURL, "/v1/docker/containers", nil, &out)
	return out.Items, err
}

func (c *HTTPClient) DockerContainerLogs(ctx context.Context, baseURL, id string, tail int) (DockerContainerLogsResponse, error) {
	var out DockerContainerLogsResponse
	query := url.Values{}
	if tail > 0 {
		query.Set("tail", fmt.Sprintf("%d", tail))
	}
	err := c.get(ctx, baseURL, "/v1/docker/containers/"+url.PathEscape(id)+"/logs", query, &out)
	return out, err
}

func (c *HTTPClient) DockerContainerAction(ctx context.Context, baseURL, id, action string) error {
	return c.post(ctx, baseURL, "/v1/docker/containers/"+url.PathEscape(id)+"/"+url.PathEscape(action), nil, nil)
}

func (c *HTTPClient) DockerContainerDelete(ctx context.Context, baseURL, id string) error {
	return c.delete(ctx, baseURL, "/v1/docker/containers/"+url.PathEscape(id))
}

func (c *HTTPClient) DockerImages(ctx context.Context, baseURL string) ([]DockerImage, error) {
	var out DockerImagesResponse
	err := c.get(ctx, baseURL, "/v1/docker/images", nil, &out)
	return out.Items, err
}

func (c *HTTPClient) DockerImagePull(ctx context.Context, baseURL, reference string) error {
	return c.post(ctx, baseURL, "/v1/docker/images/pull", DockerImagePullRequest{Reference: reference}, nil)
}

func (c *HTTPClient) DockerImageDelete(ctx context.Context, baseURL, id string) error {
	return c.delete(ctx, baseURL, "/v1/docker/images/"+url.PathEscape(id))
}

func (c *HTTPClient) DockerNetworks(ctx context.Context, baseURL string) ([]DockerNetwork, error) {
	var out DockerNetworksResponse
	err := c.get(ctx, baseURL, "/v1/docker/networks", nil, &out)
	return out.Items, err
}

func (c *HTTPClient) DockerVolumes(ctx context.Context, baseURL string) ([]DockerVolume, error) {
	var out DockerVolumesResponse
	err := c.get(ctx, baseURL, "/v1/docker/volumes", nil, &out)
	return out.Items, err
}

func (c *HTTPClient) DockerVolumeDelete(ctx context.Context, baseURL, name string) error {
	return c.delete(ctx, baseURL, "/v1/docker/volumes/"+url.PathEscape(name))
}

func (c *HTTPClient) get(ctx context.Context, baseURL, path string, query url.Values, out any) error {
	res, err := c.getResponse(ctx, baseURL, path, query)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return decodeAgentResponse(res, out)
}

func (c *HTTPClient) getResponse(ctx context.Context, baseURL, path string, query url.Values) (*http.Response, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/") + path)
	if err != nil {
		return nil, err
	}
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	res, err := c.do(req)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func decodeAgentResponse(res *http.Response, out any) error {
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
	if in != nil {
		if err := json.NewEncoder(&body).Encode(in); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.do(req)
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

func (c *HTTPClient) delete(ctx context.Context, baseURL, path string) error {
	u, err := url.Parse(strings.TrimRight(baseURL, "/") + path)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u.String(), nil)
	if err != nil {
		return err
	}
	res, err := c.do(req)
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
	return nil
}
