# 按钮

## 基础

统一使用 Vuetify `v-btn`。全局圆角为 `--lp-radius-sm`，按钮具有短过渡、悬浮轻微上移和按下反馈。
用户可见文本按钮通常添加 `class="text-none"`，避免英文文案自动大写。

跨页面业务操作优先使用 `AppActionButton.vue`，并用 `AppActionGroup.vue` 组织操作区。页面不应直接复制 `v-btn` 的颜色、圆角、gap 或图标按钮规则；只有 `v-btn-toggle` 内部选项和少数 Vuetify 专用控件可以直接使用 `v-btn`。

## 操作层级

| 层级 | 建议写法 | 用途 |
| --- | --- | --- |
| 主要操作 | `color="primary" variant="flat"` | 新建、保存、执行页面主动作 |
| 次要操作 | `variant="outlined"` | 编辑、刷新、切换、辅助动作 |
| 低强调操作 | `variant="text"` | 取消、关闭、工具按钮 |
| 破坏性主要操作 | `color="error" variant="flat"` | 已确认的删除或不可逆动作 |
| 破坏性次要操作 | `color="error" variant="outlined"` | 表格行删除、进入确认流 |
| 风险操作 | `color="warning" variant="outlined"` | 重建、重置等高风险但非删除动作 |
| Snackbar 操作 | `AppActionButton kind="snackbar"` | Snackbar 中的关闭、查看任务、下载等动作 |

同一操作区通常只放一个主要按钮。

## 尺寸

- 页面级主要操作使用默认尺寸并可添加 `.action-btn`，最小高度 `40px`。
- 表格行操作使用 `size="small"`。
- 工具栏纯图标按钮使用 `icon`、`size="small"`、`variant="text"`。
- 对话框操作按钮最小宽度为 `92px`，窄屏时改为整行。

## 图标

- 新建：`mdi-plus` 或业务明确的新增图标。
- 编辑：`mdi-pencil`。
- 删除：`mdi-delete`。
- 刷新：`mdi-refresh`。
- 关闭：`mdi-close`。
- 图标按钮必须提供 `aria-label` 或可见 `title`。
- 文本按钮可使用 `prepend-icon`，避免同时使用多个装饰图标。
- `AppActionButton kind="tool"` 是纯图标按钮的唯一常规入口，只用于关闭/更多/拖拽手柄/输入框附属工具/选择器标题区新增等空间受限工具位，必须传入 `label`。
- 对象操作、行操作、刷新、保存、同步、启用、编辑、删除默认使用文字按钮加图标，不要为了省宽度改成裸图标。

## 状态

- 异步操作使用 `:loading`，避免另加重复进度指示。
- 不可执行时使用 `:disabled`，并在附近提供原因。
- 刷新已有内容时，按钮 loading 不应遮挡当前内容。
- 主要按钮焦点由全局 `focus-visible` 环提供，不移除键盘焦点样式。

## 操作区布局

- 页面、详情、工具栏、表格、section、选择器、筛选、对话框、Snackbar 和行内操作优先使用 `AppActionGroup`，通过 `context="page|toolbar|detail|table|section|selector|filter|dialog|snackbar|inline"` 表达位置语义。
- 操作区必须允许 `flex-wrap: wrap`，窄屏需要整列时使用 `mobile-stack`，不要在页面里重新写一套按钮 flex。
- 表格操作靠右，间距约 `6px`。
- 表格或摘要列表行操作使用 `AppActionGroup context="table"`，行尾显示“编辑 / 删除 / 日志”等文字小按钮；除非该行本身位于 dialog 内，不使用纯图标删除。
- 页面级和详情级对象操作放在标题区右侧；设置页等长表单的保存按钮可以放在所属 section 底部，但仍使用 `AppActionGroup context="section"`。
- 对话框底部使用 `AppActionGroup context="dialog"`，取消为 `kind="plain"`，确认/保存为 `kind="primary"`，危险确认使用 `kind="danger-primary"`，风险确认使用 `kind="warning-primary"`。
- Snackbar 操作使用 `AppActionGroup context="snackbar"` 和 `AppActionButton kind="snackbar"`，不要在各页面手写白色 text 按钮。
- `760px` 以下页面工具栏按钮通常占满宽度。
- 设置页的保存按钮位于所属 section 底部并左对齐；存在多个 section 时不使用页头全局保存。

## 禁忌

- 不使用颜色区分两个同等主要操作。
- 删除入口不直接采用无确认的 `error flat`，除非操作本身可立即撤销。
- 不用只有颜色、没有文本或图标语义的按钮。
- 不在同一区域混用多套圆角、阴影和按钮高度。
- 不在普通页面正文底部另放对象主操作；对象级同步、保存、删除、编辑等应进入页面或详情标题区，表单提交和 dialog footer 例外。

## 源码依据

- `web/src/styles/main.css`
- `web/src/layouts/AppLayout.vue`
- `web/src/views/servers/_shared/ServersPageContent.vue`
- `web/src/views/applications/apps/index.vue`
