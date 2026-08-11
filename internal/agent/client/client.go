package client

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	agentcontract "panel/internal/agent/contract"
	agentpb "panel/internal/agent/pb"
	agentrpc "panel/internal/agent/rpc"
	agentsecurity "panel/internal/agent/security"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/linux"
	"panel/internal/platform/linux/remoteops"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type GRPCClient struct {
	mu        sync.RWMutex
	tlsAssets *agentsecurity.TLSAssets
	timeout   time.Duration
}

const dockerImagePullTimeout = 15 * time.Minute
const maintenanceTimeout = 65 * time.Minute

// prepareRestartTimeout bounds how long PrepareRestart waits for the agent to
// become ready to restart. It is a var so tests can lower it; production
// behavior is fixed and not configurable.
var prepareRestartTimeout = 10 * time.Minute

type ReportConfig struct {
	ServerID                  string
	MetricsIntervalSeconds    int
	ContainersIntervalSeconds int
}

type AgentReport struct {
	SampleAt       time.Time
	Metrics        *linux.MetricsSnapshot
	Containers     []agentcontract.DockerContainer
	HasContainers  bool
	Reason         string
	PackageUpdates []linux.PackageUpdate
	Images         []agentcontract.DockerImage
}

func NewGRPCClient(tlsAssets *agentsecurity.TLSAssets, timeout time.Duration) (*GRPCClient, error) {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	client := &GRPCClient{timeout: timeout}
	if err := client.ReloadTLSAssets(tlsAssets); err != nil {
		return nil, err
	}
	return client, nil
}

func (c *GRPCClient) ReloadTLSAssets(tlsAssets *agentsecurity.TLSAssets) error {
	if tlsAssets == nil {
		return fmt.Errorf("agent tls assets are not configured")
	}
	if _, err := tlsAssets.ClientTLSConfig(); err != nil {
		return err
	}
	c.mu.Lock()
	c.tlsAssets = tlsAssets
	c.mu.Unlock()
	return nil
}

func (c *GRPCClient) Health(ctx context.Context, endpoint string) (agentcontract.HealthResponse, error) {
	var pr peer.Peer
	out, err := callRPC(c, ctx, endpoint, c.timeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.HealthResponse, error) {
		return client.Health(ctx, &agentpb.Empty{}, grpc.Peer(&pr))
	})
	if err != nil {
		return agentcontract.HealthResponse{}, err
	}
	health := agentrpc.ContractHealth(out)
	if tlsInfo, ok := pr.AuthInfo.(credentials.TLSInfo); ok && len(tlsInfo.State.PeerCertificates) > 0 {
		cert := tlsInfo.State.PeerCertificates[0]
		sum := sha256.Sum256(cert.Raw)
		health.Certificate = &agentcontract.CertificateInfo{
			Fingerprint: fmt.Sprintf("%X", sum[:]),
			CommonName:  cert.Subject.CommonName,
			NotBefore:   cert.NotBefore,
			NotAfter:    cert.NotAfter,
		}
	}
	return health, nil
}

func (c *GRPCClient) OSRelease(ctx context.Context, endpoint string) (linux.OSRelease, error) {
	out, err := callRPC(c, ctx, endpoint, c.timeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.OSReleaseResponse, error) {
		return client.OSRelease(ctx, &agentpb.Empty{})
	})
	if err != nil {
		return linux.OSRelease{}, err
	}
	return agentrpc.GoOSRelease(out), nil
}

func (c *GRPCClient) SystemTraits(ctx context.Context, endpoint string) (map[string]string, error) {
	out, err := callRPC(c, ctx, endpoint, c.timeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.SystemTraitsResponse, error) {
		return client.SystemTraits(ctx, &agentpb.Empty{})
	})
	if err != nil {
		return nil, err
	}
	if out.Traits == nil {
		out.Traits = map[string]string{}
	}
	return out.Traits, nil
}

func (c *GRPCClient) MetricsSnapshot(ctx context.Context, endpoint string, serverID string) (linux.MetricsSnapshot, error) {
	out, err := callRPC(c, ctx, endpoint, c.timeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.MetricsSnapshotResponse, error) {
		return client.MetricsSnapshot(ctx, &agentpb.MetricsSnapshotRequest{ServerId: serverID})
	})
	if err != nil {
		return linux.MetricsSnapshot{}, err
	}
	return agentrpc.GoSnapshot(out), nil
}

