# 多语言翻译状态

- `web/src/views/applications/index.vue`、`web/src/components/ui/Dropdown.vue`
  - 应用文件和设施资产共用一个上传入口；上传弹窗的文本文件、普通文件、文件夹压缩包类型选择，以及编辑器/文件选择分支、替换、两态下载、字节摘要、应用与设施连续纵向配置区（瀑布流配置）、资产空态和冲突重载入口已补齐英文与简体中文词条；文件名、路径和后端错误保持运行时原值，kind/MIME 不再作为用户输入展示。

- `web/src/components/ui/Dialog.vue`、`web/src/components/ui/ToastProvider.vue`、`web/src/components/shell/AppShell.vue`
  - 对话框关闭、Toast 关闭和主导航区域的可访问名称已接入英文与简体中文词条。

- `web/src/views/application-operations/index.vue`、`web/src/views/system-events/index.vue`、`web/src/components/shell/navModel.ts`、`web/src/views/applications/index.vue`、`web/src/views/certificates/index.vue`
  - “操作记录”导航位于“应用”一级菜单最后一个，路由仍保留 `/application-operations`；操作记录和系统事件的路由、筛选、分页、空态、状态、详情弹窗和“详情已清理”提示已接入英文与简体中文词条；证书详情旧任务中心提示改为系统事件语义；应用 ID、操作 ID、事件类型、日志引用和后端诊断摘要保持运行时原值。

- `web/src/views/application-operations/index.vue`、`web/src/views/system-events/index.vue`、`web/src/i18n/index.ts`
  - 系统事件列表、系统事件详情标题和操作记录详情事件线的 runtime event type 已通过 `runtimeEventTypes.*` 接入英文与简体中文展示文案；筛选参数和后端 `eventType` 稳定 key 仍保持原值，未知事件类型回退显示原始 key。

- `web/src/views/applications/index.vue`
  - Panel 入口固定使用 setup 登记的宿主节点，缺少宿主节点及尝试移出全局网关时的英文与简体中文提示已补齐。

- `web/src/views/application-operations/index.vue`, `web/src/views/system-events/index.vue`, `web/src/views/settings/index.vue`
  - Runtime event contract fixes use `applicationNameSnapshot`, `eventType`, `partial_failed`, canonical system event categories, and runtime event retention settings; new user-visible labels and validation text are wired through English and Simplified Chinese frontend i18n entries.
本文档记录当前多语言实现仍未完成翻译的区域。后续每次处理多语言相关任务时，都应同步更新。

## 最近已补齐

- `web/src/views/credentials/index.vue`、`web/src/views/certificates/index.vue`
  - SSH 私钥凭据和密钥资产导入弹窗改用共享材料编辑器，编辑器可访问名称及 CA/TLS 材料 tab 复用既有英文与简体中文词条；私钥、证书正文和后端错误保持用户或运行时原值。

- `web/src/views/applications/index.vue`、`web/src/components/ui/CodeEditor.vue`
  - 应用模板文件弹出编辑器的高亮语言选项、正文加载状态、加载失败保护和二进制不可文本编辑提示已接入英文与简体中文词条；文件路径、MIME 和正文保持用户原始值。

- `web/src/components/shell/navModel.ts`、`web/src/router/index.ts`、`web/src/views/resources/index.vue`、`web/src/views/security/index.vue`、`web/src/views/applications/index.vue`
  - 导航调整为一级“资产”分组，资源下展开软件包、容器、镜像、网络、卷和防火墙，并在 dev 构建显示 Fail2Ban；诊断从菜单隐藏但保留 `/debug` 直达；资源、防火墙、应用和设施应用顶层互切 tabs 已移除。新增导航分组文案 `layout.nav.assets` 已补齐英文与简体中文；既有页面标题文案复用原 routes 词条。

- `web/src/views/certificates/index.vue`
  - 域名证书申请弹窗改为多前缀输入，前缀标签、占位示例和每行一个前缀提示已接入英文与简体中文词条；域名、前缀值、变量名和后端诊断保持用户原始输入。

- `web/src/views/auth/LoginPage.vue`、`web/src/router/index.ts`
  - 普通登录新增强制改密闭环：`passwordChangeRequired` session 会停留在登录页，通过 `/api/v1/auth/account` 提交当前密码、用户名和新密码后再进入控制台；相关表单、校验和失败提示已接入英文与简体中文词条。

- `web/src/views/settings/index.vue`、`web/src/api/keyAssets.ts`
  - 设置页安全分区新增 JWT 密钥重置入口，系统证书分区改为接入 `/api/v1/key-assets/system` 和 `/api/v1/key-assets/system/{id}/reset` 的真实后端能力；证书名称、类型、状态、指纹、有效期、重置确认与任务接受反馈已接入英文与简体中文词条。证书 ID、服务器名、指纹和 task ID 保持后端原值。

