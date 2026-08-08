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
- `Dropdown`：菜单 Teleport 到 `body`，使用 fixed 定位、视口边界收敛和上下碰撞选择，不能留在业务容器内被 `overflow` 裁切；菜单宽度按内容收缩并保持紧凑，禁止 fixed 后按视口宽度拉伸；继续支持方向键、Home/End、Escape 和焦点恢复。
- `Dialog`：不响应遮罩点击关闭，只能通过关闭按钮、取消操作或 Escape 关闭；普通和 large 尺寸的 body 都必须有可靠的内部纵向滚动，页脚保持在弹窗网格底部；业务正文不能依赖外层页面滚动才能到达。
- `ToastProvider`：全局顶部 toast；页面和组件内 catch 到的异常统一以 danger toast 展示，字段校验与结构化诊断仍就地展示。
- `LoadingOverlay`：用于对话框正文、文本编辑区、卡片或区块等待网络响应时的统一加载覆盖；不得用裸文字代替加载效果。
- `Table`：用于表格型列表。首次加载且没有旧数据时传入 `loading` 与本地化 `loadingLabel`，由组件渲染表格骨架行；已有数据刷新时保留当前 rows，只让刷新入口或分页入口显示 loading。

## Pattern components

- `FilterBar`：用于列表页顶部筛选区，组合搜索、状态筛选和操作槽。业务页仍负责 query 同步、过滤条件命名和空态区分。
- `ServerContextSelector`：用于安全、资源、调试等单服务器上下文页面，是单一服务器选择器。组件只呈现服务器列表/卡片选择，不再叠加 Select 下拉；卡片负责展示 capability badge、状态和不可用原因；首次加载时传入 `loading` 显示服务器卡片骨架。
- `ServerMultiPicker`：用于应用部署、设施覆盖节点、批量任务等服务器多选。支持禁用 id 与禁用原因，选中态、禁用态和能力标签保持一致。
- `MasterList`：用于主从布局左侧列表容器，统一选中态、空态、加载骨架、滚动边界和 item slot。详情区标题与动作应由页面自己的 detail header 承载。
- `EditorSectionRail`：用于编辑器左侧分区导航。支持 complete、error、dirty badge 和二级 child item list；编辑器页面不得再自造不同视觉的分区 rail，badge 文案由页面 i18n 传入。
- `AssetFileManager`：用于应用文件和设施静态资产这两类同构的文件工作区。顶部只提供一个上传入口，弹窗内选择文本文件、普通文件或文件夹归档；文本类型显示编辑器，普通文件和归档显示文件选择控件。新建文本资产时下载文件名自动跟随引用名称，仍可单独修改，留空保存时使用引用名称；组件还统一提供文本编辑、替换、下载、删除、错误行展示和并发冲突重载；页面只通过 `AssetFileAdapter` 注入领域 API 和文案。`items[].key` 必须是应用内或设施内唯一的 `name`，不把物理 `id`、`fileKey` 或 `assetKey` 暴露给组件。

## 自动刷新模式

概览页折线图自动刷新在页头刷新按钮旁使用 `Switch` + 间隔 `Select` 的组合：开关控制是否启用，`Select` 提供 5s/10s 选项，标签、提示和 aria-label 全部走页面 i18n。轮询实现必须满足：防重入、标签页不可见时暂停、进入编辑态时暂停，保存成功后恢复并整卡重载；目标数据加载中跳过本轮；每次请求只带 `since`（上次数据点时间）增量拉取新点并追加到现有序列，同时按卡片时间范围滑动清理等量旧点，不整段重载。其他页面接入同类自动刷新时复用该组合，不另造控件。
## 使用规则

1. 页面内不得为同类控件重新手写尺寸、圆角、选中态、禁用态或 loading 样式；缺能力时先扩展上述组件。
2. 组件只承载通用交互与视觉，不直接绑定业务 API、路由或 i18n key。业务文案从页面传入，并按 i18n 规范维护；新增页面替换时不得依赖组件内英文默认值。
3. `StatusBadge` 的 tone map 是默认规则；业务状态显示文案仍由页面翻译或格式化后传入 `label`。
4. 上传、下载、确认对话框只负责交互入口；请求、错误摘要、任务入口和两阶段反馈由业务页面按照 [interaction-model.md](interaction-model.md) 实现。
5. 应用文件与设施静态资产如果具备相同的上传、替换、下载、删除和文本编辑行为，必须复用 `AssetFileManager`，不得在页面内复制另一套文件行和弹窗；领域差异只能放在 adapter/API 中。
6. 中大屏布局中，`MasterList`、`ServerContextSelector`、`EditorSectionRail`、`AssetFileManager` 必须放在模板提供的内部滚动区域，不得恢复页面级滚动。

## 当前接入记录

- `web/src/views/tasks/index.vue`：任务搜索、分页和任务状态已接入 `SearchInput`、`PaginationBar`、`StatusBadge`。
- `web/src/views/application-operations/index.vue` 与 `web/src/views/system-events/index.vue`：运行事件列表筛选、分页、状态、首次加载表格骨架和详情可用性提示已接入 `SearchInput`、`Select`、`Table`、`PaginationBar`、`StatusBadge`、`Tooltip` 与 `Dialog`。
- `web/src/views/applications/index.vue`：应用搜索、状态、应用/设施连续纵向配置流、部署/网关/源站服务器多选、持久化数据下载/恢复，以及应用文件和设施静态资产共用的 `AssetFileManager` 已接入统一 primitives/patterns。 未保存修改的离开/取消保护已接入 `ConfirmDialog`。
- `web/src/views/security/index.vue` 与 `web/src/views/resources/index.vue`：服务器上下文选择已接入 `ServerContextSelector` 及其加载骨架，页面不得再在同一上下文区域叠加 Select 下拉。
