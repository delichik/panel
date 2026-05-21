<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import ServerSelector from '@/components/ServerSelector.vue';
import TaskLogPanel from '@/components/tasks/TaskLogPanel.vue';
import { packagesApi } from '@/api/packages';
import { serversApi } from '@/api/servers';
import type { PackageUpdateDto, PackageUpdatesDto, ServerDto } from '@/types/api';

const servers = ref<ServerDto[]>([]);
const serverId = ref('');
const updates = ref<PackageUpdatesDto | null>(null);
const selectedPackageNames = ref<string[]>([]);
const taskId = ref('');
const loadingServers = ref(false);
const loadingUpdates = ref(false);
const error = ref('');

// Snackbar notification state
const snackbar = ref(false);
const snackbarText = ref('');
const snackbarColor = ref('success');

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

async function loadUpdates() {
  if (!serverId.value) {
    updates.value = null;
    return;
  }
  loadingUpdates.value = true;
  try {
    updates.value = await packagesApi.listUpdates(serverId.value);
    selectedPackageNames.value = [];
    error.value = '';
  } catch (err) {
    updates.value = null;
    error.value = err instanceof Error ? err.message : 'Unable to load package updates';
  } finally {
    loadingUpdates.value = false;
  }
}

async function refreshUpdates() {
  if (!serverId.value) return;
  try {
    const result = await packagesApi.refresh(serverId.value);
    taskId.value = result.taskId;
    showMessage('Package refresh started');
  } catch (err) {
    showMessage(err instanceof Error ? err.message : 'Failed to refresh packages', 'error');
  }
}

async function upgradeSelected() {
  if (!serverId.value || selectedPackages.value.length === 0) return;
  const names = selectedPackages.value.map((item) => item.name);
  try {
    const result = await packagesApi.upgradeSelected(serverId.value, names);
    taskId.value = result.taskId;
    showMessage('Selected package upgrade started');
  } catch (err) {
    showMessage(err instanceof Error ? err.message : 'Upgrade failed', 'error');
  }
}

async function upgradeAll() {
  if (!serverId.value) return;
  try {
    const result = await packagesApi.upgradeAll(serverId.value);
    taskId.value = result.taskId;
    showMessage('Full package upgrade started');
  } catch (err) {
    showMessage(err instanceof Error ? err.message : 'Full upgrade failed', 'error');
  }
}

async function handleTaskFinished() {
  await Promise.all([loadServers(), loadUpdates()]);
  showMessage('Package task finished');
}

watch(serverId, loadUpdates);
onMounted(async () => {
  await loadServers();
  await loadUpdates();
});
</script>

<template>
  <div>
    <div class="d-flex justify-space-between align-center mb-6">
      <div>
        <h1 class="text-h4 font-weight-bold">Package Updates</h1>
        <p class="text-subtitle-1 text-medium-emphasis">Refresh package caches and execute Debian upgrades as tracked tasks.</p>
      </div>
      <div>
        <v-btn
          prepend-icon="mdi-refresh"
          :disabled="!serverId"
          :loading="loadingUpdates"
          variant="flat"
          color="primary"
          @click="loadUpdates"
          class="text-none font-weight-bold"
        >
          Reload
        </v-btn>
      </div>
    </div>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>
    <v-alert
      v-if="operationBlocked && currentServer"
      type="warning"
      variant="tonal"
      class="mb-4"
    >
      This server is not eligible for package operations until distro support and passwordless sudo are confirmed.
    </v-alert>

    <div class="package-grid">
      <ServerSelector v-model="serverId" :servers="servers" :loading="loadingServers" />

      <v-card :loading="loadingUpdates" variant="outlined">
        <v-card-item class="bg-surface-variant py-3">
          <div class="d-flex justify-space-between align-center">
            <div>
              <v-card-title class="text-subtitle-1 font-weight-bold">{{ currentServer?.name || 'Select server' }}</v-card-title>
              <v-card-subtitle class="text-caption">
                Last refreshed: {{ updates?.lastRefreshedAt ? new Date(updates.lastRefreshedAt).toLocaleString() : 'never' }}
              </v-card-subtitle>
            </div>
            <div class="d-flex" style="gap: 8px;">
              <v-btn
                prepend-icon="mdi-sync"
                size="small"
                variant="outlined"
                :disabled="!serverId || operationBlocked"
                @click="refreshUpdates"
                class="text-none"
              >
                Refresh updates
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
                Upgrade selected
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
                Upgrade all
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
                <th class="font-weight-bold">Package</th>
                <th class="font-weight-bold">Installed Version</th>
                <th class="font-weight-bold">Candidate Version</th>
                <th class="font-weight-bold">Source</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="!updates || updates.updates.length === 0">
                <td colspan="5" class="text-center py-6 text-grey-darken-1">No upgradeable packages</td>
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
      <v-card-title class="px-0 pt-0 text-subtitle-1 font-weight-bold">Running Task</v-card-title>
      <v-card-text class="px-0 pb-0">
        <TaskLogPanel :task-id="taskId" :server-name="currentServer?.name" @finished="handleTaskFinished" />
      </v-card-text>
    </v-card>

    <!-- Global Snackbar -->
    <v-snackbar v-model="snackbar" :color="snackbarColor" timeout="3000">
      {{ snackbarText }}
      <template v-slot:actions>
        <v-btn color="white" variant="text" @click="snackbar = false">Close</v-btn>
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
