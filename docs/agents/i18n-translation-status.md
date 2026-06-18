# 多语言翻译状态

本文档记录当前多语言实现仍未完成翻译的区域。后续每次处理多语言相关任务时，都应同步更新。

## 最近已补齐

- `web/src/views/debug/index.vue`
  - 隐藏 Debug 诊断页的刷新状态、Go runtime、内存/GC、数据库连接与表统计文案已接入英文和简体中文词条；数据库表名和稳定诊断标识不翻译。
- `web/src/components/RuntimeLogsDialog.vue`、`web/src/views/containerization/containers/index.vue`
  - 应用运行时日志和容器日志弹窗、刷新按钮、tail 行数和空日志文案已接入英文与简体中文词条；日志正文保留容器原始输出。
- `web/src/views/runtime/applications/ApplicationDetail.vue`
  - 持久化数据下载、上传覆盖并重启、无损迁移对话框和失败提示已接入英文与简体中文词条；下载/上传内容为 zip 文件，文件内容保留原始数据。
- `web/src/views/containerization/`、`internal/containerization`
  - 容器化导航、容器/镜像/网络/卷页面、托管警告、镜像批量升级和资源删除文案已接入英文与简体中文词条；Docker Engine 和镜像仓库原始诊断保留原文。
  - 镜像“更多”菜单、全部更新确认、删除未使用镜像确认，以及卷删除未使用确认已接入英文与简体中文词条。
  - 容器、镜像和卷同步操作完成提示，以及 `container_refresh` / `volume_refresh` 刷新任务类型已接入英文与简体中文词条。

- `web/src/views/settings/_shared/SettingsPageContent.vue`、`internal/settings`
  - Runtime 日志等级设置的前端标签、选项文案和 `invalid_log_level` 后端错误码已接入英文、简体中文词条；进程日志内容保持英文，不纳入翻译。
- `internal/overview`、`web/src/views/overview/index.vue`
  - 概览卡片数据库保存失败文案及卡片配置校验错误码已接入英文、简体中文词条。
- `web/src/layouts/AppLayout.vue`
  - Header 的 DEV 构建通道标识、完整版本提示和移动端导航入口已接入英文、简体中文词条。
  - 已移除旧运行时节点导航入口，对应词条不再保留。
- `web/src/views/settings/_shared/SettingsPageContent.vue`
  - Token 过期时间、语言、登录页标题/说明、安全设置、证书设置和系统信息文案已接入 `web/src/i18n/index.ts`。
  - 旧运行时设置分类和字段已移除。
- `web/src/views/servers/_shared/ServersPageContent.vue`
  - 服务器凭据必选提示、Docker host 表单、Agent 正常/不兼容/异常/无法部署状态、版本检查时间、安装/重装入口和 agent 部署任务文案已接入 `web/src/i18n/index.ts`。
- `web/src/views/runtime/applications/`
  - 应用运行时实例状态、期望状态、日志入口、容器名和实例 ID 文案已接入 `web/src/i18n/index.ts`。
- `web/src/views/runtime/applications/ApplicationEditor.vue`
  - Command 和 args 数组行标签、提示已接入英文、简体中文词条。
- `web/src/views/tasks/index.vue`
  - 任务类型名称、类型筛选选项、搜索按钮、多选筛选占位、操作标题、步骤名称、任务阶段和日志面板任务类型已按稳定标识翻译。
  - ACME 账号、订单、授权、DNS 验证、DNS 清理和 finalize 任务阶段已接入英文与简体中文词条；ACME/Cloudflare 原始诊断仍保留原文。
  - 容器、镜像、卷、应用协调、Agent 证书重置和常见执行阶段已补齐英文与简体中文词条，避免任务中心显示原始枚举名。
- `web/src/views/certificates/key-assets/index.vue`
  - 自签证书/密钥拆分导航、系统内置与用户域标签、Agent 系统证书状态和重置确认，以及 CA、TLS、SSH、批量导入导出、冲突确认和引用提示已接入英文与简体中文词条。
- `internal/i18n`
  - 统一 API 错误翻译入口覆盖认证、设置、服务器、UFW、DNS、证书、应用、任务和密钥资产的主要错误码。
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

## 后端翻译仍为部分覆盖

当前后端已覆盖统一 API 错误翻译入口，但以下类别仍需继续补齐：

- Cloudflare / ACME / 镜像仓库相关错误码。
- SSH / 远程执行 / 超时相关错误码。
- 模板渲染、选择器解析等底层错误码。
- 任务 summary、任务 system 日志、任务过期清理写入的错误原因与远程命令诊断文本。
- 服务器删除写入任务的取消原因仍按任务错误文本展示，后续任务日志/原因统一翻译时一并处理。

## 更新规则

发生以下任一情况时，必须更新本文档：

- 新页面或新组件接入了多语言。
- 某个页面仍未翻译但继续被修改。
- 新增后端错误码或用户可见错误文本。
- 新增用户可见文案但暂未完成翻译。
