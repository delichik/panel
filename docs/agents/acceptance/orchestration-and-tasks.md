# 编排、任务与运行事件验收合同

## 1. 范围与判定

本文约束应用 desired/observed 控制面、Planner/Controller/Job/lease/retry/drift、通用任务/步骤/日志/手动运行/周期/恢复/清理，以及系统日志（runtime events）。它是变更验收合同，不是实现教程。

每条稳定编号由**前置、动作、结果、失败、不变量、验证**组成。结果或不变量不满足即为回归。禁止因重排复用编号；失效条目应标注废弃并保留编号。

三类事实必须保持隔离：AppDB `application_instances + application_revisions + jobs` 是应用部署事实；LogDB `tasks + task_steps + task_logs` 是通用任务执行记录；runtime events 是可丢弃的简明系统日志。任何一类不得推断、回滚或覆盖另一类事实。

## 2. Desired / Observed 控制面

### ORCH-STATE-001 Instance 冲突域和字段所有权

- **前置**：同一 application/server 已有或没有 Instance。
- **动作**：Planner 接收 apply/stop/purge 计划。
- **结果**：按 `application_id + server_id` upsert 唯一 Instance；desired_state/generation/spec_hash/revision/spec_json 由 Planner 写，observed_* 由 ObservationWriter 写。
- **失败**：业务 handler/service 不得直接把 Agent 结果写入 observed；report 不得反向覆盖 desired。
- **不变量**：旧 `status/runtime_spec_json/last_deployed_generation` 仅兼容投影，不能成为新逻辑事实源。
- **验证**：Store schema/ownership review、observation writer tests。

### ORCH-STATE-002 Desired action 映射

- **前置**：计划输入提供 action 或 desired_state。
- **动作**：Planner 规范化输入。
- **结果**：apply↔running、stop↔stopped、purge↔purged；缺省 action 从 desired 推导，缺省 desired 从 action 推导。
- **失败**：非 apply/stop/purge action 或缺 application/server 返回 validation error，数据库无变化。
- **不变量**：purge 的 removeData 语义由删除意图明确携带，不能由 stop 猜测。
- **验证**：planner validation/mapping tests。

### ORCH-STATE-003 Observed 状态语义

- **前置**：Agent report 或 reconcile 返回 running/stopped/missing/failed/unknown。
- **动作**：ObservationWriter 写回。
- **结果**：保存容器身份、observed generation/hash/image/time/sequence/source、last reconcile job 和结构化错误；missing 保持独立状态。
- **失败**：空状态正规化 unknown；不得把 missing 映射 stopped 或把 unknown 当成功。
- **不变量**：只有 reconcile source 且 desired spec 非空时更新兼容 runtime_spec_json；成功状态清空兼容 last_error。
- **验证**：observation/state projection tests。

### ORCH-STATE-004 source/sequence CAS

- **前置**：已有 observed_source/sequence/time，收到乱序 report 或 reconcile 回写。
- **动作**：写 Observation。
- **结果**：有 sequence 的报告仅在 sequence 严格递增时接受；无 sequence report 仅在时间不旧且当前 source 非 reconcile 时接受；reconcile source 可按 fencing 条件更新。
- **失败**：陈旧 observation 返回 `Accepted=false` 且实例不变；无 durable Job 的 report/cache CAS miss 属预期竞争，不得逐实例追加用户 warning。
- **不变量**：只要携带 jobId 就必须验证该 Job 仍 running 且非空 leaseToken 匹配，空 token 不得绕过 fencing；durable Job 拒绝记录明确 reason、应用/服务器资源及 incoming/current 诊断，但不得记录 lease token。
- **验证**：sequence、routine stale no-warning、lease/instance rejection diagnostics tests。

## 3. 不可变修订与 Planner

### ORCH-REV-001 修订不可变与唯一

- **前置**：应用 generation>0，提供 runtime spec、managed-file manifest、镜像信息和 SpecYAML。
- **动作**：EnsureRevision。
- **结果**：按 application+generation 唯一保存不可变快照；空 runtime/manifest 正规化 `{}`/`[]`；重复调用返回既有 revision。
- **失败**：缺 application 或 generation≤0 拒绝；新 worker 不能从旧 LogDB revision 取执行输入。
- **不变量**：新 revision 只写 AppDB；旧 LogDB 行仅迁移/兼容诊断。
- **验证**：revision uniqueness/migration tests。

### ORCH-REV-002 修订与批量计划原子性

- **前置**：一次新 generation 需要 N 个目标。
- **动作**：EnsureRevisionAndPlanBatch。
- **结果**：revision、全部 Instance desired 和全部 Job create/merge 在单一短 AppDB 事务提交；任一失败全部回滚。
- **失败**：不得向 Controller 暴露部分目标集或无 revision 的 apply Job。
- **不变量**：wake 只能在事务成功后发送。
- **验证**：atomic batch failure injection test。

### ORCH-PLAN-001 Job 创建与 active 唯一性

