# 服务器、凭据、指标与软件包

## 适用场景

修改 SSH 凭据、服务器登记、Docker host 配置、agent 部署、连通性测试、系统探测、sudo 检查、UFW、概览指标采集、APT 软件包刷新或升级时，先读本文档。

## 后端入口

- SSH 凭据：`internal/credential/`
- 服务器：`internal/server/`
- Agent 客户端与 TLS：`internal/agent/`
- SSH 执行器：`internal/sshx/`
- Linux 发行版适配：`internal/linux/`
- 通用远程运维操作：`internal/remoteops/`
- 指标采集：`internal/metrics/`
- 概览聚合：`internal/overview/`
- 软件包维护：`internal/packages/`
- 调度触发：`internal/scheduler/`
- 任务记录：`internal/tasks/`
- 路由装配：`internal/app/app.go`

## 前端入口

- 服务器与凭据页面：`web/src/views/servers/_shared/ServersPageContent.vue`
- 服务器选择器：`web/src/components/ServerSelector.vue`
- 软件包页面：`web/src/views/servers/packages/index.vue`
- 防火墙页面：`web/src/views/servers/firewall/index.vue`
- 概览页面：`web/src/views/overview/index.vue`
- API：`web/src/api/servers.ts`、`web/src/api/packages.ts`、`web/src/api/overview.ts`
- 类型：`web/src/types/api.ts`

## API 范围

- 凭据：`GET/POST /api/v1/credentials`，`PUT/DELETE /api/v1/credentials/{id}`
- 服务器：`GET/POST /api/v1/servers`，`POST /api/v1/servers/probe`，`PUT/DELETE /api/v1/servers/{id}`
- Agent 证书包：`POST /api/v1/servers/{id}/agent/certificate`
- 服务器操作：`POST /api/v1/servers/{id}/test`，`POST /api/v1/servers/{id}/restart`，`POST /api/v1/servers/{id}/ufw/install`
- UFW：`GET /api/v1/servers/{id}/ufw`，`POST /api/v1/servers/{id}/ufw/enable`，`POST /api/v1/servers/{id}/ufw/rules`，`DELETE /api/v1/servers/{id}/ufw/rules/{number}`
- 指标：`GET /api/v1/servers/{id}/metrics`
- 软件包：`GET /api/v1/servers/{id}/packages/updates`，`POST /api/v1/servers/{id}/packages/refresh`，`POST /api/v1/servers/{id}/packages/upgrade-selected`，`POST /api/v1/servers/{id}/packages/upgrade-all`
- 概览：`GET /api/v1/overview`
- 概览卡片布局：`GET/PUT /api/v1/overview/cards`
- 概览卡片数据：`GET /api/v1/overview/cards/{cardId}/data`

## 数据与行为约定

- `servers` 和 `credentials` 在应用数据库，指标快照在指标数据库。
- 服务器创建/编辑必须配置 `dockerHost`，默认值为 `unix:///var/run/docker.sock`。该值会写入 agent 部署包的 `PANEL_AGENT_DOCKER_HOST`，agent 使用 Docker Engine API 与 Docker 通信，不调用 Docker CLI。
- 新增服务器响应可携带 `initialTaskId` 指向首次信息采集任务；首次采集失败时必须标记任务失败并删除刚创建的服务器记录，让用户回到表单修正 SSH 信息。
- 系统探测通过 SSH 或已启用的 agent 读取远端信息，并交给 `internal/linux/` 解析支持的 Debian/Ubuntu 版本。
- 系统探测写入 `sys.*` traits；网卡采集要求 `/sys/class/net/{name}/device` 存在，并过滤 Docker、veth、bridge、CNI、隧道和 overlay 等常见虚拟接口。
- SSH 密码、私钥和私钥口令统一封装到 `credentials.secret_ciphertext`，并通过 `internal/secretstore` 加密；不得通过 API 响应或任务日志返回秘密内容。
- 安装软件、日志化 sudo 命令、sudo 写文件和 UFW allow/delete/status 优先复用 `internal/remoteops/`。
- 软件包维护基于 APT，只对支持的系统执行；刷新和升级依赖远程 sudo。
- `POST /api/v1/servers/{id}/packages/refresh` 创建或复用 `package_refresh` 任务并返回 `taskId`；调度器一轮多服务器刷新必须共享同一个 `operationId`。
- 周期性指标采集记录为 `metrics_collect` 任务；同一轮多服务器采集共享一个 `operationId`，任务中心默认常用类型会隐藏该高频类型。
- `POST /api/v1/servers/{id}/ufw/install` 返回 `server_ufw_install` 任务；该任务由内存 goroutine 执行，创建后必须先标记为 `running` 再返回。
- `POST /api/v1/servers/{id}/restart` 要求服务器可达且已确认免密 sudo，返回 `server_restart` 任务；前端必须二次确认并保留任务中心入口。

## Panel Agent

- `cmd/panel-agent` 是部署在目标服务器上的被动 HTTPS agent，使用 Panel 专用 agent CA 做 mTLS 双向认证；Panel 启动时在 `dataRoot/agent/tls` 生成或复用 agent CA 与 Panel client 证书。
- 服务器通过 traits 启用 agent：`agent.enabled=true` 且 `agent.url=https://host:9443`。Panel 启动后会检查已配置 agent 的服务器，检查结果写入 `agent.status`、`agent.last_checked_at`、`agent.version` 和 `agent.last_error` traits。
- Agent 健康检查必须返回 Docker 健康状态和 Docker host；Panel 要求 Docker 正常且 agent 报告的 Docker host 与服务器配置一致。
- Agent 当前覆盖健康检查、`/etc/os-release`、系统 traits、metrics snapshot、UFW status，以及应用 runtime deploy/stop/restart/status/logs。
- 启用 agent 后，读取类能力和应用运行时操作走 agent；软件包刷新/升级、UFW 写操作、服务器重启等写入型服务器维护仍走 SSH。
- `POST /api/v1/servers/{id}/agent/certificate` 签发目标机 `panel-agent` 的 mTLS server 证书包；响应包含 CA、server certificate、server private key、建议监听地址、agent URL 和 Docker host，只用于安装配置，不落库。

## 验证

- 后端服务器、凭据、agent、指标、软件包或 UFW 行为改动运行 `task test:backend`。
- 前端服务器页面、API 或类型改动按影响范围运行 `task test:web` 或 `task build:web`。

## 文档更新触发

新增支持系统、远程命令、服务器字段、凭据字段、agent 能力、指标字段、软件包行为或相关 API 时，必须更新本文档。
