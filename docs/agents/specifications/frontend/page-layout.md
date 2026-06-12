# 页面布局

## 页面壳

所有登录后功能页的根容器优先使用 `.page-shell`：

```html
<div class="page-shell">
  <!-- alerts, toolbar, summary, content -->
</div>
```

`.page-shell` 使用网格布局、`--lp-space-5` 间距、`min-width: 0`，防止表格和长文本撑破主内容区。

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
- 页面纵向滚动发生在 `v-main`，`body` 本身不滚动。

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
- 不在页面内部创建第二个不必要的全页纵向滚动容器。

## 源码依据

- `web/src/styles/main.css`
- `web/src/layouts/AppLayout.vue`
- `web/src/features/servers/pages/ServersPage.vue`
- `web/src/features/dns/pages/DomainsPage.vue`
- `web/src/features/firewall/pages/FirewallPage.vue`
- `web/src/features/packages/pages/PackageUpdatesPage.vue`

