package agent

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"panel/internal/appruntime"
)

const defaultRuntimeRoot = "/opt/panel/apps"

type LocalRuntime struct {
	dockerHost string
	root       string
	client     *dockerAPIClient
}

func NewLocalRuntime(dockerHost string) (*LocalRuntime, error) {
	dockerHost = strings.TrimSpace(dockerHost)
	if dockerHost == "" {
		dockerHost = DefaultDockerHost
	}
	client, err := newDockerAPIClient(dockerHost)
	if err != nil {
		return nil, err
	}
	return &LocalRuntime{dockerHost: dockerHost, root: defaultRuntimeRoot, client: client}, nil
}

func (r *LocalRuntime) DockerHealth(ctx context.Context) DockerHealth {
	if r == nil || r.client == nil {
		return DockerHealth{Host: DefaultDockerHost, Status: StatusUnavailable, Error: "runtime is not configured"}
	}
	if err := r.client.ping(ctx); err != nil {
		return DockerHealth{Host: r.dockerHost, Status: StatusUnavailable, Error: err.Error()}
	}
	return DockerHealth{Host: r.dockerHost, Status: "ok"}
}

func (r *LocalRuntime) Deploy(ctx context.Context, req RuntimeDeployRequest) (RuntimeInstanceResponse, error) {
	if r == nil || r.client == nil {
		return RuntimeInstanceResponse{}, errors.New("runtime is not configured")
	}
	spec := req.Spec
	if strings.TrimSpace(spec.InstanceID) == "" {
		return RuntimeInstanceResponse{}, errors.New("instance id is required")
	}
	if strings.TrimSpace(spec.ContainerName) == "" {
		return RuntimeInstanceResponse{}, errors.New("container name is required")
	}
	if strings.TrimSpace(spec.Image) == "" {
		return RuntimeInstanceResponse{}, errors.New("image is required")
	}
	if err := r.writeManagedFiles(spec); err != nil {
		return RuntimeInstanceResponse{}, err
	}
	if err := r.client.pullImage(ctx, spec.Image); err != nil {
		return RuntimeInstanceResponse{}, err
	}
	if previous := strings.TrimSpace(req.PreviousContainerName); previous != "" && previous != spec.ContainerName {
		_ = r.client.removeContainer(ctx, previous, true)
	}
	_ = r.client.removeContainer(ctx, spec.ContainerName, true)
	id, err := r.client.createContainer(ctx, spec)
	if err != nil {
		return RuntimeInstanceResponse{}, err
	}
	if err := r.client.startContainer(ctx, id); err != nil {
		return RuntimeInstanceResponse{}, err
	}
	status, err := r.Status(ctx, spec.InstanceID, spec.ContainerName, req.ServerID)
	if err != nil {
		return RuntimeInstanceResponse{}, err
	}
	return RuntimeInstanceResponse{
		InstanceID:    spec.InstanceID,
		ContainerName: spec.ContainerName,
		ContainerID:   firstNonEmpty(status.ContainerID, id),
		Status:        status.Status,
		ObservedAt:    status.ObservedAt,
	}, nil
}

func (r *LocalRuntime) Stop(ctx context.Context, req RuntimeStopRequest) (RuntimeInstanceResponse, error) {
	if r == nil || r.client == nil {
		return RuntimeInstanceResponse{}, errors.New("runtime is not configured")
	}
	name := firstNonEmpty(req.ContainerName, containerNameForInstance(req.InstanceID))
	if err := r.client.stopContainer(ctx, name, 10); err != nil && !isDockerNotFound(err) {
		return RuntimeInstanceResponse{}, err
	}
	if req.Purge {
		if err := r.client.removeContainer(ctx, name, true); err != nil && !isDockerNotFound(err) {
			return RuntimeInstanceResponse{}, err
		}
	}
	status := appruntime.StatusStopped
	if req.Purge {
		status = "purged"
	}
	return RuntimeInstanceResponse{InstanceID: req.InstanceID, ContainerName: name, Status: status, ObservedAt: time.Now().UTC()}, nil
}