- **前置**：同一 app/server 无 active Job。
- **动作**：Plan。
- **结果**：创建 pending Job，保存 action、desired snapshot、priority、intent/trigger/reason/idempotency 和空 lease/error/steps。
- **失败**：partial unique index `uq_jobs_active_app_server` 必须阻止 pending/running/failed_retryable 并存。
- **不变量**：Job 是该冲突域唯一 durable 工作；tasks 并发键不是替代品。
- **验证**：store constraint/concurrent plan tests。

### ORCH-PLAN-002 幂等键重放

- **前置**：同 application 已存在非空 idempotencyKey 的 Job。
- **动作**：以相同 key 再 Plan。
- **结果**：返回同 Job、`Merged=true`，不创建新行、不重复 runtime 工作。
- **失败**：key 不得跨 application 错误复用。
- **不变量**：幂等重放优先于创建新 intent/instance。
- **验证**：planner replay test。

### ORCH-PLAN-003 pending/retryable Job 合并

- **前置**：冲突域已有 pending 或 failed_retryable Job，desired 发生变化。
- **动作**：Plan 新意图。
- **结果**：复用同 Job 并用最新 desired/action/revision/spec/removeData/priority/intent/trigger/reason 更新，清 nextRun/error/finished。
- **失败**：不得增加第二条 active Job或保留旧 backoff 阻挡新 desired。
- **不变量**：Instance desired 与合并 Job 的 desired snapshot 在同事务一致。
- **验证**：planner merge/update desired test。

### ORCH-PLAN-004 running Job 合并与 force nonce

- **前置**：冲突域 Job 正 running，新的 force 操作到达。
- **动作**：Plan。
- **结果**：不替换 in-flight snapshot、不创建新 Job；仅当新 forceNonce 更大时更新该行 nonce，worker 回写后通过 desired-change 检测 requeue 最新 desired。
- **失败**：旧 RPC 成功不得吞掉重启/新配置意图。
- **不变量**：旧 lease/token 继续 fence 当前执行；运行中非 force desired 由 Instance 最新值触发 requeue。
- **验证**：stop-during-apply、force nonce integration tests。

### ORCH-PLAN-005 满足态与 force

- **前置**：目标 observed 已匹配 desired，或部分节点 drift。
- **动作**：应用服务构造完整计划。
- **结果**：普通计划跳过满足节点；明确 report drift 的节点必须规划；force 可绕过满足态和应用级 backoff。
- **失败**：不得因一个节点失败而重部署其它健康节点；不得用滞后 running cache 过滤掉已确认 drift 节点。
- **不变量**：force 永远不能绕过 active Job 唯一性或 lease fencing。
- **验证**：healthy no extra apply、drift bypass/satisfied skip tests。

## 4. Controller、租约与重试

### ORCH-CTRL-001 启动与 due scan

- **前置**：Store 可用，存在 pending、到期 retryable 或过期 running leases。
- **动作**：Controller Start/Wake/定时 scan。
- **结果**：启动先 RecoverExpiredLeases；扫描按 priority DESC、created/id ASC 取到期工作；wake 仅降低延迟，丢失后 scan 仍收敛。
- **失败**：Store 不可用或 schema 无效则启动失败，不得运行半配置 worker；扫描、Job 查询和处理错误必须输出按 stage/job 限频的结构化进程诊断，不得每 250ms 刷屏或静默吞掉。
- **不变量**：Start 幂等；默认 worker=8、scan=250ms、lease=3m、队列至少 worker×2（除非显式合法配置）。
- **验证**：controller start/recovery/order tests。

### ORCH-CTRL-002 进程内冲突域队列

- **前置**：多个 due Jobs，可能同 app/server。
- **动作**：enqueue/processAsync。
- **结果**：同 app/server key 同时最多一个进入本进程队列，不同 key 可并发。
- **失败**：队列满可丢 latency wake，但 Job 保持 durable 并由后续 scan 捡起，同时输出限频结构化诊断。
- **不变量**：跨进程互斥依靠 DB claim/index，而非进程 map。
- **验证**：concurrent claim and queue saturation recovery tests。

### ORCH-LEASE-001 条件 claim

- **前置**：Job pending 或到期 failed_retryable。
- **动作**：Store.Claim。
- **结果**：单条条件 UPDATE 转 running，生成唯一 leaseToken/executionId、attempts+1、设置 owner/expiry 和首次 startedAt；竞争者 affected=0。请求准入时已持久化的 Job 属在途工作，claim 不重复执行“新请求”日志容量门禁。
- **失败**：未来 nextRunAt、终态或已 running 不可 claim。
- **不变量**：一个 Job 每次执行有新 executionId/token。
- **验证**：concurrent claim test。

### ORCH-LEASE-002 heartbeat 与 RPC 时间边界

- **前置**：Job 已 claim 并执行长 RuntimeReconcile。
- **动作**：每 leaseTTL/3 heartbeat Renew。
- **结果**：仅匹配 running+owner+token 才延长期限；合法长任务不被恢复器抢走。
- **失败**：renew 失败/ownership lost 后旧 worker 的 observation/succeed/fail 均被 fencing 拒绝。
- **不变量**：业务 RPC deadline 必须小于 lease TTL；pull image 例外仍须 heartbeat 覆盖其 15 分钟操作。
- **验证**：lease heartbeat/lost ownership tests。

