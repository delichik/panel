# 统一交互组件模式

> 本规范记录跨页面复用的交互 primitives 和 patterns。业务页面遇到同类问题时必须优先复用这些组件，避免在页面内手写视觉相近但行为不同的控件。

## UI primitives

- `SearchInput`：用于列表、资源、任务、证书等搜索输入。支持 `v-model`、`placeholder`、可选清空按钮和禁用态；启用清空按钮时必须传入本地化 `clearLabel`。搜索状态仍由页面同步到 URL query。
- `PaginationBar`：支持 `mode="cursor"`、`hasPrevious`、`hasNext`，此模式只发出 previous/next，不计算或显示页码；总数未知时不得传入伪造 `total`（默认也不再假定 `0`），且不得显示「x / y」或「a-b of total」摘要——仅在传入 `summaryLabel` 或自定义 `#summary` 时展示左侧文案；页面应维护真实游标历史。原有 page 模式在提供有限 `total` 时保持不变。用于长列表分页底栏。支持 `page`、`pageSize`、`total`、`v-model:page`、`previous`、`next`，固定放在列表模板底部，不随表格正文滚动；上一页、下一页和摘要文案由页面 i18n 传入。
- `ConfirmDialog`：用于删除、停用、重置、覆盖导入等需要用户决策的操作。必须传入影响说明 `impact`；危险操作使用 `tone="danger"`，高风险操作可启用 `requireCheckbox`；确认、取消、勾选确认文案由页面 i18n 传入。
- `FileUploadButton`：用于单文件或多文件选择，页面负责执行上传。禁止在业务页裸露不同样式的 `<input type="file">`。
- `DownloadButton`：用于 blob、归档、证书、密钥等下载动作，页面负责调用 API 并处理 `saveBlobDownload`。
- `StatusBadge`：集中维护状态到 tone 的映射。支持 `generic`、`server`、`task`、`certificate`、`resource`、`operation` domain；页面只有在确有业务差异时才传入显式 `tone`。
- `Select`：用于单选下拉。交付形态必须是 Panel 自有 combobox + listbox 浮层，使用 popover 表面、统一 hover/selected/focus/motion 状态和暗色主题 token；不得把浏览器原生 `<option>` 展开菜单作为用户可见交互形态。必须把页面传入的 `aria-label` / `aria-invalid` 等 ARIA 属性绑定到 combobox 控件（组件 `inheritAttrs: false`，不得丢弃），并支持方向键、Home/End、Escape 与 `Tab` 关闭。可选搜索模式由 `searchable` 启用，配套 `searchPlaceholder`（下拉内搜索框占位与无障碍名称）和 `searchEmptyText`（输入非空且无匹配时的空态文案）两个文案 props；输入变化触发 `search(query)` 事件，原样携带当前搜索词，空串表示清空。默认搜索只过滤下拉可见选项，不清空、不改变已选标签：当前值不在过滤结果中时，触发器仍展示已选中的标签。搜索框内支持 ArrowUp/ArrowDown 移动、Enter 选择、Escape/Tab 关闭下拉；非搜索模式行为完全不变。父级可监听 `search` 做远程搜索并把结果按稳定 id 合并回 `options`（搜索词应防抖，空串时清理远程候选），加载/失败/无匹配文案由父级传入并在下拉外就地提示；组件不得内置业务 API 或路由。页面级搜索仍使用 `SearchInput`，不得手写放大镜 + Input 组合；下拉内部的候选项过滤与远程搜索才使用本搜索模式。
- `Dropdown`：菜单 Teleport 到 `body`，使用 fixed 定位、视口边界收敛和上下碰撞选择，不能留在业务容器内被 `overflow` 裁切；菜单宽度按内容收缩并保持紧凑，禁止 fixed 后按视口宽度拉伸；继续支持方向键、Home/End、Escape 和焦点恢复；`Tab` 离开菜单时必须关闭并同步 `aria-expanded`。
- `Dialog`：不响应遮罩点击关闭，只能通过关闭按钮、取消操作或 Escape 关闭；普通和 large 尺寸的 body 都必须有可靠的内部纵向滚动，页脚保持在弹窗网格底部；业务正文不能依赖外层页面滚动才能到达。
- `ToastProvider`：支持可选 `action={label,to}` 路由动作，带动作保留 15 秒；`useSuccessToast(title, receipt)` / `useErrorToast(title, error)` 从本次返回的真实关联生成「查看日志」（默认 `/logs?...&level=info,warning,error`），禁止用全局最近请求或解析提示文本关联。全局顶部 toast；页面和组件内 catch 到的异常统一以 danger toast 展示，字段校验与结构化诊断仍就地展示。
- `LoadingOverlay`：用于对话框正文、文本编辑区、卡片或区块等待网络响应时的统一加载覆盖；不得用裸文字代替加载效果。
- `Table`：WinUI ListView 语义。行高 40px（`h-10`）、数据单元格单行（`whitespace-nowrap` + `align-middle`）；首次加载且没有旧数据时传 `loading` 与本地化 `loadingLabel`，由组件渲染表格骨架行；已有数据刷新时保留当前 rows，只让刷新入口或分页入口显示 loading。表格行由组件统一加 `motion-table-row` 交错入场（`--panel-stagger`，仅首屏/新增行播放）。表头单元格必须输出 `scope="col"`，sticky 表头用不透明 card 底。表格嵌入已有边框的卡片时传 `framed=false`，由卡片承担边框（同一区域只保留一层卡片框）。单元格插槽按列 key 命名（`#<key>`），表头插槽 `#head-<key>`（如全选复选框），`#empty` 插槽负责真实无数据空态且必须用 `variant="inset"`；行操作列每行固定「详情」入口，高频操作直接平铺为行内按钮，只有低频/危险操作收进行内「更多」菜单；`active-key` 标记覆盖详情当前对象（`aria-current` + accent-selected 表面），表格行本身不可点击。
- `TableDetailName`（`web/src/components/patterns/TableDetailName.vue`）：实体工作台名称列的详情入口；点击打开与操作列「详情」相同的覆盖式详情；必须传入本地化 `label`（`common.openDetails`）；不得用整行 click 替代。
- `CodeEditor`：文本/代码编辑器（CodeMirror）。内置查找/替换面板：`Ctrl/Cmd+F` 打开查找、`Ctrl/Cmd+H` 打开同一面板进入替换、`F3/Shift+F3` 或 `Ctrl/Cmd+G` 下一个/上一个、`Esc` 关闭；全部匹配高亮，支持 正则 / 区分大小写 / 整词 三个开关；只读态自动隐藏替换区；面板文案由组件内 i18n 词条（`codeEditor.*`）随界面语言切换，业务页面无需额外传参。

