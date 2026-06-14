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
- 路由标题使用 `meta.titleKey`，不要在路由元信息里写用户可见文案。
- 新增或修改用户可见文案必须走 `useI18n()` 和 `web/src/i18n/index.ts`，并更新多语言状态文档。
- 持久化 UI 配置只保存稳定值，不保存已经翻译的标题或说明。
- API 调用经 `ApiClient`，默认 base URL 是 `/api/v1`；后端 API 变更时同步更新 `web/src/api/`、`web/src/types/api.ts` 和相关测试。
- `ApiClient` 期望后端返回统一 JSON envelope；如果服务返回 HTML 或其他非 JSON 内容，前端必须转换为本地化的可读错误。
- 用户可见的数据列表应使用共享分页组件 `web/src/components/AppPagination.vue`；数组型接口优先通过 `web/src/composables/usePagination.ts` 做前端分页。
- 页面或列表的初次网络加载应使用 `web/src/components/PageLoadingState.vue`；已有内容后的刷新保留卡片 `:loading` 或按钮 loading。
- 中大屏保持桌面或多栏布局时，页面最外层容器必须填满全局页头以下的剩余视口并隐藏自身溢出，禁止 `v-main` 或整页纵向滚动；滚动只允许发生在明确的内部内容区。
- 页面级 Alert、工具栏、摘要和分页只按内容高度展示；只有主工作区吸收中大屏的剩余高度。
- `760px` 以下恢复页面自然高度和 `v-main` 页面级纵向滚动，列表与卡片解除桌面端纵向内部滚动，表格仅保留必要的横向滚动。
- 紧凑页头保持菜单、标题和工具按钮单行；只有存在活跃任务时才增加第二行 ticker。
- 主从双栏页面统一使用 `clamp(300px, 26vw, 340px)` 左栏和 `18px` 栏间距，并在 `1080px` 以下折叠为单列。
- 切换左侧选择项后如果右侧需要异步加载详情，必须立即清空旧详情并展示 `PageLoadingState`，同时忽略迟到响应。

## 常见页面对应关系

- 概览：`web/src/views/overview/index.vue`
  - 卡片布局通过 `web/src/api/overview.ts` 的 `GET/PUT /api/v1/overview/cards` 从数据库读取和保存，不使用 localStorage。
  - 指标卡片数据通过 `GET /api/v1/overview/cards/{cardId}/data` 按卡片 ID 拉取，由后端展开该卡片的服务器范围并返回 `metricsByServer`，前端不再按服务器逐个请求指标。
  - 新增、编辑、删除和重置后保存完整布局；拖拽时只更新本地顺序，在拖拽结束时保存一次。
- 服务器菜单组：`web/src/views/servers/`
  - 节点：`node/index.vue`
  - 凭据：`credentials/index.vue`
  - 防火墙：`firewall/index.vue`
  - 软件包：`packages/index.vue`
  - 组内共享：`_shared/`
- Runtime 菜单组：`web/src/views/runtime/`
  - 应用：`applications/index.vue`
  - Nomad 节点：`nomad/nodes/index.vue`
  - Nomad 初始化：`nomad/setup/index.vue`
  - 组内共享：`nomad/_shared/`
- DNS 域名与记录：`web/src/views/dns/domains/index.vue`
- 证书菜单组：`web/src/views/certificates/`
  - 内置证书：`builtin/index.vue`
  - 域名证书：`domains/index.vue`
  - 密钥与证书：`key-assets/index.vue`
  - 旧 `/dns/certificates` 重定向到 `/certificates/domains`
- 任务中心：`web/src/views/tasks/index.vue`
- 设置菜单组：`web/src/views/settings/`
  - 通用：`general/index.vue`
    - 运行时表单包含指标保留、采集间隔、远程命令超时、清理计划、Token 过期时间、语言和后端日志等级；日志等级保存为稳定值 `debug`、`info`、`warn` 或 `error`。
  - 安全：`security/index.vue`
  - Nomad：`nomad/index.vue`
  - 证书：`certificates/index.vue`
  - 系统：`system/index.vue`
  - 组内共享：`_shared/`
- 登录与强制改密：`web/src/views/auth/`

## 验证

- 先按模块索引的“检查和测试范围”判断是否需要验证。
- 需要验证前端逻辑和组件改动时，优先运行 `task test:web`。
- 类型、路由、构建链路、样式或依赖变更且需要构建检查时，运行 `task build:web`。
- 本仓库要求不要使用 curl、browser 等工具强行检查页面。

## 文档更新触发

新增页面、路由、API 模块、store、共享组件约定、用户可见工作流或前端持久化结构时，必须更新本文档或对应功能模块文档。

## 密钥与证书页面

- 证书一级菜单的第三个二级入口为“密钥与证书”，路由 `/certificates/key-assets`；旧自签路径重定向到该页面。
- 页面按 CA 证书、TLS 证书、SSH 密钥对分栏，支持生成、导入、下载、删除、重新签发或重新生成。
- 批量导出要求输入至少 12 位密码；批量导入使用 multipart 上传，先展示预检摘要、冲突和受影响引用，再提交处理策略。
- 前端 API 位于 `web/src/api/keyAssets.ts`，共享 client 的 `postForm` 负责 multipart 请求。
