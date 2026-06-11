<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useI18n } from '@/i18n';
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
const operationBlocked = computed(() => {
  const server = currentServer.value;
  return !server || server.os?.supported === false || server.sudo?.passwordless === false;
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
  if (!serverId.value) {
    updates.value = null;
    packageRefreshRunning.value = false;
    stopRefreshPolling();
    return;
  }
  if (showLoading) loadingUpdates.value = true;
  try {
    updates.value = await packagesApi.listUpdates(serverId.value);
    packageRefreshRunning.value = updates.value.refreshing;
    selectedPackageNames.value = [];
    error.value = '';
    if (updates.value.refreshing) {
      startRefreshPolling();
    }
  } catch (err) {
    updates.value = null;
    error.value = err instanceof Error ? err.message : t('packagesPage.loadFailed');
  } finally {
    if (showLoading) loadingUpdates.value = false;
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

watch(serverId, () => loadUpdates());
onMounted(async () => {
  await loadServers();
  await loadUpdates();
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

    <div class="package-grid">
      <ServerSelector v-model="serverId" :servers="servers" :loading="loadingServers" />

      <v-card :loading="loadingUpdates" variant="outlined">
        <v-card-item class="bg-surface-variant py-3">
          <div class="package-card-header d-flex justify-space-between align-center">
            <div class="package-card-title">
              <v-card-title class="text-subtitle-1 font-weight-bold">{{ currentServer?.name || t('common.selectServer') }}</v-card-title>
              <v-card-subtitle class="text-caption">
                {{ t('packagesPage.lastRefreshed') }}: {{ updates?.lastRefreshedAt ? formatDateTime(updates.lastRefreshedAt) : t('common.never') }}
              </v-card-subtitle>
            </div>
            <div class="package-actions">
              <v-chip v-if="refreshInProgress" size="small" color="info" variant="tonal" prepend-icon="mdi-sync">
                {{ t('packagesPage.refreshing') }}
              </v-chip>
              <v-btn
                prepend-icon="mdi-refresh"
                size="small"
                variant="outlined"
                :disabled="operationBlocked || refreshInProgress"
                :loading="refreshInProgress"
                @click="refreshPackages"
                class="text-none"
              >
                {{ t('common.refresh') }}
              </v-btn>
              <v-btn
                color="primary"
                prepend-icon="mdi-upload"
                size="small"
                variant="flat"
                :disabled="operationBlocked || selectedPackageNames.length === 0"
                @click="upgradeSelected"
                class="text-none"
              >
                {{ t('packagesPage.upgradeSelected') }}
              </v-btn>
              <v-btn
                color="error"
                prepend-icon="mdi-arrow-up-bold-box"
                size="small"
                variant="outlined"
                :disabled="operationBlocked || !updates?.updates.length"
                @click="confirmUpgradeAllDialog = true"
                class="text-none"
              >
                {{ t('packagesPage.upgradeAll') }}
              </v-btn>
            </div>
          </div>
        </v-card-item>

        <v-card-text class="pa-4">
          <PageLoadingState v-if="loadingUpdates && !updates" min-height="280px" />
          <v-table v-else class="text-left" style="background: transparent;">
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
          <AppPagination v-model:page="page" v-model:page-size="pageSize" :total="total" />
        </v-card-text>
      </v-card>
    </div>

    <v-dialog v-model="confirmUpgradeAllDialog" width="460">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('packagesPage.confirmUpgradeAllTitle') }}</span>
          <v-btn icon="mdi-close" variant="text" @click="confirmUpgradeAllDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          {{ t('packagesPage.confirmUpgradeAllMessage', { name: currentServer?.name || t('common.unknown') }) }}
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <v-btn variant="text" class="text-none" @click="confirmUpgradeAllDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="error" variant="flat" class="text-none" @click="upgradeAll">{{ t('packagesPage.upgradeAll') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- Global Snackbar -->
    <v-snackbar v-model="snackbar" :color="snackbarColor" timeout="3000">
      {{ snackbarText }}
      <template v-slot:actions>
        <v-btn v-if="lastTaskId" color="white" variant="text" :to="taskRoute()">{{ t('taskCenter.task') }}</v-btn>
        <v-btn color="white" variant="text" @click="snackbar = false">{{ t('common.close') }}</v-btn>
      </template>
    </v-snackbar>
  </div>
</template>

<style scoped>
.package-grid {
  display: grid;
  grid-template-columns: minmax(0, 340px) minmax(0, 1fr);
  gap: 20px;
  align-items: start;
}

.package-card-header {
  gap: 12px;
}

.package-card-title {
  min-width: 0;
}

.package-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  flex-wrap: wrap;
}

@media (max-width: 980px) {
  .package-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 760px) {
  .package-card-header {
    align-items: stretch;
    flex-direction: column;
  }

  .package-actions,
  .package-actions .v-btn,
  .package-actions .v-chip {
    width: 100%;
  }
}
</style>
