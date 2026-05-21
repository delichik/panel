package docker

import (
	"bufio"
	"context"
	"encoding/json"
	"strings"
	"time"

	"panel/internal/panelerr"
	"panel/internal/sshx"
)

const commandTimeout = 20 * time.Second

type CLIRuntime struct {
	exec sshx.RemoteExecutor
}

func NewCLIRuntime(exec sshx.RemoteExecutor) *CLIRuntime {
	return &CLIRuntime{exec: exec}
}

func (r *CLIRuntime) Detect(ctx context.Context, target sshx.Target) (DockerCapability, error) {
	now := time.Now().UTC()
	cap := DockerCapability{ServerID: target.ServerID, LastCheckedAt: &now}
	res, err := r.exec.Exec(ctx, target, sshx.CommandSpec{Command: detectCommand, Timeout: commandTimeout})
	if err != nil {
		cap.LastError = "docker capability check failed"
		return cap, err
	}
	values := parseKeyValues(res.Stdout)
	cap.DockerInstalled = values["docker_installed"] == "true"
	cap.DockerVersion = values["docker_version"]
	cap.ComposeInstalled = values["compose_installed"] == "true"
	cap.ComposeVersion = values["compose_version"]
	daemonReachable := values["docker_daemon_reachable"] == "true"
	cap.Supported = cap.DockerInstalled && cap.ComposeInstalled && daemonReachable
	if !cap.DockerInstalled {
		cap.LastError = "docker is not installed or not available in PATH"
	} else if !daemonReachable {
		cap.LastError = "docker daemon is not reachable by this SSH user"
	} else if !cap.ComposeInstalled {
		cap.LastError = "docker compose plugin is not installed or not available"
	}
	return cap, nil
}

func (r *CLIRuntime) ListComposeProjects(ctx context.Context, target sshx.Target) ([]ComposeProject, error) {
	res, err := r.exec.Exec(ctx, target, sshx.CommandSpec{Command: `docker compose ls --all --format json`, Timeout: commandTimeout})
	if err != nil {
		return nil, dockerCommandError("docker_compose_projects_failed", res)
	}
	var rows []struct {
		Name        string `json:"Name"`
		Status      string `json:"Status"`
		ConfigFiles string `json:"ConfigFiles"`
	}
	if strings.TrimSpace(res.Stdout) == "" {
		return []ComposeProject{}, nil
	}
	if err := json.Unmarshal([]byte(res.Stdout), &rows); err != nil {
		return nil, panelerr.BadGateway("docker_parse_failed", "Failed to parse Docker Compose projects")
	}
	out := make([]ComposeProject, 0, len(rows))
	for _, row := range rows {
		out = append(out, ComposeProject{Name: row.Name, Status: row.Status, ConfigFiles: row.ConfigFiles})
	}
	return out, nil
}

func (r *CLIRuntime) ListServices(ctx context.Context, target sshx.Target) ([]RuntimeService, error) {
	res, err := r.exec.Exec(ctx, target, sshx.CommandSpec{Command: `docker ps -a --format '{{json .}}'`, Timeout: commandTimeout})
	if err != nil {
		return nil, dockerCommandError("docker_services_failed", res)
	}
	return ParseServices(res.Stdout)
}

func (r *CLIRuntime) ListNetworks(ctx context.Context, target sshx.Target) ([]RuntimeNetwork, error) {
	res, err := r.exec.Exec(ctx, target, sshx.CommandSpec{Command: `docker network ls --format '{{json .}}'`, Timeout: commandTimeout})
	if err != nil {
		return nil, dockerCommandError("docker_networks_failed", res)
	}
	return ParseNetworks(res.Stdout)
}

func (r *CLIRuntime) ListVolumes(ctx context.Context, target sshx.Target) ([]RuntimeVolume, error) {
	res, err := r.exec.Exec(ctx, target, sshx.CommandSpec{Command: `docker volume ls --format '{{json .}}'`, Timeout: commandTimeout})
	if err != nil {
		return nil, dockerCommandError("docker_volumes_failed", res)
	}
	return ParseVolumes(res.Stdout)
}

func (r *CLIRuntime) ListImages(ctx context.Context, target sshx.Target) ([]RuntimeImage, error) {
	res, err := r.exec.Exec(ctx, target, sshx.CommandSpec{Command: `docker image ls --digests --format '{{json .}}'`, Timeout: commandTimeout})
	if err != nil {
		return nil, dockerCommandError("docker_images_failed", res)
	}
	return ParseImages(res.Stdout)
}

func (r *CLIRuntime) ReadComposeStatus(ctx context.Context, target sshx.Target, project string) (ComposeStatus, error) {
	project = strings.TrimSpace(project)
	if project == "" {
		return ComposeStatus{}, panelerr.Validation("project_required", "Project name is required")
	}
	res, err := r.exec.Exec(ctx, target, sshx.CommandSpec{Command: `docker compose -p ` + shellQuote(project) + ` ps -a --format json`, Timeout: commandTimeout})
	if err != nil {
		return ComposeStatus{}, dockerCommandError("docker_compose_status_failed", res)
	}
	services, err := parseComposeStatusServices(res.Stdout, project)
	if err != nil {
		return ComposeStatus{}, err
	}
	state := "empty"
	for _, svc := range services {
		if svc.State == "running" {
			state = "running"
			break
		}
		state = svc.State
	}
	return ComposeStatus{Project: project, State: state, Services: services, CheckedAt: time.Now().UTC()}, nil
}

