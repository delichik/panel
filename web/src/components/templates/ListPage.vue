<template>
  <div class="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)_auto]">
    <div v-if="$slots.toolbar" class="border-b border-border p-4"><slot name="toolbar" /></div>
    <div class="min-h-0 overflow-auto p-4"><slot /></div>
    <div v-if="$slots.pagination" class="border-t border-border p-4"><slot name="pagination" /></div>
  </div>
</template>
