# 旧功能模块指引（非规范）

本目录已由 [项目设计与验收规范](../acceptance/README.md) 替代。这里保留的文件只用于追查历史实现背景，不再是后续开发、评审或验收的行为依据，也不要求随功能改动继续维护。

处理任何功能改动时必须从 [验收规范索引](../acceptance/README.md) 定位稳定验收编号；代码与旧模块指引冲突时，不得据此覆盖验收合同。

## 替代关系

| 旧指引 | 规范性替代文档 |
| --- | --- |
| 启动装配、API 路由、配置、存储、认证、运行时设置、系统版本与更新检查 | [backend-core.md](backend-core.md) |
| 轻量 ORM（模型注册、链式查询、CRUD、AutoMigrate、迁移步骤） | [database-orm.md](database-orm.md) |
| 全量备份导出、维护模式、启动期覆盖还原、恢复模式页面 | [backup-restore.md](backup-restore.md) |
| Vue 页面、API client、Pinia store、路由、样式和前端测试 | [frontend.md](frontend.md) |
| 服务器、SSH 凭据、系统探测、UFW、概览指标、APT 软件包 | [servers.md](servers.md) |
| 后台任务、任务步骤、任务日志、重试、手动运行、周期调度 | [tasks-scheduler.md](tasks-scheduler.md) |
| 统一只追加日志、事件流、操作聚合、证据、查询与导出 | [activity.md](activity.md) |
| Agent 执行事件持久缓冲、补传 ACK、未知结果核对 | [agent-execution-events.md](agent-execution-events.md) |
| 应用定义、appspec、文件、修订、部署、运行时、日志、镜像更新 | [applications.md](applications.md) |
| Docker 容器、镜像、网络、卷、设施应用、容器操作队列、镜像检查和 Application 容器协调 | [containerization.md](containerization.md) |
| DNS 域名、Cloudflare、ACME 证书、证书续签 | [dns-certificates.md](dns-certificates.md) |
| GitHub Actions、main/dev 自动版本发布、版本注入、目标平台二进制编译矩阵、容器镜像和 GitHub Release 创建 | [release-workflow.md](release-workflow.md) |
| 用户可见文案、翻译 key、语言设置、后端错误翻译 | [../i18n-guide.md](../i18n-guide.md) 和 [../i18n-translation-status.md](../i18n-translation-status.md) |

## 常见跨模块关系

- 应用部署依赖 `modules/applications`、`agent`、`modules/servers`、`modules/tasks`，反向代理还会读取证书模块。
- 应用部署控制面位于 `internal/orchestrator`：AppDB 的 immutable revision、instance desired/observed 与 Job lease 由 Planner/Controller 管理，Agent 通过 `RuntimeReconcile` 执行；任务表仅承担 AppDB 内执行控制；历史由 AppDB activity_events 追加事实及 LogDB 投影查询。CoordDB 不再注册模型，不允许从当前 Job/Instance 反推历史。
- 服务器 agent 健康检查依赖 `modules/servers`、`agent`、`modules/tasks`；应用 runtime 和设施应用操作通过 agent 调用 Docker Engine API。
- DNS 证书签发依赖 `modules/certificates` 和 `modules/tasks`，证书变量会被应用模块解析。
- 软件包维护和指标采集依赖 `modules/servers`、`platform/ssh`、`platform/linux`、`modules/tasks`，结果分别落在应用数据库和指标数据库。
- 前端页面改动通常同时影响 `web/src/api/`、`web/src/types/api.ts`、`web/src/i18n/index.ts` 和对应 feature 页面。