## Pattern components

- `DateTimeRangePicker`（`web/src/components/ui/DateTimeRangePicker.vue`）：时间范围选择器。触发器样式与 Select/Input 一致（`h-control` 32px、同 token 边框/圆角/背景），点开是左右联动的双月历（支持跨月/跨年）：先点开始日期、再点结束日期，仅高亮起止之间的日期并带 hover 预览；日格/导航/预设按钮遵守 WinUI 最小命中区（日格 ~40、按钮 `h-control-sm`）；开始/结束的 时:分:秒 用 `TimeSegmentInput` 设置在同一行；带最近 24 小时/7 天/30 天快捷预设和应用/取消；无弹层套弹层。值以 ISO `{ from, to }` 双向绑定。
- `TimeSegmentInput`（`web/src/components/ui/TimeSegmentInput.vue`）：两位数数字输入框。无弹层、无滚动条，直接输入数字、键盘上下键或滚轮微调，失焦自动补零；用于时分秒等数字段。

- `ServerMultiPicker`：用于应用部署、设施覆盖节点、批量任务等服务器多选。支持禁用 id 与禁用原因，选中态、禁用态和能力标签保持一致。提供可选 `variant`：默认 `list` 保持全宽纵向列表（既有 5 处使用不变）；`compact` 用网格瓦片（checkbox + 服务器名 + 状态/地址一行，`sm` 两列、`xl` 三列），用于指标设施与网络探测设施的节点范围选择，不得把紧凑模式用于需要逐台展开说明的场景。原 `ServerContextSelector` 已随资源/安全页并入服务器详情而删除，不得再新增单服务器上下文选择器；单服务器操作一律通过该服务器的详情 tabs 进入。
- `AssetFileManager`：用于应用文件和设施静态资产这两类同构的文件工作区。顶部只提供一个上传入口，弹窗内选择文本文件、普通文件或文件夹归档；文本类型显示编辑器，普通文件和归档显示文件选择控件。新建文本资产时下载文件名自动跟随引用名称，仍可单独修改，留空保存时使用引用名称；组件还统一提供文本编辑、替换、下载、删除、错误行展示和并发冲突重载；页面只通过 `AssetFileAdapter` 注入领域 API 和文案。`items[].key` 必须是应用内或设施内唯一的 `name`，不把物理 `id`、`fileKey` 或 `assetKey` 暴露给组件。
- `TemplateVariableInput`（`web/src/components/patterns/TemplateVariableInput.vue`）：用于支持模板表达式的单行字段；复用 Input 和 Dropdown，在原光标插入表达式或替换当前选区，插入后恢复输入焦点。业务页面负责传入按上下文筛选后的目录项和本地化文案，不得把后端返回的全部变量无差别暴露给不支持相应渲染阶段的字段。
- `MetricChart`（`web/src/components/patterns/MetricChart.vue`）：指标曲线图。输入 `series`（每项含 `id`/`name`/`points[{time,value}]`，缺失值保持 `null`）、`valueKind`（`percent`/`bytes`/`latency`）、可选 `thresholds` 与本地化 `label`；组件负责时间轴与单位轴、虚线网格、单序列面积、双序列配色、悬停十字 tooltip、阈值参考线，并从 `--panel-*` token 读取颜色（经 `resolveCssColor` 解析为 canvas 可用的具体颜色，兼容深色 accent `color-mix`），响应 `data-theme`/`data-accent`/`data-background`/`data-mica`。悬停只加重当前线，**不得因 `emphasis.focus` 把其它线压暗/压没**（暗色下会看起来像线段消失）。业务页面负责图例文案、平均/峰值统计、当前值与空态；缺少采样时必须传 `null` 而不是补 0。服务器概览指标区的组合见 `MetricsPanel.vue` 与 `utils/metrics.ts`。
- `ProbeHistoryCharts`（`web/src/components/patterns/ProbeHistoryCharts.vue`）：网络探测的 24h 延迟/丢包折线图卡片。输入两条 `MetricSeriesData`（延迟、丢包）与当前值，内部复用 `MetricChart`（延迟用 `latency` 单位、丢包用 `percent`），固定渲染两张卡片；没有点时卡片仍显示并给出本地化空态，不得整块隐藏。设施详情与服务器详情概览共用该组件，业务页面负责探测源/目标选择与数据加载。
- `CredentialFormFields`（`web/src/components/patterns/CredentialFormFields.vue`）：SSH 凭据表单字段的共享实现（name/type/username/password 或 privateKey+passphrase，含类型切换、编辑保留 secret、字段级错误），供凭据页与任意需要快捷创建凭据的弹窗复用；凭据校验/负载逻辑在 `web/src/components/patterns/credentialForm.ts`（空输入、校验、secret 裁剪），文案经 `labels` 从页面传入。服务器创建弹窗在无可用凭据时内嵌本组件实现“快捷创建凭据 → 自动选中”，作为死胡同的引导闭环。
- `FormField`（`web/src/components/patterns/FormField.vue`）：WinUI 3 表单字段行。Caption label + 控件 + Caption hint/error；通过默认插槽属性 `{ id, describedBy, invalid, required, disabled }` 自动接线 `<label for>`、`aria-describedby`、`aria-invalid`；支持 vertical / horizontal（设计卡式）。弹窗、编辑器、设置页表单统一使用，不再手写 label + error span。
- WinUI primitives：`NumberBox`（role=spinbutton、min/max/step、Up/Down/Home/End、±按钮）、`RadioButton`（role=radio/aria-checked/roving tabindex，外部 radiogroup 负责方向键）、`ToggleButton`（aria-pressed）、`Expander`（aria-expanded/controls、v-show 保留 DOM）、`BreadcrumbBar`（nav aria-label、祖先为普通 anchor（RouterLink custom，不自动 aria-current）、最后一项 aria-current）。详情/编辑器通过 `ConsolePage :breadcrumbs` 接入；列表页不显示面包屑。
- `DetailSection`（`DetailSection.vue`）：只读详情分区。卡外 Body Strong 标题 + 说明 + 单一 `surface-card` + `divide-y divide-divider` 行；支持 actions/footer/description 槽。需要子项各自成卡时传 `surface=false`，只渲染标题与裸容器，避免 card-in-card。实体详情统一使用，禁止 border-b 分节与 muted 当卡底。
- `PropertyList`（`PropertyList.vue`）：只读属性行（12 Caption label + 14 Body value），1/2/3 列、链接/等宽/复制、`#value` 槽；与 DetailSection 组合，替代各页手写 `<dl>`。
- `ListCommandBar`（`ListCommandBar.vue`）：列表命令栏（search/filters/actions/default 槽；32px 控件、gap-2、窄屏折叠、宽屏单行）。筛选选中态由 Tabs/ToggleButton 表达，命令栏不提供私有选中样式。
- `InfoBar`（`InfoBar.vue`）：WinUI InfoBar（4px 圆角、语义 bg/border、无阴影、p-3）；info/success=`role=status`，warning/danger=`role=alert`；可关闭。替代各页手写提示条。
- `ProgressBar`（`ProgressBar.vue`）：确定性进度条，`role=progressbar` + aria 值；4px 圆角、token 过渡、语义 tone；替代概览/详情的手写进度条。
- `DiskUsageChart`（`patterns/DiskUsageChart.vue`）：磁盘**当前值**环形图（不是历史曲线图，也不是 `ChartCard` 图表卡，用于摘要卡内联）。现为通用 `RatioRing`（`patterns/RatioRing.vue`）的磁盘专用薄包装，保持 `data-testid="disk-usage-chart"`、`tone`/`size` 与空态语义不变。输入 `percent`（`null` 表示尚无有效样本）、已格式化的 `valueText`、含百分比的本地化 `label`、`tone`（success/warning/danger/accent）与 `size`（80/96px）；内部 SVG `pathLength=100` 绘制已用比例、中心显示百分比，`role="img"` + `aria-label`，空样本时环形为空并显示 `—`。已用/可用/总量与采样时间图例由页面提供；数据只消费指标响应既有 `diskCurrent`/`latest`，不得画历史曲线或另发请求。
- `RatioRing`（`patterns/RatioRing.vue`）：通用**当前值占比环**，用于磁盘使用率、节点在线率、集群容量等“比例 + 中心数值”场景；输入 `percent`（0–100，`null` = 无有效样本、空环）、`valueText`、`label`（必含百分比）、`tone`（accent/success/warning/danger/info）、`size`（sm 56 / md 80 / lg 96）与可选 `testId`。纯 SVG、无图表库、无动画；`role="img"` + `aria-label`，颜色不是唯一信息（中心文本必须存在）。无样本时中心显示调用方传入的 `—`，不得伪造 0%；只表达当前值，不承载历史曲线。
- `OsMark`（`patterns/OsMark.vue`）：服务器发行版识别徽标。输入 `os`（`ServerOsRelease` 的 `id`/`prettyName`）与尺寸 `sm|md|lg`（28/36/44px）；由 `osMark.ts` 规范化 `os.id`（含 `arch`→`archlinux`、`rocky`→`rockylinux`、`rhel`→`redhat` 等别名），`os.id` 缺失/未知时按 `prettyName` 回退，仍无法识别时使用中性 Fluent server 图标。品牌色 + 24×24 单路径字形来自 Simple Icons（CC0 1.0）本地 vendored 数据（`osMarkData.ts`，`tmp/gen-os-marks.mjs` 生成），字形前景只取 `--panel-mark-on-brand`/`--panel-mark-on-light`。徽标只作识别：装饰 `aria-hidden`，相邻文本始终保留 OS 名称，不得用图标/颜色表达受支持或健康，不得请求外部 CDN；品牌色常量只允许出现在 `osMarkData.ts`（token guard allowlist）。
- `ArchitectureMark`（`patterns/ArchitectureMark.vue`）：机器架构芯片。输入 `architecture`，输出 CPU 芯片图形（x86/ARM/RISC-V 家族内符号）+ 可读标签（`amd64`），`title`/`aria-label` 使用完整可读形态（`amd64 (x86_64)`）；中性 `bg-muted`/`text-secondary-foreground`，不得用品牌色或状态色；`architecture` 缺失时不渲染。
- `ChartCard`（`ChartCard.vue`）：图表卡（surface-card + Body Strong 标题 + 右上当前值/操作/空态）；指标与探测图表统一使用，禁止 card-in-card。
- `SettingsSection` / `SettingsCard`：设置页唯一分区/行实现（卡外标题 + 单卡行 + Section actions/footer）；设置页与设置型表单必须复用。
- `AutoSaveStatus`（`web/src/components/patterns/AutoSaveStatus.vue`）：设置分区自动保存的只读状态指示，不承载业务逻辑。放在 `SettingsSection` 的 `#actions` 插槽（system 分区的服务器变量卡片在卡内单独一个指示）；`saving`/`pending` 显示 `ProgressRing` + 保存中文案，`saved` 用成功色并在约 2s 后回到 idle，`invalid`/`error` 用危险色；`error` 时提供重试按钮（复用 `common.retry`）。状态文本必须通过 `role=status` / `aria-live` 播报，重试按钮有可访问名称；组件不得自行弹 toast 或发请求。

