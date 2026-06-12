package appspec

import (
	"strconv"
	"strings"
	"time"

	"panel/internal/nomad"
)

type RenderInput struct {
	AppID      string
	Generation int
	SpecHash   string
	Namespace  string
	Region     string
	Datacenter string
	Spec       Spec
}

func Render(in RenderInput) (nomad.Job, []Issue) {
	spec := Normalize(in.Spec)
	if issues := Validate(spec); len(issues) > 0 {
		return nomad.Job{}, issues
	}

	task := nomad.Task{
		Name:   spec.Name,
		Driver: "docker",
		Config: map[string]any{"image": spec.Image},
		Env:    spec.Env,
		Resources: &nomad.Resources{
			CPU:      spec.Resources.CPU,
			MemoryMB: spec.Resources.MemoryMB,
		},
	}
	if len(spec.Command) > 0 {
		task.Config["command"] = spec.Command[0]
	}
	if len(spec.Args) > 0 {
		task.Config["args"] = spec.Args
	}
	if spec.Privileged {
		task.Config["privileged"] = true
	}
	if !strings.Contains(spec.Image, "@sha256:") {
		task.Config["force_pull"] = true
	}
	if labels := portLabels(spec.Ports); len(labels) > 0 {
		task.Config["ports"] = labels
	}
	if mounts := renderDockerMounts(in.AppID, spec.Volumes, spec.Mounts); len(mounts) > 0 {
		task.Config["mounts"] = mounts
	}

	group := nomad.TaskGroup{
		Name:        spec.Name,
		Count:       spec.Count,
		Networks:    []nomad.Network{renderNetwork(spec.NetworkMode, spec.Ports)},
		Tasks:       []nomad.Task{task},
		Services:    renderServices(spec.Services, spec.Checks),
		Constraints: renderConstraints(spec.Constraints),
		RestartPolicy: &nomad.RestartPolicy{
			Attempts: spec.Restart.Attempts,
			Interval: int64(time.Duration(spec.Restart.IntervalSeconds) * time.Second),
			Delay:    int64(time.Duration(spec.Restart.DelaySeconds) * time.Second),
			Mode:     spec.Restart.Mode,
		},
	}

	return nomad.Job{
		ID:          "panel-" + spec.Name,
		Name:        spec.Name,
		Type:        "service",
		Region:      in.Region,
		Namespace:   in.Namespace,
		Datacenters: []string{in.Datacenter},
		Meta: map[string]string{
			"panel.app.id":     in.AppID,
			"panel.app.name":   spec.Name,
			"panel.generation": strconv.Itoa(in.Generation),
			"panel.spec_hash":  in.SpecHash,
		},
		TaskGroups: []nomad.TaskGroup{group},
	}, nil
}

func portLabels(ports []Port) []string {
	labels := make([]string, 0, len(ports))
	for _, port := range ports {
		if port.Label != "" {
			labels = append(labels, port.Label)
		}
	}
	return labels
}

func renderNetwork(mode string, ports []Port) nomad.Network {
	network := nomad.Network{Mode: mode}
	for _, port := range ports {
		mapping := nomad.PortMapping{Label: port.Label, To: port.To}
		if port.Static > 0 {
			mapping.Value = port.Static
			network.ReservedPorts = append(network.ReservedPorts, mapping)
			continue
		}
		network.DynamicPorts = append(network.DynamicPorts, mapping)
	}
	return network
}

func renderServices(services []Service, checks []Check) []nomad.Service {
	out := make([]nomad.Service, 0, len(services))
	renderedChecks := renderChecks(checks)
	for _, service := range services {
		out = append(out, nomad.Service{
			Name:   service.Name,
			Port:   service.Port,
			Tags:   append([]string(nil), service.Tags...),
			Checks: append([]nomad.Check(nil), renderedChecks...),
		})
	}
	return out
}

func renderChecks(checks []Check) []nomad.Check {
	out := make([]nomad.Check, 0, len(checks))
	for _, check := range checks {
		out = append(out, nomad.Check{
			Name:     check.Name,
			Type:     check.Type,
			Port:     check.Port,
			Path:     check.Path,
			Command:  check.Command,
			Interval: int64(time.Duration(check.IntervalSeconds) * time.Second),
			Timeout:  int64(time.Duration(check.TimeoutSeconds) * time.Second),
		})
	}
	return out
}

func renderConstraints(constraints []Constraint) []nomad.Constraint {
	out := make([]nomad.Constraint, 0, len(constraints))
	for _, constraint := range constraints {
		out = append(out, nomad.Constraint{
			LTarget: constraint.Attribute,
			Operand: constraint.Operator,
			RTarget: constraint.Value,
		})
	}
	return out
}

func renderDockerMounts(appID string, volumes []Volume, mounts []Mount) []map[string]any {
	out := make([]map[string]any, 0, len(volumes)+len(mounts))
	for _, volume := range volumes {
		out = append(out, map[string]any{
			"type":     "volume",
			"source":   volume.Source,
			"target":   volume.Target,
			"readonly": volume.ReadOnly,
		})
	}
	for _, mount := range mounts {
		mountType := strings.TrimSpace(mount.Type)
		if mountType == "file" || mountType == "panel_file" {
			continue
		}
		source := strings.TrimSpace(mount.Source)
		dockerType := "bind"
		if mountType == "volume" {
			dockerType = "volume"
		}
		if mountType == "persistent" {
			source = persistentMountSource(appID, source)
		}
		out = append(out, map[string]any{
			"type":     dockerType,
			"source":   source,
			"target":   mount.Target,
			"readonly": mount.ReadOnly,
		})
	}
	return out
}

func persistentMountSource(appID, source string) string {
	base := "/opt/panel/apps/" + appID + "/persistent"
	source = strings.Trim(source, "/")
	if source == "" {
		return base
	}
	return base + "/" + source
}
