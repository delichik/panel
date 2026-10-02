# 服务器、安全防护与软件包验收规范

本文约束 SSH 凭据、服务器登记、主机信任、系统探测、Panel Agent、UFW、fail2ban 和 APT 软件包维护。异步操作的 2xx 仅代表任务可靠创建或复用，不代表远端动作已完成。

## 1. SSH 凭据

- `CRED-API-001`：认证用户调用 `GET /api/v1/credentials` 时，只接受 `page`、`pageSize`、`q`，按创建时间倒序和 ID 稳定分页；行只含 ID、名称、类型、用户名和时间，不得选择或返回任何密文、密码、私钥或口令。
- `CRED-API-002`：`q` 必须只匹配名称或用户名并转义 LIKE 元字符；空结果返回合法 `ListPage`，越界或非法分页按通用分页合同处理。
- `CRED-API-003`：创建凭据必须要求非空名称、用户名及 `password|private_key` 类型；password 必须提供密码，private_key 必须提供私钥；验证失败不得写入记录。
- `CRED-API-004`：创建成功必须仅在 `credentials.secret_ciphertext` 保存经 secret store 加密的秘密 JSON；API、任务参数、日志和旧 `password_secret/private_key_path/passphrase_secret` 不得出现新明文。
- `CRED-API-005`：编辑同类型凭据时空 secret 表示保留旧秘密；非空 secret 才替换。切换类型时必须提供新类型必需秘密，并清除旧类型不再适用的秘密。
- `CRED-API-006`：`GET /credentials/{id}` 对私钥可返回算法、位数、SHA256 指纹和注释摘要；解密或摘要解析失败只省略摘要，仍不得返回私钥；不存在返回 404。
- `CRED-API-007`：删除未引用凭据成功后记录不可再读；被任一服务器引用时必须返回 409 `credential_in_use` 且保留凭据和引用。
- `CRED-MIG-001`：启动迁移旧秘密时必须先写入密文并验证可解密，再删除旧私钥文件，最后清空旧字段；任一步失败必须拒绝启动，重复执行不得丢失秘密。

## 2. 服务器列表、登记与删除

- `SRV-API-001`：`GET /api/v1/servers` 只接受 `page`、`pageSize`、`q` 并返回 `ListPage<ServerSummary>`；摘要只含列表所需身份、IP/地址、可达性、credentialId及 Agent/UFW/权限信号，不得读取或返回 notes、variables、完整 traits、Docker 配置、完整 OS/架构、指标或凭据秘密。
- `SRV-API-002`：`GET /servers/{id}` 才返回完整详情；纯读取列表或详情不得创建连通性、系统探测、Agent 部署或其他后台任务。
- `SRV-SAVE-001`：创建/更新必须要求名称、credentialId、1..65535 端口和非空 dockerHost；ipv4/ipv6 至少一个，且必须分别是对应族的 IP 字面量；客户端提交 `host` 必须以 `server_host_derived` 拒绝。
- `SRV-SAVE-002`：连接 host 必须由 ipv4 优先、否则 ipv6 派生；旧记录仅有 IP 字面量 host 时读取可回填对应族，非 IP hostname 不得伪造为新合法地址。
- `SRV-SAVE-003`：dockerHost 缺省展示值为 `unix:///var/run/docker.sock`，保存后必须用于 Agent 环境 `PANEL_AGENT_DOCKER_HOST`；Agent 通过 Docker Engine API 工作，不得改用 Docker CLI。
- `SRV-SAVE-004`：用户提交的 traits 必须忽略；系统 traits、架构、权限、Agent 和设施标记只由后端探测与协调更新。variables 和 notes按资源字段保存，秘密不得混入。
- `SRV-SAVE-005`：创建记录成功且 SSH executor 可用时必须创建并立即启动 `server_info_collect` bootstrap 任务，响应携带 `initialTaskId`；任务创建失败必须保留刚建记录并标记不可达/失败态，不得删除用户数据。
- `SRV-SAVE-006`：首次 bootstrap 只经 SSH探测发行版、结构化架构和非交互特权；初始信息采集失败必须把任务置失败、标记服务器不可达并记录具体错误，记录保留供用户重试、编辑或自行删除；之后 Agent 部署或完整信息刷新失败同样不得删除服务器。
- `SRV-SAVE-007`：更新必须先保存资源；随后的连通性探测失败只把节点标记不可达并记录错误，不得回滚更新或阻断 DNS 同步触发。
- `SRV-SAVE-008`：更新改变连接 host 且已配置 Agent 时，必须更新该服务器当前有效的 Agent endpoint、标记 incompatible、清除节点证书指纹/有效期并要求重部署。普通服务器固定为 `https://host:9786`；NAT 服务器使用 `SRV-NAT-002` 配置的对外端口。
- `SRV-NAT-001`：服务器 `kind` 只允许 `normal|nat`，缺省为 `normal`；创建、更新、详情和列表摘要都必须返回该字段。非法 kind 返回 `server_kind_invalid`，不得保存。
- `SRV-NAT-002`：`agentPublicPort` 只对 NAT 服务器生效，必须位于 1..65535；普通服务器或非法值保存为 0。NAT Agent 仍在服务器内部监听 9786，但 Panel 生成和校验的 Agent URL 必须使用 `https://host:<agentPublicPort>`。未配置时沿用 9786。
- `SRV-NAT-003`：NAT 服务器不得成为反向代理全局网关、应用 origin 或 AnyAccess relay；保存网关或校验应用路由时分别返回 `reverse_proxy_server_nat_unsupported`、`reverse_proxy_origin_server_nat_unsupported`。服务器从普通切换为 NAT 时必须移除 `agent.reverse_proxy.enabled`，后续协调不得继续放行 80/443。NAT 服务器同样豁免 `AGT-FW-001` 的防火墙自动接管。
- `SRV-NAT-004`：`GET/POST /servers/{id}/nat-ports` 与 `PUT/DELETE /servers/{id}/nat-ports/{mappingID}` 只允许 NAT 服务器；普通服务器返回 `nat_port_server_not_nat`。映射记录宿主端口、供应商开放的公网端口、协议、应用、标签和备注；两个端口必须位于 1..65535，同一服务器的宿主端口和公网端口各自唯一，冲突返回 `nat_port_host_conflict` 或 `nat_port_public_conflict`。
- `SRV-NAT-005`：NAT 端口读取必须同时返回只读“需开放端口”清单，至少包含 SSH 端口、有效 Agent 对外端口和全部应用公网端口。Panel 不得探测、申请或实际开放这些端口；删除服务器时映射必须随外键级联删除。该禁止是 `AGT-FW-004` 中 NAT 豁免防火墙自动接管的直接理由：在公网端口由服务商映射的节点上启用默认拒绝策略，可能切断服务器且无人能重新打开。
- `SRV-SAVE-009`：保存 IP 变化或删除服务器时必须异步触发引用该服务器的入口代理 DNS 同步；同步失败不回滚本地保存，且必须可由任务状态诊断。
- `SRV-DEL-001`：删除服务器是纯本地控制面操作，不连接目标机；目标机失联不得阻止删除。
- `SRV-DEL-002`：删除必须取消该服务器 queued、scheduled、failed_retryable 和可取消的 running 任务；已取消任务不得被迟到 worker 覆盖终态，正在执行的不可取消软件包升级不得被取消。
- `SRV-DEL-003`：同一 AppDB 事务内必须删除服务器、修剪应用 deployment server IDs、递增受影响应用 version并更新时间、移除概览卡片 serverIds；外键级联负责包/镜像缓存、实例和协调状态。
- `SRV-DEL-004`：AppDB 删除完成后必须清理 MetricsDB 的服务器指标；任一步失败必须返回错误，不得假称完整删除成功；再次删除不存在 ID 返回 404。
- `SRV-DEL-005`：AppDB 删除完成后应尽力删除该服务器的 Agent 节点证书资产（`agent-server-<serverID>`）；清理失败只记录告警日志，不得把已提交的删除报为失败，残留孤儿资产不得影响后续任何服务器的证书签发。

