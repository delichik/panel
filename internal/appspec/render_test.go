package appspec

import "testing"

const sampleSpecYAML = `
name: web
image: nginx:1.27
count: 2
command: []
args: []
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
`

func TestRenderApplicationSpecAsNomadJob(t *testing.T) {
	spec, issues := DecodeYAML(sampleSpecYAML)
	if len(issues) > 0 {
		t.Fatalf("decode issues = %#v", issues)
	}

	job, issues := Render(RenderInput{
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

	if job.ID != "panel-web" || job.Name != "web" || job.Type != "service" {
		t.Fatalf("job identity = %#v", job)
	}
	if job.Namespace != "apps" || job.Region != "global" {
		t.Fatalf("job scope namespace=%q region=%q", job.Namespace, job.Region)
	}
	if len(job.Datacenters) != 1 || job.Datacenters[0] != "dc1" {
		t.Fatalf("datacenters = %#v", job.Datacenters)
	}
	if job.Meta["panel.app.id"] != "app-1" || job.Meta["panel.app.name"] != "web" || job.Meta["panel.generation"] != "3" || job.Meta["panel.spec_hash"] != "hash-1" {
		t.Fatalf("meta = %#v", job.Meta)
	}
	if len(job.TaskGroups) != 1 {
		t.Fatalf("task groups = %#v", job.TaskGroups)
	}
	group := job.TaskGroups[0]
	if group.Name != "web" || group.Count != 2 {
		t.Fatalf("group = %#v", group)
	}
	if len(group.Networks) != 1 || group.Networks[0].Mode != "bridge" {
		t.Fatalf("networks = %#v", group.Networks)
	}
	if got := group.Networks[0].ReservedPorts; len(got) != 1 || got[0].Label != "http" || got[0].Value != 8080 || got[0].To != 80 {
		t.Fatalf("reserved ports = %#v", got)
	}
	if len(group.Tasks) != 1 {
		t.Fatalf("tasks = %#v", group.Tasks)
	}
	task := group.Tasks[0]
	if task.Name != "web" || task.Driver != "docker" {
		t.Fatalf("task = %#v", task)
	}
	if task.Config["image"] != "nginx:1.27" {
		t.Fatalf("task config = %#v", task.Config)
	}
	if task.Env["MODE"] != "prod" {
		t.Fatalf("env = %#v", task.Env)
	}
	if task.Resources == nil || task.Resources.CPU != 200 || task.Resources.MemoryMB != 128 {
		t.Fatalf("resources = %#v", task.Resources)
	}
	if len(group.Services) != 1 || group.Services[0].Name != "web" || group.Services[0].Port != "http" {
		t.Fatalf("services = %#v", group.Services)
	}
	if checks := group.Services[0].Checks; len(checks) != 1 || checks[0].Name != "http" || checks[0].Type != "http" || checks[0].Port != "http" || checks[0].Path != "/" {
		t.Fatalf("checks = %#v", group.Services[0].Checks)
	}
	if volume := group.Volumes["web-data"]; volume.Type != "host" || volume.Source != "web-data" || volume.ReadOnly {
		t.Fatalf("volumes = %#v", group.Volumes)
	}
	if mounts := task.VolumeMounts; len(mounts) != 1 || mounts[0].Volume != "web-data" || mounts[0].Destination != "/usr/share/nginx/html" || mounts[0].ReadOnly {
		t.Fatalf("volume mounts = %#v", mounts)
	}
}

func TestHashIsStableAcrossVariableMapOrder(t *testing.T) {
	spec, issues := DecodeYAML(sampleSpecYAML)
	if len(issues) > 0 {
		t.Fatalf("decode issues = %#v", issues)
	}

	first, err := Hash(spec, map[string]string{"B": "2", "A": "1"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Hash(spec, map[string]string{"A": "1", "B": "2"})
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("hashes differ: %q != %q", first, second)
	}
}
