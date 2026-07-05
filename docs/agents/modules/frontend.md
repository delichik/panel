# 前端模块

## 适用场景

修改 Vue 页面、共享组件、前端 API client、类型定义、Pinia store、路由、样式、图表或前端测试时，先读本文档。

## 关键入口

- 前端入口：`web/src/main.ts`
- 根组件：`web/src/app/App.vue`
- 布局：`web/src/layouts/AppLayout.vue`
- 路由：`web/src/router/index.ts`
- 页面入口：`web/src/views/`
- API client：`web/src/api/client.ts`
- API 模块：`web/src/api/`
- API 类型：`web/src/types/api.ts`
- 本地 Mock API：`web/src/mocks/`
- 状态：`web/src/stores/`
- 共享组件：`web/src/components/`
- 通用组合逻辑：`web/src/composables/`
- 多语言：`web/src/i18n/index.ts`
- 样式：`web/src/styles/main.css`、`web/src/theme.ts`
- 可复用组件设计规范：`docs/agents/specifications/frontend/INDEX.md`

## 技术约定

- 前端使用 Vue 3、Vue Router、Pinia、Vuetify、ECharts 和 Vitest。
- 新增页面或共享组件前先查阅 `docs/agents/specifications/frontend/INDEX.md`，复用已有基础组件、组合模式和响应式规则。
- 页面按菜单层级组织在 `web/src/views/` 下；每个菜单页面使用独立目录和 `index.vue` 入口。旧路径兼容重定向不保留重复导航入口。
- 仅在同一菜单组内复用的实现放在对应 `web/src/views/<group>/_shared/`；跨页面共享组件继续放在 `web/src/components/`。
- 全局布局在 `web/src/layouts/AppLayout.vue`；侧边导航列表必须在抽屉内部独立滚动。
- 全局页头根据系统版本 API 返回的构建通道展示环境标识；`dev` 通道在标题旁显示紧凑 DEV Chip，正式通道不显示。
- 全局主题偏好只保存在当前浏览器，由 `web/src/theme.ts` 管理；支持自动跟随系统、手动浅色/深色，以及明暗共用或分别选择内置主题预设。旧 `linux-panel-theme` 值会迁移为对应手动模式。
- 页头只展示主题和账户图标入口；用户名与退出登录操作收纳在账户菜单中，不在页头常驻展示。
- 路由标题使用 `meta.titleKey`，不要在路由元信息里写用户可见文案。
- 新增或修改用户可见文案必须走 `useI18n()` 和 `web/src/i18n/index.ts`，并更新多语言状态文档。
- 持久化 UI 配置只保存稳定值，不保存已经翻译的标题或说明。
- API 调用经 `ApiClient`，默认 base URL 是 `/api/v1`；后端 API 变更时同步更新 `web/src/api/`、`web/src/types/api.ts` 和相关测试。
- 全量备份主流程是在正常设置页写 pending export 并提示重启；重启后的启动期维护服务使用 `/maintenance/backup` 展示导出进度、密码输入和下载入口。
- 不依赖后端的前端测试模式通过 `task run:web:test` 启动；该任务设置 `VITE_PANEL_TEST_MODE=true`，由 `web/src/mocks/` 接管 `/api/v1` 请求并自动建立演示会话。
- Mock API 必须保持统一 JSON envelope，并与 `web/src/api/` 的现有路径和响应结构一致；主要页面新增或修改接口时同步维护 Mock 路由和种子数据。
- Mock 数据只保存在当前浏览器页面的内存中，常用写操作会更新内存状态，刷新页面后恢复种子数据；未实现的 Mock 路由必须返回明确的 `mock_route_not_found` 错误。
- Mock 种子数据应覆盖主要页面和共享组件的展示状态，包括正常、异常、禁用、长文本、批量任务、空列表、证书/镜像/运行时错误和调试页不健康状态；新增页面状态时同步补充 `web/src/mocks/data.ts` 与相关 Mock 路由测试。
- `ApiClient` 期望后端返回统一 JSON envelope；如果服务返回 HTML 或其他非 JSON 内容，前端必须转换为本地化的可读错误。
- 用户可见的数据列表应使用共享分页组件 `web/src/components/AppPagination.vue`；数组型接口优先通过 `web/src/composables/usePagination.ts` 做前端分页。
- 页面或列表的初次网络加载应使用 `web/src/components/PageLoadingState.vue`；已有内容后的刷新保留卡片 `:loading` 或按钮 loading。
- 中大屏保持桌面或多栏布局时，页面最外层容器必须填满全局页头以下的剩余视口并隐藏自身溢出，禁止 `v-main` 或整页纵向滚动；滚动只允许发生在明确的内部内容区。
- 页面级 Alert、工具栏、摘要和分页只按内容高度展示；只有主工作区吸收中大屏的剩余高度。
- 满高列表或表格卡片中，分页和固定操作区必须贴底；中间列表/表格体负责吸收剩余高度并内部滚动。
- `760px` 以下恢复页面自然高度和 `v-main` 页面级纵向滚动，列表与卡片解除桌面端纵向内部滚动，表格仅保留必要的横向滚动。
- 紧凑页头保持菜单、标题和工具按钮单行；只有存在活跃任务时才增加第二行 ticker。
- 主从双栏页面统一使用 `AppMasterDetailWorkspace.vue`，由组件提供 `clamp(300px, 26vw, 340px)` 左栏、`18px` 栏间距和 `1080px` 单列断点；feature 页面不得复制这一网格。
- 页面、详情、工具栏、表格行、section、筛选、对话框和 Snackbar 操作统一使用 `AppActionButton.vue` 与 `AppActionGroup.vue`。对象级编辑、删除、同步、刷新、保存等操作放在页面或右侧详情标题区；表格和摘要列表行尾显示带图标的文字小按钮；对话框底部使用 `context="dialog"`，Snackbar 使用 `context="snackbar"` 和 `kind="snackbar"`；纯图标只用于 `kind="tool"` 的关闭、更多、拖拽手柄、选择器工具位或输入框附属工具。
- 切换左侧选择项后如果右侧需要异步加载详情，必须立即清空旧详情并展示 `PageLoadingState`，同时忽略迟到响应。
- 嵌在详情中的异步子面板同样遵守上下文切换规则，例如应用运行时列表、日志弹窗、任务步骤和服务器资源页；切换应用、任务或服务器后不得继续展示上一上下文的异步数据。
- 服务器节点、服务器资源、域名和任务中心的左侧选择器统一使用 `AppSelectorPanel.vue`；普通“名称 + 副标题 + 状态/行尾内容”选择行使用 `AppSelectorSummaryItem.vue`，特殊操作列表可直接使用 `AppSelectorItem.vue`。服务器节点与服务器业务选择还必须共同使用 `ServerSelectorItem.vue`，防火墙、fail2ban、软件包更新、容器、镜像、网络和卷通过 `ServerSelector.vue` 复用同一展示。
- 域名证书、自签证书、密钥和系统证书页面同样使用 `AppSelectorPanel.vue` 主从布局，并通过 `AppSelectorSummaryItem.vue` 统一对象选择行；右侧标准详情使用 `AppDetailPanel.vue`，对象操作位于右侧详情，导入与导出位于选择器更多菜单。
- 应用页面使用同一选择器主从布局，应用操作位于右侧详情标题区。共享选择器标题区不显示数量 Chip。普通应用详情、设施应用详情和创建/编辑应用页都只能有一张外层工作面；正文里的基本信息、路由、部署记录、运行实例和表单分区使用无阴影分区、分隔线或轻量数据行，不得在外层卡片内再套同等级边框、圆角、阴影或渐变卡片。

