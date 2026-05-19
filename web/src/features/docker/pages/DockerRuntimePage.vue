<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { Delete, Refresh, Search, Upload } from '@element-plus/icons-vue';
import { ElMessage, ElMessageBox } from 'element-plus';
import ServerSelector from '@/components/ServerSelector.vue';
import TaskLogPanel from '@/components/tasks/TaskLogPanel.vue';
import { dockerApi } from '@/api/docker';
import { serversApi } from '@/api/servers';
import type {
  DockerCapabilityDto,
  DockerComposeStatusDto,
  DockerImageDto,
  DockerNetworkDto,
  DockerRuntimeServiceDto,
  DockerVolumeDto,
  ServerDto,
} from '@/types/api';

const servers = ref<ServerDto[]>([]);
const serverId = ref('');
const capability = ref<DockerCapabilityDto | null>(null);
const services = ref<DockerRuntimeServiceDto[]>([]);
const networks = ref<DockerNetworkDto[]>([]);
const volumes = ref<DockerVolumeDto[]>([]);
const images = ref<DockerImageDto[]>([]);
const selectedUpdateRows = ref<DockerImageDto[]>([]);
const taskId = ref('');
const taskTitle = ref('Docker Task');
const activeTab = ref('services');
const serviceDrawer = ref(false);
const selectedService = ref<DockerRuntimeServiceDto | null>(null);
const composeStatus = ref<DockerComposeStatusDto | null>(null);
const loadingServers = ref(false);
const loadingCapability = ref(false);
const loadingResources = ref(false);
const checkingUpdates = ref(false);
const loadingServiceStatus = ref(false);
const actionLoading = ref('');
const error = ref('');
let bootstrapping = false;
let reloadSeq = 0;

const currentServer = computed(() => servers.value.find((server) => server.id === serverId.value));
const dockerSupported = computed(() => capability.value?.supported === true);
const composeSupported = computed(() => capability.value?.composeInstalled === true);
const capabilityPending = computed(() => capability.value?.pending === true);
const resourceDisabled = computed(() => !serverId.value || !dockerSupported.value || Boolean(actionLoading.value));
const updateableRows = computed(() => images.value.filter((row) => row.update?.updateAvailable || row.updateAvailable));

function displayLabels(labels?: Record<string, string>) {
  if (!labels) return [];
  return Object.entries(labels).slice(0, 4);
}

function imageName(image: DockerImageDto) {
  const repository = image.repository || '<none>';
  const tag = image.tag || 'latest';
  return `${repository}:${tag}`;
}

function capabilityType() {
  if (!capability.value || capabilityPending.value) return 'info';
  if (capability.value.lastError) return 'warning';
  return dockerSupported.value ? 'success' : 'danger';
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
    services.value = [];
    networks.value = [];
    volumes.value = [];
    images.value = [];
    selectedUpdateRows.value = [];
    return;
  }
  loadingResources.value = true;
  try {
    const [serviceRows, networkRows, volumeRows, imageRows] = await Promise.all([
      dockerApi.listServices(targetServerId),
      dockerApi.listNetworks(targetServerId),
      dockerApi.listVolumes(targetServerId),
      dockerApi.listImages(targetServerId),
    ]);
    if (targetServerId !== serverId.value) return;
    services.value = serviceRows.items ?? [];
    networks.value = networkRows.items ?? [];
    volumes.value = volumeRows.items ?? [];
    images.value = imageRows.items ?? [];
    selectedUpdateRows.value = [];
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

async function openService(row: DockerRuntimeServiceDto) {
  selectedService.value = row;
  composeStatus.value = null;
  serviceDrawer.value = true;
  const projectName = row.projectName || row.project;
  if (!serverId.value || !projectName) return;
  loadingServiceStatus.value = true;
  try {
    composeStatus.value = await dockerApi.getProjectStatus(serverId.value, projectName);
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : 'Unable to load service status');
  } finally {
    loadingServiceStatus.value = false;
  }
}

async function runDelete(kind: 'network' | 'volume' | 'image', id: string, name: string) {
  if (!serverId.value) return;
  await ElMessageBox.confirm(`Delete Docker ${kind} ${name}?`, 'Confirm delete', { type: 'warning' });
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
    ElMessage.success(`Docker ${kind} delete started`);
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : `Unable to delete Docker ${kind}`);
  } finally {
    actionLoading.value = '';
  }
}

