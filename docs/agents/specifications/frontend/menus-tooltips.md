# 菜单与提示

## 溢出操作菜单

当一行或一个卡片有三个以上次要操作时，使用 `v-menu` 收纳，触发按钮通常为：

```html
<v-menu location="bottom end">
  <template #activator="{ props }">
    <v-btn
      v-bind="props"
      icon="mdi-dots-vertical"
      variant="text"
      size="small"
      aria-label="..."
    />
  </template>
  <v-list density="compact">...</v-list>
</v-menu>
```

## 菜单项

- 使用 `v-list density="compact"`。
- 使用 `v-list-item`，标题必须本地化。
- 常见动作可添加 `prepend-icon`。
- 编辑等普通操作使用默认文本色。
- 删除等破坏性操作使用 `class="text-error"`。
- 菜单项顺序为常用操作在前、破坏性操作在后。
- 点击卡片或列表行内菜单时使用事件阻止，避免同时触发行选择。

## 触发按钮

- 默认使用 text/icon/small。
- 图标为 `mdi-dots-vertical` 时必须有 `aria-label`。
- 单一明确动作不应为了视觉简洁而藏入菜单。
- 页面主操作不放入溢出菜单。

## 选择菜单

用于插入变量、选择任务日志等业务选项时：

- 触发器可以是 outlined 文本按钮或小型图标按钮。
- 列表使用 compact 密度。
- 选项标题应能独立说明选择结果。
- 选项较多或需要搜索时改用 `v-select` 或 `v-combobox`，不使用超长菜单。

## 页头设置菜单

主题等少量全局偏好可以使用 `v-menu` 承载紧凑设置卡：

- 触发器使用有本地化可访问名称的图标按钮。
- 表单内容较多时设置 `:close-on-content-click="false"`，避免每次选择后关闭。
- 模式切换使用紧凑按钮组，布尔设置使用开关；主题颜色使用展示语义色组合的内置预设卡片，不允许任意颜色输入。
- 设置即时生效并持久化时不额外添加保存按钮；应提供恢复默认值动作。
- 菜单宽度必须受视口约束，窄屏将并排控件折叠为单列。

账户菜单继续使用紧凑 `v-list`：用户名作为非破坏性信息项，退出登录作为后续操作项。

## Tooltip

`v-tooltip` 用于解释：

- 仅图标表达、但 `aria-label` 仍不足以帮助鼠标用户理解的工具按钮。
- 截断但必须可查看完整值的短文本。
- 不常见的状态图标。

Tooltip 不是以下内容的替代：

- 表单 `label` 和 `hint`。
- 重要错误和风险提示。
- 需要用户操作的确认信息。
- 移动端必须可发现的核心功能说明。

## 弹出位置与视口

- 行操作菜单优先 `bottom end`，与右对齐操作列一致。
- 菜单不得超出视口；使用 Vuetify 默认 overlay 定位能力。
- 不通过固定绝对坐标定位菜单。
- 对话框内菜单应确保层级由 Vuetify overlay 管理。

## 键盘与无障碍

- 触发器可通过键盘聚焦和打开。
- 图标触发器必须有可访问名称。
- 不在菜单项中嵌套另一组难以键盘访问的按钮。
- Tooltip 只提供补充信息，核心语义仍由按钮名称或正文提供。

## 禁忌

- 不把主要保存、创建或确认动作隐藏到菜单。
- 不用菜单代替复杂表单或多步骤流程。
- 不为每个表格行始终展开四五个按钮。
- 不用 Tooltip 承载长段说明。

## 源码依据

- `web/src/views/dns/domains/index.vue`
- `web/src/views/overview/index.vue`
- `web/src/views/runtime/applications/ApplicationRuntimePanel.vue`
- `web/src/views/runtime/applications/ApplicationEditor.vue`
