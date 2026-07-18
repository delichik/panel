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
- 全局页头根据系统版本 API 返回的构建通道展示环境标识；`dev` 通道在标题旁显示紧凑 DEV Chip，正式通道不显示。fail2ban 侧边导航同样只在 `dev` 通道显示，正式通道仍保留可直接访问的路由。
- 全局主题偏好只保存在当前浏览器，由 `web/src/theme.ts` 管理；支持自动跟随系统、手动浅色/深色，以及明暗共用或分别选择内置主题预设。旧 `linux-panel-theme` 值会迁移为对应手动模式。
- 页头只展示主题和账户图标入口；用户名与退出登录操作收纳在账户菜单中，不在页头常驻展示。
- 路由标题使用 `meta.titleKey`，不要在路由元信息里写用户可见文案。
- 新增或修改用户可见文案必须走 `useI18n()` 和 `web/src/i18n/index.ts`，并更新多语言状态文档。
- 持久化 UI 配置只保存稳定值，不保存已经翻译的标题或说明。
- API 调用经 `ApiClient`，默认 base URL 是 `/api/v1`；后端 API 变更时同步更新 `web/src/api/`、`web/src/types/api.ts` 和相关测试。
- 全量备份主流程是在正常设置页写 pending export 并提示重启；重启后的启动期维护服务使用 `/maintenance/backup` 展示导出进度、密码输入和下载入口。
- 不依赖后端的前端测试模式通过 `task run:web:test` 启动；该任务设置 `VITE_PANEL_TEST_MODE=true`，由 `web/src/mocks/` 接管 `/api/v1` 请求并自动建立演示会话。
- G4 正式切换前，可通过 `task run:web:testv2` 在 `http://localhost:5174/` 查看隔离 V2 harness；根页面默认展示产品级 AppShell 与 Overview V2。概览固定状态使用 `?fixture=overview&state=normal|loading|no-servers|no-cards|partial|error|stale|editing|conflict`；服务器、凭据、防火墙和软件包页面在产品壳导航中直接展示 V2 页面，其中服务器支持 `normal|empty|loading|error|stale|offline|initializing|agent-incompatible|agent-undeployable|unsupported|panel-host|editing|probe-success|probe-failed|delete-impact|operation-running`，凭据支持 `normal|empty|error|unused|delete-impact`，防火墙支持 `normal|missing|disabled|offline|operation|empty`，软件包支持 `normal|empty|offline|operation`。`fixture=data|dashboard|resource|editor|transactional|settings|workspace` 继续用于七种模板，另有 auth 和 maintenance fixture。该入口不注册正式 router、Pinia 或 Mock API；最终切换时由 `task run:web:test` 承接 V2，并删除临时 V2 命令。
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

### Auth V2 隔离页面族

