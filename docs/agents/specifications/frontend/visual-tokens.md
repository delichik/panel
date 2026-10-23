# v4 视觉 Token 规范

> 当前落地处为 `web/src/styles/main.css` 与 `web/src/design/theme.ts`（CSS 变量为真源；文档中若仍出现 `tokens.ts` 以 CSS 为准）。视觉为克制高密度运维工作台：对比清晰、表面分层明确、交互反馈可感知；不复制第三方品牌、logo、文案或页面结构。

## 红线

1. 禁止在业务页面引入 Naive UI / Vuetify 或它们的组件写法。
2. 业务代码禁止裸色值、裸尺寸、裸阴影和新增视觉常量；颜色和状态经 `--panel-*` token 或 Tailwind theme（`@theme` 桥接）。
3. 图标统一用 `@lucide/vue`，图标按钮必须有可访问名称或 Tooltip。
4. 主操作使用方案主色 `Button variant="primary"`（颜色由 `--panel-primary` 决定：灯塔为青绿、蔚蓝为深蓝/青）；**primary/brand 与 success 必须色相可区分**，语义状态只用于 success / warning / danger / info，不用品牌色表达健康或错误。
5. 卡片/面板保持单层边框；不要叠卡片，不要用装饰性渐变或大色块制造视觉重量。浮层（Dialog / 菜单 / Toast / Tooltip）统一走 `.panel-shadow-overlay`。

## 色彩与层级

- 页面背景：`--panel-bg`（灯塔浅色为中性冷灰，避免暖纸底）
- 面板表面：`--panel-surface`
- 抬升表面：`--panel-surface-raised`（相对 surface 的浅一档抬升，用于需要轻微层次的内嵌区；业务页仍禁止叠卡片）
- 弱表面：`--panel-muted`（中性灰内嵌/弱表面；不带方案色调）
- hover 表面：`--panel-hover`（与 `--panel-muted` 同基调，加深一档）
- 边框：`--panel-border` / `--panel-border-strong`（Tailwind：`border-border` / `border-border-strong`）
- 文本：`--panel-text` / `--panel-text-secondary` / `--panel-text-muted`（Tailwind：`text-foreground` / `text-secondary-foreground` / `text-muted-foreground`；次要文案优先 `secondary-foreground`，弱化提示用 `muted-foreground`）
- 主按钮：`--panel-primary` / `--panel-primary-foreground`
- 强调色（激活 / 选中）：`--panel-brand` / `--panel-brand-bg` / `--panel-brand-border`，用于导航激活、Tab 激活、下拉与选择选中态
- 状态：`--panel-success-*`、`--panel-warning-*`、`--panel-danger-*`、`--panel-info-*`、`--panel-neutral-*`

### 表面层级约定

页面、卡片与卡片内组件构成固定的表面层级，禁止跨层复用：

- `--panel-bg` 只用于页面根（AppShell / LoginPage / maintenance 根容器）以及直接铺在页面上的页面级区域。
- 侧栏与顶栏可用 `--panel-surface`（或半透明 surface + blur）与内容底区分；页脚条可用 surface。
- 卡片 / 面板一律 `--panel-surface`；卡片内部任何组件**禁止**再使用 `--panel-bg`。
- 卡片内的内嵌分组、统计小格、表格头、选中列表项使用 `--panel-muted`（中性灰井道），选中项叠加 `--panel-border-strong`。
- **同一视觉区域内只允许一层卡片**：卡片内部不得再出现带边框 + 圆角 + 背景的卡片式容器。卡内分组用分节标题 + 分隔线（`border-t`）；纯数据块可用无边框的 muted 背景带（保留圆角但不加边框）；卡片内嵌的表格使用 `Table` 的 `framed=false`，由外层卡片承担边框；纯布局包装（仪表盘画布、编辑器外壳、详情区列容器）不得自带卡片外观。
- 例外：语义提示横幅（success/warning/danger/info）、可交互列表行/下拉选项、输入控件、Dialog/菜单浮层内部与 EmptyState 不受“一层卡片”限制。
- 输入类控件（Input / Select / Textarea / CodeEditor / 次要按钮 / 开关滑块 / 服务器选择行）使用 `--panel-surface` + 边框，在卡片内与卡片同色、靠边框区分。
- 表格体透明继承容器背景（framed 表外表可用 surface），行间用 `divide-border` 分隔；列表行默认透明 + 边框，hover 用 `--panel-hover`。

浅色主题以冷中性灰底 + 白表面 + 更实边框/文字对比为主；深色主题拉开 bg ↔ surface ↔ raised。内嵌表面固定为中性灰，不与页面底色同色。状态色使用浅背景 + 有色文本/边框。

## 字体与尺寸

