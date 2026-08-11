# 服务器、凭据、指标与软件包

## List API Contract

- `GET /api/v1/servers` returns `ListPage<ServerSummary>` and accepts only `page`, `pageSize`, and `q`.
- List rows include only the `credentialId` reference (never credential secret content) and exclude notes, variables, full traits, operating-system detail, and metrics; the only trait signals returned are `agent.enabled`, `agent.url`, `agent.status`, and UFW flags. `GET /api/v1/servers/{id}` owns the complete view.
- `GET /api/v1/credentials` follows the same `ListPage` and strict `page`/`pageSize`/`q` contract and never selects encrypted secret columns.

## 适用场景

修改 SSH 凭据、服务器登记、Docker host 配置、agent 部署、连通性测试、系统探测、sudo 检查、UFW、fail2ban、概览指标采集、APT 软件包刷新或升级时，先读本文档。

## 后端入口

- SSH 凭据：`internal/modules/servers/credential/`
- 服务器模块：`internal/modules/servers/`
- 服务器领域模型与仓储端口：`internal/modules/servers/domain/`、`internal/modules/servers/ports/`
- SQLite 服务器仓储：`internal/modules/servers/store/sqlite/`
- 服务器登记、更新、删除和读取用例：`internal/modules/servers/registry.go`
- fail2ban 配置：`internal/modules/servers/fail2ban.go`
- Agent 部署任务、自动部署限流和安装流程：`internal/modules/servers/agent_deployment.go`
- Agent 健康检查、兼容性判断和证书时间错误恢复：`internal/modules/servers/agent_health.go`
- Agent 系统证书签发、展示与重置：`internal/modules/servers/agent_certificates.go`
- Agent gRPC 协议、客户端与 TLS：`internal/agent/contract/`、`internal/agent/client/`、`internal/agent/rpc/`、`internal/agent/security/`
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

- 服务器页面位于 `web/src/views/servers/index.vue`，SSH 凭据独立页面位于 `web/src/views/credentials/index.vue`；分别通过 `/servers` 与 `/credentials` 进入，不共享页面内 tab。
- 节点 tab（MasterDetailLayout：搜索 + 列表 + 分页 / 四区详情）：`web/src/views/servers/ServersNodesView.vue`、`ServerDetail.vue`。
- 服务器添加/编辑由 `web/src/views/servers/index.vue` 内的表单对话框承载（含 `POST /servers/probe`）；凭据添加/编辑由 `web/src/views/credentials/index.vue` 内的表单对话框承载。凭据按 password/private_key 类型裁剪字段，编辑时 secret 留空 = 不更新；私钥类型使用 large Dialog 和共享 plain CodeMirror，password 类型仍使用普通输入与默认 Dialog。
- 凭据页面：`web/src/views/credentials/index.vue`（MasterDetailLayout：搜索 + 列表 + 分页 / 详情）；列表引用计数、详情引用列表与删除前引用预检都基于已加载服务器摘要中的 `credentialId` 客户端匹配，后端 409 `credential_in_use` 兜底；详情展示密钥摘要（私钥的算法、长度、SHA256 指纹与密钥注释/名称）和引用服务器列表，不再展示“密钥策略”卡片。
- 纯函数逻辑：`serverTraits.ts`（traits/网卡解析、Agent/UFW 状态判定）、`serverProbe.ts`（probe 结果映射）、`serverInitPolling.ts`（创建后初始化轮询状态机）、`credentialReferences`（引用预检，位于 `web/src/views/servers/model.ts`）、`useTaskMessage.ts`（任务反馈 + "查看任务"跳转）。
- 防火墙与 Fail2Ban 页面（v4 阶段 4A）：`web/src/views/security/index.vue`；`/resources/firewall` 与 dev-only `/resources/fail2ban` 共享服务器选择器和 URL `server` query，但作为资源菜单下的独立页面呈现，不使用页内 tabs。旧 `/security/*` 只保留重定向。UFW 右侧为规则/状态矩阵、添加规则、删除规则、启用/安装确认；Fail2Ban 右侧为托管状态、检测到的 jail、预设、可视草稿、使用共享 CodeMirror 的 YAML 高级模式、保存草稿、应用/安装/释放接管确认。YAML 编辑仅更新草稿，仍需显式保存或启用，不自动格式化；纯函数和 `parseSimpleJailsFromYaml` 边界保持在 `web/src/views/security/model.ts`。
- 软件包页（v4 阶段 4A）：软件包维护位于独立路由 `/resources/packages`，由 `web/src/views/resources/index.vue` 按当前资源页面渲染；支持服务器选择、客户端搜索、刷新 metadata、升级已选/全部和 root/免密 sudo 准入阻断。纯函数在 `web/src/views/resources/model.ts`。
- 概览页面（v4）：`web/src/views/overview/index.vue`；概览卡片布局使用 6 列整数网格，卡片配置中的 `width`/`height` 分别对应 1x1 方格跨度，宽度上限 6、高度上限 4；浏览态只展示指标内容，编辑态支持带实时顺序预览的拖动重排、拖拽缩放、添加/删除和单卡属性编辑。卡片可通过 `serverIds` 选择一台或多台服务器，空数组表示全部服务器；卡片元信息显示服务器数量，多服务器指标通过 ECharts 独立 series 和悬停 tooltip 标识服务器与数值，tooltip 挂到 body 并使用 Panel popover token，不能受卡片 `overflow` 裁切；`networkDirection` 只在网络卡片编辑时展示。保存时继续通过 `PUT /api/v1/overview/cards` 持久化有序卡片配置；容器、镜像、网络、卷等资源页面由 `web/src/views/resources/index.vue` 承载独立路由页面。
- 概览折线图自动刷新：页头刷新按钮旁提供共享自动刷新控件 `AutoRefreshControl`（见前端规范「自动刷新模式」，设置全局共享、默认 5 秒），开启后只对 CPU/内存/磁盘/网络折线卡片生效。轮询防重入、标签页不可见暂停；进入编辑态时停止自动刷新，保存成功后恢复并整卡重载，卡片加载中跳过本轮；每次请求 `GET /api/v1/overview/cards/{cardId}/data?since=<RFC3339Nano>` 只拉取上次数据点之后的新指标点，前端按服务器追加到现有序列，并按卡片时间范围滑动清理等量旧点（例如 1d 卡片始终只保留最近一天），不重新加载整段范围。`since` 缺省时保持原全量行为；非法时间戳返回 422；后端按秒对齐 `since` 后再比较，避免 SQLite 文本时间精度问题。 自动刷新失败时静默保留旧数据并下个周期重试，不弹 toast。
- API：`web/src/api/servers.ts`、`web/src/api/security.ts`、`web/src/api/packages.ts`、`web/src/api/containers.ts`、`web/src/api/overview.ts`
- 类型：`web/src/types/api.ts`

