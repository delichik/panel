<script setup lang="ts">
import { computed, useAttrs } from 'vue';
import ProgressRing from './ProgressRing.vue';
import { disabledControlColors, focusRing } from './states';
import { cn } from './cn';

const props = withDefaults(defineProps<{
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger';
  size?: 'sm' | 'md' | 'lg';
  loading?: boolean;
  disabled?: boolean;
  type?: 'button' | 'submit' | 'reset';
}>(), {
  variant: 'secondary',
  size: 'md',
  type: 'button',
});

const attrs = useAttrs();
const classes = computed(() => cn(
  // WinUI 3 Button：Body 14/400、32(28/40)px 高、4px 圆角、1px 半透明描边；
  // hover/press 只变填充（167/83ms），无上浮/缩放/水波纹；图标 16。
  'inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-control border text-body font-normal transition-control duration-fast ease-move active:duration-fade',
  focusRing,
  'disabled:pointer-events-none [&_svg]:size-4',
  props.size === 'sm' && 'h-control-sm px-2 text-caption',
  props.size === 'md' && 'h-control px-3',
  props.size === 'lg' && 'h-control-lg px-4',
  props.variant === 'primary' && 'border-transparent bg-primary text-primary-foreground hover:bg-accent-strong active:bg-accent-stronger',
  props.variant === 'secondary' && 'border-stroke bg-control text-foreground hover:bg-hover active:bg-pressed',
  props.variant === 'ghost' && 'border-transparent bg-transparent text-secondary-foreground hover:bg-hover hover:text-foreground active:bg-pressed',
  props.variant === 'danger' && 'border-danger-border bg-danger-bg text-danger hover:bg-danger-bg/80 active:bg-danger-bg/70',
  // loading 时按钮仍带 disabled 属性以避免重复提交，但保持变体配色；只有显式 disabled 才走禁用色板。
  props.disabled && disabledControlColors,
  attrs.class as string,
));
</script>

<template>
  <button :type="type" :disabled="disabled || loading" :aria-busy="loading ? 'true' : undefined" :class="classes">
    <ProgressRing v-if="loading" :size="16" aria-hidden="true" />
    <slot />
  </button>
</template>
