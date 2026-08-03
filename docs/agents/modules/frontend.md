# 前端模块

## List And Refresh Contracts

- Applications, servers, domain certificates, self-signed certificates, and key assets consume server-side `ListPage` responses.
- Searchable master lists send `q` to the backend and reset to page 1 when the query changes.
- Async resource and DNS refresh actions wait for task completion and then reload the local snapshot. GET lists are never implicit refresh commands.

> **状态说明（2026-07-21）：前端基础设施已进入 v4。** 上一版“统一 CollectionPage + Naive UI”的重构判定不合格，当前已移除 Naive UI，不恢复 Vuetify。新标准为 Vue 3 + Vite + Vue Router + Pinia + Tailwind + Panel 自有 UI primitives + lucide + ECharts + YAML。`tmp/v1-archive/` 只能作为业务能力参照，禁止复制视觉、布局、操作流或旧 UI 组件写法。

## 适用场景

修改 Vue 页面、共享组件、前端 API client、Pinia store、路由、样式、图表、Mock 或前端测试时，先读本文档，并按 `docs/agents/specifications/frontend/INDEX.md` 加载相关规范。

## 当前入口

- 前端入口：`web/src/main.ts`
- 根组件：`web/src/app/App.vue`
- 全局样式与 Tailwind token：`web/src/styles/main.css`
- 主题运行时：`web/src/design/theme.ts`
- 组件 token 辅助：`web/src/design/tokens.ts`
- 自有 UI primitives：`web/src/components/ui/`
- 外壳与导航：`web/src/components/shell/`
- 页面模板：`web/src/components/templates/`
- 一级对象选择工作台统一使用 `web/src/components/templates/MasterDetailLayout.vue`：`xl` 及以上左侧选择区固定 `360px`，较窄宽度折叠为单列；组件只负责双栏几何和溢出保护，业务页面继续负责面板视觉与内部滚动。当前接入 servers、credentials、applications、certificates、dns、tasks、security、resources；settings 导航、tasks 内层列表和 AppShell 导航保持各自布局。
- 路由：`web/src/router/index.ts`
- API client：`web/src/api/client.ts`
- Session 基座：`web/src/stores/session.ts`
- Mock API 基座：`web/src/mocks/`
- 多语言：`web/src/i18n/index.ts`

## 技术约定

- 禁止新增 `naive-ui`、Vuetify 或旧 UI 框架依赖。
- 复杂无样式交互可使用 headless 组件库；业务页面仍必须通过 Panel 自有 primitives 暴露一致样式。
- `web/src/components/ui/` 当前基础组件：Button、IconButton、Input、Textarea、Select、Dialog、Dropdown、DropdownItem、Tabs、Badge、Table、ToastProvider/useToast、Skeleton、EmptyState、Tooltip、Switch。
- 等待网络接口响应的加载效果统一使用 `web/src/components/ui/LoadingOverlay.vue` 或既有骨架/按钮 loading；文本加载位置不得只显示文字。
- 列表、详情、轮询等异步加载必须使用 `createLatestRequestGuard` 或 AbortController 丢弃过期响应；轮询必须防重入，刷新按钮应覆盖其对应面板的全部数据源。
- 新增或替换跨页面同类交互时，优先复用 `web/src/components/ui/` 的 SearchInput、PaginationBar、ConfirmDialog、FileUploadButton、DownloadButton、StatusBadge，以及 `web/src/components/patterns/` 的 FilterBar、ServerContextSelector、ServerMultiPicker、MasterList、EditorSectionRail；适用边界见 `docs/agents/specifications/frontend/interaction-patterns.md`。
- `web/src/views/applications/index.vue`、`web/src/views/tasks/index.vue`、`web/src/views/security/index.vue` 与 `web/src/views/resources/index.vue` 已开始接入统一 patterns：搜索使用 `SearchInput`，任务分页使用 `PaginationBar`，任务/应用状态使用 `StatusBadge`，应用/设施服务器多选使用 `ServerMultiPicker`，安全/资源服务器上下文使用单一 `ServerContextSelector`，持久化与文件内容操作使用 `DownloadButton` / `FileUploadButton`。应用和设施编辑器采用同一连续纵向瀑布流：所有配置区在一个编辑正文中按业务顺序展开，正文独立滚动，右侧保留摘要；不得恢复分区切换、分页卡片或隐藏其他配置区的交互。后续页面修改不得在 `ServerContextSelector` 上方叠加服务器 Select 下拉。
- 图标统一使用 `@lucide/vue`。
- 主题只支持 `system` / `light` / `dark`，通过 `data-theme` 和 CSS 变量运行。
- 中大屏 AppShell 必须填满视口并禁止页面级滚动；滚动限制在模板正文、表格、详情、日志或编辑正文内部。
- 异常消息统一通过 `ToastProvider` 以顶部 toast 展示；`Dialog` 不因点击遮罩关闭，只能通过显式关闭、取消或 Escape 关闭。

