# 任务与调度

## 适用场景

修改后台任务、任务注册、任务状态、步骤、日志、重试、手动运行、周期任务、软件包调度、证书续签调度或应用运行任务时，先读本文档。

## 关键入口

- 任务模型、注册表、manager 和服务：`internal/modules/tasks/`
- 任务内部 worker 与周期驱动：`internal/modules/tasks/worker.go`、`internal/modules/tasks/periodic.go`
- 路由注册：`internal/modules/tasks/routes.go`
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
- 任务系统建模使用“操作 + 任务”两层语义：操作是由用户、调度器、系统恢复或其他业务因素发起的一次聚合意图，使用同一个 `operation_id` 追踪；任务是操作中的具体执行对象，必须包含本次执行所需变量，例如目标服务器、资源 ID、参数 JSON、触发来源和资源类型。
- 业务动作只要可能拆成多个同类型但变量不同的执行对象，就应优先按一个操作拆多个任务建模，而不是把所有目标藏进单个任务的 metadata 或业务私有 target 表。典型例子：一次应用部署操作面向服务器 A 和 B，应创建一个操作聚合，并在该操作下创建“部署 A”和“部署 B”两个 `application_deploy` 任务。
- 私有业务表可以继续记录领域状态，例如应用 lifecycle operation/target、镜像检查缓存或证书签发详情；但这些表不能取代任务系统里的执行对象。任务中心需要能通过 `operation_id`、父子任务字段和任务参数看见操作拆分后的具体任务。
- 单目标动作使用 `tasks.Manager.Create` 或封装入口创建单个任务；多目标或多变量动作使用 `tasks.Manager.CreateBatch` / `CreateBatchAndRun` 创建父任务和子任务，所有子任务必须使用同一注册任务类型、同一 `operation_id`，并把每个目标自己的变量写入对应 `CreateInput`。
- 注册方负责通过任务定义、并发策略和业务封装入口约束该任务类型是否允许异步/并行执行：允许并行时批量输入可使用 `ExecutionModeParallel`，否则必须使用 `ExecutionModeSerial` 或单任务执行。调用方不得绕开注册定义，把不允许并行的任务强行拆成并行 goroutine；后续如果新增显式异步能力字段，也必须由注册方声明并同步任务中心展示。
- `internal/modules/tasks` 只提供任务框架能力，不维护业务任务类型清单、业务 executor 或业务周期输入。业务任务类型必须由拥有该任务的业务模块在自己的 `RegisterTasks` 入口注册；生产装配由 `internal/bootstrap/panel/app.go` 的集中任务注册阶段统一调用各模块入口。
- 所有任务类型必须提前注册后才能创建或执行。业务模块通过任务 manager 创建任务；未注册类型必须返回校验错误，不能直接写入任务表。框架测试如果需要业务形状的任务类型，必须在测试夹具中显式注册本测试所需的假定义，不允许把业务类型补回 `tasks` 包默认清单。
- 注册方通过任务定义声明参数校验、执行函数、`BeforeStart`、完成 hook、失败 hook、重试/手动运行能力、并发策略、一次性 worker 排队超时清理能力和周期配置。
- 业务模块的 `tasks.go` 必须保持声明式：`Execute` 直接绑定接受 `tasks.TaskContext` 的具名方法，`CollectInputs` 直接绑定具名 collector。禁止使用只为转调另一函数而存在的匿名 executor，也禁止在任务定义中内联节流、扫描和批量输入组装等大段逻辑。
- 多个周期任务共用的时间节流使用 `tasks.NewIntervalCollector`；业务模块仍负责提供实际输入 collector，tasks 框架只管理调用间隔和上次成功产出时间。
- `Hidden`、`AllowRunNow`、`AllowRetry`、`DefaultMaxRetries`、`StaleQueuedAfter`、并发策略、executor 和周期配置都属于任务定义的一部分；这些能力随业务模块注册，不由任务中心或任务内部 worker 根据 task type 字符串另行维护。没有注册 `Execute` 的记录型任务不得声明 `AllowRunNow` 或 `AllowRetry`，避免任务中心暴露无法真正执行的手动运行或重试入口。
- 任务列表、详情、重试和手动运行 API 返回任务时，会根据当前注册定义补充 `allowRunNow` 与 `allowRetry`。前端必须使用这两个响应字段决定操作入口，不得维护 task type 白名单；即使定义误声明能力但没有 executor，API 也必须返回不可操作。
- 周期任务类型通过 `Periodic.CollectInputs` 收集本轮自动触发需要的参数，并决定是否创建任务实例。返回 `shouldRun=false` 时不创建任务、不执行、不写日志、不进入任务中心；返回 `shouldRun=true` 时交给任务 manager 创建任务。单个输入创建普通任务，多个输入创建一个父任务和多个子任务，子任务执行同一注册任务类型，父任务只负责编排和汇总。手动执行周期任务仍按普通任务语义处理，调用方必须显式传入参数，不会调用 `CollectInputs` 自动补齐。周期 collector 面向多个资源时必须共享一个 `operation_id`，每个资源生成独立任务输入。
- 任务框架统一管理活跃任务并发准入。注册方只声明策略，例如允许并行、同资源互斥、同资源排队、同类型全局互斥或自定义并发 key；业务代码不要再手写同类型活跃任务查询。需要复用活跃任务时通过 `tasks.Manager.Create` 的 `created=false` 结果处理。
- 多组同类型参数会创建父任务和多个子任务。父任务负责串行或并行编排与汇总，子任务执行同一注册定义；在任务中心语义中，`operation_id` 对应一次“操作”聚合，子任务对应操作中的具体执行任务。任一子任务结束为 `failed`、`failed_retryable`、`blocked` 或 `cancelled` 时，父任务必须汇总为失败，不能因为 executor 已经完成落库而把操作误标为 completed。
- 任务状态、触发来源、资源类型和操作 ID 是前后端筛选与追踪的稳定字段，改名需要迁移并同步前端。
- 任务中心筛选控件清空时可能产生 `null`；前端页面应用筛选前、前端任务 API 组装查询参数前都应统一归一化空值和空白字符串，不发送空筛选参数。
- 任务中心类型筛选默认使用“常用类型”，排除所有 `trigger_type=scheduler` 的定时任务，并隐藏内部高频的 `metrics_collect`。
- 任务中心支持多选 `status` / `type`；API 使用重复的 `status` / `type` 查询参数，`commonOnly=true` 表示常用类型，`includeInternal=true` 表示所有类型。`所有类型` 是互斥筛选模式，搜索时必须保持 `includeInternal=true` 且不发送具体 `type`，不能在归一化过程中退回 `commonOnly=true`。任务中心左侧按“操作”展示时必须使用 `operationPage=true` 按 operation 分页，后端先分页去重后的 `operation_id`（空值退回任务 ID）再返回这些操作下的任务；不得用原始任务分页结果在前端折叠，否则批量任务会导致一页只显示少量操作。
- 任务中心不做后台自动轮询刷新；进入页面、路由指定任务变化、筛选、分页、手动运行、重试和日志完成事件可以主动重新加载。需要恢复自动刷新时必须提供明确的用户可控开关并更新本文档。
- 任务中心不在筛选区上方重复展示活跃、排队、运行、失败和完成数量摘要；左侧操作选择列表展示状态图标、任务名称、状态，以及一行“创建时间 · 对象”弱化上下文。对象优先使用服务器名称，没有可解析名称时只显示本地化资源类别，不回退裸资源 ID；操作 ID、任务数量、执行模式与进度等诊断信息统一放在右侧详情。存在批量父任务时，“操作中的任务”表格只展示子任务，父任务只作为操作聚合和执行模式来源。
- 操作标题、任务类型、步骤名称和阶段应在前端按稳定的 `type` / `stage` 标识翻译，不直接展示持久化的英文 summary 作为标题。
- `tasks.Service` 在内存中维护当前进程的 running execution registry。任务进入 `running` 前必须注册执行对象，进入完成、失败、可重试失败或阻塞等终态后必须注销；显式取消会取消 execution context 并移除 registry 项。
- Debug 快照会只读导出任务注册定义的稳定诊断字段，包括隐藏、执行器、周期配置、手动运行/重试、默认重试、并发策略和 stale queued 超时；不得导出任务参数、collector 输入或业务数据。
- 任务进入 `completed`、`failed`、`failed_retryable`、`blocked` 或 `cancelled` 等终态后，后台 worker 后续的完成/失败/重试写入不得覆盖既有终态；服务器删除会把该服务器的 `queued`、`scheduled`、`failed_retryable` 和 `running` 任务标记为 `cancelled`，避免卡住删除或被调度器继续捡起。
- `FinishExecution` 只在数据库中的任务状态已经不再是 `running` 时清理内存执行对象；如果终态写库失败导致数据库仍为 `running`，必须保留 execution，避免 orphan 检查误判。
- Panel 启动时以及 tasks 内部 worker 运行期间每 5 秒检查数据库中的 `running` 任务；如果任务 ID 无法在当前进程 execution registry 中找到，会立即标记为失败并记录为 orphaned。
- 由内存 goroutine 直接执行、无法跨进程恢复的一次性 worker 任务，例如服务器重启、UFW 安装/启用、fail2ban 应用，必须在 API 返回前先标记为 `running`。`server_agent_deploy` 虽然也由内存 goroutine 执行，但必须接入调度器 `run-now` / `retry`，用于恢复旧的排队部署任务并重新同步 agent 证书。
- `server_agent_deploy` 自动触发失败达到上限后不得继续自动排队或启动新任务；任务中心和服务器详情仍允许用户手动重试。
- 由内存 goroutine 直接执行、无法跨进程恢复的一次性 worker 任务，如果需要清理遗留 `queued` 状态，必须在任务定义中设置 `StaleQueuedAfter`；tasks 内部 worker 只扫描注册表中声明了该能力的任务类型，并在超时后标记为失败提示用户重试。
- 长耗时后台操作应写入任务日志，并尽量拆出步骤，方便任务中心展示进度。
- tasks 内部 worker 负责驱动注册的周期任务、唤醒到期队列任务、清理 stale queued 状态和检查 orphan running 状态。它不是独立业务模块，不注册任何特殊任务，也不通过业务 task type 字符串维护 executor 或 run-now/retry switch。
- 全量备份导出不在正常业务运行期执行。设置页只写 pending export 并提示重启；下一次启动进入备份导出维护模式，此时正常 tasks worker 与周期驱动尚未启动，导出进度本身不依赖任务系统。
- 到期队列唤醒直接扫描注册表中带 executor 的定义，并统一调用 `tasks.Manager.Run`；不得注册或持久化 `task_queue_drain`，不得直接调用 `Definition.Execute` 绕过任务启动、execution registry、hook、完成和失败落库。
- 业务是否需要周期执行以及本轮执行参数由对应任务定义的 `Periodic.CollectInputs` 判断和生成；任务执行函数只消费已经落到任务输入中的参数，不在执行阶段重新扫描本轮资源列表。
- 生产代码创建任务应使用 `tasks.NewManager(taskSvc).Create` 或 manager 封装入口，不直接调用 `Service.Create`；`Service.Create` 保留给任务存储层、测试和低层兼容场景。
- 任务 HTTP handler 依赖 `ServeMux` pattern 注入的 `PathValue` 读取任务 ID；新增任务 API 时在 `routes.go` 注册 method-pattern，不在 bootstrap 增加业务路径 switch。
- 任务中心的 `run-now` / `retry` 必须按任务定义受控，不在 tasks worker 中维护硬编码 switch。允许手动运行或重试的任务必须同时注册对应能力和 `Execute`，实际执行统一经过 `tasks.Manager.Run`。
- `retry` 创建的新任务会立即交给任务 manager 执行；如果执行器启动前返回错误，handler 会把新任务标记为失败，避免永久排队。

