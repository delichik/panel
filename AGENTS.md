# Agent 工作入口

本文档是仓库协作入口。开始任务时先读本文件，再按任务范围只加载需要的模块文档。

## 开发流程

- 开发前先收集必要信息；如果需要收集的信息较多，应该使用 explorer subagent 执行搜索并总结关键要点。
- 先通过 [docs/agents/modules/README.md](docs/agents/modules/README.md) 定位功能模块，再只阅读对应的小模块指引。
- 修改任何功能时，必须同步更新相关功能模块指引；新增功能模块时，必须新增对应指引并更新模块索引。
- 开发完成后按“检查和测试范围”执行必要验证，不要使用 curl、browser 等工具强行检查。
- 执行测试、编译请使用 `task build:*` 或 `task test:*`，例如编译网页使用 `task build:web`。

## 检查和测试范围

- 只在需要时执行测试、静态检查或编译检查；如果只是修改文档，不应该执行测试或编译。
- 只执行与改动范围相关的检查；如果只是修改前端，不应该执行后端测试。
- 只改后端时，优先使用 `task test:backend` 或 `task build:backend`。
- 只改前端时，优先使用 `task test:web` 或 `task build:web`。
- 同时改动前后端契约、构建链路或共享行为时，再按需要执行两侧检查或 `task test` / `task build`。
- 如果判断无需执行测试或编译，需要在最终回复中说明原因。

## 文档索引

- 功能模块索引：[docs/agents/modules/README.md](docs/agents/modules/README.md)
- 多语言指南：[docs/agents/i18n-guide.md](docs/agents/i18n-guide.md)
- 多语言状态：[docs/agents/i18n-translation-status.md](docs/agents/i18n-translation-status.md)

## 文档更新要求

发生以下任一情况时，必须更新对应模块文档和索引：

- 新增、删除或重命名功能、页面、API、后台任务、配置项、数据库表或重要字段。
- 修改跨模块调用关系、持久化结构、任务流程、调度行为、远程命令行为或权限要求。
- 调整用户可见行为，即使代码入口没有变化，也要在相关模块指引中记录。
- 发现模块文档与代码不一致时，应在同一改动中修正文档。

## 多语言要求

- 所有新增或修改的用户可见文案，必须遵守 [docs/agents/i18n-guide.md](docs/agents/i18n-guide.md)。
- 开始改动多语言相关代码前，先阅读：
  - [docs/agents/i18n-guide.md](docs/agents/i18n-guide.md)
  - [docs/agents/i18n-translation-status.md](docs/agents/i18n-translation-status.md)
- 当你新增了翻译、迁移了文案、或发现新的未翻译区域时，必须同步更新 [docs/agents/i18n-translation-status.md](docs/agents/i18n-translation-status.md)。
- 禁止在前端页面、共享组件、路由元信息、后端错误响应中继续直接硬编码用户可见文案，除非同时完成该文案的多语言接入。
- 任何会被持久化的展示配置，不要保存当前语言下的标题或文案，必须保存稳定 key / kind / value，并在渲染时翻译。

## 注意事项

- 当前为 alpha 阶段，已经有地方在使用了，所以操作数据库时应该考虑版本之间的迁移问题。
- 测试和调试产生的中间文件需要放在 `tmp` 下。
- 禁止执行 git 命令，也不需要检查待提交内容，除非得到明确指示。
- 所有文件使用 UTF-8，使用 Windows 命令请注意编码问题。