## 页面族边界

每个页面族必须有独立入口目录，不得作为参数挂到同一个通用页面：

- `web/src/views/overview/`
- `web/src/views/servers/`
- `web/src/views/credentials/`
- `web/src/views/security/`
- `web/src/views/resources/`
- `web/src/views/applications/`
- `web/src/views/dns/`
- `web/src/views/certificates/`
- `web/src/views/application-operations/`
- `web/src/views/system-events/`
- `web/src/views/tasks/`
- `web/src/views/settings/`
- `web/src/views/auth/`
- `web/src/views/maintenance/`
- `web/src/views/debug/`

页面族必须在自己的目录内完成真实信息架构、闭环操作、API 模块、Mock 路由和测试；禁止复用占位页作为交付页。

### 认证与 API Client

- 普通控制台认证使用真实后端 `/api/v1/auth/login|logout|session|account|jwt-secret`。登录成功和 session 恢复返回的 Bearer token 保存在 `web/src/stores/session.ts`，`web/src/api/client.ts` 通过 token provider 为受保护请求注入 `Authorization: Bearer <token>`；`/auth/login` 和 `/settings/public-branding` 使用 `skipAuth`。
- 前端启动时必须先恢复 `/api/v1/auth/session`，路由守卫不得默认已登录。未认证访问普通控制台路由跳转 `/login?redirect=...`；如果 session 返回 `passwordChangeRequired`，登录页必须显示 `/api/v1/auth/account` 改密表单，普通控制台路由继续重定向回登录页直到改密完成；维护模式 `/maintenance/backup` 使用独立维护 token，不与普通 session 混用。
- 非 ApiClient 的 blob、multipart 和 DELETE body fetch helper 也必须复用 `authHeaders()`，包括应用/设施编辑资产、密钥资产导入、备份恢复和下载路径，避免真实环境下绕过 token 注入。
- `/api/v1/settings/public-branding` 是公共 branding 接口，登录页可使用它覆盖本地 `app.name` / `app.subtitle` fallback；设置页安全分区展示并提交 `/api/v1/auth/jwt-secret` 的 JWT 密钥重置；设置页系统分区展示 `/api/v1/system/version` 的版本、通道、最新版本和更新状态；服务器详情指标卡展示 `/api/v1/servers/{id}/metrics` 的最新 CPU、内存、磁盘、网络收发和负载，API client 的默认 range 必须是后端支持的 `1h`。`/api/v1/servers/{id}/agent/certificate` 应在 API 层保留 typed client，但该接口返回私钥材料，新增 UI 入口前必须明确使用场景和权限提示。
- v4 应用与设施配置已使用 durable edit-session 工作流。`application-save-sessions/*`、`facility-apps/reverse-proxy/save-sessions/*`、`PUT /facility-apps/reverse-proxy`、`static-assets/*` 上传/删除、`application-template-catalog` 以及 `/applications/{id}/files|package|plan|validate|migrate` 等 legacy/低层接口已从后端移除，前端不得重新暴露；生产工作流统一走 edit-session。

### 阶段 3：概览 + 服务器 + SSH 凭据

`web/src/views/overview/`、`web/src/views/servers/`、`web/src/views/credentials/` 已替换阶段占位：

