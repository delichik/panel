<script setup lang="ts">
import { Inbox } from '@lucide/vue';
import { computed } from 'vue';
import { cn } from './cn';

const props = withDefaults(defineProps<{
  title: string;
  description?: string;
  /** page = dashed panel; inset = light in-table/filter empty; plain = text only */
  variant?: 'page' | 'inset' | 'plain';
}>(), {
  variant: 'page',
});

const rootClass = computed(() => cn(
  'grid place-items-center text-center',
  props.variant === 'page' && 'min-h-48 rounded-xl border border-dashed border-border-strong bg-muted/30 p-8',
  props.variant === 'inset' && 'min-h-32 px-4 py-8',
  props.variant === 'plain' && 'min-h-24 px-2 py-6',
));
</script>

<template>
  <div :class="rootClass">
    <div class="grid max-w-sm gap-2 justify-items-center">
      <div
        v-if="variant !== 'plain'"
        class="grid size-10 place-items-center rounded-lg border border-border bg-card text-secondary-foreground"
        :class="variant === 'inset' ? 'size-9 rounded-lg' : undefined"
      >
        <Inbox class="size-5" aria-hidden="true" />
      </div>
      <h3 class="m-0 text-sm font-semibold text-foreground">{{ title }}</h3>
      <p v-if="description" class="m-0 text-sm leading-6 text-secondary-foreground">{{ description }}</p>
      <div v-if="$slots.actions" class="mt-2 flex items-center justify-center gap-2">
        <slot name="actions" />
      </div>
    </div>
  </div>
</template>
