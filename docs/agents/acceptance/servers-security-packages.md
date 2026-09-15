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
- `SRV-SAVE-005`：创建记录成功且 SSH executor 可用时必须创建并立即启动 `server_info_collect` bootstrap 任务，响应携带 `initialTaskId`；任务创建失败必须删除刚建记录。
- `SRV-SAVE-006`：首次 bootstrap 只经 SSH探测发行版、结构化架构和非交互特权；架构成功落库前失败必须把任务置失败并回滚新服务器；之后 Agent 部署或完整信息刷新失败不得删除服务器。
- `SRV-SAVE-007`：更新必须先保存资源；随后的连通性探测失败只把节点标记不可达并记录错误，不得回滚更新或阻断 DNS 同步触发。
- `SRV-SAVE-008`：更新改变连接 host 且已配置 Agent 时，必须更新默认 `https://host:9786` endpoint、标记 incompatible、清除节点证书指纹/有效期并要求重部署。
- `SRV-SAVE-009`：保存 IP 变化或删除服务器时必须异步触发引用该服务器的入口代理 DNS 同步；同步失败不回滚本地保存，且必须可由任务状态诊断。
- `SRV-DEL-001`：删除服务器是纯本地控制面操作，不连接目标机；目标机失联不得阻止删除。
- `SRV-DEL-002`：删除必须取消该服务器 queued、scheduled、failed_retryable 和可取消的 running 任务；已取消任务不得被迟到 worker 覆盖终态，正在执行的不可取消软件包升级不得被取消。
- `SRV-DEL-003`：同一 AppDB 事务内必须删除服务器、修剪应用 deployment server IDs、递增受影响应用 version并更新时间、移除概览卡片 serverIds；外键级联负责包/镜像缓存、实例和协调状态。
- `SRV-DEL-004`：AppDB 删除完成后必须清理 MetricsDB 的服务器指标；任一步失败必须返回错误，不得假称完整删除成功；再次删除不存在 ID 返回 404。

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
- `AGT-AUTO-001`：无 URL、URL 非默认地址、incompatible、版本不一致、证书过期/未生效/7天内到期或证书时间握手错误时，系统检查必须创建或复用 `server_agent_deploy`；普通 unavailable、连接拒绝、服务器失联或 Docker失败不得触发重装。
- `AGT-AUTO-002`：证书进入 7 天窗口时保持 compatible且不写错误，只静默刷新；系统自动复用任务必须尊重 `next_run_at` 和指数退避，不能借复用绕过。
- `AGT-AUTO-003`：同一服务器系统自动部署连续失败 2 次必须置 undeployable并停止周期自动尝试；手动部署解除阻止并重置退避基准；失败计数仅在连续 5 次健康检查成功后清零。
- `AGT-DEP-001`：手动 `POST /servers/{id}/agent/deploy` 必须创建或复用并启动任务，先标记 running再响应；任务 executor必须直到安装、健康检查和终态写入完成才返回。
- `AGT-DEP-002`：部署必须按持久化的 `architecture.os/arch` 选择固定 `/app/panel-agents/linux-amd64|linux-arm64/panel-agent`；缺架构时先 SSH探测并写回，不支持平台返回 `agent_binary_unavailable`。
- `AGT-DEP-003`：完整安装上传二进制并写证书/env/systemd；仅证书续期或 URL修复时只重写配置并重启，不重复传输二进制。
- `AGT-DEP-004`：重启 Agent 前若能力包含 `prepare-restart`，必须最长等待10分钟至 ready；holdon 首次立即记录且最多每15秒记录一次进度；能力缺失、健康/流失败或超时可记录后继续，任务取消必须停止部署。
- `AGT-DEP-005`：远端部署必须停止 systemd和残留进程，写入 `/etc/panel-agent`、`--srv` service及端口9786，验证文件证书和实际服务证书指纹，失败日志包含 systemctl/journal诊断但不得包含私钥。
- `AGT-CERT-001`：`POST /servers/{id}/agent/certificate` 只作为高级手动安装兜底返回 CA、节点证书、私钥、监听地址、Agent URL 和 Docker host，不落库；响应必须受认证且不得进入日志或缓存。
- `AGT-CERT-002`：系统证书列表只展示已有元数据的 Agent CA、Panel client、节点 server证书和 Panel TLS链；系统资产不可通过普通 key asset API下载、导出、删除、重签或作为应用文件。
- `AGT-CERT-003`：重置 Panel Agent client 保留 CA并热加载共享 gRPC client；重置 Agent CA同时生成 client并为全部已配置服务器排队重部署；重置单节点证书复用节点部署任务。
- `AGT-RPT-001`：Panel 必须主动拨号打开 mTLS report stream，节点不得保存 Panel callback地址；stream状态仅写 `agent.report.*`，不得降级普通 Agent status。
- `AGT-RPT-002`：流断开重连采用连续失败 5秒起至5分钟封顶退避；一旦该连接成功交付报告即重置退避，等待退避的连接不得被静默检测循环反复取消。
- `AGT-RPT-003`：指标、容器、镜像或软件包报告的落库失败只记录日志，不中断流；容器保存失败不得触发应用 reconcile，空容器报告不得清空已有观察。
- `AGT-RPT-004`：周期样本时间必须 Unix interval对齐；Docker事件触发的 `container_change` 可不对齐。缓存未齐或任一指标超过15秒未成功采样时不提交指标，Panel保留旧值。

## 5. UFW

- `UFW-API-001`：UFW读取和写入前必须确认服务器存在、支持发行版、可达且适配器支持UFW；无兼容 Agent 时还要求 root或免密sudo，兼容 Agent存在时按 Agent准入。
- `UFW-API-002`：`GET /servers/{id}/ufw` 必须返回 supported、installed、active、status、default policy及带稳定序号的 rules；启用 Agent 后只经 Agent读取，不得回退SSH。
- `UFW-API-003`：添加规则只接受 1..65535端口、`tcp|udp|any` 协议和合法来源；必须确认UFW已安装，成功后返回重新读取的状态。
- `UFW-API-004`：删除规则要求正整数规则号，成功后返回新状态；不存在/远端失败不得在UI本地假删。
- `UFW-API-005`：UFW安装和启用必须创建独立任务并在响应前置 running；重复触发复用进行中任务，不得并发安装或启用。
- `UFW-API-006`：安装流程必须在开启防火墙前保留 SSH端口，并按派生 reverse-proxy标记放行入口端口；任务失败保留真实失败终态，前端不能仅凭提交成功显示已启用。

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

## 9. 验收证据

- `SRV-EVD-001`：路由清单必须覆盖 credentials CRUD、servers CRUD/probe/test/trust/restart/agent、UFW、fail2ban及packages全部路径，且与前端 typed client一致。
- `SRV-EVD-002`：测试必须证明列表不选择秘密/大字段、编辑空secret保留、主机key变化失败关闭、服务器删除事务清引用，以及删除不依赖远端可达。
- `SRV-EVD-003`：任务测试必须证明同步executor才结束任务、相同资源重复触发的复用边界、自动部署退避/封禁/手动解封和不可取消升级语义。
- `SRV-EVD-004`：Agent替身必须验证生产能力不回退SSH、版本/Docker/证书状态转换、report stream空快照保护和失败不致断流；单元测试不得依赖真实SSH、apt、UFW或Docker。
- `SRV-EVD-005`：CLI测试必须覆盖显式模式门禁、三条apps命令的表格/JSON、Docker host优先级、selector优先级与歧义、退出码及非托管容器隔离；测试使用本地runtime替身，不依赖真实Docker。
