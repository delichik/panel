# 加载与空状态

## 首次加载组件

首次网络加载统一使用 `PageLoadingState.vue`。

### Props

| 属性 | 类型 | 默认值 | 用途 |
| --- | --- | --- | --- |
| `message` | `string` | `common.loading` | 自定义本地化加载说明 |
| `minHeight` | `string` | `260px` | 匹配目标内容区域高度 |
| `compact` | `boolean` | `false` | 用于左侧选择器等窄容器，缩小 spinner、间距和骨架 |

组件包含：

- 主色圆形进度。
- 加载文案。
- 三行 shimmer 骨架。
- `role="status"` 和 `aria-live="polite"`。
- reduced-motion 下关闭 shimmer 动画。

## 使用条件

```html
<PageLoadingState v-if="loading && items.length === 0" min-height="320px" />
```

- 只在没有可展示旧内容时显示。
- 列表刷新且旧内容仍存在时保留内容，改用卡片或按钮 loading。
- 切换服务器、应用、域名、任务等上下文后，异步子详情必须立即清空旧内容并显示 `PageLoadingState` 或等价局部加载遮罩。
- 上下文型异步请求必须记录请求时的上下文 ID，并在响应返回时忽略已经不匹配当前上下文的迟到响应。
- 组件高度应接近加载完成后的区域，减少布局跳动。
- 骨架宽度必须相对当前容器计算；禁止使用 `vw`，避免在窄卡片中横向溢出。

常用高度：

- 小卡片或子区：`220px`。
- 普通页面区：`260px` 到 `320px`。
- 主工作区：`340px` 到 `360px`。

## 空状态

全局模式包括：

- `.empty-state`
- `.empty-detail`
- `.empty-panel`

默认设计：

- grid 居中。
- 最小高度约 `220px`。
- `32px` 内边距。
- 文本居中。
- 使用弱文本色。
- 可包含一个 `40px` 左右的 MDI 图标。

## 空状态类型

### 数据为空

说明当前没有数据，并在用户可创建时提供一个主要操作。

### 未选择详情

主从布局右侧使用 `.empty-detail`，提示先从左侧选择，不显示创建按钮除非创建是该区域的主任务。

### 前置条件不足

使用 info 或 warning Alert 解释缺少服务器、域名或凭据等条件；不要伪装成普通空数据。

### 筛选无结果

说明筛选条件没有匹配项，并提供清除筛选动作；不要误导用户认为系统没有任何数据。

## 加载、错误、空数据优先级

建议判断顺序：

1. 初次加载。
2. 加载错误。
3. 有数据。
4. 空状态。

已有数据刷新失败时保留数据，并显示局部错误提示。

切换上下文加载详情时不保留旧详情；清空内容后按“加载、错误、空数据”顺序展示，避免用户误以为旧数据属于新选择项。

## 禁忌

- 不在加载中同时显示“暂无数据”。
- 不为每个页面重新实现 spinner 和骨架。
- 不让空状态只有图标而没有说明。
- 不在纯展示空状态中放多个竞争操作。
- 不让旧上下文的异步响应覆盖当前选择项。

## 源码依据

- `web/src/components/PageLoadingState.vue`
- `web/src/styles/main.css`
- `web/src/views/servers/_shared/ServersPageContent.vue`
- `web/src/views/security/firewall/index.vue`
- `web/src/views/security/fail2ban/index.vue`
- `web/src/views/applications/apps/ApplicationRuntimePanel.vue`
- `web/src/views/resources/_shared/ResourcePage.vue`
