# 前端页面与操作验收标准

## 1. 文档定位与使用

本文逐路由、逐主要操作定义用户可验证的前端契约。它不是用户手册，也不规定代码组织步骤。全局会话、布局、加载/错误、浮层、URL、i18n 和可访问性规则同时受 [ui-shell-and-conventions.md](ui-shell-and-conventions.md) 约束；本文件未重复的全局条件仍然必须满足。

条目中的“必须”是后续改动不可破坏的验收目标。行为来源按“当前测试 > API/类型与页面源码 > 现有模块/前端规范”交叉核对；代码与既有规范不一致时，保留正确产品契约并在文末列出现状缺口。

## 2. 路由覆盖表

| 页面族 | 规范路由 | 兼容、隐藏或环境路由 |
| --- | --- | --- |
| 登录与改密 | `/login` | 已登录自动去 `/overview` |
| 维护 | `/maintenance/backup` | 独立 shell、独立维护 token |
| 概览 | `/overview` | `/` 重定向至此 |
| 服务器 / 凭据 | `/servers`、`/credentials` | — |
| 服务器 / 凭据 | `/servers`、`/credentials` | 资源与安全已并入服务器详情 tabs；`/resources/*`、`/security/*` 已删除并落入 404 |
| 应用 | `/applications/apps`、`/applications/apps/create`、`/applications/apps/:applicationId/edit` | `/applications` 重定向 |
| 设施 | `/applications/facility-apps`、`/applications/facility-apps/:facilityKind` | `/:facilityKind/config` 为可直达编辑态深链 |
| DNS / 证书 | `/dns/domains`、`/certificates/domains`、`/certificates/self-signed`、`/certificates/keys` | — |
| 进程事件 / 诊断日志 | `/events`、`/logs` | `/operations`、`/tasks`、`/activity` 兼容重定向 |
| 设置 / 诊断 | `/settings/general`、`security`、`certificates`、`agent`、`tailscale`、`system-certificates`、`system`、`backups` | `/settings` 重定向；`/debug` 隐藏直达 |
| 未找到 | AppShell 内 catch-all | 保留导航 |

## 3. 登录与强制改密

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-AUTH-001 | 未认证进入 `/login` | 显示公共 branding 的标题/副标题，失败则回退 Seamark 本地文案；用户名自动聚焦，表单可回车提交 | branding 加载失败不得阻止登录；用户名或密码为空只显示本地校验，不发请求 | 表单 label/autocomplete 正确，空提交无 `/auth/login` 请求 |
| UI-AUTH-002 | 输入用户名和密码并登录 | 请求期间锁定提交；成功保存 session，并安全跳回 redirect 或 `/overview` | 登录失败以 danger toast 反馈并清空密码，但保留用户名；不得触发全局 401 跳转循环 | 失败后密码框为空；成功后受保护请求带新 token |
| UI-AUTH-003 | 登录/session 返回 `passwordChangeRequired` | 同页切换为用户名、当前密码、新密码、确认密码表单；完成前所有受保护路由继续回登录页 | 必填缺失、新密码少于 8 位或两次不一致时就地报错且不请求 | 合法提交 `/auth/account` 后按安全 redirect 进入控制台 |
| UI-AUTH-004 | 登录或改密页面渲染 | 使用独立全视口布局，底部显示 Seamark 图标与英文产品名 | 不得加载 AppShell 导航，也不得因窄屏遮住提交按钮 | 1024px 两侧均可完成表单，footer 不覆盖内容 |

## 4. 概览 `/overview`

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-OVR-001 | 首次进入或手动刷新 | 并行请求服务器列表与探测设施配置，随后并行请求每台服务器的指标历史（`GET /servers/{id}/metrics?range=1d&bucket=1800`，含最新原始样本 `latest`）、所选探测源的探测结果与 24h 历史（`GET /servers/{id}/network-probes?history=1`）与软件包/镜像更新统计；展示节点情况与服务器卡片；**不调用** `GET /api/v1/overview` | 服务器列表失败显示错误空态+重试；无服务器显示添加服务器引导；单台服务器指标/更新统计失败只让该卡片数值显示 `—`，不阻断其它卡片；路由切换/卸载/新请求到达时丢弃迟到响应 | 请求按“列表+配置 → 每服务器指标+探测结果”两批并行发出；单台失败时其它卡片仍显示数据 |
| UI-OVR-002 | （已废弃）可配置指标卡多服务器折线 | 首页已改为固定只读状态页，不再有可配置指标卡与折线图；替代项 `UI-OVR-016` | — | — |
| UI-OVR-003 | （已废弃）自动刷新增量取数 | 不再有卡片 `since` 增量与桶后缀合并；自动刷新行为见 `UI-OVR-018` | — | — |
| UI-OVR-004 | （已废弃）点击“编辑仪表盘” | 首页不再提供编辑态；替代项 `UI-OVR-013` | — | — |
| UI-OVR-005 | （已废弃）添加/删除/重置卡片 | 首页不再有卡片增删与重置；替代项 `UI-OVR-013` | — | — |
| UI-OVR-006 | （已废弃）拖动或指针缩放卡片 | 首页不再有卡片拖拽/缩放；替代项 `UI-OVR-013` | — | — |
| UI-OVR-007 | （已废弃）打开单卡编辑器 | 首页不再有卡片类型/尺寸/range/服务器范围编辑；替代项 `UI-OVR-013` | — | — |
| UI-OVR-008 | （已废弃）保存或取消编辑 | 首页布局固定，无保存/取消语义；替代项 `UI-OVR-013` | — | — |
| UI-OVR-009 | （已废弃）编辑中离开确认 | 首页无未保存布局，不再需要放弃确认；替代项 `UI-OVR-013` | — | — |
| UI-OVR-010 | （已废弃）风险队列或快捷入口 | 首页移除右侧风险队列/快捷入口；异常通过节点情况与卡片状态表达，详情入口见 `UI-OVR-016` | — | — |
| UI-OVR-011 | （已废弃）概览 `diskIO` 卡片 | 首页图表卡下线；磁盘 IO 历史仍可在服务器详情指标页查看 | — | — |
| UI-OVR-012 | （已废弃）概览 `disk`/`networkProbe` 卡片 | 首页图表卡下线；磁盘当前值进入卡片内存/磁盘进度条（`UI-OVR-016`），探测摘要进入网络综述（`UI-OVR-015`） | — | — |
| UI-OVR-013 | 浏览首页 | 首页为固定只读状态页：不出现“编辑/保存布局”、添加/删除卡片、拖拽/缩放、卡片编辑器、自定义图标/类型/range/服务器范围等入口；刷新与自动刷新仍可用 | 直接访问 `/overview` 不得触发卡片配置或 card-data 请求 | DOM 中不存在编辑/添加卡片按钮；调用列表只包含 `servers`/`metrics`/`packages`/`images`/`network-probe` 相关请求 |
| UI-OVR-014 | 查看节点情况摘要 | 展示总节点、在线、离线、在线率条与指标新鲜数；**在线=服务器可达（`reachable`），离线=不可达**，在线状态与指标新鲜度解耦；指标新鲜数是独立统计（有 5 分钟内指标样本的节点数），不改变在线/离线计数；不在摘要中单列“系统不支持” | 无服务器时统计为 0 并显示添加服务器引导；不因无数据伪造健康；可达但无指标样本的节点必须计入在线 | 摘要数字与卡片状态一致；`aria-label` 含文本比例 |
| UI-OVR-015 | 切换探测来源 | “探测来源”选择器位于**页面头部操作行**（与自动刷新/刷新同一行）：候选取自探测设施配置（`mode=all` → 全部服务器，`mode=selected` → 选中服务器），默认第一个并同步 URL `?probe=`；页面不再展示“网络综述”汇总卡 | 无候选时选择器禁用；探测结果读取失败只让卡片相关指标显示 `—`，不阻断页面其它数据；切换来源只重新请求该源结果 | 选择后 URL 更新；刷新后按 URL 恢复所选来源 |
| UI-OVR-016 | 查看服务器卡片 | 卡片标题只用状态点（绿/红）表达在线/离线（在线=服务器可达，离线=不可达，**指标是否新鲜不改变状态点**），**不显示“在线”“系统不支持”等状态文字徽标**（状态文字通过 `title` 与读屏文本提供）；标题只显示服务器名称与“探测源”标记，**不显示 IP:端口**；详情入口为图标按钮（aria-label）而非大号文字按钮；正文按行分组：第一行 CPU（24h 历史条）+ 负载（仅当前值），第二行 内存（进度条）+ 磁盘（进度条），第三行 磁盘读取（左列）+ 磁盘写入（右列），均为当前值，第四行 网络下行（左列）+ 网络上行（右列），均为当前值，第五行 延迟（24h 历史条）+ 丢包率（24h 历史条）；卡片使用较大内边距/行距，历史条按 2 小时合并为 12 段以降低视觉密度；**不显示最近上报/最近探测时间**；延迟/丢包按所选探测源：**探测源自身卡片的延迟/丢包显示占位控件**（数值 `—` + 虚线占位条，不请求该源的历史数据、不画历史条，aria-label 说明探测源不测量），其它卡片显示该源到该服务器的历史 | 无指标样本、指标过期、探测过期、未探测或未参与探测时当前值显示 `—`、历史条灰显，不得显示 0 或伪造数值；磁盘未采样不得当空盘；不得整卡可点 | 五行分组与左右列归属正确；磁盘读写取自 `diskIo`、上下行取自 `latest` 且均为当前值无历史条；探测源卡片延迟/丢包为占位控件且无历史条；历史条为 12 段且时间窗与桶数据一致；详情进入 `/servers?server=<id>` |
| UI-OVR-017 | 服务器存在软件包或镜像更新 | 卡片标题显示更新数量徽标（软件包/镜像分别显示，0 不显示）；徽标图标必须符合语义：镜像更新使用镜像语义图标（`Layers`），不得使用图片/照片图标（`Image`），软件包使用 `Package`；文案仍为「镜像更新」，图标仅表达容器镜像语义；点击分别跳转 `/servers?server=<id>&tab=packages` 与 `/servers?server=<id>&tab=resources&subtab=images`；徽标有本地化 title 说明数量与目标页 | 更新统计失败不显示徽标、不阻断卡片；数量不得缓存到布局或持久化 | 点击后 URL 的 server/tab/subtab 与徽标类型匹配 |
| UI-OVR-018 | 自动刷新 10 分钟/30 分钟/1 小时、手动刷新或切换探测源 | 自动刷新在页面可见时重取服务器列表、每服务器指标与当前探测源结果（不重复拉取软件包/镜像更新统计），隐藏标签时暂停；**同一时刻只允许一轮加载，在途时跳过下一拍而不是互相 abort**；手动刷新重取全部数据；切换探测源只重取该源探测结果；请求期间防重入 | 迟到响应必须丢弃；自动刷新失败保留旧数据并静默重试，不重复弹 toast；**单台指标请求失败保留该服务器上一次的值，不得把卡片清空**；服务器列表偶发空响应时保留现有列表；探测失败/无数据不得阻断或清空指标；手动刷新失败保留旧数据并提示；卸载/离开页面清理定时器 | 不会出现旧探测源的结果覆盖新选择；自动刷新不闪白、不重放卡片入场；清空探测数据后指标仍显示 |
| UI-OVR-019 | 无服务器或窄屏、辅助技术 | 无服务器显示添加服务器引导；桌面外层填满除全局页头外的视口且无页面级滚动，仅卡片网格内部滚动；卡片网格按宽度 2/3/4 列自适应（`sm`/`xl`/`2xl`），窄屏单列/双列并恢复页面滚动、无横向滚动；状态点、历史条与进度条具有可读文本（`title`/`aria-label`/读屏文本），装饰图标 `aria-hidden` | 不得用横向滚动兜底；不得只靠颜色表达状态（状态点必须附带 `title` 与读屏文本；无指标/指标不新鲜通过 `—`、灰条与摘要新鲜数说明）；状态与数值文案全部经 i18n | 1024px/1280px/1920px/窄屏四档下卡片列数、滚动区域正确；读屏可读出指标名、数值与状态 |

