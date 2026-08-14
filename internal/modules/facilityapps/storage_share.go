package facilityapps

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"panel/internal/modules/applications"
	appruntime "panel/internal/modules/applications/runtime"
	server "panel/internal/modules/servers"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	sshx "panel/internal/platform/ssh"
)

const (
	// StorageShareID 是存储共享设施的唯一配置 ID。
	StorageShareID = "storage-share"

	storageExportsMarker = "# panel-storage-share:managed"
	storageExportsPath   = "/etc/exports"
)

var (
	storageRootPattern = regexp.MustCompile(`^/[A-Za-z0-9._/-]+$`)
	storageIDPattern   = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	storageHostPattern = regexp.MustCompile(`^[A-Za-z0-9.-]+$`)
)

// StorageShareConfig 是存储共享设施的对外配置与状态。
type StorageShareConfig struct {
	ID         string                            `json:"id"`
	Version    int                               `json:"version"`
	ServerID   string                            `json:"serverId"`
	ServerName string                            `json:"serverName,omitempty"`
	Root       string                            `json:"root"`
	Enabled    bool                              `json:"enabled"`
	Servers    []string                          `json:"servers"`
	Partitions []StoragePartition                `json:"partitions"`
	References []applications.StorageShareUsage  `json:"references,omitempty"`
	LastError  string                            `json:"lastError,omitempty"`
	UpdatedAt  time.Time                         `json:"updatedAt"`
}

// StorageShareSaveInput 是存储共享设施的保存输入。
type StorageShareSaveInput struct {
	ServerID string `json:"serverId"`
	Root     string `json:"root"`
}

// StoragePartition 是按（应用 × 应用节点）分配的分区记录。
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

type storageConfigRow struct {
	Version   int
	ServerID  string
	Root      string
	LastError string
	UpdatedAt string
}

func (s *Service) loadStorageConfig(ctx context.Context) (StorageShareConfig, error) {
	cfg := StorageShareConfig{ID: StorageShareID}
	var row storageConfigRow
	err := orm.New(s.db).From("storage_share_configs").Select("version", "server_id", "root", "last_error", "updated_at").Where("id=?", StorageShareID).First(ctx, &row)
	if err == sql.ErrNoRows {
		return cfg, nil
	}
	if err != nil {
		return StorageShareConfig{}, err
	}
	cfg.Version = row.Version
	cfg.ServerID = strings.TrimSpace(row.ServerID)
	cfg.Root = strings.TrimSpace(row.Root)
	cfg.LastError = row.LastError
	cfg.UpdatedAt = parseTime(row.UpdatedAt)
	cfg.Enabled = cfg.ServerID != "" && cfg.Root != ""
	return cfg, nil
}

func (s *Service) saveStorageConfig(ctx context.Context, cfg StorageShareConfig) error {
	_, err := orm.RawExec(ctx, s.db, `INSERT INTO storage_share_configs(id,version,server_id,root,last_error,updated_at)
VALUES(?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET version=excluded.version,server_id=excluded.server_id,root=excluded.root,last_error=excluded.last_error,updated_at=excluded.updated_at`,
		StorageShareID, cfg.Version, cfg.ServerID, cfg.Root, cfg.LastError, formatTime(cfg.UpdatedAt))
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
	if cfg.Enabled {
		if srv, getErr := s.servers.Get(ctx, cfg.ServerID); getErr == nil {
			cfg.ServerName = srv.Name
		}
	}
	partitions, err := s.listStoragePartitions(ctx)
	if err != nil {
		return StorageShareConfig{}, err
	}
	cfg.Partitions = partitions
	if servers, listErr := s.servers.List(ctx); listErr == nil {
		for _, srv := range servers {
			cfg.Servers = append(cfg.Servers, srv.ID)
		}
		sort.Strings(cfg.Servers)
	}
	if provider, ok := s.apps.(applications.StorageShareUsageProvider); ok {
		if usages, usageErr := provider.ApplicationsUsingStorageShare(ctx); usageErr == nil {
			cfg.References = usages
		}
	}
	return cfg, nil
}

// SaveStorageShare 保存设施配置并立即尝试在存储服务器上导出 NFS。
func (s *Service) SaveStorageShare(ctx context.Context, in StorageShareSaveInput) (StorageShareConfig, error) {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	serverID := strings.TrimSpace(in.ServerID)
	root := strings.TrimSpace(in.Root)
	if serverID == "" || root == "" {
		return StorageShareConfig{}, panelerr.Validation("storage_share_config_required", "storage server and root are required")
	}
	if !validStorageRoot(root) {
		return StorageShareConfig{}, panelerr.Validation("storage_share_root_invalid", "storage root must be an absolute Linux path without spaces")
	}
	srv, err := s.servers.Get(ctx, serverID)
	if err != nil {
		return StorageShareConfig{}, err
	}
	cfg, err := s.loadStorageConfig(ctx)
	if err != nil {
		return StorageShareConfig{}, err
	}
	cfg.ServerID = serverID
	cfg.ServerName = srv.Name
	cfg.Root = root
	cfg.Version++
	cfg.LastError = ""
	cfg.UpdatedAt = time.Now().UTC()
	if err := s.saveStorageConfig(ctx, cfg); err != nil {
		return StorageShareConfig{}, err
	}
	if err := s.reconcileStorageServer(ctx, cfg); err != nil {
		_ = s.setStorageLastError(ctx, err.Error())
	}
	return s.GetStorageShare(ctx)
}

