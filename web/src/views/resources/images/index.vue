<script setup lang="ts">
import { computed, ref } from 'vue';
import { containerizationApi, type DockerImageDto, type DockerImageListDto } from '@/api/containerization';
import { useI18n } from '@/i18n';
import AppActionButton from '@/components/AppActionButton.vue';
import AppActionGroup from '@/components/AppActionGroup.vue';
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
let imagesRequestId = 0;

const items = computed(() => data.value?.items ?? []);
const selectableApps = computed(() => [...new Set(items.value.filter(item => item.updateAvailable && item.upgradeable).flatMap(item => item.applicationIds))]);
const unusedImages = computed(() => items.value.filter(item => !item.inUse));

async function load() {
  const requestedServerId = serverId.value;
  const requestId = ++imagesRequestId;
  data.value = null;
  selected.value = [];
  deleteTarget.value = null;
  error.value = '';
  if (!requestedServerId) {
    loading.value = false;
    return;
  }
  loading.value = true;
  try {
    const result = await containerizationApi.images(requestedServerId);
    if (requestId !== imagesRequestId || serverId.value !== requestedServerId) return;
    data.value = result;
    error.value = '';
  } catch (err) {
    if (requestId !== imagesRequestId || serverId.value !== requestedServerId) return;
    data.value = null;
    error.value = err instanceof Error ? err.message : t('containerization.loadFailed');
  } finally {
    if (requestId === imagesRequestId && serverId.value === requestedServerId) {
      loading.value = false;
    }
  }
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
    <section class="resource-table-panel">
      <div class="app-card-header image-header">
        <div class="image-title min-width-0">
          <strong>{{ t('containerization.images') }}</strong>
          <div class="text-caption text-medium-emphasis text-truncate">
            {{ t('containerization.lastChecked') }}: {{ data?.lastRefreshedAt ? formatDateTime(data.lastRefreshedAt) : t('common.never') }}
          </div>
        </div>
        <AppActionGroup context="toolbar" class="image-actions">
          <AppActionButton icon="mdi-download" :label="t('containerization.pullImage')" @click="pullDialog = true" />
          <AppActionButton icon="mdi-refresh" :label="t('common.refresh')" :loading="data?.refreshing" @click="refresh" />
          <AppActionButton kind="primary" icon="mdi-upload" :label="t('containerization.upgradeSelected')" :disabled="!selected.length" @click="upgradeSelected" />
          <v-menu location="bottom end">
            <template #activator="{ props }">
              <AppActionButton v-bind="props" kind="tool" icon="mdi-dots-vertical" :label="t('common.more')" />
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
        </AppActionGroup>
      </div>
      <div class="resource-table-body">
        <v-table>
          <thead>
            <tr>
              <th></th>
              <th>{{ t('containerization.image') }}</th>
              <th>{{ t('containerization.localDigest') }}</th>
              <th>{{ t('containerization.latestDigest') }}</th>
              <th>{{ t('common.status') }}</th>
              <th>{{ t('containerization.usage') }}</th>
              <th>{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!items.length"><td colspan="7" class="text-center py-8">{{ t('containerization.empty') }}</td></tr>
            <tr v-for="item in items" :key="item.id">
              <td><v-checkbox-btn v-model="selected" :value="item.id" :disabled="!item.updateAvailable || !item.upgradeable || !item.applicationIds.length" /></td>
              <td><strong>{{ item.reference || item.id.slice(0, 12) }}</strong><div class="text-caption text-medium-emphasis">{{ item.id.slice(0, 19) }}</div></td>
              <td>{{ shortDigest(item.localDigest) }}</td>
              <td>{{ shortDigest(item.latestDigest) }}</td>
              <td>
                <v-chip v-if="item.updateAvailable" color="warning" size="small" variant="tonal">{{ t('containerization.updateAvailable') }}</v-chip>
                <v-chip v-else-if="item.checkable" color="success" size="small" variant="tonal">{{ t('containerization.upToDate') }}</v-chip>
                <v-chip v-else size="small" variant="tonal">{{ t('containerization.notCheckable') }}</v-chip>
                <div v-if="item.lastError" class="text-caption text-error">{{ item.lastError }}</div>
              </td>
              <td>{{ item.applicationIds.length ? t('containerization.applicationCount', { count: item.applicationIds.length }) : item.inUse ? t('containerization.containerOnly') : t('containerization.unused') }}</td>
              <td class="text-right">
                <AppActionGroup context="table">
                  <AppActionButton kind="danger" icon="mdi-delete" :label="t('common.delete')" :disabled="item.inUse" @click="deleteTarget = item" />
                </AppActionGroup>
              </td>
            </tr>
          </tbody>
        </v-table>
      </div>
    </section>
    <v-dialog v-model="pullDialog" width="520">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('containerization.pullImage') }}</span>
          <AppActionButton kind="tool" icon="mdi-close" :label="t('common.cancel')" @click="pullDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          <v-text-field v-model="pullReference" :label="t('containerization.imageReference')" density="comfortable" variant="outlined" hide-details="auto" />
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <AppActionGroup context="dialog">
            <AppActionButton kind="plain" :label="t('common.cancel')" :disabled="operationLoading" @click="pullDialog = false" />
            <AppActionButton kind="primary" :label="t('containerization.pull')" :loading="operationLoading" :disabled="!pullReference.trim()" @click="pull" />
          </AppActionGroup>
        </v-card-actions>
      </v-card>
    </v-dialog>
    <v-dialog :model-value="!!deleteTarget" width="440" @update:model-value="!$event && (deleteTarget = null)">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('containerization.deleteImage') }}</span>
          <AppActionButton kind="tool" icon="mdi-close" :label="t('common.cancel')" @click="deleteTarget = null" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body text-body-1">{{ t('containerization.deleteImageMessage', { name: deleteTarget?.reference || deleteTarget?.id }) }}</v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <AppActionGroup context="dialog">
            <AppActionButton kind="plain" :label="t('common.cancel')" :disabled="operationLoading" @click="deleteTarget = null" />
            <AppActionButton kind="danger-primary" :label="t('common.delete')" :loading="operationLoading" @click="remove" />
          </AppActionGroup>
        </v-card-actions>
      </v-card>
    </v-dialog>
    <v-dialog v-model="upgradeAllDialog" width="440">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('containerization.upgradeAll') }}</span>
          <AppActionButton kind="tool" icon="mdi-close" :label="t('common.cancel')" @click="upgradeAllDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body text-body-1">{{ t('containerization.upgradeAllMessage') }}</v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <AppActionGroup context="dialog">
            <AppActionButton kind="plain" :label="t('common.cancel')" :disabled="confirmLoading" @click="upgradeAllDialog = false" />
            <AppActionButton kind="primary" :label="t('common.update')" :loading="confirmLoading" @click="upgradeAll" />
          </AppActionGroup>
        </v-card-actions>
      </v-card>
    </v-dialog>
    <v-dialog v-model="deleteUnusedDialog" width="440">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('containerization.deleteUnusedImages') }}</span>
          <AppActionButton kind="tool" icon="mdi-close" :label="t('common.cancel')" @click="deleteUnusedDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body text-body-1">{{ t('containerization.deleteUnusedImagesMessage') }}</v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <AppActionGroup context="dialog">
            <AppActionButton kind="plain" :label="t('common.cancel')" :disabled="operationLoading" @click="deleteUnusedDialog = false" />
            <AppActionButton kind="danger-primary" :label="t('common.delete')" :loading="operationLoading" @click="deleteUnusedImages" />
          </AppActionGroup>
        </v-card-actions>
      </v-card>
    </v-dialog>
    <v-snackbar v-model="snackbar">{{ message }}</v-snackbar>
  </ResourcePage>
</template>

<style scoped>
.resource-table-panel {
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.image-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.image-title {
  display: grid;
  gap: 2px;
}

.resource-table-body {
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
}

.min-width-0 {
  min-width: 0;
}

@media (max-width: 760px) {
  .image-header { align-items: stretch; flex-direction: column; }
  .resource-table-panel { min-height: auto; }
  .resource-table-body { overflow-x: auto; overflow-y: visible; }
}
</style>