func (c *GRPCClient) UFWStatus(ctx context.Context, endpoint string) (remoteops.UFWStatus, error) {
	out, err := callRPC(c, ctx, endpoint, c.timeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.UFWStatusResponse, error) {
		return client.UFWStatus(ctx, &agentpb.Empty{})
	})
	return agentrpc.GoUFWStatus(out), err
}

func (c *GRPCClient) PackageUpdates(ctx context.Context, endpoint string) ([]linux.PackageUpdate, error) {
	out, err := callRPC(c, ctx, endpoint, maintenanceTimeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.PackageUpdatesResponse, error) {
		return client.PackageUpdates(ctx, &agentpb.Empty{})
	})
	if err != nil {
		return nil, err
	}
	return agentrpc.GoPackageUpdates(out.Items), nil
}

func (c *GRPCClient) UpgradePackages(ctx context.Context, endpoint string, req agentcontract.PackageUpgradeRequest) (agentcontract.CommandResponse, error) {
	out, err := callRPC(c, ctx, endpoint, maintenanceTimeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.CommandResponse, error) {
		return client.UpgradePackages(ctx, &agentpb.PackageUpgradeRequest{Names: append([]string(nil), req.Names...), All: req.All})
	})
	if out == nil {
		out = &agentpb.CommandResponse{}
	}
	return agentcontract.CommandResponse{Output: out.Output}, err
}

// PrepareRestart opens the agent restart-readiness stream and blocks until the
// agent reports "ready" or the stream/context ends. Agents that do not support
// the RPC return an error so callers can fall back to proceeding directly.
func (c *GRPCClient) PrepareRestart(ctx context.Context, endpoint string) error {
	// The readiness stream is expected to end quickly after a package upgrade,
	// but a broken agent must not be able to hold a deployment open forever.
	// The deadline covers both connection establishment and the stream.
	waitCtx, cancel := context.WithTimeout(ctx, prepareRestartTimeout)
	defer cancel()
	conn, err := c.dial(waitCtx, endpoint)
	if err != nil {
		return err
	}
	defer conn.Close()
	stream, err := agentpb.NewAgentServiceClient(conn).PrepareRestart(waitCtx, &agentpb.Empty{})
	if err != nil {
		return wrapAgentError(err)
	}
	for {
		msg, err := stream.Recv()
		if err != nil {
			if ctxErr := waitCtx.Err(); ctxErr != nil {
				return ctxErr
			}
			return wrapAgentError(err)
		}
		if msg.GetState() == agentcontract.PrepareRestartStateReady {
			return nil
		}
	}
}

func (c *GRPCClient) UFWInstall(ctx context.Context, endpoint string, req agentcontract.UFWInstallRequest) (remoteops.UFWStatus, error) {
	rules := make([]*agentpb.UFWRule, 0, len(req.Rules))
	for _, rule := range req.Rules {
		rules = append(rules, agentrpc.PBUFWRule(rule))
	}
	out, err := callRPC(c, ctx, endpoint, maintenanceTimeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.UFWStatusResponse, error) {
		return client.UFWInstall(ctx, &agentpb.UFWInstallRequest{Rules: rules})
	})
	return agentrpc.GoUFWStatus(out), err
}

func (c *GRPCClient) UFWEnable(ctx context.Context, endpoint string, req agentcontract.UFWEnableRequest) (remoteops.UFWStatus, error) {
	out, err := callRPC(c, ctx, endpoint, maintenanceTimeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.UFWStatusResponse, error) {
		return client.UFWEnable(ctx, &agentpb.UFWEnableRequest{SshPort: int32(req.SSHPort)})
	})
	return agentrpc.GoUFWStatus(out), err
}

func (c *GRPCClient) UFWAllow(ctx context.Context, endpoint string, req agentcontract.UFWAllowRequest) (remoteops.UFWStatus, error) {
	out, err := callRPC(c, ctx, endpoint, maintenanceTimeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.UFWStatusResponse, error) {
		return client.UFWAllow(ctx, &agentpb.UFWAllowRequest{Rule: agentrpc.PBUFWRule(req.Rule)})
	})
	return agentrpc.GoUFWStatus(out), err
}

func (c *GRPCClient) UFWDelete(ctx context.Context, endpoint string, req agentcontract.UFWDeleteRequest) (remoteops.UFWStatus, error) {
	out, err := callRPC(c, ctx, endpoint, maintenanceTimeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.UFWStatusResponse, error) {
		return client.UFWDelete(ctx, &agentpb.UFWDeleteRequest{Number: int32(req.Number)})
	})
	return agentrpc.GoUFWStatus(out), err
}

