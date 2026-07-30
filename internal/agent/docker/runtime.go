package docker

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/applications/runtime"
)

const defaultRuntimeRoot = "/opt/panel/apps"
const dockerImagePullTimeout = 15 * time.Minute
const managedBridgeNetwork = "panel-apps"
const managedFilesManifestPath = "managed-files.json"
const appliedStatePath = "applied-state.json"
const maxRuntimeCommandOutput = 1024 * 1024

const (
	managedArchiveMaxFiles     = 10000
	managedArchiveMaxDepth     = 32
	managedArchiveMaxExtracted = int64(256 << 20)
	managedArchiveMaxRatio     = int64(100)
)

const (
	labelManagedFilesHash  = "panel.application.managed_files.hash"
	labelManagedFilesDrift = "panel.application.managed_files.drift"
	labelManagedFilesError = "panel.application.managed_files.error"
	labelGeneration        = "panel.application.generation"
	labelSpecHash          = "panel.application.spec.hash"
	labelApplyMode         = "panel.application.apply.mode"
)

type LocalRuntime struct {
	dockerHost string
	root       string
	client     *dockerAPIClient
}

func NewLocalRuntime(dockerHost string) (*LocalRuntime, error) {
	dockerHost = strings.TrimSpace(dockerHost)
	if dockerHost == "" {
		dockerHost = agentcontract.DefaultDockerHost
	}
	client, err := newDockerAPIClient(dockerHost)
	if err != nil {
		return nil, err
	}
	return &LocalRuntime{dockerHost: dockerHost, root: defaultRuntimeRoot, client: client}, nil
}

func (r *LocalRuntime) DockerHealth(ctx context.Context) agentcontract.DockerHealth {
	if r == nil || r.client == nil {
		return agentcontract.DockerHealth{Host: agentcontract.DefaultDockerHost, Status: agentcontract.StatusUnavailable, Error: "runtime is not configured"}
	}
	if err := r.client.ping(ctx); err != nil {
		return agentcontract.DockerHealth{Host: r.dockerHost, Status: agentcontract.StatusUnavailable, Error: err.Error()}
	}
	return agentcontract.DockerHealth{Host: r.dockerHost, Status: "ok"}
}

func (r *LocalRuntime) Stop(ctx context.Context, req agentcontract.RuntimeStopRequest) (agentcontract.RuntimeInstanceResponse, error) {
	if r == nil || r.client == nil {
		return agentcontract.RuntimeInstanceResponse{}, errors.New("runtime is not configured")
	}
	name := firstNonEmpty(req.ContainerName, containerNameForInstance(req.InstanceID))
	if err := r.client.stopContainer(ctx, name, 10); err != nil && !isDockerNotFound(err) {
		return agentcontract.RuntimeInstanceResponse{}, err
	}
	if err := r.client.removeContainer(ctx, name, true); err != nil && !isDockerNotFound(err) {
		return agentcontract.RuntimeInstanceResponse{}, err
	}
	if req.Purge {
		if req.RemoveApplicationData {
			appDir, err := safeApplicationRootDir(r.root, req.ApplicationID)
			if err != nil {
				return agentcontract.RuntimeInstanceResponse{}, err
			}
			if err := os.RemoveAll(appDir); err != nil {
				return agentcontract.RuntimeInstanceResponse{}, err
			}
		} else {
			instanceDir, err := safeApplicationRuntimeDir(r.root, req.ApplicationID, filepath.Join("instances", req.InstanceID))
			if err != nil {
				return agentcontract.RuntimeInstanceResponse{}, err
			}
			if err := os.RemoveAll(instanceDir); err != nil {
				return agentcontract.RuntimeInstanceResponse{}, err
			}
		}
	}
	status := appruntime.StatusStopped
	if req.Purge {
		status = "purged"
	}
	return agentcontract.RuntimeInstanceResponse{InstanceID: req.InstanceID, ContainerName: name, Status: status, ObservedAt: time.Now().UTC()}, nil
}

func (r *LocalRuntime) Restart(ctx context.Context, req agentcontract.RuntimeRestartRequest) (agentcontract.RuntimeInstanceResponse, error) {
	if r == nil || r.client == nil {
		return agentcontract.RuntimeInstanceResponse{}, errors.New("runtime is not configured")
	}
	name := firstNonEmpty(req.ContainerName, containerNameForInstance(req.InstanceID))
	if err := r.client.restartContainer(ctx, name, 10); err != nil {
		return agentcontract.RuntimeInstanceResponse{}, err
	}
	status, err := r.Status(ctx, req.InstanceID, name, "")
	if err != nil {
		return agentcontract.RuntimeInstanceResponse{}, err
	}
	return agentcontract.RuntimeInstanceResponse{InstanceID: req.InstanceID, ContainerName: name, ContainerID: status.ContainerID, Status: status.Status, ObservedAt: status.ObservedAt}, nil
}

func (r *LocalRuntime) Status(ctx context.Context, instanceID, containerName, serverID string) (appruntime.InstanceStatus, error) {
	if r == nil || r.client == nil {
		return appruntime.InstanceStatus{}, errors.New("runtime is not configured")
	}
	if containerName == "" {
		containerName = containerNameForInstance(instanceID)
	}
	inspect, err := r.client.inspectContainer(ctx, containerName)
	now := time.Now().UTC()
	if err != nil {
		if isDockerNotFound(err) {
			return appruntime.InstanceStatus{
				InstanceID:    instanceID,
				ServerID:      serverID,
				ContainerName: containerName,
				Status:        appruntime.StatusMissing,
				DesiredState:  appruntime.DesiredRunning,
				ObservedAt:    now,
			}, nil
		}
		return appruntime.InstanceStatus{}, err
	}
	status := dockerStateToRuntime(inspect.State.Status, inspect.State.Running, inspect.State.ExitCode)
	return appruntime.InstanceStatus{
		InstanceID:    instanceID,
		ServerID:      serverID,
		ContainerName: strings.TrimPrefix(inspect.Name, "/"),
		ContainerID:   inspect.ID,
		Status:        status,
		DesiredState:  appruntime.DesiredRunning,
		Image:         inspect.Config.Image,
		StartedAt:     inspect.State.StartedAt,
		FinishedAt:    inspect.State.FinishedAt,
		ExitCode:      inspect.State.ExitCode,
		LastError:     inspect.State.Error,
		ObservedAt:    now,
	}, nil
}

func (r *LocalRuntime) Logs(ctx context.Context, instanceID, containerName string, tail int) (string, error) {
	if r == nil || r.client == nil {
		return "", errors.New("runtime is not configured")
	}
	return r.client.containerLogs(ctx, firstNonEmpty(containerName, containerNameForInstance(instanceID)), normalizeLogTail(tail))
}

func (r *LocalRuntime) Containers(ctx context.Context) ([]agentcontract.DockerContainer, error) {
	if r == nil || r.client == nil {
		return nil, errors.New("runtime is not configured")
	}
	items, err := r.client.listContainers(ctx)
	if err != nil {
		return nil, err
	}
	for i := range items {
		appID := strings.TrimSpace(items[i].Labels["panel.application.id"])
		instanceID := strings.TrimSpace(items[i].Labels["panel.application.instance.id"])
		if items[i].Labels["panel.application.managed"] != "true" || appID == "" || instanceID == "" {
			continue
		}
		if items[i].Labels == nil {
			items[i].Labels = map[string]string{}
		}
		hash, drifted, err := r.managedFilesDrift(appID, instanceID)
		if hash != "" {
			items[i].Labels[labelManagedFilesHash] = hash
		}
		if drifted {
			items[i].Labels[labelManagedFilesDrift] = "true"
		}
		if err != nil {
			items[i].Labels[labelManagedFilesError] = err.Error()
		}
		if state, err := r.readAppliedState(appID, instanceID); err == nil && state.matches(items[i]) {
			items[i].Labels[labelGeneration] = strconv.Itoa(state.Generation)
			items[i].Labels[labelSpecHash] = state.SpecHash
			items[i].Labels[labelApplyMode] = state.ApplyMode
		}
	}
	return items, nil
}

