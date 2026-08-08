package rpc

import (
	"encoding/base64"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	agentpb "panel/internal/agent/pb"
	appruntime "panel/internal/modules/applications/runtime"
	"panel/internal/platform/linux"
	"panel/internal/platform/linux/remoteops"

	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const runtimeManagedArchiveModeSentinel = "__panel_archive__"

func pbTime(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

func goTime(t *timestamppb.Timestamp) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.AsTime()
}

func pbHealth(in agentcontract.HealthResponse) *agentpb.HealthResponse {
	return &agentpb.HealthResponse{
		Status:       in.Status,
		Time:         in.Time,
		Version:      in.Version,
		Capabilities: append([]string(nil), in.Capabilities...),
		ContractHash: in.ContractHash,
		Docker:       pbDockerHealth(in.Docker),
	}
}

func contractHealth(in *agentpb.HealthResponse) agentcontract.HealthResponse {
	if in == nil {
		return agentcontract.HealthResponse{}
	}
	return agentcontract.HealthResponse{
		Status:       in.Status,
		Time:         in.Time,
		Version:      in.Version,
		Capabilities: append([]string(nil), in.Capabilities...),
		ContractHash: in.ContractHash,
		Docker:       contractDockerHealth(in.Docker),
	}
}

func pbDockerHealth(in agentcontract.DockerHealth) *agentpb.DockerHealth {
	return &agentpb.DockerHealth{Host: in.Host, Status: in.Status, Error: in.Error}
}

func contractDockerHealth(in *agentpb.DockerHealth) agentcontract.DockerHealth {
	if in == nil {
		return agentcontract.DockerHealth{}
	}
	return agentcontract.DockerHealth{Host: in.Host, Status: in.Status, Error: in.Error}
}

func pbOSRelease(in linux.OSRelease) *agentpb.OSReleaseResponse {
	return &agentpb.OSReleaseResponse{Id: in.ID, VersionId: in.VersionID, PrettyName: in.PrettyName, Supported: in.Supported}
}

func goOSRelease(in *agentpb.OSReleaseResponse) linux.OSRelease {
	if in == nil {
		return linux.OSRelease{}
	}
	return linux.OSRelease{ID: in.Id, VersionID: in.VersionId, PrettyName: in.PrettyName, Supported: in.Supported}
}

func pbSnapshot(in linux.MetricsSnapshot) *agentpb.MetricsSnapshotResponse {
	return &agentpb.MetricsSnapshotResponse{
		ServerId:           in.ServerID,
		Time:               pbTime(in.Time),
		CpuUsagePercent:    in.CPUUsagePercent,
		MemoryUsedBytes:    in.MemoryUsedBytes,
		MemoryTotalBytes:   in.MemoryTotalBytes,
		DiskUsedBytes:      in.DiskUsedBytes,
		DiskTotalBytes:     in.DiskTotalBytes,
		NetworkRxBytesRate: in.NetworkRxBytesRate,
		NetworkTxBytesRate: in.NetworkTxBytesRate,
		Status: &agentpb.SystemStatus{
			Hostname:      in.Status.Hostname,
			KernelVersion: in.Status.KernelVersion,
			OsVersion:     in.Status.OSVersion,
			ServerTime:    pbTime(in.Status.ServerTime),
			UptimeSeconds: in.Status.UptimeSeconds,
			LoadAverage:   in.Status.LoadAverage,
			Load1:         in.Status.Load1,
			Load5:         in.Status.Load5,
			Load15:        in.Status.Load15,
		},
	}
}

func goSnapshot(in *agentpb.MetricsSnapshotResponse) linux.MetricsSnapshot {
	if in == nil {
		return linux.MetricsSnapshot{}
	}
	out := linux.MetricsSnapshot{
		ServerID:           in.ServerId,
		Time:               goTime(in.Time),
		CPUUsagePercent:    in.CpuUsagePercent,
		MemoryUsedBytes:    in.MemoryUsedBytes,
		MemoryTotalBytes:   in.MemoryTotalBytes,
		DiskUsedBytes:      in.DiskUsedBytes,
		DiskTotalBytes:     in.DiskTotalBytes,
		NetworkRxBytesRate: in.NetworkRxBytesRate,
		NetworkTxBytesRate: in.NetworkTxBytesRate,
	}
	if in.Status != nil {
		out.Status = linux.SystemStatus{
			Hostname:      in.Status.Hostname,
			KernelVersion: in.Status.KernelVersion,
			OSVersion:     in.Status.OsVersion,
			ServerTime:    goTime(in.Status.ServerTime),
			UptimeSeconds: in.Status.UptimeSeconds,
			LoadAverage:   in.Status.LoadAverage,
			Load1:         in.Status.Load1,
			Load5:         in.Status.Load5,
			Load15:        in.Status.Load15,
		}
	}
	return out
}

func pbPackageUpdates(items []linux.PackageUpdate) []*agentpb.PackageUpdate {
	out := make([]*agentpb.PackageUpdate, 0, len(items))
	for _, item := range items {
		out = append(out, &agentpb.PackageUpdate{Name: item.Name, InstalledVersion: item.InstalledVersion, CandidateVersion: item.CandidateVersion, Source: item.Source})
	}
	return out
}

func goPackageUpdates(items []*agentpb.PackageUpdate) []linux.PackageUpdate {
	out := make([]linux.PackageUpdate, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, linux.PackageUpdate{Name: item.Name, InstalledVersion: item.InstalledVersion, CandidateVersion: item.CandidateVersion, Source: item.Source})
	}
	return out
}

