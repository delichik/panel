# 服务器、凭据、指标与软件包

## 适用场景

修改 SSH 凭据、服务器登记、Docker host 配置、agent 部署、连通性测试、系统探测、sudo 检查、UFW、概览指标采集、APT 软件包刷新或升级时，先读本文档。

## 后端入口

- SSH 凭据：`internal/modules/servers/credential/`
- 服务器模块：`internal/modules/servers/`
- 服务器领域模型与仓储端口：`internal/modules/servers/domain/`、`internal/modules/servers/ports/`
- SQLite 服务器仓储：`internal/modules/servers/store/sqlite/`
- 服务器登记、更新、删除和读取用例：`internal/modules/servers/registry.go`
- Agent 部署任务、自动部署限流和安装流程：`internal/modules/servers/agent_deployment.go`
- Agent 健康检查、兼容性判断和证书时间错误恢复：`internal/modules/servers/agent_health.go`
- Agent 系统证书签发、展示与重置：`internal/modules/servers/agent_certificates.go`
- Agent 协议、客户端与 TLS：`internal/agent/contract/`、`internal/agent/client/`、`internal/agent/security/`
- SSH 执行器：`internal/platform/ssh/`
- Linux 发行版适配：`internal/platform/linux/`
- 通用远程运维操作：`internal/platform/linux/remoteops/`
- 指标采集：`internal/modules/observability/metrics/`
- 概览聚合：`internal/modules/observability/overview/`
- 软件包维护：`internal/modules/packages/`
- 周期任务触发：各业务模块 `tasks.go` 注册周期输入，由 `internal/modules/tasks/` 内部 worker 驱动
- 任务记录：`internal/modules/tasks/`
- 路由注册：`internal/modules/servers/routes.go`、`internal/modules/servers/credential/routes.go`

## 前端入口

- 服务器与凭据页面：`web/src/views/servers/_shared/ServersPageContent.vue`
- 服务器选择器：`web/src/components/ServerSelector.vue`
- 软件包页面：`web/src/views/servers/packages/index.vue`
- 防火墙页面：`web/src/views/servers/firewall/index.vue`
- 概览页面：`web/src/views/overview/index.vue`
- API：`web/src/api/servers.ts`、`web/src/api/packages.ts`、`web/src/api/overview.ts`
- 类型：`web/src/types/api.ts`

## 前端布局约定

- 服务器列表和共享服务器选择器在桌面端作为内部滚动选择列表使用；列表行必须按内容高度从顶部排列，不能被剩余高度拉伸。
- 服务器节点页直接进入选择器与详情工作区，不在顶部重复展示服务器数量、可达数量或 Agent 就绪数量摘要。
- 防火墙、软件包和资源页左侧服务器选择栏复用 `ServerSelector.vue`，不要复制一套不同尺寸的选择行。
- 凭据、软件包和防火墙规则等带分页表格在桌面端必须让表格体吸收剩余高度，分页固定在卡片或面板底部。

## API 范围

- 凭据：`GET/POST /api/v1/credentials`，`PUT/DELETE /api/v1/credentials/{id}`
- 服务器：`GET/POST /api/v1/servers`，`POST /api/v1/servers/probe`，`PUT/DELETE /api/v1/servers/{id}`
- Agent 部署：`POST /api/v1/servers/{id}/agent/deploy`，Agent 证书包：`POST /api/v1/servers/{id}/agent/certificate`
- Agent 系统证书：`GET /api/v1/key-assets/system`，重置：`POST /api/v1/key-assets/system/{id}/reset`
- 服务器操作：同步连通性检查 `POST /api/v1/servers/{id}/test`，任务型重启 `POST /api/v1/servers/{id}/restart`，任务型 UFW 安装 `POST /api/v1/servers/{id}/ufw/install`
- UFW：`GET /api/v1/servers/{id}/ufw`，`POST /api/v1/servers/{id}/ufw/enable`，`POST /api/v1/servers/{id}/ufw/rules`，`DELETE /api/v1/servers/{id}/ufw/rules/{number}`
- 指标：`GET /api/v1/servers/{id}/metrics`
- 软件包：`GET /api/v1/servers/{id}/packages/updates`，`POST /api/v1/servers/{id}/packages/refresh`，`POST /api/v1/servers/{id}/packages/upgrade-selected`，`POST /api/v1/servers/{id}/packages/upgrade-all`
- 概览：`GET /api/v1/overview`
- 概览卡片布局：`GET/PUT /api/v1/overview/cards`
- 概览卡片数据：`GET /api/v1/overview/cards/{cardId}/data`