## 前端布局约定

- 节点 tab 使用 MasterDetailLayout 模板：左列固定 280px，搜索为客户端过滤（name/host）并同步进 URL query `q`，初始化中的服务器显示"初始化中"进度态；右列详情分头部（可达性 + 操作组 + 错误横幅，`agent.last_error` 优先）与状态 / 系统 / 运行时 / 访问四区。
- 凭据页使用 MasterDetailLayout 模板：左列搜索为客户端过滤（name/username），分页固定底部并同步进 URL query `page`；右列详情展示密钥摘要与服务器引用，选择凭据时按需加载 `GET /api/v1/credentials/{id}`。
- 创建服务器保存成功即关对话框：新记录插入列表顶部，前端轮询 `initialTaskId`（1.5s × 90 上限）到终态；成功刷新数据，失败显示原因与"服务器记录已回滚"提示。
- 任务类操作（Agent 部署 / 重启 / UFW 安装）反馈只承诺任务已提交；旧 `/tasks` 兼容路由不再作为产品入口，后续诊断应优先通过系统事件或后端提供的任务引用查看。

## API 范围

- 凭据：`GET/POST /api/v1/credentials`，`GET/PUT/DELETE /api/v1/credentials/{id}`
- 服务器：`GET/POST /api/v1/servers`，`POST /api/v1/servers/probe`，`PUT/DELETE /api/v1/servers/{id}`
- Agent 部署：`POST /api/v1/servers/{id}/agent/deploy`，Agent 证书包：`POST /api/v1/servers/{id}/agent/certificate`
- Agent 系统证书：`GET /api/v1/key-assets/system`，重置：`POST /api/v1/key-assets/system/{id}/reset`
- 服务器操作：同步连通性检查 `POST /api/v1/servers/{id}/test`，信任新主机密钥 `POST /api/v1/servers/{id}/trust-host-key`，任务型重启 `POST /api/v1/servers/{id}/restart`，任务型 UFW 安装 `POST /api/v1/servers/{id}/ufw/install`
- UFW：`GET /api/v1/servers/{id}/ufw`，`POST /api/v1/servers/{id}/ufw/enable`，`POST /api/v1/servers/{id}/ufw/rules`，`DELETE /api/v1/servers/{id}/ufw/rules/{number}`
- fail2ban：`GET /api/v1/servers/{id}/fail2ban`，`PUT /api/v1/servers/{id}/fail2ban`，`POST /api/v1/servers/{id}/fail2ban/enable`，`POST /api/v1/servers/{id}/fail2ban/release`，`POST /api/v1/servers/{id}/fail2ban/install`
- 指标：`GET /api/v1/servers/{id}/metrics?range=1h|6h|1d|7d`（默认 `1h`）。服务器详情指标区默认用折线图展示最近 1 小时数据（CPU、内存、磁盘、网络 RX/TX 四个图表），支持范围选择，页头提供共享自动刷新控件 `AutoRefreshControl`；刷新/切范围时保留旧数据避免闪跳。折线图 tooltip 单位按图表类型区分：CPU/内存/磁盘为百分比，网络为 B/s 自适应速率。
- 软件包：`GET /api/v1/servers/{id}/packages/updates`，`POST /api/v1/servers/{id}/packages/refresh`，`POST /api/v1/servers/{id}/packages/upgrade-selected`，`POST /api/v1/servers/{id}/packages/upgrade-all`
- 概览：`GET /api/v1/overview`
- 概览卡片布局：`GET/PUT /api/v1/overview/cards`
- 概览卡片数据：`GET /api/v1/overview/cards/{cardId}/data`
- 概览 V2 目标契约：`GET /api/v1/overview/dashboard` 聚合布局、服务器选项与逐卡结果；`PUT /api/v1/overview/cards` 目标请求包含 `baseVersion`。当前只完成前端类型与隔离 fixture，后端适配在后续契约集成阶段实施。