- 概览页是独立 Dashboard：左侧健康、容量、任务、安全摘要和可配置卡片；右侧 sticky 风险队列与快捷入口。正式 API 使用 `/api/v1/overview`、`/api/v1/overview/cards`、`/api/v1/overview/cards/{cardId}/data`；卡片区域是 6 列整数网格，`width`/`height` 表示 1x1 方格跨度（宽度 1-6，高度 1-4），卡片尺寸决定展示密度：小卡显示核心数字，中大卡显示趋势摘要或折线图。指标卡元信息必须显示实际服务器数量；多服务器指标不得只展示无来源的聚合单线，图表按服务器绘制独立折线，并在鼠标悬停时通过 tooltip 显示服务器名和对应数值。图表 tooltip 必须挂到页面级浮层而非限制在卡片内部，并使用 Panel popover token，避免被卡片滚动或 `overflow` 裁切。页面操作使用“编辑”进入布局编辑模式，编辑时允许拖动重排（经过目标卡片时实时更新顺序预览）、拖拽缩放、添加/删除卡片，并通过单卡编辑按钮调整类型、尺寸、时间范围和服务器范围；网络方向只在网络卡片中出现。服务器多选写入 `serverIds`，空数组表示全部服务器。退出编辑时统一保存布局。单卡失败留在卡片内，不在普通浏览态提供“查看/重试”等对象操作。布局上禁止用横向滚动兜底，卡片跨列必须受容器宽度保护，右侧栏必须在可用空间内收缩或在较窄宽度换到下方。
- 服务器页是运维主从工作台：`GET /api/v1/servers` 只加载列表摘要；选择或编辑服务器时通过 `GET /api/v1/servers/{id}` 按需加载完整详情。列表行展示 reachability、Agent 和权限信号；详情区展示连接、Agent、最新指标、最近操作，并接入创建、编辑、探测、测试连接、删除、重启、Agent 部署、UFW 安装。其他正式 API 使用 `/api/v1/servers/probe`、`/api/v1/servers/{id}/test`、`/restart`、`/agent/deploy`、`/agent/certificate`、`/metrics`、`/ufw/install`；指标展示使用 `/metrics` 返回的最新采样，不要求服务器列表携带指标详情。
- SSH 凭据页是独立 vault：支持密码/私钥创建编辑、secret 留空不更新、删除前引用影响、引用服务器测试。后端当前没有独立凭据测试或引用预检接口，前端用 `/api/v1/servers` 计算引用影响，并仅在存在引用服务器时通过 `/api/v1/servers/{id}/test` 测试；无引用时测试入口禁用。
- Mock 模式覆盖同名正式路径，包含正常、空、错误、冲突、长文本、不可达服务器、只读凭据、Agent 不同状态和十几台服务器的规模化样本；未实现路径继续返回 `mock_route_not_found`。
- `ApiClient` 已补充 204 No Content 支持，以匹配服务器/凭据 DELETE 真实响应。

### 阶段 4A：安全 + 资源

`web/src/views/security/` 与 `web/src/views/resources/` 已替换阶段占位：

- 防火墙与 Fail2Ban 归入“资源”一级菜单。`/resources/firewall` 使用 UFW 规则/状态矩阵；`/resources/fail2ban` 仅 dev 构建可直达和展示，非 dev 访问会跳回 `/resources/firewall`。旧 `/security/*` 只保留重定向。正式 API 使用 `/api/v1/servers/{id}/ufw`、`/ufw/rules`、`/ufw/enable`、`/ufw/install`、`/fail2ban`、`/fail2ban/enable`、`/fail2ban/release`、`/fail2ban/install`。
- 资源页是服务器上下文资源维护台：软件包、容器、镜像、网络、卷是 `/resources/packages|containers|images|networks|volumes` 独立路由页面，不使用页内 tabs。容器、镜像、网络、卷 GET 只读本地快照；镜像、网络、卷刷新按钮分别提交 `/images/refresh`、`/networks/refresh`、`/volumes/refresh` 异步任务，不得通过重复 GET 隐式访问节点。镜像应用升级接 `/api/v1/images/upgrade-selected|upgrade-all`。容器页对应用托管容器直接开放 start/stop/restart/delete，并展示“由应用托管，协调可能自动恢复”提示；卡片操作行换行时不得溢出隐藏。
- 网络资源当前后端只提供列表接口，页面只读展示拓扑并禁用删除，不使用 Mock 伪装不存在的能力。
- Mock 模式覆盖同名正式路径，包含正常、空、错误、权限不足、Agent 不兼容、不可达、长日志和危险确认状态；未实现路径继续返回 `mock_route_not_found`。

### 阶段 5：应用 + 设施应用

`web/src/views/applications/` 已替换阶段占位：