func pbUFWRule(in remoteops.UFWRule) *agentpb.UFWRule {
	return &agentpb.UFWRule{Port: int32(in.Port), Protocol: in.Protocol, From: in.From}
}

func goUFWRule(in *agentpb.UFWRule) remoteops.UFWRule {
	if in == nil {
		return remoteops.UFWRule{}
	}
	return remoteops.UFWRule{Port: int(in.Port), Protocol: in.Protocol, From: in.From}
}

func pbUFWStatus(in remoteops.UFWStatus) *agentpb.UFWStatusResponse {
	rules := make([]*agentpb.UFWRuleStatus, 0, len(in.Rules))
	for _, rule := range in.Rules {
		rules = append(rules, &agentpb.UFWRuleStatus{Number: int32(rule.Number), To: rule.To, Action: rule.Action, From: rule.From})
	}
	return &agentpb.UFWStatusResponse{Installed: in.Installed, Active: in.Active, Status: in.Status, Default: in.Default, Rules: rules, Raw: in.Raw}
}

func goUFWStatus(in *agentpb.UFWStatusResponse) remoteops.UFWStatus {
	if in == nil {
		return remoteops.UFWStatus{}
	}
	rules := make([]remoteops.UFWRuleStatus, 0, len(in.Rules))
	for _, rule := range in.Rules {
		if rule == nil {
			continue
		}
		rules = append(rules, remoteops.UFWRuleStatus{Number: int(rule.Number), To: rule.To, Action: rule.Action, From: rule.From})
	}
	return remoteops.UFWStatus{Installed: in.Installed, Active: in.Active, Status: in.Status, Default: in.Default, Rules: rules, Raw: in.Raw}
}

func pbFail2BanConfig(in agentcontract.Fail2BanConfig) *agentpb.Fail2BanConfig {
	jails := make([]*agentpb.Fail2BanJail, 0, len(in.Jails))
	for _, jail := range in.Jails {
		jails = append(jails, &agentpb.Fail2BanJail{
			Name: jail.Name, Enabled: jail.Enabled, Preset: jail.Preset, Filter: jail.Filter,
			LogPath: jail.LogPath, Backend: jail.Backend, Port: jail.Port, Protocol: jail.Protocol,
			Action: jail.Action, MaxRetry: int32(jail.MaxRetry), FindTime: jail.FindTime, BanTime: jail.BanTime,
			IgnoreIp: append([]string(nil), jail.IgnoreIP...), Options: cloneMap(jail.Options),
		})
	}
	return &agentpb.Fail2BanConfig{Jails: jails}
}

