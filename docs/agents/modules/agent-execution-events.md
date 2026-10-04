# Agent 执行事件运输

## 适用范围与入口

Application 的 `RuntimeReconcile` 执行过程使用可靠事件协议。事件源位于 `internal/agent/executionevents`，步骤采集位于 `internal/agent/docker/reconcile_events.go`，RPC 与客户端位于各自目录下的 `execution_events.go`。本协议不把 Docker 容器持续运行 stdout/stderr 历史伪装为部署执行输出。

## 持久边界

- Agent 使用专门 SQLite 运输缓冲，默认 `/opt/panel/agent/execution-events/events.db`；`HandlerConfig.ExecutionEventsDir` 可在装配或测试中覆盖。目录权限 `0700`、文件 `0600`，WAL 与 `synchronous=FULL`。这里是 Panel 事实库的待确认运输副本，不是用户历史查询源。
- 每次 RPC 必须带 `operationId/runId/executionId/instanceId`。首次执行在一个本地事务中保存请求散列、脱敏身份/目标版本和 `execution.started`，然后才能触碰 Docker。完整运行规格、环境变量和文件内容不保存到事件库。
- 相同 execution ID、相同请求返回已持久化结果；同 ID 不同请求拒绝。未结束或未知执行占用实例冲突域，禁止另一个执行重复远端副作用。
- 每个实际步骤同步追加 `step.started`，执行完成后追加 `step.finished`，记录独立 step ID 和实测时间。写入失败阻止后续步骤。RPC 取消后仍尝试持久保存已经观察到的步骤/执行结果；结果无法持久时保留未知状态，不返回假成功。
- Docker image pull 逐条解析 Docker 进度，仅传递公共进度字段为 `output.chunk`。HTTP 200 中的 Docker 错误也按失败处理。文本在切块前整体脱敏，按 UTF-8 字符边界切分，每块至多 4096 个字符，不按行数裁剪；分块携带 line ID/part index。
- 传输取消或网络超时追加 `uncertainty.detected` 并保留未关闭流。普通已知结果与 `execution.finished`、`stream.closed` 在同一事务提交。关闭事件声明最终源序号，不能仅凭 RPC 结束判定证据读完。

## 补传、ACK 与核对

- Required capability 新增 `execution-events-v1`。Panel 客户端 `RuntimeReconcile` 先检查 Health；旧 Agent 必须升级，不能在缺少可靠事件协议时执行此远端变更。
- `ReadExecutionEvents` 接受 execution ID、source epoch/stream、afterSourceSeq 和 limit（默认 100、最大 500）。首次请求可留空 epoch/stream 以发现身份，后续必须校验完整身份。
- 响应包含 source ID/epoch/stream、原始事件、headSeq、endSeq、hasMore、ackedThroughSeq。一个 execution 独占一个序号域，stdout/stderr/system 只是事件字段。重传保留原 event ID、源序号、实际发生时间。
- `AckExecutionEvents` 必须带完整源身份及连续确认水位；拒绝超过 head 的确认。Panel 只有在事件事务成功提交后才能 ACK。Agent 可删除已经 ACK 的运输副本，但永不删除执行身份/结果，防止重试变成重复执行。源身份在 Agent 重启后保持稳定。
- `GetExecutionResult` 返回 missing/running/unknown/finished 和持久结果。服务重启后未完成执行为 unknown，不能被解释为未执行。
- unknown 查询可触发只读核对：apply 验证容器归属、目标 generation/specHash、运行状态与实际文件 manifest；stop 验证停止或缺失；purge 在不涉及数据删除时验证容器缺失。完全匹配后追加验证及结束事实，解除实例冲突保护。删除持久数据的结果不能仅凭容器消失断定，因此此情形保持 unknown；确认实际节点状态后可通过下述人工核对命令显式结束。
- 当前未完成的真实步骤仍保持开始事实；恢复核对不伪造崩溃期间的步骤结束时间。

## 验证

协议变更通过 `task generate:agent-proto` 生成 protobuf/gRPC（需要 protoc、protoc-gen-go、protoc-gen-go-grpc），契约 hash 沿用 `task generate:agent-contract-hash`。Agent 范围执行 `task test:agent`，整体集成由 `task test:backend` / `task build:backend` 验证。

关键测试覆盖持久重启、相同请求重放、不同请求身份冲突、跨页补传、错误 epoch ACK、已确认结果保留、未知执行冲突保护、超长 Unicode 输出与跨块脱敏，以及步骤意图写入失败时不执行副作用。

## Panel 导入与部署回归

`internal/modules/applications/activity_runtime.go` 只在 AppDB 事实提交后确认同一 source epoch/stream 的连续序号。导入校验原 operation/run/execution、事件类型、步骤标识、源时间及水位；恢复查询复用该 execution 最初事件保存的意图、资源名称/版本和 initiator，即使 Job 后来被合并或资源改名也不改写历史归属。

源声明结束时必须已经收到相同序号的 `stream.closed` 事实；RPC 返回成功但流未关闭不能作为完整执行证据。缺失序列追加 `evidence.gap_detected`，重复检查不改其时间；后续补齐并验证连续范围后追加关联的 `evidence.gap_resolved`。

`task test:appdeploy` 运行真实 applications/controller/SQLite 加真实 Agent spool 的剧本集成及应用模块测试。场景覆盖成功、停止、删除、漂移修复、无变化、并发 claim、错误分类及重试，并要求租约过期先核对原 execution 的持久结果，不重新执行已经完成的副作用；原结果缺失时保留未知冲突保护。


## 受控人工核对

- `ResolveExecution` RPC 对应独立 `ExecutionResolutionClient`，声明包含 execution ID、`outcome=succeeded|failed`、非空理由、发起人的 ID 与名称。Panel 必须从当前认证会话绑定身份，再通过原有 Panel mTLS 连接转发；日志公共读取 API 不提供任意编辑事实入口。
- Agent 仅接受已经 unknown 且没有活跃执行对象的 execution。仍在执行、已经自动结束、身份/理由缺失、非法 outcome 都拒绝。新的 `execution-resolution-v1` 能力纳入握手，客户端会在命令前检查。
- 一次人工声明在同一个事务内追加 `execution.manually_verified`、`execution.finished`、`stream.closed`，保存最终结果和幂等声明，最后解除实例冲突保护。任一写入失败则全部回滚，原未知状态和保护继续有效。ACK 仍只回收运输副本，不删除幂等声明或执行结果。
- 同一 execution 的相同规范化声明跨重启/ACK 重传返回原回执；不同理由、结果或发起者产生冲突，不能覆盖首次声明。
- 人工核对不是自动观察。事件和结果明确记录 `verificationSource=manual`、理由、核对人及实际核对时间；已有 `Observed*` 字段保持最后真实观察值，无观察时保持 unknown/空时间。人工失败使用 `execution_manually_failed` 错误码，不自动重试。此前超时、失败或未知事实始终保留。
- 持久数据删除等无法自动验证的操作因此有明确闭环：管理员先检查节点，再声明结果；界面仍需区分人工声明与自动验证。