func (r *CLIRuntime) DeleteNetwork(ctx context.Context, target sshx.Target, networkID string) error {
	return r.runDockerMutation(ctx, target, "docker_network_delete_failed", `docker network rm `+shellQuote(networkID))
}

func (r *CLIRuntime) DeleteVolume(ctx context.Context, target sshx.Target, volumeID string) error {
	return r.runDockerMutation(ctx, target, "docker_volume_delete_failed", `docker volume rm `+shellQuote(volumeID))
}

func (r *CLIRuntime) DeleteImage(ctx context.Context, target sshx.Target, imageID string) error {
	return r.runDockerMutation(ctx, target, "docker_image_delete_failed", `docker image rm `+shellQuote(imageID))
}

func (r *CLIRuntime) PruneNetworks(ctx context.Context, target sshx.Target) error {
	return r.runDockerMutation(ctx, target, "docker_network_prune_failed", `docker network prune -f`)
}

func (r *CLIRuntime) PruneVolumes(ctx context.Context, target sshx.Target) error {
	return r.runDockerMutation(ctx, target, "docker_volume_prune_failed", `docker volume prune -f`)
}

func (r *CLIRuntime) PruneImages(ctx context.Context, target sshx.Target) error {
	return r.runDockerMutation(ctx, target, "docker_image_prune_failed", `docker image prune -f`)
}

func (r *CLIRuntime) CheckImageUpdate(ctx context.Context, target sshx.Target, image RuntimeImage) (ImageUpdate, error) {
	now := time.Now().UTC()
	update := ImageUpdate{
		ImageID:       image.ID,
		Repository:    image.Repository,
		Tag:           image.Tag,
		CurrentDigest: image.Digest,
		CheckedAt:     &now,
	}
	if image.Repository == "" || image.Repository == "<none>" || image.Tag == "" || image.Tag == "<none>" {
		update.LastError = "image has no repository/tag"
		return update, nil
	}
	res, err := r.exec.Exec(ctx, target, sshx.CommandSpec{Command: `docker manifest inspect ` + shellQuote(image.Repository+":"+image.Tag), Timeout: commandTimeout})
	if err != nil {
		update.LastError = dockerCommandError("docker_image_update_check_failed", res).Error()
		return update, nil
	}
	update.LatestDigest = manifestDigest(res.Stdout)
	update.UpdateAvailable = update.CurrentDigest != "" && update.LatestDigest != "" && update.CurrentDigest != update.LatestDigest
	return update, nil
}

func (r *CLIRuntime) PullImage(ctx context.Context, target sshx.Target, repository, tag string) error {
	if strings.TrimSpace(repository) == "" || repository == "<none>" || strings.TrimSpace(tag) == "" || tag == "<none>" {
		return panelerr.Validation("docker_image_invalid", "Image repository and tag are required")
	}
	return r.runDockerMutation(ctx, target, "docker_image_pull_failed", `docker pull `+shellQuote(repository+":"+tag))
}

func (r *CLIRuntime) runDockerMutation(ctx context.Context, target sshx.Target, code, command string) error {
	res, err := r.exec.Exec(ctx, target, sshx.CommandSpec{Command: command, Timeout: commandTimeout})
	if err != nil {
		return dockerCommandError(code, res)
	}
	return nil
}

func manifestDigest(raw string) string {
	var payload any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return ""
	}
	return findDigest(payload)
}

func findDigest(v any) string {
	switch x := v.(type) {
	case map[string]any:
		if descriptor, ok := x["Descriptor"]; ok {
			if digest := findDigest(descriptor); digest != "" {
				return digest
			}
		}
		if digest, ok := x["digest"].(string); ok && strings.HasPrefix(digest, "sha256:") {
			return digest
		}
		for _, value := range x {
			if digest := findDigest(value); digest != "" {
				return digest
			}
		}
	case []any:
		for _, value := range x {
			if digest := findDigest(value); digest != "" {
				return digest
			}
		}
	}
	return ""
}

func ParseServices(raw string) ([]RuntimeService, error) {
	var out []RuntimeService
	err := parseJSONLines(raw, func(row map[string]any) error {
		labels := parseLabels(str(row["Labels"]))
		svc := RuntimeService{
			ID:        str(row["ID"]),
			Name:      str(row["Names"]),
			Image:     str(row["Image"]),
			Command:   str(row["Command"]),
			State:     str(row["State"]),
			Status:    str(row["Status"]),
			Ports:     str(row["Ports"]),
			CreatedAt: str(row["CreatedAt"]),
			Project:   labels["com.docker.compose.project"],
			Service:   labels["com.docker.compose.service"],
			Labels:    labels,
			Managed:   labels["panel.managed"] == "true",
		}
		out = append(out, svc)
		return nil
	})
	return out, err
}