## 自动刷新模式

自动刷新设置由共享 composable `web/src/composables/useAutoRefresh.ts` 统一管理（localStorage `panel.autoRefresh`，默认 `10m`），所有页面共用同一设置；控件统一使用 `web/src/components/patterns/DataRefreshControl.vue`（内部包装 `AutoRefreshControl` 并注入中英文档位文案；触发按钮显示当前状态：`关闭` / `10 分钟` / `30 分钟` / `1 小时`，弹出菜单选择并带勾选）。已接入：概览页服务器卡片、服务器详情概览指标面板、网络探测设施结果区。服务器详情指标面板只在当前 tab 激活时刷新，非激活/加载中/页面隐藏时跳过；网络探测设施结果区只重取当前探测源结果，配置表单有未保存修改时不刷新。轮询实现必须满足：防重入、标签页不可见时暂停；目标数据加载中跳过本轮；概览每次整页重取列表、指标与当前探测源结果（不拉取软件包/镜像更新统计），失败保留旧数据。旧版秒级模式（5s/10s）不再兼容，读取到旧值时回落到默认 10 分钟。其他页面接入同类自动刷新时复用 `DataRefreshControl` + `useAutoRefresh`，不另造控件。

## 设置页自动保存模式

设置页 general/security/certificates/agent/system（品牌+服务器变量）/tailscale 分区不再提供保存按钮，统一复用 `web/src/composables/useDebouncedAutoSave.ts`（延迟常量 `AUTO_SAVE_DELAY_MS = 500`）与 `AutoSaveStatus.vue`（`UI-SET-014`）：

