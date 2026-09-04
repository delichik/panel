<script setup lang="ts">
import { computed, useAttrs } from 'vue';
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
  'motion-icon-control inline-grid shrink-0 place-items-center rounded-xl border transition-colors duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 focus-visible:ring-offset-2 focus-visible:ring-offset-card disabled:pointer-events-none disabled:opacity-45 [&_svg]:size-4',
  props.size === 'sm' ? 'size-8' : 'size-9',
  props.variant === 'ghost' && 'border-transparent text-foreground/60 hover:bg-accent hover:text-foreground',
  props.variant === 'secondary' && 'border-border bg-card text-foreground/70 hover:bg-accent hover:text-foreground',
  props.variant === 'danger' && 'border-danger-border bg-danger-bg text-danger hover:bg-danger-bg/80',
  attrs.class as string,
));
</script>

<template>
  <button :type="type" :disabled="disabled" :aria-label="label" :title="label" :class="classes">
    <slot />
  </button>
</template>
