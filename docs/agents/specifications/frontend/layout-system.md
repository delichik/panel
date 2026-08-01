# v4 布局体系规范

## 总体结构

```
AppShell
├── Sidebar（桌面常驻，260px / 折叠 76px）
└── Main
    ├── GlobalHeader（56px）
    └── RouteContent（overflow: hidden）
        └── PageHeader + 页面模板内部滚动区
```

登录和维护启动页不使用全局 AppShell。

## AppShell

- 桌面 `>=1024px`：`h-dvh`、`overflow-hidden`，左侧导航固定，顶部条固定，路由内容区 `overflow-hidden`。
- 窄屏 `<1024px`：左侧导航变自有抽屉，body 恢复页面级滚动。
- 导航定义在 `web/src/components/shell/navModel.ts`；新增导航项必须同步 i18n 和模块文档。
- 侧边导航只放页面族入口，不展开页面内部 tab：例如安全只保留“安全”，资源只保留“资源”，证书只保留“证书”，应用只保留“应用”。`/security/firewall`、`/resources/containers`、`/certificates/keys` 等深链路继续由路由支持并在页面内切换 tab，但不得作为独立侧边菜单项。
- 全局顶部条只承载当前路由标题、alpha 标识、主题、语言、账户和移动抽屉入口。页面主操作放 PageHeader actions，对象级操作放详情区。

## PageHeader

- 源码：`web/src/components/shell/PageHeader.vue`。
- 统一结构：标题、说明、可选 actions、可选 breadcrumb。
- actions 区主操作至多一个，其余操作用次要按钮或 Dropdown。
- 中窄屏下标题区与 actions 区上下堆叠，actions 可换行，不得裁切关键操作。

## Table

- 源码：`web/src/components/ui/Table.vue`。
- `Table` 根容器自身承担滚动；页面可通过 `h-*` / `max-h-*` 约束高度，表头在该滚动区内 sticky。
- 根容器保留圆角和边框，不得在外层限高后再依赖内层百分比高度建立滚动区。

## 页面模板

模板位于 `web/src/components/templates/`，只负责结构、间距、滚动和断点折叠，不含业务逻辑。

- `DashboardPage`：指标、图表、风险队列等概览。
- `ListPage`：工具栏 + 表格内部滚动 + 固定分页。
- `MasterDetailPage`：左选择、右详情，适合服务器、DNS、证书等对象管理。
- `MasterDetailLayout`：一级对象选择工作台的轻量双栏几何。默认单列，`xl` 及以上使用 `360px minmax(0, 1fr)`，统一 `gap-4`、两侧 `min-width/min-height: 0` 和横向溢出保护；页面通过 `master` / `detail` named slots 保留自己的边框、背景、padding 与业务滚动。不得通过页面 class 或公共 API覆盖左栏宽度。settings 分区导航、任务详情内层 `280px` 列表和 AppShell 导航不适用。
- `EditorPage`：复杂创建/编辑，正文内部滚动，底部固定提交栏。
- `SettingsPage`：分区短表单，最大宽度收敛，分区独立保存。
- `WorkspacePage`：诊断、日志、终端等满高工作面。
- `ConsolePage`：PageHeader + 内部滚动正文的基础容器，可被上面模板组合。正文容器必须 `min-width: 0`，桌面禁止横向滚动兜底；页面内部 grid、按钮和侧栏需要自行用 `minmax(0, 1fr)`、`min-w-0`、`overflow-hidden` 和断点换列处理宽度。

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