func (r *LocalRuntime) ContainerEvents(ctx context.Context) (<-chan struct{}, <-chan error) {
	events := make(chan struct{}, 16)
	errs := make(chan error, 1)
	go func() {
		defer close(events)
		defer close(errs)
		if r == nil || r.client == nil {
			errs <- errors.New("runtime is not configured")
			return
		}
		if err := r.client.watchContainerEvents(ctx, events); err != nil && ctx.Err() == nil {
			errs <- err
		}
	}()
	return events, errs
}

func (r *LocalRuntime) ContainerLogs(ctx context.Context, id string, tail int) (string, error) {
	if r == nil || r.client == nil {
		return "", errors.New("runtime is not configured")
	}
	return r.client.containerLogs(ctx, id, normalizeLogTail(tail))
}

func (r *LocalRuntime) PersistentArchive(ctx context.Context, applicationID string) ([]byte, error) {
	if r == nil {
		return nil, errors.New("runtime is not configured")
	}
	dir, err := safeApplicationRuntimeDir(r.root, applicationID, "persistent")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	err = filepath.WalkDir(dir, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filePath == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, filePath)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if entry.IsDir() {
			_, err := zw.Create(name + "/")
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = name
		header.Method = zip.Deflate
		writer, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		file, err := os.Open(filePath)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(writer, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if closeErr := zw.Close(); err == nil {
		err = closeErr
	}
	_ = ctx
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (r *LocalRuntime) RestorePersistentArchive(ctx context.Context, applicationID string, content []byte) error {
	if r == nil {
		return errors.New("runtime is not configured")
	}
	dir, err := safeApplicationRuntimeDir(r.root, applicationID, "persistent")
	if err != nil {
		return err
	}
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return err
	}
	for _, file := range reader.File {
		if _, err := safeArchiveTarget(dir, file.Name); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, file := range reader.File {
		target, err := safeArchiveTarget(dir, file.Name)
		if err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		mode := file.FileInfo().Mode()
		if mode == 0 {
			mode = 0o600
		}
		dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
		if err != nil {
			_ = src.Close()
			return err
		}
		_, copyErr := io.Copy(dst, src)
		closeSrcErr := src.Close()
		closeDstErr := dst.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeSrcErr != nil {
			return closeSrcErr
		}
		if closeDstErr != nil {
			return closeDstErr
		}
	}
	_ = ctx
	return nil
}

func (r *LocalRuntime) ContainerStart(ctx context.Context, id string) error {
	return r.client.startContainer(ctx, id)
}

func (r *LocalRuntime) ContainerStop(ctx context.Context, id string) error {
	err := r.client.stopContainer(ctx, id, 10)
	if isDockerNotModified(err) || isDockerNotFound(err) {
		return nil
	}
	return err
}

func (r *LocalRuntime) ContainerRestart(ctx context.Context, id string) error {
	return r.client.restartContainer(ctx, id, 10)
}

func (r *LocalRuntime) ContainerDelete(ctx context.Context, id string) error {
	err := r.client.removeContainer(ctx, id, true)
	if isDockerNotFound(err) {
		return nil
	}
	return err
}

func (r *LocalRuntime) WriteManagedFiles(ctx context.Context, spec appruntime.Spec) error {
	if r == nil || r.client == nil {
		return errors.New("runtime is not configured")
	}
	_ = ctx
	return r.writeManagedFiles(spec)
}

func (r *LocalRuntime) Reload(ctx context.Context, req agentcontract.RuntimeReloadRequest) (agentcontract.RuntimeReloadResponse, error) {
	if r == nil || r.client == nil {
		return agentcontract.RuntimeReloadResponse{}, errors.New("runtime is not configured")
	}
	if len(req.ValidateCommand) == 0 || len(req.ReloadCommand) == 0 {
		return agentcontract.RuntimeReloadResponse{Phase: "unsupported", Error: "reload commands are required"}, nil
	}
	for _, file := range req.Spec.Files {
		if strings.TrimSpace(file.Kind) == appruntime.ManagedFileKindArchive {
			return agentcontract.RuntimeReloadResponse{Phase: "unsupported", Error: "managed archives require container recreation"}, nil
		}
	}
	containerName := firstNonEmpty(req.ContainerName, req.Spec.ContainerName, containerNameForInstance(req.Spec.InstanceID))
	inspect, err := r.client.inspectContainer(ctx, containerName)
	if err != nil {
		return agentcontract.RuntimeReloadResponse{}, err
	}
	if !inspect.State.Running {
		return agentcontract.RuntimeReloadResponse{Phase: "unsupported", Error: "container is not running"}, nil
	}
	snapshot, err := r.snapshotManagedFiles(req.Spec.ApplicationID, req.Spec.InstanceID)
	if err != nil {
		return agentcontract.RuntimeReloadResponse{}, err
	}
	if err := r.writeManagedFiles(req.Spec); err != nil {
		_ = r.restoreManagedFiles(req.Spec.ApplicationID, req.Spec.InstanceID, snapshot)
		return agentcontract.RuntimeReloadResponse{}, err
	}
	validation, err := r.client.execContainer(ctx, containerName, req.ValidateCommand)
	if err != nil {
		_ = r.restoreManagedFiles(req.Spec.ApplicationID, req.Spec.InstanceID, snapshot)
		return agentcontract.RuntimeReloadResponse{}, err
	}
	if validation.ExitCode != 0 {
		if restoreErr := r.restoreManagedFiles(req.Spec.ApplicationID, req.Spec.InstanceID, snapshot); restoreErr != nil {
			return agentcontract.RuntimeReloadResponse{}, fmt.Errorf("restore managed files after validation failure: %w", restoreErr)
		}
		return agentcontract.RuntimeReloadResponse{Phase: "validate", ExitCode: validation.ExitCode, Output: validation.Output, Error: "reload validation failed"}, nil
	}
	reloaded, err := r.client.execContainer(ctx, containerName, req.ReloadCommand)
	if err != nil {
		return agentcontract.RuntimeReloadResponse{}, err
	}
	if reloaded.ExitCode != 0 {
		return agentcontract.RuntimeReloadResponse{Phase: "reload", ExitCode: reloaded.ExitCode, Output: reloaded.Output, Error: "reload command failed"}, nil
	}
	current, err := r.client.inspectContainer(ctx, containerName)
	if err != nil {
		return agentcontract.RuntimeReloadResponse{}, err
	}
	if !current.State.Running || current.ID != inspect.ID {
		return agentcontract.RuntimeReloadResponse{Phase: "verify", Error: "container changed or stopped during reload"}, nil
	}
	if err := r.writeAppliedState(req.Spec, current.ID, "reload"); err != nil {
		return agentcontract.RuntimeReloadResponse{}, err
	}
	return agentcontract.RuntimeReloadResponse{Reloaded: true, Phase: "complete", ExitCode: 0, Output: reloaded.Output}, nil
}

func (r *LocalRuntime) CreateContainer(ctx context.Context, spec appruntime.Spec) (string, error) {
	if r == nil || r.client == nil {
		return "", errors.New("runtime is not configured")
	}
	if strings.TrimSpace(spec.InstanceID) == "" {
		return "", errors.New("instance id is required")
	}
	if strings.TrimSpace(spec.ContainerName) == "" {
		return "", errors.New("container name is required")
	}
	if strings.TrimSpace(spec.Image) == "" {
		return "", errors.New("image is required")
	}
	if err := r.client.ensureManagedNetwork(ctx, dockerNetworkMode(spec.NetworkMode)); err != nil {
		return "", err
	}
	id, err := r.client.createContainer(ctx, spec)
	if err != nil {
		return "", err
	}
	if err := r.writeAppliedState(spec, id, "recreate"); err != nil {
		_ = r.client.removeContainer(ctx, id, true)
		return "", err
	}
	return id, nil
}

func (r *LocalRuntime) Images(ctx context.Context) ([]agentcontract.DockerImage, error) {
	return r.client.listImages(ctx)
}

func (r *LocalRuntime) PullImage(ctx context.Context, reference string) error {
	return r.client.pullImage(ctx, reference)
}

func (r *LocalRuntime) DeleteImage(ctx context.Context, id string) error {
	return r.client.removeImage(ctx, id)
}

func (r *LocalRuntime) Networks(ctx context.Context) ([]agentcontract.DockerNetwork, error) {
	return r.client.listNetworks(ctx)
}

func (r *LocalRuntime) Volumes(ctx context.Context) ([]agentcontract.DockerVolume, error) {
	return r.client.listVolumes(ctx)
}

func (r *LocalRuntime) DeleteVolume(ctx context.Context, name string) error {
	return r.client.removeVolume(ctx, name)
}

func (r *LocalRuntime) writeManagedFiles(spec appruntime.Spec) error {
	previous, err := r.readManagedFilesManifest(spec.ApplicationID, spec.InstanceID)
	if err != nil {
		return err
	}
	manifest := managedFilesManifest{Entries: []managedFileManifestEntry{}}
	for _, file := range spec.Files {
		if strings.TrimSpace(file.Kind) == appruntime.ManagedFileKindArchive {
			entry, err := r.writeManagedArchive(spec, file)
			if err != nil {
				return err
			}
			manifest.Entries = append(manifest.Entries, entry)
			continue
		}
		target, err := safeRuntimePath(r.root, spec.ApplicationID, spec.InstanceID, "files", file.Path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		mode, err := managedFileMode(file.Mode)
		if err != nil {
			return err
		}
		if err := writeFileAtomic(target, file.Content, mode); err != nil {
			return err
		}
		if err := os.Chmod(target, mode); err != nil {
			return err
		}
		if err := applyOwnership(target, file.UID, file.GID); err != nil {
			return err
		}
		manifest.Entries = append(manifest.Entries, managedFileManifestEntry{
			Kind:   appruntime.ManagedFileKindFile,
			Path:   path.Clean(strings.TrimPrefix(file.Path, "/")),
			SHA256: sha256Hex(file.Content),
			Mode:   formatFileMode(mode),
			UID:    cloneInt(file.UID),
			GID:    cloneInt(file.GID),
		})
	}
	if err := r.removeStaleManagedFiles(spec.ApplicationID, spec.InstanceID, previous, manifest); err != nil {
		return err
	}
	if err := r.writeManagedFilesManifest(spec.ApplicationID, spec.InstanceID, manifest); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(r.root, spec.ApplicationID, "persistent"), 0o700); err != nil {
		return err
	}
	return r.preparePersistentMounts(spec)
}

func writeFileAtomic(target string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".panel-write-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if goruntime.GOOS == "windows" {
		_ = os.Remove(target)
	}
	return os.Rename(tmpName, target)
}

func (r *LocalRuntime) writeManagedArchive(spec appruntime.Spec, file appruntime.ManagedFile) (managedFileManifestEntry, error) {
	targetDir, err := safeRuntimePath(r.root, spec.ApplicationID, spec.InstanceID, "files", file.Path)
	if err != nil {
		return managedFileManifestEntry{}, err
	}
	archivePath, err := safeRuntimePath(r.root, spec.ApplicationID, spec.InstanceID, "archives", file.Path+".archive")
	if err != nil {
		return managedFileManifestEntry{}, err
	}
	entries, err := managedArchiveEntries(file.Content)
	if err != nil {
		return managedFileManifestEntry{}, err
	}
	expected := sha256Hex(file.Content)
	current, err := fileSHA256Hex(archivePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return managedFileManifestEntry{}, err
	}
	if current != expected {
		if err := os.MkdirAll(filepath.Dir(archivePath), 0o700); err != nil {
			return managedFileManifestEntry{}, err
		}
		if err := os.WriteFile(archivePath, file.Content, 0o600); err != nil {
			return managedFileManifestEntry{}, err
		}
		if err := os.Chmod(archivePath, 0o600); err != nil {
			return managedFileManifestEntry{}, err
		}
		current, err = fileSHA256Hex(archivePath)
		if err != nil {
			return managedFileManifestEntry{}, err
		}
	}
	if current != expected {
		return managedFileManifestEntry{}, fmt.Errorf("managed archive sha256 mismatch for %s", file.Path)
	}
	archiveContent, err := os.ReadFile(archivePath)
	if err != nil {
		return managedFileManifestEntry{}, err
	}
	if sha256Hex(archiveContent) != expected {
		return managedFileManifestEntry{}, fmt.Errorf("managed archive sha256 mismatch for %s", file.Path)
	}
	entries, err = managedArchiveEntries(archiveContent)
	if err != nil {
		return managedFileManifestEntry{}, err
	}
	if err := os.RemoveAll(targetDir); err != nil {
		return managedFileManifestEntry{}, err
	}
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		return managedFileManifestEntry{}, err
	}
	for _, entry := range entries {
		target, err := safeArchiveTarget(targetDir, entry.Name)
		if err != nil {
			return managedFileManifestEntry{}, err
		}
		if entry.Dir {
			if err := os.MkdirAll(target, entry.Mode); err != nil {
				return managedFileManifestEntry{}, err
			}
			if err := os.Chmod(target, entry.Mode); err != nil {
				return managedFileManifestEntry{}, err
			}
			if err := applyOwnership(target, file.UID, file.GID); err != nil {
				return managedFileManifestEntry{}, err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return managedFileManifestEntry{}, err
		}
		if err := os.WriteFile(target, entry.Content, entry.Mode); err != nil {
			return managedFileManifestEntry{}, err
		}
		if err := os.Chmod(target, entry.Mode); err != nil {
			return managedFileManifestEntry{}, err
		}
		if err := applyOwnership(target, file.UID, file.GID); err != nil {
			return managedFileManifestEntry{}, err
		}
	}
	return managedFileManifestEntry{
		Kind:     appruntime.ManagedFileKindArchive,
		Path:     path.Clean(strings.TrimPrefix(file.Path, "/")),
		SHA256:   expected,
		TreeHash: managedArchiveTreeHash(entries),
		UID:      cloneInt(file.UID),
		GID:      cloneInt(file.GID),
	}, nil
}

func (r *LocalRuntime) preparePersistentMounts(spec appruntime.Spec) error {
	for _, mount := range spec.Mounts {
		if mount.Type != "persistent" {
			continue
		}
		source, err := safePersistentMountDir(r.root, spec.ApplicationID, mount.Source)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o700)
		if strings.TrimSpace(mount.Mode) != "" {
			parsed, err := strconv.ParseUint(strings.TrimSpace(mount.Mode), 8, 32)
			if err != nil {
				return fmt.Errorf("persistent mount mode is invalid: %w", err)
			}
			mode = os.FileMode(parsed)
		}
		if err := os.MkdirAll(source, mode); err != nil {
			return err
		}
		if strings.TrimSpace(mount.Mode) != "" {
			if err := os.Chmod(source, mode); err != nil {
				return err
			}
		}
		if err := applyOwnership(source, mount.UID, mount.GID); err != nil {
			return err
		}
	}
	return nil
}

func managedFileMode(value string) (os.FileMode, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0o600, nil
	}
	parsed, err := strconv.ParseUint(value, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("managed file mode is invalid: %w", err)
	}
	return os.FileMode(parsed), nil
}

