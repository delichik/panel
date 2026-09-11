# 统一追加日志

## 适用范围

任务过程、协调过程、系统事件、统一查询、日志导出、原始证据与日志关联入口均使用本模块。日志事实只追加；队列、Job、租约等内部执行控制状态可修改，不能作为历史查询来源。

## 入口

- 原始事实及不可变 schema：`internal/platform/activitylog/`。
- 查询及可重建投影：`internal/modules/activity/`。
- 装配、认证上下文与响应回执：`internal/bootstrap/panel/activity.go`、`app.go`。
- Agent 持久事件协议：[agent-execution-events.md](agent-execution-events.md)。
- 界面、API、类型、Mock：`web/src/views/activity/`、`web/src/api/activity.ts`、`web/src/types/activity.ts`、`web/src/mocks/activity.ts`。

## 持久化约束

- AppDB `activity_events` 是原始事实源；`activity_evidence_chunks` 保存分块证据。禁止 UPDATE、DELETE、REPLACE、裁剪和过期清理。
- 两表使用专有幂等 schema 初始化，排除 destructive ORM 管理。禁止对历史载荷做启动清洗、重写或语言转换。
- 资源快照、身份、消息码、原文、结构化数据随事件插入；历史详情不能从当前服务器/应用/instance 回填名字、版本和观察结果。
- 任务执行控制位于 AppDB；tasks/steps/jobs/观测的关键变化由 SQLite triggers 同事务追加事实。参数及原始 runtime spec 不写入日志。
- SQL 和 Go 入口均脱敏；actor、initiator 区分事件执行者与最初发起者。认证上下文通过 `activitylog.WithActor` 传递。
- 全局 seq 是接收次序；生产者 sourceId/sourceEpoch/sourceStreamId/sourceSeq 标识独立源流。源序号不与数据库 seq 混用。
- AppDB WAL + synchronous FULL + immediate 写事务；不要持有事务执行网络请求。
- 旧记录不迁移。业务和现有 AppDB Job/Instance/revision 不因日志替换被清空。

## 写入与可靠性

- `activitylog.Append` / `AppendTx` 是 Go 写入口。固定 eventId 重传时同内容返回原回执，不同内容报冲突；源序号身份也不可重复使用。
- AppendTx 回执只在外层事务提交后有效。HTTP 响应在确认事实存在后附加 operationId/acceptedEventId/acceptedSeq，成功与错误均可关联日志；下载和流式响应不缓冲改写。
- 任务日志不再截断或删除；完成后仍可接收迟到输出。TaskID 的所有尝试可用 activity executionId 筛选查询。
- Agent 输出在源端持久缓冲，Panel 提交原始事件后才 ACK；源流关闭与操作结束是独立事实。缺口检测与补齐必须分别追加事件。
- 内部系统事件同步耐久写入，不再通过旧可丢弃 BufferedWriter。写入失败可见，不允许满队列或失败批次直接丢弃。
- 原始证据不会因投影重建删除。事件格式以 eventVersion 扩展，不能覆盖已写历史。

## 关联与结果

- operationId 是一次意图，可为空；独立 Agent 断连等事件通过资源和源流定位。
- runId 是执行轮次，executionId 是实际执行，stepId 只属于一个执行尝试。
- Planner 合并等价意图追加 execution.linked，随后为共享意图追加执行结果；取代旧意图追加 intent.superseded，不改原事件。
- 当前结果由事件投影：phase queued/running/waiting/ended，result succeeded/partial/failed/cancelled/superseded/unknown。
- 多目标按服务器执行域汇总，不能将同一应用的多个服务器合成一个目标；重试选择同目标最新执行，旧失败仍保留。
- 核对中 phase=waiting/result=null/uncertainty=true；终止核对但结果不明才是 ended/unknown。
- 孤立任务及丢失RPC回执保留执行冲突锁，先核对，不能直接自动重试。`POST /executions/{id}/resolve` 记录认证用户人工核对结果与依据，不能把人工声明表述成自动验证。

## 查询与界面

`/api/v1/activity` 提供 events、单事件/context、operations/详情、tail、summary、export、evidence。日志命名空间没有用户写入、修改或删除接口。

- 默认事件视图，按操作是同一事实的聚合视图，统一详情显示时间线和证据。
- LogDB 保存事件索引、FTS搜索及版本化操作投影；checkpoint 与投影在一个事务提交。
- 按固定 snapshotSeq 和游标分页，返回 headSeq/projectedThroughSeq/hasMore。后端按操作分页，前端不得从当前页事件拼分组。
- 事件每批默认100、最多500；全局搜索支持短文本和正文索引，筛选输入必须绑定参数。
- timeline 增量读取不因完成事件停止；Agent stream.closed 前不宣告远端证据完整。
- 业务页面使用 ActivityLink 和 Toast action 进入日志；错误响应仍保留真实关联标识。
- 应用资源入口默认进入按操作视图，本次操作有真实 `operationId` 时直接打开对应时间线，不能让用户从全局事件接收顺序猜测因果关系。
- 操作投影的失败摘要只从结构化 `data.error`、`data.detail`、`data.errorCode` 依次取值，不使用 trigger reason 或普通 `text` 冒充失败原因；这三个结构化错误字段同时进入 FTS，允许按真实错误和稳定错误码搜索。投影 schema 版本变化会重建 LogDB 可重建索引，不改写 AppDB 原始事实。
- 预期的无 Job 陈旧/重复观测不写 `observation.rejected` warning；真正的 durable Job lease/fencing/实例异常拒绝必须携带关联操作、资源、明确 reason 与新旧观测诊断。
- 复用 v4 ConsolePage/MasterDetailLayout/primitives，桌面滚动限制在正文，窄屏恢复自然布局。
- 原 `/tasks`、`/system-events`、`/application-operations` 历史路由退出使用。`/executions` 只负责当前执行控制与允许的命令，不是另一个日志入口。

## 验证

修改后端执行 `task test:backend` / `task build:backend`；前端执行 `task test:web` / `task build:web`；Agent协议执行 `task test:agent`，生成走 `task generate:agent-proto`。

关键回归：防改写/删除/REPLACE、事务回滚、身份冲突、长日志/短中文搜索、快照分页、投影重建、并行目标、共享意图结果、真实Agent补传ACK、未知状态fencing、资源改名历史不变。

## 容量与请求审计

- 新任务、Planner请求和认证后HTTP变更在接收前检查事实库所在文件系统。可用空间不足1 GiB停止新变更；不足2 GiB或10%在日志界面告警。查询、导出和在途结果写入继续可用，人工resolve核对不受新变更门禁阻断。初始阈值为代码常量；不通过删除日志释放容量。
- 认证后的同步变更同样先追加request.received；没有专属后台执行时追加独立operation.finished，记录HTTP执行结果与请求关联。后台任务/协调存在时request.finished只表达HTTP请求结果，不替代后台执行结果。
- 响应关联只引用已提交事实；结果日志写入失败会明确说明业务可能已完成，不能直接重试。收到请求不等于业务已接受，request.received不会伪装成已完成操作。
- 普通输出只写事件索引，不逐行复制完整执行树；首次错误或完整性变化保留版本，计数与最近事件时间按查询快照从事件索引聚合。
