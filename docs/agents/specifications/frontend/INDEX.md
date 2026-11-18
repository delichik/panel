# 前端组件设计规范索引

> **v4 基础设施于 2026-07-21 建立。** 前端保留 Vue 3 / Vite / Vue Router / Pinia / lucide / ECharts / YAML，移除 Naive UI，不恢复 Vuetify。新体系采用 Tailwind + Panel 自有 CSS token + 自有 UI primitives；复杂无样式交互可使用 headless 组件，但业务页面不得直接依赖旧 UI 框架。

## 规范索引

| 范围 | 文档 | 主要源码 |
| --- | --- | --- |
| 色彩、字体、圆角、阴影、间距、控件高度、断点、Mica 背景与 WinUI 3 动效 | [visual-tokens.md](visual-tokens.md) | `web/src/styles/main.css`、`web/src/design/appearance.ts`（服务器持久化外观；CSS 变量为真源） |
| AppShell、NavList、PageHeader、页面模板（TableDetailLayout/覆盖式详情为实体与操作/任务标准壳；日志为 ListPage+Dialog；设置页为单页 + 顶部 Tabs + SettingsSection/SettingsCard；MasterDetailLayout 仅遗留等）的使用边界与反例 | [layout-system.md](layout-system.md) | `web/src/components/shell/`、`web/src/components/templates/`、`web/src/components/patterns/SettingsSection.vue`、`SettingsCard.vue` |
| 操作分级、列表查询进 URL、编辑脏状态、异步两阶段与规划失败/系统核实中反馈、后台清理状态恢复、反馈分层、加载骨架屏、浮层与键盘焦点模型、覆盖式详情开合与焦点归还 | [interaction-model.md](interaction-model.md) | 页面族目录、`web/src/components/ui/`、`web/src/composables/useOverlayBehavior.ts` |
| 搜索、游标分页、只读事件时间线、Toast 操作深链、确认、上传下载、状态、服务器选择、主从列表、表格行操作与名称列打开覆盖式详情、连续配置流、应用文件与设施资产管理、指标图表、设置卡行、设置分区 500ms 自动保存与 AutoSaveStatus、详情分区/属性、列表命令栏、InfoBar、ProgressBar、ChartCard、NumberBox/RadioButton/ToggleButton/Expander/BreadcrumbBar | [interaction-patterns.md](interaction-patterns.md) | `web/src/components/ui/`、`web/src/components/patterns/`（含 `SettingsSection`、`SettingsCard`、`AutoSaveStatus`、`FormField`、`DetailSection`、`PropertyList`、`ListCommandBar`、`InfoBar`、`ProgressBar`、`ChartCard`）、`web/src/composables/useDebouncedAutoSave.ts` |

> **视觉真源**：WinUI 3 官方规范。`winui3-conformance.md` 是按 UX 维度组织的符合性检查表（页面几何、导航、主题/平台态、控件状态、动效、页面模式、验证证据）；`visual-tokens.md` / `layout-system.md` / `interaction-patterns.md` 是落地说明；历史 P0/P2 配方、tmp/winui3-contract v1 与冲突条目废止。
| 测试命令、单测组织、e2e/a11y 重建约定、Mock 体系 | [testing.md](testing.md) | `web/vitest.unit.config.ts`、`web/src/mocks/`、Taskfile |

## 使用要求

应用容器日志的实例选择与弹窗交互见 [interaction-patterns.md](interaction-patterns.md) 当前接入记录。

操作/任务/日志三分：意图页用覆盖式详情；日志页用 ListPage + Dialog、默认 warning/error、手动游标分页；不使用共享定时刷新或统计横幅。

设施逐节点诊断、失败运行日志和按需代理错误样本的反馈规则见 [interaction-model.md](interaction-model.md)。

1. 写页面先选模板：Dashboard / List / Editor / Settings / Workspace。实体工作台与操作/任务使用 `TableDetailLayout`；日志 `/logs` 使用 `ListPage` + Dialog。`MasterDetailLayout` 不得再接入新交付页。
2. 视觉一律经 token：业务代码禁止裸色值、裸尺寸和自造阴影。颜色、状态和控件层级来自 `web/src/styles/main.css` 中的 `--panel-*` / Tailwind theme 变量。
3. 组件一律经自有 primitives：Button、IconButton、Input、Textarea、Select、Dialog、Dropdown、Tabs、Badge、Table、Toast、Skeleton、EmptyState、Tooltip、Switch 位于 `web/src/components/ui/`。业务页面不得引入 Naive UI 或 Vuetify。
4. 页面族必须独立：overview、servers、credentials、applications、dns、certificates、operations、tasks、logs、settings、auth、maintenance、debug。资源/安全并入服务器详情；旧 activity 双视图不得恢复。
5. 新增或修改用户可见文案遵守 `docs/agents/i18n-guide.md`。
6. 新增可跨页面复用的组件或模式时，新增或更新规范并维护本索引。