- 新普通认证页位于 `web/src/views/auth/_v2/`，包含 `LoginPage`、`ChangePasswordPage`、`StandaloneFlowFrame` 和 `AuthFormError`；G4 集成前不接入正式 router/main，也不修改旧登录页。
- 页面使用 G1 `Ui*` 与 `.ui-foundation`，保持单一主操作。低高度、窄屏和放大场景由页面自然滚动，不使用登录页专属 orb、网格或强渐变背景。
- `web/src/auth/safeRedirect.ts` 是登录和改密完成后的唯一跳转清洗入口：仅保留安全站内路径，拒绝 scheme/host、双斜线、反斜线、控制字符、登录/改密循环、维护路由，以及 query/fragment 中大小写、分隔符和多层 percent-encoding 变体的凭据语义。
- 普通认证、backup maintenance 和 restore maintenance token 使用显式不同的共享存储单例与 key；普通认证默认 localStorage，两个维护上下文默认 sessionStorage。`ApiClient` 按 context 自动读取对应单例，绝不把普通 token 发给维护 API；同一 context 的多个 client 在 sessionStorage 不可用时仍共享同一内存 fallback。任一 write/remove 发生后内存状态成为当前单例的权威值，持久层失败不得让旧 token 复活。
- `AuthSessionDto` 包含可选 `expiresAt`、`sessionVersion`；session GET 兼容旧响应。login/account/JWT secret 等凭据 mutation 必须返回 `authenticated=true` 和非空 token，否则按契约错误清理普通 token，不能继续使用旧会话。
- `ApiClient` 的 401 协调通过注入的 `onUnauthorized` 或按 `normal`、`backup`、`restore` 注册的 `registerUnauthorizedHandler` hook 完成，不直接依赖 Pinia、router 或正式页面。请求必须用 typed `auth.operation` 表达操作，并可用 `skipUnauthorized()` 跳过凭据校验类 401；禁止按 URL 字符串推断。协调按认证上下文使用跨 `ApiClient` 的全局 singleflight，两个 client 同时失效只启动一个 handler；handler 内发起的同 context 检查不会等待自身，也不会递归启动新 handler。
- 认证 mutation 提交成功后即锁定表单；emit 或导航失败只显示本地化的安全继续提示，不得重新归类为凭据失败或允许重复 mutation。字段错误只接受页面声明的 camelCase 白名单，未知字段与非 API Error 降级为本地化总错误。
- 隔离浏览器 fixture 使用 `?fixture=auth-login` 和 `?fixture=auth-change-password`；相关 E2E/a11y 与 component 测试覆盖 branding 降级、密码可见性、错误聚焦、缩放回流和自动可访问性检查。

### AppShell V2 隔离适配层

- `web/src/app/v2/AppShellV2.vue` 仅接收 typed shell model、偏好和回调，不读取 router、Pinia 或业务 API。当前路由、路由标题、版本/通道、更新状态、任务 ticker、账户和导航可见性均由集成层显式传入。
- 导航适配会移除 `hidden` 条目；`devOnly` 条目只在明确的 `dev` 通道展示。导航点击只发出稳定 item，不自行跳转；移动抽屉在导航后关闭。
- 全局页头只承载路由标题、DEV/更新弱提示、主题和账户入口；页面主要操作仍属于 PageHeader。主题仅支持 system/light/dark，折叠与主题变化通过事件交给正式集成层持久化。
- `task run:web:testv2` 默认使用 `ProductShellHarness.vue` 展示产品壳和 Overview V2；概览使用固定注入 client，不访问网络。自动 Foundation 测试环境未设置产品预览变量时仍保留原根 fixture，避免预览入口改变基础组件基线。

### Maintenance V2 隔离页面族

- 新备份导出与还原维护页位于 `web/src/views/maintenance/_v2/`；G4 集成前不接正式 router/main，也不修改旧 `maintenance/backup.vue` 或后端过渡 HTML。
- `MaintenanceFrame` 提供无 AppShell 的满视口工作面；`760px` 起页面自身不滚动，正文内部滚动，窄屏恢复自然页面流。页面只组合 G1 `Ui*`，当前阶段最多一个 primary action。
- backup 与 restore 分别使用 `backupMaintenanceSession`、`restoreMaintenanceSession` 和显式 API auth context。401 清理对应 token 并回到当前 URL 内的维护登录，不跳普通 `/login`。
- 状态必须通过 schema、mode+phase、revision、capability 组合、布尔字段、RFC3339 时间、manifest/file shape、结构化 error 和严格稳定 ID 白名单校验。`pollAfterMs` 上限为 30 秒，stale timer 最多 90 秒，避免无界定时器。未知 schema/phase、错误 mode、越权 capability 或不完整 payload 一律 fail-safe，不根据字符串猜测命令，也不得把非法日期传给 formatter。
- 命令携带 `expectedRevision` 与 `clientOperationId`；409 后刷新最新状态。轮询遵守 `pollAfterMs`，使用 AbortController 和 request generation 忽略迟到响应；有效 command adopt 会取消旧 poll、清除旧 contract failure，并在 hidden/offline 时暂停，恢复可见或在线后刷新。
- restore applying 不展示 retry/clear；failed 只按 capability 展示操作，clear pending 必须进入危险确认对话框。backup completed 在下载成功后把 primary action从“下载”转换为“结束导出”，并保留次要的再次下载。
- 隔离 fixture 使用 `?fixture=maintenance-backup|maintenance-restore` 和显式 state；component、E2E、a11y、visual 覆盖登录、过期、阶段、冲突/能力门、低高度、200% 回流、759/760、离线及中英文代表状态。