- 触发与合并：任一表单控件修改后重置 500ms 计时器；连续修改只发最后一次；同一分区同一时刻只允许一个请求在飞，请求期间的新修改不立即补发，而是在本次响应返回后重新开始 500ms 防抖（继续输入会继续取消并顺延，停止输入满 500ms 才补发）。每个分区一个控制器，不同分区互不阻塞。
- 负载与回填：只提交本分区子资源（`PUT /api/v1/settings/runtime/{general|security|certificates|agent|branding}`、`PUT /api/v1/settings/tailscale`、`PUT /api/v1/settings/server-variables`），成功响应返回完整 `RuntimeSettings`，用其回填规范化值；回填必须抑制 watcher，防循环保存。
- 状态机：`idle | pending | saving | saved | invalid | error`；`saved` 约 2s 后回 idle；成功不弹 success toast，仅状态指示；失败 danger toast + `error` + 重试；`invalid` 只表示本地校验未通过，不发请求。
- 与动作按钮边界：JWT 重置、清除 Tailscale 认证密钥、应用/重连、系统证书重置、清空事件与日志、备份导出/还原、外观上传/移除/恢复默认仍是显式动作按钮（确认/loading/两阶段反馈沿用 [interaction-model.md](interaction-model.md)），不得改造成自动保存；外观分区保持乐观即时保存（`UI-SET-013`），不纳入 500ms 防抖。
- 生命周期：刷新按钮取消所有待发计时器并整体重载；切 tab 不卸载页面、允许在途请求完成；页面卸载时若有待发修改立即 fire-and-forget 发送一次；卸载后不得再改状态；迟到响应不得覆盖更新的本地输入。
- 语言：语言下拉改变时立即 `setLocale` 并进入 general 防抖保存；持久化失败回滚界面语言到服务端已持久化的 `runtime.language`，不得产生循环保存。
- 可访问性：`AutoSaveStatus` 的保存中/已保存/失败/待修正文本经 `role=status` / `aria-live="polite"` 播报；错误状态的重试按钮必须有本地化可访问名称；不得只靠颜色表达保存状态。