## 5. 服务器 `/servers`

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-SRV-001 | 打开列表、搜索、翻页或选择服务器 | 服务端 `page/pageSize/q` 查询；URL 恢复 search/page/server/tab/subtab；表格列展示名称、地址、**单一综合状态**、最近检查（权限细节/Tailscale 进 Overview/对应 tab，不常驻列表）；综合状态只显示一枚徽标，优先级为不可达 → 主机密钥不匹配 → Agent 受阻/不可用/不匹配/未安装 → 正常（仅 `reachable` 且 Agent `compatible`）；**不得**单独用「可达」冒充健康；当摘要含非 `excellent` 的 `agent.report.stability` 时，状态列可在综合徽标旁另附一枚**链路稳定性**徽标（不得替换综合状态）；行操作保留「详情」+ 至多 1 个高频（测试连接），编辑/删除进 More；点击「详情」或名称列按需 GET 完整详情并覆盖列表 | 列表失败显示重试而非空态；切换服务器时取消/忽略旧详情并清空旧 tab 数据；无选中时不自动打开详情；不得依赖整行点击；无 report 稳定性数据或等级为 `excellent` 时不得在列表画稳定性徽标 | 分享 URL 恢复相同页、服务器与 tab/subtab；不会显示上一服务器的迟到详情；Agent `undeployable` 且仍 reachable 时列表状态为受阻而非可达/正常 |
| UI-SRV-002 | 查看服务器详情 | 覆盖式详情展示连接地址、端口、凭据、连接方式（含代理失效警告，见 `UI-SRV-024`）、Docker host、OS、权限与安全、最近检查/更新时间、初始化任务与资源 tabs；**概览合并状态与指标**：顶部概览摘要卡展示综合状态、Agent 上报链路、链路稳定性、连接代理、Tailscale、Agent 版本、OS/架构/权限、最近检查/更新时间与磁盘当前值（已用/总量/百分比/采样时间）；下方为指标仪表盘（见 `UI-SRV-008/026`）、连接信息与最近操作；架构对象按可读文案（如 `amd64 (x86_64)`）渲染，不得输出原始 JSON；页头 actions 含「编辑」（打开与列表 More 相同的编辑表单）以及重启等运维操作；摘要卡只展示**同一枚**综合状态徽标，特权能力以正文说明而非第二枚状态徽标；Agent report 链路稳定性按 `UI-SRV-020` 展示，不得并入综合状态徽标 | 凭据列表失败只影响凭据显示/表单并 toast，不得抹掉服务器列表；选中服务器不在当前页时按 id 单独补载；不得并列展示可达+特权两枚状态徽标造成“一切正常”误读；概览内容超出正文高度时只允许详情正文内部滚动，不得出现页面级滚动 | 详情先显示已加载字段，完整详情加载有局部覆盖；综合状态与列表一致；页头编辑与列表编辑同一入口；首屏可同时看到状态摘要与指标区 |
| UI-SRV-003 | 新建/编辑服务器 | 表单含 name、IPv4、IPv6、port、credential、SSH username、Docker host、变量、notes，以及连接代理小节（见 `UI-SRV-021`）；至少一个地址且为 IP 字面量，port 1–65535，name/credential/dockerHost 必填；基础连接（名称/地址/端口/凭据/SSH 用户名）与高级配置（Docker host/变量/备注）分区，创建时高级区默认折叠、编辑时展开 | host 只读由 IPv4 优先、否则 IPv6 派生；无效字段在字段下方就地显示本地化错误，底部摘要列出全部错误并点击聚焦对应字段 | 无效字段有本地化提示且无写请求；编辑需以完整详情填充；任何字段错误都可通过摘要定位 |
| UI-SRV-004 | 点击“探测” | 用当前未保存表单调 probe 并在基础连接区显示 reachable/error/OS/架构/特权；探测请求只发送连接字段（地址、端口、凭据、SSH 用户名，以及代理草稿字段与编辑态可选 `serverId`，见 `UI-SRV-022`） | 探测闸门只受地址/端口/凭据约束，名称与 Docker host 错误不得连坐禁用探测；连接字段变化后旧结果标记过期（灰化）提示重新探测；探测失败保留输入并非保存、不产生服务器记录 | probe 请求不含名称/Docker host/变量，结果与输入对应；创建/探测按钮可用性互不影响 |
| UI-SRV-005 | 提交服务器表单 | 创建/更新成功关闭弹窗、打开新对象覆盖详情、失效详情缓存并重载列表；创建返回 initialTaskId 时 toast 与概览只表达“服务器已登记、初始化任务已启动”，并跟踪该任务 | 提交中防重复；API 失败保持弹窗及输入；初始化任务失败时服务器记录保留（不自动删除）、标记不可达与原因，概览任务状态卡展示失败与错误，用户可编辑/删除 | 成功 toast 不早于服务端确认；活跃初始化任务每 2 秒刷新状态，失败态可经任务状态、服务器详情与活动日志追溯 |
| UI-SRV-006 | 点击测试连接 | 调用所选服务器 test，成功刷新该详情和列表并提示服务器名 | 失败保留页面，不能伪造 reachable | 仅目标服务器状态改变，按钮 loading 覆盖请求期 |
| UI-SRV-007 | 删除服务器 | 先显示对象名与影响说明的 danger 确认；成功清详情缓存、清选择并重载 | 失败不关闭确认/不移除行 | 取消零请求；成功后 URL 不再选中已删除 ID |
| UI-SRV-008 | 概览指标区首次加载、换 range、手刷或自动刷新 | 指标区位于概览摘要卡之后；默认 range=1h，支持 1h/6h/1d/7d；按范围请求 `GET /servers/{id}/metrics?range=…&bucket=…`（1h→60s、6h→120s、1d→300s、7d→1800s），当前值优先取响应 `latest`，不得为当前值另发原始请求；展示 CPU、内存、磁盘 IO（读/写双序列）、网络 rx/tx 四张曲线图和当前值；磁盘使用率只作为摘要卡内的**当前值**（已用/总量+百分比+采样时间），不画历史曲线；`diskIo` 缺测样本按时间轴断线，不得补 0；1d/7d 视图必须标注“按桶平均”；提供共享自动刷新选择（关闭/10 分钟/30 分钟/1 小时，默认 10 分钟），**仅在当前 tab 激活且页面可见时刷新**，手动刷新/切 range 行为不变；被指标设施禁用的节点（`FAC-MET-002/004`）不再产生新样本，页面保留并展示其历史曲线与最后已知当前值（带采样时间），不得因禁用清空历史、补 0 或报错 | 同服务器刷新保留旧图；换服务器清旧图；无数据时显示局部空态；磁盘区域无历史曲线时不得显示空图骨架或伪造曲线；切换 range 时不得先清空旧图；禁用节点无新样本不得解释为服务器离线，也不得用其它节点数据冒充；自动刷新在 tab 非激活/加载中时跳过 | 请求同时含 range 与 bucket 且与范围映射一致；`GET /servers/{id}/metrics` 响应含 `diskIo`/`diskCurrent`/`latest` 且不含 `disk` 时前端仍能正确渲染；失败只影响指标区并保留可用旧数据；看不到高频轮询；被禁用节点历史仍可读取、仅缺少新样本 |
| UI-SRV-009 | 点击部署 Agent | 提交后台任务并显示真实 taskId；立即加载该任务，显示阶段、错误、最近 20 行日志；页头主机状态与列表共用综合状态文案，部署任务另用任务 StatusBadge，二者不得用「可达」代替 Agent 问题；入口位于「系统 → Agent」子视图 | 无历史显示明确空态；任务加载失败有局部重试 | 活跃任务每 2 秒增量取日志，隐藏标签暂停，完成时刷新服务器详情 |
| UI-SRV-010 | 重启或信任新 host key | 详情页头与系统 → Agent 子视图均可发起主机重启；按能力禁用不可执行项；两者均先 danger 确认，信任 host key 额外强制勾选 | 确认后重启只提示任务已接受；信任成功刷新主机指纹状态 | 未确认零请求；响应 taskId/服务器名与 toast 一致 |
| UI-SRV-015 | 打开新建服务器表单（非编辑） | 在连接区之后显示**非阻断**警示横幅：面板将在部署 Agent 时接管该机防火墙（仅 UFW）——缺失时安装、放行 SSH/Agent 端口与反向代理 80/443、启用默认拒绝策略；未声明「开放防火墙」的应用端口会被阻断 | 横幅不得出现在编辑态；不得新增勾选项、弹窗或确认步骤 | 横幅不影响 `canSave`，不得因其存在而阻止提交或发出额外请求 |
| UI-SRV-011 | 填写变量 | 逐行 `key=value`；缺 `=`、空变量名属阻断错误，逐行就地提示并阻止提交；重复 key 保留 last-wins 并显示非阻断警告 | 空行忽略；阻断错误存在时创建/保存禁用 | 每行行号定位对应错误；无写请求直到阻断错误消除 |
| UI-SRV-012 | 无可用凭据时新建服务器 | 凭据区显示“尚无 SSH 凭据”引导与添加入口，可在弹窗内快捷创建凭据；成功后自动选中新凭据 | 凭据列表加载失败在弹窗内就地显示错误与重试，不影响服务器其余表单 | 快捷创建后凭据即时可用于表单与探测；无需离开当前弹窗 |
| UI-SRV-013 | 查看新建服务器初始化状态 | 概览以状态卡展示初始化任务（StatusBadge + 错误 + 活动入口），而非仅纯文本任务 id | 初始化任务加载失败有局部错误；任务失败时服务器保留、标记不可达并在状态卡持久展示原因，供编辑/删除 | 活跃初始化任务刷新，终态与失败原因可经任务状态与活动日志追溯 |
| UI-SRV-014 | 在服务器表单中配置 Tailscale 意图 | 「将该节点加入 tailnet」与两个偏好开关（Agent 连接、节点互联）；未启用时两个偏好开关禁用并显示说明，提交时未启用的一侧偏好被规范化为 false | 不得在未启用时提交偏好组合；不得只依赖后端强制而在界面保留可点击的无效开关 | 重新打开表单回显与服务端一致的三个开关，保存后详情与列表按服务端返回状态渲染 |
| UI-SRV-016 | 查看服务器 Tailscale 状态或手动重试 | `tailscaleEnabled` 时概览状态条与「系统 → Tailscale」子视图显示状态徽标（`disabled`/`pending`/`installing`/`running`/`degraded`/`unsupported`/`error` 的本地化文案）；列表不再常驻 Tailscale 列；Tailscale 子视图单列**只读**展示加入意图与两个偏好（用已启用/未启用徽标而非可操作开关）、主机名、IPv4/IPv6、状态更新时间与 `lastError`；「应用 Tailscale」位于详情页头 actions（仅该子视图激活时出现），提交任务并提示真实 taskId，随后刷新服务器详情 | 未启用时显示未配置说明且页头应用按钮禁用；从未上报时显示明确空态而不是伪造状态；失败必须显示原因且可重试，不得把“任务已受理”表述为节点已加入；只读偏好不得渲染成可点击控件 | toast 中的 taskId 可在 `/executions` 或 `/events` 定位；徽标与 `tailscale.status` 一致，`lastError` 脱敏后原样展示 |
| UI-SRV-017 | 在服务器详情切换 tabs | 详情一级 tab 固定为 **概览/系统/资源/网络与安全**；概览合并状态与指标（见 `UI-SRV-002/008/026`）；系统子视图为 `agent`/`tailscale`/`packages`（默认 agent），资源子视图为 `containers`/`images`/`networks`/`volumes`（默认 containers），网络与安全子视图为 `probes`/`firewall`（默认 probes），分别用 URL `subtab` 切换；旧深链 `tab=metrics` 映射到概览，`tab=agent\|tailscale\|packages` 映射到 `tab=system&subtab=…`，`tab=probes\|firewall` 映射到 `tab=network-security&subtab=…`，旧 `tab=containers\|images\|networks\|volumes` 仍映射到 `tab=resources&subtab=…`；一级/子视图首次激活才请求并在详情存活期内缓存，切换服务器或关闭详情清空旧数据；指标区只在首次打开、切换 range 或手动刷新时请求，不做定时轮询；防火墙内 UFW/Fail2Ban 用分段控件（非嵌套 Tabs），选择进 URL `subtab`（`fail2ban` 同时表示防火墙子视图与 Fail2Ban 视图），Fail2Ban 仅 dev 可见 | 切 tab/subtab 不得清空已加载快照或重复请求；能力不足在对应面板内给出阻断原因；不得用页面级滚动承载 tab 内容；概览/系统/网络与安全子视图单列展示，禁止无关 aside；旧 tab 别名不得把用户留在空 tab | tab 切换无闪烁且状态保留；深链 `?server=&tab=resources&subtab=images`、`?tab=system&subtab=tailscale`、`?tab=network-security&subtab=firewall`、`?subtab=fail2ban`、旧 `?tab=probes`/`?tab=metrics`/`?tab=images` 均恢复对应面板；非 dev 无 Fail2Ban 子视图 |
| UI-SRV-018 | 查看/操作资源表格 | 容器/镜像/网络/存储卷在「资源」子视图、软件包在「系统 → 软件包」子视图内以表格展示；子页去掉重复「左侧标题复述 tab 名」；镜像/Fail2Ban 工具栏收敛为**一个主操作 + Refresh，其余进 More**；容器行操作收紧为图标主操作（启动/日志）+ 溢出（停止/重启/删除，确认 + 托管提示）；镜像：拉取、升级应用、清理未使用、删除（in-use 禁删、危险确认强制勾选）；网络只读且删除禁用；存储卷：清理未使用、删除（in-use 禁删、确认强制勾选）；软件包：勾选只对当前快照，批量栏仅在有选中时出现，升级全部先 danger 确认；网络/存储卷首次打开且无快照时自动提交刷新任务 | 根据状态/能力禁用无效动作；操作按钮按（行, 操作）单独 loading；失败保留快照并 toast；不得把失败当作空列表 | 表格列包含名称/关键状态；请求 payload 与选中项一致；取消确认零请求；成功后重载当前面板 |
| UI-SRV-019 | 在「网络与安全 → 防火墙」维护 UFW / Fail2Ban | UFW 展示 supported/installed/active/default policy 与规则号/action/to/from，提供新增 allow rule 与删除规则（确认、受保护端口/受管规则由后端拒绝），并展示「安装与启用由 Agent 部署自动完成」说明；Fail2Ban 可在 visual 与 YAML 间切换，提供 preset、jail 删除、保存草稿、安装、启用/接管确认、释放（danger 确认）；保存草稿仅在 YAML 变化时启用；UFW/Fail2Ban 切换用分段控件而非嵌套一级 Tabs | 页面不得出现 UFW 安装/启用入口；未勾选接管不提交；保存不等于启用；失败保留草稿/输入 | 规则增删使用返回值重建列表；toast 只承诺任务接受并含真实 taskId；visual 往返可解析 |
| UI-SRV-020 | 查看 Agent report 链路稳定性 | 概览摘要卡与「系统 → Agent」必须展示 report 链路状态（connected/disconnected）与稳定性等级（优/良好/一般/差/极差，对应 `excellent`/`good`/`fair`/`poor`/`critical`），并在有计数时展示近 1 小时断连次数；文案与色调独立于综合主机状态与部署任务 StatusBadge；列表仅在非优时附稳定性徽标（见 `UI-SRV-001`） | 尚无 `agent.report.status` 时显示明确空态/暂无数据，不得假装为优；不得把稳定性画进综合状态徽标或替代 Agent 健康文案；标签缺失不得只显示无说明的裸徽标 | 等级与 `AGT-RPT-005` 阈值一致；断连次数与摘要/详情 traits 一致 |
| UI-SRV-021 | 新建或编辑服务器时查看“连接”区 | 连接区新增「连接代理」小节：模式三选一（直连 / SSH 隧道 / SOCKS5 代理，默认直连）；SSH 隧道模式显示**可搜索**的隧道服务器下拉：候选来源为**全量服务器列表**（打开表单时加载，与当前已加载列表按稳定 id 合并去重，排除当前服务器自身），输入时按名称/地址远程搜索（防抖 250ms 复用 `GET /api/v1/servers?page=1&pageSize=50&q=<term>`，结果按 id 合并进候选集，可命中预载 200 台之外的分页外服务器）；加载中/搜索/搜索失败/无匹配均有就地提示，已选服务器始终可见（不在候选集时保留占位项）；无候选时显示明确说明，并展示“经该服务器的 SSH 连接转发目标服务器 SSH 与 Agent 流量”说明；SOCKS5 模式显示地址（占位符 `127.0.0.1:1080`）、用户名（可选）、密码（可选）；编辑态密码框占位提示“留空保持原密码”；切换模式只显示当前模式字段，隐藏字段不得随表单携带残留值 | 编辑既有服务器时必须按详情回显 `proxyMode` 与对应字段，密码永不回显；候选加载失败降级为当前已加载列表并提示，搜索失败提示可继续输入重试，二者都不得阻塞直连/新建保存，也不得清空已选隧道服务器或伪造候选；切换模式不得丢失已填写的名称/地址/凭据等基础字段 | 默认打开表单为直连；三种模式的字段与说明使用 `serversPage.proxy.*` 文案，且表单继续遵守无页面级滚动的布局约定；搜索关键词能命中预载集之外服务器，清空搜索后候选恢复且已选值不丢 |
| UI-SRV-022 | 编辑代理字段、点击“探测”或提交表单 | 本地校验：SSH 隧道必须选隧道服务器；SOCKS5 必须填合法 `host:port`；密码非空而用户名为空时报错；错误在字段下方内联显示并进入底部可点击错误摘要，摘要项聚焦对应字段；探测与保存携带全部代理字段，编辑态探测且密码留空时附带 `serverId`；探测结果按当前草稿展示 reachable/error/OS/架构/特权，代理草稿变化后旧结果标记过期 | 本地校验失败不发探测/保存请求；服务端返回代理错误码时就地展示本地化错误，不得匹配英文消息文本；探测失败保留输入且不创建记录；切换为直连后探测/保存不得再提交隧道或 SOCKS5 字段 | 合法草稿的探测请求含 `proxyMode`/`tunnelServerId`/`socks5Address`/`socks5Username`/`socks5Password`（编辑留空时另含 `serverId`）；保存请求不含 `serverId` |
| UI-SRV-023 | 保存代理配置或重新打开编辑 | 保存成功后关闭表单并刷新详情；重新打开编辑时回显模式与隧道服务器/SOCKS5 地址、用户名，密码只以“已设置/留空保持原密码”表达；选择直连保存成功后再次编辑显示直连且无代理残留，`socks5PasswordSet` 等派生状态与详情一致 | 保存失败保持弹窗与输入；代理字段服务端校验失败按错误码展示且不关闭弹窗；切换直连不得清除基础连接字段 | 连续“设置密码→重开不回显→留空保存后 `socks5PasswordSet` 仍为 true→切直连保存后 `socks5PasswordSet` 为 false”可复现 |
| UI-SRV-024 | 查看服务器详情连接区或列表连接提示 | 概览「连接」区与状态条展示当前模式：`直连` / `SSH 隧道 · <服务器名>` / `SOCKS5 · <地址>`；`proxyInvalid=true` 时以警告样式展示并翻译 `proxyError`（`tunnel_missing`/`cycle`/`chain_too_deep`/`socks5_address_missing`/`socks5_address_invalid`），用户可用详情页头既有「编辑」入口重新指定（不新增独立入口）；列表在综合状态旁以 `SSH`/`SOCKS5` 短标签提示，tooltip 为“经代理连接”或失效原因，失效时 warning 色调，且不替代综合状态/稳定性徽标 | `tunnelServerName` 为空时回退显示 `tunnelServerId`，两者皆空才显示“未知服务器”，不得留空；直连时该行显示“直连”（不隐藏，便于确认当前连接方式）；不得把代理失效并入综合状态徽标或据此误报主机健康；新增文案 en/zh-CN 同键（见 `i18n-translation-status.md`） | 删除隧道服务器后详情出现 `proxyInvalid` 警告与本地化原因；重新指定隧道服务器并保存后警告消失；切换语言后模式与错误文案随之切换 |
| UI-SRV-025 | 打开「网络与安全 → 网络探测」子视图（含深链 `?tab=network-security&subtab=probes` 与旧 `?tab=probes`） | 该子视图只在详情存活期内缓存，首次激活按需请求 `GET /api/v1/servers/{id}/network-probes`；页头显示最近采样时间、手动刷新与共享自动刷新选择（关闭/10 分钟/30 分钟/1 小时，默认 10 分钟；仅当前子视图激活且页面可见时刷新）；探测节点以表格展示目标服务器名、目标地址、平均延迟、丢包率、发送/接收、采样时间与状态徽标（状态由 `error`/丢包/`stale` 派生：正常/降级/已过期/失败）；非探测节点（`enabled=false`）展示「该节点不是探测节点」空态与去设施配置的入口（`/applications/facility-apps/network-probe`），而不是空表格或伪造 0 值；探测节点但无结果时显示表格空态；旧 Agent 不刷新时保留最后一次结果并标 stale（`FAC-PRB-006`） | 切换服务器/离开子视图取消旧请求且迟到结果不得串节点；失败显示子视图内局部错误与重试，不得清空其他 tab 快照；不得把缺失延迟/丢包显示为 0（失败行的延迟显示不可用）；不得影响服务器综合状态；自动刷新在子视图非激活时跳过 | 深链恢复网络探测子视图；非探测节点空态可直达设施页；`ProbesTab` 模型/组件测试（`probeOutcome` 派生、空态、请求竞态）与 typed client 测试 |
| UI-SRV-026 | 查看概览指标图表（含悬停、双序列、阈值与空档） | 四张图均展示可见时间轴（1h/6h 用 `HH:mm` 本地时间刻度、7d 用 `MM-DD`）和带单位的 Y 轴（百分比 0–100% 或自适应 B/s），使用虚线网格；单序列加淡面积填充，双序列展示图例与独立颜色；悬停显示十字准线与包含完整时间、各序列名称和值的 tooltip；图下展示平均/峰值；CPU 画 80%、内存画 90% 阈值参考线（`- -` 虚线）；图表颜色、网格和文字必须从 `--panel-*` token 读取并响应 `data-theme`/`data-scheme` 切换；每个图表提供本地化 `aria-label` | 不得用颜色作为双序列唯一区分；不得补 0 伪造缺测；不得隐藏坐标轴/时间刻度导致无法读数；不得为图表引入装饰性循环动画；`prefers-reduced-motion` 下不播放动画；加载/错误不得遮挡状态条与连接区 | `metricsModel`/`utils/metrics` 单测覆盖范围→桶、统计、格式化、空值；1600/1280/窄屏与深浅色截图复核坐标轴、图例、阈值可读；悬停 tooltip 值带单位 |
| UI-SRV-027 | 查看服务器概览首页的延迟与丢包 | 概览首页在指标图表之后展示「延迟与丢包 · 最近 24 小时」面板：**不再有探测源 Select**，也不加载探测配置/服务器候选；面板加载 `GET /api/v1/facility-apps/network-probe/results?view=targets&history=1` 并取 `nodes` 中当前 serverId 的条目（即**聚合所有探测源到本服务器**的结果）；使用与指标图表一致的 `MetricChart` 折线图展示最近 24h 延迟（ms）与丢包率（%），并显示聚合延迟/丢包、状态徽标（正常/存在丢包/已过期/无数据）与最近采样时间；无结果时显示“无数据/结果不可用”提示且图表仍保留空态；刷新失败时保留旧数据并给出提示。 | 不得恢复探测源 Select 或按单一探测源读取；不得把延迟/丢包继续藏在「网络与安全 → 网络探测」子页；不得用彩色分段历史条替代折线图；不得把缺失值画成 0；聚合读取失败不得影响指标图表、连接信息或页面其它区域；首次失败显示局部错误空态，已有数据时保留旧数据且迟到响应不覆盖新数据。 | 服务器概览面板请求携带 `view=targets&history=1` 且按当前 serverId 取聚合条目；无结果/无历史时图表空态可见且无 0 值曲线；`ProbeOverviewPanel` 组件测试覆盖加载、聚合当前值、无数据/无历史空态、无选择器、首次错误空态与刷新失败保留旧数据。 |

