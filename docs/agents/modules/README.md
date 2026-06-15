# 功能模块指引索引

本目录把 Panel 的功能说明拆成小文档。处理任务时先在本索引定位模块，再只加载对应文档；不要为了一个小改动一次性阅读所有模块指引。

## 使用方式

1. 先读仓库根目录 [AGENTS.md](../../../AGENTS.md)。
2. 在下表找到任务涉及的功能模块。
3. 只加载相关模块文档；跨模块改动再按依赖补读相邻模块。
4. 修改功能时，同步更新对应模块文档；新增模块时，新增文档并更新本索引。

## 模块索引

| 任务范围 | 优先阅读 |
| --- | --- |
| 启动装配、API 路由、配置、存储、认证、运行时设置、系统版本与更新检查 | [backend-core.md](backend-core.md) |
| Vue 页面、API client、Pinia store、路由、样式和前端测试 | [frontend.md](frontend.md) |
| 服务器、SSH 凭据、系统探测、UFW、概览指标、APT 软件包 | [servers.md](servers.md) |
| 后台任务、任务步骤、任务日志、重试、手动运行、周期调度 | [tasks-scheduler.md](tasks-scheduler.md) |
| 应用定义、appspec、文件、修订、部署、运行时、日志、镜像更新 | [applications.md](applications.md) |
| DNS 域名、Cloudflare、ACME 证书、证书续签 | [dns-certificates.md](dns-certificates.md) |
| GitHub Actions、main 自动版本发布、版本注入、容器镜像和 GitHub Release 创建 | [release-workflow.md](release-workflow.md) |
| 用户可见文案、翻译 key、语言设置、后端错误翻译 | [../i18n-guide.md](../i18n-guide.md)，[../i18n-translation-status.md](../i18n-translation-status.md) |

## 常见跨模块关系

- 应用部署依赖 `applications`、`appspec`、`agent`、`appruntime`、`server`、`tasks`，反向代理还会读取证书模块。
- 服务器 agent 健康检查依赖 `server`、`agent`、`tasks`；应用 runtime 操作通过 agent 调用 Docker Engine API。
- DNS 证书签发依赖 `dns`、`certs`、`tasks`，证书变量会被应用模块解析。
- 软件包维护和指标采集依赖 `server`、`sshx`、`linux`、`tasks`，结果分别落在应用数据库和指标数据库。
- 前端页面改动通常同时影响 `web/src/api/`、`web/src/types/api.ts`、`web/src/i18n/index.ts` 和对应 feature 页面。