- `web/src/views/auth/LoginPage.vue`、`web/src/stores/session.ts`、`web/src/views/settings/index.vue`、`web/src/views/servers/index.vue`
  - 普通认证已接入真实 `/api/v1/auth/login|logout|session|account|jwt-secret` Bearer token 链路；设置页系统分区展示 `/api/v1/system/version` 版本与更新状态；服务器详情展示 `/api/v1/servers/{id}/metrics` 最新 CPU、内存、磁盘、网络和负载指标。新增登录失败、强制改密、系统版本、更新状态、服务器指标失败与网络收发文案已接入英文与简体中文；token、版本号、通道名、服务器 ID 和后端诊断保持实例原值。

- `web/src/views/auth/LoginPage.vue`、`web/src/stores/session.ts`
  - 普通登录改为真实 `/api/v1/auth/login|session|logout` 链路后，登录失败和强制改密提示已补齐英文与简体中文；用户名、后端错误详情和 branding 配置保持实例原值。

- `web/src/views/applications/index.vue`
  - 应用创建/编辑器重做为连续纵向配置流，配置区改用基本信息、运行设置、网络与访问、环境与存储、部署目标、应用文件等面向任务的名称；YAML 源码、配置检查、变更预览、保存并应用、文件引用名称/下载文件名说明和空状态文案已接入英文与简体中文词条；应用名、镜像引用、服务器 ID、域名、Path、文件路径和后端诊断保持实例原值。

- `web/src/views/applications/index.vue`、`web/src/views/tasks/index.vue`
  - 页面接入统一 `SearchInput`、`PaginationBar`、`ServerMultiPicker`、`FileUploadButton`、`DownloadButton`、`StatusBadge` 后，新增公共文案 `common.clearSearch`、`common.complete`、`common.error` 以及应用服务器选项说明已补齐英文与简体中文；应用与设施编辑器在同一连续纵向配置流中展开全部配置区；服务器 ID、任务状态原始值和上传文件名保持实例原值。

- `web/src/views/applications/index.vue`、`web/src/api/facilityApps.ts`
  - 设施应用目录/详情/配置三层信息架构、入口代理设施卡片、设施分类、设施状态和未知设施不可用空态已接入 `web/src/i18n/index.ts` 的英文与简体中文词条；`facilityKind`、服务器 ID、域名、Path、后端诊断和操作状态保持实例原值。

- `web/src/views/applications/`、`web/src/api/applications.ts`、`web/src/api/facilityApps.ts`
  - 应用持久化数据下载/恢复、应用编辑会话压缩包上传、设施入口静态资产上传，以及对应成功反馈和能力说明已接入 `web/src/i18n/index.ts` 的英文与简体中文词条；上传文件名、归档内容和后端任务 ID 保持原值。

- `web/src/views/tasks/index.vue`
  - 任务列表分页控件、上一页/下一页和分页摘要已接入 `web/src/i18n/index.ts` 的英文与简体中文词条；任务 ID、操作 ID 和后端日志保持原始文本。

- `web/src/views/applications/`（应用/设施编辑体验专项）
  - 应用创建/编辑器的连续纵向配置流、基本信息/运行设置/网络与访问/环境与存储/部署目标/应用文件标题、结构化变量/环境/端口/挂载/反向代理对话框、YAML 生成/应用、配置检查、变更预览、保存并应用和变更摘要文案已接入 `web/src/i18n/index.ts` 的英文与简体中文词条。
  - 设施入口网关配置的网关服务器、域名和路由、Path 类型、静态文件引用、Panel 访问入口、变更摘要、配置检查和对话框文案已接入英文与简体中文词条；静态文件的引用名称与下载文件名已分别说明，应用名、服务器 ID、域名、Path、镜像引用、后端诊断和日志正文保持实例原值。

- `web/src/views/tasks/`、`web/src/views/settings/`、`web/src/views/maintenance/`、`web/src/views/debug/`（v4 阶段 7 任务 + 设置 + 维护 + 诊断页面族）
  - 任务操作组、状态筛选、步骤/日志/错误、重试/立即运行，设置分区保存、备份导出、还原预检/确认，维护登录、阶段、能力按钮、下载入口，以及诊断 Runtime/Tasks/Database tab、暂停/恢复、stale 快照提示、任务运行时指标和任务定义表格列名已接入 `web/src/i18n/index.ts` 的英文与简体中文词条；任务类型、并发策略、数据库表名、日志正文和后端原始诊断保持实例原值。

- `web/src/views/dns/`、`web/src/views/certificates/`（v4 阶段 6 DNS + 证书 + 密钥资产页面族）
  - DNS 域名、Cloudflare Provider 状态、DNS 记录 CRUD、域名证书签发/续签/删除、自签证书生成/重签/删除、密钥资产生成/导入/导出/下载/批量导入预检/执行和冲突反馈文案已接入 `web/src/i18n/index.ts` 的英文与简体中文词条；域名、证书名、指纹、任务 ID、Cloudflare/ACME/密钥归档原始诊断保持实例原值。

