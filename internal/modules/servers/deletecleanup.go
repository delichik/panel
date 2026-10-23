package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"go.uber.org/zap"

	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
	"panel/internal/platform/logcontexts"
	"panel/internal/platform/logging"
)

// deploymentModeSelected 与 applications 模块的部署模式取值保持一致。servers
// 模块不能直接引用 applications 的常量：applications 已经依赖 servers，引用会
// 形成导入环。其余模式（all）按现存节点部署，节点列表不保存在应用行里。
const deploymentModeSelected = "selected"

// cleanupServerReferences 在删除服务器的同一 AppDB 事务内清理所有本地引用。
//
// 外键级联只覆盖带外键的表。服务器 ID 还以普通列或 JSON 形式保存在多处，
// 不显式清理就会留下指向已不存在节点的悬空引用：
//
//   - jobs.server_id 对 servers 是 RESTRICT 外键，不清理会直接让删除失败；
//   - application_persistent_locations 记录应用可能残留持久数据的节点，悬空后
//     应用删除流程会因为取不到节点而永久失败；
//   - 入口代理的网关、源站和中继节点，以及存储共享的存储节点都内嵌节点 ID，
//     悬空后设施渲染会生成指向已不存在节点的 upstream。
func (s *Service) cleanupServerReferences(ctx context.Context, tx *sql.Tx, serverID string) error {
	if err := cancelAndDeleteServerJobs(ctx, tx, serverID); err != nil {
		return err
	}
	affected, err := s.removeServerFromApplicationTargets(ctx, tx, serverID)
	if err != nil {
		return err
	}
	if err := s.removeServerFromOverviewCards(ctx, tx, serverID); err != nil {
		return err
	}
	if err := removeServerPersistentLocations(ctx, tx, serverID); err != nil {
		return err
	}
	if err := removeServerFromProxyRoutes(ctx, tx, serverID); err != nil {
		return err
	}
	if err := removeServerFromFacilityGateways(ctx, tx, serverID); err != nil {
		return err
	}
	if err := removeServerFromStorageShare(ctx, tx, serverID); err != nil {
		return err
	}
	return disableApplicationsWithoutDeploymentTarget(ctx, tx, serverID, affected)
}

// disableApplicationsWithoutDeploymentTarget 停用因节点删除而失去全部部署目标的应用。
//
// 失去最后一个部署节点的应用如果保持启用，编排器会反复为它规划无法执行的
// 部署，页面也会停留在“需要选择部署节点”的校验错误上。系统必须显式停用它：
// 停用后编排器改为下停止指令，用户重新选定节点并启用后即可恢复。
//
// 判定依据：
//   - selected 模式：修剪后的部署节点列表为空；
//   - all 模式：面板内已无其他节点（该模式按现存节点部署）。
//
// 只处理确实把被删节点当作部署目标的应用：显式列出该节点，或在该节点上仍有
// 实例。实例随节点级联删除，之后应用在该节点上不再有任何运行态。
func disableApplicationsWithoutDeploymentTarget(ctx context.Context, tx *sql.Tx, serverID string, listed []string) error {
	affected := map[string]struct{}{}
	for _, id := range listed {
		affected[id] = struct{}{}
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT application_id FROM application_instances WHERE server_id=?`, serverID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		affected[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(affected) == 0 {
		return nil
	}
	otherServers := 0
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM servers WHERE id<>?`, serverID).Scan(&otherServers); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for id := range affected {
		var mode string
		var selected string
		var enabled int
		var name string
		if err := tx.QueryRowContext(ctx, `SELECT deployment_mode,deployment_server_ids_json,enabled,name FROM applications WHERE id=?`, id).Scan(&mode, &selected, &enabled, &name); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return err
		}
		if enabled == 0 {
			continue
		}
		hasTarget := otherServers > 0
		if strings.TrimSpace(mode) == deploymentModeSelected {
			ids := []string{}
			if err := json.Unmarshal([]byte(selected), &ids); err != nil {
				return err
			}
			hasTarget = len(ids) > 0
		}
		if hasTarget {
			continue
		}
		if _, err := orm.RawExec(ctx, tx, `UPDATE applications SET enabled=0,version=version+1,updated_at=? WHERE id=?`, now, id); err != nil {
			return err
		}
		// 停用是系统代替操作者做出的决定，必须经统一日志留痕。
		logCtx := logging.Ensure(ctx, logging.Context{Module: "servers"})
		msg := "Application disabled because it lost its last deployment node"
		logging.From(logCtx).Warn(msg,
			zap.String("eventType", "application.disabled"),
			zap.String("action", "disable"),
			zap.String("trigger", "server_removed"),
			zap.String("reason", "server_removed"),
			zap.String("serverId", serverID),
			zap.String("applicationId", id),
			zap.String("deploymentMode", mode),
			zap.String("applicationName", name),
		)
		_ = logging.RecordContext(logCtx, logging.ContextMeta{
			Origin:  logcontexts.OriginSystem,
			Title:   "application.disabled",
			Summary: msg,
			ResourcesJSON: logging.MarshalResourcesJSON([]map[string]string{
				{"resourceType": "application", "resourceId": id, "name": name, "role": "target"},
				{"resourceType": "server", "resourceId": serverID, "role": "executor"},
			}),
		})
	}
	return nil
}

