# 稳定应用编排子系统设计（v3）

> 本文档是 `orchestrator-design.md` 的改进版。原文档保留作为历史方案参考；后续设计和实现以本文档为准。
>
> 目标不是模拟 Kubernetes 的全部能力，而是借鉴其控制器、期望态/观测态、幂等收敛、租约恢复和声明式删除思想，建立一个稳定、可恢复、可追踪的单进程应用部署子系统。

---

## 1. 稳定性优先的取舍

### 1.1 必须保证

- 期望态和待执行工作不能因为队列丢失、Panel 重启或 Worker 崩溃而丢失。
- 远端操作按至少一次调用设计；RPC 超时后允许重放，重放必须幂等。
- 同一 `(application_id, server_id)` 同时最多一个远端协调执行者。
- 旧 Worker、旧租约和旧 Agent 观测不能覆盖新状态。
- 每次触发、每次执行尝试、每个失败步骤都可以追踪。
- Panel 和 Agent 恢复后，系统能够通过重扫自动收敛，不依赖人工修复数据库。

### 1.2 明确不保证

- 跨服务器事务和分布式 Panel。
- Docker 操作 exactly-once；系统只保证至少一次调用加幂等结果。
- 可变 image tag 在不同时间代表同一镜像内容。
- move 具备零停机和跨节点原子性。

### 1.3 核心对象

控制面只保留三个核心对象：

```text
Application  应用级期望配置和部署目标集合
Instance      application/server 的期望态与观测态
Job           application/server 当前尚未完成的收敛工作
```

执行尝试、状态迁移和步骤诊断写入 runtime events 或等价的追加式事件记录，不再创建 operation/target/stage 三层控制对象。

---

## 2. 总体架构和数据库边界

```text
保存/删除/停止/重启/镜像/回滚/恢复/迁移
Agent report/周期巡检/Panel 启动/租约恢复/服务器变化
                              │
                              ▼
                  Desired State + Planner
                    （只写期望和 Job）
                              │ AppDB
             ┌────────────────┴────────────────┐
             ▼                                 ▼
       applications                       instances
       期望配置/版本                    desired + observed
                              │
                              ▼
                    Orchestrator Controller
              scan → claim → reconcile → writeback
                              │ mTLS
                              ▼
                    RuntimeReconcile Agent RPC
                              │
                              ▼
                         Agent / Docker
                              │
                              ▼
                 ObservationWriter + Events
```

### 2.1 数据库归属

新的控制面表必须放在 `AppDB`：

```text
AppDB:
  applications
  application_instances
  jobs
  application_reconcile_states
  application_revisions（若现有 revision 机制保留）

LogDB:
  tasks
  runtime_events
  运行日志和事件摘要

CoordDB:
  旧 lifecycle 表，仅在迁移/下线阶段使用
```

原因是以下写入必须在同一个事务中提交：

```text
Application desired state
Instance desired state
Job 创建或合并
```

不能依赖跨数据库事务、内存队列或任务表来保证这组写入的一致性。提交事务后再发送 wake 信号；wake 丢失由数据库扫描修复。

### 2.2 模块职责

#### applications

- 保存、校验和渲染应用期望配置。
- 生成不可变 revision。
- 在 AppDB 事务内更新 Application、Instance desired 并调用 planner。
- 不直接写 Job SQL，不执行 Agent/Docker RPC。

#### orchestrator

- 拥有 Job 和 Instance 的全部 SQL。
- 实现 planner、状态转移、claim、租约、退避、恢复和写回。
- 实现统一 `ObservationWriter`。
- 是唯一允许调用 RuntimeReconcile 的模块。

#### containers

- 接收 Agent report 和周期巡检结果。
- 把报告交给 ObservationWriter。
- 识别漂移后调用 planner；不直接修复远端、不直接写 Instance。

#### tasks

- 提供用户操作、周期触发和兼容投影。
- 不再拥有应用部署的远端 executor。
- 任务表不是部署事实来源。

#### agent/client 与 agent/docker

