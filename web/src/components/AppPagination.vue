<script setup lang="ts">
import { computed } from 'vue';
import { useDisplay } from 'vuetify';
import { useI18n } from '@/i18n';

const props = withDefaults(defineProps<{
  page: number;
  pageSize: number;
  total: number;
  pageSizes?: number[];
}>(), {
  pageSizes: () => [10, 20, 50, 100],
});

const emit = defineEmits<{
  'update:page': [value: number];
  'update:pageSize': [value: number];
}>();

const display = useDisplay();
const { t } = useI18n();
const totalPages = computed(() => Math.max(1, Math.ceil(props.total / props.pageSize)));
const paginationVisible = computed(() => display.smAndDown.value ? 5 : 10);

function updatePageSize(value: number) {
  emit('update:pageSize', value);
  emit('update:page', 1);
}
</script>

<template>
  <div v-if="total > 0" class="app-pagination">
    <div class="app-pagination__total text-caption text-medium-emphasis">
      {{ t('common.total', { count: total }) }}
    </div>
    <v-select
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
    />
  </div>
</template>

<style scoped>
.app-pagination {
  display: flex;
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

.app-pagination :deep(.v-pagination__item--is-active .v-btn) {
  background: rgb(var(--v-theme-primary));
  color: rgb(var(--v-theme-on-primary));
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
