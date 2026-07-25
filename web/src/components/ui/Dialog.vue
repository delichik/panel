<script setup lang="ts">
import { X } from '@lucide/vue';
import IconButton from './IconButton.vue';

defineProps<{
  open: boolean;
  title: string;
  description?: string;
  closeLabel?: string;
}>();
defineEmits<{ 'update:open': [value: boolean] }>();
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="motion-overlay fixed inset-0 z-50 grid place-items-center bg-overlay p-4" role="presentation" @click.self="$emit('update:open', false)">
      <section class="motion-popover max-h-[min(720px,calc(100dvh-32px))] w-full max-w-lg overflow-hidden rounded-2xl border border-border bg-popover text-popover-foreground shadow-2xl" role="dialog" aria-modal="true" :aria-label="title">
        <header class="flex items-start justify-between gap-4 border-b border-border px-5 py-4">
          <div class="min-w-0">
            <h2 class="m-0 text-base font-semibold text-foreground">{{ title }}</h2>
            <p v-if="description" class="m-0 mt-1 text-sm leading-6 text-muted-foreground">{{ description }}</p>
          </div>
          <IconButton :label="closeLabel || 'Close'" size="sm" @click="$emit('update:open', false)">
            <X />
          </IconButton>
        </header>
        <div class="max-h-[calc(100dvh-220px)] overflow-auto px-5 py-4">
          <slot />
        </div>
        <footer v-if="$slots.footer" class="flex justify-end gap-2 border-t border-border px-5 py-4">
          <slot name="footer" />
        </footer>
      </section>
    </div>
  </Teleport>
</template>
