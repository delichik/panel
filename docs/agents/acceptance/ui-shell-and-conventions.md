# 前端外壳与交互约定验收标准

## 1. 文档定位

本文是前端全局行为的验收契约，不是实现教程，也不是用户操作手册。任何页面、组件、路由、API client、主题、国际化或响应式改动，都不得破坏本文的稳定编号条目。页面专属行为见 [ui-pages.md](ui-pages.md)。

判定优先级：可执行测试与当前 API/类型契约优先于旧功能指引；源码用于补足尚无测试的用户可见行为；现有规范用于约束未来实现。条目中的“必须”表示交付验收条件，而不代表当前实现已全部满足。已知现状缺口列在文末，不得以缺口为理由降低验收标准。

每项均包含触发/前置、必须行为、失败/边界和可验证结果。测试可采用单元、组件或 e2e，但测试和构建命令必须走 `task test:web` / `task build:web`。

## 2. 路由、会话与启动

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-SHELL-001 | 浏览器进入任意路由，session 尚未恢复 | 挂载业务页面前调用会话恢复；守卫不得默认已登录 | 恢复中的网络错误若本地仍有 token，可保留 token 并乐观进入；明确 401/`unauthorized` 必须清空 session | 首次导航不会先闪出受保护数据；401 后 `panel.auth.token` 被移除并转到登录页 |
| UI-SHELL-002 | 未认证且没有可复用 token 访问受保护路由 | 跳转 `/login?redirect=<原始完整路径>`，保留 path 与 query | `/login` 和 `/maintenance/backup` 是公开路由；不得造成重定向循环 | 地址栏含安全编码的原目标，登录后可返回该目标 |
| UI-SHELL-003 | 已认证且无需强制改密访问 `/login` | 自动转到 `/overview` | `passwordChangeRequired=true` 时必须停留登录页并显示改密流程 | 普通已登录用户看不到登录表单；强制改密用户不能进入控制台 |
| UI-SHELL-004 | 登录成功或 session 恢复返回 Bearer token | 保存 token、用户名、认证态和强制改密标志；受保护请求统一注入 `Authorization: Bearer` | 公共登录与公共 branding 请求不得注入旧 token | 刷新后 session 可恢复；抓取请求可区分受保护和 `skipAuth` 请求 |
| UI-SHELL-005 | 任一受保护 API 返回 401（含非 JSON 401） | 统一归一化为未授权，清空 session，并把当前完整路由作为 redirect 转到登录页 | 登录请求自身的 401 不得触发全局跳转；并发 401 不得形成循环 | token 被清除，当前页面退出，登录失败仍留在登录表单 |
| UI-SHELL-006 | 用户执行退出 | 显示全局阻塞加载，尝试后端 logout；无论后端结果如何都清除本地 session，再进入 `/login` | 请求失败不能把用户困在已认证 UI，也不能遗留 token | 退出按钮防重复触发，完成后受保护页面不可继续访问 |
| UI-SHELL-007 | `redirect` query 被用于登录/改密后的跳转 | 仅接受以单个 `/` 开头的站内路径 | 非字符串、非 `/` 开头或 `//` 开头值一律回退 `/overview` | 构造外站或协议相对 redirect 无法跳出应用 |
| UI-SHELL-008 | 应用启动或懒加载路由 chunk | 所有页面族路由懒加载；chunk 网络/旧哈希失败时用 sessionStorage 标志最多整页重试一次，成功导航后清标志 | 永久缺失 chunk 不得无限刷新；其他路由异常不得被误判为 chunk 错误 | 初始包不静态打入页面族；模拟 chunk 错误只刷新一次 |
| UI-SHELL-009 | Vue/Mock/session 等 bootstrap 抛出未处理错误 | 在根节点显示可见的启动失败标题与刷新提示 | 错误兜底不得依赖已经失败的组件树或 i18n | 即使应用未挂载，页面仍有可读错误而非空白屏 |
| UI-SHELL-010 | 访问 `/`、应用/设置父入口 | `/`→`/overview`；`/applications`→`/applications/apps`；`/settings`→`/settings/general`，并保留兼容重定向中原 query | 资源与安全页已并入服务器详情 tabs：`/resources/*` 与 `/security/*` 不再提供重定向，访问落入 404；非 dev 构建不得暴露 Fail2Ban 入口 | 每个别名最终落到唯一规范路径，刷新和分享链接结果一致 |
| UI-SHELL-011 | 访问 AppShell 下不存在的路径 | 保留全局导航并显示本地化 404 空态和“返回概览”操作 | 不得跳出外壳、白屏或伪装成 `/overview` 成功 | 404 页面标题、说明和返回按钮可见，按钮到 `/overview` |

