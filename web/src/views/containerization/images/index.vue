<script setup lang="ts">
import { computed, ref } from 'vue';
import { containerizationApi, type DockerImageDto, type DockerImageListDto } from '@/api/containerization';
import { useI18n } from '@/i18n';
import ResourcePage from '../_shared/ResourcePage.vue';
import { useDockerServers } from '../_shared/useDockerServers';

const { t, formatDateTime } = useI18n();
const data = ref<DockerImageListDto | null>(null);
const loading = ref(false);
const error = ref('');
const selected = ref<string[]>([]);
const pullDialog = ref(false);
const pullReference = ref('');
const deleteTarget = ref<DockerImageDto | null>(null);
const upgradeAllDialog = ref(false);
const deleteUnusedDialog = ref(false);
const confirmLoading = ref(false);
const operationLoading = ref(false);
const snackbar = ref(false);
const message = ref('');
const { servers, serverId, loadingServers } = useDockerServers(load);

const items = computed(() => data.value?.items ?? []);
const selectableApps = computed(() => [...new Set(items.value.filter(item => item.updateAvailable && item.upgradeable).flatMap(item => item.applicationIds))]);
const unusedImages = computed(() => items.value.filter(item => !item.inUse));

async function load() {
  if (!serverId.value) return;
  loading.value = true;
  selected.value = [];
  try { data.value = await containerizationApi.images(serverId.value); error.value = ''; }
  catch (err) { data.value = null; error.value = err instanceof Error ? err.message : t('containerization.loadFailed'); }
  finally { loading.value = false; }
}

async function refresh() {
  const result = await containerizationApi.refreshImages(serverId.value);
  notify(result.taskId);
}

async function pull() {
  operationLoading.value = true;
  try {
    await containerizationApi.pullImage(serverId.value, pullReference.value);
    pullDialog.value = false;
    pullReference.value = '';
    await notifyOperation();
  } finally {
    operationLoading.value = false;
  }
}

async function remove() {
  if (!deleteTarget.value) return;
  operationLoading.value = true;
  try {
    await containerizationApi.deleteImage(serverId.value, deleteTarget.value.id);
    deleteTarget.value = null;
    await notifyOperation();
  } finally {
    operationLoading.value = false;
  }
}

async function upgradeSelected() {
  const applicationIds = [...new Set(items.value.filter(item => selected.value.includes(item.id)).flatMap(item => item.applicationIds))];
  const result = await containerizationApi.upgradeSelected(applicationIds);
  notify(result.taskId);
}

async function upgradeAll() {
  confirmLoading.value = true;
  try {
    const result = await containerizationApi.upgradeAll();
    upgradeAllDialog.value = false;
    notify(result.taskId);
  } finally {
    confirmLoading.value = false;
  }
}

async function deleteUnusedImages() {
  operationLoading.value = true;
  try {
    await containerizationApi.deleteUnusedImages(serverId.value);
    deleteUnusedDialog.value = false;
    await notifyOperation();
  } finally {
    operationLoading.value = false;
  }
}

function notify(taskId: string) {
  message.value = t('containerization.taskCreated', { id: taskId });
  snackbar.value = true;
  window.setTimeout(load, 1200);
}

async function notifyOperation() {
  message.value = t('containerization.operationCompleted');
  snackbar.value = true;
  await load();
}

function shortDigest(value?: string) {
  return value ? value.replace(/^sha256:/, '').slice(0, 12) : '-';
}
</script>

