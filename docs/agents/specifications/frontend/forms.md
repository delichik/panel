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
- 新增按钮位于列表之后，使用 outlined。

## 分区

长表单使用 `.section-title` 和 `v-divider` 分组。分区标题描述领域，如“运行时”“网络”“凭据”，不能只写模糊的“其他”。

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
