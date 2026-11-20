# Seamark 项目设计与验收规范

本目录是 Seamark 后续功能变更的规范性行为合同，用于替代按代码入口讲解实现方式的旧功能模块指引。它面向开发、评审和验收，不是用户操作手册，也不规定内部实现必须采用某个函数或文件结构。

## 1. 规范效力

- 文档中的“必须”“不得”“仅可”是强制验收条件；“应”表示没有充分理由不得偏离；“可”表示允许但不要求。
- 代码、测试与本规范不一致时，不得默认为代码正确。先确认产品意图：若行为错误则修改代码；若需求已合法变化则在同一改动中更新本规范。
- 每个带编号的条目都是独立可引用的验收项。编号一旦发布不得重用；删除能力时保留编号并标注“已废弃”及替代项。
- 示例不能削弱正文约束。未写明的边界按同类功能的全局契约执行。
- 本目录只描述可观察行为、数据与安全不变量、跨模块责任和验证结果；代码位置只作为追踪线索，不作为设计本身。

## 2. 验收项写法

每个新增或修改的功能必须在对应文档中至少明确：

1. 触发入口：页面操作、API、后台触发器、启动恢复或远端报告。
2. 前置条件：身份、权限、资源状态、依赖能力和输入限制。
3. 成功结果：响应、界面反馈、持久化变化、远端副作用和事件记录。
4. 失败结果：错误码/提示、是否允许重试、已产生副作用如何处理。
5. 边界条件：空数据、重复提交、并发、超时、刷新、重启和旧版本数据。
6. 验证点：能用自动测试、状态查询、任务/事件记录或构建产物证明的结果。

不得只写“支持新增/删除/编辑”或“调用某服务完成操作”。此类描述无法约束错误改动。

## 3. 全局完成定义

一次功能改动只有同时满足以下条件才可验收：

- `GOV-DOD-001`：改动涉及的所有既有验收项仍成立；有意改变的行为已在同一改动中更新条目及影响矩阵。
- `GOV-DOD-002`：新增页面、API、后台任务、配置、表或重要字段都有稳定验收编号，并在本索引中可定位。
- `GOV-DOD-003`：前后端共享的字段、枚举、分页、时间、错误语义一致；不得让前端通过猜测字符串弥补后端契约缺失。
- `GOV-DOD-004`：用户可见文案符合 `docs/agents/i18n-guide.md`，英文和简体中文状态按翻译状态文档同步。
- `GOV-DOD-005`：数据库变更具有从现存 alpha 数据升级的迁移路径；升级失败不得留下部分提交的结构或数据。
- `GOV-DOD-006`：远端写操作、删除、覆盖、密钥导出、恢复和证书替换等高风险动作具有显式确认、范围约束或服务端安全门。
- `GOV-DOD-007`：异步操作可追踪到任务、运行事件或协调 Job；成功提示不得早于服务端确认已接受并持久化操作。
- `GOV-DOD-008`：按影响范围使用根目录 `Taskfile.yml` 中的 `task test:*` 或 `task build:*` 验证；纯文档改动不运行测试或编译。
- `GOV-DOD-009`：新增失败路径不会泄漏密码、令牌、私钥、完整敏感环境变量、证书私钥或上传文件内容。
- `GOV-DOD-010`：删除/禁用/取消语义彼此区分；界面、API、后台处理和持久化结果不得混用。

## 4. 规范导航

