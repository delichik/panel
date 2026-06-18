# 任务与调度

## 适用场景

修改后台任务、任务注册、任务状态、步骤、日志、重试、手动运行、周期任务、软件包调度、证书续签调度或应用运行任务时，先读本文档。

## 关键入口

- 任务模型、注册表、manager 和服务：`internal/tasks/`
- 周期任务调度：`internal/scheduler/scheduler.go`
- 路由装配：`internal/app/app.go`
- 前端任务中心：`web/src/views/tasks/index.vue`
- 前端任务操作：`web/src/views/tasks/_shared/taskOperations.ts`
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

- 任务数据使用独立 SQLite 数据库 `Store.TaskDB()`，默认文件是 `data/db/tasks.db`；任务主表是 `tasks`，步骤表是 `task_steps`，日志表是 `task_logs`。任务参数写入 `tasks.params_json`，展示和诊断补充信息写入 `tasks.metadata_json`，步骤级执行详情写入 `task_steps.metadata_json`。
- 当前 alpha 阶段任务历史不是稳定持久化契约；注册式任务系统重构会重建 `tasks`、`task_steps` 和 `task_logs`，旧任务中心历史可直接丢弃，但业务数据库和指标数据库不得受影响。
- 所有任务类型必须提前注册后才能创建或执行。业务模块通过任务 manager 创建任务；未注册类型必须返回校验错误，不能直接写入任务表。
- 注册方通过任务定义声明参数校验、执行函数、`BeforeStart`、完成 hook、失败 hook、重试/手动运行能力、并发策略和周期配置。
- 周期任务类型通过 `Periodic.CollectInputs` 收集本轮自动触发需要的参数，并决定是否创建任务实例。返回 `shouldRun=false` 时不创建任务、不执行、不写日志、不进入任务中心；返回 `shouldRun=true` 时交给任务 manager 创建任务。单个输入创建普通任务，多个输入创建一个父任务和多个子任务，子任务执行同一注册任务类型，父任务只负责编排和汇总。手动执行周期任务仍按普通任务语义处理，调用方必须显式传入参数，不会调用 `CollectInputs` 自动补齐。
- 任务框架统一管理活跃任务并发准入。注册方只声明策略，例如允许并行、同资源互斥、同资源排队、同类型全局互斥或自定义并发 key；业务代码不要再手写同类型活跃任务查询。需要复用活跃任务时通过 `tasks.Manager.Create` 的 `created=false` 结果处理。
- 多组同类型参数会创建父任务和多个子任务。父任务负责串行或并行编排与汇总，子任务执行同一注册定义。第一轮前端可平铺展示父子任务，不强制树形任务中心。
- 任务状态、触发来源、资源类型和操作 ID 是前后端筛选与追踪的稳定字段，改名需要迁移并同步前端。
- 任务中心筛选控件清空时可能产生 `null`；前端任务 API 应统一归一化空值和空白字符串，不发送空筛选参数。
- 任务中心类型筛选默认使用“常用类型”，排除所有 `trigger_type=scheduler` 的定时任务，并隐藏内部高频的 `server_connectivity_test` 和 `metrics_collect`。
- 任务中心支持多选 `status` / `type`；API 使用重复的 `status` / `type` 查询参数，`commonOnly=true` 表示常用类型，`includeInternal=true` 表示所有类型。
- 操作标题、任务类型、步骤名称和阶段应在前端按稳定的 `type` / `stage` 标识翻译，不直接展示持久化的英文 summary 作为标题。
- `tasks.Service` 在内存中维护当前进程的 running execution registry。任务进入 `running` 前必须注册执行对象，进入完成、失败、可重试失败或阻塞等终态后必须注销；显式取消会取消 execution context 并移除 registry 项。
- 任务进入 `completed`、`failed`、`failed_retryable`、`blocked` 或 `cancelled` 等终态后，后台 worker 后续的完成/失败/重试写入不得覆盖既有终态；服务器删除会把该服务器的 `queued`、`scheduled`、`failed_retryable` 和 `running` 任务标记为 `cancelled`，避免卡住删除或被调度器继续捡起。
- `FinishExecution` 只在数据库中的任务状态已经不再是 `running` 时清理内存执行对象；如果终态写库失败导致数据库仍为 `running`，必须保留 execution，避免 orphan 检查误判。
- Panel 启动时以及 scheduler 运行期间每 5 秒检查数据库中的 `running` 任务；如果任务 ID 无法在当前进程 execution registry 中找到，会立即标记为失败并记录为 orphaned。
- 由内存 goroutine 直接执行、无法跨进程恢复的一次性 worker 任务，例如服务器重启、UFW 安装/启用，必须在 API 返回前先标记为 `running`。`server_agent_deploy` 虽然也由内存 goroutine 执行，但必须接入调度器 `run-now` / `retry`，用于恢复旧的排队部署任务并重新同步 agent 证书。
- `server_agent_deploy` 自动触发失败达到上限后不得继续自动排队或启动新任务；任务中心和服务器详情仍允许用户手动重试。
- 遗留 `queued` 超过 `scheduler.StaleQueuedWorkerTaskAfter` 的选定 worker 类型会在清理循环中标记为失败并提示用户重试。
- 长耗时后台操作应写入任务日志，并尽量拆出步骤，方便任务中心展示进度。
- `scheduler` 负责驱动注册的周期任务，例如 agent 检查、指标采集、软件包刷新、镜像更新检查、Application 容器监控、证书签发 due task 和证书续签。业务是否需要实际执行以及本轮执行参数由对应任务定义的 `Periodic.CollectInputs` 判断和生成；任务执行函数只消费已经落到任务输入中的参数，不在执行阶段重新扫描本轮资源列表。
- 生产代码创建任务应使用 `tasks.NewManager(taskSvc).Create` 或 manager 封装入口，不直接调用 `Service.Create`；`Service.Create` 保留给任务存储层、测试和低层兼容场景。
- 任务中心的 `run-now` / `retry` 必须按任务定义受控，不再在 scheduler 中维护硬编码 switch。允许手动运行或重试的任务必须注册对应能力。
- `retry` 创建的新任务会立即交给任务 manager 执行；如果执行器启动前返回错误，handler 会把新任务标记为失败，避免永久排队。

