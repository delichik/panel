<script setup lang="ts">
import { Inbox } from '@/icons';
import { computed } from 'vue';
import { cn } from './cn';

const props = withDefaults(defineProps<{
  title: string;
  description?: string;
  /** page = framed panel; inset = light in-table/filter empty; plain = text only */
  variant?: 'page' | 'inset' | 'plain';
}>(), {
  variant: 'page',
});

const rootClass = computed(() => cn(
  'grid place-items-center text-center',
  props.variant === 'page' && 'min-h-48 rounded-overlay border border-stroke bg-card p-8',
  props.variant === 'inset' && 'min-h-32 px-4 py-8',
  props.variant === 'plain' && 'min-h-24 px-2 py-6',
));
</script>

<template>
  <div :class="rootClass">
    <div class="grid max-w-sm gap-2 justify-items-center">
      <div
        v-if="variant !== 'plain'"
        class="grid size-10 place-items-center rounded-full border border-stroke bg-control text-secondary-foreground"
        :class="variant === 'inset' ? 'size-9' : undefined"
      >
        <Inbox class="size-5" aria-hidden="true" />
      </div>
      <h3 class="m-0 text-body-strong text-foreground">{{ title }}</h3>
      <p v-if="description" class="m-0 text-body text-secondary-foreground">{{ description }}</p>
      <div v-if="$slots.actions" class="mt-2 flex items-center justify-center gap-2">
        <slot name="actions" />
      </div>
    </div>
  </div>
</template>