func applyOwnership(path string, uidValue, gidValue *int) error {
	if uidValue == nil && gidValue == nil {
		return nil
	}
	uid := -1
	gid := -1
	if uidValue != nil {
		uid = *uidValue
	}
	if gidValue != nil {
		gid = *gidValue
	}
	return os.Chown(path, uid, gid)
}

type managedFilesManifest struct {
	Entries []managedFileManifestEntry `json:"entries"`
}

type managedFileManifestEntry struct {
	Kind     string `json:"kind"`
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	Mode     string `json:"mode,omitempty"`
	TreeHash string `json:"treeHash,omitempty"`
	UID      *int   `json:"uid,omitempty"`
	GID      *int   `json:"gid,omitempty"`
}

type managedFilesSnapshot struct {
	Manifest managedFilesManifest
	Files    map[string][]byte
}

func (r *LocalRuntime) readManagedFilesManifest(appID, instanceID string) (managedFilesManifest, error) {
	target, err := safeRuntimePath(r.root, appID, instanceID, "manifest", managedFilesManifestPath)
	if err != nil {
		return managedFilesManifest{}, err
	}
	raw, err := os.ReadFile(target)
	if errors.Is(err, os.ErrNotExist) {
		return managedFilesManifest{Entries: []managedFileManifestEntry{}}, nil
	}
	if err != nil {
		return managedFilesManifest{}, err
	}
	var manifest managedFilesManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return managedFilesManifest{}, err
	}
	if manifest.Entries == nil {
		manifest.Entries = []managedFileManifestEntry{}
	}
	return manifest, nil
}