- 概览：`web/src/views/overview/index.vue`
  - 隔离 V2 位于 `web/src/views/overview/_v2/`，使用 `DashboardPage` 单内部滚动工作区、局部卡片状态、命名尺寸、键盘等价排序和版本冲突模型；G4 前不接正式 router，也不修改旧概览页。
  - V2 读取契约为 typed `GET /overview/dashboard`，布局写入使用 `baseVersion + cards`；当前仅由隔离 fixture 注入，后端聚合实现留待契约集成阶段。
  - 布局写入由页面族内的串行 writer 统一管理：连续操作合并等待项，已开始请求完成后才使用新版本提交下一项；writer 的独立 epoch 在请求开始和提交完成时变化，因此 active PUT 期间启动的旧 dashboard 读取也不得覆盖较新的本地布局。客户端卡片 ID 使用可注入的 UUID 生成器并检查现有 ID。
  - 指标图使用所有服务器及主/次通道共同的数值域和单位；网络 RX/TX 生成独立稳定曲线和数据表列。SVG 保持纯视觉并对辅助技术隐藏；当前值 tooltip 仅由带完整 series/当前值名称的紧凑图例原生按钮触发，不增加常驻 per-server 摘要条。更新目标使用判别联合，软件包目标必须携带服务器 ID，应用目标必须携带应用 ID。
  - 页面顶部只保留卡片编辑操作，不展示独立的 signal 摘要区。
  - 顶部编辑操作使用共享 action 组件；卡片拖拽和更多菜单属于工具位，必须使用 `AppActionButton kind="tool"` 并提供本地化 label。
  - 卡片区使用单一满高仪表盘工作面承载可配置卡片，工作面内部滚动；单张卡片只用弱表面、边框和左侧状态轨表达类型，不再使用强装饰顶部色条。
  - 指标卡片不在图表上方展示独立的 per-server 当前值摘要条；当前值通过图表 tooltip 查看，卡片内空间优先留给曲线和图例。
  - 卡片布局通过 `web/src/api/overview.ts` 的 `GET/PUT /api/v1/overview/cards` 从数据库读取和保存，不使用 localStorage。
  - 指标卡片数据通过 `GET /api/v1/overview/cards/{cardId}/data` 按卡片 ID 拉取，由后端展开该卡片的服务器范围并返回 `metricsByServer`，前端不再按服务器逐个请求指标。
  - 新增、编辑、删除和重置后保存完整布局；拖拽时只更新本地顺序，在拖拽结束时保存一次。
- 服务器菜单组：`web/src/views/servers/`
  - 隔离 Servers V2 位于 `web/src/views/servers/_v2/`，使用 `ResourcePage` 主从布局、注入式 `ServerResourceApi`、结构化 capability/operation/delete impact 和独立 probe 草稿；列表刷新同步重取当前详情，编辑器显式处理凭据 loading/error/empty，Agent/重启/UFW 安装共用版本化防重复确认模型，probe 原始诊断不进入用户文案；G4 前不接正式 router 或旧页面。
  - `task run:web:testv2` 的 `?fixture=servers` 与兼容入口 `servers/product-shell` 展示真实 Servers V2；支持 `normal|empty|loading|error|stale|offline|initializing|agent-incompatible|agent-undeployable|unsupported|panel-host|editing|probe-success|probe-failed|delete-impact|operation-running`。
  - 隔离 Credentials V2 位于 `web/src/views/credentials/_v2/`，使用 `DataPage` 与注入式 `CredentialResourceApi`；列表支持搜索、认证方式和引用状态筛选，编辑时 secret 字段留空表示保留原密钥，删除前展示引用影响。G4 前不接正式 router 或旧页面；`task run:web:testv2` 的产品壳导航可直接打开，状态支持 `normal|empty|error|unused|delete-impact`。
  - 隔离 server-context V2 页面共用 `web/src/views/_v2/ServerContextList.vue`；该组件只表达服务器选择上下文，视觉语言对齐 Servers V2 的选择行。对象级操作必须放在右侧当前服务器详情头，不能放在全局 PageHeader 里形成“未绑定当前资源”的误导。
  - 节点：`node/index.vue`
  - 凭据：`credentials/index.vue`
  - 组内共享：`_shared/`
  - 服务器详情的 Agent 操作通过 `POST /api/v1/servers/{id}/agent/deploy` 创建部署任务；未安装时显示安装，已安装但异常时显示重装，并通过任务中心跟踪结果。
