<script setup lang="ts">
import { computed } from 'vue';
import { valueTone, type HistoryKind, type MetricTone } from './model';

const props = defineProps<{
  kind: HistoryKind;
  values: Array<number | null>;
  label: string;
  /** 配置的警告阈值；缺省时使用固定色阶。 */
  threshold?: number | null;
}>();

// 24 个小时桶直接映射为 24 段（不再两两合并），趋势更细；条高降低避免压过字段名。
const displayValues = computed<Array<number | null>>(() => props.values);

const tones = computed<Array<MetricTone | null>>(() => displayValues.value.map((value) => valueTone(props.kind, value, props.threshold)));

const segmentClass: Record<MetricTone, string> = {
  success: 'bg-success',
  warning: 'bg-warning',
  danger: 'bg-danger',
};
</script>

<template>
  <div class="flex h-2 min-w-0 items-stretch gap-0.5 overflow-hidden rounded-sm" role="img" :aria-label="label" data-testid="history-strip">
    <span
      v-for="(tone, index) in tones"
      :key="index"
      class="min-w-0 flex-1 rounded-micro"
      :class="tone ? segmentClass[tone] : 'bg-muted'"
      aria-hidden="true"
    />
  </div>
</template>