// cancelAndDeleteServerJobs 终结并删除引用该服务器的协调 Job。
//
// jobs.server_id 对 servers 使用 RESTRICT 外键，是服务器删除唯一的硬阻塞点：
// 终态 Job 永不被清理，任何曾经承载过部署的节点都无法删除。先把活跃 Job 置为
// cancelled，让活动日志记录一次取消结果并释放冲突域锁，再删除全部 Job 行。
func cancelAndDeleteServerJobs(ctx context.Context, tx *sql.Tx, serverID string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := orm.RawExec(ctx, tx, `UPDATE jobs SET state='cancelled',lease_owner='',lease_token='',lease_expires_at=NULL,next_run_at=NULL,finished_at=?,updated_at=?,error_code='server_removed',error_class='cancelled',error_message='Server was removed',error_detail='' WHERE server_id=? AND state IN ('pending','running','failed_retryable')`,
		now, now, serverID); err != nil {
		return err
	}
	// applications.job_id 指向被删除的 Job 会留下悬空引用。
	if _, err := orm.RawExec(ctx, tx, `UPDATE applications SET job_id='' WHERE job_id IN (SELECT id FROM jobs WHERE server_id=?)`, serverID); err != nil {
		return err
	}
	_, err := orm.RawExec(ctx, tx, `DELETE FROM jobs WHERE server_id=?`, serverID)
	return err
}

// removeServerPersistentLocations 删除该服务器上的应用持久位置记录。
//
// 该表没有指向 servers 的外键，删除服务器后记录仍会保留；应用删除流程把它
// 当作待清理的节点，取不到节点就会让应用永远删不掉。节点已不存在时其上的
// 持久数据本就无法回收，留着记录只会阻塞应用删除。
func removeServerPersistentLocations(ctx context.Context, tx *sql.Tx, serverID string) error {
	_, err := orm.RawExec(ctx, tx, `DELETE FROM application_persistent_locations WHERE server_id=?`, serverID)
	return err
}

// removeServerFromProxyRoutes 修剪入口代理路由的源站与中继节点。
//
// 源站列表被清空后渲染出的 upstream 没有后端，nginx 会拒绝加载配置，因此这类
// 路由必须整条删除，而不是留下空源站。
func removeServerFromProxyRoutes(ctx context.Context, tx *sql.Tx, serverID string) error {
	var rows []models.ReverseProxyRoute
	if err := orm.New(tx).From("reverse_proxy_routes").All(ctx, &rows); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, row := range rows {
		origins, originsChanged := removeString(row.OriginServerIDs, serverID)
		anyAccess, anyAccessChanged := removeServerFromAnyAccess(row.AnyAccessJSON, serverID)
		if !originsChanged && !anyAccessChanged {
			continue
		}
		if len(origins) == 0 {
			if _, err := orm.RawExec(ctx, tx, `DELETE FROM reverse_proxy_routes WHERE domain=?`, row.Domain); err != nil {
				return err
			}
			continue
		}
		originsRaw, err := json.Marshal(origins)
		if err != nil {
			return err
		}
		anyAccessRaw, err := json.Marshal(anyAccess)
		if err != nil {
			return err
		}
		if _, err := orm.RawExec(ctx, tx, `UPDATE reverse_proxy_routes SET origin_server_ids=?,any_access_json=?,updated_at=? WHERE domain=?`, string(originsRaw), string(anyAccessRaw), now, row.Domain); err != nil {
			return err
		}
	}
	return nil
}

