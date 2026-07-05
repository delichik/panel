# 表单

## 基础组件

使用 Vuetify：

- 单行输入：`v-text-field`
- 选择：`v-select`
- 可输入选择：`v-combobox`
- 多行输入：`v-textarea`
- 布尔设置：`v-switch`
- 多选：`v-checkbox` 或表格中的 `v-checkbox-btn`
- 文件：`v-file-input`

输入框全局圆角为 `8px`，聚焦时使用主题色焦点环。

## 变体与密度

- 对话框和普通编辑表单：`variant="outlined" density="comfortable"`。
- 筛选栏、重复行和紧凑工具区：`variant="outlined" density="compact" hide-details`。
- 同一个表单分区内保持统一密度。
- 只有无需展示校验、提示或错误文本的紧凑控件才使用 `hide-details`。

## 标签与提示

- 所有可编辑字段必须有本地化 `label`。
- 格式要求使用 `hint`；持续重要的要求使用 `persistent-hint`。
- 示例值可使用 `placeholder`，但不能替代标签。
- 密码字段使用 `type="password"`。
- 路径、密钥、YAML、哈希等内容使用等宽字体。

## 表单布局

### 单列表单

设置页等连续配置使用：

```css
display: grid;
max-width: 560px;
gap: 16px;
```

### 双列表单

短字段可使用：

```css
grid-template-columns: repeat(2, minmax(0, 1fr));
gap: 12px;
```

- 长文本域使用 `.span-all` 跨越整行。
- `760px` 以下转为单列。

### 重复行

- 使用 grid，间距 `8px` 到 `10px`。
- 删除按钮位于行尾，使用 text/error 图标按钮。
- `760px` 以下必须纵向堆叠。
- 弹窗或受限容器里的复杂重复行不要只等到窄屏断点；当字段、说明文本和按钮会挤压时，应提前在中等宽度折叠为单列。
- 如果一行包含 4 个以上控件、箭头/说明文本或多个固定宽度列，必须按实际容器宽度设置提前折叠断点，不能只按全局页面断点判断。
- 折叠前的固定列只用于短标签、图标按钮等稳定尺寸内容；用户输入字段必须使用 `minmax(0, 1fr)` 或明确的响应式约束，不能被相邻 `auto` 列挤压到不可读。
- 新增按钮位于列表之后，使用 outlined。
- 页面正文中的复杂重复配置项不得把每一项完整表单直接展开在列表下方。反向代理规则、设施路由、挂载目标等包含多个字段或子项的配置，应先显示摘要列表，每行提供编辑和删除动作，点击编辑后使用 dialog 承载完整表单；只有本身已经位于 dialog、抽屉或短小受限编辑容器中的表单可直接展示完整字段。

### 筛选工具区

- 使用 grid 搭配 `auto-fit` / `minmax`，让筛选字段按容器宽度自然换行。
- 操作按钮放在独立按钮组内，按钮组整体参与布局，避免搜索、清除等按钮被拆成拥挤的独立列。
- 紧凑筛选字段使用 `variant="outlined" density="compact" hide-details`，同一区域保持一致。
- 极窄屏按钮组可以等宽铺满，但中大屏优先保持内容宽度并靠右。
- 禁止把筛选字段和多个按钮写成固定的 `字段 字段 字段 auto auto` 栅格；这会在中等宽度下把按钮和选择器挤成一团。
- 多选 `v-select` 带 chips 时应获得不小于普通文本字段的最小列宽，且必须允许换行或重新分配列宽。

## 分区

长表单使用 `.section-title` 和 `v-divider` 分组。分区标题描述领域，如“运行时”“网络”“凭据”，不能只写模糊的“其他”。

设置页包含多个可独立修改的分区时，每个 section 在自身内容底部放置保存按钮。按钮只保存所属 section，不在页头放置会同时提交多个分区草稿的全局保存按钮。

## 布尔控件

- `v-switch` 用于立即表达启用/禁用或模式切换。
- `v-checkbox` 用于确认、批量选择或附加选项。
- 危险确认复选框必须配合说明文本，不能只显示控件。

## 校验与提示

- 字段错误由 Vuetify 输入组件显示。
- 表单级错误使用 `v-alert type="error" variant="tonal"`，置于表单顶部或相关分区上方。
- 提交按钮使用 `:loading`。
- 提交期间按业务需要禁用相关字段或其他冲突动作。
- 保存成功使用 Snackbar，不在表单中永久堆积成功 Alert。

## 无障碍

- 不移除默认标签关联和键盘操作。
- 图标尾部操作必须有可访问名称。
- 错误信息不能只通过红色边框表达。
- 表单提交应支持原生 `submit`，优先使用 `v-form @submit.prevent`。

## 禁忌

- 不在同一区域混用 filled、solo 和 outlined。
- 不用 placeholder 作为唯一字段说明。
- 不为紧凑而隐藏仍需要展示的校验信息。
- 不让双列表单在窄屏保持固定两列。

## 源码依据

- `web/src/styles/main.css`
- `web/src/views/settings/_shared/SettingsPageContent.vue`
- `web/src/views/servers/_shared/ServersPageContent.vue`
- `web/src/views/dns/domains/index.vue`
