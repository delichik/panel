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
| UI-SHELL-010 | 访问 `/`、旧安全入口、资源/应用/设置父入口 | `/`→`/overview`；`/security[/firewall]`→`/resources/firewall`；`/resources`→packages；`/applications`→apps；`/settings`→general，并保留兼容重定向中原 query | 非 dev 的 Fail2Ban 路由必须回到 firewall；dev 才可直达 Fail2Ban | 每个别名最终落到唯一规范路径，刷新和分享链接结果一致 |
| UI-SHELL-011 | 访问 AppShell 下不存在的路径 | 保留全局导航并显示本地化 404 空态和“返回概览”操作 | 不得跳出外壳、白屏或伪装成 `/overview` 成功 | 404 页面标题、说明和返回按钮可见，按钮到 `/overview` |

## 3. AppShell、导航与布局

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-SHELL-012 | 认证用户进入控制台，宽度 `>=1024px` | AppShell 高度等于动态视口；导航、56px 顶栏和路由内容区固定；页面级禁止纵向/横向滚动，滚动限于列表、表格、详情、日志、编辑正文 | 页面内容不得撑开 body；长文本不得以整页横滚兜底 | body 尺寸不超过视口，至少一个明确内部区域承担滚动 |
| UI-SHELL-013 | 宽度 `<1024px` | 桌面侧栏隐藏，移动抽屉入口出现；RouteContent 与页面模板解除桌面裁切，双栏折叠单栏并允许页面级滚动 | 关键操作、弹窗 footer、下拉菜单不得因视口高度或 overflow 不可达 | 断点两侧无内容裁切；窄屏可滚到全部字段和操作 |
| UI-SHELL-014 | 渲染主导航 | 分组及顺序固定为概览；资产（服务器、SSH 凭据、证书、DNS）；应用（应用、设施应用、协调记录）；资源（包、容器、镜像、网络、卷、防火墙，dev 再含 Fail2Ban）；设置（系统事件、设置） | `/tasks`、`/debug` 和页面内子分区是兼容深链，不得加入主菜单 | 导航项、标题和目标与 `navModel` 一致；当前页面族正确高亮 |
| UI-SHELL-015 | 桌面用户折叠/展开导航 | 侧栏宽度在 260px/76px 切换；折叠态隐藏文案并用 title/可访问名称保留含义；偏好存 `panel.nav.collapsed` | 折叠不得改变页面路由或丢失当前高亮 | 刷新后保持折叠偏好，键盘和鼠标均可切换 |
| UI-SHELL-016 | 窄屏打开移动导航 | 抽屉使用 `role=dialog`、`aria-modal=true` 和本地化名称；打开后焦点进入抽屉、Tab 圈定、背景锁滚动且不可交互 | Escape、可见关闭按钮、点击遮罩、选择导航项均应关闭；跨入 1024px 自动关闭 | 关闭后焦点回到触发按钮，body overflow 恢复；组件测试覆盖上述路径 |
| UI-SHELL-017 | 路由变化 | 顶栏标题从 `route.meta.titleKey` 翻译；路由内容按 path 淡入淡出，query 变化不重挂载页面 | `meta` 不得写用户可见硬编码标题；多根页面不得触发 Transition 警告 | 同一路径改筛选不丢组件状态，换路径才重挂载；标题随语言切换 |
| UI-SHELL-018 | 页面需要标题、返回或操作 | 使用 PageHeader/ConsolePage 语义：返回目标由页面明确指定，不用历史栈兜底；同一作用域至多一个 primary，危险操作不与保存同色 | 中窄屏标题和 actions 必须堆叠/换行，不得裁切 | 返回落到约定父页；所有关键按钮在 1024px 附近仍可见 |
| UI-SHELL-019 | 一级对象选择工作台 | 使用统一 MasterDetail 几何：默认单列，`xl` 起左 360px、右侧弹性；两栏均 `min-width/min-height:0` 并由业务区内部滚动 | 不得页面私改左栏宽度；设置导航、任务内层 280px 执行列表和 shell 侧栏例外 | servers/credentials/applications/certificates/dns/tasks/security/resources 均保持一致主从几何 |

