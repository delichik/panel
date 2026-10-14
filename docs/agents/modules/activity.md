# 活动事实（操作 / 任务 / 日志）

## 产品职责（必须精简、利于排障）

三类入口回答三个问题，不得再合成「统一活动双视图」：

| 入口 | 回答 | 排障作用 |
| --- | --- | --- |
| `/operations` | 用户干了什么 | 确认人工意图与结果；失败看摘要后跳日志 |
| `/tasks` | 系统自动干了什么（协调/定时等） | 确认自动意图与结果；失败看摘要后跳日志 |
| `/logs` | 运行中写出了哪些诊断行 | 用等级+来源+消息定位原因；默认先看 warning/error |

排障最短路径：**意图失败摘要 → 查看日志（warning/error）→ Dialog 看 errorCode/error/detail 或正文**。产品 UI 不得要求用户先理解时间线、上下文、证据块或 executions/steps 树。

## 适用范围（实现层）

任务过程、协调过程、系统事件仍以**只追加事实**写入同一 activity 存储；队列/Job/租约等控制态可修改，不能当作历史查询真源。产品三分是查询与呈现合同，不是要求立刻拆成三套物理表；但查询与 UI 必须按上表职责分流。

## 入口

- 原始事实：`internal/platform/activitylog/`。
- 查询投影：`internal/modules/activity/`。
- 装配：`internal/bootstrap/panel/activity.go`、`app.go`。
- Agent 协议：[agent-execution-events.md](agent-execution-events.md)。
- 界面：`web/src/views/operations/`、`tasks/`、`logs/`；共享 `IntentRecordsPage.vue` 与 `web/src/views/activity/model.ts` 等纯逻辑。

## 持久化约束

- AppDB `activity_events` 是原始事实源；`activity_evidence_chunks` 保存分块证据。禁止 UPDATE、DELETE、REPLACE、裁剪和过期清理。
- 两表使用专有幂等 schema；禁止对历史载荷做启动清洗、重写或语言转换。
- 资源快照、身份、消息码、原文、结构化数据随事件插入；历史详情不能从当前对象回填名字。
- 任务执行控制位于 AppDB；关键变化由 SQLite triggers 同事务追加事实。参数及原始 runtime spec 不写入日志。
- SQL/Go 入口脱敏；`activitylog.WithActor` 传递认证上下文。
- 全局 seq 是接收次序；源序号不与数据库 seq 混用。
- 旧记录不迁移。

## 写入与可靠性

- `activitylog.Append` / `AppendTx` 为写入口；固定 eventId 重传同内容返回原回执，不同内容冲突。
- HTTP 响应在确认事实存在后附加 operationId/acceptedEventId/acceptedSeq。
- Quiet 任务成功流转可为 `debug`；失败/stderr 仍为 `error`，保证默认诊断筛选可见故障。
- Agent 输出源端缓冲，Panel 提交后 ACK；缺口检测与补齐分别追加事件。
- 写入失败可见；不得满队列静默丢弃。

## 关联与结果

- `operationId` 是一次意图（可空）；用户意图与系统意图靠 trigger/actor 区分（查询 `origin=user|system`）。
- runId / executionId / stepId 仍用于执行关联；**产品意图详情不展开执行树**，排障以日志行为主。
- 失败摘要只从 `data.error` / `data.detail` / `data.errorCode` 取值，不使用 trigger reason 冒充。
- `POST /executions/{id}/resolve` 仅记录人工核对，不得表述为自动验证。

## 查询与界面合同

`/api/v1/activity` 仍提供 events、operations、export 等；产品页只使用其中必要子集：

- 意图列表：`GET /activity/operations?origin=user|system`。
- 意图详情：`GET /activity/operations/{id}?detail=summary`（仅摘要 + availableCommands；`detail=full` 仅供工具链）。
- 日志列表：`GET /activity/events`，默认 `level=warning,error`；可按 operationId/executionId/资源过滤。
- 产品 UI **不得**调用或展示 context / evidence / tail / summary，也不得渲染 executions/steps/事件时间线。
- Toast / ActivityLink → `/logs?...&level=warning,error`；应用/设施协调 → `/tasks`。
- 旧 `/activity` 仅兼容重定向（`view=operations` → `/tasks`，其余 → `/logs`）。`/executions` 只做执行控制命令，不是历史入口。
- 容量门禁与写入失败仍在服务端生效；产品日志页不展示容量统计横幅（避免 summary 探测）。

## 验证

- 后端：`task test:backend`（含 origin 分流）。
- 前端：`task test:web`（logs 列表+Dialog、深链带诊断等级、redirect）。

## 容量与请求审计

- 可用空间不足 1 GiB 停止新变更；不足 2 GiB 或 10% 由服务端门禁/写入失败处理。产品 `/logs` 页不展示容量统计横幅，也不为此调用 summary。查询/导出与人工 resolve 不受新变更门禁阻断。
- 认证后同步变更先追加 request.received；无后台执行时追加 operation.finished。密钥与秘密不得进入事件正文。
