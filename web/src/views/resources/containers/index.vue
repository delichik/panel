<script setup lang="ts">
import { ref } from 'vue';
import { containerizationApi, type DockerContainerDto } from '@/api/containerization';
import { useI18n } from '@/i18n';
import AppActionButton from '@/components/AppActionButton.vue';
import AppActionGroup from '@/components/AppActionGroup.vue';
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
    <section class="resource-table-panel">
      <div class="app-card-header resource-header">
        <strong>{{ t('containerization.containers') }}</strong>
        <AppActionGroup context="toolbar" class="resource-actions">
          <AppActionButton icon="mdi-refresh" :label="t('common.refresh')" @click="load" />
        </AppActionGroup>
      </div>
      <div class="resource-table-body">
        <v-table>
          <thead>
            <tr>
              <th>{{ t('common.name') }}</th>
              <th>{{ t('containerization.image') }}</th>
              <th>{{ t('common.status') }}</th>
              <th>{{ t('containerization.managed') }}</th>
              <th>{{ t('containerization.created') }}</th>
              <th>{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!items.length"><td colspan="6" class="text-center py-8">{{ t('containerization.empty') }}</td></tr>
            <tr v-for="item in items" :key="item.id">
              <td><strong>{{ nameOf(item) }}</strong><div class="text-caption text-medium-emphasis">{{ item.id.slice(0, 12) }}</div></td>
              <td>{{ item.image }}</td>
              <td><v-chip size="small" variant="tonal" :color="item.state === 'running' ? 'success' : 'default'">{{ item.status || item.state }}</v-chip></td>
              <td><v-chip v-if="item.managed" size="small" color="primary" variant="tonal">{{ t('containerization.applicationManaged') }}</v-chip><span v-else>{{ t('common.no') }}</span></td>
              <td>{{ formatDateTime(new Date(item.created * 1000).toISOString()) }}</td>
              <td class="text-right">
                <AppActionGroup context="table">
                  <AppActionButton icon="mdi-text-box-search-outline" :label="t('containerization.viewLogs')" @click="openLogs(item)" />
                  <v-menu location="bottom end">
                    <template #activator="{ props }">
                      <AppActionButton v-bind="props" kind="tool" icon="mdi-dots-vertical" :label="t('common.more')" />
                    </template>
                    <v-list density="compact">
                      <v-list-item v-if="item.state !== 'running'" prepend-icon="mdi-play" @click="ask(item, 'start')">
                        <v-list-item-title>{{ t('containerization.start') }}</v-list-item-title>
                      </v-list-item>
                      <v-list-item v-if="item.state === 'running'" prepend-icon="mdi-stop" @click="ask(item, 'stop')">
                        <v-list-item-title>{{ t('containerization.stop') }}</v-list-item-title>
                      </v-list-item>
                      <v-list-item v-if="item.state === 'running'" prepend-icon="mdi-restart" @click="ask(item, 'restart')">
                        <v-list-item-title>{{ t('containerization.restart') }}</v-list-item-title>
                      </v-list-item>
                      <v-list-item prepend-icon="mdi-delete" class="text-error" @click="ask(item, 'delete')">
                        <v-list-item-title>{{ t('common.delete') }}</v-list-item-title>
                      </v-list-item>
                    </v-list>
                  </v-menu>
                </AppActionGroup>
              </td>
            </tr>
          </tbody>
        </v-table>
      </div>
    </section>
    <v-dialog :model-value="!!pending" width="480" @update:model-value="!$event && (pending = null)">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('containerization.confirmAction') }}</span>
          <AppActionButton kind="tool" icon="mdi-close" :label="t('common.cancel')" @click="pending = null" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          <v-alert v-if="pending?.item.managed" type="warning" variant="tonal" class="mb-4">{{ t('containerization.managedWarning') }}</v-alert>
          {{ t('containerization.confirmMessage', { action: pending ? t(`containerization.${pending.action}`) : '', name: pending ? nameOf(pending.item) : '' }) }}
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <AppActionGroup context="dialog">
            <AppActionButton kind="plain" :label="t('common.cancel')" :disabled="actionLoading" @click="pending = null" />
            <AppActionButton kind="primary" :label="t('common.confirm')" :loading="actionLoading" @click="run" />
          </AppActionGroup>
        </v-card-actions>
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
