# v6 视觉 Token 规范（WinUI 3 / Fluent 2）

> **WinUI 3 官方规范是唯一视觉真源**；本文件、`layout-system.md`、`interaction-patterns.md` 与验收合同都是它的
> 项目落地说明。历史 P0/P2 配方与 tmp/winui3-contract v1 已废止；与 WinUI 冲突的旧条目以本文件为准。
> 真源：`web/src/styles/main.css` 的 CSS 变量 + `web/src/design/appearance.ts` 的服务器外观状态；
> `@theme inline` 只负责把 `--panel-*` / accent 变量桥接成 Tailwind utility。
> 视觉目标：Windows 11 / WinUI 3 观感——中性灰阶分层、Mica 半透明层、1px 半透明描边、4/8px 圆角、
> 32px 控件、短促的状态填充动效；不复制第三方品牌、壁纸、字体等受版权内容。

## 红线

1. 禁止引入 Naive UI / Vuetify 或它们的组件写法；业务页面必须使用 `web/src/components/ui/` 的自有 primitives。
2. 业务代码禁止裸色值、裸尺寸、裸阴影和新增视觉常量；颜色/圆角/阴影/动效一律经 `--panel-*` token
   或 Tailwind theme（`@theme inline`）。强调色只能来自 `--accent-*`。
3. 图标统一使用本地 vendored 的 Fluent System Icons 兼容层 `@/icons`（`web/src/icons/`，官方 24px regular，MIT）；
   业务/组件不得直接 import `@lucide/vue`。新增图标必须同时更新 `tmp/gen-fluent-icons.mjs` 白名单与
   `web/src/icons/README.md`，图标按钮必须有可访问名称或 Tooltip；纯装饰图标默认 `aria-hidden`。
4. 语义状态色（success/warning/danger/info/neutral）只表达状态，不得用强调色表达健康/错误，
   也不得用状态色冒充主操作。
5. 普通面板/卡片只有一层 1px 描边、无阴影；禁止 card-in-card；浮层（Dialog/菜单/Dropdown/Toast/Tooltip）
   才使用 `shadow-flyout` 与 `bg-flyout`/`bg-popover`（不透明或亚克力近似）。
6. 禁止 Material 水波纹、hover 上浮/translateY、按压缩放、弹性/循环装饰动画、Reveal 高光。

## 色彩与层级（WinUI 3 语义）

- 页面底：`--panel-bg`（浅 `#F3F3F3` / 深 `#202020`）。**页面根不再画不透明背景**：AppShell 主区用
  `--panel-layer`（`bg-layer`）透出 Mica，卡片用 `--panel-card`（`bg-card`）。
- Mica 底：`body::before` 固定层 = `--panel-mica-tint` + 用户背景（服务器下发的模糊缩略图 `--panel-mica-thumb`
  或 `--panel-mica-preset` 预设渐变）；`data-mica='off'` / `data-background='none'` 时回纯色。
  禁止对全屏内容使用 `backdrop-filter`；只有浮层可小面积亚克力。
- 内容层：`--panel-layer`；浮层表面：`--panel-flyout` / `--panel-popover`；控件填充：`--panel-control-fill`（`bg-control`）。
- 状态填充：`--panel-hover`（hover，浅 `rgba(0,0,0,.037)` / 深 `rgba(255,255,255,.06)`）、
  `--panel-pressed`（按压，浅 `.06` / 深 `.09`）。
- 文本：`--panel-text`（正文 89.5% / 100%）、`--panel-text-secondary`（60.6% / 78.6%）、
  `--panel-text-muted`（44.6% / 54.4%）。Tailwind：`text-foreground` / `text-secondary-foreground` / `text-muted-foreground`。
- 描边：`--panel-stroke`（`border-stroke`）、`--panel-card-stroke`（`border-card-stroke`）、
  `--panel-divider`（`border-divider`）、`--panel-border-strong`（仅需要更强分隔时）；输入/选择器边框使用
  `--panel-input-border`（`surface-control` 已消费），与 Checkbox 同一强度。
