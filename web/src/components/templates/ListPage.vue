<script setup lang="ts">
/**
 * WinUI list workbench: toolbar row + table scroll region + pagination row.
 * No outer card frame — the Table itself owns the single card-stroke surface,
 * so this stays a pure layout container and the scroll contract (table body
 * scrolls, page does not) is unchanged.
 *
 * Page-level states live in the template so every family renders them in the
 * same place: pass `state` and use the matching slot (`#error`, `#loading`,
 * `#empty`) instead of branching around a hand-rolled Table. `state` defaults
 * to `ready`, so existing pages keep using the default slot unchanged.
 */
withDefaults(defineProps<{
  state?: 'ready' | 'loading' | 'error' | 'empty';
}>(), {
  state: 'ready',
});
</script>

<template>
  <div class="grid h-full min-h-0 min-w-0 grid-rows-page-footer gap-3">
    <div v-if="$slots.toolbar" class="shrink-0"><slot name="toolbar" /></div>
    <div class="min-h-0 min-w-0 overflow-hidden">
      <slot v-if="state === 'ready'" />
      <slot v-else-if="state === 'loading'" name="loading" />
      <slot v-else-if="state === 'error'" name="error" />
      <slot v-else name="empty" />
    </div>
    <div v-if="$slots.pagination" class="shrink-0 text-secondary-foreground"><slot name="pagination" /></div>
  </div>
</template>