func (r *LocalRuntime) removeStaleManagedFiles(appID, instanceID string, previous, next managedFilesManifest) error {
	wanted := map[string]struct{}{}
	for _, entry := range next.Entries {
		wanted[entry.Kind+"\x00"+entry.Path] = struct{}{}
	}
	for _, entry := range previous.Entries {
		if _, ok := wanted[entry.Kind+"\x00"+entry.Path]; ok {
			continue
		}
		if strings.TrimSpace(entry.Kind) == appruntime.ManagedFileKindArchive {
			targetDir, err := safeRuntimePath(r.root, appID, instanceID, "files", entry.Path)
			if err != nil {
				return err
			}
			archivePath, err := safeRuntimePath(r.root, appID, instanceID, "archives", entry.Path+".archive")
			if err != nil {
				return err
			}
			if err := os.RemoveAll(targetDir); err != nil {
				return err
			}
			if err := os.Remove(archivePath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			continue
		}
		target, err := safeRuntimePath(r.root, appID, instanceID, "files", entry.Path)
		if err != nil {
			return err
		}
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		removeEmptyParents(filepath.Dir(target), filepath.Join(r.root, appID, "instances", instanceID, "files"))
	}
	return nil
}

func removeEmptyParents(dir, stop string) {
	stop = filepath.Clean(stop)
	for current := filepath.Clean(dir); current != stop && strings.HasPrefix(current, stop+string(os.PathSeparator)); current = filepath.Dir(current) {
		if err := os.Remove(current); err != nil {
			return
		}
	}
}

func (r *LocalRuntime) snapshotManagedFiles(appID, instanceID string) (managedFilesSnapshot, error) {
	manifest, err := r.readManagedFilesManifest(appID, instanceID)
	if err != nil {
		return managedFilesSnapshot{}, err
	}
	snapshot := managedFilesSnapshot{Manifest: manifest, Files: map[string][]byte{}}
	for _, entry := range manifest.Entries {
		if strings.TrimSpace(entry.Kind) == appruntime.ManagedFileKindArchive {
			return managedFilesSnapshot{}, errors.New("managed archives cannot be snapshotted for reload")
		}
		target, err := safeRuntimePath(r.root, appID, instanceID, "files", entry.Path)
		if err != nil {
			return managedFilesSnapshot{}, err
		}
		content, err := os.ReadFile(target)
		if err != nil {
			return managedFilesSnapshot{}, err
		}
		snapshot.Files[entry.Path] = content
	}
	return snapshot, nil
}

func (r *LocalRuntime) restoreManagedFiles(appID, instanceID string, snapshot managedFilesSnapshot) error {
	current, err := r.readManagedFilesManifest(appID, instanceID)
	if err != nil {
		return err
	}
	if err := r.removeStaleManagedFiles(appID, instanceID, current, snapshot.Manifest); err != nil {
		return err
	}
	for _, entry := range snapshot.Manifest.Entries {
		content, ok := snapshot.Files[entry.Path]
		if !ok {
			return fmt.Errorf("managed file snapshot is missing %s", entry.Path)
		}
		target, err := safeRuntimePath(r.root, appID, instanceID, "files", entry.Path)
		if err != nil {
			return err
		}
		mode, err := managedFileMode(entry.Mode)
		if err != nil {
			return err
		}
		if err := writeFileAtomic(target, content, mode); err != nil {
			return err
		}
		if err := applyOwnership(target, entry.UID, entry.GID); err != nil {
			return err
		}
	}
	return r.writeManagedFilesManifest(appID, instanceID, snapshot.Manifest)
}

func (r *LocalRuntime) writeManagedFilesManifest(appID, instanceID string, manifest managedFilesManifest) error {
	sort.Slice(manifest.Entries, func(i, j int) bool { return manifest.Entries[i].Path < manifest.Entries[j].Path })
	raw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	target, err := safeRuntimePath(r.root, appID, instanceID, "manifest", managedFilesManifestPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	if err := writeFileAtomic(target, raw, 0o600); err != nil {
		return err
	}
	return os.Chmod(target, 0o600)
}

type appliedState struct {
	ContainerID   string `json:"containerId"`
	ContainerName string `json:"containerName"`
	Generation    int    `json:"generation"`
	SpecHash      string `json:"specHash"`
	ApplyMode     string `json:"applyMode"`
	AppliedAt     string `json:"appliedAt"`
}

func (s appliedState) matches(container agentcontract.DockerContainer) bool {
	if strings.TrimSpace(s.ContainerID) != "" && s.ContainerID != container.ID {
		return false
	}
	if strings.TrimSpace(s.ContainerName) == "" {
		return true
	}
	for _, name := range container.Names {
		if strings.TrimPrefix(name, "/") == s.ContainerName {
			return true
		}
	}
	return false
}

func (r *LocalRuntime) writeAppliedState(spec appruntime.Spec, containerID, applyMode string) error {
	state := appliedState{
		ContainerID: containerID, ContainerName: spec.ContainerName, Generation: spec.Generation,
		SpecHash: spec.SpecHash, ApplyMode: applyMode, AppliedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	target, err := safeRuntimePath(r.root, spec.ApplicationID, spec.InstanceID, "state", appliedStatePath)
	if err != nil {
		return err
	}
	return writeFileAtomic(target, raw, 0o600)
}

func (r *LocalRuntime) readAppliedState(appID, instanceID string) (appliedState, error) {
	target, err := safeRuntimePath(r.root, appID, instanceID, "state", appliedStatePath)
	if err != nil {
		return appliedState{}, err
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		return appliedState{}, err
	}
	var state appliedState
	if err := json.Unmarshal(raw, &state); err != nil {
		return appliedState{}, err
	}
	return state, nil
}

func (r *LocalRuntime) managedFilesDrift(appID, instanceID string) (string, bool, error) {
	target, err := safeRuntimePath(r.root, appID, instanceID, "manifest", managedFilesManifestPath)
	if err != nil {
		return "", false, err
	}
	raw, err := os.ReadFile(target)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", true, err
	}
	var manifest managedFilesManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return sha256Hex(raw), true, err
	}
	for _, entry := range manifest.Entries {
		drifted, err := r.managedFileEntryDrift(appID, instanceID, entry)
		if err != nil || drifted {
			return sha256Hex(raw), true, err
		}
	}
	return sha256Hex(raw), false, nil
}

func (r *LocalRuntime) managedFileEntryDrift(appID, instanceID string, entry managedFileManifestEntry) (bool, error) {
	switch strings.TrimSpace(entry.Kind) {
	case appruntime.ManagedFileKindArchive:
		archivePath, err := safeRuntimePath(r.root, appID, instanceID, "archives", entry.Path+".archive")
		if err != nil {
			return true, err
		}
		got, err := fileSHA256Hex(archivePath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return true, nil
			}
			return true, err
		}
		if got != entry.SHA256 {
			return true, nil
		}
		targetDir, err := safeRuntimePath(r.root, appID, instanceID, "files", entry.Path)
		if err != nil {
			return true, err
		}
		treeHash, err := directoryTreeHash(targetDir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return true, nil
			}
			return true, err
		}
		return treeHash != entry.TreeHash, nil
	default:
		target, err := safeRuntimePath(r.root, appID, instanceID, "files", entry.Path)
		if err != nil {
			return true, err
		}
		got, err := fileSHA256Hex(target)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return true, nil
			}
			return true, err
		}
		if got != entry.SHA256 {
			return true, nil
		}
		info, err := os.Stat(target)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return true, nil
			}
			return true, err
		}
		if entry.Mode != "" && formatFileMode(info.Mode()) != entry.Mode {
			return true, nil
		}
		ownerMatches, err := fileOwnerMatches(info, entry.UID, entry.GID)
		if err != nil {
			return true, err
		}
		return !ownerMatches, nil
	}
}

