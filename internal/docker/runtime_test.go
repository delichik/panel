package docker

import (
	"context"
	"strings"
	"testing"

	"panel/internal/sshx"
)

func TestParseServicesReadsComposeAndPanelLabels(t *testing.T) {
	raw := `{"ID":"abc123","Image":"nginx:latest","Names":"web-1","State":"running","Status":"Up 3 minutes","Ports":"80/tcp","Labels":"com.docker.compose.project=demo,com.docker.compose.service=web,panel.managed=true,panel.service_id=svc_1"}`
	services, err := ParseServices(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(services) != 1 {
		t.Fatalf("expected one service, got %d", len(services))
	}
	got := services[0]
	if got.Project != "demo" || got.Service != "web" || !got.Managed {
		t.Fatalf("labels were not mapped: %#v", got)
	}
	if got.Labels["panel.service_id"] != "svc_1" {
		t.Fatalf("panel label missing: %#v", got.Labels)
	}
}

func TestParseNetworksVolumesImages(t *testing.T) {
	networks, err := ParseNetworks(`{"ID":"net1","Name":"bridge","Driver":"bridge","Scope":"local","Internal":"false","Labels":"panel.managed=true"}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(networks) != 1 || networks[0].Internal || !networks[0].Managed {
		t.Fatalf("unexpected networks: %#v", networks)
	}

	volumes, err := ParseVolumes(`{"Name":"data","Driver":"local","Scope":"local","Labels":"panel.managed=true"}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(volumes) != 1 || volumes[0].Name != "data" || !volumes[0].Managed {
		t.Fatalf("unexpected volumes: %#v", volumes)
	}

	images, err := ParseImages(`{"ID":"sha256:1","Repository":"redis","Tag":"7","Digest":"sha256:abc","Size":"45MB","Labels":"panel.managed=true"}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 || images[0].Repository != "redis" || images[0].Digest != "sha256:abc" || !images[0].Managed {
		t.Fatalf("unexpected images: %#v", images)
	}
}

func TestParseInvalidJSONFails(t *testing.T) {
	if _, err := ParseServices(`not-json`); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestDetectKeyValuesUnsupported(t *testing.T) {
	values := parseKeyValues("docker_installed=false\ncompose_installed=false\n")
	if values["docker_installed"] != "false" || values["compose_installed"] != "false" {
		t.Fatalf("unexpected values: %#v", values)
	}
}

func TestDockerCommandErrorIncludesStderr(t *testing.T) {
	err := dockerCommandError("docker_images_failed", sshx.CommandResult{Stderr: "permission denied while trying to connect to the Docker daemon socket"})
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("expected stderr in error, got %v", err)
	}
}

func TestManifestDigestFindsNestedDigest(t *testing.T) {
	got := manifestDigest(`{"Descriptor":{"digest":"sha256:latest"},"manifests":[{"digest":"sha256:other"}]}`)
	if got != "sha256:latest" {
		t.Fatalf("unexpected digest: %s", got)
	}
}

func TestComposeStatusParsesArrayOutput(t *testing.T) {
	raw := `[{"ID":"abc","Name":"demo-web-1","Service":"web","State":"running","Health":"","Image":"nginx","Labels":"com.docker.compose.project=demo"}]`
	services, err := parseComposeStatusServices(raw, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(services) != 1 || services[0].Project != "demo" || services[0].Service != "web" {
		t.Fatalf("unexpected services: %#v", services)
	}
}

func TestCLIRuntimeContainerMutationsUseContainerCommands(t *testing.T) {
	exec := &recordingExecutor{}
	runtime := NewCLIRuntime(exec)
	target := sshx.Target{ServerID: "server-1"}

	if err := runtime.StartContainer(context.Background(), target, "abc123"); err != nil {
		t.Fatal(err)
	}
	if err := runtime.StopContainer(context.Background(), target, "abc123"); err != nil {
		t.Fatal(err)
	}
	if err := runtime.DeleteContainer(context.Background(), target, "abc123"); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"docker container start 'abc123'",
		"docker container stop 'abc123'",
		"docker container rm 'abc123'",
	}
	if strings.Join(exec.commands, "\n") != strings.Join(want, "\n") {
		t.Fatalf("unexpected commands:\n%s", strings.Join(exec.commands, "\n"))
	}
}

type recordingExecutor struct {
	commands []string
}

func (e *recordingExecutor) Exec(_ context.Context, _ sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	e.commands = append(e.commands, command.Command)
	return sshx.CommandResult{}, nil
}

func (e *recordingExecutor) ExecSudo(ctx context.Context, target sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	return e.Exec(ctx, target, command)
}

func (e *recordingExecutor) Upload(context.Context, sshx.Target, sshx.UploadSpec) error { return nil }
func (e *recordingExecutor) Download(context.Context, sshx.Target, sshx.DownloadSpec) error {
	return nil
}
