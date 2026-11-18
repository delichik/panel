<script setup lang="ts">
import { computed, useAttrs } from 'vue';
import { disabledText, focusRingInset } from './states';
import { cn } from './cn';

const props = withDefaults(defineProps<{ tone?: 'default' | 'danger'; disabled?: boolean }>(), { tone: 'default' });
const attrs = useAttrs();
const classes = computed(() => cn(
  // WinUI MenuFlyout 项：Body 14、32px 高、4px 圆角；hover/press 仅填充变化。
  'motion-menu-item flex h-control w-full items-center gap-2 rounded-control px-3 text-left text-body text-foreground transition-colors duration-fast active:duration-fade',
  focusRingInset,
  disabledText,
  props.tone === 'danger' ? 'text-danger hover:bg-danger-bg [--panel-hover:var(--panel-danger-bg)] [--panel-pressed:var(--panel-danger-bg)]' : 'hover:bg-hover',
  attrs.class as string,
));
</script>

<template>
  <button type="button" role="menuitem" :disabled="disabled" :class="classes">
    <slot />
  </button>
</template>