func ParseNetworks(raw string) ([]RuntimeNetwork, error) {
	var out []RuntimeNetwork
	err := parseJSONLines(raw, func(row map[string]any) error {
		labels := parseLabels(str(row["Labels"]))
		out = append(out, RuntimeNetwork{
			ID:       str(row["ID"]),
			Name:     str(row["Name"]),
			Driver:   str(row["Driver"]),
			Scope:    str(row["Scope"]),
			Internal: strings.EqualFold(str(row["Internal"]), "true"),
			Labels:   labels,
			Managed:  labels["panel.managed"] == "true",
		})
		return nil
	})
	return out, err
}

func ParseVolumes(raw string) ([]RuntimeVolume, error) {
	var out []RuntimeVolume
	err := parseJSONLines(raw, func(row map[string]any) error {
		labels := parseLabels(str(row["Labels"]))
		out = append(out, RuntimeVolume{
			Name:    str(row["Name"]),
			Driver:  str(row["Driver"]),
			Scope:   str(row["Scope"]),
			Labels:  labels,
			Managed: labels["panel.managed"] == "true",
		})
		return nil
	})
	return out, err
}

func ParseImages(raw string) ([]RuntimeImage, error) {
	var out []RuntimeImage
	err := parseJSONLines(raw, func(row map[string]any) error {
		labels := parseLabels(str(row["Labels"]))
		out = append(out, RuntimeImage{
			ID:         str(row["ID"]),
			Repository: str(row["Repository"]),
			Tag:        str(row["Tag"]),
			Digest:     str(row["Digest"]),
			Size:       str(row["Size"]),
			CreatedAt:  str(row["CreatedAt"]),
			Labels:     labels,
			Managed:    labels["panel.managed"] == "true",
		})
		return nil
	})
	return out, err
}

func parseComposeStatusServices(raw, project string) ([]RuntimeService, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []RuntimeService{}, nil
	}
	if strings.HasPrefix(raw, "[") {
		var rows []map[string]any
		if err := json.Unmarshal([]byte(raw), &rows); err != nil {
			return nil, panelerr.BadGateway("docker_parse_failed", "Failed to parse Docker Compose status")
		}
		out := make([]RuntimeService, 0, len(rows))
		for _, row := range rows {
			out = append(out, composeRow(row, project))
		}
		return out, nil
	}
	var out []RuntimeService
	err := parseJSONLines(raw, func(row map[string]any) error {
		out = append(out, composeRow(row, project))
		return nil
	})
	return out, err
}

func composeRow(row map[string]any, project string) RuntimeService {
	labels := parseLabels(str(row["Labels"]))
	name := str(row["Name"])
	if name == "" {
		name = str(row["Names"])
	}
	return RuntimeService{
		ID:      str(row["ID"]),
		Name:    name,
		Image:   str(row["Image"]),
		Command: str(row["Command"]),
		State:   str(row["State"]),
		Status:  firstNonEmpty(str(row["Status"]), str(row["Health"])),
		Project: firstNonEmpty(labels["com.docker.compose.project"], project),
		Service: firstNonEmpty(labels["com.docker.compose.service"], str(row["Service"])),
		Labels:  labels,
		Managed: labels["panel.managed"] == "true",
	}
}

func parseJSONLines(raw string, handle func(map[string]any) error) error {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return panelerr.BadGateway("docker_parse_failed", "Failed to parse Docker command output")
		}
		if err := handle(row); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func parseLabels(raw string) map[string]string {
	labels := map[string]string{}
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "<no value>" {
		return labels
	}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		labels[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return labels
}

func applyVolumeInspect(volume *RuntimeVolume, raw string) {
	var row struct {
		Mountpoint string            `json:"Mountpoint"`
		Scope      string            `json:"Scope"`
		Labels     map[string]string `json:"Labels"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &row); err != nil {
		return
	}
	volume.Mountpoint = row.Mountpoint
	if row.Scope != "" {
		volume.Scope = row.Scope
	}
	if len(row.Labels) > 0 {
		volume.Labels = row.Labels
		volume.Managed = row.Labels["panel.managed"] == "true"
	}
}

func parseKeyValues(raw string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			out[key] = value
		}
	}
	return out
}

func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func dockerCommandError(code string, result sshx.CommandResult) error {
	message := "Docker command failed"
	detail := strings.TrimSpace(result.Stderr)
	if detail == "" {
		detail = strings.TrimSpace(result.Stdout)
	}
	if detail != "" {
		message += ": " + truncate(detail, 240)
	}
	return panelerr.BadGateway(code, message)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

const detectCommand = `sh -lc 'if command -v docker >/dev/null 2>&1; then echo docker_installed=true; if docker info >/dev/null 2>&1; then echo docker_daemon_reachable=true; docker version --format "docker_version={{.Server.Version}}" 2>/dev/null || true; else echo docker_daemon_reachable=false; fi; if docker compose version >/dev/null 2>&1; then echo compose_installed=true; docker compose version --short 2>/dev/null | sed "s/^/compose_version=/"; else echo compose_installed=false; fi; else echo docker_installed=false; echo compose_installed=false; echo docker_daemon_reachable=false; fi'`
