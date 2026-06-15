<script setup lang="ts">
import { ref } from 'vue';
import { containerizationApi, type DockerVolumeDto } from '@/api/containerization';
import { useI18n } from '@/i18n';
import ResourcePage from '../_shared/ResourcePage.vue';
import { useDockerServers } from '../_shared/useDockerServers';

const { t, formatDateTime } = useI18n();
const items = ref<DockerVolumeDto[]>([]);
const loading = ref(false);
const error = ref('');
const pending = ref<DockerVolumeDto | null>(null);
const { servers, serverId, loadingServers } = useDockerServers(load);
async function load() {
  if (!serverId.value) return;
  loading.value = true;
  try { items.value = await containerizationApi.volumes(serverId.value); error.value = ''; }
  catch (err) { error.value = err instanceof Error ? err.message : t('containerization.loadFailed'); }
  finally { loading.value = false; }
}
async function remove() {
  if (!pending.value) return;
  await containerizationApi.deleteVolume(serverId.value, pending.value.name);
  pending.value = null;
  window.setTimeout(load, 800);
}
</script>
<template>
  <ResourcePage v-model:server-id="serverId" :servers="servers" :loading-servers="loadingServers" :loading="loading" :error="error">
    <div class="app-card-header"><strong>{{ t('containerization.volumes') }}</strong><v-btn size="small" variant="outlined" prepend-icon="mdi-refresh" @click="load">{{ t('common.refresh') }}</v-btn></div>
    <v-table><thead><tr><th>{{ t('common.name') }}</th><th>{{ t('containerization.driver') }}</th><th>{{ t('containerization.mountpoint') }}</th><th>{{ t('common.status') }}</th><th>{{ t('containerization.created') }}</th><th>{{ t('common.actions') }}</th></tr></thead>
      <tbody><tr v-if="!items.length"><td colspan="6" class="text-center py-8">{{ t('containerization.empty') }}</td></tr><tr v-for="item in items" :key="item.name"><td><strong>{{ item.name }}</strong></td><td>{{ item.driver }}</td><td class="text-caption">{{ item.mountpoint }}</td><td><v-chip size="small" variant="tonal" :color="item.inUse ? 'info' : 'default'">{{ item.inUse ? t('containerization.inUse', { count: item.containerCount }) : t('containerization.unused') }}</v-chip></td><td>{{ item.createdAt ? formatDateTime(item.createdAt) : '-' }}</td><td><v-btn size="small" color="error" variant="outlined" :disabled="item.inUse" @click="pending = item">{{ t('common.delete') }}</v-btn></td></tr></tbody>
    </v-table>
    <v-dialog :model-value="!!pending" width="440" @update:model-value="!$event && (pending = null)"><v-card class="app-dialog-card"><v-card-title class="app-dialog-title">{{ t('containerization.deleteVolume') }}</v-card-title><v-divider/><v-card-text class="app-dialog-body">{{ t('containerization.deleteVolumeMessage', { name: pending?.name }) }}</v-card-text><v-divider/><v-card-actions class="app-dialog-actions"><v-btn variant="text" @click="pending = null">{{ t('common.cancel') }}</v-btn><v-btn color="error" variant="flat" @click="remove">{{ t('common.delete') }}</v-btn></v-card-actions></v-card></v-dialog>
  </ResourcePage>
</template>
