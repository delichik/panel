# 新编排服务：详尽设计与实现计划（v2）

> 状态：已拍板——就地重写；编排核心 + 节点接口全新设计；干净起点（不迁移旧部署数据）。
> 节点协议选型 A（RuntimeReconcile 整段收敛）；漂移检测保留 agent 上报流 + 周期巡检。
> 本文件是唯一权威设计与实施依据。实现前先评审本文件。

---

## 1. 目标与非目标

### 1.1 目标
- **轻量**：对象 ≤ 3（Application / Instance / Job）；Job 状态机 ≤ 6 态；单个协调循环；
  无 operation/target/stages 三层对象，无 plan/execute/verify/aggregate 四队列，无每步任务锚点。
- **高效**：一次部署（单节点）= 1 次 `RuntimeReconcile` RPC + 1 行 job + 1 行 instance 更新；
  稳态零轮询（漂移由 agent 上报流驱动，周期巡检仅兜底）。
- **稳定**：任意点崩溃可恢复（节点收敛幂等 + 租约恢复 + 启动重扫）；Job 状态单写入者；
  DB 是唯一事实；无 outbox（正确性来自"期望 vs 观测重推 + ensure 重放"）。

### 1.2 非目标（明确不做）
- 多实例/分布式 panel、跨节点事务。
- 迁移旧部署数据（`application_lifecycle_*` 数据直接丢弃）。
- 可变 image tag 的 pull 幂等（另行立项；本设计假定期望引用稳定，节点侧按引用 ensure）。
- v1 不做 reload 优化（env-only 变更热重载）；`RuntimeReload` RPC 保留但编排不调用，
  RuntimeReconcile 总是走"收敛到 spec"路径。
- 不保留旧任务类型（`application_target_apply|stop|purge|batch` 下线）。

### 1.3 验收指标（实现完成时逐项核对）
1. 概念数：job 状态机状态 ≤ 6；协调循环实现 ≤ 1 个 goroutine 调度 + 固定 worker 池。
2. 单节点 apply 全程：1 次 RuntimeReconcile、1 行 job、1 行 instance、0 个任务锚点。
3. 崩溃注入测试：在任意 tick 点杀掉 worker 后重启，系统自动收敛（无需人工干预）。
4. `go test ./...` 全绿；`task build:backend` 通过；前端构建通过。
5. 旧代码删除清单（§10）全部执行完毕，grep 无残留引用。

---

## 2. 总体架构

```
                        Panel（单进程）
┌─────────────────────────────────────────────────────────────┐
│ 业务入口（保存/启用/停用/删除/重启/镜像更新/迁移/持久化恢复）   │
│        │ 只写 desired state                                  │
│        ▼                                                     │
│  applications 表（期望态）          containers 模块（漂移观测）│
│        │                                 │ agent 上报流/巡检  │
│        ▼                                 ▼                   │
│  ┌──────────────────  orchestrator  ──────────────────┐      │
│  │ planner(建/复用/替换 job) → 循环(claim→执行→写回)    │      │
│  │ store: jobs + instances（DB 唯一事实）               │      │
│  │ 单写入者 transition(job, event)                     │      │
│  └──────────────────────┬────────────────────────────┘      │
│                         │ RuntimeReconcile（gRPC，mTLS）     │
└─────────────────────────┼───────────────────────────────────┘
                          ▼
                   Agent（强一致版本）
         RuntimeReconcile：写文件→拉镜像→删旧→建→启→inspect→标签
```

装配关系（避免包循环）：`internal/orchestrator` 定义 `SpecRenderer` / `AgentClient` /
`Store` 接口；`internal/bootstrap/panel/app.go` 注入 `applications.Service`
（渲染器）与 `internal/agent/client`；`applications.Service` 通过注入的
`DeploymentPlanner` 接口（orchestrator 实现）创建 job。与现有
`DeploymentDispatcher` 注入模式相同。

---

## 3. 对象模型

