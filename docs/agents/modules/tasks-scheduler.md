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
- 任务中心类型筛选默认使用“常用类型”，排除所有 `trigger_type=scheduler` 的定时任务，并隐藏内部/高频的 `server_connectivity_test` 和 `metrics_collect`；切到“所有类型”或精确选择对应类型时可显示这些任务。列表按最新创建时间优先展示。
- 任务中心筛选支持多选 `status` / `type`，前端通过搜索按钮提交；API 使用重复的 `status` / `type` 查询参数，`commonOnly=true` 表示常用类型，`includeInternal=true` 表示“所有类型”。
- 操作标题、任务类型、步骤名称和阶段应在前端按稳定的 `type` / `stage` 标识翻译，不直接展示持久化的英文 summary 作为标题。
- 任务中心每页默认 20 条；分页在手机显示 5 个页码，在桌面显示 10 个页码，并确保当前页数字与选中背景有足够对比度。
- `tasks.Service` 在内存中维护当前进程的 running execution registry。任务进入 `running` 前必须注册执行对象，进入完成、失败、可重试失败或阻塞等终态后必须注销。
- Panel 启动时以及 scheduler 运行期间每 5 秒检查一次数据库中的 `running` 任务；如果任务 ID 无法在当前进程的 execution registry 中找到，会立即标记为失败并记录为 orphaned。该检查用于处理进程重启、异常退出或状态与实际执行脱节，不能依赖固定时长判断。
- 由内存 goroutine 直接执行、无法跨进程恢复的一次性任务（Nomad 加入/引导/重建/切换/移除、服务器重启、UFW 安装/启用）必须在 API 返回前先标记为 `running`；遗留 `queued` 超过 `scheduler.StaleQueuedWorkerTaskAfter`（当前 10 分钟）会在清理循环中标记为失败并提示用户重试，避免永久排队。
- 长耗时后台操作应写入任务日志，并尽量拆出步骤，方便任务中心展示进度。
- `nomad_reverse_proxy_sync` 用于追踪反向代理配置保存、远程防火墙放行和 Nomad 反向代理 job reconcile；该任务当前由保存接口同步完成或失败，不提供 `run-now` / `retry`。
- `nomad_tls_rotate` 用于追踪 Nomad CA/证书重新生成、全部节点重部署、应用恢复和反向代理同步。
- `scheduler` 负责周期性指标采集、软件包刷新、证书续签和 due 的包刷新任务补扫，并可作为 `run-now` 执行入口；同一轮调度为多台服务器创建任务时，应共享一个 `operationId`，由任务中心展示为一个 operation 下的多个 task。周期性指标采集记录为 `metrics_collect` 任务，默认由“常用类型”筛选隐藏。
- 任务中心的 `run-now` / `retry` 必须按任务类型受控；当前只允许 `server_connectivity_test`、`server_info_collect`、`package_refresh`、`certificate_issue` 这类有调度器执行器的任务。后端 handler 会按状态和类型拒绝不支持的调用，前端也只展示可闭环的操作。
- `retry` 创建的新任务会立即交给调度器执行；如果调度器在启动前返回错误，handler 会把新任务标记为失败，避免产生永久排队任务。`package_refresh` 的已排队任务还会被调度器持续补扫，避免被周期刷新节流或已有刷新状态长期挡住。
- 软件包刷新现在记录为 `package_refresh` 任务；手动刷新返回 `taskId`，自动/周期刷新失败会在任务中心可见，并对近期失败做短时间节流；周期刷新同一轮创建的多台服务器任务必须共享一个 operation。
- 远程命令原始输出可能包含第三方文本，翻译前要先评估是否应保留原样。

## 跨模块依赖

- 服务器测试、重启、UFW、软件包维护依赖本模块记录任务。
- Nomad 引导、加入、移除节点、server 切换、集群重建和反向代理同步依赖本模块记录任务；其中直接起 worker 的操作不能只保持 `queued` 等待 goroutine 内部再启动。
- 应用部署、停止、重启、镜像更新依赖本模块记录任务。
- 证书签发和续签依赖本模块记录任务。

## 验证

- 先按模块索引的“检查和测试范围”判断是否需要验证。
- 需要验证后端任务或调度改动时，运行 `task test:backend`，重点关注 `internal/tasks` 和 `internal/scheduler` 测试。
- 前端任务中心或 API 类型改动只按需要运行 `task test:web`。

## 文档更新触发

新增任务类型、状态、步骤结构、日志语义、调度项、手动运行行为或任务筛选字段时，必须更新本文档。

## 密钥资产任务

- 新增 `key_asset_tls_reissue`、`key_asset_ssh_regenerate`、`key_asset_export`、`key_asset_import`、`key_asset_sync`。
- 重新签发、重新生成和导入任务包含资产更新、已启用应用重部署和反向代理同步；任务终态必须注销 execution。
- 导出任务完成后通过 `/api/v1/key-assets/exports/{taskId}/download` 下载短期加密归档。
