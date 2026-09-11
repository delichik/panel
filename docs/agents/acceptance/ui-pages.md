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
| 资源与安全 | `/resources/packages`、`containers`、`images`、`networks`、`volumes`、`firewall` | `/resources`、`/security/*` 重定向；`/resources/fail2ban` 仅 dev 可用 |
| 应用 | `/applications/apps`、`/applications/apps/create`、`/applications/apps/:applicationId/edit` | `/applications` 重定向 |
| 设施应用 | `/applications/facility-apps`、`/applications/facility-apps/:facilityKind` | `/:facilityKind/config` 为可直达编辑态深链 |
| DNS / 证书 | `/dns/domains`、`/certificates/domains`、`/certificates/self-signed`、`/certificates/keys` | — |
| 运行记录 | `/application-operations`、`/system-events` | `/tasks` 为兼容任务中心 |
| 设置 / 诊断 | `/settings/general`、`security`、`certificates`、`system-certificates`、`system`、`backups` | `/settings` 重定向；`/debug` 隐藏直达 |
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
| UI-OVR-010 | 风险队列或快捷入口 | 风险项跳到自身明确目标；快捷入口分别到 servers、credentials、packages、application-operations | 无风险显示健康空提示，不生成不可点击假项 | 每个按钮目标与文案匹配，较窄宽度不溢出 |

## 5. 服务器 `/servers`

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-SRV-001 | 打开列表、搜索、翻页或选择服务器 | 服务端 `page/pageSize/q` 查询；URL 恢复 search/page/server；行展示可达性、Agent、权限；选择后按需 GET 完整详情 | 列表失败显示重试而非空态；切换选择时取消/忽略旧详情 | 分享 URL 恢复相同页和服务器，不会显示上一服务器的迟到详情 |
| UI-SRV-002 | 查看服务器详情 | 展示连接地址、端口、凭据、Docker host、OS、最新指标、最近检查/更新时间、Agent 和安全能力 | 凭据列表失败只影响凭据显示/表单并 toast，不得抹掉服务器列表 | 摘要先可见，完整详情加载有局部 overlay |
| UI-SRV-003 | 新建/编辑服务器 | 表单含 name、IPv4、IPv6、port、credential、SSH username、Docker host、变量、notes；至少一个地址且为 IP 字面量，port 1–65535，name/credential/dockerHost 必填 | host 只读由 IPv4 优先、否则 IPv6 派生；无效表单禁 probe/save | 无效字段有本地化提示且无写请求；编辑需以完整详情填充 |
| UI-SRV-004 | 点击“探测” | 用当前未保存表单调用 probe，并在弹窗内显示 reachable/error | 探测失败保留输入并 toast/就地错误；探测不等于保存 | probe 请求不产生服务器记录，结果与请求输入对应 |
| UI-SRV-005 | 提交服务器表单 | 创建/更新成功关闭弹窗、选中新对象、失效详情缓存并重载列表；创建返回 initialTaskId 时只提示任务已创建 | 提交中防重复；API 失败保持弹窗及输入 | 成功列表/详情一致，错误可见且可修正重试 |
| UI-SRV-006 | 点击测试连接 | 调用所选服务器 test，成功刷新该详情和列表并提示服务器名 | 失败保留页面，不能伪造 reachable | 仅目标服务器状态改变，按钮 loading 覆盖请求期 |
| UI-SRV-007 | 删除服务器 | 先显示对象名与影响说明的 danger 确认；成功清详情缓存、清选择并重载 | 失败不关闭确认/不移除行 | 取消零请求；成功后 URL 不再选中已删除 ID |
| UI-SRV-008 | 指标卡首次加载、换 range、手刷或自动刷 | 默认 range=1h，支持 1h/6h/1d/7d；展示 CPU、内存、磁盘、网络 rx/tx 图和当前值 | 同服务器刷新保留旧图；换服务器清旧图；自动刷防重入且隐藏页暂停 | range 请求合法，失败只影响指标卡并保留可用旧数据 |
| UI-SRV-009 | 点击部署 Agent | 提交后台任务并显示真实 taskId；立即加载该任务，显示状态、阶段、错误、最近 20 行日志 | 无历史显示明确空态；任务加载失败有局部重试 | 活跃任务每 2 秒增量取日志，隐藏标签暂停，完成时刷新服务器详情 |
| UI-SRV-010 | 重启、安装 UFW 或信任新 host key | 按能力禁用不可执行项；三者均先 danger 确认，信任 host key 额外强制勾选 | 确认后重启/UFW只提示任务已接受；信任成功刷新主机指纹状态 | 未确认零请求；响应 taskId/服务器名与 toast 一致 |

## 6. SSH 凭据 `/credentials`

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-CRED-001 | 列表搜索、翻页、选择 | 服务端分页/q；URL 恢复 search/page/credential；列表显示类型、用户名和引用数，选择后按需加载 key summary | 列表/详情错误分别显示重试；服务器列表失败不得泄露或伪造引用 | 选择变化只展示对应详情，私钥材料正文从不出现在详情 |
| UI-CRED-002 | 新建密码凭据 | name、username、password 必填；成功后选中新凭据并刷新 | 密码不得出现在列表、toast 或日志 | 空 secret 无请求，保存后仅显示元数据 |
| UI-CRED-003 | 新建私钥凭据 | name、username、private key 必填，passphrase 可选；large 弹窗用 CodeEditor | 无效/空私钥不提交；密钥正文不回显到详情 | 成功后详情仅展示 comment/algorithm/bits/fingerprint |
| UI-CRED-004 | 编辑凭据 | 预填非 secret 字段；类型未变时 secret 留空表示保留；类型切换必须重新提供新类型 secret，并清旧 secret 输入 | 不得以空字符串覆盖现有 secret；passphrase 仅非空才发送 | API payload 单测证明编辑空 secret 字段被省略 |
| UI-CRED-005 | 测试凭据 | 只有存在引用服务器才启用；弹窗列出引用服务器并选目标，实际调用该服务器 test | 无独立凭据 test 接口；无引用不能打开/发送请求 | 成功提示目标服务器名并关闭，失败保留选择以重试 |
| UI-CRED-006 | 删除凭据 | 确认弹窗列出引用影响；存在引用时禁用删除并引导先解除 | 前端引用计算与后端冲突任一都应阻止删除 | 无引用成功后刷新；有引用时点击不会发 DELETE |