## 数据与行为约定

- `servers` 和 `credentials` 在应用数据库，指标快照在指标数据库。
- 服务器列表、详情、新增和更新持久化通过 `internal/modules/servers/ports` 中的 `ServerRepository`；SQLite 实现在 `store/sqlite`。跨应用目标和概览配置的服务器删除事务暂由服务器用例协调，迁移时必须保持现有原子性。
- `service.go` 保留服务器运维、探测和 UFW 等流程；服务器资源 CRUD 放在 `registry.go`，Agent 部署和健康检查分别放在 `agent_deployment.go` 与 `agent_health.go`，新增代码不要重新揉回主 service 文件。
- 删除服务器是本地控制面操作，不连接目标机，也不得因为服务器失联而失败。删除时必须取消该服务器所有 `queued`、`scheduled`、`failed_retryable` 和 `running` 任务，已取消任务不得被后台 worker 后续覆盖为成功或失败；同时清理指标库中的该服务器指标、应用 `deployment_server_ids_json` 中的服务器 ID、概览卡片 `serverIds` 引用，并依赖应用数据库外键级联删除包缓存、镜像缓存、应用实例和协调状态。
- 服务器创建/编辑必须配置 `dockerHost`，默认值为 `unix:///var/run/docker.sock`。该值会写入 agent systemd 环境文件的 `PANEL_AGENT_DOCKER_HOST`，agent 使用 Docker Engine API 与 Docker 通信，不调用 Docker CLI。
- 新增服务器响应可携带 `initialTaskId` 指向首次 bootstrap 探测任务。该任务只通过 SSH 读取发行版、CPU 架构并检查非交互特权能力；在架构信息成功落库前失败时必须标记任务失败并删除刚创建的服务器记录，让用户回到表单修正 SSH 信息。
- 特权能力统一持久化为 `privilege.mode=root|passwordless_sudo|none`、派生的 `privilege.privileged` 和检查时间。UID 0 使用 `root` 并直接执行特权命令；非 root 且 `sudo -n` 成功时使用 `passwordless_sudo`；其他情况使用 `none`。软件包、UFW、重启和 Agent bootstrap 只按 `privilege.mode` 判断准入。
- 服务器架构信息使用结构化 `architecture.os`、`architecture.arch` 和 `architecture.rawMachine`，数据库列为 `architecture_os`、`architecture_arch`、`architecture_machine`。Agent 部署选包优先读取结构化架构字段；字段缺失时通过 SSH `uname -m` 探测目标节点并写回结构化字段。
- bootstrap 成功后按现有受限自动部署状态机创建 Agent 部署任务。Agent 部署或完整系统信息刷新失败不得删除节点；自动部署连续失败 2 次后标记 `agent.status=undeployable` 和 `agent.auto_deploy_blocked=true`，周期检查停止自动部署。用户手动部署会解除阻止，部署及兼容性检查成功后恢复为 `compatible`。
- 完整系统信息由兼容 Agent 读取并交给 `internal/platform/linux/` 解析支持的 Debian/Ubuntu 版本；Agent 内部直接读取 `/etc/os-release`、`/proc`、`/sys`、网络接口和 `statfs`，不再使用系统信息 bash 脚本。如果已启用 agent，读取必须要求 `agent.status=compatible` 并走 agent，不允许在 agent 未就绪、异常、不可部署或客户端缺失时回落 SSH。
- 服务器列表和详情读取不得创建连通性或系统信息后台任务。`POST /api/v1/servers/{id}/test` 是同步普通函数，只验证 SSH 并更新可达状态与特权模式，不进入任务中心。周期可达状态以 `metrics_collect` 为准：采集成功标记可达，实际采集失败标记不可达。
- `server_info_collect` 首次 bootstrap 在创建服务器后立即运行；后续完整系统信息 refresh 固定每小时一次且只走兼容 Agent。refresh 失败只能使任务失败或可重试，不得回滚或删除已有服务器。
- 系统探测写入 `sys.*` traits；网卡采集要求 `/sys/class/net/{name}/device` 存在，并过滤 Docker、veth、bridge、CNI、隧道和 overlay 等常见虚拟接口。
- SSH 密码、私钥和私钥口令统一封装到 `credentials.secret_ciphertext`，并通过 `internal/platform/secrets` 加密；不得通过 API 响应或任务日志返回秘密内容。
- `internal/platform/linux/remoteops/` 仅用于 Agent bootstrap、安装、证书验证和恢复路径中的 SSH 特权操作。
- 软件包维护基于 APT，只对支持的系统执行；刷新和升级必须通过兼容 Agent 使用固定 `apt-get`/`apt` 可执行文件及参数调用，不拼接 shell，也不回退 SSH。
- `POST /api/v1/servers/{id}/packages/refresh` 创建或复用 `package_refresh` 任务并返回 `taskId`；调度器一轮多服务器刷新必须共享同一个 `operationId`。
- 周期性指标采集依赖 agent，只对 `agent.enabled=true`、存在 `agent.url` 且 `agent.status=compatible` 的服务器创建 `metrics_collect` 任务；不再因旧 `reachable=false` 跳过，以便恢复成功时重新标记可达。同一轮多服务器采集共享一个 `operationId`，任务中心默认常用类型会隐藏该高频类型。指标快照除 CPU、内存、磁盘和网络外，结构化保存 Linux 标准的 1、5、15 分钟负载 `load1`、`load5`、`load15`。
- 指标历史清理由 `internal/modules/observability/metrics/cleanup_worker.go` 自主管理，按运行时设置中的保留天数和清理周期执行，不属于 tasks 内部 worker。
- `POST /api/v1/servers/{id}/ufw/install` 返回 `server_ufw_install` 任务；该任务由内存 goroutine 执行，创建后必须先标记为 `running` 再返回。
- `POST /api/v1/servers/{id}/restart` 要求服务器可达且已确认 root 或免密 sudo 特权能力，返回 `server_restart` 任务；前端必须二次确认并保留任务中心入口。

