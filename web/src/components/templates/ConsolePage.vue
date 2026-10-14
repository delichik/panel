<script setup lang="ts">
import PageHeader from '@/components/shell/PageHeader.vue';

const props = withDefaults(defineProps<{
  title: string;
  description?: string;
  backLabel?: string;
  /**
   * Scroll the body internally (default). Pages that manage their own region
   * scrolling (for example a tabbed covered detail with tables) pass false and
   * keep every panel's scrolling inside its own content area.
   */
  bodyScroll?: boolean;
}>(), { bodyScroll: true });
const emit = defineEmits<{ back: [] }>();
</script>

<template>
  <div class="grid h-full min-h-0 min-w-0 grid-rows-[auto_minmax(0,1fr)] max-lg:block max-lg:min-h-[calc(100dvh-56px)]">
    <PageHeader :title="title" :description="description" :back-label="backLabel" @back="emit('back')">
      <template v-if="$slots.actions" #actions><slot name="actions" /></template>
      <template v-if="$slots.breadcrumb" #breadcrumb><slot name="breadcrumb" /></template>
    </PageHeader>
    <div class="min-h-0 min-w-0 p-4 max-lg:overflow-visible max-sm:p-3" :class="bodyScroll ? 'overflow-y-auto overflow-x-hidden' : 'overflow-hidden'">
      <slot />
    </div>
  </div>
</template>