## 数据与行为约定

- v3 页面经 `web/src/api/servers.ts` typed client 接真实后端；保存与 probe 语义分离（`POST /servers/probe` 只在添加/编辑对话框内预检，不落库）。
- 详情错误横幅 `agent.last_error` 优先于 `lastError`；traits 对用户只读，编辑表单不展示也不提交 traits，后端保存接口忽略用户传入的 traits，服务器特征完全由系统探测、Agent 和设施/应用协调维护。旧数据中 `sudo_passwordless=1` 但 `privilege_mode=none` 的记录由一次性迁移修正为 `passwordless_sudo`，读取层兜底识别 `sudo.passwordless`；服务器摘要读取同样把 `none`+`sudo_passwordless=1` 归一化为 `passwordless_sudo`，保证摘要 `privileged` 信号与详情一致。
- 凭据 secret 只提交非空值，编辑时留空代表保留既有 secret；删除前用已加载服务器列表做引用预检并列出引用服务器，后端 409 `credential_in_use` 兜底。
- `GET /api/v1/credentials/{id}` 对私钥类型凭据按需解密并返回非敏感密钥摘要（算法、位长、SHA256 指纹、密钥注释/名称），密码类型不返回摘要；摘要解析失败时仅省略摘要字段，任何响应都不返回秘密内容。
- SSH 执行器 `internal/platform/ssh` 默认开启主机密钥 TOFU：首次连接把目标机公钥按 `host:port` 身份记录到 known_hosts 文件并持久化，后续连接校验公钥；指纹不匹配拒绝连接并返回 `ssh_host_key_mismatch` 错误（BadGateway）。known_hosts 默认位于 `<dataRoot>/known_hosts`（`PANEL_DATA_ROOT` 未设置时回退 `data`），可用 `WithKnownHosts` 指定路径或 `WithoutKnownHosts` 显式关闭；涉及 SSH 的测试应提供临时目录或显式关闭 TOFU。
- 主机密钥不一致时，服务器读取层（`Server`/`ServerSummary`）根据 `lastError` 是否包含稳定特征 `ssh host key mismatch` 暴露 `hostKeyMismatch`（JSON `hostKeyMismatch`，`omitempty`），List 与 Get 一致返回。`POST /api/v1/servers/{id}/trust-host-key` 是管理员显式信任流程：Service 先取服务器并按 `TestConnectivity` 相同方式构造 Target，调用执行器 `TrustHostKey` 建立一次性 SSH 连接（HostKeyCallback 只捕获公钥并接受连接，不校验 known_hosts），认证成功后以 `net.JoinHostPort(host, port)` 身份调用 `knownHosts.Replace` 覆盖记录；随后复用 `TestConnectivity` 刷新可达性并清空旧 `lastError`，成功返回更新后的服务器 JSON。known_hosts 未启用时返回 Validation `host_key_verification_disabled`；连接失败 `ssh_connection_failed`、认证失败 `ssh_auth_failed`、写入失败 `ssh_host_key_verification_failed`。前端在详情错误/状态区对 `hostKeyMismatch === true` 展示显著安全提示（说明可能是主机重装或中间人攻击），并提供“信任新主机密钥”按钮：`ConfirmDialog`（danger + requireCheckbox）确认后调用 `trustHostKey`，成功后 toast 并失效详情缓存重拉（`invalidateServerDetail` + `load`），失败显示错误。
- 创建初始化、Agent 部署、重启、UFW 安装为任务型操作：前端只展示已提交/进行中，不承诺请求返回时已完成；新诊断入口应使用系统事件或后端返回的稳定任务引用，不能新增 `/tasks?task=<id>` 产品链接。
- 本阶段验证仅限 `task test:web:unit` 与 `task build:web`。

