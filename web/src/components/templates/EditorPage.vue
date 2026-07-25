<template>
  <div class="grid h-full min-h-0 grid-rows-[minmax(0,1fr)_auto]">
    <div class="min-h-0 overflow-auto p-6 max-sm:p-4"><slot /></div>
    <footer v-if="$slots.footer" class="border-t border-border bg-background p-4">
      <slot name="footer" />
    </footer>
  </div>
</template>