### 3.1 `applications`（保留，不动）
现有表（`internal/platform/database/models/app_models.go` Application）。
期望态唯一事实。`generation`/`spec_hash` 继续由保存路径维护（现有逻辑保留）。

### 3.2 `instances`（收敛 `application_instances`）
现状列：`id, application_id, server_id, container_name, container_id, desired_state,
status, runtime_spec_json, last_deployed_generation, last_error, created_at, updated_at`
（唯一索引 `(application_id, server_id)`）。

收敛后列（迁移方案见 §8）：
| 列 | 变化 | 语义 |
| --- | --- | --- |
| id / application_id / server_id | 保留 | 主键/外键 |
| container_name / container_id | 保留 | observed |
| desired_state | 保留 | running/stopped |
| desired_generation / desired_spec_hash | **新增** | 最近一次 job 写入的期望代次 |
| status | 保留 | observed 状态（running/stopped/missing/failed/deploying） |
| runtime_spec_json | 保留 | 渲染后的 spec（供漂移对比与下次 reconcile） |
| last_deployed_generation | **改名** observed_generation | observed 代次 |
| observed_spec_hash | **新增** | observed spec hash |
| last_error | 保留 | observed 错误 |
| observed_at | **新增** | 观测时间 |

规则：instance = 观测报告（status subresource），脏/旧不破坏正确性；
只有 orchestrator 写 instance；业务读取方只读。

### 3.3 `jobs`（新表，唯一工作单元）
```sql
CREATE TABLE IF NOT EXISTS jobs (
  id                 TEXT PRIMARY KEY,
  application_id     TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
  server_id          TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
  action             TEXT NOT NULL CHECK(action IN ('apply','stop','purge')),
  generation         INTEGER NOT NULL DEFAULT 0,
  spec_hash          TEXT NOT NULL DEFAULT '',
  state              TEXT NOT NULL DEFAULT 'pending'
                     CHECK(state IN ('pending','running','succeeded','failed_retryable','failed','cancelled')),
  stage              TEXT NOT NULL DEFAULT '',          -- 节点 steps 最后一项（仅展示）
  error_code         TEXT NOT NULL DEFAULT '',
  error_message      TEXT NOT NULL DEFAULT '',
  error_detail       TEXT NOT NULL DEFAULT '',
  attempts           INTEGER NOT NULL DEFAULT 0,
  next_run_at        TEXT NOT NULL DEFAULT '',          -- RFC3339Nano；'' = 立即
  lease_owner        TEXT NOT NULL DEFAULT '',
  lease_expires_at   TEXT NOT NULL DEFAULT '',
  created_at         TEXT NOT NULL,
  started_at         TEXT NOT NULL DEFAULT '',
  finished_at        TEXT NOT NULL DEFAULT '',
  updated_at         TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_jobs_active_app_server
  ON jobs(application_id, server_id) WHERE state IN ('pending','running','failed_retryable');
CREATE INDEX IF NOT EXISTS idx_jobs_due ON jobs(state, next_run_at);
CREATE INDEX IF NOT EXISTS idx_jobs_app_created ON jobs(application_id, created_at);
```
模型文件：`internal/platform/database/models/job_models.go`（Job struct + ExtraIndexDDL）。
时间列沿用全库约定：非终态空值用 `''` 字符串而非 NULL（与现有表一致）。

---

## 4. Job 状态机（单写入者）

### 4.1 状态与事件
状态：`pending`（可 claim）、`running`（已 claim 持租约）、`succeeded` / `failed` /
`cancelled`（终态）、`failed_retryable`（退避中）。

事件：`claim`、`succeed`、`fail_retryable`、`fail_terminal`、`cancel`、
`backoff_due`（退避到期回 pending）、`lease_lost`（恢复）。

