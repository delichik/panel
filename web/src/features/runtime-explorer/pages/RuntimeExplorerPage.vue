<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import ServerSelector from '@/components/ServerSelector.vue';
import TaskLogPanel from '@/components/tasks/TaskLogPanel.vue';
import { runtimeExplorerApi } from '@/api/runtimeExplorer';
import { serversApi } from '@/api/servers';
import type { RuntimeExplorerContainerDto, RuntimeExplorerNodeDto, ServerDto } from '@/types/api';
import { runtimeContainerActions } from '../runtimeActions';

const servers = ref<ServerDto[]>([]);
const nodeId = ref('');
const runtime = ref<RuntimeExplorerNodeDto | null>(null);
const activeTab = ref('containers');
const taskId = ref('');
const taskTitle = ref('Runtime Explorer Task');
const loading = ref(false);
const loadingServers = ref(false);
const actionLoading = ref('');
const error = ref('');
const managedDeleteDialog = ref(false);
const managedDeleteTarget = ref<RuntimeExplorerContainerDto | null>(null);

const currentServer = computed(() => servers.value.find((server) => server.id === nodeId.value));
const containers = computed(() => runtime.value?.containers ?? []);
const networks = computed(() => runtime.value?.networks ?? []);
const volumes = computed(() => runtime.value?.volumes ?? []);
const images = computed(() => runtime.value?.images ?? []);

function markerColor(managed: boolean) {
  return managed ? 'primary' : 'grey';
}

function containerPorts(container: RuntimeExplorerContainerDto) {
  if (Array.isArray(container.ports)) return container.ports.join(', ') || '-';
  return container.ports || '-';
}

function actionState(container: RuntimeExplorerContainerDto) {
  return runtimeContainerActions(container);
}

function taskFromResult(result: { taskId?: string; tasks?: Array<{ id: string }> }, title: string) {
  taskId.value = result.taskId || result.tasks?.[0]?.id || '';
  taskTitle.value = title;
}

async function loadServers() {
  loadingServers.value = true;
  try {
    servers.value = await serversApi.listServers();
    if (!nodeId.value && servers.value.length) nodeId.value = servers.value[0].id;
  } finally {
    loadingServers.value = false;
  }
}

async function loadRuntime() {
  if (!nodeId.value) {
    runtime.value = null;
    return;
  }
  loading.value = true;
  try {
    runtime.value = await runtimeExplorerApi.getNodeRuntime(nodeId.value);
    error.value = '';
  } catch (err) {
    runtime.value = null;
    error.value = err instanceof Error ? err.message : 'Unable to load Runtime Explorer data';
  } finally {
    loading.value = false;
  }
}

async function runContainerAction(action: 'restart' | 'stop' | 'delete', container: RuntimeExplorerContainerDto) {
  if (action === 'delete' && container.managed) {
    managedDeleteTarget.value = container;
    managedDeleteDialog.value = true;
    return;
  }
  await executeContainerAction(action, container);
}

async function executeContainerAction(action: 'restart' | 'stop' | 'delete', container: RuntimeExplorerContainerDto) {
  if (!nodeId.value) return;
  actionLoading.value = `${action}:${container.id}`;
  try {
    const result = action === 'restart'
      ? await runtimeExplorerApi.restartContainer(nodeId.value, container.id)
      : action === 'stop'
        ? await runtimeExplorerApi.stopContainer(nodeId.value, container.id)
        : await runtimeExplorerApi.deleteContainer(nodeId.value, container.id);
    taskFromResult(result, `${action} container`);
    await loadRuntime();
  } catch (err) {
    error.value = err instanceof Error ? err.message : `Unable to ${action} container`;
  } finally {
    actionLoading.value = '';
  }
}

async function confirmManagedDelete() {
  const target = managedDeleteTarget.value;
  if (!target) return;
  managedDeleteDialog.value = false;
  managedDeleteTarget.value = null;
  await executeContainerAction('delete', target);
}

async function prune() {
  if (!nodeId.value) return;
  actionLoading.value = 'prune';
  try {
    const result = await runtimeExplorerApi.prune(nodeId.value);
    taskFromResult(result, 'Prune unmanaged runtime resources');
    await loadRuntime();
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to prune runtime resources';
  } finally {
    actionLoading.value = '';
  }
}