### ORCH-CTRL-003 RuntimeReconcile 请求快照

- **前置**：claimed apply/stop/purge Job。
- **动作**：Controller 组装 RPC。
- **结果**：携带 job/execution/app/instance/server/action、desired generation/hash/revision、immutable runtime spec、removeData 和 previous container name。
- **失败**：apply 缺 revision 为 terminal `invalid_revision`；revision 读取失败为 retryable `revision_unavailable`；runtime 不可用为 retryable `runtime_unavailable`。
- **不变量**：优先使用 Job per-server desiredSpec，空时才回退 immutable revision runtime spec。
- **验证**：controller error classification/request capture tests。

### ORCH-CTRL-004 成功写回顺序

- **前置**：Agent 返回无错误 reconcile response。
- **动作**：Controller 收敛。
- **结果**：先以 token 写 Observation；再比较 Instance 最新 desired；未变才条件 Succeed，清 lease/error、保存 steps/stage/finishedAt 并调用 OnSucceeded。
- **失败**：observation rejected 或 succeed affected=0 返回 ownership lost；不得调用成功 hook。
- **不变量**：Job 成功绝不先于 observed 写回。
- **验证**：stale token、happy convergence tests。

### ORCH-CTRL-005 执行中 desired 变化

- **前置**：RPC 期间 Instance desired/revision/spec/force nonce 改变。
- **动作**：旧 RPC 返回成功。
- **结果**：旧 observation 仍仅在旧 lease 合法时记录，Job 随即 Requeue 为 pending，刷新到当前 Instance desired，stage=`superseded`、errorClass=`superseded`、清 lease/nextRun。
- **失败**：旧结果不得把 Job 标 succeeded 或覆盖最新 desired。
- **不变量**：purge finalizer 已删 Instance 时可将旧 purge 直接幂等 succeed；apply/stop 缺 Instance 仍视为 desired changed。
- **验证**：desired-change and purge finalizer tests。

### ORCH-RETRY-001 错误结构和分类

- **前置**：RuntimeReconcile 返回结构化错误或普通 error。
- **动作**：Controller fail。
- **结果**：保存 error_code/class/message/detail、last steps/stage；普通 error 且无 message 时正规化 `runtime_reconcile_failed/runtime` 并设 retryable。
- **失败**：不得把原始 Docker/Agent 诊断压成仅一条翻译文案或 secret/full env/file content。
- **不变量**：任务表不得反写 Job 错误或状态。
- **验证**：error propagation/trace redaction tests。

### ORCH-RETRY-002 backoff 与终止

- **前置**：Job 第 N 次失败，response retryable。
- **动作**：Fail。
- **结果**：进入 failed_retryable，nextRunAt 以 30s 为指数退避基数、上限 1h，并施加 ±20% jitter（结果仍不超过 1h）或尊重不超过 1h 的 RetryAfter；达到 MaxAttempts（默认总尝试10）改 terminal failed、`max_attempts_exceeded/retry_exhausted`。
- **失败**：永久错误不得无限重试；未到上限的 retryable 不得提前 terminal。
- **不变量**：attempts 包含首次执行。
- **验证**：controller max-attempts/backoff tests。

### ORCH-LEASE-003 过期租约恢复

- **前置**：running Job lease 已过期。
- **动作**：RecoverExpiredLeases。
- **结果**：未开始 stage 的恢复 pending；已有 stage 的恢复 failed_retryable 并按 attempts 退避，记录 `lease_lost/ownership`；清 owner/token/expiry。
- **失败**：更新必须再次以过期状态谓词 fence 已续租/重领 Job。
- **不变量**：逐 Job 计算 retry delay，不能批量使用同一错误 attempts。
- **验证**：lease expiry replay/stale snapshot tests。

### ORCH-DRIFT-001 漂移来源与修复范围

- **前置**：Agent report cache 显示 missing/stopped/failed、generation/hash 或 managed-file manifest 漂移。
- **动作**：5 秒 application_reconcile collector。
- **结果**：只把明确 drift 的 app/server 交给 Planner 创建/复用 Job；健康目标保持不动。
- **失败**：collector 不直接访问远端、不创建 `application_target_*` 任务、不以日志推断漂移。
- **不变量**：nil 容器快照不表示空；明确空 full snapshot 才可清空观测。
- **验证**：container drift/missing/nil-vs-empty tests。

### ORCH-DRIFT-002 应用级熔断与恢复

- **前置**：应用连续 Job 失败或连续健康观测。
- **动作**：OnFailed/健康记录/显式人工操作。
- **结果**：每次 retryable/terminal Job 失败累计 `application_reconcile_states` 并设置指数 backoff；达到10置 reconcile_stopped；自动扫描停止，人工同步/部署/重启清零；连续5次健康观测才清失败。
- **失败**：reconcile_stopped 不得改变应用 config updated_at；report drift 可绕过 next_run_at 只修明确节点，但不得等同 force 全量重部署。
- **不变量**：此表只保存应用级熔断，不能代替 Job attempts/nextRunAt。
- **验证**：backoff/stopped/healthy streak tests。

