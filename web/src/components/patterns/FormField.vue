<script setup lang="ts">
import { computed, useId } from 'vue';

/**
 * WinUI 3 表单字段：Caption label（12/400，secondary 色）+ 控件 + Caption hint/error。
 *
 * 控件由调用方作为默认插槽传入，组件通过插槽属性下发无障碍接线：
 * `id` / `describedBy` / `invalid` / `required` / `disabled`，调用方把它们绑到
 * Input、Select、Textarea 等控件的对应属性上（Input 会把 aria-* 透传到原生 input）。
 * 错误态：label/error 使用 danger 语义色，`invalid` 为 true；hint 使用 secondary 色。
 * 两种布局：默认 vertical（弹窗/编辑器字段堆叠），horizontal 为 SettingsCard 式
 * label 在左、控件在右（`grid-cols-field`），窄屏自动回落为纵向。
 *
 * 不使用 `<label>` 包裹控件：本仓 Select/Tabs 等是可聚焦的自定义控件，包裹会导致
 * 点击 label 文本触发内部按钮而非聚焦，故用 `for` + 显式 id 关联。
 */
const props = withDefaults(defineProps<{
  label?: string;
  /** 控件 id；缺省时按实例生成，调用方应使用插槽属性里的 id。 */
  id?: string;
  hint?: string;
  error?: string;
  /** 强制无效态（即使暂无文案）；默认由 error 推导。 */
  invalid?: boolean;
  required?: boolean;
  disabled?: boolean;
  layout?: 'vertical' | 'horizontal';
}>(), {
  label: '',
  hint: '',
  error: '',
  invalid: false,
  required: false,
  disabled: false,
  layout: 'vertical',
});

const instanceId = useId();
const fieldId = computed(() => props.id || `${instanceId}-field`);
const hintId = computed(() => `${fieldId.value}-hint`);
const errorId = computed(() => `${fieldId.value}-error`);
const isInvalid = computed(() => props.invalid || Boolean(props.error));
/** 同一时刻只渲染一条描述（error 优先），避免 aria-describedby 指向不存在的元素。 */
const describedBy = computed(() => (props.error ? errorId.value : props.hint ? hintId.value : undefined));

const slotProps = computed(() => ({
  id: fieldId.value,
  describedBy: describedBy.value,
  invalid: isInvalid.value,
  required: props.required,
  disabled: props.disabled,
}));
</script>

<template>
  <div
    data-form-field
    :data-invalid="isInvalid || undefined"
    :data-disabled="disabled || undefined"
    :data-layout="layout"
    class="min-w-0"
    :class="[
      layout === 'horizontal' ? 'grid gap-1 sm:grid-cols-field sm:items-start sm:gap-4' : 'grid gap-1',
      disabled ? 'opacity-45' : undefined,
    ]"
  >
    <div v-if="label || $slots.label" class="grid min-w-0 gap-0.5" :class="layout === 'horizontal' ? 'sm:pt-2' : undefined">
      <label
        v-if="label || $slots.label"
        :for="fieldId"
        class="text-caption font-normal"
        :class="isInvalid ? 'text-danger' : 'text-muted-foreground'"
      >
        <slot name="label">{{ label }}</slot>
        <span v-if="required" class="ml-1 text-danger" aria-hidden="true">*</span>
      </label>
    </div>

    <div class="grid min-w-0 gap-1">
      <slot v-bind="slotProps" />
      <p v-if="error" :id="errorId" class="m-0 text-caption text-danger">{{ error }}</p>
      <p v-else-if="hint" :id="hintId" class="m-0 text-caption text-secondary-foreground">{{ hint }}</p>
    </div>
  </div>
</template>