## 3. AppShell、导航与布局

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-SHELL-012 | 认证用户进入控制台，宽度 `>=1024px` | AppShell 高度等于动态视口；导航、约 52px 细顶栏和路由内容区固定；页面级禁止纵向/横向滚动，滚动限于列表、表格、详情、日志、编辑正文 | 页面内容不得撑开 body；长文本不得以整页横滚兜底；覆盖式详情由详情正文承担内部滚动，宽表格在自身容器内横向滚动 | body 尺寸不超过视口，至少一个明确内部区域承担滚动 |
| UI-SHELL-013 | 宽度 `<1024px` | 桌面侧栏隐藏，移动抽屉入口出现；顶部条粘性置顶且层级低于抽屉/Dialog/Toast，保证导航与全局操作随时可达；RouteContent 与页面模板解除桌面裁切，允许页面级滚动；表格列表容器限高（约 60dvh）并保持内部滚动，宽表格允许横向滚动，详情不被长列表推到很远；抽屉关闭按钮与导航行按触屏尺寸放大到 44px | 关键操作、弹窗 footer、下拉菜单不得因视口高度或 overflow 不可达；嵌套滚动区域必须能自然回链到页面滚动；放大触屏目标不得让顶栏在 360px 视口横向溢出 | 断点两侧无内容裁切；窄屏可滚到全部字段和操作，滚动中仍能打开导航；抽屉内导航行实测高度 ≥44px |
| UI-SHELL-014 | 渲染主导航 | 分组及顺序固定为概览；资产（服务器、SSH 凭据、证书、DNS）；应用（应用、设施应用）；设置（操作、任务、日志、设置）。资源与安全不再是独立导航组，其能力并入服务器详情 tabs。侧栏为独立 chrome 面；激活项实心 primary + 反色文字 | `/debug`、`/resources/*`、`/security/*`、`/activity`（仅重定向）和页面内子分区不得加入主菜单；不得恢复淡 tint + 细轨作为唯一激活态 | 导航项、标题和目标与 `navModel` 一致；当前页面族正确高亮且与非激活项对比清晰 |
| UI-SHELL-015 | 桌面用户折叠/展开导航 | 侧栏宽度在 260px/76px 切换；折叠态隐藏文案并用 title/可访问名称保留含义；偏好存 `panel.nav.collapsed`；折叠按钮位于品牌区 | 折叠不得改变页面路由或丢失当前高亮；文案随列宽渐变而不是瞬时重挂载；折叠按钮暴露 `aria-expanded`/`aria-controls` | 刷新后保持折叠偏好，键盘和鼠标均可切换 |
| UI-SHELL-016 | 窄屏打开移动导航 | 抽屉使用 `role=dialog`、`aria-modal=true` 和本地化名称；打开后焦点进入抽屉、Tab 圈定、背景锁滚动且不可交互 | Escape、可见关闭按钮、点击遮罩、选择导航项均应关闭；跨入 1024px 自动关闭 | 关闭后焦点回到触发按钮，body overflow 恢复；组件测试覆盖上述路径 |
| UI-SHELL-017 | 路由变化 | 顶栏显示从 `route.meta.titleKey` 翻译的**次级上下文标签**（muted，非页面主标题）；右侧主题/配色/语言/账户为边框工具簇；路由内容按 path 淡入淡出，query 变化不重挂载页面 | `meta` 不得写用户可见硬编码标题；多根页面不得触发 Transition 警告 | 同一路径改筛选不丢组件状态，换路径才重挂载；顶栏标签与 PageHeader 主标题随语言切换 |
| UI-SHELL-018 | 页面需要标题、返回或操作 | 使用 PageHeader/ConsolePage 语义：PageHeader 为唯一主标题；返回目标由页面明确指定，不用历史栈兜底；同一作用域至多一个 primary，危险操作不与保存同色；实体列表在 ConsolePage 内使用 ListPage，不得再套第二层卡片外框 | 中窄屏标题和 actions 必须堆叠/换行，不得裁切 | 返回落到约定父页；所有关键按钮在 1024px 附近仍可见；列表仅为单一边框层 |
| UI-SHELL-019 | 实体工作台列表与详情 | 一级对象工作台（servers/credentials/dns/certificates/applications/operations/tasks）统一为「表格 + 覆盖式详情」：列表为全宽表格，点击行内「详情」后详情覆盖整个内容区并自带返回操作；列表保持挂载但不可交互，返回后滚动位置与筛选状态不丢。`/logs` 为表格列表 + Dialog 看单条 | 不得恢复 xl 常驻双栏几何；不得在窄屏自动选中首条对象；不得再为日志恢复时间线/覆盖详情/`MasterDetailLayout`；设置导航与 shell 侧栏例外 | 实体工作台保持覆盖式详情；日志页 Dialog 展示单条正文 |