| 改动范围 | 必读验收规范 |
| --- | --- |
| 全部改动、跨模块行为 | 本文、[架构、数据与 API](architecture-data-api.md) |
| 产品范围、端到端用户旅程、全局非目标 | [产品范围与端到端验收](product-scope.md) |
| 全局导航、慢网路由反馈、会话、布局、通用交互 | [界面外壳与交互约定](ui-shell-and-conventions.md) |
| 任一具体页面、页面首次加载或用户操作 | [逐页面验收](ui-pages.md) |
| 应用、挂载来源、应用文件、修订、部署、节点工作区布局、设施、存储共享、网络探测设施 `FAC-PRB-001..010`、指标设施 `FAC-MET-001..008`；规划失败 `APP-PLAN-001`、系统核实中 `APP-RUN-004` | [应用与设施应用](applications-and-facilities.md) |
| desired/observed、协调 Job、统一追加活动日志、任务、运行事件；规划失败隔离 `ORCH-PLAN-006`、启动稳定观察 `ORCH-CTRL-007`；RunEngine Phase A `ENG-RUN-001..005`、`ENG-RET-001`、`TASK-DISPATCH-001..003` | [协调、任务与运行事件](orchestration-and-tasks.md) |
| 容器、镜像、网络、卷、资源缓存与操作队列 | [容器与资源](containers-and-resources.md) |
| 登录、账号、运行时设置（分区写入 `SET-RUN-022`；指标保留/采集间隔/清理计划已移出，见 `SET-RUN-021`）、概览、系统版本 | [身份、设置与系统](identity-settings-system.md) |
| 凭据、服务器、Agent、SSH、UFW、Fail2ban、APT；Agent report 补报 `AGT-RPT-007`、网络探测上报 `AGT-RPT-008` | [服务器、安全与软件包](servers-security-packages.md) |
| DNS、ACME、自签名证书、密钥资产 | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 备份、恢复、维护模式、调试与诊断；清理数据进度/恢复/写入暂停 `DIAG-CLR-001..008`、`UI-DBG-007/008`；指标磁盘当前值/磁盘 IO `OBS-MET-007/008`、指标补报 `OBS-MET-009`、概览卡片数据 `OBS-OVW-005..007` | [备份、恢复与诊断](backup-restore-diagnostics.md)、[逐页面验收](ui-pages.md) |
| ORM、数据库模型、迁移与兼容 | [架构、数据与 API](architecture-data-api.md) |
| 测试入口、构建、容器和发布；Panel 进程日志 `ENG-LOG-001..009` 与产品读 API `ENG-EVT-001`、清空 `ENG-EVT-002`、事件通知长轮询 `ENG-EVT-011`（`/api/v1/events*`、`/api/v1/logs`、`/api/v1/notifications`；legacy `/api/v1/log-contexts*`） | [工程质量与发布](engineering-release.md) |
| 检查是否遗漏路由、API、表、后台行为或产物 | [项目覆盖基线](coverage-matrix.md) |
| 逐项核对全部主 Panel HTTP method/path | [API 路由逐项清单](api-route-inventory.md) |
| 核对当前实现尚未达到的合同，或修改触及实现/合同不一致 | [已知实现与验收差距](known-gaps.md) |

## 5. 变更影响矩阵

例行周期任务通知降噪 + debug 事件不进入通知（2026-11-20）：后端按任务定义 `Quiet` 把例行过程事实写入统一 `events` 的 `debug` 级（`package_refresh`、`image_refresh`、`image_unused_cleanup`、`panel_tls_reconcile`、`storage_share_reconcile` 新声明 `Quiet`，`server_agent_check` 原有），失败/可重试/blocked 仍为 `error`；通知 Hub 忽略 `level=debug` 过程事件，前端通知桥再做防御性过滤，右上角不再被每 5 分钟 Agent 巡检、每 30 分钟软件包/镜像刷新等刷屏。无 API/表/路由/后台任务/i18n 变化（主 Panel `method/path` 203、ORM 表 49、前端路由 31 不变）。见 [协调、任务与运行事件](orchestration-and-tasks.md) `TASK-REG-003`、[工程质量与发布](engineering-release.md) `ENG-EVT-011`、[界面外壳与交互约定](ui-shell-and-conventions.md) `UI-SHELL-065`、[覆盖基线](coverage-matrix.md) §2 最新条目、[交互模型](../specifications/frontend/interaction-model.md) §5。

清理 Tailscale 时代遗留的 root+降权运行模型（2026-11-20）：容器回到“整体非 root、运行身份固定”的模型——两个 runtime target 末尾 `USER panel`，`/app/panel-init` 仅以同一身份做 PID 1 监督，不再 setuid/setgid、不再接受 `-panel-user`/`PANEL_INIT_PANEL_USER`；维护重启 socket 从 `<dataRoot>/run/panel-control.sock` 迁到 panel-init 进程私有运行目录（不落数据卷、不跨重启复用），`panelsupervision.ControlSocketPath` 改为 `ControlSocketFileName`。无 API/表/路由/后台任务变化（主 Panel `method/path` 194、ORM 表 49、前端路由 31 不变）；既有数据卷若由其他 uid 写入需一次性修正归属。见 [工程质量与发布](engineering-release.md) `ENG-BUILD-005`/`ENG-BUILD-009`、[覆盖基线](coverage-matrix.md) §2 最新条目、[部署说明](../../deployment.md)、[后端核心](../modules/backend-core.md)、[备份与恢复](../modules/backup-restore.md)。

