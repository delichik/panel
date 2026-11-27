package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	appruntime "panel/internal/modules/applications/runtime"
)

const (
	imageProbePurposePreflight = "preflight"
	imageProbePurposeVersion   = "version"

	// labelImageProbe marks disposable containers created only to validate a
	// pulled image before it replaces a serving container. Panel container
	// inventory ignores them because they never carry the managed-application
	// label; the label lets a later reconcile recognize and remove a leftover
	// probe container after an agent crash between create and cleanup.
	labelImageProbe = "panel.agent.image.probe"

	// imageProbeTimeout bounds one disposable probe from create to exit. Real
	// checks (`nginx -t`, `nginx -v`) finish in milliseconds; the bound stops a
	// misbehaving image or a command that never exits from blocking a deploy.
	imageProbeTimeout = 60 * time.Second
	// imageProbeCleanupTimeout bounds the force removal of a probe container. It
	// runs detached from the reconcile context so a cancelled RPC cannot leak
	// the temporary container.
	imageProbeCleanupTimeout = 30 * time.Second
	// imageProbeLogTimeout bounds the best-effort log read after the probe exited.
	imageProbeLogTimeout = 10 * time.Second
	imageProbeLogTail    = 100
	// imageProbeOutputRunes bounds the diagnostic text returned to Panel.
	imageProbeOutputRunes = 2048
)

// imageProbeResult is the outcome of one disposable container command run.
type imageProbeResult struct {
	ExitCode int
	Output   string
}

// imageProbeContainerName derives the deterministic disposable container name
// for one purpose. The name is a child of the application container name so a
// leftover container from a crashed reconcile can be found and removed without
// scanning every container label.
func imageProbeContainerName(containerName, purpose string) string {
	base := strings.TrimSpace(containerName)
	if base == "" {
		base = "panel-runtime"
	}
	return sanitizeContainerPart(base+"-"+purpose) + "-probe"
}

// runImageProbe creates a disposable container from the desired spec, runs one
// command in it and removes it again. It never publishes ports and never joins
// the managed-application identity, so the probe cannot appear as an application
// instance. Mounts, environment, network mode and capabilities mirror the spec
// so `nginx -t` sees exactly the configuration the real container would get.
func (r *LocalRuntime) runImageProbe(ctx context.Context, spec appruntime.Spec, purpose string, command []string) (imageProbeResult, error) {
	if r == nil || r.client == nil {
		return imageProbeResult{}, errors.New("runtime is not configured")
	}
	if len(command) == 0 {
		return imageProbeResult{}, errors.New("image probe command is required")
	}
	if err := r.client.ensureManagedNetwork(ctx); err != nil {
		return imageProbeResult{}, fmt.Errorf("prepare probe network: %w", err)
	}
	// Bind mounts for NFS volumes resolve to docker volume names; create the
	// expected volumes first so Docker cannot auto-create a shadowing local
	// volume for an NFS mount that the real container would use.
	if err := r.prepareNFSVolumes(ctx, spec); err != nil {
		return imageProbeResult{}, fmt.Errorf("prepare probe volumes: %w", err)
	}
	name := imageProbeContainerName(spec.ContainerName, purpose)
	id, err := r.client.createProbeContainer(ctx, spec, name, purpose, command)
	if err != nil && isDockerNameConflict(err) {
		// A previous process may have crashed between create and cleanup. Only
		// remove the occupant when it is proven to be one of our probes.
		occupant, inspectErr := r.client.inspectContainer(ctx, name)
		if inspectErr != nil {
			return imageProbeResult{}, fmt.Errorf("inspect leftover probe container %s: %w", name, inspectErr)
		}
		if strings.TrimSpace(occupant.Config.Labels[labelImageProbe]) == "" {
			return imageProbeResult{}, fmt.Errorf("container name %s is in use by a non-probe container", name)
		}
		if removeErr := r.client.removeContainer(ctx, firstNonEmpty(occupant.ID, name), true); removeErr != nil && !isDockerNotFound(removeErr) {
			return imageProbeResult{}, fmt.Errorf("remove leftover probe container %s: %w", name, removeErr)
		}
		id, err = r.client.createProbeContainer(ctx, spec, name, purpose, command)
	}
	if err != nil {
		return imageProbeResult{}, fmt.Errorf("create image probe container: %w", err)
	}
	defer func() {
		// The probe is disposable: always remove it, even when the caller's
		// context was cancelled mid-probe.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), imageProbeCleanupTimeout)
		defer cancel()
		_ = r.client.removeContainer(cleanupCtx, id, true)
	}()

	probeCtx, cancel := context.WithTimeout(ctx, imageProbeTimeout)
	defer cancel()
	if err := r.client.startContainer(probeCtx, id); err != nil {
		return imageProbeResult{}, fmt.Errorf("start image probe container: %w", err)
	}
	exitCode, err := r.client.waitContainerExit(probeCtx, id)
	if err != nil {
		return imageProbeResult{}, fmt.Errorf("wait for image probe container: %w", err)
	}
	return imageProbeResult{ExitCode: exitCode, Output: r.imageProbeOutput(probeCtx, id, spec)}, nil
}

