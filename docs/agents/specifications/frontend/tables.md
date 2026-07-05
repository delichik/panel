# 数据表格

## 基础

使用 Vuetify `v-table`。表格背景透明，由外层卡片提供表面、边框和裁切。

```html
<v-card variant="outlined" class="table-card">
  <div class="app-card-header">...</div>
  <div class="table-body">
    <v-table class="text-left">...</v-table>
  </div>
  <AppPagination ... />
</v-card>
```

## 全局视觉

- 表头：约 `0.74rem`、高字重、弱文本色、英文大写。
- 单元格垂直居中。
- 行分隔线使用弱化后的 `--lp-border`。
- 表格宽度至少填满容器；内容宽时允许自然扩展。
- `.v-table__wrapper` 统一提供横向滚动；中大屏的满高表格还应在该包装器内提供纵向滚动。

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
- 类应放在表格单元格内的操作组容器上，外层 `td` 保持标准 table-cell 布局和垂直居中。
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

满高表格卡片必须把 `v-table` 放进专用内容体，内容体使用 `flex: 1 1 auto; min-height: 0; overflow: auto`。不要让 `v-table` 直接接在标题和分页之间，否则数据少时分页会停在卡片中间。

服务器资源页如果通过 `ResourcePage.vue` 提供外层右侧卡片，slot 内仍要按同一三段式组织：固定标题/操作区、内部滚动表格正文、可选分页或固定底部操作区。资源页 slot 不再额外套同等级 outlined 卡片，但也不得把裸表格直接贴在 slot 顶部。

## 响应式

- 不通过固定表格宽度撑开整页。
- 中大屏下表格卡片填满父级剩余高度，卡片本身隐藏溢出，表头和分页之外的数据区在 `.v-table__wrapper` 内滚动。
- 横向和纵向表格滚动只发生在 `.v-table__wrapper`。
- 操作列和状态列可保持 `white-space: nowrap`。
- 长文本列允许 `overflow-wrap: anywhere`。
- 在移动端不把结构复杂的数据表强行改成无规范的卡片列表；需要转换时应建立新的共享模式。
- 窄屏单列布局可取消固定高度，让表格随页面自然排列，但横向溢出仍由 `.v-table__wrapper` 承担。

## 禁忌

- 不在页面根节点使用横向滚动修复表格溢出。
- 中大屏不让数据行撑高页面并触发整页纵向滚动。
- 不为每张表重新实现边框和表头颜色。
- 不在单元格中放默认尺寸的大按钮组。
- 不让加载、空状态和已有数据同时竞争显示。

## 源码依据

- `web/src/styles/main.css`
- `web/src/views/servers/_shared/ServersPageContent.vue`
- `web/src/views/dns/domains/index.vue`
- `web/src/views/tasks/index.vue`
- `web/src/views/applications/apps/index.vue`
- `web/src/views/resources/packages/index.vue`
- `web/src/views/resources/images/index.vue`
- `web/src/views/certificates/key-assets/index.vue`
