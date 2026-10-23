# v4 布局体系规范

## 总体结构

```
AppShell
├── Sidebar（桌面常驻，260px / 折叠 76px；独立 chrome 面，与内容区明显分层）
│   ├── 品牌区（左侧品牌色竖条 + 折叠按钮；折叠时文案随列宽渐隐）
│   └── NavList（桌面侧栏与移动抽屉共用的导航实现）
└── Main
    ├── 跳过链接（首个可聚焦元素，指向 #main-content）
    ├── GlobalHeader（约 52px 细工具条）
    └── RouteContent（overflow: hidden）
        └── PageHeader + 页面模板内部滚动区
```

登录和维护启动页不使用全局 AppShell。

## AppShell

- 桌面 `>=1024px`：`h-dvh`、`overflow-hidden`，左侧导航固定，顶部条固定，路由内容区 `overflow-hidden`。
- 窄屏 `<1024px`：左侧导航变自有抽屉，body 恢复页面级滚动；顶部条在该断点下必须粘性置顶（`sticky top-0`，层级低于抽屉/Dialog/Toast），保证抽屉入口、主题、语言和账户随时可达。
- 触控尺寸策略：桌面保持 token 密集高度（36px 行高、32/36/40px 控件，满足 WCAG 2.2 AA 的 24px 最小目标）；窄屏抽屉的关闭按钮与导航行必须放大到 44px（`max-lg:size-11` / `max-lg:h-11`），因为它们在手指最容易误触的位置。顶栏全局操作按钮保持 36px——手机宽度下 5 个 44px 按钮会使 360px 视口横向溢出，属于显式取舍而非遗漏。
- 导航定义在 `web/src/components/shell/navModel.ts`，列表渲染在 `web/src/components/shell/NavList.vue`；桌面侧栏与移动抽屉必须复用同一实现，不得各写一份。新增导航项必须同步 i18n 和模块文档。
- 侧边导航只放页面族入口，不展开页面内部 tab：例如证书只保留“证书”，应用只保留“应用”，服务器只保留“服务器”。资源与安全不再是导航组，其能力位于服务器详情 tabs；`/certificates/keys` 等深链路继续由路由支持并在页面内切换 tab，但不得作为独立侧边菜单项。
- 导航项必须显式绑定 `aria-current="page"`（以 `navModel.activeNavKey` 计算归属），使 `/certificates/keys` 这类深层子路径仍能正确标记所属入口；**激活项使用实心 primary 填充 + primary-foreground 文字**（不再使用淡 tint + 细轨）。
- 折叠时导航文案保留在无障碍树中（`max-width` + `opacity` 渐变 + `overflow-hidden`），不得用 `v-if` 直接删除；折叠按钮位于品牌区并暴露 `aria-expanded` 与 `aria-controls`。
- 全局顶部条为细工具条：左侧仅窄屏菜单入口、路由短标签（muted）与 alpha；右侧主题/配色/语言/账户收成 `rounded-xl border bg-card` 工具簇。页面主操作放 PageHeader actions，对象级操作放对象所在表格的操作列；需要参数面板或逐文件/逐节点/逐步骤的操作保留在覆盖详情内。
- 窄屏抽屉以 dialog 语义呈现（`role="dialog"` / `aria-modal` / `aria-label`），支持 Escape 关闭、焦点圈定与恢复、背景滚动锁定；打开后焦点自动移入抽屉，关闭后回到触发按钮；抽屉品牌区与桌面侧栏同一视觉语言。
- 外壳必须提供本地化的“跳到主要内容”链接作为首个可聚焦元素，目标为路由内容区 `#main-content`。

## PageHeader

- 源码：`web/src/components/shell/PageHeader.vue`。
- 统一结构：标题（`h1`）、说明、可选 back（返回上一级）、可选 actions、可选 breadcrumb。
- **页面主标题以 PageHeader 为唯一主标题**；AppShell 顶栏只显示路由上下文短标签（次级 `text-muted`），不得再与 PageHeader 并列成两个同级大标题。
- 间距节奏：header `px-4 pt-4 pb-3`，标题与 actions 底对齐；`ConsolePage` 正文默认 `p-4`（窄屏 `p-3`）。不得再叠一层大 padding 的装饰卡片包住整个列表。
- 需要返回上一级时传入 `backLabel` 并监听 `back` 事件，返回按钮渲染在标题左侧；返回目标由页面决定（如详情返回列表、编辑器返回详情），不得用 `router.back()` 兜底。
- actions 区主操作至多一个，其余操作用次要按钮或 Dropdown。
- 中窄屏下标题区与 actions 区上下堆叠，actions 可换行，不得裁切关键操作。

