package applications

import (
	"context"

	"panel/internal/modules/applications/runtime"
	"panel/internal/modules/servers"
	"panel/internal/modules/applications/spec"
	"strings"
)

// StorageShareResolver 解析应用运行时规格中的 storage_share 挂载为具体 NFS
// 挂载，并由设施模块登记按（应用 × 服务器）分配的分区记录。
type StorageShareResolver interface {
	ResolveStorageShareMounts(ctx context.Context, app Application, srv server.Server, mounts []appruntime.Mount) ([]appruntime.Mount, error)
}

// StorageShareUsage 描述仍引用存储共享设施的应用。
type StorageShareUsage struct {
	ApplicationID   string `json:"applicationId"`
	ApplicationName string `json:"applicationName"`
}

// StorageShareUsageProvider 供设施模块查询仍引用存储共享的应用。
type StorageShareUsageProvider interface {
	ApplicationsUsingStorageShare(ctx context.Context) ([]StorageShareUsage, error)
}

func (s *Service) SetStorageShareResolver(provider StorageShareResolver) {
	s.storageResolver = provider
}

// ApplicationsUsingStorageShare 扫描普通应用的 spec，返回仍使用 storage_share
// 挂载类型的应用清单，用于存储共享设施的卸载门禁与分区删除校验。
func (s *Service) ApplicationsUsingStorageShare(ctx context.Context) ([]StorageShareUsage, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,spec_yaml FROM applications WHERE kind <> 'facility'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StorageShareUsage{}
	for rows.Next() {
		var id, name, raw string
		if err := rows.Scan(&id, &name, &raw); err != nil {
			return nil, err
		}
		specValue, issues := appspec.DecodeYAML(raw)
		if len(issues) > 0 {
			continue
		}
		for _, mount := range specValue.Mounts {
			if strings.TrimSpace(mount.Type) == "storage_share" {
				out = append(out, StorageShareUsage{ApplicationID: id, ApplicationName: name})
				break
			}
		}
	}
	return out, rows.Err()
}