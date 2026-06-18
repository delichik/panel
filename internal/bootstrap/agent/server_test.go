package agent

import "testing"

func TestLoadConfigDefaults(t *testing.T) {
	t.Setenv("PANEL_AGENT_LISTEN_ADDRESS", "")
	t.Setenv("PANEL_AGENT_DOCKER_HOST", "")

	cfg := LoadConfig()
	if cfg.ListenAddress != "0.0.0.0:9786" {
		t.Fatalf("listen address = %q", cfg.ListenAddress)
	}
	if cfg.DockerHost != "unix:///var/run/docker.sock" {
		t.Fatalf("docker host = %q", cfg.DockerHost)
	}
}