## 常见页面对应关系

- 概览：`web/src/views/overview/index.vue`
  - 页面顶部只保留卡片编辑操作，不展示独立的 signal 摘要区。
  - 顶部编辑操作使用共享 action 组件；卡片拖拽和更多菜单属于工具位，必须使用 `AppActionButton kind="tool"` 并提供本地化 label。
  - 卡片区使用单一满高仪表盘工作面承载可配置卡片，工作面内部滚动；单张卡片只用弱表面、边框和左侧状态轨表达类型，不再使用强装饰顶部色条。
  - 卡片布局通过 `web/src/api/overview.ts` 的 `GET/PUT /api/v1/overview/cards` 从数据库读取和保存，不使用 localStorage。
  - 指标卡片数据通过 `GET /api/v1/overview/cards/{cardId}/data` 按卡片 ID 拉取，由后端展开该卡片的服务器范围并返回 `metricsByServer`，前端不再按服务器逐个请求指标。
  - 新增、编辑、删除和重置后保存完整布局；拖拽时只更新本地顺序，在拖拽结束时保存一次。
- 服务器菜单组：`web/src/views/servers/`
  - 节点：`node/index.vue`
  - 凭据：`credentials/index.vue`
  - 组内共享：`_shared/`
  - 服务器详情的 Agent 操作通过 `POST /api/v1/servers/{id}/agent/deploy` 创建部署任务；未安装时显示安装，已安装但异常时显示重装，并通过任务中心跟踪结果。