## 7. 资源页面通用与 `/resources/*`

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-RES-001 | 打开 packages/containers/images/networks/volumes | 左侧唯一 ServerContextSelector；URL 恢复 server，行展示 APT/Docker 能力和不可用原因；路由 path 决定资源种类 | 不可达服务器不可选；换服务器/路由立即清旧资源并忽略迟到响应 | 不出现额外服务器下拉，资源始终属于当前 server/path |
| UI-RES-002 | 资源 GET 失败或能力不足 | 对应区块显示 load failed、详情和重试；能力不足显示明确 block reason | 不得把失败/未上报当作空列表 | 重试仅请求当前资源；其他页面区块不受影响 |
| UI-RES-003 | packages 搜索与勾选 | 搜索过滤 package/name/version/description 等可空字段；勾选状态只对应当前快照 | 切服务器/重载清选择；无选中隐藏“升级已选”和计数 | 首个勾选后批量栏出现，清空后消失 |
| UI-RES-004 | 刷新包元数据 | 提交 refresh 任务，显示真实 taskId，等待最多约 90 秒完成后 GET 新快照 | 等待失败/超时显示错误且可再刷；不得用 GET 冒充 refresh | 请求顺序为 POST→任务轮询→GET |
| UI-RES-005 | 升级已选/全部包 | 已选直接提交选中包任务；全部升级先 danger 确认影响 | 无权限/空列表/空选择禁用；接受不等于升级完成 | 请求 payload 与选中名一致，toast 显示真实 taskId |
| UI-RES-006 | 查看容器 | 卡片显示名称、镜像、状态、端口及应用托管提示；操作行可换行不裁切 | 应用托管容器仍允许直接动作，但提示协调可能恢复 | 长名称/窄宽度下按钮仍可达 |
| UI-RES-007 | 容器 start/stop/restart/delete | start 可直接执行；stop/restart/delete 先确认，托管容器确认增加协调影响；delete 强制勾选 | 根据状态/能力禁用无效动作；请求期单项 loading | 操作成功刷新快照；取消确认零请求 |
| UI-RES-008 | 打开容器日志 | 读取最近 500 行，在可滚动 Dialog `pre` 中展示 | 失败只 toast，不打开伪空日志；长行换行且无页面横滚 | 日志对应容器名，关闭后返回原滚动位置 |
| UI-RES-009 | 镜像刷新/拉取 | 刷新提交任务并等待完成后重载；拉取弹窗要求非空 reference，成功关闭并重载 | 悬空镜像 repoTags=null 用 reference 或 ID 展示 | 任务刷新不靠重复 GET，拉取空值零请求 |
| UI-RES-010 | 镜像升级应用 | “升级有更新应用”仅提交可升级镜像关联 applicationIds 去重集合；“升级全部应用”走 all 接口 | 选中集合为空禁用；接受只提示 taskId | payload 不含重复/不可升级 ID |
| UI-RES-011 | 删除镜像或清理未使用镜像 | 均 danger 确认且强制勾选；in-use 单镜像禁删 | 失败保留快照并 toast | 成功后当前镜像不再出现；取消无请求 |
| UI-RES-012 | 首次打开 networks 且快照 stale、无 observedAt | 自动提交一次 refresh 任务、等待并重新 GET；手刷同闭环 | 同 server/tab 防重复自动刷新；网络只读，删除始终禁用 | 初始“未刷新”不会误显示成真空态；无网络 DELETE 请求 |
| UI-RES-013 | 首次打开/刷新 volumes | stale 且无 observedAt 时自动任务刷新；手刷等待完成后重载 | 同 server/tab 防重复；空快照与失败分离 | taskId 可追踪，刷新后 observed 数据出现 |
| UI-RES-014 | 删除卷或清理未使用卷 | danger 确认且强制勾选；in-use 卷禁删 | 失败不从 UI 乐观移除 | 成功后重载；确认影响说明可见 |

## 8. 防火墙与 Fail2Ban

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-SEC-001 | 打开 firewall/fail2ban 或换服务器 | 使用唯一 ServerContextSelector 和 URL server；加载当前服务器对应安全面板 | 切换时清旧状态、取消旧请求；不可达服务器显示原因 | 面板数据、标题与 route/server 同步 |
| UI-SEC-002 | 查看 UFW | 展示 supported/installed/active/default policy 与规则号、action、to、from；初始骨架、空态、失败态分离 | unsupported 时禁新增/启用/安装 | 状态徽标与接口字段一致 |
| UI-SEC-003 | 新增 UFW allow rule | 弹窗含 port、TCP/UDP、from；校验端口与来源后提交并使用返回的新 UFW state | 无效表单不请求；失败保留弹窗和输入 | 成功关闭、规则立即出现并 toast |
| UI-SEC-004 | 删除规则、启用或安装 UFW | 均先显示影响确认；删除直接更新返回 state，启用/安装显示后台 taskId 并重载 | active/installed 时相应按钮禁用；取消零请求 | 只对当前服务器/规则号发送一次请求 |
| UI-SEC-005 | 非 dev 访问 Fail2Ban | 路由回到 firewall 且保留 query；dev 显示完整编辑页 | production 导航也不得出现 Fail2Ban | 构建环境切换时行为稳定 |
| UI-SEC-006 | 查看/编辑 Fail2Ban | 可在 visual 与 YAML 间切换；visual 提供 ssh/nginx-auth/recidive preset、jail 删除，任一变化同步 YAML | YAML 含 visual 无法表达的高级配置时，切 visual 必须警告放弃；取消回 YAML | 确认后才发生降级转换，preset 往返仍可解析 |
| UI-SEC-007 | 保存 Fail2Ban 草稿 | 仅有 YAML 变化才启用；保存 configYaml，成功以返回 state 重新建立草稿基线 | 失败保留草稿；保存不等于启用服务 | 成功 dirty 消失，运行时 jail 与草稿配置分开显示 |
| UI-SEC-008 | 安装、启用/接管、释放 Fail2Ban | 安装提交任务；启用先确认，若已安装但非 Panel 托管则必须勾选接管；释放为 danger 确认 | 已安装禁安装、非托管禁释放；未勾接管不提交 | toast 只承诺任务接受并含真实 taskId，随后重载面板 |

