<script setup lang="ts">
import { computed, useAttrs } from 'vue';
import { cn } from './cn';

const props = withDefaults(defineProps<{ tone?: 'neutral' | 'success' | 'warning' | 'danger' | 'info' }>(), { tone: 'neutral' });
const attrs = useAttrs();
// WinUI InfoBadge：胶囊表面，24px 高、12px 文案。
const classes = computed(() => cn(
  'inline-flex h-6 items-center rounded-pill border px-2 text-caption font-semibold leading-none',
  props.tone === 'neutral' && 'border-neutral-border bg-neutral-bg text-neutral',
  props.tone === 'success' && 'border-success-border bg-success-bg text-success',
  props.tone === 'warning' && 'border-warning-border bg-warning-bg text-warning',
  props.tone === 'danger' && 'border-danger-border bg-danger-bg text-danger',
  props.tone === 'info' && 'border-info-border bg-info-bg text-info',
  attrs.class as string,
));
</script>

<template>
  <span :class="classes"><slot /></span>
</template>
