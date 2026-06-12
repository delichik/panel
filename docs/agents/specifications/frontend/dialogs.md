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

全局 overlay 限制为视口宽高减 `24px`，并保留 `12px` 外边距。

## 标题

- 标题区最小高度 `64px`。
- 标题文字约 `1.05rem`、高字重。
- 标题必须能截断或换行，不能被关闭按钮挤出。
- 关闭按钮使用 text/icon，并提供可访问名称。

## 内容

- `.app-dialog-body` 默认 `24px` 内边距。
- 最大高度为 `min(68vh, 720px)`，超出时内容区内部滚动。
- 表单级错误放在内容顶部。
- 大型编辑器可扩展内部布局，但不修改全局 overlay 尺寸约束。

## 操作区

- 操作按钮靠右。
- 取消使用 `variant="text"`。
- 保存或确认使用 `primary flat`。
- 删除确认使用 `error flat`。
- 按钮最小宽度 `92px`。
- `600px` 以下按钮纵向占满整行。

## 确认对话框

- 标题明确动作对象，例如“删除域名”，避免只写“确认”。
- 正文说明后果和对象名称。
- 高风险操作可增加 warning/error tonal Alert 或确认复选框。
- 取消必须保持可见且不弱化到难以发现。
- 删除完成前使用按钮 loading，防止重复提交。

## 表单对话框

- 表单字段遵守 [forms.md](forms.md)。
- 关闭对话框时清理临时错误和不应保留的草稿状态。
- 编辑和新建可共用对话框，但标题、主要按钮和初始值必须明确切换。

## 禁忌

- 不直接使用裸 `v-card-title/text/actions` 而绕过全局结构类。
- 不让整个页面在对话框打开后承担对话框内容滚动。
- 不在一个对话框内继续打开尺寸相近的第二层编辑对话框。
- 不把 Snackbar 用作需要用户确认的对话框替代品。

## 源码依据

- `web/src/styles/main.css`
- `web/src/features/dns/pages/DomainsPage.vue`
- `web/src/features/servers/pages/ServersPage.vue`
- `web/src/features/nomad/pages/NomadNodesPage.vue`
- `web/src/features/certificates/pages/KeyAssetsPage.vue`

