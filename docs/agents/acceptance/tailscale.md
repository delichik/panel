# Tailscale 验收规范

本文约束 Tailscale 在 Seamark 中的四段行为：Panel 容器内 `tailscaled` 的生命周期（由 `panel-init` 拥有）、全局 Tailscale 设置与只写认证密钥、每节点加入意图与观测态、以及把 tailnet 地址用于 Panel→节点和节点→节点连接的优先规则。

本域不是独立功能岛。以下既有合同同时生效，本文只补充 Tailscale 特有语义：

- 设置读写的通用校验、热更新与失败原子性见 [身份、设置与系统](identity-settings-system.md) 的 `SET-RUN-*`、`SET-TLS-*`。
- 服务器登记、Agent 状态机、能力门禁与任务语义见 [服务器、安全与软件包](servers-security-packages.md) 的 `SRV-*`、`AGT-*`。
- 存储共享与入口网关的地址、导出白名单与上游渲染见 [应用与设施应用](applications-and-facilities.md) 的 `FAC-STO-*`、`FAC-RP-*`。
- 页面操作与两阶段反馈见 [逐页面验收](ui-pages.md) 的 `UI-SET-*`、`UI-SRV-*`。
- 任务中心、运行事件与结果待核实语义见 [协调、任务与运行事件](orchestration-and-tasks.md)。

每个条目都按触发入口、前置条件、成功结果、失败结果、边界条件和验证点六项书写；只写“支持 Tailscale”不构成验收。

## 1. 全局设置与容器期望态（`TS-SET-*`）

### TS-SET-001 runtime 设置暴露 Tailscale 分组与容器实际态

- **触发入口**：认证用户调用 `GET /api/v1/settings/runtime`。
- **前置条件**：任意部署形态，包括镜像不含 `tailscale` 包或容器缺少运行前提的部署。
- **成功结果**：响应新增 `tailscale` 分组：`authKeyConfigured`（布尔）、`tags`（归一化后的字符串数组）、`container`（`available`、`running`、`loggedIn`、`hostname`、`ipv4`、`ipv6`、`version`、`backendState`、`lastError`、`updatedAt`）。容器内 tailscaled 不可管理时 `available=false`，其余字段为空值/零值。
- **失败结果**：未认证返回 401。读取容器实际态失败只降级为不可用与空字段并记录进程日志，不得让整个运行时设置接口失败；任何字段都不得回显认证密钥明文或密文。
- **边界条件**：从未保存过 Tailscale 设置时返回 `authKeyConfigured=false`、`tags=[]`；`runtime_settings` 中不存在 `tailscale.authKey`、`tailscale.tags` 行等同于未配置；进程重启后由持久值恢复；容器尚未启动 tailscaled 时 `backendState` 与 `updatedAt` 可以为空；旧版本数据库缺少这些键时按未配置处理，不得报错。
- **验证点**：`internal/modules/settings/tailscale_test.go` 断言响应 JSON 含 `tailscale` 分组且不含认证密钥；`web/src/views/settings/tailscaleSection.test.ts` 覆盖前端渲染与降级态。

### TS-SET-002 认证密钥只写语义与清除语义

- **触发入口**：`PUT /api/v1/settings/runtime` 的 `tailscale.authKey` 与 `tailscale.clearAuthKey`。
- **前置条件**：认证用户；已存在或尚不存在 `runtime_settings` 键 `tailscale.authKey`。
- **成功结果**：`authKey` 非空时先校验后替换存储值，此后任何 API 只暴露 `authKeyConfigured=true`；`authKey` 为空或省略时保留已存密钥；`clearAuthKey=true` 时删除该键、把容器期望态置为关闭（`enabled=false`），随即 `authKeyConfigured=false`。
- **失败结果**：密钥格式非法返回 `invalid_tailscale_auth_key`，且认证密钥、标签与同请求内其它字段都不得部分写入。密钥不得出现在任何 API 响应、日志、任务参数或运行事件中（`GOV-DOD-009`）。
- **边界条件**：同一请求同时给出非空 `authKey` 与 `clearAuthKey=true` 时必须有稳定且单一的最终语义，不得出现“密钥已清除但期望态仍为启用”的半状态；重复清除幂等；键本就不存在时清除返回成功；密钥只能整体替换，不提供读取接口，也不进入备份以外的任何导出面。
- **验证点**：`tailscale_test.go` 断言响应与任务参数中不含密钥、清除后 `authKeyConfigured=false` 且期望态文件为 `enabled=false`；`settingsPage.confirm.tailscale-auth-key.*` 覆盖清除前确认。

### TS-SET-003 标签归一化与非法输入拒绝

- **触发入口**：`PUT /api/v1/settings/runtime` 的 `tailscale.tags`。
- **前置条件**：认证用户；提交换行、逗号或空格分隔的标签输入，或已归一化的数组。
- **成功结果**：每项裁剪后小写化、去重、排序，写入 `runtime_settings` 键 `tailscale.tags`（换行编码）；读写响应回显归一化结果，后续节点加入使用同一集合。
- **失败结果**：任一项不符合 `tag:name`（只允许小写字母、数字和连字符，且首尾必须是字母或数字）返回 `invalid_tailscale_tag`，整套标签不写入，且本次请求的其它字段也不得写入。
- **边界条件**：空数组或空串表示明确清空；大小写不同视为同一标签只保留一份；含合法与非法混合项时整体拒绝，不得静默丢弃非法项后再保存；数量上限超限同样拒绝。
- **验证点**：`internal/platform/tailscale/tailscale_test.go` 的 `NormalizeTags`/`ValidateTag` 用例，以及 `tailscale_test.go` 的 PUT 拒绝用例；前端 `web/src/views/settings/tailscale.test.ts` 覆盖本地解析与阻断。

### TS-SET-004 设置先持久化，容器收敛失败不回流

