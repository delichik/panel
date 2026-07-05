# 设计基础

## 技术基础

- 组件框架：Vuetify 4。
- 图标：Material Design Icons，使用 `mdi-*`。
- 字体：Aptos、Segoe UI Variable、Segoe UI、系统无衬线字体。
- 等宽字体：Cascadia Code、SFMono-Regular、Consolas。
- 主题：`light` 和 `dark`，由 Vuetify 主题和 `data-theme` 同步控制；用户可选择自动跟随系统或手动固定模式。

## 设计方向

界面以紧凑、清晰、低焦虑的运维工作台为目标。全局框架、选择器、表格和卡片采用安静的控制台表面：弱渐变、细边框、克制状态轨和低饱和网格背景；主色只用于当前选择、主要操作、状态轨和进度强调，不使用高饱和大面积背景或强刺激纹理。

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

默认使用绿色预设：浅色主题为深青绿，深色主题为亮青绿。用户可让明暗主题共用一个预设，或分别选择预设；配置只保存在当前浏览器。组件必须通过 Vuetify 主题变量或 `color` 属性取色，不能缓存或复制预设色值。

## 主题偏好

- 未保存偏好时使用 `system` 模式，根据 `prefers-color-scheme` 解析实际的 `light` 或 `dark`。
- 手动选择 `light` 或 `dark` 后，系统外观变化不覆盖用户选择。
- 自动模式必须监听系统外观变化并即时同步 Vuetify 主题、根节点 `data-theme` 和 `color-scheme`。
- 主题提供蓝、绿、红、橘、紫、粉、黄七套适配当前低饱和控制台风格的预设，不提供任意颜色输入。
- 每套预设分别定义浅色和深色变体，并同步覆盖 `primary`、`on-primary` 与 `surface-variant`；`on-primary` 必须满足正文对比度，`surface-variant` 只使用轻微同色倾向。
- 无效或损坏的本地配置回退到绿色默认预设。
- 旧版只保存 `light` / `dark` 的浏览器配置迁移为对应手动模式；曾保存任意颜色值的配置迁移为绿色预设。
- 主题设置属于浏览器级 UI 偏好，不写入后端运行时设置或账号信息。

## 排版

- 页面标题由应用布局提供，约 `1.28rem`、高字重。
- 卡片标题通常使用 `text-subtitle-1` 或约 `1rem` 的高字重文本。
- 分区标题使用 `.section-title`，小号、高字重、通常大写，用于长表单或详情区分组。
- 元数据和表格辅助文本使用 `0.76rem` 至 `0.82rem`。
- 数值使用 `.font-tabular`，避免刷新时宽度跳动。
- 路径、哈希、命令和日志使用 `.mono`、`.font-mono` 或 `.app-mono`。

## 边框与阴影

- 普通表面：`1px solid var(--lp-border)` 加 `--lp-shadow-sm`，可叠加来自 `--lp-surface-container` 的弱渐变以区分页级工作面与内部内容。
- 可交互卡片悬浮：轻微提高边框主色和 `--lp-shadow-md`。
- 不应在静态信息卡片上叠加夸张阴影。
- 同一容器内的内嵌信息块通常只有边框和弱表面背景，不再叠加阴影。

## 动效

- 常规颜色、边框、阴影和位移过渡为 `160ms` 至 `250ms`。
- 全局 `main.css` 统一覆盖 Vuetify 常用显示/隐藏动效，包括 dialog、menu/tooltip 的 scale/fade、snackbar、message、slide 和 expand transition；新增业务页面优先复用这些默认过渡，不要为同类浮层另写独立时长曲线。
- fade transition 只统一透明起点和时长，不得把显示终点强制设为 `opacity: 1`；对话框遮罩等组件需要保留 Vuetify 定义的目标透明度，避免打开时先变黑再回落。
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