func (r *LocalRuntime) Restart(ctx context.Context, req RuntimeRestartRequest) (RuntimeInstanceResponse, error) {
	if r == nil || r.client == nil {
		return RuntimeInstanceResponse{}, errors.New("runtime is not configured")
	}
	name := firstNonEmpty(req.ContainerName, containerNameForInstance(req.InstanceID))
	if err := r.client.restartContainer(ctx, name, 10); err != nil {
		return RuntimeInstanceResponse{}, err
	}
	status, err := r.Status(ctx, req.InstanceID, name, "")
	if err != nil {
		return RuntimeInstanceResponse{}, err
	}
	return RuntimeInstanceResponse{InstanceID: req.InstanceID, ContainerName: name, ContainerID: status.ContainerID, Status: status.Status, ObservedAt: status.ObservedAt}, nil
}

func (r *LocalRuntime) Status(ctx context.Context, instanceID, containerName, serverID string) (appruntime.InstanceStatus, error) {
	if r == nil || r.client == nil {
		return appruntime.InstanceStatus{}, errors.New("runtime is not configured")
	}
	if containerName == "" {
		containerName = containerNameForInstance(instanceID)
	}
	inspect, err := r.client.inspectContainer(ctx, containerName)
	now := time.Now().UTC()
	if err != nil {
		if isDockerNotFound(err) {
			return appruntime.InstanceStatus{
				InstanceID:    instanceID,
				ServerID:      serverID,
				ContainerName: containerName,
				Status:        appruntime.StatusStopped,
				DesiredState:  appruntime.DesiredRunning,
				ObservedAt:    now,
			}, nil
		}
		return appruntime.InstanceStatus{}, err
	}
	status := dockerStateToRuntime(inspect.State.Status, inspect.State.Running, inspect.State.ExitCode)
	return appruntime.InstanceStatus{
		InstanceID:    instanceID,
		ServerID:      serverID,
		ContainerName: strings.TrimPrefix(inspect.Name, "/"),
		ContainerID:   inspect.ID,
		Status:        status,
		DesiredState:  appruntime.DesiredRunning,
		Image:         inspect.Config.Image,
		StartedAt:     inspect.State.StartedAt,
		FinishedAt:    inspect.State.FinishedAt,
		ExitCode:      inspect.State.ExitCode,
		LastError:     inspect.State.Error,
		ObservedAt:    now,
	}, nil
}

func (r *LocalRuntime) Logs(ctx context.Context, instanceID, containerName string, tail int) (string, error) {
	if r == nil || r.client == nil {
		return "", errors.New("runtime is not configured")
	}
	return r.client.containerLogs(ctx, firstNonEmpty(containerName, containerNameForInstance(instanceID)), normalizeLogTail(tail))
}

func (r *LocalRuntime) Containers(ctx context.Context) ([]DockerContainer, error) {
	if r == nil || r.client == nil {
		return nil, errors.New("runtime is not configured")
	}
	return r.client.listContainers(ctx)
}

func (r *LocalRuntime) ContainerLogs(ctx context.Context, id string, tail int) (string, error) {
	if r == nil || r.client == nil {
		return "", errors.New("runtime is not configured")
	}
	return r.client.containerLogs(ctx, id, normalizeLogTail(tail))
}