- 安全菜单组：
  - 防火墙：`web/src/views/security/firewall/index.vue`
    - 页面只管理 UFW 规则和启用流程。
    - 隔离 Firewall V2 位于 `web/src/views/firewall/_v2/`，使用 `ResourcePage` 作为服务器上下文页；左侧只在防火墙场景展示服务器选择，右侧展示 UFW 安装/启用状态、默认策略、受保护端口、规则表格、添加规则和删除确认。G4 前通过注入 fixture 运行，不接正式 router 或真实后端；`task run:web:testv2` 产品壳状态支持 `normal|missing|disabled|offline|operation|empty`。
    - Firewall V2 右侧遵循 Servers V2 详情骨架：固定详情头展示当前服务器、状态和操作，正文使用分区标题、`dl` 信息网格和单一规则表格工作区，不使用独立摘要卡片排。
  - fail2ban：`web/src/views/security/fail2ban/index.vue`
    - 侧边导航入口仅在系统版本 API 返回 `dev` 构建通道时显示；正式通道不展示入口，但认证路由仍保留，可通过直接地址访问。
    - 页面以防护规则列表作为默认模式，正文只展示 jail 摘要行、启用状态和编辑/删除动作；SSH、Nginx、Apache、Postfix、Dovecot 和自定义模板只负责生成 jail 初值，完整 jail 字段和高级 options 进入标准 dialog 编辑。结构化 YAML 是高级模式，使用 Panel 的 `jails` 结构并可与规则列表双向转换，不是目标机原始 fail2ban 配置文件。未接管时保存只落 Panel 草稿，接管/启用和取消接管通过独立动作触发任务。
- 资源菜单组：
  - 软件包：`web/src/views/resources/packages/index.vue`
    - 隔离 Packages V2 位于 `web/src/views/packages/_v2/`，使用 `ResourcePage` 作为服务器上下文页；支持刷新元数据、按软件包/仓库搜索、按安全/普通更新筛选、选择更新项、更新全部或已选，并把运行操作链接到任务中心。G4 前通过注入 fixture 运行，不接正式 router 或真实后端；`task run:web:testv2` 产品壳状态支持 `normal|empty|offline|operation`。
    - Packages V2 右侧遵循 Servers V2 详情骨架：固定详情头展示当前服务器、更新状态和操作，正文用摘要分区与更新表格分区承载筛选和选择，不使用独立摘要卡片排。
  - 容器、镜像、网络、卷：`web/src/views/resources/`
  - 资源页共享：`web/src/views/resources/_shared/`
  - 容器、镜像、网络和卷通过 `ResourcePage.vue` 共用页面级工作区：左侧服务器选择器固定，右侧先显示当前服务器上下文头，再由内部内容区滚动承载表格和操作；`ResourcePage.vue` 自身复用 `AppMasterDetailWorkspace.vue`。
  - 资源页 slot 内使用统一表格工作面：标题和主要操作固定在上方，`v-table` 放在内部滚动正文中，次要或危险批量动作收进更多菜单并通过标准对话框确认，避免把裸表格和一排行为按钮直接贴在右侧详情卡里。
