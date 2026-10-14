<script setup lang="ts">
import { ArrowLeft } from '@lucide/vue';
import IconButton from '@/components/ui/IconButton.vue';

defineProps<{ title: string; description?: string; backLabel?: string }>();
const emit = defineEmits<{ back: [] }>();
</script>

<template>
  <header class="flex shrink-0 items-start justify-between gap-4 border-b border-border px-4 py-3 max-lg:flex-col max-sm:px-3">
    <div class="min-w-0">
      <slot name="breadcrumb" />
      <div class="flex items-start gap-2">
        <IconButton v-if="backLabel" size="sm" variant="ghost" class="mt-0.5 shrink-0" :label="backLabel" @click="emit('back')">
          <ArrowLeft />
        </IconButton>
        <div class="min-w-0">
          <h1 class="m-0 text-lg font-semibold leading-7 text-foreground">{{ title }}</h1>
          <p v-if="description" class="m-0 mt-0.5 max-w-3xl text-sm leading-5 text-muted-foreground">{{ description }}</p>
        </div>
      </div>
    </div>
    <div v-if="$slots.actions" class="flex max-w-full shrink-0 flex-wrap items-center justify-end gap-2 max-lg:w-full max-lg:justify-start">
      <slot name="actions" />
    </div>
  </header>
</template>
