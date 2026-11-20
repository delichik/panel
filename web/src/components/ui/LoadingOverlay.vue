<script setup lang="ts">
import ProgressRing from './ProgressRing.vue';
import { zIndex } from './states';

withDefaults(defineProps<{ label?: string }>(), { label: '' });
</script>

<template>
  <div class="motion-overlay loading-overlay absolute inset-0 grid place-items-center gap-2 rounded-inherit bg-layer/80 text-secondary-foreground backdrop-blur-subtle" :class="zIndex.pageOverlay" role="status" aria-live="polite">
    <ProgressRing :size="24" class="text-primary" aria-hidden="true" />
    <span v-if="label" class="text-body font-medium">{{ label }}</span>
  </div>
</template>
