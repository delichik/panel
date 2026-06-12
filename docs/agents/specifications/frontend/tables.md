# 数据表格

## 基础

使用 Vuetify `v-table`。表格背景透明，由外层卡片提供表面、边框和裁切。

```html
<v-card variant="outlined" class="overflow-hidden">
  <div class="app-card-header">...</div>
  <v-table class="text-left">...</v-table>
  <AppPagination ... />
</v-card>
```

## 全局视觉

- 表头：约 `0.74rem`、高字重、弱文本色、英文大写。
- 单元格垂直居中。
- 行分隔线使用弱化后的 `--lp-border`。
- 表格宽度至少填满容器；内容宽时允许自然扩展。
- `.v-table__wrapper` 统一提供横向滚动。

## 密度

- 普通业务列表使用默认密度。
- 对话框、详情子表或信息密集表格使用 `density="compact"`。
- 同一张表中不通过自定义 padding 制造不同密度的行。

## 列内容

- 状态使用小型 tonal Chip。
- 数值使用 `.font-tabular`。
- 路径、ID、哈希使用等宽字体，并允许截断或换行。
- 长说明列应设置合理 `max-width` 并允许换行。
- 时间列保持可扫描，但窄屏依赖横向滚动，不强制压缩到不可读。

## 行操作

使用 `.app-table-actions`：

- 靠右排列。
- `6px` 间距并允许换行。
- 常规编辑、删除使用 `size="small" variant="outlined"`。
- 操作较多时使用 `v-menu` 收纳次要动作。
- 只有图标的行操作必须提供 `title` 或 `aria-label`。

## 选择

- 批量选择使用首列表头和行内 `v-checkbox-btn`。
- 全选只影响当前可见或当前定义范围，行为必须明确。
- 选中行可使用淡主色背景，不能只依赖复选框位置表达。

## 空数据

- 简单表格可以使用一行 `colspan` 的居中空文案。
- 需要引导用户创建或选择对象时，使用独立空状态区域。
- 初次加载不得同时显示空数据行，改用 `PageLoadingState`。

## 分页

用户可见数据列表使用 `AppPagination`，放在表格之后、卡片内部。具体规则见 [pagination.md](pagination.md)。

## 响应式

- 不通过固定表格宽度撑开整页。
- 横向滚动只发生在 `.v-table__wrapper`。
- 操作列和状态列可保持 `white-space: nowrap`。
- 长文本列允许 `overflow-wrap: anywhere`。
- 在移动端不把结构复杂的数据表强行改成无规范的卡片列表；需要转换时应建立新的共享模式。

## 禁忌

- 不在页面根节点使用横向滚动修复表格溢出。
- 不为每张表重新实现边框和表头颜色。
- 不在单元格中放默认尺寸的大按钮组。
- 不让加载、空状态和已有数据同时竞争显示。

## 源码依据

- `web/src/styles/main.css`
- `web/src/features/servers/pages/ServersPage.vue`
- `web/src/features/dns/pages/DomainsPage.vue`
- `web/src/features/tasks/pages/TaskCenterPage.vue`
- `web/src/features/certificates/pages/KeyAssetsPage.vue`