## 4. 主题、语言与展示格式

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-SHELL-020 | 首帧、主题切换或系统配色变化 | 支持 `system/light/dark`，存 `panel.theme.mode`；解析后写 `html[data-theme]` 和 `color-scheme`；system 实时跟随媒体查询 | 非法存储值回退 system；启动遮罩必须在 Vue 前使用同一偏好，避免深色白闪 | 刷新保持选择；系统主题变化只在 system 模式生效 |
| UI-SHELL-021 | 配色方案切换 | 支持 `lighthouse/ocean`，存 `panel.theme.scheme` 并写 `html[data-scheme]` | 非法值回退 lighthouse；状态色不得被方案色替代 | 两种方案在明/暗主题均可用，刷新不丢失 |
| UI-SHELL-022 | 用户点击顶栏语言按钮或保存运行时语言 | 仅在 `en` 与 `zh-CN` 间切换，立即更新 `panel.locale`、全部翻译和 `<html lang>`；顶栏切换必须同步保存 `/settings/runtime` 中的语言，等待期间显示保存状态并防重复提交 | 持久化失败必须回滚界面语言、本地偏好与 `<html lang>` 并显示 danger toast；非法 locale 回退 `en`；业务代码不得自行写 html.lang | 进入控制台后以已持久化的 runtime language 校准本地状态；切换无需刷新，重新读取设置及进程重启后保持新语言 |
| UI-SHELL-023 | 新增或修改用户可见文案 | 通过稳定 i18n key 渲染，en/zh-CN key 集合完全一致、值非空、英文表无中文残留 | 后端/远端自由技术文本可原样展示；路由 meta 仅存 key | `web/src/i18n/i18n.test.ts` 通过，界面不出现缺失 key 字面量 |
| UI-SHELL-024 | 展示任务/系统事件摘要或运行事件类型 | 英文存储摘要在 zh-CN 下经统一翻译辅助函数映射；未命中内容保持原文，不臆造翻译 | 技术标识、镜像名、容器名和自由错误不得被错误替换 | 已知摘要显示中文，未知摘要无损回退；英文界面保持后端原文 |
| UI-SHELL-025 | 展示时间 | 所有页面调用统一格式化，按浏览器本地时区输出 `yyyy-MM-dd HH:mm:ss` | 空值使用调用方给定 fallback；无法解析值原样返回 | 不同页面同一时间戳格式一致，相关单测通过 |
| UI-SHELL-026 | 展示状态、视觉层级或图标 | 状态经 StatusBadge/语义 token 映射 success/warning/danger/info/neutral；主操作用 primary；图标用 lucide | 禁止用品牌色表达健康/错误，禁止业务裸色/自造阴影/旧 UI 框架 | 状态含文本而非只靠颜色；源码无 Naive UI/Vuetify 回流 |