### 4.2 转移表（唯一权威，代码中一张表驱动）
| 当前 | 事件 | 守卫 | 下一状态 | 副作用 |
| --- | --- | --- | --- | --- |
| pending | claim | next_run_at<=now | running | 写 lease_owner/lease_expires_at/started_at；attempts+1 |
| pending | cancel | — | cancelled | finished_at=now；error_code=superseded |
| running | succeed | — | succeeded | 清租约；写 instance observed |
| running | fail_retryable | — | failed_retryable | 清租约；next_run_at=退避(attempts)；写错误三字段 |
| running | fail_terminal | — | failed | 清租约；写错误三字段；写 instance failed |
| running | cancel | stage=''（未进远端） | cancelled | 清租约 |
| failed_retryable | backoff_due | next_run_at<=now | pending | 清 next_run_at |
| failed_retryable | cancel | — | cancelled | finished_at=now |
| running | lease_lost | stage='' | pending | 清租约 |
| running | lease_lost | stage<>'' | failed_retryable | 清租约；attempts+1；next_run_at=退避；error_code=lease_lost |
| 终态 | 任何 | — | （不变） | 忽略并记录日志 |

### 4.3 单写入者纪律
- `transition(job, event) (job, error)` 是唯一修改 `state/stage/error_*/attempts/next_run_at/lease_*/finished_at` 的代码路径；全部经条件 UPDATE（WHERE 带当前 state 与守卫）实现原子性。
- 禁止在业务代码、投影代码、容器模块中裸写 jobs 表。
- Code review 检查点：任何 UPDATE jobs 的 SQL 必须出现在 `internal/orchestrator/store.go`。

### 4.4 supersede（同 (app,server) 活跃 job 唯一）
- 优先级：apply=1 < stop=2 < purge=3。
- planner 创建新 job 前查活跃 job：
  - 旧 job 为 pending / failed_retryable → `cancel`（任何新 action 都可替换）。
  - 旧 job 为 running 且 stage=''（未进远端）→ `cancel`。
  - 旧 job 为 running 且已进远端 → 不抢；新 job 直接创建为 pending（worker 按
    (app,server) 键串行，旧 job 完成后自然轮到新 job）。
- **过期期望检查**：claim 时若 `job.action='apply'` 且 `job.generation != applications.generation`
  → 不执行，`cancel`（error_code=`superseded_by_newer_desired`）。stop/purge 的
  generation 语义：只要求与创建时一致即可（它们针对删除/停止期望）。

---

## 5. 节点协议：RuntimeReconcile（方案 A）

### 5.1 proto（`internal/agent/proto/agent.proto` 新增）
```proto
rpc RuntimeReconcile(RuntimeReconcileRequest) returns (RuntimeReconcileResponse);

message RuntimeReconcileRequest {
  string application_id = 1;
  string server_id = 2;
  string action = 3;                    // apply | stop | purge
  Spec spec = 4;                        // apply：渲染后的实例 spec（复用现有 Spec）
  int32 generation = 5;
  string spec_hash = 6;
  bool remove_data = 7;                 // purge 时删除持久化目录
  string previous_container_name = 8;   // apply：实例缓存旧容器名
}

message RuntimeReconcileStep {
  string name = 1;
  string status = 2;                    // ok | failed
  string detail = 3;
}

message RuntimeReconcileResponse {
  string status = 1;                    // running | stopped | missing | failed
  string container_id = 2;
  repeated RuntimeReconcileStep steps = 3;
  string error_code = 4;
  string error_message = 5;
  string error_detail = 6;
}
```
- pb 再生成命令（本机已具备 protoc 35.1 / protoc-gen-go v1.36.11 / protoc-gen-go-grpc 1.5.1）：
  `protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative internal/agent/proto/agent.proto`
  （输出路径 `internal/agent/pb/`，生成后跑 `task generate:agent-contract-hash` 更新 contract hash）。
- 同步更新 `internal/agent/contract/` 的 CurrentContract 模型（model.go）与新 Go 类型
  （request/response/step），以及 `internal/agent/rpc/convert.go` 的 pb↔contract 转换。

