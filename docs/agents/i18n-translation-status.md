# 多语言翻译状态

本文档记录当前多语言实现的覆盖范围，以及仍未完成翻译的区域。后续每次处理多语言相关任务时，都应同步更新。

## 本轮已完成

### 基础设施

- `web/src/i18n/index.ts`
  - 建立前端词典与格式化工具
  - 补齐共享 `common.*` 词条
  - 为总览页补充专用词条
  - 更新登录副标题的 Debian / Ubuntu 多发行版描述
  - 更新 Nomad 引导页的多发行版与免密 sudo eligibility 提示
  - 为服务器页补充 UFW 状态与安装入口词条
- `internal/i18n/i18n.go`
  - 补充 `server_not_reachable` 后端错误码的简体中文翻译
- `web/src/stores/settings.ts`
  - 运行时设置加载后同步前端语言
- `internal/settings/service.go`
  - 运行时设置支持 `language`
- `internal/i18n/i18n.go`
  - 后端基础翻译入口已建立
- `internal/httpx/httpx.go`
  - API 错误响应接入统一翻译

### 已接入翻译的前端页面 / 组件

- `web/src/layouts/AppLayout.vue`
- `web/src/router/index.ts`
- `web/src/features/auth/pages/LoginPage.vue`
- `web/src/features/settings/pages/SettingsPage.vue`
- `web/src/features/overview/pages/OverviewPage.vue`
- `web/src/features/servers/pages/ServersPage.vue`
- `web/src/features/dns/pages/DomainsPage.vue`
- `web/src/features/certificates/pages/CertificatesPage.vue`
- `web/src/features/packages/pages/PackageUpdatesPage.vue`
- `web/src/features/applications/pages/ApplicationsPage.vue`
- `web/src/features/applications/components/ApplicationDetail.vue`
- `web/src/features/applications/components/ApplicationEditor.vue`
- `web/src/features/applications/components/ApplicationRuntimePanel.vue`
- `web/src/features/applications/components/ApplicationLogsPanel.vue`
- `web/src/features/tasks/pages/TaskCenterPage.vue`
- `web/src/features/nomad/pages/NomadNodesPage.vue`
- `web/src/features/nomad/pages/NomadSetupPage.vue`
- `web/src/components/ServerSelector.vue`
- `web/src/components/tasks/TaskLogPanel.vue`

### 架构调整

- 仪表盘卡片标题改为按 `kind` 在渲染时动态翻译，不再依赖持久化文案
- 多处筛选项、对话框选项、状态标签改为运行时按当前语言生成

## 当前仍未完全覆盖

### 前端仍有少量第三方原始文本

以下内容仍会展示第三方系统直接返回的原始描述，当前保留原样以避免误译：

- `web/src/features/applications/components/ApplicationRuntimePanel.vue`
  - `deployment.StatusDescription`
  - `evaluation.StatusDescription`
  - `evaluation.Type`

### 仍未纳入本轮整理的页面

以下页面当前仍是占位或重定向路径，不在本轮主要翻译范围内，后续如果启用真实界面需要重新纳入：

- `web/src/features/deployments/pages/DeploymentsPage.vue`
- `web/src/features/nomad/pages/NomadJobsPage.vue`

### 后端翻译仍为部分覆盖

当前后端已覆盖统一 API 错误翻译入口，但以下类别仍需继续补齐：

- Cloudflare / ACME / 镜像仓库相关错误码
- SSH / 远程执行 / 超时相关错误码
- 模板渲染、选择器解析等底层错误码
- 第三方系统直接返回的原始错误文本

## 后续优先级

### P1

- 补齐高频 Nomad / 基础设施第三方原始描述的简体中文映射策略
- 补齐高频后端错误码的简体中文映射

### P2

- 为尚未启用的页面预留统一翻译接入方式
- 持续梳理新增运行时枚举值，避免前端出现新的直出技术值

## 更新规则

发生以下任一情况时，必须更新本文档：

- 新页面或新组件接入了多语言
- 某个页面仍未翻译但继续被修改
- 新增了后端错误码或用户可见错误文本
- 新增了用户可见文案但暂未完成翻译
