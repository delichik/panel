<script setup lang="ts">
import { LoaderCircle } from '@lucide/vue';

withDefaults(defineProps<{ label?: string }>(), { label: '' });
</script>

<template>
  <div class="motion-overlay loading-overlay absolute inset-0 z-20 grid place-items-center gap-2 rounded-[inherit] bg-background/70 text-muted-foreground backdrop-blur-[1px]" role="status" aria-live="polite">
    <LoaderCircle class="size-6 animate-spin" aria-hidden="true" />
    <span v-if="label" class="text-sm leading-5">{{ label }}</span>
  </div>
</template>