## 5. 加载、空态、错误与异步操作

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-SHELL-027 | 首次整页或列表加载且无旧数据 | 按最终结构显示 Skeleton/Table 骨架；卡片、对话框正文、文本区等待请求时使用 LoadingOverlay 或按钮 loading | 不得只写“加载中”、全页裸 spinner 或让布局塌缩 | 慢请求期间结构稳定且有 `aria-busy`/可读 loading label（适用时） |
| UI-SHELL-028 | 已有内容后手动刷新/轮询 | 保留旧内容，只在对应按钮或局部区域显示 loading；刷新成功原位替换 | 切换服务器/对象时例外：必须立即清除旧对象数据，不能展示串台内容 | 同对象刷新无白屏；换对象期间看不到上一对象详情 |
| UI-SHELL-029 | 同一资源连续发起多个列表/详情请求 | 用 latest-request guard 或 AbortController 忽略迟到响应；卸载时终止可取消请求 | Abort 不得显示成业务错误；迟到响应不得覆盖新筛选/新对象 | 人为打乱响应顺序后 UI 仍对应最后一次选择 |
| UI-SHELL-030 | 页面进入无结果状态 | 区分：真实无数据（给创建/配置引导）、筛选为空（`common.filterEmpty` / `common.filterEmptyHint`）、无选中（提示选择）、无权限/能力不足（说明原因）；表内空态用 EmptyState `inset`，页面级失败用 `page` | 加载失败不得伪装成“暂无数据”；有搜索词时不得显示“暂无数据”创建文案 | 每类状态有不同标题/说明/可用下一步 |
| UI-SHELL-031 | 列表、详情或区块加载失败 | 在失败作用域保留错误空态和重试；同时用 danger toast 报异常 | 有旧快照时保留旧快照并标记陈旧；局部失败不得清空无关页面区块 | 重试只重载失败作用域；错误内容可见且不会静默吞掉 |
| UI-SHELL-032 | 操作成功、短提示或异常 | 成功/已复制/任务已提交用顶部 toast；捕获的异常统一 danger toast；字段校验、诊断、引用冲突仍就地显示 | 不用会消失的 toast 承载唯一的多行结构化错误；不保留页面级成功横幅 | toast 可关闭且关闭按钮本地化；错误字段旁仍能定位原因 |
| UI-SHELL-033 | API 接受后台任务/协调请求 | 文案只承诺“已接受/已提交”；返回真实 task/operation ID 才显示 ID；需要等待本地快照的页面按契约等待任务完成后重载 | 没有 ID 时不得显示“不可用”假 ID，不得把接受等同执行成功 | toast 与响应字段一致；刷新后仍能从任务/协调记录恢复进度 |
| UI-SHELL-034 | 自动轮询启用 | 防重入、页面不可见时暂停、恢复可见后继续；共享刷新设置仅用 `panel.autoRefresh`（off/5s/10s） | 已有在途请求时跳过，不通过不断 abort 自伤；离开页面清 timer | 后台标签无新请求；同时最多一笔同类轮询请求 |