## 9. 应用列表、详情与运行操作

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-APP-001 | `/applications/apps` 搜索、翻页、选择 | 服务端分页/q；URL 恢复 search/page/application；列表显示状态、镜像引用、实例数和镜像更新 | summary 缺 imageReference 时仅按行补详情并显示骨架；失败不得把整表清空为假空态 | 行级补载互不阻塞，选中项完整详情/runtime/files 按需请求 |
| UI-APP-002 | 查看 Runtime tab | 显示实例总数/运行/失败及每服务器实例、容器和 `lastError`；当前 Job 显示真实状态、步骤、attempt、nextRunAt、错误码/消息/详情与 operation 深链 | runtime 失败 toast 且局部可重试/刷新；换应用清旧 runtime；终态 failed 不得消失 | 实例/Job 状态文本与徽标一致；active 状态基线 3 秒有界轮询，按 nextRunAt 对齐，隐藏暂停，最多 24 次或 2 分钟且终态停止 |
| UI-APP-003 | 查看 Routes/Files tab | Routes 显示域名、目标端口、来源服务器名和 paths；Files 只列已提交文件并支持 blob 下载 | 无路由/文件有专属空态；服务器名未知才回退 ID | 文件名和下载名符合 kind/contentType，下载带 auth |
| UI-APP-004 | 点击同步/部署 | 提交人工 deploy；实际规划时显示真实 operationId/deploymentId 并刷新，`noChange=true` 时明确提示已是期望状态 | 不得被自动退避静默吞掉，不得把 no-change 或请求接受宣称为部署完成 | toast 与实际响应字段一致，operation 深链可恢复后续状态 |
| UI-APP-005 | 停用应用 | enabled 时可点，先 danger 确认后调用 stop；成功只提示请求接受并刷新 | 已停用禁用；失败保持应用状态 | 取消无请求，成功后状态来自重载而非前端假改 |
| UI-APP-006 | 检查并更新镜像 | 仅 `imageUpdateAvailable` 时启用；提交 update 并按两阶段反馈 | 无更新时不请求 | 返回 ID 的 toast 正确，刷新后徽标由后端决定 |
| UI-APP-007 | 查看日志 | 立即打开带 LoadingOverlay 的日志 Dialog，拉取 tail=240 并在内部滚动展示 | 失败 toast 且弹窗显示明确失败文案，不假装空日志 | 长日志不撑开页面，关闭可恢复详情 |
| UI-APP-008 | 打开协调记录 | 携带 `view=operations` 与当前 application resource filter 跳 `/activity`；已有 operationId 时直接打开本次时间线 | 不从全局相邻事件猜测因果，不直接展示原始 ID 作为用户标题 | 目标页面筛选与当前应用对应，本次部署入口定位同一 operation |
| UI-APP-009 | 删除应用 | 显示影响说明并 danger 确认；成功重载列表 | 失败不移除对象；需要由后端处理引用/协调冲突 | 取消零请求，已删除应用不再选中 |
| UI-APP-010 | 下载持久化数据 | 仅有 persistentPath 时启用，走授权 blob 下载 | 无持久化路径禁用；下载失败不生成损坏文件 | 浏览器保存有效归档，按钮有单独 loading |
| UI-APP-011 | 选择 zip 恢复持久化数据 | 文件选择后必须经 danger+强制勾选确认，再 multipart restore | 取消选择/确认不上传；没有 persistentPath 禁用 | 仅确认后发一笔请求，反馈只承诺已接受 |

### UI-ACT-001 Activity 事件与操作可读性

- **前置**：`/activity` 同时包含部署 operation、普通事件、结构化错误及观测拒绝。
- **动作**：浏览事件/按操作视图、打开时间线或复制摘要。
- **结果**：错误优先显示 errorCode/error/detail；观测拒绝按稳定 reason 显示人类可读说明；复制内容与可见摘要一致，完整事件 JSON 只在技术信息中折叠展示。
- **失败**：不得仅显示 eventType 或 trigger reason 掩盖真实错误；不同 resource/operation 的相邻事件不得被表现为同一因果链。
- **不变量**：原始输出和已脱敏技术证据不翻译、不改写；应用入口默认按 operation 查看。
- **验证**：activity model/timeline、operation deep-link tests。