右上角通知改为事件通知 + 阻塞错误 Dialog + HTTP 请求日志 debug（2026-11-19）：右上角 toast 只保留两类内容——后端过程事件的开始/成功/失败通知（长轮询 `GET /api/v1/notifications`、进程内内存通道、不读写数据库、`module=http` 的 HTTP 请求永不进入）与用户操作成功提示；用户当前阻塞操作失败改为全局错误 Dialog；页面/区块加载失败与后台自动刷新、自动保存失败改为就地状态；所有 `http request completed` 完成行统一 `debug`（含 4xx/5xx）。新增 `GET /api/v1/notifications` 路由，主 Panel `method/path` 202 → **203**，ORM 表 49、前端路由声明 31 均不变；无新后台任务类型、无新表/列/迁移。见 [界面外壳与交互约定](ui-shell-and-conventions.md) `UI-SHELL-065` 与 `UI-SHELL-020/022/031/032/056`、[工程质量与发布](engineering-release.md) `ENG-EVT-011`/`ENG-LOG-009`、[API 路由清单](api-route-inventory.md)、[覆盖基线](coverage-matrix.md)、[交互模型](../specifications/frontend/interaction-model.md) §5、[多语言翻译状态](../i18n-translation-status.md)。

下线早期新增的外部网络互联能力（2026-11-19）：删除 3 条路由（2 条服务器/设置侧收敛入口与 1 条设置分区写入），主 Panel `method/path` 计数 197 → **194**；服务器创建/更新/详情不再携带互联意图字段，详情删除对应子视图与表单开关，设置页删除该分区及其认证密钥/标签/应用重连/清除确认；`panel-init` 删除对应进程监管与控制面路由，容器不再要求额外的网络能力与设备，但仍以 root 作为 PID 1 降权运行 Panel 子进程；数据库删除 `servers` 三个旧列并由一次性迁移清理 `runtime_settings`/`servers.traits` 中的废弃键。见 [覆盖基线](coverage-matrix.md) §2 最新条目、[ARCH-API-009](architecture-data-api.md)、[API 路由清单](api-route-inventory.md)、[服务器、安全与软件包](servers-security-packages.md)、[身份、设置与系统](identity-settings-system.md)、[工程质量与发布](engineering-release.md)、[多语言翻译状态](../i18n-translation-status.md)。

应用页“容器日志”的实例选择、手动读取及请求隔离见 [APP-RUN-002](applications-and-facilities.md) 和 [UI-APP-007](ui-pages.md)。

应用同步动作（deploy/stop/restart）回执与 runtime 读取的请求内 Agent 调用边界见 [APP-RUN-001/005](applications-and-facilities.md)。

服务器连接代理模式（`direct` / `ssh_tunnel` / `socks5`）的字段、校验、成环与层数限制、删除隧道服务器后的派生失效、探测复用与 SSH/Agent 两条连接链路见 [SRV-PROXY-001..014](servers-security-packages.md)，表单、详情与 i18n 行为见 [UI-SRV-021..024](ui-pages.md)，表结构与加密说明见 [架构、数据与 API](architecture-data-api.md)。

进程事件 `/events`、诊断日志 `/logs`、按需读取与 `/activity`/`/operations`/`/tasks` 重定向见 [UI-EVT-004..007](ui-pages.md)、[UI-LOG-001..003](ui-pages.md)（`UI-ACT-*` 已废弃）。

