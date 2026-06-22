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
- 页面按菜单层级组织在 `web/src/views/` 下；每个菜单页面使用独立目录和 `index.vue` 入口。
- 仅在同一菜单组内复用的实现放在对应 `web/src/views/<group>/_shared/`；跨页面共享组件继续放在 `web/src/components/`。
- 全局布局在 `web/src/layouts/AppLayout.vue`；侧边导航列表必须在抽屉内部独立滚动。
- 全局页头根据系统版本 API 返回的构建通道展示环境标识；`dev` 通道在标题旁显示紧凑 DEV Chip，正式通道不显示。
- 全局主题偏好只保存在当前浏览器，由 `web/src/theme.ts` 管理；支持自动跟随系统、手动浅色/深色，以及明暗共用或分别选择内置主题预设。旧 `linux-panel-theme` 值会迁移为对应手动模式。
- 页头只展示主题和账户图标入口；用户名与退出登录操作收纳在账户菜单中，不在页头常驻展示。
- 路由标题使用 `meta.titleKey`，不要在路由元信息里写用户可见文案。
- 新增或修改用户可见文案必须走 `useI18n()` 和 `web/src/i18n/index.ts`，并更新多语言状态文档。
- 持久化 UI 配置只保存稳定值，不保存已经翻译的标题或说明。
- API 调用经 `ApiClient`，默认 base URL 是 `/api/v1`；后端 API 变更时同步更新 `web/src/api/`、`web/src/types/api.ts` 和相关测试。
- 不依赖后端的前端测试模式通过 `task run:web:test` 启动；该任务设置 `VITE_PANEL_TEST_MODE=true`，由 `web/src/mocks/` 接管 `/api/v1` 请求并自动建立演示会话。
- Mock API 必须保持统一 JSON envelope，并与 `web/src/api/` 的现有路径和响应结构一致；主要页面新增或修改接口时同步维护 Mock 路由和种子数据。
- Mock 数据只保存在当前浏览器页面的内存中，常用写操作会更新内存状态，刷新页面后恢复种子数据；未实现的 Mock 路由必须返回明确的 `mock_route_not_found` 错误。
- `ApiClient` 期望后端返回统一 JSON envelope；如果服务返回 HTML 或其他非 JSON 内容，前端必须转换为本地化的可读错误。
- 用户可见的数据列表应使用共享分页组件 `web/src/components/AppPagination.vue`；数组型接口优先通过 `web/src/composables/usePagination.ts` 做前端分页。
- 页面或列表的初次网络加载应使用 `web/src/components/PageLoadingState.vue`；已有内容后的刷新保留卡片 `:loading` 或按钮 loading。
- 中大屏保持桌面或多栏布局时，页面最外层容器必须填满全局页头以下的剩余视口并隐藏自身溢出，禁止 `v-main` 或整页纵向滚动；滚动只允许发生在明确的内部内容区。
- 页面级 Alert、工具栏、摘要和分页只按内容高度展示；只有主工作区吸收中大屏的剩余高度。
- 满高列表或表格卡片中，分页和固定操作区必须贴底；中间列表/表格体负责吸收剩余高度并内部滚动。
- `760px` 以下恢复页面自然高度和 `v-main` 页面级纵向滚动，列表与卡片解除桌面端纵向内部滚动，表格仅保留必要的横向滚动。
- 紧凑页头保持菜单、标题和工具按钮单行；只有存在活跃任务时才增加第二行 ticker。
- 主从双栏页面统一使用 `clamp(300px, 26vw, 340px)` 左栏和 `18px` 栏间距，并在 `1080px` 以下折叠为单列。
- 切换左侧选择项后如果右侧需要异步加载详情，必须立即清空旧详情并展示 `PageLoadingState`，同时忽略迟到响应。
- 服务器节点、服务器资源、域名和任务中心的左侧选择器统一使用 `AppSelectorPanel.vue` 与 `AppSelectorItem.vue`；服务器节点与服务器业务选择还必须共同使用 `ServerSelectorItem.vue`，防火墙、软件包更新、容器、镜像、网络和卷通过 `ServerSelector.vue` 复用同一展示。
- 域名证书、自签证书和密钥页面同样使用 `AppSelectorPanel.vue` 主从布局；对象操作位于右侧详情，导入与导出位于选择器更多菜单。
- 应用页面使用同一选择器主从布局，应用操作位于右侧详情标题区。共享选择器标题区不显示数量 Chip。

## 常见页面对应关系

- 概览：`web/src/views/overview/index.vue`
  - 页面顶部只保留卡片编辑操作，不展示独立的 signal 摘要区。
  - 卡片布局通过 `web/src/api/overview.ts` 的 `GET/PUT /api/v1/overview/cards` 从数据库读取和保存，不使用 localStorage。
  - 指标卡片数据通过 `GET /api/v1/overview/cards/{cardId}/data` 按卡片 ID 拉取，由后端展开该卡片的服务器范围并返回 `metricsByServer`，前端不再按服务器逐个请求指标。
  - 新增、编辑、删除和重置后保存完整布局；拖拽时只更新本地顺序，在拖拽结束时保存一次。
- 服务器菜单组：`web/src/views/servers/`
  - 节点：`node/index.vue`
  - 凭据：`credentials/index.vue`
  - 防火墙：`firewall/index.vue`
  - 软件包：`packages/index.vue`
  - 组内共享：`_shared/`
  - 服务器详情的 Agent 操作通过 `POST /api/v1/servers/{id}/agent/deploy` 创建部署任务；未安装时显示安装，已安装但异常时显示重装，并通过任务中心跟踪结果。
- 容器化菜单组：
  - 一级菜单排列在域名菜单之前。
  - 应用：`web/src/views/runtime/applications/index.vue`
  - 容器、镜像、网络、卷：`web/src/views/containerization/`
  - API：`web/src/api/containerization.ts`
- Runtime 应用实现：
  - 应用运行时面板位于 `applications/` 目录内，日志查看复用 `web/src/components/RuntimeLogsDialog.vue` 弹窗展示 agent runtime 实例或 Docker 容器日志。
- DNS 域名与记录：`web/src/views/dns/domains/index.vue`
- 证书菜单组：`web/src/views/certificates/`
  - 域名证书：`domains/index.vue`
  - 自签证书：`self-signed/index.vue`
  - 密钥：`keys/index.vue`
  - 两个页面复用 `key-assets/index.vue` 的资产工作区
- 任务中心：`web/src/views/tasks/index.vue`
- 设置菜单组：`web/src/views/settings/`
  - 通用：`general/index.vue`
    - 运行时表单包含指标保留、采集间隔、远程命令超时、清理计划、Token 过期时间、语言和后端日志等级；日志等级保存为稳定值 `debug`、`info`、`warn` 或 `error`。
    - 设置页不使用页头全局保存；每个可编辑 section 在自身底部提供独立保存按钮，并且只提交该 section 的字段，避免连带保存其他 section 的草稿。
  - 安全：`security/index.vue`
  - 证书：`certificates/index.vue`
  - 系统：`system/index.vue`
  - 系统证书：`system-certificates/index.vue`
  - 组内共享：`_shared/`
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
