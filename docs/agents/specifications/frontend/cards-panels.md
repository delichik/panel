# 卡片与面板

## 普通卡片

统一使用 `v-card variant="outlined"`。全局样式提供：

- `8px` 圆角。
- `--lp-border` 边框。
- `--lp-surface` 背景。
- `--lp-shadow-sm` 阴影。
- 悬浮时轻微增强边框和阴影。

静态卡片如不应表现为可点击，可通过局部规则保持默认边框和阴影，不增加位移。

## 自定义面板

非 `v-card` 容器可使用 `.panel`：

```html
<section class="panel">
  <header class="panel-header">...</header>
  <div class="panel-body">...</div>
</section>
```

- `.panel-header`：左右分布，`20px 24px` 左右内边距。
- `.panel-body`：`24px` 内边距，可滚动，`min-height: 0`。
- 面板内部出现滚动时，外层必须限制可用高度。

## 标题区

以下全局类共享同一设计语言：

- `.list-header`
- `.panel-title`
- `.app-card-header`

标题区为 flex、两端分布、最小高度 `64px`，默认内边距约 `16px 18px 12px`。

标题区右侧可放：

- 数量 Chip。
- 新建或刷新按钮。
- 小型状态或操作集合。

`600px` 左右应改为纵向堆叠，按钮可占满宽度。

## 摘要卡

`.summary-card` 和 `.page-summary-card` 用于页面顶部指标：

- 横向排列图标、标签和数值。
- 内边距约 `14px 16px`。
- 最小宽度由父级 `180px` 网格控制。
- 只放一个主要数值和一条短说明。
- 图标背景使用 `surface-primary/success/warning/info/secondary/error`。

## 信息网格

`.info-grid` 用于详情中的标签值对：

- 自适应最小列宽 `180px`。
- 每项使用边框、弱表面背景和 `10px 12px` 内边距。
- 标签为 `12px` 弱文本。
- 值为约 `13px`，允许任意位置换行。

对于需要左右对齐的属性，也可使用两栏属性块，但窄屏必须转单栏。

## 详情与空详情

- 详情卡通常 `padding: 16px`。
- 详情头部允许标题、状态与操作并列，窄屏纵向排列。
- 未选择对象时使用 `.empty-detail`，不要渲染一张看似可操作的空表单。

## Glass 表面

`.glass-panel` 只用于确有背景层次的特殊展示：

- 使用透明表面和 `12px` blur。
- 不应作为默认卡片样式。
- 内容可读性优先，背景复杂时不得使用。

## 禁忌

- 不在卡片内再次嵌套同等阴影和边框的卡片，除非有明确层级。
- 不用大面积主题色填充普通内容卡片。
- 不把页面级错误塞进无关卡片标题。
- 不复制标题区样式创建仅命名不同的新类。

## 源码依据

- `web/src/styles/main.css`
- `web/src/features/servers/pages/ServersPage.vue`
- `web/src/features/applications/pages/ApplicationsPage.vue`
- `web/src/features/dns/pages/DomainsPage.vue`

