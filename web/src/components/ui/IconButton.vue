<script setup lang="ts">
import { computed, useAttrs } from 'vue';
import { disabledControl, disabledControlColors, focusRing } from './states';
import { cn } from './cn';

const props = withDefaults(defineProps<{
  label: string;
  variant?: 'ghost' | 'secondary' | 'danger';
  size?: 'sm' | 'md';
  disabled?: boolean;
  type?: 'button' | 'submit' | 'reset';
}>(), {
  variant: 'ghost',
  size: 'md',
  type: 'button',
});

const attrs = useAttrs();
const classes = computed(() => cn(
  // 图标按钮 32(28)px 命中区，4px 圆角；hover/press 仅填充变化；图标 16。
  'inline-grid shrink-0 place-items-center rounded-control border transition-control duration-fast ease-move active:duration-fade',
  focusRing,
  disabledControl,
  'disabled:opacity-100 [&_svg]:size-4',
  props.size === 'sm' ? 'size-control-sm' : 'size-control',
  props.variant === 'ghost' && 'border-transparent text-secondary-foreground hover:bg-hover hover:text-foreground active:bg-pressed',
  props.variant === 'secondary' && 'border-stroke bg-control text-foreground hover:bg-hover active:bg-pressed',
  props.variant === 'danger' && 'border-danger-border bg-danger-bg text-danger hover:bg-danger-bg/80 active:bg-danger-bg/70',
  props.disabled && disabledControlColors,
  attrs.class as string,
));
</script>

<template>
  <button :type="type" :disabled="disabled" :aria-label="label" :title="label" :class="classes">
    <slot />
  </button>
</template>
