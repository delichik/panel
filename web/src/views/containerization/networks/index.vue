<script setup lang="ts">
import { ref } from 'vue';
import { containerizationApi, type DockerNetworkDto } from '@/api/containerization';
import { useI18n } from '@/i18n';
import ResourcePage from '../_shared/ResourcePage.vue';
import { useDockerServers } from '../_shared/useDockerServers';

const { t, formatDateTime } = useI18n();
const items = ref<DockerNetworkDto[]>([]);
const loading = ref(false);
const error = ref('');
const { servers, serverId, loadingServers } = useDockerServers(load);
async function load() {
  if (!serverId.value) return;
  loading.value = true;
  try { items.value = await containerizationApi.networks(serverId.value); error.value = ''; }
  catch (err) { error.value = err instanceof Error ? err.message : t('containerization.loadFailed'); }
  finally { loading.value = false; }
}
</script>
<template>
  <ResourcePage v-model:server-id="serverId" :servers="servers" :loading-servers="loadingServers" :loading="loading" :error="error">
    <div class="app-card-header"><strong>{{ t('containerization.networks') }}</strong><v-btn size="small" variant="outlined" prepend-icon="mdi-refresh" @click="load">{{ t('common.refresh') }}</v-btn></div>
    <v-table><thead><tr><th>{{ t('common.name') }}</th><th>ID</th><th>{{ t('containerization.driver') }}</th><th>{{ t('containerization.scope') }}</th><th>{{ t('containerization.created') }}</th></tr></thead>
      <tbody><tr v-if="!items.length"><td colspan="5" class="text-center py-8">{{ t('containerization.empty') }}</td></tr><tr v-for="item in items" :key="item.id"><td><strong>{{ item.name }}</strong></td><td>{{ item.id.slice(0, 12) }}</td><td>{{ item.driver }}</td><td>{{ item.scope }}</td><td>{{ item.created ? formatDateTime(item.created) : '-' }}</td></tr></tbody>
    </v-table>
  </ResourcePage>
</template>
