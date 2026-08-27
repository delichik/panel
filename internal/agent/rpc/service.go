package rpc

import (
	"context"
	"path/filepath"
	"time"

	agentcontract "panel/internal/agent/contract"
	agentdocker "panel/internal/agent/docker"
	"panel/internal/agent/nfsvol"
	agentpb "panel/internal/agent/pb"
	agentstorage "panel/internal/agent/storage"
	agentsystem "panel/internal/agent/system"
	"panel/internal/platform/linux/remoteops"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Handler struct {
	agentpb.UnimplementedAgentServiceServer
	agentpb.UnimplementedAgentReportServiceServer
	collector agentsystem.LocalCollector
	runtime   *agentdocker.LocalRuntime
	reports   *reportHub
	upgrades  packageUpgradeTracker
}

type HandlerConfig struct {
	DockerHost string
}

func NewHandler(cfg ...HandlerConfig) *Handler {
	dockerHost := agentcontract.DefaultDockerHost
	if len(cfg) > 0 && cfg[0].DockerHost != "" {
		dockerHost = cfg[0].DockerHost
	}
	runtime, _ := agentdocker.NewLocalRuntime(dockerHost)
	collector := agentsystem.LocalCollector{}
	return &Handler{collector: collector, runtime: runtime, reports: newReportHub(collector, runtime)}
}

func RegisterAgentService(server *grpc.Server, handler *Handler) {
	agentpb.RegisterAgentServiceServer(server, handler)
}

func RegisterAgentReportService(server *grpc.Server, handler *Handler) {
	agentpb.RegisterAgentReportServiceServer(server, handler)
}

func remoteError(err error) error {
	if err == nil {
		return nil
	}
	return status.Error(codes.Internal, err.Error())
}

func (h *Handler) requireRuntime() error {
	if h.runtime == nil {
		return status.Error(codes.FailedPrecondition, "runtime is not configured")
	}
	return nil
}

func (h *Handler) Health(ctx context.Context, _ *agentpb.Empty) (*agentpb.HealthResponse, error) {
	docker := agentcontract.DockerHealth{Host: agentcontract.DefaultDockerHost, Status: agentcontract.StatusUnavailable, Error: "runtime is not configured"}
	if h.runtime != nil {
		docker = h.runtime.DockerHealth(ctx)
	}
	capabilities := append(append([]string(nil), agentcontract.RequiredCapabilities...), agentcontract.CapabilityPrepareRestart, agentcontract.CapabilityStorageShare)
	return pbHealth(agentcontract.HealthResponse{Status: "ok", Time: time.Now().UTC().Format(time.RFC3339Nano), Version: agentcontract.Version, Capabilities: capabilities, ContractHash: agentcontract.CurrentHash(), Docker: docker}), nil
}

func (h *Handler) OSRelease(ctx context.Context, _ *agentpb.Empty) (*agentpb.OSReleaseResponse, error) {
	info, err := h.collector.OSRelease(ctx)
	return pbOSRelease(info), remoteError(err)
}

func (h *Handler) SystemTraits(ctx context.Context, _ *agentpb.Empty) (*agentpb.SystemTraitsResponse, error) {
	traits, err := h.collector.SystemTraits(ctx)
	return &agentpb.SystemTraitsResponse{Traits: cloneMap(traits)}, remoteError(err)
}

func (h *Handler) MetricsSnapshot(ctx context.Context, req *agentpb.MetricsSnapshotRequest) (*agentpb.MetricsSnapshotResponse, error) {
	snap, err := h.collector.MetricsSnapshot(ctx, req.ServerId)
	return pbSnapshot(snap), remoteError(err)
}

func (h *Handler) PackageUpdates(ctx context.Context, _ *agentpb.Empty) (*agentpb.PackageUpdatesResponse, error) {
	items, err := h.collector.PackageUpdates(ctx)
	return &agentpb.PackageUpdatesResponse{Items: pbPackageUpdates(items)}, remoteError(err)
}

func (h *Handler) UpgradePackages(ctx context.Context, req *agentpb.PackageUpgradeRequest) (*agentpb.CommandResponse, error) {
	h.upgrades.begin()
	defer h.upgrades.end()
	output, err := h.collector.UpgradePackages(ctx, agentcontract.PackageUpgradeRequest{Names: append([]string(nil), req.Names...), All: req.All})
	return &agentpb.CommandResponse{Output: output}, remoteError(err)
}

func (h *Handler) UFWStatus(ctx context.Context, _ *agentpb.Empty) (*agentpb.UFWStatusResponse, error) {
	status, err := h.collector.UFWStatus(ctx)
	return pbUFWStatus(status), remoteError(err)
}

func (h *Handler) UFWInstall(ctx context.Context, req *agentpb.UFWInstallRequest) (*agentpb.UFWStatusResponse, error) {
	rules := make([]remoteops.UFWRule, 0, len(req.Rules))
	for _, rule := range req.Rules {
		rules = append(rules, goUFWRule(rule))
	}
	status, err := h.collector.InstallUFW(ctx, agentcontract.UFWInstallRequest{Rules: rules})
	return pbUFWStatus(status), remoteError(err)
}

func (h *Handler) UFWEnable(ctx context.Context, req *agentpb.UFWEnableRequest) (*agentpb.UFWStatusResponse, error) {
	status, err := h.collector.EnableUFW(ctx, agentcontract.UFWEnableRequest{SSHPort: int(req.SshPort)})
	return pbUFWStatus(status), remoteError(err)
}

func (h *Handler) UFWAllow(ctx context.Context, req *agentpb.UFWAllowRequest) (*agentpb.UFWStatusResponse, error) {
	status, err := h.collector.AllowUFW(ctx, agentcontract.UFWAllowRequest{Rule: goUFWRule(req.Rule)})
	return pbUFWStatus(status), remoteError(err)
}

func (h *Handler) UFWDelete(ctx context.Context, req *agentpb.UFWDeleteRequest) (*agentpb.UFWStatusResponse, error) {
	status, err := h.collector.DeleteUFW(ctx, agentcontract.UFWDeleteRequest{Number: int(req.Number)})
	return pbUFWStatus(status), remoteError(err)
}

func (h *Handler) Fail2BanStatus(ctx context.Context, _ *agentpb.Empty) (*agentpb.Fail2BanStatusResponse, error) {
	status, err := h.collector.Fail2BanStatus(ctx)
	return pbFail2BanStatus(status), remoteError(err)
}

func (h *Handler) ApplyFail2Ban(ctx context.Context, req *agentpb.Fail2BanApplyRequest) (*agentpb.Fail2BanStatusResponse, error) {
	status, err := h.collector.ApplyFail2Ban(ctx, agentcontract.Fail2BanApplyRequest{Config: goFail2BanConfig(req.Config)})
	return pbFail2BanStatus(status), remoteError(err)
}

func (h *Handler) ReleaseFail2Ban(ctx context.Context, _ *agentpb.Empty) (*agentpb.Fail2BanStatusResponse, error) {
	status, err := h.collector.ReleaseFail2Ban(ctx)
	return pbFail2BanStatus(status), remoteError(err)
}

func (h *Handler) RestartSystem(ctx context.Context, _ *agentpb.Empty) (*agentpb.OKResponse, error) {
	err := h.collector.RestartSystem(ctx)
	return &agentpb.OKResponse{Ok: err == nil}, remoteError(err)
}

func (h *Handler) DockerContainers(ctx context.Context, _ *agentpb.Empty) (*agentpb.DockerContainersResponse, error) {
	if err := h.requireRuntime(); err != nil {
		return nil, err
	}
	items, err := h.runtime.Containers(ctx)
	out := make([]*agentpb.DockerContainer, 0, len(items))
	for _, item := range items {
		out = append(out, pbDockerContainer(item))
	}
	return &agentpb.DockerContainersResponse{Items: out}, remoteError(err)
}

func (h *Handler) DockerContainerLogs(ctx context.Context, req *agentpb.DockerContainerLogsRequest) (*agentpb.DockerContainerLogsResponse, error) {
	if err := h.requireRuntime(); err != nil {
		return nil, err
	}
	logs, err := h.runtime.ContainerLogs(ctx, req.Id, int(req.Tail))
	return &agentpb.DockerContainerLogsResponse{ContainerId: req.Id, Logs: logs}, remoteError(err)
}

func (h *Handler) DockerContainerAction(ctx context.Context, req *agentpb.DockerContainerActionRequest) (*agentpb.OKResponse, error) {
	if err := h.requireRuntime(); err != nil {
		return nil, err
	}
	var err error
	switch req.Action {
	case "start":
		err = h.runtime.ContainerStart(ctx, req.Id)
	case "stop":
		err = h.runtime.ContainerStop(ctx, req.Id)
	case "restart":
		err = h.runtime.ContainerRestart(ctx, req.Id)
	default:
		return nil, status.Error(codes.InvalidArgument, "unsupported container action")
	}
	return &agentpb.OKResponse{Ok: err == nil}, remoteError(err)
}

func (h *Handler) DockerContainerDelete(ctx context.Context, req *agentpb.DockerContainerDeleteRequest) (*agentpb.OKResponse, error) {
	if err := h.requireRuntime(); err != nil {
		return nil, err
	}
	err := h.runtime.ContainerDelete(ctx, req.Id)
	return &agentpb.OKResponse{Ok: err == nil}, remoteError(err)
}

func (h *Handler) DockerImages(ctx context.Context, _ *agentpb.Empty) (*agentpb.DockerImagesResponse, error) {
	if err := h.requireRuntime(); err != nil {
		return nil, err
	}
	items, err := h.runtime.Images(ctx)
	out := make([]*agentpb.DockerImage, 0, len(items))
	for _, item := range items {
		out = append(out, pbDockerImage(item))
	}
	return &agentpb.DockerImagesResponse{Items: out}, remoteError(err)
}

func (h *Handler) DockerImagePull(ctx context.Context, req *agentpb.DockerImagePullRequest) (*agentpb.OKResponse, error) {
	if err := h.requireRuntime(); err != nil {
		return nil, err
	}
	err := h.runtime.PullImage(ctx, req.Reference)
	return &agentpb.OKResponse{Ok: err == nil}, remoteError(err)
}

func (h *Handler) DockerImageDelete(ctx context.Context, req *agentpb.DockerImageDeleteRequest) (*agentpb.OKResponse, error) {
	if err := h.requireRuntime(); err != nil {
		return nil, err
	}
	err := h.runtime.DeleteImage(ctx, req.Id)
	return &agentpb.OKResponse{Ok: err == nil}, remoteError(err)
}

func (h *Handler) DockerNetworks(ctx context.Context, _ *agentpb.Empty) (*agentpb.DockerNetworksResponse, error) {
	if err := h.requireRuntime(); err != nil {
		return nil, err
	}
	items, err := h.runtime.Networks(ctx)
	out := make([]*agentpb.DockerNetwork, 0, len(items))
	for _, item := range items {
		out = append(out, pbDockerNetwork(item))
	}
	return &agentpb.DockerNetworksResponse{Items: out}, remoteError(err)
}

func (h *Handler) DockerVolumes(ctx context.Context, _ *agentpb.Empty) (*agentpb.DockerVolumesResponse, error) {
	if err := h.requireRuntime(); err != nil {
		return nil, err
	}
	items, err := h.runtime.Volumes(ctx)
	out := make([]*agentpb.DockerVolume, 0, len(items))
	for _, item := range items {
		out = append(out, pbDockerVolume(item))
	}
	return &agentpb.DockerVolumesResponse{Items: out}, remoteError(err)
}

func (h *Handler) DockerVolumeDelete(ctx context.Context, req *agentpb.DockerVolumeDeleteRequest) (*agentpb.OKResponse, error) {
	if err := h.requireRuntime(); err != nil {
		return nil, err
	}
	err := h.runtime.DeleteVolume(ctx, req.Name)
	return &agentpb.OKResponse{Ok: err == nil}, remoteError(err)
}

func (h *Handler) RuntimeWriteFiles(ctx context.Context, req *agentpb.RuntimeWriteFilesRequest) (*agentpb.OKResponse, error) {
	if err := h.requireRuntime(); err != nil {
		return nil, err
	}
	err := h.runtime.WriteManagedFiles(ctx, goSpec(req.Spec))
	return &agentpb.OKResponse{Ok: err == nil}, remoteError(err)
}

func (h *Handler) RuntimeReconcile(ctx context.Context, req *agentpb.RuntimeReconcileRequest) (*agentpb.RuntimeReconcileResponse, error) {
	if err := h.requireRuntime(); err != nil {
		return nil, err
	}
	result, err := h.runtime.Reconcile(ctx, agentcontract.RuntimeReconcileRequest{
		JobID: req.JobId, ExecutionID: req.ExecutionId, ApplicationID: req.ApplicationId, InstanceID: req.InstanceId,
		ServerID: req.ServerId, Action: req.Action, DesiredGeneration: int(req.DesiredGeneration), DesiredSpecHash: req.DesiredSpecHash,
		DesiredRevisionID: req.DesiredRevisionId, Spec: goSpec(req.Spec), RemoveData: req.RemoveData, PreviousContainerName: req.PreviousContainerName,
	})
	return PBRuntimeReconcileResponse(result), remoteError(err)
}

func (h *Handler) RuntimeReload(ctx context.Context, req *agentpb.RuntimeReloadRequest) (*agentpb.RuntimeReloadResponse, error) {
	if err := h.requireRuntime(); err != nil {
		return nil, err
	}
	result, err := h.runtime.Reload(ctx, agentcontract.RuntimeReloadRequest{
		Spec:            goSpec(req.Spec),
		ContainerName:   req.ContainerName,
		ValidateCommand: append([]string(nil), req.ValidateCommand...),
		ReloadCommand:   append([]string(nil), req.ReloadCommand...),
	})
	return &agentpb.RuntimeReloadResponse{
		Reloaded: result.Reloaded,
		Phase:    result.Phase,
		ExitCode: int32(result.ExitCode),
		Output:   result.Output,
		Error:    result.Error,
	}, remoteError(err)
}

func (h *Handler) RuntimeCreateContainer(ctx context.Context, req *agentpb.RuntimeCreateContainerRequest) (*agentpb.RuntimeCreateContainerResponse, error) {
	if err := h.requireRuntime(); err != nil {
		return nil, err
	}
	id, err := h.runtime.CreateContainer(ctx, goSpec(req.Spec))
	return &agentpb.RuntimeCreateContainerResponse{ContainerId: id}, remoteError(err)
}

func (h *Handler) RuntimeStop(ctx context.Context, req *agentpb.RuntimeStopRequest) (*agentpb.RuntimeInstanceResponse, error) {
	if err := h.requireRuntime(); err != nil {
		return nil, err
	}
	result, err := h.runtime.Stop(ctx, agentcontract.RuntimeStopRequest{ApplicationID: req.ApplicationId, InstanceID: req.InstanceId, ContainerName: req.ContainerName, Purge: req.Purge, RemoveApplicationData: req.RemoveApplicationData})
	return pbRuntimeInstance(result), remoteError(err)
}

func (h *Handler) RuntimeRestart(ctx context.Context, req *agentpb.RuntimeRestartRequest) (*agentpb.RuntimeInstanceResponse, error) {
	if err := h.requireRuntime(); err != nil {
		return nil, err
	}
	result, err := h.runtime.Restart(ctx, agentcontract.RuntimeRestartRequest{InstanceID: req.InstanceId, ContainerName: req.ContainerName})
	return pbRuntimeInstance(result), remoteError(err)
}

func (h *Handler) RuntimeStatus(ctx context.Context, req *agentpb.RuntimeStatusRequest) (*agentpb.RuntimeStatusResponse, error) {
	if err := h.requireRuntime(); err != nil {
		return nil, err
	}
	status, err := h.runtime.Status(ctx, req.InstanceId, req.ContainerName, "")
	return pbRuntimeStatus(agentcontract.RuntimeStatusResponse{InstanceStatus: status}), remoteError(err)
}

func (h *Handler) RuntimeLogs(ctx context.Context, req *agentpb.RuntimeLogsRequest) (*agentpb.RuntimeLogsResponse, error) {
	if err := h.requireRuntime(); err != nil {
		return nil, err
	}
	logs, err := h.runtime.Logs(ctx, req.InstanceId, req.ContainerName, int(req.Tail))
	return &agentpb.RuntimeLogsResponse{InstanceId: req.InstanceId, Logs: logs}, remoteError(err)
}

func (h *Handler) RuntimePersistentArchive(ctx context.Context, req *agentpb.RuntimePersistentArchiveRequest) (*agentpb.RuntimePersistentArchiveResponse, error) {
	if err := h.requireRuntime(); err != nil {
		return nil, err
	}
	content, err := h.runtime.PersistentArchive(ctx, req.ApplicationId)
	return &agentpb.RuntimePersistentArchiveResponse{ApplicationId: req.ApplicationId, Filename: req.ApplicationId + "-persistent.zip", Content: content}, remoteError(err)
}

func (h *Handler) RuntimePersistentRestore(ctx context.Context, req *agentpb.RuntimePersistentRestoreRequest) (*agentpb.RuntimePersistentRestoreResponse, error) {
	if err := h.requireRuntime(); err != nil {
		return nil, err
	}
	err := h.runtime.RestorePersistentArchive(ctx, req.ApplicationId, req.Content)
	return &agentpb.RuntimePersistentRestoreResponse{ApplicationId: req.ApplicationId, Restored: err == nil}, remoteError(err)
}

func (h *Handler) StorageConfigureExport(ctx context.Context, req *agentpb.StorageConfigureExportRequest) (*agentpb.OKResponse, error) {
	err := agentstorage.ConfigureExport(ctx, req.Root, req.AllowedHosts, req.Enabled)
	return &agentpb.OKResponse{Ok: err == nil}, remoteError(err)
}

func (h *Handler) StorageArchiveDirectory(ctx context.Context, req *agentpb.StorageArchiveDirectoryRequest) (*agentpb.StorageArchiveDirectoryResponse, error) {
	content, err := agentstorage.ArchiveDirectory(ctx, req.Path)
	if err != nil {
		return nil, remoteError(err)
	}
	filename := "storage"
	if base := filepath.Base(req.Path); base != "" && base != "." && base != "/" {
		filename = base
	}
	return &agentpb.StorageArchiveDirectoryResponse{Filename: filename + ".tgz", Content: content}, nil
}

func (h *Handler) StorageDeleteDirectory(ctx context.Context, req *agentpb.StorageDeleteDirectoryRequest) (*agentpb.OKResponse, error) {
	err := agentstorage.DeleteDirectory(ctx, req.Path)
	return &agentpb.OKResponse{Ok: err == nil}, remoteError(err)
}

func (h *Handler) StorageEnsureDirectory(ctx context.Context, req *agentpb.StorageEnsureDirectoryRequest) (*agentpb.OKResponse, error) {
	err := agentstorage.EnsureDirectory(ctx, req.Path)
	return &agentpb.OKResponse{Ok: err == nil}, remoteError(err)
}

func (h *Handler) StorageStatus(ctx context.Context, req *agentpb.StorageStatusRequest) (*agentpb.StorageStatusResponse, error) {
	status := agentstorage.Status(ctx, req.Root)
	return &agentpb.StorageStatusResponse{
		ServerInstalled: status.ServerInstalled,
		RootExists:      status.RootExists,
		ExportLive:      status.ExportLive,
		Detail:          status.Detail,
	}, nil
}

func (h *Handler) StorageMountStatus(ctx context.Context, req *agentpb.StorageMountStatusRequest) (*agentpb.StorageMountStatusResponse, error) {
	volumeName := nfsvol.Name(req.Source, req.Target)
	if err := h.requireRuntime(); err != nil {
		return &agentpb.StorageMountStatusResponse{Detail: "runtime is not configured"}, nil
	}
	volumes, err := h.runtime.Volumes(ctx)
	if err != nil {
		return &agentpb.StorageMountStatusResponse{Detail: err.Error()}, nil
	}
	for _, volume := range volumes {
		if volume.Name != volumeName {
			continue
		}
		status := agentstorage.MountStatusAt(ctx, volume.Mountpoint)
		return &agentpb.StorageMountStatusResponse{
			VolumeExists: true,
			Mountpoint:   volume.Mountpoint,
			Mounted:      status.Mounted,
			Writable:     status.Writable,
			Detail:       status.Detail,
		}, nil
	}
	return &agentpb.StorageMountStatusResponse{Detail: "nfs volume not found"}, nil
}