### ORCH-TRACE-001 协调追踪

- **前置**：运行设置 `reconcile.trace` 开/关。
- **动作**：执行 plan→claim→RPC→observation→retry/success/failure。
- **结果**：默认关闭；开启后可按 app/server/job/intent/execution 串联全部关键阶段和 lease 丢失。
- **失败**：不得记录 secret、完整 env 或敏感文件正文。
- **不变量**：追踪失败不得影响控制面事务和协调结果。
- **验证**：设置开关、字段存在与敏感值扫描。

## 5. 协调记录

### ORCH-REC-001 intent 聚合

- **前置**：同一触发 intent 下有多个 app/server Jobs。
- **动作**：GET `/api/v1/application-operations`。
- **结果**：按 intent_id 一条记录，一个 Job 一个 target，列表支持合法筛选/分页；直接聚合 AppDB，不建投影表。
- **失败**：隐藏 facility application/identity 排除；不得读取 CoordDB lifecycle 或 runtime events。
- **不变量**：Job/Instance 不随 task cleanup 删除。
- **验证**：records exclude hidden/aggregation tests。

### ORCH-REC-002 详情、阶段和可空时间

- **前置**：Job 有 last_steps_json、结构化错误和可空时间，Instance 可能缺失。
- **动作**：GET operation detail。
- **结果**：targets 由 Job+Instance observed 组成，stages 由 steps 派生；无 Instance 显示 unknown；空时间列按 NULL/未设置处理。
- **失败**：存量空字符串时间不得导致 500；不得凭 task log 合成 stage。
- **不变量**：ORM 启动归一化 blank nullable times 为 NULL。
- **验证**：legacy empty time/operation detail tests。

### ORCH-ACT-001 结构化失败摘要与搜索

- **前置**：同一 operation 的错误事件同时含稳定 trigger/reason 文本与 `data.error/detail/errorCode`。
- **动作**：重建 Activity 查询投影、列出 operation 或搜索错误。
- **结果**：failure summary 依次使用 error、detail、errorCode；三个字段均进入 FTS，原始只追加事件保持不变。
- **失败**：不得把 `application_sync`、`agent_report` 等触发原因或普通 Event.Text 冒充真实失败；投影失败不得改写 AppDB 原始事实。
- **不变量**：投影 schema 升级只清理并重建 LogDB 可重建索引；结构化错误沿用写入时脱敏结果。
- **验证**：structured failure summary、redaction、error-code search、projection rebuild tests。

### ORCH-ACT-002 观测拒绝的用户可见边界

- **前置**：ObservationWriter 因普通陈旧 report 或 durable Job lease/fencing/实例异常拒绝写入。
- **动作**：生成并展示 Activity 事件。
- **结果**：普通无 Job CAS miss 不生成 warning；durable Job 拒绝使用明确 reason，关联 operation/execution 与资源，界面说明该事件不是部署重试并将原始 JSON 留在折叠技术信息中。
- **失败**：不得用 `stale_or_ownership_lost` 混淆新事件的陈旧与所有权原因，也不得让一轮多实例扫描线性制造 warning。
- **不变量**：历史旧 reason 仍可读；真正 lease/fencing 异常不得因降噪而丢失。
- **验证**：observation writer rejection、activity display-message tests。

## 6. 通用任务注册与创建

### TASK-REG-001 类型注册

- **前置**：业务模块声明任务 Definition。
- **动作**：生产 bootstrap 集中 RegisterTasks。
- **结果**：type 非空且唯一；定义携带 summary/hidden/capabilities/cancel/retry/concurrency/stale/execute/hooks/periodic；重复注册冲突。
- **失败**：未注册类型不能创建或执行，不能直接写 tasks 表。
- **不变量**：tasks 内核不维护业务 type switch；业务 `tasks.go` 使用具名 Execute/collector，禁止大段匿名编排。
- **验证**：registry/manager unregistered tests、debug snapshot review。

### TASK-REG-002 capability 一致性

- **前置**：定义声明 AllowRunNow/AllowRetry/DisallowCancel。
- **动作**：详情/列表装饰权限或调用操作。
- **结果**：无 Execute 的定义即使误声明也返回不可 run/retry；AllowRetry+Execute 缺 max 时默认3次重试；DisallowCancel 返回 allowCancel=false。
- **失败**：前端不得维护 type 白名单；删除服务器也不能取消 DisallowCancel 任务。
- **不变量**：能力唯一来源为当前 registry definition。
- **验证**：handler capability/cancel tests。

### TASK-CREATE-001 单任务创建

