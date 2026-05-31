<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useI18n } from '@/i18n';
import ServerSelector from '@/components/ServerSelector.vue';
import TaskLogPanel from '@/components/tasks/TaskLogPanel.vue';
import { packagesApi } from '@/api/packages';
import { serversApi } from '@/api/servers';
import type { PackageUpdatesDto, ServerDto } from '@/types/api';

const servers = ref<ServerDto[]>([]);
const serverId = ref('');
const updates = ref<PackageUpdatesDto | null>(null);
const selectedPackageNames = ref<string[]>([]);
const taskId = ref('');
const loadingServers = ref(false);
const loadingUpdates = ref(false);
const packageRefreshRunning = ref(false);
const error = ref('');
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

const currentServer = computed(() => servers.value.find((server) => server.id === serverId.value));
const operationBlocked = computed(() => {
  const server = currentServer.value;
  return !server || server.os?.supported === false || server.sudo?.passwordless === false;
});

const selectedPackages = computed(() => (updates.value?.updates ?? []).filter(item => selectedPackageNames.value.includes(item.name)));
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

async function upgradeSelected() {
  if (!serverId.value || selectedPackages.value.length === 0) return;
  const names = selectedPackages.value.map((item) => item.name);
  try {
    const result = await packagesApi.upgradeSelected(serverId.value, names);
    taskId.value = result.taskId;
    showMessage(t('packagesPage.selectedUpgradeStarted'));
  } catch (err) {
    showMessage(err instanceof Error ? err.message : t('packagesPage.upgradeFailed'), 'error');
  }
}

async function upgradeAll() {
  if (!serverId.value) return;
  try {
    const result = await packagesApi.upgradeAll(serverId.value);
    taskId.value = result.taskId;
    showMessage(t('packagesPage.fullUpgradeStarted'));
  } catch (err) {
    showMessage(err instanceof Error ? err.message : t('packagesPage.fullUpgradeFailed'), 'error');
  }
}

async function handleTaskFinished() {
  await Promise.all([loadServers(), loadUpdates()]);
  showMessage(t('packagesPage.packageTaskFinished'));
}

watch(serverId, () => loadUpdates());
onMounted(async () => {
  await loadServers();
  await loadUpdates();
});
onBeforeUnmount(stopRefreshPolling);
</script>

<template>
  <div>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>
    <v-alert
      v-if="operationBlocked && currentServer"
      type="warning"
      variant="tonal"
      class="mb-4"
    >
      {{ t('packagesPage.blockedHint') }}
    </v-alert>

    <div class="package-grid">
      <ServerSelector v-model="serverId" :servers="servers" :loading="loadingServers" />

      <v-card :loading="loadingUpdates" variant="outlined">
        <v-card-item class="bg-surface-variant py-3">
          <div class="d-flex justify-space-between align-center">
            <div>
              <v-card-title class="text-subtitle-1 font-weight-bold">{{ currentServer?.name || t('common.selectServer') }}</v-card-title>
              <v-card-subtitle class="text-caption">
                {{ t('packagesPage.lastRefreshed') }}: {{ updates?.lastRefreshedAt ? formatDateTime(updates.lastRefreshedAt) : t('common.never') }}
              </v-card-subtitle>
            </div>
            <div class="d-flex" style="gap: 8px;">
              <v-chip v-if="refreshInProgress" size="small" color="info" variant="tonal" prepend-icon="mdi-sync">
                {{ t('packagesPage.refreshing') }}
              </v-chip>
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
                @click="upgradeAll"
                class="text-none"
              >
                {{ t('packagesPage.upgradeAll') }}
              </v-btn>
            </div>
          </div>
        </v-card-item>

        <v-card-text class="pa-4">
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
              <tr v-for="row in updates?.updates ?? []" :key="row.name">
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
        </v-card-text>
      </v-card>
    </div>

    <!-- Active Task Progress -->
    <v-card v-slot:prepend v-if="taskId" class="mt-6 pa-4" variant="outlined">
      <v-card-title class="px-0 pt-0 text-subtitle-1 font-weight-bold">{{ t('packagesPage.runningTask') }}</v-card-title>
      <v-card-text class="px-0 pb-0">
        <TaskLogPanel :task-id="taskId" :server-name="currentServer?.name" @finished="handleTaskFinished" />
      </v-card-text>
    </v-card>

    <!-- Global Snackbar -->
    <v-snackbar v-model="snackbar" :color="snackbarColor" timeout="3000">
      {{ snackbarText }}
      <template v-slot:actions>
        <v-btn color="white" variant="text" @click="snackbar = false">{{ t('common.close') }}</v-btn>
      </template>
    </v-snackbar>
  </div>
</template>

<style scoped>
.package-grid {
  display: grid;
  grid-template-columns: 340px minmax(0, 1fr);
  gap: 24px;
}
</style>
