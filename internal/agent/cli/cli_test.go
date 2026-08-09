package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	agentcontract "panel/internal/agent/contract"
)

type fakeRuntime struct {
	containers  []agentcontract.DockerContainer
	home        string
	instanceDir string
	persistent  string
}

func (f *fakeRuntime) Containers(ctx context.Context) ([]agentcontract.DockerContainer, error) {
	return f.containers, nil
}

func (f *fakeRuntime) ApplicationHome(applicationID string) (string, error) {
	return f.home, nil
}

func (f *fakeRuntime) InstanceDir(applicationID, instanceID string) (string, error) {
	return f.instanceDir, nil
}

func (f *fakeRuntime) PersistentDir(applicationID string) (string, error) {
	return f.persistent, nil
}

func fakeFactory(f *fakeRuntime) runtimeFactory {
	return func(host string) (containerSource, error) { return f, nil }
}

func managedContainer(id, name, appID, instanceID string) agentcontract.DockerContainer {
	return agentcontract.DockerContainer{
		ID:      id,
		Names:   []string{"/" + name},
		Image:   "nginx:latest",
		State:   "running",
		Status:  "Up 2 hours",
		Created: 1750000000,
		Labels: map[string]string{
			"panel.application.managed":     "true",
			"panel.application.id":          appID,
			"panel.application.instance.id": instanceID,
			"panel.application.generation":  "3",
			"panel.application.spec.hash":   "spec123",
			"panel.application.apply.mode":  "recreate",
		},
	}
}

func TestRunUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	f := &fakeRuntime{}
	if code := run(nil, &stdout, &stderr, fakeFactory(f)); code != exitUsage {
		t.Fatalf("no args: got exit %d, want %d", code, exitUsage)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"nope"}, &stdout, &stderr, fakeFactory(f)); code != exitUsage {
		t.Fatalf("unknown command: got exit %d, want %d", code, exitUsage)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"help"}, &stdout, &stderr, fakeFactory(f)); code != exitOK {
		t.Fatalf("help: got exit %d, want %d", code, exitOK)
	}
	if !strings.Contains(stdout.String(), "apps list") {
		t.Fatalf("help output missing apps list: %q", stdout.String())
	}
}

func TestRunListTable(t *testing.T) {
	f := &fakeRuntime{containers: []agentcontract.DockerContainer{
		managedContainer("aaaaaaaaaaaa111111111111", "b-app", "app-b", "inst-b"),
		managedContainer("bbbbbbbbbbbb222222222222", "a-app", "app-a", "inst-a"),
		{ID: "cccccccccccc333333333333", Names: []string{"/other"}, Image: "busybox", Labels: map[string]string{}},
	}}
	var stdout, stderr bytes.Buffer
	code := run([]string{"apps", "list"}, &stdout, &stderr, fakeFactory(f))
	if code != exitOK {
		t.Fatalf("list exit = %d, stderr = %q", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"ID", "a-app", "b-app", "app-a", "inst-a", "nginx:latest", "running"} {
		if !strings.Contains(out, want) {
			t.Errorf("list table missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "other") || strings.Contains(out, "busybox") {
		t.Errorf("list table contains unmanaged container:\n%s", out)
	}
	if strings.Index(out, "a-app") > strings.Index(out, "b-app") {
		t.Errorf("list table not sorted by name:\n%s", out)
	}
}

func TestRunListJSON(t *testing.T) {
	f := &fakeRuntime{containers: []agentcontract.DockerContainer{
		managedContainer("aaaaaaaaaaaa111111111111", "a-app", "app-a", "inst-a"),
		{ID: "cccccccccccc333333333333", Names: []string{"/other"}, Labels: map[string]string{}},
	}}
	var stdout, stderr bytes.Buffer
	code := run([]string{"apps", "list", "--json"}, &stdout, &stderr, fakeFactory(f))
	if code != exitOK {
		t.Fatalf("list --json exit = %d, stderr = %q", code, stderr.String())
	}
	var items []agentcontract.DockerContainer
	if err := json.Unmarshal(stdout.Bytes(), &items); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if len(items) != 1 || items[0].Labels["panel.application.id"] != "app-a" {
		t.Fatalf("unexpected JSON items: %+v", items)
	}
}

func TestRunListUsage(t *testing.T) {
	f := &fakeRuntime{}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"apps", "list", "extra"}, &stdout, &stderr, fakeFactory(f)); code != exitUsage {
		t.Fatalf("extra arg: got exit %d, want %d", code, exitUsage)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"apps", "list", "--bogus"}, &stdout, &stderr, fakeFactory(f)); code != exitUsage {
		t.Fatalf("bad flag: got exit %d, want %d", code, exitUsage)
	}
}

