<script setup lang="ts">
defineProps<{ title: string; description?: string }>();
</script>

<template>
  <header class="flex shrink-0 items-start justify-between gap-4 border-b border-border px-6 py-5 max-lg:flex-col max-sm:px-4">
    <div class="min-w-0">
      <slot name="breadcrumb" />
      <h2 class="m-0 text-xl font-semibold leading-7 text-foreground">{{ title }}</h2>
      <p v-if="description" class="m-0 mt-1 max-w-3xl text-sm leading-6 text-muted-foreground">{{ description }}</p>
    </div>
    <div v-if="$slots.actions" class="flex max-w-full shrink-0 flex-wrap items-center justify-end gap-2 max-lg:w-full max-lg:justify-start">
      <slot name="actions" />
    </div>
  </header>
</template>