- 普通应用页 `/applications/apps` 是独立控制面，不再通过应用/设施应用顶层 tabs 互切。左侧应用选择与镜像/实例摘要读取 `/api/v1/applications` 的 `ApplicationSummary[]`，右侧详情和编辑入口按需读取 `/api/v1/applications/{id}` 完整 DTO 后展示状态、镜像更新、反向代理路由、运行时节点实例、日志入口、同步、停用、删除和持久化数据操作。运行时仍使用 `/api/v1/applications/{id}/runtime`，其他正式 API 使用 `/logs`、`/deploy`、`/stop`、`/image/check`、`/image/update`、`GET/POST /persistent-data` 和 `DELETE /api/v1/applications/{id}`；持久化下载走 blob 下载，上传走 multipart restore。
- 创建/编辑应用走隐藏路由 `/applications/apps/create` 与 `/applications/apps/:applicationId/edit`，使用 `EditorPage` 与 `/api/v1/application-edit-sessions` durable 会话；编辑器是分层 header + 连续纵向配置流 + 摘要区，不得恢复分区切换或等宽分页卡片。基本信息、运行设置、网络与访问、环境与存储、部署目标、应用文件按顺序全部展开，正文统一滚动；窄屏字段单列组织，禁止横向裁切。AppSpec 只有一个“YAML source / 源码”视图。文件区固定为一个上传入口，弹窗内选择文本文件、普通文件或文件夹压缩包：文本 JSON PUT 固定保存为 `template`，普通文件通过 `/uploads/{name}` multipart 固定保存为 `binary`，文件夹压缩包通过 `/archives` 保存为单个 `archive` 条目；类型和 MIME 不由用户填写。三类文件都支持会话态下载，binary/archive 替换必须保留原 `name`，正式详情支持已提交文件下载。文件工作区必须复用 `AssetFileManager`，不在应用页另写文件行交互。编辑器提交按钮点击时自动执行服务端检查并展示诊断，不再提供单独的「检查配置」按钮；修改草稿后自动清除过期的检查结果。
- 应用编辑器把命令作为参数列表编辑（不再用 textarea），新建对话框项默认空值；应用反向代理对话框提供 AnyAccess（保留英文）和入口代理同款 HTTP 选项，origin servers 默认按部署目标与网关节点自动推导并可手动覆盖；应用与设施编辑器保存期间使用覆盖整个编辑器的阻塞遮罩并禁止关闭。
- 设施应用页 `/applications/facility-apps` 是独立入口，不再通过应用/设施应用顶层 tabs 互切，也不暴露隐藏 `facility-reverse-proxy` 应用。设施类型由前端内置适配器固定提供，不调用设施 list API；当前唯一内置设施是 `reverse-proxy`，页面直接使用其专属详情与配置 API。新增设施类型时新增自己的类型、配置和 API adapter。
- 设施应用目录以带大图标和路由数等摘要的卡片按两列网格展示，点击卡片直接进入设施详情 `/applications/facility-apps/:facilityKind`；详情读取 `/api/v1/facility-apps/reverse-proxy`，展示网关节点、路由摘要、静态资产、应用路由、Panel 入口和当前 lifecycle operation。详情页“编辑”按钮在同一页就地展开编辑器，不再跳转独立配置页；兼容深链 `/applications/facility-apps/:facilityKind/config` 仍可直达编辑态。编辑器走 `/api/v1/facility-apps/reverse-proxy/edit-sessions`，与应用编辑器统一为连续纵向配置流 + sticky 摘要；网关服务器、域名和路由、Panel 访问入口和静态文件按顺序全部展开，正文统一滚动；域名和 Path 使用列表 + 对话框；域名对话框可配置负载均衡策略（轮询 / IP 哈希 / 主备切换及主源服务器），location 对话框可配置 gzip、请求体上限、代理超时、代理缓冲、WebSocket 模式、来源信息转发和自定义请求/响应头；服务器选择与展示统一使用服务器名称（名称来自 `/api/v1/servers`，未知 ID 回退为原值），Panel 入口服务器使用单选选择器。提交或取消后回到详情视图（提交后刷新设施数据）。静态文件新增/同 name 替换使用 `PUT .../assets/{assetName}` multipart，会话态和正式态分别通过对应 `/content` 接口下载；冲突时提供放弃当前修改并重新加载正式配置的明确入口。静态文件与应用文件共用 `AssetFileManager` 和请求封装，设施 adapter 只负责映射自己的配置契约。Path 弹窗保存时前端即时校验 path、跳转 URL、代理目标并内联报错；提交按钮点击时自动执行检查并展示诊断，不再提供单独的「检查配置」按钮；修改草稿后自动清除过期的检查结果。
- The facility asset workspace exposes one upload action. The upload dialog chooses text file, regular file, or folder archive; text selection opens the editor, while regular files and bundles use the upload control. New text assets derive the download filename from the reference name by default, still allow a separate download filename, and fall back to the reference name when it is left empty. Existing text assets open the same dialog in fixed editor mode. Only assets returned with `contentMode=text` can open the code editor; binary files and bundles remain replace/download/delete only, regardless of filename extension.
- Mock 模式覆盖同名正式路径，包含持久化数据 zip 下载/恢复、应用 archive 上传、设施静态资产上传/删除、正常应用、空/删除后状态、保存冲突、部署中、日志错误和长配置诊断；未实现路径继续返回 `mock_route_not_found`。