### 5.2 节点 ensure 序列（`internal/agent/docker/runtime.go` 新增 `Reconcile` 方法，
复用现有辅助，全部子步骤幂等）
```
apply:
  1. 写托管文件        RuntimeWriteFiles（覆盖写 + manifest：sha256/mode/uid/gid）
  2. 拉镜像            DockerImagePull（已存在=成功）
  3. 删旧容器          仅 previous_container_name≠'' 且 ≠spec.container_name；
                      缺失=成功（isDockerNotFound 归一）
  4. 删目标容器        缺失=成功
  5. 建容器            RuntimeCreateContainer（已有 name-conflict 自动清理重试；
                      若同名容器存在且 applied-state 标签匹配 → 复用）
  6. 启动              已运行=成功（isDockerNotModified 归一）
  7. inspect           必须 running；否则返回退出/运行时诊断并失败
  8. 写 applied-state 标签（generation+spec_hash+spec_hash 清单，现有 appliedStatePath 机制）
stop:
  1. 停止容器           已停止/缺失=成功（复用 RuntimeStop 现有逻辑）
  2. inspect            stopped/missing 为成功
purge:
  1. 删除容器           缺失=成功
  2. 删除运行目录       （现有 purge 路径）
  3. remove_data 时删除 persistent 目录
每一步把 (name, status, detail) 追加进 response.steps。
```
- 失败语义：任一步失败 → 返回 `status=failed` + `error_code/message/detail` +
  已执行 steps；整个 RPC 无部分成功状态（节点侧尽力收敛，失败即整体失败，面板按
  error_code 分类重试/终态）。
- 重发安全论证：所有子步骤 ensure 幂等 + 整段序列收敛到 spec 定义的状态，因此
  崩溃后重发同一请求必然收敛；不需要 intent 日志/outbox。

### 5.3 错误码与重试分类（面板 job 记录 + 用户可见）
| error_code | retryable | 说明 |
| --- | --- | --- |
| agent_unavailable | true | agent 未就绪/不兼容（沿用现有标记逻辑） |
| write_files_failed | true | 远端写文件瞬态失败 |
| pull_image_failed | true | 镜像拉取失败（网络/仓库） |
| remove_container_failed | true | 删除容器失败 |
| create_container_failed | false | 确定性失败（spec 问题） |
| start_container_failed | false | 启动失败 |
| verify_failed | true | inspect 读取失败（瞬态） |
| not_running | false | 启动后立即退出（带退出诊断） |
| purge_failed | true | 清理失败 |
| lease_lost | true | 面板侧租约丢失（面板恢复产生，非节点返回） |
| superseded | — | 被更高优先级/更新期望替换 |

---

## 6. 协调循环（orchestrator）

### 6.1 包结构（全部新增）
```
internal/orchestrator/
  state.go          // 转移表（§4.2 纯函数，表驱动）
  store.go          // jobs/instances 全部 SQL（条件 UPDATE claim、扫描、写回、恢复）
  backoff.go        // 指数退避 + jitter（基 30s，上限 1h，attempt 指数）
  planner.go        // CreateJob / ReuseJob / supersede 规则（§4.4）
  orchestrator.go   // 循环：扫描→claim→执行→写回；启动恢复；wake 接口
  drift.go          // 周期巡检（desired vs observed → apply job）
  *_test.go
```

### 6.2 循环语义
- 调度：1 个 goroutine 每 250ms（或 wake 触发）扫描 `state='pending' AND
  (next_run_at='' OR next_run_at<=now)` 的 job id；worker 池（默认 8）按
  `(application_id, server_id)` 键串行执行（同键不并发，跨键并行）。
- claim：条件 UPDATE（`state='pending' AND next_run_at<=now`）→ `running` +
  租约（默认 TTL 3min，与现状一致）。
- 执行：调一次 `RuntimeReconcile`（agent URL 来自 servers traits；`agent.status=compatible`
  才执行，否则按现有 handleAgentError 逻辑标记 agent 状态并 fail_retryable）。