- `web/src/views/security/`、`web/src/views/resources/`（v4 阶段 4A 安全 + 资源页面族）
  - UFW、Fail2Ban、软件包、容器、镜像、网络和卷工作台的页面标题、状态、空态、确认对话框、日志弹窗、权限/Agent 阻断、任务提交和操作反馈文案已接入 `web/src/i18n/index.ts` 的英文与简体中文词条；服务器名、容器名、镜像引用、卷路径、Docker/Agent/Fail2Ban/UFW 原始诊断和日志正文保持实例原值。

- `web/src/views/applications/`（v4 阶段 5 应用 + 设施应用页面族）
  - 应用列表/详情、运行时节点实例、镜像更新、日志、同步/停用/删除确认、隐藏创建/编辑 EditorPage、durable edit-session、文件对话框、校验/预览/提交反馈、设施入口网关只读页和配置页文案已接入 `web/src/i18n/index.ts` 的英文与简体中文词条；应用名、镜像引用、域名、Path、服务器 ID、后端诊断和日志正文保持实例原值。

- `web/src/views/overview/`、`web/src/views/servers/`、`web/src/views/credentials/`（v4 阶段 3 页面族）
  - 概览 Dashboard 编辑/保存布局、6 列整数网格、拖动/缩放/添加/删除/单卡编辑、卡片范围/网络流量方向/服务器多选、服务器数量、单卡失败状态、风险队列、快捷入口，服务器主从详情、添加/编辑/探测/测试/删除/Agent/UFW 操作，SSH 凭据 vault、secret 留空不更新、引用影响与引用服务器测试文案已接入 `web/src/i18n/index.ts` 的英文与简体中文词条；图表 tooltip 中的服务器名、服务器主机、任务 ID、SSH/Agent 原始诊断保持实例原值。

- `web/src/components/shell/`、`web/src/components/ui/`、`web/src/views/auth/LoginPage.vue`
  - 2026-07-21 v4 基础设施移除 Naive UI 后，AppShell、主题/语言/账户入口、自有 UI 基础反馈、登录页、页面族独立入口占位和模板名称已接入 `web/src/i18n/index.ts` 的英文与简体中文词条；示例业务对象名、服务器名和后端原始诊断保持实例原值。

- `web/src/components/shell/`、`web/src/views/overview/`、`web/src/views/operations/`、`web/src/views/auth/LoginPage.vue`
  - 2026-07-21 当前 v3 交付版本的 AppShell、导航、主题/语言/账户入口、概览 Dashboard、主要页面族共享数据工作台、登录页、Mock 操作反馈和状态/摘要文案已接入 `web/src/i18n/index.ts` 的英文与简体中文词条；Mock 示例对象名、服务器名、应用名、域名、镜像引用和任务 ID 保持实例原值。

- `web/src/components/shell/`、`web/src/views/overview/`、`web/src/views/console/`、`web/src/views/auth/LoginPage.vue`
  - 2026-07-21 重建后的控制台 shell、导航、主题/语言入口、概览和通用工作台页面已接入 `web/src/i18n/index.ts` 的英文与简体中文词条；示例对象名、服务器名、应用名、域名和运行时诊断保持实例原值。

- `web/src/views/resources/`（v3 容器资源页面族）
  - 容器、镜像、网络、卷四个资源页的 PageHeader 说明、服务器选择器、表格列、空态、确认对话框、日志弹窗、镜像拉取/刷新/升级、未使用资源清理和托管容器禁用提示已接入英文与简体中文词条；服务器名、容器名、镜像引用、卷路径、Docker/registry 原始诊断和日志正文保持原值。

- `web/src/views/applications/`（v3 应用 + 设施应用页面族，子阶段 5c/5d）
  - 普通应用创建/编辑隐藏页、durable edit-session 状态条、恢复草稿、离开保护、保存并应用校验/预览/提交反馈、文件/挂载/反代对话框，以及设施应用只读详情与入口网关配置页新增文案已接入英文与简体中文词条；应用名、服务器名、域名、Path、Header、后端诊断和用户文件路径保持原值。

- `web/src/views/applications/`（v3 应用列表页面族，子阶段 5b）
  - 列表页搜索/空态/分页、`运行中 · 有更新` 复合状态、详情四区（状态/镜像更新/反代路由/运行时）、停用与删除确认（删除带容器/反代路由/持久化数据影响说明）、迁移与持久化恢复/导入对话框、运行时日志弹窗、任务类操作"查看任务"反馈均已接入英文与简体中文词条；应用名、服务器名、digest、域名、后端原始错误保持原值。

