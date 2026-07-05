<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useI18n } from '@/i18n';
import AppActionButton from '@/components/AppActionButton.vue';
import AppActionGroup from '@/components/AppActionGroup.vue';
import AppMasterDetailWorkspace from '@/components/AppMasterDetailWorkspace.vue';
import ServerSelector from '@/components/ServerSelector.vue';
import AppPagination from '@/components/AppPagination.vue';
import PageLoadingState from '@/components/PageLoadingState.vue';
import { packagesApi } from '@/api/packages';
import { serversApi } from '@/api/servers';
import { usePagination } from '@/composables/usePagination';
import type { PackageUpdatesDto, ServerDto } from '@/types/api';

const servers = ref<ServerDto[]>([]);
const serverId = ref('');
const updates = ref<PackageUpdatesDto | null>(null);
const selectedPackageNames = ref<string[]>([]);
const loadingServers = ref(false);
const loadingUpdates = ref(false);
const packageRefreshRunning = ref(false);
const error = ref('');
const confirmUpgradeAllDialog = ref(false);
const lastTaskId = ref('');
let refreshPollTimer: number | undefined;
let updatesRequestId = 0;

// Snackbar notification state
const snackbar = ref(false);
const snackbarText = ref('');
const snackbarColor = ref('success');
const { t, formatDateTime } = useI18n();

function showMessage(text: string, color = 'success') {
  snackbarText.value = text;
  snackbarColor.value = color;
  snackbar.value = true;
}

function taskRoute(taskId = lastTaskId.value) {
  return taskId ? { path: '/tasks', query: { task: taskId } } : '/tasks';
}

const currentServer = computed(() => servers.value.find((server) => server.id === serverId.value));
function hasPrivilege(server?: ServerDto | null) {
  return server?.privilege?.privileged === true || server?.sudo?.passwordless === true;
}
const operationBlocked = computed(() => {
  const server = currentServer.value;
  return !server || server.os?.supported === false || !hasPrivilege(server);
});

const selectedPackages = computed(() => (updates.value?.updates ?? []).filter(item => selectedPackageNames.value.includes(item.name)));
const updateRows = computed(() => updates.value?.updates ?? []);
const {
  page,
  pageSize,
  total,
  pageItems: pagedUpdates,
} = usePagination(updateRows);
const refreshInProgress = computed(() => packageRefreshRunning.value || updates.value?.refreshing === true);
const selectAll = computed({
  get() {
    const items = updates.value?.updates ?? [];
    return items.length > 0 && selectedPackageNames.value.length === items.length;
  },
  set(val) {
    const items = updates.value?.updates ?? [];
    if (val) {
      selectedPackageNames.value = items.map(item => item.name);
    } else {
      selectedPackageNames.value = [];
    }
  }
});

async function loadServers() {
  loadingServers.value = true;
  try {
    servers.value = await serversApi.listServers();
    if (!serverId.value && servers.value.length) serverId.value = servers.value[0].id;
  } finally {
    loadingServers.value = false;
  }
}

function stopRefreshPolling() {
  if (refreshPollTimer) {
    window.clearInterval(refreshPollTimer);
    refreshPollTimer = undefined;
  }
}

function startRefreshPolling() {
  if (refreshPollTimer) return;
  stopRefreshPolling();
  refreshPollTimer = window.setInterval(async () => {
    await loadUpdates(false);
    if (!updates.value?.refreshing) {
      packageRefreshRunning.value = false;
      stopRefreshPolling();
    }
  }, 1500);
}