## 6. SSH 凭据 `/credentials`

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-CRED-001 | 列表搜索、翻页、选择 | 服务端分页/q；URL 恢复 search/page/credential；表格显示名称、类型、用户名、引用数和更新时间；点击「详情」或名称列覆盖列表并按需加载 key summary；详情单列纵向分节（密钥摘要 + 引用），不得短侧栏左右分栏；页头 actions 含「编辑」（与行内编辑同一表单） | 列表/详情错误分别显示重试；服务器列表失败不得泄露或伪造引用；详情打开时列表保持挂载但不可交互；不得依赖整行点击 | 选择变化只展示对应详情，私钥材料正文从不出现在详情；页头编辑可打开表单 |
| UI-CRED-002 | 新建密码凭据 | name、username、password 必填；成功后打开新凭据覆盖详情并刷新列表 | 密码不得出现在列表、toast 或日志 | 空 secret 无请求，保存后仅显示元数据 |
| UI-CRED-003 | 新建私钥凭据 | name、username、private key 必填，passphrase 可选；large 弹窗用 CodeEditor | 无效/空私钥不提交；密钥正文不回显到详情 | 成功后详情仅展示 comment/algorithm/bits/fingerprint |
| UI-CRED-004 | 编辑凭据 | 预填非 secret 字段；类型未变时 secret 留空表示保留；类型切换必须重新提供新类型 secret，并清旧 secret 输入 | 不得以空字符串覆盖现有 secret；passphrase 仅非空才发送 | API payload 单测证明编辑空 secret 字段被省略 |
| UI-CRED-005 | 测试凭据 | 只有存在引用服务器才启用；弹窗列出引用服务器并选目标，实际调用该服务器 test | 无独立凭据 test 接口；无引用不能打开/发送请求 | 成功提示目标服务器名并关闭，失败保留选择以重试 |
| UI-CRED-006 | 删除凭据 | 确认弹窗列出引用影响（本地化 impact 文本含引用服务器列表）；存在引用时禁用确认按钮并引导先解除；确认期间锁定重复提交 | 前端引用计算与后端冲突任一都应阻止删除 | 无引用成功后刷新并关闭详情；有引用时点击不会发 DELETE |