- **触发入口**：`PUT /api/v1/settings/runtime` 成功后的容器期望态写入与收敛请求。
- **前置条件**：设置已通过校验；分别覆盖“由 panel-init 监管”和“以 `go run ./cmd/panel` 独立运行”两种形态。
- **成功结果**：设置先持久化并同步内存快照后返回；随后写 `<dataRoot>/tailscale/config.json` 并请求 panel-init 收敛。收敛失败只体现在随后读取的 `tailscale.container.lastError` 中。
- **失败结果**：期望态写入失败返回 `tailscale_config_write_failed`，已保存的设置不回滚；未经 panel-init 监管时不得把容器态报成“已收敛”。
- **边界条件**：期望态文件不存在等同于未配置；连续两次保存按最后一次提交的内容收敛；在保存与收敛之间重启进程时以持久化设置为准，不得出现数据库已改而内存未改（或反之）的状态。
- **验证点**：`tailscale_test.go` 断言保存后 `config.json` 内容、失败错误码与“不回滚已保存设置”。

### TS-SET-005 容器收敛入口 `POST /api/v1/settings/tailscale/apply`

- **触发入口**：设置页「应用 / 重连」，或直接调用 `POST /api/v1/settings/tailscale/apply`。
- **前置条件**：认证用户；Panel 由 panel-init 监管；`<dataRoot>/tailscale` 可写。
- **成功结果**：重新生成期望态文件并请求 panel-init 重新收敛，返回 202 与当前容器实际态（结构与 `GET` 的 `container` 一致）。2xx 只代表请求已受理，不代表 tailscaled 已登录 tailnet（`GOV-DOD-007`）。
- **失败结果**：Panel 未经 panel-init 监管（例如 `go run ./cmd/panel`）时返回 `tailscale_container_unavailable`，不得伪造 `container` 状态；期望态写入失败返回 `tailscale_config_write_failed`。
- **边界条件**：容器 tailscale 不可用时前端必须禁用该操作，服务端仍须显式拒绝而不是静默跳过（`GOV-TRACE-005`）；允许重复触发且每次都要真正下发；Panel 重启后入口继续可用；该接口不涉及旧版本数据迁移。
- **验证点**：`tailscale_test.go` 使用替身控制面断言 202 语义、`tailscale_container_unavailable` 与“不可管理时显式失败”。

## 2. Panel 容器内 tailscaled 生命周期（`TS-INIT-*`）

### TS-INIT-001 镜像能力与运行前提缺失时的降级

- **触发入口**：容器启动（`/app/panel-init` 作为 PID 1）。
- **前置条件**：镜像内置 Alpine `tailscale` 包；分别覆盖具备与不具备 `--cap-add=NET_ADMIN`、`--cap-add=NET_RAW`、`--device=/dev/net/tun` 与宿主 `tun` 模块的两种容器。
- **成功结果**：具备前提时以内核 TUN 模式运行 `tailscaled`；不具备前提时容器 Tailscale 整体报告为不可用（`available=false`），设置页显示“此部署无法管理容器内 tailscale”而不是静默失败或无提示的 `running`。
- **失败结果**：缺少能力、设备或内核模块时不得让容器进入重启循环，不得以用户态网络等替代方式伪装成功，也不得把失败归因于用户设置。
- **边界条件**：镜像内 `tailscale`/`tailscaled` 可执行文件不存在时等同于不可用；宿主 `tun` 模块未加载时同上；`panel-init` 的 `-tailscale`/`-tailscaled` 显式置空时（含 `PANEL_INIT_TAILSCALE_PATH`、`PANEL_INIT_TAILSCALED_PATH`）表示有意关闭容器内 Tailscale 管理，必须与“能力缺失”走同一条降级路径。
- **验证点**：`cmd/panel-init/tailscale_test.go`、`cmd/panel-init/main_test.go` 的能力探测与降级用例；部署文档的必需 flags 说明。

### TS-INIT-002 首次启动：期望态→启动→等待 socket→加入

- **触发入口**：容器首次启动，或 `<dataRoot>/tailscale/config.json` 首次由 Panel 写入 `enabled=true`。
- **前置条件**：`panel-init` 以 root 运行并持有期望态配置的读取权限；Panel 进程可写该配置。
- **成功结果**：`panel-init` 读取期望态后启动 `tailscaled`（状态文件与 LocalAPI socket 位于 `<dataRoot>/tailscale/`），等待 socket 就绪，再执行 `tailscale up --hostname=seamark-panel --accept-dns=false`；节点以固定主机名 `seamark-panel` 加入 tailnet，容器实际态随即可由 `GET /settings/runtime` 读到。
- **失败结果**：tailscaled 启动失败、socket 等待超时或加入失败时，实际态写为不可用/错误并记录 `lastError`，不得反复重启整个容器，也不得让期望态文件被改写成“成功”。
- **边界条件**：期望态为 `enabled=false` 或缺少认证密钥时不得尝试加入；已存在状态文件时按已登录处理不重复加入；写入期望态的进程与读取进程分属不同用户（Panel 非 root、`panel-init` root），目录属主必须允许 Panel 写入。
- **验证点**：`cmd/panel-init/tailscale_test.go` 覆盖读取配置、启动、socket 等待与 `up` 参数；`internal/platform/tailscale/tailscale_test.go` 覆盖配置读写与 `0640` 权限。

### TS-INIT-003 重启与节点身份存活

- **触发入口**：容器重启、Panel 子进程重启（含备份/还原维护模式往返），或期望态再次下发。
- **前置条件**：`<dataRoot>` 为持久卷，`tailscaled.state` 仍在其中。
- **成功结果**：节点身份从状态文件恢复，不重新申请节点密钥即保持在线；`tailscale up` 不重复要求认证密钥；容器实际态中的 `hostname`、`ipv4`、`ipv6` 与重启前一致。
- **失败结果**：状态文件损坏或不可读时按未登录处理并记录错误，不得静默生成新身份，也不得删除既有状态文件。
- **边界条件**：`<dataRoot>` 未挂载持久卷时必须表现为需要重新登录，而不是报成功；状态文件位于 `0700`/root 私有路径，Panel 进程只能读实际态快照而不能读状态文件本身。
- **验证点**：`cmd/panel-init/tailscale_test.go` 的重启路径用例；`tailscale.state` 位于 `<dataRoot>/tailscale/` 的路径断言。

