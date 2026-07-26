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

- “操作记录”是“应用”一级菜单下的产品入口，展示用户主动操作和系统自动协调产生的应用变更，不复用旧 `/tasks` 页面；路由保持 `/application-operations`。
- “系统事件”用于诊断，展示后端提供的运行事件类型、严重级别、关联对象、来源、摘要和详情引用，不承担应用工作进度页职责。
- 旧 `/tasks` 路由可保留兼容，但不在主导航、概览快捷入口或应用详情入口中出现，也不重定向到新页面。
- 应用详情的操作记录入口跳转到 `/application-operations?applicationId=<id>`，由应用操作投影查询承载最近操作，不维护独立应用操作历史逻辑。
- 详情保留和记录保留是不同配置。详情已清理时列表仍显示摘要，详情按钮必须禁用并提示 `详情已清理` / `Detail has been cleaned up`。
- 前端不假设独立告警服务。系统事件页只按 `system-events` API 返回的 `category` / `eventType` / `severity` 展示。
- 事件 `category` 只使用 `application`、`task`、`alert`、`log`、`runtime`、`system`。任务事件写入 `task`，任务日志引用写入 `log`，应用操作事件写入 `application`。
- 新事件系统不迁移旧任务历史；空态必须说明只显示启用后产生的新记录。
- 操作记录和系统事件页的筛选变化必须先把页码归一到第一页，并只发起一次有效列表加载；非法 URL 页码回退为第一页。列表与详情只允许最新请求提交状态，避免快速筛选、翻页或切换详情时旧响应覆盖当前内容。

## 后端实现入口

- 后端运行事件模块位于 `internal/modules/runtimeevents/`，包含事件写入服务、应用操作投影查询、系统事件查询、HTTP handler 和独立清理 worker。
- 事件摘要表 `runtime_events`、详情表 `runtime_event_details`、应用操作投影表 `application_operation_records` 都位于 `Store.LogDB()`，通过 `internal/platform/database/migrations.go` 创建；runtime settings 仍位于 `Store.AppDB()` 的 `runtime_settings`。
- `/api/v1/application-operations` 查询 `application_operation_records` 投影，不实时扫描事件流聚合；详情接口返回 `{ operation, events, targets }`，当前无专门 target 明细投影时 `targets` 返回空数组。
- `/api/v1/system-events` 查询 `runtime_events`，默认不固定 `category=system`；详情接口返回 `{ event, payload, logRefs, taskRefs, targetRefs }`，详情过期后引用字段返回空数组并保留摘要事件。
- `runtimeEventRetentionDays`、`runtimeEventDetailRetentionDays`、`runtimeEventCleanupSchedule` 通过 `/api/v1/settings/runtime` 暴露。记录保留时间必须大于或等于详情保留时间，校验失败不自动吞掉错误配置。
- `runtimeevents.CleanupWorker` 独立于 metrics cleanup：先清理详情并把事件与应用操作投影标记为详情不可用，再按记录保留时间删除摘要和投影。

## 首批写入点

- `internal/modules/applications` 在 lifecycle operation 创建、target queued、dispatcher claim target、target succeeded/failed、operation completed/failed 时写入应用类事件并更新应用操作投影。
- `internal/modules/tasks` 在任务创建、开始、完成、失败、可重试失败、取消、重试时写入任务类事件，任务日志引用写入日志类事件。日志事件只保存日志引用和摘要，不复制完整日志正文。
- 应用 lifecycle 仍以 `application_lifecycle_operations` / `application_lifecycle_targets` 作为 durable 协调事实；操作记录是面向页面的投影，不替代 lifecycle 状态机。

## 验证

- 后端事件表、写入服务、settings、清理 worker 或写入点改动后运行 `task test:backend`。
- 前端页面、API、路由、Mock 或类型改动后，按范围运行 `task test:web` 或 `task build:web`。

## 文档更新触发

新增事件类型、列表筛选字段、详情字段、保留策略展示、导航入口或旧任务中心兼容边界变化时，必须更新本文档。
