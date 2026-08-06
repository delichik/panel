# Panel

[English](README.md) | 简体中文

Panel 是一个 alpha 阶段的 Linux 服务器运维面板，适合管理小规模自有服务器。它可以通过 SSH 接入 Debian 和 Ubuntu 服务器，查看基础健康状态，执行软件包维护，通过 panel-agent 和 Docker Engine API 部署容器应用，并在同一个 Web 界面里管理 DNS 和 ACME 证书。

这个项目面向两类人：想把服务器管得更省心的普通用户，以及愿意一起读代码、改代码、分享代码的人类协作者。它的技术结构尽量保持直接：Go 后端、Vue 前端、本地 SQLite 数据，以及一组容易运行的 Task 命令。

## 能做什么

- 添加 SSH 凭据并登记服务器。
- 探测服务器连通性、系统信息、硬件特征和免密 sudo 状态。
- 采集 CPU、内存、磁盘、网络、运行时间、内核和负载等概览指标。
- 刷新 APT 可升级软件包，并执行选择性升级或整机升级。
- 在支持的系统上安装 UFW。
- 使用 mTLS 为服务器部署 panel-agent，并配置 Docker host。
- 检查 agent 兼容性、Docker 健康状态和应用运行时状态。
- 通过 panel-agent 调用 Docker Engine API 部署 Docker 应用。
- 配置应用文件、挂载、端口、调度位置、运行时操作、日志和反向代理路由。
- 管理 Cloudflare 域名并签发 ACME 证书。
- 查看后台任务和任务日志。
- 在英文和简体中文界面之间切换。

## 当前状态

Panel 目前仍处于 alpha 阶段。它已经具备一些可用工作流，但配置、数据库迁移和界面行为仍可能随着项目发展而变化。建议先在开发环境或非关键服务器上试用；如果用于真实工作，请备份 Panel 数据卷。

## 支持的目标系统

Panel 的系统支持范围是显式列出的，后续会逐步扩展。

当前支持：

- Debian 12
- Debian 13
- Ubuntu 20.04 LTS
- Ubuntu 22.04 LTS
- Ubuntu 24.04 LTS
- Ubuntu 24.10
- Ubuntu 25.04
- Ubuntu 25.10
- Ubuntu 26.04 LTS

说明：

- Panel 通过 SSH 管理服务器。
- 支持密码和私钥两种 SSH 凭据。
- 很多维护操作需要 root 或免密 sudo。
- 软件包维护基于 APT。
- 应用运行时要求目标服务器部署 panel-agent，并能访问 Docker Engine 端点。默认 Docker host 为 `unix:///var/run/docker.sock`。

## 开始使用

普通用户只通过容器部署 Panel：

- [使用 Docker Compose 或 Docker 部署 Panel](docs/deployment.zh-CN.md)
- [按照首次使用和应用部署说明完成配置](docs/user-guide.zh-CN.md)

部署说明包含持久化、首次登录、HTTPS、备份、升级和常见问题；使用说明按照凭据、服务器、panel-agent、Docker 状态、第一个应用、域名证书、任务和日常维护的顺序介绍。

## 开发

以下内容用于从源码开发 Panel，普通部署不需要这些工具或命令。

### 环境要求

- Go 1.25+
- Node.js 22+
- npm
- [Task](https://taskfile.dev/)

只有构建或测试生产容器镜像时才需要 Docker。

### 从源码运行

安装前端依赖：

```bash
npm --prefix web ci
```

复制示例配置：

```bash
cp config.example.json config.json
export PANEL_CONFIG=./config.json
```

PowerShell：

```powershell
Copy-Item config.example.json config.json
$env:PANEL_CONFIG = ".\config.json"
```

启动后端：

```bash
task run:backend
```

另开一个终端启动前端开发服务器：

```bash
task run:web
```

访问 `http://127.0.0.1:5173`。Vite 会把 `/api` 请求代理到后端。

本地开发账号为 `admin/admin`，首次使用仍会要求修改密码。

### 开发配置

Panel 按以下顺序加载配置：

1. 内置默认值。
2. `PANEL_CONFIG` 指向的 JSON 配置文件。
3. 环境变量。

常用配置项：

| 配置项 | 用途 | 默认值 |
| --- | --- | --- |
| `listenAddress` | 后端监听地址 | `127.0.0.1:8080` |
| `dataRoot` | Panel 数据根目录 | `data` |
| `appDatabase` | 主 SQLite 数据库 | `data/db/app.db` |
| `metricsDatabase` | 指标 SQLite 数据库 | `data/db/metrics.db` |
| `certificates.acmeDirectoryUrl` | ACME 目录地址 | Let's Encrypt 正式环境 |

支持的环境变量：

- `PANEL_CONFIG`
- `PANEL_LISTEN_ADDRESS`
- `PANEL_DATA_ROOT`
- `PANEL_APP_DATABASE`
- `PANEL_METRICS_DATABASE`
- `PANEL_CERT_ACME_DIRECTORY_URL`
- `PANEL_WEB_PROXY_TARGET`，仅用于前端开发代理

管理员账号、JWT 密钥、远程命令超时、证书邮箱、语言、Token 有效期、指标保留、安全设置和证书默认值保存在应用数据库中，并通过界面的 **设置** 管理。

### 常用命令

```bash
task run:backend
task run:web
task test:backend
task test:web
task build:backend
task build:web
task build
```

### 目录结构

```text
cmd/panel              后端入口
internal/              后端服务、处理器、存储和集成逻辑
web/                   Vue 3 前端工程
web/src/i18n/          前端翻译
docs/agents/           仓库协作和 i18n 说明
tmp/                   测试和构建产生的临时文件
Taskfile.yml           常用任务入口
config.example.json    示例运行配置
Dockerfile             生产容器构建
```

常用代码入口：

- 后端启动：`cmd/panel/main.go`
- 路由装配与静态页面托管：`internal/bootstrap/panel/app.go`
- 数据库迁移：`internal/platform/database/migrations.go`
- 目标系统适配：`internal/platform/linux/`
- Agent runtime 与 Docker API 逻辑：`internal/agent/`、`internal/modules/applications/runtime/`
- 应用部署逻辑：`internal/modules/applications/`
- 前端路由：`web/src/router/index.ts`
- 前端 i18n 初始化：`web/src/i18n/index.ts`

## 修改文案

Panel 的界面文案支持英文和简体中文。修改应用内用户可见文案时，请遵守 `docs/agents/i18n-guide.md`，并同步更新 `docs/agents/i18n-translation-status.md`。

## 许可证

当前仓库还没有包含许可证文件。如果你计划复用或再分发这个项目，请先和项目所有者确认授权方式。