### TS-INIT-004 关停顺序：先 panel 后 tailscaled

- **触发入口**：容器收到 SIGTERM/SIGINT（例如 `docker stop`），或 Panel 子进程退出。
- **前置条件**：作为 PID 1 的 `panel-init` 已注册信号处理；Panel 子进程与 tailscaled 均在运行。
- **成功结果**：`panel-init` 先优雅停止 Panel 子进程，再停止容器内 `tailscaled`，随后退出；不遗留孤儿进程。
- **失败结果**：任一阶段失败都必须有界完成并在日志中说明，不得因为 tailscale 停止失败而无限等待或阻止容器退出。
- **边界条件**：tailscaled 未运行、Panel 子进程已退出、重复收到信号都必须幂等；信号处理不得改变既有备份/还原重启语义。
- **验证点**：`cmd/panel-init/main_test.go` 的信号与关停顺序用例。

### TS-INIT-005 控制面与进程权限模型

- **触发入口**：Panel 侧调用 panel-init 控制面 `POST /tailscale/apply`、`GET /tailscale/status`；进程启动时校验运行身份。
- **前置条件**：控制面与既有重启入口共用同一个 loopback 监听器、随机端口、随机 token 与 `X-Panel-Init-Token` 头；`panel-init` 必须以 root 运行（uid 0），Panel 子进程降权到非 root 的 `panel` 用户（可由 `-panel-user` 或 `PANEL_INIT_PANEL_USER` 覆盖）。
- **成功结果**：Panel 能读取容器实际态并请求重新收敛；tailscaled 以 root 运行、Panel 业务进程以非 root 运行；`panel-init` 通过 `--init-tailscale-url` 把控制面地址传给 Panel 子进程。
- **失败结果**：token 不匹配的请求必须被拒绝；控制面不得暴露在对外端口、不得注册为 Panel 业务路由；Panel 子进程不得因为非 root 而失去对 `<dataRoot>/tailscale/config.json` 的写入能力。
- **边界条件（不允许降级）**：`panel-init` 不以 root 运行时必须**拒绝启动**并以非零码退出，错误文本指出原因（内核 TUN 模式与 Panel 子进程降权都需要 root）与修法（去掉 compose 的 `user:`、`docker run --user`，或改用未设置 `USER` 的镜像）；不得以非 root 身份继续运行、也不得静默把 Panel 子进程跑成 root。以 root 运行但解析不到目标用户时同样必须拒绝启动，只有显式传 `-panel-user=""` 才表示“有意让 Panel 子进程与 panel-init 同身份运行”。`-data-root` 缺省取 `$PANEL_DATA_ROOT`、再回退 `/app/data`；控制面地址未下发（例如本地开发直接运行 `cmd/panel`）时，Panel 必须表现为“不支持”而不是轮询一个不存在端口；Panel 子进程退出导致容器重启后控制面必须重新生成 token。
- **验证点**：`cmd/panel-init/privilege_test.go` 的非 root 拒绝、未解析用户拒绝与显式 `-panel-user=""` 放行用例；`cmd/panel-init/main_test.go` 的参数/环境变量解析与控制面 token 用例；`tailscale_test.go` 的“未经 panel-init 监管”分支。

### TS-INIT-006 认证密钥只经 `TS_AUTHKEY` 传递并脱敏

- **触发入口**：`panel-init` 执行 `tailscale up`；节点侧 Agent 执行加入。
- **前置条件**：期望态中包含认证密钥，或节点侧 Agent 收到 `TailscaleConfigure` 请求。
- **成功结果**：密钥仅通过 `TS_AUTHKEY` 环境变量传递；命令参数列表、任务参数以外的持久化位置、进程日志与错误输出中都不出现密钥，错误输出中的密钥被替换为固定占位符。
- **失败结果**：任何把密钥拼进命令行参数、写入 `/etc/panel-agent/*`、服务器 traits、运行事件或日志的实现都视为安全回归；脱敏失败时宁可截断输出也不得原文外泄（`GOV-DOD-009`）。
- **边界条件**：密钥为空时不得向 `up` 传递空 `TS_AUTHKEY`；节点已登录后重新下发配置不需要密钥；远端输出中同时包含其它敏感文本时仍必须完成密钥替换。
- **验证点**：`cmd/panel-init/tailscale_test.go` 断言密钥经环境变量而不是参数传递；`internal/agent/system/tailscale.go` 的 `redactSecret` 单测覆盖。

### TS-INIT-007 自愈与 Panel 就绪门

- **触发入口**：既有的 30 秒 Agent 检查后台周期（不新增 ticker、cron 或任务类型）。
- **前置条件**：Panel 运行中、存在期望态配置；容器内 tailscaled 可能已退出或尚未登录。
- **成功结果**：周期内刷新容器实际态；发现 tailscaled 不在运行或期望态为启用而实际未登录时触发一次收敛；Panel 就绪标志随实际态更新。
- **失败结果**：收敛失败只更新 `container.lastError` 与实际态，绝不终止或阻塞 Panel，也不影响同周期其它服务器检查。
- **边界条件**：Panel 自身未登录 tailnet 时，就绪门必须为假，从而任何 `tailscale.prefer*` 意图都不会把连接切到 tailnet 地址（见 `TS-ADDR-001`）；周期重复执行必须幂等，不得产生并发收敛；进程重启后由第一次周期恢复。
- **验证点**：`tailscale_test.go` 断言“tailscaled 停止后 Panel 就绪被清除”；后台周期复用既有 Agent 检查而非新 ticker 的装配断言。

## 3. 节点加入意图与观测态（`TS-NODE-*`）

### TS-NODE-001 三列意图持久化与旧库迁移