- `servers` 和 `credentials` 在应用数据库，指标快照在指标数据库。
- `GET /api/v1/servers` 只返回服务器摘要：身份、地址、可达状态以及列表展示所需的 Agent/UFW/权限信号；不得读取完整 traits、variables、notes、凭据 secret 内容、Docker 配置、完整 OS/架构或逐服务器指标；仅返回 `credentialId` 引用 ID 供凭据页解析引用。`GET /api/v1/servers/{id}` 按需返回完整详情，前端选择或编辑服务器时使用详情接口。服务器列表、详情、新增和更新持久化通过 `internal/modules/servers/ports` 中的 `ServerRepository`；SQLite 实现在 `store/sqlite`。跨应用目标和概览配置的服务器删除事务暂由服务器用例协调，迁移时必须保持现有原子性。
- `service.go` 保留服务器运维、探测和 UFW 等流程；服务器资源 CRUD 放在 `registry.go`，Agent 部署和健康检查分别放在 `agent_deployment.go` 与 `agent_health.go`，fail2ban 配置放在 `fail2ban.go`，新增代码不要重新揉回主 service 文件。
- 删除服务器是本地控制面操作，不连接目标机，也不得因为服务器失联而失败。删除时必须取消该服务器所有 `queued`、`scheduled`、`failed_retryable` 和 `running` 任务，已取消任务不得被后台 worker 后续覆盖为成功或失败；同时清理指标库中的该服务器指标、应用 `deployment_server_ids_json` 中的服务器 ID、概览卡片 `serverIds` 引用，并依赖应用数据库外键级联删除包缓存、镜像缓存、应用实例和协调状态。修剪应用部署节点属于应用配置变化，必须在同一删除事务中递增对应应用的 `version` 和配置 `updated_at`。
- 服务器创建/编辑必须配置 `dockerHost`，默认值为 `unix:///var/run/docker.sock`。该值会写入 agent systemd 环境文件的 `PANEL_AGENT_DOCKER_HOST`，agent 使用 Docker Engine API 与 Docker 通信，不调用 Docker CLI。
- 服务器连接地址由 `ipv4`/`ipv6` 派生，不再允许直接提交 `host`：创建/编辑时两个字段至少填写一个且必须是合法 IP 字面量，`ipv4` 优先作为连接地址。读取服务器时若 `ipv4`/`ipv6` 均为空且 `host` 是 IP 字面量，会自动从 `host` 派生（`SplitAddress`），因此旧服务器编辑表单可回显地址，入口代理 DNS 同步也会使用派生后的地址；仅当 `host` 不是 IP 字面量时才保持跳过并标记提示。
- 服务器保存 `ipv4`/`ipv6` 或删除服务器时，会通过装配层桥接触发引用该服务器的入口代理域名 DNS 同步；同步为异步任务，失败不阻断服务器保存。编辑服务器先落库，保存后的连通性探测失败只把服务器标记为不可达并记录日志，不返回错误、不阻断保存与 DNS 同步。
- fail2ban 配置按服务器保存到应用数据库 `fail2ban_configs`，其中 `managed` 表示 Panel 是否接管目标机 fail2ban。前端默认展示“防护规则”列表，结构化 YAML 是高级编辑模式；YAML 是 Panel 自己的 `jails` 结构，不是直接写入目标机的原始 fail2ban 配置。`PUT /fail2ban` 只保存 Panel 草稿，不写目标机；`POST /fail2ban/enable` 在未安装时安装 fail2ban 并自动接管，在已安装未接管时必须由前端传入确认后才接管；接管后通过兼容 Agent 渲染为 `/etc/fail2ban/jail.d/panel.local`，目标机用 `fail2ban-client -t` 校验通过后才重启或 reload 服务，成功后 Panel 才把 `managed` 置为 true。`POST /fail2ban/release` 删除 Panel 生成配置并把 `managed` 置为 false，不导入或恢复用户原始 fail2ban 文件。apply 与 release 分别使用 `server_fail2ban_apply`、`server_fail2ban_release` 两个任务类型，避免共用类型时互相去重。
- 新增服务器响应可携带 `initialTaskId` 指向首次 bootstrap 探测任务。该任务只通过 SSH 读取发行版、CPU 架构并检查非交互特权能力；在架构信息成功落库前失败时必须标记任务失败并删除刚创建的服务器记录，让用户回到表单修正 SSH 信息。
- 特权能力统一持久化为 `privilege.mode=root|passwordless_sudo|none`、派生的 `privilege.privileged` 和检查时间。UID 0 使用 `root` 并直接执行特权命令；非 root 且 `sudo -n` 成功时使用 `passwordless_sudo`；其他情况使用 `none`。软件包、UFW、重启和 Agent bootstrap 只按 `privilege.mode` 判断准入。
- 服务器架构信息使用结构化 `architecture.os`、`architecture.arch` 和 `architecture.rawMachine`，数据库列为 `architecture_os`、`architecture_arch`、`architecture_machine`。Agent 部署选包优先读取结构化架构字段；字段缺失时通过 SSH `uname -m` 探测目标节点并写回结构化字段。
- bootstrap 成功后按现有受限自动部署状态机创建 Agent 部署任务。Agent 部署或完整系统信息刷新失败不得删除节点；系统自动部署失败按 `agent.auto_deploy_failures` 和 `agent.auto_deploy_last_failure_at` 做指数退避，连续失败 2 次后标记 `agent.status=undeployable` 和 `agent.auto_deploy_blocked=true`，周期检查停止自动部署。用户手动部署会解除阻止并重置自动退避时间基准，部署及兼容性检查成功后恢复为 `compatible`；自动部署失败计数必须等 Agent 健康检查连续 5 次正常后才清空。
- `docker exec -it panel /app/panel setup` 通过 Panel 本地控制 socket 复用凭据创建、服务器首次 bootstrap 和 Agent 部署流程。Agent 兼容后节点才会从 `panel_installation.pending_server_id` 提升为唯一 `host_server_id`；Agent 或入口部署失败时保存阶段和错误供重复 setup 恢复。
- 普通服务器删除流程必须拒绝删除 `panel_installation.host_server_id` 指向的 Panel 宿主节点。替换或解除宿主关系需要独立迁移流程，不能由普通 Delete 隐式完成。
- 完整系统信息由兼容 Agent 读取并交给 `internal/platform/linux/` 解析支持的 Debian/Ubuntu 版本；Agent 内部直接读取 `/etc/os-release`、`/proc`、`/sys`、网络接口和 `statfs`，不再使用系统信息 bash 脚本。如果已启用 agent，读取必须要求 `agent.status=compatible` 并走 agent，不允许在 agent 未就绪、异常、不可部署或客户端缺失时回落 SSH。
- 服务器列表和详情读取不得创建连通性或系统信息后台任务。`POST /api/v1/servers/{id}/test` 是同步普通函数，只验证 SSH 并更新可达状态与特权模式，不进入任务中心。周期可达状态以 `metrics_collect` 为准：采集成功标记可达，实际采集失败标记不可达。
- `server_info_collect` 首次 bootstrap 在创建服务器后立即运行；后续完整系统信息 refresh 固定每小时一次且只走兼容 Agent。refresh 失败只能使任务失败或可重试，不得回滚或删除已有服务器。任务中心、周期 worker 和 retry/run-now 通过注册 executor 执行时必须同步跑到任务终态，不能只启动后台 goroutine 后返回成功；创建服务器等需要快速返回任务 ID 的入口可使用模块内后台启动 helper。
- 系统探测写入 `sys.*` traits；网卡采集要求 `/sys/class/net/{name}/device` 存在，并过滤 Docker、veth、bridge、CNI、隧道和 overlay 等常见虚拟接口。
- Agent 写入 `sys.cpu_model` 时优先使用 `/proc/cpuinfo` 的 `model name` 或 `hardware`；`processor` 仅在不是纯数字 CPU 编号时作为兜底，避免把 `processor: 0` 展示成 CPU 型号。
- SSH 密码、私钥和私钥口令统一封装到 `credentials.secret_ciphertext`，并通过 `internal/platform/secrets` 加密；不得通过 API 响应或任务日志返回秘密内容。
- `internal/platform/linux/remoteops/` 仅用于 Agent bootstrap、安装、证书验证和恢复路径中的 SSH 特权操作。
- 软件包维护基于 APT，只对支持的系统执行；刷新和升级必须通过兼容 Agent 使用固定 `apt-get`/`apt` 可执行文件及参数调用，不拼接 shell，也不回退 SSH。
- 软件包升级在 agent 端必须与 RPC 请求 context 隔离并保留独立超时，避免 Panel 重启或连接断开中断 apt 事务后留下 dpkg/Docker 半升级状态；对应 Panel 升级任务必须声明不可取消，删除服务器也不得取消正在运行的升级。
- `POST /api/v1/servers/{id}/packages/refresh` 创建或复用 `package_refresh` 任务并返回 `taskId`；调度器一轮多服务器刷新必须共享同一个 `operationId`。
- 软件包刷新与升级共用 per-server 维护互斥（`packages.Service` 的 refreshing 机制即“维护中”）：同一服务器同一时间只允许一种软件包维护动作，进行中被拒绝的刷新/升级任务会以明确的“维护中”错误进入失败态，不再被误标成功。
- 升级任务（`package_upgrade_selected` / `package_upgrade_all`）已注册 `Execute` 并声明 `AllowRunNow` / `AllowRetry`，选中的软件包名单持久化在 `tasks.params_json`；Panel 重启后遗留的 `queued` 升级任务可由 tasks worker 恢复执行，不再是死任务。
- 周期性指标采集依赖 agent，只对 `agent.enabled=true`、存在 `agent.url` 且 `agent.status=compatible` 的服务器创建 `metrics_collect` 任务；不再因旧 `reachable=false` 跳过，以便恢复成功时重新标记可达。同一轮多服务器采集共享一个 `operationId`，任务中心默认常用类型会隐藏该高频类型。指标快照除 CPU、内存、磁盘和网络外，结构化保存 Linux 标准的 1、5、15 分钟负载 `load1`、`load5`、`load15`。
- 指标历史清理由 `internal/modules/observability/metrics/cleanup_worker.go` 自主管理，按运行时设置中的保留天数和清理周期执行，不属于 tasks 内部 worker。
- `POST /api/v1/servers/{id}/ufw/install` 返回 `server_ufw_install` 任务；该任务由内存 goroutine 执行，创建后必须先标记为 `running` 再返回。
- `POST /api/v1/servers/{id}/restart` 要求服务器可达且已确认 root 或免密 sudo 特权能力，返回 `server_restart` 任务；前端必须二次确认并保留任务中心入口。

