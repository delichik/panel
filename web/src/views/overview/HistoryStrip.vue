<script setup lang="ts">
import { computed } from 'vue';
import { valueTone, type HistoryKind, type MetricTone } from './model';

const props = defineProps<{
  kind: HistoryKind;
  values: Array<number | null>;
  label: string;
}>();

// 视觉降噪：24 个小时桶合并为 12 段（每段 2 小时，任一非空则取均值），
// 保留趋势但减少细碎色块。
const displayValues = computed<Array<number | null>>(() => {
  const source = props.values;
  if (source.length <= 12) return source;
  const groups: Array<Array<number>> = Array.from({ length: 12 }, () => []);
  source.forEach((value, index) => {
    if (value === null || !Number.isFinite(value)) return;
    const bucket = Math.min(11, Math.floor((index * 12) / source.length));
    groups[bucket]!.push(value);
  });
  return groups.map((group) => (group.length ? group.reduce((sum, value) => sum + value, 0) / group.length : null));
});

const tones = computed<Array<MetricTone | null>>(() => displayValues.value.map((value) => valueTone(props.kind, value)));

const segmentClass: Record<MetricTone, string> = {
  success: 'bg-success',
  warning: 'bg-warning',
  danger: 'bg-danger',
};
</script>

<template>
  <div class="flex h-2.5 min-w-0 items-stretch gap-0.5 overflow-hidden rounded-sm" role="img" :aria-label="label" data-testid="history-strip">
    <span
      v-for="(tone, index) in tones"
      :key="index"
      class="min-w-0 flex-1 rounded-[1px]"
      :class="tone ? segmentClass[tone] : 'bg-muted'"
      aria-hidden="true"
    />
  </div>
</template>
