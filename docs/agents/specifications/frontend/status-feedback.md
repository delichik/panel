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

同一业务状态在列表、详情、对话框和日志中必须保持同一颜色映射。

## Chip

状态和短分类使用 `v-chip`：

- 常用 `size="small"` 或表格中的 `x-small`。
- 状态标签优先 `variant="tonal" label`。
- 数量可使用 `primary tonal`。
- Chip 文案应短，不放完整错误信息。
- 多个 Chip 允许换行并保持 `6px` 左右间距。

## 状态点

紧凑选择列表可使用 `.status-dot`：

- 直径 `8px` 至 `9px`。
- 圆形。
- `success/warning/error` 取主题色。
- 可用淡色外环增强识别。
- 状态点必须配合文本或上下文，不能作为唯一信息载体。

## Alert

使用 `v-alert variant="tonal"`：

- 页面加载或操作失败：`type="error"`。
- 可继续但需注意：`type="warning"`。
- 流程说明、任务已创建：`type="info"`。
- 成功状态通常使用 Snackbar；只有需要持续可见时才使用 success Alert。
- 局部 Alert 放在相关卡片或表单内部，页面级 Alert 放在 `.page-shell` 顶部。
- 紧凑区域使用 `density="compact"`。

## Snackbar

用于操作完成后的短暂反馈：

- 默认 timeout `3000ms`，需要阅读更多内容时可为 `5000ms`。
- 颜色跟随结果语义。
- 可提供“查看任务”或“关闭”动作。
- 不承载需要确认、填写或长期保留的信息。

## 进度

- 已知百分比使用 `v-progress-linear :model-value`。
- 未知时长使用 `indeterminate`。
- 任务完成为 success，失败为 error，运行中为 primary。
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

- 不仅依靠颜色区分状态。
- 不把 warning 用作所有非成功状态的默认色。
- 不用 Snackbar 展示不可消失的系统故障。
- 不同时显示同一结果的 Alert 和 Snackbar。

## 源码依据

- `web/src/styles/main.css`
- `web/src/components/tasks/TaskLogPanel.vue`
- `web/src/features/tasks/pages/TaskCenterPage.vue`
- `web/src/features/nomad/pages/NomadNodesPage.vue`