- 文本对比度：light `--panel-text-muted` 提升到 0.56（AA 4.5+）；dark accent 文本使用 45% seed + white；
  `tokensContrast.test.ts` 覆盖 muted/深色 accent on card/selected。
- forced-colors：全局 `:focus-visible` 使用 2px `CanvasText` outline；导航/选中项/表格行用 `Highlight` outline；
  不得只依赖 box-shadow ring 或自定义 accent 背景。`prefers-reduced-transparency` 下深色同样回退实色并禁用
  `backdrop-filter`。
- 强调色：`--accent-seed` 由 `[data-accent='blue|teal|green|gold|orange|red|pink|purple|gray']` 提供；
  派生 `--accent-fill-default/secondary/tertiary`、`--accent-text`、`--accent-selected-bg`（`bg-accent-selected`）、
  `--accent-focus`。`--panel-primary`/`--panel-primary-foreground`/`--panel-brand*` 是 accent 的别名，组件不要直写 seed。
- 加载：不确定进度统一用 `web/src/components/ui/ProgressRing.vue`（`winui-progress-ring`，短弧旋转 + 轨道，
  `prefers-reduced-motion` 下静止）；LoadingOverlay / 按钮 loading / 顶栏语言保存都用它。禁止裸 spinner 图标。
- 焦点：`--panel-ring` = 前景色（WinUI 2px 实色焦点框），禁止再用 accent ring 充当焦点态。

## 字体与尺寸

- 字体：`"Segoe UI Variable Text", "Segoe UI Variable Display", "Segoe UI", system-ui, "PingFang SC", "Microsoft YaHei", Arial, sans-serif`
  （非 Windows 走系统回退；不打包受版权字体）。
- **WinUI 3 字号阶梯**：Caption 12/16、Body 14/20、Body Strong 14/20 semibold、Subtitle 20/28、Title 28/36、
  TitleLarge 40/52、Display 68/92。前端对应 `text-caption` / `text-body` / `text-subtitle` / `text-title` /
  `text-title-large` / `text-display`（`text-xs|sm|xl` 为 12/14/20 的旧别名，新代码优先用语义名）。
  `PageHeader h1` 用 Title(28)、Dialog h2 用 Subtitle(20)、分区标题用 Body Strong(14)、摘要数字用 Title(28)；
  层级靠字重与颜色表达，不得自造 16/18/24/30px 等阶梯外字号。
- **WinUI 3 间距**：4px 基线，布局/卡片/表单只允许 0/4/8/12/16/20/24 等步进（`p-1`…`p-6`、`gap-2`、`gap-3`）；
  2px 仅允许用于图标与文本的光学对齐；页面留白 16、卡片内边距 16（紧凑卡 12）、控件横向内边距 12、
  分区间距 12、字段间距 8。
- **WinUI 3 最小尺寸**：控件高 28/32/40（`h-control-sm|h-control|h-control-lg`），触控目标 ≥40
  （`min-h-touch`），文本/选择器最小宽 64（`min-w-field`），ContentDialog min 320 / max 548（默认）
  （`min-w-dialog`），页面最小宽 320（body `min-width`），侧栏展开 320（`--panel-nav-width`）/ compact 48。
  除图标按钮等显式紧凑控件外，不得低于上述值。
- 字号/断点：`--text-body-strong` 自带 600；内容栏断点使用项目 token `--breakpoint-wide:1239px` / `--breakpoint-wider:1599px`（`wide:` / `wider:`），外壳断点仍为 1024；不得用 `xl/2xl`（1280/1536）做内容栅格。
- 控件高度：md 32px（`h-control`）、sm 28px（`h-control-sm`）、lg 40px（`h-control-lg`）。
- 表格：WinUI ListView 语义——行高 40px（`h-10`），表头 Body Strong 14，数据单元格默认单行
  （`whitespace-nowrap` + `align-middle`），长文本由页面插槽 `truncate`/列宽处理，不得靠换行改变行高；
  分页区只留间距，不画分隔线。

