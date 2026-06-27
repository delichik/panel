<script setup lang="ts">
import { computed } from 'vue';
import { useDisplay } from 'vuetify';
import { useI18n } from '@/i18n';

const props = withDefaults(defineProps<{
  page: number;
  pageSize: number;
  total: number;
  pageSizes?: number[];
  compact?: boolean;
}>(), {
  pageSizes: () => [10, 20, 50, 100],
  compact: false,
});

const emit = defineEmits<{
  'update:page': [value: number];
  'update:pageSize': [value: number];
}>();

const display = useDisplay();
const { t } = useI18n();
const totalPages = computed(() => Math.max(1, Math.ceil(props.total / props.pageSize)));
const paginationVisible = computed(() => props.compact || display.smAndDown.value ? 5 : 10);

function updatePageSize(value: number) {
  emit('update:pageSize', value);
  emit('update:page', 1);
}
</script>

<template>
  <div v-if="total > 0" class="app-pagination" :class="{ 'app-pagination--compact': compact }">
    <div v-if="!compact" class="app-pagination__total text-caption text-medium-emphasis">
      {{ t('common.total', { count: total }) }}
    </div>
    <v-select
      v-if="!compact"
      :model-value="pageSize"
      :items="pageSizes"
      :label="t('common.pageSize')"
      density="compact"
      hide-details
      variant="outlined"
      class="app-pagination__size"
      @update:model-value="updatePageSize"
    />
    <v-pagination
      :model-value="page"
      :length="totalPages"
      density="compact"
      :total-visible="paginationVisible"
      @update:model-value="emit('update:page', Number($event))"
    >
      <template #item="{ props: itemProps, page: itemPage, isActive }">
        <button
          v-bind="itemProps"
          type="button"
          class="app-pagination__page"
          :class="{ 'app-pagination__page--active': isActive }"
        >
          {{ itemPage }}
        </button>
      </template>
    </v-pagination>
  </div>
</template>

<style scoped>
.app-pagination {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
  padding: 12px 14px;
  border-top: 1px solid var(--lp-border);
  background: color-mix(in srgb, var(--lp-surface-container), transparent 42%);
}

.app-pagination__total {
  margin-right: auto;
  white-space: nowrap;
}

.app-pagination__size {
  max-width: 118px;
  flex: 0 0 118px;
}

.app-pagination--compact {
  justify-content: center;
  padding-inline: 8px;
}

.app-pagination__page {
  display: inline-grid;
  place-items: center;
  min-width: 32px;
  height: 32px;
  padding: 0 8px;
  border: 1px solid transparent;
  border-radius: var(--lp-radius-sm);
  background: transparent;
  color: var(--lp-text);
  font: inherit;
  font-size: 0.875rem;
  font-weight: 700;
  line-height: 1;
  cursor: pointer;
}

.app-pagination__page:hover {
  background: rgba(var(--v-theme-primary), 0.08);
  color: var(--lp-text);
}

.app-pagination__page--active {
  border-color: rgba(var(--v-theme-primary), 0.68);
  background: transparent;
  color: var(--lp-text);
}

.app-pagination__page:disabled {
  cursor: default;
  opacity: 0.5;
}

@media (max-width: 760px) {
  .app-pagination {
    align-items: stretch;
    flex-direction: column;
  }

  .app-pagination__total {
    margin-right: 0;
  }

  .app-pagination__size {
    max-width: none;
    flex-basis: auto;
  }
}
</style>
