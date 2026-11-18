<script setup lang="ts">
import { Check } from '@/icons';

/**
 * WinUI 3 CheckBox：原生 checkbox 作为可访问控件（sr-only），视觉为 20px 小方框 +
 * 强调色勾选；命中区至少 32px（窄屏 40px）。
 * PointerOver 改变方框描边/填充、Pressed 加深填充，均为 167/83ms 纯填充/描边过渡。
 *
 * 图标说明：项目 icons/ 仅 vendored Fluent 24px regular 母版，这里按控件内 16px
 * 渲染（`size-4`）并在 20px 方框内居中；24px 母版缩放策略在本轮保留，后续再补
 * 16px 母版时只需替换 icons 层，不影响此处的尺寸约定。
 */
withDefaults(defineProps<{
  modelValue?: boolean;
  disabled?: boolean;
  label?: string;
  ariaLabel?: string;
  id?: string;
}>(), {
  modelValue: false,
  disabled: false,
  label: '',
  ariaLabel: '',
});

const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>();

function onChange(event: Event) {
  emit('update:modelValue', (event.target as HTMLInputElement).checked);
}
</script>

<template>
  <label
    class="group inline-flex min-h-control max-lg:min-h-touch min-w-0 items-center gap-2 text-body text-foreground"
    :class="disabled ? 'cursor-not-allowed text-muted-foreground' : 'cursor-pointer'"
  >
    <input
      :id="id"
      type="checkbox"
      class="peer sr-only"
      :checked="modelValue"
      :disabled="disabled"
      :aria-label="ariaLabel || undefined"
      @change="onChange"
    />
    <span
      class="grid size-5 shrink-0 place-items-center rounded-control border transition-fill duration-fast ease-move group-active:duration-fade peer-focus-visible:ring-2 peer-focus-visible:ring-ring motion-reduce:transition-none"
      :class="disabled
        ? 'border-stroke bg-muted text-muted-foreground'
        : 'border-input bg-control text-primary-foreground group-hover:border-border-strong group-hover:bg-hover group-active:bg-pressed peer-checked:border-primary peer-checked:bg-primary peer-checked:group-hover:bg-accent-strong peer-checked:group-active:bg-accent-stronger'"
      aria-hidden="true"
    >
      <Check v-if="modelValue" class="size-4" />
    </span>
    <span v-if="label || $slots.default" class="min-w-0"><slot>{{ label }}</slot></span>
  </label>
</template>
