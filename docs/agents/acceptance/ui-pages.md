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
| 设施应用 | `/applications/facility-apps`、`/applications/facility-apps/:facilityKind` | `/:facilityKind/config` 为可直达编辑态深链 |
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
| UI-OVR-001 | 首次进入或手动刷新 | 并行加载 overview 与卡片配置，再按卡片加载数据；展示健康、指标新鲜度、更新压力、安全摘要 | 初始失败显示错误空态+重试；无服务器显示添加服务器引导；单卡失败仅留在卡内并 toast | 页面其余卡片不因一张卡失败消失，添加入口去 `/servers` |
| UI-OVR-002 | 指标卡有多个服务器 | 以时间戳对齐，每台服务器独立折线；缺失点显示 gap 而非 0；tooltip 显示时间、服务器和数值且不被卡片裁切 | 不能按数组下标错位聚合；空数据明确显示卡片空态 | 改变样本顺序/缺点后曲线仍与时间、服务器一致 |
| UI-OVR-003 | 选择自动刷新 5s/10s | 普通浏览态按 `since=最近点时间` 增量取数，合并严格更新点并按卡片 range 清理旧点 | 关闭、隐藏标签、编辑态或已有在途请求时不刷新；无增量不改旧序列 | 请求不是整段重载，刷新不闪白也不重放列表动画 |
| UI-OVR-004 | 点击“编辑仪表盘” | 进入编辑态并保存进入前快照，暂停自动刷新；显示添加、重置、取消、卡片编辑/删除/拖动/缩放 | 编辑态不应立即写后端；卡片宽 1–6、高 1–4，布局不得横向溢出 | 编辑前后卡片配置可比对，编辑态显示脏操作区 |
| UI-OVR-005 | 添加/删除/重置卡片 | 添加按类型给默认尺寸/range；删除须 danger 确认；重置恢复默认卡片并重载数据 | 删除取消不改变列表；新增未持久化指标卡不得调用不存在的 card-data ID | 卡片数量和顺序仅在确认后变化，删除确认关闭后状态清空 |
| UI-OVR-006 | 拖动或指针缩放卡片 | 拖过目标时实时预览重排；缩放按网格单位且限制范围；结束时清理拖拽/全局 pointer listener | 非编辑态完全禁用；容器列数变化不得生成隐式横向列 | 顺序、宽高在 UI 中即时反映，离开后无残留 listener |
| UI-OVR-007 | 打开单卡编辑器 | 可改类型、宽高、range、服务器范围；网络卡额外显示 rx/tx/both；空 serverIds 表示全部服务器 | 类型改变需归一默认尺寸；非网络卡不得保留可见网络方向控件 | 关闭弹窗保留当前编辑草稿，最终统一由仪表盘保存提交 |
| UI-OVR-008 | 保存或取消编辑 | 保存一次提交完整可见卡片配置，成功退出编辑并整卡重载；取消恢复进入前快照 | 保存失败留在编辑态、显示 toast/错误；保存中防重复 | 后端返回配置成为新基线，失败时用户改动未丢失 |
| UI-OVR-009 | 编辑中点击其他路由 | 弹出放弃确认；取消留在原页，确认恢复旧快照后再跳转 | 不得静默丢改动；仅打开卡片编辑器也视为需保护 | 两条确认分支的 URL 与卡片数据均正确 |
| UI-OVR-010 | 风险队列或快捷入口 | 风险项跳到自身明确目标；快捷入口分别到 servers、credentials、servers（软件包 tab）、logs | 无风险显示健康空提示，不生成不可点击假项 | 每个按钮目标与文案匹配，较窄宽度不溢出 |

