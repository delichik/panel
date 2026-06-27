# 分页

## 共享组件

所有用户可见数据列表优先使用 `AppPagination.vue`。

### Props

| 属性 | 类型 | 默认值 |
| --- | --- | --- |
| `page` | `number` | 必填 |
| `pageSize` | `number` | 必填 |
| `total` | `number` | 必填 |
| `pageSizes` | `number[]` | `[10, 20, 50, 100]` |
| `compact` | `boolean` | `false` |

### Events

- `update:page`
- `update:pageSize`

修改 page size 时组件会自动将 page 重置为 `1`。

## 显示规则

- `total <= 0` 时不渲染。
- 左侧显示本地化总数。
- 中间为页大小选择器，宽度约 `118px`。
- 右侧为 Vuetify `v-pagination`。
- `compact` 模式只显示页码和翻页按钮，不显示总数和页大小选择器，并最多显示 `5` 个分页项；左侧选择器等窄容器必须使用该模式。
- 小屏最多显示 `5` 个分页项，其他屏幕最多 `10` 个。
- 当前页通过 `v-pagination` 的 `item` 插槽渲染为透明背景页码按钮，文字使用全局正文色，active 只增加主色描边；不得使用 Vuetify active 填充色块或手写明暗模式特例。

## 布局

- 分页放在列表或表格卡片底部。
- 中大屏满高列表或表格卡片必须使用纵向 flex 或等价 grid：标题/工具栏固定高度，列表或表格体 `flex: 1 1 auto; min-height: 0; overflow: auto`，分页 `flex: 0 0 auto` 留在底部。
- 不能让分页紧跟少量数据停在卡片中间；即使当前页只有一两条数据，分页也应贴住卡片底边，空白留在可滚动内容区内。
- 顶部有 `--lp-border` 分隔线。
- 背景使用弱化的 `--lp-surface-container`。
- 桌面横向靠右，总数通过 `margin-right: auto` 靠左。
- `760px` 以下改为纵向，页大小选择器填满宽度。
- 窄屏恢复自然高度时，内容体可取消纵向 flex 填充，分页随内容自然排列。

## 前端分页

数组型接口使用 `usePagination.ts`。

- 输入数据变化时保持页码合法。
- 页面只消费 `pageItems`。
- 总数来自原始数组长度。
- 搜索或筛选变化后应回到第一页。

## 服务端分页

- 保留接口返回的 `total`。
- `update:page` 和 `update:pageSize` 触发重新请求。
- page size 变化后从第一页请求。
- 请求期间保留现有内容并使用局部 loading。

## 嵌套区域

对话框或详情子表可继续使用 `AppPagination`，但必须确保：

- 容器宽度足够。
- 移动端纵向布局不会遮挡表单操作。
- 分页归属清楚，不与页面主列表分页混淆。

## 禁忌

- 不直接在页面中拼装另一套总数、页大小和分页按钮。
- 不在 `total = 0` 时保留空分页条。
- 不让页大小变化后停留在越界页码。
- 不把前端数组分页伪装成服务端请求分页。
- 不在满高卡片里省略列表或表格体的 `flex: 1`，否则分页会漂在内容下方而不是卡片底部。

## 源码依据

- `web/src/components/AppPagination.vue`
- `web/src/composables/usePagination.ts`
- `web/src/views/runtime/applications/index.vue`
- `web/src/views/tasks/index.vue`
- `web/src/views/dns/domains/index.vue`
- `web/src/views/servers/packages/index.vue`
- `web/src/views/certificates/domains/index.vue`
