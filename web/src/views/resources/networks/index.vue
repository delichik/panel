<script setup lang="ts">
import { ref } from 'vue';
import { containerizationApi, type DockerNetworkDto } from '@/api/containerization';
import { useI18n } from '@/i18n';
import AppActionButton from '@/components/AppActionButton.vue';
import AppActionGroup from '@/components/AppActionGroup.vue';
import ResourcePage from '../_shared/ResourcePage.vue';
import { useDockerServers } from '../_shared/useDockerServers';

const { t, formatDateTime } = useI18n();
const items = ref<DockerNetworkDto[]>([]);
const loading = ref(false);
const error = ref('');
const { servers, serverId, loadingServers } = useDockerServers(load);
let networksRequestId = 0;
async function load() {
  const requestedServerId = serverId.value;
  const requestId = ++networksRequestId;
  items.value = [];
  error.value = '';
  if (!requestedServerId) {
    loading.value = false;
    return;
  }
  loading.value = true;
  try {
    const result = await containerizationApi.networks(requestedServerId);
    if (requestId !== networksRequestId || serverId.value !== requestedServerId) return;
    items.value = result;
    error.value = '';
  } catch (err) {
    if (requestId !== networksRequestId || serverId.value !== requestedServerId) return;
    error.value = err instanceof Error ? err.message : t('containerization.loadFailed');
  } finally {
    if (requestId === networksRequestId && serverId.value === requestedServerId) {
      loading.value = false;
    }
  }
}
</script>
<template>
  <ResourcePage v-model:server-id="serverId" :servers="servers" :loading-servers="loadingServers" :loading="loading" :error="error">
    <section class="resource-table-panel">
      <div class="app-card-header resource-header">
        <strong>{{ t('containerization.networks') }}</strong>
        <AppActionGroup context="toolbar" class="resource-actions">
          <AppActionButton icon="mdi-refresh" :label="t('common.refresh')" @click="load" />
        </AppActionGroup>
      </div>
      <div class="resource-table-body">
        <v-table>
          <thead>
            <tr>
              <th>{{ t('common.name') }}</th>
              <th>ID</th>
              <th>{{ t('containerization.driver') }}</th>
              <th>{{ t('containerization.scope') }}</th>
              <th>{{ t('containerization.created') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!items.length"><td colspan="5" class="text-center py-8">{{ t('containerization.empty') }}</td></tr>
            <tr v-for="item in items" :key="item.id">
              <td><strong>{{ item.name }}</strong></td>
              <td>{{ item.id.slice(0, 12) }}</td>
              <td>{{ item.driver }}</td>
              <td>{{ item.scope }}</td>
              <td>{{ item.created ? formatDateTime(item.created) : '-' }}</td>
            </tr>
          </tbody>
        </v-table>
      </div>
    </section>
  </ResourcePage>
</template>

<style scoped>
.resource-table-panel {
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.resource-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.resource-table-body {
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
}

@media (max-width: 760px) {
  .resource-header {
    align-items: stretch;
    flex-direction: column;
  }

  .resource-table-panel {
    min-height: auto;
  }

  .resource-table-body {
    overflow-x: auto;
    overflow-y: visible;
  }
}
</style>