- **触发入口**：创建/更新服务器（`POST /api/v1/servers`、`PUT /api/v1/servers/{id}`）、读取列表与详情。
- **前置条件**：已存在的 alpha 数据库（无这三列）与全新数据库都要覆盖。
- **成功结果**：`servers` 新增 `tailscale_enabled`、`tailscale_prefer_agent`、`tailscale_prefer_interconnect` 三列，由自动 ORM 增列迁移补齐、默认值为 0；创建、更新、详情 DTO 全部携带三个字段；列表摘要只携带 `tailscaleEnabled`。
- **失败结果**：迁移失败不得留下部分提交的结构或数据（`GOV-DOD-005`）；列表不得因为新增字段而扩大选择范围（仍不得返回完整 traits）。
- **边界条件**：旧记录迁移后三列均为 0（未启用、不偏好）；重启后意图保持不变；三列都是持久化意图，不因观测态变化而被改写。
- **验证点**：`internal/modules/servers/store/sqlite/servers.go` 读写映射测试与 `internal/modules/servers/tailscale_report_test.go` 的往返用例。

### TS-NODE-002 未启用时两个偏好开关恒为假

- **触发入口**：创建或更新服务器时提交任意组合的三个开关。
- **前置条件**：`tailscaleEnabled=false`，同时提交 `tailscalePreferAgent=true` 或 `tailscalePreferInterconnect=true`。
- **成功结果**：保存结果中两个偏好列都被强制为 false，数据库、详情 DTO 与后续读取一致；关闭启用开关时两个偏好一并清零。
- **失败结果**：不得保存“未启用但偏好”的中间状态；不得以校验错误代替规范化，也不得静默忽略后仍保留旧偏好值。
- **边界条件**：同一请求先关后开、重复保存同一组合必须幂等；从启用改为不启用时，已有观测态同时按 `TS-NODE-004` 清除。
- **验证点**：`tailscale_report_test.go` 覆盖“启用=false 时三个字段归零”的用例；`web/src/views/servers/model.ts` 的 `tailscalePreferences` 前端用例。

### TS-NODE-003 观测态写入合并且不影响 `agent.*`

- **触发入口**：节点上报（`AgentReport.tailscale`）、收敛任务写回、系统周期刷新。
- **前置条件**：节点已启用 Tailscale；`servers.traits` 中已存在 `agent.*`、`sys.*` 等系统事实；用户提交的 traits 必须继续被忽略。
- **成功结果**：写入 `tailscale.status`（`disabled|pending|installing|running|degraded|unsupported|error`）、`tailscale.ipv4`、`tailscale.ipv6`、`tailscale.hostname`、`tailscale.version`、`tailscale.last_error`、`tailscale.updated_at`；写入按“读改写”合并，既有 `agent.*` 及其它系统 traits 保持不变。状态语义稳定：已登录为 `running`，有错误文本为 `error`，其余为 `pending`。
- **失败结果**：写入失败不得清空其它 traits，也不得把任务或服务器标成成功；非法/非 tailnet 地址不得写入地址字段。
- **边界条件**：空值表示删除该键而不是写入空串；并发上报与任务写回按最后写入合并，不得互相清空；未启用节点收到节点自行运行 tailscale 的上报时必须忽略，不生成观测态。
- **验证点**：`tailscale_report_test.go` 的合并与状态映射用例；`internal/platform/tailscale/tailscale_test.go` 覆盖 `IsTailnetAddress`/`FirstTailnetAddress`（只接受 `100.64.0.0/10` 与 `fd7a:115c:a1e0::/48`）。

### TS-NODE-004 关闭开关清除观测态

- **触发入口**：更新服务器时把 `tailscaleEnabled` 置为 false。
- **前置条件**：该节点已有 `tailscale.*` 观测态（状态、地址、主机名、错误或时间中的任意一项）。
- **成功结果**：保存在同一事务内清除这些观测键，详情与列表不再显示过期地址或状态；随后按 `TS-TASK-002` 排队一次收敛，让节点真正退出 tailnet。
- **失败结果**：不得因为收敛失败而恢复或保留已清除的观测键；不得把清除误解为“删除节点身份”（节点侧只断连，见 `TS-AGT-004`）。
- **边界条件**：重复关闭幂等；节点从未上报过观测态时不得创建空观测；清除后再次启用必须能重新收敛并重新写入观测。
- **验证点**：`tailscale_report_test.go` 的清除用例；`registry.go` 保存路径在同一事务内清除的断言。

### TS-NODE-005 旧 Agent 报告缺少 tailscale 字段不得清除观测

- **触发入口**：旧版本 panel-agent（未实现 `AgentReport.tailscale`）持续上报。
- **前置条件**：`servers.traits` 中已有 tailscale 观测态；上报消息中该字段整体缺省。
- **成功结果**：缺省字段不触发任何 tailscale 观测写入，既有观测态、地址与 `last_error` 保持不变；其它报告字段照常落库。
- **失败结果**：不得把“字段缺省”解释为 `disabled`、不得清空地址、不得因此触发互联重算或设施重新渲染。
- **边界条件**：升级到新 Agent 后字段重新出现时必须正常覆盖；新 Agent 明确报告 `installed=false` 时按未安装处理并清除地址（这是显式事实，不属于缺省）。
- **验证点**：`tailscale_report_test.go` 覆盖缺省报告保留既有观测与显式未安装清除地址的分支。

## 4. 节点收敛任务与能力门禁（`TS-TASK-*`）

### TS-TASK-001 手动 apply 创建或复用 `server_tailscale_apply` 任务