// imageProbeOutput reads the bounded, redacted log text of an exited probe.
// Log capture is best effort: a probe command may exit non-zero without any
// output, and that is still a meaningful result.
func (r *LocalRuntime) imageProbeOutput(ctx context.Context, containerID string, spec appruntime.Spec) string {
	logCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), imageProbeLogTimeout)
	defer cancel()
	query := url.Values{
		"stdout": {"true"},
		"stderr": {"true"},
		"tail":   {strconv.Itoa(imageProbeLogTail)},
	}
	req, err := r.client.newRequest(logCtx, http.MethodGet, "/containers/"+url.PathEscape(containerID)+"/logs?"+query.Encode(), nil)
	if err != nil {
		return ""
	}
	res, err := r.client.client.Do(req)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return ""
	}
	text, truncated, err := readStartupLogs(res.Body)
	if err != nil {
		return ""
	}
	text = redactStartupLogs(text, spec)
	if truncated {
		text += "\n[truncated]"
	}
	return boundImageProbeOutput(text)
}

func boundImageProbeOutput(text string) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= imageProbeOutputRunes {
		return text
	}
	return string(runes[:imageProbeOutputRunes]) + "…"
}

// createProbeContainer creates a disposable container that runs exactly one
// command. Ports are deliberately neither exposed nor published: a preflight
// must never take over a host port from the serving container.
func (c *dockerAPIClient) createProbeContainer(ctx context.Context, spec appruntime.Spec, name, purpose string, command []string) (string, error) {
	binds, err := dockerBinds(defaultRuntimeRoot, spec)
	if err != nil {
		return "", err
	}
	payload := dockerCreateRequest{
		Image: spec.Image,
		Env:   dockerEnv(spec.Env),
		Cmd:   append([]string(nil), command...),
		Labels: map[string]string{
			labelImageProbe: purpose,
		},
		HostConfig: dockerHostConfig{
			Binds:       binds,
			NetworkMode: managedBridgeNetwork,
			Privileged:  spec.Privileged,
			CapAdd:      append([]string(nil), spec.CapAdd...),
		},
	}
	query := url.Values{}
	query.Set("name", name)
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
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
		return "", dockerError(res, "create image probe container")
	}
	var out struct {
		ID string `json:"Id"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return "", err
	}
	if strings.TrimSpace(out.ID) == "" {
		return "", errors.New("docker create image probe container returned an empty id")
	}
	return out.ID, nil
}

// waitContainerExit blocks until the container exits and returns its exit code.
// Unlike the runtime's long-lived verify polling, a probe is expected to be
// short-lived, so the Docker wait endpoint is the exact and cheapest signal.
func (c *dockerAPIClient) waitContainerExit(ctx context.Context, id string) (int, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/wait", nil)
	if err != nil {
		return 0, err
	}
	res, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return 0, dockerNotFound{id}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return 0, dockerError(res, "wait for container")
	}
	var out struct {
		StatusCode int `json:"StatusCode"`
		Error      *struct {
			Message string `json:"Message"`
		} `json:"Error"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, int64(maxRuntimeCommandOutput))).Decode(&out); err != nil {
		return 0, err
	}
	if out.Error != nil && strings.TrimSpace(out.Error.Message) != "" {
		return out.StatusCode, errors.New("docker wait failed: " + out.Error.Message)
	}
	return out.StatusCode, nil
}