// DeleteStorageShare 卸载设施：仅当没有应用再引用时才允许；停止导出但保留
// 分区历史与数据，用户仍可在设施页下载或删除分区。
func (s *Service) DeleteStorageShare(ctx context.Context) error {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	cfg, err := s.loadStorageConfig(ctx)
	if err != nil {
		return err
	}
	if provider, ok := s.apps.(applications.StorageShareUsageProvider); ok {
		usages, usageErr := provider.ApplicationsUsingStorageShare(ctx)
		if usageErr != nil {
			return usageErr
		}
		if len(usages) > 0 {
			names := make([]string, 0, len(usages))
			for _, usage := range usages {
				names = append(names, usage.ApplicationName)
			}
			return panelerr.Conflict("storage_share_in_use", "Storage share is still used by applications: "+strings.Join(names, ", "))
		}
	}
	if cfg.Enabled {
		if err := s.removeStorageExports(ctx, cfg); err != nil {
			return err
		}
	}
	return orm.New(s.db).From("storage_share_configs").Where("id=?", StorageShareID).Delete(ctx)
}

// ReconcileStorageShareNow 手动触发一次存储服务器导出同步。
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
	if err := s.reconcileStorageServer(ctx, cfg); err != nil {
		_ = s.setStorageLastError(ctx, err.Error())
		return StorageShareConfig{}, err
	}
	_ = s.setStorageLastError(ctx, "")
	return s.GetStorageShare(ctx)
}