func (c *GRPCClient) Fail2BanStatus(ctx context.Context, endpoint string) (agentcontract.Fail2BanStatusResponse, error) {
	out, err := callRPC(c, ctx, endpoint, maintenanceTimeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.Fail2BanStatusResponse, error) {
		return client.Fail2BanStatus(ctx, &agentpb.Empty{})
	})
	return agentrpc.GoFail2BanStatus(out), err
}

func (c *GRPCClient) ApplyFail2Ban(ctx context.Context, endpoint string, req agentcontract.Fail2BanApplyRequest) (agentcontract.Fail2BanStatusResponse, error) {
	out, err := callRPC(c, ctx, endpoint, maintenanceTimeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.Fail2BanStatusResponse, error) {
		return client.ApplyFail2Ban(ctx, &agentpb.Fail2BanApplyRequest{Config: agentrpc.PBFail2BanConfig(req.Config)})
	})
	return agentrpc.GoFail2BanStatus(out), err
}

func (c *GRPCClient) ReleaseFail2Ban(ctx context.Context, endpoint string) (agentcontract.Fail2BanStatusResponse, error) {
	out, err := callRPC(c, ctx, endpoint, maintenanceTimeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.Fail2BanStatusResponse, error) {
		return client.ReleaseFail2Ban(ctx, &agentpb.Empty{})
	})
	return agentrpc.GoFail2BanStatus(out), err
}

func (c *GRPCClient) RestartSystem(ctx context.Context, endpoint string) error {
	_, err := callRPC(c, ctx, endpoint, maintenanceTimeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.OKResponse, error) {
		return client.RestartSystem(ctx, &agentpb.Empty{})
	})
	return err
}

func (c *GRPCClient) RuntimeWriteFiles(ctx context.Context, endpoint string, req agentcontract.RuntimeWriteFilesRequest) error {
	_, err := callRPC(c, ctx, endpoint, c.timeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.OKResponse, error) {
		return client.RuntimeWriteFiles(ctx, &agentpb.RuntimeWriteFilesRequest{Spec: agentrpc.PBSpec(req.Spec)})
	})
	return err
}

func (c *GRPCClient) RuntimeReload(ctx context.Context, endpoint string, req agentcontract.RuntimeReloadRequest) (agentcontract.RuntimeReloadResponse, error) {
	out, err := callRPC(c, ctx, endpoint, c.timeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.RuntimeReloadResponse, error) {
		return client.RuntimeReload(ctx, &agentpb.RuntimeReloadRequest{
			Spec:            agentrpc.PBSpec(req.Spec),
			ContainerName:   req.ContainerName,
			ValidateCommand: append([]string(nil), req.ValidateCommand...),
			ReloadCommand:   append([]string(nil), req.ReloadCommand...),
		})
	})
	if out == nil {
		out = &agentpb.RuntimeReloadResponse{}
	}
	return agentcontract.RuntimeReloadResponse{
		Reloaded: out.Reloaded,
		Phase:    out.Phase,
		ExitCode: int(out.ExitCode),
		Output:   out.Output,
		Error:    out.Error,
	}, err
}

func (c *GRPCClient) RuntimeCreateContainer(ctx context.Context, endpoint string, req agentcontract.RuntimeCreateContainerRequest) (agentcontract.RuntimeCreateContainerResponse, error) {
	out, err := callRPC(c, ctx, endpoint, c.timeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.RuntimeCreateContainerResponse, error) {
		return client.RuntimeCreateContainer(ctx, &agentpb.RuntimeCreateContainerRequest{ServerId: req.ServerID, Spec: agentrpc.PBSpec(req.Spec)})
	})
	if out == nil {
		out = &agentpb.RuntimeCreateContainerResponse{}
	}
	return agentcontract.RuntimeCreateContainerResponse{ContainerID: out.ContainerId}, err
}

func (c *GRPCClient) RuntimeStop(ctx context.Context, endpoint string, req agentcontract.RuntimeStopRequest) (agentcontract.RuntimeInstanceResponse, error) {
	out, err := callRPC(c, ctx, endpoint, c.timeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.RuntimeInstanceResponse, error) {
		return client.RuntimeStop(ctx, &agentpb.RuntimeStopRequest{ApplicationId: req.ApplicationID, InstanceId: req.InstanceID, ContainerName: req.ContainerName, Purge: req.Purge, RemoveApplicationData: req.RemoveApplicationData})
	})
	return agentrpc.GoRuntimeInstance(out), err
}