func goFail2BanConfig(in *agentpb.Fail2BanConfig) agentcontract.Fail2BanConfig {
	if in == nil {
		return agentcontract.Fail2BanConfig{}
	}
	jails := make([]agentcontract.Fail2BanJail, 0, len(in.Jails))
	for _, jail := range in.Jails {
		if jail == nil {
			continue
		}
		jails = append(jails, agentcontract.Fail2BanJail{
			Name: jail.Name, Enabled: jail.Enabled, Preset: jail.Preset, Filter: jail.Filter,
			LogPath: jail.LogPath, Backend: jail.Backend, Port: jail.Port, Protocol: jail.Protocol,
			Action: jail.Action, MaxRetry: int(jail.MaxRetry), FindTime: jail.FindTime, BanTime: jail.BanTime,
			IgnoreIP: append([]string(nil), jail.IgnoreIp...), Options: cloneMap(jail.Options),
		})
	}
	return agentcontract.Fail2BanConfig{Jails: jails}
}

func pbFail2BanStatus(in agentcontract.Fail2BanStatusResponse) *agentpb.Fail2BanStatusResponse {
	return &agentpb.Fail2BanStatusResponse{Installed: in.Installed, Active: in.Active, PanelConfigPresent: in.PanelConfigPresent, Jails: append([]string(nil), in.Jails...), Raw: in.Raw}
}

func goFail2BanStatus(in *agentpb.Fail2BanStatusResponse) agentcontract.Fail2BanStatusResponse {
	if in == nil {
		return agentcontract.Fail2BanStatusResponse{}
	}
	return agentcontract.Fail2BanStatusResponse{Installed: in.Installed, Active: in.Active, PanelConfigPresent: in.PanelConfigPresent, Jails: append([]string(nil), in.Jails...), Raw: in.Raw}
}

func pbSpec(in appruntime.Spec) *agentpb.RuntimeSpec {
	ports := make([]*agentpb.RuntimePort, 0, len(in.Ports))
	for _, item := range in.Ports {
		ports = append(ports, &agentpb.RuntimePort{Label: item.Label, ContainerPort: int32(item.ContainerPort), HostPort: int32(item.HostPort), Protocol: item.Protocol})
	}
	mounts := make([]*agentpb.RuntimeMount, 0, len(in.Mounts))
	for _, item := range in.Mounts {
		mount := &agentpb.RuntimeMount{Type: item.Type, Source: item.Source, Target: item.Target, ReadOnly: item.ReadOnly, Mode: item.Mode}
		if item.UID != nil {
			mount.Uid = wrapperspb.Int32(int32(*item.UID))
		}
		if item.GID != nil {
			mount.Gid = wrapperspb.Int32(int32(*item.GID))
		}
		mounts = append(mounts, mount)
	}
	files := make([]*agentpb.RuntimeManagedFile, 0, len(in.Files))
	for _, item := range in.Files {
		mode := item.Mode
		if strings.TrimSpace(item.Kind) == appruntime.ManagedFileKindArchive {
			mode = runtimeManagedArchiveModeSentinel
		}
		file := &agentpb.RuntimeManagedFile{Path: item.Path, Content: append([]byte(nil), item.Content...), Mode: mode}
		if item.UID != nil {
			file.Uid = wrapperspb.Int32(int32(*item.UID))
		}
		if item.GID != nil {
			file.Gid = wrapperspb.Int32(int32(*item.GID))
		}
		files = append(files, file)
	}
	services := make([]*agentpb.RuntimeService, 0, len(in.Services))
	for _, item := range in.Services {
		services = append(services, &agentpb.RuntimeService{Name: item.Name, Port: item.Port, Tags: append([]string(nil), item.Tags...)})
	}
	checks := make([]*agentpb.RuntimeCheck, 0, len(in.Checks))
	for _, item := range in.Checks {
		checks = append(checks, &agentpb.RuntimeCheck{Name: item.Name, Type: item.Type, Port: item.Port, Path: item.Path, IntervalSeconds: int32(item.IntervalSeconds), TimeoutSeconds: int32(item.TimeoutSeconds), Command: item.Command})
	}
	return &agentpb.RuntimeSpec{
		Id: in.ID, ApplicationId: in.ApplicationID, InstanceId: in.InstanceID, ContainerName: in.ContainerName,
		Name: in.Name, Image: in.Image, Command: append([]string(nil), in.Command...),
		Env: cloneMap(in.Env), Ports: ports, NetworkMode: in.NetworkMode,
		Resources:  &agentpb.RuntimeResources{Cpu: int32(in.Resources.CPU), MemoryMb: int32(in.Resources.MemoryMB)},
		Privileged: in.Privileged, CapAdd: append([]string(nil), in.CapAdd...), Mounts: mounts, Files: files,
		Restart:  &agentpb.RuntimeRestart{Policy: in.Restart.Policy, Attempts: int32(in.Restart.Attempts), IntervalSeconds: int32(in.Restart.IntervalSeconds), DelaySeconds: int32(in.Restart.DelaySeconds), Mode: in.Restart.Mode},
		Services: services, Checks: checks, Generation: int32(in.Generation), SpecHash: in.SpecHash,
	}
}

