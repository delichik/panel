<script setup lang="ts">
import { computed, ref } from 'vue';
import { containerizationApi, type DockerVolumeDto } from '@/api/containerization';
import { useI18n } from '@/i18n';
import ResourcePage from '../_shared/ResourcePage.vue';
import { useDockerServers } from '../_shared/useDockerServers';

const { t, formatDateTime } = useI18n();
const items = ref<DockerVolumeDto[]>([]);
const loading = ref(false);
const error = ref('');
const pending = ref<DockerVolumeDto | null>(null);
const deleteUnusedDialog = ref(false);
const confirmLoading = ref(false);
const snackbar = ref(false);
const message = ref('');
const { servers, serverId, loadingServers } = useDockerServers(load);
const unusedVolumes = computed(() => items.value.filter(item => !item.inUse));
let volumesRequestId = 0;
async function load() {
  const requestedServerId = serverId.value;
  const requestId = ++volumesRequestId;
  items.value = [];
  pending.value = null;
  error.value = '';
  if (!requestedServerId) {
    loading.value = false;
    return;
  }
  loading.value = true;
  try {
    const result = await containerizationApi.volumes(requestedServerId);
    if (requestId !== volumesRequestId || serverId.value !== requestedServerId) return;
    items.value = result;
    error.value = '';
  } catch (err) {
    if (requestId !== volumesRequestId || serverId.value !== requestedServerId) return;
    error.value = err instanceof Error ? err.message : t('containerization.loadFailed');
  } finally {
    if (requestId === volumesRequestId && serverId.value === requestedServerId) {
      loading.value = false;
    }
  }
}
async function remove() {
  if (!pending.value) return;
  confirmLoading.value = true;
  try {
    await containerizationApi.deleteVolume(serverId.value, pending.value.name);
    pending.value = null;
    await notifyOperation();
  } finally {
    confirmLoading.value = false;
  }
}
async function deleteUnusedVolumes() {
  confirmLoading.value = true;
  try {
    await containerizationApi.deleteUnusedVolumes(serverId.value);
    deleteUnusedDialog.value = false;
    await notifyOperation();
  } finally {
    confirmLoading.value = false;
  }
}
async function notifyOperation() {
  message.value = t('containerization.operationCompleted');
  snackbar.value = true;
  await load();
}
</script>
<template>
  <ResourcePage v-model:server-id="serverId" :servers="servers" :loading-servers="loadingServers" :loading="loading" :error="error">
    <div class="app-card-header"><strong>{{ t('containerization.volumes') }}</strong><div class="page-actions"><v-btn size="small" variant="outlined" prepend-icon="mdi-refresh" @click="load">{{ t('common.refresh') }}</v-btn><v-menu location="bottom end"><template #activator="{ props }"><v-btn v-bind="props" icon="mdi-dots-vertical" variant="text" size="small" :aria-label="t('common.more')" /></template><v-list density="compact"><v-list-item prepend-icon="mdi-delete-sweep" :disabled="!unusedVolumes.length" class="text-error" @click="deleteUnusedDialog = true"><v-list-item-title>{{ t('containerization.deleteUnusedVolumes') }}</v-list-item-title></v-list-item></v-list></v-menu></div></div>
    <v-table><thead><tr><th>{{ t('common.name') }}</th><th>{{ t('containerization.driver') }}</th><th>{{ t('containerization.mountpoint') }}</th><th>{{ t('common.status') }}</th><th>{{ t('containerization.created') }}</th><th>{{ t('common.actions') }}</th></tr></thead>
      <tbody><tr v-if="!items.length"><td colspan="6" class="text-center py-8">{{ t('containerization.empty') }}</td></tr><tr v-for="item in items" :key="item.name"><td><strong>{{ item.name }}</strong></td><td>{{ item.driver }}</td><td class="text-caption">{{ item.mountpoint }}</td><td><v-chip size="small" variant="tonal" :color="item.inUse ? 'info' : 'default'">{{ item.inUse ? t('containerization.inUse', { count: item.containerCount }) : t('containerization.unused') }}</v-chip></td><td>{{ item.createdAt ? formatDateTime(item.createdAt) : '-' }}</td><td><v-btn size="small" color="error" variant="outlined" :disabled="item.inUse" @click="pending = item">{{ t('common.delete') }}</v-btn></td></tr></tbody>
    </v-table>
    <v-dialog :model-value="!!pending" width="440" @update:model-value="!$event && (pending = null)"><v-card class="app-dialog-card"><v-card-title class="app-dialog-title">{{ t('containerization.deleteVolume') }}</v-card-title><v-divider/><v-card-text class="app-dialog-body">{{ t('containerization.deleteVolumeMessage', { name: pending?.name }) }}</v-card-text><v-divider/><v-card-actions class="app-dialog-actions"><v-btn variant="text" :disabled="confirmLoading" @click="pending = null">{{ t('common.cancel') }}</v-btn><v-btn color="error" variant="flat" :loading="confirmLoading" @click="remove">{{ t('common.delete') }}</v-btn></v-card-actions></v-card></v-dialog>
    <v-dialog v-model="deleteUnusedDialog" width="440"><v-card class="app-dialog-card"><v-card-title class="app-dialog-title">{{ t('containerization.deleteUnusedVolumes') }}</v-card-title><v-divider/><v-card-text class="app-dialog-body">{{ t('containerization.deleteUnusedVolumesMessage') }}</v-card-text><v-divider/><v-card-actions class="app-dialog-actions"><v-btn variant="text" :disabled="confirmLoading" @click="deleteUnusedDialog = false">{{ t('common.cancel') }}</v-btn><v-btn color="error" variant="flat" :loading="confirmLoading" @click="deleteUnusedVolumes">{{ t('common.delete') }}</v-btn></v-card-actions></v-card></v-dialog>
    <v-snackbar v-model="snackbar">{{ message }}</v-snackbar>
  </ResourcePage>
</template>
