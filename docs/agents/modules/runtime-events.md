# 运行事件、操作记录与系统事件

## 适用场景

修改统一运行事件、操作记录、系统事件、详情保留提示、事件投影查询或旧任务中心入口替换时，先读本文档。

## 前端入口

- 操作记录页面族：`web/src/views/application-operations/`
- 系统事件页面族：`web/src/views/system-events/`
- API：`web/src/api/applicationOperations.ts`、`web/src/api/systemEvents.ts`
- 类型：`web/src/types/applicationOperations.ts`、`web/src/types/systemEvents.ts`
- Mock：`web/src/mocks/runtimeEvents.ts`，由 `web/src/mocks/browser.ts` 挂载同名正式路径。
- 导航：`web/src/components/shell/navModel.ts`
- 路由：`web/src/router/index.ts`

## API 范围

- `GET /api/v1/application-operations`
- `GET /api/v1/application-operations/{operationId}`
- `GET /api/v1/system-events`
- `GET /api/v1/system-events/{eventId}`

操作记录列表支持 `applicationId`、`action`、`source`、`status`、`from`、`to`、`page`、`pageSize`。系统事件列表支持 `category`、`severity`、`eventType`、`subjectType`、`subjectId`、`source`、`from`、`to`、`page`、`pageSize`。

## 数据与行为约定

- “协调记录”（原“操作记录”）是“应用”一级菜单下的产品入口，展示“应用在服务器上检测到状态不一致后执行变更”的记录：改了什么、过程（步骤日志）、结果与失败原因；只读，不复用旧 `/tasks` 页面；路由保持 `/application-operations`。
- “系统事件”用于诊断，展示后端提供的运行事件类型、严重级别、关联对象、来源、摘要和详情引用，不承担应用工作进度页职责。
- 旧 `/tasks` 路由可保留兼容，但不在主导航、概览快捷入口或应用详情入口中出现，也不重定向到新页面。
- 应用详情的协调记录入口跳转到 `/application-operations?applicationId=<id>`。记录列表与详情由应用模块读取时直接聚合协调库的 `application_lifecycle_operations` + `application_lifecycle_targets`，不建投影表；列表应用列使用解析出的应用名（应用已删除回退应用 id），不直接展示 `applicationId` / `operationId` 原始 id。
- 失败或部分失败的记录在详情头部展示失败目标与 `failureSummary` 失败摘要（来自目标错误信息）；失败只标注在对应服务器上，不影响其他并行执行的服务器。
- 协调记录列表展示设施隐藏身份 `facility-reverse-proxy` 的部署/同步记录；前端按 `applicationId` 把应用列与详情标题显示为翻译后的“入口代理设施”，不展示内部名 `__panel_facility_reverse_proxy__`。
- 记录不依赖事件保留期：记录摘要与目标长期保留；仅目标步骤日志按保留设置清理（抽屉显示“暂无步骤日志”）。
- 前端不假设独立告警服务。系统事件页只按 `system-events` API 返回的 `category` / `eventType` / `severity` 展示。
- 事件 `category` 只使用 `application`、`task`、`alert`、`log`、`runtime`、`system`。任务事件写入 `task`，任务日志引用写入 `log`，应用操作事件写入 `application`。
- 新事件系统不迁移旧任务历史；空态必须说明只显示启用后产生的新记录。
- 协调记录页的筛选变化必须先把页码归一到第一页，并只发起一次有效列表加载；非法 URL 页码回退为第一页。列表与详情只允许最新请求提交状态，避免快速筛选、翻页或切换详情时旧响应覆盖当前内容。
- 协调记录页为左列表 + 右详情 + 右侧“步骤日志”抽屉；抽屉在切换记录/筛选/翻页时关闭。
- 系统事件列表与详情的 `event` 包含 `subjectName` 字段：后端在读取时按 `subjectType` 实时解析正式名称（应用、服务器、证书、DNS 域名、密钥资产、任务摘要、操作记录名称），不落库、不保存快照；解析不到时前端回退显示“类型标签 + id”。前端“关联对象”列与详情按“类型标签 + 名称”展示。

## 后端实现入口

- 后端运行事件模块位于 `internal/modules/runtimeevents/`，只负责系统事件的写入、查询、HTTP handler 和独立清理 worker；不再提供应用操作记录接口。
- 事件摘要表 `runtime_events`、详情表 `runtime_event_details` 位于 `Store.LogDB()`；`application_operation_records` 投影已废弃（不建、不用）。
- 协调库 `Store.CoordDB()`（默认 `data/db/coordination.db`）保存 `application_lifecycle_operations`、`application_lifecycle_targets`（含观测快照列）与 `application_target_stages`（步骤日志）。
- `/api/v1/application-operations` 列表/详情由应用模块（`internal/modules/applications`）提供：列表/详情读取时直接聚合协调库生命周期表；详情返回 `{ operation, targets }`，targets 含每台服务器的期望 vs 观测快照、错误、`stages[]` 步骤日志；不再返回事件。
- `/api/v1/system-events` 查询 `runtime_events`，默认不固定 `category=system`；详情接口返回 `{ event, payload, error, logRefs, taskRefs, targetRefs }`，`error` 携带详情中保存的错误信息；详情过期后引用字段返回空数组并保留摘要事件。系统事件的 `operation` 主体名称从协调库解析。
- `runtimeEventRetentionDays`、`runtimeEventDetailRetentionDays`、`runtimeEventCleanupSchedule` 通过 `/api/v1/settings/runtime` 暴露。记录保留时间必须大于或等于详情保留时间，校验失败不自动吞掉错误配置。
- `runtimeevents.CleanupWorker` 独立于 metrics cleanup：先清理详情并把事件与应用操作投影标记为详情不可用，再按记录保留时间删除摘要和投影。

## 首批写入点

- `internal/modules/applications` 在 lifecycle operation 创建、target queued、dispatcher claim target、target succeeded/failed、operation completed/failed 时写入应用类事件（仅作系统事件页诊断；协调记录不依赖这些事件）。
- `internal/modules/tasks` 在任务创建、开始、完成、失败、可重试失败、取消、重试时写入任务类事件，任务日志引用写入日志类事件。日志事件只保存日志引用和摘要，不复制完整日志正文。
- 应用 lifecycle 以 `application_lifecycle_operations` / `application_lifecycle_targets` 作为 durable 协调事实（位于协调库）；协调记录页读取时直接聚合这两张表，不建投影。

## 验证

- 后端事件表、写入服务、settings、清理 worker 或写入点改动后运行 `task test:backend`。
- 前端页面、API、路由、Mock 或类型改动后，按范围运行 `task test:web` 或 `task build:web`。

## 文档更新触发

新增事件类型、列表筛选字段、详情字段、保留策略展示、导航入口或旧任务中心兼容边界变化时，必须更新本文档。