## 7. 资源页面（已废弃，能力并入服务器详情 tabs）

> `web/src/views/resources` 与 `/resources/*` 路由、资源导航分组已删除；下列编号保留以供追踪，其可观察能力改由服务器详情的资源 tabs 承接：tab 框架与缓存见 `UI-SRV-017`（全局约定 `UI-SHELL-062`），资源表格与行操作见 `UI-SRV-018`。原条目中的分页/筛选/空错态/两阶段等待等要求继续以对应新条目与全局约定为准。

| 原编号 | 原能力 | 替代 |
| --- | --- | --- |
| UI-RES-001 | 资源页服务器上下文选择 | UI-SRV-017（服务器由详情上下文固定） |
| UI-RES-002 | 资源 GET 失败或能力不足 | UI-SRV-018（能力阻断与失败重试） |
| UI-RES-003 | packages 搜索与勾选 | UI-SRV-018（勾选只对当前快照，批量栏约定不变） |
| UI-RES-004 | 刷新包元数据 | UI-SRV-018（POST→任务轮询→GET） |
| UI-RES-005 | 升级已选/全部包 | UI-SRV-018（全部升级 danger 确认） |
| UI-RES-006 | 查看容器 | UI-SRV-018（容器表格列与托管提示） |
| UI-RES-007 | 容器 start/stop/restart/delete | UI-SRV-018（确认、托管影响、逐项 loading） |
| UI-RES-008 | 打开容器日志 | UI-SRV-018（Dialog 内 500 行） |
| UI-RES-009 | 镜像刷新/拉取 | UI-SRV-018 |
| UI-RES-010 | 镜像升级应用 | UI-SRV-018（applicationIds 去重集合） |
| UI-RES-011 | 删除镜像或清理未使用镜像 | UI-SRV-018（danger + 强制勾选） |
| UI-RES-012 | 网络首次打开自动刷新且只读 | UI-SRV-018（网络表格只读、删除禁用） |
| UI-RES-013 | 存储卷首次打开/刷新 | UI-SRV-018（stale 且无快照自动刷新） |
| UI-RES-014 | 删除卷或清理未使用卷 | UI-SRV-018（danger + 强制勾选） |

## 8. 防火墙与 Fail2Ban（已废弃，能力并入服务器详情「防火墙」tab）

> `/resources/firewall`、`/resources/fail2ban` 与 `/security/*` 路由已删除；防火墙能力成为服务器详情的一级 tab（内含 UFW / Fail2Ban 子视图，Fail2Ban 仅 dev 可见）。下列编号保留以供追踪，替代项为 `UI-SRV-019`。

| 原编号 | 原能力 | 替代 |
| --- | --- | --- |
| UI-SEC-001 | 打开 firewall/fail2ban 或换服务器 | UI-SRV-017（服务器由详情上下文固定） |
| UI-SEC-002 | 查看 UFW | UI-SRV-019（不得出现安装/启用入口） |
| UI-SEC-003 | 新增 UFW allow rule | UI-SRV-019 |
| UI-SEC-004 | 删除规则 | UI-SRV-019（受保护端口/受管规则由后端拒绝） |
| UI-SEC-005 | 非 dev 访问 Fail2Ban | UI-SRV-017（Fail2Ban 子视图仅 dev） |
| UI-SEC-006 | 查看/编辑 Fail2Ban | UI-SRV-019（visual/YAML 切换与高级配置警告） |
| UI-SEC-007 | 保存 Fail2Ban 草稿 | UI-SRV-019（仅有变化才启用） |
| UI-SEC-008 | 安装、启用/接管、释放 Fail2Ban | UI-SRV-019（接管勾选、danger 确认、真实 taskId） |

## 9. 应用列表、详情与运行操作

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-APP-001 | `/applications/apps` 搜索、翻页、选择 | 服务端分页/q；URL 恢复 search/page/application；表格显示名称、**启停状态**、镜像引用、健康/异常/部署中三计数（**仅展示 count>0 的桶**）、镜像更新与更新时间；点击「详情」或名称列覆盖列表；规划失败以行内告警显示“部署被阻塞”；`reconcileStopped` 行内显示“需人工处理”；主徽标不得显示部署中/系统核实中/已部署/失败 | summary 缺 imageReference 时仅按行补详情并在该行显示骨架；补齐失败显示不可用，**不得**用 `jobId`/`panel-<name>` 容器名冒充镜像；失败不得把整表清空为假空态；不能将运行实例存在解释为最新部署成功；不得依赖整行点击；三计数之和可小于 instanceCount（仅禁用应用上已收敛 stop/purge；已启用不得因 stale stop desired 省略为 0 异常）；不得渲染 “0 健康/0 异常/0 部署中” 徽标 | 行级补载互不阻塞，选中项完整详情/runtime/files 按需请求；启停与告警按 `APP-PLAN-001`、三计数按节点归类 |
| UI-APP-002 | 查看 Runtime tab | 显示健康/异常/部署中三计数（详情页头徽标与 Runtime 汇总卡均**仅展示 count>0 的桶**）及每服务器节点卡片（观测态、容器、`lastError`）；各节点 Job 显示真实状态、`last_stage` 或无进度时的“系统核实中”、attempt、nextRunAt、错误码/消息/详情与 operation 深链；独立显示规划失败原因与 reconcile 告警；不得再渲染应用级单一「当前部署」主块；详情单列，页头 actions 含「编辑」（进入 `/applications/apps/:id/edit`）与 `ActivityLink` | runtime 失败 toast 且详情内 EmptyState 可重试；详情/files 加载失败用 `detailError` EmptyState 重试；换应用清旧 runtime/错误；节点终态 failed 不得消失；无 Job 的规划失败也必须显示；`execution_monitoring` 不得渲染为部署失败危险块；全为 0 时不占位展示零计数卡 | 节点观测/Job 文案一致；任一节点活跃 Job 或当前规划错误基线 3 秒有界轮询，按最早 nextRunAt 对齐，隐藏暂停，最多 24 次或 2 分钟；新 runtime 无规划错误时清除旧 DTO 告警；`tab` 查询与 runtime/routes/files/data 双向同步；页头编辑与行内编辑同一路由 |
| UI-APP-003 | 查看 Routes/Files/Data tab | Routes 显示域名、目标端口、来源服务器名和 paths；Files 只列已提交文件并支持 blob 下载；Data tab 承载持久化数据节点选择与下载/恢复；URL `tab=routes\|files\|data` 可深链恢复（缺省 runtime） | 无路由/文件有专属空态；详情/files 失败走详情作用域错误重试；服务器名未知才回退 ID；关闭详情清 `tab` | 文件名和下载名符合 kind/contentType，下载带 auth |
| UI-APP-004 | 点击行内同步/部署 | 提交人工 deploy；实际规划时显示真实 operationId/deploymentId 并刷新，`noChange=true` 时明确提示已是期望状态 | 不得被自动退避静默吞掉，不得把 no-change 或请求接受宣称为部署完成；失败后也重载列表/详情/runtime，使持久化规划诊断立即可见 | toast 与实际响应字段一致，operation 深链可恢复后续状态；修正后计划成功或已满足期望时清当前告警、保留失败历史 |
| UI-APP-005 | 行内停用应用 | enabled 时可点，先 danger 确认后调用 stop；成功只提示请求接受并刷新 | 已停用禁用；失败保持应用状态 | 取消无请求，成功后状态来自重载而非前端假改 |
| UI-APP-006 | 行内检查并更新镜像 | 仅 `imageUpdateAvailable` 时启用；提交 update 并按两阶段反馈 | 无更新时不请求 | 返回 ID 的 toast 正确，刷新后徽标由后端决定 |
| UI-APP-007 | 点击行内“容器日志” | 打开 Dialog 并加载该应用实例；按服务器名/容器名选择目标，单个可读实例自动加载，多个等待选择；请求携带 instanceId、tail=240，正文内部滚动并支持手动刷新/刷新实例列表 | 已退出但存在的容器仍可选；无实例、不可读、空日志和读取失败分开；失败 toast 加就地重试；切换实例/应用/关闭时取消旧请求且迟到结果不串日志 | 只请求选中实例，不通过任务或协调记录代替容器日志；不定时轮询，长日志不撑开页面 |
| UI-APP-008 | 打开协调记录 | 携带当前 application / facility resource filter 跳 `/events`；已有 operationId 时打开对应事件详情；节点 operation 若有 `executionId` 一并带入以便打开执行证据过程；诊断输出经「查看日志」进 `/logs?eventId=`（默认 `level=info,warning,error`）；监控态展示 `monitorReasonCode` 与证据/宽限截止（若 API 返回） | 不从全局相邻事件猜测因果，不直接展示原始 ID 作为用户标题 | 目标页面筛选与当前应用对应，本次部署入口定位同一 operation，并可跳到配对 execution 日志 |
| UI-APP-009 | 行内删除应用 | 普通应用显示影响说明并 danger 确认；存在持久化配置/历史位置时先补载完整详情，使用 danger 强制勾选确认，明确说明将永久删除所有已记录节点的数据，并向后端传确认参数 | 取消零请求；未确认或清理失败不移除对象；补载详情失败不进入确认 | 持久化删除只有勾选后发出，成功后重载列表并关闭覆盖详情 |
| UI-APP-010 | 下载持久化数据（详情上下文面板） | 展示历史数据节点选择器；单节点自动选择，多节点要求用户选择，携带 serverId 走授权 blob 下载 | 无记录位置或未选择节点时禁用；下载失败不生成损坏文件 | 未 apply 与已移除部署节点的数据均可按节点下载，按钮有单独 loading |
| UI-APP-011 | 选择 zip 恢复持久化数据 | 文件选择后必须经 danger+强制勾选确认，再 multipart restore | 取消选择/确认不上传；没有 persistentPath 禁用 | 仅确认后发一笔请求，反馈只承诺已接受 |

### UI-EVT-004 进程事件 `/events`：精简且利于排障

- **前置**：认证用户进入 `/events`；旧 `/operations`、`/tasks` 重定向到 `/events`（保留 query）；`/activity` 按 `UI-EVT-007` 分流。
- **动作**：看 AppDB **过程**行（一件事：如部署 Agent、一次执行），含 state/module/level、失败摘要，并跳到 JSONL 日志定位原因。
- **结果**（排障路径必须短）：
  1. 列表只展示真正的过程，不得把瞬时诊断（如 `agent.connected` / `agent.disconnected`、stream timeout）当作事件行。列表主列必须同时可读 **title + summary + state**（summary 不得被裁空或只显示 module）；详情保留状态、summary、模块/来源/时间、`operationId`/`executionId`，以及「查看日志」；不得展示 event executor 的 steps 树、时间线、上下文或证据下载。过程行文案须符合 `ENG-EVT-010`。
  2. 「查看日志」优先当前行 `eventId` 进入 `/logs?eventId=`，默认带 `level=info,warning,error`（里程碑 info 可见；进度噪声应为 debug）。若同行同时有配对的 intent 过程与 execution 证据过程，详情须提供第二入口（协调过程日志 / 执行证据日志）。仅有 `operationId`/`executionId` 时先在 `/events` 解析或筛选。
  3. 应用协调入口进 `/events`；节点 operation 深链须带齐 `operationId` 与可用的 `executionId`（`UI-APP-008`）。
  4. 产品读 API 为 `/api/v1/events*`（legacy `/api/v1/log-contexts*` 仍可用）；不得再调用已停注册的 `/api/v1/activity/*`。
