# 服务器、凭据、指标与软件包

## 适用场景

修改 SSH 凭据、服务器登记、连通性测试、系统探测、sudo 检查、UFW 安装、概览指标采集、APT 软件包刷新或升级时，先读本文档。

## 后端入口

- SSH 凭据：`internal/credential/`
- 服务器：`internal/server/`
- SSH 执行器：`internal/sshx/`
- Linux 发行版适配：`internal/linux/`
- 指标采集：`internal/metrics/`
- 概览聚合：`internal/overview/`
- 软件包维护：`internal/packages/`
- 调度触发：`internal/scheduler/`
- 任务记录：`internal/tasks/`
- 路由装配：`internal/app/app.go`

## 前端入口

- 服务器与凭据页面：`web/src/features/servers/pages/ServersPage.vue`
- 服务器选择器：`web/src/components/ServerSelector.vue`
- 软件包页面：`web/src/features/packages/pages/PackageUpdatesPage.vue`
- 概览页面：`web/src/features/overview/pages/OverviewPage.vue`
- API：`web/src/api/servers.ts`、`web/src/api/packages.ts`、`web/src/api/overview.ts`
- 类型：`web/src/types/api.ts`

## API 范围

- 凭据：`GET/POST /api/v1/credentials`，`PUT/DELETE /api/v1/credentials/{id}`
- 服务器：`GET/POST /api/v1/servers`，`POST /api/v1/servers/probe`，`PUT/DELETE /api/v1/servers/{id}`
- 服务器操作：`POST /api/v1/servers/{id}/test`，`POST /api/v1/servers/{id}/ufw/install`
- 指标：`GET /api/v1/servers/{id}/metrics`
- 软件包：`GET /api/v1/servers/{id}/packages/updates`，`POST /api/v1/servers/{id}/packages/refresh`，`POST /api/v1/servers/{id}/packages/upgrade-selected`，`POST /api/v1/servers/{id}/packages/upgrade-all`
- 概览：`GET /api/v1/overview`

## 数据与行为约定

- `servers` 和 `credentials` 在应用数据库，指标快照在指标数据库。
- 系统探测通过 SSH 执行远程命令，并交给 `internal/linux/` 解析支持的 Debian/Ubuntu 版本。
- 维护操作通常要求 root 或免密 sudo；相关检查结果写回服务器记录。
- 软件包维护基于 APT，只对支持的系统执行。
- 长耗时操作应记录为任务，日志和步骤交给 `internal/tasks/`。

## 验证

- 先按模块索引的“检查和测试范围”判断是否需要验证。
- 需要验证后端改动时，运行 `task test:backend`，重点关注 `server`、`credential`、`linux`、`metrics`、`packages` 相关测试。
- 前端页面或 API 类型改动只按需要运行 `task test:web` 或 `task build:web`。

## 文档更新触发

新增支持系统、远程命令、服务器字段、凭据字段、指标字段、软件包行为或相关 API 时，必须更新本文档。