- 写回：成功 → `succeed` + 更新 instance observed（status/container_id/generation/
  spec_hash/observed_at/runtime_spec_json）；失败 → 按 error_code 分类
  `fail_retryable`（退避）或 `fail_terminal`。
- wake：业务入口/漂移触发时置内存 dirty 标记并唤醒调度；**队列只是提示**，
  溢出/丢失由下一次扫描兜底（DB 唯一事实）。
- 启动恢复（顺序）：
  1. `running` 且租约过期 → 按 stage 归 pending（stage=''）或 failed_retryable（stage≠''，error_code=lease_lost）。
  2. 重新入队所有到期 pending。
- 无 verify 阶段、无 aggregate：apply 的最终 inspect 在节点内完成；操作级状态由
  读取时从 jobs+instances 聚合派生。

### 6.3 关键 SQL（store.go 内，条件更新原子性）
```
claim:
  UPDATE jobs SET state='running', lease_owner=?, lease_expires_at=?, started_at=?,
    attempts=attempts+1, updated_at=?
  WHERE id=? AND state='pending' AND (next_run_at='' OR next_run_at<=?)
succeed:
  UPDATE jobs SET state='succeeded', lease_owner='', lease_expires_at='',
    stage=?, finished_at=?, updated_at=? WHERE id=? AND state='running' AND lease_owner=?
fail_retryable:
  UPDATE jobs SET state='failed_retryable', lease_owner='', lease_expires_at='',
    stage=?, error_code=?, error_message=?, error_detail=?, next_run_at=?, updated_at=?
  WHERE id=? AND state='running' AND lease_owner=?
```

---

## 7. 漂移检测（保留两条路径，改终点）

- **agent 上报流**：containers 模块现有观测流程（`containers/service.go`：容器缺失/
  停止/generation/spec_hash/managed_files 漂移 → `driftReason` → plan 请求）终点从
  `PlanApplicationDeployment` 改为 orchestrator planner（`ObservedRuntimeDrift=true`
  语义保留：绕过退避与满足态过滤，但不绕过活跃 job 唯一性）。
- **周期巡检**：现有 5s 扫描保留为兜底，终点同样改为 orchestrator planner。
- `application_reconcile_states`（退避计数）与 `reconcile_stopped`（连续失败 10 次
  停自动协调）机制保留：planner 在自动触发路径沿用计数/退避/停止语义；手动操作
  清除 reconcile_stopped 的现有规则保留。

---

## 8. 数据与迁移（干净起点）

### 8.1 迁移内容（`internal/platform/database/migrations.go` 与 models 同步）
1. 新增 `jobs` 表（§3.3 DDL）。
2. `application_instances` 增列：`desired_generation`、`desired_spec_hash`、
   `observed_spec_hash`、`observed_at`；`last_deployed_generation` 改名
   `observed_generation`（SQLite：重建表或 ADD COLUMN + 复制，按迁移层现有模式）。
3. 删除表：`application_lifecycle_operations`、`application_lifecycle_targets`、
   `application_target_stages`（CoordDB）。**不迁移数据**。
4. `application_reconcile_states` 保留。
5. 删除 `coordination_models.go` 的对应模型，新增 `job_models.go`。

### 8.2 旧任务类型下线（`internal/modules/applications/tasks.go`）
- 删除 `TaskTypeTargetBatch/Apply/Stop/Purge` 四个注册与
  `applicationTargetConcurrencyKey`；`handleTargetTaskFailure`、`RunDeployTask` 删除。
- `TaskTypeStop/Restart/Refresh/ImageCheck/ImageUpdate` 保留，但 executor 内部不再
  走 lifecycle target：改为"写期望 + 触发 orchestrator planner"（stop/restart/refresh
  语义与现在一致：stop=建 stop job；restart=建 apply job 并强制；image update=更新
  desired 后建 apply job）。
- LogDB 中旧任务历史保留（只读历史），不再产生新记录。