## 3. 连通性、特权与主机密钥

- `SSH-TOFU-001`：首次 SSH 连接必须以 `host:port` 身份把服务端公钥持久化到 `<dataRoot>/known_hosts`；后续公钥匹配才可继续，不得默认使用 InsecureIgnoreHostKey。
- `SSH-TOFU-002`：公钥变化必须失败关闭并返回 502 `ssh_host_key_mismatch`；服务器详情和摘要必须暴露 `hostKeyMismatch=true`，同时保留可能为重装或中间人攻击的安全提示语义。
- `SSH-TOFU-003`：known_hosts 读取、解析或写入失败必须返回 `ssh_host_key_verification_failed`；不得为了可用性跳过验证。
- `SSH-TOFU-004`：管理员调用 `POST /servers/{id}/trust-host-key` 时必须先进行明确危险确认；后端只在 TCP、SSH握手和凭据认证成功后替换该身份的 key，再复用连通性测试清除旧错误并返回更新服务器。
- `SSH-TOFU-005`：显式信任时网络/握手失败返回 `ssh_connection_failed`，凭据拒绝才返回 `ssh_auth_failed`，未启用 known_hosts 返回 `host_key_verification_disabled`；失败不得改写已有 key。
- `SSH-OPS-001`：`POST /servers/probe` 只对表单输入进行实时 SSH预检并返回 reachability、root/passwordless sudo、OS、架构和 traits，不得写服务器或创建任务。
- `SSH-OPS-002`：`POST /servers/{id}/test` 必须同步验证 SSH并持久化可达性与 privilege mode，不进入任务中心；失败也必须记录不可达、类型化 host-key 标志和安全摘要。
- `SSH-OPS-003`：特权状态固定为 `root|passwordless_sudo|none`；UID 0 为 root，非 root 仅 `sudo -n` 成功才为 passwordless_sudo，派生 privileged 与时间必须一致。
- `SSH-OPS-004`：远程命令、连接和上传必须受运行时 timeout 或操作专用 timeout 控制；超时返回稳定 timeout错误，命令退出非零返回 `remote_command_failed`，不得把 stdout/stderr 中秘密写入对外错误。
- `SSH-OPS-005`：特权命令仅 root 直跑，其他允许路径使用非交互 sudo；软件包名、UFW 参数和远程路径必须经过白名单/结构化参数验证，禁止用户输入直接拼 shell。

## 4. 系统探测与 Agent 状态机