### 阶段 7：任务 + 设置 + 维护 + 诊断

`web/src/views/tasks/`、`web/src/views/settings/`、`web/src/views/maintenance/`、`web/src/views/debug/` 已替换阶段占位：

- 旧任务中心 `/tasks` 保留兼容路由，但不再作为产品导航入口；诊断页 `/debug` 也保留直达但不显示在菜单中。新的产品入口是“应用”一级菜单下的 `/application-operations` 操作记录与 `/system-events`：应用详情和概览快捷入口应跳转到操作记录，系统诊断事件通过系统事件页展示。
- 任务中心按 `operationId` 聚合，左侧保留操作组搜索、状态筛选和 URL query 恢复，右侧展示具体任务、步骤、日志、错误、重试和立即运行。任务执行项以 `summary` 为标题、`type` 为副标题，操作组副标题展示 `type`，用户可见位置不直接展示任务/操作原始 id；原始 id 仍用于搜索与 URL 恢复。正式 API 使用 `/api/v1/tasks`、`/api/v1/tasks/{id}`、`/logs`、`/steps`、`/retry`、`/run-now`。
- 设置页按 Runtime、安全、证书、系统、系统证书、备份还原分区独立保存，不提供全局保存。正式 API 使用 `/api/v1/settings/runtime`、`/api/v1/settings/server-variables`、`/api/v1/auth/jwt-secret`、`/api/v1/system/version`、`/api/v1/key-assets/system`、`/api/v1/key-assets/system/{id}/reset`、`/api/v1/backups/export`、`/api/v1/backups/restore/preflight`、`/api/v1/backups/restore/confirm`；系统版本只读展示，不和 Runtime 设置保存混在一起。由于 `/settings/runtime` 后端仍接收完整 runtime payload，前端保存某个分区时必须以已加载的 runtime 当前值为基底，只合入当前分区表单，避免提交其他分区尚未保存的脏值。系统证书分区展示 Panel 侧 Agent CA、Panel Agent client 证书以及服务器上报的 Agent 服务端证书，重置操作通过后台任务执行。
- 维护页是独立 shell，不走全局 AppShell；导出和还原维护 token 分别保存在 `sessionStorage.panel.maintenance.export.token` 与 `sessionStorage.panel.maintenance.restore.token`，二者和普通登录 session 隔离。正式 API 使用维护模式下的 `/api/v1/auth/*`、`/api/v1/backups/export/current|start|password|exit|{id}/download`、`/api/v1/restore/status|password|retry|clear-pending`；导出归档下载通过带 Authorization header 的 blob 请求完成。
- 诊断页使用 Runtime / Tasks / Database tabs，支持暂停/恢复轮询和手动刷新；刷新失败时保留上一份可用快照。Tasks tab 将运行时计数与任务定义分开呈现，任务定义必须使用可滚动表格展示，禁止直接把对象数组字符串化为 `[object Object]`。正式 API 使用 `/api/v1/debug/snapshot`。
- Mock 模式覆盖同名正式路径，包含正常、失败、长日志、维护中、保存冲突、诊断失败保留旧快照和任务中心多页分页样本；未实现路径继续返回 `mock_route_not_found`。

### 运行事件：操作记录 + 系统事件

`web/src/views/application-operations/` 与 `web/src/views/system-events/` 是统一运行事件能力的两个前端页面族：

- 操作记录读取 `/api/v1/application-operations`，主体是应用 operation 投影，支持按应用 ID、来源和状态筛选。列表应用列只展示应用名称快照，详情标题使用应用名称快照，用户可见位置不直接展示 `applicationId` / `operationId` 原始 id。详情可用时打开详情弹窗展示 targets 和 events；失败或部分失败记录在列表和详情中直接展示失败摘要；详情已清理时详情按钮禁用并显示清理提示。
- 系统事件读取 `/api/v1/system-events`，主体是诊断事件，支持按关联对象 ID、级别和类别筛选。页面只展示后端提供的事件类型与类别，不假设独立 alert 服务。
- 两个页面均使用 `ListPage`、`SearchInput`、`Select`、`Table`、`PaginationBar`、`StatusBadge` 和 `Dialog`，保持桌面内部滚动，不恢复页面级滚动。
- Mock 模式覆盖同名正式路径，包含详情可用、详情已清理、分页和筛选样本。