- **失败**：不得用 trigger reason 冒充失败摘要；不得把机器枚举码当作用户标题。
- **不变量**：原始 JSONL 输出不翻译、不改写；不得恢复 `/activity` 双投影或日志关联工作台。
- **验证**：`activityRedirect`、`IntentRecordsPage`（events 列表 title/summary + View logs + 双日志入口）、eventId→logs 深链。

> `UI-ACT-001` 已废弃，由 `UI-EVT-004` 与 `UI-LOG-001` 替代。

### UI-LOG-001 诊断日志 `/logs`：JSONL 行

- **前置**：认证用户进入 `/logs` 或经 `/events` 深链；旧 `/activity` 带 `eventId`/`streamId` 重定向到此。
- **动作**：按 level 与可选 eventId 筛选 JSONL 行。
- **结果**：默认全局扫描近期 `panel-*.jsonl`（优先新文件）；列表展示时间、级别、eventId、msg（无 state）；瞬时系统观测（agent 连接/断开、超时等）只出现在此页，不进 `/events`。有结构化错误时分开展示 errorCode/error/detail。`eventId` 仅作可选筛选，不是进入页面的前置条件。不得做时间线、证据下载、技术 JSON 或覆盖式详情。产品读 API 为 `/api/v1/logs`（`eventId` 可选）或 `/api/v1/events/{eventId}/logs`。
- **失败**：无匹配行时展示空态；查询失败保留错误与重试。不得伪造行数据。
- **验证**：`views/logs/index.test.ts`、logs API client 测试。

> `UI-ACT-001` 日志部分已废弃，由本条目替代。

### UI-EVT-005 与 UI-LOG-002 列表首次加载

- **前置**：认证用户进入 `/events` 或 `/logs`，URL 可能带完整查询条件，也可能缺少 `from`（events）或 `eventId`（logs）。
- **动作**：页面首次挂载。
- **结果**：`/events` 缺少 `from` 时使用最近 24 小时并写回 URL；`/logs` 缺省 `level` 使用 `info,warning,error`（可写回 URL），无 `eventId` 时直接加载全局日志行。
- **失败**：默认时间写回不得替代、延迟或重复首次列表请求；失败保留错误空态、toast 和重试入口。
- **不变量**：首次列表加载不依赖定时器，不启动轮询。
- **验证**：logs/events initial-load 覆盖无 `from`、默认诊断等级和显式范围。

> `UI-ACT-002` 已废弃，由 `UI-EVT-005` / `UI-LOG-002` 替代。

### UI-EVT-006 与 UI-LOG-003 只按需刷新

- **前置**：用户浏览进程事件或日志行，可能保留其它页面的全局自动刷新设置。
- **动作**：首次进入、手动刷新、修改查询条件、选择对象或翻页。
- **结果**：仅这些动作读取对应 events 或 logs API；不提供定时刷新开关，不读取或探测新数据，不显示总条数、总页数、错误/警告计数或新数据提示。cursor 分页在总数不可用时必须隐藏摘要与页码（`PaginationBar` `mode="cursor"`，不得用 `total=0` 伪造「0-0 of 0」或「1 / 1」）。
- **失败**：查询失败仍显示错误与手动重试；旧筛选响应不得覆盖当前结果。没有自动错误重试或页面重新可见时的隐式轮询。
- **不变量**：列表保持 cursor/after 分页；events 手动刷新/导出走 `/api/v1/events/export`。页面不调用已停用的 activity summary/tail/operation 详情树。
- **验证**：`views/logs/index.test.ts`、`IntentRecordsPage.test.ts`、`PaginationBar.test.ts` 覆盖首屏一次请求、游标翻页、未知总数隐藏摘要与迟到结果隔离。

> `UI-ACT-003` 已废弃，由 `UI-EVT-006` / `UI-LOG-003` 替代。

### UI-EVT-007 `/activity` 与旧路由兼容重定向

- **前置**：旧书签或深链打开 `/activity`、`/operations` 或 `/tasks`，可能带 `view`、`operationId`、`eventId` 等 query。
- **动作**：路由解析。
- **结果**：`/operations`、`/tasks` 重定向到 `/events` 并保留筛选/选中参数；`/activity` 中 `view=operations` 等同；带 `eventId`（或 legacy `streamId`）重定向到 `/logs` 并补 `level=info,warning,error`；仅 `operationId`/`executionId` 时重定向到 `/events`；其余缺省重定向到 `/logs`。
- **失败**：不得落到空白页或仍渲染旧双 tab Activity 页。
- **验证**：`activityRedirect` 单测。

> `UI-ACT-004` 已废弃，由本条目替代。

## 10. 应用创建/编辑器

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-APP-012 | 进入 create 或 `:applicationId/edit` | 只加载必要应用/设施/文件清单到本地草稿；**不**创建 edit-session；加载完成前不渲染可编辑表单，保存禁用 | 已提交定义/清单加载失败就地显示错误和重试 | 成功后表单与文件列表来自本地快照；重试重新拉取已提交数据并清空 pending |
| UI-APP-013 | 编辑连续配置流 | 依次完整展开：身份、运行源、网络访问、容器环境、存储与挂载、部署目标、应用文件；网络、环境、存储必须为同级独立面板并各自提供操作与空态；正文内部滚动、右侧摘要 sticky/下移 | 不得把环境变量与挂载混入同一面板，不得恢复 YAML 源码模式、分页卡片或分区隐藏；窄屏单列无横滚 | 所有区块在同一编辑正文可连续到达，网络、环境、存储的标题和内容边界可独立辨认 |
| UI-APP-014 | 编辑基本/运行字段 | name、enabled、image、CPU、memory、命令参数数组、privileged 任一变化标脏并清过期诊断 | CPU/memory 非数字、name/image 等必填错误阻止保存；命令新增项默认空 | 右侧 diff/dirty 立即更新，序列化保留未被 UI 覆盖的 spec 字段 |
| UI-APP-015 | 新增/编辑环境变量或端口 | 弹窗草稿与主草稿隔离，保存才写入；env 含 key/value，value 可从目录在当前光标/选区插入当前应用或其它应用容器占位符；port 含 label/container/host port | 目录失败只禁用插入能力，不阻止手工编辑；取消不得污染主草稿；删除/保存均标脏 | model 测试证明 dialog draft 独立，插入测试覆盖光标与替换选区，列表摘要更新 |
| UI-APP-016 | 新增/编辑挂载 | 支持 persistent/volume/host/file/panel_file/storage_share，含 source/target/mode/readOnly；file 仅从**本地可见**应用文件 name 下拉选择（已提交∪pending−删除），storage_share 从已启用设施服务器选来源 | 应用文件为空时提示先添加；旧 file 引用已不存在时保留原值并标记失效、禁止再次保存；storage share 未配置显示配置入口且无来源不能保存；该类型不显示 mode | file 与 storage_share 保存后 source 均使用稳定值而非展示文案，取消无改变 |
| UI-APP-017 | 新增/编辑应用反向代理 | 配置 domain、targetPort、paths、AnyAccess；来源服务器由部署目标与网关自动推导，只读，不允许手填 | primary_backup 无来源优先级时报错/阻止提交；domain/port/path 无效阻止最终保存 | 保存生成规范小写域名、数值端口和稳定 originPriority |
| UI-APP-018 | 配置 AnyAccess | 可选全部/指定 relay 网关、round_robin/ip_hash/primary_backup；主备通过上下移动明确优先顺序 | relay 只能选网关且不能与 origin 重合；移除部署/网关后无效项被清理 | payload 仅在启用且选择指定时包含 relayServerIds |
| UI-APP-019 | 新增或编辑代理 path | 打开既有 path 时必须完整回显 path、gzip、请求体上限、连接/读/写超时、buffering、WebSocket mode、自定义请求/响应头；WebSocket 只提供 auto/on/off 单一选择器，编辑副本与父规则隔离 | 数值非负；空 header 行过滤；payload 不得出现旧 `webSocket` 布尔字段；不得直接用浏览器深拷贝 API 复制 Vue reactive Proxy；取消子弹窗返回代理弹窗且不写主草稿 | 选择 auto/on/off 后 payload 仅含 `options.webSocketMode`；保存回父弹窗后 path 摘要可见，最终保存才标主草稿脏 |
| UI-APP-020 | 管理应用文件 | 统一 AssetFileManager；文本/普通/归档变更写入**本地 pending**；文本「保存」只更新 pending；下载已提交文件走正式 content API，pending 文本从内存读 | 文件名拒绝 `/`、`\\`、`..`、控制字符；替换必须保留原 name；编辑期不得调用 session 文件 API | 列表=已提交基线⊕pending；页面保存时才 flush |
| UI-APP-021 | （已并入保存）原 Preview | 不再提供独立 Preview 主按钮 | — | 诊断由保存流水线在 preview 步展示 |
| UI-APP-022 | 点击保存 | 本地校验→begin（编辑无客户端草稿/创建带初始草稿）→patch→flush pending 文件→preview→无 blocking diagnostic 才 commit→清理会话；保存期间整编辑器阻塞且不可离开 | 冲突/失败 discard 临时会话、保留本地草稿并 toast；不重复提交 | 成功按 applyRequested 区分提示，选中新应用回列表 |
| UI-APP-023 | 取消、返回、路由跳转或关页 | 任意脏字段/本地 pending 触发放弃确认和 beforeunload；明确放弃后才离开；保存中拒绝离开；footer 取消与页头返回均明确 `push` 到 `/applications/apps`（编辑时带 `application` 选中） | 返回目标必须明确为 apps 列表，不得使用 `router.back()` 或不稳定历史栈 | 确认后丢弃本地草稿；无进入时创建的 session 需 discard |

