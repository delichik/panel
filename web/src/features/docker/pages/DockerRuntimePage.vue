<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import ServerSelector from '@/components/ServerSelector.vue';
import TaskLogPanel from '@/components/tasks/TaskLogPanel.vue';
import { dockerApi } from '@/api/docker';
import { serversApi } from '@/api/servers';
import type {
  DockerCapabilityDto,
  DockerImageDto,
  DockerNetworkDto,
  DockerRuntimeServiceDto,
  DockerVolumeDto,
  ServerDto,
} from '@/types/api';

const servers = ref<ServerDto[]>([]);
const serverId = ref('');
const capability = ref<DockerCapabilityDto | null>(null);
const networks = ref<DockerNetworkDto[]>([]);
const volumes = ref<DockerVolumeDto[]>([]);
const images = ref<DockerImageDto[]>([]);
const unmanagedContainers = ref<DockerRuntimeServiceDto[]>([]);
const selectedImageIds = ref<string[]>([]);
const taskId = ref('');
const taskTitle = ref('Docker Task');
const activeTab = ref('containers');
const loadingServers = ref(false);
const loadingCapability = ref(false);
const loadingResources = ref(false);
const checkingUpdates = ref(false);
const actionLoading = ref('');
const error = ref('');
let bootstrapping = false;
let reloadSeq = 0;

// Snackbar notifications
const snackbar = ref(false);
const snackbarText = ref('');
const snackbarColor = ref('success');

function showMessage(text: string, color = 'success') {
  snackbarText.value = text;
  snackbarColor.value = color;
  snackbar.value = true;
}

// Confirmation Dialog
const confirmDialog = ref(false);
const confirmTitle = ref('Confirm action');
const confirmMessage = ref('');
const confirmAction = ref<(() => Promise<void>) | null>(null);

function confirm(title: string, message: string, action: () => Promise<void>) {
  confirmTitle.value = title;
  confirmMessage.value = message;
  confirmAction.value = action;
  confirmDialog.value = true;
}

async function executeConfirm() {
  if (confirmAction.value) {
    try {
      await confirmAction.value();
    } catch (err) {
      showMessage(err instanceof Error ? err.message : 'Action failed', 'error');
    }
  }
  confirmDialog.value = false;
}

const currentServer = computed(() => servers.value.find((server) => server.id === serverId.value));
const dockerSupported = computed(() => capability.value?.supported === true);
const composeSupported = computed(() => capability.value?.composeInstalled === true);
const capabilityPending = computed(() => capability.value?.pending === true);
const resourceDisabled = computed(() => !serverId.value || !dockerSupported.value || Boolean(actionLoading.value));
const updateableRows = computed(() => images.value.filter((row) => row.update?.updateAvailable || row.updateAvailable));

// Selection compute for updateable images
const selectedUpdateRows = computed(() => updateableRows.value.filter(row => selectedImageIds.value.includes(row.id)));
const selectAllUpdates = computed({
  get() {
    return updateableRows.value.length > 0 && selectedImageIds.value.length === updateableRows.value.length;
  },
  set(val) {
    if (val) {
      selectedImageIds.value = updateableRows.value.map(row => row.id);
    } else {
      selectedImageIds.value = [];
    }
  }
});

function displayLabels(labels?: Record<string, string>) {
  if (!labels) return [];
  return Object.entries(labels).slice(0, 4);
}

function imageName(image: DockerImageDto) {
  const repository = image.repository || '<none>';
  const tag = image.tag || 'latest';
  return `${repository}:${tag}`;
}

function containerName(container: DockerRuntimeServiceDto) {
  return container.name || container.id;
}

function shortContainerId(container: DockerRuntimeServiceDto) {
  return container.id.length > 12 ? container.id.slice(0, 12) : container.id;
}

function isContainerRunning(container: DockerRuntimeServiceDto) {
  return container.state === 'running' || (container.status ?? '').toLowerCase().startsWith('up');
}

function containerStatusColor(container: DockerRuntimeServiceDto) {
  if (isContainerRunning(container)) return 'success';
  if (container.state === 'exited') return 'warning';
  if (container.state === 'created') return 'info';
  return 'grey';
}