### 阶段 6：DNS + 证书 + 密钥资产

`web/src/views/dns/` 与 `web/src/views/certificates/` 已替换阶段占位：

- DNS 页是域名主从工作台：左侧域名/Provider 状态与搜索，右侧 Cloudflare 凭据说明、域名编辑/删除、记录同步、新增/编辑/删除记录和 Provider 错误区块。正式 API 使用 `/api/v1/dns/domains` 与 `/api/v1/dns/domains/{id}/records`。
- 域名证书页位于 `/certificates/domains`：展示 ACME 证书覆盖域名、签发任务入口、续签、删除、到期和失败状态。正式 API 使用 `/api/v1/certificates` 与 `/api/v1/certificates/{id}/renew`。
- 自签证书页位于 `/certificates/self-signed`：管理用户 CA/leaf，支持生成 CA、生成 leaf、重签/重生成和删除。正式 API 使用 `/api/v1/self-signed-cas`、`/api/v1/self-signed-certificates`、`/api/v1/self-signed-certificates/{id}/renew`。
- 密钥资产页位于 `/certificates/keys`：管理 CA/TLS/SSH key asset，支持生成、单资产导入、导出、下载、批量导入预检/执行、TLS 重签、SSH 重生成和删除。`GET /api/v1/key-assets` 和自签证书列表使用不含证书/私钥密文、public key 正文、metadata 与引用明细的摘要查询；引用安全校验在删除等定向操作中执行，不允许列表扫描全部应用 YAML 或反向代理配置。单资产文件与导出归档下载使用 blob 下载，不经 JSON `ApiClient`。
- 批量导入预检是 multipart route，前端在 `web/src/api/keyAssets.ts` 中局部使用 `fetch` 并保持 envelope 校验；未改全局 `ApiClient`。
- Mock 模式覆盖同名正式路径，包含正常、空记录、Cloudflare Provider 错误、域名删除冲突、多域名列表、证书签发/等待/过期/失败、任务创建、密钥资产引用冲突和批量导入冲突；未实现路径继续返回 `mock_route_not_found`。
- 主列表 Mock 与正式 API 对齐为 `ListPage`：`/servers`、`/credentials`、`/applications`、`/dns/domains`、`/certificates`、`/self-signed-certificates`、`/key-assets` 支持 `page`/`pageSize`/`q`；`GET /servers/:id` 返回完整服务器详情。

## API 与 Mock

- 正式 API 调用统一经 `ApiClient`。
- `ApiClient` 只接受统一 JSON envelope：`{ data }` 或 `{ error }`。
- 401、HTML 响应、非 JSON 响应、JSON 解析失败、缺少 data envelope、Abort 和网络错误必须明确抛出 `ApiError`，不得吞错并返回 fallback 数据。
- Mock API 只在 `VITE_PANEL_TEST_MODE=true` 时由 `web/src/main.ts` 安装。`task run:web:test` 默认不启用认证验证，Mock `/auth/session` 直接返回已认证演示 session；需要登录、token、强制改密和 JWT secret 校验时运行 `task run:web:test AUTH=true`，由 `VITE_PANEL_TEST_AUTH=true` 开启。
- 主资源列表 Mock 必须返回 `ListPage`（`items`/`total`/`page`/`pageSize`），并支持 `page`/`pageSize`/`q`：`/servers`、`/credentials`、`/applications`、`/dns/domains`、`/certificates`、`/self-signed-certificates`、`/key-assets`。`GET /servers/:id` 返回完整详情。
- 演示样本需覆盖多样状态与分页规模：约 20 台服务器、多命名空间应用（部署中/部分部署/失败/停用）、DNS/证书签发与续签/失败、任务与运行事件多页、资源与安全按服务器差异化默认值。
- 未实现 Mock 路由必须返回 `{ error: { code: "mock_route_not_found" } }`，不得返回假成功。

## i18n

- 新增或修改用户可见文案必须写入 `web/src/i18n/index.ts` 英文和简体中文词条。
- 路由元信息只写 `meta.titleKey`，不写用户可见文案。

## 验证

- 只改前端时运行 `task test:web`。
- 依赖、构建链路、路由、样式或类型变化后运行 `task build:web`。
- 需要 Mock 人工联调时运行 `task run:web:test`；需要认证链路联调时运行 `task run:web:test AUTH=true`。
- 不使用 curl、browser 或浏览器强行验收。
