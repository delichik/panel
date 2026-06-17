# 页面布局

## 页面壳

所有登录后功能页的根容器优先使用 `.page-shell`：

```html
<div class="page-shell">
  <!-- alerts, toolbar, summary, workspace -->
</div>
```

`.page-shell` 使用纵向 flex 布局、统一间距和 `min-width: 0`。页面级 Alert、toolbar、actions、summary 必须保持内容高度，不得吸收剩余视口空间。

`.page-shell` 及其直接子项必须使用 `width: 100%`、`min-width: 0` 和 `max-width: 100%` 形成明确的宽度包含块。页面内卡片也必须允许收缩，宽表和长内容不得反向撑宽页面。

## 视口占满与滚动边界

中大尺寸屏幕维持桌面或多栏布局时：

- 页面最外围容器填满全局页头以下的剩余视口。
- 只有主工作区使用 `flex: 1 1 auto` 或 `minmax(0, 1fr)` 吸收剩余高度。
- 页面壳、主工作区及其直接网格子项设置 `min-height: 0`。
- 页面壳使用 `overflow: hidden`，禁止 `v-main` 或整个页面纵向滚动。
- 纵向滚动只发生在列表体、表格包装器、详情正文、卡片正文和日志框等明确的内部内容区。
- 内部滚动区必须有来自父级的有效高度约束，并显式设置 `overflow: auto`。
- 内部滚动区如果使用 CSS grid 排列列表行或内容章节，必须显式让内容从顶部按自身高度排列，例如 `align-content: start` 和按需使用 `grid-auto-rows: max-content`；不能让少量子项被拉伸平分剩余高度。

页面顶部的 Alert、提示横幅、工具栏、摘要区和分页不得使用 `flex: 1`，也不得被等分到剩余高度中。满高详情卡或面板内部的局部 Alert、固定头部和操作区同样不得吸收剩余高度。

满高列表和表格卡片的底部分页或固定操作区必须贴住卡片底部。实现时不要让表格按内容高度自然结束后直接接分页；应让中间内容体吸收剩余高度并内部滚动，把空白留在内容体内。

## 窄屏降级

`760px` 以下进入移动布局：

- `v-main` 恢复页面级纵向滚动。
- `.page-shell` 恢复自然高度和可见溢出。
- 工作区、列表、卡片和详情面板解除桌面端的剩余高度分配。
- 卡片和列表的纵向 `overflow` 恢复可见，避免内容被压缩在狭窄的内部滚动框中。
- 表格可以保留横向滚动，但纵向高度由内容决定。
- 宽表的横向溢出必须限制在 `.v-table__wrapper`，不得让页面壳或 `v-main` 产生横向滚动。
- 页面操作按钮优先保持紧凑并允许换行；只有表单提交、危险确认等确实需要强调的操作才占满整行。

## 内容顺序

推荐顺序：

1. 页面级错误或操作提示。
2. 页面工具栏或关键操作。
3. 摘要指标。
4. 主内容卡片、列表或主从工作区。
5. 对话框和 Snackbar。

应用布局已经提供页面标题，功能页通常不重复渲染同级 `h1`。

## 工具栏

- `.page-toolbar`：左右分布的页面级工具栏。
- `.page-actions`：靠右的操作集合。
- `.page-toolbar-actions`：工具栏内部按钮集合。
- `.toolbar`：局部通用工具条。
- 按钮间距通常为 `8px` 至 `12px`，允许换行。
- 窄屏默认保持按钮内容宽度，避免每个按钮各占一行。

## 摘要区

使用 `.summary-strip` 或 `.page-summary-grid`：

- 默认 `repeat(auto-fit, minmax(180px, 1fr))`。
- 卡片使用 `.summary-card` 或 `.page-summary-card`。
- 数值使用 `.font-tabular`。
- 摘要只展示可快速扫描的指标，不承载复杂交互。
- 摘要区始终按内容高度展示，不参与页面剩余高度分配。

## 主从双栏

服务器、域名、防火墙和软件包页面使用：

```css
grid-template-columns: clamp(300px, 26vw, 340px) minmax(0, 1fr);
gap: 18px;
```

- 左栏用于选择或导航，宽度保持 `300px` 至 `340px`。
- 右栏使用 `minmax(0, 1fr)`。
- `1080px` 以下折叠为单列；在 `760px` 以上仍需让工作区内部滚动，不能把整页变为滚动容器。
- `760px` 以下单列恢复自然高度和页面级纵向滚动。

## 多栏信息区

- 自适应信息格：`repeat(auto-fit, minmax(180px, 1fr))`。
- 两栏表单或属性网格：`repeat(2, minmax(0, 1fr))`。
- 指标块可使用四栏，但在窄屏降为一栏或两栏。
- 所有网格子项设置或继承 `min-width: 0`。

## 应用主框架

- 主内容最大宽度为 `1640px`，水平居中。
- 页面内边距使用 `clamp(16px, 2vw, 28px)`，`980px` 以下为 `16px`。
- `body` 始终不滚动。
- 中大屏的 `v-main` 和页面壳不滚动，页面内容在内部区域独立滚动。
- 窄屏让 `v-main` 恢复纵向滚动。

## 断点约定

| 断点 | 用途 |
| --- | --- |
| `1080px` | 主从双栏转单栏 |
| `980px` | 应用页头与复杂工作区收窄 |
| `760px` | 恢复自然页面高度和页面级纵向滚动 |
| `640px/600px` | 卡片标题、对话框进一步收紧 |
| `560px` | 日志、进度与极窄屏操作布局 |

## 禁忌

- 不给主内容区设置固定像素宽度。
- 不使用 `1fr` 代替可能包含宽内容的 `minmax(0, 1fr)`。
- 不让 Alert、toolbar、summary 或分页吸收剩余高度。
- 不在窄屏保留桌面端的固定高度或纵向内部滚动。
- 不默认让窄屏页面级按钮全部占满一行。
- 不创建没有明确高度来源的内部滚动区。

## 源码依据

- `web/src/styles/main.css`
- `web/src/layouts/AppLayout.vue`
- `web/src/views/servers/_shared/ServersPageContent.vue`
- `web/src/views/dns/domains/index.vue`
- `web/src/views/servers/firewall/index.vue`
- `web/src/views/servers/packages/index.vue`