### 8.3 旧代码删除清单（Step 6 执行，逐条 grep 确认无引用）
| 文件/函数 | 处理 |
| --- | --- |
| `deployment_dispatcher.go` 全部 | 删除 |
| `target_stages.go` 全部 | 删除（recordTargetStage/finishTargetRunningStages） |
| `deployment_planner_test.go`、`deployment_dispatcher_test.go`、`deployment_executor_test.go` | 删除 |
| `service.go`：runApplyLifecycleTargetTask / runStopLifecycleTargetTask / verifyLifecycleTargetNow / finishDeploymentOperationFromTargets / failLifecycleTargetExecution / enqueueLifecycleTargetVerification / afterLifecycleTargetVerified / PlanApplicationDeployment（改造为 planner 入口或删除） | 删除/改造 |
| `records.go`：operation/target 聚合查询 | 删除或改为 jobs 聚合 |
| `task_projection.go` | 改为 jobs 投影 |
| `cleanup_worker.go` | 改为清理旧 jobs（按保留期，只清终态） |
| `tasks.go` 四个 Target 任务类型 | 删除 |
| `model.go`：Lifecycle* 常量与结构体 | 删除 |
| `orm_mappers.go`：lifecycle 映射 | 删除 |
| containers/service.go：对 PlanApplicationDeployment 的调用 | 改调 orchestrator planner |
| bootstrap/panel/app.go：DeploymentDispatcher 装配与启动 | 改为 orchestrator 装配与启动 |

---

## 9. 前端与 API 投影（保持契约）

- 列表/详情/运行时端点：字段名不变。
  - `runtimeStatus` 推导映射：无活跃 job + observed=running → running；无活跃 job +
    observed=stopped → stopped；无活跃 job + 无实例 → missing；有 pending/running/
    failed_retryable job → deploying；终态 failed → failed；reconcile_stopped → 需人工处理。
- `GET /api/v1/applications/{id}/runtime` 的 `operation` 字段：从"最近 lifecycle
  operation"改为"该应用最近一批 job"（派生：同 application 且 created_at 相近 +
  同 trigger 的 job 集合；不建表）。
- 操作记录页（`/application-operations`）：改读 jobs，每条 job 一行（action/state/
  stage/error/attempt/nextRunAt/时间），字段名与旧 target 展示字段对齐，前端无需大改。
- 任务中心 deployment projection provider（tasks HTTP handler）：改读 jobs。
- 前端代码改动预期：无或极小（mock 与字段对齐检查）。

---

## 10. 实现步骤（每步独立可验收，按序执行）

### Step 1 — 节点协议 RuntimeReconcile
范围：proto + pb 再生成 + contract 模型/转换 + agent 侧 Reconcile 实现 + client 方法。
文件：
- 改：`internal/agent/proto/agent.proto`、`internal/agent/contract/model.go`（CurrentContract）、
  `internal/agent/rpc/convert.go`、`internal/agent/rpc/service.go`（handler）、
  `internal/agent/docker/runtime.go`（Reconcile 方法）、`internal/agent/client/client.go`
- 生成：protoc → `internal/agent/pb/`；`task generate:agent-contract-hash`
测试（agent 侧，复用现有 fake docker client 模式）：
- apply 全流程收敛到 running；重复调用幂等（第二次无副作用）；
- 各 ensure 归一：删缺失容器=成功、启已运行=成功、create name-conflict 自动清理重试；
- 任一步失败 → 整体 failed + steps 携带已执行步骤；
- 启动后立即退出 → not_running + 退出诊断；
- stop/purge（含 remove_data）收敛；purge 缺失=成功。
验收：`task build:backend` + `task test:backend` 通过；contract hash 更新。

### Step 2 — 数据模型与迁移
范围：jobs 表、instance 收敛、旧表删除、模型与迁移。
文件：`internal/platform/database/models/job_models.go`（新）、
`app_models.go`（instance 收敛）、`coordination_models.go`（删）、
`migrations.go`（建/删表）。
测试：models_test.go 表结构断言更新；迁移幂等（重复执行）。
验收：新库建表正确；旧库升级路径走通（迁移脚本幂等）。