func (r *LocalRuntime) PersistentArchive(ctx context.Context, applicationID string) ([]byte, error) {
	if r == nil {
		return nil, errors.New("runtime is not configured")
	}
	dir, err := safeApplicationRuntimeDir(r.root, applicationID, "persistent")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	err = filepath.WalkDir(dir, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filePath == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, filePath)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if entry.IsDir() {
			_, err := zw.Create(name + "/")
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = name
		header.Method = zip.Deflate
		writer, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		file, err := os.Open(filePath)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(writer, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if closeErr := zw.Close(); err == nil {
		err = closeErr
	}
	_ = ctx
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (r *LocalRuntime) ContainerStart(ctx context.Context, id string) error {
	return r.client.startContainer(ctx, id)
}

func (r *LocalRuntime) ContainerStop(ctx context.Context, id string) error {
	err := r.client.stopContainer(ctx, id, 10)
	if isDockerNotModified(err) || isDockerNotFound(err) {
		return nil
	}
	return err
}

func (r *LocalRuntime) ContainerRestart(ctx context.Context, id string) error {
	return r.client.restartContainer(ctx, id, 10)
}

func (r *LocalRuntime) ContainerDelete(ctx context.Context, id string) error {
	err := r.client.removeContainer(ctx, id, true)
	if isDockerNotFound(err) {
		return nil
	}
	return err
}

func (r *LocalRuntime) Images(ctx context.Context) ([]DockerImage, error) {
	return r.client.listImages(ctx)
}

func (r *LocalRuntime) PullImage(ctx context.Context, reference string) error {
	return r.client.pullImage(ctx, reference)
}

func (r *LocalRuntime) DeleteImage(ctx context.Context, id string) error {
	return r.client.removeImage(ctx, id)
}

func (r *LocalRuntime) Networks(ctx context.Context) ([]DockerNetwork, error) {
	return r.client.listNetworks(ctx)
}

func (r *LocalRuntime) Volumes(ctx context.Context) ([]DockerVolume, error) {
	return r.client.listVolumes(ctx)
}

func (r *LocalRuntime) DeleteVolume(ctx context.Context, name string) error {
	return r.client.removeVolume(ctx, name)
}

func (r *LocalRuntime) writeManagedFiles(spec appruntime.Spec) error {
	for _, file := range spec.Files {
		target, err := safeRuntimePath(r.root, spec.ApplicationID, spec.InstanceID, "files", file.Path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		mode := os.FileMode(0o600)
		if file.Mode == "0644" {
			mode = 0o644
		}
		if err := os.WriteFile(target, file.Content, mode); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Join(r.root, spec.ApplicationID, "persistent"), 0o700); err != nil {
		return err
	}
	return nil
}

func safeRuntimePath(root, appID, instanceID, area, rel string) (string, error) {
	rel = path.Clean(strings.TrimPrefix(rel, "/"))
	if rel == "." || strings.HasPrefix(rel, "../") || rel == ".." {
		return "", errors.New("runtime file path must stay inside the application workspace")
	}
	base := filepath.Join(root, appID, "instances", instanceID, area)
	target := filepath.Join(base, filepath.FromSlash(rel))
	cleanBase, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	cleanTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if cleanTarget != cleanBase && !strings.HasPrefix(cleanTarget, cleanBase+string(os.PathSeparator)) {
		return "", errors.New("runtime file path escapes the application workspace")
	}
	return cleanTarget, nil
}

func safeApplicationRuntimeDir(root, appID, area string) (string, error) {
	appID = strings.TrimSpace(appID)
	area = strings.TrimSpace(area)
	if appID == "" || strings.ContainsAny(appID, `/\`) || area == "" || strings.ContainsAny(area, `/\`) {
		return "", errors.New("runtime application path is invalid")
	}
	base := filepath.Join(root, appID)
	target := filepath.Join(base, area)
	cleanBase, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	cleanTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if cleanTarget != cleanBase && !strings.HasPrefix(cleanTarget, cleanBase+string(os.PathSeparator)) {
		return "", errors.New("runtime application path escapes the application workspace")
	}
	return cleanTarget, nil
}

func containerNameForInstance(instanceID string) string {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return "panel-unknown"
	}
	if strings.HasPrefix(instanceID, "panel-") {
		return instanceID
	}
	return "panel-" + sanitizeContainerPart(instanceID)
}

type dockerAPIClient struct {
	host   string
	client *http.Client
}

func newDockerAPIClient(host string) (*dockerAPIClient, error) {
	u, err := url.Parse(host)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "unix":
		socketPath := u.Path
		if socketPath == "" {
			socketPath = strings.TrimPrefix(host, "unix://")
		}
		if socketPath == "" {
			socketPath = "/var/run/docker.sock"
		}
		transport := &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		}
		return &dockerAPIClient{host: host, client: &http.Client{Transport: transport, Timeout: 2 * time.Minute}}, nil
	case "http", "https":
		return &dockerAPIClient{host: host, client: &http.Client{Timeout: 2 * time.Minute}}, nil
	default:
		return nil, fmt.Errorf("unsupported docker host %q", host)
	}
}

func (c *dockerAPIClient) ping(ctx context.Context) error {
	req, err := c.newRequest(ctx, http.MethodGet, "/_ping", nil)
	if err != nil {
		return err
	}
	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("docker ping failed: %s", res.Status)
	}
	return nil
}

func (c *dockerAPIClient) pullImage(ctx context.Context, image string) error {
	query := url.Values{}
	query.Set("fromImage", image)
	req, err := c.newRequest(ctx, http.MethodPost, "/images/create?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return dockerError(res, "pull image")
	}
	_, _ = io.Copy(io.Discard, res.Body)
	return nil
}

func (c *dockerAPIClient) createContainer(ctx context.Context, spec appruntime.Spec) (string, error) {
	payload := dockerCreateRequest{
		Image:        spec.Image,
		Env:          dockerEnv(spec.Env),
		Cmd:          append([]string(nil), spec.Args...),
		Entrypoint:   append([]string(nil), spec.Command...),
		ExposedPorts: dockerExposedPorts(spec.Ports),
		Labels: map[string]string{
			"panel.application.managed":     "true",
			"panel.application.id":          spec.ApplicationID,
			"panel.application.instance.id": spec.InstanceID,
			"panel.application.generation":  strconv.Itoa(spec.Generation),
			"panel.application.spec.hash":   spec.SpecHash,
		},
		HostConfig: dockerHostConfig{
			Binds:        dockerBinds(defaultRuntimeRoot, spec),
			PortBindings: dockerPortBindings(spec.Ports),
			NetworkMode:  spec.NetworkMode,
			Privileged:   spec.Privileged,
			RestartPolicy: dockerRestartPolicy{
				Name: dockerRestartName(spec.Restart.Policy),
			},
		},
	}
	if spec.Resources.MemoryMB > 0 {
		payload.HostConfig.Memory = int64(spec.Resources.MemoryMB) * 1024 * 1024
	}
	if spec.Resources.CPU > 0 {
		payload.HostConfig.NanoCPUs = int64(spec.Resources.CPU) * 1_000_000
	}
	query := url.Values{}
	query.Set("name", spec.ContainerName)
	body, _ := json.Marshal(payload)
	req, err := c.newRequest(ctx, http.MethodPost, "/containers/create?"+query.Encode(), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", dockerError(res, "create container")
	}
	var out struct {
		ID string `json:"Id"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.ID, nil
}

func (c *dockerAPIClient) startContainer(ctx context.Context, id string) error {
	err := c.emptyPost(ctx, "/containers/"+url.PathEscape(id)+"/start", "start container")
	if isDockerNotModified(err) {
		return nil
	}
	return err
}

func (c *dockerAPIClient) stopContainer(ctx context.Context, name string, timeout int) error {
	query := url.Values{}
	query.Set("t", strconv.Itoa(timeout))
	return c.emptyPost(ctx, "/containers/"+url.PathEscape(name)+"/stop?"+query.Encode(), "stop container")
}

func (c *dockerAPIClient) restartContainer(ctx context.Context, name string, timeout int) error {
	query := url.Values{}
	query.Set("t", strconv.Itoa(timeout))
	return c.emptyPost(ctx, "/containers/"+url.PathEscape(name)+"/restart?"+query.Encode(), "restart container")
}

func (c *dockerAPIClient) removeContainer(ctx context.Context, name string, force bool) error {
	query := url.Values{}
	if force {
		query.Set("force", "true")
	}
	query.Set("v", "true")
	req, err := c.newRequest(ctx, http.MethodDelete, "/containers/"+url.PathEscape(name)+"?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return dockerNotFound{name}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return dockerError(res, "remove container")
	}
	return nil
}

func (c *dockerAPIClient) inspectContainer(ctx context.Context, name string) (dockerInspectResponse, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/containers/"+url.PathEscape(name)+"/json", nil)
	if err != nil {
		return dockerInspectResponse{}, err
	}
	res, err := c.client.Do(req)
	if err != nil {
		return dockerInspectResponse{}, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return dockerInspectResponse{}, dockerNotFound{name}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return dockerInspectResponse{}, dockerError(res, "inspect container")
	}
	var out dockerInspectResponse
	return out, json.NewDecoder(res.Body).Decode(&out)
}

func (c *dockerAPIClient) containerLogs(ctx context.Context, name string, tail int) (string, error) {
	query := url.Values{}
	query.Set("stdout", "true")
	query.Set("stderr", "true")
	query.Set("tail", strconv.Itoa(tail))
	req, err := c.newRequest(ctx, http.MethodGet, "/containers/"+url.PathEscape(name)+"/logs?"+query.Encode(), nil)
	if err != nil {
		return "", err
	}
	res, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return "", dockerNotFound{name}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", dockerError(res, "read container logs")
	}
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	return decodeDockerLogs(raw), nil
}

func (c *dockerAPIClient) emptyPost(ctx context.Context, endpoint, action string) error {
	req, err := c.newRequest(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return dockerNotFound{endpoint}
	}
	if res.StatusCode == http.StatusNotModified {
		return dockerNotModified{endpoint}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return dockerError(res, action)
	}
	return nil
}

func (c *dockerAPIClient) listContainers(ctx context.Context) ([]DockerContainer, error) {
	var out []DockerContainer
	if err := c.getJSON(ctx, "/containers/json?all=true", "list containers", &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []DockerContainer{}
	}
	return out, nil
}

func (c *dockerAPIClient) listImages(ctx context.Context) ([]DockerImage, error) {
	var out []DockerImage
	if err := c.getJSON(ctx, "/images/json?all=true", "list images", &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []DockerImage{}
	}
	return out, nil
}

func (c *dockerAPIClient) removeImage(ctx context.Context, id string) error {
	query := url.Values{}
	query.Set("force", "false")
	req, err := c.newRequest(ctx, http.MethodDelete, "/images/"+url.PathEscape(id)+"?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return nil
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return dockerError(res, "remove image")
	}
	return nil
}

func (c *dockerAPIClient) listNetworks(ctx context.Context) ([]DockerNetwork, error) {
	var out []DockerNetwork
	if err := c.getJSON(ctx, "/networks", "list networks", &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []DockerNetwork{}
	}
	return out, nil
}

func (c *dockerAPIClient) listVolumes(ctx context.Context) ([]DockerVolume, error) {
	var raw struct {
		Volumes []DockerVolume `json:"Volumes"`
	}
	if err := c.getJSON(ctx, "/volumes", "list volumes", &raw); err != nil {
		return nil, err
	}
	containers, err := c.listContainers(ctx)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, container := range containers {
		for _, mount := range container.Mounts {
			if mount.Type == "volume" && mount.Name != "" {
				counts[mount.Name]++
			}
		}
	}
	for i := range raw.Volumes {
		raw.Volumes[i].ContainerCount = counts[raw.Volumes[i].Name]
		raw.Volumes[i].InUse = raw.Volumes[i].ContainerCount > 0
	}
	if raw.Volumes == nil {
		raw.Volumes = []DockerVolume{}
	}
	return raw.Volumes, nil
}

func (c *dockerAPIClient) removeVolume(ctx context.Context, name string) error {
	req, err := c.newRequest(ctx, http.MethodDelete, "/volumes/"+url.PathEscape(name), nil)
	if err != nil {
		return err
	}
	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return nil
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return dockerError(res, "remove volume")
	}
	return nil
}

func (c *dockerAPIClient) getJSON(ctx context.Context, endpoint, action string, out any) error {
	req, err := c.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return dockerError(res, action)
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func (c *dockerAPIClient) newRequest(ctx context.Context, method, endpoint string, body io.Reader) (*http.Request, error) {
	target := "http://docker" + endpoint
	if strings.HasPrefix(c.host, "http://") || strings.HasPrefix(c.host, "https://") {
		target = strings.TrimRight(c.host, "/") + endpoint
	}
	return http.NewRequestWithContext(ctx, method, target, body)
}

type dockerCreateRequest struct {
	Image        string              `json:"Image"`
	Env          []string            `json:"Env,omitempty"`
	Cmd          []string            `json:"Cmd,omitempty"`
	Entrypoint   []string            `json:"Entrypoint,omitempty"`
	ExposedPorts map[string]struct{} `json:"ExposedPorts,omitempty"`
	Labels       map[string]string   `json:"Labels,omitempty"`
	HostConfig   dockerHostConfig    `json:"HostConfig"`
}

type dockerHostConfig struct {
	Binds         []string                       `json:"Binds,omitempty"`
	PortBindings  map[string][]map[string]string `json:"PortBindings,omitempty"`
	NetworkMode   string                         `json:"NetworkMode,omitempty"`
	Privileged    bool                           `json:"Privileged,omitempty"`
	RestartPolicy dockerRestartPolicy            `json:"RestartPolicy,omitempty"`
	Memory        int64                          `json:"Memory,omitempty"`
	NanoCPUs      int64                          `json:"NanoCpus,omitempty"`
}

type dockerRestartPolicy struct {
	Name string `json:"Name,omitempty"`
}

type dockerInspectResponse struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	Config struct {
		Image string `json:"Image"`
	} `json:"Config"`
	State struct {
		Status     string `json:"Status"`
		Running    bool   `json:"Running"`
		ExitCode   int    `json:"ExitCode"`
		Error      string `json:"Error"`
		StartedAt  string `json:"StartedAt"`
		FinishedAt string `json:"FinishedAt"`
	} `json:"State"`
}

func dockerEnv(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for key, value := range env {
		out = append(out, key+"="+value)
	}
	return out
}

func dockerExposedPorts(ports []appruntime.Port) map[string]struct{} {
	out := map[string]struct{}{}
	for _, port := range ports {
		if port.ContainerPort <= 0 {
			continue
		}
		out[dockerPortKey(port)] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func dockerPortBindings(ports []appruntime.Port) map[string][]map[string]string {
	out := map[string][]map[string]string{}
	for _, port := range ports {
		if port.ContainerPort <= 0 || port.HostPort <= 0 {
			continue
		}
		out[dockerPortKey(port)] = []map[string]string{{"HostPort": strconv.Itoa(port.HostPort)}}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func dockerPortKey(port appruntime.Port) string {
	proto := strings.TrimSpace(port.Protocol)
	if proto == "" {
		proto = "tcp"
	}
	return strconv.Itoa(port.ContainerPort) + "/" + proto
}

func dockerBinds(root string, spec appruntime.Spec) []string {
	binds := []string{}
	for _, mount := range spec.Mounts {
		if strings.TrimSpace(mount.Target) == "" || strings.TrimSpace(mount.Source) == "" {
			continue
		}
		source := mount.Source
		switch mount.Type {
		case "managed_file":
			source = filepath.Join(root, spec.ApplicationID, "instances", spec.InstanceID, "files", filepath.FromSlash(path.Clean(strings.TrimPrefix(mount.Source, "/"))))
		case "persistent":
			source = mount.Source
		}
		mode := "rw"
		if mount.ReadOnly {
			mode = "ro"
		}
		binds = append(binds, source+":"+mount.Target+":"+mode)
	}
	return binds
}

func dockerRestartName(policy string) string {
	switch strings.TrimSpace(policy) {
	case "always":
		return "always"
	case "unless-stopped":
		return "unless-stopped"
	case "on-failure":
		return "on-failure"
	default:
		return "no"
	}
}

func dockerStateToRuntime(status string, running bool, exitCode int) string {
	if running || status == "running" {
		return appruntime.StatusRunning
	}
	switch status {
	case "created", "restarting", "paused":
		return appruntime.StatusPending
	case "exited":
		if exitCode == 0 {
			return appruntime.StatusStopped
		}
		return appruntime.StatusFailed
	case "dead":
		return appruntime.StatusFailed
	default:
		if status == "" {
			return appruntime.StatusUnknown
		}
		return status
	}
}

type dockerErrorResponse struct {
	Message string `json:"message"`
	Error   string `json:"error"`
}

func dockerError(res *http.Response, action string) error {
	var er dockerErrorResponse
	raw, _ := io.ReadAll(res.Body)
	_ = json.Unmarshal(raw, &er)
	msg := firstNonEmpty(er.Message, er.Error, strings.TrimSpace(string(raw)), res.Status)
	return fmt.Errorf("%s failed: %s", action, msg)
}

type dockerNotFound struct{ name string }

func (e dockerNotFound) Error() string { return "docker container not found: " + e.name }

func isDockerNotFound(err error) bool {
	var nf dockerNotFound
	return errors.As(err, &nf)
}

type dockerNotModified struct{ name string }

func (e dockerNotModified) Error() string {
	return "docker resource already has requested state: " + e.name
}

func isDockerNotModified(err error) bool {
	var target dockerNotModified
	return errors.As(err, &target)
}

func decodeDockerLogs(raw []byte) string {
	if len(raw) < 8 {
		return string(raw)
	}
	var out bytes.Buffer
	for len(raw) >= 8 {
		size := int(binary.BigEndian.Uint32(raw[4:8]))
		if size < 0 || len(raw) < 8+size {
			return string(raw)
		}
		out.Write(raw[8 : 8+size])
		raw = raw[8+size:]
	}
	if len(raw) > 0 {
		out.Write(raw)
	}
	return out.String()
}

func sanitizeContainerPart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('-')
	}
	out := strings.Trim(b.String(), "-_.")
	if out == "" {
		return "runtime"
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func normalizeLogTail(tail int) int {
	if tail <= 0 {
		return 200
	}
	if tail > 10000 {
		return 10000
	}
	return tail
}