## 6. 列表、表单、浮层、上传下载

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-SHELL-035 | 页面含搜索、筛选、排序、分页或选中主对象 | 可分享状态进入 URL query，初始化、刷新、前进/后退均能恢复；搜索/筛选改变时页码回 1 | query 中非法页码规范为正整数 1；未知 query 尽量保留 | 复制 URL 新开页面得到同一查询和选择；清空条件移除对应 query |
| UI-SHELL-036 | 服务端分页列表 | 查询把 `page/pageSize/q` 等透传后端；分页栏固定在列表底部，不随列表正文滚动 | 不得只筛当前页；请求返回校正页码时 UI 与 URL 同步 | 跨页搜索可命中，上一页/下一页 loading 防重复，摘要总数正确 |
| UI-SHELL-037 | 表单本地输入无效 | 字段旁显示本地化错误并设置 invalid；提交/预览禁用或拒绝调用 API | 服务端校验另在提交区/诊断区展示；提交中锁定重复操作 | 无效输入不产生写请求，修正后错误消失且可提交 |
| UI-SHELL-038 | 编辑器存在未提交修改 | 显示脏状态；路由离开和浏览器关闭触发保护；保存/提交期间不可关闭或离开 | 用户明确放弃后才离开；取消确认必须原地保留草稿 | 修改任意受管字段/文件后显示未保存，离开确认路径可重复验证 |
| UI-SHELL-039 | 打开普通 Dialog | Teleport 到 body；固定 header/footer，唯一 body 内滚动；焦点移入并圈定，Escape/关闭/取消退出并恢复触发点；遮罩点击不关闭；打开期间锁定背景滚动（引用计数，嵌套浮层只在最后一个关闭时恢复） | `closeDisabled`/保存中禁止 Escape 和关闭；large 在窄屏 footer 仍可达；Escape 在焦点意外离开面板时仍须生效 | 键盘组件测试通过，点击遮罩保持打开，关闭后焦点恢复且 body overflow 复原 |
| UI-SHELL-040 | 危险、不可逆或覆盖操作 | 使用确认对话框，明确影响范围并使用 danger；高风险动作要求勾选后才启用确认；loading 时禁用关闭和重复确认 | 引用占用/能力不足时阻止确认并给解决路径 | 未勾选无请求；确认一次只发一笔请求；关闭后勾选状态重置 |
| UI-SHELL-041 | 打开 Select、Dropdown、Tabs | Select/Dropdown 浮层 Teleport body、fixed 定位、视口碰撞收敛；菜单紧凑；Select 的 `aria-label` 等 ARIA 属性必须落到 combobox 控件而不是被丢弃；Tabs 具 tablist/tab/panel 关联 | 必须支持方向键、Home/End、Escape（适用）、跳过 disabled；Dropdown 在 Tab 离开时关闭并同步 `aria-expanded`；中窄屏 tabs 可横滚 | 键盘组件测试通过；浮层不被业务 overflow 裁切 |
| UI-SHELL-042 | 文件选择、上传、下载 | 文件选择统一 FileUploadButton；下载统一 DownloadButton/Blob helper，保留服务端文件名；blob/multipart/fetch helper 复用 authHeaders | 取消选择不发请求；错误不生成损坏下载；不得把二进制塞进 JSON client | 上传请求类型和 token 正确；下载生成可打开文件并释放 object URL |
| UI-SHELL-043 | 使用 CodeEditor 编辑文本 | 支持 Ctrl/Cmd+F、Ctrl/Cmd+H、F3/Shift+F3 或 Ctrl/Cmd+G、Escape；搜索选项和替换文案随 locale；只读时隐藏替换 | 外部 model 更新不得反向误发 change；非法 UTF-8/加载失败必须显式错误 | CodeEditor 单测通过，查找/替换可键盘完成 |
| UI-SHELL-044 | 页面含批量操作 | 仅在至少一个有效选中项时显示批量栏/按钮与计数；操作结束按页面契约刷新选择和数据 | 空选择不留占位、不发请求；不可操作项不得进入有效选择 | 选第一个项后操作出现，清空选择后立即消失 |