## Panel Agent

- `cmd/panel-agent` 是部署在目标服务器上的被动 gRPC agent，使用 Panel 专用 agent CA 做 mTLS 双向认证；Panel 启动时生成或复用 agent CA 与 Panel client 证书。agent 服务端在 CA 校验之外通过 `VerifyPeerCertificate` 拒绝携带 `ServerAuth` EKU 的客户端证书，节点证书（ServerAuth+ClientAuth）不能作为客户端横向调用其他 agent；Panel client 证书只含 `ClientAuth`。
- Agent gRPC service 契约定义在 `internal/agent/proto/agent.proto`，生成代码位于 `internal/agent/pb`；`internal/agent/rpc` 只负责 protobuf message 与现有业务类型之间的转换和服务实现。不要恢复 HTTP fallback，也不要在业务模块中直接拼远端路径。
- Panel Agent 启动时必须先校验构建生成的 gRPC contract hash 非空；该 hash 基于生成代码暴露的 protobuf descriptor，缺少生成文件或生成流程失效时直接返回启动错误。
- Agent 状态机固定为四类：`compatible` 表示正常，只有该状态允许依赖 Agent 的系统探测、UFW 状态、fail2ban 状态与配置应用、指标、应用运行时和容器化操作；`incompatible` 表示 agent 构建版本与 Panel 不一致或证书时间需要修复，系统定时检查必须自动创建或复用部署任务修复；`unavailable` 表示当前不可用，包括连不上、健康检查失败、Docker 不可用或尚未部署，其中尚未配置 agent URL 会自动部署，普通网络/远端不可达错误只记录状态并跳过依赖 agent 的工作，不得直接触发重装；`undeployable` 表示连续 2 次系统自动部署失败后的无法部署状态，系统定时检查不得继续部署，只保留手动重装入口，手动重装会清除自动部署停止标记。
- Agent CA、Panel Agent 客户端证书和每台服务器已签发的 Agent 服务端证书作为“系统内置”资产展示，底层可作为 `metadata.systemManaged=true` 的系统托管资产保存，但不属于用户域 key asset，不能删除、导入、导出或注册为应用内部文件来源，只允许重置。每台服务器的 Agent 服务端证书是安装/重装任务同步到目标机 `/etc/panel-agent` 的部署产物；只有已记录证书指纹和有效期元数据的服务器证书会进入系统证书列表，重置单台服务器证书会复用该服务器的 Agent 部署任务。
- 重置 Panel Agent 客户端证书时保留 Agent CA，并热加载所有服务共享的 Agent gRPC client；重置 Agent CA 时同时生成新的客户端证书、热加载 gRPC client，并为所有已配置服务器排队重部署 Agent；重置单台服务器证书复用该服务器的 Agent 部署任务。
- Agent 部署成功后把服务端证书指纹和有效期写入服务器 traits，供服务器 Agent 状态、最后错误和部署任务排查使用；健康检查成功时也会从 TLS 握手中的远端服务端证书刷新这些元数据。
- 服务器必须启用 agent，通过 traits 记录：`agent.enabled=true` 且 `agent.url=https://host:9786`。该值表示 mTLS gRPC endpoint，沿用 `https://` 形式以兼容既有 trait 和证书部署逻辑，不再表示 HTTP API。Panel 启动后会扫描服务器，调度器也会周期检查已配置 agent；没有配置 agent URL 的服务器会自动创建 `server_agent_deploy` 任务；已配置 agent 但 URL 不是当前默认地址的服务器会标记为 `incompatible` 并自动重装；已配置当前默认 URL 的服务器会执行健康检查，检查结果写入 `agent.status`、`agent.last_checked_at`、`agent.version` 和 `agent.last_error` traits。`agent.version` 必须与当前 Panel 构建版本完全一致，否则标记 `incompatible` 并自动重装；健康检查返回的 `capabilities`、agent gRPC contract hash 和 Docker host 不作为兼容性门槛。连续系统自动部署失败达到上限后进入 `undeployable`。
- Agent 健康检查必须返回 Docker 健康状态和 Docker host；Panel 要求 Docker 正常且 agent 报告的 Docker host 与服务器配置一致。
- Application 运行时要求 agent 与 Panel 构建版本一致；部署编排在 Panel 侧完成，agent 只执行写托管文件、创建容器、容器动作和状态读取等原子接口。
- Agent 当前覆盖健康检查、`/etc/os-release`、系统 traits、metrics snapshot、UFW status、fail2ban status/apply、应用 runtime 文件写入/容器创建/stop/restart/status/logs/持久化目录打包与恢复，以及 Docker 容器、容器日志、镜像、网络和卷资源 API。持久化目录恢复先解压到同目录临时目录并校验，再通过 rename 原子交换；失败或取消时删除临时目录并保留原目录，全程响应 context 取消。应用 runtime stop 总会删除目标容器；`purge=true` 时额外删除实例运行目录，`removeApplicationData=true` 时删除整个应用运行目录。
- 反向代理设施应用依赖兼容 Agent 的 runtime 文件写入、容器创建、容器停止/删除、镜像拉取和容器启动能力；未部署、不兼容或不可用的 Agent 不会处理设施应用配置，也不回退 SSH。服务器 traits 中的 `agent.reverse_proxy.enabled` 由设施应用部署服务器列表派生维护，仅用于 UFW 安装时自动放行反向代理端口，不提供独立节点开关。
- 依赖 agent 的读取和运行时能力必须只在 `agent.status=compatible` 且 `agent.url` 存在时执行；agent 未部署、异常、不兼容、无法部署或客户端不可用时，当前操作或定时任务不得执行，也不得回退 SSH。例外是 agent 部署、重装、证书同步等恢复 agent 本身的任务。
- Docker 资源查询和操作只走 agent Docker Engine API，不回退 SSH。
- 启用 agent 后，读取类能力、软件包刷新/升级、UFW 状态及写操作、fail2ban 状态及配置应用、服务器重启、指标采集和应用运行时操作必须走 agent，不允许回退 SSH。UFW 在 `agent.status=compatible` 且 `agent.url` 存在时按 Agent 准入，不再依赖旧记录中的 `privilege.mode` 或 `sudo_passwordless` 字段；无可用兼容 Agent 时仍要求 root 或免密 sudo。APT/UFW/fail2ban 由 Agent 参数化调用固定命令；服务器重启由 Agent 通过 `busctl` 调用 logind D-Bus `Reboot`。
- 新增服务器完成首次信息采集且确认 root 或免密 sudo 特权能力后，会按自动部署修复判断创建 `server_agent_deploy` 任务安装 agent；后续 Agent 健康检查只在 agent 未配置、版本不一致、证书需要修复时自动部署，`agent.status=compatible` 且版本正常时不得重装，普通 `unavailable` 网络/远端错误和 Docker 不可用不得触发重装。
- Panel 启动检查或周期检查发现服务器未配置 agent URL、agent URL 不是当前默认地址、`agent.status=incompatible`、agent 版本与 Panel 不一致、证书过期/尚未生效/距过期不足 7 天，或健康检查因 mTLS server 证书时间错误失败时，必须自动创建或复用 `server_agent_deploy` 任务安装/修复 agent；证书进入 7 天续期窗口时保持 `agent.status=compatible` 且不写错误提示，只静默刷新部署；缺少能力、agent gRPC contract hash 不一致和 Docker host 不一致不单独触发重装；单纯 `agent.status=unavailable`、网络超时、连接拒绝、服务器失联或 Docker 不可用不得触发自动重装。安装/重装任务负责把当前 Panel 签发的 CA、服务端证书和私钥同步到目标机 `/etc/panel-agent`，默认监听 `tcp/9786`，重启前必须停止 systemd 服务并清理残留 `panel-agent` 进程，写入后必须校验远端 `server.pem` 确实是新签发证书，启动失败必须输出 `systemctl status` 和 `journalctl -u panel-agent.service` 诊断，启动后必须等待 `tcp/9786` 进入监听状态，并校验实际吐出的服务端证书指纹匹配新证书；部署证书包的 `agent.url` 必须使用当前服务器 `host` 生成；如果服务器 `host` 被修改，Agent URL 和证书元数据必须失效并要求重部署。如果已有排队或可重试的 `server_agent_deploy` 任务，手动部署、CA 重置、版本不一致、证书修复和服务器 host 变更触发的重装必须复用并立即启动该任务；系统自动触发复用任务时必须尊重任务 `next_run_at` 和自动部署退避时间，不得绕过指数退避立即重跑。同一服务器系统自动触发的 agent 部署失败达到 2 次后必须标记 `agent.status=undeployable` 并停止自动尝试；`undeployable` 状态不得由周期检查继续部署，失败计数只有在后续 Agent 健康检查连续 5 次正常后清空。
- `POST /api/v1/servers/{id}/agent/deploy` 是手动兜底入口，返回并启动 `server_agent_deploy` 任务；任务中心重试或立即运行也支持该任务类型。该任务的注册 executor 必须同步执行安装、健康检查和终态写入，避免任务中心在远端部署完成前显示 completed；HTTP 创建入口如果需要快速返回，只能在任务已标记 running 后由模块内 helper 后台执行。未安装时前端显示安装按钮，已安装但异常时显示重装按钮。服务器详情页只显示一条紧凑的服务器错误提示，优先展示 `agent.last_error`，不再在访问信息区重复渲染第二条 Agent 错误横幅。
- agent 部署任务通过 SSH 上传独立 `panel-agent` 二进制到目标机，再以 `/usr/local/bin/panel-agent` 的 systemd 服务运行；任务会写入 mTLS 证书、`PANEL_AGENT_DOCKER_HOST` 和 `/etc/systemd/system/panel-agent.service`，启动后回写 `agent.enabled=true`、`agent.url`，并以最多 10 次、每次 1 秒的健康轮询替代固定 sleep 检查兼容性。
- Panel 固定从 `/app/panel-agents/<goos>-<goarch>/panel-agent` 读取 agent bundle，并根据目标服务器结构化 `architecture.os`/`architecture.arch` 选择 `linux-amd64` 或 `linux-arm64`；结构化架构缺失时先探测目标节点并持久化结果。该位置不可通过配置或环境变量修改；发布镜像会把随 Panel 构建的 agent bundle 复制到 `/app/panel-agents`，部署任务每次直接读取对应文件并上传到目标机。
- `POST /api/v1/servers/{id}/agent/certificate` 签发目标机 `panel-agent` 的 mTLS server 证书包；响应包含 CA、server certificate、server private key、建议监听地址、agent URL 和 Docker host，只作为高级手动安装兜底，不会落库。

