<script setup lang="ts">
import { computed } from 'vue';
import AppMasterDetailWorkspace from '@/components/AppMasterDetailWorkspace.vue';
import ServerSelector from '@/components/ServerSelector.vue';
import PageLoadingState from '@/components/PageLoadingState.vue';
import type { ServerDto } from '@/types/api';

const props = defineProps<{
  servers: ServerDto[];
  serverId: string;
  loadingServers: boolean;
  loading: boolean;
  error: string;
}>();

defineEmits<{ 'update:serverId': [value: string] }>();

const selectedServer = computed(() => props.servers.find((server) => server.id === props.serverId) ?? null);
</script>

<template>
  <div class="page-shell">
    <v-alert v-if="error" type="error" variant="tonal">{{ error }}</v-alert>
    <AppMasterDetailWorkspace>
      <template #aside>
        <ServerSelector
          :model-value="serverId"
          :servers="servers"
          :loading="loadingServers"
          @update:model-value="$emit('update:serverId', $event)"
        />
      </template>
      <v-card variant="outlined" class="resource-panel">
        <div class="resource-panel__context">
          <div class="resource-panel__server min-width-0">
            <span class="resource-panel__status" :class="{ 'resource-panel__status--ok': selectedServer?.reachable }" />
            <div class="min-width-0">
              <div class="resource-panel__server-name text-truncate">{{ selectedServer?.name || '-' }}</div>
              <div class="resource-panel__server-host text-truncate">{{ selectedServer ? `${selectedServer.host}:${selectedServer.port}` : '-' }}</div>
            </div>
          </div>
        </div>
        <div class="resource-panel__body">
          <PageLoadingState v-if="loading" min-height="280px" />
          <slot v-else />
        </div>
      </v-card>
    </AppMasterDetailWorkspace>
  </div>
</template>

<style scoped>
.resource-panel {
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}

.resource-panel__context {
  flex: 0 0 auto;
  padding: 14px 16px;
  border-bottom: 1px solid color-mix(in srgb, var(--lp-border), transparent 12%);
  background: color-mix(in srgb, var(--lp-surface-container), transparent 42%);
}

.resource-panel__server {
  display: flex;
  align-items: center;
  gap: 10px;
}

.resource-panel__status {
  width: 9px;
  height: 9px;
  border-radius: 999px;
  background: rgb(var(--v-theme-warning));
  box-shadow: 0 0 0 4px rgba(var(--v-theme-warning), 0.12);
}

.resource-panel__status--ok {
  background: rgb(var(--v-theme-success));
  box-shadow: 0 0 0 4px rgba(var(--v-theme-success), 0.1);
}

.resource-panel__server-name {
  color: var(--lp-text);
  font-size: 0.98rem;
  font-weight: 780;
}

.resource-panel__server-host {
  margin-top: 2px;
  color: var(--lp-text-muted);
  font-size: 0.78rem;
  font-variant-numeric: tabular-nums;
}

.resource-panel__body {
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
}

.resource-panel__body :deep(.app-card-header) {
  min-height: 58px;
  padding: 14px 16px 10px;
}

.resource-panel__body :deep(.v-table__wrapper) {
  max-height: none;
}

.min-width-0 {
  min-width: 0;
}

@media (max-width: 760px) {
  .resource-panel {
    overflow: visible;
  }

  .resource-panel__body {
    overflow-x: auto;
    overflow-y: visible;
  }
}
</style>
