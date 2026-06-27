<script setup lang="ts">
import { ref } from 'vue';
import { containerizationApi, type DockerContainerDto } from '@/api/containerization';
import { useI18n } from '@/i18n';
import RuntimeLogsDialog from '@/components/RuntimeLogsDialog.vue';
import ResourcePage from '../_shared/ResourcePage.vue';
import { useDockerServers } from '../_shared/useDockerServers';

const { t, formatDateTime } = useI18n();
const items = ref<DockerContainerDto[]>([]);
const loading = ref(false);
const error = ref('');
const pending = ref<{ item: DockerContainerDto; action: 'start' | 'stop' | 'restart' | 'delete' } | null>(null);
const actionLoading = ref(false);
const snackbar = ref(false);
const message = ref('');
const logTarget = ref<DockerContainerDto | null>(null);
const logsDialog = ref(false);
const { servers, serverId, loadingServers } = useDockerServers(load);
let containersRequestId = 0;

async function load() {
  const requestedServerId = serverId.value;
  const requestId = ++containersRequestId;
  items.value = [];
  pending.value = null;
  logTarget.value = null;
  logsDialog.value = false;
  error.value = '';
  if (!requestedServerId) {
    loading.value = false;
    return;
  }
  loading.value = true;
  try {
    const result = await containerizationApi.containers(requestedServerId);
    if (requestId !== containersRequestId || serverId.value !== requestedServerId) return;
    items.value = result;
    error.value = '';
  } catch (err) {
    if (requestId !== containersRequestId || serverId.value !== requestedServerId) return;
    error.value = err instanceof Error ? err.message : t('containerization.loadFailed');
  } finally {
    if (requestId === containersRequestId && serverId.value === requestedServerId) {
      loading.value = false;
    }
  }
}

function ask(item: DockerContainerDto, action: 'start' | 'stop' | 'restart' | 'delete') {
  pending.value = { item, action };
}

async function run() {
  if (!pending.value) return;
  const { item, action } = pending.value;
  actionLoading.value = true;
  try {
    action === 'delete'
      ? await containerizationApi.deleteContainer(serverId.value, item.id)
      : await containerizationApi.containerAction(serverId.value, item.id, action);
    pending.value = null;
    message.value = t('containerization.operationCompleted');
    snackbar.value = true;
    await load();
  } finally {
    actionLoading.value = false;
  }
}

function nameOf(item: DockerContainerDto) {
  return item.names?.[0]?.replace(/^\//, '') || item.id.slice(0, 12);
}

function openLogs(item: DockerContainerDto) {
  logTarget.value = item;
  logsDialog.value = true;
}

async function loadSelectedLogs(tail: number) {
  if (!logTarget.value) return '';
  const result = await containerizationApi.containerLogs(serverId.value, logTarget.value.id, tail);
  return result.logs;
}
</script>

<template>
  <ResourcePage v-model:server-id="serverId" :servers="servers" :loading-servers="loadingServers" :loading="loading" :error="error">
    <div class="app-card-header">
      <strong>{{ t('containerization.containers') }}</strong>
      <v-btn size="small" variant="outlined" prepend-icon="mdi-refresh" @click="load">{{ t('common.refresh') }}</v-btn>
    </div>
    <v-table>
      <thead><tr><th>{{ t('common.name') }}</th><th>{{ t('containerization.image') }}</th><th>{{ t('common.status') }}</th><th>{{ t('containerization.managed') }}</th><th>{{ t('containerization.created') }}</th><th>{{ t('common.actions') }}</th></tr></thead>
      <tbody>
        <tr v-if="!items.length"><td colspan="6" class="text-center py-8">{{ t('containerization.empty') }}</td></tr>
        <tr v-for="item in items" :key="item.id">
          <td><strong>{{ nameOf(item) }}</strong><div class="text-caption text-medium-emphasis">{{ item.id.slice(0, 12) }}</div></td>
          <td>{{ item.image }}</td>
          <td><v-chip size="small" variant="tonal" :color="item.state === 'running' ? 'success' : 'default'">{{ item.status || item.state }}</v-chip></td>
          <td><v-chip v-if="item.managed" size="small" color="primary" variant="tonal">{{ t('containerization.applicationManaged') }}</v-chip><span v-else>{{ t('common.no') }}</span></td>
          <td>{{ formatDateTime(new Date(item.created * 1000).toISOString()) }}</td>
          <td class="text-right">
            <div class="app-table-actions">
              <v-btn size="small" variant="outlined" prepend-icon="mdi-text-box-search-outline" @click="openLogs(item)">{{ t('containerization.viewLogs') }}</v-btn>
              <v-btn v-if="item.state !== 'running'" size="small" variant="outlined" @click="ask(item, 'start')">{{ t('containerization.start') }}</v-btn>
              <v-btn v-if="item.state === 'running'" size="small" variant="outlined" @click="ask(item, 'stop')">{{ t('containerization.stop') }}</v-btn>
              <v-btn v-if="item.state === 'running'" size="small" variant="outlined" @click="ask(item, 'restart')">{{ t('containerization.restart') }}</v-btn>
              <v-btn size="small" color="error" variant="outlined" @click="ask(item, 'delete')">{{ t('common.delete') }}</v-btn>
            </div>
          </td>
        </tr>
      </tbody>
    </v-table>
    <v-dialog :model-value="!!pending" width="480" @update:model-value="!$event && (pending = null)">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">{{ t('containerization.confirmAction') }}</v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          <v-alert v-if="pending?.item.managed" type="warning" variant="tonal" class="mb-4">{{ t('containerization.managedWarning') }}</v-alert>
          {{ t('containerization.confirmMessage', { action: pending ? t(`containerization.${pending.action}`) : '', name: pending ? nameOf(pending.item) : '' }) }}
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions"><v-btn variant="text" :disabled="actionLoading" @click="pending = null">{{ t('common.cancel') }}</v-btn><v-btn color="primary" variant="flat" :loading="actionLoading" @click="run">{{ t('common.confirm') }}</v-btn></v-card-actions>
      </v-card>
    </v-dialog>
    <RuntimeLogsDialog
      v-model:open="logsDialog"
      :title="t('containerization.containerLogs')"
      :subtitle="logTarget ? `${nameOf(logTarget)} / ${logTarget.id.slice(0, 12)}` : ''"
      :target-key="logTarget ? `${serverId}:${logTarget.id}` : ''"
      :loader="loadSelectedLogs"
    />
    <v-snackbar v-model="snackbar">{{ message }}</v-snackbar>
  </ResourcePage>
</template>
