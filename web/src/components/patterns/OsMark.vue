<script setup lang="ts">
import { computed } from 'vue';
import { Server } from '@/icons';
import type { ServerOsRelease } from '@/types/servers';
import { resolveOsMark } from './osMark';

/**
 * Linux distribution identification mark (24×24 glyph on the official brand color).
 *
 * Decorative by design: the adjacent text always names the OS, so the mark is
 * `aria-hidden`. It identifies the distribution only — never support/health.
 * Data comes from the vendored Simple Icons set (`osMarkData.ts`, CC0 1.0);
 * unknown distributions fall back to a neutral Fluent server glyph.
 */
const props = withDefaults(defineProps<{
  os?: Pick<ServerOsRelease, 'id' | 'prettyName'> | null;
  /** Badge edge length: 28 / 36 / 44px. */
  size?: 'sm' | 'md' | 'lg';
}>(), { os: null, size: 'md' });

const sizeClass = computed(() => ({
  sm: 'size-7',
  md: 'size-9',
  lg: 'size-11',
}[props.size]));

const mark = computed(() => resolveOsMark(props.os));
const badgeStyle = computed(() => (mark.value ? { backgroundColor: mark.value.color } : undefined));
const glyphStyle = computed(() => (mark.value?.glyph === 'dark'
  ? { fill: 'var(--panel-mark-on-light)' }
  : { fill: 'var(--panel-mark-on-brand)' }));
</script>

<template>
  <span
    data-testid="os-mark"
    :data-os="mark?.key ?? 'unknown'"
    :title="mark?.title"
    aria-hidden="true"
    class="inline-grid shrink-0 place-items-center overflow-hidden rounded-control border border-card-stroke"
    :class="[sizeClass, mark ? undefined : 'bg-muted']"
    :style="badgeStyle"
  >
    <svg v-if="mark" viewBox="0 0 24 24" class="size-3/5" :style="glyphStyle" focusable="false">
      <path :d="mark.path" />
    </svg>
    <Server v-else class="size-1/2 text-secondary-foreground" />
  </span>
</template>