- client 负责 transport、超时和协议转换。
- docker 负责 RuntimeReconcile 的幂等 ensure、托管资源保护和诊断。

#### runtime events

- 记录 intent、Job 状态迁移、execution、步骤摘要和错误摘要。
- 只用于审计和排错，不参与调度决策。

---

## 3. 数据模型

### 3.1 Application

Application 是应用级期望的唯一事实来源，至少包含：

```text
enabled
deletion_requested
deployment_mode
deployment_server_ids
generation
spec_hash
```

现有 `job_id`、`last_deployment_id`、`last_error` 如果为了 API 兼容保留，只能作为投影缓存，不能被 orchestrator 用来判断是否健康或是否需要执行。

### 3.2 Instance：明确拆分 desired 和 observed

建议字段分组如下：

```text
identity:
  id, application_id, server_id

desired:
  desired_state                 running | stopped | purged
  desired_generation
  desired_spec_hash
  desired_revision_id
  desired_spec_json（必要时保存安全快照）

observed:
  observed_state                running | stopped | missing | failed | unknown
  observed_container_name
  observed_container_id
  observed_generation
  observed_spec_hash
  observed_image_digest
  observed_at
  observed_sequence
  observed_source               reconcile | agent_report | inspection

diagnostic:
  last_reconcile_job_id
  last_error_code
  last_error_class
  last_error_message
  last_error_detail
  last_error_at
```

`deploying` 不写入 Instance，由 API 根据“存在 active Job 或 desired 与 observed 不一致”派生。所有 observed 写入都必须经过 ObservationWriter；业务模块只能读。

### 3.3 Job：当前协调工作，不是操作历史

同一 `(application_id, server_id)` 最多一个 active Job。新触发到来时合并或更新当前 Job，不创建重复 active Job。

```text
id, application_id, server_id, instance_id
action                         apply | stop | purge
desired_generation
desired_spec_hash
desired_revision_id
remove_data
force_nonce                    重启/强制重部署时变化

state                          pending | running | succeeded |
                               failed_retryable | failed | cancelled
priority
attempts
next_run_at

lease_owner
lease_token
lease_expires_at
execution_id

intent_id
trigger_type
trigger_resource_type
trigger_resource_id
reason
idempotency_key

last_stage
last_steps_json
error_code
error_class
error_message
error_detail
created_at, started_at, finished_at, updated_at
```

索引：

```sql
CREATE UNIQUE INDEX uq_jobs_active_app_server
ON jobs(application_id, server_id)
WHERE state IN ('pending', 'running', 'failed_retryable');

CREATE INDEX idx_jobs_due
ON jobs(state, next_run_at, priority, created_at);

CREATE INDEX idx_jobs_intent
ON jobs(intent_id, created_at);
```

Job 不使用 `ON DELETE CASCADE` 删除 Application；删除必须走 finalizer。

### 3.4 不可变 revision

Worker 不得直接读取可变 Application spec 执行。每次影响运行时的期望变更都生成不可变 revision，Job 保存 `desired_revision_id`。

Revision 至少包含：

```text
application_id, generation, spec_hash
rendered_runtime_spec
managed_file_manifest
image_reference
resolved_image_digest（如果已解析）
created_at
```

敏感内容使用安全存储引用或脱敏快照，不能写入错误详情、任务参数或 runtime event。

---

## 4. 统一触发模型

所有入口归一到：

```text
ReconcileRequest {
  application_id
  server_ids（可选）
  action_override（可选）
  trigger_type
  trigger_resource_type
  trigger_resource_id
  reason
  force
  remove_data
  idempotency_key
}
```

触发器只能做两类工作：

```text
Intent：修改 desired state，然后确保 Job 存在
Repair：不修改 desired state，只根据 observed/lease/数据库修复确保 Job 存在
```

### 4.1 期望态触发