- 安全菜单组：
  - 防火墙：`web/src/views/security/firewall/index.vue`
    - 页面只管理 UFW 规则和启用流程。
  - fail2ban：`web/src/views/security/fail2ban/index.vue`
    - 页面以防护规则列表作为默认模式，正文只展示 jail 摘要行、启用状态和编辑/删除动作；SSH、Nginx、Apache、Postfix、Dovecot 和自定义模板只负责生成 jail 初值，完整 jail 字段和高级 options 进入标准 dialog 编辑。结构化 YAML 是高级模式，使用 Panel 的 `jails` 结构并可与规则列表双向转换，不是目标机原始 fail2ban 配置文件。未接管时保存只落 Panel 草稿，接管/启用和取消接管通过独立动作触发任务。
- 资源菜单组：
  - 软件包：`web/src/views/resources/packages/index.vue`
  - 容器、镜像、网络、卷：`web/src/views/resources/`
  - 资源页共享：`web/src/views/resources/_shared/`
  - 容器、镜像、网络和卷通过 `ResourcePage.vue` 共用页面级工作区：左侧服务器选择器固定，右侧先显示当前服务器上下文头，再由内部内容区滚动承载表格和操作；`ResourcePage.vue` 自身复用 `AppMasterDetailWorkspace.vue`。
  - 资源页 slot 内使用统一表格工作面：标题和主要操作固定在上方，`v-table` 放在内部滚动正文中，次要或危险批量动作收进更多菜单并通过标准对话框确认，避免把裸表格和一排行为按钮直接贴在右侧详情卡里。
- 应用菜单组：
  - 应用：`web/src/views/applications/apps/index.vue`
  - 创建/编辑应用隐藏页：`web/src/views/applications/apps/create.vue`，路由 `/applications/apps/create` 和 `/applications/apps/:applicationId/edit` 不加入侧边菜单，保存或取消后返回普通应用页。
  - 设施应用：`web/src/views/applications/facility-apps/index.vue`
  - API：`web/src/api/applications.ts`、`web/src/api/containerization.ts`、`web/src/api/facilityApps.ts`
  - 设施应用页面当前只有“反向代理/入口网关”这一项，但页面结构必须按后续多设施应用扩展保留左侧选择器；左侧复用 `AppSelectorPanel` 和 `AppSelectorSummaryItem`，不得为入口网关手写一套单项列表，也不得把入口节点、路由数、静态资产数等详情指标塞进选择器。入口网关详情页必须优先保证路由编辑区的横向空间；中大屏详情正文使用单列工作流并横向填满右侧剩余工作区，不得用固定最大宽度把内容收成居中窄列。路由规则是主工作面，入口节点、面板访问入口、静态资产和部署记录作为后续支撑区展示，不得为了展示部署记录或其他摘要增加右侧栏压缩路由编辑区。应用路由属于普通应用的反向代理配置，不放进设施应用编辑流里竞争注意力。部署记录应作为轻量运行状态信息展示，不应形成和普通应用详情不一致的大型独立卡片。设施详情正文沿用普通详情页的紧凑内边距，域名路由组是主工作面里的段落而不是嵌套卡片。
- 应用页面操作语义跟随轻量控制平面：编辑器主按钮“保存并应用”只提交保存会话，启用应用由保存接口触发协调，前端不得在保存成功后额外调用 `/applications/{id}/deploy`；保存期间编辑器用覆盖弹窗的阻塞进度遮罩展示当前阶段，并禁用关闭、取消和重复提交。详情页“同步”表示保存/启用 desired state 后触发协调，不承诺当前请求内完成部署，也不强制重建已经满足 desired state 的成功节点；“停用”只写 disabled desired state 并触发协调停止现有实例；删除会提交清理期望并由协调任务清理运行时资源。设施应用的手动操作显示为“立即同步”，不使用“协调”作为用户按钮文案；应用路由在各普通应用的反向代理配置中维护。
- 普通应用创建和编辑共用隐藏独立页承载 `ApplicationEditor.vue` 的页面嵌入模式；详情页编辑按钮跳转 `/applications/apps/:applicationId/edit`，不再打开编辑弹窗。创建/编辑页不出现在导航中，保存成功按应用 ID 返回应用页查询参数，取消直接返回应用页。页面嵌入模式只保留全局页头标题，编辑器自身不重复显示标题；可视化表单保持一页式扫读，左侧提供轻量锚点导航，右侧分区横向填满编辑工作面。分区不得渲染成同等级 nested card；使用正文分隔线、标题层级和行内数据底色表达结构，避免靠密集横线或多层边框切碎内容。YAML 作为创建/编辑页底部内嵌高级分区纳入锚点导航，不使用弹窗；应用文件新增、模板编辑和二进制替换使用聚焦对话框；新增文件可选择类型，编辑模板或覆盖二进制时必须锁定原类型。
  - 挂载目标和反向代理规则等复杂重复配置在页面正文只展示摘要列表和编辑/删除动作；完整字段进入标准对话框编辑。端口、环境变量等短行配置可保留紧凑重复行，但必须按容器宽度提前折叠，不能挤压到不可读。
