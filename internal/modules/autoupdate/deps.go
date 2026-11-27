package autoupdate

import (
	"context"
	"time"

	"panel/internal/modules/applications"
	"panel/internal/modules/packages"
	"panel/internal/modules/servers"
	"panel/internal/modules/settings"
	"panel/internal/modules/tasks"
)

// 本模块是跨模块协调者：所有能力都通过窄接口注入，不 import 具体实现模块
// （facilityapps/containers 等），由 bootstrap 装配。接口方法签名与各模块现有
// 公开方法一致，因此多数依赖可以直接传服务实例，无需适配器。

// SettingsStore 读热更新设置，并持久化只读运行态（跨越任务历史的 24h 清理）。
type SettingsStore interface {
	Runtime() settings.RuntimeSettings
	RecordAutoUpdateLastRun(context.Context, settings.RuntimeAutoUpdateRunState) error
	// RecordAutoUpdateAutoRun 只记录「调度自动运行」时间，是当日去重的唯一依据。
	// 手动触发不得调用它。
	RecordAutoUpdateAutoRun(context.Context, time.Time) error
}

// ApplicationImageRefresher 触发所有参与自动更新的应用刷新镜像 digest
// （application_image_check 任务），返回创建并派发的任务 ID 列表。
// 应用阶段读取的 ImageUpdateAvailable 只由该检查任务更新，因此刷新阶段必须先
// 请求它，否则自动更新永远筛不到可用更新。
type ApplicationImageRefresher interface {
	RefreshImageUpdates(context.Context) ([]string, error)
}

// ServerLister 提供节点清单（应用/软件包阶段按节点遍历）。
type ServerLister interface {
	List(context.Context) ([]server.Server, error)
}

// ApplicationLister 列出应用，用于筛选参与自动更新的对象
// （Enabled && AutoUpdateEnabled && ImageUpdateAvailable）。
type ApplicationLister interface {
	List(context.Context) ([]applications.Application, error)
}

// ApplicationUpgrader 创建并异步执行应用镜像升级任务；调用方负责过滤对象，
// 只传显式 applicationIDs（all=false）。
type ApplicationUpgrader interface {
	UpgradeApplications(context.Context, []string, bool) (tasks.Task, error)
}

// ImageRefresher 触发单节点的镜像更新检查任务（有界等待阶段复用缓存）。
type ImageRefresher interface {
	RefreshImages(ctx context.Context, serverID, triggerType, operationID string) (tasks.Task, error)
}

// PackageMaintainer 提供软件包刷新与升级能力。security 模式使用
// package_upgrade_security（实时安全分类），all 模式使用 package_upgrade_all。
type PackageMaintainer interface {
	Refresh(ctx context.Context, serverID string, triggerType ...string) (packages.RefreshResult, error)
	UpgradeAll(ctx context.Context, serverID string, triggerType ...string) (tasks.Task, error)
	UpgradeSecurity(ctx context.Context, serverID string, triggerType ...string) (tasks.Task, error)
}

// ReverseProxyImageUpdater 是入口代理镜像的同步检查 + 逐节点滚动更新能力。
// 实现内部负责判定设施设置 imageAutoUpdateEnabled=true 且存在可用更新：
// 无更新时返回 nil；失败返回错误（协调任务记录后继续软件包阶段，不中止整次运行）。
type ReverseProxyImageUpdater interface {
	AutoUpdateReverseProxyImage(context.Context) error
}