## 7. API、Mock、可访问性与动效

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-SHELL-045 | JSON API 返回响应 | ApiClient 只接受 `{data}` 或 `{error}` envelope，并支持 204 No Content | HTML、非 JSON、解析失败、缺 data、网络、Abort 都抛明确 ApiError，不得返回伪 fallback | client 单测覆盖成功、结构化错误、HTML、204、token 与 401 |
| UI-SHELL-046 | Mock 模式启动 | 仅 `VITE_PANEL_TEST_MODE=true` 安装；默认给已认证演示 session，`AUTH=true` 才验证登录/token/强制改密/JWT | 未实现路由必须 `mock_route_not_found`，不得假成功 | 正式路径与 envelope 一致；代表性正常、空、错误、冲突、长文本和分页样本可复现 |
| UI-SHELL-047 | 用户只用键盘或辅助技术 | 所有图标按钮有本地化名称/Tooltip；输入有可关联 label；选中项用 `aria-current`/ARIA 状态；状态和错误不用颜色作唯一信息 | 装饰图标 `aria-hidden`; 焦点环不可移除；动态日志/关键进度适用时 `aria-live` | Tab 顺序可完成核心操作，读屏能说出按钮、Dialog、tab 和状态 |
| UI-SHELL-048 | 异步列表首次填充或 UI 状态变化 | 使用统一 motion token（含 `--panel-motion-hover-y` / `enter-y` / `press-scale`）；列表仅首屏/新增项交错入场，已有内容刷新不重播；对话框、菜单、抽屉、toast 成对进退场；进场动画不得在结束后继续压制 hover/按压 transform；Button/IconButton/可交互卡片 hover 可轻微上移，列表/菜单项仅变色 | 不得用位移动画改变尺寸/滚动结构，不做循环装饰动画；不确定进度可用位移进度条但不改变布局；业务层禁止裸 `shadow-xl`/`shadow-2xl` | 刷新列表无整屏重播，进入/退出浮层视觉成对，控件入场结束后仍有 hover/按压反馈 |
| UI-SHELL-049 | 系统启用 `prefers-reduced-motion: reduce` | 关闭位移和骨架动画，把 transition/animation 降为近似无动画 | 降级不能影响焦点时序、挂载或可用性 | 媒体查询下无明显移动/闪烁，所有操作结果不变 |
| UI-SHELL-050 | 对前端行为做交付验收 | 至少覆盖共享逻辑单测、关键组件交互、页面关键 e2e、axe 可访问性；三档桌面加一档窄屏做布局回归 | 纯文档改动不跑测试/编译；业务改动只跑影响范围但不能省略关键链路 | 证据可追溯到稳定编号；`task test:web` / 必要时 `task build:web` 通过 |
| UI-SHELL-051 | 用户触发跨路径导航，新页面路由代码块或守卫尚未完成 | 在导航解析开始时立即高亮目标主导航项并显示进行中图标；顶栏显示本地化的“正在打开”目标标题和可访问进度指示 | 导航成功、取消、守卫拒绝或路由代码块加载失败时必须清除进行中状态；动效降级不得隐去状态文本 | 慢速解析期间旧页面可保留，但点击后立即可见目标高亮、图标、标题与 `aria-busy`/progressbar；完成或失败后均无残留加载态 |
| UI-SHELL-052 | 渲染主导航（桌面侧栏或移动抽屉） | 桌面与抽屉复用同一导航实现，分组、图标、进行中指示和激活样式一致；当前页族在深层子路径上仍以 `aria-current="page"` 标记并显示左侧激活条 | 两个入口不得各自漂移出不同标签/状态；折叠态文案仍留在无障碍树中 | 组件测试断言深链归属项带 `aria-current`、折叠标签仍是可读名称、点击发出导航事件 |
| UI-SHELL-053 | 键盘用户进入控制台 | 外壳首个可聚焦元素是本地化“跳到主要内容”链接，激活后把焦点移动到路由内容区 | 链接在未激活时不得占据视觉空间或进入其他焦点圈定逻辑 | 首次 Tab 出现跳转链接，激活后焦点进入 `#main-content` |
| UI-SHELL-054 | MasterDetailLayout 高度契约（遗留模板） | `MasterDetailLayout` 若仍被测试或遗留调用，自己承担 `h-full`/`min-h-0` 高度契约；详情区用 `grid-rows-[auto_minmax(0,1fr)]`（有可选横幅时横幅必须放进内部滚动区），页面不得再靠 `min-h-[600px+]` 撑高来换取滚动 | 操作/任务/日志交付页不得再接入；详情区在无横幅时也必须由正文承担内部滚动；桌面不得因此产生页面级滚动 | 模板组件测试覆盖高度契约 |
| UI-SHELL-055 | 用户打开、切换或关闭覆盖式详情 | 详情按选中对象 key 播放一次进场（`detail-swap-*` 仅进场）；关闭详情不播放拖沓退场，列表即时恢复且保留原滚动位置；同一对象刷新不重播 | 不得让出场动画延迟返回反馈；不得在刷新时清空详情正文；不得在打开/关闭详情时重置列表筛选、搜索或分页 | 连续打开不同对象每次都有进场；同对象数据刷新无重播；关闭后列表位置与筛选不变 |
| UI-SHELL-056 | 产生成功/异常 toast | 顶部 toast 带语义图标；可见堆叠有上限，超出时淘汰最旧一条；悬停或焦点进入时暂停自动消失，移出后按剩余时间继续；组件卸载清理全部计时器；桌面堆叠位于 56px 顶栏之下 | 暂停不得把剩余时间重置为完整时长；淘汰/关闭都必须清理计时器与 DOM | toast 组件测试覆盖自身关联动作；手动悬停 5 秒后 toast 仍按剩余时间消失 |
| UI-SHELL-057 | 多个浮层同时打开或焦点离开浮层面板 | 只有最上层浮层响应文档级 Escape；下层浮层不得被连带关闭；焦点在面板外时最上层浮层仍可 Escape 关闭 | 关闭后仍按 UI-SHELL-039/016 恢复焦点与背景滚动 | `useOverlayBehavior` 测试覆盖 LIFO 背景锁与面板外 Escape |
| UI-SHELL-058 | 紧凑视口（`<xl`）实体工作台 | 操作/任务/日志与其它实体工作台一律使用覆盖式详情；窄屏由 `TableDetailLayout` 覆盖几何承担 | 不得再为日志族恢复 `MasterDetailLayout` 单视图；未传返回文案的页面不得被模板强制单视图 | 实体工作台组件测试断言覆盖式详情 |
| UI-SHELL-059 | 列表页点击「详情」打开覆盖式详情 | 详情覆盖整个内容区（含列表页标题栏），同页不换路由；选中参数进入 URL query（`server/credential/domain/selected/application`）并在刷新、分享、前进/后退时恢复；打开详情创建一条历史，浏览器 Back 等同关闭详情；页内返回清空选中与 tab 参数 | 详情打开时列表必须保持挂载但 `inert` + `aria-hidden`，不得可聚焦、不得响应点击；非法/过期选中参数规范化或回落到列表；选中对象不在当前页时按需单独补载详情 | URL 与页面显示一致；关闭后列表滚动位置、筛选、搜索、分页保持不变 |
| UI-SHELL-060 | 覆盖式详情打开与关闭的键盘与焦点 | 打开时焦点进入详情（返回按钮或详情容器），Escape 关闭且与内层 Dialog/Dropdown 组成 LIFO（内层优先）；关闭后焦点归还触发行「详情」按钮；返回按钮使用本地化 `common.backToList` | 覆盖式详情不得锁背景滚动；Escape 不得穿透关闭内层浮层；触发元素已移除时焦点回落到可聚焦的页面标题 | 键盘可完成打开→操作→返回；关闭后焦点为触发按钮 |
| UI-SHELL-061 | 表格列表行操作区 | 每行固定提供「详情」入口（操作列按钮），并把高频操作（如编辑、测试连接、续签、同步、容器生命周期）直接平铺为行内按钮；只有低频、危险或需要确认的操作才收进行内「更多」菜单；危险操作必须确认（高风险勾选）；单个操作按（行, 操作）记录 loading 并只禁用该行该项；名称列使用可聚焦控件（`TableDetailName`）打开同一覆盖式详情，等价于「详情」按钮 | 不得把所有操作一律收进「更多」而隐藏常用入口；行内直接可见操作（不含「详情」与容器生命周期按钮）建议不超过 2 个；表格行本身不可点击，不得依赖整行点击；名称点击不得替代危险确认；禁用项必须给出原因或可访问名称；行级操作失败不得清空表格其余行 | 组件测试覆盖行内常用按钮、名称打开详情、菜单、确认、禁用与逐行 loading |
| UI-SHELL-062 | 服务器详情 tabs | 一级 tab 固定为 概况/监控/Agent/Tailscale/容器/镜像/网络/存储卷/软件包/防火墙；tab 首次激活才请求并在详情存活期内缓存；切换服务器或关闭详情清空旧服务器数据并忽略迟到响应；网络/存储卷首次打开且无快照时自动刷新；监控轮询仅在详情打开且该 tab 激活时运行；防火墙内 UFW/Fail2Ban 子视图选择进 URL（Fail2Ban 仅 dev） | 不得用页面级滚动承载 tab 内容；切 tab 不得清空该服务器已加载的快照；能力不足在对应 tab 内给阻断原因而不是隐藏 tab | tab 切换无重复请求；切换服务器无上一服务器数据；深链 `?server=&tab=` 直达对应 tab |

