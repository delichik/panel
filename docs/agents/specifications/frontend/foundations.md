# 设计基础

## 技术基础

- 组件框架：Vuetify 4。
- 图标：Material Design Icons，使用 `mdi-*`。
- 字体：Aptos、Segoe UI Variable、Segoe UI、系统无衬线字体。
- 等宽字体：Cascadia Code、SFMono-Regular、Consolas。
- 主题：`light` 和 `dark`，由 Vuetify 主题和 `data-theme` 同步控制。

## 设计方向

界面以紧凑、清晰的运维控制台为目标。主要表面使用低对比边框和轻阴影，不使用高饱和大面积背景；主色只用于当前选择、主要操作和进度强调。

## 全局变量

优先使用 `web/src/styles/main.css` 中的变量，禁止在业务组件中重新定义同义颜色。

| 类型 | 变量 | 当前值或用途 |
| --- | --- | --- |
| 圆角 | `--lp-radius-sm` | `8px`，按钮、输入框、普通卡片、内部块 |
| 圆角 | `--lp-radius-md` | `10px`，面板、对话框、日志框 |
| 圆角 | `--lp-radius-lg` | `12px`，较大展示容器 |
| 间距 | `--lp-space-1` 至 `--lp-space-6` | `8/12/16/20/24/32px` |
| 页面背景 | `--lp-background` | 应用主背景 |
| 表面 | `--lp-surface` | 卡片、抽屉、对话框 |
| 次级表面 | `--lp-surface-container` | 内嵌区块、分页背景 |
| 弱表面 | `--lp-surface-muted` | 骨架、弱强调背景 |
| 边框 | `--lp-border` | 所有结构边界 |
| 文本 | `--lp-text` | 主文本 |
| 文本 | `--lp-text-muted` | 说明、标签、元数据 |
| 文本 | `--lp-text-subtle` | 更弱的辅助信息 |
| 阴影 | `--lp-shadow-sm/md/dialog` | 普通、悬浮、对话框层级 |

## 语义色

使用 Vuetify 主题色，不直接写固定色值：

- `primary`：主要操作、选中项、进行中状态。
- `success`：成功、在线、健康。
- `warning`：风险、待处理、降级、需要注意。
- `error`：错误、失败、删除等破坏性动作。
- `info`：中性通知、信息状态。
- `secondary`：次级分类或中性标签。

浅色主题主色为深青绿，深色主题主色为亮青绿。组件应通过 `rgb(var(--v-theme-primary))` 或 Vuetify `color` 属性取色。

## 排版

- 页面标题由应用布局提供，约 `1.28rem`、高字重。
- 卡片标题通常使用 `text-subtitle-1` 或约 `1rem` 的高字重文本。
- 分区标题使用 `.section-title`，小号、高字重、通常大写，用于长表单或详情区分组。
- 元数据和表格辅助文本使用 `0.76rem` 至 `0.82rem`。
- 数值使用 `.font-tabular`，避免刷新时宽度跳动。
- 路径、哈希、命令和日志使用 `.mono`、`.font-mono` 或 `.app-mono`。

## 边框与阴影

- 普通表面：`1px solid var(--lp-border)` 加 `--lp-shadow-sm`。
- 可交互卡片悬浮：轻微提高边框主色和 `--lp-shadow-md`。
- 不应在静态信息卡片上叠加夸张阴影。
- 同一容器内的内嵌信息块通常只有边框和弱表面背景，不再叠加阴影。

## 动效

- 常规颜色、边框、阴影和位移过渡为 `160ms` 至 `250ms`。
- 全局 `main.css` 统一覆盖 Vuetify 常用显示/隐藏动效，包括 dialog、menu/tooltip 的 scale/fade、snackbar、message、slide 和 expand transition；新增业务页面优先复用这些默认过渡，不要为同类浮层另写独立时长曲线。
- 主要按钮悬浮上移 `1px`，按下下移 `1px`。
- 主题切换由 `.theme-changing` 统一添加短过渡。
- 必须尊重 `prefers-reduced-motion: reduce`，关闭非必要动画。

## 约束

- 不在业务页面写独立的 light/dark 固定颜色组。
- 不用纯黑作为普通文本，不用纯白作为普通页面背景。
- 不新增与现有 `8/10/12px` 接近但无明确意义的圆角值。
- 外部媒体必须受 `max-width: 100%` 约束。
- 页面最小支持宽度为 `320px`。

## 源码依据

- `web/src/main.ts`
- `web/src/theme.ts`
- `web/src/styles/main.css`