## 圆角与阴影

- 圆角以 WinUI 3 两档为准：控件 4px（`rounded-control` / `surface-control`，Button/Input/Select/Tabs）；
  表面 8px（`rounded-overlay` / `surface-card` / `surface-list` / `surface-flyout`，卡片/表格/列表表面/Dialog/Flyout；`Table framed` 使用 `surface-card`）。
  Badge/InfoBadge 为胶囊（`rounded-pill`）。不得让控件使用表面圆角或反之。
- 普通表面只用 1px 描边，不加阴影；浮层用 `shadow-flyout`（`0 8px 24px rgba(0,0,0,.12)` / 深 `.4`）。
  禁止业务层 `shadow-sm/md/xl/2xl`。

## 表面层级

- `--panel-bg` 只用于 `body` 纯色 fallback 与 Mica 关闭时的页面底，不再用于卡片或卡片内部。
- 页面根（AppShell 主区）用 `--panel-layer` 或透明，透出 Mica；**同一视觉区域内只允许一层卡片**。
- 卡片/表格外框 `--panel-card` + `border-card-stroke`；卡片内嵌分组用 `--panel-muted` 井道 + 分隔线，
  不得再叠带边框卡片；粘性表头用不透明 `--panel-card`（避免滚动透出）。
- 输入类控件用 `--panel-control-fill` + `--panel-input-border`；浮层内选项 hover 用 `--panel-hover`。

## Motion（WinUI 3）

- 统一 token：`--panel-motion-duration-fade` 83ms、`fast` 167ms、`normal` 250ms、`slow` 333ms；
  曲线 `ease-enter` `cubic-bezier(0,0,0,1)`、`ease-exit` `cubic-bezier(1,0,1,1)`、
  `ease-move` `cubic-bezier(0.55,0.55,0,1)`、`ease-linear`。
- 控件：hover 只变填充 167ms；press 83ms；**无位移、无缩放**。Tooltip 悬停约 500ms 后 83ms 淡入。
- 浮层：Dialog/菜单/Dropdown 进场淡入 + `scale(0.98→1)` 250ms，退场 167ms 淡出；Toast 是顶边瞬态条（InfoBar 语义），保留 8px 垂直滑入 + 淡入 250ms。Dialog 退场使用 `ease-exit` 167ms。
- 控件变体（primary/danger/选中项等）允许使用显式 transition utility 而非 `motion-control`：`motion-*` 的中性 hover/pressed 填充会覆盖变体语义；或用局部 `--panel-hover`/`--panel-pressed` 覆盖。
- 路由：`route-*` 为 Page refresh（旧页 167ms 淡出，新页 8px 上滑 + 淡入 333ms）；覆盖式详情返回/关闭用 `detail-swap-leave-*` 向右 16px 淡出 167ms，避免瞬间消失。
  覆盖式详情：`detail-swap-*` 用 Drill 观感（右侧 24px 滑入 + 淡入 250ms）。
- Tab 选中指示：TabView header 只保留**一个共享指示器**，选中变化时用 `transition-indicator`（left/width 250ms
  point-to-point）滑动，不得每条 tab 各自静态画 underline；表格/列表仍只变色不位移。
- 弹层/浮层：Teleport 浮层必须先完成 `position: fixed` 定位再移动焦点，并使用 `focusWithoutScroll()`
  （`web/src/utils/focus.ts`）聚焦；禁止打开下拉/菜单/选择器时让文档滚动或把外壳顶起。