- **触发入口**：`POST /api/v1/servers/{id}/tailscale/apply`（详情页「重试 / 应用 Tailscale」）。
- **前置条件**：服务器存在且 `tailscaleEnabled=true`；可能存在进行中的同类任务。
- **成功结果**：创建或复用 `server_tailscale_apply` 任务，响应 202 与 `taskId`，任务先进入 `running` 再返回；任务执行体经 Agent 安装/加入或断连并写回观测态；任务在任务中心可见并可追踪。
- **失败结果**：`tailscaleEnabled=false` 时返回 `tailscale_not_enabled`，不得创建任务；服务器不存在返回 not-found；任务执行失败必须写失败终态并保留结构化错误。
- **边界条件**：这是手动重试与首次加入路径，重复点击允许复用而非并发创建；已有 `running` 任务时不得返回“已受理”却什么都不做（与 `AGT-DEP-001` 同类语义）；Panel 重启后仍可从任务中心重试/立即运行。
- **验证点**：`internal/modules/servers/tailscale.go` 的任务创建/复用分支测试；路由清单包含该路径（`TS-EVD-001`）。

### TS-TASK-002 保存开关自动排队收敛

- **触发入口**：创建或更新服务器保存成功后，由服务端自动排队。
- **前置条件**：保存请求改变了 `tailscaleEnabled`，或在该节点已启用时改变了偏好开关。
- **成功结果**：保存先成功落库，随后自动创建/复用 `server_tailscale_apply` 任务；用户在详情页看到状态从 `pending` 过渡到实际上报状态。
- **失败结果**：排队失败只记录告警，不影响保存结果本身；不得把保存回滚成失败，也不得让用户以为节点已加入。
- **边界条件**：节点尚未具备可用 Agent 时先标记为 `pending`，由 Agent 就绪后的状态收敛再次触发，不得在无 Agent 时反复排队失败任务；保存未改变任何 Tailscale 字段时不得产生多余任务。
- **验证点**：`registry.go` 保存路径的排队断言；`tailscale_report_test.go` 覆盖无 Agent 时的 `pending` 行为。

### TS-TASK-003 缺少 `agent.tailscale` 能力只失败该操作

- **触发入口**：`server_tailscale_apply` 任务执行时的能力探测（旧版本 Agent 或未声明能力的 Agent）。
- **前置条件**：Agent 健康但 `HealthResponse.capabilities` 不含 `agent.tailscale`。
- **成功结果**：该任务失败，错误码为 `tailscale_agent_unsupported`，提示升级该服务器上的 Agent；观测态写为 `error` 并保留原因；其它依赖 Agent 的能力（UFW、软件包、Docker 资源、应用运行时）不受影响。
- **失败结果**：不得因此触发整节点 Agent 重装（`AGT-STATE-004`），不得回退 SSH 执行安装，也不得把任务标成成功。
- **边界条件**：能力缺失与 Agent 不可达/不兼容必须给出不同错误；升级 Agent 后可经手动 apply 重试；同一节点重复失败不得累积成 `undeployable`。
- **验证点**：`tailscale.go` 中 `requireTailscaleCapability` 的失败分支测试；`AGT-STATE-004` 的非回归用例。

### TS-TASK-004 加入失败与观测态一致性

- **触发入口**：节点执行 `TailscaleConfigure` 后仍未登录（例如密钥被撤销、需要管理员批准）。
- **前置条件**：节点已安装 tailscale、Agent 具备能力、全局配置存在。
- **成功结果**：任务失败并返回 `tailscale_join_failed`（消息取节点上报的原因或稳定兜底文本）；观测态保留节点实际上报的地址（若已获得），`last_error` 记录原因。
- **失败结果**：不得把“已安装但未登录”报告为 `running`；不得在失败时删除已安装软件包或节点身份。
- **边界条件**：任务失败后地址字段为空的节点不得参与地址优先替换（见 `TS-ADDR-*`）；重试成功必须把状态改回 `running` 并清除 `last_error`；旧版本数据库中不存在观测键时按未上报处理。
- **验证点**：`tailscale.go` 的 `LoggedIn=false` 分支测试；`tailscale_report_test.go` 的地址与错误映射用例。

## 5. Agent 契约与节点侧语义（`TS-AGT-*`）

### TS-AGT-001 新增 RPC、上报字段与契约 hash 兼容性

- **触发入口**：Panel 侧 Agent client 调用 `TailscaleStatus` / `TailscaleConfigure` / `TailscaleDisable`；Agent 侧在报告流中携带 `AgentReport.tailscale`。
- **前置条件**：Panel 与 Agent 构建同一源码；节点上可能运行旧版本 Agent。
- **成功结果**：新 RPC 与可选上报字段在 protobuf 源中定义，生成代码与根目录兼容副本一致；Agent 在能力列表中声明 `agent.tailscale`；契约 hash 随本次变更更新，构建期由 `ENG-GEN-001` 的流程生成与校验。
- **失败结果**：旧 Agent 不得因为未知字段或未知 RPC 而崩溃；Panel 不得向缺少该能力的 Agent 发送会破坏节点的写操作（`ENG-GEN-003`、`ENG-GEN-004`）。
- **边界条件**：字段缺省按 `TS-NODE-005` 处理；契约 hash 不匹配本身不作为通用兼容门槛（`AGT-STATE-004`），只让具体操作失败；Agent bundle 两架构行为一致。
- **验证点**：`internal/agent/contract` 与 `pb` 生成一致性测试、contract hash 守卫；`internal/agent/system/tailscale.go` 单测。

### TS-AGT-002 安装策略：发行版仓库优先，不把远程脚本管道进 shell

- **触发入口**：`TailscaleConfigure` 在节点上发现未安装 `tailscale` 时。
- **前置条件**：目标节点为受支持发行版（Debian/Ubuntu）；可能需要先添加供应商软件源与密钥环。
- **成功结果**：优先直接安装发行版仓库中的 `tailscale` 包；发行版仓库不可用时才添加供应商标记的 keyring 与 apt 源后安装。安装使用固定非交互 apt 参数与超时，事务不与 RPC 请求上下文共享取消信号以避免 dpkg 半升级。
- **失败结果**：安装失败时任务失败并保留可诊断错误；不得使用 `curl … | sh` 之类的远程脚本管道方式，也不得在失败后回退到 SSH 安装。
- **边界条件**：不受支持发行版必须明确报告 `unsupported` 而不是尝试安装；重复调用幂等（已安装时不重复下载源与密钥）；网络不可达与包管理器失败的诊断文案必须区分。
- **验证点**：`internal/agent/system/tailscale.go` 的安装分支单测（含“不执行远程脚本”的断言）；`tailscaleDiagnostic` 的错误分类用例。