## 使用规则

1. 页面内不得为同类控件重新手写尺寸、圆角、选中态、禁用态或 loading 样式；缺能力时先扩展上述组件。
2. 组件只承载通用交互与视觉，不直接绑定业务 API、路由或 i18n key。业务文案从页面传入，并按 i18n 规范维护；新增页面替换时不得依赖组件内英文默认值。
3. `StatusBadge` 的 tone map 是默认规则；业务状态显示文案仍由页面翻译或格式化后传入 `label`。
4. 上传、下载、确认对话框只负责交互入口；请求、错误摘要、任务入口和两阶段反馈由业务页面按照 [interaction-model.md](interaction-model.md) 实现。
5. 应用文件与设施静态资产如果具备相同的上传、替换、下载、删除和文本编辑行为，必须复用 `AssetFileManager`，不得在页面内复制另一套文件行和弹窗；领域差异只能放在 adapter/API 中。
6. 中大屏布局中，表格与覆盖式详情必须各自在模板提供的内部滚动区域内完成滚动，不得恢复页面级滚动；宽表格允许在表格容器内横向滚动。
7. 实体工作台（servers/credentials/dns/certificates/applications）的列表与详情切换必须复用 `TableDetailLayout`：列表保持挂载（`inert` + `aria-hidden`），详情的选中参数与 tab 参数进 URL，打开详情 `push` 历史、关闭 `replace` 清参数；页面不得自建第二套切换壳。
8. 操作/任务与其它实体工作台使用 `TableDetailLayout`；日志使用 `ListPage` + Dialog；不得再接入 `MasterDetailLayout` 交付页，也不得新增“紧凑视口自动选中首条”逻辑。
9. 页面内分段切换（如证书的域名/自签/密钥）使用带 `aria-current` 的链接或 `Tabs`，不得用一排 `primary` 按钮表达选中态；页面内搜索一律复用 `SearchInput`，不得手写放大镜 + Input 组合；下拉内部的候选过滤/远程搜索使用 `Select` 的可选搜索模式，不在下拉外另造搜索框。
10. 设置页：单页 + 顶部固定 `Tabs`（`settings/:section`、`/settings` 重定向 general、`pageKey='settings'`），切分区不重载、不重播转场、不重拉全量设置；分区内容为 `SettingsSection` + `SettingsCard` 单列卡行，内容区独立滚动；卡内空态用 `variant="inset"`；每个分区没有保存按钮，Section actions 只放 `AutoSaveStatus` 与保留的动作按钮（见“设置页自动保存模式”），不再使用空 Save 行。
11. 列表筛选：宽屏用单行 `ListCommandBar`（搜索 + 1–2 个下拉 + 「筛选」Flyout/Dropdown），窄屏折叠；不得用一排 primary 按钮表达筛选选中态，也不得把多个长文本搜索框硬塞一行。

