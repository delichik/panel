<script setup lang="ts">
import { Inbox } from '@lucide/vue';

defineProps<{ title: string; description?: string }>();
</script>

<template>
  <div class="grid min-h-48 place-items-center rounded-xl border border-dashed border-border bg-muted/30 p-8 text-center">
    <div class="grid max-w-sm gap-2 justify-items-center">
      <div class="grid size-10 place-items-center rounded-xl border border-border bg-muted text-muted-foreground">
        <Inbox class="size-5" aria-hidden="true" />
      </div>
      <h3 class="m-0 text-sm font-semibold text-foreground">{{ title }}</h3>
      <p v-if="description" class="m-0 text-sm leading-6 text-muted-foreground">{{ description }}</p>
      <div v-if="$slots.actions" class="mt-2 flex items-center justify-center gap-2">
        <slot name="actions" />
      </div>
    </div>
  </div>
</template>