function containerPorts(container: DockerRuntimeServiceDto) {
  if (Array.isArray(container.ports)) return container.ports.join(', ') || '-';
  return container.ports || '-';
}

function capabilityType() {
  if (!capability.value || capabilityPending.value) return 'info';
  if (capability.value.lastError) return 'warning';
  return dockerSupported.value ? 'success' : 'error';
}

async function loadServers() {
  loadingServers.value = true;
  try {
    servers.value = await serversApi.listServers();
    if (!serverId.value && servers.value.length) serverId.value = servers.value[0].id;
  } finally {
    loadingServers.value = false;
  }
}

async function loadCapability(targetServerId = serverId.value) {
  if (!targetServerId) {
    capability.value = null;
    return;
  }
  loadingCapability.value = true;
  try {
    const nextCapability = await dockerApi.getCapability(targetServerId);
    if (targetServerId !== serverId.value) return;
    capability.value = nextCapability;
    error.value = '';
  } catch (err) {
    if (targetServerId !== serverId.value) return;
    capability.value = null;
    error.value = err instanceof Error ? err.message : 'Unable to load Docker capability';
  } finally {
    loadingCapability.value = false;
  }
}

async function loadResources(targetServerId = serverId.value) {
  if (!targetServerId || !dockerSupported.value) {
    networks.value = [];
    volumes.value = [];
    images.value = [];
    unmanagedContainers.value = [];
    selectedImageIds.value = [];
    return;
  }
  loadingResources.value = true;
  try {
    const [networkRows, volumeRows, imageRows, serviceRows] = await Promise.all([
      dockerApi.listNetworks(targetServerId),
      dockerApi.listVolumes(targetServerId),
      dockerApi.listImages(targetServerId),
      dockerApi.listServices(targetServerId),
    ]);
    if (targetServerId !== serverId.value) return;
    networks.value = networkRows.items ?? [];
    volumes.value = volumeRows.items ?? [];
    images.value = imageRows.items ?? [];
    unmanagedContainers.value = (serviceRows.items ?? []).filter((container) => !container.managed);
    selectedImageIds.value = [];
    error.value = '';
  } catch (err) {
    if (targetServerId !== serverId.value) return;
    error.value = err instanceof Error ? err.message : 'Unable to load Docker runtime resources';
  } finally {
    loadingResources.value = false;
  }
}

async function reloadAll(refreshRemote = false) {
  const targetServerId = serverId.value;
  const seq = ++reloadSeq;
  if (!targetServerId) return;
  if (refreshRemote) {
    try {
      const result = await dockerApi.refreshCapability(targetServerId);
      if ('taskId' in result && result.taskId) {
        taskId.value = result.taskId;
        taskTitle.value = 'Refresh Docker Runtime';
      }
    } catch (err) {
      error.value = err instanceof Error ? err.message : 'Unable to start Docker refresh';
      return;
    }
  }
  await loadCapability(targetServerId);
  if (seq !== reloadSeq || targetServerId !== serverId.value) return;
  await loadResources(targetServerId);
}

async function runDelete(kind: 'network' | 'volume' | 'image', id: string, name: string) {
  if (!serverId.value) return;
  confirm('Confirm delete', `Delete Docker ${kind} ${name}?`, async () => {
    actionLoading.value = `${kind}:${id}`;
    try {
      const result =
        kind === 'network'
          ? await dockerApi.deleteNetwork(serverId.value, id)
          : kind === 'volume'
            ? await dockerApi.deleteVolume(serverId.value, id)
            : await dockerApi.deleteImage(serverId.value, id);
      taskId.value = result.taskId;
      taskTitle.value = `Delete Docker ${kind}`;
      showMessage(`Docker ${kind} delete started`);
    } catch (err) {
      showMessage(err instanceof Error ? err.message : `Unable to delete Docker ${kind}`, 'error');
    } finally {
      actionLoading.value = '';
    }
  });
}