- `web/src/views/security/`、`web/src/views/packages/`（v3 安全防护 + 软件包页面族）
  - 安全防护页页内 tabs（防火墙 / fail2ban）、防火墙状态摘要与启用确认（区分安装/启用并提示 SSH 端口）、添加规则 dialog 端口校验、删除规则/jail 确认、fail2ban 状态行与草稿保存时间、接管/释放确认、YAML 形状校验提示、软件包页搜索与筛选空态、批量选中栏、准入阻断 warning 均已接入英文与简体中文词条；服务器名、jail 名、软件包名与版本、后端原始错误保持原值。

- `web/src/views/servers/`（v3 服务器 + SSH 凭据页面族）
  - 页内 tabs（节点 / SSH 凭据）、搜索与空态、详情四区（状态/系统/运行时/访问）、Agent 与 UFW 状态、初始化中进度态、测试连接与 probe 结果、添加/编辑服务器与凭据对话框（secret 留空 = 不更新）、删除影响说明、凭据引用预检与 409 `credential_in_use` 兜底、任务类操作"查看任务"反馈均已接入英文与简体中文词条；服务器名、主机地址、接口地址、后端原始错误与备注保持原值。

- `web/src/views/overview/`（v3 概览页面族）
  - 概览 Dashboard 的页面说明、"卡片设置"对话框（显示开关、时间范围、网络方向、服务器范围、恢复默认）、卡片四态（partial/unavailable/empty）文案、单卡重试、保存成功与乐观锁冲突提示、无服务器空态 CTA 均已接入英文与简体中文词条；issue code、服务器名、软件包/应用名与版本等实例数据保持原值。

- `web/src/views/auth/`、`web/src/views/maintenance/BackupMaintenancePage.vue`（v3 认证页面族）
  - 登录/修改密码的必填校验、强制改密标题、新密码与当前密码相同提示、改密成功反馈，以及备份维护页的维护登录卡、n-steps 步骤名、各阶段上下文文案均已接入英文与简体中文；后端返回的字段级错误消息与安全错误摘要保持原值。

- `web/src/views/credentials/_v2/`、`web/src/views/firewall/_v2/`、`web/src/views/packages/_v2/`
  - 隔离 Credentials/Firewall/Packages V2 页面的列表筛选、状态、空状态、添加/编辑/删除确认、规则和软件包操作、任务中心入口已接入英文与简体中文；服务器名、IP、软件包名、版本、仓库、用户备注和引用对象名称保持实例原值。

- `web/src/views/servers/_v2/`
  - Servers V2 的列表筛选、连接/Agent/系统状态、详情分区、编辑、凭据加载/空状态、候选连接探测安全原因、初始化、Agent/重启/UFW 安装操作、Panel host 限制和删除影响已补齐英文与简体中文；服务器名、地址、接口名、用户备注与仅供内部诊断的 probe `diagnostic` 保持原值，其中内部诊断不得直接展示。

- `web/src/views/overview/_v2/`
  - Overview V2 的页面状态、卡片类型、局部失败、stale、布局保存/冲突、命名尺寸、键盘移动播报、紧凑图例按钮触发的当前值 tooltip、RX/TX 数据表和编辑操作已补齐英文与简体中文；SVG 图形保持纯视觉，服务器名、主机地址、软件包/应用名称与版本等实例数据保持原值。

- `web/src/app/v2/AppShellV2.vue`、`web/tests/foundation/harness/ProductShellHarness.vue`
  - 隔离产品壳的领域导航、主题/账户入口、任务 ticker、更新提示、服务器示例页和可访问名称已接入英文与简体中文；版本、用户名、节点名称、IP 地址等稳定或实例数据保持原值。

- `web/src/views/maintenance/_v2/`
  - 备份导出与还原维护页的内嵌登录、会话过期、离线/stale、状态协议失败、进度播报、版本/加密信息、下载/退出、重试与危险清除确认均已补齐英文和简体中文。
  - 稳定 phase 继续复用 `backupRestore.phases.*`；后端安全错误摘要、版本和归档元数据保持原值。

- `web/src/views/auth/_v2/`
  - 新普通登录与强制改密隔离页的品牌、密码显示/隐藏、表单总错误、提交失败及“认证成功但下一页无法打开”的安全继续提示已接入英文与简体中文；只有页面白名单内的字段错误展示后端安全消息，未知字段降级为本地化总错误。

- `web/src/views/applications/apps/ApplicationEditor.vue`、`ApplicationDetail.vue`、`facility-apps/index.vue`、`config.vue`、`internal/platform/i18n`
  - AnyAccess、全局网关节点、源站节点、转发节点、流量分配策略、主源站，以及设施/应用路由只读详情和 Path 高级摘要已补齐英文与简体中文词条。
  - 新增源站有效性、AnyAccess 策略、主源站和全局域名所有权错误码的简体中文翻译；稳定枚举值和用户输入的域名、Path、Header、地址保持原值。

