package appspec

import (
	"strconv"
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
	volumes, mounts := renderVolumes(spec.Volumes)
	task.VolumeMounts = mounts

	group := nomad.TaskGroup{
		Name:        spec.Name,
		Count:       spec.Count,
		Networks:    []nomad.Network{renderNetwork(spec.Ports)},
		Tasks:       []nomad.Task{task},
		Services:    renderServices(spec.Services, spec.Checks),
		Constraints: renderConstraints(spec.Constraints),
		Volumes:     volumes,
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

func renderNetwork(ports []Port) nomad.Network {
	network := nomad.Network{Mode: "bridge"}
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

func renderVolumes(volumes []Volume) (map[string]nomad.VolumeRequest, []nomad.VolumeMount) {
	if len(volumes) == 0 {
		return nil, nil
	}
	requests := map[string]nomad.VolumeRequest{}
	mounts := make([]nomad.VolumeMount, 0, len(volumes))
	for _, volume := range volumes {
		requests[volume.Source] = nomad.VolumeRequest{
			Type:     "host",
			Source:   volume.Source,
			ReadOnly: volume.ReadOnly,
		}
		mounts = append(mounts, nomad.VolumeMount{
			Volume:      volume.Source,
			Destination: volume.Target,
			ReadOnly:    volume.ReadOnly,
		})
	}
	return requests, mounts
}