<template>
  <ResourcePage v-model:server-id="serverId" :servers="servers" :loading-servers="loadingServers" :loading="loading" :error="error">
    <div class="app-card-header image-header">
      <div><strong>{{ t('containerization.images') }}</strong><div class="text-caption text-medium-emphasis">{{ t('containerization.lastChecked') }}: {{ data?.lastRefreshedAt ? formatDateTime(data.lastRefreshedAt) : t('common.never') }}</div></div>
      <div class="page-actions">
        <v-btn size="small" variant="outlined" prepend-icon="mdi-download" @click="pullDialog = true">{{ t('containerization.pullImage') }}</v-btn>
        <v-btn size="small" variant="outlined" prepend-icon="mdi-refresh" :loading="data?.refreshing" @click="refresh">{{ t('common.refresh') }}</v-btn>
        <v-btn size="small" color="primary" variant="flat" :disabled="!selected.length" @click="upgradeSelected">{{ t('containerization.upgradeSelected') }}</v-btn>
        <v-menu location="bottom end">
          <template #activator="{ props }">
            <v-btn v-bind="props" icon="mdi-dots-vertical" variant="text" size="small" :aria-label="t('common.more')" />
          </template>
          <v-list density="compact">
            <v-list-item prepend-icon="mdi-update" :disabled="!selectableApps.length" @click="upgradeAllDialog = true">
              <v-list-item-title>{{ t('containerization.upgradeAll') }}</v-list-item-title>
            </v-list-item>
            <v-list-item prepend-icon="mdi-delete-sweep" :disabled="!unusedImages.length" class="text-error" @click="deleteUnusedDialog = true">
              <v-list-item-title>{{ t('containerization.deleteUnusedImages') }}</v-list-item-title>
            </v-list-item>
          </v-list>
        </v-menu>
      </div>
    </div>
    <v-table>
      <thead><tr><th></th><th>{{ t('containerization.image') }}</th><th>{{ t('containerization.localDigest') }}</th><th>{{ t('containerization.latestDigest') }}</th><th>{{ t('common.status') }}</th><th>{{ t('containerization.usage') }}</th><th>{{ t('common.actions') }}</th></tr></thead>
      <tbody>
        <tr v-if="!items.length"><td colspan="7" class="text-center py-8">{{ t('containerization.empty') }}</td></tr>
        <tr v-for="item in items" :key="item.id">
          <td><v-checkbox-btn v-model="selected" :value="item.id" :disabled="!item.updateAvailable || !item.upgradeable || !item.applicationIds.length" /></td>
          <td><strong>{{ item.reference || item.id.slice(0, 12) }}</strong><div class="text-caption text-medium-emphasis">{{ item.id.slice(0, 19) }}</div></td>
          <td>{{ shortDigest(item.localDigest) }}</td><td>{{ shortDigest(item.latestDigest) }}</td>
          <td>
            <v-chip v-if="item.updateAvailable" color="warning" size="small" variant="tonal">{{ t('containerization.updateAvailable') }}</v-chip>
            <v-chip v-else-if="item.checkable" color="success" size="small" variant="tonal">{{ t('containerization.upToDate') }}</v-chip>
            <v-chip v-else size="small" variant="tonal">{{ t('containerization.notCheckable') }}</v-chip>
            <div v-if="item.lastError" class="text-caption text-error">{{ item.lastError }}</div>
          </td>
          <td>{{ item.applicationIds.length ? t('containerization.applicationCount', { count: item.applicationIds.length }) : item.inUse ? t('containerization.containerOnly') : t('containerization.unused') }}</td>
          <td><v-btn size="small" color="error" variant="outlined" :disabled="item.inUse" @click="deleteTarget = item">{{ t('common.delete') }}</v-btn></td>
        </tr>
      </tbody>
    </v-table>
    <v-dialog v-model="pullDialog" width="520"><v-card class="app-dialog-card"><v-card-title class="app-dialog-title">{{ t('containerization.pullImage') }}</v-card-title><v-divider/><v-card-text class="app-dialog-body"><v-text-field v-model="pullReference" :label="t('containerization.imageReference')" variant="outlined" /></v-card-text><v-divider/><v-card-actions class="app-dialog-actions"><v-btn variant="text" :disabled="operationLoading" @click="pullDialog = false">{{ t('common.cancel') }}</v-btn><v-btn color="primary" variant="flat" :loading="operationLoading" :disabled="!pullReference.trim()" @click="pull">{{ t('containerization.pull') }}</v-btn></v-card-actions></v-card></v-dialog>
    <v-dialog :model-value="!!deleteTarget" width="440" @update:model-value="!$event && (deleteTarget = null)"><v-card class="app-dialog-card"><v-card-title class="app-dialog-title">{{ t('containerization.deleteImage') }}</v-card-title><v-divider/><v-card-text class="app-dialog-body">{{ t('containerization.deleteImageMessage', { name: deleteTarget?.reference || deleteTarget?.id }) }}</v-card-text><v-divider/><v-card-actions class="app-dialog-actions"><v-btn variant="text" :disabled="operationLoading" @click="deleteTarget = null">{{ t('common.cancel') }}</v-btn><v-btn color="error" variant="flat" :loading="operationLoading" @click="remove">{{ t('common.delete') }}</v-btn></v-card-actions></v-card></v-dialog>
    <v-dialog v-model="upgradeAllDialog" width="440"><v-card class="app-dialog-card"><v-card-title class="app-dialog-title">{{ t('containerization.upgradeAll') }}</v-card-title><v-divider/><v-card-text class="app-dialog-body">{{ t('containerization.upgradeAllMessage') }}</v-card-text><v-divider/><v-card-actions class="app-dialog-actions"><v-btn variant="text" :disabled="confirmLoading" @click="upgradeAllDialog = false">{{ t('common.cancel') }}</v-btn><v-btn color="primary" variant="flat" :loading="confirmLoading" @click="upgradeAll">{{ t('common.update') }}</v-btn></v-card-actions></v-card></v-dialog>
    <v-dialog v-model="deleteUnusedDialog" width="440"><v-card class="app-dialog-card"><v-card-title class="app-dialog-title">{{ t('containerization.deleteUnusedImages') }}</v-card-title><v-divider/><v-card-text class="app-dialog-body">{{ t('containerization.deleteUnusedImagesMessage') }}</v-card-text><v-divider/><v-card-actions class="app-dialog-actions"><v-btn variant="text" :disabled="operationLoading" @click="deleteUnusedDialog = false">{{ t('common.cancel') }}</v-btn><v-btn color="error" variant="flat" :loading="operationLoading" @click="deleteUnusedImages">{{ t('common.delete') }}</v-btn></v-card-actions></v-card></v-dialog>
    <v-snackbar v-model="snackbar">{{ message }}</v-snackbar>
  </ResourcePage>
</template>

<style scoped>
.image-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
@media (max-width: 760px) {
  .image-header { align-items: stretch; flex-direction: column; }
}
</style>