- `AGT-STATE-001`：Agent 状态只有 `compatible|incompatible|unavailable|undeployable`；只有 compatible 且 URL存在时，依赖 Agent 的探测、UFW/fail2ban、指标、软件包、Docker和应用运行时才可执行。
- `AGT-STATE-002`：Agent enabled 后读取和写入能力不得回退 SSH；恢复 Agent 本身的部署、证书同步和 bootstrap SSH 是唯一例外。
- `AGT-STATE-003`：健康检查必须验证 Panel/Agent 构建版本相同、Docker健康且报告的 Docker host 与服务器配置一致；版本不一致为 incompatible，网络/远端/Docker不可用为 unavailable。
- `AGT-STATE-004`：capabilities 和 gRPC contract hash 不作为通用兼容门槛；具体操作缺少必需 capability 时仅该操作失败或重试，不得因此无条件重装 Agent。
- `AGT-STATE-005`：周期 Agent 检查由 `server_agent_check` 任务承载（每 5 分钟一轮，按服务器各一个子任务，定义同时声明 `Hidden` 与 `Quiet`）。检查成功只写回服务器 traits（状态、版本、证书、检查时间），其执行事实按 `TASK-REG-003` 记为 `debug`，默认级别筛选下不进入事件与操作视图；检查失败（含证书时间错误导致的自动修复判定）仍记 `error`。降噪不得跳过状态写回、自动部署判定或失败可见性。
- `AGT-AUTO-001`：无 URL、URL 非默认地址、incompatible、版本不一致、证书过期/未生效/7天内到期或证书时间握手错误时，系统检查必须创建或复用 `server_agent_deploy`；普通 unavailable、连接拒绝、服务器失联或 Docker失败不得触发重装。
- `AGT-AUTO-002`：证书进入 7 天窗口时保持 compatible且不写错误，只静默刷新；系统自动复用任务必须尊重 `next_run_at` 和指数退避，不能借复用绕过。
- `AGT-AUTO-003`：同一服务器系统自动部署连续失败 2 次必须置 undeployable并停止周期自动尝试；手动部署解除阻止并重置退避基准；失败计数仅在连续 5 次健康检查成功后清零。
- `AGT-DEP-001`：手动 `POST /servers/{id}/agent/deploy` 必须创建或复用并启动任务，先标记 running再响应；任务 executor必须直到安装、健康检查和终态写入完成才返回。**若复用到的既有 `server_agent_deploy` 任务状态仍为 `running`，必须返回冲突 `agent_deploy_in_progress`（消息含 taskId 与 stage），不得返回成功**：`running` 的任务无法重启，返回成功只会让界面弹出「已受理」而实际什么都没发生（日志不变、没有新执行），操作者无从判断点击是否生效。`stage=uncertain` 的任务状态同样是 `running`，消息必须指向任务中心作为唯一出路。系统自动触发路径不受此约束，仍按退避与复用语义返回任务。
- `AGT-DEP-006`：远端步骤因超时关闭 SSH 会话后，等待会话 goroutine 必须有界（`sessionCloseGrace`，15 秒）。远端进程不配合时执行器也不得永久阻塞：执行器不返回会让拥有它的任务永远停在 `running`，而 `running` 的任务会让之后每一次手动重新部署都变成静默无操作（见 `AGT-DEP-001`）。
- `AGT-DEP-002`：部署必须按持久化的 `architecture.os/arch` 选择固定 `/app/panel-agents/linux-amd64|linux-arm64/`，并同时要求构建期产出的 `panel-agent.gz` 与 `panel-agent.sha256` 存在且可读；缺架构时先 SSH探测并写回，文件缺失、平台不支持或 sha256 文件不是规范小写十六进制时返回 `agent_binary_unavailable`。Panel 不得在运行时压缩二进制或计算其哈希。
- `AGT-DEP-003`：完整安装必须先把压缩包投递到目标机（HTTP 下载或 SSH 回退），再执行共用安装段（`gzip -dc` 解压、sha256 校验、停 systemd 与残留进程、安装、启动并等待 `tcp/9786`）；仅证书续期或 URL修复时只重写证书/env/systemd 配置并重启，不重复传输二进制。下载与安装必须是两个独立的远端步骤，避免分类下载失败时重复执行已停过服务的安装逻辑。
- `AGT-DEP-004`：重启 Agent 前若能力包含 `prepare-restart`，必须最长等待10分钟至 ready；holdon 首次立即记录且最多每15秒记录一次进度；能力缺失、健康/流失败或超时可记录后继续，任务取消必须停止部署。
- `AGT-DEP-005`：远端部署必须停止 systemd和残留进程，写入 `/etc/panel-agent`、`--srv` service及端口9786，验证文件证书和实际服务证书指纹，失败日志包含 systemctl/journal诊断但不得包含私钥。两条投递路径都必须校验压缩包解压后的 sha256 与经本次 SSH 调用下发的期望值一致，不匹配必须终止安装。
- `AGT-CERT-001`：`POST /servers/{id}/agent/certificate` 只作为高级手动安装兜底返回 CA、节点证书、私钥、监听地址、Agent URL 和 Docker host，不落库；响应必须受认证且不得进入日志或缓存。
- `AGT-CERT-002`：系统证书列表只展示已有元数据的 Agent CA、Panel client、节点 server证书和 Panel TLS链；系统资产不可通过普通 key asset API下载、导出、删除、重签或作为应用文件。
- `AGT-CERT-003`：重置 Panel Agent client 保留 CA并热加载共享 gRPC client；重置 Agent CA同时生成 client并为全部已配置服务器排队重部署；重置单节点证书复用节点部署任务。
- `AGT-CERT-004`：为服务器签发 Agent 证书时，节点证书材料持久化为 `key_assets` 中按 `agent-server-<serverID>` 命名的系统资产；资产名称必须包含稳定 serverID，服务器重名、删除后重建同名服务器或重复签发都不得违反 `key_assets.name` 唯一约束；重新签发必须按 serverID 原地更新同一资产，不产生重复资产。
- `AGT-DL-001`：目标机下载端点固定为 `GET /agent/{version}/{platform}/panel-agent.gz`，注册在主 Panel mux 且位于 `/api` 之外。`{version}` 取 `buildinfo` 版本号；仅当版本号不唯一（本地构建的 `dev`）时才使用内容寻址的 `dev-<sha256前12位>`，其中 sha256 取解压后二进制。`{platform}` 只接受 `linux-amd64`、`linux-arm64`。同一二进制对所有服务器必须产生同一个 URL。`agent.downloadBaseUrl` 为空时部署行为必须与引入本能力之前完全一致（只走 SSH 上传）。
- `AGT-DL-002`：端点不鉴权，且不得设置 `Set-Cookie`、不得读取或回显会话状态。命中响应必须为 200（Range 请求为 206）并带 `Content-Type: application/gzip`、`Cache-Control: public, max-age=31536000, immutable`、sha256 的强 `ETag`、`Accept-Ranges: bytes`、`X-Content-Type-Options: nosniff`，且不得带 `Content-Encoding`。必须支持 `Range` 请求。
- `AGT-DL-003`：`{version}` 与当前构建不匹配时必须 404 且带 `Cache-Control: no-store`，不得退化为「服务当前版本」，也不得把请求重写为另一个版本。`/agent/` 前缀下的其他路径必须 404，不得落到 SPA 静态兜底返回 200 HTML。
- `AGT-DL-004`：目标机安装前必须用同一次 SSH 调用下发的期望 sha256 校验解压后的二进制。**两条投递路径都必须校验**（SSH 回退路径此前没有校验）。校验和不匹配、解压失败或缺少 `gzip`/`sha256sum` 时必须终止安装，不得继续，也不得回退。
- `AGT-DL-005`：HTTP 下载失败的回退条件严格限定为可达性问题：目标机没有带总时限的 fetcher（curl，或 wget + `timeout`）、DNS 或连接失败、超时、HTTP 4xx/5xx。传输中断或截断、解压失败、哈希不匹配属于数据问题，必须直接失败且任务标记失败，不得回退 SSH 上传。回退时任务日志必须写明分类原因与「falling back to SSH upload」。
- `AGT-DL-012`：目标机没有可用 fetcher 时，部署必须**尝试在该机器上安装 `curl` 并重试下载一次**，不得直接回退——否则每个最小化镜像都会静默失去 HTTP 投递，功能等于默认不生效。约束：只有在已配置下载基址、且发行版受支持（`linux.Supported(server.OS)`）时才安装；安装必须使用固定的非交互 apt 参数（`DEBIAN_FRONTEND=noninteractive`、`-o Dpkg::Options::=--force-confdef`、`--force-confold`、`--no-install-recommends`），并复用 `remoteops.APTInstallPrelude` 以保持参数单一来源。**该步骤的任何失败（apt 到不了软件源、安装无效、超时、传输错误、不支持发行版）都只能导致回退 SSH 上传，绝不能让部署失败。** 该步骤必须与下载步骤分离并各有独立超时；其目标机侧最坏耗时（最多两次 apt 调用）必须严格小于 Panel 侧上限，使脚本总能自行结束并回报。重试只允许一次。
- `AGT-DL-006`：传输必须有界且可配置（`agent.transferTimeoutSeconds`，默认 300 秒，取值范围 60..3600）。Panel 侧与目标机侧都必须限时，且**目标机侧整个下载的最坏耗时**必须严格短于 Panel 侧：该最坏耗时等于「轮数 × 单轮上限 + 轮间延迟之和」，验收测试必须按脚本实际生成的参数断言这个乘积，不得只断言某个辅助函数的算术（早期版本正是因此让 `curl --retry` 把最坏耗时放大到 Panel 侧上限之外）。**下载脚本禁止使用 curl `--retry`/`--retry-delay`**：curl 的 `--max-time` 按次生效，每次重试都会重新获得完整时长。Panel 侧超时本身不可分类，不得作为回退依据；该情形一旦发生，失败会退化为不可分类的远端超时。
- `AGT-DL-013`：下载必须由**有限轮次的续传尝试**完成（curl `-C -`、wget `-c`），每轮受单轮上限约束、轮间有固定延迟，整体仍受 `AGT-DL-006` 的最坏耗时约束。比单轮预算更慢但可用的链路必须能跨轮收敛，而不是每轮从 0 重来。服务器明确拒绝 Range（curl 退出码 33）时必须丢弃残file 并按整轮重新下载，不得当作数据损坏；因此类截断而失败时，任务日志必须提示操作者可以调大传输超时。
- `AGT-DL-007`：`agent.downloadVerifyTls` 为 false（默认）时 fetcher 必须带 `-k` / `--no-check-certificate`，为 true 时不得带。关闭校验只影响传输保密性：完整性由 `AGT-DL-004` 的 sha256 锚定保证，因此该默认值不构成完整性降级，但必须在设置页文案中说明。
- `AGT-DL-008`：下载 URL 与期望 sha256 只能作为一次性命令参数出现，不得写入 `/etc/panel-agent/*`、不得进入服务器 traits、不得落库到任务参数之外的任何持久化位置，也不得出现在 agent 环境文件或 systemd 单元中；部署完成后目标机不得保留任何 Panel 地址。
- `AGT-DL-009`：镜像产物必须是 `panel-agent.gz` 与 `panel-agent.sha256`（解压后二进制的 sha256），`.gz` 必须用 `gzip -9 -n` 生成以免同一输入产生不同字节。Panel 运行时不得做实时压缩或实时计算哈希。
- `AGT-DL-010`：端点路径必须固定——`panel-agent.gz` 是注册 pattern 里的字面量而非路径参数；`{version}` 与 `{platform}` 只作为固定表查找键，任何请求数据都不得参与拼接文件系统路径。`..`、编码斜杠、反斜杠与非白名单平台一律不可服务。
- `AGT-DL-011`：脚本中所有插值必须经 `ShellQuote`；`agent.downloadBaseUrl` 保存时必须拒绝引号、空白、反斜杠与 shell 元字符，并限制为不含 path/query/fragment/userinfo 的 `http(s)://host[:port]` 源。
- `AGT-FW-001`：防火墙是 Agent 部署的**前提**，必须在触碰节点上任何东西之前执行（签发节点证书之后、二进制投递之前），且**完整安装与仅重启两条分支都要执行**，使本次改动前已加入的存量服务器在下次部署时收敛。任一步失败必须让部署任务失败，且**不得改动已在运行的 Agent**——节点上已有可用 Agent 时，防火墙步骤失败只能让它保持原样。
- `AGT-FW-002`：Panel 只通过 UFW 管理防火墙。发行版没有 UFW 适配器时必须在部署前以 `agent_firewall_unsupported` 失败：`SRV-SAVE-006` 的受支持发行版范围因此收窄为 Debian/Ubuntu，Agent 不再部署到其他发行版。发行版记录为空（首次信息采集尚未完成就手动部署）时必须先 SSH 探测再判定，不得因空记录直接拒绝。
- `AGT-FW-003`：放行的基础端口集合固定为 SSH 端口（`normalizedTCPPort(server.Port)`）、Agent 内网监听端口（`agentControlPort`）与反向代理 trait 为真时的 80/443；**基础集合内不得包含应用端口**。应用端口由 Agent 在 `RuntimeReconcile` 中按应用 spec 的 `openFirewall` 声明自行放行（`panel:application:<id>` 规则），UFW 处于 disabled 时规则仍保留，因此先前写入的应用规则会在本次启用时一并生效。声明了宿主端口但 `openFirewall=false` 的应用会被阻断，必须在添加服务器的提示中写明。
- `AGT-FW-004`：**NAT 服务器豁免**：不安装、不放行、不启用，也不因防火墙失败而阻断部署。理由：其公网端口由服务商映射，`SRV-NAT-005` 禁止 Panel 探测、申请或实际开放这些端口，在 NAT 节点启用默认拒绝策略可能切断服务器且无人能重新打开。
- `AGT-FW-005`：只有一次 SSH 状态探测确认 UFW 已安装且已启用时才允许跳过远端变更，此时不得下发任何 `ufw --force ...` 或 apt 命令。需要启用时，`ufw --force enable` 之前必须先放行 SSH 端口与 Agent 端口；`UFWEnableScript` 是唯一允许触碰 SSH 端口的实现，该顺序必须有测试断言。防火墙步骤经 **SSH** 而非 Agent RPC 执行：首次安装时 Agent 尚不存在，且「恢复 Agent 自身」是 `AGT-STATE-002` 允许回退 SSH 的唯一例外。
- `AGT-FW-006`：安装步骤必须复用发行版适配器的固定 apt 参数（`DEBIAN_FRONTEND=noninteractive`、`--force-confdef`、`--force-confold`），并整体受 `ufwInstallTimeout` 约束；重复 `ufw allow` 必须幂等，步骤失败必须可由任务日志中的端口清单与远端输出定位。
- `AGT-RPT-001`：Panel 必须主动拨号打开 mTLS report stream，节点不得保存 Panel callback地址；stream状态仅写 `agent.report.*`，不得降级普通 Agent status。**运行时不变量**：已安装的 Agent 不得假设 Panel 可达，也不得主动连接 Panel。安装期例外仅限一次性的 `AGT-DL-001` 下载：目标机可以用命令参数里的 URL 拉取自己的二进制，连不上时必须按 `AGT-DL-005` 回退 SSH 上传，且不得因此在本机持久化任何 Panel 地址。
- `AGT-RPT-002`：流断开重连采用连续失败 5秒起至5分钟封顶退避；一旦该连接成功交付报告即重置退避，等待退避的连接不得被静默检测循环反复取消。
- `AGT-RPT-003`：指标、容器、镜像或软件包报告的落库失败只记录日志，不中断流；容器保存失败不得触发应用 reconcile，空容器报告不得清空已有观察。
- `AGT-RPT-004`：周期样本时间必须 Unix interval对齐；Docker事件触发的 `container_change` 可不对齐。缓存未齐或任一指标超过15秒未成功采样时不提交指标，Panel保留旧值。网络速率必须是采集窗口内的平均字节数每秒：Agent 按固定节拍读取 `/proc/net/dev` 累计收发字节并在内存累加相邻读数差，上报整点消费累加值除以自上次提交以来读数窗口的实际时长，因此采集间隔为 N 秒时样本反映这 N 秒产生的流量平均值，不得上报单次 1 秒瞬时样本；读数之间计数器回退（网卡重建、计数器回绕）按 0 增量跳过，消费后没有新读数时不产出网络指标（整体保持不提交）。