## 跨模块依赖

- 服务器测试、重启、UFW、fail2ban、agent 部署和软件包维护依赖本模块记录任务；其中没有 executor 的一次性 worker 或记录型任务只保留任务记录，不暴露任务中心重试。fail2ban 的接管/应用和取消接管共用 `server_fail2ban_apply` 一次性 worker 任务，保存草稿不创建任务。
- 应用部署、停止、重启、镜像检查和镜像更新依赖本模块记录任务；实际容器操作由应用服务调用 agent runtime API。
- 容器启动、停止、重启、删除，镜像拉取、删除、删除未使用，以及卷删除、删除未使用由容器化模块同步串行执行，不再创建操作任务；成功后会立即创建 `container_refresh`、`image_refresh` 或 `volume_refresh` 刷新任务。
- 手动镜像刷新、Application 镜像升级和 Application 协调恢复仍依赖本模块记录任务；同服务器 Docker 写操作由容器化模块串行执行。
- 证书签发、续签、密钥资产重新签发、SSH 密钥重新生成和导入依赖本模块记录任务；ACME 签发/续签任务会记录 `acme_*` 阶段和对应步骤 metadata。
- `server_info_collect` 的首次 bootstrap 输入在服务器创建后立即执行，失败时允许回滚尚未完成初始化的服务器；普通 refresh 输入固定每小时收集一次完整系统信息，失败只记录为可重试任务，绝不能删除服务器。周期 refresh 仅为存在兼容 Agent 的服务器创建。
- 启用服务器 agent 后，`metrics_collect` 与普通 `server_info_collect` 中的读取能力会走目标机 `panel-agent` mTLS 通道，不允许在 agent 失败时回落 SSH。依赖 agent 的定时工作只在 `agent.status=compatible` 且存在 `agent.url` 时创建或执行；agent 未部署、异常、不可部署或版本不一致时跳过当前资源工作，不创建新的资源操作任务。`server_info_collect`、`metrics_collect`、应用运行时任务和容器化任务遇到 agent mTLS server 证书过期或尚未生效时，会标记 agent 不兼容、按受限自动重装策略处理 `server_agent_deploy`，并按当前 agent 错误失败；恢复 agent 本身的 `server_agent_deploy` 不受该跳过规则限制。
- 软件包刷新/升级、UFW 写操作、fail2ban 安装/接管/应用/取消接管和服务器重启必须路由到兼容 Agent，不允许回退 SSH。长耗时 APT 请求使用独立 Agent maintenance HTTP 超时并把命令输出写入 Panel 任务日志；SSH 只保留 Agent bootstrap、安装、修复和证书恢复。

## 密钥资产任务

- `key_asset_tls_reissue`、`key_asset_ssh_regenerate`、`key_asset_export`、`key_asset_import`、`key_asset_sync` 记录密钥资产操作；当前这些记录型任务不注册 executor，因此不暴露任务中心手动运行或重试。
- 重新签发、重新生成和导入任务会触发已启用应用重新部署，任务终态必须注销 execution。
- 导出任务完成后通过 `/api/v1/key-assets/exports/{taskId}/download` 下载短期加密归档。

## 验证

- 后端任务或周期驱动改动运行 `task test:backend`，重点关注 `internal/modules/tasks` 及注册周期任务的业务模块。
- 前端任务中心或 API 类型改动按影响范围运行 `task test:web` 或 `task build:web`。

## 文档更新触发

新增任务类型、注册字段、并发策略、一次性 worker 排队超时清理能力、周期输入收集规则、状态、步骤结构、日志语义、调度项、手动运行行为或任务筛选字段时，必须更新本文档。