- `web/src/views/applications/facility-apps/index.vue`、`config.vue`、`RoutePathAdvancedFields.vue`、`internal/platform/i18n`
  - 入口网关只读详情、独立反向代理配置页、未保存提示、保存会话阶段、域名入口节点/上游策略、删除确认和 Path 高级字段已接入英文与简体中文词条。
  - 新增设施域名、上游域名独占、配置并发冲突和 `reverse_proxy_*` 高级字段校验错误码的简体中文翻译；服务器地址、域名、Path、Header 名称和值以及 Nginx/Agent 原始诊断保持用户原始内容。

- `web/src/views/applications/apps/ApplicationEditor.vue`
  - 应用文件夹压缩包改为独立 `archive` 文件类型展示和替换，新增文件类型文案 `applicationEditor.archiveKind`，已接入英文与简体中文词条。
- `web/src/views/overview/index.vue`
  - 概览卡片拖拽手柄新增本地化无障碍文案 `overviewPage.moveCard`，已接入英文与简体中文词条。
- `web/src/views/applications/apps/create.vue`、`web/src/router/index.ts`
  - 应用创建与编辑共用隐藏独立页，新增创建/编辑路由标题和“返回应用”按钮文案，已接入英文与简体中文词条。
- `web/src/views/applications/apps/ApplicationEditor.vue`
  - 创建页 AppSpec 新增“可视化 / YAML”互斥模式和 YAML 代码编辑器状态文案，已接入英文与简体中文词条；部署目标、反向代理、变量和文件仍作为 YAML 外的应用级字段展示。
- `web/src/views/applications/apps/ApplicationEditor.vue`
  - 创建页编辑器的文件新增/替换入口文案已接入英文与简体中文词条；应用文件编辑保持聚焦对话框，编辑/覆盖时锁定类型。
- `web/src/views/applications/apps/ApplicationEditor.vue`
  - 应用文件区新增模板编辑、二进制文件替换和文件夹压缩包替换文案，已接入英文与简体中文词条；文件内容和压缩包文件名保持用户原始输入。
- `web/src/views/tasks/index.vue`
  - Task Center deployment target projection labels, target diagnostics, retry/backoff text, claimed-task text, and lifecycle target state labels are wired through `web/src/i18n/index.ts` for English and Simplified Chinese. Docker/Agent diagnostic payloads are displayed as original runtime text and are not translated.
- `web/src/views/applications/apps/ApplicationEditor.vue`
  - 应用编辑器主按钮从“保存并部署”调整为“保存并应用 / Save and apply”，保存中的阻塞遮罩和保存阶段文案已接入英文与简体中文词条；保存启用应用只提交保存会话，由保存接口触发协调。
- `web/src/views/applications/apps/ApplicationEditor.vue`、`internal/modules/applications/spec`、`internal/platform/i18n`
  - 应用挂载高级权限项新增默认收起的“挂载选项”、Docker 只读挂载和应用文件可执行开关，已接入英文与简体中文词条；后端新增 file/panel_file/persistent 挂载属主和 file/persistent mode 校验文案的简体中文翻译。
- `web/src/views/applications/apps/index.vue`、`web/src/views/applications/facility-apps/index.vue`、`internal/platform/i18n`
  - 应用控制平面重构后的“同步 / 停用 / 立即同步 / 删除后由同步清理运行时资源”文案已接入英文与简体中文词条；新增后端错误码 `application_reconciler_unavailable` 已接入简体中文翻译。
- `web/src/views/tasks/index.vue`
  - 任务中心新增应用协调与目标任务展示名：`application_target_batch`、`application_target_apply`、`application_target_stop`、`application_target_purge` 已接入英文与简体中文词条。
  - 任务中心新增应用部署上下文字段：部署列、目标标题、动作、generation 与 spec hash 摘要已接入英文与简体中文词条。
- `web/src/views/applications/facility-apps/index.vue`
  - 入口网关应用路由区域新增“打开应用”入口，英文与简体中文词条已接入 `web/src/i18n/index.ts`。
- `web/src/views/applications/apps/ApplicationEditor.vue`
  - 应用编辑器新增 Docker capability (`capAdd`) 可视化输入，字段标签、提示和添加按钮已接入英文与简体中文词条；保存值为 Docker capability 稳定标识，不翻译。
- `web/src/views/applications/apps/ApplicationEditor.vue`
  - 应用编辑器运行时分区新增“Resources / 资源限制”小节文案，Docker capability 可视化文案优化为“Linux capabilities / 容器能力”和“添加能力”，保存值仍为 Docker capability 稳定标识，不翻译。
- `web/src/views/tasks/index.vue`
  - 任务中心批量父任务新增“操作汇总”行，父任务步骤、日志和错误可直接查看；新增标签已接入英文与简体中文词条。
