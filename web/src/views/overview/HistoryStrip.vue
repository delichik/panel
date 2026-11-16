<script setup lang="ts">
import { computed } from 'vue';
import { valueTone, type HistoryKind, type MetricTone } from './model';

const props = defineProps<{
  kind: HistoryKind;
  values: Array<number | null>;
  label: string;
}>();

const tones = computed<Array<MetricTone | null>>(() => props.values.map((value) => valueTone(props.kind, value)));

const segmentClass: Record<MetricTone, string> = {
  success: 'bg-success',
  warning: 'bg-warning',
  danger: 'bg-danger',
};
</script>

<template>
  <div class="flex h-2 min-w-0 items-stretch gap-px overflow-hidden rounded-sm" role="img" :aria-label="label" data-testid="history-strip">
    <span
      v-for="(tone, index) in tones"
      :key="index"
      class="min-w-0 flex-1 rounded-[1px]"
      :class="tone ? segmentClass[tone] : 'bg-muted'"
      aria-hidden="true"
    />
  </div>
</template>