func (c *GRPCClient) RuntimeRestart(ctx context.Context, endpoint string, req agentcontract.RuntimeRestartRequest) (agentcontract.RuntimeInstanceResponse, error) {
	out, err := callRPC(c, ctx, endpoint, c.timeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.RuntimeInstanceResponse, error) {
		return client.RuntimeRestart(ctx, &agentpb.RuntimeRestartRequest{InstanceId: req.InstanceID, ContainerName: req.ContainerName})
	})
	return agentrpc.GoRuntimeInstance(out), err
}

func (c *GRPCClient) RuntimeStatus(ctx context.Context, endpoint, instanceID, containerName string) (agentcontract.RuntimeStatusResponse, error) {
	out, err := callRPC(c, ctx, endpoint, c.timeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.RuntimeStatusResponse, error) {
		return client.RuntimeStatus(ctx, &agentpb.RuntimeStatusRequest{InstanceId: instanceID, ContainerName: containerName})
	})
	return agentrpc.GoRuntimeStatus(out), err
}

func (c *GRPCClient) RuntimeLogs(ctx context.Context, endpoint, instanceID, containerName string, tail int) (agentcontract.RuntimeLogsResponse, error) {
	out, err := callRPC(c, ctx, endpoint, c.timeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.RuntimeLogsResponse, error) {
		return client.RuntimeLogs(ctx, &agentpb.RuntimeLogsRequest{InstanceId: instanceID, ContainerName: containerName, Tail: int32(tail)})
	})
	if out == nil {
		out = &agentpb.RuntimeLogsResponse{}
	}
	return agentcontract.RuntimeLogsResponse{InstanceID: out.InstanceId, Logs: out.Logs}, err
}

func (c *GRPCClient) RuntimePersistentArchive(ctx context.Context, endpoint, applicationID string) (agentcontract.RuntimePersistentArchiveResponse, error) {
	out, err := callRPC(c, ctx, endpoint, c.timeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.RuntimePersistentArchiveResponse, error) {
		return client.RuntimePersistentArchive(ctx, &agentpb.RuntimePersistentArchiveRequest{ApplicationId: applicationID})
	})
	if err != nil {
		return agentcontract.RuntimePersistentArchiveResponse{}, err
	}
	return agentcontract.RuntimePersistentArchiveResponse{
		ApplicationID: out.ApplicationId,
		Filename:      out.Filename,
		ContentBase64: base64.StdEncoding.EncodeToString(out.Content),
	}, nil
}

func (c *GRPCClient) RuntimePersistentRestore(ctx context.Context, endpoint, applicationID string, content []byte) (agentcontract.RuntimePersistentRestoreResponse, error) {
	out, err := callRPC(c, ctx, endpoint, c.timeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.RuntimePersistentRestoreResponse, error) {
		return client.RuntimePersistentRestore(ctx, &agentpb.RuntimePersistentRestoreRequest{ApplicationId: applicationID, Content: append([]byte(nil), content...)})
	})
	if out == nil {
		out = &agentpb.RuntimePersistentRestoreResponse{}
	}
	return agentcontract.RuntimePersistentRestoreResponse{ApplicationID: out.ApplicationId, Restored: out.Restored}, err
}

func (c *GRPCClient) DockerContainers(ctx context.Context, endpoint string) ([]agentcontract.DockerContainer, error) {
	out, err := callRPC(c, ctx, endpoint, c.timeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.DockerContainersResponse, error) {
		return client.DockerContainers(ctx, &agentpb.Empty{})
	})
	if err != nil {
		return nil, err
	}
	items := make([]agentcontract.DockerContainer, 0, len(out.Items))
	for _, item := range out.Items {
		items = append(items, agentrpc.GoDockerContainer(item))
	}
	return items, nil
}

func (c *GRPCClient) DockerContainerLogs(ctx context.Context, endpoint, id string, tail int) (agentcontract.DockerContainerLogsResponse, error) {
	out, err := callRPC(c, ctx, endpoint, c.timeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.DockerContainerLogsResponse, error) {
		return client.DockerContainerLogs(ctx, &agentpb.DockerContainerLogsRequest{Id: id, Tail: int32(tail)})
	})
	if out == nil {
		out = &agentpb.DockerContainerLogsResponse{}
	}
	return agentcontract.DockerContainerLogsResponse{ContainerID: out.ContainerId, Logs: out.Logs}, err
}

