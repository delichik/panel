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
- Agent 部署：`POST /api/v1/servers/{id}/agent/deploy`，Agent 证书包：`POST /api/v1/servers/{id}/agent/certificate`
- Agent 系统证书：`GET /api/v1/key-assets/system`，重置：`POST /api/v1/key-assets/system/{id}/reset`
- 服务器操作：`POST /api/v1/servers/{id}/test`，`POST /api/v1/servers/{id}/restart`，`POST /api/v1/servers/{id}/ufw/install`
- UFW：`GET /api/v1/servers/{id}/ufw`，`POST /api/v1/servers/{id}/ufw/enable`，`POST /api/v1/servers/{id}/ufw/rules`，`DELETE /api/v1/servers/{id}/ufw/rules/{number}`
- 指标：`GET /api/v1/servers/{id}/metrics`
- 软件包：`GET /api/v1/servers/{id}/packages/updates`，`POST /api/v1/servers/{id}/packages/refresh`，`POST /api/v1/servers/{id}/packages/upgrade-selected`，`POST /api/v1/servers/{id}/packages/upgrade-all`
- 概览：`GET /api/v1/overview`
- 概览卡片布局：`GET/PUT /api/v1/overview/cards`
- 概览卡片数据：`GET /api/v1/overview/cards/{cardId}/data`

## 数据与行为约定

- `servers` 和 `credentials` 在应用数据库，指标快照在指标数据库。
- 服务器创建/编辑必须配置 `dockerHost`，默认值为 `unix:///var/run/docker.sock`。该值会写入 agent systemd 环境文件的 `PANEL_AGENT_DOCKER_HOST`，agent 使用 Docker Engine API 与 Docker 通信，不调用 Docker CLI。
- 新增服务器响应可携带 `initialTaskId` 指向首次信息采集任务；首次采集失败时必须标记任务失败并删除刚创建的服务器记录，让用户回到表单修正 SSH 信息。
- 系统探测通过 SSH 或已启用的 agent 读取远端信息，并交给 `internal/linux/` 解析支持的 Debian/Ubuntu 版本；如果已启用 agent，读取必须要求 `agent.status=compatible` 并走 agent，不允许在 agent 未就绪、不可用、失败或客户端缺失时回落 SSH。agent mTLS server 证书过期或尚未生效时，Panel 会标记 agent 不兼容、自动排队重部署，并返回当前 agent 错误。
- 系统探测写入 `sys.*` traits；网卡采集要求 `/sys/class/net/{name}/device` 存在，并过滤 Docker、veth、bridge、CNI、隧道和 overlay 等常见虚拟接口。
- SSH 密码、私钥和私钥口令统一封装到 `credentials.secret_ciphertext`，并通过 `internal/secretstore` 加密；不得通过 API 响应或任务日志返回秘密内容。
- 安装软件、日志化 sudo 命令、sudo 写文件和 UFW allow/delete/status 优先复用 `internal/remoteops/`。
- 软件包维护基于 APT，只对支持的系统执行；刷新和升级依赖远程 sudo。
- `POST /api/v1/servers/{id}/packages/refresh` 创建或复用 `package_refresh` 任务并返回 `taskId`；调度器一轮多服务器刷新必须共享同一个 `operationId`。
- 周期性指标采集依赖 agent，只对 `agent.enabled=true`、存在 `agent.url` 且 `agent.status=compatible` 的服务器创建 `metrics_collect` 任务；同一轮多服务器采集共享一个 `operationId`，任务中心默认常用类型会隐藏该高频类型。agent 未部署、不可用或不兼容时直接跳过，不创建任务，也不回退 SSH 采集。
- `POST /api/v1/servers/{id}/ufw/install` 返回 `server_ufw_install` 任务；该任务由内存 goroutine 执行，创建后必须先标记为 `running` 再返回。
- `POST /api/v1/servers/{id}/restart` 要求服务器可达且已确认免密 sudo，返回 `server_restart` 任务；前端必须二次确认并保留任务中心入口。

## Panel Agent

