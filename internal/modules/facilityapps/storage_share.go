package facilityapps

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/agent/nfsvol"
	"panel/internal/modules/applications"
	appruntime "panel/internal/modules/applications/runtime"
	server "panel/internal/modules/servers"
	"panel/internal/modules/tasks"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/logging"
	sshx "panel/internal/platform/ssh"

	"go.uber.org/zap"
)

const (
	// StorageShareID 是存储共享设施的唯一配置 ID。
	StorageShareID = "storage-share"
	// storageReconcileTaskType 是存储共享导出同步的后台任务类型。
	storageReconcileTaskType = "storage_share_reconcile"
)

var (
	storageRootPattern = regexp.MustCompile(`^/[A-Za-z0-9._/-]+$`)
	storageIDPattern   = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	storageHostPattern = regexp.MustCompile(`^[A-Za-z0-9.-]+$`)
)

// StorageServerSetting 是一台存储服务器及其独立根目录。
type StorageServerSetting struct {
	ServerID string `json:"serverId"`
	Root     string `json:"root"`
}

// StorageShareConfig 是存储共享设施的对外配置与状态。
type StorageShareConfig struct {
	ID         string                           `json:"id"`
	Version    int                              `json:"version"`
	Servers    []StorageServerSetting           `json:"servers"`
	Partitions []StoragePartition               `json:"partitions"`
	References []applications.StorageShareUsage `json:"references,omitempty"`
	LastError  string                           `json:"lastError,omitempty"`
	UpdatedAt  time.Time                        `json:"updatedAt"`
	Enabled    bool                             `json:"enabled"`
}

// StorageShareSaveInput 是存储共享设施的保存输入（每台服务器各自根目录）。
type StorageShareSaveInput struct {
	Servers []StorageServerSetting `json:"servers"`
	Version int                    `json:"version"`
}