type managedArchiveEntry struct {
	Name    string
	Dir     bool
	Mode    os.FileMode
	Content []byte
}

func managedArchiveEntries(content []byte) ([]managedArchiveEntry, error) {
	if len(content) == 0 {
		return nil, errors.New("managed archive content is required")
	}
	if looksLikeZip(content) {
		reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
		if err != nil {
			return nil, fmt.Errorf("managed archive zip is invalid: %w", err)
		}
		return managedZipEntries(reader, int64(len(content)))
	}
	if gzipReader, err := gzip.NewReader(bytes.NewReader(content)); err == nil {
		defer gzipReader.Close()
		return managedTarEntries(tar.NewReader(gzipReader), int64(len(content)))
	}
	if entries, err := managedTarEntries(tar.NewReader(bytes.NewReader(content)), int64(len(content))); err == nil {
		return entries, nil
	}
	return nil, errors.New("managed archive must be zip, tar, tar.gz, or tgz")
}

func managedZipEntries(reader *zip.Reader, compressedSize int64) ([]managedArchiveEntry, error) {
	out := []managedArchiveEntry{}
	entries := 0
	var extracted int64
	for _, file := range reader.File {
		name, ok := cleanManagedArchiveEntryName(file.Name)
		if !ok {
			return nil, errors.New("managed archive path must stay inside the archive root")
		}
		if name == "" {
			continue
		}
		entries++
		if entries > managedArchiveMaxFiles {
			return nil, managedArchiveLimitError()
		}
		info := file.FileInfo()
		if info.IsDir() {
			if err := validateManagedArchiveLimits(name, entries, extracted, compressedSize); err != nil {
				return nil, err
			}
			out = append(out, managedArchiveEntry{Name: name, Dir: true, Mode: archiveEntryMode(info.Mode(), true)})
			continue
		}
		if file.UncompressedSize64 > uint64(managedArchiveMaxExtracted) {
			return nil, managedArchiveLimitError()
		}
		extracted += int64(file.UncompressedSize64)
		if err := validateManagedArchiveLimits(name, entries, extracted, compressedSize); err != nil {
			return nil, err
		}
		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		content, readErr := io.ReadAll(rc)
		closeErr := rc.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		out = append(out, managedArchiveEntry{Name: name, Mode: archiveEntryMode(info.Mode(), false), Content: content})
	}
	return nonEmptyManagedArchive(out)
}

func managedTarEntries(reader *tar.Reader, compressedSize int64) ([]managedArchiveEntry, error) {
	out := []managedArchiveEntry{}
	entries := 0
	var extracted int64
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("managed archive tar is invalid: %w", err)
		}
		name, ok := cleanManagedArchiveEntryName(header.Name)
		if !ok {
			return nil, errors.New("managed archive path must stay inside the archive root")
		}
		if name == "" {
			continue
		}
		entries++
		if entries > managedArchiveMaxFiles {
			return nil, managedArchiveLimitError()
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := validateManagedArchiveLimits(name, entries, extracted, compressedSize); err != nil {
				return nil, err
			}
			out = append(out, managedArchiveEntry{Name: name, Dir: true, Mode: archiveEntryMode(header.FileInfo().Mode(), true)})
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 {
				return nil, managedArchiveLimitError()
			}
			extracted += header.Size
			if err := validateManagedArchiveLimits(name, entries, extracted, compressedSize); err != nil {
				return nil, err
			}
			content, err := io.ReadAll(reader)
			if err != nil {
				return nil, err
			}
			out = append(out, managedArchiveEntry{Name: name, Mode: archiveEntryMode(header.FileInfo().Mode(), false), Content: content})
		default:
			continue
		}
	}
	return nonEmptyManagedArchive(out)
}

func validateManagedArchiveLimits(name string, count int, extracted, compressed int64) error {
	depth := strings.Count(strings.Trim(name, "/"), "/") + 1
	if count > managedArchiveMaxFiles || depth > managedArchiveMaxDepth || extracted > managedArchiveMaxExtracted || (compressed > 0 && extracted > compressed*managedArchiveMaxRatio && extracted > 1<<20) {
		return managedArchiveLimitError()
	}
	return nil
}

func managedArchiveLimitError() error {
	return errors.New("managed archive exceeds file count, depth, extracted size, or compression ratio limits")
}

func nonEmptyManagedArchive(entries []managedArchiveEntry) ([]managedArchiveEntry, error) {
	if len(entries) == 0 {
		return nil, errors.New("managed archive is empty")
	}
	return entries, nil
}

type treeHashItem struct {
	Name   string
	Dir    bool
	Mode   os.FileMode
	SHA256 string
}

func managedArchiveTreeHash(entries []managedArchiveEntry) string {
	items := map[string]treeHashItem{}
	for _, entry := range entries {
		addImplicitTreeDirs(items, entry.Name)
		item := treeHashItem{Name: entry.Name, Dir: entry.Dir, Mode: entry.Mode.Perm()}
		if !entry.Dir {
			item.SHA256 = sha256Hex(entry.Content)
		}
		items[entry.Name] = item
	}
	return treeHash(items)
}

func directoryTreeHash(root string) (string, error) {
	items := map[string]treeHashItem{}
	err := filepath.WalkDir(root, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filePath == root {
			return nil
		}
		rel, err := filepath.Rel(root, filePath)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		item := treeHashItem{Name: name, Dir: entry.IsDir(), Mode: info.Mode().Perm()}
		if !entry.IsDir() {
			sum, err := fileSHA256Hex(filePath)
			if err != nil {
				return err
			}
			item.SHA256 = sum
		}
		items[name] = item
		return nil
	})
	if err != nil {
		return "", err
	}
	return treeHash(items), nil
}

func addImplicitTreeDirs(items map[string]treeHashItem, name string) {
	dir := path.Dir(name)
	if dir == "." || dir == "/" {
		return
	}
	parts := strings.Split(dir, "/")
	current := ""
	for _, part := range parts {
		if part == "" {
			continue
		}
		if current == "" {
			current = part
		} else {
			current += "/" + part
		}
		if _, ok := items[current]; !ok {
			items[current] = treeHashItem{Name: current, Dir: true, Mode: 0o700}
		}
	}
}