## 当前接入记录

- `web/src/views/applications/ApplicationLogsDialog.vue`：业务容器日志弹窗复用 Dialog/Select/Button/LoadingOverlay；按服务器与容器展示实例，日志在固定弹窗正文内滚动，切换/关闭取消旧请求，不使用活动日志替代容器输出。

- `web/src/views/applications/index.vue`：应用搜索、状态、应用/设施连续纵向配置流、部署/网关/源站服务器多选、持久化数据下载/恢复，以及应用文件和设施静态资产共用的 `AssetFileManager` 已接入统一 primitives/patterns。 未保存修改的离开/取消保护已接入 `ConfirmDialog`。应用列表为表格 + 覆盖式详情，行操作区承载 详情/编辑/同步/停用/更新镜像/日志/删除，详情页头含编辑与 `ActivityLink`，详情内保留 runtime/routes/files tabs 与持久化数据上下文面板。
- `web/src/views/servers/index.vue` 与 `tabs/`：服务器列表为表格 + 覆盖式详情；详情页头含编辑与重启等运维操作；详情**只有一层 tab**，一级 tab 为 概览/系统/资源/网络与安全，概览合并状态条与 `MetricChart` 指标区；概览摘要卡用 `OsMark`/`ArchitectureMark` 标识发行版与架构（相邻文本保留 `prettyName` 与 `amd64 (x86_64)`）、`DiskUsageChart` 展示磁盘当前值，状态摘要为图标化紧凑瓦片，<640px 单列并恢复页面级滚动、≥1024px 仍由详情正文内部滚动；**CPU/内存/磁盘 IO/网络与延迟/丢包的当前值与历史只由下方指标与探测面板展示，摘要卡不重复这些文字摘要**；系统内 Agent/Tailscale/软件包、资源内 容器/镜像/网络/存储卷、网络与安全内 防火墙/Fail2Ban 均用 tab 内的**下拉选择器**切换（不再嵌套 Tabs）；网络探测子视图已移除，探测结果只出现在设施页与概览面板；tab/子视图懒加载并在详情存活期内缓存，旧一级 tab 名映射到新 tab + subtab；资源页与安全页的独立实现已删除。凭据/DNS 域名覆盖式详情页头同样提供编辑入口（与行内编辑同一表单）。
- `web/src/views/overview/index.vue`：概览顶部为**开放状态总览头**（不套 `surface-card`，直接位于页面层）：节点情况组用 `RatioRing` + 胶囊 `StatChip` 展示在线率与节点计数；应用组用总数 + 胶囊 KPI + 互斥启用/停用分布条；容量组用三个 `sm` 尺寸 `RatioRing` 展示在线节点 CPU 均值/内存/磁盘比例（无样本 `—` + 空环）。三组以留白和非对称栅格组织（`wide` 两列且容量组整行、`wider` 三列、`<wide` 单列），不画通长分隔线；服务器卡片网格保持既有卡片语言，`UI-OVR-016/017` 不变。
- `web/src/components/templates/TableDetailLayout.vue`：servers、credentials、dns、certificates、applications、operations、tasks 通过 `detail-swap` 接入覆盖式详情进场；列表在详情打开时 `inert` + `aria-hidden`，关闭后保留滚动位置与筛选。`/logs` 使用 `ListPage` + Dialog，不走覆盖式详情。

- 产品 UI 不得再使用 EventTimeline / context / evidence。日志页只按需读取 events 列表，默认 `level=info,warning,error`，手动刷新与 cursor 分页。

- `ActivityLink` 与 Toast 统一进入 `/logs?...&level=info,warning,error`；应用/设施协调入口进入 `/tasks`；用户意图列表为 `/operations`。

- `web/src/views/settings/index.vue`：分区表单统一接入 `useDebouncedAutoSave` + `AutoSaveStatus`；general/security/certificates/agent/system/tailscale 修改后 500ms 自动保存且只提交本分区；保留 JWT 重置、清除 Tailscale 认证密钥、应用/重连、系统证书重置、清空事件与日志、备份导出/还原等动作按钮；外观分区仍用乐观即时保存。
