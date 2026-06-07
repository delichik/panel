# Panel

[English](README.md) | 简体中文

Panel 是一个 alpha 阶段的 Linux 服务器运维面板，适合管理小规模自有服务器。它可以通过 SSH 接入 Debian 和 Ubuntu 服务器，查看基础健康状态，执行软件包维护，引导或加入 Nomad 节点，部署容器应用，并在同一个 Web 界面里管理 DNS 和 ACME 证书。

这个项目面向两类人：想把服务器管得更省心的普通用户，以及愿意一起读代码、改代码、分享代码的人类协作者。它的技术结构尽量保持直接：Go 后端、Vue 前端、本地 SQLite 数据，以及一组容易运行的 Task 命令。

## 能做什么

- 添加 SSH 凭据并登记服务器。
- 探测服务器连通性、系统信息、硬件特征和免密 sudo 状态。
- 采集 CPU、内存、磁盘、网络、运行时间、内核和负载等概览指标。
- 刷新 APT 可升级软件包，并执行选择性升级或整机升级。
- 在支持的系统上安装 UFW。
- 引导 Nomad server，或把服务器加入为 Nomad client。
- 查看 Nomad 节点、任务、部署、评估和服务。
- 通过 Nomad 部署 Docker 应用。
- 配置应用文件、变量、挂载、端口、调度位置、运行时操作、日志和反向代理路由。
- 管理 Cloudflare 域名并签发 ACME 证书。
- 查看后台任务和任务日志。
- 在英文和简体中文界面之间切换。

## 当前状态

Panel 目前仍处于 alpha 阶段。它已经具备一些可用工作流，但配置、数据库迁移和界面行为仍可能随着项目发展而变化。建议先在开发环境或非关键服务器上试用；如果用于真实工作，请备份 `data` 目录。

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
- Nomad 设置流程会在需要时为受支持系统安装 Nomad、Docker 和 CNI 插件。

## 快速开始

### 环境要求

在开发或运行 Panel 的机器上安装：

- Go 1.25+
- Node.js 22+
- npm
- [Task](https://taskfile.dev/)

Docker 只在需要容器化部署时使用。

### 1. 安装前端依赖

```bash
npm --prefix web ci
```

如果你没有使用锁文件流程，也可以执行：

```bash
npm --prefix web install
```

### 2. 创建配置文件

```bash
cp config.example.json config.json
```

然后指定配置文件路径：

```bash
export PANEL_CONFIG=./config.json
```

PowerShell:

```powershell
$env:PANEL_CONFIG = ".\config.json"
```

默认本地登录账号：

- 用户名：`admin`
- 密码：`admin`

Panel 首次使用时会要求修改密码，并在密码修改后自动轮换 JWT 签名密钥。

### 3. 启动后端

```bash
task run:backend
```

后端默认监听 `127.0.0.1:8080`。

### 4. 启动前端界面

另开一个终端：

```bash
task run:web
```

访问 `http://127.0.0.1:5173`。开发模式下，Vite 会把 `/api` 请求代理到后端。

## 配置

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
| `nomad.address` | Nomad HTTP API 地址 | `http://127.0.0.1:4646` |
| `nomad.token` | Nomad ACL token，如启用 ACL 时使用 | 空 |
| `certificates.acmeDirectoryUrl` | ACME 目录地址 | Let's Encrypt 正式环境 |

管理员用户名和密码、JWT 密钥、远程命令超时、Nomad namespace/region/datacenter、证书邮箱和证书 DNS 生效等待时间，会保存在应用数据库中，并在界面的 **设置** 中配置。

支持的环境变量：

- `PANEL_CONFIG`
- `PANEL_LISTEN_ADDRESS`
- `PANEL_DATA_ROOT`
- `PANEL_APP_DATABASE`
- `PANEL_METRICS_DATABASE`
- `PANEL_CERT_ACME_DIRECTORY_URL`

语言、登录令牌有效期、指标保留时间、Nomad 作用域、安全设置和证书默认值等运行时设置，可以在界面中调整。

仅开发前端代理使用的环境变量：

- `PANEL_WEB_PROXY_TARGET`

## Docker

构建镜像：

```bash
docker build -t panel .
```

运行容器：

```bash
docker run --rm -p 8080:8080 -v panel-data:/app/data panel
```

容器默认行为：

- 监听 `0.0.0.0:8080`。
- 将数据保存在 `/app/data`。
- 由后端直接托管构建后的前端页面。

如果用于长期运行，请挂载或备份数据卷。包括 JWT 密钥在内的安全设置会保存在数据卷中。

## 开发

常用命令：

```bash
task run:backend
task run:web
task test:backend
task test:web
task build:backend
task build:web
task build
```

目录结构：

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
- 路由装配与静态页面托管：`internal/app/app.go`
- 数据库迁移：`internal/storage/migrations.go`
- 目标系统适配：`internal/linux/`
- Nomad 控制面逻辑：`internal/nomad/`
- 应用部署逻辑：`internal/applications/`
- 前端路由：`web/src/router/index.ts`
- 前端 i18n 初始化：`web/src/i18n/index.ts`

## 修改文案

Panel 的界面文案支持英文和简体中文。修改应用内用户可见文案时，请遵守 `docs/agents/i18n-guide.md`，并同步更新 `docs/agents/i18n-translation-status.md`。

## 许可证

当前仓库还没有包含许可证文件。如果你计划复用或再分发这个项目，请先和项目所有者确认授权方式。
