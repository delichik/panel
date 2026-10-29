<script setup lang="ts">
withDefaults(defineProps<{ text: string; triggerClass?: string }>(), { triggerClass: '' });
</script>

<template>
  <span class="group relative inline-flex" :class="triggerClass">
    <slot />
    <span class="motion-tooltip panel-shadow-overlay pointer-events-none absolute bottom-full left-1/2 z-40 mb-2 whitespace-nowrap rounded-lg border border-border-strong bg-popover px-2.5 py-1 text-xs font-medium text-popover-foreground">
      {{ text }}
    </span>
  </span>
</template>