- **前置**：注册类型和合法 CreateInput。
- **动作**：Manager.Create。
- **结果**：持久化 operation/trigger/resource/params/metadata/status/retry/schedule/concurrency；活跃并发复用按定义返回 `created=false`。
- **失败**：生产业务不得绕过 Manager 调 Service.Create。
- **不变量**：任务含本次执行全部变量；内存 PeriodicTrigger.Payload 不自动落库。
- **验证**：manager create/exclusive tests。

### TASK-CREATE-002 batch 操作建模

- **前置**：一次操作有多个变量不同目标。
- **动作**：CreateBatch/CreateBatchAndRun。
- **结果**：父子共享 operation_id；子任务保留各自 type/input/definition，支持 serial/parallel；ForceParent 可在单子任务时保留父汇总。
- **失败**：父 type 不得覆盖混合子 type；调用方不得把禁止并行任务硬拆 goroutine。
- **不变量**：应用部署目标例外使用 AppDB Jobs，不创建 application target children。
- **验证**：batch/mixed type/force parent tests。

### TASK-CONC-001 并发策略

- **前置**：定义 parallel/resource-exclusive/resource-queue/global/custom。
- **动作**：计算 concurrency key 并创建/运行。
- **结果**：显式 input key 优先；queue/exclusive 的 callback 次之；再退回 type+resource；global 为 type；parallel 无 key。
- **失败**：需要跨动作同资源串行的业务不得因 type 不同落入两个默认 key，应声明 custom key。
- **不变量**：exclusive 复用/拒绝后项，queue 创建全部并 FIFO 执行，二者不得混淆。
- **验证**：custom callback/exclusive/queue tests。

### TASK-CONC-002 资源队列公平与超时

- **前置**：同 key 多个 queued/scheduled/retryable。
- **动作**：Manager.Run/worker due scan。
- **结果**：只队首可运行，按 created ASC；wait 最多5分钟、每250ms进程内缓存检查，超时保持原状态交后续轮次。
- **失败**：后项不得越过 active 队首；单个阻塞 key 不得拖死 worker 整轮。
- **不变量**：队首缓存仅单进程优化，所有终态/cancel/expire/orphan/cleanup 都使缓存失效并回填。
- **验证**：queue order/cache handoff/concurrent tests。

## 7. 任务执行与状态

### TASK-RUN-001 execution registry 生命周期

- **前置**：queued/scheduled/retryable task 准备执行。
- **动作**：Manager.Run/Service.Start。
- **结果**：进入 running 前原子注册 context/cancel；start 对已活跃 execution 幂等；completed/failed/retryable/blocked/cancelled 后注销。
- **失败**：数据库终态写失败且仍 running 时 `FinishExecution` 保留 registry，避免 orphan 误判。
- **不变量**：同 task 不得有两个活跃 execution。
- **验证**：execution lifecycle/idempotent start tests。

### TASK-RUN-002 executor 完成约定

- **前置**：Definition.Execute 存在。
- **动作**：Manager.Run 调用 executor。
- **结果**：BeforeStart→Execute→完成/失败 hook 顺序稳定；Execute nil 表示任务已完成或由 executor 写入终态，Manager 在仍 running 时完成。
- **失败**：executor 不得只启动未注册后台 goroutine后返回 nil。
- **不变量**：终态后任何完成/失败/重试写入不覆盖原终态。
- **验证**：hook/lifecycle/terminal immutability tests。

### TASK-RUN-003 panic 隔离

- **前置**：单 executor、worker 唤醒、CreateAndRun 或 parallel child panic。
- **动作**：执行边界 recover。
- **结果**：panic 转为该 task Fail 并写日志，其它 worker/children/进程继续；parallel 错误通道按最多发送数容量并非阻塞发送。
- **失败**：不得死锁或使整个 Panel 崩溃。
- **不变量**：父汇总仍准确计入失败 child。
- **验证**：recovery/panic/no-deadlock tests。

### TASK-RUN-004 父任务汇总

- **前置**：父任务含 completed/failed/retryable/blocked/cancelled children。
- **动作**：RunParent/RunChildren。
- **结果**：completed child 幂等跳过；任一失败类/blocked/cancelled 使父失败；全部完成才 completed。
- **失败**：不得因重复触碰 completed child 的 `not runnable` 把父误标失败。
- **不变量**：操作内任务数按具体 children；父作为汇总行独立可查看日志。
- **验证**：completed-child replay/aggregate tests。

### TASK-RUN-005 可重试失败与 blocked

- **前置**：executor 返回可重试/阻断错误。
- **动作**：FailRetryable/Block。
- **结果**：分别进入对应终态，保存 error、retryCount/maxRetries/nextRunAt 或阻断原因；到期 retryable 可再 claim。
- **失败**：达到任务定义 max retries 后不得继续自动运行。
- **不变量**：通用任务 retry 次数定义为首次执行后的重试数，与 Job 总 attempts 语义不同。
- **验证**：service retry-state tests。

### TASK-CANCEL-001 显式取消与服务器删除