- 应用菜单组：
  - 应用：`web/src/views/applications/apps/index.vue`
  - 创建/编辑应用隐藏页：`web/src/views/applications/apps/create.vue`，路由 `/applications/apps/create` 和 `/applications/apps/:applicationId/edit` 不加入侧边菜单，保存或取消后返回普通应用页。
  - 设施应用：`web/src/views/applications/facility-apps/index.vue`
  - API：`web/src/api/applications.ts`、`web/src/api/containerization.ts`、`web/src/api/facilityApps.ts`
  - 设施应用页面当前只有“反向代理/入口网关”这一项，但页面结构必须按后续多设施应用扩展保留左侧选择器；左侧复用 `AppSelectorPanel` 和 `AppSelectorSummaryItem`，不得为入口网关手写一套单项列表，也不得把入口节点、路由数、静态资产数等详情指标塞进选择器。`facility-apps/index.vue` 是只读状态面，右侧复用 `AppDetailPanel`，只提供“立即同步”和“配置反向代理”，展示设施路由、普通应用路由和部署状态，不得出现输入框、选择器、开关或保存按钮。反向代理编辑位于隐藏独立页 `/applications/facility-apps/reverse-proxy/config`；中大屏使用满高单工作面和正文内部滚动，所有字段与静态资产先进入本地草稿，最终一次“保存并应用”。应用路由仍属于普通应用配置，只在设施详情汇总展示并提供“打开应用”。
  - 反向代理设施配置页的域名入口节点只能从设施全局网关节点中选择，每个域名至少选择一台；上游模式、调度策略和主节点属于域名级配置。Path 在摘要列表中展示，静态内容、重定向和手工代理的完整字段进入标准对话框，编辑时使用独立副本，取消不得污染全页草稿。删除域名、Path 或静态资产使用确认对话框；保存期间使用 contained persistent 覆盖层展示保存会话阶段，并阻止返回、关闭和重复提交。
  - Panel 入口节点不是普通可选入口节点。配置 API 返回 `panelHostServerId` 后，配置页固定显示该 setup 登记节点；缺少宿主节点时禁用 Panel 入口开关，入口启用时不得把宿主节点移出全局网关节点。后端仍必须重复校验，不能只依赖 disabled 控件。
- 应用页面操作语义跟随轻量控制平面：编辑器主按钮“保存并应用”只提交保存会话，启用应用由保存接口触发协调，前端不得在保存成功后额外调用 `/applications/{id}/deploy`；保存期间编辑器用覆盖弹窗的阻塞进度遮罩展示当前阶段，并禁用关闭、取消和重复提交。详情页“同步”表示保存/启用 desired state 后触发协调，不承诺当前请求内完成部署，也不强制重建已经满足 desired state 的成功节点；“停用”只写 disabled desired state 并触发协调停止现有实例；删除会提交清理期望并由协调任务清理运行时资源。设施应用的手动操作显示为“立即同步”，不使用“协调”作为用户按钮文案；应用路由在各普通应用的反向代理配置中维护。
- 普通应用创建和编辑共用隐藏独立页承载 `ApplicationEditor.vue` 的页面嵌入模式；详情页编辑按钮跳转 `/applications/apps/:applicationId/edit`，不再打开编辑弹窗。创建/编辑页不出现在导航中，保存成功按应用 ID 返回应用页查询参数，取消直接返回应用页。页面嵌入模式只保留全局页头标题，编辑器自身不重复显示标题；可视化表单保持一页式扫读，左侧提供轻量锚点导航，右侧分区横向填满编辑工作面。分区不得渲染成同等级 nested card；使用正文分隔线、标题层级和行内数据底色表达结构，避免靠密集横线或多层边框切碎内容。YAML 作为创建/编辑页底部内嵌高级分区纳入锚点导航，不使用弹窗；应用文件新增、模板编辑和二进制替换使用聚焦对话框；新增文件可选择类型，编辑模板或覆盖二进制时必须锁定原类型。
  - 挂载目标和反向代理规则等复杂重复配置在页面正文只展示摘要列表和编辑/删除动作；完整字段进入标准对话框编辑。端口、环境变量等短行配置可保留紧凑重复行，但必须按容器宽度提前折叠，不能挤压到不可读。
  - 设施 Path 与普通应用反向代理 Path 共用 `web/src/components/RoutePathAdvancedFields.vue`。共享字段包含 Gzip、请求体大小、连接/读取/发送超时、代理缓冲、WebSocket 模式和请求/响应 Header；组件只管理结构化 Path 选项，不包含域名、目标端口、静态来源或上游 URL 等业务字段。
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
- G1 Foundation 可分别使用 `task test:web:unit`、`task test:web:component`、`task test:web:e2e` 和 `task test:web:a11y`；默认 `task test:web` 聚合这四层。
- 视觉比较使用 `task test:web:visual`。只有人工批准基线变化后才能运行 `task test:web:visual:update`。
- Foundation 浏览器测试使用 `web/tests/foundation/harness/`，不得为了测试把 harness 接入正式路由。