磁盘使用率改为 5 分钟低频采样且只展示当前值、磁盘 IO 读写速率与指标弱网补报见 [OBS-MET-007..009](backup-restore-diagnostics.md)、[AGT-RPT-004/007](servers-security-packages.md)；首页概览已改为固定只读状态页（节点情况摘要 + 带 24h 历史条与上下行速率的服务器卡片，并行多接口取数，不再消费 `/overview` 卡片接口），见 [UI-OVR-001/013..019](ui-pages.md) 与 [GAP-031](known-gaps.md)；旧 `UI-OVR-002..012` 卡片条目已废弃。指标 bucket/latest 见 [OBS-MET-003](backup-restore-diagnostics.md)，探测 24h 历史见 [FAC-PRB-009](applications-and-facilities.md)。设施网络探测（`network-probe`）的配置、周期/包数/超时参数（`FAC-PRB-010`）、按目标聚合读取视图（`FAC-PRB-006` 的 `view=targets&history=1`）、结果与删除清理见 [FAC-PRB-001..010](applications-and-facilities.md)，设施详情与服务器概览的“所有服务器按目标聚合 + 24h 折线图”（无探测源/目标选择器）见 [UI-FAC-014..018](ui-pages.md)、[UI-SRV-025/027](ui-pages.md)（横向矩阵与选择器 UI 均已废弃，`UI-FAC-016`）；表结构与可空磁盘 IO 列、`network_probe_configs` 三参数列见 [架构、数据与 API](architecture-data-api.md)。

服务器详情一级 tab 收敛为 概览/系统/资源/网络与安全；概览合并原「概况」与「监控」，状态条与指标仪表盘同屏，系统内为 Agent/软件包，网络与安全内为 防火墙/Fail2Ban（网络探测子视图已移除，探测结果只在设施页与概览面板），子视图用 tab 内的下拉选择器切换，页面只保留一层 tab；旧一级 tab 深链保留别名，`tab=probes` 回落到防火墙。指标改按范围请求后端既有 `bucket` 桶聚合并展示坐标轴/图例/平均峰值/阈值/空档断线。见 [UI-SRV-002/008/009/017/018/019/020/025/026](ui-pages.md)、[UI-SHELL-062](ui-shell-and-conventions.md)；不新增后端 API、数据库表或后台任务。

CPU 指标图统计改为同时展示平均/最大/最小（2026-11-19）：服务器详情概览的 CPU 折线图 footer 统计由「平均/峰值」改为「平均/最大/最小」，内存、磁盘 IO、网络保持「平均/峰值」；`seriesStats` 新增 `minimum`，统计基于当前展示序列（1d/7d 为桶平均后的统计），无有效样本显示占位而非 0。见 [UI-SRV-026](ui-pages.md)、[前端交互模式规范](../specifications/frontend/interaction-patterns.md)、[覆盖基线](coverage-matrix.md)；不新增后端 API、数据库表、前端路由或后台任务。

网络、磁盘 IO、延迟与丢包统计改为同时展示平均/最大/最小（2026-11-19）：服务器详情指标区的内存、磁盘 IO 读写、网络 RX/TX 卡片与延迟/丢包历史卡 footer 统一展示平均/最大/最小值（双序列按读/写、RX/TX 分别给出），无样本显示占位；复用既有 `serversPage.metrics.average/maximum/minimum` 词条，不新增后端 API、数据库表、前端路由或后台任务。见 [UI-SRV-026/027](ui-pages.md)、[前端交互模式规范](../specifications/frontend/interaction-patterns.md)、[覆盖基线](coverage-matrix.md)。

指标图改为按时间桶的最小~最大阴影带并拆分磁盘/网络方向（2026-11-20）：后端指标 bucket 查询补充每桶原始采样的 min/max，网络探测历史桶改为累计平均/最小/最大（旧数据回退为 min=max=平均）；前端把磁盘读取、磁盘写入、网络接收、网络发送拆成独立卡片（加 CPU/内存共六图），CPU/磁盘/网络/延迟/丢包以平均线 + 最小~最大阴影带展示，tooltip 同时给出平均/最小/最大，统计摘要使用区间真实极值。指标设施与网络探测设施分别新增六项/两项警告阈值（`thresholds_json`，AutoMigrate 增列），超阈值的平均线与区间显示红色并保留阈值虚线，新增 `metrics_facility_threshold_invalid`/`network_probe_threshold_invalid`。网络探测设施的结果区取消独立滚动，与上方配置一起由页面正文滚动。见 [OBS-MET-003/004](backup-restore-diagnostics.md)、[FAC-PRB-009](applications-and-facilities.md)、[FAC-MET-003](applications-and-facilities.md)、[FAC-PRB-003](applications-and-facilities.md)、[UI-SRV-026/027](ui-pages.md)、[UI-FAC-018](ui-pages.md)、[前端交互模式规范](../specifications/frontend/interaction-patterns.md)、[覆盖基线](coverage-matrix.md)；不新增后端 API 路由、数据库表、前端路由或后台任务。

