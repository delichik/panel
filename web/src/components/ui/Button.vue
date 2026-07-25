<script setup lang="ts">
import { LoaderCircle } from '@lucide/vue';
import { computed, useAttrs } from 'vue';
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
  'motion-control inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-xl border text-sm font-medium transition-colors duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:pointer-events-none disabled:opacity-45 [&_svg]:size-4',
  props.size === 'sm' && 'h-8 px-3 text-xs',
  props.size === 'md' && 'h-9 px-3.5',
  props.size === 'lg' && 'h-10 px-4',
  props.variant === 'primary' && 'border-primary bg-primary text-primary-foreground hover:bg-primary/85',
  props.variant === 'secondary' && 'border-border bg-background text-foreground/70 hover:bg-accent hover:text-foreground',
  props.variant === 'ghost' && 'border-transparent bg-transparent text-foreground/60 hover:bg-accent hover:text-foreground',
  props.variant === 'danger' && 'border-danger-border bg-danger-bg text-danger hover:border-danger-border hover:bg-danger-bg/80',
  attrs.class as string,
));
</script>

<template>
  <button :type="type" :disabled="disabled || loading" :class="classes">
    <LoaderCircle v-if="loading" class="animate-spin" aria-hidden="true" />
    <slot />
  </button>
</template>
