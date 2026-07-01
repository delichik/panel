# 多语言翻译状态

本文档记录当前多语言实现仍未完成翻译的区域。后续每次处理多语言相关任务时，都应同步更新。

## 最近已补齐

- `web/src/views/applications/apps/ApplicationEditor.vue`
  - 应用编辑器主按钮从“保存并部署”调整为“保存并应用 / Save and apply”，已接入英文与简体中文词条；保存启用应用只提交保存会话，由保存接口触发协调。
- `web/src/views/applications/apps/ApplicationEditor.vue`、`internal/modules/applications/spec`、`internal/platform/i18n`
  - 应用挂载高级权限项新增默认收起的“挂载选项”、Docker 只读挂载和应用文件可执行开关，已接入英文与简体中文词条；后端新增 file/panel_file/persistent 挂载属主和 file/persistent mode 校验文案的简体中文翻译。
- `web/src/views/applications/apps/index.vue`、`web/src/views/applications/facility-apps/index.vue`、`internal/platform/i18n`
  - 应用控制平面重构后的“同步 / 停用 / 立即同步 / 删除后由同步清理运行时资源”文案已接入英文与简体中文词条；新增后端错误码 `application_reconciler_unavailable` 已接入简体中文翻译。
- `web/src/views/tasks/index.vue`
  - 任务中心新增应用协调与目标任务展示名：`application_target_batch`、`application_target_apply`、`application_target_stop`、`application_target_purge` 已接入英文与简体中文词条。
- `web/src/views/applications/facility-apps/index.vue`
  - 入口网关应用路由区域新增“打开应用”入口，英文与简体中文词条已接入 `web/src/i18n/index.ts`。
- `web/src/views/applications/apps/ApplicationEditor.vue`
  - 应用编辑器新增 Docker capability (`capAdd`) 可视化输入，字段标签、提示和添加按钮已接入英文与简体中文词条；保存值为 Docker capability 稳定标识，不翻译。
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
