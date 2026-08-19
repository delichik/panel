# 多语言翻译状态

本文档记录多语言覆盖现状与已知未翻译项；规则与流程见 [i18n-guide.md](i18n-guide.md)。

## 当前状态

- 本轮设置页新增「协调追踪」开关：`settingsPage.reconcileTrace` / `settingsPage.reconcileTraceHint` 词条（en / zh-CN 已同步）。
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