- `internal/modules/applications/spec`、`internal/platform/i18n`
  - appspec `persistent` 挂载新增 `uid`、`gid`、`mode` 权限校验文案，应用编辑器可视化挂载行新增 UID/GID/权限字段，已接入英文与简体中文翻译；宿主机权限应用失败继续保留系统原始诊断。
- `web/src/layouts/AppLayout.vue`、`web/src/router/index.ts`
  - 菜单重排为服务器、安全、资源和应用分组，新增 fail2ban 独立路由标题，已接入英文与简体中文词条；旧路径仅作为重定向，不新增展示文案。
- `internal/agent/client/client.go`、`internal/platform/i18n`
  - Agent maintenance gRPC 错误现在作为 `agent_request_failed` 业务错误返回，简体中文按前缀翻译为“Agent 请求失败：...”，保留远端命令原始诊断。
- `web/src/views/settings/backups/index.vue`、`web/src/views/maintenance/backup.vue`、`internal/modules/backups`
  - 新增备份与还原设置页、备份导出维护页、维护阶段、启动期输入导出密码、危险确认、重启进入备份导出模式、还原预检和 pending restore 提示文案，已接入英文与简体中文词条。
  - 导出维护页改为登录后手动开始导出，补齐“开始导出”、ready 阶段和开始失败提示的英文与简体中文词条。
  - 新增备份/还原后端错误码 `restore_*` 等简体中文翻译；备份归档中的业务数据和第三方诊断不在页面展开显示。
- `web/src/views/applications/apps/ApplicationDetail.vue`
  - 持久化数据上传入口新增“导入持久化数据”未部署迁移文案，已接入英文与简体中文词条；已部署应用继续使用恢复并重启文案。
- `web/src/views/applications/apps/ApplicationEditor.vue`
  - 应用反向代理目的地选择、本地/容器选项已接入英文与简体中文词条；稳定值 `local`、`container` 不翻译。
- `internal/platform/http`、`internal/platform/i18n`、`internal/modules/applications`
  - 应用保存、计划、部署、迁移、刷新、镜像更新和重部署流程中的校验失败会保留具体字段与错误原因：`application_invalid` 的 `error.message` 显示第一条 `<field>: <message>`，完整列表放在 `error.details.issues`，并按当前语言翻译 issue message，避免统一翻译覆盖 appspec/YAML 的真实问题。
- `web/src/views/applications/facility-apps/index.vue`
  - Entrance gateway domain groups, route type labels, redirect fields, proxy_pass fields, and source-request mode labels are wired through `web/src/i18n/index.ts` for English and Simplified Chinese.
  - Facility reverse proxy deployment records, empty states, target table labels, and facility-specific lifecycle stages are wired through `web/src/i18n/index.ts` for English and Simplified Chinese.
  - Panel access entry labels, host/domain fields, enable switch, and helper text are wired through `web/src/i18n/index.ts` for English and Simplified Chinese.
  - Panel access entry backend validation codes are wired through `internal/platform/i18n` for Simplified Chinese.
- `web/src/views/applications/apps/ApplicationRuntimePanel.vue`
  - 应用运行时新增 lifecycle operation 摘要、部署阶段列、部分部署状态和部署阶段文案，已接入英文与简体中文词条；Agent/Docker 原始错误原因继续保留原文。
- `web/src/views/applications/apps/ApplicationRuntimePanel.vue`、`web/src/views/applications/apps/index.vue`、`web/src/views/applications/apps/ApplicationDetail.vue`
  - 应用运行时新增 `missing` 状态，用于展示期望存在但 Docker 中已缺失的托管容器；英文与简体中文词条已接入 `web/src/i18n/index.ts`，稳定值 `missing` 不翻译。
- `web/src/views/security/fail2ban/index.vue`、`internal/modules/servers/fail2ban.go`
  - fail2ban 页面重构为防护规则默认模式与 YAML 高级模式，新增接管、保存草稿、取消接管、规则模板、Panel 配置状态和确认提示文案，已接入英文与简体中文词条；目标机 `fail2ban-client`、systemd 和命令诊断保持原文。
- `web/src/views/applications/apps/index.vue`
  - 应用选择器中“运行中 · 有更新”状态已接入英文和简体中文词条。
- `web/src/layouts/AppLayout.vue`、`web/src/theme.ts`
  - 页头主题菜单的自动/浅色/深色模式、明暗共用或独立预设、蓝绿红橘紫粉黄预设名称、恢复默认主题，以及账户菜单的用户名与退出入口已接入英文和简体中文词条。
- `web/src/views/applications/apps/ApplicationDetail.vue`
  - 应用详情统一为基本信息、镜像和运行实例分区，新增“基本信息”英文与简体中文标题。
- `web/src/views/settings/system-certificates/index.vue`、`web/src/layouts/AppLayout.vue`
  - 新增“设置 → 系统证书”导航与路由标题，复用系统证书状态、详情和重置确认的英文与简体中文词条。
