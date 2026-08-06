package appspec

import "testing"

const sampleSpecYAML = `
name: web
image: nginx:1.27
command: []
env:
  MODE: prod
ports:
  - label: http
    to: 80
    static: 8080
resources:
  cpu: 200
  memoryMb: 128
constraints:
  - attribute: "${node.class}"
    operator: "="
    value: "apps"
services:
  - name: web
    port: http
    tags: ["public"]
checks:
  - name: http
    type: http
    port: http
    path: /
    intervalSeconds: 10
    timeoutSeconds: 2
volumes:
  - source: web-data
    target: /usr/share/nginx/html
    readOnly: false
mounts:
  - type: host
    source: /srv/web-extra
    target: /srv/extra
    readOnly: true
restart:
  policy: unless-stopped
`

func TestRenderApplicationSpecAsRuntimeSpec(t *testing.T) {
	spec, issues := DecodeYAML(sampleSpecYAML)
	if len(issues) > 0 {
		t.Fatalf("decode issues = %#v", issues)
	}

	runtimeSpec, issues := Render(RenderInput{
		AppID:      "app-1",
		Generation: 3,
		SpecHash:   "hash-1",
		Namespace:  "apps",
		Region:     "global",
		Datacenter: "dc1",
		Spec:       spec,
	})
	if len(issues) > 0 {
		t.Fatalf("render issues = %#v", issues)
	}

	if runtimeSpec.ID != "panel-web" || runtimeSpec.ApplicationID != "app-1" || runtimeSpec.Name != "web" {
		t.Fatalf("runtime identity = %#v", runtimeSpec)
	}
	if runtimeSpec.Image != "nginx:1.27" || runtimeSpec.Env["MODE"] != "prod" {
		t.Fatalf("runtime image/env = %#v", runtimeSpec)
	}
	if runtimeSpec.Generation != 3 || runtimeSpec.SpecHash != "hash-1" {
		t.Fatalf("runtime revision = %#v", runtimeSpec)
	}
	if runtimeSpec.NetworkMode != "bridge" {
		t.Fatalf("network mode = %q", runtimeSpec.NetworkMode)
	}
	if got := runtimeSpec.Ports; len(got) != 1 || got[0].Label != "http" || got[0].ContainerPort != 80 || got[0].HostPort != 8080 || got[0].Protocol != "tcp" {
		t.Fatalf("ports = %#v", got)
	}
	if runtimeSpec.Resources.CPU != 200 || runtimeSpec.Resources.MemoryMB != 128 {
		t.Fatalf("resources = %#v", runtimeSpec.Resources)
	}
	if len(runtimeSpec.Services) != 1 || runtimeSpec.Services[0].Name != "web" || runtimeSpec.Services[0].Port != "http" {
		t.Fatalf("services = %#v", runtimeSpec.Services)
	}
	if checks := runtimeSpec.Checks; len(checks) != 1 || checks[0].Name != "http" || checks[0].Type != "http" || checks[0].Port != "http" || checks[0].Path != "/" || checks[0].IntervalSeconds != 10 || checks[0].TimeoutSeconds != 2 {
		t.Fatalf("checks = %#v", runtimeSpec.Checks)
	}
	if runtimeSpec.Restart.Policy != "unless-stopped" || runtimeSpec.Restart.Attempts != 2 || runtimeSpec.Restart.IntervalSeconds != 1800 || runtimeSpec.Restart.DelaySeconds != 15 || runtimeSpec.Restart.Mode != "delay" {
		t.Fatalf("restart = %#v", runtimeSpec.Restart)
	}
	if got := runtimeSpec.Mounts; len(got) != 2 {
		t.Fatalf("mounts = %#v", got)
	} else {
		if got[0].Type != "volume" || got[0].Source != "web-data" || got[0].Target != "/usr/share/nginx/html" || got[0].ReadOnly {
			t.Fatalf("volume mount = %#v", got[0])
		}
		if got[1].Type != "bind" || got[1].Source != "/srv/web-extra" || got[1].Target != "/srv/extra" || !got[1].ReadOnly {
			t.Fatalf("host mount = %#v", got[1])
		}
	}
}

func TestHashIsStable(t *testing.T) {
	spec, issues := DecodeYAML(sampleSpecYAML)
	if len(issues) > 0 {
		t.Fatalf("decode issues = %#v", issues)
	}

	first, err := Hash(spec)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Hash(spec)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("hashes differ: %q != %q", first, second)
	}
}

