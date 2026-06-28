# 按钮

## 基础

统一使用 Vuetify `v-btn`。全局圆角为 `--lp-radius-sm`，按钮具有短过渡、悬浮轻微上移和按下反馈。
用户可见文本按钮通常添加 `class="text-none"`，避免英文文案自动大写。

## 操作层级

| 层级 | 建议写法 | 用途 |
| --- | --- | --- |
| 主要操作 | `color="primary" variant="flat"` | 新建、保存、执行页面主动作 |
| 次要操作 | `variant="outlined"` | 编辑、刷新、切换、辅助动作 |
| 低强调操作 | `variant="text"` | 取消、关闭、工具按钮 |
| 破坏性主要操作 | `color="error" variant="flat"` | 已确认的删除或不可逆动作 |
| 破坏性次要操作 | `color="error" variant="outlined"` | 表格行删除、进入确认流 |
| 风险操作 | `color="warning" variant="outlined"` | 重建、重置等高风险但非删除动作 |

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

## 状态

- 异步操作使用 `:loading`，避免另加重复进度指示。
- 不可执行时使用 `:disabled`，并在附近提供原因。
- 刷新已有内容时，按钮 loading 不应遮挡当前内容。
- 主要按钮焦点由全局 `focus-visible` 环提供，不移除键盘焦点样式。

## 操作区布局

- 使用 `.page-actions`、`.page-toolbar-actions`、`.app-table-actions` 或局部 flex 容器。
- 操作区必须允许 `flex-wrap: wrap`。
- 表格操作靠右，间距约 `6px`。
- `760px` 以下页面工具栏按钮通常占满宽度。
- 设置页的保存按钮位于所属 section 底部并左对齐；存在多个 section 时不使用页头全局保存。

## 禁忌

- 不使用颜色区分两个同等主要操作。
- 删除入口不直接采用无确认的 `error flat`，除非操作本身可立即撤销。
- 不用只有颜色、没有文本或图标语义的按钮。
- 不在同一区域混用多套圆角、阴影和按钮高度。

## 源码依据

- `web/src/styles/main.css`
- `web/src/layouts/AppLayout.vue`
- `web/src/views/servers/_shared/ServersPageContent.vue`
- `web/src/views/applications/apps/index.vue`