func treeHash(items map[string]treeHashItem) string {
	names := make([]string, 0, len(items))
	for name := range items {
		names = append(names, name)
	}
	sort.Strings(names)
	hash := sha256.New()
	for _, name := range names {
		item := items[name]
		_, _ = fmt.Fprintf(hash, "%s\x00%t\x00%04o\x00%s\x00", item.Name, item.Dir, item.Mode.Perm(), item.SHA256)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func cleanManagedArchiveEntryName(value string) (string, bool) {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	value = strings.TrimPrefix(value, "/")
	cleaned := path.Clean(value)
	if cleaned == "." {
		return "", true
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	return cleaned, true
}

func archiveEntryMode(mode os.FileMode, dir bool) os.FileMode {
	perm := mode.Perm()
	if perm != 0 {
		return perm
	}
	if dir {
		return 0o755
	}
	return 0o644
}

func looksLikeZip(content []byte) bool {
	return len(content) >= 4 && content[0] == 'P' && content[1] == 'K'
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func fileSHA256Hex(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func formatFileMode(mode os.FileMode) string {
	return fmt.Sprintf("%04o", mode.Perm())
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func safeRuntimePath(root, appID, instanceID, area, rel string) (string, error) {
	rel = path.Clean(strings.TrimPrefix(rel, "/"))
	if rel == "." || strings.HasPrefix(rel, "../") || rel == ".." {
		return "", errors.New("runtime file path must stay inside the application workspace")
	}
	base := filepath.Join(root, appID, "instances", instanceID, area)
	target := filepath.Join(base, filepath.FromSlash(rel))
	cleanBase, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	cleanTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if cleanTarget != cleanBase && !strings.HasPrefix(cleanTarget, cleanBase+string(os.PathSeparator)) {
		return "", errors.New("runtime file path escapes the application workspace")
	}
	return cleanTarget, nil
}

func safeApplicationRuntimeDir(root, appID, area string) (string, error) {
	appID = strings.TrimSpace(appID)
	area = strings.TrimSpace(area)
	if appID == "" || strings.ContainsAny(appID, `/\`) || area == "" || strings.ContainsAny(area, `/\`) {
		return "", errors.New("runtime application path is invalid")
	}
	base := filepath.Join(root, appID)
	target := filepath.Join(base, area)
	cleanBase, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	cleanTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if cleanTarget != cleanBase && !strings.HasPrefix(cleanTarget, cleanBase+string(os.PathSeparator)) {
		return "", errors.New("runtime application path escapes the application workspace")
	}
	return cleanTarget, nil
}

func safeApplicationRootDir(root, appID string) (string, error) {
	appID = strings.TrimSpace(appID)
	if appID == "" || strings.ContainsAny(appID, `/\`) {
		return "", errors.New("runtime application path is invalid")
	}
	target := filepath.Join(root, appID)
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	cleanTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if cleanTarget != cleanRoot && !strings.HasPrefix(cleanTarget, cleanRoot+string(os.PathSeparator)) {
		return "", errors.New("runtime application path escapes the runtime root")
	}
	return cleanTarget, nil
}

func safePersistentMountDir(root, appID, source string) (string, error) {
	appID = strings.TrimSpace(appID)
	source = strings.TrimSpace(source)
	if appID == "" || strings.ContainsAny(appID, `/\`) || source == "" {
		return "", errors.New("persistent mount path is invalid")
	}
	base := filepath.Join(root, appID, "persistent")
	target := filepath.Clean(source)
	cleanBase, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	cleanTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if cleanTarget != cleanBase && !strings.HasPrefix(cleanTarget, cleanBase+string(os.PathSeparator)) {
		return "", errors.New("persistent mount path escapes the persistent directory")
	}
	return cleanTarget, nil
}

func safeArchiveTarget(base, name string) (string, error) {
	name = path.Clean(strings.TrimPrefix(strings.ReplaceAll(name, "\\", "/"), "/"))
	if name == "." || strings.HasPrefix(name, "../") || name == ".." {
		return "", errors.New("archive path must stay inside the persistent directory")
	}
	target := filepath.Join(base, filepath.FromSlash(name))
	cleanBase, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	cleanTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if cleanTarget != cleanBase && !strings.HasPrefix(cleanTarget, cleanBase+string(os.PathSeparator)) {
		return "", errors.New("archive path escapes the persistent directory")
	}
	return cleanTarget, nil
}

func containerNameForInstance(instanceID string) string {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return "panel-unknown"
	}
	if strings.HasPrefix(instanceID, "panel-") {
		return instanceID
	}
	return "panel-" + sanitizeContainerPart(instanceID)
}

type dockerAPIClient struct {
	host        string
	client      *http.Client
	pullClient  *http.Client
	eventClient *http.Client
}

func newDockerAPIClient(host string) (*dockerAPIClient, error) {
	u, err := url.Parse(host)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "unix":
		socketPath := u.Path
		if socketPath == "" {
			socketPath = strings.TrimPrefix(host, "unix://")
		}
		if socketPath == "" {
			socketPath = "/var/run/docker.sock"
		}
		transport := &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		}
		pullTransport := &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		}
		return &dockerAPIClient{
			host:        host,
			client:      &http.Client{Transport: transport, Timeout: 2 * time.Minute},
			pullClient:  &http.Client{Transport: pullTransport, Timeout: dockerImagePullTimeout},
			eventClient: &http.Client{Transport: cloneDockerTransport(transport)},
		}, nil
	case "http", "https":
		return &dockerAPIClient{
			host:        host,
			client:      &http.Client{Timeout: 2 * time.Minute},
			pullClient:  &http.Client{Timeout: dockerImagePullTimeout},
			eventClient: &http.Client{},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported docker host %q", host)
	}
}

func cloneDockerTransport(base *http.Transport) *http.Transport {
	if base == nil {
		return nil
	}
	return base.Clone()
}

func (c *dockerAPIClient) ping(ctx context.Context) error {
	req, err := c.newRequest(ctx, http.MethodGet, "/_ping", nil)
	if err != nil {
		return err
	}
	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("docker ping failed: %s", res.Status)
	}
	return nil
}

func (c *dockerAPIClient) pullImage(ctx context.Context, image string) error {
	query := dockerPullImageQuery(image)
	req, err := c.newRequest(ctx, http.MethodPost, "/images/create?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	res, err := c.pullClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return dockerError(res, "pull image")
	}
	_, _ = io.Copy(io.Discard, res.Body)
	return nil
}

func dockerPullImageQuery(image string) url.Values {
	fromImage, tag := dockerPullImageParts(strings.TrimSpace(image))
	query := url.Values{}
	query.Set("fromImage", fromImage)
	if tag != "" {
		query.Set("tag", tag)
	}
	return query
}

func dockerPullImageParts(image string) (string, string) {
	if image == "" || strings.Contains(image, "@") {
		return image, ""
	}
	lastSlash := strings.LastIndex(image, "/")
	lastColon := strings.LastIndex(image, ":")
	if lastColon > lastSlash {
		fromImage := image[:lastColon]
		tag := image[lastColon+1:]
		if fromImage == "" || tag == "" {
			return image, ""
		}
		return fromImage, tag
	}
	return image, "latest"
}

func (c *dockerAPIClient) createContainer(ctx context.Context, spec appruntime.Spec) (string, error) {
	payload := dockerCreateRequest{
		Image:        spec.Image,
		Env:          dockerEnv(spec.Env),
		Cmd:          append([]string(nil), spec.Command...),
		ExposedPorts: dockerExposedPorts(spec.Ports),
		Labels: map[string]string{
			"panel.application.managed":     "true",
			"panel.application.id":          spec.ApplicationID,
			"panel.application.instance.id": spec.InstanceID,
			"panel.application.generation":  strconv.Itoa(spec.Generation),
			"panel.application.spec.hash":   spec.SpecHash,
		},
		HostConfig: dockerHostConfig{
			Binds:        dockerBinds(defaultRuntimeRoot, spec),
			PortBindings: dockerPortBindings(spec.Ports),
			NetworkMode:  dockerNetworkMode(spec.NetworkMode),
			ExtraHosts:   dockerExtraHosts(spec.NetworkMode),
			Privileged:   spec.Privileged,
			CapAdd:       append([]string(nil), spec.CapAdd...),
		},
	}
	if spec.Resources.MemoryMB > 0 {
		payload.HostConfig.Memory = int64(spec.Resources.MemoryMB) * 1024 * 1024
	}
	if spec.Resources.CPU > 0 {
		payload.HostConfig.NanoCPUs = int64(spec.Resources.CPU) * 1_000_000
	}
	query := url.Values{}
	query.Set("name", spec.ContainerName)
	body, _ := json.Marshal(payload)
	req, err := c.newRequest(ctx, http.MethodPost, "/containers/create?"+query.Encode(), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", dockerError(res, "create container")
	}
	var out struct {
		ID string `json:"Id"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.ID, nil
}

func (c *dockerAPIClient) startContainer(ctx context.Context, id string) error {
	err := c.emptyPost(ctx, "/containers/"+url.PathEscape(id)+"/start", "start container")
	if isDockerNotModified(err) {
		return nil
	}
	return err
}

func (c *dockerAPIClient) stopContainer(ctx context.Context, name string, timeout int) error {
	query := url.Values{}
	query.Set("t", strconv.Itoa(timeout))
	return c.emptyPost(ctx, "/containers/"+url.PathEscape(name)+"/stop?"+query.Encode(), "stop container")
}

func (c *dockerAPIClient) restartContainer(ctx context.Context, name string, timeout int) error {
	query := url.Values{}
	query.Set("t", strconv.Itoa(timeout))
	return c.emptyPost(ctx, "/containers/"+url.PathEscape(name)+"/restart?"+query.Encode(), "restart container")
}

func (c *dockerAPIClient) removeContainer(ctx context.Context, name string, force bool) error {
	query := url.Values{}
	if force {
		query.Set("force", "true")
	}
	query.Set("v", "true")
	req, err := c.newRequest(ctx, http.MethodDelete, "/containers/"+url.PathEscape(name)+"?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return dockerNotFound{name}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return dockerError(res, "remove container")
	}
	return nil
}

func (c *dockerAPIClient) inspectContainer(ctx context.Context, name string) (dockerInspectResponse, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/containers/"+url.PathEscape(name)+"/json", nil)
	if err != nil {
		return dockerInspectResponse{}, err
	}
	res, err := c.client.Do(req)
	if err != nil {
		return dockerInspectResponse{}, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return dockerInspectResponse{}, dockerNotFound{name}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return dockerInspectResponse{}, dockerError(res, "inspect container")
	}
	var out dockerInspectResponse
	return out, json.NewDecoder(res.Body).Decode(&out)
}

type dockerExecResult struct {
	ExitCode int
	Output   string
}

func (c *dockerAPIClient) execContainer(ctx context.Context, containerName string, command []string) (dockerExecResult, error) {
	if len(command) == 0 {
		return dockerExecResult{}, errors.New("container command is required")
	}
	createBody, _ := json.Marshal(map[string]any{
		"AttachStdout": true,
		"AttachStderr": true,
		"Tty":          true,
		"Cmd":          command,
	})
	req, err := c.newRequest(ctx, http.MethodPost, "/containers/"+url.PathEscape(containerName)+"/exec", bytes.NewReader(createBody))
	if err != nil {
		return dockerExecResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.client.Do(req)
	if err != nil {
		return dockerExecResult{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return dockerExecResult{}, dockerError(res, "create container exec")
	}
	var created struct {
		ID string `json:"Id"`
	}
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		return dockerExecResult{}, err
	}
	if strings.TrimSpace(created.ID) == "" {
		return dockerExecResult{}, errors.New("docker exec id is empty")
	}
	startBody, _ := json.Marshal(map[string]any{"Detach": false, "Tty": true})
	startReq, err := c.newRequest(ctx, http.MethodPost, "/exec/"+url.PathEscape(created.ID)+"/start", bytes.NewReader(startBody))
	if err != nil {
		return dockerExecResult{}, err
	}
	startReq.Header.Set("Content-Type", "application/json")
	startRes, err := c.client.Do(startReq)
	if err != nil {
		return dockerExecResult{}, err
	}
	defer startRes.Body.Close()
	if startRes.StatusCode < 200 || startRes.StatusCode >= 300 {
		return dockerExecResult{}, dockerError(startRes, "start container exec")
	}
	output, err := io.ReadAll(io.LimitReader(startRes.Body, maxRuntimeCommandOutput+1))
	if err != nil {
		return dockerExecResult{}, err
	}
	if len(output) > maxRuntimeCommandOutput {
		return dockerExecResult{}, errors.New("container command output exceeds limit")
	}
	inspectReq, err := c.newRequest(ctx, http.MethodGet, "/exec/"+url.PathEscape(created.ID)+"/json", nil)
	if err != nil {
		return dockerExecResult{}, err
	}
	inspectRes, err := c.client.Do(inspectReq)
	if err != nil {
		return dockerExecResult{}, err
	}
	defer inspectRes.Body.Close()
	if inspectRes.StatusCode < 200 || inspectRes.StatusCode >= 300 {
		return dockerExecResult{}, dockerError(inspectRes, "inspect container exec")
	}
	var inspected struct {
		Running  bool `json:"Running"`
		ExitCode int  `json:"ExitCode"`
	}
	if err := json.NewDecoder(inspectRes.Body).Decode(&inspected); err != nil {
		return dockerExecResult{}, err
	}
	if inspected.Running {
		return dockerExecResult{}, errors.New("container command is still running")
	}
	return dockerExecResult{ExitCode: inspected.ExitCode, Output: string(output)}, nil
}

func (c *dockerAPIClient) containerLogs(ctx context.Context, name string, tail int) (string, error) {
	query := url.Values{}
	query.Set("stdout", "true")
	query.Set("stderr", "true")
	query.Set("tail", strconv.Itoa(tail))
	req, err := c.newRequest(ctx, http.MethodGet, "/containers/"+url.PathEscape(name)+"/logs?"+query.Encode(), nil)
	if err != nil {
		return "", err
	}
	res, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return "", dockerNotFound{name}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", dockerError(res, "read container logs")
	}
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	return decodeDockerLogs(raw), nil
}

func (c *dockerAPIClient) emptyPost(ctx context.Context, endpoint, action string) error {
	req, err := c.newRequest(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return dockerNotFound{endpoint}
	}
	if res.StatusCode == http.StatusNotModified {
		return dockerNotModified{endpoint}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return dockerError(res, action)
	}
	return nil
}

func (c *dockerAPIClient) listContainers(ctx context.Context) ([]agentcontract.DockerContainer, error) {
	var out []agentcontract.DockerContainer
	if err := c.getJSON(ctx, "/containers/json?all=true", "list containers", &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []agentcontract.DockerContainer{}
	}
	return out, nil
}

func (c *dockerAPIClient) watchContainerEvents(ctx context.Context, out chan<- struct{}) error {
	filters, _ := json.Marshal(map[string][]string{
		"type":  {"container"},
		"event": {"create", "start", "stop", "die", "destroy", "restart", "kill", "pause", "unpause"},
	})
	query := url.Values{}
	query.Set("filters", string(filters))
	req, err := c.newRequest(ctx, http.MethodGet, "/events?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	client := c.eventClient
	if client == nil {
		client = c.client
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return dockerError(res, "watch container events")
	}
	decoder := json.NewDecoder(res.Body)
	for ctx.Err() == nil {
		var event map[string]any
		if err := decoder.Decode(&event); err != nil {
			if errors.Is(err, io.EOF) && ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case out <- struct{}{}:
		default:
		}
	}
	return ctx.Err()
}

func (c *dockerAPIClient) listImages(ctx context.Context) ([]agentcontract.DockerImage, error) {
	var out []agentcontract.DockerImage
	if err := c.getJSON(ctx, "/images/json?all=true", "list images", &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []agentcontract.DockerImage{}
	}
	return out, nil
}

func (c *dockerAPIClient) removeImage(ctx context.Context, id string) error {
	query := url.Values{}
	query.Set("force", "false")
	req, err := c.newRequest(ctx, http.MethodDelete, "/images/"+url.PathEscape(id)+"?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return nil
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return dockerError(res, "remove image")
	}
	return nil
}

func (c *dockerAPIClient) listNetworks(ctx context.Context) ([]agentcontract.DockerNetwork, error) {
	var out []agentcontract.DockerNetwork
	if err := c.getJSON(ctx, "/networks", "list networks", &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []agentcontract.DockerNetwork{}
	}
	return out, nil
}

func (c *dockerAPIClient) listVolumes(ctx context.Context) ([]agentcontract.DockerVolume, error) {
	var raw struct {
		Volumes []agentcontract.DockerVolume `json:"Volumes"`
	}
	if err := c.getJSON(ctx, "/volumes", "list volumes", &raw); err != nil {
		return nil, err
	}
	containers, err := c.listContainers(ctx)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, container := range containers {
		for _, mount := range container.Mounts {
			if mount.Type == "volume" && mount.Name != "" {
				counts[mount.Name]++
			}
		}
	}
	for i := range raw.Volumes {
		raw.Volumes[i].ContainerCount = counts[raw.Volumes[i].Name]
		raw.Volumes[i].InUse = raw.Volumes[i].ContainerCount > 0
	}
	if raw.Volumes == nil {
		raw.Volumes = []agentcontract.DockerVolume{}
	}
	return raw.Volumes, nil
}

func (c *dockerAPIClient) removeVolume(ctx context.Context, name string) error {
	req, err := c.newRequest(ctx, http.MethodDelete, "/volumes/"+url.PathEscape(name), nil)
	if err != nil {
		return err
	}
	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return nil
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return dockerError(res, "remove volume")
	}
	return nil
}

func (c *dockerAPIClient) getJSON(ctx context.Context, endpoint, action string, out any) error {
	req, err := c.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return dockerError(res, action)
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func (c *dockerAPIClient) newRequest(ctx context.Context, method, endpoint string, body io.Reader) (*http.Request, error) {
	target := "http://docker" + endpoint
	if strings.HasPrefix(c.host, "http://") || strings.HasPrefix(c.host, "https://") {
		target = strings.TrimRight(c.host, "/") + endpoint
	}
	return http.NewRequestWithContext(ctx, method, target, body)
}

type dockerCreateRequest struct {
	Image        string              `json:"Image"`
	Env          []string            `json:"Env,omitempty"`
	Cmd          []string            `json:"Cmd,omitempty"`
	ExposedPorts map[string]struct{} `json:"ExposedPorts,omitempty"`
	Labels       map[string]string   `json:"Labels,omitempty"`
	HostConfig   dockerHostConfig    `json:"HostConfig"`
}

type dockerHostConfig struct {
	Binds        []string                       `json:"Binds,omitempty"`
	PortBindings map[string][]map[string]string `json:"PortBindings,omitempty"`
	NetworkMode  string                         `json:"NetworkMode,omitempty"`
	ExtraHosts   []string                       `json:"ExtraHosts,omitempty"`
	Privileged   bool                           `json:"Privileged,omitempty"`
	CapAdd       []string                       `json:"CapAdd,omitempty"`
	Memory       int64                          `json:"Memory,omitempty"`
	NanoCPUs     int64                          `json:"NanoCpus,omitempty"`
}

func (c *dockerAPIClient) ensureManagedNetwork(ctx context.Context, networkMode string) error {
	if networkMode != managedBridgeNetwork {
		return nil
	}
	if err := c.inspectNetwork(ctx, managedBridgeNetwork); err == nil {
		return nil
	} else if !isDockerNotFound(err) {
		return err
	}
	return c.createNetwork(ctx, managedBridgeNetwork)
}

func (c *dockerAPIClient) inspectNetwork(ctx context.Context, name string) error {
	req, err := c.newRequest(ctx, http.MethodGet, "/networks/"+url.PathEscape(name), nil)
	if err != nil {
		return err
	}
	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return dockerNotFound{name}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return dockerError(res, "inspect network")
	}
	_, _ = io.Copy(io.Discard, res.Body)
	return nil
}

func (c *dockerAPIClient) createNetwork(ctx context.Context, name string) error {
	body, _ := json.Marshal(map[string]any{
		"Name":       name,
		"Driver":     "bridge",
		"Attachable": true,
		"Labels": map[string]string{
			"panel.managed": "true",
		},
	})
	req, err := c.newRequest(ctx, http.MethodPost, "/networks/create", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		err := dockerError(res, "create network")
		if isDockerAlreadyExists(err) {
			return nil
		}
		return err
	}
	_, _ = io.Copy(io.Discard, res.Body)
	return nil
}

func dockerNetworkMode(mode string) string {
	if strings.TrimSpace(mode) == "bridge" {
		return managedBridgeNetwork
	}
	return mode
}

func dockerExtraHosts(mode string) []string {
	if strings.TrimSpace(mode) == "bridge" {
		return []string{"host.docker.internal:host-gateway"}
	}
	return nil
}

type dockerInspectResponse struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	Config struct {
		Image string `json:"Image"`
	} `json:"Config"`
	State struct {
		Status     string `json:"Status"`
		Running    bool   `json:"Running"`
		ExitCode   int    `json:"ExitCode"`
		Error      string `json:"Error"`
		StartedAt  string `json:"StartedAt"`
		FinishedAt string `json:"FinishedAt"`
	} `json:"State"`
}

func dockerEnv(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for key, value := range env {
		out = append(out, key+"="+value)
	}
	return out
}

func dockerExposedPorts(ports []appruntime.Port) map[string]struct{} {
	out := map[string]struct{}{}
	for _, port := range ports {
		if port.ContainerPort <= 0 {
			continue
		}
		out[dockerPortKey(port)] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func dockerPortBindings(ports []appruntime.Port) map[string][]map[string]string {
	out := map[string][]map[string]string{}
	for _, port := range ports {
		if port.ContainerPort <= 0 || port.HostPort <= 0 {
			continue
		}
		out[dockerPortKey(port)] = []map[string]string{{"HostPort": strconv.Itoa(port.HostPort)}}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func dockerPortKey(port appruntime.Port) string {
	proto := strings.TrimSpace(port.Protocol)
	if proto == "" {
		proto = "tcp"
	}
	return strconv.Itoa(port.ContainerPort) + "/" + proto
}

func dockerBinds(root string, spec appruntime.Spec) []string {
	binds := []string{}
	for _, mount := range spec.Mounts {
		if strings.TrimSpace(mount.Target) == "" || strings.TrimSpace(mount.Source) == "" {
			continue
		}
		source := mount.Source
		switch mount.Type {
		case "managed_file":
			source = filepath.Join(root, spec.ApplicationID, "instances", spec.InstanceID, "files", filepath.FromSlash(path.Clean(strings.TrimPrefix(mount.Source, "/"))))
		case "persistent":
			source = mount.Source
		}
		mode := "rw"
		if mount.ReadOnly {
			mode = "ro"
		}
		binds = append(binds, source+":"+mount.Target+":"+mode)
	}
	return binds
}

func dockerStateToRuntime(status string, running bool, exitCode int) string {
	if running || status == "running" {
		return appruntime.StatusRunning
	}
	switch status {
	case "created", "restarting", "paused":
		return appruntime.StatusPending
	case "exited":
		if exitCode == 0 {
			return appruntime.StatusStopped
		}
		return appruntime.StatusFailed
	case "dead":
		return appruntime.StatusFailed
	default:
		if status == "" {
			return appruntime.StatusUnknown
		}
		return status
	}
}

type dockerErrorResponse struct {
	Message string `json:"message"`
	Error   string `json:"error"`
}

func dockerError(res *http.Response, action string) error {
	var er dockerErrorResponse
	raw, _ := io.ReadAll(res.Body)
	_ = json.Unmarshal(raw, &er)
	msg := firstNonEmpty(er.Message, er.Error, strings.TrimSpace(string(raw)), res.Status)
	return fmt.Errorf("%s failed: %s", action, msg)
}

type dockerNotFound struct{ name string }

func (e dockerNotFound) Error() string { return "docker container not found: " + e.name }

func isDockerNotFound(err error) bool {
	var nf dockerNotFound
	return errors.As(err, &nf)
}

func isDockerAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exists") || strings.Contains(msg, "is already present")
}

type dockerNotModified struct{ name string }

func (e dockerNotModified) Error() string {
	return "docker resource already has requested state: " + e.name
}

func isDockerNotModified(err error) bool {
	var target dockerNotModified
	return errors.As(err, &target)
}

func decodeDockerLogs(raw []byte) string {
	if len(raw) < 8 {
		return string(raw)
	}
	var out bytes.Buffer
	for len(raw) >= 8 {
		size := int(binary.BigEndian.Uint32(raw[4:8]))
		if size < 0 || len(raw) < 8+size {
			return string(raw)
		}
		out.Write(raw[8 : 8+size])
		raw = raw[8+size:]
	}
	if len(raw) > 0 {
		out.Write(raw)
	}
	return out.String()
}

func sanitizeContainerPart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('-')
	}
	out := strings.Trim(b.String(), "-_.")
	if out == "" {
		return "runtime"
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func normalizeLogTail(tail int) int {
	if tail <= 0 {
		return 200
	}
	if tail > 10000 {
		return 10000
	}
	return tail
}
