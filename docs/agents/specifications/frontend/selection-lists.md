# 选择列表与主从模式

## 共享选择器组件

`AppSelectorPanel.vue` 是所有主从页面左侧选择器的统一外壳，负责 outlined 满高卡片、标题、数量、紧凑加载态、空状态、内部滚动区和底部分页。

`AppSelectorItem.vue` 是统一选择行，默认渲染原生 `button`；存在行尾菜单等嵌套操作时使用 `as="div"`，组件会补充 option 角色、焦点和 Enter/Space 键盘选择行为。业务名称、元数据、状态、菜单或进度通过插槽提供。

服务器节点、服务器资源页、域名和任务中心操作列表必须组合这两个组件，不得各自复制面板或选择行 CSS。

## 共享服务器选择器

`ServerSelector.vue` 是基于 `AppSelectorPanel` 和 `AppSelectorItem` 的服务器业务适配层，用于从服务器集合中选择一个目标，当前复用于防火墙、软件包和容器资源页面。

### Props 与事件

| 名称 | 类型 | 说明 |
| --- | --- | --- |
| `modelValue` | `string` | 当前服务器 ID |
| `servers` | `ServerDto[]` | 可选服务器 |
| `loading` | `boolean` | 加载状态 |
| `update:modelValue` | event | 选择变化 |

组件内部使用 `AppSelectorPanel`、`AppSelectorItem` 和 `usePagination`。

## 服务器选择器结构

- 外层为可填满高度的 outlined 卡片。
- 标题区使用弱表面背景，显示标题和总数 Chip。
- 列表项为原生 `button`，保证键盘和语义正确。
- 每项显示状态点、服务器名称和一分钟负载。
- 选中项使用淡主色背景和主色边框。
- 空数据时显示服务器离线图标和说明。

## 加载态约束

- 选择器首次加载由 `AppSelectorPanel` 使用 `PageLoadingState` 的 `compact` 模式展示。
- 紧凑模式 spinner 为 `32px`，骨架宽度按选择器容器的 `100%` 计算，不得使用 `vw`，避免超出约 `300px` 到 `340px` 的左栏。
- 已有选择项刷新时保留列表，并使用卡片顶部 loading 状态。

## 通用选择行

服务器页和域名页形成统一选择行设计：

- 整行可点击。
- 两列布局：`minmax(0, 1fr) auto`。
- 内边距约 `11px 12px`。
- `8px` 圆角。
- 默认边框透明。
- 悬浮使用极弱 surface 背景。
- 选中使用 `rgba(primary, 0.06)` 背景和约 `0.26` 透明度主色边框。
- 主文本约 `0.9rem`、高字重。
- 元数据约 `0.76rem`、弱文本色。

## 主从工作区

- 左侧为选择列表，右侧为详情。
- 桌面左栏约 `300px` 到 `340px`。
- 中大屏下工作区填满页面剩余高度，工作区和左右栏都设置 `min-height: 0`、隐藏外层溢出。
- 左侧列表体和右侧详情正文分别独立滚动，标题、操作区和分页保持可见。
- `1080px` 以下折叠为单列。
- 未选择时右侧使用 `.empty-detail`。
- 切换选择不应重置与当前对象无关的页面级筛选。

## 列表滚动

- 长列表可在卡片体内部滚动。
- 桌面 sticky 列表必须在单列断点取消 sticky。
- 分页位于卡片底部，不随列表体滚动。
- 滚动容器必须有明确高度来源，避免无效的 `overflow-y: auto`。
- 列表体使用 grid 或 flex 排列行时，行必须按内容高度排列，不能被 stretch 撑满剩余高度。
- 当列表体同时具备 `flex: 1` / `min-height: 0` / `overflow: auto` 和 `display: grid` 时，必须显式设置 `align-content: start`，必要时加 `grid-auto-rows: max-content`。
- 单条数据、少量数据和空状态都必须看起来像正常列表；不能让一条选择行占满整块列表面板。
- 中大屏禁止通过增加整个工作区高度容纳长列表。
- 进入窄屏单列布局后可取消内部固定高度并恢复页面自然滚动。

## 状态表达

- 在线、可达使用 success。
- 不可达或需要注意使用 warning。
- 状态点必须伴随名称、说明或可访问上下文。
- 业务分类可用 Chip，但不要让每行出现过多颜色。

## 何时抽共享组件

同时满足以下条件时，应复用或扩展选择列表组件：

- 两个以上页面选择同一类实体。
- 行结构和状态语义相同。
- 分页、加载和空状态行为相同。

仅字段不同但交互结构相同，可考虑通过 slot 或轻量配置扩展；不要复制完整 CSS。

## 禁忌

- 不用普通 `div @click` 代替可交互按钮或列表项。
- 不让选中状态只通过极细边框表达。
- 不在每个页面复制服务器选择器。
- 不把大型业务详情塞进选择行。
- 不让左右任一栏的长内容推动整个页面滚动。

## 源码依据

- `web/src/components/AppSelectorPanel.vue`
- `web/src/components/AppSelectorItem.vue`
- `web/src/components/ServerSelector.vue`
- `web/src/views/servers/_shared/ServersPageContent.vue`
- `web/src/views/dns/domains/index.vue`
- `web/src/views/tasks/index.vue`
- `web/src/views/servers/firewall/index.vue`
- `web/src/views/servers/packages/index.vue`