func TestRenderHostNetworkAndPrivilegedContainer(t *testing.T) {
	runtimeSpec, issues := Render(RenderInput{
		AppID:      "app-1",
		Generation: 1,
		SpecHash:   "hash-1",
		Namespace:  "apps",
		Region:     "global",
		Datacenter: "dc1",
		Spec: Spec{
			Name:        "agent",
			Image:       "alpine",
			NetworkMode: "host",
			Privileged:  true,
			CapAdd:      []string{"net_admin", "NET_ADMIN", "SYS_TIME"},
		},
	})
	if len(issues) > 0 {
		t.Fatalf("issues = %#v", issues)
	}
	if runtimeSpec.NetworkMode != "host" {
		t.Fatalf("network = %q", runtimeSpec.NetworkMode)
	}
	if !runtimeSpec.Privileged {
		t.Fatalf("privileged = false")
	}
	if got := runtimeSpec.CapAdd; len(got) != 2 || got[0] != "NET_ADMIN" || got[1] != "SYS_TIME" {
		t.Fatalf("capAdd = %#v", got)
	}
	if len(runtimeSpec.Ports) != 0 {
		t.Fatalf("host mode should not render ports: %#v", runtimeSpec.Ports)
	}
}

func TestRenderLeavesEmptyAndZeroResourcesUnlimited(t *testing.T) {
	for _, tc := range []struct {
		name string
		yaml string
	}{
		{name: "empty", yaml: "name: web\nimage: nginx\n"},
		{name: "zero", yaml: "name: web\nimage: nginx\nresources:\n  cpu: 0\n  memoryMb: 0\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec, issues := DecodeYAML(tc.yaml)
			if len(issues) > 0 {
				t.Fatalf("decode issues = %#v", issues)
			}

			runtimeSpec, issues := Render(RenderInput{
				AppID:    "app-1",
				Spec:     spec,
				SpecHash: "hash-1",
			})
			if len(issues) > 0 {
				t.Fatalf("render issues = %#v", issues)
			}
			if runtimeSpec.Resources.CPU != 0 || runtimeSpec.Resources.MemoryMB != 0 {
				t.Fatalf("resources should remain unlimited: %#v", runtimeSpec.Resources)
			}
		})
	}
}

func TestRenderAnyTLSHostNetworkSpec(t *testing.T) {
	spec, issues := DecodeYAML(`name: anytls
image: jiasongji/anytls
networkMode: host
command:
  - "/app/anytls-server"
  - "-l"
  - ":9443"
  - "-p"
  - "password"
restart:
  policy: "unless-stopped"
`)
	if len(issues) > 0 {
		t.Fatalf("decode issues = %#v", issues)
	}
	runtimeSpec, issues := Render(RenderInput{
		AppID:      "app-1",
		Generation: 1,
		SpecHash:   "hash-1",
		Namespace:  "apps",
		Region:     "global",
		Datacenter: "dc1",
		Spec:       spec,
	})
	if len(issues) > 0 {
		t.Fatalf("render issues = %#v", issues)
	}
	if runtimeSpec.NetworkMode != "host" || len(runtimeSpec.Ports) != 0 {
		t.Fatalf("host network render = mode %q ports %#v", runtimeSpec.NetworkMode, runtimeSpec.Ports)
	}
	if got := runtimeSpec.Command; len(got) != 5 || got[0] != "/app/anytls-server" || got[2] != ":9443" {
		t.Fatalf("command = %#v", got)
	}
}

func TestRenderMountPermissions(t *testing.T) {
	uid := 1000
	gid := 1001
	runtimeSpec, issues := Render(RenderInput{
		AppID:      "app-1",
		Generation: 1,
		SpecHash:   "hash-1",
		Spec: Spec{
			Name:  "web",
			Image: "nginx",
			Mounts: []Mount{
				{Type: "persistent", Source: "data", Target: "/opt/data", UID: &uid, GID: &gid, Mode: "0755"},
				{Type: "file", Source: "config/app.conf", Target: "/etc/app.conf", UID: &uid, GID: &gid, Mode: "0755"},
				{Type: "panel_file", Source: "certificate:cert-1:certificate", Target: "/etc/tls/cert.pem", ReadOnly: true, UID: &uid, GID: &gid},
			},
		},
	})
	if len(issues) > 0 {
		t.Fatalf("issues = %#v", issues)
	}
	if got := runtimeSpec.Mounts[0]; got.Type != "persistent" || got.Source != "/opt/panel/apps/app-1/persistent/data" || got.UID == nil || *got.UID != uid || got.GID == nil || *got.GID != gid || got.Mode != "0755" {
		t.Fatalf("persistent mount = %#v", got)
	}
	if got := runtimeSpec.Mounts[1]; got.Type != "managed_file" || got.ReadOnly || got.UID == nil || *got.UID != uid || got.GID == nil || *got.GID != gid || got.Mode != "0755" {
		t.Fatalf("application file mount = %#v", got)
	}
	if got := runtimeSpec.Mounts[2]; got.Type != "managed_file" || !got.ReadOnly || got.UID == nil || *got.UID != uid || got.GID == nil || *got.GID != gid || got.Mode != "" {
		t.Fatalf("panel file mount = %#v", got)
	}
}
