<script setup lang="ts">
defineProps<{ text: string }>();
</script>

<template>
  <span class="group relative inline-flex">
    <slot />
    <span class="motion-tooltip pointer-events-none absolute bottom-full left-1/2 z-40 mb-2 whitespace-nowrap rounded-lg border border-border bg-popover px-2 py-1 text-xs text-popover-foreground shadow-lg">
      {{ text }}
    </span>
  </span>
</template>
