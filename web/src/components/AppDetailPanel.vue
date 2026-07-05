<script setup lang="ts">
import PageLoadingState from '@/components/PageLoadingState.vue';

withDefaults(defineProps<{
  loading?: boolean;
  loadingMinHeight?: string;
  empty?: boolean;
  emptyText?: string;
}>(), {
  loading: false,
  loadingMinHeight: '300px',
  empty: false,
  emptyText: '',
});
</script>

<template>
  <v-card variant="outlined" class="app-detail-panel">
    <PageLoadingState v-if="loading" :min-height="loadingMinHeight" />
    <template v-else-if="!empty">
      <div v-if="$slots.header" class="app-detail-panel__header">
        <slot name="header" />
      </div>
      <div v-if="$slots.body" class="app-detail-panel__body">
        <slot name="body" />
      </div>
      <slot />
    </template>
    <slot v-else name="empty">
      <div class="app-detail-panel__empty text-medium-emphasis">{{ emptyText }}</div>
    </slot>
  </v-card>
</template>

<style scoped>
.app-detail-panel {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
}

.app-detail-panel__header {
  display: flex;
  flex: 0 0 auto;
  align-items: flex-start;
  justify-content: space-between;
  gap: 14px;
  padding: 16px 18px;
  border-bottom: 1px solid color-mix(in srgb, var(--lp-border), transparent 14%);
  background: color-mix(in srgb, var(--lp-surface-container), transparent 46%);
}

.app-detail-panel__header :deep(.app-detail-actions) {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  flex-wrap: wrap;
}

.app-detail-panel__body {
  display: grid;
  flex: 1 1 auto;
  gap: 16px;
  align-content: start;
  min-height: 0;
  padding: 16px 18px 18px;
  overflow: auto;
}

.app-detail-panel__empty {
  display: grid;
  flex: 1 1 auto;
  place-items: center;
  min-height: 220px;
  padding: 32px;
  text-align: center;
}

@media (max-width: 760px) {
  .app-detail-panel,
  .app-detail-panel__body {
    overflow: visible;
  }

  .app-detail-panel__header {
    align-items: stretch;
    flex-direction: column;
    padding: 16px;
  }

  .app-detail-panel__header :deep(.app-detail-actions) {
    justify-content: flex-start;
  }

  .app-detail-panel__body {
    padding: 16px;
  }
}
</style>