## Agent 重启就绪检查（PrepareRestart）

- Agent 新增流式 gRPC 接口 `PrepareRestart(Empty) returns (stream PrepareRestartResponse)`，响应 `state` 取值 `holdon` 或 `ready`；服务端实现位于 `internal/agent/rpc/prepare_restart.go`，Panel 侧客户端调用在 `internal/agent/client`，契约常量在 `internal/agent/contract`。
- Agent 在 `HealthResponse.capabilities` 中额外声明可选能力 `prepare-restart`；它不加入 `RequiredCapabilities`，不作为兼容性门槛，旧 agent 缺失该能力时 Panel 直接继续部署。
- Agent 进程内通过 `packageUpgradeTracker`（引用计数 + done channel）跟踪经 agent 发起的软件包升级（`UpgradePackages`）。`PrepareRestart` 在升级进行中每秒返回一次 `holdon`；升级结束后返回 `ready` 并关闭流；Panel 断开或取消（流上下文结束）时直接退出、不返回 ready。该检查只感知 agent 自己发起的升级，不检查外部 apt/dpkg 状态。
- Panel 执行 agent 部署（`runDeployAgent`）时，只要 `agent.url` 已配置，就在任何停止/重启动作前先调用 `Health` 读取能力：能力包含 `prepare-restart` 且客户端实现 `RestartReadinessClient` 时调用 `PrepareRestart`（客户端与 Panel 侧均有最长 10 分钟的等待上限），收到 `ready` 才继续；能力缺失（旧 agent）、Health 或调用失败（含等待超时）时记录日志后直接继续；任务取消时中止部署。
- 部署任务按 `agentNeedsBinaryUpgrade` 拆分两条路径：需要完整安装/升级（未配置 URL、版本缺失或与 Panel 不一致、agent 状态非 compatible/incompatible）时上传二进制并执行 `agentInstallScript`；证书续期、URL 修正等仅需重启的场景只重写证书/env/systemd 配置并执行 `agentRestartScript`，不传输二进制。二进制 bundle 根路径仍固定为 `/app/panel-agents`。
## Agent 只读 CLI（--cli apps）