## Table

- 源码：`web/src/components/ui/Table.vue`。
- `Table` 根容器自身承担滚动；页面可通过 `h-*` / `max-h-*` 约束高度，表头在该滚动区内 sticky。
- 根容器默认保留圆角和边框；嵌入已有边框的卡片时使用 `framed=false`，避免同区域出现两层卡片框。不得在外层限高后再依赖内层百分比高度建立滚动区。
- 列定义支持可选 `width` / `nowrap`，并可通过 `fixed` 开启 `table-fixed`：需要稳定单行的列（时间、级别、操作）用 `nowrap`，长文本列用 `truncate` 截断，需要悬浮完整内容时配合 `Tooltip`。
- 单元格插槽按列 key 命名（`#<key>`），表头插槽为 `#head-<key>`（如全选复选框）；`#empty` 插槽在真实无数据时渲染空态，加载失败/筛选为空由页面在表格外先行分支。
- 行操作列：每行固定「详情」入口；高频操作直接平铺为行内按钮（如编辑、测试连接、续签、同步、容器生命周期），低频/危险操作收进行内「更多」菜单；操作列右对齐并 `nowrap`。
- 名称列：实体列表用 `TableDetailName` 打开覆盖式详情（与「详情」按钮同语义）；须有本地化 `aria-label`（`common.openDetails`）。
- 当前选中行（覆盖详情打开的对象）通过 `active-key` 标记 `aria-current="true"` 与 muted 表面；表格行本身不可点击，不得把整行绑成点击区。

## 页面模板

模板位于 `web/src/components/templates/`，只负责结构、间距、滚动和断点折叠，不含业务逻辑。

- `ListPage`：实体列表工作台的标准列表壳——`toolbar` + 表格滚动区 + `pagination`；**不得**再包一层 `rounded-2xl border bg-card` 外框。表格默认 `framed=true` 承担唯一边框层。servers / credentials / dns / certificates / applications / operations / tasks / logs 列表必须使用本模板。
- `TableDetailLayout`：实体工作台（servers/credentials/dns/certificates/applications/operations/tasks）的统一形态——列表与覆盖式详情叠放在同一网格单元，详情覆盖整个内容区（含列表页标题栏）。页面提供 `list` / `detail` 两个具名槽；详情打开时列表保持挂载但 `inert` + `aria-hidden`，关闭后滚动位置与筛选不丢；详情只播放一次进场（`detail-swap-*`）。日志 `/logs` 不使用本模板，详情用 Dialog。
  - 高度契约由模板自身承担（`h-full min-h-0`）：页面不得用页面级滚动替代模板或详情正文的内部滚动；详情正文由页面按需选择整体滚动或分区滚动（服务器详情用 `ConsolePage` 的 `body-scroll=false`，资源 tab 各自内部滚动）。
  - 详情自带 `ConsolePage`/`PageHeader` 标题与本地化返回（`common.backToList`），返回清空选中参数与 tab 参数。
  - 选中对象不在当前页时（深链），页面按 id 补载详情，不得回落到“无选中”空态。
  - **内容区信息架构**：同一任务面的信息纵向叠在同一页/同一 tab（KPI + 列表 + 相关元数据）；**不同关注点**才用 `Tabs`（例：反向代理 `routes` vs `nodes`；服务器 Docker 资源用一级 `resources` + URL `subtab`）。禁止详情里「主栏正文 + 右侧无关信息栏」常驻左右分栏；禁止为短摘要硬拆 tab。例外：设置页左侧分区导航、表单内「输入 + 删除」双列控件、Metrics 图表网格（同属监控）。
