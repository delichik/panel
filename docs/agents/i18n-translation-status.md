# 多语言翻译状态

本文档记录多语言覆盖现状与已知未翻译项；规则与流程见 [i18n-guide.md](i18n-guide.md)。

## 当前状态

- 本轮应用部署与统一日志修复新增 `applicationsPage.deployNoChange`、当前部署阶段/步骤/尝试/下次重试/错误、`failed_retryable` 状态，以及 `activity.observationRejected.*` 精确拒绝原因词条；en / zh-CN 已同步。观测拒绝文案明确说明不是部署重试，应用页直接展示结构化 Job 错误。
- 本轮应用端口映射新增 TCP/UDP 协议、Panel 防火墙管理开关、固定端口约束和规则状态摘要词条，en / zh-CN 已同步；后端补充端口协议、UFW 未安装、SSH/Agent 管理端口保护、双通道连通性门禁和受管规则冲突等稳定错误翻译。

- 统一日志新增容量 warning/blocked 横幅与可用空间词条，明确容量持续增长和停止接收新变更时仍可查询导出，en / zh-CN 已同步。
- 统一日志补充 Toast“查看过程”和人工核对执行结果的双语文案；人工核对明确标注人为判断并要求依据，不将其表述为系统自动验证。
- 统一日志页面新增 `activity.*` 与 `routes.activity.title`，en / zh-CN 同步；请求、生命周期及状态显示按稳定词条渲染，原始远端输出保留原文。旧日志保留天数设置退出界面。

- 本轮应用代理 Path 移除旧 `webSocket` 布尔兼容字段与重复开关，只保留 `applicationsPage.webSocketMode` 的 auto/on/off 选择器；删除 `applicationsPage.webSocket` 词条，en / zh-CN 已同步。
- 本轮应用环境变量编辑新增占位符目录与稳定应用容器引用词条（`applicationsPage.insertVariable`、`noInsertableVariables`、`variableReferenceHint`、`applicationContainerVariable`、`currentApplicationVariable`），en / zh-CN 已同步。
- 本轮应用挂载的“应用文件”来源改为当前编辑会话文件下拉选择，新增空文件提示、失效旧引用和必选校验词条（`applicationsPage.applicationFileMount*`）；“Seamark 文件”来源接入通用 `panelFiles` 目录下拉，并新增资源/文件类型、空目录、失效引用和必选校验词条（`applicationsPage.panelFile*`），en / zh-CN 已同步。
- 本轮服务器详情 Agent 卡片新增部署状态、阶段、日志空态/错误及 PrepareRestart 词条（`serversPage.agentTask*`），en / zh-CN 已同步；结构化的重启就绪、不透明 `holdon` 等待和超时日志会在前端转换为当前语言，不再把未携带原因的 `holdon` 推断为软件包升级；其他远端自由文本仍保持原样。
- 本轮存储共享详情页新增「导出健康/分区数据/设置」分段词条（`applicationsPage.storageShareTab*`、状态检查与分区字段、删除确认勾选等），en / zh-CN 已同步。
- 本轮移除应用编辑器的 YAML 源码编辑模式（表单 / YAML 源码切换、源码面板与往返同步），删除 `applicationsPage.editMode`、`configureMode`、`sourceMode`、`syncSource`、`applySource`、`sourceViewTitle`、`sourceViewHint`、`sourceGuardHint`、`yamlSynced`、`yamlApplied`、`yamlDirtySummary`、`specYaml`、`validationSpec`、`validationYaml`、`validationSourceStaged` 词条，en / zh-CN 已同步；`editorFlowHint` 文案已更新。
- 前端词条：en / zh-CN key 集合一致，由 `web/src/i18n/i18n.test.ts` 强制校验（含 en 不得残留中文值、词条非空）。本轮移除应用反向代理对话框中的只读“源服务器”摘要展示及对应 `applicationsPage.proxyOriginHint` 词条；`applicationsPage.originServers` 与 `proxyOriginEmptyHint` 保留（设施域名选择器、路由摘要与主备优先级仍在使用），en / zh-CN 已同步。本轮新增存储共享设施与共享存储挂载相关词条；应用代理规则新增源站优先级（`applicationsPage.originPriority`、`originPriorityHint`、`moveUp`、`moveDown`）词条，en / zh-CN 已同步。
- 本轮为共享编辑器 `CodeEditor` 新增查找/替换面板词条（`codeEditor.*`，en / zh-CN 已同步）；面板文案（查找、替换、下一个/上一个、全部、区分大小写、正则、整词、关闭及替换播报）随界面语言切换。
- 前端语言逻辑：`web/src/i18n/index.ts`（`state.locale` + `setLocale`）。
- 前端辅助函数：`translateRuntimeEventType`、`translateTaskSummary`、`translateEventSummary`；后两者用于把后端英文任务摘要 / 运行时事件摘要按当前语言渲染翻译，任务中心与系统事件页使用。
- 本轮新增 `api.*` 前缀错误文案 key，供前端错误展示使用。
- 后端错误码翻译（`internal/platform/i18n/i18n.go`）本轮补齐：
  - 大量缺失错误码的中文词条；
  - not-found 错误的精确翻译（英文 fallback 以 " not found" 结尾时也有兜底翻译）；
  - `remote_timeout` 翻译；
  - agent / ssh 相关错误的前缀翻译。
  - 现状：全部已知 panelerr 错误码均已覆盖。本轮补充 `storage_share_*` 系列错误码（配置校验、分区、挂载、Agent 要求等静态词条 + 带服务器/应用名的前缀词条）与 `range_invalid` 中文文案中的 `24h` 取值；SSH 主机密钥错误（`ssh_host_key_mismatch` / `ssh_host_key_verification_failed`）在执行器侧剥离 x/crypto 的 `ssh: handshake failed:` 包装后再翻译，前缀匹配可命中；已删除无发射点的 `application_reconcile_collector_only` 词条。
- 任务错误：任务 error 在写入前对 panelerr 错误做 i18n 翻译，避免把当前语言下的文案固化进任务记录。
- 本轮新增 `key_asset_system_managed` 后端错误码，已补充简体中文翻译；用于阻止普通密钥资产接口修改 Panel/Agent 系统托管资产。
- 本轮新增 Panel HTTPS 域名、证书选择及内置自签名证书的 en / zh-CN 词条。
- 前端摘要词典（`taskSummaryTranslations`）：新增 "Syncing storage share exports"、"Collecting initial server information"、"Refreshing volumes/networks"、"Volumes/Networks refreshed"、"Image updates refreshed"、"Syncing application <name>" 前缀与 "Running <type> batch" 前缀；删除已无发射点的 "Collecting scheduled metrics" / "Collecting metrics for " 词条。`translateEventSummary` 在事件词典未命中时回退到任务摘要词典，任务类事件摘要（续签证书、刷新软件包等）在 zh-CN 下不再显示英文。

## 已知剩余未翻译项

- 任务 / 事件中非 panelerr 的自由文本错误（例如远端命令返回的原始错误文本）。
- 应用操作 `stage.detail` 中的英文技术文本（如镜像名、容器名等）。
- Mock 数据中的文案（dev 专用，不走正式翻译链路）。

## 维护约定

- 新增错误码时必须同步补充中文词条。
- en / zh-CN 的 key 集合必须保持一致，新增词条时两侧同步。
- 摘要以稳定英文存储、前端按当前语言渲染翻译，不要把展示语言耦合进摘要写入逻辑。
- 修改用户可见文案后，按需同步更新本文档与 [i18n-guide.md](i18n-guide.md)。
