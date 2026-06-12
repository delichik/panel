# 日志

## 任务日志组件

`TaskLogPanel.vue` 是任务详情的共享日志展示组件。

### Props

| 属性 | 类型 | 说明 |
| --- | --- | --- |
| `taskId` | `string` | 任务 ID |
| `compact` | `boolean` | 使用较小日志高度 |
| `serverName` | `string` | 可选服务器显示名 |

### Events

- `updated`：任务轮询更新。
- `finished`：任务首次进入结束状态。

## 轮询行为

- taskId 变化时重置日志、游标和完成状态。
- 每 `2500ms` 获取任务状态和增量日志。
- 任务完成、失败或取消后停止轮询。
- 组件卸载时清理计时器。
- 加载失败显示 error tonal Alert，不清空已有日志。

## 任务摘要

- 标题显示任务类型。
- 状态使用小型 Chip。
- 元数据允许换行，包括服务器、类型、阶段、开始和结束时间。
- 进度已知时显示百分比，未知且运行中时使用 indeterminate。

## 日志容器

- 背景使用 `--lp-log-background`。
- 主文本使用 `--lp-log-text`。
- 时间、流和空提示使用 `--lp-log-muted`。
- stderr 使用 `--lp-log-error`。
- 边框使用 `--lp-border`。
- 圆角使用 `--lp-radius-md`。
- 等宽字体，字号 `12px`。
- 默认高度 `180px` 至 `360px`；compact 最大 `240px`。
- 只在日志框内部纵向滚动。
- 日志作为页面主内容时，中大屏应填满父级剩余高度，并由日志框吸收全部增量内容滚动，不能推动页面整体增长。

## 日志行

桌面三列：

```css
grid-template-columns: 86px 64px minmax(0, 1fr);
```

依次为时间、流、正文。正文使用 `white-space: pre-wrap` 和 `overflow-wrap: anywhere`。

`560px` 以下改为单列，每条日志使用弱分隔线，字号降为 `11px`。

## 应用日志控制区

应用日志虽为业务组件，但其控制区遵循通用日志模式：

- 日志容器上方放紧凑筛选控件。
- 控件使用 compact outlined。
- 加载按钮使用 primary flat。
- `760px` 以下控制区转为单列。

## 安全与可读性

- 日志按纯文本渲染，不解释 HTML。
- 保留换行和空格，但允许长 token 换行。
- 不用正文颜色表达唯一状态。
- 大量日志必须增量加载或限制条数，不能一次渲染无限数据。

## 禁忌

- 不使用普通白色卡片背景展示终端日志。
- 不让日志框撑高整个页面而失去内部滚动。
- 不让日志摘要、控制区和日志正文共同触发整页滚动；固定区与可滚动日志区必须分轨布局。
- 不在多个页面复制轮询与游标逻辑。
- 不在任务结束后继续无意义轮询。

## 源码依据

- `web/src/components/tasks/TaskLogPanel.vue`
- `web/src/features/tasks/pages/TaskCenterPage.vue`
- `web/src/features/applications/components/ApplicationLogsPanel.vue`
