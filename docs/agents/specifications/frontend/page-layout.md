# 页面布局

## 页面壳

所有登录后功能页的根容器优先使用 `.page-shell`：

```html
<div class="page-shell">
  <!-- alerts, toolbar, summary, content -->
</div>
```

`.page-shell` 使用网格布局、`--lp-space-5` 间距、`min-width: 0`，防止表格和长文本撑破主内容区。

## 视口占满与滚动边界

中大尺寸屏幕维持桌面或多栏布局时：

- 页面最外围容器必须填满全局页头以下的全部剩余视口。
- 页面壳、主工作区及其直接网格子项必须设置 `min-height: 0`。
- 页面最外围容器使用 `overflow: hidden`，禁止 `v-main` 或整个页面产生纵向滚动。
- 可增长的主内容轨道使用 `minmax(0, 1fr)`；固定工具栏、摘要区和分页不参与滚动。
- 纵向滚动只能发生在列表体、表格包装器、详情正文、卡片正文、日志框等明确的内部内容区。
- 内部滚动区必须有来自父级的有效高度约束，并显式设置 `overflow: auto` 或 `overflow-y: auto`。

推荐结构：

```css
.page-shell {
  height: 100%;
  min-height: 0;
  overflow: hidden;
  grid-template-rows: auto minmax(0, 1fr);
}

.page-workspace,
.page-workspace > * {
  min-height: 0;
}

.scroll-region {
  min-height: 0;
  overflow: auto;
}
```

进入窄屏单列或移动布局后，可以按页面内容恢复自然高度和页面级纵向滚动，避免多个狭窄嵌套滚动区。

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
- `760px` 以下操作区改为可拉伸布局，主要按钮通常占满宽度。

## 摘要区

使用 `.summary-strip` 或 `.page-summary-grid`：

- 默认 `repeat(auto-fit, minmax(180px, 1fr))`。
- 卡片使用 `.summary-card` 或 `.page-summary-card`。
- 数值使用 `.font-tabular`。
- 图标容器使用 `.summary-icon` 和语义表面类，如 `.surface-success`。
- 摘要只展示可快速扫描的指标，不承载复杂交互。

## 主从双栏

服务器、域名、防火墙和软件包页面形成统一模式：

```css
grid-template-columns: clamp(300px, 26vw, 340px) minmax(0, 1fr);
gap: 18px;
```

- 左栏用于选择或导航，宽度保持 `300px` 至 `340px`。
- 右栏必须使用 `minmax(0, 1fr)`，允许表格和详情正确收缩。
- `1080px` 以下折叠为单列。
- 左侧列表可在桌面端使用 sticky，但折叠后必须恢复普通流。

## 多栏信息区

- 自适应信息格：`repeat(auto-fit, minmax(180px, 1fr))`。
- 两栏表单或属性网格：`repeat(2, minmax(0, 1fr))`。
- 指标块可使用四栏，但在 `760px` 左右降为一栏或两栏。
- 所有网格子项必须设置或继承 `min-width: 0`。

## 应用主框架

- 主内容最大宽度为 `1640px`，水平居中。
- 页面内边距使用 `clamp(16px, 2vw, 28px)`。
- `980px` 以下收紧为 `16px`。
- `body` 始终不滚动。
- 中大屏的 `v-main` 和页面最外围容器不滚动，页面内容在内部区域独立滚动。
- 窄屏单列布局可让 `v-main` 恢复纵向滚动。

## 断点约定

| 断点 | 用途 |
| --- | --- |
| `1080px` | 主从双栏转单栏 |
| `980px` | 应用页头折行、复杂工作区收窄 |
| `760px` | 工具栏、表单重复行和操作区纵向堆叠 |
| `640px/600px` | 卡片标题、对话框、页头进一步收紧 |
| `560px` | 日志、进度与极窄屏操作布局 |

新页面应优先复用这些断点，不为相近宽度新增零散断点。

## 禁忌

- 不给主内容区设置固定像素宽度。
- 不使用 `1fr` 代替可能包含宽内容的 `minmax(0, 1fr)`。
- 不让页面级按钮在窄屏继续强制单行。
- 中大屏不允许 `v-main`、`.main-content` 或 `.page-shell` 成为整页纵向滚动容器。
- 不创建没有明确高度来源的内部滚动区。
- 不让固定标题、工具栏或分页随内部内容一起滚走。

## 源码依据

- `web/src/styles/main.css`
- `web/src/layouts/AppLayout.vue`
- `web/src/features/servers/pages/ServersPage.vue`
- `web/src/features/dns/pages/DomainsPage.vue`
- `web/src/features/firewall/pages/FirewallPage.vue`
- `web/src/features/packages/pages/PackageUpdatesPage.vue`