## 11. 设施目录与入口代理

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-FAC-001 | 打开设施目录 | 固定展示 reverse-proxy 与 storage-share 卡片、类别/可用状态、异步统计；管理进入对应详情 | 不调用不存在的设施 list API；统计加载用骨架 | reverse-proxy 显示路由/节点/资产，storage-share 显示分区/服务器/根目录数 |
| UI-FAC-002 | 目录点击“协调/同步” | reverse proxy 调 reconcile；storage share 仅 enabled 时可调；反馈遵守两阶段 | 未配置 storage share 禁用；无 ID 不显示假 ID | 当前卡片独立 loading，完成后目录重载 |
| UI-FAC-003 | 查看 reverse-proxy 详情 | 显式两栏/两段：**代理平面**（网关成员、容器部署/ensure、`FacilityDiagnostics`）与 **配置记录列表**（设施域名记录 + 链到应用反代）；DNS 区列出各记录域名并标注来源；`tab` 可区分 `proxy`/`configs`/`dns` | 未知 facilityKind 显示不可用空态；加载失败可重试 | 不得再把多域名塞进同一编辑画布；深链可直达代理或配置列表 |
| UI-FAC-004 | DNS 同步为 pending | 非编辑态、可见标签每约 3 秒重载设施直至不 pending | 单条编辑中停止轮询；离开清 timer | 状态从 pending 更新且不覆盖单条编辑草稿 |
| UI-FAC-013 | DNS 失败/跳过/同步中重试与覆盖接管 | `pending`/失败/跳过/未同步均显示「重试 DNS」（长期「DNS 同步中」不得无操作入口）；失败（或冲突类错误）另提供「覆盖已有记录」，经危险确认后调用 `POST .../dns-sync`（`takeover`+`confirmTakeover`） | 未确认不得接管；请求中只锁当前域名操作 | 成功提示含 taskId 并刷新 DNS 状态 |
| UI-FAC-005 | 进入单条配置编辑 | 从配置记录列表点进；进入前拉取**该条**已提交记录与私有资产基线；**不**创建 edit-session；编辑器只含该域名/路径/本记录资产 | 配置加载失败显示重试；编辑中隐藏代理 ensure 的破坏性操作入口 | 一次只编辑一条；不得加载其他记录草稿 |
| UI-FAC-006 | 修改代理平面网关 | 仅从服务器选择并保存代理平面；移除网关时对仍引用该网关的配置记录给出阻断或剪除确认 | 不得留下指向非网关服务器的 origin/relay | 网关变更走代理平面 API，不 bump 各配置记录 version |
| UI-FAC-007 | 新增/编辑单条域名记录 | 校验合法、非重复域名且至少一台 origin；origin 只能从当前网关选；可配置 AnyAccess | primary 必须属于 origin；指定 relay 只能来自可用网关；已有记录的域名由 `recordId` 决定，编辑器只读（改名需删除后重建） | 错误字段就地显示；成功标脏仅本条 |
| UI-FAC-008 | 新增/编辑设施 path | path 必须以 `/` 开头；static 要选**本记录本地可见** asset；redirect 要合法 URL 与 301/302/307/308；proxy_pass 要 http(s) URL并可配 source mode | 各 ruleType 只发送相关字段；非法值就地阻止保存 | model 测试覆盖空/非法 path、redirect、proxy 和缺 asset |
| UI-FAC-009 | 配置设施 path 高级项 | 支持 gzip、请求体、超时、buffering、WebSocket、自定义请求/响应头；redirect 仅保留适用项 | 数值非负，空 header 过滤 | clone/编辑往返不丢 proxy_pass 和 options |
| UI-FAC-010 | 管理本记录私有资产 | 复用 AssetFileManager；变更写入本地 pending；文本「保存」只更新 pending；路由引用用 asset.name；已提交资产下载走 `GET .../static-assets/{assetName}/content?recordId={recordId}` | binary/archive 不可当文本编辑；替换保留 name；编辑期不得调用 session asset API；下载必须带本记录 recordId | 列表=本记录已提交⊕pending；不得显示其他记录资产 |
| UI-FAC-011 | 保存/取消单条配置 | 唯一主操作「保存」：本地校验→begin（绑定 recordId）→patch→flush pending→preview→commit→若 `applyNeeded` 再 config-plane apply；成功退出单条编辑回列表并刷新；取消脏草稿先确认 | 冲突 discard 会话并保留本地；apply 失败仍提示配置已提交；保存中整页阻塞 | 其他记录 version 不变；applyNeeded 决定是否提示已请求 reload |

| UI-FAC-014 | 打开设施目录 `/applications/facility-apps` | 主导航、路由标题与目录页头中的用户可见产品名称为「设施 / Facilities」（原「设施应用 / Facility Apps」，路由路径 `/applications/facility-apps` 不变）；目录卡片固定包含 reverse-proxy、storage-share 与 `network-probe`（Radar 图标、network 分类）、`metrics`（Activity 图标、observability 分类）；网络探测卡展示探测节点数、目标数与当前模式（`all`/`selected`），指标卡展示指标节点数（`all`→服务器总数、`selected`→选中服务器数）、采集范围与保留天数；目录加载时并行读取网络探测配置与指标设施配置，加载中显示占位；两张观测设施卡都不提供「协调/同步」按钮（无协调 Job），不得调用 reconcile API | 统计/配置失败只让对应卡片显示不可用或 `—`，不影响其他设施卡（指标配置读取失败置空即可，不得阻断目录）；未知 facilityKind 显示不可用空态 | 目录可点击进入网络探测与指标设施详情；`FacilityCatalog` 模型/组件测试与 Mock 路由测试（含 `GET /facility-apps/metrics`） |
| UI-FAC-015 | 进入网络探测设施详情/配置（`/applications/facility-apps/network-probe`，`/config` 可直达；两种入口复用同一组件） | 页面并行加载配置 `GET /facility-apps/network-probe` 与结果区聚合视图 `GET /facility-apps/network-probe/results?view=targets&history=1`（结果区见 `UI-FAC-018`）；配置区上下堆叠为两个独立区域（**「探测范围」**在上、**「探测配置」**在下；结果区为第三区域）：**「探测范围」**（标题 `applicationsPage.networkProbeScopeTitle`）只含模式 Select（`all` 全部节点 / `selected` 指定节点），`selected` 时用 `ServerMultiPicker variant="compact"` 以紧凑网格（`sm` 两列、`xl` 三列，不再一台一行占满整行）选择探测节点（候选来自服务器列表，含名称/地址/可达状态），`all` 时隐藏选择器；**「探测配置」** 区域（标题 `applicationsPage.networkProbeConfigTitle`）只含三个数值输入（`type=number`，带 min/max/step 与区间/默认值提示）与「保存」：探测间隔以**分钟**输入（默认 1、范围 1..60 分钟，`applicationsPage.networkProbeInterval / networkProbeIntervalHint / networkProbeIntervalInvalid`；加载时按 `max(1, 四舍五入(intervalSeconds/60))` 换算且最小 1，保存时换算 `intervalSeconds = 分钟 × 60`）、每次发包数 `pingCount`（默认 3，范围 1..10）、单包超时 `pingTimeoutSeconds`（默认 2，范围 1..10）；输入为空、非整数或越界时字段就地报错并禁用保存（不得静默夹紧或按默认值提交）；点「保存」提交 `{mode, serverIds, intervalSeconds, pingCount, pingTimeoutSeconds}`，成功后刷新配置与结果区；保存按钮仅在草稿（含三个参数）与已保存配置不同且非保存中时可用；加载失败显示页面级错误与重试 | `selected` 未选节点时本地阻止保存并就地提示，不发请求；参数非法时不发请求，服务端校验错误（`network_probe_interval_invalid`/`network_probe_ping_count_invalid`/`network_probe_ping_timeout_invalid` 等）按错误码就地展示；保存失败保留草稿；切换 `selected`→`all` 保留已选节点与参数以便切回，但保存时按模式归一化；加载中不得显示可编辑的旧配置 | 保存后重新进入回显归一化 mode/serverIds/intervalSeconds/pingCount/pingTimeoutSeconds（间隔按分钟回显）；`NetworkProbeFacility` 组件/模型测试覆盖分钟默认 1 与 1/60 边界、秒↔分钟换算、两区域渲染、紧凑网格选择器、脏检测、保存 payload `intervalSeconds=分钟×60`、selected 校验与失败保留 |
| UI-FAC-016 | （已废弃）查看网络探测结果矩阵 | “源×目标”横向矩阵与“探测源/探测目标”选择器均不再存在：横向矩阵在多目标/窄屏下产生横向滚动且信息密度差，前置选择器又让用户误以为需要人工指定探测关系；已由“直接展示所有服务器（按目标聚合）+ 24h 折线图”替代（`UI-FAC-018`）。后端默认矩阵读取契约仍按 `FAC-PRB-006` 保留供兼容/旧客户端使用，但设施详情必须使用 `view=targets&history=1` 聚合视图，不得再渲染矩阵或要求用户选择探测源/目标 | 不得同时保留矩阵、选择器与新聚合视图；不得用横向滚动兜底；不得把默认矩阵响应当作设施详情结果来源；不得恢复探测源/目标 Select | 设施详情 DOM 中不存在矩阵表头/横向滚动容器，也不存在探测源/目标选择控件；Mock 默认以 `view=targets` 聚合响应呈现 |
| UI-FAC-017 | 网络探测设施的滚动、窄屏与离开行为 | 中大型屏幕下页面外层容器填满页头外剩余视口，「探测范围」「探测配置」与结果区各自成独立卡片（三个区域，配置区不合并回一块）；结果区为**所有服务器卡片网格/纵向列表 + 每卡两张 24h 折线图**，长内容只在结果容器内部纵向滚动，不产生页面级滚动或横向滚动；窄屏单列可恢复页面滚动；保存按钮按草稿脏状态启用/禁用，未保存改动不得被静默保存；返回设施目录使用页面自身返回入口 | 不得用页面级滚动或横向滚动承载结果区；不得在无改动时提交保存；失败时不得把页面清空为假空态；不得回退为横向矩阵、选择器或历史条 | 深链进入详情/配置页状态一致；窄屏布局与内部滚动符合 `APP-UI-001`；`NetworkProbeFacility` 布局/滚动测试（或人工观察点） |
| UI-FAC-018 | 查看网络探测设施结果：所有服务器按目标聚合 + 24h 折线图 | 结果区保持为独立于「探测范围」「探测配置」的第三个区域，**不提供任何探测源/探测目标选择器、搜索或前置筛选**：挂载即加载 `GET /api/v1/facility-apps/network-probe/results?view=targets&history=1`，直接以纵向全宽卡片列表展示**所有服务器**（含没有任何探测结果的服务器，按服务端返回的稳定顺序）；每张卡片展示名称、地址、探测源数（`sourceCount`）、当前延迟、丢包率、状态徽标（正常/存在丢包/已过期/无数据，失败样本并入降级或过期语义）、最近采样时间，并用与服务器指标一致的 `MetricChart` 折线图分别展示该服务器的 24h 延迟（ms）与 24h 丢包率（%）（来自 `nodes[].history` 的 30 分钟跨源聚合桶）。结果区顶部显示服务器总数与“有结果”服务器数、最近采样时间；所有服务器均无探测源时提示“暂无探测源上报”。无结果或无历史时卡片与两张图表仍必须渲染并显示空态/「暂无历史」占位，不得隐藏卡片、不得把缺失值画成 0；保留手动刷新与自动刷新（自动刷新遵循全局刷新设置），刷新失败时保留上次成功数据并提示。 | 不得恢复探测源/目标 Select、目标搜索或横向矩阵；不得隐藏无数据服务器；不得把聚合失败/无源当成 0 延迟或 0 丢包；不得因结果区失败影响配置表单；迟到响应不得覆盖新数据；>100 台服务器时只允许结果容器内部滚动，禁止页面级横向滚动或整页滚动。 | 请求参数固定含 `view=targets`（刷新/自动刷新同样携带）与 `history=1`；卡片数量等于服务器总数（含无结果项）；探测源数、当前值、状态、采样时间与 24h 折线数据来自聚合响应；空历史/无结果时图表占位可见且无 0 值曲线；`NetworkProbeFacility` 组件测试覆盖全量卡片、无数据卡片、折线图、空态、>100 内部滚动与刷新失败保留旧数据（不再断言 Select）。 |
| UI-FAC-019 | 进入指标设施详情/配置（`/applications/facility-apps/metrics`，`/config` 可直达；两种入口复用同一组件） | 挂载即读取配置 `GET /api/v1/facility-apps/metrics`；页面上下堆叠为两个独立区域（**「采集范围」**在上、**「采集配置」**在下）：**「采集范围」**（标题 `applicationsPage.metricsScopeTitle`）只含采集范围 Select（`all` 全部服务器 / `selected` 指定节点；`selected` 时用 `ServerMultiPicker variant="compact"` 以紧凑网格选择节点，不再一台一行，`all` 时隐藏选择器）；**「采集配置」** 区域（标题 `applicationsPage.metricsConfigTitle`）只含保留天数数值输入（`type=number`，min 1）、采集间隔秒数数值输入（min 1）、清理计划 Select（`hourly`/`daily`/`weekly`）与「保存」；「保存」提交 `{mode, serverIds, retentionDays, collectionIntervalSeconds, cleanupSchedule}` 到 `PUT /api/v1/facility-apps/metrics`，成功 toast 并重新加载配置，按钮仅在草稿与已保存配置不同且非保存中时可用。中大型屏幕下页面外层容器填满除全局页头外的剩余视口，两个配置区域各自成卡片、长配置在配置卡片内部纵向滚动，不产生页面级滚动或横向滚动；窄屏单列可恢复页面滚动；返回设施目录使用页面自身返回入口 | `selected` 未选节点时本地阻止保存并就地提示，不发请求；保留天数/采集间隔为空、非整数或 <1 时就地报错并禁用保存（不得静默夹紧）；服务端校验错误按稳定错误码就地展示（`metrics_facility_mode_invalid`/`metrics_facility_servers_required`/`metrics_facility_server_not_found`/`metrics_facility_retention_invalid`/`metrics_facility_interval_invalid`/`metrics_facility_cleanup_schedule_invalid`）；保存失败保留草稿并 toast；配置读取失败显示页面级错误与重试（不得渲染可编辑的默认值冒充已保存配置）；无服务器时 `all` 仍可保存，但提示当前没有可采集节点 | 保存后重新进入回显归一化的 `mode`/`serverIds`/`retentionDays`/`collectionIntervalSeconds`/`cleanupSchedule`/`version`；`mode=all` 保存后 `serverIds` 为空数组；`MetricsFacility` 组件/模型测试覆盖默认值、脏检测、保存 payload、两区域渲染、紧凑网格选择器、selected 校验、数值边界、失败保留与错误码映射；Mock 路由测试覆盖 GET/PUT 与 `selected` 空集合 422 稳定校验错误 |