| 触发 | desired 变化 | Job | 优先级 | 规则 |
| --- | --- | --- | --- | --- |
| 创建/保存 | 创建或递增 generation/revision | apply | 100 | Application、Instance desired、Job 同事务 |
| 启用 | running | apply | 100 | 清除自动熔断 |
| 停用 | stopped | stop | 200 | 可替换未开始的 apply |
| 删除 | deletion_requested=true，purged | purge | 300 | 禁止新的 apply |
| 重启 | desired 不变，force_nonce 变化 | apply | 150 | 满足态也执行 |
| 镜像更新 | image revision 变化 | apply | 150 | 优先使用 digest |
| 回滚 | desired revision 改为旧 revision | apply | 150 | generation 仍单调递增 |
| 持久化恢复 | 恢复结果进入新 revision | apply | 150 | 不临时修改旧 spec |
| 增加目标节点 | 创建 running Instance | apply | 100 | 与目标集合变更同事务 |
| 移除目标节点 | purged | purge | 300 | 全局补齐清理 |

### 4.2 用户操作触发

| 触发 | 规则 |
| --- | --- |
| 手动重试 | 指定 Job 立即 pending，不修改 desired |
| 强制重部署 | 更新 force_nonce，不绕过 active Job 唯一性 |
| 手动停止/清理 | 更新 desired 并合并 stop/purge Job |
| 取消 | 只能取消未进入远端执行的 pending Job |
| 恢复协调 | 清除应用级熔断，并为不一致 Instance 触发 repair |

停止、清理和取消不能依赖任务中心的 task 行作为事实来源。

### 4.3 观测触发

| 观测 | 处理 |
| --- | --- |
| 容器缺失/停止 | desired=running 时 apply；desired=stopped 时只更新 observed |
| generation/spec hash 漂移 | 确保 apply Job，绕过应用级退避但不绕过 active Job |
| managed file 漂移 | 确保 apply Job |
| image digest 漂移 | 确保 apply Job |
| 非托管同名容器 | 不删除，记录 terminal conflict |
| Agent 恢复在线 | 触发该服务器 repair scan |
| 旧 report | 丢弃，不触发新 Job |

### 4.4 系统修复触发

| 触发 | 处理 |
| --- | --- |
| Panel 启动 | 恢复过期 lease，并全量扫描 desired/observed 差异 |
| Worker 崩溃 | lease 到期后重新入队 |
| RPC 超时 | 按结果未知处理，retryable 重放完整 reconcile |
| wake 丢失/队列溢出 | DB due scan 自动补回 |
| Job 到期 | failed_retryable → pending |
| lease 到期 | 失效旧 token，重新排队；不依赖 stage 猜测远端状态 |
| 数据库迁移完成 | 执行一次 repair scan |
| Server 恢复 | 扫描该服务器不一致 Instance |

### 4.5 Move

Move 不能简单拆成两个相互独立的 Job。稳定方案必须具备持久化依赖：

```text
数据传输/恢复成功
        ↓
目标 apply 成功并 inspect
        ↓
源 purge
```

如果 v1 不支持 Job 依赖，则 move 必须列为非目标，不能隐式执行“目标 apply + 源 purge”。

---

## 5. Planner 规则

### 5.1 期望变更事务

所有期望态入口在一个 AppDB 短事务内完成：

```text
读取 Application
计算完整目标集合
生成不可变 revision
更新 Instance desired
upsert/merge Job
提交事务
```

事务提交后才发送 wake。wake 失败不影响正确性，scanner 会补偿。

### 5.2 同键合并

| 当前 Job | 新触发 | 处理 |
| --- | --- | --- |
| pending | 任意 | 更新 action、revision、priority，保持 pending |
| failed_retryable | 任意 | 更新目标并立即 pending |
| running | 新 apply | 当前执行完成后读取最新 desired |
| running | stop/purge | 更新 desired，旧 Worker 不得覆盖新目标 |
| succeeded/failed/cancelled | 新触发 | 创建新的 active Job |
| failed | 手动重试 | 明确重置 pending，并生成新的 intent |

running 状态不创建第二个同键 active Job。执行写回后必须重新检查 Instance desired；如果目标已变更，立即创建或唤醒下一轮 Job。

### 5.3 幂等