async function loadUpdates(showLoading = true) {
  const requestedServerId = serverId.value;
  const requestId = ++updatesRequestId;
  if (!requestedServerId) {
    updates.value = null;
    packageRefreshRunning.value = false;
    loadingUpdates.value = false;
    error.value = '';
    stopRefreshPolling();
    return;
  }
  if (showLoading) {
    updates.value = null;
    selectedPackageNames.value = [];
    packageRefreshRunning.value = false;
    stopRefreshPolling();
    loadingUpdates.value = true;
  }
  try {
    const result = await packagesApi.listUpdates(requestedServerId);
    if (requestId !== updatesRequestId || serverId.value !== requestedServerId) return;
    updates.value = result;
    packageRefreshRunning.value = result.refreshing;
    selectedPackageNames.value = [];
    error.value = '';
    if (result.refreshing) {
      startRefreshPolling();
    }
  } catch (err) {
    if (requestId !== updatesRequestId || serverId.value !== requestedServerId) return;
    updates.value = null;
    error.value = err instanceof Error ? err.message : t('packagesPage.loadFailed');
  } finally {
    if (showLoading && requestId === updatesRequestId && serverId.value === requestedServerId) {
      loadingUpdates.value = false;
    }
  }
}

async function refreshPackages() {
  if (!serverId.value) return;
  packageRefreshRunning.value = true;
  try {
    const result = await packagesApi.refresh(serverId.value);
    lastTaskId.value = result.taskId || '';
    showMessage(t('packagesPage.refreshStarted'));
    startRefreshPolling();
  } catch (err) {
    packageRefreshRunning.value = false;
    lastTaskId.value = '';
    showMessage(err instanceof Error ? err.message : t('packagesPage.refreshFailed'), 'error');
  }
}

async function upgradeSelected() {
  if (!serverId.value || selectedPackages.value.length === 0) return;
  const names = selectedPackages.value.map((item) => item.name);
  try {
    const result = await packagesApi.upgradeSelected(serverId.value, names);
    lastTaskId.value = result.taskId || '';
    showMessage(t('packagesPage.selectedUpgradeStarted'));
  } catch (err) {
    lastTaskId.value = '';
    showMessage(err instanceof Error ? err.message : t('packagesPage.upgradeFailed'), 'error');
  }
}

async function upgradeAll() {
  if (!serverId.value) return;
  try {
    const result = await packagesApi.upgradeAll(serverId.value);
    lastTaskId.value = result.taskId || '';
    confirmUpgradeAllDialog.value = false;
    showMessage(t('packagesPage.fullUpgradeStarted'));
  } catch (err) {
    lastTaskId.value = '';
    showMessage(err instanceof Error ? err.message : t('packagesPage.fullUpgradeFailed'), 'error');
  }
}

watch(serverId, () => {
  void loadUpdates();
});
onMounted(async () => {
  await loadServers();
});
onBeforeUnmount(stopRefreshPolling);
</script>