async function runPrune(kind: 'networks' | 'volumes' | 'images') {
  if (!serverId.value) return;
  await ElMessageBox.confirm(`Delete unused Docker ${kind}?`, 'Confirm prune', { type: 'warning' });
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
    ElMessage.success(`Docker ${kind} prune started`);
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : `Unable to prune Docker ${kind}`);
  } finally {
    actionLoading.value = '';
  }
}

async function checkUpdates() {
  if (!serverId.value) return;
  checkingUpdates.value = true;
  try {
    const result = await dockerApi.checkImageUpdates(serverId.value);
    taskId.value = result.taskId;
    taskTitle.value = 'Check Docker Image Updates';
    selectedUpdateRows.value = [];
    ElMessage.success('Docker image update check started');
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : 'Unable to check image updates');
  } finally {
    checkingUpdates.value = false;
  }
}

async function updateSelected() {
  if (!serverId.value || selectedUpdateRows.value.length === 0) return;
  const imageIds = selectedUpdateRows.value.map((row) => row.id);
  const result = await dockerApi.updateSelectedImages(serverId.value, imageIds);
  taskId.value = result.taskId;
  taskTitle.value = 'Update Selected Images';
  ElMessage.success('Selected Docker image update started');
}

async function updateAll() {
  if (!serverId.value) return;
  const result = await dockerApi.updateAllImages(serverId.value);
  taskId.value = result.taskId;
  taskTitle.value = 'Update All Images';
  ElMessage.success('Docker image update started');
}