## 跨模块依赖

- 服务器测试、重启、UFW、agent 部署和软件包维护依赖本模块记录任务。
- 应用部署、停止、重启、镜像检查和镜像更新依赖本模块记录任务；实际容器操作由应用服务调用 agent runtime API。
- 容器启动、停止、重启、删除，镜像拉取、删除、删除未使用，以及卷删除、删除未使用由容器化模块同步串行执行，不再创建操作任务；成功后会立即创建 `container_refresh`、`image_refresh` 或 `volume_refresh` 刷新任务。
- 手动镜像刷新、Application 镜像升级和 Application 协调恢复仍依赖本模块记录任务；同服务器 Docker 写操作由容器化模块串行执行。
- 证书签发、续签、密钥资产重新签发、SSH 密钥重新生成和导入依赖本模块记录任务；ACME 签发/续签任务会记录 `acme_*` 阶段和对应步骤 metadata。
- 启用服务器 agent 后，`metrics_collect` 与 `server_info_collect` 中的读取能力会走目标机 `panel-agent` mTLS 通道，不允许在 agent 失败时回落 SSH。依赖 agent 的定时工作只在 `agent.status=compatible` 且存在 `agent.url` 时创建或执行；agent 未部署、异常、不可部署或版本能力不兼容时跳过当前资源工作，不创建新的资源操作任务。`server_info_collect`、`metrics_collect`、应用运行时任务和容器化任务遇到 agent mTLS server 证书过期或尚未生效时，会标记 agent 不兼容、按受限自动重装策略处理 `server_agent_deploy`，并按当前 agent 错误失败；恢复 agent 本身的 `server_agent_deploy` 不受该跳过规则限制，但自动触发必须先确认 agent 确实未配置 agent URL、不兼容、旧端口或证书需要修复，`agent.status=compatible` 且配置正常时不得重装，普通 `agent.status=unavailable`、网络错误、服务器失联或 Docker 不可用不得触发自动重装，`agent.status=undeployable` 时周期检查不得继续自动部署。
- 软件包刷新/升级、UFW 写操作和服务器重启仍走 SSH，不要把这些写入型或长流程任务路由到 agent。

## 密钥资产任务

- `key_asset_tls_reissue`、`key_asset_ssh_regenerate`、`key_asset_export`、`key_asset_import`、`key_asset_sync` 记录密钥资产操作。
- 重新签发、重新生成和导入任务会触发已启用应用重新部署，任务终态必须注销 execution。
- 导出任务完成后通过 `/api/v1/key-assets/exports/{taskId}/download` 下载短期加密归档。

## 验证

- 后端任务或调度改动运行 `task test:backend`，重点关注 `internal/tasks` 和 `internal/scheduler`。
- 前端任务中心或 API 类型改动按影响范围运行 `task test:web` 或 `task build:web`。

## 文档更新触发

新增任务类型、注册字段、并发策略、周期输入收集规则、状态、步骤结构、日志语义、调度项、手动运行行为或任务筛选字段时，必须更新本文档。