- Runtime 应用实现：
  - 应用运行时面板位于 `web/src/views/applications/apps/` 目录内，日志查看复用 `web/src/components/RuntimeLogsDialog.vue` 弹窗展示 agent runtime 实例或 Docker 容器日志。
  - 应用编辑器的反向代理规则为每条规则展示目的地选择，稳定值为 `local` 或 `container`；旧数据按 `local` 展示和保存。
- DNS 域名与记录：`web/src/views/dns/domains/index.vue`
- 证书菜单组：`web/src/views/certificates/`
  - 域名证书：`domains/index.vue`
  - 自签证书：`self-signed/index.vue`
  - 密钥：`keys/index.vue`
  - 两个页面复用 `key-assets/index.vue` 的资产工作区
- 任务中心：`web/src/views/tasks/index.vue`
  - 左侧选择器展示按 `operationId` 聚合的操作；右侧“操作中的任务”展示具体执行任务。批量父任务作为“操作汇总”行展示在子任务之前，可查看父任务步骤、日志和错误；操作内任务数仍按子任务数量展示。
- 设置菜单组：`web/src/views/settings/`
  - 通用：`general/index.vue`
    - 运行时表单包含指标保留、采集间隔、远程命令超时、清理计划、Token 过期时间、语言和后端日志等级；日志等级保存为稳定值 `debug`、`info`、`warn` 或 `error`。
    - 设置页不使用页头全局保存；每个可编辑 section 在自身底部通过 `AppActionGroup context="section"` 提供独立保存按钮，并且只提交该 section 的字段，避免连带保存其他 section 的草稿。
  - 安全：`security/index.vue`
  - 证书：`certificates/index.vue`
  - 系统：`system/index.vue`
  - 系统证书：`system-certificates/index.vue`
  - 备份与还原：`backups/index.vue`
  - 组内共享：`_shared/`
  - 设置共享页和备份与还原页使用适合表单阅读的收敛宽度，卡片内部固定分类标题区，正文表单内部滚动，section 使用弱表面分区；不要把设置表单铺满超宽工作区，确认操作使用标准 `app-dialog-*` 对话框。
- 维护页：
  - 备份导出维护页：`web/src/views/maintenance/backup.vue`
  - 该页不使用全局 AppLayout；启动期备份导出维护服务会托管前端静态资源并显示阶段、进度、不敏感备份元数据、下载和结束维护入口。结束维护只清理 pending 标记，仍需重启回到正常 Panel。
- 登录与强制改密：`web/src/views/auth/`
- 隐藏诊断页：`web/src/views/debug/index.vue`
  - 认证路由为 `/debug`，不加入侧边菜单，只能通过直接地址访问。
  - 页面每 5 秒读取一次 `GET /api/v1/debug/snapshot`，支持暂停、恢复和手动刷新；刷新失败时保留最近一次成功快照。
  - 页面使用“运行时 / Tasks / 数据库”主 Tab；数据库区域再按 app、task、metrics 数据库文件使用子 Tab 分隔。
  - Tasks Tab 展示注册任务定义能力；数据库 Tab 展示每张表当前行数、表数据大小、索引大小、总占用和数据库占比。
  - 中大屏使用满高 `.page-shell` 和内部诊断内容滚动区，窄屏恢复页面级滚动。

## 全局主题与账户菜单

- 主题配置入口位于 `web/src/layouts/AppLayout.vue` 页头主题菜单。
- 配置结构、默认值、旧值迁移、系统外观监听和 `localStorage` 持久化集中在 `web/src/theme.ts`。
- 未设置过主题的浏览器默认跟随 `prefers-color-scheme`；选择手动浅色或深色后不再响应系统外观变化。
- 蓝、绿、红、橘、紫、粉、黄七套内置预设可以由明暗主题共用，也可以分别配置；每套同步修改 Vuetify `primary`、`on-primary` 和 `surface-variant`，其他语义色和表面色继续使用固定主题定义。
- 账户图标菜单显示当前用户名和退出入口；退出继续调用认证 store 并跳转登录页。