func goSpec(in *agentpb.RuntimeSpec) appruntime.Spec {
	if in == nil {
		return appruntime.Spec{}
	}
	ports := make([]appruntime.Port, 0, len(in.Ports))
	for _, item := range in.Ports {
		if item == nil {
			continue
		}
		ports = append(ports, appruntime.Port{Label: item.Label, ContainerPort: int(item.ContainerPort), HostPort: int(item.HostPort), Protocol: item.Protocol})
	}
	mounts := make([]appruntime.Mount, 0, len(in.Mounts))
	for _, item := range in.Mounts {
		if item == nil {
			continue
		}
		mount := appruntime.Mount{Type: item.Type, Source: item.Source, Target: item.Target, ReadOnly: item.ReadOnly, Mode: item.Mode}
		if item.Uid != nil {
			value := int(item.Uid.Value)
			mount.UID = &value
		}
		if item.Gid != nil {
			value := int(item.Gid.Value)
			mount.GID = &value
		}
		mounts = append(mounts, mount)
	}
	files := make([]appruntime.ManagedFile, 0, len(in.Files))
	for _, item := range in.Files {
		if item == nil {
			continue
		}
		file := appruntime.ManagedFile{Path: item.Path, Content: append([]byte(nil), item.Content...), Mode: item.Mode}
		if strings.TrimSpace(item.Mode) == runtimeManagedArchiveModeSentinel {
			file.Kind = appruntime.ManagedFileKindArchive
			file.Mode = ""
		}
		if item.Uid != nil {
			value := int(item.Uid.Value)
			file.UID = &value
		}
		if item.Gid != nil {
			value := int(item.Gid.Value)
			file.GID = &value
		}
		files = append(files, file)
	}
	services := make([]appruntime.Service, 0, len(in.Services))
	for _, item := range in.Services {
		if item == nil {
			continue
		}
		services = append(services, appruntime.Service{Name: item.Name, Port: item.Port, Tags: append([]string(nil), item.Tags...)})
	}
	checks := make([]appruntime.Check, 0, len(in.Checks))
	for _, item := range in.Checks {
		if item == nil {
			continue
		}
		checks = append(checks, appruntime.Check{Name: item.Name, Type: item.Type, Port: item.Port, Path: item.Path, IntervalSeconds: int(item.IntervalSeconds), TimeoutSeconds: int(item.TimeoutSeconds), Command: item.Command})
	}
	out := appruntime.Spec{
		ID: in.Id, ApplicationID: in.ApplicationId, InstanceID: in.InstanceId, ContainerName: in.ContainerName,
		Name: in.Name, Image: in.Image, Command: append([]string(nil), in.Command...),
		Env: cloneMap(in.Env), Ports: ports, NetworkMode: in.NetworkMode, Privileged: in.Privileged, CapAdd: append([]string(nil), in.CapAdd...),
		Mounts: mounts, Files: files, Services: services, Checks: checks, Generation: int(in.Generation), SpecHash: in.SpecHash,
	}
	if in.Resources != nil {
		out.Resources = appruntime.Resources{CPU: int(in.Resources.Cpu), MemoryMB: int(in.Resources.MemoryMb)}
	}
	if in.Restart != nil {
		out.Restart = appruntime.Restart{Policy: in.Restart.Policy, Attempts: int(in.Restart.Attempts), IntervalSeconds: int(in.Restart.IntervalSeconds), DelaySeconds: int(in.Restart.DelaySeconds), Mode: in.Restart.Mode}
	}
	return out
}