`UI-FAC-012`：入口代理按节点显示容器观测与部署结果、失败阶段、下次重试及活动入口；错误带原因与处理提示，技术诊断/本次运行日志放入内部可滚动折叠区。按需代理检查显示样本限制、节点/域名/上游、分类解释与下一步；空样本不表示全部路由健康，旧配置/离页响应不得覆盖当前结果。

## 12. 存储共享设施

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-STO-001 | 打开 storage-share 详情 | 加载配置和状态，显示 enabled/lastError、服务器数、live exports、分区及健康挂载；每 15 秒刷新状态 | 未启用不请求 status；状态失败保留配置且状态 unknown | 隐藏/卸载时停止 timer，手刷重载配置+状态 |
| UI-STO-002 | 切换健康/分区/设置 | 健康列每服务器 Agent/root/NFS/export 与关联应用；分区列来源、节点、路径、target、volume、mount 状态；设置走 `/config` 深链 | 从脏设置切 tab 必须先确认离开 | 返回详情后恢复用户请求的详情 tab |
| UI-STO-003 | 编辑服务器与 root | 至少一行有效 server+绝对 root；禁止重复 server、空格、`..`、`//`、尾 `/`；启用后已保存 server 的 root 只读 | 至少保留一行；新增行默认 `/opt/panel-shared-storage` | 错误逐行显示，无效配置无 PUT |
| UI-STO-004 | 保存设置 | 仅提交有效行、trim root 和当前 version；成功后退出设置页并回到 storage-share 详情 | 并发/version 冲突保留草稿且留在设置页；返回 lastError 额外 danger toast | dirty 清除后导航到详情；失败不离开设置页 |
| UI-STO-005 | 离开脏设置或关闭页 | 路由离开与 beforeunload 均保护；取消保留草稿，确认才导航 | 保存中也不得静默离开 | 三种入口（tab、返回、外部路由）行为一致 |
| UI-STO-006 | 点击同步导出 | 仅 enabled 时提交 reconcile，提示任务已接受并刷新 status | 禁用态不请求；不得宣称全部导出已生效 | 后续状态由 status 接口决定 |
| UI-STO-007 | 卸载设施 | 先确认影响；有引用应用时阻止并列出“去应用编辑解除”入口；无引用才允许 DELETE | 卸载保留历史分区数据；失败不清配置 | 成功 enabled=false、status 清空，引用存在时零 DELETE |
| UI-STO-008 | 下载/删除分区 | 下载授权归档，文件名含应用和服务器；删除若仍引用则阻止并引导编辑应用，否则 danger+强制勾选 | 失败不移除分区；取消无请求 | 成功删除后配置/status 均重载 |

## 13. DNS `/dns/domains`

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-DNS-001 | 搜索、翻页、选择域名 | 服务端分页/q；URL 恢复 search/page/domain；表格显示域名、provider 状态与更新时间；点击「详情」或名称列覆盖列表并加载记录；详情页头 actions 含「编辑」（与行内编辑同一域名表单） | 列表失败与空域名分离；切域名立即清旧记录并 abort；详情打开时列表保持挂载但不可交互；不得依赖整行点击 | 分享 URL 恢复相同域名与页码，迟到记录不串域名；页头编辑可打开域名表单 |
| UI-DNS-002 | 新建/编辑域名 | provider 固定 Cloudflare；新建必须 name+API token；编辑 token 留空保持当前；本地校验非空与基本域名形状，无效时字段错误并禁用保存 | token 永不回显/出现在列表；本地失败不请求；服务端错误保持弹窗 | payload 编辑空 token 时省略，成功打开新域名覆盖详情 |
| UI-DNS-003 | 加载记录 | 仅当前域名请求，详情内表格显示 A/AAAA/CNAME/TXT/MX、name、content、TTL、操作；标题栏展示快照时间与 stale 标记 | provider/记录失败在记录区显示错误+重试，并使该域名状态 danger；真实空列表用 no records；`lastRefreshError` 就地展示 | 错误、加载、空、内容四态互斥；行内操作只影响当前域名 |
| UI-DNS-004 | 同步记录 | POST refresh，显示 taskId，等待任务完成后重新 GET 当前域名 | 等待中切域名时旧结果不得覆盖；超时/失败保留错误重试 | 请求顺序正确且按钮只锁当前记录区 |
| UI-DNS-005 | 新增/编辑记录 | type、name、content、TTL、proxy mode；name 统一归一，空 TTL 回退 300；type/name/content 本地必填校验，无效时字段错误并禁用保存 | 本地失败不请求；非代理支持由后端校验；服务端失败保留弹窗/记录表 | 成功关闭并重载当前域名记录 |
| UI-DNS-006 | 删除记录或域名 | 分别显示目标和影响说明的 danger 确认；成功只刷新对应作用域 | 域名引用冲突由后端明确显示，不得乐观删除 | 取消零请求；域名删除成功清 selection |

## 14. 证书、自签证书与密钥资产

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-CERT-001 | 切换 domains/self-signed/keys、搜索、翻页、选择 | path 决定模式；page/search/selected 同步 URL；对应服务端列表使用 q，分页固定底部；列表为表格（domains：名称/覆盖域名/状态/到期；self：名称/类型/commonName/到期；keys：名称/类型/算法/指纹/到期），点击「详情」或名称列覆盖列表 | 切模式清 selection/page；列表错误与空态分离；能力不足的行操作禁用；不得依赖整行点击 | 三个深链刷新可恢复模式、查询与选中项；覆盖详情保留列表滚动与筛选 |
| UI-CERT-002 | 查看域名证书 | 覆盖详情显示覆盖域名、issuer、状态、到期、lastError 与是否签发中 | failed 优先 danger，再区分 expired/expiring | 徽标与 model 测试一致 |
| UI-CERT-003 | 签发或调整重签 | 选择已管理 DNS 域名，配置名称、根域、通配符、子域前缀并实时预览覆盖；重签预填已有覆盖 | name/domain/覆盖域名本地必填校验并禁用无效保存；去重/清空子域；只提示任务创建不宣称签发完成；服务端错误保留 | POST/PUT payload 的 prefixes 与预览一致，返回证书被选中 |
| UI-CERT-004 | 立即续签或删除域名证书 | renew 返回 202 与 taskId，页面用 `waitForTask` 等到终态再提示接受并重载；delete 先 danger 确认 | 请求失败或任务失败保留详情；签发中操作由后端拒绝时明确显示 | 任务成功后重载状态，取消删除零请求 |
| UI-CERT-005 | 生成自签 CA | name/commonName 必填，years 数值化并默认 5；本地字段错误并禁用无效保存 | 本地失败不请求；非法数值等由后端错误明确反馈 | 成功选中新 CA 并刷新 |
| UI-CERT-006 | 生成 self-signed leaf | 选择 CA，填写 name/commonName、DNS names、IP addresses、days（默认 90） | name/commonName/CA 本地必填；无 CA/非法 SAN 不得伪成功；服务端错误保留 | 成功详情显示 kind/fingerprint/expiry/SAN |
| UI-CERT-007 | 重签或删除自签资产 | 重签调用 renew 并选返回 ID；删除先确认 | 引用/父子冲突保留对象并 toast | 成功重载，状态与新到期时间一致 |
| UI-CERT-008 | 打开密钥资产页 | 过滤系统托管资产，仅列用户 CA/TLS/SSH；详情显示算法、fingerprint、reference/child count、expiry 与允许动作 | 系统资产只在设置的系统证书分区；列表不得包含私钥、公钥正文或 metadata | `canDelete/canReissue/canRegenerate/downloadKinds` 控制按钮 |
| UI-CERT-009 | 生成 CA/TLS/SSH | keys 模式页头提供生成 SSH/生成 CA/生成 TLS 入口，CA/TLS 填 name/commonName/algorithm/RSA size/validity，TLS 另选 parent CA 与 SAN；SSH 填 name/algorithm/RSA size/comment | 生成入口只属于 keys 模式（表格页头）；domains/self-signed 页头不得出现；本地必填 name（及 CA/TLS 的 commonName/parent），无效禁用保存；其他错误显式返回 | 成功打开新 asset 覆盖详情，不在 UI 展示私钥正文 |
| UI-CERT-010 | 单资产导入 | 选择 type/name/parent/算法；SSH 只编辑私钥材料，CA/TLS 用 tabs 分别编辑私钥和证书，默认打开私钥 | 导入失败在 Dialog 内保留结构化错误和材料；不得把 secret 写 toast | tabs/CodeEditor 可键盘操作，成功关闭并选中新资产 |
| UI-CERT-011 | 重签/重生成/删除资产 | SSH 用 regenerate，证书用 reissue；202 只表示已排队，页面 `waitForTask` 后再重载；根据 capability 禁用；delete 先 danger 确认并由后端做引用安全检查 | 引用中/有子资产时删除失败不移除行；任务失败不把旧材料显示成已更新 | 有 taskId 才显示，任务成功后重载能力与 fingerprint |
| UI-CERT-012 | 下载单资产材料 | 仅渲染 downloadKinds，逐项 blob 下载 | 失败不生成文件；不得经 JSON ApiClient 解析二进制 | 文件种类/名称与服务端 Content-Disposition 对应 |
| UI-CERT-013 | 导出资产归档 | 默认导出所选 asset，无选择则所有用户资产；密码必填，创建 export task 后 `waitForTask` 到 completed 再下载 | 系统托管资产不得进入集合；任务未完成不得下载；任务/下载失败明确 toast | 请求 assetIds 正确，下载归档可打开 |
| UI-CERT-014 | 批量导入 | 选择支持归档+密码，先 multipart preflight；显示冲突数/ready；无危险冲突可执行，`requiresDangerConfirm` 时必须 danger+强制勾选后 overwrite；执行返回 202 后 `waitForTask` 再重载 | 改文件清旧 plan；未 preflight 不出现执行；不得预先 `confirmDanger=true` | 危险执行 payload 仅确认后带确认标志，任务成功后提示真实 taskId |

## 15. 已废弃的可观测性路由

进程事件与诊断日志以 `UI-EVT-004..007`、`UI-LOG-001..003` 为准；下列编号保留为已废弃，不得再实现独立旧页。

| 编号 | 状态 |
| --- | --- |
| UI-ACT-001..004 | 已废弃：由 `UI-EVT-004..007` + `UI-LOG-001..003` 替代 |
| UI-OPS-001..004 | 已废弃：原 `/application-operations`；由 `/events` + `/logs` 替代 |
| UI-EVT-001..003 | 已废弃：原 `/system-events`；由 `/events` + `/logs` 替代 |
| UI-TASK-001..005 | 已废弃：原轮询任务中心；由 `/events` + `/logs` 替代，且不得恢复定时轮询 |