- `panel-agent` 支持 `--cli` 命令行入口：`panel-agent --cli apps <command>`，用于在节点上直接读取本机 Docker 中 Panel 管理的容器信息；不带 `--cli` 时行为完全不变，仍启动 gRPC 服务。实现位于 `internal/agent/cli`，入口在 `cmd/panel-agent/main.go`。
- 命令：
  - `apps list [--json] [--docker-host <host>]`：列出 Panel 管理容器（标签 `panel.application.managed=true`），默认表格输出，`--json` 输出完整 `DockerContainer` 数组；
  - `apps inspect <selector> [--json]`：单个容器详情，含 Panel 关联字段（application/instance id、generation、spec hash、apply mode、managed files 观测字段）与路径（应用 home、实例目录、persistent 目录）；
  - `apps where <selector>`：打印应用主目录 `/opt/panel/apps/<appID>`（目录必须存在）；
  - `apps cd <selector>`：在应用主目录下启动交互式子 shell（优先 `$SHELL`，回退 `sh`）。
- selector 按 容器名 > 实例 ID > 应用 ID 匹配；应用 ID 对应多个实例时报错，提示使用容器名或实例 ID。
- Docker host 优先级：`--docker-host` > `PANEL_AGENT_DOCKER_HOST` > `unix:///var/run/docker.sock`。
- 退出码：0 成功；1 运行错误（Docker 不可达、selector 找不到/歧义、home 不存在）；2 用法错误。错误写 stderr，帮助写 stdout。
- 该 CLI 只在本机读取 Docker，不接入 gRPC，不提供任何容器变更操作。