- `cmd/panel-agent` 是部署在目标服务器上的被动 HTTPS agent，使用 Panel 专用 agent CA 做 mTLS 双向认证；Panel 启动时在 `dataRoot/agent/tls` 生成或复用 agent CA 与 Panel client 证书。
- Agent CA 和 Panel Agent 客户端证书作为“系统内置”资产展示，不写入用户 `key_assets`，不能删除、导入、导出或作为应用文件引用，只允许重置。每台服务器的 Agent 服务端证书是安装/重装任务同步到目标机 `/etc/panel-agent` 的部署产物，不在系统证书页逐台展示；其状态通过服务器 Agent 状态、最后错误和部署任务日志排查。
- 重置 Panel Agent 客户端证书时保留 Agent CA，并热加载所有服务共享的 Agent HTTP client；重置 Agent CA 时同时生成新的客户端证书、热加载 HTTP client，并为所有已配置服务器排队重部署 Agent；重置单台服务器证书复用该服务器的 Agent 部署任务。
- Agent 部署成功后把服务端证书指纹和有效期写入服务器 traits，供服务器 Agent 状态、最后错误和部署任务排查使用；健康检查成功时也会从 TLS 握手中的远端服务端证书刷新这些元数据，旧服务器缺少该元数据时不必等到下一次重部署才补齐。
- 服务器必须启用 agent，通过 traits 记录：`agent.enabled=true` 且 `agent.url=https://host:9443`。Panel 启动后会扫描服务器，调度器也会周期检查已配置 agent；没有配置 agent 的服务器会自动创建 `server_agent_deploy` 任务；已配置 agent 的服务器会执行健康检查，检查结果写入 `agent.status`、`agent.last_checked_at`、`agent.version` 和 `agent.last_error` traits；agent 版本必须与当前 Panel 版本一致，否则视为不兼容并触发自动重装。
- Agent 健康检查必须返回 Docker 健康状态和 Docker host；Panel 要求 Docker 正常且 agent 报告的 Docker host 与服务器配置一致。
- Agent 当前覆盖健康检查、`/etc/os-release`、系统 traits、metrics snapshot、UFW status、应用 runtime deploy/stop/restart/status/logs，以及 Docker 容器、镜像、网络和卷资源 API。
- 依赖 agent 的读取和运行时能力必须只在 `agent.status=compatible` 且 `agent.url` 存在时执行；agent 未部署、不可用、不兼容或客户端不可用时，当前操作或定时任务不得执行，也不得回退 SSH。例外是 agent 部署、重装、证书同步等恢复 agent 本身的任务。
- Docker 资源查询和操作只走 agent Docker Engine API，不回退 SSH。
- 启用 agent 后，读取类能力、UFW 状态、指标采集和应用运行时操作必须走 agent；软件包刷新/升级、UFW 写操作、服务器重启等写入型服务器维护仍走 SSH。UFW allow/delete 等 SSH 写操作完成后的状态确认仍使用 SSH，不依赖 agent 状态读取入口。
- 新增服务器完成首次信息采集且确认免密 sudo 后，会自动创建 `server_agent_deploy` 任务安装或更新 agent。
- Panel 启动检查或周期检查发现服务器未配置 agent 时，会自动创建 `server_agent_deploy` 任务安装 agent；发现已配置 agent 的服务器处于 `agent.status=incompatible` 时，会自动创建 `server_agent_deploy` 任务重装 agent；已记录的 Agent 服务端证书有效期过期、尚未生效或距过期不足 30 天时，不再继续请求旧 agent，而是直接标记不兼容并排队重装；健康检查、系统探测、应用运行时、容器化资源和指标采集等 Agent API 因 mTLS server 证书过期或尚未生效失败时，也视为不兼容并自动重装。安装/重装任务负责把当前 Panel 签发的 CA、服务端证书和私钥同步到目标机 `/etc/panel-agent`，重启前必须停止 systemd 服务并清理残留 `panel-agent` 进程，写入后必须校验远端 `server.pem` 确实是新签发证书，启动后必须校验 tcp/9443 由 `panel-agent` 监听且实际吐出的服务端证书指纹匹配新证书；部署证书包的 `agent.url` 必须使用当前服务器 `host` 生成，不能复用历史 traits 中可能已经失效的旧地址；如果服务器 `host` 被修改，旧 Agent URL 和旧证书元数据必须失效并要求重部署。如果已有排队或可重试的 `server_agent_deploy` 任务，手动部署和证书错误触发的自动重装必须复用并立即启动该任务，不能让旧任务挡住证书同步。同一服务器在最近一次成功部署后，系统自动触发的 agent 部署失败达到 2 次后会停止自动尝试，只保留 Agent 错误状态和手动重装入口；服务器已经处于 `agent.status=unavailable` 时，启动检查和周期检查只保留错误状态，不再自动重试。
- `POST /api/v1/servers/{id}/agent/deploy` 是手动兜底入口，返回并启动 `server_agent_deploy` 任务；任务中心重试或立即运行也支持该任务类型。未安装时前端显示安装按钮，已安装但异常时显示重装按钮。
- agent 部署任务通过 SSH 上传独立 `panel-agent` 二进制到目标机，再以 `/usr/local/bin/panel-agent` 的 systemd 服务运行；任务会写入 mTLS 证书、`PANEL_AGENT_DOCKER_HOST` 和 `/etc/systemd/system/panel-agent.service`，启动后回写 `agent.enabled=true`、`agent.url` 并立即执行健康检查。
- Panel 固定从 `/app/panel-agents/<goos>-<goarch>/panel-agent` 读取 agent bundle，并根据目标服务器 `sys.architecture` 选择 `linux-amd64` 或 `linux-arm64`；缺失架构信息时通过 SSH `uname -m` 探测。该位置不可通过配置或环境变量修改；发布镜像会把同版本 agent bundle 复制到 `/app/panel-agents`，部署任务每次直接读取对应文件并上传到目标机。
- `POST /api/v1/servers/{id}/agent/certificate` 签发目标机 `panel-agent` 的 mTLS server 证书包；响应包含 CA、server certificate、server private key、建议监听地址、agent URL 和 Docker host，只作为高级手动安装兜底，不会落库。

## 验证

- 后端服务器、凭据、agent、指标、软件包或 UFW 行为改动运行 `task test:backend`。
- 前端服务器页面、API 或类型改动按影响范围运行 `task test:web` 或 `task build:web`。

## 文档更新触发

新增支持系统、远程命令、服务器字段、凭据字段、agent 能力、指标字段、软件包行为或相关 API 时，必须更新本文档。
