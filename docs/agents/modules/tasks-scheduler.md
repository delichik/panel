# 任务与调度

## 适用场景

修改后台任务、任务状态、步骤、日志、重试、手动运行、周期采集、软件包调度或证书续签调度时，先读本文档。

## 关键入口

- 任务模型和服务：`internal/tasks/`
- 周期调度：`internal/scheduler/scheduler.go`
- 路由装配：`internal/app/app.go`
- 前端任务中心：`web/src/features/tasks/pages/TaskCenterPage.vue`
- 前端任务操作：`web/src/features/tasks/taskOperations.ts`
- 任务日志组件：`web/src/components/tasks/TaskLogPanel.vue`
- API：`web/src/api/tasks.ts`
- 类型：`web/src/types/api.ts`

## API 范围

- `GET /api/v1/tasks`
- `GET /api/v1/tasks/{id}`
- `GET /api/v1/tasks/{id}/logs`
- `GET /api/v1/tasks/{id}/steps`
- `POST /api/v1/tasks/{id}/retry`
- `POST /api/v1/tasks/{id}/run-now`

## 数据与行为约定

- 任务主表是 `tasks`，步骤表是 `task_steps`，日志表是 `task_logs`。
- 任务状态、触发来源、资源类型和操作 ID 是前后端筛选与追踪的稳定字段，改名需要迁移和前端同步。
- 任务中心的筛选控件清空时可能产生 `null`；前端任务 API 应统一归一化空值和空白字符串，不发送空筛选参数。
- 任务中心默认隐藏内部 `server_connectivity_test` 刷新任务，列表按最新创建时间优先展示，避免后台健康检查挤掉用户操作任务。
- `running` 状态任务超过 `tasks.StaleRunningTaskAfter`（当前 24 小时）仍未完成时，会在启动或清理循环中自动标记为失败，避免旧任务长期卡住。
- 长耗时后台操作应写入任务日志，并尽量拆出步骤，方便任务中心展示进度。
- `nomad_reverse_proxy_sync` 用于追踪反向代理配置保存、远程防火墙放行和 Nomad 反向代理 job reconcile；该任务当前由保存接口同步完成或失败，不提供 `run-now` / `retry`。
- `scheduler` 负责周期性指标采集、软件包刷新、证书续签和 due 的包刷新任务补扫，并可作为 `run-now` 执行入口。
- 任务中心的 `run-now` / `retry` 必须按任务类型受控；当前只允许 `server_connectivity_test`、`server_info_collect`、`package_refresh`、`certificate_issue` 这类有调度器执行器的任务。后端 handler 会按状态和类型拒绝不支持的调用，前端也只展示可闭环的操作。
- `retry` 创建的新任务会立即交给调度器执行；如果调度器在启动前返回错误，handler 会把新任务标记为失败，避免产生永久排队任务。`package_refresh` 的已排队任务还会被调度器持续补扫，避免被周期刷新节流或已有刷新状态长期挡住。
- 软件包刷新现在记录为 `package_refresh` 任务；手动刷新返回 `taskId`，自动/周期刷新失败会在任务中心可见，并对近期失败做短时间节流。
- 远程命令原始输出可能包含第三方文本，翻译前要先评估是否应保留原样。

## 跨模块依赖

- 服务器测试、UFW、软件包维护依赖本模块记录任务。
- Nomad 引导、加入、移除节点、server 切换、集群重建和反向代理同步依赖本模块记录任务。
- 应用部署、停止、重启、镜像更新依赖本模块记录任务。
- 证书签发和续签依赖本模块记录任务。

## 验证

- 先按模块索引的“检查和测试范围”判断是否需要验证。
- 需要验证后端任务或调度改动时，运行 `task test:backend`，重点关注 `internal/tasks` 和 `internal/scheduler` 测试。
- 前端任务中心或 API 类型改动只按需要运行 `task test:web`。

## 文档更新触发

新增任务类型、状态、步骤结构、日志语义、调度项、手动运行行为或任务筛选字段时，必须更新本文档。
