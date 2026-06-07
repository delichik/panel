# 开发流程

- 当执行开发前，需要先收集必要信息；如果需要收集的信息较多，应该使用 explorer subagent 执行搜索并总结关键要点
- 开发完成后执行静态检查和编译检查，可以编写单元测试验证结果，但不要使用 curl、browser 等工具强行检查
- 执行测试、编译请使用 `task build:*` 或 `task test:*`，例如编译网页使用 `task build:web`

# 多语言要求

- 所有新增或修改的用户可见文案，必须遵守 [docs/agents/i18n-guide.md](docs/agents/i18n-guide.md)
- 开始改动多语言相关代码前，先阅读：
  - [docs/agents/i18n-guide.md](docs/agents/i18n-guide.md)
  - [docs/agents/i18n-translation-status.md](docs/agents/i18n-translation-status.md)
- 当你新增了翻译、迁移了文案、或发现新的未翻译区域时，必须同步更新 [docs/agents/i18n-translation-status.md](docs/agents/i18n-translation-status.md)
- 禁止在前端页面、共享组件、路由元信息、后端错误响应中继续直接硬编码用户可见文案，除非同时完成该文案的多语言接入
- 任何会被持久化的展示配置，不要保存当前语言下的标题或文案，必须保存稳定 key / kind / value，并在渲染时翻译

# 注意事项

- 当前为 alpha 阶段，已经有地方在使用了，所以操作数据库时应该考虑版本之间的迁移问题
- 测试和调试产生的中间文件需要放在 `tmp` 下
- 禁止执行 git 命令，也不需要你检查待提交内容，除非得到明确指示
- 所有文件使用 UTF-8，使用 windows 命令请注意编码问题