## V2 Foundation 隔离阶段

- 新设计系统只能通过 `web/src/design/index.ts` 显式导入，样式 reset 只作用于 `.ui-foundation` 子树；G4 正式切换前不得修改 `main.ts`、正式路由或旧业务页面来接入。
- `AppShell` 是当前 Foundation 根，提供跳到主内容链接；折叠导航项必须有键盘和鼠标均可见的 tooltip。
- `DataPage` 只负责固定 header/filter/bulk/pagination 与表格区域尺寸，纵横滚动由 `UiTable` 的单一 scroll wrapper 持有。
- DataPage 到 UiTable 的桌面高度链必须逐层保持 `flex/height:100%/min-height:0`，移动端统一恢复自然高度。
- `UiSelect` G1 V1 使用原生 select，不提供 searchable 或任意 item 渲染；readonly 仍可聚焦，多选值以可移除文本标签呈现。
- `TransactionalEditorPage` 分开暴露草稿状态与验证摘要，blocking 时编辑工作区和提交区 inert；业务页面还必须配置路由离开守卫。
- Foundation 移动 drawer 打开时 header/main 背景 inert 且从辅助技术树隐藏；主题 runtime controller 必须在手动模式和 dispose 时解除 system media query 监听。
- AppShell 通过 `navigationMode=persistent|temporary` 区分桌面常驻导航和移动 drawer；temporary 关闭态导航同样 inert/隐藏。主题控制器通过 adapter 同步 Vuetify theme，但 G1 仍不接正式入口。
- Foundation `AppNavigation` 在未传 `collapsed` 时使用 `panel-navigation-collapsed` 初始化；受控 prop 优先，折叠操作保持 emit 并安全持久化，SSR 或 storage 不可用时退化为内存状态。

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
- The entrance-gateway configuration page follows the application editor skeleton: left section navigation and one right-side internal scroll surface containing Basic settings, Facility routes, Panel entry, and Static assets. It must not show the fixed nginx image or read-only application routes.
- Use a domain-group editor: domain, origin nodes, AnyAccess, traffic strategy, and primary origin are edited in a domain dialog; paths are shown as compact summaries and edited in a separate dialog. Do not expand full route forms inline below the list.
- Application-family work surfaces should not stack cards inside cards. Keep the outer detail/editor card as the only framed surface; internal facility sections, domain groups, deployment records, and normal application detail sections should read as unframed body sections or lightweight rows.
- Facility detail splits Facility routes and Application routes into two read-only lists. Both lists show origins, AnyAccess/strategy, and complete Path option summaries; only application rows link to the owning application.
- Normal application reverse proxy rules use the same interaction model: page body shows a compact rule list with edit/delete actions, and the full rule form including paths is edited in a dialog.

## Security UI

- Firewall and Fail2Ban pages use `AppMasterDetailWorkspace` with `ServerSelector` on the left and `AppDetailPanel` on the right. The right content pane must rely on `AppDetailPanel` for the outer outlined card, fixed header, scroll body, loading state, and empty state.
- Security detail panes must not recreate local `security-panel`, `panel-header`, `security-panel-body`, `empty-panel`, or `status-grid` shells. Summary metrics use the global `.info-grid`; page-local CSS is limited to business body sections such as UFW rule forms, rule tables, Fail2Ban jail rows, YAML editor, and dialogs.
