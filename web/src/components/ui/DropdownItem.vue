<script setup lang="ts">
import { computed, useAttrs } from 'vue';
import { cn } from './cn';

const props = withDefaults(defineProps<{ tone?: 'default' | 'danger'; disabled?: boolean }>(), { tone: 'default' });
const attrs = useAttrs();
const classes = computed(() => cn(
  'motion-menu-item flex w-full items-center gap-2 rounded-xl px-3 py-2 text-left text-sm font-medium transition-colors focus-visible:outline-none disabled:pointer-events-none disabled:opacity-45',
  props.tone === 'danger' ? 'text-danger hover:bg-danger-bg' : 'text-foreground/70 hover:bg-accent hover:text-foreground',
  attrs.class as string,
));
</script>

<template>
  <button type="button" role="menuitem" :disabled="disabled" :class="classes">
    <slot />
  </button>
</template>
