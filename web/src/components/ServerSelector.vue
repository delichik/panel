<script setup lang="ts">
import type { ServerDto } from '@/types/api';
import { useI18n } from '@/i18n';
import AppSelectorItem from '@/components/AppSelectorItem.vue';
import AppSelectorPanel from '@/components/AppSelectorPanel.vue';
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
  <AppSelectorPanel
    class="server-selector"
    :title="t('shared.serverSelector.title')"
    :count="servers.length"
    :loading="loading"
    :empty="servers.length === 0"
    empty-icon="mdi-server-off"
    :empty-text="t('shared.serverSelector.empty')"
    :page="page"
    :page-size="pageSize"
    :total="total"
    @update:page="page = $event"
    @update:page-size="pageSize = $event"
  >
        <AppSelectorItem
          v-for="server in pagedServers"
          :key="server.id"
          :selected="server.id === modelValue"
          @select="emit('update:modelValue', server.id)"
        >
          <span class="server-item__main">
            <span class="status-dot" :class="server.reachable ? 'success' : 'warning'" />
            <span class="server-item__name text-truncate">{{ server.name }}</span>
          </span>
          <span class="server-item__meta">
            {{ t('shared.serverSelector.load') }}: {{ getOneMinLoad(server.loadAverage) }}
          </span>
        </AppSelectorItem>
  </AppSelectorPanel>
</template>

<style scoped>
.server-selector {
  min-width: 0;
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