## 5. UFW

- `UFW-API-001`：UFW读取和写入前必须确认服务器存在、支持发行版、可达且适配器支持UFW；无兼容 Agent 时还要求 root或免密sudo，兼容 Agent存在时按 Agent准入。
- `UFW-API-002`：`GET /servers/{id}/ufw` 必须返回 supported、installed、active、status、default policy及带稳定序号的 rules；启用 Agent 后只经 Agent读取，不得回退SSH。
- `UFW-API-003`：添加规则只接受 1..65535端口、`tcp|udp|any` 协议和合法来源；必须确认UFW已安装，成功后返回重新读取的状态。
- `UFW-API-004`：删除规则要求正整数规则号，成功后返回新状态；不存在/远端失败不得在UI本地假删。
- `UFW-API-005`：**已废弃（替代项 `AGT-FW-001..005`）**。UFW 安装与启用不再由用户触发，也不再各自创建任务；本能力改为 Agent 部署时的自动前提，因此不再有 `POST /ufw/install`、`POST /ufw/enable` 与 `server_ufw_install`、`server_ufw_enable` 任务类型。保留编号以维持引用稳定。
- `UFW-API-006`：开启防火墙前必须先放行 SSH端口，再按派生 reverse-proxy标记放行入口端口；该不变量现由 `AGT-FW-005` 在 Agent 部署路径上保证，并必须由测试断言顺序。
- `UFW-API-007`：状态读取与规则增删是防火墙页仅存的操作。页面不得再提供安装或启用入口，也不得把 `sys.ufw_installed`/`sys.ufw_supported` 用作按钮可用性判断；必须改为说明「安装与启用由 Agent 部署自动完成」（`securityPage.managedAutomatically`）。

