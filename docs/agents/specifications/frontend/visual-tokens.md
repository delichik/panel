# v4 视觉 Token 规范

> 当前落地处为 `web/src/styles/main.css`、`web/src/design/theme.ts` 与 `web/src/design/tokens.ts`。视觉参考 OpenShip 的克制低边框、高密度工作台和黑白主操作，但不复制其品牌、logo、文案或页面结构。

## 红线

1. 禁止在业务页面引入 Naive UI / Vuetify 或它们的组件写法。
2. 业务代码禁止裸色值、裸尺寸、裸阴影和新增视觉常量；颜色和状态经 `--panel-*` token、Tailwind theme 或 `web/src/design/tokens.ts`。
3. 图标统一用 `@lucide/vue`，图标按钮必须有可访问名称或 Tooltip。
4. 主操作使用黑白反转的 `Button variant="primary"`；语义状态只用于 success / warning / danger / info，不用品牌色表达健康或错误。
5. 卡片/面板保持低边框、少阴影；不要叠卡片，不要用装饰性渐变或大色块制造视觉重量。

## 色彩与层级

- 页面背景：`--panel-bg`
- 面板表面：`--panel-surface`
- 弱表面：`--panel-muted`
- hover 表面：`--panel-hover`
- 边框：`--panel-border` / `--panel-border-strong`
- 文本：`--panel-text` / `--panel-text-secondary` / `--panel-text-muted`
- 主按钮：`--panel-primary` / `--panel-primary-foreground`
- 状态：`--panel-success-*`、`--panel-warning-*`、`--panel-danger-*`、`--panel-info-*`、`--panel-neutral-*`

浅色主题以白、近白和低对比灰为主；深色主题以黑、近黑和半透明白边框为主。状态色使用浅背景 + 有色文本/边框，避免整页单色化。

## 字体与尺寸

- 字体：Inter + 系统字体栈；中文回退为 `PingFang SC` / `Microsoft YaHei`。
- 等宽：`var(--font-mono)`，用于日志、终端、代码、密钥指纹。
- 字号：12 / 13 / 14 / 16 / 20 / 24。页面标题 20px，分节标题 16px，正文 14px，辅助信息 12-13px。
- 控件高度：sm 32px，md 36px，lg 40px；密集表格默认 36px 行高。

## 圆角与阴影

- 控件圆角：12px，紧凑元素可 8px。
- 面板/浮层：16px，单项行可 12px。
- 普通面板只用边框，不加阴影；浮层使用轻量 shadow。

## Motion

- 动效只服务工作台反馈：hover / focus / pressed / selected / 浮层进出场 / 路由内容切换 / tab 内容切换 / 状态变化 / skeleton loading。不做装饰性、循环性或大幅位移动画。
- 统一经 `web/src/styles/main.css` 中的 `--panel-motion-*` token 与 `motion-*` utility；业务页面不要自造 duration、easing、translate、scale 或 shadow 常量，也不要自建同名过渡类。
- 常规反馈控制在 150-220ms；位移只允许 1-2px 或轻微 scale，必须使用 `transform`，不能改变尺寸、间距或滚动结构。
- `motion-control` / `motion-icon-control` 用于 Button 和 IconButton；`motion-field` 用于 Input / Select / Textarea；`motion-tab`、`motion-menu-item`、`motion-list-item`、`motion-card` 分别用于 tab、菜单项、列表/选择项、卡片；表格行使用只变色不位移的 `motion-table-row`，并支持 `--panel-stagger` 变量做交错入场（每行延迟上限约 6 行，仅首屏/新增行播放，刷新不重放）；`motion-overlay` / `motion-popover` / `motion-toast` 保留为元素挂载时的入场动画工具类。
- 浮层进出场成对实现（进场与退场都在）：Dialog、Dropdown、Select 下拉、移动端侧边抽屉、Toast 经 Vue `<Transition>` / `<TransitionGroup>`，统一类名 `dialog-*`、`menu-*`、`drawer-*`、`toast-stack-*`；路由内容切换用 `route-*`（仅淡入淡出，避免 transform 影响页面内 fixed 元素）；Tab 内容切换用 `tab-panel-*`；状态徽标变化用 `status-*`；侧边栏折叠文字淡入用 `fade-*`、导航列宽过渡用 `shell-grid`。
- 必须支持 `prefers-reduced-motion: reduce`：关闭位移和骨架动画，将 transition / animation 降到近似无动画。新增动效前先确认该降级规则覆盖到对应元素。

## 断点与滚动

唯一布局断点为 1024px：

- `>=1024px`：AppShell 填满视口，页面级禁止滚动；滚动限制在模板内容、表格体、日志正文、详情正文等内部区域。
- `<1024px`：导航转抽屉，允许页面级滚动，双栏模板折叠为单栏。

## 主题运行时

主题模式为 `system` / `light` / `dark`，由 `web/src/design/theme.ts` 管理，偏好存 localStorage `panel.theme.mode`。运行时通过 `document.documentElement.dataset.theme` 和 CSS 变量切换，不再通过 UI 框架 provider 注入主题。 为消除首屏白闪，`web/index.html` 内联脚本会在首帧渲染前按 `panel.theme.mode` / `prefers-color-scheme` 预置 `data-theme`，启动遮罩使用与 `--panel-bg`、`--panel-border-strong`、`--panel-primary` 一致的内联色值。
