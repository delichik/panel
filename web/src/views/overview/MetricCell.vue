<script setup lang="ts">
import HistoryStrip from './HistoryStrip.vue';
import MeterBar from './MeterBar.vue';
import type { HistoryKind } from './model';

defineProps<{
  label: string;
  text: string;
  valueClass: string;
  muted?: boolean;
  empty?: boolean;
  hint?: string;
  ariaLabel: string;
  kind?: HistoryKind;
  history?: Array<number | null>;
  percent?: number | null;
}>();
</script>

<template>
  <div class="grid min-w-0 gap-1" :class="muted ? 'opacity-60' : ''">
    <div class="flex min-w-0 items-baseline justify-between gap-2">
      <span class="truncate text-xs text-muted-foreground">{{ label }}</span>
      <span class="shrink-0 text-xs font-medium tabular-nums" :class="valueClass" :title="hint">{{ text }}</span>
    </div>
    <template v-if="!empty">
      <HistoryStrip v-if="kind" :kind="kind" :values="history ?? []" :label="ariaLabel" />
      <MeterBar v-else :percent="percent ?? null" :label="ariaLabel" />
    </template>
  </div>
</template>
