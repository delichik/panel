package rpc

import (
	"reflect"
	"testing"
	"time"

	agentcontract "panel/internal/agent/contract"
	appruntime "panel/internal/modules/applications/runtime"
	"panel/internal/platform/linux"
	"panel/internal/platform/linux/remoteops"
)

func TestHealthRoundTrip(t *testing.T) {
	in := agentcontract.HealthResponse{
		Status:       "ok",
		Time:         "2026-07-01T12:34:56Z",
		Version:      "1.2.3",
		Capabilities: []string{"health", "docker-containers"},
		ContractHash: "hash-123",
		Docker: agentcontract.DockerHealth{
			Host:   "unix:///var/run/docker.sock",
			Status: agentcontract.StatusCompatible,
			Error:  "",
		},
	}

	got := contractHealth(pbHealth(in))
	assertDeepEqual(t, got, in)
}

func TestOSReleaseRoundTrip(t *testing.T) {
	in := linux.OSRelease{
		ID:         "ubuntu",
		VersionID:  "24.04",
		PrettyName: "Ubuntu 24.04.2 LTS",
		Supported:  true,
	}

	got := goOSRelease(pbOSRelease(in))
	assertDeepEqual(t, got, in)
}

func TestMetricsSnapshotRoundTripIncludesLoadAverages(t *testing.T) {
	snapshotTime := time.Date(2026, 7, 1, 8, 9, 10, 11, time.UTC)
	serverTime := time.Date(2026, 7, 1, 8, 10, 11, 12, time.UTC)
	in := linux.MetricsSnapshot{
		ServerID:           "server-1",
		Time:               snapshotTime,
		CPUUsagePercent:    42.5,
		MemoryUsedBytes:    2048,
		MemoryTotalBytes:   8192,
		DiskUsedBytes:      4096,
		DiskTotalBytes:     16384,
		NetworkRxBytesRate: 12.75,
		NetworkTxBytesRate: 34.25,
		Status: linux.SystemStatus{
			Hostname:      "node-a",
			KernelVersion: "6.8.0",
			OSVersion:     "Ubuntu 24.04",
			ServerTime:    serverTime,
			UptimeSeconds: 98765,
			LoadAverage:   "0.10 0.20 0.30",
			Load1:         0.10,
			Load5:         0.20,
			Load15:        0.30,
		},
	}

	got := goSnapshot(pbSnapshot(in))
	assertDeepEqual(t, got, in)
}

func TestUFWStatusRoundTrip(t *testing.T) {
	in := remoteops.UFWStatus{
		Installed: true,
		Active:    true,
		Status:    "active",
		Default:   "deny (incoming), allow (outgoing), disabled (routed)",
		Rules: []remoteops.UFWRuleStatus{
			{Number: 1, To: "22/tcp", Action: "ALLOW IN", From: "10.0.0.0/8"},
			{Number: 2, To: "443/tcp", Action: "ALLOW IN", From: "Anywhere"},
		},
		Raw: "ufw status output",
	}

	got := goUFWStatus(pbUFWStatus(in))
	assertDeepEqual(t, got, in)
}

func TestFail2BanRoundTrip(t *testing.T) {
	config := agentcontract.Fail2BanConfig{
		Jails: []agentcontract.Fail2BanJail{
			{
				Name:     "sshd",
				Enabled:  true,
				Preset:   "ssh",
				Filter:   "sshd",
				LogPath:  "/var/log/auth.log",
				Backend:  "systemd",
				Port:     "ssh",
				Protocol: "tcp",
				Action:   "iptables-multiport",
				MaxRetry: 5,
				FindTime: "10m",
				BanTime:  "1h",
				IgnoreIP: []string{"127.0.0.1/8", "10.0.0.0/8"},
				Options:  map[string]string{"mode": "aggressive"},
			},
		},
	}
	status := agentcontract.Fail2BanStatusResponse{
		Installed:          true,
		Active:             true,
		PanelConfigPresent: true,
		Jails:              []string{"sshd", "nginx-http-auth"},
		Raw:                "fail2ban-client status",
	}

	gotConfig := goFail2BanConfig(pbFail2BanConfig(config))
	assertDeepEqual(t, gotConfig, config)

	gotStatus := goFail2BanStatus(pbFail2BanStatus(status))
	assertDeepEqual(t, gotStatus, status)
}