## Panel Agent

- `cmd/panel-agent` 是部署在目标服务器上的被动 HTTPS agent，使用 Panel 专用 agent CA 做 mTLS 双向认证；Panel 启动时在 `dataRoot/agent/tls` 生成或复用 agent CA 与 Panel client 证书。
- Panel Agent 启动时必须先校验构建生成的 HTTP contract hash 非空；缺少生成文件或生成流程失效时直接返回启动错误。
- Agent 状态机固定为四类：`compatible` 表示正常，只有该状态允许依赖 Agent 的系统探测、UFW 状态、指标、应用运行时和容器化操作；`incompatible` 表示 agent 构建版本与 Panel 不一致或证书时间需要修复，系统定时检查必须自动创建或复用部署任务修复；`unavailable` 表示当前不可用，包括连不上、健康检查失败、Docker 不可用或尚未部署，其中尚未配置 agent URL 会自动部署，普通网络/远端不可达错误只记录状态并跳过依赖 agent 的工作，不得直接触发重装；`undeployable` 表示连续 2 次系统自动部署失败后的无法部署状态，系统定时检查不得继续部署，只保留手动重装入口，手动重装会清除自动部署停止标记。
- Agent CA、Panel Agent 客户端证书和每台服务器已签发的 Agent 服务端证书作为“系统内置”资产展示，底层可作为 `metadata.systemManaged=true` 的系统托管资产保存，但不属于用户域 key asset，不能删除、导入、导出或注册为应用内部文件来源，只允许重置。每台服务器的 Agent 服务端证书是安装/重装任务同步到目标机 `/etc/panel-agent` 的部署产物；只有已记录证书指纹和有效期元数据的服务器证书会进入系统证书列表，重置单台服务器证书会复用该服务器的 Agent 部署任务。
- 重置 Panel Agent 客户端证书时保留 Agent CA，并热加载所有服务共享的 Agent HTTP client；重置 Agent CA 时同时生成新的客户端证书、热加载 HTTP client，并为所有已配置服务器排队重部署 Agent；重置单台服务器证书复用该服务器的 Agent 部署任务。
- Agent 部署成功后把服务端证书指纹和有效期写入服务器 traits，供服务器 Agent 状态、最后错误和部署任务排查使用；健康检查成功时也会从 TLS 握手中的远端服务端证书刷新这些元数据。
- 服务器必须启用 agent，通过 traits 记录：`agent.enabled=true` 且 `agent.url=https://host:9786`。Panel 启动后会扫描服务器，调度器也会周期检查已配置 agent；没有配置 agent URL 的服务器会自动创建 `server_agent_deploy` 任务；已配置 agent 但 URL 不是当前默认地址的服务器会标记为 `incompatible` 并自动重装；已配置当前默认 URL 的服务器会执行健康检查，检查结果写入 `agent.status`、`agent.last_checked_at`、`agent.version` 和 `agent.last_error` traits。`agent.version` 必须与当前 Panel 构建版本完全一致，否则标记 `incompatible` 并自动重装；健康检查返回的 `capabilities`、agent HTTP contract hash 和 Docker host 不作为兼容性门槛。连续系统自动部署失败达到上限后进入 `undeployable`。
- Agent 健康检查必须返回 Docker 健康状态和 Docker host；Panel 要求 Docker 正常且 agent 报告的 Docker host 与服务器配置一致。
- Application 运行时要求 agent 与 Panel 构建版本一致；部署编排在 Panel 侧完成，agent 只执行写托管文件、创建容器、容器动作和状态读取等原子接口。
- Agent 当前覆盖健康检查、`/etc/os-release`、系统 traits、metrics snapshot、UFW status、应用 runtime 文件写入/容器创建/stop/restart/status/logs/持久化目录打包与恢复，以及 Docker 容器、容器日志、镜像、网络和卷资源 API。
- 依赖 agent 的读取和运行时能力必须只在 `agent.status=compatible` 且 `agent.url` 存在时执行；agent 未部署、异常、不兼容、无法部署或客户端不可用时，当前操作或定时任务不得执行，也不得回退 SSH。例外是 agent 部署、重装、证书同步等恢复 agent 本身的任务。
- Docker 资源查询和操作只走 agent Docker Engine API，不回退 SSH。
- 启用 agent 后，读取类能力、软件包刷新/升级、UFW 状态及写操作、服务器重启、指标采集和应用运行时操作必须走 agent，不允许回退 SSH。APT/UFW 由 Agent 参数化调用固定命令；服务器重启由 Agent 通过 `busctl` 调用 logind D-Bus `Reboot`。
- 新增服务器完成首次信息采集且确认 root 或免密 sudo 特权能力后，会按自动部署修复判断创建 `server_agent_deploy` 任务安装 agent；后续 Agent 健康检查只在 agent 未配置、版本不一致、证书需要修复时自动部署，`agent.status=compatible` 且版本正常时不得重装，普通 `unavailable` 网络/远端错误和 Docker 不可用不得触发重装。
- Panel 启动检查或周期检查发现服务器未配置 agent URL、agent URL 不是当前默认地址、`agent.status=incompatible`、agent 版本与 Panel 不一致、证书过期/尚未生效/距过期不足 7 天，或健康检查因 mTLS server 证书时间错误失败时，必须自动创建或复用 `server_agent_deploy` 任务安装/修复 agent；证书进入 7 天续期窗口时保持 `agent.status=compatible` 且不写错误提示，只静默刷新部署；缺少能力、agent HTTP contract hash 不一致和 Docker host 不一致不单独触发重装；单纯 `agent.status=unavailable`、网络超时、连接拒绝、服务器失联或 Docker 不可用不得触发自动重装。安装/重装任务负责把当前 Panel 签发的 CA、服务端证书和私钥同步到目标机 `/etc/panel-agent`，默认监听 `tcp/9786`，重启前必须停止 systemd 服务并清理残留 `panel-agent` 进程，写入后必须校验远端 `server.pem` 确实是新签发证书，启动失败必须输出 `systemctl status` 和 `journalctl -u panel-agent.service` 诊断，启动后必须校验 `tcp/9786` 实际吐出的服务端证书指纹匹配新证书；部署证书包的 `agent.url` 必须使用当前服务器 `host` 生成；如果服务器 `host` 被修改，Agent URL 和证书元数据必须失效并要求重部署。如果已有排队或可重试的 `server_agent_deploy` 任务，手动部署、CA 重置、版本不一致、证书修复和服务器 host 变更触发的重装必须复用并立即启动该任务。同一服务器在最近一次成功部署后，系统自动触发的 agent 部署失败达到 2 次后必须标记 `agent.status=undeployable` 并停止自动尝试；`undeployable` 状态不得由周期检查继续部署。
- `POST /api/v1/servers/{id}/agent/deploy` 是手动兜底入口，返回并启动 `server_agent_deploy` 任务；任务中心重试或立即运行也支持该任务类型。未安装时前端显示安装按钮，已安装但异常时显示重装按钮。服务器详情页只显示一条紧凑的服务器错误提示，优先展示 `agent.last_error`，不再在访问信息区重复渲染第二条 Agent 错误横幅。
- agent 部署任务通过 SSH 上传独立 `panel-agent` 二进制到目标机，再以 `/usr/local/bin/panel-agent` 的 systemd 服务运行；任务会写入 mTLS 证书、`PANEL_AGENT_DOCKER_HOST` 和 `/etc/systemd/system/panel-agent.service`，启动后回写 `agent.enabled=true`、`agent.url` 并立即执行健康检查。
- Panel 固定从 `/app/panel-agents/<goos>-<goarch>/panel-agent` 读取 agent bundle，并根据目标服务器结构化 `architecture.os`/`architecture.arch` 选择 `linux-amd64` 或 `linux-arm64`；结构化架构缺失时先探测目标节点并持久化结果。该位置不可通过配置或环境变量修改；发布镜像会把随 Panel 构建的 agent bundle 复制到 `/app/panel-agents`，部署任务每次直接读取对应文件并上传到目标机。
- `POST /api/v1/servers/{id}/agent/certificate` 签发目标机 `panel-agent` 的 mTLS server 证书包；响应包含 CA、server certificate、server private key、建议监听地址、agent URL 和 Docker host，只作为高级手动安装兜底，不会落库。

## 验证

- 后端服务器、凭据、agent、指标、软件包或 UFW 行为改动运行 `task test:backend`。
- 前端服务器页面、API 或类型改动按影响范围运行 `task test:web` 或 `task build:web`。

## 文档更新触发

新增支持系统、远程命令、服务器字段、凭据字段、agent 能力、指标字段、软件包行为或相关 API 时，必须更新本文档。