### TS-AGT-003 启用与加入语义：systemd 托管，仅在未登录时使用密钥

- **触发入口**：`TailscaleConfigure`（Panel 已启用该节点）。
- **前置条件**：软件包已安装；节点可能已登录 tailnet。
- **成功结果**：通过 systemd 启用并启动 `tailscaled`；节点尚未登录时才使用 `TS_AUTHKEY`（经环境变量）执行加入，并带上全局 ACL 标签；等待后端进入 Running（有界等待），随后返回本机 tailnet 地址、主机名与版本。
- **失败结果**：服务无法启用、加入失败或等待超时必须返回结构化失败；不得把“已安装但未登录”报告为成功，也不得在失败时删除状态文件。
- **边界条件**：已登录节点不携带密钥也能成功；需要管理员批准的状态必须返回稳定原因而不是超时泛化错误；重复启用幂等；无 systemd 的环境按不支持处理。
- **验证点**：`internal/agent/system/tailscale.go` 的登录等待与幂等用例；Panel 侧 `TS-TASK-004` 联合验证。

### TS-AGT-004 “关闭”是断连而非删除

- **触发入口**：`TailscaleDisable`（Panel 关闭节点开关或清除全局密钥后收敛）。
- **前置条件**：节点已安装并可能已登录 tailnet。
- **成功结果**：节点断开 tailnet 连接，保留已安装软件包与节点身份（状态文件不删除），因此再次启用无需重新申请节点密钥；返回实际态。
- **失败结果**：失败必须显式返回并写入观测态；不得用“删除软件包/清空状态”的方式实现关闭（与删除节点的语义必须可区分，`GOV-DOD-010`）。
- **边界条件**：未安装或未登录时关闭是幂等的成功；关闭后上报的地址必须从观测态消失，从而不再参与地址优先替换；再次启用按 `TS-AGT-003` 走加入路径。
- **验证点**：`internal/agent/system/tailscale.go` 的关闭用例；`TS-NODE-004` 与 `TS-ADDR-*` 的联合断言。

### TS-AGT-005 节点侧秘密处理与上报范围

- **触发入口**：节点执行安装、加入、状态读取与周期上报。
- **前置条件**：全局认证密钥与标签可能通过请求下发。
- **成功结果**：密钥只经环境变量传给 `tailscale up`；节点不把密钥写入本地配置文件、systemd 单元、服务器 traits 或上报内容；上报内容仅包含本机事实（是否安装、是否登录、地址、主机名、版本、后端状态、脱敏错误）。
- **失败结果**：任何把密钥写入目标机持久配置或日志的实现都视为回归；错误文本中出现密钥时必须脱敏后再返回或记录。
- **边界条件**：密钥为空时不得传递空环境变量；节点上报的错误文本可能包含路径等自由文本，Panel 只做脱敏存储与展示，不解析为状态；密钥轮换不影响已登录节点。
- **验证点**：`internal/agent/system/tailscale.go` 的 `redactSecret` 与上报字段用例；`GOV-DOD-009` 的秘密不外泄检查。

## 6. tailnet 地址优先规则（`TS-ADDR-*`）

### TS-ADDR-001 Panel→节点连接改用 tailnet 地址的四个前提

- **触发入口**：任意 Panel→Agent 调用（健康检查、部署、上报流、指标、容器资源、软件包、存储、代理日志诊断）。
- **前置条件**：节点 `tailscaleEnabled=true`、节点要求 `tailscalePreferAgent`、节点已上报位于 `100.64.0.0/10` 或 `fd7a:115c:a1e0::/48` 的地址、Panel 容器自身已登录 tailnet。
- **成功结果**：四个前提同时满足时，Panel 使用 `https://<tailnet 地址>:<agent 端口>` 连接该节点；解析保留原 URL 的 scheme 与端口，只替换主机部分。
- **失败结果**：任一前提不满足（未启用、未偏好、地址缺失或不在 tailnet 网段、Panel 未登录）必须原样使用规范 `agent.url`；不得因为开关把可达节点变成不可达。
- **边界条件**：同时有 IPv4/IPv6 时 IPv4 优先；地址字段为空串或非 tailnet 地址（如 `192.168.*`、公网地址）一律回落到规范地址；Panel 就绪状态在 tailscaled 停止后必须立即失效；重启后按持久意图与最新观测重新判定。
- **验证点**：`internal/agent/endpoint/endpoint_test.go` 的 `TestAgentURLPrefersTailnetAddressAndKeepsPort` 及四条件表驱动用例。

### TS-ADDR-002 `agent.url` 不被改写，不触发重部署

- **触发入口**：保存偏好开关、观测地址出现或变化。
- **前置条件**：节点已配置规范 `agent.url`。
- **成功结果**：`agent.url` trait 保持规范地址不变；地址优先只是运行时解析结果，因此不产生 `AGT-AUTO-001` 中“URL 非默认地址需要重部署”的判定。
- **失败结果**：不得把 tailnet 地址写入 `agent.url`，也不得因为开关更新而清除节点证书指纹或标记 `incompatible`。
- **边界条件**：用户修改服务器 `host` 时的既有重部署规则仍照常生效（与开关无关）；关闭开关后立即回到规范地址；观测地址变化只影响解析结果。
- **验证点**：`endpoint_test.go` 断言 `agent.url` trait 未被改写；`AGT-AUTO-001` 的非回归用例。

### TS-ADDR-003 所有 Panel→Agent 路径共用同一解析器