func TestRuntimeSpecRoundTripIncludesWrappersFilesCapsAndRestart(t *testing.T) {
	uid := 1001
	gid := 1002
	in := appruntime.Spec{
		ID:            "spec-1",
		ApplicationID: "app-1",
		InstanceID:    "instance-1",
		ContainerName: "panel-app-1",
		Name:          "demo",
		Image:         "example/demo:latest",
		Command:       []string{"demo", "--serve"},
		Env:           map[string]string{"PANEL_ENV": "test"},
		Ports: []appruntime.Port{
			{Label: "http", ContainerPort: 8080, HostPort: 18080, Protocol: "tcp"},
		},
		NetworkMode: "bridge",
		Resources:   appruntime.Resources{CPU: 2, MemoryMB: 512},
		Privileged:  true,
		CapAdd:      []string{"NET_ADMIN", "SYS_TIME"},
		Mounts: []appruntime.Mount{
			{Type: "bind", Source: "/srv/app", Target: "/app/data", ReadOnly: true, UID: &uid, GID: &gid, Mode: "0750"},
			{Type: "volume", Source: "cache", Target: "/cache", Mode: "rw"},
		},
		Files: []appruntime.ManagedFile{
			{Path: "/app/config.yaml", Content: []byte("enabled: true\n"), Mode: "0640"},
		},
		Restart: appruntime.Restart{
			Policy:          "on-failure",
			Attempts:        3,
			IntervalSeconds: 30,
			DelaySeconds:    5,
			Mode:            "linear",
		},
		Services: []appruntime.Service{
			{Name: "web", Port: "8080", Tags: []string{"http", "public"}},
		},
		Checks: []appruntime.Check{
			{Name: "ready", Type: "http", Port: "8080", Path: "/health", IntervalSeconds: 10, TimeoutSeconds: 2, Command: "demo health"},
		},
		Generation: 7,
		SpecHash:   "sha256:abc",
	}

	pb := pbSpec(in)
	if pb.Mounts[0].Uid == nil || pb.Mounts[0].Uid.Value != int32(uid) {
		t.Fatalf("UID wrapper was not preserved: %#v", pb.Mounts[0].Uid)
	}
	if pb.Mounts[0].Gid == nil || pb.Mounts[0].Gid.Value != int32(gid) {
		t.Fatalf("GID wrapper was not preserved: %#v", pb.Mounts[0].Gid)
	}

	got := goSpec(pb)
	assertDeepEqual(t, got, in)
}

func TestDockerResourcesRoundTrip(t *testing.T) {
	container := agentcontract.DockerContainer{
		ID:      "container-1",
		Names:   []string{"/demo", "/demo-alias"},
		Image:   "example/demo:latest",
		ImageID: "sha256:image",
		Command: "demo --serve",
		Created: 1782921600,
		State:   "running",
		Status:  "Up 10 minutes",
		Ports: []agentcontract.DockerPort{
			{IP: "0.0.0.0", PrivatePort: 8080, PublicPort: 18080, Type: "tcp"},
			{PrivatePort: 8443, Type: "tcp"},
		},
		Labels: map[string]string{"app": "demo"},
		Mounts: []agentcontract.DockerMount{
			{Type: "bind", Name: "data", Source: "/srv/app", Destination: "/app/data", Driver: "local", Mode: "rw", RW: true},
		},
	}
	image := agentcontract.DockerImage{
		ID:          "sha256:image",
		ParentID:    "sha256:parent",
		RepoTags:    []string{"example/demo:latest"},
		RepoDigests: []string{"example/demo@sha256:digest"},
		Created:     1782921601,
		Size:        123456789,
		Containers:  2,
	}
	network := agentcontract.DockerNetwork{
		ID:       "network-1",
		Name:     "demo-net",
		Driver:   "bridge",
		Scope:    "local",
		Created:  "2026-07-01T00:00:00Z",
		Internal: true,
		Labels:   map[string]string{"tier": "backend"},
	}
	volume := agentcontract.DockerVolume{
		Name:           "demo-data",
		Driver:         "local",
		Mountpoint:     "/var/lib/docker/volumes/demo-data/_data",
		CreatedAt:      "2026-07-01T00:00:00Z",
		Labels:         map[string]string{"app": "demo"},
		UsageData:      &agentcontract.DockerVolumeUsage{Size: 987654321, RefCount: 3},
		InUse:          true,
		ContainerCount: 2,
	}

	assertDeepEqual(t, goDockerContainer(pbDockerContainer(container)), container)
	assertDeepEqual(t, goDockerImage(pbDockerImage(image)), image)
	assertDeepEqual(t, goDockerNetwork(pbDockerNetwork(network)), network)
	assertDeepEqual(t, goDockerVolume(pbDockerVolume(volume)), volume)
}

func assertDeepEqual[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round-trip mismatch\ngot:  %#v\nwant: %#v", got, want)
	}
}