## 10. 应用创建/编辑器

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-APP-012 | 进入 create 或 `:applicationId/edit` | 先加载必要应用/设施数据并创建 durable edit session；会话就绪前不渲染可编辑表单，preview/commit 禁用 | 会话失败就地显示错误和重试，不允许编辑未加载草稿 | 成功后表单来自 session.draft；重试可丢弃旧 session 重建 |
| UI-APP-013 | 编辑连续配置流 | 依次完整展开：身份、运行源、网络访问、容器环境、存储与挂载、部署目标、应用文件；网络、环境、存储必须为同级独立面板并各自提供操作与空态；正文内部滚动、右侧摘要 sticky/下移 | 不得把环境变量与挂载混入同一面板，不得恢复 YAML 源码模式、分页卡片或分区隐藏；窄屏单列无横滚 | 所有区块在同一编辑正文可连续到达，网络、环境、存储的标题和内容边界可独立辨认 |
| UI-APP-014 | 编辑基本/运行字段 | name、enabled、image、CPU、memory、命令参数数组、privileged 任一变化标脏并清过期预览/诊断 | CPU/memory 非数字、name/image 等必填错误阻止 preview/commit；命令新增项默认空 | 右侧 diff/dirty 立即更新，序列化保留未被 UI 覆盖的 spec 字段 |
| UI-APP-015 | 新增/编辑环境变量或端口 | 弹窗草稿与主草稿隔离，保存才写入；env 含 key/value，value 可从目录在当前光标/选区插入当前应用或其它应用容器占位符；port 含 label/container/host port | 目录失败只禁用插入能力，不阻止手工编辑；取消不得污染主草稿；删除/保存均标脏 | model 测试证明 dialog draft 独立，插入测试覆盖光标与替换选区，列表摘要更新 |
| UI-APP-016 | 新增/编辑挂载 | 支持 persistent/volume/host/file/panel_file/storage_share，含 source/target/mode/readOnly；file 仅从当前编辑会话的应用文件 name 下拉选择，storage_share 从已启用设施服务器选来源 | 应用文件为空时提示先上传；旧 file 引用已不存在时保留原值并标记失效、禁止再次保存；storage share 未配置显示配置入口且无来源不能保存；该类型不显示 mode | file 与 storage_share 保存后 source 均使用稳定值而非展示文案，取消无改变 |
| UI-APP-017 | 新增/编辑应用反向代理 | 配置 domain、targetPort、paths、AnyAccess；来源服务器由部署目标与网关自动推导，只读，不允许手填 | primary_backup 无来源优先级时报错/阻止提交；domain/port/path 无效阻止最终 preview | 保存生成规范小写域名、数值端口和稳定 originPriority |
| UI-APP-018 | 配置 AnyAccess | 可选全部/指定 relay 网关、round_robin/ip_hash/primary_backup；主备通过上下移动明确优先顺序 | relay 只能选网关且不能与 origin 重合；移除部署/网关后无效项被清理 | payload 仅在启用且选择指定时包含 relayServerIds |
| UI-APP-019 | 新增或编辑代理 path | 打开既有 path 时必须完整回显 path、gzip、请求体上限、连接/读/写超时、buffering、WebSocket mode、自定义请求/响应头；WebSocket 只提供 auto/on/off 单一选择器，编辑副本与父规则隔离 | 数值非负；空 header 行过滤；payload 不得出现旧 `webSocket` 布尔字段；不得直接用浏览器深拷贝 API 复制 Vue reactive Proxy；取消子弹窗返回代理弹窗且不写主草稿 | 选择 auto/on/off 后 payload 仅含 `options.webSocketMode`；保存回父弹窗后 path 摘要可见，最终保存才标主草稿脏 |
| UI-APP-020 | 管理应用文件 | 统一 AssetFileManager 单入口选择文本/普通文件/文件夹归档；文本可编辑，binary/archive 仅替换/下载/删除 | 文件名拒绝 `/`、`\\`、`..`、控制字符；替换必须保留原 name；并发冲突提供 reload | 所有动作走 session revision，成功更新 session 文件列表并标脏 |
| UI-APP-021 | 点击 Preview | 先本地校验，再 PATCH 当前 draft，再调用 session preview，展示 diagnostics/diff | 任一 error diagnostic 阻止 commit；修改草稿自动清掉旧 diagnostics | 请求 revision 单调，诊断显示 field/message/detail |
| UI-APP-022 | 点击 Commit | 自动执行同一 preview 流程，只有无 blocking diagnostics 才 commit；保存期间整编辑器阻塞且不可离开 | commit 冲突/失败保留会话并 toast；不重复提交 | 成功按 applyRequested 区分提示，选中新应用回列表 |
| UI-APP-023 | 取消、返回、路由跳转或关页 | 任意脏字段/文件触发放弃确认和 beforeunload；明确放弃后才离开；保存中拒绝离开 | 返回目标必须明确为 apps 列表，不依赖不稳定历史栈 | 取消确认保留草稿；确认后 session 状态被放弃/页面离开 |