func pbRuntimeInstance(in agentcontract.RuntimeInstanceResponse) *agentpb.RuntimeInstanceResponse {
	return &agentpb.RuntimeInstanceResponse{InstanceId: in.InstanceID, ContainerName: in.ContainerName, ContainerId: in.ContainerID, Status: in.Status, Error: in.Error, ObservedAt: pbTime(in.ObservedAt)}
}

func goRuntimeInstance(in *agentpb.RuntimeInstanceResponse) agentcontract.RuntimeInstanceResponse {
	if in == nil {
		return agentcontract.RuntimeInstanceResponse{}
	}
	return agentcontract.RuntimeInstanceResponse{InstanceID: in.InstanceId, ContainerName: in.ContainerName, ContainerID: in.ContainerId, Status: in.Status, Error: in.Error, ObservedAt: goTime(in.ObservedAt)}
}

func pbRuntimeStatus(in agentcontract.RuntimeStatusResponse) *agentpb.RuntimeStatusResponse {
	status := in.InstanceStatus
	return &agentpb.RuntimeStatusResponse{
		InstanceId: status.InstanceID, ServerId: status.ServerID, ServerName: status.ServerName, ContainerName: status.ContainerName,
		ContainerId: status.ContainerID, Status: status.Status, DesiredState: status.DesiredState, Stage: status.Stage, Image: status.Image,
		StartedAt: status.StartedAt, FinishedAt: status.FinishedAt, ExitCode: int32(status.ExitCode), LastError: status.LastError, ObservedAt: pbTime(status.ObservedAt),
	}
}

func goRuntimeStatus(in *agentpb.RuntimeStatusResponse) agentcontract.RuntimeStatusResponse {
	if in == nil {
		return agentcontract.RuntimeStatusResponse{}
	}
	return agentcontract.RuntimeStatusResponse{InstanceStatus: appruntime.InstanceStatus{
		InstanceID: in.InstanceId, ServerID: in.ServerId, ServerName: in.ServerName, ContainerName: in.ContainerName, ContainerID: in.ContainerId,
		Status: in.Status, DesiredState: in.DesiredState, Stage: in.Stage, Image: in.Image, StartedAt: in.StartedAt, FinishedAt: in.FinishedAt,
		ExitCode: int(in.ExitCode), LastError: in.LastError, ObservedAt: goTime(in.ObservedAt),
	}}
}

