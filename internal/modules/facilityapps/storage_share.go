package facilityapps

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/applications"
	appruntime "panel/internal/modules/applications/runtime"
	server "panel/internal/modules/servers"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/logging"

	"go.uber.org/zap"
)

const (
	// StorageShareID 是存储共享设施的唯一配置 ID。
	StorageShareID = "storage-share"
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
	cfg, err := s.loadStorageConfig(ctx)
	if err != nil {
		return StorageShareConfig{}, err
	}
	cfg.Servers = servers
	cfg.Version++
	cfg.LastError = ""
	cfg.UpdatedAt = time.Now().UTC()
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
	if err := orm.New(s.db).From("storage_share_configs").Where("id=?", StorageShareID).Delete(ctx); err != nil {
		return StorageShareConfig{}, err
	}
	result, err := s.GetStorageShare(ctx)
	if err != nil {
		return StorageShareConfig{}, err
	}
	if cleanupErr != nil {
		result.LastError = "Remote export cleanup failed: " + cleanupErr.Error()
	}
	return result, nil
}

// ReconcileStorageShareNow 手动触发一次全部存储服务器的导出同步。
func (s *Service) ReconcileStorageShareNow(ctx context.Context) (StorageShareConfig, error) {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	cfg, err := s.loadStorageConfig(ctx)
	if err != nil {
		return StorageShareConfig{}, err
	}
	if !cfg.Enabled {
		return StorageShareConfig{}, panelerr.Validation("storage_share_not_configured", "Storage share is not configured")
	}
	if err := s.reconcileStorageServers(ctx, cfg); err != nil {
		_ = s.setStorageLastError(ctx, err.Error())
		return StorageShareConfig{}, err
	}
	_ = s.setStorageLastError(ctx, "")
	return s.GetStorageShare(ctx)
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
	if !storageIDPattern.MatchString(app.ID) || !storageIDPattern.MatchString(srv.ID) {
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
		out = append(out, appruntime.Mount{
			Type:     "nfs",
			Source:   storageNFSSource(storageServer.Host, partitionPath),
			Target:   mount.Target,
			ReadOnly: mount.ReadOnly,
		})
		if err := s.upsertStoragePartition(ctx, app, srv, storageServer, setting.Root, partitionPath); err != nil {
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
		return StoragePartitionDownload{}, err
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
		return err
	}
	return orm.New(s.db).From("storage_share_partitions").Where("id=?", partition.ID).Delete(ctx)
}

func (s *Service) getStoragePartition(ctx context.Context, partitionID string) (StoragePartition, error) {
	var row StoragePartition
	var createdAt, updatedAt string
	err := orm.New(s.db).From("storage_share_partitions").
		Select("id", "application_id", "application_name", "server_id", "server_name", "storage_server_id", "storage_server_name", "path", "created_at", "updated_at").
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
	rows, err := s.db.QueryContext(ctx, `SELECT id,application_id,application_name,server_id,server_name,storage_server_id,storage_server_name,path,created_at,updated_at FROM storage_share_partitions ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StoragePartition{}
	for rows.Next() {
		var row StoragePartition
		var createdAt, updatedAt string
		if err := rows.Scan(&row.ID, &row.ApplicationID, &row.ApplicationName, &row.ServerID, &row.ServerName, &row.StorageServerID, &row.StorageServerName, &row.Path, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		row.CreatedAt = parseTime(createdAt)
		row.UpdatedAt = parseTime(updatedAt)
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Service) upsertStoragePartition(ctx context.Context, app applications.Application, srv server.Server, storageServer server.Server, root, partitionPath string) error {
	now := time.Now().UTC()
	id := storageServer.ID + "-" + app.ID + "-" + srv.ID
	_, err := orm.RawExec(ctx, s.db, `INSERT INTO storage_share_partitions(id,application_id,application_name,server_id,server_name,storage_server_id,storage_server_name,path,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET application_name=excluded.application_name,server_name=excluded.server_name,storage_server_id=excluded.storage_server_id,storage_server_name=excluded.storage_server_name,path=excluded.path,updated_at=excluded.updated_at`,
		id, app.ID, app.Name, srv.ID, srv.Name, storageServer.ID, storageServer.Name, partitionPath, formatTime(now), formatTime(now))
	return err
}

func (s *Service) storageServerForPartition(ctx context.Context, partition StoragePartition) (server.Server, error) {
	serverID := strings.TrimSpace(partition.StorageServerID)
	if serverID == "" {
		cfg, err := s.loadStorageConfig(ctx)
		if err != nil {
			return server.Server{}, err
		}
		if len(cfg.Servers) > 0 {
			serverID = cfg.Servers[0].ServerID
		}
	}
	if serverID == "" {
		return server.Server{}, panelerr.Validation("storage_share_server_unavailable", "storage server is not known for this partition")
	}
	return s.servers.Get(ctx, serverID)
}

func (s *Service) reconcileStorageServer(ctx context.Context, setting StorageServerSetting) error {
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
		return fmt.Errorf("configure storage export on %s: %w", setting.ServerID, err)
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
	return agent.StorageConfigureExport(ctx, baseURL, setting.Root, nil, false)
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

func validStorageRoot(root string) bool {
	if !storageRootPattern.MatchString(root) {
		return false
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