async function executeContainerAction(action: 'start' | 'stop' | 'delete', container: DockerRuntimeServiceDto) {
  if (!serverId.value) return;
  const id = container.id;
  const name = containerName(container);
  actionLoading.value = `container:${action}:${id}`;
  try {
    const result =
      action === 'start'
        ? await dockerApi.startContainer(serverId.value, id)
        : action === 'stop'
          ? await dockerApi.stopContainer(serverId.value, id)
          : await dockerApi.deleteContainer(serverId.value, id);
    taskId.value = result.taskId;
    taskTitle.value = `${action[0].toUpperCase()}${action.slice(1)} Docker container`;
    showMessage(`Docker container ${name} ${action} started`);
  } catch (err) {
    showMessage(err instanceof Error ? err.message : `Unable to ${action} Docker container`, 'error');
  } finally {
    actionLoading.value = '';
  }
}

function runContainerAction(action: 'start' | 'stop' | 'delete', container: DockerRuntimeServiceDto) {
  if (action === 'start') {
    void executeContainerAction(action, container);
    return;
  }
  const name = containerName(container);
  confirm(
    action === 'stop' ? 'Confirm stop' : 'Confirm delete',
    `${action === 'stop' ? 'Stop' : 'Delete'} Docker container ${name}?`,
    () => executeContainerAction(action, container),
  );
}

async function runPrune(kind: 'networks' | 'volumes' | 'images') {
  if (!serverId.value) return;
  confirm('Confirm prune', `Delete unused Docker ${kind}?`, async () => {
    actionLoading.value = `prune:${kind}`;
    try {
      const result =
        kind === 'networks'
          ? await dockerApi.pruneNetworks(serverId.value)
          : kind === 'volumes'
            ? await dockerApi.pruneVolumes(serverId.value)
            : await dockerApi.pruneImages(serverId.value);
      taskId.value = result.taskId;
      taskTitle.value = `Prune Docker ${kind}`;
      showMessage(`Docker ${kind} prune started`);
    } catch (err) {
      showMessage(err instanceof Error ? err.message : `Unable to prune Docker ${kind}`, 'error');
    } finally {
      actionLoading.value = '';
    }
  });
}

async function checkUpdates() {
  if (!serverId.value) return;
  checkingUpdates.value = true;
  try {
    const result = await dockerApi.checkImageUpdates(serverId.value);
    taskId.value = result.taskId;
    taskTitle.value = 'Check Docker Image Updates';
    selectedImageIds.value = [];
    showMessage('Docker image update check started');
  } catch (err) {
    showMessage(err instanceof Error ? err.message : 'Unable to check image updates', 'error');
  } finally {
    checkingUpdates.value = false;
  }
}

async function updateSelected() {
  if (!serverId.value || selectedUpdateRows.value.length === 0) return;
  const imageIds = selectedUpdateRows.value.map((row) => row.id);
  try {
    const result = await dockerApi.updateSelectedImages(serverId.value, imageIds);
    taskId.value = result.taskId;
    taskTitle.value = 'Update Selected Images';
    showMessage('Selected Docker image update started');
  } catch (err) {
    showMessage(err instanceof Error ? err.message : 'Unable to update selected images', 'error');
  }
}

async function updateAll() {
  if (!serverId.value) return;
  try {
    const result = await dockerApi.updateAllImages(serverId.value);
    taskId.value = result.taskId;
    taskTitle.value = 'Update All Images';
    showMessage('Docker image update started');
  } catch (err) {
    showMessage(err instanceof Error ? err.message : 'Unable to update images', 'error');
  }
}

async function handleTaskFinished() {
  await reloadAll();
  showMessage('Docker task finished');
}

watch(serverId, async () => {
  if (bootstrapping) return;
  capability.value = null;
  await reloadAll(false);
});

onMounted(async () => {
  bootstrapping = true;
  await loadServers();
  bootstrapping = false;
  await reloadAll(false);
});
</script>