服务器详情概览图标化与窄屏重排（2026-11-18）：概览摘要卡新增发行版标识（Ubuntu/Debian/Arch 等本地 vendored 图形 + 品牌色底）与架构标识（芯片图形 + 可读架构文案），磁盘当前值由进度条改为环形图（百分比 + 已用/可用/总量 + 采样时间），状态摘要改为图标化紧凑瓦片并重排手机宽度（<640px）布局；CPU/内存/磁盘 IO/网络与延迟/丢包的当前值与历史只保留在下方指标/探测面板，摘要卡不再重复文字摘要；仅消费详情响应既有 `os`/`architecture` 与指标响应既有 `diskCurrent`/`latest`，**不新增后端 API、数据库表、前端路由或后台任务**，标识为纯识别用途、不表达受支持或健康。见 [UI-SRV-002/008/028/029/030](ui-pages.md)、[UI-SHELL-013/018/062](ui-shell-and-conventions.md)、[OBS-MET-003/004](backup-restore-diagnostics.md)、[前端交互模式规范](../specifications/frontend/interaction-patterns.md)、[覆盖基线](coverage-matrix.md)。

概览顶部摘要区去方化（2026-11-18）：`/overview` 服务器卡片以上的两张摘要卡改为**开放状态总览头**（不套卡片表面、直接位于页面层），拆为节点情况/应用/容量三组，以留白与非对称栅格组织；节点在线率改为语义色环形占比 + 胶囊 KPI，应用改为总数 + 状态胶囊 + 互斥的启用/停用分布条，新增容量组（仅聚合在线且有样本节点：CPU 均值、内存/磁盘合计比例，无样本显示 `—` 与空环不伪造 0）；`DiskUsageChart` 抽取通用 `RatioRing` 薄包装并新增概览本地 `StatChip`；服务器卡片、页头操作行、请求批次、滚动逻辑与 `UI-OVR-016/017` 均不变，**不新增后端 API、数据库表、前端路由或后台任务**（194/48/31 不变）。见 [UI-OVR-014/020/021](ui-pages.md)、[UI-SHELL-026/048/064](ui-shell-and-conventions.md)、[前端交互模式规范](../specifications/frontend/interaction-patterns.md)、[布局体系规范](../specifications/frontend/layout-system.md)、[覆盖基线](coverage-matrix.md)。

失败运行日志与有界降级见 [ORCH-CTRL-008](orchestration-and-tasks.md)；设施逐节点部署、按需请求错误诊断见 [FAC-RP-012/013](applications-and-facilities.md)。

指标采集与保留从设置页移出为**指标设施**（`metrics`）：配置读写 `GET/PUT /api/v1/facility-apps/metrics`、采集节点选择与按节点下发、采集间隔对齐生效、保留清理、旧运行时设置播种与设置页移除、服务器删除配置修剪、失败语义见 [FAC-MET-001..008](applications-and-facilities.md)；设施目录与页面行为见 [UI-FAC-014/019](ui-pages.md)、[UI-SRV-008](ui-pages.md)、[UI-SET-002](ui-pages.md)；设置接口移除三项见 [SET-RUN-021](identity-settings-system.md)，容器上报间隔仍由 [SET-RUN-014](identity-settings-system.md) 约束；表/路由基线见 [项目覆盖基线](coverage-matrix.md)、[API 路由逐项清单](api-route-inventory.md)。

设施命名、布局与探测节拍（2026-11-17）：导航、路由标题与用户指南的用户可见产品名由「设施应用 / Facility Apps」改为「设施 / Facilities」（路由路径 `/applications/facility-apps` 不变）；网络探测与指标设施页把「范围」与「配置」上下堆叠为两个独立区域（范围在上、配置在下），服务器选择器改为紧凑网格；网络探测间隔改为分钟输入（1..60 分钟、默认 1，保存换算 `intervalSeconds=分钟×60`，Panel 校验范围 60..3600 秒），概览服务器卡片的镜像更新徽标改用镜像语义图标。无新增路由/表/后台任务，见 [UI-FAC-014/015/017/018/019](ui-pages.md)、[UI-OVR-017](ui-pages.md)、[UI-SHELL-014](ui-shell-and-conventions.md)、[FAC-PRB-003/004/010](applications-and-facilities.md)、[FAC-MET-003](applications-and-facilities.md)、[AGT-RPT-008](servers-security-packages.md)。