- **前置**：可取消 queued/scheduled/retryable/running tasks，部分任务 DisallowCancel。
- **动作**：Cancel 或 CancelByServer。
- **结果**：可取消任务标 cancelled，running context 被 cancel，registry/cache 清理；CancelByServer 逐任务写 deduped `task.cancelled` system event。
- **失败**：不可取消任务保持原状态；终态任务不被改写。
- **不变量**：服务器删除覆盖 queued/scheduled/failed_retryable/running。
- **验证**：cancel/skip disallowed/event tests。

## 8. 步骤与日志

### TASK-STEP-001 步骤 upsert

- **前置**：任务 running，业务拆分长操作阶段。
- **动作**：UpsertStep(task, step/status/percentage/metadata/error)。
- **结果**：同 task+step 更新同一记录，保留 started/finished 语义，百分比与 task stage 可供 UI 展示。
- **失败**：不存在 task 或非法阶段不产生孤儿步骤。
- **不变量**：步骤 metadata 存诊断字段；应用 Job steps 存于 Job，不得双写为部署事实。
- **验证**：task operation metadata/steps tests。

### TASK-LOG-001 增量日志

- **前置**：任务存在且未阻止追加。
- **动作**：AppendLog 与 `GET /tasks/{id}/logs?after=`。
- **结果**：每条有单调 cursor/time/stream/line；API 仅返回 cursor 后日志和 nextCursor；长操作持续输出可读进度。
- **失败**：已终态任务的误导性 Fail 不追加失败行；不存在 task 返回稳定错误。
- **不变量**：每任务最多1000条，超出滚动删最旧；单行最多8192字符并截断。
- **验证**：lifecycle/log cursor/cap/truncate tests。

### TASK-LOG-002 运行事件不复制日志正文

- **前置**：tasks service 注入 runtime EventWriter。
- **动作**：任务生命周期或日志相关写入。
- **结果**：只写稳定英文摘要/引用性质的简明系统日志；不得复制完整 task log body 或 params/metadata。
- **失败**：runtime event 写失败不得改变任务结果。
- **不变量**：当前不再写 `log.attached`；事件系统不是日志全文存储。
- **验证**：event payload absence tests。

## 9. 手动运行与重试 API

### TASK-MAN-001 run-now 权限与异步响应

- **前置**：任务定义有 Execute+AllowRunNow，任务处于可运行状态。
- **动作**：POST `/tasks/{id}/run-now`。
- **结果**：立即 202 返回装饰后的 task snapshot，后台经 Worker.RunNow→Manager.Run 执行完整 lifecycle。
- **失败**：无 executor/能力或状态不合法返回稳定错误；后台启动失败且任务仍非终态时标 failed。
- **不变量**：HTTP 不同步占用到长任务结束；不得直接调用 Definition.Execute。
- **验证**：handler run-now dispatch/reject tests。

### TASK-MAN-002 retry 克隆语义

- **前置**：failed/failed_retryable 且 definition Execute+AllowRetry，未超过限制。
- **动作**：POST `/tasks/{id}/retry`。
- **结果**：创建新 task 并立即异步执行；保留 params_json、必要 metadata、schedule/execution context、资源和 trigger 定位；关联 triggerTaskId/triggeredBy。
- **失败**：非失败状态、不支持或超限拒绝；启动前错误把新 task 标 failed，不留永久 queued。
- **不变量**：不自动复用旧 parent/batch 归属，避免挂到已结束操作。
- **验证**：retry preserves params/handler dispatch tests。

### TASK-MAN-003 前端能力与刷新

- **前置**：任务列表/详情返回 allowRunNow/allowRetry/allowCancel。
- **动作**：任务中心呈现操作并执行。
- **结果**：仅按响应字段展示；操作后主动刷新；无后台自动轮询（若恢复必须用户可控）。
- **失败**：旧请求/旧任务日志不得覆盖当前筛选、选择或 cursor。
- **不变量**：`/tasks` 仅兼容入口，不成为新应用协调记录入口。
- **验证**：task model/API stale-response tests。

## 10. 周期调度与 worker 恢复

### TASK-PER-001 周期 collector 决策

- **前置**：Definition.Periodic 指定 interval/CollectInputs。
- **动作**：首个 interval tick 或 TriggerPeriodicNow。
- **结果**：`shouldRun=false` 不创建 task/log；true 将 collector 输出交 Manager；启动后首轮延迟至第一个 tick，不在重启瞬间齐发。
- **失败**：Execute 不得重新扫描本轮资源列表；collector-only definition 可无 Execute。
- **不变量**：多个周期任务共用 NewIntervalCollector 节流，只有成功产出推进其时间。
- **验证**：periodic false/collector-only/interval tests。

### TASK-PER-002 立即触发 payload

- **前置**：业务调用 TriggerPeriodicNow，携带 PeriodicTrigger payload/资源定位。
- **动作**：运行 collector。
- **结果**：payload 原样作为内存上下文提供；collector 产出的 CreateInput 才持久化；创建与执行仍走 Manager 并发规则。
- **失败**：不得手写 goroutine 绕过任务框架。
- **不变量**：scheduler tick 使用 Type=scheduler 空 payload；manual run-now 不调用 collector 自动补参数。
- **验证**：payload pass-through tests。

