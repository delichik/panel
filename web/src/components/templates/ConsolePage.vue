<script setup lang="ts">
import PageHeader from '@/components/shell/PageHeader.vue';

defineProps<{ title: string; description?: string }>();
</script>

<template>
  <div class="grid h-full min-h-0 min-w-0 grid-rows-[auto_minmax(0,1fr)] max-lg:block max-lg:min-h-[calc(100dvh-56px)]">
    <PageHeader :title="title" :description="description">
      <template v-if="$slots.actions" #actions><slot name="actions" /></template>
      <template v-if="$slots.breadcrumb" #breadcrumb><slot name="breadcrumb" /></template>
    </PageHeader>
    <div class="min-h-0 min-w-0 overflow-y-auto overflow-x-hidden p-6 max-sm:p-4">
      <slot />
    </div>
  </div>
</template>
