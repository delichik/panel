# 多语言翻译状态

本文档记录当前多语言实现仍未完成翻译的区域。后续每次处理多语言相关任务时，都应同步更新。

## 当前仍未完全覆盖

### 最近已补齐

- `web/src/features/settings/pages/SettingsPage.vue`
  - Token 过期时间设置及选项文案已接入 `web/src/i18n/index.ts`
- `internal/auth`
  - 登录失败的通用错误文案 `Authentication failed` 已接入 `internal/i18n/i18n.go`

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

## 更新规则

发生以下任一情况时，必须更新本文档：

- 新页面或新组件接入了多语言
- 某个页面仍未翻译但继续被修改
- 新增了后端错误码或用户可见错误文本
- 新增了用户可见文案但暂未完成翻译