## 11. 设施目录与入口代理

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-FAC-001 | 打开设施目录 | 固定展示 reverse-proxy 与 storage-share 卡片、类别/可用状态、异步统计；管理进入对应详情 | 不调用不存在的设施 list API；统计加载用骨架 | reverse-proxy 显示路由/节点/资产，storage-share 显示分区/服务器/根目录数 |
| UI-FAC-002 | 目录点击“协调/同步” | reverse proxy 调 reconcile；storage share 仅 enabled 时可调；反馈遵守两阶段 | 未配置 storage share 禁用；无 ID 不显示假 ID | 当前卡片独立 loading，完成后目录重载 |
| UI-FAC-003 | 查看 reverse-proxy 详情 | 展示网关/路由/资产/应用路由统计、route summaries、DNS 同步、静态资产下载、当前 operation/reconcileStopped/lastError | 未知 facilityKind 显示不可用空态；加载失败可重试 | 用户可见服务器使用名称，未知才回退 ID |
| UI-FAC-004 | DNS 同步为 pending | 非编辑态、可见标签每约 3 秒重载设施直至不 pending | 编辑中停止轮询；离开清 timer | 状态从 pending 更新且不覆盖编辑草稿 |
| UI-FAC-005 | 进入入口代理编辑 | 详情页就地展开，config 深链也可直达；先创建 durable session，再显示连续流：网关、域名/路由、静态资产 | 会话失败显示重试；编辑中隐藏详情 reconcile/refresh 操作 | 两个入口得到同一草稿与提交行为 |
| UI-FAC-006 | 修改网关服务器 | 仅从服务器选择；移除网关时自动剪除各域名 origin/primary/relay，并以 warning 告知数量 | 不得留下指向非网关服务器的引用 | payload 中所有关联服务器均属于 deploymentServers |
| UI-FAC-007 | 新增/编辑域名 | 校验合法、非重复域名且至少一台 origin；origin 只能从网关选；可配置 AnyAccess relay/策略/主源 | primary 必须属于 origin，指定 relay 只能来自可用网关 | 错误字段就地显示且弹窗不关闭；成功才标脏 |
| UI-FAC-008 | 新增/编辑设施 path | path 必须以 `/` 开头；static 要选 session asset；redirect 要合法 URL 与 301/302/307/308；proxy_pass 要 http(s) URL并可配 source mode | 各 ruleType 只发送相关字段；非法值就地阻止保存 | model 测试覆盖空/非法 path、redirect、proxy 和缺 asset |
| UI-FAC-009 | 配置设施 path 高级项 | 支持 gzip、请求体、超时、buffering、WebSocket、自定义请求/响应头；redirect 仅保留适用项 | 数值非负，空 header 过滤 | clone/编辑往返不丢 proxy_pass 和 options |
| UI-FAC-010 | 管理静态资产 | 复用 AssetFileManager；文本资产才可编辑；新文本下载名默认跟引用名且可分开改；路由引用用 asset.name | binary/archive 即使扩展名像文本也不可编辑；替换保留 name | 会话下载/正式下载路径正确，删除被引用资产由服务端诊断/冲突阻止 |
| UI-FAC-011 | Preview/Commit/取消设施编辑 | 本地校验→patch→preview→无 blocking diagnostic 才 commit；变更摘要按域名和网关服务器实际对象互斥归类为新增、修改、删除，同一变化不得重复计数；成功回详情并刷新；取消脏草稿先确认 | 冲突提供放弃本地并重载正式配置；保存中整页阻塞；仅默认字段规范化、域名面板顺序或服务器选择顺序变化不得产生假修改 | applyRequested 决定提示，详情展示后端返回配置；diff model 测试覆盖新增、删除、同名内容修改、等量替换和未变更基线 |

## 12. 存储共享设施

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-STO-001 | 打开 storage-share 详情 | 加载配置和状态，显示 enabled/lastError、服务器数、live exports、分区及健康挂载；每 15 秒刷新状态 | 未启用不请求 status；状态失败保留配置且状态 unknown | 隐藏/卸载时停止 timer，手刷重载配置+状态 |
| UI-STO-002 | 切换健康/分区/设置 | 健康列每服务器 Agent/root/NFS/export 与关联应用；分区列来源、节点、路径、target、volume、mount 状态；设置走 `/config` 深链 | 从脏设置切 tab 必须先确认离开 | 返回详情后恢复用户请求的详情 tab |
| UI-STO-003 | 编辑服务器与 root | 至少一行有效 server+绝对 root；禁止重复 server、空格、`..`、`//`、尾 `/`；启用后已保存 server 的 root 只读 | 至少保留一行；新增行默认 `/opt/panel-shared-storage` | 错误逐行显示，无效配置无 PUT |
| UI-STO-004 | 保存设置 | 仅提交有效行、trim root 和当前 version；成功重建基线并加载状态 | 并发/version 冲突保留草稿；返回 lastError 额外 danger toast | dirty 消失，取消按钮恢复服务端配置 |
| UI-STO-005 | 离开脏设置或关闭页 | 路由离开与 beforeunload 均保护；取消保留草稿，确认才导航 | 保存中也不得静默离开 | 三种入口（tab、返回、外部路由）行为一致 |
| UI-STO-006 | 点击同步导出 | 仅 enabled 时提交 reconcile，提示任务已接受并刷新 status | 禁用态不请求；不得宣称全部导出已生效 | 后续状态由 status 接口决定 |
| UI-STO-007 | 卸载设施 | 先确认影响；有引用应用时阻止并列出“去应用编辑解除”入口；无引用才允许 DELETE | 卸载保留历史分区数据；失败不清配置 | 成功 enabled=false、status 清空，引用存在时零 DELETE |
| UI-STO-008 | 下载/删除分区 | 下载授权归档，文件名含应用和服务器；删除若仍引用则阻止并引导编辑应用，否则 danger+强制勾选 | 失败不移除分区；取消无请求 | 成功删除后配置/status 均重载 |

## 13. DNS `/dns/domains`

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-DNS-001 | 搜索、翻页、选择域名 | 服务端分页/q；URL 恢复 search/page/domain；列表显示 Cloudflare provider 状态与更新时间 | 列表失败与空域名分离；切域名立即清旧记录并 abort | 分享 URL 恢复相同域名与页码，迟到记录不串域名 |
| UI-DNS-002 | 新建/编辑域名 | provider 固定 Cloudflare；新建必须 name+API token；编辑 token 留空保持当前 | token 永不回显/出现在列表；错误保持弹窗 | payload 编辑空 token 时省略，成功选中新域名 |
| UI-DNS-003 | 加载记录 | 仅当前域名请求，表格显示 A/AAAA/CNAME/TXT/MX、name、content、TTL、操作 | provider/记录失败在表格内显示错误+重试，并使该域名状态 danger；真实空列表用 no records | 错误、加载、空、内容四态互斥 |
| UI-DNS-004 | 同步记录 | POST refresh，显示 taskId，等待任务完成后重新 GET 当前域名 | 等待中切域名时旧结果不得覆盖；超时/失败保留错误重试 | 请求顺序正确且按钮只锁当前记录区 |
| UI-DNS-005 | 新增/编辑记录 | type、name、content、TTL、proxy mode；name 统一归一，空 TTL 回退 300，content 必填 | 非代理支持由后端校验；失败保留弹窗/记录表 | 成功关闭并重载当前域名记录 |
| UI-DNS-006 | 删除记录或域名 | 分别显示目标和影响说明的 danger 确认；成功只刷新对应作用域 | 域名引用冲突由后端明确显示，不得乐观删除 | 取消零请求；域名删除成功清 selection |

