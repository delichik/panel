# 系统日志（原系统事件）

## 适用场景

修改系统日志（原“系统事件”）、统一日志写入接口、批量落库、清理保留或操作记录页时，先读本文档。

## 前端入口

- 系统日志页面族：`web/src/views/system-events/`（菜单与标题为“系统日志”，路由保持 `/system-events`）
- API：`web/src/api/systemEvents.ts`
- 类型：`web/src/types/systemEvents.ts`
- Mock：`web/src/mocks/runtimeEvents.ts`，由 `web/src/mocks/browser.ts` 挂载同名正式路径。
- 导航：`web/src/components/shell/navModel.ts`
- 路由：`web/src/router/index.ts`

## API 范围

- `GET /api/v1/system-events`

列表支持 `category`、`severity`、`eventType`、`source`、`from`、`to`、`page`、`pageSize`。前端只暴露“类型搜索 + 级别 + 时间范围”三个筛选。

## 数据与行为约定

- “系统日志”是简单执行日志：每次执行操作（应用操作、任务）与 Agent 连接/断开等关键运行状态记录一条可读日志，**不承载 payload、日志/任务/目标引用，不展示关联对象**。
- 操作记录（应用目标/阶段/步骤日志/失败原因）由应用模块承载（`internal/modules/applications/records.go`），不再依赖系统事件。
- 日志写入走**专门日志接口** `runtimeevents.EventWriter`：
  - 各模块通过非阻塞 `Log(ctx, WriteEventInput)` 入内存缓冲；
  - 后台 `BufferedWriter` 每 5 秒批量取出，**一个事务批量落库**（`INSERT OR IGNORE`）；
  - 缓冲区满时丢弃该条日志，不阻塞业务；服务停止时 flush 剩余。
- 事件 `category` 只使用 `application`、`task`、`system`（`alert`/`log`/`runtime` 保留兼容，不再写入）。
- 事件类型：
  - 应用操作：`application.operation.created` / `completed` / `failed`（不再写每个节点的 target 过程事件）；
  - 任务：`task.created` / `started` / `completed` / `failed` / `retried` / `cancelled`（不再写 `log.attached`）；
  - 删除服务器时 `CancelByServer` 也会逐任务写 `task.cancelled` 事件（复用 `DedupeKey`，重复取消不会重复落库）。
  - Agent 状态：`agent.connected` / `agent.disconnected`（仅状态转换时写入，避免刷屏）。
- 失败/错误原因直接并入日志“内容”字段（summary），列表行内即可读，无详情弹窗。
- 页面列表只展示：时间 / 级别 / 类型 / 内容 / 来源；无“查看”按钮与详情接口。
- 保留策略：`runtimeEventRetentionDays` 控制系统日志保留天数；`runtimeEventDetailRetentionDays` 已改用于**应用操作阶段清理**（`applications.NewStageCleanupWorker`），不再用于系统事件详情。
- `runtime_event_details` 表不再读写（表保留，避免破坏性迁移）。

## 后端实现入口

- 日志模块位于 `internal/modules/runtimeevents/`：`Service`（写入/查询/清理）、`BufferedWriter`（5 秒批量落库）、`CleanupWorker`。
- 写入点：
  - `internal/modules/applications`：`writeApplicationOperationEvent` 只写创建/完成/失败，失败原因并入 summary；
  - `internal/modules/tasks`：`writeTaskEvent` 写任务生命周期事件；
  - `internal/bootstrap/panel/agent_report_collector.go`：`logStreamStatus` 写 Agent 连接/断开。
- 装配在 `internal/bootstrap/panel/app.go`：创建 `BufferedWriter` 并注入应用/任务/Agent 收集器，启动/停止与清理 worker 一致。
- 清理：`Service.Cleanup(ctx, retentionDays)` 按 `occurred_at` 删除过期日志。

## 验证

- 后端写入、批量落库、清理或写入点改动后运行 `task test:backend`。
- 前端页面、API、Mock、类型或文案改动后，按范围运行 `task build:web` 或 `task test:web`。

## 文档更新触发

新增事件类型、列表筛选字段、批量间隔、保留策略或导航入口变化时，必须更新本文档。