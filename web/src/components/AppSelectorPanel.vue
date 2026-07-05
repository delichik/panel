<script setup lang="ts">
import AppActionGroup from '@/components/AppActionGroup.vue';
import AppPagination from '@/components/AppPagination.vue';
import PageLoadingState from '@/components/PageLoadingState.vue';

withDefaults(defineProps<{
  title: string;
  loading?: boolean;
  empty?: boolean;
  emptyIcon?: string;
  emptyText?: string;
  page?: number;
  pageSize?: number;
  total?: number;
}>(), {
  loading: false,
  empty: false,
  emptyIcon: 'mdi-inbox-outline',
  emptyText: '',
  page: 1,
  pageSize: 10,
  total: 0,
});

const emit = defineEmits<{
  'update:page': [value: number];
  'update:pageSize': [value: number];
}>();
</script>

<template>
  <v-card class="app-selector-panel" variant="outlined" :loading="loading && !empty">
    <div class="app-selector-panel__header">
      <div class="app-selector-panel__header-main min-width-0">
        <div v-if="$slots.leading" class="app-selector-panel__header-leading">
          <slot name="leading" />
        </div>
        <div class="app-selector-panel__heading min-width-0">
          <div class="text-subtitle-1 font-weight-bold text-truncate">{{ title }}</div>
          <slot name="subtitle" />
        </div>
      </div>
      <AppActionGroup v-if="$slots.actions" context="selector" class="app-selector-panel__header-actions">
        <slot name="actions" />
      </AppActionGroup>
    </div>

    <div class="app-selector-panel__body">
      <PageLoadingState v-if="loading && empty" compact min-height="220px" />
      <div v-else-if="!empty" class="app-selector-panel__items">
        <slot />
      </div>
      <slot v-else name="empty">
        <div class="app-selector-panel__empty text-medium-emphasis">
          <v-icon size="40" color="medium-emphasis">{{ emptyIcon }}</v-icon>
          <div class="text-body-2">{{ emptyText }}</div>
        </div>
      </slot>
    </div>

    <AppPagination
      v-if="total > 0"
      :page="page"
      :page-size="pageSize"
      :total="total"
      compact
      @update:page="emit('update:page', $event)"
      @update:page-size="emit('update:pageSize', $event)"
    />
  </v-card>
</template>

<style scoped>
.app-selector-panel {
  display: flex;
  flex-direction: column;
  width: 100%;
  min-width: 0;
  min-height: 0;
  height: 100%;
  overflow: hidden;
  border-radius: 10px !important;
  background:
    linear-gradient(180deg, color-mix(in srgb, rgb(var(--v-theme-primary)), transparent 98%), transparent 220px),
    var(--lp-surface) !important;
}

.app-selector-panel__header {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-width: 0;
  min-height: 64px;
  padding: 13px 14px;
  border-bottom: 1px solid color-mix(in srgb, var(--lp-border), transparent 12%);
  background:
    linear-gradient(90deg, rgba(var(--v-theme-primary), 0.07), transparent 78%),
    color-mix(in srgb, var(--lp-surface-container), transparent 34%);
}

.app-selector-panel__header-main {
  display: flex;
  align-items: center;
}

.app-selector-panel__header-leading {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  align-self: stretch;
  margin-right: 8px;
}

.app-selector-panel__heading {
  display: grid;
  gap: 2px;
}

.app-selector-panel__heading :deep(.text-subtitle-1) {
  font-size: 0.96rem !important;
  font-weight: 800 !important;
  letter-spacing: 0;
}

.app-selector-panel__header-actions {
  flex: 0 0 auto;
}

.app-selector-panel__body {
  flex: 1 1 auto;
  width: 100%;
  min-width: 0;
  min-height: 0;
  padding: 10px 9px;
  overflow: auto;
}

.app-selector-panel__items {
  display: grid;
  gap: 8px;
  align-content: start;
  grid-auto-rows: max-content;
  min-width: 0;
}

.app-selector-panel__empty {
  display: grid;
  place-items: center;
  align-content: center;
  gap: 10px;
  min-height: 220px;
  padding: 24px;
  border: 1px dashed color-mix(in srgb, var(--lp-border), transparent 8%);
  border-radius: var(--lp-radius-sm);
  background: color-mix(in srgb, var(--lp-surface-container), transparent 30%);
  text-align: center;
}

@media (max-width: 760px) {
  .app-selector-panel {
    height: auto;
    overflow: visible;
  }

  .app-selector-panel__body {
    overflow: visible;
  }
}
</style>
