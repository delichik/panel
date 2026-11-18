<script setup lang="ts">
import { computed } from 'vue';

/**
 * Disk usage ring chart (current value only).
 *
 * WinUI 3 surface language: a 4px token stroke ring (used = semantic tone,
 * remainder = divider track) with the percentage centered. The page owns the
 * surrounding legend (used / total / free / sampled at) and passes localised
 * texts; the ring itself only draws the ratio, so no chart library or history
 * series is involved. `role="img"` + `label` must include the percentage.
 */
export type DiskUsageTone = 'accent' | 'success' | 'warning' | 'danger';

const props = withDefaults(defineProps<{
  /** 0–100; `null` means no valid disk sample yet (renders an empty ring). */
  percent: number | null;
  /** Centered text, already formatted/localised (e.g. `68.4%` or `—`). */
  valueText: string;
  /** Accessible name including the current percentage. */
  label: string;
  tone?: DiskUsageTone;
  /** Ring diameter: 80px (compact) or 96px (summary hero). */
  size?: 'md' | 'lg';
}>(), { tone: 'accent', size: 'md' });

const clamped = computed(() => {
  if (props.percent === null || !Number.isFinite(props.percent)) return null;
  return Math.min(100, Math.max(0, props.percent));
});
const hasData = computed(() => clamped.value !== null);
const dashArray = computed(() => (hasData.value ? `${clamped.value} 100` : '0 100'));

const toneClass = computed(() => ({
  accent: 'text-primary',
  success: 'text-success',
  warning: 'text-warning',
  danger: 'text-danger',
}[props.tone]));
const valueClass = computed(() => (hasData.value ? toneClass.value : 'text-muted-foreground'));
const sizeClass = computed(() => (props.size === 'lg' ? 'size-24' : 'size-20'));
const valueTextClass = computed(() => (props.size === 'lg' ? 'text-subtitle' : 'text-body-strong'));
</script>

<template>
  <div
    data-testid="disk-usage-chart"
    :data-empty="hasData ? undefined : 'true'"
    :data-tone="tone"
    role="img"
    :aria-label="label"
    class="relative inline-grid shrink-0 place-items-center"
    :class="sizeClass"
  >
    <svg viewBox="0 0 36 36" class="size-full -rotate-90 text-foreground" aria-hidden="true" focusable="false">
      <circle cx="18" cy="18" r="15.9155" fill="none" stroke-width="4" pathLength="100" class="stroke-divider" />
      <circle
        cx="18"
        cy="18"
        r="15.9155"
        fill="none"
        stroke-width="4"
        pathLength="100"
        class="stroke-current"
        :class="valueClass"
        :stroke-dasharray="dashArray"
      />
    </svg>
    <span class="absolute inset-0 grid place-items-center tabular-nums" :class="[valueTextClass, valueClass]">{{ valueText }}</span>
  </div>
</template>