func (c *GRPCClient) DockerContainerAction(ctx context.Context, endpoint, id, action string) error {
	_, err := callRPC(c, ctx, endpoint, c.timeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.OKResponse, error) {
		return client.DockerContainerAction(ctx, &agentpb.DockerContainerActionRequest{Id: id, Action: action})
	})
	return err
}

func (c *GRPCClient) DockerContainerDelete(ctx context.Context, endpoint, id string) error {
	_, err := callRPC(c, ctx, endpoint, c.timeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.OKResponse, error) {
		return client.DockerContainerDelete(ctx, &agentpb.DockerContainerDeleteRequest{Id: id})
	})
	return err
}

func (c *GRPCClient) DockerImages(ctx context.Context, endpoint string) ([]agentcontract.DockerImage, error) {
	out, err := callRPC(c, ctx, endpoint, c.timeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.DockerImagesResponse, error) {
		return client.DockerImages(ctx, &agentpb.Empty{})
	})
	if err != nil {
		return nil, err
	}
	items := make([]agentcontract.DockerImage, 0, len(out.Items))
	for _, item := range out.Items {
		items = append(items, agentrpc.GoDockerImage(item))
	}
	return items, nil
}

func (c *GRPCClient) DockerImagePull(ctx context.Context, endpoint, reference string) error {
	_, err := callRPC(c, ctx, endpoint, dockerImagePullTimeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.OKResponse, error) {
		return client.DockerImagePull(ctx, &agentpb.DockerImagePullRequest{Reference: reference})
	})
	return err
}

func (c *GRPCClient) DockerImageDelete(ctx context.Context, endpoint, id string) error {
	_, err := callRPC(c, ctx, endpoint, c.timeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.OKResponse, error) {
		return client.DockerImageDelete(ctx, &agentpb.DockerImageDeleteRequest{Id: id})
	})
	return err
}

func (c *GRPCClient) DockerNetworks(ctx context.Context, endpoint string) ([]agentcontract.DockerNetwork, error) {
	out, err := callRPC(c, ctx, endpoint, c.timeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.DockerNetworksResponse, error) {
		return client.DockerNetworks(ctx, &agentpb.Empty{})
	})
	if err != nil {
		return nil, err
	}
	items := make([]agentcontract.DockerNetwork, 0, len(out.Items))
	for _, item := range out.Items {
		items = append(items, agentrpc.GoDockerNetwork(item))
	}
	return items, nil
}

func (c *GRPCClient) DockerVolumes(ctx context.Context, endpoint string) ([]agentcontract.DockerVolume, error) {
	out, err := callRPC(c, ctx, endpoint, c.timeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.DockerVolumesResponse, error) {
		return client.DockerVolumes(ctx, &agentpb.Empty{})
	})
	if err != nil {
		return nil, err
	}
	items := make([]agentcontract.DockerVolume, 0, len(out.Items))
	for _, item := range out.Items {
		items = append(items, agentrpc.GoDockerVolume(item))
	}
	return items, nil
}

func (c *GRPCClient) DockerVolumeDelete(ctx context.Context, endpoint, name string) error {
	_, err := callRPC(c, ctx, endpoint, c.timeout, func(ctx context.Context, client agentpb.AgentServiceClient) (*agentpb.OKResponse, error) {
		return client.DockerVolumeDelete(ctx, &agentpb.DockerVolumeDeleteRequest{Name: name})
	})
	return err
}