func TestRunInspectByContainerName(t *testing.T) {
	f := &fakeRuntime{containers: []agentcontract.DockerContainer{
		managedContainer("aaaaaaaaaaaa111111111111", "web", "app-a", "inst-a"),
	}}
	f.home = "/opt/panel/apps/app-a"
	f.instanceDir = "/opt/panel/apps/app-a/instances/inst-a"
	f.persistent = "/opt/panel/apps/app-a/persistent"
	var stdout, stderr bytes.Buffer
	for _, selector := range []string{"web", "/web"} {
		stdout.Reset()
		stderr.Reset()
		code := run([]string{"apps", "inspect", selector}, &stdout, &stderr, fakeFactory(f))
		if code != exitOK {
			t.Fatalf("inspect %q exit = %d, stderr = %q", selector, code, stderr.String())
		}
		out := stdout.String()
		for _, want := range []string{"APPLICATION ID", "app-a", "INSTANCE ID", "inst-a", "GENERATION", "3", "SPEC HASH", "spec123", "APPLY MODE", "recreate", "HOME", "/opt/panel/apps/app-a"} {
			if !strings.Contains(out, want) {
				t.Errorf("inspect %q missing %q in:\n%s", selector, want, out)
			}
		}
	}
}

func TestRunInspectByInstanceAndAppID(t *testing.T) {
	f := &fakeRuntime{containers: []agentcontract.DockerContainer{
		managedContainer("aaaaaaaaaaaa111111111111", "web", "app-a", "inst-a"),
	}}
	f.home = "/opt/panel/apps/app-a"
	f.instanceDir = "/opt/panel/apps/app-a/instances/inst-a"
	f.persistent = "/opt/panel/apps/app-a/persistent"
	var stdout, stderr bytes.Buffer
	if code := run([]string{"apps", "inspect", "inst-a"}, &stdout, &stderr, fakeFactory(f)); code != exitOK {
		t.Fatalf("inspect by instance id exit = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"apps", "inspect", "app-a"}, &stdout, &stderr, fakeFactory(f)); code != exitOK {
		t.Fatalf("inspect by app id exit = %d, stderr = %q", code, stderr.String())
	}
}

func TestRunInspectAmbiguousAppID(t *testing.T) {
	f := &fakeRuntime{containers: []agentcontract.DockerContainer{
		managedContainer("aaaaaaaaaaaa111111111111", "web-a", "app-a", "inst-a"),
		managedContainer("bbbbbbbbbbbb222222222222", "web-b", "app-a", "inst-b"),
	}}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"apps", "inspect", "app-a"}, &stdout, &stderr, fakeFactory(f)); code != exitError {
		t.Fatalf("ambiguous app id: got exit %d, want %d (stderr %q)", code, exitError, stderr.String())
	}
	if !strings.Contains(stderr.String(), "matches 2 instances") {
		t.Fatalf("unexpected error: %q", stderr.String())
	}
}

func TestRunInspectNotFound(t *testing.T) {
	f := &fakeRuntime{containers: []agentcontract.DockerContainer{
		managedContainer("aaaaaaaaaaaa111111111111", "web", "app-a", "inst-a"),
	}}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"apps", "inspect", "missing"}, &stdout, &stderr, fakeFactory(f)); code != exitError {
		t.Fatalf("not found: got exit %d, want %d", code, exitError)
	}
}