侧栏品牌区去装饰竖条（2026-11-17）：AppShell 桌面侧栏与移动抽屉品牌区左缘的 3px 品牌色装饰竖条已移除，品牌区仅保留图标/名称/折叠按钮；无新增路由/表/后台任务，见 [UI-SHELL-014](ui-shell-and-conventions.md)、[布局体系规范](../specifications/frontend/layout-system.md)。

WinUI 3 风格改版与服务器持久化外观（2026-11-17）：前端 token/动效/外壳/控件/模板改为 WinUI 3 观感（中性灰阶、Mica、4/8px 圆角、32px 控件、83/167/250/333ms 无上浮/无缩放动效、Page refresh 路由切换）；外观（明暗/Mica/9 强调色/背景图上传）改为服务器持久化：新增 `appearance_settings` 表（表 47→48）、6 条 `settings/appearance*` 路由（186→192）与 `<dataRoot>/appearance/` 文件目录，登录页走公开外观接口（仅模糊缩略图），localStorage 仅首屏缓存，旧 lighthouse/ocean 偏好首次登录一次性迁移；外观设置新增分区 `/settings/appearance`（路由声明 34→35）。P2 又把 14 个页面族的页面/分区表面统一为 WinUI 3 token（卡片 `rounded-overlay` + `border-card-stroke`、行内元素 `rounded-control` + `border-divider`，清除 `rounded-2xl/xl/lg`、`bg-background`、静态阴影与 hover 位移；无新增 API/表/路由）。P3 统一图标与加载指示：本地 vendored Fluent System Icons 兼容层 `@/icons`（52 文件迁移，MIT，见 `web/src/icons/README.md`）、WinUI `ProgressRing` 与 `Checkbox` primitive；仍无新增 API/表/路由。见 [SET-APP-001..012](identity-settings-system.md)、[UI-SHELL-017/020/021(已废弃)/026/047/048/049/052/063](ui-shell-and-conventions.md)、[UI-SET-013](ui-pages.md)、[视觉 Token 规范](../specifications/frontend/visual-tokens.md)、[布局体系规范](../specifications/frontend/layout-system.md)、[覆盖基线](coverage-matrix.md)。

设置写入拆分与设置页自动保存（2026-11-18）：运行时设置写入由整体 `PUT /api/v1/settings/runtime` 拆成 6 条分区子资源端点（含 `PUT /settings/runtime/general|security|certificates|agent|branding`），`GET /settings/runtime` 与完整 `RuntimeSettings` 响应不变，整体 PUT 删除；主 Panel `method/path` 计数 192 → 197（该口径包含其后整体下线的 3 条互联路由，当前基线 194，见 [覆盖基线](coverage-matrix.md) 与 [ARCH-API-009](architecture-data-api.md)），无数据库表/后台任务变更。设置页 general/security/certificates/agent/system（品牌+服务器变量）移除保存按钮，改为 500ms 防抖自动保存（分区串行、本地校验失败不发请求、成功仅状态指示、失败 toast+重试、语言即时切换与失败回滚、切 tab 不卸载、刷新取消待发），保留 JWT 重置、系统证书重置、清空事件与日志、备份导出/还原、外观上传/移除/恢复默认等动作按钮，外观分区仍走乐观即时保存。见 [SET-RUN-022](identity-settings-system.md)、[UI-SET-002/003/005/007/010/014](ui-pages.md)、[UI-SHELL-022](ui-shell-and-conventions.md)、[ARCH-API-009](architecture-data-api.md)、[AutoSaveStatus 模式](../specifications/frontend/interaction-patterns.md)、[覆盖基线](coverage-matrix.md)、[API 路由清单](api-route-inventory.md)。