async function handleTaskFinished() {
  await reloadAll();
  ElMessage.success('Docker task finished');
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
    <div class="panel-header panel">
      <div>
        <p class="page-subtitle">Inspect Docker runtime resources and run task-backed cleanup or image updates.</p>
      </div>
      <div class="toolbar">
        <el-button :icon="Refresh" :disabled="!serverId" :loading="loadingCapability || loadingResources" @click="reloadAll(true)">
          Reload
        </el-button>
      </div>
    </div>

    <el-alert v-if="error" class="page-alert" type="error" :title="error" show-icon />

    <div class="docker-grid">
      <ServerSelector v-model="serverId" :servers="servers" :loading="loadingServers" />

      <div class="runtime-column">
        <section class="panel capability-panel" v-loading="loadingCapability">
          <div class="panel-header">
            <div>
              <strong>{{ currentServer?.name || 'Select server' }}</strong>
              <div class="muted">Checked: {{ capability?.lastCheckedAt || capability?.checkedAt || 'never' }}</div>
            </div>
            <div class="toolbar">
              <el-tag :type="capabilityType()">
                {{ capabilityPending ? 'Checking Docker' : dockerSupported ? 'Docker ready' : capability ? 'Docker unsupported' : 'Not checked' }}
              </el-tag>
              <el-tag :type="composeSupported ? 'success' : 'warning'">
                {{ composeSupported ? 'Compose ready' : 'Compose unavailable' }}
              </el-tag>
            </div>
          </div>
          <div class="capability-body">
            <div>
              <span class="muted">Docker</span>
              <strong>{{ capability?.dockerVersion || '-' }}</strong>
            </div>
            <div>
              <span class="muted">Compose</span>
              <strong>{{ capability?.composeVersion || '-' }}</strong>
            </div>
            <div>
              <span class="muted">State</span>
              <strong>{{ capability?.stale ? 'stale cache' : capability ? 'current cache' : 'unknown' }}</strong>
            </div>
          </div>
          <el-alert
            v-if="capability?.lastError && !capabilityPending"
            class="capability-alert"
            type="warning"
            :title="capability.lastError"
            show-icon
          />
          <el-alert
            v-if="capabilityPending"
            class="capability-alert"
            type="info"
            title="Docker capability is being checked in the background."
            show-icon
          />
        </section>

        <el-alert
          v-if="capability && !dockerSupported && !capabilityPending"
          class="page-alert"
          type="warning"
          title="This server does not currently expose Docker runtime capability."
          show-icon
        />

        <section class="panel runtime-panel" v-loading="loadingResources">
          <el-tabs v-model="activeTab" class="runtime-tabs">
            <el-tab-pane label="Services" name="services">
              <el-table :data="services" empty-text="No Docker services or containers discovered">
                <el-table-column prop="name" label="Name" min-width="180" />
                <el-table-column prop="image" label="Image" min-width="220" />
                <el-table-column label="State" width="140">
                  <template #default="{ row }">
                    <el-tag :type="row.state === 'running' || row.status === 'running' ? 'success' : 'info'">
                      {{ row.state || row.status || 'unknown' }}
                    </el-tag>
                  </template>
                </el-table-column>
                <el-table-column label="Project" min-width="150">
                  <template #default="{ row }">{{ row.projectName || row.project || '-' }}</template>
                </el-table-column>
                <el-table-column label="Labels" min-width="220">
                  <template #default="{ row }">
                    <div class="label-list">
                      <el-tag v-for="[key, value] in displayLabels(row.labels)" :key="key" size="small">
                        {{ key }}={{ value }}
                      </el-tag>
                    </div>
                  </template>
                </el-table-column>
                <el-table-column label="Actions" width="130" fixed="right">
                  <template #default="{ row }">
                    <el-button size="small" :icon="Search" @click="openService(row)">Status</el-button>
                  </template>
                </el-table-column>
              </el-table>
            </el-tab-pane>

            <el-tab-pane label="Networks" name="networks">
              <div class="tab-toolbar">
                <el-button
                  type="danger"
                  :icon="Delete"
                  :disabled="resourceDisabled"
                  :loading="actionLoading === 'prune:networks'"
                  @click="runPrune('networks')"
                >
                  Delete unused
                </el-button>
              </div>
              <el-table :data="networks" empty-text="No Docker networks discovered">
                <el-table-column prop="name" label="Name" min-width="180" />
                <el-table-column prop="driver" label="Driver" width="140" />
                <el-table-column prop="scope" label="Scope" width="120" />
                <el-table-column label="Labels" min-width="220">
                  <template #default="{ row }">
                    <div class="label-list">
                      <el-tag v-for="[key, value] in displayLabels(row.labels)" :key="key" size="small">
                        {{ key }}={{ value }}
                      </el-tag>
                    </div>
                  </template>
                </el-table-column>
                <el-table-column label="Actions" width="120" fixed="right">
                  <template #default="{ row }">
                    <el-button
                      size="small"
                      type="danger"
                      :icon="Delete"
                      :disabled="resourceDisabled"
                      :loading="actionLoading === `network:${row.id}`"
                      @click="runDelete('network', row.id, row.name)"
                    >
                      Delete
                    </el-button>
                  </template>
                </el-table-column>
              </el-table>
            </el-tab-pane>

            <el-tab-pane label="Volumes" name="volumes">
              <div class="tab-toolbar">
                <el-button
                  type="danger"
                  :icon="Delete"
                  :disabled="resourceDisabled"
                  :loading="actionLoading === 'prune:volumes'"
                  @click="runPrune('volumes')"
                >
                  Delete unused
                </el-button>
              </div>
              <el-table :data="volumes" empty-text="No Docker volumes discovered">
                <el-table-column prop="name" label="Name" min-width="200" />
                <el-table-column prop="driver" label="Driver" width="140" />
                <el-table-column prop="mountpoint" label="Mountpoint" min-width="260" show-overflow-tooltip />
                <el-table-column label="Actions" width="120" fixed="right">
                  <template #default="{ row }">
                    <el-button
                      size="small"
                      type="danger"
                      :icon="Delete"
                      :disabled="resourceDisabled"
                      :loading="actionLoading === `volume:${row.name}`"
                      @click="runDelete('volume', row.name, row.name)"
                    >
                      Delete
                    </el-button>
                  </template>
                </el-table-column>
              </el-table>
            </el-tab-pane>

            <el-tab-pane label="Images" name="images">
              <div class="tab-toolbar">
                <el-button :icon="Refresh" :disabled="resourceDisabled" :loading="checkingUpdates" @click="checkUpdates">
                  Check updates
                </el-button>
                <el-button
                  type="primary"
                  :icon="Upload"
                  :disabled="resourceDisabled || selectedUpdateRows.length === 0"
                  @click="updateSelected"
                >
                  Update selected
                </el-button>
                <el-button type="primary" :disabled="resourceDisabled || updateableRows.length === 0" @click="updateAll">
                  Update all
                </el-button>
                <el-button
                  type="danger"
                  :icon="Delete"
                  :disabled="resourceDisabled"
                  :loading="actionLoading === 'prune:images'"
                  @click="runPrune('images')"
                >
                  Delete unused
                </el-button>
              </div>

              <el-alert
                v-if="updateableRows.length > 0"
                class="image-update-alert"
                type="info"
                :title="`${updateableRows.length} image updates available`"
                show-icon
              />

              <el-table
                v-if="updateableRows.length > 0"
                class="updates-table"
                :data="updateableRows"
                empty-text="No image update results"
                @selection-change="selectedUpdateRows = $event"
              >
                <el-table-column type="selection" width="48" />
                <el-table-column label="Image" min-width="220">
                  <template #default="{ row }">{{ imageName(row) }}</template>
                </el-table-column>
                <el-table-column label="Current" min-width="160">
                  <template #default="{ row }">{{ row.update?.currentDigest || row.currentVersion || '-' }}</template>
                </el-table-column>
                <el-table-column label="Latest" min-width="160">
                  <template #default="{ row }">{{ row.update?.latestDigest || row.latestVersion || '-' }}</template>
                </el-table-column>
                <el-table-column label="Status" width="140">
                  <template #default="{ row }">
                    <el-tag :type="row.update?.updateAvailable || row.updateAvailable ? 'warning' : 'success'">
                      {{ row.update?.updateAvailable || row.updateAvailable ? 'update' : 'current' }}
                    </el-tag>
                  </template>
                </el-table-column>
              </el-table>

              <el-table :data="images" empty-text="No Docker images discovered">
                <el-table-column type="selection" width="48" />
                <el-table-column label="Image" min-width="240">
                  <template #default="{ row }">{{ imageName(row) }}</template>
                </el-table-column>
                <el-table-column prop="id" label="ID" min-width="180" show-overflow-tooltip />
                <el-table-column prop="size" label="Size" width="120" />
                <el-table-column label="Update" width="120">
                  <template #default="{ row }">
                    <el-tag :type="row.update?.updateAvailable || row.updateAvailable ? 'warning' : 'info'">
                      {{ row.update?.updateAvailable || row.updateAvailable ? 'available' : 'unknown' }}
                    </el-tag>
                  </template>
                </el-table-column>
                <el-table-column label="Actions" width="120" fixed="right">
                  <template #default="{ row }">
                    <el-button
                      size="small"
                      type="danger"
                      :icon="Delete"
                      :disabled="resourceDisabled"
                      :loading="actionLoading === `image:${row.id}`"
                      @click="runDelete('image', row.id, imageName(row))"
                    >
                      Delete
                    </el-button>
                  </template>
                </el-table-column>
              </el-table>
            </el-tab-pane>
          </el-tabs>
        </section>
      </div>
    </div>

    <section v-if="taskId" class="panel task-section">
      <div class="panel-header"><strong>{{ taskTitle }}</strong></div>
      <div class="panel-body">
        <TaskLogPanel :task-id="taskId" :server-name="currentServer?.name" compact @finished="handleTaskFinished" />
      </div>
    </section>

    <el-drawer v-model="serviceDrawer" title="Runtime Service Status" size="520px">
      <div v-loading="loadingServiceStatus" class="service-detail">
        <section>
          <div class="detail-label">Container</div>
          <strong>{{ selectedService?.name || '-' }}</strong>
        </section>
        <section>
          <div class="detail-label">Image</div>
          <span>{{ selectedService?.image || '-' }}</span>
        </section>
        <section>
          <div class="detail-label">Status</div>
          <span>{{ selectedService?.status || selectedService?.state || '-' }}</span>
        </section>
        <section>
          <div class="detail-label">Compose project</div>
          <span>{{ selectedService?.projectName || selectedService?.project || 'not associated' }}</span>
        </section>
        <section v-if="composeStatus">
          <div class="detail-label">Project status</div>
          <span>{{ composeStatus.status || composeStatus.state }}</span>
        </section>
        <section>
          <div class="detail-label">Labels</div>
          <div class="label-list">
            <el-tag v-for="[key, value] in displayLabels(selectedService?.labels)" :key="key" size="small">
              {{ key }}={{ value }}
            </el-tag>
          </div>
        </section>
      </div>
    </el-drawer>
  </div>
</template>

<style scoped>
.page-alert,
.docker-grid,
.task-section {
  margin-top: 20px;
}

.docker-grid {
  display: grid;
  grid-template-columns: 340px minmax(0, 1fr);
  gap: 20px;
}

.runtime-column {
  display: grid;
  gap: 20px;
  min-width: 0;
}

.capability-body {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  padding: 16px 20px;
}

.capability-body > div {
  display: grid;
  gap: 4px;
}

.capability-alert {
  margin: 0 20px 18px;
}

.runtime-tabs {
  padding: 0 20px 20px;
}

.tab-toolbar {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  flex-wrap: wrap;
  margin-bottom: 12px;
}

.label-list {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}

.image-update-alert,
.updates-table {
  margin-bottom: 14px;
}

.service-detail {
  display: grid;
  gap: 18px;
}

.detail-label {
  margin-bottom: 6px;
  color: #667085;
  font-size: 12px;
  text-transform: uppercase;
}
</style>