## 8. 覆盖来源

- 路由与会话：`web/src/router/index.ts`、`web/src/stores/session.ts`、`web/src/api/client.ts`、`web/src/views/auth/LoginPage.vue`。
- 外壳与响应式：`web/src/components/shell/AppShell.vue`、`NavList.vue`、`navModel.ts`、页面模板、`web/src/styles/main.css`。
- 主题与 i18n：`web/src/design/theme.ts`、`web/index.html`、`web/src/i18n/index.ts`、`docs/agents/i18n-guide.md`。
- 交互 primitives/patterns：`web/src/components/ui/`、`web/src/components/patterns/`、`web/src/composables/useOverlayBehavior.ts`、`useAutoRefresh.ts`。
- 表格与覆盖式详情：`web/src/components/ui/Table.vue`、`web/src/components/templates/TableDetailLayout.vue`、`web/src/views/servers/tabs/`。
- 自动化证据：`client.test.ts`、`AppShell.test.ts`、`NavList.test.ts`、`MasterDetailLayout.test.ts`、`Table.test.ts`、`TableDetailLayout.test.ts`、`ConsolePage.test.ts`、`views/servers/index.test.ts`、`views/servers/tabs/*.test.ts`、`interactions.test.ts`、`layout-i18n.test.ts`、`main.test.ts`、`theme.test.ts`、`i18n.test.ts`、`datetime.test.ts`、`useOverlayBehavior.test.ts`、`AssetFileManager.test.ts`。

## 9. 当前已知缺口（不得降级为设计）

1. 现有前端自动化以单元/组件测试为主，规范所要求的页面族 Playwright e2e、axe 和多断点视觉回归尚未系统恢复，因此 UI-SHELL-035、038、047、050 的页面级证据不足。
2. 启动失败兜底发生在 i18n 初始化不可用阶段，目前只有英文；这是 bootstrap 灾难兜底例外，不可复制到正常页面文案。
3. AppShell 移动抽屉当前且已有测试固定为“点击遮罩关闭”，普通 Dialog 则明确不响应遮罩；后续不得把两种浮层语义混用。
4. `AssetFileManager.vue` 与 `views/applications/index.vue` 仍各保留一份 `.item-row` 局部动效 CSS（过渡与 hover 已按触屏策略加 `@media (hover: hover)` 保护，但仍是 `motion-list-item` 的重复实现）；后续应收敛为共享工具类，避免两处漂移。
5. 覆盖式详情的浏览器前进/后退恢复与跨页深链（选中对象不在当前页）依赖组件测试，尚未有页面级 e2e 证据。
