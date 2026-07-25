# 统一交互组件模式

> 本规范记录跨页面复用的交互 primitives 和 patterns。业务页面遇到同类问题时必须优先复用这些组件，避免在页面内手写视觉相近但行为不同的控件。

## UI primitives

- `SearchInput`：用于列表、资源、任务、证书等搜索输入。支持 `v-model`、`placeholder`、可选清空按钮和禁用态；启用清空按钮时必须传入本地化 `clearLabel`。搜索状态仍由页面同步到 URL query。
- `PaginationBar`：用于长列表分页底栏。支持 `page`、`pageSize`、`total`、`v-model:page`、`previous`、`next`，固定放在列表模板底部，不随表格正文滚动；上一页、下一页和摘要文案由页面 i18n 传入。
- `ConfirmDialog`：用于删除、停用、重置、覆盖导入等需要用户决策的操作。必须传入影响说明 `impact`；危险操作使用 `tone="danger"`，高风险操作可启用 `requireCheckbox`；确认、取消、勾选确认文案由页面 i18n 传入。
- `FileUploadButton`：用于单文件或多文件选择，页面负责执行上传。禁止在业务页裸露不同样式的 `<input type="file">`。
- `DownloadButton`：用于 blob、归档、证书、密钥等下载动作，页面负责调用 API 并处理 `saveBlobDownload`。
- `StatusBadge`：集中维护状态到 tone 的映射。支持 `generic`、`server`、`task`、`certificate`、`resource`、`operation` domain；页面只有在确有业务差异时才传入显式 `tone`。
- `Select`：用于单选下拉。交付形态必须是 Panel 自有 combobox + listbox 浮层，使用 popover 表面、统一 hover/selected/focus/motion 状态和暗色主题 token；不得把浏览器原生 `<option>` 展开菜单作为用户可见交互形态。

## Pattern components

- `FilterBar`：用于列表页顶部筛选区，组合搜索、状态筛选和操作槽。业务页仍负责 query 同步、过滤条件命名和空态区分。
- `ServerContextSelector`：用于安全、资源、调试等单服务器上下文页面，是单一服务器选择器。组件只呈现服务器列表/卡片选择，不再叠加 Select 下拉；卡片负责展示 capability badge、状态和不可用原因。
- `ServerMultiPicker`：用于应用部署、设施覆盖节点、批量任务等服务器多选。支持禁用 id 与禁用原因，选中态、禁用态和能力标签保持一致。
- `MasterList`：用于主从布局左侧列表容器，统一选中态、空态、滚动边界和 item slot。详情区标题与动作应由页面自己的 detail header 承载。
- `EditorSectionRail`：用于编辑器左侧分区导航。支持 complete、error、dirty badge 和二级 child item list；编辑器页面不得再自造不同视觉的分区 rail，badge 文案由页面 i18n 传入。

## 使用规则

1. 页面内不得为同类控件重新手写尺寸、圆角、选中态、禁用态或 loading 样式；缺能力时先扩展上述组件。
2. 组件只承载通用交互与视觉，不直接绑定业务 API、路由或 i18n key。业务文案从页面传入，并按 i18n 规范维护；新增页面替换时不得依赖组件内英文默认值。
3. `StatusBadge` 的 tone map 是默认规则；业务状态显示文案仍由页面翻译或格式化后传入 `label`。
4. 上传、下载、确认对话框只负责交互入口；请求、错误摘要、任务入口和两阶段反馈由业务页面按照 [interaction-model.md](interaction-model.md) 实现。
5. 中大屏布局中，`MasterList`、`ServerContextSelector`、`EditorSectionRail` 必须放在模板提供的内部滚动区域，不得恢复页面级滚动。

## 当前接入记录

- `web/src/views/tasks/index.vue`：任务搜索、分页和任务状态已接入 `SearchInput`、`PaginationBar`、`StatusBadge`。
- `web/src/views/application-operations/index.vue` 与 `web/src/views/system-events/index.vue`：运行事件列表筛选、分页、状态和详情可用性提示已接入 `SearchInput`、`Select`、`PaginationBar`、`StatusBadge`、`Tooltip` 与 `Dialog`。
- `web/src/views/applications/index.vue`：应用搜索、状态、应用/设施编辑分区导航、部署/网关/源站服务器多选、持久化数据下载/恢复、应用归档上传和设施静态资产上传已接入统一 primitives/patterns。
- `web/src/views/security/index.vue` 与 `web/src/views/resources/index.vue`：服务器上下文选择已接入 `ServerContextSelector`，页面不得再在同一上下文区域叠加 Select 下拉。
