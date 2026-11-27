package facilityapps

import (
	"context"
	"errors"
	"testing"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/servers"
)

// fakeProxyImageVersionAgent 在通用设施 fake 之上提供入口代理容器采集与版本
// 采集结果，用于覆盖检查任务的 image_version 持久化与投影。
type fakeProxyImageVersionAgent struct {
	fakeStorageAgent
	containers     []agentcontract.DockerContainer
	images         []agentcontract.DockerImage
	version        string
	versionErr     error
	versionCalls   int
	versionCommand []string
}

func (f *fakeProxyImageVersionAgent) DockerContainers(context.Context, string) ([]agentcontract.DockerContainer, error) {
	return f.containers, nil
}

func (f *fakeProxyImageVersionAgent) DockerImages(context.Context, string) ([]agentcontract.DockerImage, error) {
	return f.images, nil
}

func (f *fakeProxyImageVersionAgent) RuntimeStatusWithVersion(_ context.Context, _ string, instanceID, containerName string, command []string) (agentcontract.RuntimeStatusResponse, error) {
	f.versionCalls++
	f.versionCommand = append([]string(nil), command...)
	if f.versionErr != nil {
		return agentcontract.RuntimeStatusResponse{}, f.versionErr
	}
	return agentcontract.RuntimeStatusResponse{ImageVersion: f.version}, nil
}

// newProxyImageVersionTestService seeds one gateway, a managed running proxy
// container and a registry digest matching the running digest so the check
// reaches the version collection branch instead of reporting an update.
func newProxyImageVersionTestService(t *testing.T, version string, versionErr error) (*Service, string, *fakeProxyImageVersionAgent, func()) {
	t.Helper()
	svc, _, closeStore := newFacilityEditTestService(t)
	seedP1ProxyGateway(t, svc)
	digest := p1Digest('a')
	svc.imageResolver = facilityTestImageResolver{digest: digest}
	svc.servers = facilityTestServers{items: map[string]server.Server{
		"srv-p1": {
			ID: "srv-p1",
			Traits: map[string]string{
				agentcontract.TraitURL:    "https://127.0.0.1:17890",
				agentcontract.TraitStatus: agentcontract.StatusCompatible,
			},
		},
	}}
	agent := &fakeProxyImageVersionAgent{
		version:    version,
		versionErr: versionErr,
		containers: []agentcontract.DockerContainer{{
			ID:    "container-proxy",
			Names: []string{"/" + proxyContainerName},
			Image: supportedProxyImage + "@" + digest,
			Labels: map[string]string{
				"panel.application.managed": "true",
				"panel.application.id":      proxyApplicationID,
			},
		}},
	}
	svc.agent = agent
	return svc, digest, agent, closeStore
}

func TestProxyImageCheckPersistsRuntimeImageVersion(t *testing.T) {
	svc, _, agent, closeStore := newProxyImageVersionTestService(t, "1.28.3", nil)
	defer closeStore()
	ctx := context.Background()

	status, err := svc.runProxyImageCheck(ctx, svc.newProxyImageReporter(ctx, ""))
	if err != nil {
		t.Fatalf("runProxyImageCheck() error = %v", err)
	}
	if status.Status != ProxyImageStatusUpToDate {
		t.Fatalf("aggregate status = %q, want %q", status.Status, ProxyImageStatusUpToDate)
	}
	if status.ImageVersion != "1.28.3" {
		t.Fatalf("aggregate imageVersion = %q, want 1.28.3", status.ImageVersion)
	}
	if len(status.Servers) != 1 || status.Servers[0].ImageVersion != "1.28.3" {
		t.Fatalf("per-gateway imageVersion = %#v", status.Servers)
	}
	state, err := svc.loadProxyImageState(ctx, "srv-p1")
	if err != nil {
		t.Fatal(err)
	}
	if state.ImageVersion != "1.28.3" {
		t.Fatalf("persisted image_version = %q, want 1.28.3", state.ImageVersion)
	}
	// GET 反代配置的 image 投影必须带上同一版本。
	config, err := svc.GetReverseProxy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if config.Image.ImageVersion != "1.28.3" {
		t.Fatalf("projected imageVersion = %q, want 1.28.3", config.Image.ImageVersion)
	}
	if agent.versionCalls != 1 {
		t.Fatalf("RuntimeStatusWithVersion calls = %d, want 1", agent.versionCalls)
	}
	if len(agent.versionCommand) != 2 || agent.versionCommand[0] != "nginx" || agent.versionCommand[1] != "-v" {
		t.Fatalf("version command = %#v, want [nginx -v]", agent.versionCommand)
	}
}

func TestProxyImageCheckPersistsEmptyVersionWhenUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name       string
		version    string
		versionErr error
	}{
		{name: "old agent returns no version"},
		{name: "version probe fails", versionErr: errors.New("container exec failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _, closeStore := newProxyImageVersionTestService(t, tc.version, tc.versionErr)
			defer closeStore()
			ctx := context.Background()

			status, err := svc.runProxyImageCheck(ctx, svc.newProxyImageReporter(ctx, ""))
			if err != nil {
				// A version observation failure must not fail the digest check.
				t.Fatalf("runProxyImageCheck() error = %v", err)
			}
			if status.Status != ProxyImageStatusUpToDate {
				t.Fatalf("aggregate status = %q, want %q", status.Status, ProxyImageStatusUpToDate)
			}
			if status.ImageVersion != "" {
				t.Fatalf("aggregate imageVersion = %q, want empty", status.ImageVersion)
			}
			if len(status.Servers) != 1 || status.Servers[0].ImageVersion != "" {
				t.Fatalf("per-gateway imageVersion = %#v", status.Servers)
			}
			state, err := svc.loadProxyImageState(ctx, "srv-p1")
			if err != nil {
				t.Fatal(err)
			}
			if state.ImageVersion != "" {
				t.Fatalf("persisted image_version = %q, want empty", state.ImageVersion)
			}
		})
	}
}
