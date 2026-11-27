# WinUI 3 符合性检查表

> **定位**：本文件是项目的前端设计**检查表**，不是 WinUI 3 官方文档的替代。任何页面/控件实现都必须先通过
> 本表检查；它与 `visual-tokens.md`、`layout-system.md`、`interaction-patterns.md`、验收合同冲突时，以
> **WinUI 3 官方规范 + 本检查表**为准，并同步修正文档。
>
> 历史教训：只按"代码区域"审计、把自己的 P0/P2 配方当真理、只做静态 grep/测试，会漏掉内容列宽度、
> 页面几何、平台态等基础问题。本表按 UX 维度组织，防止再出现"设置页有宽度约束、其它页全宽"这类分裂。

## 1. 页面几何（Page geometry）
- [ ] 内容列有统一最大宽：所有页面 `ConsolePage width="content"` = **1200px**（含页面留白，内容盒 1168）；`narrow` 仅作为未来聚焦页 API，不作为当前页面宽度差来源。
- [ ] 设置页标题使用当前分区名（如“运行时/外观”），不得再出现“设置 + 分区”双重标题；分区卡不再重复页标题。- [ ] 内容列**水平居中**（`mx-auto`），页头与正文同一列；不是全宽铺满。
- [ ] 页头（PageHeader）与正文在同一内容列内，左缘对齐；带返回按钮的标题需与正文左缘对齐（back 行不得让 h1 固定内缩）。
- [ ] 页面外边距：桌面 16px、窄屏（<1024）12px；页头/正文/footer 由 `ConsolePage` 单一 padding owner 承担。
- [ ] 数据表格超宽时在内容列/表格容器内部横向滚动，不以整页横向滚动兜底。
- [ ] 桌面 `>=1024px` 页面无整页滚动；滚动只在列表/表格/详情/编辑正文/日志内部。窄屏允许页面滚动。
- [ ] 窗口尺寸变化时内容列不抖动：断点使用项目明确映射（外壳 1024；内容参考 640/1007/1239/1599），不混用 1280/1536 断点。
- [ ] 悬浮层/Dialog 宽度独立于内容列：Dialog 320–548（large 单独 token），不随页面全宽拉伸。

## 2. 外壳与导航（NavigationView / TitleBar）
- [ ] NavigationView 桌面展开 **320px**、compact **48px**（`--panel-nav-width(-compact)`）。
- [ ] 标题栏整宽横跨 pane + content；标题栏只放应用标识/全局命令，不重复路由标题/PageHeader h1。
- [ ] 导航选中 = 中性填充 + 前景文字 + 3×16 accent 指示条；compact 保留同一指示语义。
- [ ] 导航项 `aria-current="page"`；skip link 指向 `#main-content`；路由切换后焦点移交 h1/main。
- [ ] 窄屏抽屉与桌面 pane 同一导航实现、同一宽度 token；标题栏 sticky 时不透明。
- [ ] 页面级路由切换为单段 Page refresh；同一 `pageKey` 内（设置分区、证书 tab）不重挂载/不重播转场。

## 3. 主题与平台态（Theme / Mica / accessibility）
- [ ] 明暗主题 + Mica 分层；页面根不铺不透明背景；卡片/表面对比度符合 AA。
- [ ] `forced-colors` / 高对比度：文本、边框、焦点、禁用态可辨；不被自定义颜色吞掉。
- [ ] RTL：布局方向、图标方向、文本截断可接受（如产品支持 RTL，需单独验收）。
- [ ] `prefers-reduced-motion`：所有 entrances/hover/press/indicator 动画降级。
- [ ] `transparency`/Mica 关闭/背景图失败时回退纯色，不出现空白/透明黑洞。

## 4. 排版、间距、尺寸
- [ ] Type ramp：Caption 12、Body 14、Body Strong 14/600、Subtitle 20、Title 28、TitleLarge 40、Display 68；无阶梯外字号。
- [ ] 布局间距 4px 网格（0/4/8/12/16/20/24）；2px 仅图标/文本光学对齐。
- [ ] 控件高 28/32/40（`h-control*`）；**28 仅允许表格行内紧凑操作或密集工具栏**，可点击命中区仍须 ≥32（触控 ≥40，可用 padding/伪元素扩展而不改视觉高度）；字段最小宽 64；Dialog min 320。
- [ ] 圆角两档：控件/输入 4px，卡片/表面/Dialog/Flyout 8px；Badge 胶囊。
- [ ] 图标：控件/导航 16px、较大操作 20px；优先使用对应尺寸母版；仅有 24px 母版时允许等比缩放到 16/20，禁止 12/14px 非标准尺寸。

## 5. 控件状态
- [ ] Button/Input/Select/Checkbox/Switch/Tabs/Table/Dropdown/Dialog：PointerOver、Pressed、Focus、Disabled、Loading 状态都有定义。
- [ ] 焦点：控件 2px `ring-ring`；字段保留 accent 底线；无 `ring-offset`/accent ring 充焦点。
- [ ] 选中语义分控件：TabView 文字主色+下划线；ComboBox 勾选；ListView 行 accent 填充；不得互串。
- [ ] 禁用态用色板而非单一透明度；禁用控件仍能解释原因（tooltip/描述）。
- [ ] 最小命中区：桌面 32、触控 40；图标按钮有可访问名称。
- [ ] 表格行高 40（`h-10`）、表头 Body Strong 14、sticky 表头不透明；多行内容必须显式使用 `rowHeight="auto"` 变体。

## 6. 动效
- [ ] 时长 83/167/250/333ms；Page refresh 单段；Dialog/Flyout/Toast 各自进出场。
- [ ] 无 hover 上浮、press 缩放、水波纹、循环装饰动画。
- [ ] 动效不阻塞交互；`prefers-reduced-motion` 全量降级。

