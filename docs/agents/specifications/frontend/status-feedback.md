# 状态与反馈

## 语义映射

| 语义 | 颜色 |
| --- | --- |
| 正常、完成、在线 | `success` |
| 进行中、主要活动 | `primary` |
| 排队、中性进度、信息 | `info` 或 `warning`，按业务含义选择 |
| 风险、降级、待处理 | `warning` |
| 失败、错误、危险 | `error` |
| 中性分类 | `secondary` 或无显式颜色 |

同一业务状态在列表、详情、对话框和日志中保持相同颜色映射。

## Chip

- 状态和短分类使用 `v-chip`。
- 常用 `size="small"`，表格中可使用 `x-small`。
- 状态标签优先 `variant="tonal" label`。
- Chip 文案应简短，不放完整错误信息。
- 多个 Chip 允许换行并保持约 `6px` 间距。

## 状态点

紧凑选择列表可使用 `.status-dot`：

- 直径 `8px` 至 `9px`。
- 使用圆形和对应语义色。
- 必须配合文本或上下文，不能作为唯一信息载体。

## Alert

使用 `v-alert variant="tonal"`：

- 页面加载或操作失败：`type="error"`。
- 可继续但需注意：`type="warning"`。
- 流程说明或任务已创建：`type="info"`。
- 成功状态通常使用 Snackbar；只有需要持续可见时使用 success Alert。
- 局部 Alert 放在相关卡片或表单内部，页面级 Alert 放在 `.page-shell` 顶部。
- 紧凑区域使用 `density="compact"`。
- 页面级 Alert 始终按内容高度展示，不得使用 `flex: 1`、固定高度或参与剩余视口空间分配。
- 非阻断性的辅助说明、安全提醒和使用提示不得横向撑满页面或满宽卡片；优先放到实际相关的字段、按钮、表格操作或确认对话框附近。确需常驻展示时，使用内容宽度容器（`width: fit-content`，并设置合理 `max-width`），不能仅因父容器可用空间较大而拉伸。
- 只有影响整个页面的加载错误、阻断性警告或全局状态才允许使用撑满内容区的页面级 Alert。
- 主从详情、卡片或面板顶部的局部 Alert 也必须按内容高度展示；如果父容器是 flex 或满高卡片，必须显式使用 `flex: 0 0 auto`，不能让 Alert 吸收剩余高度。
- 详情页存在多个错误来源时，先归并为一个最具体、最可行动的错误提示；不要在同一可视区域同时渲染语义相同的 Alert、tip 或状态横幅。
- Alert 内存在操作按钮时允许紧凑换行，不默认让按钮占满整行。

## Snackbar

- 默认 timeout 为 `3000ms`，需要阅读更多内容时可使用 `5000ms`。
- 颜色跟随结果语义。
- 可提供“查看任务”或“关闭”动作。
- 不承载需要确认、填写或长期保留的信息。

## 进度

- 已知百分比使用 `v-progress-linear :model-value`。
- 未知时长使用 `indeterminate`。
- 完成为 success，失败为 error，运行中为 primary。
- 表格中的进度条高度约 `6px` 至 `8px`。
- 详情中的主进度可达到 `18px` 并显示百分比。

## 加载与反馈边界

- 初次加载使用 `PageLoadingState`。
- 已有内容刷新使用卡片 `:loading`、按钮 `:loading` 或局部进度。
- 不在已有内容上覆盖全页加载状态。

## 文案

- 所有状态和反馈文案必须本地化。
- 错误优先给出用户可行动的信息。
- 不直接暴露 JSON 解析错误、堆栈或底层异常格式。

## 禁忌

- 不仅依赖颜色区分状态。
- 不把 warning 用作所有非成功状态的默认色。
- 不用 Snackbar 展示不可消失的系统故障。
- 不同时显示同一结果的 Alert 和 Snackbar。
- 不在同一个详情面板内重复显示同一故障的概括错误和底层错误；需要展示底层原因时应替换概括错误，而不是额外堆一条提示。
- 不让提示横幅拉伸填充页面剩余高度。
- 不让一两句辅助说明以满宽 Alert 形式占据整行或整张卡片。

## 源码依据

- `web/src/styles/main.css`
- `web/src/components/tasks/TaskLogPanel.vue`
- `web/src/views/tasks/index.vue`
- `web/src/views/applications/apps/ApplicationRuntimePanel.vue`
