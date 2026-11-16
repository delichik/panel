<script setup lang="ts">
import { computed } from 'vue';
import { percentTone, type MetricTone } from './model';

const props = defineProps<{
  percent: number | null;
  label: string;
}>();

const tone = computed(() => percentTone(props.percent));
const width = computed(() => {
  if (props.percent === null || props.percent === undefined) return 0;
  return Math.min(100, Math.max(0, props.percent));
});
const barClass: Record<MetricTone, string> = {
  success: 'bg-success',
  warning: 'bg-warning',
  danger: 'bg-danger',
};
</script>

<template>
  <div class="h-2 min-w-0 overflow-hidden rounded-full bg-muted" role="progressbar" :aria-label="label" aria-valuemin="0" aria-valuemax="100" :aria-valuenow="Math.round(width)">
    <span
      class="block h-full rounded-full transition-[width] duration-300"
      :class="tone ? barClass[tone] : 'bg-muted-foreground/30'"
      :style="{ width: `${width}%` }"
    />
  </div>
</template>
