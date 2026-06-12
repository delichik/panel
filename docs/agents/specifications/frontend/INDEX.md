# 前端组件设计规范索引

本目录记录 Panel 前端中已经形成复用关系的组件与设计模式。规范来自 `web/src` 当前实现，用于新增页面、共享组件和跨页面交互时保持一致。

## 收录范围

- `web/src/components/` 下被多个页面使用的共享组件。
- Vuetify 基础组件在项目中的统一变体、颜色、密度和交互规则。
- 在多个 feature 页面重复出现、虽未抽成 Vue 组件但已经形成约定的组合模式。
- 全局主题、布局、响应式和无障碍约束。

## 不收录范围

- 仅服务单一页面或单一业务流程的大型组件。
- 概览页可自定义卡片、应用编辑器、应用详情等没有跨业务复用的组合实现。
- 纯业务字段、接口契约和数据处理逻辑。

这些实现可以作为某项基础模式的源码例证，但不为其单独建立组件规范。

## 规范索引

| 范围 | 文档 | 主要源码 |
| --- | --- | --- |
| 色彩、圆角、间距、排版、阴影、主题与动效 | [foundations.md](foundations.md) | `web/src/main.ts`、`web/src/theme.ts`、`web/src/styles/main.css` |
| 页面壳、工具栏、摘要区、双栏工作区与响应式 | [page-layout.md](page-layout.md) | `web/src/styles/main.css`、各 feature 页面 |
| 按钮、图标按钮与操作分级 | [buttons.md](buttons.md) | Vuetify `v-btn`、`web/src/styles/main.css` |
| 普通卡片、面板、标题区、摘要卡与信息网格 | [cards-panels.md](cards-panels.md) | Vuetify `v-card`、全局组合类 |
| 文本框、选择器、文本域、开关、复选框与表单布局 | [forms.md](forms.md) | Vuetify 表单组件、各对话框和设置页 |
| 数据表格、行操作与窄屏滚动 | [tables.md](tables.md) | Vuetify `v-table`、全局表格覆盖 |
| 普通、确认和大型编辑对话框 | [dialogs.md](dialogs.md) | 全局 `app-dialog-*` 类 |
| 应用框架、侧边导航、页头与菜单 | [navigation.md](navigation.md) | `web/src/layouts/AppLayout.vue` |
| 溢出菜单、选择菜单与 Tooltip | [menus-tooltips.md](menus-tooltips.md) | Vuetify `v-menu`、`v-tooltip` |
| Alert、Snackbar、Chip、状态色和进度 | [status-feedback.md](status-feedback.md) | Vuetify 反馈组件、任务页面 |
| 初次加载、局部刷新和空状态 | [loading-empty.md](loading-empty.md) | `PageLoadingState.vue`、全局空状态类 |
| 列表分页 | [pagination.md](pagination.md) | `AppPagination.vue`、`usePagination.ts` |
| 可选择列表、主从布局和服务器选择器 | [selection-lists.md](selection-lists.md) | `ServerSelector.vue`、服务器与域名页面 |
| 任务与运行日志 | [logs.md](logs.md) | `TaskLogPanel.vue`、应用日志面板 |

## 共享组件清单

| 组件 | 复用情况 | 对应规范 |
| --- | --- | --- |
| `AppPagination.vue` | 多个列表、表格和子面板 | [pagination.md](pagination.md) |
| `PageLoadingState.vue` | 多数页面的首次网络加载 | [loading-empty.md](loading-empty.md) |
| `ServerSelector.vue` | 防火墙、软件包页面 | [selection-lists.md](selection-lists.md) |
| `TaskLogPanel.vue` | 任务详情日志 | [logs.md](logs.md) |

## 使用要求

1. 新增组件前先查本索引，已有规范时沿用现有组件、全局类和 Vuetify 变体。
2. 新增或修改用户可见文案时遵守 `docs/agents/i18n-guide.md`。
3. 改变这里记录的用户可见行为时，同步更新对应规范文档。
4. 新增可跨页面复用的组件或模式时，新增或更新规范并维护本索引。
5. 业务页面可以扩展布局，但不得复制一套仅颜色、圆角或间距不同的基础组件。
