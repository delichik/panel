<script setup lang="ts">
import { computed, useAttrs } from 'vue';
import { disabledControl, disabledControlColors, focusRing } from './states';
import { cn } from './cn';

/**
 * WinUI 3 ToggleButton：`aria-pressed` 常驻，选中态使用 accent-selected 填充，
 * 32(28)px 高、4px 圆角、图标 16；hover/press 只变填充。
 */
const props = withDefaults(defineProps<{
  modelValue?: boolean;
  label: string;
  size?: 'sm' | 'md';
  disabled?: boolean;
}>(), {
  modelValue: false,
  size: 'md',
  disabled: false,
});

const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>();
const attrs = useAttrs();

const classes = computed(() => cn(
  'inline-flex shrink-0 items-center justify-center gap-2 rounded-control border font-normal transition-control duration-fast ease-move active:duration-fade',
  focusRing,
  disabledControl,
  '[&_svg]:size-4',
  props.size === 'sm' ? 'h-control-sm px-2 text-caption' : 'h-control px-3 text-body',
  props.modelValue
    ? 'border-transparent bg-accent-selected text-accent-text'
    : 'border-transparent bg-transparent text-secondary-foreground hover:bg-hover hover:text-foreground active:bg-pressed',
  props.disabled && disabledControlColors,
  attrs.class as string,
));

function toggle() {
  if (props.disabled) return;
  emit('update:modelValue', !props.modelValue);
}
</script>

<template>
  <button
    type="button"
    :aria-pressed="modelValue"
    :aria-label="label"
    :title="label"
    :disabled="disabled"
    :class="classes"
    @click="toggle"
  >
    <slot />
  </button>
</template>