<template>
  <div>
    <div class="d-flex justify-space-between align-center mb-6">
      <div>
        <h1 class="text-h4 font-weight-bold">Runtime Resources</h1>
        <p class="text-subtitle-1 text-medium-emphasis">Inspect Docker runtime resources and run task-backed cleanup or image updates.</p>
      </div>
      <div>
        <v-btn
          prepend-icon="mdi-refresh"
          :disabled="!serverId"
          :loading="loadingCapability || loadingResources"
          variant="flat"
          color="primary"
          @click="reloadAll(true)"
          class="text-none font-weight-bold"
        >
          Reload
        </v-btn>
      </div>
    </div>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>

    <div class="docker-grid">
      <ServerSelector v-model="serverId" :servers="servers" :loading="loadingServers" />

      <div class="runtime-column">
        <!-- Capability Card -->
        <v-card :loading="loadingCapability" variant="outlined" class="mb-4">
          <v-card-item class="bg-surface-variant py-3">
            <div class="d-flex justify-space-between align-center">
              <div>
                <v-card-title class="text-subtitle-1 font-weight-bold">{{ currentServer?.name || 'Select server' }}</v-card-title>
                <v-card-subtitle class="text-caption">
                  Checked: {{ capability?.lastCheckedAt || capability?.checkedAt || 'never' }}
                </v-card-subtitle>
              </div>
              <div class="d-flex" style="gap: 6px;">
                <v-chip :color="capabilityType()" size="small" label>
                  {{ capabilityPending ? 'Checking Docker' : dockerSupported ? 'Docker ready' : capability ? 'Docker unsupported' : 'Not checked' }}
                </v-chip>
                <v-chip :color="composeSupported ? 'success' : 'warning'" size="small" label>
                  {{ composeSupported ? 'Compose ready' : 'Compose unavailable' }}
                </v-chip>
              </div>
            </div>
          </v-card-item>

          <v-card-text class="pa-4">
            <div class="capability-body">
              <div>
                <span class="text-caption text-grey-darken-1">Docker</span>
                <strong class="text-body-1">{{ capability?.dockerVersion || '-' }}</strong>
              </div>
              <div>
                <span class="text-caption text-grey-darken-1">Compose</span>
                <strong class="text-body-1">{{ capability?.composeVersion || '-' }}</strong>
              </div>
              <div>
                <span class="text-caption text-grey-darken-1">State</span>
                <strong class="text-body-1">{{ capability?.stale ? 'stale cache' : capability ? 'current cache' : 'unknown' }}</strong>
              </div>
            </div>

            <v-alert
              v-if="capability?.lastError && !capabilityPending"
              type="warning"
              variant="tonal"
              class="mt-3"
            >
              {{ capability.lastError }}
            </v-alert>
            <v-alert
              v-if="capabilityPending"
              type="info"
              variant="tonal"
              class="mt-3"
            >
              Docker capability is being checked in the background.
            </v-alert>
          </v-card-text>
        </v-card>

        <v-alert
          v-if="capability && !dockerSupported && !capabilityPending"
          type="warning"
          variant="tonal"
          class="mb-4"
        >
          This server does not currently expose Docker runtime capability.
        </v-alert>

        <!-- Resources Card with Tabs -->
        <v-card :loading="loadingResources" variant="outlined">
          <v-tabs v-model="activeTab" color="primary" border-bottom>
            <v-tab value="containers" class="text-none font-weight-bold">Containers</v-tab>
            <v-tab value="networks" class="text-none font-weight-bold">Networks</v-tab>
            <v-tab value="volumes" class="text-none font-weight-bold">Volumes</v-tab>
            <v-tab value="images" class="text-none font-weight-bold">Images</v-tab>
          </v-tabs>

          <v-window v-model="activeTab" class="pa-4">
            <v-window-item value="containers">
              <v-table class="text-left" style="background: transparent;">
                <thead>
                  <tr>
                    <th class="font-weight-bold">Container</th>
                    <th class="font-weight-bold">Image</th>
                    <th class="font-weight-bold">State</th>
                    <th class="font-weight-bold">Ports</th>
                    <th class="font-weight-bold">Labels</th>
                    <th class="font-weight-bold text-right" style="width: 260px;">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-if="unmanagedContainers.length === 0">
                    <td colspan="6" class="text-center py-6 text-grey-darken-1">No unmanaged containers discovered</td>
                  </tr>
                  <tr v-for="row in unmanagedContainers" :key="row.id">
                    <td class="py-3">
                      <div class="font-weight-bold text-truncate" style="max-width: 220px;" :title="containerName(row)">{{ containerName(row) }}</div>
                      <div class="text-caption text-mono text-grey-darken-1">{{ shortContainerId(row) }}</div>
                    </td>
                    <td class="text-truncate" style="max-width: 220px;" :title="row.image">{{ row.image || '-' }}</td>
                    <td>
                      <v-chip :color="containerStatusColor(row)" size="small" label>
                        {{ row.state || row.status || 'unknown' }}
                      </v-chip>
                      <div v-if="row.status" class="text-caption text-grey-darken-1 mt-1">{{ row.status }}</div>
                    </td>
                    <td class="text-caption text-grey-darken-1 text-truncate" style="max-width: 220px;" :title="containerPorts(row)">
                      {{ containerPorts(row) }}
                    </td>
                    <td>
                      <div class="d-flex flex-wrap" style="gap: 4px;">
                        <v-chip v-for="[key, value] in displayLabels(row.labels)" :key="key" size="x-small" label color="grey-lighten-1">
                          {{ key }}={{ value }}
                        </v-chip>
                      </div>
                    </td>
                    <td class="text-right">
                      <div class="d-flex justify-end" style="gap: 8px;">
                        <v-btn
                          size="small"
                          color="success"
                          variant="outlined"
                          prepend-icon="mdi-play"
                          :disabled="resourceDisabled || isContainerRunning(row)"
                          :loading="actionLoading === `container:start:${row.id}`"
                          @click="runContainerAction('start', row)"
                          class="text-none"
                        >
                          Start
                        </v-btn>
                        <v-btn
                          size="small"
                          color="warning"
                          variant="outlined"
                          prepend-icon="mdi-stop"
                          :disabled="resourceDisabled || !isContainerRunning(row)"
                          :loading="actionLoading === `container:stop:${row.id}`"
                          @click="runContainerAction('stop', row)"
                          class="text-none"
                        >
                          Stop
                        </v-btn>
                        <v-btn
                          size="small"
                          color="error"
                          variant="outlined"
                          prepend-icon="mdi-delete"
                          :disabled="resourceDisabled"
                          :loading="actionLoading === `container:delete:${row.id}`"
                          @click="runContainerAction('delete', row)"
                          class="text-none"
                        >
                          Delete
                        </v-btn>
                      </div>
                    </td>
                  </tr>
                </tbody>
              </v-table>
            </v-window-item>

            <v-window-item value="networks">
              <div class="d-flex justify-end mb-3">
                <v-btn
                  color="error"
                  prepend-icon="mdi-delete-sweep"
                  variant="outlined"
                  size="small"
                  :disabled="resourceDisabled"
                  :loading="actionLoading === 'prune:networks'"
                  @click="runPrune('networks')"
                  class="text-none"
                >
                  Delete unused
                </v-btn>
              </div>

              <v-table class="text-left" style="background: transparent;">
                <thead>
                  <tr>
                    <th class="font-weight-bold">Name</th>
                    <th class="font-weight-bold">Driver</th>
                    <th class="font-weight-bold">Scope</th>
                    <th class="font-weight-bold">Labels</th>
                    <th class="font-weight-bold text-right" style="width: 120px;">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-if="networks.length === 0">
                    <td colspan="5" class="text-center py-6 text-grey-darken-1">No Docker networks discovered</td>
                  </tr>
                  <tr v-for="row in networks" :key="row.id">
                    <td class="font-weight-bold">{{ row.name }}</td>
                    <td>{{ row.driver }}</td>
                    <td>{{ row.scope }}</td>
                    <td>
                      <div class="d-flex flex-wrap" style="gap: 4px;">
                        <v-chip v-for="[key, value] in displayLabels(row.labels)" :key="key" size="x-small" label color="grey-lighten-1">
                          {{ key }}={{ value }}
                        </v-chip>
                      </div>
                    </td>
                    <td class="text-right">
                      <v-btn
                        size="small"
                        color="error"
                        variant="outlined"
                        prepend-icon="mdi-delete"
                        :disabled="resourceDisabled"
                        :loading="actionLoading === `network:${row.id}`"
                        @click="runDelete('network', row.id, row.name)"
                        class="text-none"
                      >
                        Delete
                      </v-btn>
                    </td>
                  </tr>
                </tbody>
              </v-table>
            </v-window-item>

            <v-window-item value="volumes">
              <div class="d-flex justify-end mb-3">
                <v-btn
                  color="error"
                  prepend-icon="mdi-delete-sweep"
                  variant="outlined"
                  size="small"
                  :disabled="resourceDisabled"
                  :loading="actionLoading === 'prune:volumes'"
                  @click="runPrune('volumes')"
                  class="text-none"
                >
                  Delete unused
                </v-btn>
              </div>

              <v-table class="text-left" style="background: transparent;">
                <thead>
                  <tr>
                    <th class="font-weight-bold">Name</th>
                    <th class="font-weight-bold">Driver</th>
                    <th class="font-weight-bold">Mountpoint</th>
                    <th class="font-weight-bold text-right" style="width: 120px;">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-if="volumes.length === 0">
                    <td colspan="4" class="text-center py-6 text-grey-darken-1">No Docker volumes discovered</td>
                  </tr>
                  <tr v-for="row in volumes" :key="row.name">
                    <td class="font-weight-bold text-truncate" style="max-width: 250px;">{{ row.name }}</td>
                    <td>{{ row.driver }}</td>
                    <td class="text-caption text-grey-darken-1 text-truncate" style="max-width: 300px;" :title="row.mountpoint">
                      {{ row.mountpoint }}
                    </td>
                    <td class="text-right">
                      <v-btn
                        size="small"
                        color="error"
                        variant="outlined"
                        prepend-icon="mdi-delete"
                        :disabled="resourceDisabled"
                        :loading="actionLoading === `volume:${row.name}`"
                        @click="runDelete('volume', row.name, row.name)"
                        class="text-none"
                      >
                        Delete
                      </v-btn>
                    </td>
                  </tr>
                </tbody>
              </v-table>
            </v-window-item>

            <v-window-item value="images">
              <div class="d-flex flex-wrap mb-3" style="gap: 8px; justify-content: flex-end;">
                <v-btn
                  prepend-icon="mdi-refresh"
                  variant="outlined"
                  size="small"
                  :disabled="resourceDisabled"
                  :loading="checkingUpdates"
                  @click="checkUpdates"
                  class="text-none"
                >
                  Check updates
                </v-btn>
                <v-btn
                  color="primary"
                  prepend-icon="mdi-arrow-up-bold-box"
                  variant="flat"
                  size="small"
                  :disabled="resourceDisabled || selectedUpdateRows.length === 0"
                  @click="updateSelected"
                  class="text-none"
                >
                  Update selected ({{ selectedUpdateRows.length }})
                </v-btn>
                <v-btn
                  color="primary"
                  variant="outlined"
                  size="small"
                  :disabled="resourceDisabled || updateableRows.length === 0"
                  @click="updateAll"
                  class="text-none"
                >
                  Update all
                </v-btn>
                <v-btn
                  color="error"
                  prepend-icon="mdi-delete-sweep"
                  variant="outlined"
                  size="small"
                  :disabled="resourceDisabled"
                  :loading="actionLoading === 'prune:images'"
                  @click="runPrune('images')"
                  class="text-none"
                >
                  Delete unused
                </v-btn>
              </div>

              <v-alert
                v-if="updateableRows.length > 0"
                type="info"
                variant="tonal"
                class="mb-4"
              >
                {{ updateableRows.length }} image updates available
              </v-alert>

              <!-- Image Updates Table (Shown only if updates exist) -->
              <v-card v-if="updateableRows.length > 0" variant="outlined" class="mb-4">
                <v-card-title class="text-subtitle-2 font-weight-bold bg-orange-lighten-5 py-2">Available Updates</v-card-title>
                <v-divider />
                <v-table class="text-left" style="background: transparent;">
                  <thead>
                    <tr>
                      <th style="width: 48px;">
                        <v-checkbox-btn v-model="selectAllUpdates" color="primary" />
                      </th>
                      <th class="font-weight-bold">Image</th>
                      <th class="font-weight-bold">Current</th>
                      <th class="font-weight-bold">Latest</th>
                      <th class="font-weight-bold">Status</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="row in updateableRows" :key="row.id">
                      <td>
                        <v-checkbox-btn v-model="selectedImageIds" :value="row.id" color="primary" />
                      </td>
                      <td class="font-weight-bold">{{ imageName(row) }}</td>
                      <td class="text-caption text-mono" style="max-width: 150px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">
                        {{ row.update?.currentDigest || row.currentVersion || '-' }}
                      </td>
                      <td class="text-caption text-mono" style="max-width: 150px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">
                        {{ row.update?.latestDigest || row.latestVersion || '-' }}
                      </td>
                      <td>
                        <v-chip color="warning" size="small" label>update</v-chip>
                      </td>
                    </tr>
                  </tbody>
                </v-table>
              </v-card>

              <!-- All Images Table -->
              <v-table class="text-left" style="background: transparent;">
                <thead>
                  <tr>
                    <th class="font-weight-bold">Image</th>
                    <th class="font-weight-bold">ID</th>
                    <th class="font-weight-bold">Size</th>
                    <th class="font-weight-bold">Update</th>
                    <th class="font-weight-bold text-right" style="width: 120px;">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-if="images.length === 0">
                    <td colspan="5" class="text-center py-6 text-grey-darken-1">No Docker images discovered</td>
                  </tr>
                  <tr v-for="row in images" :key="row.id">
                    <td class="font-weight-bold">{{ imageName(row) }}</td>
                    <td class="text-caption text-mono text-truncate" style="max-width: 150px;" :title="row.id">{{ row.id }}</td>
                    <td>{{ row.size }}</td>
                    <td>
                      <v-chip :color="row.update?.updateAvailable || row.updateAvailable ? 'warning' : 'info'" size="small" label>
                        {{ row.update?.updateAvailable || row.updateAvailable ? 'available' : 'unknown' }}
                      </v-chip>
                    </td>
                    <td class="text-right">
                      <v-btn
                        size="small"
                        color="error"
                        variant="outlined"
                        prepend-icon="mdi-delete"
                        :disabled="resourceDisabled"
                        :loading="actionLoading === `image:${row.id}`"
                        @click="runDelete('image', row.id, imageName(row))"
                        class="text-none"
                      >
                        Delete
                      </v-btn>
                    </td>
                  </tr>
                </tbody>
              </v-table>
            </v-window-item>
          </v-window>
        </v-card>
      </div>
    </div>

    <!-- Active Task Status Panel -->
    <v-card v-if="taskId" class="mt-6 pa-4" variant="outlined">
      <v-card-title class="px-0 pt-0 text-subtitle-1 font-weight-bold">{{ taskTitle }}</v-card-title>
      <v-card-text class="px-0 pb-0">
        <TaskLogPanel :task-id="taskId" :server-name="currentServer?.name" compact @finished="handleTaskFinished" />
      </v-card-text>
    </v-card>

    <!-- Confirmation Dialog -->
    <v-dialog v-model="confirmDialog" max-width="400px">
      <v-card>
        <v-card-title class="bg-surface-variant py-3 font-weight-bold">
          {{ confirmTitle }}
        </v-card-title>
        <v-divider />
        <v-card-text class="pa-4 text-body-1">
          {{ confirmMessage }}
        </v-card-text>
        <v-divider />
        <v-card-actions class="pa-3 bg-surface-variant">
          <v-spacer />
          <v-btn variant="outlined" class="text-none font-weight-bold" @click="confirmDialog = false">Cancel</v-btn>
          <v-btn color="error" variant="flat" class="text-none font-weight-bold" @click="executeConfirm">Confirm</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

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
.docker-grid {
  display: grid;
  grid-template-columns: 340px minmax(0, 1fr);
  gap: 24px;
}

.runtime-column {
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.capability-body {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 16px;
}

.text-mono {
  font-family: monospace;
}
</style>
