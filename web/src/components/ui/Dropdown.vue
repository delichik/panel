<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue';

defineProps<{ align?: 'left' | 'right' }>();

const open = ref(false);
const root = ref<HTMLElement | null>(null);

function onDocumentClick(event: MouseEvent) {
  if (root.value && !root.value.contains(event.target as Node)) open.value = false;
}

onMounted(() => document.addEventListener('click', onDocumentClick));
onBeforeUnmount(() => document.removeEventListener('click', onDocumentClick));
</script>

<template>
  <div ref="root" class="relative inline-block text-left">
    <div @click.stop="open = !open">
      <slot name="trigger" :open="open" />
    </div>
    <div
      v-if="open"
      class="motion-popover absolute z-40 mt-2 min-w-52 overflow-hidden rounded-2xl border border-border bg-popover p-1 text-popover-foreground shadow-xl"
      :class="align === 'left' ? 'left-0' : 'right-0'"
      role="menu"
      @click="open = false"
    >
      <slot />
    </div>
  </div>
</template>