## 16. 设置 `/settings/*`

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-SET-001 | 进入任一设置分区或刷新 | 并行加载 runtime、server variables、version、system certs、可激活 TLS；分区由 path 决定且独立保存，无全局保存 | 某个辅助接口失败应保留其他成功数据并 toast；runtime 完全不可用显示重试 | 左侧路由可直达六分区，刷新不跳回 general |
| UI-SET-002 | 保存 General/Runtime | 只校验本分区保留项（容器上报间隔 >=1 等；event retention 若存在则 >= detail retention）；保存语言、日志等级、协调 trace 与清空事件日志，以已加载 runtime 为基底只合入本分区。指标保留天数、采集间隔与清理计划已移出设置页（`FAC-MET-006`、`SET-RUN-021`）：表单、默认值、校验、回填、dirty/保存负载和类型都不得再出现这三项；容器上报间隔仍可在此修改并按 `SET-RUN-014` 热更新 | 不得提交其他分区未保存脏值；非法数字不请求；不得把旧三项写回 `runtime_settings` 或指标设施配置；旧客户端 payload 中的三项按未知字段忽略 | 成功重新 hydrate，语言立即 setLocale 并更新 html.lang；`GET/PUT /settings/runtime` 响应/请求不含 `metricsRetentionDays`/`metricsCollectionIntervalSeconds`/`cleanupSchedule`，`containerReportIntervalSeconds` 仍回显保存值 |
| UI-SET-003 | 保存 Security | token expiration 从固定值选，remote timeout >=1；只合入安全字段 | 失败不覆盖 runtime 基线 | 请求中仅安全差异变化 |
| UI-SET-004 | 重置 JWT secret | secret trim 后至少 16 字符，先 danger 确认；成功更新 session、清输入并标记 configured | 短值就地错误；失败保持输入并防止登出假象 | 未确认无请求，成功后新 token/session 生效 |
| UI-SET-005 | 保存 Certificates/Panel HTTPS | 编辑 Panel domain、TLS 资产、ACME email、DNS delay>=0；TLS 选择来自 `/key-assets/tls`，内置 `panel-tls` 保存为空 ID | TLS 列表不得按域名/SAN过滤，不含 Agent 专用证书；domain trim+小写 | 请求仅合入 certificates/panel 分区，成功重新 hydrate |
| UI-SET-006 | 查看/重置系统证书 | 以表格展示 Panel Agent CA/client 和服务器 Agent server cert 的名称/归属、type/status/fingerprint/有效期；仅 canReset 行可点，先 danger 确认后提交任务 | 不得通过普通 key asset API 修改；不可重置项禁用 | toast 显示真实 taskId，列表后续刷新反映状态 |
| UI-SET-007 | 查看 System | 版本、channel、latest、updateAvailable 只读；branding title/subtitle 和 server variables 分别独立保存 | 版本不混入 runtime 保存；变量格式为 `[*]KEY=Name`，空行忽略 | 保存 branding 不提交变量，保存变量不提交 branding |
| UI-SET-008 | 配置备份导出 | 默认加密；关闭加密显示身份可恢复性警示；加密时密码必填，经统一 `ConfirmDialog`（impact + loading 锁）确认后提交 export 并显示 pending | 不得在此假装归档已可下载；失败不进入 pending | 未满足密码时按钮禁用，成功提示真实 exportId |
| UI-SET-009 | 选择还原文件 | FileUploadButton 选 `.panel-backup` 后先 preflight，显示 manifest 版本/文件数；用户另勾覆盖确认并再经 danger `ConfirmDialog`（impact + 强制勾选 + loading 锁）才 confirm restore | 更换文件清旧 preflight；无 preflight/未勾选不能恢复；确认中拒绝关闭 | 仅最终确认发送 multipart restore，成功显示 pending/restarting |
| UI-SET-010 | 保存 Agent download（`/settings/agent`） | 编辑下载基址、传输超时与「校验下载 TLS 证书」开关；只合入 agent 分区；超时校验 60..3600 秒，越界就地报错且不发请求 | 基址留空即表示关闭 HTTP 投递，文案必须说明回落到 SSH 上传；校验开关关闭时必须说明完整性仍由 SSH 下发的 sha256 保证 | 保存后重新 hydrate 且只回显归一化基址；保存本分区不得提交其他分区未保存脏值 |
| UI-SET-011 | 打开 `/settings/tailscale`、编辑认证密钥与 ACL 标签，或点击「应用 / 重连」 | 密钥为只写密码输入：留空保存表示保留已存密钥，已配置时显示「认证密钥已配置」徽标、未配置时警示节点无法加入 tailnet；标签按 `tag:name` 本地解析并对非法项就地阻断（错误文案列出具体标签）；保存只合入 `tailscale` 分区并重新 hydrate；「清除认证密钥」须 danger 确认后提交清除；容器状态区展示 available/running/loggedIn、主机名、版本、IPv4/IPv6、backendState、最近应用时间与 `lastError`；`available=false` 时显示不可用空态并禁用「应用 / 重连」 | 存在非法标签时不发请求；清除失败保留当前状态与输入；容器不可用时按钮禁用且不得伪造状态或轮询不存在的接口；「应用」只表示已请求协调，不得表述为 tailscaled 已登录 | 保存后只回显归一化标签与 `authKeyConfigured`（密钥框保持为空）；确认弹窗取消时零请求；不可用时展示需在 Seamark 之外配置的说明而不是错误码堆叠 |
| UI-SET-012 | 在 `/settings/general` 点击「清空事件与日志」 | 危险确认（impact + 强制勾选）后调用 `POST /api/v1/events/clear`；成功 toast；仅清除过程事件与 JSONL，不影响配置与资源 | 未确认零请求；确认失败保留按钮可用并 toast 错误 | 文案须说明与 Debug 全量运行数据清理的范围差异（`ENG-EVT-002`） |

## 17. 维护 `/maintenance/backup`

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-MAINT-001 | 进入维护页 | 独立 shell，不读取普通 session；export/restore token 分别存 `sessionStorage.panel.maintenance.export.token` / `.restore.token` | 普通 token 不得授权维护 API，维护 token 不得进入普通 client | 页面无 AppShell，关闭浏览器 session 后维护认证消失 |
| UI-MAINT-002 | 维护登录 | 用当前后端模式对应 token 登录并加载状态；当前模式由后端状态判断，用户不可手切 | export status 失败可尝试 restore status；401 清维护 token并回登录 | 登录后展示实际 mode/phase/capabilities，而非前端猜测 |
| UI-MAINT-003 | 查看状态和轮询 | 显示 phase、progress、revision、startedAt、restart support、manifest/error；按 pollAfterMs（默认2s）轮询 | completed/failed 或隐藏标签暂停；防重复 timer | 终态不再请求，手刷可立即更新 |
| UI-MAINT-004 | canStart/canSubmitPassword/canRetry/canExit | 只按 capability 显示相应动作；密码必填；成功提示“命令已接受”并继续轮询 | 接受不等于完成；失败保留状态/错误 | 页面按钮集合严格等于后端 capabilities |
| UI-MAINT-005 | 清除 pending restore | `canClearPending` 且 phase 非 applying 才显示，必须 danger 确认 | applying 阶段绝不提供；取消零请求 | 成功刷新状态，确认影响可见 |
| UI-MAINT-006 | 下载完成的 export | 仅 canDownload 显示，用当前 export token 进行授权 blob 下载 | 401 退出维护登录；失败不生成文件 | 文件名/内容来自响应，普通 auth header 不混入 |
| UI-MAINT-007 | 退出维护登录 | 停止轮询、清当前模式 token和状态，回维护登录表单 | logout API 失败也应完成本地清理 | 后续 status 请求不再发送旧 token |

## 18. 诊断 `/debug`

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-DBG-001 | 进入诊断页 | 并行加载 runtime、event executor 快照（`GET /api/v1/debug/tasks`）、databases 与 pprof 状态；三类诊断默认每 8 秒各自轮询，可暂停/恢复/手刷，隐藏标签暂停 | 各请求独立防重入；并发/迟到响应不可覆盖同类新数据 | 顶部显示聚合 live/stale 和最近 collectedAt，任一区块不阻塞其他区块 |
| UI-DBG-002 | 任一诊断刷新失败 | 该区块有最后成功数据时继续展示并标 stale，同时 toast；无旧值时仅对应 tab 显示失败空态+重试 | 不得清空其他区块或让单接口失败阻塞其他接口完成 | 失败接口恢复后 stale 消失且更新时间前进 |
| UI-DBG-003 | Runtime tab | 展示 uptime、goroutines、heap、PID、Go version、OS/arch、CPU | 空/未知值使用统一 fallback，不显示 `[object Object]` | 数值与 runtime 响应字段一致 |
| UI-DBG-004 | Events runtime tab | scalar runtime metrics 与 event executor 注册定义分开；definitions 用可滚动 Table 展示 kind/actions/concurrency/retries/stale（无 periodic interval 列） | definitions 数组不得字符串化；无定义有明确空提示 | 任意对象定义都以列呈现 |
| UI-DBG-005 | Database tab | 展示健康数/总数/used 汇总；每数据库显示大小、used/free、健康/错误和表行数/大小 | 单库错误以 danger 状态保留其他库 | 大表清单在卡片内部滚动 |
| UI-DBG-006 | 切换 pprof | Switch PUT enabled，期间禁重复；开启后显示本机 `http://<address>/debug/pprof/` 新窗口链接 | GET/PUT 失败仅 toast，不伪改状态；链接带 `rel=noreferrer` | 返回状态决定 Switch 与链接是否出现 |
| UI-DBG-007 | 清理运行数据 | 首次进入读取清理状态，确认已无在途清理后才启用操作；危险确认说明清理范围、配置保留及后台暂停，并要求勾选；提交后独立展示状态、阶段、开始/结束时间 | 禁重复提交；查询失败保留上次快照并显示“系统确认中”，继续退避 GET，不自动重发 POST；失败说明部分批次可能已删除，恢复后台失败单独呈现 | 同一次 runId 的完成才触发完成反馈；成功/失败后重载诊断；完成后说明后台可能立即产生新数据 |
| UI-DBG-008 | 清理中刷新、离页或隐藏标签 | 返回后 GET 恢复进度，暂停诊断快照刷新不影响清理状态查询；清理中不周期性扫描数据库快照；隐藏标签暂停状态轮询，重新可见立即 GET | 卸载取消本页定时器及在途查询，不取消服务端清理；原任务结果丢失/Panel 重启为未确认，不能无限转圈或伪报成功 | 清理轮询不重入，网络异常退避后继续 GET；手动刷新可选但非必经；无后台清理时也可查询最近结果 |

## 19. 未找到页面

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-404-001 | 认证用户进入未知 AppShell 路径 | 显示本地化标题、说明、空态和 primary“返回概览” | 不静默改 URL、不白屏、不丢导航 | 点击后到 `/overview`，顶栏标题为 notFound titleKey |

## 20. 覆盖来源

- 全部路由：`web/src/router/index.ts`。
- 页面实现：`web/src/views/auth`、`overview`、`servers`（含 `tabs/` 资源、安全与探测 tab）、`credentials`、`applications`（含设施 `NetworkProbeFacility.vue`、`MetricsFacility.vue`）、`dns`、`certificates`、`events`、`logs`（`events` 列表复用 `IntentRecordsPage`；`/operations`、`/tasks` 仅重定向）、`settings`、`maintenance`、`debug`。不得再保留可路由的 Activity 双视图页。
- 操作/API：`web/src/api/*.ts`（含 `networkProbe.ts`、`metricsFacility.ts`、`servers.ts` 的指标/探测方法）；DTO 与 capability：`web/src/types/*.ts`（含 `networkProbe.ts`、`metricsFacility.ts`、`overview.ts`）。
- 字段校验和派生状态：各页面族 `model.ts`、`servers/tabs/resourceModel.ts`、`servers/tabs/securityModel.ts`、`assetImportEditor.ts`、`templateLanguage.ts`、`archivePath.ts`。
- 共享交互：`web/src/components/ui`（含 `Table.vue`）、`web/src/components/templates/TableDetailLayout.vue`、`patterns/AssetFileManager.vue`、`ServerMultiPicker.vue`、`AutoRefreshControl.vue`。
- 自动化证据：页面 model 测试、API client/API 模块测试、`domainRoutes.test.ts` Mock 集成样本、`Table.test.ts`、`TableDetailLayout.test.ts`、`servers/index.test.ts`、`servers/tabs/*.test.ts`，以及共享组件/布局/i18n/主题测试。

## 21. 当前实现与自动化缺口（不得作为放宽验收的依据）

1. ~~applications 编辑器 footer 取消调用 `router.back()`~~：已改为明确返回 `/applications/apps`（见 GAP-003 / UI-APP-023）。
2. ~~application detail/runtime/files 加载失败仅 toast~~：已补详情作用域 EmptyState 与重试（见 GAP-004 / UI-APP-002/003）。
3. 除共享组件和纯逻辑外，当前缺少覆盖上述完整页面链路的 Playwright e2e、axe 与多断点视觉回归。尤其 URL 恢复、脏状态离开、后台任务两阶段、上传下载和维护 token 隔离缺少浏览器级证据；覆盖式详情的前进/后退与跨页深链也依赖组件测试。
4. DNS 与 certificates 高频表单已补轻量本地校验；applications 编辑器与 settings 仍有更多输入依赖后端返回（见 GAP-027）；验收需同时验证本地阻止与服务端结构化错误不丢失。
5. `/operations`、`/tasks` 与 `/debug` 为兼容/隐藏入口，仍必须维持可用与可访问，但不应重新加入主导航。