## 验证

- 先按模块索引的“检查和测试范围”判断是否需要验证。
- 需要验证前端逻辑和组件改动时，优先运行 `task test:web`。
- 类型、路由、构建链路、样式或依赖变更且需要构建检查时，运行 `task build:web`。
- 本仓库要求不要使用 curl、browser 等工具强行检查页面。

## 文档更新触发

新增页面、路由、API 模块、store、共享组件约定、用户可见工作流或前端持久化结构时，必须更新本文档或对应功能模块文档。

## 自签证书与密钥页面

- 证书一级菜单包含“自签证书”与“密钥”两个二级入口，路由分别为 `/certificates/self-signed` 和 `/certificates/keys`。
- 自签证书页面只展示用户域 CA/TLS 证书。Panel 侧 Agent CA、Panel Agent 客户端证书和服务器 Agent 服务端证书迁移到 `/settings/system-certificates`；系统证书只能查看和重置，不能删除、导入、导出、下载或参与批量选择，重置单台服务器 Agent 服务端证书会触发该服务器 Agent 重装。
- 密钥页面只展示用户域 SSH 密钥对；用户生成或导入的 CA、TLS 和 SSH 资产统一标记为“用户域”。
- 用户域资产支持生成、导入、下载、删除、重新签发或重新生成。
- 批量导出要求输入至少 12 位密码；批量导入使用 multipart 上传，先展示预检摘要、冲突和受影响引用，再提交处理策略。
- 前端 API 位于 `web/src/api/keyAssets.ts`，共享 client 的 `postForm` 负责 multipart 请求。

## Backup/restore container restart UI

- Backup and restore pages must not call a standalone restart endpoint. There is no such API.
- Existing backup/restore responses expose `restartSupported`. When it is true, the UI should tell the user Panel is restarting automatically as part of the current backup/restore workflow. When false, keep the manual restart instruction.
- Settings backup page uses the result of `POST /api/v1/backups/export` and `POST /api/v1/backups/restore/confirm` to choose the pending/restarting snackbar text.
- Backup export maintenance page uses the status returned by `POST /api/v1/backups/export/exit` to choose the finished/restarting message.

## Application Create Editor

- `/applications/apps/create` and `/applications/apps/:applicationId/edit` embed `ApplicationEditor.vue` as a full-height working surface. On medium and large screens the outer page must not scroll; the editor body is the internal scroll region.
- The AppSpec switch in the embedded editor is mutually exclusive: visual mode shows runtime, network, and mounts; YAML mode replaces those blocks with an inline code editor. Deployment targets, reverse proxy, variables, and application files remain outside YAML and stay visible in both modes.
- The embedded YAML surface should feel like a code editor: line-number gutter, monospace text, stable height, internal scrolling, and variable insertion in place. Do not move YAML into a dialog on the create page.

## Facility Apps UI

- Facility app details use a single-column working flow on medium and large screens. Do not add a right rail for deployment records, route summaries, or secondary status because route editing needs the horizontal room.
- The facility app selector only identifies facility apps. Do not put gateway nodes, route counts, static asset counts, or other configuration metrics in the selector item.
- Entrance gateway routes are the primary editor surface and should appear before supporting gateway, Panel entry, asset, and deployment sections. Use a domain-group editor: domain and gateway-node controls stay in the group header, routes are shown as a compact list with edit/delete actions, and editing opens a dialog with route-specific fields for static content, redirects, or manual proxy targets. Do not expand route forms inline below the list.
- Application-family work surfaces should not stack cards inside cards. Keep the outer detail/editor card as the only framed surface; internal facility sections, domain groups, deployment records, and normal application detail sections should read as unframed body sections or lightweight rows.
- Application route summaries are not part of the facility app editor flow; they are maintained in normal application reverse proxy settings and should not compete with facility route editing.
- Normal application reverse proxy rules use the same interaction model: page body shows a compact rule list with edit/delete actions, and the full rule form including paths is edited in a dialog.

## Security UI

- Firewall and Fail2Ban pages use `AppMasterDetailWorkspace` with `ServerSelector` on the left and `AppDetailPanel` on the right. The right content pane must rely on `AppDetailPanel` for the outer outlined card, fixed header, scroll body, loading state, and empty state.
- Security detail panes must not recreate local `security-panel`, `panel-header`, `security-panel-body`, `empty-panel`, or `status-grid` shells. Summary metrics use the global `.info-grid`; page-local CSS is limited to business body sections such as UFW rule forms, rule tables, Fail2Ban jail rows, YAML editor, and dialogs.