- `web/src/views/applications/apps/ApplicationDetail.vue`
  - 应用详情镜像更新闭环新增节点级状态、更新节点数量、检查失败和更新镜像按钮文案，已接入英文与简体中文词条；镜像仓库或 Docker 原始诊断继续保留原文。
- `web/src/views/resources/packages/index.vue`、`web/src/views/security/firewall/index.vue`、`internal/platform/i18n`
  - 软件包与防火墙页面的特权能力提示已改为 root 或免密 sudo，并补齐统一特权准入错误的简体中文翻译。
- `web/src/views/debug/index.vue`
  - 隐藏 Debug 诊断页的运行时/Tasks/数据库 Tab、任务定义能力、数据库文件切换、表与索引空间统计文案已接入英文和简体中文词条；数据库表名和任务类型稳定标识不翻译。
- `web/src/components/RuntimeLogsDialog.vue`、`web/src/views/resources/containers/index.vue`
  - 应用运行时日志和容器日志弹窗、刷新按钮、tail 行数和空日志文案已接入英文与简体中文词条；日志正文保留容器原始输出。
- `web/src/views/applications/apps/ApplicationDetail.vue`
  - 持久化数据下载、上传覆盖并重启、无损迁移对话框和失败提示已接入英文与简体中文词条；下载/上传内容为 zip 文件，文件内容保留原始数据。
- `web/src/views/resources/`、`internal/modules/containers`
  - 资源导航、容器/镜像/网络/卷页面、托管警告、镜像批量升级和资源删除文案已接入英文与简体中文词条；Docker Engine 和镜像仓库原始诊断保留原文。
  - 镜像“更多”菜单、全部更新确认、删除未使用镜像确认，以及卷删除未使用确认已接入英文与简体中文词条。
  - 容器、镜像和卷同步操作完成提示，以及 `volume_refresh` 刷新任务类型已接入英文与简体中文词条；容器状态改由 Agent report stream 更新，不再提供 `container_refresh` 任务类型。

- `web/src/views/settings/_shared/SettingsPageContent.vue`、`internal/modules/settings`
  - Runtime 日志等级设置的前端标签、选项文案和 `invalid_log_level` 后端错误码已接入英文、简体中文词条；进程日志内容保持英文，不纳入翻译。
- `internal/modules/observability/overview`、`web/src/views/overview/index.vue`
  - 概览卡片数据库保存失败文案及卡片配置校验错误码已接入英文、简体中文词条。
- `web/src/layouts/AppLayout.vue`
  - Header 的 DEV 构建通道标识、完整版本提示和移动端导航入口已接入英文、简体中文词条。
  - 已移除旧运行时节点导航入口，对应词条不再保留。
- `web/src/views/settings/_shared/SettingsPageContent.vue`
  - Token 过期时间、语言、登录页标题/说明、安全设置、证书设置和系统信息文案已接入 `web/src/i18n/index.ts`。
  - 旧运行时设置分类和字段已移除。
- `web/src/views/servers/_shared/ServersPageContent.vue`
  - 服务器凭据必选提示、Docker host 表单、Agent 正常/不兼容/异常/无法部署状态、版本检查时间、安装/重装入口和 agent 部署任务文案已接入 `web/src/i18n/index.ts`。
- `web/src/views/applications/apps/`
  - 应用运行时实例状态、期望状态、日志入口、容器名和实例 ID 文案已接入 `web/src/i18n/index.ts`。
- `web/src/views/applications/apps/ApplicationEditor.vue`
  - Application command 数组行标签、提示已接入英文、简体中文词条；应用编辑器不再提供独立 args 字段。
- `web/src/views/tasks/index.vue`
  - 任务中心按“操作”为聚合对象、“任务”为具体执行对象展示；批量父任务的执行模式、操作内任务数和子任务序号已接入英文与简体中文词条。
  - 任务类型名称、类型筛选选项、搜索按钮、多选筛选占位、操作标题、步骤名称、任务阶段和日志面板任务类型已按稳定标识翻译。
  - 操作选择列表的服务器、应用、证书、密钥资产、任务批次和系统任务资源类别已接入英文与简体中文词条；对象缺少可解析名称时不显示裸资源 ID。
  - 手动运行与重试入口由任务 API 返回的注册能力控制，不再按任务类型维护前端白名单；本次未新增需要翻译的任务操作文案。
  - ACME 账号、订单、授权、DNS 验证、DNS 清理和 finalize 任务阶段已接入英文与简体中文词条；ACME/Cloudflare 原始诊断仍保留原文。
  - 容器、镜像、卷、应用协调、Agent 证书重置和常见执行阶段已补齐英文与简体中文词条，避免任务中心显示原始枚举名。