## 6. fail2ban

- `F2B-API-001`：状态读取必须要求受支持、可达和特权节点；有兼容 Agent 时合并远端 installed/active/panelConfigPresent/jails/raw 与 AppDB 草稿、managed和更新时间。
- `F2B-API-002`：没有保存记录时必须返回稳定默认 jail草稿且 `managed=false`；远端不可查询时不得伪造 installed/active成功状态。
- `F2B-API-003`：`PUT /fail2ban` 只解析、规范化并保存 Panel YAML草稿，不写目标机、不自动启用；保存成功后返回状态。
- `F2B-VAL-001`：YAML必须拒绝未知字段、空 jail集合、非法/重复 jail名、负 maxretry、含换行的标量与非法 option key/value；失败不得覆盖旧草稿。
- `F2B-API-004`：启用时可使用请求 YAML或已存草稿；远端已安装但尚未接管时必须要求 `confirmTakeover=true`，否则返回 `fail2ban_takeover_confirmation_required`。
- `F2B-API-005`：apply必须由 `server_fail2ban_apply` 任务经 Agent写 `/etc/fail2ban/jail.d/panel.local`，远端配置测试通过并reload/restart成功后才把 managed置true；失败保留原 managed状态。
- `F2B-API-006`：release必须使用独立 `server_fail2ban_release` 任务，仅删除 Panel生成配置并在成功后置 managed=false；不得删除、导入或恢复用户自建配置。
- `F2B-API-007`：install等价于经确认的 apply并可安装缺失软件；apply与release的任务去重域必须分离，重复同操作可复用但相反操作不得误复用。

