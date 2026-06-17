# 容器化资源管理

## 适用场景

修改 Docker 容器、镜像、网络、卷资源页，Application 托管 Label，每服务器容器操作队列，镜像更新检查或容器协调监控时，先读本文档。

## 关键入口

- Panel 服务与 API：`internal/containerization/`
- Agent Docker Engine API：`internal/agent/docker_runtime.go`、`internal/agent/handler.go`
- Application 运行时：`internal/applications/service.go`
- 调度：`internal/scheduler/scheduler.go`
- 前端页面：`web/src/views/containerization/`
- 前端 API：`web/src/api/containerization.ts`

## 页面与 API

“容器化”一级菜单包含应用、容器、镜像、网络和卷。容器、镜像、网络、卷使用左侧服务器选择器和右侧内部滚动列表。

- 容器支持查询、启动、停止、重启、删除。
- 镜像支持查询、拉取、删除、删除未使用镜像、刷新更新状态、升级选中 Application 和全部升级；批量危险操作必须通过确认对话框触发。
- 网络只读。
- 卷支持查询、单个删除和批量删除未使用卷，必须展示使用状态；批量删除执行时需重新查询使用状态，只删除执行瞬间仍未使用的卷。

Panel API 挂在 `/api/v1/servers/{serverId}/containers|images|networks|volumes`；批量 Application 镜像更新使用 `/api/v1/images/upgrade-selected|upgrade-all`。

## 托管 Label

Application 新部署容器只写入：

- `panel.application.managed`
- `panel.application.id`
- `panel.application.instance.id`
- `panel.application.generation`
- `panel.application.spec.hash`

不兼容旧下划线 Label，也不自动迁移旧容器。

## 队列与协调

- 每台服务器一条独立容器操作队列；同服务器串行，不同服务器并行。
- 普通容器操作和 Application 部署、停止、重启共享该队列。
- 相同任务类型、服务器和资源的活跃请求复用现有任务；Agent 操作按目标状态幂等。
- 容器、镜像、网络、卷查询和队列操作遇到 agent mTLS server 证书过期或尚未生效时，必须交给服务器模块标记 Agent 状态并按受限自动重装策略处理；当前容器化任务或请求仍按原始 agent 错误失败。
- 镜像和卷的“删除未使用”是 Panel 侧批量任务，通过现有 Agent 单项删除接口逐项执行；任务 metadata 记录动作和服务器，`delete_unused` 步骤 metadata 记录扫描数量、跳过数量、删除数量、删除对象和失败对象。
- scheduler 每 5 秒运行容器监控。只有已经观察到新托管 Label 并写入 `application_reconcile_states` 的实例会持续协调，避免旧 Label 自动迁移。
- 监控发现容器缺失、停止或 generation/spec hash 偏差时创建 `application_reconcile`。

## 镜像更新

- `image_updates` 保存每服务器镜像引用、本地摘要、远端摘要、状态、错误和检查时间。
- `image_refreshes` 保存最近刷新时间。
- scheduler 的镜像检查节奏与软件包刷新一致。
- 所有带标签且可解析的镜像都显示更新状态；普通容器镜像不提供升级操作。
- Application 镜像升级复用 `applications.Service.UpdateImage` 并重新部署。

## 验证

- 后端改动运行 `task test:backend`，必要时运行 `task build:backend`。
- 前端改动运行 `task test:web` 和 `task build:web`。