- `web/src/views/certificates/key-assets/index.vue`
  - 自签证书/密钥拆分导航、系统内置与用户域标签、Agent 系统证书状态和重置确认，以及 CA、TLS、SSH、批量导入导出、冲突确认和引用提示已接入英文与简体中文词条。
- `internal/platform/i18n`
  - 统一 API 错误翻译入口覆盖认证、设置、服务器、UFW、DNS、证书、应用、任务和密钥资产的主要错误码。
  - 补齐应用文件/迁移/持久化数据、镜像仓库、ACME/Cloudflare、容器选择器、设施静态站点、SSH、模板渲染和任务服务等后端 API 错误码的简体中文翻译；动态错误使用按错误码的前缀翻译保留第三方原始诊断。
  - 任务已结束后再次运行的 `task_not_runnable` 后端错误码已接入简体中文翻译。
  - 应用名称重复保存错误和 appspec `command` 数组用法错误已接入翻译。
  - 应用运行时操作失败和多目标部署汇总失败使用中文前缀翻译并保留 Agent/Docker 原始诊断，避免创建或部署应用时显示泛化内部错误。
  - Application 部署改为 Panel 侧原子步骤编排；部署步骤名写入任务日志和运行时错误详情，作为任务诊断文本暂保留原文。
  - Agent 自动部署的 TLS 不可用、版本不兼容、本地 `panel-agent` 二进制缺失、自动部署多次失败，以及 agent 必需、agent 不兼容、agent runtime client 不可用错误码已接入翻译。
- `web/src/views/certificates/key-assets/index.vue`
  - 系统内置证书说明已更新为展示 Panel 侧 Agent CA、客户端证书，以及每台服务器已签发的 Agent 服务端证书。

## 前端仍有少量第三方原始文本

以下内容仍会展示第三方系统直接返回的原始描述，当前保留原样以避免误译：

- Cloudflare / ACME 返回的 provider 诊断文本。
- SSH / 远程执行 / sudo 命令输出和超时诊断。
- 镜像仓库或容器运行时返回的错误文本。

## 恢复维护过渡页

- `internal/modules/backups/restore_app.go` 中的内嵌恢复维护页已补上维护登录和 session 过期处理，以保证 Vue 重构切换前恢复流程仍可安全使用；该过渡页当前只提供英文。最终 `/maintenance/restore` Vue SPA 必须把 auth、phase、capability、结构化错误和按钮文案接入 `web/src/i18n/index.ts` 的英文与简体中文词条后再移除内嵌页。

## 后端翻译仍为部分覆盖

当前后端已覆盖统一 API 错误翻译入口中的主要业务错误码；以下类别仍按原文或后续任务日志统一方案处理：

- 任务 summary、任务 system 日志、任务过期清理写入的错误原因与远程命令诊断文本。
- 服务器删除写入任务的取消原因仍按任务错误文本展示，后续任务日志/原因统一翻译时一并处理。

## 更新规则

发生以下任一情况时，必须更新本文档：

- 新页面或新组件接入了多语言。
- 某个页面仍未翻译但继续被修改。
- 新增后端错误码或用户可见错误文本。
- 新增用户可见文案但暂未完成翻译。

- `web/src/views/settings/backups/index.vue`、`web/src/views/maintenance/backup.vue`、`internal/modules/backups`
  - 备份导出、还原确认和导出维护退出在容器内自重启时新增自动重启提示文案，已接入英文与简体中文词条；不新增独立 restart API。
 
- `web/src/views/settings/_shared/SettingsPageContent.vue`
  - Metrics report interval and container report interval labels are wired through `web/src/i18n/index.ts` for English and Simplified Chinese.

## Backend Error Translation Notes

- `container_managed_by_application` currently returns its English source message. Add Simplified Chinese coverage when container resource action errors are next wired through backend i18n.
# 2026-08-01 shared asset upload dialog

- Application files and facility static assets now expose one upload button. The dialog selects text, regular file, or folder archive; text selection shows the editor while the other types show the shared file upload control. Existing text assets open the same dialog in fixed editor mode. English and Simplified Chinese labels are wired through `web/src/i18n/index.ts`.

# 2026-07-30 facility text assets

- Facility create-text, upload-file, upload-archive, edit, loading, and save controls reuse existing English and Simplified Chinese translation keys. Asset names, filenames, and backend diagnostics remain instance values.
- The facility text dialog revision-conflict recovery action has matching English and Simplified Chinese text and explicitly states that unsaved text will be discarded.

# 2026-07-31 facility and application file names

- Application files and facility static assets now share the `AssetFileManager` interaction and multipart/DELETE request helpers; no new user-facing translation keys were needed. Facility types remain fixed adapters without a generic facility list API.
- Application files and facility static assets use scope-local `name` identity rather than path semantics. The duplicate facility asset name error and application file name validation are covered by Simplified Chinese backend translations, and the editor labels use Name/名称.
