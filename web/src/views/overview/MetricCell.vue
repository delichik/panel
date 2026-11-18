<script setup lang="ts">
import type { Component } from 'vue';
import ProgressBar from '@/components/patterns/ProgressBar.vue';
import HistoryStrip from './HistoryStrip.vue';
import { percentTone, type HistoryKind } from './model';

defineProps<{
  label: string;
  icon?: Component;
  text: string;
  valueClass: string;
  muted?: boolean;
  empty?: boolean;
  placeholder?: boolean;
  plain?: boolean;
  hint?: string;
  ariaLabel: string;
  kind?: HistoryKind;
  history?: Array<number | null>;
  percent?: number | null;
}>();
</script>

<template>
  <div class="grid min-w-0 gap-1.5" :class="muted ? 'opacity-60' : ''">
    <div class="flex min-w-0 items-center justify-between gap-2">
      <span class="flex min-w-0 items-center gap-1.5 text-body text-muted-foreground">
        <component :is="icon" v-if="icon" class="size-3.5 shrink-0" aria-hidden="true" />
        <span class="truncate">{{ label }}</span>
      </span>
      <span class="shrink-0 text-caption font-medium tabular-nums" :class="valueClass" :title="hint">{{ text }}</span>
    </div>
    <template v-if="!empty && !plain">
      <div
        v-if="placeholder"
        class="h-1.5 min-w-0 rounded-sm border border-dashed border-border-strong"
        role="img"
        :aria-label="ariaLabel"
        data-testid="metric-placeholder"
      />
      <HistoryStrip v-else-if="kind" :kind="kind" :values="history ?? []" :label="ariaLabel" />
      <ProgressBar v-else :value="percent ?? 0" :label="ariaLabel" :tone="percentTone(percent ?? null) ?? 'accent'" />
    </template>
  </div>
</template>
