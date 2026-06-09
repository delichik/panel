# 应用模块

## 适用场景

修改应用创建、编辑、appspec、变量解析、应用文件、保存会话、修订、Nomad job 渲染、部署、停止、重启、日志、运行时状态、镜像更新或应用反向代理时，先读本文档。

## 后端入口

- 应用服务与 handler：`internal/applications/`
- 应用规格模型、校验和渲染：`internal/appspec/`
- 调度位置选择：`internal/orchestrator/`
- 模板渲染接口：`internal/templatex/`
- Nomad client：`internal/nomad/`
- 任务记录：`internal/tasks/`
- 路由装配和跨模块连接：`internal/app/app.go`

## 前端入口

- 应用页面：`web/src/features/applications/pages/ApplicationsPage.vue`
- 编辑器：`web/src/features/applications/components/ApplicationEditor.vue`
- 详情：`web/src/features/applications/components/ApplicationDetail.vue`
- 运行时：`web/src/features/applications/components/ApplicationRuntimePanel.vue`
- 日志：`web/src/features/applications/components/ApplicationLogsPanel.vue`
- API：`web/src/api/applications.ts`
- 类型：`web/src/types/api.ts`

## API 范围

- 应用 CRUD：`GET/POST /api/v1/applications`，`GET/PUT/DELETE /api/v1/applications/{id}`
- 应用文件：`GET/POST /api/v1/applications/{id}/files`，`DELETE /api/v1/applications/{id}/files/{fileId}`
- 保存会话：`POST /api/v1/application-save-sessions`，`POST /api/v1/application-save-sessions/{id}/files`，`POST /api/v1/application-save-sessions/{id}/files/delete`，`POST /api/v1/application-save-sessions/{id}/commit`
- 校验和计划：`POST /api/v1/applications/{id}/validate`，`POST /api/v1/applications/{id}/plan`
- 运行操作：`POST /api/v1/applications/{id}/deploy`，`/stop`，`/restart`
- 镜像：`POST /api/v1/applications/{id}/image/check`，`/image/update`
- 运行时和日志：`GET /api/v1/applications/{id}/runtime`，`GET /api/v1/applications/{id}/logs`
- 打包：`GET /api/v1/applications/{id}/package`

## 数据与行为约定

- 主要表包括 `applications`、`application_files`、`application_revisions`。
- 应用规格以 YAML 输入，经过 `internal/appspec/` 校验和渲染为 Nomad job。
- 应用变量、部署模式、反向代理配置等持久化字段必须保存稳定结构，不保存已翻译展示文案。
- 文件内容通过 API 以 base64 承载，保存会话用于批量上传、删除和提交。
- 启用应用、部署、镜像更新等流程需要先校验和计划，再注册 Nomad job。
- 运行时状态、部署、评估和日志部分来自 Nomad，第三方原始描述通常保留原样。
- 证书模块提供内置变量解析，Nomad 模块负责反向代理同步。

## 验证

- 先按模块索引的“检查和测试范围”判断是否需要验证。
- 需要验证后端改动时，运行 `task test:backend`，重点关注 `internal/applications`、`internal/appspec`、`internal/orchestrator`。
- 前端应用页面或 API 类型改动只按需要运行 `task test:web` 或 `task build:web`。

## 文档更新触发

新增 appspec 字段、应用持久化字段、API、应用文件行为、部署流程、镜像更新逻辑、反向代理字段或运行时展示字段时，必须更新本文档。