- **触发入口**：健康检查与 Agent 检查周期、Agent 部署、报告流、指标采集、容器/镜像/网络/卷资源、软件包维护、存储共享 Agent 调用、反向代理按需日志诊断。
- **前置条件**：节点满足 `TS-ADDR-001` 的四个前提。
- **成功结果**：上述路径全部使用同一解析结果；不存在“部分路径用 tailnet 地址、部分用规范地址”的分裂行为。
- **失败结果**：任何新增的 Panel→Agent 路径若绕过解析器直接拼接规范地址，即视为回归；诊断/部署/上报流不得各自实现一套判定。
- **边界条件**：解析器在地址缺失时返回规范地址而不返回空串（除非 `agent.url` 本身为空，此时按“未配置 Agent”处理）；解析是纯函数，不写数据库、不触发任务。
- **验证点**：仓库内 `agentendpoint.AgentURL` 调用点的覆盖检查；`endpoint_test.go` 的纯函数用例。

### TS-ADDR-004 证书 SAN 后果与证书刷新

- **触发入口**：打开偏好开关，或观测到的 tailnet 地址发生变化。
- **前置条件**：gRPC 客户端按实际拨号主机校验对端证书。
- **成功结果**：节点证书必须包含 tailnet 地址；系统发现证书 SAN 集合与期望集合不一致时，把节点标记为需要刷新证书，并复用既有 Agent 部署通道（仅重写证书与配置、不重传二进制，`AGT-DEP-003`）。
- **失败结果**：不得因为缺少 SAN 而放宽证书校验、跳过主机名校验或改用明文；证书刷新失败只保留 `incompatible` 与错误，不得静默回落到规范地址之外的未校验连接。
- **边界条件**：未开启 `tailscalePreferAgent` 时 SAN 集合不包含 tailnet 地址；地址未变化时不得反复触发刷新；关闭开关后已签发的 SAN 集合变化同样需要一次刷新；部署通道本身失败按 `AGT-AUTO-*` 的退避语义处理。
- **验证点**：`internal/modules/servers/tailscale_report_test.go` 的 `TestAgentCertificateIncludesTailnetAddressWhenPreferred` 与 `TestAgentCertificateSkipsTailnetAddressWithoutPreference`。

### TS-ADDR-005 节点互联改用 tailnet 地址的条件

- **触发入口**：节点→节点链接的地址计算（存储共享 NFS 挂载源、导出白名单、入口网关 → 源站上游）。
- **前置条件**：链接两端分别有启用意图、偏好意图与已上报的 tailnet 地址。
- **成功结果**：仅当两端都启用 Tailscale、两端都有有效 tailnet 地址、且至少一端要求 `tailscalePreferInterconnect` 时使用对端 tailnet 地址；否则保持规范地址。
- **失败结果**：本端没有 tailnet 地址时不得只替换对端地址（对端无法把本端识别为 tailnet 来源，白名单与 ACL 都会拒绝），必须整体回落规范地址。
- **边界条件**：本端与对端是同一节点时不替换；地址为空串或非 tailnet 网段时不替换；开关关闭后立即回落；重启后按持久意图重新计算。
- **验证点**：`internal/agent/endpoint/endpoint_test.go` 的 `TestInterconnectHostRequiresBothEnds`；`internal/modules/facilityapps/interconnect_test.go`。

### TS-ADDR-006 存储共享 NFS 挂载源

- **触发入口**：为应用渲染 runtime spec 时解析 `storage_share` 挂载，或存储共享设施保存/协调。
- **前置条件**：存在存储服务器与应用节点，两者 Tailscale 意图与地址满足 `TS-ADDR-005`。
- **成功结果**：满足条件时 NFS 挂载 source 使用存储服务器的 tailnet 地址；不满足时继续使用规范地址。挂载地址属于实例期望 spec 的一部分，地址变化会触发重建。
- **失败结果**：不得在只有一端满足条件时使用 tailnet 地址，也不得把地址写死在应用定义中。
- **边界条件**：未配置存储共享的应用不受影响（`FAC-STO-005`）；多存储服务器按各自地址分别判定；重启与再次协调必须得到相同结果。
- **验证点**：`interconnect_test.go` 的 `TestStorageMountUsesTailnetAddressWhenEndsQualify`。

### TS-ADDR-007 NFS 导出白名单登记 tailnet 地址

- **触发入口**：存储节点生成/刷新 NFS export 白名单。
- **前置条件**：存在已启用 Tailscale 且已上报地址的节点。
- **成功结果**：白名单除规范地址外还登记这些节点的 tailnet 地址，使来自 tailnet 来源的挂载不被拒绝；白名单随服务器增删与地址变化刷新。
- **失败结果**：不得因为白名单缺少 tailnet 地址而让 `TS-ADDR-006` 生效后的挂载被拒绝；也不得把未启用节点的地址写入白名单。
- **边界条件**：地址未上报时不得写入空项；节点关闭 Tailscale 后必须从白名单移除；导出配置写入失败仍按 `FAC-STO-004` 的原子回滚语义处理。
- **验证点**：`interconnect_test.go` 的 `TestStorageExportWhitelistIncludesTailnetAddresses`；`FAC-STO-003` 的白名单刷新用例。

### TS-ADDR-008 入口网关 → 源站上游

- **触发入口**：设施应用渲染每节点 Nginx runtime spec 时计算 upstream 地址。
- **前置条件**：网关节点与源站节点满足 `TS-ADDR-005`。
- **成功结果**：满足条件时上游使用源站的 tailnet 地址；否则保持规范地址，渲染结果与既有 reload/recreate 判定一致。
- **失败结果**：不得只在网关一侧满足条件时改写上游，也不得把 tailnet 地址用于 DNS 记录或对外发布地址。
- **边界条件**：源站不属于该域名的 origin 时不受影响；地址变化后按 `TS-ADDR-009` 重新渲染。
- **验证点**：`interconnect_test.go` 的 `TestReverseProxyUpstreamUsesTailnetAddressOnlyWhenBothQualify`；`FAC-RP-008`、`FAC-RP-009` 非回归用例。

### TS-ADDR-009 地址或意图变化时主动重同步