外部请求使用 `idempotency_key`；重复请求返回相同 `intent_id`。Agent report 使用 report sequence，周期巡检使用稳定 repair key。重复触发只能合并工作，不能制造并发执行。

---

## 6. Job 状态机、租约和恢复

### 6.1 状态

```text
pending → running → succeeded
                  ├→ failed_retryable → pending
                  ├→ failed
                  └→ cancelled（仅未进入远端时）
```

只有 orchestrator repository 可以修改 state、lease、attempt、错误、stage 和完成时间。

### 6.2 Claim 与 fencing

Claim 是条件更新，并生成新的 `lease_token` 和 `execution_id`。所有 succeed/fail/observation writeback 都必须校验：

```text
job id
state=running
lease_token
```

旧 Worker 丢失租约后，即使 RPC 返回，也只能记录“ownership lost”，不能改变 Job 或 Instance。

### 6.3 恢复

任意 `running` 且租约过期的 Job 都可以完整重放 RuntimeReconcile。`stage` 只用于诊断，不用于决定远端是否已经执行。

恢复必须记录 `lease_lost` 事件，并清除旧 owner/token。

### 6.4 退避和熔断

Job 负责单实例 attempts/next_run_at：

```text
base=30s，上限=1h，jitter=±20%
```

`application_reconcile_states` 只负责应用级自动熔断，不重复维护 Job 的下次执行时间。真实漂移可以绕过熔断退避，但不能绕过 active Job 唯一性。

---

## 7. 协调循环

```text
1 个 scanner goroutine
固定 worker 池，默认 8
wake channel 只降低延迟
DB due scan 保证正确性
同 app/server 串行，跨键并行
```

Worker 流程：

```text
1. claim Job
2. 读取 immutable revision
3. 检查 deletion、desired generation、Agent compatibility
4. 在事务外调用 RuntimeReconcile
5. 以 lease token 写入 ObservationWriter
6. 以 lease token 写回 Job
7. 重新检查 desired 是否在执行期间变化
8. 必要时立即重新排队
9. 写入状态迁移和错误事件
```

Worker 不得在持有数据库事务时等待网络。RPC 必须有明确 deadline；deadline 必须小于 lease TTL，或实现 lease heartbeat。推荐两者同时具备：RPC deadline 防止无限占用，heartbeat 防止合法的长任务失去租约。

---

## 8. RuntimeReconcile

### 8.1 请求上下文

请求至少携带：

```text
job_id
execution_id
application_id
instance_id
server_id
action
desired_generation
desired_spec_hash
desired_revision_id
spec/revision 内容
remove_data
previous_container_name
```

这表示“本次尝试要收敛到哪个不可变目标”，不表示 exactly-once。

### 8.2 Apply ensure 顺序

```text
1. 校验 spec 和 managed identity
2. 写托管文件并生成 manifest
3. 确保镜像 identity，优先使用 digest
4. inspect 目标容器
5. 同名且托管、hash 相同：复用
6. 同名且托管、hash 不同：按 replace 策略停止并删除
7. 同名但非托管：拒绝删除并返回 terminal conflict
8. 创建容器
9. 启动容器；已运行视为成功
10. inspect，必须 running
11. 写 applied-state 标签和 manifest
12. 返回 observed snapshot、steps 和结构化错误
```

所有停止、删除和复用操作必须验证：

```text
managed=true
application_id
instance_id
```

### 8.3 Stop/Purge ensure

```text
stop:
  缺失/已停止 = 成功
  非托管同名资源 = 失败，不删除

purge:
  托管容器缺失 = 成功
  删除托管容器、运行目录和可选持久化目录
  非托管同名资源 = 失败，不删除
```

### 8.4 错误分类

Agent 返回：

```text
error_code
error_class
retryable
retry_after
failed_step
steps[]
observed_snapshot
```

错误类别至少包括：

```text
invalid_spec
non_managed_conflict
agent_unavailable
docker_unavailable
registry_unavailable
permission
timeout
container_start_failed
container_not_running
verification_failed
storage_failed
lease_lost
superseded
```