## 14. 证书、自签证书与密钥资产

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-CERT-001 | 切换 domains/self-signed/keys、搜索、翻页、选择 | path 决定模式；page/search/selected 同步 URL；对应服务端列表使用 q，分页固定底部 | 切模式清 selection/page；列表错误与空态分离 | 三个深链刷新可恢复模式、查询与选中项 |
| UI-CERT-002 | 查看域名证书 | 显示覆盖域名、issuer、状态、到期、lastError 与是否签发中 | failed 优先 danger，再区分 expired/expiring | 徽标与 model 测试一致 |
| UI-CERT-003 | 签发或调整重签 | 选择已管理 DNS 域名，配置名称、根域、通配符、子域前缀并实时预览覆盖；重签预填已有覆盖 | name/domain 必填；去重/清空子域；只提示任务创建不宣称签发完成 | POST/PUT payload 的 prefixes 与预览一致，返回证书被选中 |
| UI-CERT-004 | 立即续签或删除域名证书 | renew 提交后台任务并提示接受；delete 先 danger 确认 | 请求失败保留详情；签发中操作由后端拒绝时明确显示 | 成功重载状态，取消删除零请求 |
| UI-CERT-005 | 生成自签 CA | name/commonName 必填，years 数值化并默认 5 | 非法值由后端错误明确反馈 | 成功选中新 CA 并刷新 |
| UI-CERT-006 | 生成 self-signed leaf | 选择 CA，填写 name/commonName、DNS names、IP addresses、days（默认 90） | name/commonName 必填；无 CA/非法 SAN 不得伪成功 | 成功详情显示 kind/fingerprint/expiry/SAN |
| UI-CERT-007 | 重签或删除自签资产 | 重签调用 renew 并选返回 ID；删除先确认 | 引用/父子冲突保留对象并 toast | 成功重载，状态与新到期时间一致 |
| UI-CERT-008 | 打开密钥资产页 | 过滤系统托管资产，仅列用户 CA/TLS/SSH；详情显示算法、fingerprint、reference/child count、expiry 与允许动作 | 系统资产只在设置的系统证书分区；列表不得包含私钥、公钥正文或 metadata | `canDelete/canReissue/canRegenerate/downloadKinds` 控制按钮 |
| UI-CERT-009 | 生成 CA/TLS/SSH | CA/TLS 填 name/commonName/algorithm/RSA size/validity，TLS 另选 parent CA 与 SAN；SSH 填 name/algorithm/RSA size/comment | 生成入口只属于 keys 工作台；必填 name，其他错误显式返回 | 成功选择新 asset，不在 UI 展示私钥正文 |
| UI-CERT-010 | 单资产导入 | 选择 type/name/parent/算法；SSH 只编辑私钥材料，CA/TLS 用 tabs 分别编辑私钥和证书，默认打开私钥 | 导入失败在 Dialog 内保留结构化错误和材料；不得把 secret 写 toast | tabs/CodeEditor 可键盘操作，成功关闭并选中新资产 |
| UI-CERT-011 | 重签/重生成/删除资产 | SSH 用 regenerate，证书用 reissue；根据 capability 禁用；delete 先 danger 确认并由后端做引用安全检查 | 引用中/有子资产时删除失败不移除行 | 有 taskId 才显示，成功后重载能力与 fingerprint |
| UI-CERT-012 | 下载单资产材料 | 仅渲染 downloadKinds，逐项 blob 下载 | 失败不生成文件；不得经 JSON ApiClient 解析二进制 | 文件种类/名称与服务端 Content-Disposition 对应 |
| UI-CERT-013 | 导出资产归档 | 默认导出所选 asset，无选择则所有用户资产；密码必填，先创建 export task 再下载结果 | 系统托管资产不得进入集合；任务/下载失败明确 toast | 请求 assetIds 正确，下载归档可打开 |
| UI-CERT-014 | 批量导入 | 选择支持归档+密码，先 multipart preflight；显示冲突数/ready；无危险冲突可执行，`requiresDangerConfirm` 时必须 danger+强制勾选后 overwrite | 改文件清旧 plan；未 preflight 不出现执行；不得预先 `confirmDanger=true` | 危险执行 payload 仅确认后带确认标志，成功提示真实 taskId |

## 15. 协调记录 `/application-operations`

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-OPS-001 | 搜索应用 ID、筛 status/source、翻页 | 250ms 防抖，服务端过滤，URL 恢复 applicationId/status/source/page/operationId；筛选或翻页清旧详情/抽屉 | 空筛选结果与加载失败分离 | 分享 URL 恢复同一列表和可用详情 |
| UI-OPS-002 | 浏览列表 | 每条显示应用/设施名、action、状态、目标服务器或成功/失败进度、failure summary、来源、时间 | facility-reverse-proxy 显示“入口代理设施”；用户可见标题不得展示原始 operation/application/srv ID | 有 serverName 时绝不回退 ID，长错误截断但详情可见 |
| UI-OPS-003 | 选记录 | GET 完整详情；显示开始/结束、目标结果、失败目标错误码/消息/详情和每服务器 desired/actual/generation/stage/retry | operationId 不在当前页时显示专属提示并可清除；详情失败可重试 | 切记录清旧详情，迟到响应不覆盖新记录 |
| UI-OPS-004 | 查看不一致目标的步骤日志 | 右侧抽屉显示各 stage 的开始时间、耗时、状态、翻译 stage 名与 detail；一致目标不显示日志按钮 | 无 stages 有空态；自由技术 detail 原样显示 | Escape/显式关闭、焦点圈定/恢复、背景锁滚动；遮罩点击不关闭 |