- `MasterDetailLayout`：遗留模板，保留组件测试与高度契约；**操作 `/operations`、任务 `/tasks` 与其它实体工作台一律使用 `TableDetailLayout`**；**日志 `/logs` 使用 `ListPage` + Dialog**；均不得再接入本模板交付页。
  - 高度契约由模板自身承担（`h-full min-h-0`）：调用方不再需要 `min-h-[600px+]` 之类的魔法高度，也不得用页面级滚动替代内部滚动。
  - 详情区正文统一使用 `grid-rows-[auto_minmax(0,1fr)]`（标题行 + 正文行）。存在可选横幅/告警时，横幅必须放进正文的内部滚动区，不得作为独立 grid 行，否则正文会落到 `auto` 行而失去内部滚动。
  - 传入 `detail-key`（当前选中对象标识）时，详情内容按 key 播放一次进场（`detail-swap-*`，只做进场）；同一对象的刷新不重播，未传 key 的页面保持即时替换。
  - 窄屏单列时，主列表/上下文选择器必须限高（`max-lg:max-h-[60dvh]`）并保持自身内部滚动；页面级滚动仍是窄屏的唯一页面滚动方式。
- `EditorPage`：复杂创建/编辑，正文内部滚动，底部固定提交栏。
- `SettingsPage`：分区短表单，最大宽度收敛，分区独立保存。嵌在 `ConsolePage` 内时**不得**再叠一层大 padding（由 `ConsolePage` 正文 `p-4` 承担）；分区面板保持单层 `bg-card` + `p-4`，侧栏可单独保留一层边框。
- `WorkspacePage`：诊断、日志、终端等满高工作面。
- `ConsolePage`：PageHeader + 内部滚动正文的基础容器，可被上面模板组合。正文容器必须 `min-width: 0`，桌面禁止横向滚动兜底；页面内部 grid、按钮和侧栏需要自行用 `minmax(0, 1fr)`、`min-w-0`、`overflow-hidden` 和断点换列处理宽度。

## 表面与 chrome 密度

- 主面板 / KPI / 列表外框一律 `bg-card`（`--panel-surface`）；不得把随机 `bg-muted` 当作外层卡片底。
- **禁止 card-in-card**：已在 `rounded-* border bg-card` 表面内的列表、风险行、时间线条目、设施章节，优先用 `divide-y` / `border-t` 分段或 muted 条带，不得再包一层带边框的内卡片。
- `motion-card` 仅用于真正可交互/可拖拽的磁贴；静态 KPI 与只读摘要卡用静态表面 + 可选 `motion-reveal` 入场即可。
- 路由分段模式（如证书 domains/self/keys）必须复用共享 `Tabs` 视觉（`rounded-xl border bg-muted p-1` 分段控件），通过 `v-model` + `router.push/replace` 保持 URL 路径，不得手写第二套分段导航样式。

禁止把阶段占位页作为交付物；页面族进入实现后必须拥有专属业务结构、内部 tab/分段控件、真实 API/Mock 边界和操作闭环。

## 页面族独立性

禁止把服务器、凭据、安全、资源、应用、DNS、证书、任务、设置、诊断全部挂到同一个通用组件。每个页面族必须至少具备：

- 独立视图目录和入口组件。
- 独立 API 模块或明确的 API 映射层。
- 独立 Mock 路由和代表性状态。
- 独立测试覆盖。
- 独立信息架构和闭环操作，不以参数切换同一个列表详情壳作为交付形态。

## 滚动红线

- 桌面：路由组件根必须填满内容区并禁止页面级滚动。
- 滚动只能发生在模板正文、表格体、详情正文、日志正文、编辑正文等内部区域。
- 仪表盘卡片允许跨列时，必须用容器宽度或断点保护；在容器不足以容纳两列时不得让 `grid-column: span 2` 生成隐式列撑宽页面。
- 编辑器类固定格式页面必须至少定义宽屏、中屏、窄屏三档布局：宽屏可保留主编辑区 + sticky 摘要，主编辑正文内部滚动；中屏（约 900-1279px）必须让摘要区下移或折叠到主内容之后，header 自然堆叠，连续配置正文不得被过小 `max-height` 压扁；窄屏必须单列组织 header、模式切换、字段和摘要，不得依赖横向滚动兜底。只有确实采用步骤导航的页面才允许为步骤导航定义稳定的多列布局。
- 窄屏：AppShell、RouteContent、ConsolePage 和 EditorPage 必须共同解除视口裁切并允许页面级滚动，双栏折叠为单栏或抽屉；只改 body overflow 不算完成。large Dialog 的 body 自身滚动，Dropdown 使用 body portal、fixed 定位和上下碰撞计算，确保提交栏与菜单可达。