- **触发入口**：节点 tailnet 地址出现、变化或消失；`tailscaleEnabled` 或 `tailscalePreferInterconnect` 意图变化；节点上报“未安装”导致地址失效。
- **前置条件**：存在存储共享设施与/或入口网关设施；受影响节点可能实际参与其中。
- **成功结果**：主动触发重同步：排队存储导出协调任务，并在受影响节点确实作为网关或源站参与时重新渲染入口代理设施；不参与任何设施的节点不产生多余工作。
- **失败结果**：重同步失败必须可诊断（任务状态/`lastError`），不得回滚用户的保存，也不得谎报设施已按新地址收敛。
- **边界条件**：无设施配置时重同步为 no-op；输入服务器集合为空时不做任何事；短时间内多次变化允许合并，但最终状态必须与最后一次观测一致；重启后由后续观测或周期检查恢复。
- **验证点**：`internal/modules/facilityapps/interconnect_sync_test.go` 的 `TestSyncInterconnectServersResyncsStorageExports` 与“无关节点被跳过”用例。

### TS-ADDR-010 非回归：DNS 与模板变量仍使用公网/规范地址

- **触发入口**：入口代理 DNS 记录同步、应用模板变量 `server.host`/`server.ssh_host` 渲染。
- **前置条件**：节点满足 tailnet 地址优先的全部条件。
- **成功结果**：DNS 记录与模板变量继续使用规范（公网/LAN）地址；tailnet 地址不得出现在公开 DNS 记录或变量渲染结果中。
- **失败结果**：把 tailnet 地址写入 DNS 或模板变量视为回归；这类地址对外不可路由，会把服务暴露面与可访问性同时破坏。
- **边界条件**：`agent.url` 本身仍保持规范地址（`TS-ADDR-002`）；SSH 连接地址不改写；关闭开关后这些面必须与引入 Tailscale 之前完全一致。
- **验证点**：DNS 同步的既有测试（`FAC-RP-011`）在 Tailscale 开启状态下继续通过；模板变量用例断言规范地址。

## 7. 明确限制与验证证据

### TS-LIMIT-001 UFW 不自动管理 `tailscale0` 接口

- **触发入口**：节点启用 Tailscale，且该节点 UFW 处于 active。
- **前置条件**：UFW 功能只支持端口/协议/来源三元组规则，不提供按接口放行能力。
- **成功结果**：Panel 明确不为其创建 `tailscale0` 接口规则；文档与设置/服务器页面必须说明操作者需要在 UFW 激活时自行放行 tailscale 接口，否则 tailnet 流量会被拒绝。
- **失败结果**：不得把“已安装/已启用 UFW”描述为“tailnet 已放行”，不得静默假设接口规则已存在，也不得为了让 tailnet 连通而放宽 UFW 的既有安全门（管理端口保护、双通道验证）。
- **边界条件**：UFW 未安装或未启用时该限制不适用；应用端口规则与存储服务器 2049 规则仍按既有语义协调，不因 Tailscale 变化。注意 `AGT-FW-001` 已让 Agent 部署自动接管并启用 UFW，因此这一限制的作用面变大了：新加入的节点在首次部署后即可能处于 active 状态，操作者需要在加入 tailnet 前自行放行 `tailscale0`。
- **验证点**：文档评审（`docs/deployment.md`、`servers-security-packages.md` 的 UFW 条目引用本项）与人工观察点；自动化测试只需断言“不产生接口规则”。

### TS-EVD-001 路由清单与前端契约一致性

- **触发入口**：`internal/bootstrap/panel/routes_manifest_test.go` 与 [主 Panel API 路由逐项清单](api-route-inventory.md)。
- **前置条件**：本次新增两条路由：`POST /api/v1/settings/tailscale/apply`、`POST /api/v1/servers/{id}/tailscale/apply`。
- **成功结果**：清单计数为 175，SHA-256 为 `f57a6287ff235634949f6f5fd63a3455bfaaa088ce5ea08562bfeb7a886eda44`（上一轮新增两条 Tailscale 路由，本轮删除了两条手动防火墙路由，`AGT-FW-001..006`）；两条路由在清单中分别映射到本文档与相应领域文档；前端 typed client、类型与 Mock 同步。
- **失败结果**：不得只更新哈希或计数让测试通过；新增路由缺少验收映射时必须先补合同（`API-COV-001`、`API-COV-002`）。
- **边界条件**：`GET /api/v1/settings/runtime` 与 `PUT /api/v1/settings/runtime` 的分组扩展不改变 method/path 数量；`POST /api/v1/servers/{id}/tailscale/apply` 与既有 `agent/*` 子资源互不影响。
- **验证点**：路由清单测试、`api-route-inventory.md`、`coverage-matrix.md` 三者同步。

### TS-EVD-002 测试与人工观察证据

- **触发入口**：改动 Tailscale 相关代码时的最小证据集合。
- **前置条件**：无真实 tailnet、无真实 tailscaled 的单元测试环境。
- **成功结果**：至少覆盖：设置只写密钥与标签归一化（`internal/modules/settings/tailscale_test.go`）、容器期望态读写与网段判定（`internal/platform/tailscale/tailscale_test.go`）、panel-init 生命周期与控制面（`cmd/panel-init/tailscale_test.go`、`main_test.go`）、节点观测合并与关闭清除（`internal/modules/servers/tailscale_report_test.go`）、地址解析四条件与互联条件（`internal/agent/endpoint/endpoint_test.go`）、设施互联与重同步（`internal/modules/facilityapps/interconnect_test.go`、`interconnect_sync_test.go`）、前端设置分区与表单（`web/src/views/settings/tailscale.test.ts`、`tailscaleSection.test.ts`）。
- **失败结果**：缺少任一关键分支的自动测试时，必须在评审说明中写明人工观察点与无法自动化的原因（`GOV-TRACE-002`）。
- **边界条件**：容器内 tailscaled 与真实 tailnet 行为无法在单元测试中复现，属于人工观察点；测试不得依赖真实 `tailscale` 可执行文件、真实 apt 或真实网络。
- **验证点**：按影响范围执行 `task test:backend`、`task test:web` 或两侧。