## 7. APT 软件包

- `PKG-API-001`：`GET /servers/{id}/packages/updates` 返回 AppDB缓存、lastRefreshedAt与当前进程 refreshing；缓存缺失或超过10分钟可触发一次自动刷新，但列表响应不得等待 apt完成。
- `PKG-API-002`：只允许受支持发行版；刷新与升级在生产装配下必须经 compatible Agent和固定 apt参数执行，不回退SSH。
- `PKG-API-003`：手动 refresh创建或复用 `package_refresh` 任务并返回 taskId；30分钟周期批次对所有节点共享 operationId，10分钟内已有刷新任务时跳过重复创建。
- `PKG-API-004`：同一服务器刷新、升级选中和升级全部必须共享维护互斥；并发动作返回/记录 `package_maintenance_in_progress`，不得误标成功或并行运行apt。
- `PKG-API-005`：升级选中必须至少含一个非空包名并逐项通过安全字符白名单；缺失返回 `packages_required`，非法名返回 `package_name_invalid`，不得创建可执行任务。
- `PKG-API-006`：升级要求已确认 root/免密sudo准入；成功创建 `package_upgrade_selected|all`，选中名称持久化在 task params以供重启恢复，任务允许 retry/run-now。
- `PKG-API-007`：升级任务不可取消；Agent端 apt事务使用与RPC连接隔离的执行上下文和独立超时，Panel断线或重启不得中断正在进行的dpkg事务。
- `PKG-API-008`：升级成功后必须重新刷新缓存，再将任务置 completed；升级或刷新缓存任一步失败都置 failed且不能展示成功。
- `PKG-API-009`：缓存替换必须在单一事务中删除旧行、写全部新行并更新时间；失败保留可判定旧状态，不得留下半份列表。
- `PKG-API-010`：Agent推送 dpkg变化时可直接替换缓存；推送失败只记日志不终止报告流，30分钟主动刷新仍作为兜底。

## 8. Panel Agent 本机只读 CLI

