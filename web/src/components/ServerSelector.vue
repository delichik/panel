<script setup lang="ts">
import type { ServerDto } from '@/types/api';
import { useI18n } from '@/i18n';
import AppPagination from '@/components/AppPagination.vue';
import PageLoadingState from '@/components/PageLoadingState.vue';
import { usePagination } from '@/composables/usePagination';

const props = defineProps<{
  modelValue: string;
  servers: ServerDto[];
  loading?: boolean;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: string];
}>();
const { t } = useI18n();
const {
  page,
  pageSize,
  total,
  pageItems: pagedServers,
} = usePagination(() => props.servers);

// Extract 1-min load average from raw loadAverage string (e.g. "0.15 0.08 0.02")
function getOneMinLoad(loadAverage: string | null | undefined): string {
  if (!loadAverage) return '-';
  const parts = loadAverage.trim().split(/\s+/);
  return parts[0] || '-';
}
</script>

<template>
  <v-card :loading="loading" class="server-selector d-flex flex-column h-100 overflow-hidden" variant="outlined">
    <v-card-item class="bg-surface-variant py-3 flex-shrink-0">
      <div class="d-flex justify-space-between align-center">
        <v-card-title class="text-subtitle-1 font-weight-bold my-0 py-0">{{ t('shared.serverSelector.title') }}</v-card-title>
        <v-chip size="small" color="primary">{{ servers.length }}</v-chip>
      </div>
    </v-card-item>

    <v-card-text class="flex-grow-1 overflow-y-auto pa-3">
      <PageLoadingState v-if="loading && servers.length === 0" min-height="220px" />
      <div v-else-if="servers.length" class="server-cards">
        <button
          v-for="server in pagedServers"
          :key="server.id"
          class="server-item"
          :class="{ 'selected': server.id === modelValue }"
          type="button"
          @click="emit('update:modelValue', server.id)"
        >
          <span class="server-item__main">
            <span class="status-dot" :class="server.reachable ? 'success' : 'warning'" />
            <span class="server-item__name text-truncate">{{ server.name }}</span>
          </span>
          <span class="server-item__meta">
            {{ t('shared.serverSelector.load') }}: {{ getOneMinLoad(server.loadAverage) }}
          </span>
        </button>
      </div>
      <div v-else class="text-center py-6 text-medium-emphasis h-100 d-flex flex-column align-center justify-center">
        <v-icon size="40" color="medium-emphasis" class="mb-2">mdi-server-off</v-icon>
        <div class="text-caption">{{ t('shared.serverSelector.empty') }}</div>
      </div>
    </v-card-text>
    <AppPagination v-model:page="page" v-model:page-size="pageSize" :total="total" />
  </v-card>
</template>

<style scoped>
.server-selector {
  min-width: 0;
}

.server-cards {
  display: grid;
  gap: 8px;
}

.server-item {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 11px 12px;
  border: 1px solid transparent;
  border-radius: 8px;
  background: transparent;
  color: inherit;
  text-align: left;
  cursor: pointer;
  transition: background-color 0.16s ease, border-color 0.16s ease;
}

.server-item:hover {
  background: rgba(var(--v-theme-on-surface), 0.025);
}

.server-item.selected {
  border-color: rgba(var(--v-theme-primary), 0.26);
  background: rgba(var(--v-theme-primary), 0.06);
}

.server-item__main {
  display: flex;
  align-items: center;
  gap: 9px;
  min-width: 0;
}

.server-item__name {
  display: block;
  min-width: 0;
  font-size: 0.9rem;
  font-weight: 700;
}

.server-item__meta {
  color: var(--lp-text-muted);
  font-size: 0.76rem;
  font-weight: 650;
  white-space: nowrap;
}

.status-dot {
  flex: 0 0 auto;
  width: 9px;
  height: 9px;
  border-radius: 999px;
}

.status-dot.success {
  background-color: rgb(var(--v-theme-success));
  box-shadow: 0 0 0 4px rgba(var(--v-theme-success), 0.12);
}

.status-dot.warning {
  background-color: rgb(var(--v-theme-warning));
  box-shadow: 0 0 0 4px rgba(var(--v-theme-warning), 0.14);
}
</style>