## 5. 服务器 `/servers`

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-SRV-001 | 打开列表、搜索、翻页或选择服务器 | 服务端 `page/pageSize/q` 查询；URL 恢复 search/page/server/tab/subtab；表格列展示名称、地址、**单一综合状态**、最近检查（权限细节/Tailscale 进 Overview/对应 tab，不常驻列表）；综合状态只显示一枚徽标，优先级为不可达 → 主机密钥不匹配 → Agent 受阻/不可用/不匹配/未安装 → 正常（仅 `reachable` 且 Agent `compatible`）；**不得**单独用「可达」冒充健康；行操作保留「详情」+ 至多 1 个高频（测试连接），编辑/删除进 More；点击「详情」或名称列按需 GET 完整详情并覆盖列表 | 列表失败显示重试而非空态；切换服务器时取消/忽略旧详情并清空旧 tab 数据；无选中时不自动打开详情；不得依赖整行点击 | 分享 URL 恢复相同页、服务器与 tab/subtab；不会显示上一服务器的迟到详情；Agent `undeployable` 且仍 reachable 时列表状态为受阻而非可达/正常 |
| UI-SRV-002 | 查看服务器详情 | 覆盖式详情展示连接地址、端口、凭据、Docker host、OS、权限与安全、最近检查/更新时间、初始化任务与资源 tabs；概况「特权与安全」区只展示**同一枚**综合状态徽标，特权能力以正文说明而非第二枚状态徽标 | 凭据列表失败只影响凭据显示/表单并 toast，不得抹掉服务器列表；选中服务器不在当前页时按 id 单独补载；不得并列展示可达+特权两枚状态徽标造成“一切正常”误读 | 详情先显示已加载字段，完整详情加载有局部覆盖；综合状态与列表一致 |
| UI-SRV-003 | 新建/编辑服务器 | 表单含 name、IPv4、IPv6、port、credential、SSH username、Docker host、变量、notes；至少一个地址且为 IP 字面量，port 1–65535，name/credential/dockerHost 必填；基础连接（名称/地址/端口/凭据/SSH 用户名）与高级配置（Docker host/变量/备注）分区，创建时高级区默认折叠、编辑时展开 | host 只读由 IPv4 优先、否则 IPv6 派生；无效字段在字段下方就地显示本地化错误，底部摘要列出全部错误并点击聚焦对应字段 | 无效字段有本地化提示且无写请求；编辑需以完整详情填充；任何字段错误都可通过摘要定位 |
| UI-SRV-004 | 点击“探测” | 用当前未保存表单调 probe 并在基础连接区显示 reachable/error/OS/架构/特权；探测请求只发送连接字段（地址、端口、凭据、SSH 用户名） | 探测闸门只受地址/端口/凭据约束，名称与 Docker host 错误不得连坐禁用探测；连接字段变化后旧结果标记过期（灰化）提示重新探测；探测失败保留输入并非保存、不产生服务器记录 | probe 请求不含名称/Docker host/变量，结果与输入对应；创建/探测按钮可用性互不影响 |
| UI-SRV-005 | 提交服务器表单 | 创建/更新成功关闭弹窗、打开新对象覆盖详情、失效详情缓存并重载列表；创建返回 initialTaskId 时 toast 与概况 tab 只表达“服务器已登记、初始化任务已启动”，并跟踪该任务 | 提交中防重复；API 失败保持弹窗及输入；初始化任务失败时服务器记录保留（不自动删除）、标记不可达与原因，概况 tab 任务状态卡展示失败与错误，用户可编辑/删除 | 成功 toast 不早于服务端确认；活跃初始化任务每 2 秒刷新状态，失败态可经任务状态、服务器详情与活动日志追溯 |
| UI-SRV-006 | 点击测试连接 | 调用所选服务器 test，成功刷新该详情和列表并提示服务器名 | 失败保留页面，不能伪造 reachable | 仅目标服务器状态改变，按钮 loading 覆盖请求期 |
| UI-SRV-007 | 删除服务器 | 先显示对象名与影响说明的 danger 确认；成功清详情缓存、清选择并重载 | 失败不关闭确认/不移除行 | 取消零请求；成功后 URL 不再选中已删除 ID |
| UI-SRV-008 | 指标卡首次加载、换 range、手刷或自动刷 | 默认 range=1h，支持 1h/6h/1d/7d；展示 CPU、内存、磁盘、网络 rx/tx 图和当前值 | 同服务器刷新保留旧图；换服务器清旧图；自动刷防重入且隐藏页暂停 | range 请求合法，失败只影响指标卡并保留可用旧数据 |
| UI-SRV-009 | 点击部署 Agent | 提交后台任务并显示真实 taskId；立即加载该任务，显示阶段、错误、最近 20 行日志；页头主机状态与列表共用综合状态文案，部署任务另用任务 StatusBadge，二者不得用「可达」代替 Agent 问题 | 无历史显示明确空态；任务加载失败有局部重试 | 活跃任务每 2 秒增量取日志，隐藏标签暂停，完成时刷新服务器详情 |
| UI-SRV-010 | 重启或信任新 host key | 按能力禁用不可执行项；两者均先 danger 确认，信任 host key 额外强制勾选 | 确认后重启只提示任务已接受；信任成功刷新主机指纹状态 | 未确认零请求；响应 taskId/服务器名与 toast 一致 |
| UI-SRV-015 | 打开新建服务器表单（非编辑） | 在连接区之后显示**非阻断**警示横幅：面板将在部署 Agent 时接管该机防火墙（仅 UFW）——缺失时安装、放行 SSH/Agent 端口与反向代理 80/443、启用默认拒绝策略；未声明「开放防火墙」的应用端口会被阻断 | 横幅不得出现在编辑态；不得新增勾选项、弹窗或确认步骤 | 横幅不影响 `canSave`，不得因其存在而阻止提交或发出额外请求 |
| UI-SRV-011 | 填写变量 | 逐行 `key=value`；缺 `=`、空变量名属阻断错误，逐行就地提示并阻止提交；重复 key 保留 last-wins 并显示非阻断警告 | 空行忽略；阻断错误存在时创建/保存禁用 | 每行行号定位对应错误；无写请求直到阻断错误消除 |
| UI-SRV-012 | 无可用凭据时新建服务器 | 凭据区显示“尚无 SSH 凭据”引导与添加入口，可在弹窗内快捷创建凭据；成功后自动选中新凭据 | 凭据列表加载失败在弹窗内就地显示错误与重试，不影响服务器其余表单 | 快捷创建后凭据即时可用于表单与探测；无需离开当前弹窗 |
| UI-SRV-013 | 查看新建服务器初始化状态 | 概况 tab 以状态卡展示初始化任务（StatusBadge + 错误 + 活动入口），而非仅纯文本任务 id | 初始化任务加载失败有局部错误；任务失败时服务器保留、标记不可达并在状态卡持久展示原因，供编辑/删除 | 活跃初始化任务刷新，终态与失败原因可经任务状态与活动日志追溯 |
| UI-SRV-014 | 在服务器表单中配置 Tailscale 意图 | 「将该节点加入 tailnet」与两个偏好开关（Agent 连接、节点互联）；未启用时两个偏好开关禁用并显示说明，提交时未启用的一侧偏好被规范化为 false | 不得在未启用时提交偏好组合；不得只依赖后端强制而在界面保留可点击的无效开关 | 重新打开表单回显与服务端一致的三个开关，保存后详情与列表按服务端返回状态渲染 |
| UI-SRV-016 | 查看服务器 Tailscale 状态或手动重试 | `tailscaleEnabled` 时 Overview 身份条与 Tailscale tab 显示状态徽标（`disabled`/`pending`/`installing`/`running`/`degraded`/`unsupported`/`error` 的本地化文案）；列表不再常驻 Tailscale 列；Tailscale tab 单列只读展示三个开关、主机名、IPv4/IPv6、状态更新时间与 `lastError`；「应用 Tailscale」位于详情页头 actions，提交任务并提示真实 taskId，随后刷新服务器详情 | 未启用时显示未配置说明且页头应用按钮禁用；从未上报时显示明确空态而不是伪造状态；失败必须显示原因且可重试，不得把“任务已受理”表述为节点已加入 | toast 中的 taskId 可在 `/executions` 或 `/events` 定位；徽标与 `tailscale.status` 一致，`lastError` 脱敏后原样展示 |
| UI-SRV-017 | 在服务器详情切换 tabs | 详情一级 tab 固定为 概况/监控/Agent/Tailscale/**资源**/软件包/防火墙；Docker 的容器/镜像/网络/存储卷合并进「资源」并用 URL `subtab`（`containers`/`images`/`networks`/`volumes`，默认 containers）切换；旧 `tab=containers|images|networks|volumes` 作兼容别名映射到 `tab=resources&subtab=…`；一级/资源 subtab 首次激活才请求并在详情存活期内缓存，切换服务器或关闭详情清空旧数据；监控轮询只在详情打开且该 tab 激活时运行；防火墙内 UFW/Fail2Ban 用分段控件（非嵌套 Tabs），选择进 URL `subtab`，Fail2Ban 仅 dev 可见 | 切 tab/subtab 不得清空已加载快照或重复请求；能力不足在对应面板内给出阻断原因；不得用页面级滚动承载 tab 内容；Overview/Tailscale/Agent 单列展示，禁止无关 aside | tab 切换无闪烁且状态保留；深链 `?server=&tab=resources&subtab=images` 与旧 `?tab=images` 均恢复对应面板；非 dev 无 Fail2Ban 子视图 |
| UI-SRV-018 | 查看/操作资源表格 | 容器/镜像/网络/存储卷在资源 subtab、软件包在一级 tab 内以表格展示；子页去掉重复「左侧标题复述 tab 名」；镜像/Fail2Ban 工具栏收敛为**一个主操作 + Refresh，其余进 More**；容器行操作收紧为图标主操作（启动/日志）+ 溢出（停止/重启/删除，确认 + 托管提示）；镜像：拉取、升级应用、清理未使用、删除（in-use 禁删、危险确认强制勾选）；网络只读且删除禁用；存储卷：清理未使用、删除（in-use 禁删、确认强制勾选）；软件包：勾选只对当前快照，批量栏仅在有选中时出现，升级全部先 danger 确认；网络/存储卷首次打开且无快照时自动提交刷新任务 | 根据状态/能力禁用无效动作；操作按钮按（行, 操作）单独 loading；失败保留快照并 toast；不得把失败当作空列表 | 表格列包含名称/关键状态；请求 payload 与选中项一致；取消确认零请求；成功后重载当前面板 |
| UI-SRV-019 | 在防火墙 tab 维护 UFW / Fail2Ban | UFW 展示 supported/installed/active/default policy 与规则号/action/to/from，提供新增 allow rule 与删除规则（确认、受保护端口/受管规则由后端拒绝），并展示「安装与启用由 Agent 部署自动完成」说明；Fail2Ban 可在 visual 与 YAML 间切换，提供 preset、jail 删除、保存草稿、安装、启用/接管确认、释放（danger 确认）；保存草稿仅在 YAML 变化时启用；UFW/Fail2Ban 切换用分段控件而非嵌套一级 Tabs | 页面不得出现 UFW 安装/启用入口；未勾选接管不提交；保存不等于启用；失败保留草稿/输入 | 规则增删使用返回值重建列表；toast 只承诺任务接受并含真实 taskId；visual 往返可解析 |