错误详情必须脱敏，不记录 secret、完整环境变量或敏感文件内容。

---

## 9. ObservationWriter 与漂移

所有以下来源统一调用 `ObservationWriter`：

```text
RuntimeReconcile response
Agent report stream
周期巡检
启动 repair scan
```

观测至少带有：

```text
instance_id
source
agent_report_sequence（如果有）
agent_observed_at
panel_received_at
observed_generation
observed_spec_hash
container_id
status
```

写入规则：

- 旧 report sequence 不得覆盖新观测。
- 没有 sequence 的周期观测不能覆盖已接受的更新 reconcile response，除非通过 CAS 确认仍是同一目标。
- 观测写回必须使用 Instance 版本或 compare-and-swap。
- 观测只更新 observed，不改变 Application desired。
- 漂移只确保 Job 存在，不直接执行修复。

漂移条件：

```text
desired_generation != observed_generation
desired_spec_hash != observed_spec_hash
desired running 且 observed 非 running
desired purged 且 observed 非 missing
managed file manifest 不一致
image digest 不一致
托管身份不一致
```

---

## 10. 删除、禁用和目标集合

### 10.1 删除 finalizer

```text
1. 设置 deletion_requested=true、enabled=false
2. 计算所有现有 Instance
3. 每个 Instance desired_state=purged
4. 创建/合并 purge Job
5. 禁止新的 apply/restart/image update
6. purge 成功后 observed=missing
7. 所有 Instance 无 active Job 后物理删除 Application
8. Job/events 按保留策略清理
```

不能用 `ON DELETE CASCADE` 删除 active Job。

### 10.2 禁用

禁用不等于删除：

```text
enabled=false
desired_state=stopped
```

重新启用时恢复 desired=running 并创建 apply Job。

### 10.3 目标集合变化

Planner 必须比较完整目标集合：

```text
desired - existing → apply
existing - desired → purge
intersection → 按 desired/observed 差异决定
```

来自单节点 report 的 scoped 触发也不能跳过已经离开目标集合的节点清理。

---

## 11. 可追踪性和 API 投影

每个触发有 `intent_id`，每次 claim 有新的 `execution_id`：

```text
intent_id → application/server → instance → job → execution_id
          → Agent request → steps → observation → final error/state
```

至少记录：

```text
reconcile_requested
job_created_or_merged
job_claimed
agent_reconcile_started
agent_reconcile_finished
observation_accepted
observation_discarded_stale
job_succeeded
job_retry_scheduled
job_failed
job_cancelled
lease_lost
job_superseded
```

操作记录按 `intent_id` 聚合，不能通过 created_at 相近或 trigger 猜测分组。任务中心可以保留兼容字段，但 task 行不是部署事实来源。

API 状态派生：

```text
running:  无 active Job 且 observed=running
stopped:  无 active Job 且 observed=stopped
missing:  无 active Job 且 observed=missing
deploying: 存在 active Job 或 desired != observed
failed:   最近 Job 为 terminal failed
blocked:  application_reconcile_states.reconcile_stopped=true
```

---

## 12. 启动、关闭和恢复

### 12.1 启动

```text
1. 打开数据库并完成迁移
2. 恢复过期 lease
3. 扫描 desired/observed 不一致
4. 修复 deletion finalizer
5. 启动 ObservationWriter
6. 启动 orchestrator scanner/workers
7. 启动 Agent report stream
8. 启动周期巡检
```

### 12.2 关闭

```text
1. 停止接受新的远端协调
2. 停止 scanner 发放新 Job
3. 等待有限时间让当前 RPC 返回
4. 不把 running Job 强行写成 succeeded
5. 进程退出后由 lease recovery 接管
```

### 12.3 崩溃不变量

任意 tick 点崩溃后只能出现：

```text
远端已达到目标 → 重放为幂等成功
远端未达到目标 → 重放继续收敛
结果未知 → retryable 重放
```

不能依赖内存队列、stage 或 Worker 正常退出作为正确性依据。

---