func (c *GRPCClient) StreamReports(ctx context.Context, endpoint string, config func() ReportConfig, handle func(context.Context, AgentReport) error) error {
	conn, err := c.dial(ctx, endpoint)
	if err != nil {
		return err
	}
	defer conn.Close()
	stream, err := agentpb.NewAgentReportServiceClient(conn).Report(ctx)
	if err != nil {
		return wrapAgentError(err)
	}
	errCh := make(chan error, 2)
	sendConfig := func(cfg ReportConfig) error {
		return stream.Send(&agentpb.AgentReportControl{
			ServerId: cfg.ServerID,
			Config: &agentpb.AgentReportConfig{
				MetricsIntervalSeconds:    int32(cfg.MetricsIntervalSeconds),
				ContainersIntervalSeconds: int32(cfg.ContainersIntervalSeconds),
			},
		})
	}
	go func() {
		var last ReportConfig
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			next := config()
			if next != last {
				if err := sendConfig(next); err != nil {
					errCh <- err
					return
				}
				last = next
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errCh:
			return wrapAgentError(err)
		default:
		}
		msg, err := stream.Recv()
		if err != nil {
			return wrapAgentError(err)
		}
		report := AgentReport{Reason: msg.Reason}
		if msg.SampleAt != nil {
			report.SampleAt = msg.SampleAt.AsTime().UTC().Truncate(time.Second)
		}
		if msg.Metrics != nil {
			snap := agentrpc.GoSnapshot(msg.Metrics)
			report.Metrics = &snap
		}
		if msg.Containers != nil {
			report.HasContainers = true
			report.Containers = make([]agentcontract.DockerContainer, 0, len(msg.Containers.Items))
			for _, item := range msg.Containers.Items {
				report.Containers = append(report.Containers, agentrpc.GoDockerContainer(item))
			}
		}
		if msg.PackageUpdates != nil {
			report.PackageUpdates = agentrpc.GoPackageUpdates(msg.PackageUpdates.Items)
		}
		if msg.Images != nil {
			report.Images = make([]agentcontract.DockerImage, 0, len(msg.Images.Items))
			for _, item := range msg.Images.Items {
				report.Images = append(report.Images, agentrpc.GoDockerImage(item))
			}
		}
		if handle != nil {
			if err := handle(ctx, report); err != nil {
				return err
			}
		}
	}
}

func callRPC[T any](c *GRPCClient, ctx context.Context, endpoint string, timeout time.Duration, fn func(context.Context, agentpb.AgentServiceClient) (T, error)) (T, error) {
	var zero T
	callCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	conn, err := c.dial(callCtx, endpoint)
	if err != nil {
		return zero, err
	}
	defer conn.Close()
	out, err := fn(callCtx, agentpb.NewAgentServiceClient(conn))
	if err != nil {
		return zero, wrapAgentError(err)
	}
	return out, nil
}

func (c *GRPCClient) dial(ctx context.Context, endpoint string) (*grpc.ClientConn, error) {
	target, err := grpcTarget(endpoint)
	if err != nil {
		return nil, err
	}
	c.mu.RLock()
	tlsAssets := c.tlsAssets
	c.mu.RUnlock()
	opts := []grpc.DialOption{
		// grpc.NewClient dials lazily on the first RPC; an explicit context
		// dialer makes connection establishment honor the call's deadline.
		grpc.WithContextDialer(func(dialCtx context.Context, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(dialCtx, "tcp", addr)
		}),
	}
	if tlsAssets == nil {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		tlsConfig, err := tlsAssets.ClientTLSConfig()
		if err != nil {
			return nil, err
		}
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	}
	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func grpcTarget(endpoint string) (string, error) {
	value := strings.TrimSpace(endpoint)
	if value == "" {
		return "", fmt.Errorf("agent endpoint is empty")
	}
	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil {
			return "", err
		}
		value = parsed.Host
	}
	if value == "" {
		return "", fmt.Errorf("agent endpoint is empty")
	}
	if _, _, err := net.SplitHostPort(value); err != nil {
		return "", err
	}
	return value, nil
}

func wrapAgentError(err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	message := err.Error()
	if ok && st.Message() != "" {
		message = st.Message()
	}
	if !ok {
		return panelerr.BadGateway("agent_request_failed", fmt.Sprintf("Agent request failed: %s", message))
	}
	// Preserve the gRPC code semantics so clearly invalid or denied requests
	// surface as client-side errors instead of always becoming a 502.
	switch st.Code() {
	case codes.InvalidArgument, codes.FailedPrecondition:
		return panelerr.Validation("agent_request_invalid", fmt.Sprintf("Agent request invalid: %s", message))
	case codes.NotFound:
		return panelerr.NotFound("agent resource")
	case codes.PermissionDenied:
		return panelerr.Forbidden("agent_request_forbidden", fmt.Sprintf("Agent request forbidden: %s", message))
	case codes.DeadlineExceeded:
		return panelerr.Timeout(message)
	default:
		return panelerr.BadGateway("agent_request_failed", fmt.Sprintf("Agent request failed: %s", message))
	}
}