### TASK-PER-003 应用协调 collector 特例

- **前置**：`application_reconcile` 周期/显式触发。
- **动作**：CollectInputs。
- **结果**：生产路径调用应用 Planner 创建/合并 AppDB Jobs；若仅规划或无输入则不创建 task record。
- **失败**：不得注册部署 Execute、暴露 run-now/retry 或返回 `application_target_apply/stop/purge`。
- **不变量**：generic Manager 不能在 planner 失败时创建替代 target task。
- **验证**：collector-only/no task inputs tests。

### TASK-WORK-001 due 队列兜底

- **前置**：存在 queued/scheduled/到期 failed_retryable 且 definition 有 Execute。
- **动作**：worker 每30秒扫描。
- **结果**：每类按 created ASC 最早50条，且仅并发队首经 Manager.Run；业务即时 dispatcher 仍应创建后主动启动以满足低延迟。
- **失败**：不得创建/持久化 `task_queue_drain` 或直接 Execute。
- **不变量**：未来 nextRunAt 不被提前运行。
- **验证**：worker due/no internal task/future retry tests。

### TASK-WORK-002 stale queued 清理

- **前置**：definition 声明 StaleQueuedAfter，存在旧 queued/scheduled。
- **动作**：worker 每30秒 ExpireStaleQueued。
- **结果**：仅选定 task types、created 超时且 nextRunAt 已到/空的真正孤儿标 failed；scheduled 也覆盖。
- **失败**：未来调度不清理；同 key 有更早 queued/scheduled/running/retryable 时后项是合法等待，不得清理。
- **不变量**：未声明 StaleQueuedAfter 的类型不参与。
- **验证**：stale selected/scheduled/future/head-block tests。

### TASK-WORK-003 orphan running 恢复

- **前置**：数据库 running task，当前进程 execution registry 可能有/无。
- **动作**：启动及每30秒 FailRunningWithoutExecution。
- **结果**：无 registry 的 running 立即 failed/orphaned；仍被追踪的保持 running。
- **失败**：远端不可取消维护任务是否继续由业务定义决定，但本地记录不得永久假 running。
- **不变量**：一次性内存 worker 在 API 返回前必须先标 running。
- **验证**：orphan worker/registry tests。

### TASK-CLEAN-001 历史保留

- **前置**：terminal tasks finishedAt 超过24小时，包含 steps/logs；同时 AppDB 有 active Jobs/Instances。
- **动作**：CleanupWorker 每小时分批清理。
- **结果**：只删除过期 tasks/steps/logs；活跃/未到期任务保留。
- **失败**：不得删除 AppDB application revision/job/instance 或协调记录事实。
- **不变量**：tasks `(status,next_run_at)` 索引支持扫描；失败/可重试/blocked/cancelled/completed 均属 terminal 历史。
- **验证**：CleanupRetained tests 与 AppDB row count 断言。

## 11. 任务列表与兼容 UI

### TASK-LIST-001 参数校验和搜索