- `AGT-CLI-MODE-001`：启动 `panel-agent` 必须显式选择首参数 `--srv` 或 `--cli`；无参数或未知首参数打印总用法到 stderr 并退出2，绝不能因裸命令误启动gRPC服务。
- `AGT-CLI-MODE-002`：`--srv` 是systemd使用的唯一服务模式，负责装配并运行Agent gRPC server；初始化或运行错误退出1，收到中断/SIGTERM后最多等待10秒优雅停止并正常退出，CLI命令不得装配该server。
- `AGT-CLI-MODE-003`：顶层 `-h|--help` 及CLI层 `help|-h|--help` 必须把对应帮助写stdout并退出0；缺少CLI命令、缺少apps子命令、未知命令或参数数量错误写stderr并退出2。
- `AGT-CLI-HOST-001`：`list`、`inspect`、`where` 的Docker host解析优先级固定为 `--docker-host` > `PANEL_AGENT_DOCKER_HOST` > `unix:///var/run/docker.sock`；flag可位于位置参数前后，未知flag或缺少flag值按用法错误退出2。
- `AGT-CLI-LIST-001`：`panel-agent --cli apps list` 必须只列出标签 `panel.application.managed=true` 的本机Docker容器，按规范化容器名稳定升序；无托管容器返回空表而不是错误，用户或第三方容器不得出现。
- `AGT-CLI-LIST-002`：list表格至少显示短ID、名称、应用/实例ID、镜像、state/status、端口和创建时间；ID显示截断不得改变JSON中的完整ID，输出写失败退出1。
- `AGT-CLI-LIST-003`：`apps list --json` 必须向stdout输出可解析的完整 `DockerContainer[]`，仍只含Panel托管容器且不夹杂表头/日志；额外位置参数或不支持flag退出2。
- `AGT-CLI-SEL-001`：inspect/where selector必须按“容器名（接受有无前导 `/`）> 精确实例ID > 精确应用ID”解析，并且只在已经过托管标签过滤的容器集合中匹配。
- `AGT-CLI-SEL-002`：空selector属于用法错误并退出2；未匹配或应用ID对应多个实例属于运行错误并退出1，歧义错误必须提示改用容器名或实例ID，不得任意选择一个实例。
- `AGT-CLI-INSP-001`：`apps inspect <selector>` 必须显示完整Docker详情、Panel关联标签（应用/实例ID、generation、spec hash、apply mode、managed-files hash/drift/error）及home、instance、persistent路径；路径解析失败退出1且不得伪造空路径为成功。
- `AGT-CLI-INSP-002`：`apps inspect <selector> --json` 必须输出单个可解析对象，保留完整Docker字段并增加结构化 `panel` 与 `paths`；表格和JSON必须来自同一已解析容器，不得出现选择器竞态导致对象不一致。
- `AGT-CLI-WHERE-001`：`apps where <selector>` 成功时stdout只能输出该应用home目录及换行；必须先确认容器带应用ID且目标路径存在并为目录，不存在、非目录或解析失败退出1。
- `AGT-CLI-EXIT-001`：退出码合同固定为0成功、1运行错误、2用法错误；Docker runtime创建失败、Docker不可达、列举失败、selector未找到/歧义、路径不可用或输出编码失败均为运行错误，诊断写stderr。
- `AGT-CLI-SEC-001`：CLI只能通过本机Docker runtime读取容器和计算既定应用路径，不连接Panel API、不调用Agent gRPC、不写数据库、Docker或应用目录，也不提供start/stop/restart/delete/purge等变更命令。
- `AGT-CLI-SEC-002`：CLI不得把“任意带相似应用ID的容器”视作Panel资源；托管标签过滤必须先于selector解析，未来新增子命令也必须维持只读与托管资源所有权边界。

## 9. 节点 Tailscale

本节只固定服务器侧入口与跨模块责任；容器内 tailscaled 生命周期、全局设置、地址优先规则与节点侧 Agent 语义见 [Tailscale](tailscale.md) 的 `TS-*`。

- `SRV-TS-001`：`servers` 的 `tailscale_enabled`、`tailscale_prefer_agent`、`tailscale_prefer_interconnect` 是持久化用户意图，创建、更新与详情必须携带全部三个字段，列表摘要只携带 `tailscaleEnabled`；`tailscale_enabled=false` 时两个偏好列必须被强制写为 false（含关闭启用后一并清零）；旧库由自动 ORM 增列迁移补齐、默认 0，既有服务器数据不得被删除或改写。详见 `TS-NODE-001/002`。
- `SRV-TS-002`：节点 Tailscale 观测态只由系统写入 `servers.traits` 的 `tailscale.status|ipv4|ipv6|hostname|version|last_error|updated_at`，用户提交的 traits 继续被忽略；写入必须读改写合并，不得清空 `agent.*` 等既有系统 trait；关闭启用开关必须在同一保存事务内清除这些观测键，避免互联与界面继续使用失效地址。详见 `TS-NODE-003/004/005`。
- `SRV-TS-003`：`POST /servers/{id}/tailscale/apply` 是手动重试与首次加入路径，必须创建或复用 `server_tailscale_apply` 任务、先置 running 再返回 202 与 taskId；未启用返回 `tailscale_not_enabled` 且不创建任务；保存开关成功后的自动排队失败只记录告警，不得回滚已保存的服务器。详见 `TS-TASK-001/002`。
- `SRV-TS-004`：节点侧收敛必须要求 Agent 能力 `agent.tailscale`：缺少能力时只让该任务失败并返回 `tailscale_agent_unsupported`（提示升级该服务器上的 Agent），不得触发整节点重装、不得回退 SSH，也不得影响同节点其它依赖 Agent 的能力（`AGT-STATE-004`）。节点缺少软件包时允许经 Agent 安装，仍遵守“依赖 Agent 的能力不回退 SSH”的总原则。详见 `TS-TASK-003`、`TS-AGT-002/003`。
- `SRV-TS-005`：开启 `tailscalePreferAgent`，或已上报的 tailnet 地址发生变化时，节点证书 SAN 集合必须同时覆盖规范 host 与该 tailnet 地址；集合不一致时标记该节点需要证书刷新，并复用既有 Agent 部署通道（只重写证书与配置，不重传二进制，`AGT-DEP-003`）。不得为了连通放宽证书校验、跳过主机名校验或改用明文。详见 `TS-ADDR-004`。
- `SRV-TS-006`：Panel→Agent 连接地址必须经统一解析：仅当节点启用 Tailscale、要求该偏好、已上报位于 `100.64.0.0/10` 或 `fd7a:115c:a1e0::/48` 的有效地址、且 Panel 容器自身已登录 tailnet 时，才改用 `https://<tailnet 地址>:<agent 端口>`，否则使用 `agent.url` 原值。`agent.url` 不得被改写，因此该开关不触发 `AGT-AUTO-001` 的“URL 非默认地址需要重部署”判定；健康检查、部署、上报流、指标、容器资源、软件包、存储与代理诊断必须共用同一解析。详见 `TS-ADDR-001..003`。
- `SRV-TS-007`：节点 tailnet 地址出现、变化或消失，或启用/互联偏好意图变化时，必须主动重同步互联设施（排队存储导出协调任务，并在受影响节点确实作为网关或源站参与时重新渲染入口代理设施），不参与的节点不产生多余工作。**明确限制**：UFW 只支持端口/协议/来源规则，Panel 不自动为 `tailscale0` 接口放行，操作者必须在 UFW 激活时自行允许该接口。详见 `TS-ADDR-009`、`TS-LIMIT-001`。

