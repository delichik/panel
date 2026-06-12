# 前端模块

## 适用场景

修改 Vue 页面、共享组件、前端 API client、类型定义、Pinia store、路由、样式、图表或前端测试时，先读本文档。

## 关键入口

- 前端入口：`web/src/main.ts`
- 根组件：`web/src/app/App.vue`
- 布局：`web/src/layouts/AppLayout.vue`
- 路由：`web/src/router/index.ts`
- API client：`web/src/api/client.ts`
- API 模块：`web/src/api/`
- API 类型：`web/src/types/api.ts`
- 状态：`web/src/stores/`
- 功能页面：`web/src/features/`
- 多语言：`web/src/i18n/index.ts`
- 样式：`web/src/styles/main.css`、`web/src/theme.ts`
- 可复用组件设计规范：`docs/agents/specifications/frontend/INDEX.md`

## 技术约定

- 前端使用 Vue 3、Vue Router、Pinia、Vuetify、ECharts 和 Vitest。
- 新增页面或共享组件前先查阅 `docs/agents/specifications/frontend/INDEX.md`，沿用已有基础组件、组合模式和响应式规则。
- 页面按 `web/src/features/<feature>/` 分组，共享组件放在 `web/src/components/`。
- 全局布局在 `web/src/layouts/AppLayout.vue`；侧边导航列表必须在抽屉内部独立滚动，避免菜单项超出视口后不可访问。
- 侧边导航二级菜单支持 `NavItem.icon`；DNS 域名入口应显示域名图标，避免分组展开后入口缺少视觉锚点。
- 全局布局会读取 `/api/v1/system/version`，只用弱提示展示可用新版本，不提供下载或安装入口；系统设置页展示当前版本。
- API 调用经 `ApiClient`，默认 base URL 是 `/api/v1`；后端 API 变更时同步更新 `web/src/api/`、`web/src/types/api.ts` 和相关测试。
- `ApiClient` 期望后端返回统一 JSON envelope；如果服务返回 HTML 或其他非 JSON 内容，前端必须转换为本地化的可读错误，而不是暴露底层 `Unexpected token` 解析异常。
- 路由标题使用 `meta.titleKey`，不要在路由元信息里写用户可见文案。
- 新增或修改用户可见文案必须走 `useI18n()` 和 `web/src/i18n/index.ts`，并更新多语言状态文档。
- 持久化 UI 配置只保存稳定值，不保存已翻译的标题或说明。
- 宽表格依赖全局 `.v-table__wrapper` 横向滚动；页面卡片可以隐藏外溢，但不要用固定宽度让整页在窄屏被撑开。
- 用户可见的数据列表应使用共享分页组件 `web/src/components/AppPagination.vue`；数组型接口优先通过 `web/src/composables/usePagination.ts` 做前端分页，已有服务端分页接口保留服务端分页请求。
- 页面或列表的初次网络加载应使用共享 `web/src/components/PageLoadingState.vue`；已有内容后的刷新可保留卡片 `:loading` 或按钮 loading，避免遮挡当前内容。
- 页面级多栏布局优先使用 `minmax(0, 1fr)` 和移动端断点；工具栏、弹窗操作区、表单重复行在 760px 左右必须能换行或纵向堆叠。
- 中大尺寸屏幕维持桌面或多栏布局时，页面最外围容器必须填满全局页头以下的剩余视口并隐藏自身溢出，禁止 `v-main` 或整个页面纵向滚动；列表、表格、详情正文、日志等内容区通过 `min-height: 0` 和明确的 `overflow: auto` 独立滚动。进入窄屏单列布局后才可按需恢复页面级滚动。
- 左侧选择器、右侧详情的桌面双栏页面统一使用 `clamp(300px, 26vw, 340px)` 左栏和 `18px` 栏间距，并在 `1080px` 以下切换为单栏；选择器列表使用紧凑的平面选中态，避免页面间宽度和视觉跳变。
- 切换选择项后需要异步读取右侧详情时，必须立即清空上一项详情并展示 `PageLoadingState`；请求结果需要校验当前选择，忽略快速切换产生的迟到响应。本地已有数据的即时切换不伪造加载动画。

## 常见页面对应关系

- 概览：`web/src/features/overview/pages/OverviewPage.vue`
- 服务器与凭据：`web/src/features/servers/pages/ServersPage.vue`
- 软件包：`web/src/features/packages/pages/PackageUpdatesPage.vue`
- 防火墙：`web/src/features/firewall/pages/FirewallPage.vue`
- 应用：`web/src/features/applications/`
- DNS 域名与记录：`web/src/features/dns/pages/DomainsPage.vue`；页面左侧选择域名，右侧展示域名详情和 Cloudflare DNS 记录表。
- 证书为一级菜单，包含内置证书、域名证书和自签证书三个二级页面；旧 `/dns/certificates` 重定向到 `/certificates/domains`。
- Nomad：`web/src/features/nomad/`；只保留设置、加入和节点控制平面入口，不恢复 raw jobs/deployments 清单入口。
- 任务中心：`web/src/features/tasks/`
- 设置：`web/src/features/settings/pages/SettingsPage.vue`
- 登录与强制改密：`web/src/features/auth/pages/`；登录页使用居中的单卡片布局、纯 CSS 背景装饰，并在窄屏下收紧间距，不依赖外部图片资源。登录页通过公开的 `GET /api/v1/settings/public-branding` 读取自定义标题和说明，空值或读取失败时回退到当前语言的 `login.*` 默认文案。
- 通用设置可编辑登录页标题和说明；标题最多 80 字符，说明最多 240 字符，留空表示使用多语言默认文案。

## 验证

- 先按模块索引的“检查和测试范围”判断是否需要验证。
- 需要验证前端逻辑和组件改动时，优先运行 `task test:web`。
- 类型、路由、构建链路、样式或依赖变更且需要构建检查时，运行 `task build:web`。
- 本仓库要求不要使用 curl、browser 等工具强行检查页面。

## 文档更新触发

新增页面、路由、API 模块、store、共享组件约定、用户可见工作流或前端持久化结构时，必须更新本文档或对应功能模块文档。

## 密钥与证书页面

- 证书一级菜单的第三个二级入口为“密钥与证书”，路由 `/certificates/key-assets`；旧自签路由重定向到该页面。
- 页面按 CA 证书、TLS 证书、SSH 密钥对分栏，支持生成、导入、下载、删除、重新签发或重新生成。
- 批量导出要求输入至少 12 位密码；批量导入使用 multipart 上传，先展示预检摘要、冲突和受影响引用，再提交处理策略。
- 前端 API 位于 `web/src/api/keyAssets.ts`，共享 client 的 `postForm` 负责 multipart 请求。