func TestRunInspectJSON(t *testing.T) {
	f := &fakeRuntime{containers: []agentcontract.DockerContainer{
		managedContainer("aaaaaaaaaaaa111111111111", "web", "app-a", "inst-a"),
	}}
	f.home = "/opt/panel/apps/app-a"
	f.instanceDir = "/opt/panel/apps/app-a/instances/inst-a"
	f.persistent = "/opt/panel/apps/app-a/persistent"
	var stdout, stderr bytes.Buffer
	code := run([]string{"apps", "inspect", "web", "--json"}, &stdout, &stderr, fakeFactory(f))
	if code != exitOK {
		t.Fatalf("inspect --json exit = %d, stderr = %q", code, stderr.String())
	}
	var out inspectOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if out.Panel.ApplicationID != "app-a" || out.Panel.InstanceID != "inst-a" || out.Panel.Generation != "3" {
		t.Fatalf("unexpected panel info: %+v", out.Panel)
	}
	if out.Paths.Home != "/opt/panel/apps/app-a" || out.Paths.InstanceDir != "/opt/panel/apps/app-a/instances/inst-a" || out.Paths.PersistentDir != "/opt/panel/apps/app-a/persistent" {
		t.Fatalf("unexpected paths: %+v", out.Paths)
	}
}

func TestRunWhere(t *testing.T) {
	home := t.TempDir()
	f := &fakeRuntime{containers: []agentcontract.DockerContainer{
		managedContainer("aaaaaaaaaaaa111111111111", "web", "app-a", "inst-a"),
	}}
	f.home = home
	var stdout, stderr bytes.Buffer
	if code := run([]string{"apps", "where", "web"}, &stdout, &stderr, fakeFactory(f)); code != exitOK {
		t.Fatalf("where exit = %d, stderr = %q", code, stderr.String())
	}
	if stdout.String() != home+"\n" {
		t.Fatalf("where output = %q, want %q", stdout.String(), home+"\n")
	}
}

func TestRunWhereMissingDir(t *testing.T) {
	f := &fakeRuntime{containers: []agentcontract.DockerContainer{
		managedContainer("aaaaaaaaaaaa111111111111", "web", "app-a", "inst-a"),
	}}
	f.home = filepath.Join(t.TempDir(), "missing")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"apps", "where", "web"}, &stdout, &stderr, fakeFactory(f)); code != exitError {
		t.Fatalf("where missing dir: got exit %d, want %d", code, exitError)
	}
}

func TestRunWhereUsage(t *testing.T) {
	f := &fakeRuntime{}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"apps", "where"}, &stdout, &stderr, fakeFactory(f)); code != exitUsage {
		t.Fatalf("where no selector: got exit %d, want %d", code, exitUsage)
	}
}

func TestRunCd(t *testing.T) {
	home := t.TempDir()
	orig := execShell
	defer func() { execShell = orig }()
	var gotDir string
	execShell = func(dir string) error {
		gotDir = dir
		return nil
	}
	f := &fakeRuntime{containers: []agentcontract.DockerContainer{
		managedContainer("aaaaaaaaaaaa111111111111", "web", "app-a", "inst-a"),
	}}
	f.home = home
	var stdout, stderr bytes.Buffer
	if code := run([]string{"apps", "cd", "web"}, &stdout, &stderr, fakeFactory(f)); code != exitOK {
		t.Fatalf("cd exit = %d, stderr = %q", code, stderr.String())
	}
	if gotDir != home {
		t.Fatalf("cd dir = %q, want %q", gotDir, home)
	}
}

func TestRunCdMissingDir(t *testing.T) {
	orig := execShell
	defer func() { execShell = orig }()
	execShell = func(dir string) error { t.Fatal("shell must not start for missing dir"); return nil }
	f := &fakeRuntime{containers: []agentcontract.DockerContainer{
		managedContainer("aaaaaaaaaaaa111111111111", "web", "app-a", "inst-a"),
	}}
	f.home = filepath.Join(t.TempDir(), "missing")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"apps", "cd", "web"}, &stdout, &stderr, fakeFactory(f)); code != exitError {
		t.Fatalf("cd missing dir: got exit %d, want %d", code, exitError)
	}
}