## 16. 系统事件 `/system-events`

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-EVT-001 | 设置 eventType、severity、时间范围、page | URL 恢复全部条件；条件变化页码回 1；默认最近 24h；服务端分页过滤 | 非法 page 回 1，错误清当前失败结果并提供重试 | query 与请求参数一致，清筛选移除相应 query |
| UI-EVT-002 | 选择时间范围 | 双月历选择 from/to，支持时分秒、最近 24h/7d/30d、应用/取消 | 未应用不改列表；from/to 不完整不得发无效范围 | ISO 值进入 URL 和 API，取消保持原值 |
| UI-EVT-003 | 查看事件表 | sticky 表头显示本地时间、severity、翻译 event type、翻译 summary、source；长摘要 Tooltip 查看完整 | 无结果区分有筛选/无筛选；失败显示重试 | 未知类型/摘要回退原文，分页固定底部 |

## 17. 兼容任务中心 `/tasks`

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-TASK-001 | 搜索、状态筛选、翻页、选择 operation/task | 搜索透传后端 q 并跨页匹配；状态含 running/queued/scheduled/failed/failed_retryable/blocked/cancelled/completed；URL 恢复全部状态 | 查询变化页码回 1，列表失败有重试 | 返回/刷新保持同一 operation/task；用户标题不直接显示原始 ID |
| UI-TASK-002 | 自动轮询 | 默认每 5 秒静默刷新列表；可暂停/恢复，隐藏标签暂停，防重入 | 静默刷新保留当前内容和选择，不整页 skeleton | 暂停后无周期请求，恢复后继续 |
| UI-TASK-003 | 查看操作组和执行项 | operation 聚合优先显示 running/failed 状态；每 task 以 summary 为标题、type 为副标题；切 task 清 steps/logs/cursor | 无选择显示引导；迟到详情不串 task | 状态映射覆盖所有 TaskStatus，中文摘要按当前 locale 翻译 |
| UI-TASK-004 | 查看 steps/logs/error | 并行读取 steps 与增量 logs；steps 显示百分比/状态/错误，日志内部换行滚动，error 单独 tab | 加载失败与“无步骤/无日志/无错误”分离并可重试 | 重新选 task 从 cursor=0，静默更新同 task 可增量追加 |
| UI-TASK-005 | 立即运行或重试 | 仅后端 `allowRunNow/allowRetry` 时显示；请求成功提示新 taskId 并刷新组 | 失败不改变旧 task 状态 | 每个按钮只有自身 loading，零权限时入口不存在 |

## 18. 设置 `/settings/*`

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-SET-001 | 进入任一设置分区或刷新 | 并行加载 runtime、server variables、version、system certs、可激活 TLS；分区由 path 决定且独立保存，无全局保存 | 某个辅助接口失败应保留其他成功数据并 toast；runtime 完全不可用显示重试 | 左侧路由可直达六分区，刷新不跳回 general |
| UI-SET-002 | 保存 General/Runtime | 校验各 interval/retention >=1，event retention >= detail retention；保存语言、日志、cleanup、reconcile trace；以已加载 runtime 为基底只合入本分区 | 不得提交其他分区未保存脏值；非法数字不请求 | 成功重新 hydrate，语言立即 setLocale 并更新 html.lang |
| UI-SET-003 | 保存 Security | token expiration 从固定值选，remote timeout >=1；只合入安全字段 | 失败不覆盖 runtime 基线 | 请求中仅安全差异变化 |
| UI-SET-004 | 重置 JWT secret | secret trim 后至少 16 字符，先 danger 确认；成功更新 session、清输入并标记 configured | 短值就地错误；失败保持输入并防止登出假象 | 未确认无请求，成功后新 token/session 生效 |
| UI-SET-005 | 保存 Certificates/Panel HTTPS | 编辑 Panel domain、TLS 资产、ACME email、DNS delay>=0；TLS 选择来自 `/key-assets/tls`，内置 `panel-tls` 保存为空 ID | TLS 列表不得按域名/SAN过滤，不含 Agent 专用证书；domain trim+小写 | 请求仅合入 certificates/panel 分区，成功重新 hydrate |
| UI-SET-006 | 查看/重置系统证书 | 展示 Panel Agent CA/client 和服务器 Agent server cert 的 type/status/fingerprint/有效期/归属；仅 canReset 可点，先 danger 确认后提交任务 | 不得通过普通 key asset API 修改；不可重置项禁用 | toast 显示真实 taskId，列表后续刷新反映状态 |
| UI-SET-007 | 查看 System | 版本、channel、latest、updateAvailable 只读；branding title/subtitle 和 server variables 分别独立保存 | 版本不混入 runtime 保存；变量格式为 `[*]KEY=Name`，空行忽略 | 保存 branding 不提交变量，保存变量不提交 branding |
| UI-SET-008 | 配置备份导出 | 默认加密；关闭加密显示身份可恢复性警示；加密时密码必填，确认后提交 export 并显示 pending | 不得在此假装归档已可下载；失败不进入 pending | 未满足密码时按钮禁用，成功提示真实 exportId |
| UI-SET-009 | 选择还原文件 | FileUploadButton 选 `.panel-backup` 后先 preflight，显示 manifest 版本/文件数；用户另勾覆盖确认并再经 danger Dialog 才 confirm restore | 更换文件清旧 preflight；无 preflight/未勾选不能恢复 | 仅最终确认发送 multipart restore，成功显示 pending/restarting |

