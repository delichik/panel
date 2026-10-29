<template>
  <div class="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)_auto] gap-3">
    <div v-if="$slots.toolbar" class="shrink-0"><slot name="toolbar" /></div>
    <div class="min-h-0 overflow-hidden"><slot /></div>
    <div v-if="$slots.pagination" class="shrink-0 border-t border-border pt-3 text-secondary-foreground"><slot name="pagination" /></div>
  </div>
</template>
