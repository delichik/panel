# 多语言翻译状态

本文档记录当前多语言实现仍未完成翻译的区域。后续每次处理多语言相关任务时，都应同步更新。

## 当前仍未完全覆盖

### 最近已补齐

- `web/src/layouts/AppLayout.vue`
  - 移动端导航入口文案已接入 `web/src/i18n/index.ts`
  - 未启用的 DNS 记录导航入口已移除，对应 `layout.nav.records` 词条不再保留。
- `web/src/features/dns/pages/DomainsPage.vue`
  - DNS 域名详情、Cloudflare 记录列表、记录创建/编辑/删除和 TTL/代理状态文案已接入 `web/src/i18n/index.ts`。
- `web/src/api/client.ts`
  - 非 JSON API 响应的可读错误文案已接入 `web/src/i18n/index.ts`。
- `internal/dns`
  - Cloudflare 非 JSON 响应错误码已接入 `internal/i18n/i18n.go`。
- `web/src/features/settings/pages/SettingsPage.vue`
  - Token 过期时间设置及选项文案已接入 `web/src/i18n/index.ts`
  - 设置分类子菜单、通用设置、安全设置、Nomad 设置、证书设置、系统信息文案已接入 `web/src/i18n/index.ts`
  - 登录页标题和说明自定义字段、留空回退提示已接入 `web/src/i18n/index.ts`
- `web/src/features/auth/pages/ChangePasswordPage.vue`
  - 首次强制改密页面文案已接入 `web/src/i18n/index.ts`
- `internal/auth`
  - 登录失败的通用错误文案 `Authentication failed` 已接入 `internal/i18n/i18n.go`
  - 强制改密、账号更新、JWT 密钥更新相关错误文案已接入 `internal/i18n/i18n.go`
- `web/src/features/nomad/pages/NomadNodesPage.vue`
  - Nomad 节点重部署、集群重建、server 切换及切换后 client 配置同步文案已接入 `web/src/i18n/index.ts`
  - 旧集群网络地址迁移、SSH 地址与 Nomad 网卡地址选择、重建后应用恢复提示已接入 `web/src/i18n/index.ts`
  - 首个 server 引导任务入口、反向代理同步任务提示文案已接入 `web/src/i18n/index.ts`
- `web/src/features/servers/pages/ServersPage.vue`
  - 服务器凭据必选提示、重启确认与任务入口、UFW 安装任务入口、系统架构/CPU/分项网卡详情文案已接入 `web/src/i18n/index.ts`
  - 新增服务器后的首连信息采集任务提示、失败回退提示和超时提示已接入 `web/src/i18n/index.ts`
- `web/src/features/firewall/pages/FirewallPage.vue`
  - UFW 防火墙状态、启用确认、规则表单、规则列表、删除确认和任务入口文案已接入 `web/src/i18n/index.ts`
- `web/src/features/tasks/pages/TaskCenterPage.vue`
  - 任务类型名称、类型筛选特殊选项、搜索按钮和多选筛选占位文案已接入 `web/src/i18n/index.ts`
  - 操作标题、步骤名称、任务阶段和日志面板任务类型已按稳定标识翻译。
- `web/src/components/AppPagination.vue`
  - 共享分页组件的每页条数与总数文案已接入 `web/src/i18n/index.ts`。
- `web/src/components/PageLoadingState.vue`
  - 共享加载组件文案 `common.loading` 已接入 `web/src/i18n/index.ts`，英文和简体中文均已补齐。
- `web/src/features/applications/components/ApplicationEditor.vue`
  - 自定义变量表单、变量插入和 Panel 托管文件挂载文案已接入英文和简体中文。
- `web/src/features/certificates/`
  - Nomad 内置证书、域名立即续签、自签 CA/证书管理和危险确认文案已接入英文和简体中文。
- `web/src/layouts/AppLayout.vue`、`web/src/features/settings/pages/SettingsPage.vue`
  - 当前版本、最新版本弱提示和系统版本字段已接入 `web/src/i18n/index.ts`。
- raw Nomad jobs/deployments 清单入口已移除，对应页面文案和路由词条不再保留。
- `internal/nomad`
  - Nomad 重部署、集群重建、server 切换相关 API 错误码已接入 `internal/i18n/i18n.go`

### 前端仍有少量第三方原始文本

以下内容仍会展示第三方系统直接返回的原始描述，当前保留原样以避免误译：

- `web/src/features/applications/components/ApplicationRuntimePanel.vue`
  - `deployment.StatusDescription`
  - `evaluation.StatusDescription`
  - `evaluation.Type`

### 后端翻译仍为部分覆盖

当前后端已覆盖统一 API 错误翻译入口，但以下类别仍需继续补齐：

- Cloudflare / ACME / 镜像仓库相关错误码
- SSH / 远程执行 / 超时相关错误码
- 模板渲染、选择器解析等底层错误码
- 第三方系统直接返回的原始错误文本
- 任务摘要、任务 system 日志、任务过期清理写入的错误原因与远程命令诊断文本仍以原始执行文本展示，包括 Nomad 引导/加入流程日志

## 更新规则

发生以下任一情况时，必须更新本文档：

- 新页面或新组件接入了多语言
- 某个页面仍未翻译但继续被修改
- 新增了后端错误码或用户可见错误文本
- 新增了用户可见文案但暂未完成翻译

## 密钥与证书

- `web/src/features/certificates/pages/KeyAssetsPage.vue` 的 CA、TLS、SSH、批量导入导出、冲突确认和引用提示已接入英文与简体中文词条。
- `key_asset_*` 主密钥、归档、类型、父 CA、使用中和导入冲突错误码已接入 `internal/i18n/i18n.go`。
- 任务中心已补充密钥资产任务类型、阶段和操作标题翻译。