- **前置**：任务覆盖多状态/type/server/operation。
- **动作**：GET `/tasks` 携带 camelCase filters、重复 status/type、q、分页。
- **结果**：q 在 DB 对 id/summary/type/error LIKE 跨页过滤并转义 `%_\`；多值筛选取集合；返回 `items/total/page/pageSize` 且空 items 为 `[]`。
- **失败**：未知参数、limit、snake_case、非法页码/时间返回400。
- **不变量**：HTTP 使用 ListSummaries，不加载 params_json/metadata_json 或 deployment projection；内部恢复才可用完整 List。
- **验证**：handler parser/query escaping/list summary tests。

### TASK-LIST-002 operation 分页

- **前置**：批量父子任务跨页。
- **动作**：`operationPage=true`。
- **结果**：后端先按 operation_id（空则 task ID）去重分页，再返回该页全部任务；前端按操作聚合。
- **失败**：不得先分页 task 再前端折叠导致页面操作数缩水。
- **不变量**：父汇总行置于子任务前；操作数与 child 数语义分开。
- **验证**：operation-page pagination/model tests。

### TASK-LIST-003 常用与全部类型

- **前置**：包含 hidden/internal 与 scheduler-triggered tasks。
- **动作**：commonOnly/includeInternal/type 筛选。
- **结果**：常用默认排除 scheduler；所有类型模式保持 includeInternal=true 且不发送具体 type；显式筛选可定位隐藏类型。
- **失败**：清空控件产生 null/空白不得发送无效参数或意外退回 commonOnly。
- **不变量**：持久化 type/status/stage 为稳定标识，显示时再翻译；英文 summary 不耦合存储语言。
- **验证**：hidden/common filters/front-end normalize tests。

## 12. 系统日志（runtime events）

### EVENT-WRITE-001 简明事件模型

- **前置**：任务状态变化或 Agent 连接状态转换。
- **动作**：模块调用 EventWriter.Log。
- **结果**：仅写 id/type/category/severity/source/sourceModule/dedupeKey/稳定英文 summary/occurredAt；当前新写 category 仅 application/task/system，事件类型为 task created/started/completed/failed/retried/cancelled 和 agent connected/disconnected。
- **失败**：不得写 payload、任务/目标引用、完整日志或关联对象；应用 operation 事件已停写。
- **不变量**：仅 Agent 状态转换写事件，重复心跳不刷屏。
- **验证**：write/list/event producer tests。

### EVENT-WRITE-002 非阻塞缓冲与批量落库

- **前置**：生产 bootstrap 注入 BufferedWriter。
- **动作**：模块 Log，writer 每5秒 flush 或 Stop。
- **结果**：Log 非阻塞入内存；flush 一事务 WriteBatch `INSERT OR IGNORE`；Stop 刷剩余且未 Start 时也立即返回。
- **失败**：buffer 满允许丢该日志，绝不能阻塞/回滚业务；重复 dedupe key 不重复落库。
- **不变量**：事件是 best-effort 诊断，不升级为业务事务一部分。
- **验证**：buffered writer/full buffer/dedupe/stop tests。

### EVENT-LIST-001 列表接口

- **前置**：存在多 category/severity/type/source/time 事件。
- **动作**：GET `/system-events`。
- **结果**：支持 category/source/severity/eventType/from/to/page/pageSize；时间 RFC3339Nano；返回分页列表，页面只展示时间/级别/类型/内容/来源。
- **失败**：未知参数或非法时间400；无详情 API/查看按钮。
- **不变量**：summary 前端按当前语言 `translateEventSummary`，数据库保持稳定英文。
- **验证**：handler query/list filter/UI model tests。

### EVENT-CLEAN-001 保留清理

- **前置**：事件早于 runtimeEventRetentionDays。
- **动作**：CleanupWorker/Service.Cleanup。
- **结果**：按 occurred_at 删除过期 runtime_events，未过期保留。
- **失败**：`runtimeEventDetailRetentionDays` 不得驱动已删除的应用 stage 清理。
- **不变量**：`runtime_event_details` 表仅兼容保留，不读写；事件清理不影响 tasks/jobs/instances。
- **验证**：cleanup tests 与跨库隔离断言。

## 13. 跨域任务不变量

### CROSS-TASK-001 Agent 就绪门禁

- **前置**：任务依赖 Agent，但节点无 URL、不可达或 status 非 compatible。
- **动作**：周期 collector 或 executor。
- **结果**：周期输入跳过当前资源；已执行任务按当前 Agent 错误失败/重试，并可通知服务器模块处理 mTLS 证书受限重装。
- **失败**：不得回退 SSH 执行应用、容器、指标、软件包或防火墙业务（仅 Agent bootstrap/repair/cert recovery 例外）。
- **不变量**：Panel 与 Agent 构建版本完全一致才 compatible。
- **验证**：各业务 collector/agent error handler tests。

### CROSS-TASK-002 资源写与刷新任务分工

- **前置**：容器/镜像/卷操作或镜像/网络/卷刷新。
- **动作**：执行 API。
- **结果**：普通资源突变在每服务器队列同步完成，不创建 operation task；image/network/volume refresh 以异步任务原子替换快照；应用镜像升级通过任务记录后只更新应用 desired 并交编排。
- **失败**：不得把同步容器动作偷偷变为无反馈后台任务，或让 GET 同步联系 Agent。
- **不变量**：通用 task 完成不代表应用 Job/runtime 收敛完成。
- **验证**：container/task cross-module tests。

## 14. 覆盖来源与缺口

主要取证来源：

- `docs/agents/modules/tasks-scheduler.md`、`applications.md`、`containerization.md`、`runtime-events.md`
- `internal/orchestrator/{model,state,planner,store,controller,observation,revision,backoff,trace}.go`
- `internal/modules/tasks/`、`internal/modules/runtimeevents/`、`internal/modules/applications/records.go`、`internal/modules/containers/tasks.go`
- 对应 `*_test.go` 与 `internal/integration/appdeploy/` 全链路场景
- `web/src/api/tasks.ts`、`systemEvents.ts`，`web/src/types/tasks.ts`、`systemEvents.ts` 与任务页面 model

已知缺口：

- 多进程共享 SQLite 下通用任务 `FirstActiveByConcurrencyKey` 缓存不提供一致性；当前合同只承认单进程优化，若引入多实例部署必须增加数据库级任务 claim/fencing 设计。
- runtime events 的满缓冲丢弃计数/可观测告警、批量失败重试策略当前未形成稳定对外合同；不得把日志必达作为验收前提。
- 任务中心为兼容页面且没有后台自动轮询；其响应竞态、窄屏和无障碍行为仍缺少完整端到端覆盖。
- `runtimeEventDetailRetentionDays` 和 `runtime_event_details` 是无效兼容遗留；在迁移明确删除前，不得重新赋予业务含义。
- application runtime DTO 仍保留 lifecycle 命名兼容类型，但旧 CoordDB 表、dispatcher、deployment projection 已删除；后续改动不得据此恢复旧路径。
