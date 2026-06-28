# 对话框

## 标准结构

对话框统一使用全局结构类：

```html
<v-dialog v-model="open" width="560">
  <v-card class="app-dialog-card">
    <v-card-title class="app-dialog-title">
      <span class="app-dialog-title-text">...</span>
      <v-btn icon="mdi-close" variant="text" ... />
    </v-card-title>
    <v-divider />
    <v-card-text class="app-dialog-body">...</v-card-text>
    <v-divider />
    <v-card-actions class="app-dialog-actions">...</v-card-actions>
  </v-card>
</v-dialog>
```

## 尺寸

| 类型 | 建议宽度 |
| --- | --- |
| 简单确认 | `420px` 左右 |
| 普通表单 | `560px` 至 `640px` |
| 复杂配置 | `720px` 至 `900px` |
| 大型编辑器 | 约 `1180px`，但仍受视口限制 |

全局 overlay 应保留视口边距，避免贴边；窄屏下宽度由 Vuetify 和全局样式自然收缩。
遮罩透明度由 Vuetify overlay 自身控制；全局 fade 动效不得把遮罩显示终点覆盖为完全不透明，避免弹窗打开时遮罩先变黑再回落。

## 标题

- 标题区最小高度约 `64px`。
- 标题栏背景使用 `rgb(var(--v-theme-surface-variant))`，与共享选择器标题栏保持一致，并自动适配明暗主题。
- 标题文字约 `1.05rem`，加粗。
- 标题必须能截断或换行，不能被关闭按钮挤出。
- 关闭按钮使用 text/icon，并提供可访问名称或清晰上下文。

## 内容

- `.app-dialog-body` 默认使用 `24px` 内边距。
- 内容区最大高度由全局样式限制，超出时在对话框内部滚动。
- 表单级错误放在内容顶部。
- 大型编辑器可扩展内部布局，但不要绕开全局 overlay 和内容区约束。
- 表单控件默认使用 `variant="outlined"` 与 `density="comfortable"`；密集表格或冲突列表可局部使用 `density="compact"`。

## 操作区

- 操作按钮靠右。
- 取消使用 `variant="text"`。
- 保存或确认使用 `primary flat`。
- 删除等危险确认使用 `error flat`。
- 按钮最小宽度约 `92px`。
- `600px` 以下按钮可纵向排列并占满整行。

## 确认对话框

- 标题明确动作对象，例如“删除域名”，避免只写“确认”。
- 正文使用普通段落说明后果和对象名称，不要默认把整段正文放进大号 Alert。
- 高风险操作可增加紧凑的 warning/error tonal Alert 或确认复选框。
- 取消必须保持可见且不弱化到难以发现。
- 删除、重置、覆盖等操作在完成前使用按钮 loading，防止重复提交。

## 表单对话框

- 表单字段遵守 [forms.md](forms.md)。
- 关闭对话框时清理临时错误和不应保留的草稿状态。
- 编辑和新建可共用对话框，但标题、主要按钮和初始值必须明确切换。

## 禁忌

- 不直接使用裸 `v-card-title/text/actions` 绕过全局结构类。
- 不让整个页面在对话框打开后承担对话框内容滚动。
- 不在一个对话框内继续打开尺寸相近的第二层编辑对话框。
- 不把 Snackbar 用作需要用户确认的对话框替代品。
- 不为了强调普通确认正文而使用占满内容区的大 Alert。

## 源码依据

- `web/src/styles/main.css`
- `web/src/views/dns/domains/index.vue`
- `web/src/views/servers/_shared/ServersPageContent.vue`
- `web/src/views/applications/apps/ApplicationEditor.vue`
- `web/src/views/certificates/key-assets/index.vue`