func pbDockerContainer(in agentcontract.DockerContainer) *agentpb.DockerContainer {
	ports := make([]*agentpb.DockerPort, 0, len(in.Ports))
	for _, item := range in.Ports {
		ports = append(ports, &agentpb.DockerPort{Ip: item.IP, PrivatePort: int32(item.PrivatePort), PublicPort: int32(item.PublicPort), Type: item.Type})
	}
	mounts := make([]*agentpb.DockerMount, 0, len(in.Mounts))
	for _, item := range in.Mounts {
		mounts = append(mounts, &agentpb.DockerMount{Type: item.Type, Name: item.Name, Source: item.Source, Destination: item.Destination, Driver: item.Driver, Mode: item.Mode, Rw: item.RW})
	}
	return &agentpb.DockerContainer{Id: in.ID, Names: append([]string(nil), in.Names...), Image: in.Image, ImageId: in.ImageID, Command: in.Command, Created: in.Created, State: in.State, Status: in.Status, Ports: ports, Labels: cloneMap(in.Labels), Mounts: mounts}
}

func pbDockerContainerSlim(in agentcontract.DockerContainer) *agentpb.DockerContainer {
	ports := make([]*agentpb.DockerPort, 0, len(in.Ports))
	for _, item := range in.Ports {
		ports = append(ports, &agentpb.DockerPort{Ip: item.IP, PrivatePort: int32(item.PrivatePort), PublicPort: int32(item.PublicPort), Type: item.Type})
	}
	return &agentpb.DockerContainer{Id: in.ID, Names: append([]string(nil), in.Names...), Image: in.Image, ImageId: in.ImageID, State: in.State, Status: in.Status, Ports: ports, Labels: cloneMap(in.Labels)}
}

func goDockerContainer(in *agentpb.DockerContainer) agentcontract.DockerContainer {
	if in == nil {
		return agentcontract.DockerContainer{}
	}
	ports := make([]agentcontract.DockerPort, 0, len(in.Ports))
	for _, item := range in.Ports {
		if item == nil {
			continue
		}
		ports = append(ports, agentcontract.DockerPort{IP: item.Ip, PrivatePort: int(item.PrivatePort), PublicPort: int(item.PublicPort), Type: item.Type})
	}
	mounts := make([]agentcontract.DockerMount, 0, len(in.Mounts))
	for _, item := range in.Mounts {
		if item == nil {
			continue
		}
		mounts = append(mounts, agentcontract.DockerMount{Type: item.Type, Name: item.Name, Source: item.Source, Destination: item.Destination, Driver: item.Driver, Mode: item.Mode, RW: item.Rw})
	}
	return agentcontract.DockerContainer{ID: in.Id, Names: append([]string(nil), in.Names...), Image: in.Image, ImageID: in.ImageId, Command: in.Command, Created: in.Created, State: in.State, Status: in.Status, Ports: ports, Labels: cloneMap(in.Labels), Mounts: mounts}
}

func pbDockerImage(in agentcontract.DockerImage) *agentpb.DockerImage {
	return &agentpb.DockerImage{Id: in.ID, ParentId: in.ParentID, RepoTags: append([]string(nil), in.RepoTags...), RepoDigests: append([]string(nil), in.RepoDigests...), Created: in.Created, Size: in.Size, Containers: int32(in.Containers)}
}

func goDockerImage(in *agentpb.DockerImage) agentcontract.DockerImage {
	if in == nil {
		return agentcontract.DockerImage{}
	}
	return agentcontract.DockerImage{ID: in.Id, ParentID: in.ParentId, RepoTags: append([]string(nil), in.RepoTags...), RepoDigests: append([]string(nil), in.RepoDigests...), Created: in.Created, Size: in.Size, Containers: int(in.Containers)}
}

func pbDockerNetwork(in agentcontract.DockerNetwork) *agentpb.DockerNetwork {
	return &agentpb.DockerNetwork{Id: in.ID, Name: in.Name, Driver: in.Driver, Scope: in.Scope, Created: in.Created, Internal: in.Internal, Labels: cloneMap(in.Labels)}
}

func goDockerNetwork(in *agentpb.DockerNetwork) agentcontract.DockerNetwork {
	if in == nil {
		return agentcontract.DockerNetwork{}
	}
	return agentcontract.DockerNetwork{ID: in.Id, Name: in.Name, Driver: in.Driver, Scope: in.Scope, Created: in.Created, Internal: in.Internal, Labels: cloneMap(in.Labels)}
}

