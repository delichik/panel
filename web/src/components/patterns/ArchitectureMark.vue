<script setup lang="ts">
import { computed } from 'vue';
import type { ServerArchitecture } from '@/types/servers';
import { resolveArchitectureMark } from './osMark';

/**
 * Machine architecture chip: a hand-drawn CPU glyph (family letter X / A / V in
 * the chip die) plus the readable architecture label (`amd64`, `arm64`, …).
 *
 * Unlike distribution marks this is part of the neutral WinUI chrome, so it uses
 * `bg-muted` + `text-secondary-foreground` tokens and never brand colors. The
 * accessible name keeps the full readable form (`amd64 (x86_64)`), while the
 * visible text stays compact for narrow screens.
 */
const props = defineProps<{ architecture?: ServerArchitecture | null }>();

const info = computed(() => resolveArchitectureMark(props.architecture));
const familyMark = computed(() => {
  const family = info.value?.family;
  if (family === 'x86') return 'x';
  if (family === 'arm64' || family === 'arm') return 'a';
  if (family === 'riscv') return 'v';
  return '';
});
</script>

<template>
  <span
    v-if="info"
    data-testid="arch-mark"
    :data-arch-family="info.family"
    :title="info.title"
    class="inline-flex min-w-0 items-center gap-1 rounded-control border border-divider bg-muted px-1.5 py-0.5 text-caption text-secondary-foreground"
  >
    <svg
      viewBox="0 0 24 24"
      class="size-4 shrink-0"
      fill="none"
      stroke="currentColor"
      stroke-width="1.5"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      <rect x="6.5" y="6.5" width="11" height="11" rx="2" />
      <path d="M9.5 3v3.5M14.5 3v3.5M9.5 17.5V21M14.5 17.5V21M3 9.5h3.5M3 14.5h3.5M17.5 9.5H21M17.5 14.5H21" />
      <path v-if="familyMark === 'x'" d="m9.8 9.8 4.4 4.4m0-4.4-4.4 4.4" />
      <path v-else-if="familyMark === 'a'" d="M9.8 15 12 9.5 14.2 15M10.7 12.9h2.6" />
      <path v-else-if="familyMark === 'v'" d="M9.8 9.5 12 14.9 14.2 9.5" />
      <circle v-else cx="12" cy="12" r="1.6" fill="currentColor" stroke="none" />
    </svg>
    <span class="truncate panel-mono">{{ info.label }}</span>
  </span>
</template>
