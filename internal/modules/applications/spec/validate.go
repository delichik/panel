package appspec

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var namePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?$`)
var fileModePattern = regexp.MustCompile(`^[0-7]{3,4}$`)
var capabilityPattern = regexp.MustCompile(`^[A-Z0-9_]+$`)

func Normalize(spec Spec) Spec {
	spec.Count = 1
	spec.Command = nonEmptyStringItems(spec.Command)
	spec.CapAdd = normalizeCapabilities(spec.CapAdd)
	if spec.NetworkMode == "" {
		spec.NetworkMode = "bridge"
	}
	if spec.Env == nil {
		spec.Env = map[string]string{}
	}
	spec.Restart.Policy = strings.TrimSpace(spec.Restart.Policy)
	if spec.Restart.Policy == "" {
		spec.Restart.Policy = "no"
	}
	if spec.Restart.IntervalSeconds == 0 {
		spec.Restart.IntervalSeconds = 1800
	}
	if spec.Restart.DelaySeconds == 0 {
		spec.Restart.DelaySeconds = 15
	}
	switch spec.Restart.Policy {
	case "no":
		spec.Restart.Attempts = 0
		spec.Restart.Mode = "fail"
	case "on-failure":
		if spec.Restart.Attempts == 0 {
			spec.Restart.Attempts = 2
		}
		spec.Restart.Mode = "fail"
	case "always", "unless-stopped":
		if spec.Restart.Attempts == 0 {
			spec.Restart.Attempts = 2
		}
		spec.Restart.Mode = "delay"
	}
	return spec
}

func nonEmptyStringItems(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func normalizeCapabilities(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.ToUpper(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func Validate(spec Spec) []Issue {
	spec = Normalize(spec)
	var issues []Issue
	if !validName(spec.Name) {
		issues = append(issues, Issue{Field: "name", Message: "name must be 1-32 lowercase letters, digits, or hyphens and start and end with an alphanumeric character"})
	}
	if strings.TrimSpace(spec.Image) == "" {
		issues = append(issues, Issue{Field: "image", Message: "image is required"})
	}
	if spec.NetworkMode != "bridge" && spec.NetworkMode != "host" {
		issues = append(issues, Issue{Field: "networkMode", Message: "networkMode must be bridge or host"})
	}
	if spec.Resources.CPU < 0 {
		issues = append(issues, Issue{Field: "resources.cpu", Message: "cpu cannot be negative"})
	}
	if spec.Resources.MemoryMB < 0 {
		issues = append(issues, Issue{Field: "resources.memoryMb", Message: "memoryMb cannot be negative"})
	}
	for i, capability := range spec.CapAdd {
		if !capabilityPattern.MatchString(capability) {
			issues = append(issues, Issue{Field: fmt.Sprintf("capAdd[%d]", i), Message: "capability must use uppercase letters, digits, or underscores"})
		}
	}
	switch spec.Restart.Policy {
	case "no", "on-failure", "always", "unless-stopped":
	default:
		issues = append(issues, Issue{Field: "restart.policy", Message: "restart policy must be no, on-failure, always, or unless-stopped"})
	}
	if spec.Restart.Attempts < 0 {
		issues = append(issues, Issue{Field: "restart.attempts", Message: "restart attempts cannot be negative"})
	}
	if spec.Restart.IntervalSeconds < 0 {
		issues = append(issues, Issue{Field: "restart.intervalSeconds", Message: "restart interval cannot be negative"})
	}
	if spec.Restart.DelaySeconds < 0 {
		issues = append(issues, Issue{Field: "restart.delaySeconds", Message: "restart delay cannot be negative"})
	}
	if spec.Restart.Mode != "" && spec.Restart.Mode != "fail" && spec.Restart.Mode != "delay" {
		issues = append(issues, Issue{Field: "restart.mode", Message: "restart mode must be fail or delay"})
	}

	portLabels := map[string]struct{}{}
	if spec.NetworkMode == "host" && len(spec.Ports) > 0 {
		issues = append(issues, Issue{Field: "ports", Message: "ports cannot be configured when networkMode is host"})
	}
	for i, port := range spec.Ports {
		if !validName(port.Label) {
			issues = append(issues, Issue{Field: fmt.Sprintf("ports[%d].label", i), Message: "port label must use application name format"})
		} else {
			portLabels[port.Label] = struct{}{}
		}
		if !validPort(port.To) {
			issues = append(issues, Issue{Field: fmt.Sprintf("ports[%d].to", i), Message: "target port must be between 1 and 65535"})
		}
		if port.Static != 0 && !validPort(port.Static) {
			issues = append(issues, Issue{Field: fmt.Sprintf("ports[%d].static", i), Message: "static port must be between 1 and 65535"})
		}
	}

	for i, service := range spec.Services {
		if service.Port != "" {
			if _, ok := portLabels[service.Port]; !ok {
				issues = append(issues, Issue{Field: fmt.Sprintf("services[%d].port", i), Message: "service port must reference an existing port label"})
			}
		}
	}

	for i, check := range spec.Checks {
		switch check.Type {
		case "tcp", "http", "script":
		default:
			issues = append(issues, Issue{Field: fmt.Sprintf("checks[%d].type", i), Message: "check type must be tcp, http, or script"})
		}
		if check.Type == "tcp" || check.Type == "http" {
			if _, ok := portLabels[check.Port]; !ok {
				issues = append(issues, Issue{Field: fmt.Sprintf("checks[%d].port", i), Message: "check port must reference an existing port label"})
			}
		}
	}

	for i, volume := range spec.Volumes {
		if strings.TrimSpace(volume.Source) == "" {
			issues = append(issues, Issue{Field: fmt.Sprintf("volumes[%d].source", i), Message: "volume source is required"})
		}
		if !strings.HasPrefix(volume.Target, "/") {
			issues = append(issues, Issue{Field: fmt.Sprintf("volumes[%d].target", i), Message: "volume target must be an absolute Linux path"})
		}
	}
	for i, mount := range spec.Mounts {
		mountType := strings.TrimSpace(mount.Type)
		switch mountType {
		case "volume", "host", "global", "file", "panel_file", "persistent":
		default:
			issues = append(issues, Issue{Field: fmt.Sprintf("mounts[%d].type", i), Message: "mount type must be volume, host, global, file, panel_file, or persistent"})
		}
		if strings.TrimSpace(mount.Source) == "" && mountType != "persistent" {
			issues = append(issues, Issue{Field: fmt.Sprintf("mounts[%d].source", i), Message: "mount source is required"})
		}
		if (mountType == "file" || mountType == "persistent") && !validWorkspacePath(mount.Source) {
			issues = append(issues, Issue{Field: fmt.Sprintf("mounts[%d].source", i), Message: "workspace mount source must be a relative path inside the application workspace"})
		}
		if mountType == "panel_file" && !validPanelFileSource(mount.Source) {
			issues = append(issues, Issue{Field: fmt.Sprintf("mounts[%d].source", i), Message: "Panel file source is invalid"})
		}
		if (mountType == "host" || mountType == "global") && !strings.HasPrefix(mount.Source, "/") {
			issues = append(issues, Issue{Field: fmt.Sprintf("mounts[%d].source", i), Message: "host path mount source must be an absolute Linux path"})
		}
		if !strings.HasPrefix(mount.Target, "/") {
			issues = append(issues, Issue{Field: fmt.Sprintf("mounts[%d].target", i), Message: "mount target must be an absolute Linux path"})
		}
		if mount.UID != nil && *mount.UID < 0 {
			issues = append(issues, Issue{Field: fmt.Sprintf("mounts[%d].uid", i), Message: "mount uid cannot be negative"})
		}
		if mount.GID != nil && *mount.GID < 0 {
			issues = append(issues, Issue{Field: fmt.Sprintf("mounts[%d].gid", i), Message: "mount gid cannot be negative"})
		}
		if strings.TrimSpace(mount.Mode) != "" && !fileModePattern.MatchString(strings.TrimSpace(mount.Mode)) {
			issues = append(issues, Issue{Field: fmt.Sprintf("mounts[%d].mode", i), Message: "mount mode must be an octal file mode such as 0755"})
		}
		if !mountSupportsOwnership(mountType) && (mount.UID != nil || mount.GID != nil) {
			issues = append(issues, Issue{Field: fmt.Sprintf("mounts[%d]", i), Message: "mount uid and gid are only supported for file, panel_file, and persistent mounts"})
		}
		if !mountSupportsMode(mountType) && strings.TrimSpace(mount.Mode) != "" {
			issues = append(issues, Issue{Field: fmt.Sprintf("mounts[%d]", i), Message: "mount mode is only supported for file and persistent mounts"})
		}
	}
	return issues
}

func mountSupportsOwnership(mountType string) bool {
	return mountType == "file" || mountType == "panel_file" || mountType == "persistent"
}

func mountSupportsMode(mountType string) bool {
	return mountType == "file" || mountType == "persistent"
}

func validPanelFileSource(value string) bool {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 3 || strings.TrimSpace(parts[1]) == "" {
		return false
	}
	switch parts[0] {
	case "key_asset", "certificate":
	default:
		return false
	}
	if parts[0] == "certificate" {
		switch parts[2] {
		case "certificate", "private_key":
			return true
		default:
			return false
		}
	}
	switch parts[2] {
	case "certificate", "private_key", "public_key", "ssh_public_key", "ca_certificate", "ca_private_key":
		return true
	default:
		return false
	}
}

func DecodeYAML(raw string) (Spec, []Issue) {
	var spec Spec
	if err := yaml.Unmarshal([]byte(raw), &spec); err != nil {
		return Spec{}, []Issue{{Field: "specYaml", Message: err.Error()}}
	}
	spec = Normalize(spec)
	return spec, Validate(spec)
}

func validName(value string) bool {
	return namePattern.MatchString(value)
}

func validPort(value int) bool {
	return value >= 1 && value <= 65535
}

func validWorkspacePath(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	if strings.HasPrefix(value, "/") || strings.Contains(value, "\\") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
