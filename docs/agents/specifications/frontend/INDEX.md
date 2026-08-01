# 前端组件设计规范索引

> **v4 基础设施于 2026-07-21 建立。** 前端保留 Vue 3 / Vite / Vue Router / Pinia / lucide / ECharts / YAML，移除 Naive UI，不恢复 Vuetify。新体系采用 Tailwind + Panel 自有 CSS token + 自有 UI primitives；复杂无样式交互可使用 headless 组件，但业务页面不得直接依赖旧 UI 框架。

## 规范索引

| 范围 | 文档 | 主要源码 |
| --- | --- | --- |
| 色彩、字体、圆角、阴影、间距、控件高度、断点与主题运行时 | [visual-tokens.md](visual-tokens.md) | `web/src/styles/main.css`、`web/src/design/theme.ts`、`web/src/design/tokens.ts` |
| AppShell、PageHeader、页面模板的使用边界与反例 | [layout-system.md](layout-system.md) | `web/src/components/shell/`、`web/src/components/templates/` |
| 操作分级、列表查询进 URL、编辑脏状态、异步两阶段、反馈分层、加载骨架屏、浮层与键盘焦点模型 | [interaction-model.md](interaction-model.md) | 页面族目录、`web/src/components/ui/` |
| 搜索、分页、确认、上传下载、状态、服务器选择、主从列表、连续配置流、应用文件与设施资产管理 | [interaction-patterns.md](interaction-patterns.md) | `web/src/components/ui/`、`web/src/components/patterns/` |
| 测试命令、单测组织、e2e/a11y 重建约定、Mock 体系 | [testing.md](testing.md) | `web/vitest.unit.config.ts`、`web/src/mocks/`、Taskfile |

## 使用要求

1. 写页面先选模板：Dashboard / List / MasterDetail / Editor / Settings / Workspace。一级对象选择工作台使用 `MasterDetailLayout` 统一双栏几何。阶段占位只允许在页面族接入前短期存在，交付页不得回退成通用 CollectionPage。
2. 视觉一律经 token：业务代码禁止裸色值、裸尺寸和自造阴影。颜色、状态和控件层级来自 `web/src/styles/main.css` 中的 `--panel-*` / Tailwind theme 变量。
3. 组件一律经自有 primitives：Button、IconButton、Input、Textarea、Select、Dialog、Dropdown、Tabs、Badge、Table、Toast、Skeleton、EmptyState、Tooltip、Switch 位于 `web/src/components/ui/`。业务页面不得引入 Naive UI 或 Vuetify。
4. 页面族必须独立：overview、servers、credentials、security、resources、applications、dns、certificates、application-operations、system-events、tasks、settings、auth、maintenance、debug 都有独立入口目录。可以复用模板和 primitives，但内容结构、操作闭环和 API 模块必须按业务任务设计。
5. 新增或修改用户可见文案遵守 `docs/agents/i18n-guide.md`。
6. 新增可跨页面复用的组件或模式时，新增或更新规范并维护本索引。