入口代理自动更新、每域名传输开关与自定义指令（2026-11-19）：设置页新增「自动更新」分区，可配置应用更新/软件包更新（默认关、共用每日 `HH:MM`、软件包范围仅安全/全部、错过不补跑），自动更新协调任务按服务器串行执行并受应用与入口代理各自的对象级开关约束（`SET-RUN-023`、`TASK-PER-004`、`PKG-API-011`、`APP-IMG-003`、`UI-SET-015`、`UI-APP-024`）。入口代理镜像改为受管标签线补丁自动更新（每网关 target digest 进入 spec/identity 哈希、先拉后停、逐节点滚动、状态表 `facility_proxy_image_states`，`FAC-RP-014`）；设施新增全局参数与设置 API（80 策略/HSTS/自定义 http 指令/`imageAutoUpdateEnabled`，`FAC-RP-015`）；`reverse_proxy_routes` 新增每域名 `https_enabled/http2_enabled/http3_enabled/custom_server_directives`，设施域名与应用反代共用，HTTP/3 补齐 UDP 443 端口发布与 UFW 放行并移除全局 `http2/http3/ssl_early_data`（80 端口仅 HTTP/1.1），`FAC-RP-016`；http/server 自定义单行指令采用"同名覆盖、其余追加"（`FAC-RP-017`、`UI-FAC-020/021/022`）。数据库新增表 48→49 与 `applications`/`facility_app_configs`/`reverse_proxy_routes` 列；- 主 Panel 路由 194→198、后台任务类型新增 4 个、前端设置页分区 8→9；P1 再新增镜像回滚/恢复自动更新与“立即运行一次”3 条路由（198→201）；P2 新增 `PUT /settings/runtime/image-registry`（201→202，`SET-RUN-024`/`UI-SET-016`）。见 [项目覆盖基线](coverage-matrix.md)、[API 路由逐项清单](api-route-inventory.md)、[多语言翻译状态](../i18n-translation-status.md)。

修改下列对象时，至少检查对应消费者：

| 被修改对象 | 必查消费者 |
| --- | --- |
| API 路径、方法、请求或响应 | `web/src/api`、`web/src/types`、Mock、页面、路由清单测试、对应域验收项 |
| 状态/错误枚举 | 状态徽标与筛选、重试/禁用按钮、i18n、任务和事件投影、旧数据解析 |
| 应用期望态或运行态 | orchestrator、Agent contract、容器观察、应用操作记录、`/events` 进程行、设施应用协调 |
| Server/Agent 能力 | 探测、部署、证书、指标、容器、软件包、应用调度、离线降级 |
| DNS 或证书 | 反向代理、应用内置变量、密钥资产引用、续签任务、删除引用检查 |
| 数据库模型 | 四库归属、迁移顺序、索引/约束、备份恢复、保留清理、旧版本升级 |
| 指标设施配置（采集范围/间隔/保留天数/清理计划） | Agent report stream 按节点下发、指标整点对齐与禁用节点跳过落库、metrics cleanup worker、指标查询与概览、设置页迁移播种、服务器删除修剪、i18n 与设施目录/详情页 |
| 运行时设置 | 启动加载、热更新范围、后台调度器、认证、TLS、前端表单与公开品牌接口 |
| 用户可见文本 | 中英文 key、后端错误翻译、默认/回退语言、翻译状态 |
| 构建产物或版本 | Panel、panel-init、全架构 Agent bundle、前端静态资源、镜像健康检查、发布标签 |

## 6. 需求追踪与评审

- `GOV-TRACE-001`：实现提交或评审说明应引用本目录中的验收编号；一个实现可满足多个编号，但不得以单个模糊编号代表整个模块。
- `GOV-TRACE-002`：自动测试名称或注释应能反查关键验收编号；无法自动化的项需说明人工观察点和无法自动化的原因。
- `GOV-TRACE-003`：修复回归问题时必须新增或收紧能阻止同类回归的验收项，并增加相应测试；不得只记录具体事故现象。
- `GOV-TRACE-004`：兼容性例外必须写明适用版本/数据形态、退出条件和失败时行为，禁止永久性的无边界兼容分支。
- `GOV-TRACE-005`：任何“暂不支持”的操作必须在 UI 和 API 中表现为不可用或明确拒绝，不得静默部分执行。

## 7. 明确非目标

- 本规范不教授终端用户如何部署或使用 Seamark；用户材料仍位于 `docs/user-guide*` 与 `docs/deployment*`。
- 本规范不固定私有函数、目录拆分或具体 SQL 写法，只固定可观察结果和不可破坏的不变量。
- 设计提案只有在被本目录吸收并与现有行为对齐后才具有验收效力；`docs/design` 中的历史方案不自动成为当前合同。