### Step 3 — orchestrator 循环
范围：state 转移表、store SQL、backoff、循环、启动恢复、wake。
文件：`internal/orchestrator/` 全套（§6.1）。
测试：
- 转移表全覆盖（表驱动，含非法转移拒绝）；
- claim 并发：两 worker 抢同一 job 只有一个成功；
- 退避：attempts → next_run_at 指数递增 + jitter 边界；
- 崩溃注入：claim 后不写回 → 恢复路径归 pending/failed_retryable；
- supersede：新 generation 的 pending apply 取代旧 pending；stop 取代 apply（未进远端）。
验收：循环单测全绿；`task test:backend` 通过。

### Step 4 — planner 接线
范围：业务入口与漂移路径改调 orchestrator planner。
文件：`internal/orchestrator/planner.go`（新）、`applications/service.go`（入口改造，
注入 DeploymentPlanner 接口）、`containers/service.go`（漂移终点改造）、
`bootstrap/panel/app.go`（装配）。
测试：业务入口（保存/停用/删除/重启/镜像更新）产生正确 job；漂移路径产生 apply job
且绕过退避；reconcile_stopped 规则保留。
验收：现有 applications/containers 测试改造后全绿。

### Step 5 — 投影
范围：运行时/操作记录/任务中心投影改读 jobs。
文件：`applications/service.go`（runtimeStatus 映射、runtime 端点）、
`task_projection.go`（jobs 投影）、`records.go` 改造、`cleanup_worker.go`。
测试：投影单元测试（映射表全覆盖）。
验收：前端契约字段不变；前端构建通过（`task build:web`）。

### Step 6 — 下线旧实现
范围：§8.3 删除清单全部执行；grep 无残留。
文件：deployment_dispatcher.go、target_stages.go、旧测试、tasks.go 四类型、
model.go/orm_mappers.go 的 Lifecycle 部分、coordDB 相关。
测试：`task test:backend` 全量回归；`task build:backend`。
验收：旧标识符 grep 零命中；全量测试绿。

### 文档同步（每步完成时）
- 更新 `docs/agents/modules/applications.md`（重写部署章节：jobs/orchestrator/
  RuntimeReconcile）、`containers.md`（漂移终点）、`tasks-scheduler.md`（目标任务
  类型下线）、`database` 相关指引、`docs/agents/modules/README.md` 索引（若新增
  orchestrator 模块指引）。
- 用户可见文案（错误码 message）按 `docs/agents/i18n-guide.md` 处理并更新翻译状态。

---

## 11. 风险与缓解

| 风险 | 缓解 |
| --- | --- |
| pb 再生成环境差异 | 本机已具备 protoc 35.1 + 插件；生成后立即 diff 检查 |
| contract hash 变化导致 agent 标记 incompatible | 强一致机制自动重装 agent（发版顺序：先升级 panel，agent 自动跟随） |
| 大文件删除遗漏引用（service.go 215KB） | Step 6 逐条 grep；每删一个符号跑一次编译 |
| 旧任务中心历史投影消失 | 干净起点，可接受；投影 provider 切换后旧任务无 deployment 字段 |
| 前端契约漂移 | Step 5 保持字段名；前端构建 + mock 对齐检查 |
| 迁移破坏现有库 | 迁移幂等 + models_test 断言；先在测试库验证升级路径 |

## 12. 开放假设（如需调整，改本文件后评审）
- v1 不做 reload 优化；RuntimeReload 保留不调用。
- 可变 tag 幂等另行立项（Step 1 只保证"同引用重复收敛"）。
- worker 并发默认 8；租约 TTL 3min；退避 30s 起、1h 上限、±20% jitter——与现状
  参数一致，避免行为突变。
- 持久化数据导入/下载、应用迁移（move）映射到 apply/purge job 组合，语义不变。
