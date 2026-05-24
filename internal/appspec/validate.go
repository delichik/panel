package appspec

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var namePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?$`)

func Normalize(spec Spec) Spec {
	if spec.Count == 0 {
		spec.Count = 1
	}
	if spec.Resources.CPU == 0 {
		spec.Resources.CPU = 100
	}
	if spec.Resources.MemoryMB == 0 {
		spec.Resources.MemoryMB = 128
	}
	if spec.Env == nil {
		spec.Env = map[string]string{}
	}
	return spec
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
	if spec.Count < 0 {
		issues = append(issues, Issue{Field: "count", Message: "count cannot be negative"})
	}
	if spec.Resources.CPU < 0 {
		issues = append(issues, Issue{Field: "resources.cpu", Message: "cpu cannot be negative"})
	}
	if spec.Resources.MemoryMB < 0 {
		issues = append(issues, Issue{Field: "resources.memoryMb", Message: "memoryMb cannot be negative"})
	}

	portLabels := map[string]struct{}{}
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
	return issues
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