## 10. 验收证据

- `SRV-EVD-001`：路由清单必须覆盖 credentials CRUD、servers CRUD/probe/test/trust/restart/agent、nat-ports、tailscale apply、UFW（仅 `GET /ufw` 与规则增删）、fail2ban及packages全部路径，且与前端 typed client一致。
- `SRV-EVD-002`：测试必须证明列表不选择秘密/大字段、编辑空secret保留、主机key变化失败关闭、服务器删除事务清引用、删除时清理节点证书资产，以及删除不依赖远端可达。
- `SRV-EVD-003`：任务测试必须证明同步executor才结束任务、相同资源重复触发的复用边界、自动部署退避/封禁/手动解封和不可取消升级语义。
- `SRV-EVD-004`：Agent替身必须验证生产能力不回退SSH、版本/Docker/证书状态转换、report stream空快照保护和失败不致断流；单元测试不得依赖真实SSH、apt、UFW或Docker。
- `SRV-EVD-007`：`AGT-FW-001..006` 必须各有单测：端口集合派生（SSH/Agent/反向代理 trait）、脚本顺序（安装 → 放行 → 启用，且 SSH 先于启用）、已安装且已启用时不下发任何变更、NAT 豁免、不支持发行版拒绝、以及「前提失败时不得上传二进制」的部署顺序断言。
- `SRV-EVD-005`：Agent 下载端点必须有独立于 `/api` 路由清单的**公开路由清单断言**（literal pattern 集合 + 哈希），且断言必须验证产物名是注册 pattern 的字面量、路由不在 `/api` 之下。端点测试必须覆盖未知版本/平台、`..` 与越权路径不服务、缓存头与 `ETag`、`Range` 返回 206、未命中不设置 `Set-Cookie`。
- `SRV-EVD-006`：Agent 投递测试必须覆盖：HTTP 下载成功时不上传；连不上、HTTP 4xx/5xx 时回退 SSH 上传且上传的是 `.gz`、带传输超时；传输中断时直接失败且不上传；`agent.downloadBaseUrl` 为空时只走 SSH 上传。下载脚本测试必须断言 TLS 开关、各分类退出码与回退判定表，并且**按脚本实际参数断言 `AGT-DL-006` 的最坏耗时**（轮数 × 单轮 + 延迟之和 < Panel 侧上限），同时断言脚本**不含** `--retry`、包含续传开关 `-C -` / `-c` 与拒绝 Range 的处理分支。`AGT-DL-012` 必须单独覆盖四种情形：缺 fetcher 且安装成功 → 重试成功且不上传；安装成功但重试仍缺 fetcher → 回退；安装失败 → 回退；发行版不受支持 → 完全不调用安装且回退。fetcher 安装脚本测试必须断言 apt 参数与非交互设置，并断言其目标机侧最坏耗时严格小于 Panel 侧上限。`AGT-DEP-001` 的 `running` 冲突与 `AGT-DEP-006` 的有界等待必须各有单测。
- `SRV-EVD-007`：Tailscale 证据必须覆盖：三列意图的保存/读取与“未启用强制清空偏好”、关闭开关清除观测、旧 Agent 缺省上报不清空观测、`server_tailscale_apply` 的创建/复用与缺能力失败、节点证书 SAN 在开启/未开启偏好时的差异、地址解析四条件与互联两端条件；单元测试使用临时目录与替身控制面，不依赖真实 tailnet、真实 `tailscaled` 或宿主 `tun` 模块。详见 `TS-EVD-002`。
- `SRV-EVD-008`：CLI测试必须覆盖显式模式门禁、三条apps命令的表格/JSON、Docker host优先级、selector优先级与歧义、退出码及非托管容器隔离；测试使用本地runtime替身，不依赖真实Docker。（本项此前误用 `SRV-EVD-005` 编号，与 Agent 下载端点的公开路由清单断言重号；`SRV-EVD-005` 保持原义不变。）