- 字体：Inter + 系统字体栈；中文回退为 `PingFang SC` / `Microsoft YaHei`。
- 等宽：`var(--font-mono)`，用于日志、终端、代码、密钥指纹。
- 字号：12 / 13 / 14 / 16 / 18 / 20 / 24。页面主标题（PageHeader `h1`）约 20px（`text-xl`）+ `tracking-tight`，分节标题 16px，正文 14px，辅助信息 12-13px。
- 控件高度：sm 32px，md 36px，lg 40px；密集表格默认约 36–40px 行高。
- Badge 默认 `rounded-md` + `h-6` + `text-[12px]`（非 pill）；状态语义仍走 tone token。
- EmptyState 变体：`page`（虚线面板，用于加载失败等页面级空态）、`inset`（表内/筛选空，无虚线框）、`plain`（纯文案）。

## 圆角与阴影

- 控件圆角：12px，紧凑元素（菜单项、Tab、图标按钮、编辑器容器、Badge）8px。
- 面板/弹窗：16px；下拉/菜单浮层 12px，与触发控件曲率一致（内部选项 8px）；单项行可 12px。
- 普通面板只用边框，不加阴影；浮层使用 `--panel-motion-shadow-raised`（`.panel-shadow-overlay`）。禁止业务层 `shadow-xl` / `shadow-2xl`。

## Motion

- 动效只服务工作台反馈：hover / focus / pressed / selected / 浮层进出场 / 路由内容切换 / tab 内容切换 / 状态变化 / skeleton loading / 详情区上下文切换 / 不确定进度。不做装饰性、循环性或大幅位移动画。
- 统一经 `web/src/styles/main.css` 中的 `--panel-motion-*` token 与 `motion-*` utility；业务页面不要自造 duration、easing、translate、scale 或 shadow 常量，也不要自建同名过渡类。
- 时长：`--panel-motion-duration-fast|base|slow`（约 160 / 200 / 260ms）；`--panel-motion-hover-y` 约 1.5px、`--panel-motion-enter-y` 约 8px、`--panel-motion-press-scale` 约 0.97。
- `motion-control` / `motion-icon-control` / `motion-card`：hover 可表面变色 + 轻微上移（`translateY(calc(var(--panel-motion-hover-y) * -1))`）；`motion-tab` / `motion-menu-item` / `motion-list-item` 仅变色，避免密集列表跳动。按压用 press-scale。不得给 `motion-*` 永久设置 `will-change`。
- 浮层阴影统一用 `.panel-shadow-overlay`。
- `motion-field` 用于 Input / Select / Textarea；表格行使用只变色不位移的 `motion-table-row`，并支持 `--panel-stagger` 交错入场（每行延迟上限约 6 行，仅首屏/新增行播放）；异步列表「骨架 → 内容交错填充」：容器 `motion-stagger`，项 `motion-reveal` / `motion-list-item` / `motion-table-row`。`motion-overlay` 为挂载淡入工具类。
- 入场动画一律使用 `animation-fill-mode: backwards`（`panel-motion-enter` 系列）。禁止改回 `both`/`forwards`。
- 焦点态只有一套：字段与按钮统一用 `focus-visible:ring-2 ring-offset-2`。
- 浮层进出场成对：`dialog-*`、`menu-*`、`drawer-*`、`toast-stack-*`；路由 `route-*`（仅淡入淡出）；Tab `tab-panel-*`；状态 `status-*`；侧栏 `shell-label` / `shell-grid` / `fade-*`；覆盖式详情 `detail-swap-*`（仅进场，位移走 `--panel-motion-enter-y`）；不确定进度 `motion-progress`。
- 必须支持 `prefers-reduced-motion: reduce`。
- 触屏：自有 `motion-*` 的 hover 反馈必须包在 `@media (hover: hover)` 内；交互控件统一 `touch-action: manipulation`。

## 断点与滚动

唯一布局断点为 1024px：

- `>=1024px`：AppShell 填满视口，页面级禁止滚动；滚动限制在模板内容、表格体、日志正文、详情正文等内部区域。
- `<1024px`：导航转抽屉，允许页面级滚动，双栏模板折叠为单栏。

## 主题运行时

主题模式为 `system` / `light` / `dark`，由 `web/src/design/theme.ts` 管理，偏好存 localStorage `panel.theme.mode`。配色方案为 `lighthouse`（灯塔，默认）/ `ocean`（蔚蓝），偏好存 localStorage `panel.theme.scheme`，两套方案各提供浅色与深色色板。运行时通过 `document.documentElement.dataset.theme`、`document.documentElement.dataset.scheme` 和 CSS 变量切换，不再通过 UI 框架 provider 注入主题。 为消除首屏白闪，`web/index.html` 内联脚本会在首帧渲染前按 `panel.theme.mode` / `prefers-color-scheme` 预置 `data-theme`，启动遮罩使用与 `--panel-bg`、`--panel-border-strong`、`--panel-primary` 一致的内联色值。
