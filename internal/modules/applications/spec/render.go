package appspec

import (
	"strings"

	"panel/internal/modules/applications/runtime"
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

func Render(in RenderInput) (appruntime.Spec, []Issue) {
	spec := Normalize(in.Spec)
	if issues := Validate(spec); len(issues) > 0 {
		return appruntime.Spec{}, issues
	}

	return appruntime.Spec{
		ID:            "panel-" + spec.Name,
		ApplicationID: in.AppID,
		Name:          spec.Name,
		Image:         spec.Image,
		Command:       append([]string(nil), spec.Command...),
		Env:           cloneStringMap(spec.Env),
		Ports:         renderPorts(spec.Ports),
		Resources:     appruntime.Resources{CPU: spec.Resources.CPU, MemoryMB: spec.Resources.MemoryMB},
		Privileged:    spec.Privileged,
		CapAdd:        append([]string(nil), spec.CapAdd...),
		Mounts:        renderMounts(in.AppID, spec.Volumes, spec.Mounts),
		Restart: appruntime.Restart{
			Policy:          spec.Restart.Policy,
			Attempts:        spec.Restart.Attempts,
			IntervalSeconds: spec.Restart.IntervalSeconds,
			DelaySeconds:    spec.Restart.DelaySeconds,
			Mode:            spec.Restart.Mode,
		},
		Services:   renderServices(spec.Services),
		Checks:     renderChecks(spec.Checks),
		Generation: in.Generation,
		SpecHash:   in.SpecHash,
	}, nil
}

func renderPorts(ports []Port) []appruntime.Port {
	out := make([]appruntime.Port, 0, len(ports))
	for _, port := range ports {
		out = append(out, appruntime.Port{Label: port.Label, ContainerPort: port.To, HostPort: port.Static, Protocol: "tcp"})
	}
	return out
}

func renderServices(services []Service) []appruntime.Service {
	out := make([]appruntime.Service, 0, len(services))
	for _, service := range services {
		out = append(out, appruntime.Service{
			Name: service.Name,
			Port: service.Port,
			Tags: append([]string(nil), service.Tags...),
		})
	}
	return out
}

func renderChecks(checks []Check) []appruntime.Check {
	out := make([]appruntime.Check, 0, len(checks))
	for _, check := range checks {
		out = append(out, appruntime.Check{
			Name:            check.Name,
			Type:            check.Type,
			Port:            check.Port,
			Path:            check.Path,
			Command:         check.Command,
			IntervalSeconds: check.IntervalSeconds,
			TimeoutSeconds:  check.TimeoutSeconds,
		})
	}
	return out
}

func renderMounts(appID string, volumes []Volume, mounts []Mount) []appruntime.Mount {
	out := make([]appruntime.Mount, 0, len(volumes)+len(mounts))
	for _, volume := range volumes {
		out = append(out, appruntime.Mount{Type: "volume", Source: volume.Source, Target: volume.Target, ReadOnly: volume.ReadOnly})
	}
	for _, mount := range mounts {
		mountType := strings.TrimSpace(mount.Type)
		source := strings.TrimSpace(mount.Source)
		switch mountType {
		case "file", "panel_file":
			out = append(out, appruntime.Mount{Type: "managed_file", Source: source, Target: mount.Target, ReadOnly: mount.ReadOnly, UID: cloneInt(mount.UID), GID: cloneInt(mount.GID), Mode: strings.TrimSpace(mount.Mode)})
		case "volume":
			out = append(out, appruntime.Mount{Type: "volume", Source: source, Target: mount.Target, ReadOnly: mount.ReadOnly})
		case "persistent":
			out = append(out, appruntime.Mount{Type: "persistent", Source: persistentMountSource(appID, source), Target: mount.Target, ReadOnly: mount.ReadOnly, UID: cloneInt(mount.UID), GID: cloneInt(mount.GID), Mode: strings.TrimSpace(mount.Mode)})
		case "storage_share":
			out = append(out, appruntime.Mount{Type: "storage_share", Source: source, Target: mount.Target, ReadOnly: mount.ReadOnly})
		default:
			out = append(out, appruntime.Mount{Type: "bind", Source: source, Target: mount.Target, ReadOnly: mount.ReadOnly})
		}
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

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
