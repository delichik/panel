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

## 技术约定

- 前端使用 Vue 3、Vue Router、Pinia、Vuetify、ECharts 和 Vitest。
- 页面按 `web/src/features/<feature>/` 分组，共享组件放在 `web/src/components/`。
- 全局布局在 `web/src/layouts/AppLayout.vue`；侧边导航列表必须在抽屉内部独立滚动，避免菜单项超出视口后不可访问。
- API 调用经 `ApiClient`，默认 base URL 是 `/api/v1`；后端 API 变更时同步更新 `web/src/api/`、`web/src/types/api.ts` 和相关测试。
- 路由标题使用 `meta.titleKey`，不要在路由元信息里写用户可见文案。
- 新增或修改用户可见文案必须走 `useI18n()` 和 `web/src/i18n/index.ts`，并更新多语言状态文档。
- 持久化 UI 配置只保存稳定值，不保存已翻译的标题或说明。

## 常见页面对应关系

- 概览：`web/src/features/overview/pages/OverviewPage.vue`
- 服务器与凭据：`web/src/features/servers/pages/ServersPage.vue`
- 软件包：`web/src/features/packages/pages/PackageUpdatesPage.vue`
- 应用：`web/src/features/applications/`
- DNS 域名：`web/src/features/dns/pages/DomainsPage.vue`；不要新增或恢复 DNS 记录管理入口。
- 证书：`web/src/features/certificates/pages/CertificatesPage.vue`
- Nomad：`web/src/features/nomad/`
- 任务中心：`web/src/features/tasks/`
- 设置：`web/src/features/settings/pages/SettingsPage.vue`
- 登录与强制改密：`web/src/features/auth/pages/`

## 验证

- 先按模块索引的“检查和测试范围”判断是否需要验证。
- 需要验证前端逻辑和组件改动时，优先运行 `task test:web`。
- 类型、路由、构建链路、样式或依赖变更且需要构建检查时，运行 `task build:web`。
- 本仓库要求不要使用 curl、browser 等工具强行检查页面。

## 文档更新触发

新增页面、路由、API 模块、store、共享组件约定、用户可见工作流或前端持久化结构时，必须更新本文档或对应功能模块文档。