## 19. 维护 `/maintenance/backup`

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-MAINT-001 | 进入维护页 | 独立 shell，不读取普通 session；export/restore token 分别存 `sessionStorage.panel.maintenance.export.token` / `.restore.token` | 普通 token 不得授权维护 API，维护 token 不得进入普通 client | 页面无 AppShell，关闭浏览器 session 后维护认证消失 |
| UI-MAINT-002 | 维护登录 | 用当前后端模式对应 token 登录并加载状态；当前模式由后端状态判断，用户不可手切 | export status 失败可尝试 restore status；401 清维护 token并回登录 | 登录后展示实际 mode/phase/capabilities，而非前端猜测 |
| UI-MAINT-003 | 查看状态和轮询 | 显示 phase、progress、revision、startedAt、restart support、manifest/error；按 pollAfterMs（默认2s）轮询 | completed/failed 或隐藏标签暂停；防重复 timer | 终态不再请求，手刷可立即更新 |
| UI-MAINT-004 | canStart/canSubmitPassword/canRetry/canExit | 只按 capability 显示相应动作；密码必填；成功提示“命令已接受”并继续轮询 | 接受不等于完成；失败保留状态/错误 | 页面按钮集合严格等于后端 capabilities |
| UI-MAINT-005 | 清除 pending restore | `canClearPending` 且 phase 非 applying 才显示，必须 danger 确认 | applying 阶段绝不提供；取消零请求 | 成功刷新状态，确认影响可见 |
| UI-MAINT-006 | 下载完成的 export | 仅 canDownload 显示，用当前 export token 进行授权 blob 下载 | 401 退出维护登录；失败不生成文件 | 文件名/内容来自响应，普通 auth header 不混入 |
| UI-MAINT-007 | 退出维护登录 | 停止轮询、清当前模式 token和状态，回维护登录表单 | logout API 失败也应完成本地清理 | 后续 status 请求不再发送旧 token |

## 20. 诊断 `/debug`

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-DBG-001 | 进入诊断页 | 加载 snapshot 与 pprof 状态；snapshot 默认每 8 秒轮询，可暂停/恢复/手刷，隐藏标签暂停 | 并发/迟到响应不可覆盖新快照 | 顶部显示 live/stale 和 collectedAt |
| UI-DBG-002 | snapshot 刷新失败 | 有 lastGoodSnapshot 时继续展示并标 stale，同时 toast；无旧值显示失败空态+重试 | 不得清空为“无数据”或用失败快照覆盖 lastGood | 成功恢复后 stale 消失且更新时间前进 |
| UI-DBG-003 | Runtime tab | 展示 uptime、goroutines、heap、PID、Go version、OS/arch、CPU | 空/未知值使用统一 fallback，不显示 `[object Object]` | 数值与 snapshot 字段一致 |
| UI-DBG-004 | Tasks tab | scalar runtime metrics 与 task definitions 分开；definitions 用可滚动 Table 展示 kind/actions/concurrency/retries/stale/periodic | definitions 数组不得字符串化；无定义有明确空提示 | 任意对象定义都以列呈现 |
| UI-DBG-005 | Database tab | 展示健康数/总数/used 汇总；每数据库显示大小、used/free、健康/错误和表行数/大小 | 单库错误以 danger 状态保留其他库 | 大表清单在卡片内部滚动 |
| UI-DBG-006 | 切换 pprof | Switch PUT enabled，期间禁重复；开启后显示本机 `http://<address>/debug/pprof/` 新窗口链接 | GET/PUT 失败仅 toast，不伪改状态；链接带 `rel=noreferrer` | 返回状态决定 Switch 与链接是否出现 |

## 21. 未找到页面

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-404-001 | 认证用户进入未知 AppShell 路径 | 显示本地化标题、说明、空态和 primary“返回概览” | 不静默改 URL、不白屏、不丢导航 | 点击后到 `/overview`，顶栏标题为 notFound titleKey |

## 22. 覆盖来源

- 全部路由：`web/src/router/index.ts`。
- 页面实现：`web/src/views/auth`、`overview`、`servers`、`credentials`、`resources`、`security`、`applications`、`dns`、`certificates`、`application-operations`、`system-events`、`tasks`、`settings`、`maintenance`、`debug`。
- 操作/API：`web/src/api/*.ts`；DTO 与 capability：`web/src/types/*.ts`。
- 字段校验和派生状态：各页面族 `model.ts`、`assetImportEditor.ts`、`templateLanguage.ts`、`archivePath.ts`。
- 共享交互：`web/src/components/ui`、`patterns/AssetFileManager.vue`、`ServerContextSelector.vue`、`ServerMultiPicker.vue`、`AutoRefreshControl.vue`。
- 自动化证据：页面 model 测试、API client/API 模块测试、`domainRoutes.test.ts` Mock 集成样本，以及共享组件/布局/i18n/主题测试。

## 23. 当前实现与自动化缺口（不得作为放宽验收的依据）

1. servers、applications、dns 当前页码没有完整地从 URL 初始化并同步；部分选择变化也依赖按钮内手写 query。应按 UI-SRV-001、UI-APP-001、UI-DNS-001 统一恢复。
2. applications 编辑器 footer 的取消仍调用 `router.back()`；这会受进入历史影响，与 UI-APP-023 的明确返回目标冲突。
3. application detail/runtime/files 加载失败目前主要用 toast，缺少 UI-APP-002/003 所要求的详情作用域错误空态和重试。
4. certificates 顶部条件渲染目前会在 domains/self-signed 模式出现“生成 SSH”入口；正确契约是 UI-CERT-009：生成密钥资产只属于 keys 工作台。
5. 多个页面危险动作仍由普通 Dialog 承载，未全部使用带统一 impact/checkbox/loading 锁的 ConfirmDialog；需要按对应条目补齐。
6. 除共享组件和纯逻辑外，当前缺少覆盖上述完整页面链路的 Playwright e2e、axe 与多断点视觉回归。尤其 URL 恢复、脏状态离开、后台任务两阶段、上传下载和维护 token 隔离缺少浏览器级证据。
7. DNS、applications、certificates、settings 等复杂页面的表单校验仍有大量依赖后端返回，当前只对部分必填/数值做本地约束；验收需同时验证本地阻止与服务端结构化错误不丢失。
8. `/tasks` 和 `/debug` 为兼容/隐藏入口，仍必须维持可用与可访问，但不应重新加入主导航。