## 6. SSH 凭据 `/credentials`

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-CRED-001 | 列表搜索、翻页、选择 | 服务端分页/q；URL 恢复 search/page/credential；表格显示名称、类型、用户名、引用数和更新时间；点击「详情」或名称列覆盖列表并按需加载 key summary；详情单列纵向分节（密钥摘要 + 引用），不得短侧栏左右分栏 | 列表/详情错误分别显示重试；服务器列表失败不得泄露或伪造引用；详情打开时列表保持挂载但不可交互；不得依赖整行点击 | 选择变化只展示对应详情，私钥材料正文从不出现在详情 |
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
| UI-APP-001 | `/applications/apps` 搜索、翻页、选择 | 服务端分页/q；URL 恢复 search/page/application；表格显示名称、**启停状态**、镜像引用、健康/异常/部署中三计数、镜像更新与更新时间；点击「详情」或名称列覆盖列表；规划失败以行内告警显示“部署被阻塞”；`reconcileStopped` 行内显示“需人工处理”；主徽标不得显示部署中/系统核实中/已部署/失败 | summary 缺 imageReference 时仅按行补详情并在该行显示骨架；失败不得把整表清空为假空态；不能将运行实例存在解释为最新部署成功；不得依赖整行点击；三计数之和可小于 instanceCount | 行级补载互不阻塞，选中项完整详情/runtime/files 按需请求；启停与告警按 `APP-PLAN-001`、三计数按节点归类 |
| UI-APP-002 | 查看 Runtime tab | 显示健康/异常/部署中三计数及每服务器节点卡片（观测态、容器、`lastError`）；各节点 Job 显示真实状态、`last_stage` 或无进度时的“系统核实中”、attempt、nextRunAt、错误码/消息/详情与 operation 深链；独立显示规划失败原因与 reconcile 告警；不得再渲染应用级单一「当前部署」主块；详情单列，`ActivityLink` 在页头 actions | runtime 失败 toast 且详情内 EmptyState 可重试；详情/files 加载失败用 `detailError` EmptyState 重试；换应用清旧 runtime/错误；节点终态 failed 不得消失；无 Job 的规划失败也必须显示；`execution_monitoring` 不得渲染为部署失败危险块 | 节点观测/Job 文案一致；任一节点活跃 Job 或当前规划错误基线 3 秒有界轮询，按最早 nextRunAt 对齐，隐藏暂停，最多 24 次或 2 分钟；新 runtime 无规划错误时清除旧 DTO 告警；`tab` 查询与 runtime/routes/files/data 双向同步 |
| UI-APP-003 | 查看 Routes/Files/Data tab | Routes 显示域名、目标端口、来源服务器名和 paths；Files 只列已提交文件并支持 blob 下载；Data tab 承载持久化数据节点选择与下载/恢复；URL `tab=routes|files|data` 可深链恢复（缺省 runtime） | 无路由/文件有专属空态；详情/files 失败走详情作用域错误重试；服务器名未知才回退 ID；关闭详情清 `tab` | 文件名和下载名符合 kind/contentType，下载带 auth |
| UI-APP-004 | 点击行内同步/部署 | 提交人工 deploy；实际规划时显示真实 operationId/deploymentId 并刷新，`noChange=true` 时明确提示已是期望状态 | 不得被自动退避静默吞掉，不得把 no-change 或请求接受宣称为部署完成；失败后也重载列表/详情/runtime，使持久化规划诊断立即可见 | toast 与实际响应字段一致，operation 深链可恢复后续状态；修正后计划成功或已满足期望时清当前告警、保留失败历史 |
| UI-APP-005 | 行内停用应用 | enabled 时可点，先 danger 确认后调用 stop；成功只提示请求接受并刷新 | 已停用禁用；失败保持应用状态 | 取消无请求，成功后状态来自重载而非前端假改 |
| UI-APP-006 | 行内检查并更新镜像 | 仅 `imageUpdateAvailable` 时启用；提交 update 并按两阶段反馈 | 无更新时不请求 | 返回 ID 的 toast 正确，刷新后徽标由后端决定 |
| UI-APP-007 | 点击行内“容器日志” | 打开 Dialog 并加载该应用实例；按服务器名/容器名选择目标，单个可读实例自动加载，多个等待选择；请求携带 instanceId、tail=240，正文内部滚动并支持手动刷新/刷新实例列表 | 已退出但存在的容器仍可选；无实例、不可读、空日志和读取失败分开；失败 toast 加就地重试；切换实例/应用/关闭时取消旧请求且迟到结果不串日志 | 只请求选中实例，不通过任务或协调记录代替容器日志；不定时轮询，长日志不撑开页面 |
| UI-APP-008 | 打开协调记录 | 携带当前 application / facility resource filter 跳 `/events`；已有 operationId 时打开对应事件详情；诊断输出经「查看日志」进 `/logs?eventId=`（默认 `level=warning,error`） | 不从全局相邻事件猜测因果，不直接展示原始 ID 作为用户标题 | 目标页面筛选与当前应用对应，本次部署入口定位同一 operation |
| UI-APP-009 | 行内删除应用 | 普通应用显示影响说明并 danger 确认；存在持久化配置/历史位置时先补载完整详情，使用 danger 强制勾选确认，明确说明将永久删除所有已记录节点的数据，并向后端传确认参数 | 取消零请求；未确认或清理失败不移除对象；补载详情失败不进入确认 | 持久化删除只有勾选后发出，成功后重载列表并关闭覆盖详情 |
| UI-APP-010 | 下载持久化数据（详情上下文面板） | 展示历史数据节点选择器；单节点自动选择，多节点要求用户选择，携带 serverId 走授权 blob 下载 | 无记录位置或未选择节点时禁用；下载失败不生成损坏文件 | 未 apply 与已移除部署节点的数据均可按节点下载，按钮有单独 loading |
| UI-APP-011 | 选择 zip 恢复持久化数据 | 文件选择后必须经 danger+强制勾选确认，再 multipart restore | 取消选择/确认不上传；没有 persistentPath 禁用 | 仅确认后发一笔请求，反馈只承诺已接受 |