<template>
  <div class="page-shell">
    <v-alert v-if="error" type="error" variant="tonal">{{ error }}</v-alert>
    <v-alert
      v-if="operationBlocked && currentServer"
      type="warning"
      variant="tonal"
    >
      {{ t('packagesPage.blockedHint') }}
    </v-alert>

    <AppMasterDetailWorkspace>
      <template #aside>
        <ServerSelector v-model="serverId" :servers="servers" :loading="loadingServers" />
      </template>

      <v-card :loading="loadingUpdates" variant="outlined" class="package-panel">
        <div class="app-card-header package-card-header">
          <div class="package-card-title">
            <div class="text-subtitle-1 font-weight-bold text-truncate">{{ currentServer?.name || t('common.selectServer') }}</div>
            <div class="text-caption text-medium-emphasis text-truncate">
              {{ t('packagesPage.lastRefreshed') }}: {{ updates?.lastRefreshedAt ? formatDateTime(updates.lastRefreshedAt) : t('common.never') }}
            </div>
          </div>
          <AppActionGroup context="toolbar">
            <v-chip v-if="refreshInProgress" size="small" color="info" variant="tonal" prepend-icon="mdi-sync">
              {{ t('packagesPage.refreshing') }}
            </v-chip>
            <AppActionButton
              icon="mdi-refresh"
              :label="t('common.refresh')"
              :disabled="operationBlocked || refreshInProgress"
              :loading="refreshInProgress"
              @click="refreshPackages"
            />
            <AppActionButton
              kind="primary"
              icon="mdi-upload"
              :label="t('packagesPage.upgradeSelected')"
              :disabled="operationBlocked || selectedPackageNames.length === 0"
              @click="upgradeSelected"
            />
            <v-menu location="bottom end">
              <template #activator="{ props }">
                <AppActionButton v-bind="props" kind="tool" icon="mdi-dots-vertical" :label="t('common.more')" />
              </template>
              <v-list density="compact">
                <v-list-item
                  prepend-icon="mdi-arrow-up-bold-box"
                  :disabled="operationBlocked || !updates?.updates.length"
                  class="text-error"
                  @click="confirmUpgradeAllDialog = true"
                >
                  <v-list-item-title>{{ t('packagesPage.upgradeAll') }}</v-list-item-title>
                </v-list-item>
              </v-list>
            </v-menu>
          </AppActionGroup>
        </div>

        <v-card-text class="package-panel-body pa-4">
          <PageLoadingState v-if="loadingUpdates && !updates" min-height="280px" />
          <div v-else class="package-table-wrap">
            <v-table class="text-left" style="background: transparent;">
              <thead>
                <tr>
                  <th style="width: 48px;">
                    <v-checkbox-btn v-model="selectAll" color="primary" :disabled="operationBlocked || !updates?.updates.length" />
                  </th>
                  <th class="font-weight-bold">{{ t('packagesPage.package') }}</th>
                  <th class="font-weight-bold">{{ t('packagesPage.installedVersion') }}</th>
                  <th class="font-weight-bold">{{ t('packagesPage.candidateVersion') }}</th>
                  <th class="font-weight-bold">{{ t('packagesPage.source') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-if="!updates || updates.updates.length === 0">
                  <td colspan="5" class="text-center py-6 text-medium-emphasis">{{ t('packagesPage.noPackages') }}</td>
                </tr>
                <tr v-for="row in pagedUpdates" :key="row.name">
                  <td>
                    <v-checkbox-btn v-model="selectedPackageNames" :value="row.name" color="primary" :disabled="operationBlocked" />
                  </td>
                  <td class="font-weight-bold">{{ row.name }}</td>
                  <td>{{ row.installedVersion }}</td>
                  <td>{{ row.candidateVersion }}</td>
                  <td>{{ row.source }}</td>
                </tr>
              </tbody>
            </v-table>
          </div>
          <AppPagination v-model:page="page" v-model:page-size="pageSize" :total="total" />
        </v-card-text>
      </v-card>
    </AppMasterDetailWorkspace>

    <v-dialog v-model="confirmUpgradeAllDialog" width="460">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('packagesPage.confirmUpgradeAllTitle') }}</span>
          <AppActionButton kind="tool" icon="mdi-close" :label="t('common.close')" @click="confirmUpgradeAllDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          {{ t('packagesPage.confirmUpgradeAllMessage', { name: currentServer?.name || t('common.unknown') }) }}
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <AppActionGroup context="dialog">
            <AppActionButton kind="plain" :label="t('common.cancel')" @click="confirmUpgradeAllDialog = false" />
            <AppActionButton kind="danger-primary" :label="t('packagesPage.upgradeAll')" @click="upgradeAll" />
          </AppActionGroup>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- Global Snackbar -->
    <v-snackbar v-model="snackbar" :color="snackbarColor" timeout="3000">
      {{ snackbarText }}
      <template v-slot:actions>
        <AppActionGroup context="snackbar">
          <AppActionButton v-if="lastTaskId" kind="snackbar" :label="t('taskCenter.task')" :to="taskRoute()" />
          <AppActionButton kind="snackbar" :label="t('common.close')" @click="snackbar = false" />
        </AppActionGroup>
      </template>
    </v-snackbar>
  </div>
</template>

<style scoped>
.package-card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.package-card-title {
  min-width: 0;
}

.package-panel {
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}

.package-panel-body {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  min-height: 0;
}

.package-table-wrap {
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
}

@media (max-width: 760px) {
  .package-panel-body {
    flex: none;
    min-height: auto;
  }

  .package-panel {
    overflow: visible;
  }

  .package-table-wrap {
    overflow-x: auto;
    overflow-y: visible;
  }

  .package-card-header {
    align-items: stretch;
    flex-direction: column;
  }
}
</style>
