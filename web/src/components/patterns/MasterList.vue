<script setup lang="ts" generic="T extends { id: string; disabled?: boolean }">
import EmptyState from '../ui/EmptyState.vue';

withDefaults(defineProps<{
  items: T[];
  selectedId?: string;
  emptyTitle: string;
  emptyDescription?: string;
  ariaLabel?: string;
  loading?: boolean;
  loadingRows?: number;
}>(), {
  selectedId: '',
  loading: false,
  loadingRows: 6,
});

defineEmits<{ select: [item: T] }>();
</script>

<template>
  <section class="grid min-h-0 grid-rows-[minmax(0,1fr)]" :aria-label="ariaLabel" :aria-busy="loading ? 'true' : undefined">
    <div v-if="loading && items.length === 0" class="min-h-0 overflow-auto rounded-xl border border-border bg-background" aria-hidden="true">
      <div v-for="item in loadingRows" :key="item" class="grid gap-2 border-b border-border p-3 last:border-b-0">
        <div class="motion-skeleton h-4 w-36 rounded bg-muted animate-pulse" />
        <div class="motion-skeleton h-3 w-56 max-w-full rounded bg-muted animate-pulse" />
      </div>
    </div>
    <EmptyState v-else-if="items.length === 0" :title="emptyTitle" :description="emptyDescription">
      <template v-if="$slots.emptyActions" #actions>
        <slot name="emptyActions" />
      </template>
    </EmptyState>
    <div v-else class="min-h-0 overflow-auto rounded-xl border border-border bg-background">
      <button
        v-for="item in items"
        :key="item.id"
        type="button"
        :disabled="item.disabled"
        :aria-current="selectedId === item.id ? 'true' : undefined"
        class="motion-list-item block w-full border-b border-border p-3 text-left transition-colors last:border-b-0 hover:bg-accent/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring/40 disabled:cursor-not-allowed disabled:opacity-50 aria-current:bg-primary/10"
        @click="$emit('select', item)"
      >
        <slot name="item" :item="item" :selected="selectedId === item.id" />
      </button>
    </div>
  </section>
</template>