### UI-EVT-004 进程事件 `/events`：精简且利于排障

- **前置**：认证用户进入 `/events`；旧 `/operations`、`/tasks` 重定向到 `/events`（保留 query）；`/activity` 按 `UI-EVT-007` 分流。
- **动作**：看 AppDB **过程**行（一件事：如部署 Agent、一次执行），含 state/module/level、失败摘要，并跳到 JSONL 日志定位原因。
- **结果**（排障路径必须短）：
  1. 列表只展示真正的过程，不得把瞬时诊断（如 `agent.connected` / `agent.disconnected`、stream timeout）当作事件行。列表字段：时间、模块、级别、标题、状态（`state`）；详情保留状态、失败摘要、模块/来源/时间/关联 ID，以及「查看日志」；不得展示 event executor 的 steps 树、时间线、上下文或证据下载。
  2. 「查看日志」与带 `eventId` 的 Toast/`ActivityLink` 进入 `/logs?eventId=`，默认带 `level=warning,error`；仅有 `operationId`/`executionId` 时先在 `/events` 解析或筛选。
  3. 应用协调入口进 `/events`。
  4. 产品读 API 为 `/api/v1/events*`（legacy `/api/v1/log-contexts*` 仍可用）；不得再调用已停注册的 `/api/v1/activity/*`。