## 13. 失败策略

Job 执行退避：

```text
base=30s
上限=1h
jitter=±20%
```

Job 负责单实例 attempts/next_run_at；`application_reconcile_states` 只负责应用级自动熔断：

```text
consecutive_failures
reconcile_stopped
blocked_reason
last_failure_at
health_streak
```

两者不能重复维护 next_run_at 或 attempts。

失败分类不能只由操作名决定。错误应同时包含：

```text
error_code
error_class
retryable
retry_after
```

手动重试指定 Job；恢复协调才清除应用级熔断。漂移可绕过应用级退避，但不能制造重复 active Job。

---

## 14. 验收场景

### 14.1 事务和幂等

- Application 保存成功、Job 写入失败时，事务整体回滚。
- 同一 idempotency key 重复请求返回同一 intent。
- 同一 revision 重复 RuntimeReconcile 无破坏性副作用。
- RPC 已在 Agent 完成但 Panel 超时，重放后状态正确。
- purge 缺失资源和 stop 已停止资源均成功。

### 14.2 并发和恢复

- 两个 Worker claim 同一 Job 只有一个成功。
- 旧 lease token 不能写回 Job 或 Instance。
- Worker 在 Agent 每个步骤后崩溃，重启后最终收敛。
- Panel 启动恢复过期 lease。
- wake 丢失、队列溢出后 DB scan 补回工作。
- running 期间发生 stop/purge，旧 apply 不能覆盖新 desired。

### 14.3 资源安全

- 非托管同名容器永不自动删除。
- 托管标签不匹配时返回 terminal conflict。
- Application 删除期间不再产生 apply。
- 目标节点移除会全局产生 purge。
- move 没有依赖能力时明确拒绝或标记 unsupported，不执行两个独立 Job。

### 14.4 观测和排错

- 旧 Agent report 不覆盖新 reconcile response。
- 每次失败能定位到 intent、job、execution、failed_step 和 error class。
- 错误详情不包含 secret 和完整敏感配置。
- API 不依赖旧 lifecycle 表推导运行状态。

---

## 15. 实施顺序

### Step 0：固化不变量和数据库拓扑

确认控制面表放入 AppDB；先写模型断言、唯一 active Job、事务和 lease token 设计。

### Step 1：Revision 与 Instance desired/observed

先消除 desired/observed 混写，禁止业务模块直接写 observed。

### Step 2：RuntimeReconcile

完成 Agent contract、幂等 ensure、托管资源保护、错误分类和 observed snapshot。

### Step 3：Job repository 与 controller

完成条件 claim、lease token、恢复、退避、单键串行和事件记录。

### Step 4：Planner 接入所有触发入口

覆盖保存、删除、停止、启用、重启、镜像、回滚、恢复、目标集合变化、手动重试和漂移。

### Step 5：ObservationWriter 与 repair

接入 Agent report、周期巡检、启动扫描、旧观测拒绝和目标集合全局清理。

### Step 6：投影和旧实现下线

将 runtime、操作记录和任务中心切换到 Job/Instance/events，最后删除旧 lifecycle executor、表和兼容装配。

---

## 16. 最终不变量

1. DB 是期望和当前协调工作的唯一事实来源。
2. 触发器不执行远端 runtime mutation。
3. 只有 orchestrator worker 可以调用 RuntimeReconcile。
4. 同一 app/server 至多一个 active Job。
5. 所有 Job 写回都校验 lease token。
6. 所有 Instance observed 写入都经过 ObservationWriter。
7. Worker 执行 immutable revision，不读取半途变化的 Application spec。
8. Agent 按至少一次调用设计，不能假设 exactly-once。
9. 非托管资源永不自动删除。
10. Application 删除必须等待 purge finalizer。
11. 队列只负责低延迟唤醒，不能承载正确性。
12. 所有失败具备结构化 error code、error class、步骤和 execution_id。
13. move 必须具备持久化依赖，或明确不支持。
14. API 状态从 Application、Instance、Job 派生，不能读取旧 lifecycle 状态。