- 设施页等设置型页面：卡片网格用 `motion-stagger` + `motion-reveal` 做首屏交错入场，刷新数据不重播。
- 入场动画用 `animation-fill-mode: backwards`；`prefers-reduced-motion` 下位移/缩放归零（≤83ms 淡出）。
- `prefers-reduced-transparency` / `forced-colors` 下关闭 Mica 与半透明（见 main.css 末段）。
- 触屏：自有 `motion-*` 的 hover 反馈包在 `@media (hover: hover)`；交互控件 `touch-action: manipulation`。

## 共享 utility 层（强制）

组件与页面不再各自拼裸尺寸/字体/圆角/阴影/过渡；以下语义 utility 定义在 `web/src/styles/main.css`，
由 `web/src/styles/tokenGuard.test.ts` 守卫（出现任意 `text-[Npx]`、`w/h/min-*/max-*-[Npx|Nrem]`、
`rounded-[…]`、`shadow-[…]`、`duration-[var(...)]`、`transition-[…]`、`bg-[var(...)]` 等即失败；
`calc()/min()/max()/dvh/vw/vh/var()` 这类结构性视口表达式允许）：

| 分组 | utility |
| --- | --- |
| 表面 | `surface-card`（8px + card-stroke + card 底）、`surface-panel`（8px + card-stroke）、`surface-control`（4px + stroke + control 底）、`surface-control-line`（4px + stroke）、`surface-flyout`（浮层表面 + shadow-flyout） |
| 尺寸/间距 | `h-shell-header`（52px）、`w-indicator`（3px）、`max-h-list`（60dvh）、`top-17`/`w-73`/`max-w-72` 等 Tailwind 间距刻度（不再写任意 px/rem） |
| 动效 | `duration-fade/fast/normal`、`ease-enter/exit/move`、`transition-fill/field/state/control/width/transform-fill` |
| 焦点 | `focus-accent-underline`（2px 底部 accent 线）、`focus-danger-underline` |
| 网格 | `grid-rows-page/page-footer/page-top/shell/shell-top/page-bottom/1-safe`、`grid-cols-field/safe/2-safe/main-aside/master/detail/detail-wide/settings/toolbar/shell` |
| 其他 | `rounded-micro`（1px）、`rounded-inherit`、`z-dialog`（60）、`z-skip`（70）、`backdrop-blur-flyout`（20px）、`backdrop-blur-subtle`（2px） |
| 色桥 | `bg-accent-strong` / `bg-accent-stronger`（accent hover/pressed 填充） |

新增可复用样式时先落到本层（或既有 token），再由业务/组件引用；禁止在业务代码直接写裸 CSS 值或临时 utility。

## 断点与滚动

唯一布局断点 1024px：

- `>=1024px`：AppShell 填满视口，页面级禁止滚动；滚动限制在列表、表格、详情、日志、编辑正文等内部区域。
- `<1024px`：导航转抽屉，允许页面级滚动，双栏折叠为单栏。

## 主题运行时（服务器持久化）

- 选项：外观 `system|light|dark`；Mica 开/关；强调色 9 预设；背景 纯色/4 预设/上传图片（服务器存储）。
- 存储：服务器 `appearance_settings` + `<dataRoot>/appearance/`（背景图），localStorage
  `panel.appearance.cache` 仅作首屏缓存；服务器是唯一真源。
- 运行时：`html[data-theme]`、`html[data-background]`、`html[data-mica]`、`html[data-accent]`、
  `html[data-preset]`（预设背景 id）、`--panel-mica-thumb`（图片缩略图 URL）。
- 旧 `lighthouse/ocean` 方案与 `data-scheme` 已删除；旧本地偏好由 `design/appearance.ts` 在首次登录时
  一次性迁移（lighthouse→teal、ocean→blue），服务器 `initialized=true` 后不再迁移。
- 首帧：`web/index.html` 内联脚本读缓存预置 data-* 与 `--panel-mica-thumb`，随后 `App.vue` 调用
  `bootstrapAppearance()`：已登录 GET `/settings/appearance`，未登录 GET `/settings/public-appearance`
  （仅主题/强调色/模糊缩略图，不含原图）。