// removeServerFromAnyAccess 修剪 AnyAccess 配置里的节点引用。
//
// 中继节点必须是全局网关节点，主源站和优先序也必须指向现存源站，否则入口
// 代理会渲染出指向已不存在节点的 upstream。
func removeServerFromAnyAccess(raw map[string]any, serverID string) (map[string]any, bool) {
	if len(raw) == 0 {
		return raw, false
	}
	changed := false
	if value, ok := raw["relayServerIds"].([]any); ok {
		kept := make([]any, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok && text == serverID {
				changed = true
				continue
			}
			kept = append(kept, item)
		}
		raw["relayServerIds"] = kept
	}
	if text, ok := raw["primaryOriginServerId"].(string); ok && text == serverID {
		delete(raw, "primaryOriginServerId")
		changed = true
	}
	if value, ok := raw["originPriority"].([]any); ok {
		kept := make([]any, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok && text == serverID {
				changed = true
				continue
			}
			kept = append(kept, item)
		}
		raw["originPriority"] = kept
	}
	return raw, changed
}

// removeServerFromFacilityGateways 修剪入口代理设施的全局网关节点。
//
// 网关列表为空时设施没有可用的入口节点，设施伪应用会在删除事务提交后的重新
// 收敛里被停用。
func removeServerFromFacilityGateways(ctx context.Context, tx *sql.Tx, serverID string) error {
	var rows []models.FacilityAppConfig
	if err := orm.New(tx).From("facility_app_configs").Where("deployment_server_ids_json<>?", "").All(ctx, &rows); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, row := range rows {
		next, changed := removeString(row.DeploymentServerIDsJSON, serverID)
		if !changed {
			continue
		}
		encoded, err := json.Marshal(next)
		if err != nil {
			return err
		}
		if _, err := orm.RawExec(ctx, tx, `UPDATE facility_app_configs SET version=version+1,deployment_server_ids_json=?,updated_at=? WHERE id=?`, string(encoded), now, row.ID); err != nil {
			return err
		}
	}
	return nil
}

// removeServerFromStorageShare 修剪存储共享的存储节点并删除相关分区。
//
// 分区记录指向存储节点或应用节点，两侧都可能引用被删除的服务器；节点已不存在
// 时分区既无法挂载也无法卸载，留着只会让存储共享卸载失败。
func removeServerFromStorageShare(ctx context.Context, tx *sql.Tx, serverID string) error {
	var rows []models.StorageShareConfig
	if err := orm.New(tx).From("storage_share_configs").Where("servers_json<>?", "").All(ctx, &rows); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, row := range rows {
		kept := make([]map[string]any, 0, len(row.ServersJSON))
		changed := false
		for _, item := range row.ServersJSON {
			if text, ok := item["serverId"].(string); ok && text == serverID {
				changed = true
				continue
			}
			kept = append(kept, item)
		}
		if !changed {
			continue
		}
		encoded, err := json.Marshal(kept)
		if err != nil {
			return err
		}
		if _, err := orm.RawExec(ctx, tx, `UPDATE storage_share_configs SET version=version+1,servers_json=?,updated_at=? WHERE id=?`, string(encoded), now, row.ID); err != nil {
			return err
		}
	}
	_, err := orm.RawExec(ctx, tx, `DELETE FROM storage_share_partitions WHERE server_id=? OR storage_server_id=?`, serverID, serverID)
	return err
}