## 4. 主题、语言与展示格式

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-SHELL-020 | 首帧、主题切换或系统配色变化 | 支持 `system/light/dark`，存 `panel.theme.mode`；解析后写 `html[data-theme]` 和 `color-scheme`；system 实时跟随媒体查询 | 非法存储值回退 system；启动遮罩必须在 Vue 前使用同一偏好，避免深色白闪 | 刷新保持选择；系统主题变化只在 system 模式生效 |
| UI-SHELL-021 | 配色方案切换 | 支持 `lighthouse/ocean`，存 `panel.theme.scheme` 并写 `html[data-scheme]` | 非法值回退 lighthouse；状态色不得被方案色替代 | 两种方案在明/暗主题均可用，刷新不丢失 |
| UI-SHELL-022 | 用户点击顶栏语言按钮或保存运行时语言 | 仅在 `en` 与 `zh-CN` 间切换，存 `panel.locale`，立即更新全部翻译和 `<html lang>` | 非法 locale 回退 `en`；业务代码不得自行写 html.lang | 切换无需刷新；刷新前的内联脚本即应用正确 lang |
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
| UI-SHELL-030 | 页面进入无结果状态 | 区分：真实无数据（给创建/配置引导）、筛选为空（提示调整条件）、无选中（提示选择）、无权限/能力不足（说明原因） | 加载失败不得伪装成“暂无数据” | 每类状态有不同标题/说明/可用下一步 |
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
| UI-SHELL-039 | 打开普通 Dialog | Teleport 到 body；固定 header/footer，唯一 body 内滚动；焦点移入并圈定，Escape/关闭/取消退出并恢复触发点；遮罩点击不关闭 | `closeDisabled`/保存中禁止 Escape 和关闭；large 在窄屏 footer 仍可达 | 键盘组件测试通过，点击遮罩保持打开，关闭后焦点恢复 |
| UI-SHELL-040 | 危险、不可逆或覆盖操作 | 使用确认对话框，明确影响范围并使用 danger；高风险动作要求勾选后才启用确认；loading 时禁用关闭和重复确认 | 引用占用/能力不足时阻止确认并给解决路径 | 未勾选无请求；确认一次只发一笔请求；关闭后勾选状态重置 |
| UI-SHELL-041 | 打开 Select、Dropdown、Tabs | Select/Dropdown 浮层 Teleport body、fixed 定位、视口碰撞收敛；菜单紧凑；Tabs 具 tablist/tab/panel 关联 | 必须支持方向键、Home/End、Escape（适用）、跳过 disabled；中窄屏 tabs 可横滚 | 键盘组件测试通过；浮层不被业务 overflow 裁切 |
| UI-SHELL-042 | 文件选择、上传、下载 | 文件选择统一 FileUploadButton；下载统一 DownloadButton/Blob helper，保留服务端文件名；blob/multipart/fetch helper 复用 authHeaders | 取消选择不发请求；错误不生成损坏下载；不得把二进制塞进 JSON client | 上传请求类型和 token 正确；下载生成可打开文件并释放 object URL |
| UI-SHELL-043 | 使用 CodeEditor 编辑文本 | 支持 Ctrl/Cmd+F、Ctrl/Cmd+H、F3/Shift+F3 或 Ctrl/Cmd+G、Escape；搜索选项和替换文案随 locale；只读时隐藏替换 | 外部 model 更新不得反向误发 change；非法 UTF-8/加载失败必须显式错误 | CodeEditor 单测通过，查找/替换可键盘完成 |
| UI-SHELL-044 | 页面含批量操作 | 仅在至少一个有效选中项时显示批量栏/按钮与计数；操作结束按页面契约刷新选择和数据 | 空选择不留占位、不发请求；不可操作项不得进入有效选择 | 选第一个项后操作出现，清空选择后立即消失 |

## 7. API、Mock、可访问性与动效

