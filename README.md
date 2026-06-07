# Panel

Panel 是一个面向 Linux 服务器的运维控制面板，采用 Go 后端和 Vue 3 前端的一体化架构。项目当前聚焦于通过 SSH 管理 Debian / Ubuntu 服务器，并围绕 Nomad、应用部署、软件包更新、DNS 与证书管理提供统一操作入口。

## 当前能力

- SSH 凭据与服务器接入管理
- 服务器探测、基础信息采集与总览看板
- 软件包刷新、选择性升级与整机升级任务
- 应用配置编辑、打包、部署、停止、重启与运行态查看
- Nomad 首节点引导、节点加入、反向代理配置与集群状态查看
- Cloudflare 域名管理
- ACME 证书申请与续期任务
- 任务中心与后台调度
- 中英文界面切换

## 技术栈

- 后端：Go
- 前端：Vue 3 + Vite + TypeScript + Pinia + Vue Router + Vuetify
- 存储：本地 SQLite
- 编排：HashiCorp Nomad

## 使用

### 环境要求

- Go
- Node.js 22+
- npm
- Task

如果本地没有 `task`，请先安装 [go-task](https://taskfile.dev/)。

### 快速开始

#### 1. 安装前端依赖

```bash
npm --prefix web install
```

#### 2. 准备配置文件

可直接基于示例配置启动：

```bash
cp config.example.json config.json
```

然后通过环境变量指定配置文件：

```bash
export PANEL_CONFIG=./config.json
```

Windows PowerShell:

```powershell
$env:PANEL_CONFIG = ".\config.json"
```

说明：

- 未显式配置管理员密码哈希时，默认管理员账户为 `admin`
- 默认密码为 `admin`
- 正式环境请务必修改 `jwtSecret`

#### 3. 启动后端

```bash
task run:backend
```

默认监听地址为 `127.0.0.1:8080`。

#### 4. 启动前端开发服务

另开一个终端执行：

```bash
task run:web
```

默认访问地址：

- 前端开发环境：`http://127.0.0.1:5173`
- 后端接口：`http://127.0.0.1:8080`

开发模式下，Vite 会将 `/api` 请求代理到后端。

### 配置说明

程序会先读取默认值，再按以下顺序覆盖：

1. `PANEL_CONFIG` 指向的 JSON 配置文件
2. 相关环境变量

核心配置项如下：

| 配置项 | 说明 | 默认值 |
| --- | --- | --- |
| `listenAddress` | 服务监听地址 | `127.0.0.1:8080` |
| `adminUsername` | 管理员用户名 | `admin` |
| `jwtSecret` | 登录态签名密钥 | `change-me-panel-jwt-secret` |
| `dataRoot` | 数据根目录 | `data` |
| `appDatabase` | 应用数据库路径 | `data/db/app.db` |
| `metricsDatabase` | 指标数据库路径 | `data/db/metrics.db` |
| `remoteCommandTimeoutSeconds` | 远程命令超时时间 | `30` |
| `nomad.address` | Nomad API 地址 | `http://127.0.0.1:4646` |
| `nomad.namespace` | Nomad namespace | `default` |
| `nomad.region` | Nomad region | `global` |
| `nomad.datacenter` | Nomad datacenter | `dc1` |
| `certificates.acmeDirectoryUrl` | ACME 目录地址 | Let's Encrypt 正式环境 |
| `certificates.email` | 证书申请邮箱 | 空 |
| `certificates.dnsPropagationDelaySeconds` | DNS 生效等待秒数 | `30` |

已支持的环境变量包括：

- `PANEL_LISTEN_ADDRESS`
- `PANEL_ADMIN_USERNAME`
- `PANEL_ADMIN_PASSWORD_HASH`
- `PANEL_JWT_SECRET`
- `PANEL_DATA_ROOT`
- `PANEL_APP_DATABASE`
- `PANEL_METRICS_DATABASE`
- `PANEL_REMOTE_COMMAND_TIMEOUT_SECONDS`
- `PANEL_CERT_ACME_DIRECTORY_URL`
- `PANEL_CERT_EMAIL`
- `PANEL_CERT_DNS_PROPAGATION_DELAY_SECONDS`

### Docker

仓库已提供多阶段构建的 [Dockerfile](C:/Users/illya/Desktop/panel/Dockerfile)。

示例：

```bash
docker build -t panel .
docker run --rm -p 8080:8080 -v $(pwd)/config:/config panel
```

容器默认：

- 基于 `lscr.io/linuxserver/baseimage-alpine:3.22`
- 监听 `0.0.0.0:8080`
- 将配置与数据写入 `/config`
- 自动托管构建后的前端页面

## 开发

### 目录结构

```text
cmd/panel           程序入口
internal/           后端核心业务
web/                前端工程
docs/agents/        仓库内协作与 i18n 规范
data/               默认数据目录
tmp/                测试与构建缓存
Taskfile.yml        开发任务入口
config.example.json 示例配置
```

### 构建

构建前后端：

```bash
task build
```

分别构建：

```bash
task build:backend
task build:web
```

说明：

- `task build:web` 会产出 `web/dist`
- 后端启动后，如果检测到 `web/dist` 存在，会直接托管前端静态资源

### 测试

运行全部测试：

```bash
task test
```

分别执行：

```bash
task test:backend
task test:web
```

### 开发说明

- 后端入口在 [cmd/panel/main.go](C:/Users/illya/Desktop/panel/cmd/panel/main.go)
- 路由装配与静态资源托管在 [internal/app/app.go](C:/Users/illya/Desktop/panel/internal/app/app.go)
- 前端路由在 [web/src/router/index.ts](C:/Users/illya/Desktop/panel/web/src/router/index.ts)
- 多语言实现位于 [web/src/i18n/index.ts](C:/Users/illya/Desktop/panel/web/src/i18n/index.ts)

仓库内开发协作还需注意：

- 多语言相关改动需先阅读 [docs/agents/i18n-guide.md](C:/Users/illya/Desktop/panel/docs/agents/i18n-guide.md)
- 翻译状态记录在 [docs/agents/i18n-translation-status.md](C:/Users/illya/Desktop/panel/docs/agents/i18n-translation-status.md)
- 测试和构建缓存统一放在 `tmp/`

## 后续可补充

如果后面需要，我可以继续把 README 补成更完整的版本，例如：

- 部署架构图
- 首次接入服务器的操作流程
- Nomad 引导教程
- 应用模板与变量说明
- API 概览
