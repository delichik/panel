# 多语言翻译状态

本文档记录多语言覆盖现状与已知未翻译项；规则与流程见 [i18n-guide.md](i18n-guide.md)。

## 当前状态

- 前端词条：en / zh-CN key 集合一致，由 `web/src/i18n/i18n.test.ts` 强制校验（含 en 不得残留中文值、词条非空）。本轮新增存储共享设施与共享存储挂载相关词条。
- 前端语言逻辑：`web/src/i18n/index.ts`（`state.locale` + `setLocale`）。
- 前端辅助函数：`translateRuntimeEventType`、`translateTaskSummary`、`translateEventSummary`；后两者用于把后端英文任务摘要 / 运行时事件摘要按当前语言渲染翻译，任务中心与系统事件页使用。
- 本轮新增 `api.*` 前缀错误文案 key，供前端错误展示使用。
- 后端错误码翻译（`internal/platform/i18n/i18n.go`）本轮补齐：
  - 大量缺失错误码的中文词条；
  - not-found 错误的精确翻译（英文 fallback 以 " not found" 结尾时也有兜底翻译）；
  - `remote_timeout` 翻译；
  - agent / ssh 相关错误的前缀翻译。
  - 现状：全部已知 panelerr 错误码均已覆盖。
- 任务错误：任务 error 在写入前对 panelerr 错误做 i18n 翻译，避免把当前语言下的文案固化进任务记录。

## 已知剩余未翻译项

- 任务 / 事件中非 panelerr 的自由文本错误（例如远端命令返回的原始错误文本）。
- 应用操作 `stage.detail` 中的英文技术文本（如镜像名、容器名等）。
- Mock 数据中的文案（dev 专用，不走正式翻译链路）。

## 维护约定

- 新增错误码时必须同步补充中文词条。
- en / zh-CN 的 key 集合必须保持一致，新增词条时两侧同步。
- 摘要以稳定英文存储、前端按当前语言渲染翻译，不要把展示语言耦合进摘要写入逻辑。
- 修改用户可见文案后，按需同步更新本文档与 [i18n-guide.md](i18n-guide.md)。