| 编号 | 触发 / 前置 | 必须行为 | 失败 / 边界 | 可验证结果 |
| --- | --- | --- | --- | --- |
| UI-SHELL-045 | JSON API 返回响应 | ApiClient 只接受 `{data}` 或 `{error}` envelope，并支持 204 No Content | HTML、非 JSON、解析失败、缺 data、网络、Abort 都抛明确 ApiError，不得返回伪 fallback | client 单测覆盖成功、结构化错误、HTML、204、token 与 401 |
| UI-SHELL-046 | Mock 模式启动 | 仅 `VITE_PANEL_TEST_MODE=true` 安装；默认给已认证演示 session，`AUTH=true` 才验证登录/token/强制改密/JWT | 未实现路由必须 `mock_route_not_found`，不得假成功 | 正式路径与 envelope 一致；代表性正常、空、错误、冲突、长文本和分页样本可复现 |
| UI-SHELL-047 | 用户只用键盘或辅助技术 | 所有图标按钮有本地化名称/Tooltip；输入有可关联 label；选中项用 `aria-current`/ARIA 状态；状态和错误不用颜色作唯一信息 | 装饰图标 `aria-hidden`; 焦点环不可移除；动态日志/关键进度适用时 `aria-live` | Tab 顺序可完成核心操作，读屏能说出按钮、Dialog、tab 和状态 |
| UI-SHELL-048 | 异步列表首次填充或 UI 状态变化 | 使用统一 motion token；列表仅首屏/新增项交错入场，已有内容刷新不重播；对话框、菜单、抽屉、toast 成对进退场 | 不得用位移动画改变尺寸/滚动结构，不做循环装饰动画 | 刷新列表无整屏重播，进入/退出浮层视觉成对 |
| UI-SHELL-049 | 系统启用 `prefers-reduced-motion: reduce` | 关闭位移和骨架动画，把 transition/animation 降为近似无动画 | 降级不能影响焦点时序、挂载或可用性 | 媒体查询下无明显移动/闪烁，所有操作结果不变 |
| UI-SHELL-050 | 对前端行为做交付验收 | 至少覆盖共享逻辑单测、关键组件交互、页面关键 e2e、axe 可访问性；三档桌面加一档窄屏做布局回归 | 纯文档改动不跑测试/编译；业务改动只跑影响范围但不能省略关键链路 | 证据可追溯到稳定编号；`task test:web` / 必要时 `task build:web` 通过 |

## 8. 覆盖来源

- 路由与会话：`web/src/router/index.ts`、`web/src/stores/session.ts`、`web/src/api/client.ts`、`web/src/views/auth/LoginPage.vue`。
- 外壳与响应式：`web/src/components/shell/AppShell.vue`、`navModel.ts`、页面模板、`web/src/styles/main.css`。
- 主题与 i18n：`web/src/design/theme.ts`、`web/index.html`、`web/src/i18n/index.ts`、`docs/agents/i18n-guide.md`。
- 交互 primitives/patterns：`web/src/components/ui/`、`web/src/components/patterns/`、`web/src/composables/useOverlayBehavior.ts`、`useAutoRefresh.ts`。
- 自动化证据：`client.test.ts`、`AppShell.test.ts`、`interactions.test.ts`、`layout-i18n.test.ts`、`main.test.ts`、`theme.test.ts`、`i18n.test.ts`、`datetime.test.ts`、`useOverlayBehavior.test.ts`、`AssetFileManager.test.ts`。

## 9. 当前已知缺口（不得降级为设计）

1. 现有前端自动化以单元/组件测试为主，规范所要求的页面族 Playwright e2e、axe 和多断点视觉回归尚未系统恢复，因此 UI-SHELL-035、038、047、050 的页面级证据不足。
2. 若干业务页仍使用普通 `Dialog` 手写危险确认，而不是统一 `ConfirmDialog`；影响说明和高风险勾选并非每处齐全，应按 UI-SHELL-040 收敛。
3. 启动失败兜底发生在 i18n 初始化不可用阶段，目前只有英文；这是 bootstrap 灾难兜底例外，不可复制到正常页面文案。
4. AppShell 移动抽屉当前且已有测试固定为“点击遮罩关闭”，普通 Dialog 则明确不响应遮罩；后续不得把两种浮层语义混用。
5. 部分列表页的 URL 同步实现不完整，具体列在 `ui-pages.md`；验收以 UI-SHELL-035 为统一目标。
