<script setup lang="ts">
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
      <div class="app-selector-panel__header-actions">
        <slot name="actions" />
      </div>
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
}

.app-selector-panel__header {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-width: 0;
  padding: 12px 16px;
  background: rgb(var(--v-theme-surface-variant));
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

.app-selector-panel__header-actions {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 8px;
}

.app-selector-panel__body {
  flex: 1 1 auto;
  width: 100%;
  min-width: 0;
  min-height: 0;
  padding: 10px;
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