// ResolveStorageShareMounts 把应用运行时规格里的 storage_share 挂载解析为
// NFS 挂载，并登记按（应用 × 应用节点）分配的分区记录。
func (s *Service) ResolveStorageShareMounts(ctx context.Context, app applications.Application, srv server.Server, mounts []appruntime.Mount) ([]appruntime.Mount, error) {
	if len(mounts) == 0 {
		return mounts, nil
	}
	cfg, err := s.loadStorageConfig(ctx)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, panelerr.Validation("storage_share_unavailable", "Storage share facility is not configured")
	}
	storageServer, err := s.servers.Get(ctx, cfg.ServerID)
	if err != nil {
		return nil, err
	}
	out := make([]appruntime.Mount, 0, len(mounts))
	for _, mount := range mounts {
		if strings.TrimSpace(mount.Type) != "storage_share" {
			out = append(out, mount)
			continue
		}
		source := strings.TrimSpace(mount.Source)
		if source != "" && source != StorageShareID {
			return nil, panelerr.Validation("storage_share_source_invalid", "storage share mount source must reference the storage share facility")
		}
		if !storageIDPattern.MatchString(app.ID) || !storageIDPattern.MatchString(srv.ID) {
			return nil, panelerr.Validation("storage_share_partition_invalid", "storage share partition identifiers are invalid")
		}
		partitionPath := storagePartitionPath(cfg.Root, srv.ID, app.ID)
		out = append(out, appruntime.Mount{
			Type:     "nfs",
			Source:   storageNFSSource(storageServer.Host, partitionPath),
			Target:   mount.Target,
			ReadOnly: mount.ReadOnly,
		})
		if err := s.upsertStoragePartition(ctx, app, srv, cfg, partitionPath); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// DownloadStoragePartition 通过存储服务器 SSH 打包分区目录并返回内容。
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
	parent := path.Dir(partition.Path)
	base := path.Base(partition.Path)
	command := "tar -czf - -C " + parent + " " + base
	result, err := s.ssh.ExecSudo(ctx, server.Target(storageServer), sshx.CommandSpec{Command: command, Timeout: 10 * time.Minute})
	if err != nil {
		return StoragePartitionDownload{}, err
	}
	if strings.TrimSpace(result.Stdout) == "" {
		return StoragePartitionDownload{}, panelerr.NotFound("storage_share_partition_data")
	}
	return StoragePartitionDownload{
		Filename: storagePartitionDownloadName(partition),
		Content:  []byte(result.Stdout),
	}, nil
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
	if _, err := s.ssh.ExecSudo(ctx, server.Target(storageServer), sshx.CommandSpec{Command: "rm -rf -- " + partition.Path}); err != nil {
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

func (s *Service) upsertStoragePartition(ctx context.Context, app applications.Application, srv server.Server, cfg StorageShareConfig, partitionPath string) error {
	now := time.Now().UTC()
	_, err := orm.RawExec(ctx, s.db, `INSERT INTO storage_share_partitions(id,application_id,application_name,server_id,server_name,storage_server_id,storage_server_name,path,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET application_name=excluded.application_name,server_name=excluded.server_name,storage_server_id=excluded.storage_server_id,storage_server_name=excluded.storage_server_name,path=excluded.path,updated_at=excluded.updated_at`,
		app.ID+"-"+srv.ID, app.ID, app.Name, srv.ID, srv.Name, cfg.ServerID, cfg.ServerName, partitionPath, formatTime(now), formatTime(now))
	return err
}

func (s *Service) storageServerForPartition(ctx context.Context, partition StoragePartition) (server.Server, error) {
	serverID := strings.TrimSpace(partition.StorageServerID)
	if serverID == "" {
		cfg, err := s.loadStorageConfig(ctx)
		if err != nil {
			return server.Server{}, err
		}
		serverID = cfg.ServerID
	}
	if serverID == "" {
		return server.Server{}, panelerr.Validation("storage_share_server_unavailable", "storage server is not known for this partition")
	}
	return s.servers.Get(ctx, serverID)
}

func (s *Service) reconcileStorageServer(ctx context.Context, cfg StorageShareConfig) error {
	if s.ssh == nil {
		return panelerr.Validation("storage_share_ssh_unavailable", "SSH executor is unavailable for storage share")
	}
	srv, err := s.servers.Get(ctx, cfg.ServerID)
	if err != nil {
		return err
	}
	target := server.Target(srv)
	if _, err := s.ssh.ExecSudo(ctx, target, sshx.CommandSpec{Command: "mkdir -p " + cfg.Root}); err != nil {
		return fmt.Errorf("create storage root: %w", err)
	}
	installCommand := "dpkg -s nfs-kernel-server >/dev/null 2>&1 || (apt-get update -qq && apt-get install -y --no-install-recommends nfs-kernel-server)"
	if _, err := s.ssh.ExecSudo(ctx, target, sshx.CommandSpec{Command: installCommand, Timeout: 10 * time.Minute}); err != nil {
		return fmt.Errorf("install nfs server: %w", err)
	}
	hosts, err := s.storageAllowedHosts(ctx)
	if err != nil {
		return err
	}
	next := storageExportsBlock(cfg.Root, hosts)
	current := ""
	if result, readErr := s.ssh.ExecSudo(ctx, target, sshx.CommandSpec{Command: "cat " + storageExportsPath}); readErr == nil {
		current = result.Stdout
	}
	next = stripStorageExports(current, cfg.Root) + next
	return s.writeStorageExports(ctx, target, next)
}

func (s *Service) removeStorageExports(ctx context.Context, cfg StorageShareConfig) error {
	if s.ssh == nil {
		return panelerr.Validation("storage_share_ssh_unavailable", "SSH executor is unavailable for storage share")
	}
	srv, err := s.servers.Get(ctx, cfg.ServerID)
	if err != nil {
		return err
	}
	target := server.Target(srv)
	current := ""
	if result, readErr := s.ssh.ExecSudo(ctx, target, sshx.CommandSpec{Command: "cat " + storageExportsPath}); readErr == nil {
		current = result.Stdout
	}
	return s.writeStorageExports(ctx, target, strings.TrimRight(stripStorageExports(current, cfg.Root), "\n"))
}

func (s *Service) writeStorageExports(ctx context.Context, target sshx.Target, content string) error {
	local := filepath.Join(s.dataRoot, "tmp", fmt.Sprintf("storage-exports-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		return err
	}
	defer os.Remove(local)
	if err := os.WriteFile(local, []byte(content), 0o600); err != nil {
		return err
	}
	remote := fmt.Sprintf("/tmp/panel-storage-exports-%d", time.Now().UnixNano())
	if err := s.ssh.Upload(ctx, target, sshx.UploadSpec{LocalPath: local, RemotePath: remote}); err != nil {
		return err
	}
	defer func() {
		_, _ = s.ssh.ExecSudo(ctx, target, sshx.CommandSpec{Command: "rm -f " + remote})
	}()
	if _, err := s.ssh.ExecSudo(ctx, target, sshx.CommandSpec{Command: "mv -f " + remote + " " + storageExportsPath + " && exportfs -ra"}); err != nil {
		return fmt.Errorf("apply nfs exports: %w", err)
	}
	return nil
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

func storageExportsBlock(root string, hosts []string) string {
	var builder strings.Builder
	builder.WriteString(storageExportsMarker)
	builder.WriteString("\n")
	builder.WriteString(root)
	for _, host := range hosts {
		builder.WriteString(" " + host + "(rw,sync,no_subtree_check,no_root_squash,insecure)")
	}
	builder.WriteString("\n")
	return builder.String()
}

func stripStorageExports(content, root string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	skip := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == storageExportsMarker {
			skip = true
			continue
		}
		if skip {
			// 我们的托管块是“标记 + 一条导出行”；跳过标记后的第一条非空行，
			// 空行直接忽略，随后恢复正常解析。
			if trimmed == "" {
				continue
			}
			skip = false
			continue
		}
		if strings.HasPrefix(trimmed, root+" ") {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n")
}

func storagePartitionPath(root, serverID, appID string) string {
	return root + "/" + serverID + "/" + appID
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