// StoragePartition 是按（存储服务器 × 应用 × 应用节点）分配的分区记录。
type StoragePartition struct {
	ID                string    `json:"id"`
	ApplicationID     string    `json:"applicationId"`
	ApplicationName   string    `json:"applicationName"`
	ServerID          string    `json:"serverId"`
	ServerName        string    `json:"serverName"`
	StorageServerID   string    `json:"storageServerId,omitempty"`
	StorageServerName string    `json:"storageServerName,omitempty"`
	Path              string    `json:"path"`
	Target            string    `json:"target,omitempty"`
	VolumeName        string    `json:"volumeName,omitempty"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

// StoragePartitionDownload 是分区数据的打包下载结果。
type StoragePartitionDownload struct {
	Filename string
	Content  []byte
}

// StorageAgentClient 是存储共享设施依赖的 Agent 存储能力。存储服务器的
// 导出配置、打包下载与目录删除都通过 Agent 执行，不使用 Panel 侧 SSH。
type StorageAgentClient interface {
	StorageConfigureExport(ctx context.Context, baseURL, root string, allowedHosts []string, enabled bool) error
	StorageArchiveDirectory(ctx context.Context, baseURL, path string) ([]byte, string, error)
	StorageDeleteDirectory(ctx context.Context, baseURL, path string) error
	StorageEnsureDirectory(ctx context.Context, baseURL, path string) error
	StorageStatus(ctx context.Context, baseURL, root string) (agentcontract.StorageExportStatus, error)
	StorageMountStatus(ctx context.Context, baseURL, source, target string) (agentcontract.StorageMountStatus, error)
}

type storageConfigRow struct {
	Version     int
	ServersJSON string `orm:"column:servers_json"`
	LastError   string
	UpdatedAt   string
}

func (s *Service) storageAgent() (StorageAgentClient, bool) {
	client, ok := s.agent.(StorageAgentClient)
	return client, ok
}

// storageAgentError 把旧 Agent 的 Unimplemented 类错误翻译为可操作的升级提示。
func storageAgentError(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "unimplemented") || strings.Contains(msg, "unknown service") || strings.Contains(msg, "unknown method") {
		return panelerr.Validation("storage_share_agent_required", "agent does not support storage share; upgrade the agent on this server")
	}
	return err
}

func storageAgentEndpoint(srv server.Server) (string, bool) {
	if srv.Traits == nil || strings.TrimSpace(srv.Traits[agentcontract.TraitEnabled]) != "true" {
		return "", false
	}
	u := strings.TrimSpace(srv.Traits[agentcontract.TraitURL])
	return u, u != ""
}

func (s *Service) loadStorageConfig(ctx context.Context) (StorageShareConfig, error) {
	cfg := StorageShareConfig{ID: StorageShareID}
	var row storageConfigRow
	err := orm.New(s.db).From("storage_share_configs").Select("version", "servers_json", "last_error", "updated_at").Where("id=?", StorageShareID).First(ctx, &row)
	if err == sql.ErrNoRows {
		return cfg, nil
	}
	if err != nil {
		return StorageShareConfig{}, err
	}
	cfg.Version = row.Version
	cfg.LastError = row.LastError
	cfg.UpdatedAt = parseTime(row.UpdatedAt)
	_ = json.Unmarshal([]byte(row.ServersJSON), &cfg.Servers)
	cfg.Servers = normalizeStorageServers(cfg.Servers)
	cfg.Enabled = len(cfg.Servers) > 0
	return cfg, nil
}

func normalizeStorageServers(servers []StorageServerSetting) []StorageServerSetting {
	byID := map[string]string{}
	order := []string{}
	for _, item := range servers {
		id := strings.TrimSpace(item.ServerID)
		root := strings.TrimSpace(item.Root)
		if id == "" || root == "" {
			continue
		}
		if _, ok := byID[id]; !ok {
			order = append(order, id)
		}
		byID[id] = root
	}
	sort.Strings(order)
	out := make([]StorageServerSetting, 0, len(order))
	for _, id := range order {
		out = append(out, StorageServerSetting{ServerID: id, Root: byID[id]})
	}
	return out
}

func (s *Service) saveStorageConfig(ctx context.Context, cfg StorageShareConfig) error {
	servers, _ := json.Marshal(cfg.Servers)
	_, err := orm.RawExec(ctx, s.db, `INSERT INTO storage_share_configs(id,version,servers_json,last_error,updated_at)
VALUES(?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET version=excluded.version,servers_json=excluded.servers_json,last_error=excluded.last_error,updated_at=excluded.updated_at`,
		StorageShareID, cfg.Version, string(servers), cfg.LastError, formatTime(cfg.UpdatedAt))
	return err
}

func (s *Service) setStorageLastError(ctx context.Context, message string) error {
	_, err := orm.RawExec(ctx, s.db, `UPDATE storage_share_configs SET last_error=?, updated_at=? WHERE id=?`,
		message, formatTime(time.Now().UTC()), StorageShareID)
	return err
}

// GetStorageShare 返回设施配置、分区历史与引用它的应用。
func (s *Service) GetStorageShare(ctx context.Context) (StorageShareConfig, error) {
	cfg, err := s.loadStorageConfig(ctx)
	if err != nil {
		return StorageShareConfig{}, err
	}
	partitions, err := s.listStoragePartitions(ctx)
	if err != nil {
		return StorageShareConfig{}, err
	}
	cfg.Partitions = partitions
	if provider, ok := s.apps.(applications.StorageShareUsageProvider); ok {
		if usages, usageErr := provider.ApplicationsUsingStorageShare(ctx); usageErr == nil {
			cfg.References = usages
		}
	}
	return cfg, nil
}

// SaveStorageShare 保存设施配置（多台存储服务器，各自根目录）并立即通过
// Agent 在每台存储服务器上配置 NFS 导出。
func (s *Service) SaveStorageShare(ctx context.Context, in StorageShareSaveInput) (StorageShareConfig, error) {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	servers := normalizeStorageServers(in.Servers)
	if len(servers) == 0 {
		return StorageShareConfig{}, panelerr.Validation("storage_share_config_required", "at least one storage server with a root is required")
	}
	for _, item := range servers {
		if !validStorageRoot(item.Root) {
			return StorageShareConfig{}, panelerr.Validation("storage_share_root_invalid", "storage root must be an absolute Linux path without spaces")
		}
		if _, err := s.servers.Get(ctx, item.ServerID); err != nil {
			return StorageShareConfig{}, err
		}
	}
	previous, err := s.loadStorageConfig(ctx)
	if err != nil {
		return StorageShareConfig{}, err
	}
	if in.Version != previous.Version {
		return StorageShareConfig{}, panelerr.Conflict("storage_share_version_conflict", "storage share configuration was changed by another request; reload and retry")
	}
	if previous.Enabled {
		// 根目录启用后不可修改：同一台服务器的根目录必须保持不变，要改只能先卸载再重新启用。
		for _, previousServer := range previous.Servers {
			for _, nextServer := range servers {
				if nextServer.ServerID == previousServer.ServerID && nextServer.Root != previousServer.Root {
					return StorageShareConfig{}, panelerr.Validation("storage_share_root_immutable", "storage root cannot be changed after the facility is enabled; uninstall and re-enable instead")
				}
			}
		}
		// 被应用引用的服务器不允许移除。
		for _, previousServer := range previous.Servers {
			removed := true
			for _, nextServer := range servers {
				if nextServer.ServerID == previousServer.ServerID {
					removed = false
					break
				}
			}
			if !removed {
				continue
			}
			names, checkErr := s.partitionReferenceNames(ctx, previousServer.ServerID)
			if checkErr != nil {
				return StorageShareConfig{}, checkErr
			}
			if len(names) > 0 {
				return StorageShareConfig{}, panelerr.Conflict("storage_share_server_in_use", "storage server is still referenced by applications: "+strings.Join(names, ", "))
			}
		}
		// 被移除的服务器先关闭导出，清理失败则阻止保存，避免旧导出残留。
		for _, previousServer := range previous.Servers {
			removed := true
			for _, nextServer := range servers {
				if nextServer.ServerID == previousServer.ServerID {
					removed = false
					break
				}
			}
			if !removed {
				continue
			}
			if err := s.removeStorageExport(ctx, previousServer); err != nil {
				return StorageShareConfig{}, fmt.Errorf("remove export from %s before saving: %w", previousServer.ServerID, err)
			}
		}
	}
	cfg := StorageShareConfig{ID: StorageShareID, Version: previous.Version + 1, Servers: servers, LastError: "", UpdatedAt: time.Now().UTC()}
	if err := s.saveStorageConfig(ctx, cfg); err != nil {
		return StorageShareConfig{}, err
	}
	if err := s.reconcileStorageServers(ctx, cfg); err != nil {
		_ = s.setStorageLastError(ctx, err.Error())
	}
	return s.GetStorageShare(ctx)
}

// DeleteStorageShare 卸载设施：仅当没有应用再引用时才允许。远端清理通过
// Agent 执行且为尽力而为——清理失败不阻塞卸载，配置照常删除、分区历史与
// 数据保留，失败信息通过返回配置的 lastError 暴露给前端。
func (s *Service) DeleteStorageShare(ctx context.Context) (StorageShareConfig, error) {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	cfg, err := s.loadStorageConfig(ctx)
	if err != nil {
		return StorageShareConfig{}, err
	}
	if provider, ok := s.apps.(applications.StorageShareUsageProvider); ok {
		usages, usageErr := provider.ApplicationsUsingStorageShare(ctx)
		if usageErr != nil {
			return StorageShareConfig{}, usageErr
		}
		if len(usages) > 0 {
			names := make([]string, 0, len(usages))
			for _, usage := range usages {
				names = append(names, usage.ApplicationName)
			}
			return StorageShareConfig{}, panelerr.Conflict("storage_share_in_use", "Storage share is still used by applications: "+strings.Join(names, ", "))
		}
	}
	if cfg.Enabled {
		partitions, listErr := s.listStoragePartitions(ctx)
		if listErr != nil {
			return StorageShareConfig{}, listErr
		}
		for _, partition := range partitions {
			active, _, mountErr := s.storagePartitionMountActive(ctx, partition)
			if mountErr != nil {
				continue
			}
			if active {
				return StorageShareConfig{}, panelerr.Conflict("storage_share_in_use", "storage share is still mounted by running applications; stop or remove the mounts first")
			}
		}
	}
	var cleanupErr error
	if cfg.Enabled {
		for _, item := range cfg.Servers {
			if err := s.removeStorageExport(ctx, item); err != nil {
				logging.L().Warn("storage share uninstall remote cleanup failed", zap.String("server_id", item.ServerID), zap.Error(err))
				if cleanupErr == nil {
					cleanupErr = err
				}
			}
		}
	}
	if cleanupErr != nil {
		// 清理失败时不删除配置，保留可重试入口并把错误持久化。
		_ = s.setStorageLastError(ctx, "Remote export cleanup failed: "+cleanupErr.Error())
		cfg.LastError = "Remote export cleanup failed: " + cleanupErr.Error()
		cfg.Enabled = true
		return cfg, nil
	}
	if err := orm.New(s.db).From("storage_share_configs").Where("id=?", StorageShareID).Delete(ctx); err != nil {
		return StorageShareConfig{}, err
	}
	result, err := s.GetStorageShare(ctx)
	if err != nil {
		return StorageShareConfig{}, err
	}
	return result, nil
}

// StorageShareReconcileResult 是手动同步的提交结果（后台任务执行）。
type StorageShareReconcileResult struct {
	TaskID string             `json:"taskId"`
	Config StorageShareConfig `json:"config"`
}

// ReconcileStorageShareNow 提交一次导出同步后台任务。
func (s *Service) ReconcileStorageShareNow(ctx context.Context) (StorageShareReconcileResult, error) {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	cfg, err := s.loadStorageConfig(ctx)
	if err != nil {
		return StorageShareReconcileResult{}, err
	}
	if !cfg.Enabled {
		return StorageShareReconcileResult{}, panelerr.Validation("storage_share_not_configured", "Storage share is not configured")
	}
	if s.tasks == nil {
		return StorageShareReconcileResult{}, panelerr.Validation("storage_share_task_unavailable", "task service is unavailable")
	}
	serverID := ""
	if len(cfg.Servers) > 0 {
		serverID = cfg.Servers[0].ServerID
	}
	task, err := s.tasks.Create(ctx, tasks.CreateInput{
		Type:         storageReconcileTaskType,
		Summary:      "Syncing storage share exports",
		ServerID:     serverID,
		ResourceType: "facility",
		ResourceID:   StorageShareID,
		ParamsJSON:   "{}",
	})
	if err != nil {
		_ = s.setStorageLastError(ctx, err.Error())
		return StorageShareReconcileResult{}, err
	}
	config, err := s.GetStorageShare(ctx)
	if err != nil {
		return StorageShareReconcileResult{}, err
	}
	return StorageShareReconcileResult{TaskID: task.ID, Config: config}, nil
}

// runStorageShareReconcileTask 是导出同步后台任务的执行体。
func (s *Service) runStorageShareReconcileTask(tc tasks.TaskContext) error {
	cfg, err := s.loadStorageConfig(tc.Context)
	if err != nil {
		return err
	}
	if !cfg.Enabled {
		return nil
	}
	if err := s.reconcileStorageServers(tc.Context, cfg); err != nil {
		_ = s.setStorageLastError(tc.Context, err.Error())
		return err
	}
	_ = s.setStorageLastError(tc.Context, "")
	return nil
}

func (s *Service) reconcileStorageServers(ctx context.Context, cfg StorageShareConfig) error {
	var firstErr error
	for _, item := range cfg.Servers {
		if err := s.reconcileStorageServer(ctx, item); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// ResolveStorageShareMounts 把应用运行时规格里的 storage_share 挂载解析为 NFS
// 挂载，并登记按（存储服务器 × 应用 × 应用节点）分配的分区记录。只有当挂载列表
// 里确实包含 storage_share 时才检查设施配置，避免无关应用受设施状态影响。
func (s *Service) ResolveStorageShareMounts(ctx context.Context, app applications.Application, srv server.Server, mounts []appruntime.Mount) ([]appruntime.Mount, error) {
	hasStorageShare := false
	for _, mount := range mounts {
		if strings.TrimSpace(mount.Type) == "storage_share" {
			hasStorageShare = true
			break
		}
	}
	if !hasStorageShare {
		return mounts, nil
	}
	cfg, err := s.loadStorageConfig(ctx)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, panelerr.Validation("storage_share_unavailable", "Storage share facility is not configured")
	}
	if !validStorageID(app.ID) || !validStorageID(srv.ID) {
		return nil, panelerr.Validation("storage_share_partition_invalid", "storage share partition identifiers are invalid")
	}
	out := make([]appruntime.Mount, 0, len(mounts))
	for _, mount := range mounts {
		if strings.TrimSpace(mount.Type) != "storage_share" {
			out = append(out, mount)
			continue
		}
		setting, err := resolveStorageServerSetting(mount.Source, cfg.Servers)
		if err != nil {
			return nil, err
		}
		storageServer, err := s.servers.Get(ctx, setting.ServerID)
		if err != nil {
			return nil, err
		}
		partitionPath := storagePartitionPath(setting.Root, setting.ServerID, srv.ID, app.ID)
		nfsSource := storageNFSSource(storageServer.Host, partitionPath)
		if err := s.ensureStorageDirectory(ctx, storageServer, partitionPath); err != nil {
			return nil, err
		}
		out = append(out, appruntime.Mount{
			Type:     "nfs",
			Source:   nfsSource,
			Target:   mount.Target,
			ReadOnly: mount.ReadOnly,
		})
		if err := s.upsertStoragePartition(ctx, app, srv, storageServer, setting.Root, partitionPath, mount.Target, nfsvol.Name(nfsSource, mount.Target)); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func resolveStorageServerSetting(source string, servers []StorageServerSetting) (StorageServerSetting, error) {
	source = strings.TrimSpace(source)
	switch {
	case source == "" || source == StorageShareID:
		if len(servers) == 0 {
			return StorageServerSetting{}, panelerr.Validation("storage_share_unavailable", "Storage share facility is not configured")
		}
		return servers[0], nil
	case strings.HasPrefix(source, StorageShareID+":"):
		id := strings.TrimPrefix(source, StorageShareID+":")
		for _, item := range servers {
			if item.ServerID == id {
				return item, nil
			}
		}
		return StorageServerSetting{}, panelerr.Validation("storage_share_server_invalid", "storage share mount source references a server that is not part of the facility")
	default:
		return StorageServerSetting{}, panelerr.Validation("storage_share_source_invalid", "storage share mount source must reference the storage share facility")
	}
}

// DownloadStoragePartition 通过存储服务器 Agent 打包分区目录并返回内容。
func (s *Service) DownloadStoragePartition(ctx context.Context, partitionID string) (StoragePartitionDownload, error) {
	partition, err := s.getStoragePartition(ctx, strings.TrimSpace(partitionID))
	if err != nil {
		return StoragePartitionDownload{}, err
	}
	if !validStoragePath(partition.Path) {
		return StoragePartitionDownload{}, panelerr.Validation("storage_share_partition_path_invalid", "storage share partition path is invalid")
	}
	storageServer, err := s.storageServerForPartition(ctx, partition)
	if err != nil {
		return StoragePartitionDownload{}, err
	}
	agent, ok := s.storageAgent()
	if !ok {
		return StoragePartitionDownload{}, panelerr.Validation("storage_share_agent_required", "storage share requires an agent with storage support; upgrade the agent on the storage server")
	}
	baseURL, ok := storageAgentEndpoint(storageServer)
	if !ok {
		return StoragePartitionDownload{}, panelerr.Validation("agent_required", "Agent is required for storage share operations")
	}
	content, _, err := agent.StorageArchiveDirectory(ctx, baseURL, partition.Path)
	if err != nil {
		return StoragePartitionDownload{}, storageAgentError(err)
	}
	return StoragePartitionDownload{Filename: storagePartitionDownloadName(partition), Content: content}, nil
}

// DeleteStoragePartition 删除分区记录并删除存储服务器上的实际数据。
// 只有当对应应用不再引用共享存储时才允许。
func (s *Service) DeleteStoragePartition(ctx context.Context, partitionID string) error {
	partition, err := s.getStoragePartition(ctx, strings.TrimSpace(partitionID))
	if err != nil {
		return err
	}
	if provider, ok := s.apps.(applications.StorageShareUsageProvider); ok {
		usages, usageErr := provider.ApplicationsUsingStorageShare(ctx)
		if usageErr != nil {
			return usageErr
		}
		for _, usage := range usages {
			if usage.ApplicationID == partition.ApplicationID {
				return panelerr.Conflict("storage_share_partition_in_use", "storage share partition is still used by application "+usage.ApplicationName)
			}
		}
	}
	if !validStoragePath(partition.Path) {
		return panelerr.Validation("storage_share_partition_path_invalid", "storage share partition path is invalid")
	}
	active, _, mountErr := s.storagePartitionMountActive(ctx, partition)
	if mountErr != nil {
		return mountErr
	}
	if active {
		return panelerr.Conflict("storage_share_partition_in_use", "partition is still mounted by the running application; stop or remove the mount first")
	}
	storageServer, err := s.storageServerForPartition(ctx, partition)
	if err != nil {
		return err
	}
	agent, ok := s.storageAgent()
	if !ok {
		return panelerr.Validation("storage_share_agent_required", "storage share requires an agent with storage support; upgrade the agent on the storage server")
	}
	baseURL, ok := storageAgentEndpoint(storageServer)
	if !ok {
		return panelerr.Validation("agent_required", "Agent is required for storage share operations")
	}
	if err := agent.StorageDeleteDirectory(ctx, baseURL, partition.Path); err != nil {
		return storageAgentError(err)
	}
	return orm.New(s.db).From("storage_share_partitions").Where("id=?", partition.ID).Delete(ctx)
}

func (s *Service) getStoragePartition(ctx context.Context, partitionID string) (StoragePartition, error) {
	var row StoragePartition
	var createdAt, updatedAt string
	err := orm.New(s.db).From("storage_share_partitions").
		Select("id", "application_id", "application_name", "server_id", "server_name", "storage_server_id", "storage_server_name", "path", "target", "volume_name", "created_at", "updated_at").
		Where("id=?", partitionID).First(ctx, &row)
	if err == sql.ErrNoRows {
		return StoragePartition{}, panelerr.NotFound("storage_share_partition")
	}
	if err != nil {
		return StoragePartition{}, err
	}
	row.CreatedAt = parseTime(createdAt)
	row.UpdatedAt = parseTime(updatedAt)
	return row, nil
}

func (s *Service) listStoragePartitions(ctx context.Context) ([]StoragePartition, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,application_id,application_name,server_id,server_name,storage_server_id,storage_server_name,path,target,volume_name,created_at,updated_at FROM storage_share_partitions ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StoragePartition{}
	for rows.Next() {
		var row StoragePartition
		var createdAt, updatedAt string
		if err := rows.Scan(&row.ID, &row.ApplicationID, &row.ApplicationName, &row.ServerID, &row.ServerName, &row.StorageServerID, &row.StorageServerName, &row.Path, &row.Target, &row.VolumeName, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		row.CreatedAt = parseTime(createdAt)
		row.UpdatedAt = parseTime(updatedAt)
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Service) upsertStoragePartition(ctx context.Context, app applications.Application, srv server.Server, storageServer server.Server, root, partitionPath, target, volumeName string) error {
	now := time.Now().UTC()
	targetHash := sha256.Sum256([]byte(strings.TrimSpace(target)))
	id := storageServer.ID + "-" + app.ID + "-" + srv.ID + "-" + hex.EncodeToString(targetHash[:6])
	_, err := orm.RawExec(ctx, s.db, `INSERT INTO storage_share_partitions(id,application_id,application_name,server_id,server_name,storage_server_id,storage_server_name,path,target,volume_name,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET application_name=excluded.application_name,server_name=excluded.server_name,storage_server_id=excluded.storage_server_id,storage_server_name=excluded.storage_server_name,path=excluded.path,target=excluded.target,volume_name=excluded.volume_name,updated_at=excluded.updated_at`,
		id, app.ID, app.Name, srv.ID, srv.Name, storageServer.ID, storageServer.Name, partitionPath, target, volumeName, formatTime(now), formatTime(now))
	return err
}

func (s *Service) storageServerForPartition(ctx context.Context, partition StoragePartition) (server.Server, error) {
	serverID := strings.TrimSpace(partition.StorageServerID)
	if serverID == "" {
		return server.Server{}, panelerr.Validation("storage_share_server_unavailable", "storage server is not known for this partition; re-save the facility configuration or re-deploy the application")
	}
	return s.servers.Get(ctx, serverID)
}

func (s *Service) reconcileStorageServer(ctx context.Context, setting StorageServerSetting) error {
	// 安装 nfs-kernel-server 走 SSH；其余导出配置由 Agent 完成。
	if err := s.installNFSServer(ctx, setting.ServerID); err != nil {
		return err
	}
	agent, ok := s.storageAgent()
	if !ok {
		return panelerr.Validation("storage_share_agent_required", "storage share requires an agent with storage support; upgrade the agent on the storage server")
	}
	srv, err := s.servers.Get(ctx, setting.ServerID)
	if err != nil {
		return err
	}
	baseURL, ok := storageAgentEndpoint(srv)
	if !ok {
		return panelerr.Validation("agent_required", "Agent is required for storage share operations")
	}
	hosts, err := s.storageAllowedHosts(ctx)
	if err != nil {
		return err
	}
	if err := agent.StorageConfigureExport(ctx, baseURL, setting.Root, hosts, true); err != nil {
		return fmt.Errorf("configure storage export on %s: %w", setting.ServerID, storageAgentError(err))
	}
	return nil
}

// installNFSServer 通过 SSH 在存储服务器上安装 nfs-kernel-server（唯一允许走
// SSH 的存储操作）。
func (s *Service) installNFSServer(ctx context.Context, serverID string) error {
	if s.ssh == nil {
		return panelerr.Validation("storage_share_ssh_unavailable", "SSH executor is unavailable for installing NFS on the storage server")
	}
	srv, err := s.servers.Get(ctx, serverID)
	if err != nil {
		return err
	}
	command := "dpkg -s nfs-kernel-server >/dev/null 2>&1 || (apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends nfs-kernel-server)"
	if _, err := s.ssh.ExecSudo(ctx, server.Target(srv), sshx.CommandSpec{Command: command, Timeout: 20 * time.Minute}); err != nil {
		return fmt.Errorf("install nfs-kernel-server on %s: %w", serverID, err)
	}
	return nil
}

func (s *Service) removeStorageExport(ctx context.Context, setting StorageServerSetting) error {
	agent, ok := s.storageAgent()
	if !ok {
		return panelerr.Validation("storage_share_agent_required", "storage share requires an agent with storage support; upgrade the agent on the storage server")
	}
	srv, err := s.servers.Get(ctx, setting.ServerID)
	if err != nil {
		return err
	}
	baseURL, ok := storageAgentEndpoint(srv)
	if !ok {
		return panelerr.Validation("agent_required", "Agent is required for storage share operations")
	}
	return storageAgentError(agent.StorageConfigureExport(ctx, baseURL, setting.Root, nil, false))
}

// storageAllowedHosts 返回允许挂载的服务器主机白名单（Panel 已纳管服务器）。
func (s *Service) storageAllowedHosts(ctx context.Context) ([]string, error) {
	servers, err := s.servers.List(ctx)
	if err != nil {
		return nil, err
	}
	out := []string{}
	seen := map[string]struct{}{}
	for _, srv := range servers {
		host := strings.TrimSpace(srv.Host)
		if host == "" {
			continue
		}
		spec := storageHostSpec(host)
		if spec == "" {
			continue
		}
		if _, ok := seen[spec]; ok {
			continue
		}
		seen[spec] = struct{}{}
		out = append(out, spec)
	}
	sort.Strings(out)
	return out, nil
}

func storageHostSpec(host string) string {
	if ip := net.ParseIP(host); ip != nil {
		return host
	}
	if storageHostPattern.MatchString(host) {
		return host
	}
	return ""
}

func storagePartitionPath(root, storageServerID, nodeServerID, appID string) string {
	return root + "/" + storageServerID + "/" + nodeServerID + "/" + appID
}

func storageNFSSource(host, partitionPath string) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return host + ":" + partitionPath
}

func storagePartitionDownloadName(partition StoragePartition) string {
	appName := sanitizeStorageDownloadName(partition.ApplicationName)
	serverName := sanitizeStorageDownloadName(partition.ServerName)
	if appName == "" {
		appName = "application"
	}
	if serverName == "" {
		serverName = "server"
	}
	return appName + "-" + serverName + "-storage.tgz"
}

func sanitizeStorageDownloadName(value string) string {
	value = strings.TrimSpace(value)
	var builder strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			builder.WriteRune(r)
		} else {
			builder.WriteRune('-')
		}
	}
	return strings.Trim(builder.String(), "-")
}

// deniedStorageRootPrefixes 与 agent 侧 internal/agent/storage 的
// deniedRootPrefixes 保持一致：这些系统目录不允许作为存储共享根目录，
// 否则配置在保存后会在 agent 导出阶段失败，长期处于"已启用但导出失败"状态。
var deniedStorageRootPrefixes = []string{
	"/etc", "/var", "/usr", "/bin", "/sbin", "/lib", "/boot",
	"/dev", "/proc", "/sys", "/run", "/home", "/root", "/tmp",
}

func validStorageRoot(root string) bool {
	if !storageRootPattern.MatchString(root) {
		return false
	}
	if root == "/" {
		return false
	}
	for _, prefix := range deniedStorageRootPrefixes {
		if root == prefix || strings.HasPrefix(root, prefix+"/") {
			return false
		}
	}
	return !hasUnsafePathSegment(root)
}

func validStoragePath(value string) bool {
	if !storageRootPattern.MatchString(value) {
		return false
	}
	return !hasUnsafePathSegment(value)
}

func hasUnsafePathSegment(value string) bool {
	parts := strings.Split(value, "/")
	for index, segment := range parts {
		if segment == ".." || segment == "." {
			return true
		}
		if segment == "" && index != 0 {
			return true
		}
	}
	return false
}

// StorageServerStatus 是一台存储服务器的 NFS 导出生效状态。
type StorageServerStatus struct {
	ServerID        string `json:"serverId"`
	Root            string `json:"root"`
	AgentOnline     bool   `json:"agentOnline"`
	ServerInstalled bool   `json:"serverInstalled"`
	RootExists      bool   `json:"rootExists"`
	ExportLive      bool   `json:"exportLive"`
	Detail          string `json:"detail,omitempty"`
	LastError       string `json:"lastError,omitempty"`
}

// StoragePartitionStatus 是一个分区的挂载生效状态。
type StoragePartitionStatus struct {
	StoragePartition
	VolumeExists bool   `json:"volumeExists"`
	Mounted      bool   `json:"mounted"`
	Writable     bool   `json:"writable"`
	MountDetail  string `json:"mountDetail,omitempty"`
}

// StorageShareStatus 是存储共享设施的整体生效状态：每台存储服务器的导出状态
// 与每个分区的挂载状态。单项检查失败只记录 detail，不中断整体查询。
type StorageShareStatus struct {
	Servers    []StorageServerStatus    `json:"servers"`
	Partitions []StoragePartitionStatus `json:"partitions"`
}

// StorageShareStatus 汇总各存储服务器 NFS 导出状态与各分区挂载状态，
// 供设施页展示「NFS 是否连上、是否生效」。服务器与分区检查并行执行，
// 并受整体超时保护。
func (s *Service) StorageShareStatus(ctx context.Context) (StorageShareStatus, error) {
	cfg, err := s.loadStorageConfig(ctx)
	if err != nil {
		return StorageShareStatus{}, err
	}
	statusCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out := StorageShareStatus{Servers: []StorageServerStatus{}, Partitions: []StoragePartitionStatus{}}
	var mu sync.Mutex
	addServer := func(item StorageServerStatus) {
		mu.Lock()
		out.Servers = append(out.Servers, item)
		mu.Unlock()
	}
	addPartition := func(item StoragePartitionStatus) {
		mu.Lock()
		out.Partitions = append(out.Partitions, item)
		mu.Unlock()
	}
	var wait sync.WaitGroup
	for _, setting := range cfg.Servers {
		wait.Add(1)
		go func(setting StorageServerSetting) {
			defer wait.Done()
			item := StorageServerStatus{ServerID: setting.ServerID, Root: setting.Root}
			agent, ok := s.storageAgent()
			if !ok {
				item.Detail = "agent does not support storage; upgrade the agent"
				addServer(item)
				return
			}
			srv, getErr := s.servers.Get(statusCtx, setting.ServerID)
			if getErr != nil {
				item.Detail = getErr.Error()
				addServer(item)
				return
			}
			baseURL, endpointOK := storageAgentEndpoint(srv)
			if !endpointOK {
				item.Detail = "agent endpoint unavailable"
				addServer(item)
				return
			}
			if status, statusErr := agent.StorageStatus(statusCtx, baseURL, setting.Root); statusErr == nil {
				item.AgentOnline = true
				item.ServerInstalled = status.ServerInstalled
				item.RootExists = status.RootExists
				item.ExportLive = status.ExportLive
				item.Detail = status.Detail
			} else {
				item.AgentOnline = false
				item.Detail = storageAgentError(statusErr).Error()
			}
			addServer(item)
		}(setting)
	}
	partitions, err := s.listStoragePartitions(statusCtx)
	if err != nil {
		return StorageShareStatus{}, err
	}
	for _, partition := range partitions {
		wait.Add(1)
		go func(partition StoragePartition) {
			defer wait.Done()
			item := StoragePartitionStatus{StoragePartition: partition}
			if strings.TrimSpace(partition.VolumeName) == "" || strings.TrimSpace(partition.Target) == "" {
				item.MountDetail = "volume metadata missing"
				addPartition(item)
				return
			}
			storageServer, serverErr := s.storageServerForPartition(statusCtx, partition)
			if serverErr != nil {
				item.MountDetail = serverErr.Error()
				addPartition(item)
				return
			}
			nodeServer, nodeErr := s.servers.Get(statusCtx, partition.ServerID)
			if nodeErr != nil {
				item.MountDetail = "app node server is not known"
				addPartition(item)
				return
			}
			agent, ok := s.storageAgent()
			if !ok {
				item.MountDetail = "agent does not support storage; upgrade the agent"
				addPartition(item)
				return
			}
			baseURL, endpointOK := storageAgentEndpoint(nodeServer)
			if !endpointOK {
				item.MountDetail = "app node agent is unavailable"
				addPartition(item)
				return
			}
			source := storageNFSSource(storageServer.Host, partition.Path)
			if status, statusErr := agent.StorageMountStatus(statusCtx, baseURL, source, partition.Target); statusErr == nil {
				item.VolumeExists = status.VolumeExists
				item.Mounted = status.Mounted
				item.Writable = status.Writable
				item.MountDetail = status.Detail
			} else {
				item.MountDetail = storageAgentError(statusErr).Error()
			}
			addPartition(item)
		}(partition)
	}
	wait.Wait()
	sort.Slice(out.Servers, func(i, j int) bool { return out.Servers[i].ServerID < out.Servers[j].ServerID })
	sort.Slice(out.Partitions, func(i, j int) bool { return out.Partitions[i].UpdatedAt.After(out.Partitions[j].UpdatedAt) })
	return out, nil
}

// ensureStorageDirectory 通过存储服务器 Agent 幂等创建分区目录。
func (s *Service) ensureStorageDirectory(ctx context.Context, storageServer server.Server, partitionPath string) error {
	agent, ok := s.storageAgent()
	if !ok {
		return panelerr.Validation("storage_share_agent_required", "storage share requires an agent with storage support; upgrade the agent on the storage server")
	}
	baseURL, ok := storageAgentEndpoint(storageServer)
	if !ok {
		return panelerr.Validation("agent_required", "Agent is required for storage share operations")
	}
	if err := agent.StorageEnsureDirectory(ctx, baseURL, partitionPath); err != nil {
		return fmt.Errorf("ensure storage directory on %s: %w", storageServer.ID, storageAgentError(err))
	}
	return nil
}

// storagePartitionMountActive 检查分区是否仍被运行中的容器挂载。
func (s *Service) storagePartitionMountActive(ctx context.Context, partition StoragePartition) (bool, string, error) {
	if strings.TrimSpace(partition.VolumeName) == "" || strings.TrimSpace(partition.Target) == "" {
		return false, "", nil
	}
	storageServer, err := s.storageServerForPartition(ctx, partition)
	if err != nil {
		return false, "", err
	}
	nodeServer, err := s.servers.Get(ctx, partition.ServerID)
	if err != nil {
		return false, "", err
	}
	agent, ok := s.storageAgent()
	if !ok {
		return false, "", nil
	}
	baseURL, ok := storageAgentEndpoint(nodeServer)
	if !ok {
		return false, "", nil
	}
	source := storageNFSSource(storageServer.Host, partition.Path)
	status, err := agent.StorageMountStatus(ctx, baseURL, source, partition.Target)
	if err != nil {
		return false, "", storageAgentError(err)
	}
	return status.Mounted, status.Detail, nil
}

// storageReconcileLoop 周期同步存储导出，确保白名单随服务器增删自动刷新。
func (s *Service) storageReconcileLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		cfg, err := s.loadStorageConfig(ctx)
		if err == nil && cfg.Enabled {
			if err := s.reconcileStorageServers(ctx, cfg); err != nil {
				_ = s.setStorageLastError(ctx, err.Error())
			}
		}
		cancel()
	}
}

// partitionReferenceNames 返回仍引用指定存储服务器的应用名。
func (s *Service) partitionReferenceNames(ctx context.Context, storageServerID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT application_name FROM storage_share_partitions WHERE storage_server_id=?`, storageServerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// validStorageID 校验服务器/应用 ID：允许字母数字、点、下划线、连字符，
// 但必须是合法单段，不允许 "." 或 ".." 造成路径穿越。
func validStorageID(value string) bool {
	if !storageIDPattern.MatchString(value) || value == "." || value == ".." {
		return false
	}
	return true
}