## 7. 页面模式
- [ ] 列表：`ListPage` + `ListCommandBar` + `Table` + 分页；页级 loading/error/empty 走模板状态槽。
- [ ] 详情：`TableDetailLayout`/`DetailSection` + `PropertyList`；无 card-in-card。
- [ ] 设置：单页单路由 + 固定 Tabs + `SettingsSection/SettingsCard`；切分区不重载；卡外标题、卡内行。
- [ ] 编辑器：单一滚动 owner（EditorPage）、单一 PageHeader、无第二标题带/三层 padding。
- [ ] 对话框：ContentDialog 尺寸/焦点/滚动锁正确；表单字段统一 `FormField`；错误用 `InfoBar`。

## 8. 验证证据
- [ ] 静态检查：`tokenGuard` + 旧视觉 grep + `vue-tsc` + `vite build` 全绿。
- [ ] 页面级测试：行为/键盘/滚动/空错态/URL 恢复。
- [ ] 视觉证据：至少 1280、1920、窄屏（<1024）三档截图；与 Windows 11 Settings / WinUI Gallery 对照。
      （无浏览器权限时由产品/用户提供截图，不得以"测试通过"代替视觉验收。）
- [ ] 内容宽度、内容列左缘、页头对齐、滚动容器、标题层级逐项对照。

## 开放问题（2026-11-18 记录，未关闭）
| ID | 问题 | 影响/归属 |
| --- | --- | --- |
| OI-001 | DNS 列表 `DnsDomainDto` 无 `status`/`recordCount`，当前列精简为 3 列（名称+provider/更新时间/操作） | 需后端 list DTO 补字段后才能出状态/记录数列 |
| OI-002 | credentials 列表 DTO 无 fingerprint/keySummary，摘要列暂用 username 截断 | 需后端 list 返回摘要字段或批量 detail |
| OI-003 | servers tabs 容器/网络/卷时间列无 Created 专用 i18n key，暂复用 Updated/Last checked | 需产品确认后加 key |
| OI-004 | applications imageUpdate 折叠为状态列次级徽标，不再独立列 | 若需始终可见需调整优先级/副行 |
| OI-005 | events 移除 module/state 手动输入，只保留 URL 深链 chip + 清除 | 若需手输，需要 Filter Flyout primitive |
| OI-006 | ProbeOverviewPanel 首载失败时详情右栏摘要停留在 Loading | 需 emit loading/error 态 |
| OI-007 | applications 资产区移出双列后，保存 LoadingOverlay 不覆盖全宽资产卡（按钮已 disabled） | 需要 EditorPage 外层 relative 或局部 overlay |
| OI-008 | 5 列 tables 在 1024–1239 内容列可能内部横向滚动 | 符合“容器内横滚”，如需更紧凑需窄屏隐藏列 |
| OI-009 | Dialog 尚无显式 1000 宽度能力，表单宽度由 Dialog 默认 548 承担 | 若产品要求宽表单需 ui/Dialog 扩展 |
| OI-010 | 列表页默认 1200 居中，未提供 full-width 模式 | 超宽表格页如需满宽需 ConsolePage 增加 width 变体 |
| OI-011 | RTL 未支持/未验收；图标 16/20 母版尚未 vendoring，仍 24 母版缩放 | 后续平台化批次 |
| OI-012 | 1280/1920/窄屏/forced-colors/键盘视觉证据仍缺，无 e2e/axe | 需截图或外部浏览器验收 |

## 当前已知缺口（滚动维护）
- forced-colors 基础焦点/选中已补；图表 canvas HC 已降级，但真实 HC 渲染/色觉仍缺截图证据。
- CodeEditor 深色主题已补；语法色板仍是组件内 hex，未 token 化。
- 内容列 1200/1000 已落到 ConsolePage width API，设置/表单/编辑器已接 narrow；内容断点 1007/1239/1599 映射仍未统一（xl=1280 仍在用）。
- PageHeader/正文/footer padding owner 已收口；登录页居中仍为聚焦面例外。
- WinUI primitives 已建立并迁移：NumberBox（全部 Input type=number）、RadioButton（credentials）、Expander（applications）、ToggleButton（events）、BreadcrumbBar（servers/applications/certificates/dns/credentials 详情与编辑器，`ConsolePage :breadcrumbs`）。
- Toast 已改 InfoBar 视觉（4px/语义色/无阴影）；Dialog 支持 `width="wide"`（720 token，旧 large 兼容）；DateTimeRangePicker 字段最小宽与 32px 日历目标已补。
- 设置页已统一 1200 内容列；PageHeader 标题为当前分区名，顶层分区重复标题已删除（备份导出/还原分组标题保留）。
- applications 已做按需 mode 加载/列表请求去重/行图片延迟串行去重；编辑器深链仍有既有的一次列表请求；后端 list 未直接返回 imageReference 时仍有按需补请求。
- 列表/断点/状态槽/多行表已在 servers/certificates/dns/credentials/applications/events/logs 落地；其余 `lg:` 仅外壳/滚动/触控用途。
- Type ramp/4px 间距/图标/命中区已做第一批收口；仍有写作用域外 patterns 的旧别名与手写 600，Table 表头 10px/数据 6px padding 为 40px 行高例外。
- 内容断点已落 `wide:1239`/`wider:1599`（overview/debug/metrics），编辑器仍以 1280 为双列安全点（主列≥560 的例外）；Firewall 等残余 xl 待下批。
- 启动屏 reduced-motion/硬编码 token、reduced-transparency、死 token/死模板、重复请求仍有部分残留。
- 视觉证据仍缺：需 1280/1920/<1024 三档、forced-colors/键盘/滚动容器截图；无 e2e/axe。