watch(nodeId, loadRuntime);
onMounted(async () => {
  await loadServers();
  await loadRuntime();
});
</script>

<template>
  <div>
    <div class="d-flex justify-space-between align-center mb-5">
      <div>
        <h1 class="text-h4 font-weight-bold">Runtime Explorer</h1>
        <p class="text-subtitle-1 text-medium-emphasis">Observe Docker resources and run limited task-backed actions for unmanaged runtime.</p>
      </div>
      <div class="d-flex" style="gap: 10px;">
        <v-btn prepend-icon="mdi-delete-sweep" variant="outlined" color="error" :loading="actionLoading === 'prune'" :disabled="!nodeId" @click="prune">Prune</v-btn>
        <v-btn prepend-icon="mdi-refresh" color="primary" variant="flat" :loading="loading" :disabled="!nodeId" @click="loadRuntime">Refresh</v-btn>
      </div>
    </div>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>

    <div class="runtime-grid">
      <ServerSelector v-model="nodeId" :servers="servers" :loading="loadingServers" />

      <div class="runtime-column">
        <v-card variant="outlined" :loading="loading" class="mb-4">
          <v-card-title class="d-flex align-center justify-space-between">
            <span>{{ currentServer?.name || runtime?.nodeName || 'Select node' }}</span>
            <div class="d-flex" style="gap: 6px;">
              <v-chip :color="runtime?.capability?.dockerInstalled ? 'success' : 'warning'" size="small" label>Docker</v-chip>
              <v-chip :color="runtime?.capability?.composeInstalled ? 'success' : 'warning'" size="small" label>Compose</v-chip>
              <v-chip :color="runtime?.stale ? 'warning' : 'info'" size="small" label>{{ runtime?.stale ? 'stale' : 'cache' }}</v-chip>
            </div>
          </v-card-title>
          <v-card-text v-if="runtime?.error">
            <v-alert type="warning" variant="tonal">{{ runtime.error }}</v-alert>
          </v-card-text>
        </v-card>

        <v-card variant="outlined" :loading="loading">
          <v-tabs v-model="activeTab">
            <v-tab value="containers">Containers</v-tab>
            <v-tab value="networks">Networks</v-tab>
            <v-tab value="volumes">Volumes</v-tab>
            <v-tab value="images">Images</v-tab>
          </v-tabs>
          <v-window v-model="activeTab" class="pa-4">
            <v-window-item value="containers">
              <v-table>
                <thead>
                  <tr>
                    <th>Container</th>
                    <th>Image</th>
                    <th>State</th>
                    <th>Ports</th>
                    <th>Owner</th>
                    <th class="text-right">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-if="containers.length === 0"><td colspan="6" class="text-center py-6 text-medium-emphasis">No containers discovered</td></tr>
                  <tr v-for="container in containers" :key="container.id">
                    <td>
                      <div class="font-weight-bold">{{ container.name || container.id }}</div>
                      <div class="text-caption text-medium-emphasis">{{ container.id.slice(0, 12) }}</div>
                    </td>
                    <td>{{ container.image || '-' }}</td>
                    <td><v-chip size="small" label>{{ container.health || container.state || container.status }}</v-chip></td>
                    <td class="text-caption">{{ containerPorts(container) }}</td>
                    <td>
                      <v-chip :color="markerColor(container.managed)" size="small" label>{{ container.managed ? 'managed' : 'unmanaged' }}</v-chip>
                      <RouterLink v-if="actionState(container).serviceLink" :to="actionState(container).serviceLink" class="text-caption ml-2">service</RouterLink>
                    </td>
                    <td class="text-right">
                      <div class="d-flex justify-end" style="gap: 6px;">
                        <v-btn size="small" icon="mdi-restart" variant="text" :loading="actionLoading === `restart:${container.id}`" @click="runContainerAction('restart', container)" />
                        <v-tooltip :text="actionState(container).stop.reason || 'Stop container'">
                          <template #activator="{ props }">
                            <v-btn v-bind="props" size="small" icon="mdi-stop" variant="text" :disabled="actionState(container).stop.disabled" :loading="actionLoading === `stop:${container.id}`" @click="runContainerAction('stop', container)" />
                          </template>
                        </v-tooltip>
                        <v-tooltip :text="actionState(container).delete.reason || 'Delete container'">
                          <template #activator="{ props }">
                            <v-btn v-bind="props" size="small" icon="mdi-delete" color="error" variant="text" :disabled="actionState(container).delete.disabled" :loading="actionLoading === `delete:${container.id}`" @click="runContainerAction('delete', container)" />
                          </template>
                        </v-tooltip>
                      </div>
                    </td>
                  </tr>
                </tbody>
              </v-table>
            </v-window-item>

            <v-window-item value="networks">
              <v-table>
                <tbody>
                  <tr v-if="networks.length === 0"><td class="text-center py-6 text-medium-emphasis">No networks discovered</td></tr>
                  <tr v-for="network in networks" :key="network.id">
                    <td class="font-weight-bold">{{ network.name }}</td>
                    <td>{{ network.driver || '-' }}</td>
                    <td><v-chip :color="markerColor(network.managed)" size="small" label>{{ network.managed ? 'managed' : 'unmanaged' }}</v-chip></td>
                  </tr>
                </tbody>
              </v-table>
            </v-window-item>

            <v-window-item value="volumes">
              <v-table>
                <tbody>
                  <tr v-if="volumes.length === 0"><td class="text-center py-6 text-medium-emphasis">No volumes discovered</td></tr>
                  <tr v-for="volume in volumes" :key="volume.name">
                    <td class="font-weight-bold">{{ volume.name }}</td>
                    <td>{{ volume.driver || '-' }}</td>
                    <td><v-chip :color="markerColor(volume.managed)" size="small" label>{{ volume.managed ? 'managed' : 'unmanaged' }}</v-chip></td>
                  </tr>
                </tbody>
              </v-table>
            </v-window-item>

            <v-window-item value="images">
              <v-table>
                <tbody>
                  <tr v-if="images.length === 0"><td class="text-center py-6 text-medium-emphasis">No images discovered</td></tr>
                  <tr v-for="image in images" :key="image.id">
                    <td class="font-weight-bold">{{ image.repository }}:{{ image.tag || 'latest' }}</td>
                    <td class="text-caption">{{ image.id.slice(0, 20) }}</td>
                    <td>{{ image.size || '-' }}</td>
                    <td><v-chip :color="markerColor(image.managed)" size="small" label>{{ image.managed ? 'managed' : 'unmanaged' }}</v-chip></td>
                  </tr>
                </tbody>
              </v-table>
            </v-window-item>
          </v-window>
        </v-card>

        <v-card v-if="taskId" variant="outlined" class="mt-4 pa-4">
          <v-card-title class="px-0 pt-0 text-subtitle-1 font-weight-bold">{{ taskTitle }}</v-card-title>
          <TaskLogPanel :task-id="taskId" :server-name="currentServer?.name" compact @finished="loadRuntime" />
        </v-card>
      </div>
    </div>

    <v-dialog v-model="managedDeleteDialog" max-width="560">
      <v-card>
        <v-card-title class="text-subtitle-1 font-weight-bold">Delete managed container?</v-card-title>
        <v-card-text>
          <v-alert type="warning" variant="tonal" class="mb-3">
            This container is managed by Container Service. Deleting it removes only the current Docker container; if the service is enabled, panel will automatically redeploy it.
          </v-alert>
          <div class="text-body-2">
            Container: <strong>{{ managedDeleteTarget?.name || managedDeleteTarget?.id }}</strong>
          </div>
          <div v-if="managedDeleteTarget?.serviceName" class="text-body-2">
            Service: <strong>{{ managedDeleteTarget.serviceName }}</strong>
          </div>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" class="text-none" @click="managedDeleteDialog = false">Cancel</v-btn>
          <v-btn color="error" variant="flat" class="text-none font-weight-bold" :loading="actionLoading === `delete:${managedDeleteTarget?.id}`" @click="confirmManagedDelete">Delete container</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>

<style scoped>
.runtime-grid {
  display: grid;
  grid-template-columns: 340px minmax(0, 1fr);
  gap: 20px;
}

.runtime-column {
  min-width: 0;
}
</style>