## 验证

- 后端服务器、凭据、agent、指标、软件包、UFW 或 fail2ban 行为改动运行 `task test:backend`。
- 前端服务器页面、API 或类型改动按影响范围运行 `task test:web` 或 `task build:web`。

## 文档更新触发

新增支持系统、远程命令、服务器字段、凭据字段、agent 能力、指标字段、软件包行为、防火墙/fail2ban 行为或相关 API 时，必须更新本文档。
 
## Agent Report Stream

- Agent deployment must not write any Panel callback or report address into `/etc/panel-agent/panel-agent.env`; nodes must never assume Panel is reachable from the node side.
- Panel actively dials the normal agent mTLS endpoint and opens `AgentReportService.Report` for metrics and container status reporting. The agent only responds on this Panel-initiated stream.
- The report stream uses the same Panel Agent CA and endpoint as other agent gRPC calls.
- Panel inspects report streams every 5 seconds, reconnects missing streams, rebuilds streams when `agent.url` changes, and marks streams with no incoming messages for `max(10s, min(metricsInterval, containerInterval) * 3)` as disconnected. This only updates `agent.report.status`, `agent.report.last_message_at`, and `agent.report.last_error`; it must not downgrade the normal Agent health status.
- Periodic metrics and container reports use Unix-aligned sampleAt values. For an interval of 3 seconds, the stored timestamp must be divisible by 3 and must represent the scheduled sample boundary, not the receive time. Docker event-triggered container_change snapshots use the event sampling second and are accepted outside the periodic boundary.
- Agent 观察到 Docker image 事件（pull/tag/untag/delete/import/load/save）或 dpkg 状态文件变化时，会在上报流直接推送镜像快照（`images` 字段）或软件包更新列表（`package_updates` 字段）；Panel 收到后直接落库，不再触发 Panel 侧刷新任务。Agent 侧做防抖：镜像推送至少间隔 30 秒，软件包推送至少间隔 10 分钟。Panel 的 30 分钟周期检查（`image_refresh` / `package_refresh`）继续作为主动兜底；推送或落库失败只记录日志，不中断上报流。指标落库失败同样只记录日志，不中断上报流。
- 容器快照 Panel 侧有空报告防御：agent 本次未携带容器快照（`Containers` 为空）时，Panel 不调用 `SaveReportedContainers`，不会用空列表清空既有容器观察；agent 侧不再广播空对象。
- Agent metrics and Docker status collection is driven by a shared watcher hub: no active stream means no sampling, and multiple streams reuse the same sample instead of collecting multiple times. Docker container events also wake the hub to send immediate `container_change` full snapshots in addition to periodic samples.