- **失败**：不得用 trigger reason 冒充失败摘要。
- **不变量**：原始 JSONL 输出不翻译、不改写；不得恢复 `/activity` 双投影或日志关联工作台。
- **验证**：`activityRedirect`、`IntentRecordsPage`（events 列表 + View logs）、eventId→logs 深链。

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
- **结果**：`/events` 缺少 `from` 时使用最近 24 小时并写回 URL；`/logs` 缺省 `level` 使用 `warning,error`（可写回 URL），无 `eventId` 时直接加载全局日志行。
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
- **结果**：`/operations`、`/tasks` 重定向到 `/events` 并保留筛选/选中参数；`/activity` 中 `view=operations` 等同；带 `eventId`（或 legacy `streamId`）重定向到 `/logs` 并补 `level=warning,error`；仅 `operationId`/`executionId` 时重定向到 `/events`；其余缺省重定向到 `/logs`。
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
| UI-FAC-003 | 查看 reverse-proxy 详情 | 单列 + Tabs：`routes`（默认）纵向叠放 KPI、route summaries、DNS 同步、静态资产下载；`nodes` 展示 operation/reconcileStopped 与 `FacilityDiagnostics`；`tab` 进 URL；禁止主栏+右侧诊断常驻左右分栏；DNS 区列出设施域名与应用反向代理域名并标注来源 | 未知 facilityKind 显示不可用空态；加载失败可重试 | 用户可见服务器使用名称，未知才回退 ID；深链 `?tab=nodes` 直达节点状态；应用域名不得从 DNS 区遗漏 |
| UI-FAC-004 | DNS 同步为 pending | 非编辑态、可见标签每约 3 秒重载设施直至不 pending | 编辑中停止轮询；离开清 timer | 状态从 pending 更新且不覆盖编辑草稿 |
| UI-FAC-013 | DNS 失败/跳过重试与覆盖接管 | 失败/跳过/未同步显示错误与「重试 DNS」；失败（或冲突类错误）提供「覆盖已有记录」，经危险确认后调用 `POST .../dns-sync`（`takeover`+`confirmTakeover`） | 未确认不得接管；请求中只锁当前域名操作 | 成功提示含 taskId 并刷新 DNS 状态 |
| UI-FAC-005 | 进入入口代理编辑 | 详情页就地展开，config 深链也可直达；进入前重新拉取已提交配置到本地草稿与资产基线；**不**创建 edit-session；显示连续流：网关、域名/路由、静态资产 | 配置加载失败显示重试；编辑中隐藏详情 reconcile/refresh；不得用陈旧内存快照作为保存时的服务端播种源 | 两个入口同一本地编辑与保存行为 |
| UI-FAC-006 | 修改网关服务器 | 仅从服务器选择；移除网关时自动剪除各域名 origin/primary/relay，并以 warning 告知数量 | 不得留下指向非网关服务器的引用 | payload 中所有关联服务器均属于 deploymentServers |
| UI-FAC-007 | 新增/编辑域名 | 校验合法、非重复域名且至少一台 origin；origin 只能从网关选；可配置 AnyAccess relay/策略/主源 | primary 必须属于 origin，指定 relay 只能来自可用网关 | 错误字段就地显示且弹窗不关闭；成功才标脏 |
| UI-FAC-008 | 新增/编辑设施 path | path 必须以 `/` 开头；static 要选**本地可见** asset；redirect 要合法 URL 与 301/302/307/308；proxy_pass 要 http(s) URL并可配 source mode | 各 ruleType 只发送相关字段；非法值就地阻止保存 | model 测试覆盖空/非法 path、redirect、proxy 和缺 asset |
| UI-FAC-009 | 配置设施 path 高级项 | 支持 gzip、请求体、超时、buffering、WebSocket、自定义请求/响应头；redirect 仅保留适用项 | 数值非负，空 header 过滤 | clone/编辑往返不丢 proxy_pass 和 options |
| UI-FAC-010 | 管理静态资产 | 复用 AssetFileManager；变更写入本地 pending；文本「保存」只更新 pending；新文本下载名默认跟引用名且可分开改；路由引用用 asset.name；已提交资产下载走正式 static content | binary/archive 即使扩展名像文本也不可编辑；替换保留 name；编辑期不得调用 session asset API | 列表=已提交⊕pending；页面保存时 flush；删除仍被引用由保存时服务端诊断阻止 |
| UI-FAC-011 | 保存/取消设施编辑 | 唯一主操作「保存」：本地校验→begin（无客户端草稿）→patch→flush pending 资产→preview→无 blocking diagnostic 才 commit→清理会话；变更摘要按域名和网关服务器实际对象互斥归类；成功必须退出编辑并回到详情并刷新；取消脏草稿先确认；无独立 Preview 主按钮 | 冲突 discard 临时会话并保留本地；保存中整页阻塞；仅默认字段规范化、域名面板顺序或服务器选择顺序变化不得产生假修改；仅改静态文本不得回滚已提交 redirectUrl | applyRequested 决定提示；diff model 测试覆盖新增、删除、同名内容修改、等量替换和未变更基线 |

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
| UI-DNS-001 | 搜索、翻页、选择域名 | 服务端分页/q；URL 恢复 search/page/domain；表格显示域名、provider 状态与更新时间；点击「详情」或名称列覆盖列表并加载记录 | 列表失败与空域名分离；切域名立即清旧记录并 abort；详情打开时列表保持挂载但不可交互；不得依赖整行点击 | 分享 URL 恢复相同域名与页码，迟到记录不串域名 |
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
| UI-SET-002 | 保存 General/Runtime | 校验各 interval/retention >=1，event retention >= detail retention；保存语言、日志、cleanup、reconcile trace；以已加载 runtime 为基底只合入本分区 | 不得提交其他分区未保存脏值；非法数字不请求 | 成功重新 hydrate，语言立即 setLocale 并更新 html.lang |
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
- 页面实现：`web/src/views/auth`、`overview`、`servers`（含 `tabs/` 资源与安全 tab）、`credentials`、`applications`、`dns`、`certificates`、`events`、`logs`（`events` 列表复用 `IntentRecordsPage`；`/operations`、`/tasks` 仅重定向）、`settings`、`maintenance`、`debug`。不得再保留可路由的 Activity 双视图页。
- 操作/API：`web/src/api/*.ts`；DTO 与 capability：`web/src/types/*.ts`。
- 字段校验和派生状态：各页面族 `model.ts`、`servers/tabs/resourceModel.ts`、`servers/tabs/securityModel.ts`、`assetImportEditor.ts`、`templateLanguage.ts`、`archivePath.ts`。
- 共享交互：`web/src/components/ui`（含 `Table.vue`）、`web/src/components/templates/TableDetailLayout.vue`、`patterns/AssetFileManager.vue`、`ServerMultiPicker.vue`、`AutoRefreshControl.vue`。
- 自动化证据：页面 model 测试、API client/API 模块测试、`domainRoutes.test.ts` Mock 集成样本、`Table.test.ts`、`TableDetailLayout.test.ts`、`servers/index.test.ts`、`servers/tabs/*.test.ts`，以及共享组件/布局/i18n/主题测试。

## 21. 当前实现与自动化缺口（不得作为放宽验收的依据）

1. ~~applications 编辑器 footer 取消调用 `router.back()`~~：已改为明确返回 `/applications/apps`（见 GAP-003 / UI-APP-023）。
2. ~~application detail/runtime/files 加载失败仅 toast~~：已补详情作用域 EmptyState 与重试（见 GAP-004 / UI-APP-002/003）。
3. 除共享组件和纯逻辑外，当前缺少覆盖上述完整页面链路的 Playwright e2e、axe 与多断点视觉回归。尤其 URL 恢复、脏状态离开、后台任务两阶段、上传下载和维护 token 隔离缺少浏览器级证据；覆盖式详情的前进/后退与跨页深链也依赖组件测试。
4. DNS 与 certificates 高频表单已补轻量本地校验；applications 编辑器与 settings 仍有更多输入依赖后端返回（见 GAP-027）；验收需同时验证本地阻止与服务端结构化错误不丢失。
5. `/operations`、`/tasks` 与 `/debug` 为兼容/隐藏入口，仍必须维持可用与可访问，但不应重新加入主导航。