func pbDockerVolume(in agentcontract.DockerVolume) *agentpb.DockerVolume {
	out := &agentpb.DockerVolume{Name: in.Name, Driver: in.Driver, Mountpoint: in.Mountpoint, CreatedAt: in.CreatedAt, Labels: cloneMap(in.Labels), InUse: in.InUse, ContainerCount: int32(in.ContainerCount)}
	if in.UsageData != nil {
		out.UsageData = &agentpb.DockerVolumeUsage{Size: in.UsageData.Size, RefCount: in.UsageData.RefCount}
	}
	return out
}

func goDockerVolume(in *agentpb.DockerVolume) agentcontract.DockerVolume {
	if in == nil {
		return agentcontract.DockerVolume{}
	}
	out := agentcontract.DockerVolume{Name: in.Name, Driver: in.Driver, Mountpoint: in.Mountpoint, CreatedAt: in.CreatedAt, Labels: cloneMap(in.Labels), InUse: in.InUse, ContainerCount: int(in.ContainerCount)}
	if in.UsageData != nil {
		out.UsageData = &agentcontract.DockerVolumeUsage{Size: in.UsageData.Size, RefCount: in.UsageData.RefCount}
	}
	return out
}

func archiveContentBase64(content []byte) string {
	return base64.StdEncoding.EncodeToString(content)
}

func archiveContentFromBase64(value string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(strings.TrimSpace(value))
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func ContractHealth(in *agentpb.HealthResponse) agentcontract.HealthResponse {
	return contractHealth(in)
}
func GoOSRelease(in *agentpb.OSReleaseResponse) linux.OSRelease            { return goOSRelease(in) }
func GoSnapshot(in *agentpb.MetricsSnapshotResponse) linux.MetricsSnapshot { return goSnapshot(in) }
func PBSnapshot(in linux.MetricsSnapshot) *agentpb.MetricsSnapshotResponse { return pbSnapshot(in) }
func GoPackageUpdates(items []*agentpb.PackageUpdate) []linux.PackageUpdate {
	return goPackageUpdates(items)
}
func PBUFWRule(in remoteops.UFWRule) *agentpb.UFWRule               { return pbUFWRule(in) }
func GoUFWStatus(in *agentpb.UFWStatusResponse) remoteops.UFWStatus { return goUFWStatus(in) }
func PBFail2BanConfig(in agentcontract.Fail2BanConfig) *agentpb.Fail2BanConfig {
	return pbFail2BanConfig(in)
}
func GoFail2BanStatus(in *agentpb.Fail2BanStatusResponse) agentcontract.Fail2BanStatusResponse {
	return goFail2BanStatus(in)
}
func PBSpec(in appruntime.Spec) *agentpb.RuntimeSpec { return pbSpec(in) }
func GoRuntimeInstance(in *agentpb.RuntimeInstanceResponse) agentcontract.RuntimeInstanceResponse {
	return goRuntimeInstance(in)
}
func GoRuntimeStatus(in *agentpb.RuntimeStatusResponse) agentcontract.RuntimeStatusResponse {
	return goRuntimeStatus(in)
}
func GoDockerContainer(in *agentpb.DockerContainer) agentcontract.DockerContainer {
	return goDockerContainer(in)
}
func PBDockerContainer(in agentcontract.DockerContainer) *agentpb.DockerContainer {
	return pbDockerContainer(in)
}
func GoDockerImage(in *agentpb.DockerImage) agentcontract.DockerImage { return goDockerImage(in) }
func GoDockerNetwork(in *agentpb.DockerNetwork) agentcontract.DockerNetwork {
	return goDockerNetwork(in)
}
func GoDockerVolume(in *agentpb.DockerVolume) agentcontract.DockerVolume { return goDockerVolume(in) }
