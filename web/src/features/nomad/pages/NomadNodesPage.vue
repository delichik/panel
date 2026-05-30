<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { nomadApi } from '@/api/nomad';
import TaskLogPanel from '@/components/tasks/TaskLogPanel.vue';
import type { NomadControlPlaneDto, ProjectedNomadNodeDto } from '@/types/api';

const router = useRouter();
const controlPlane = ref<NomadControlPlaneDto | null>(null);
const loading = ref(false);
const joining = ref(false);
const error = ref('');
const joinDialog = ref(false);
const selectedServerId = ref('');
const activeTaskId = ref('');
const activeTaskServerName = ref('');

const nodes = computed(() => controlPlane.value?.nodes ?? []);
const candidateServers = computed(() => controlPlane.value?.joinCandidates ?? []);
const readyCount = computed(() => nodes.value.filter((node) => node.status === 'ready').length);
const managedCount = computed(() => nodes.value.filter((node) => node.kind === 'managed').length);
const pendingCount = computed(() => nodes.value.filter((node) => node.kind === 'pending').length);
const selectedServer = computed(() => candidateServers.value.find((server) => server.id === selectedServerId.value) ?? null);
const candidateOptions = computed(() =>
  candidateServers.value.map((server) => ({
    label: `${server.name} (${server.host}:${server.port})`,
    value: server.id,
  })),
);

function statusColor(nodeStatus?: string) {
  if (nodeStatus === 'ready') return 'success';
  if (nodeStatus === 'down' || nodeStatus === 'failed') return 'error';
  if (nodeStatus === 'unmanaged') return 'grey';
  if (nodeStatus === 'registering') return 'info';
  return 'warning';
}

function kindColor(kind?: string) {
  if (kind === 'managed') return 'primary';
  if (kind === 'pending') return 'warning';
  return 'grey';
}

function taskServerName(node: ProjectedNomadNodeDto) {
  return node.name || node.host || node.serverId || '';
}

function openJoinDialog() {
  selectedServerId.value = candidateServers.value[0]?.id ?? '';
  joinDialog.value = true;
}

async function load() {
  loading.value = true;
  try {
    const result = await nomadApi.controlPlane();
    controlPlane.value = result;
    error.value = '';
    if (result.status === 'unconfigured') {
      await router.replace('/nomad/setup');
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to load Nomad control plane';
  } finally {
    loading.value = false;
  }
}

async function joinSelectedServer() {
  if (!selectedServerId.value) return;
  joining.value = true;
  try {
    const server = selectedServer.value;
    const result = await nomadApi.joinServer(selectedServerId.value);
    activeTaskId.value = result.taskId;
    activeTaskServerName.value = server?.name ?? '';
    joinDialog.value = false;
    error.value = '';
    await load();
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to join Nomad node';
  } finally {
    joining.value = false;
  }
}

async function handleTaskFinished() {
  await load();
}

onMounted(load);
</script>

<template>
  <div>
    <div class="page-heading mb-5">
      <div>
        <div class="eyebrow">Nomad runtime</div>
        <h1 class="text-h4 font-weight-bold">Nomad Nodes</h1>
      </div>
      <div class="page-actions">
        <v-btn prepend-icon="mdi-account-network" color="primary" variant="flat" class="text-none" :disabled="candidateServers.length === 0" @click="openJoinDialog">Join Node</v-btn>
        <v-btn prepend-icon="mdi-refresh" color="primary" variant="outlined" :loading="loading" class="text-none" @click="load">Refresh</v-btn>
      </div>
    </div>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>
    <v-alert v-else-if="controlPlane?.status === 'bootstrapping'" type="info" variant="tonal" class="mb-4">
      First Nomad server is bootstrapping. Pending nodes below are projected from Panel tasks until Nomad API registration succeeds.
    </v-alert>
    <v-alert v-else-if="controlPlane?.status === 'degraded'" type="warning" variant="tonal" class="mb-4">
      Nomad was configured before, but the API is currently unreachable. Showing Panel task projection.
    </v-alert>
    <v-alert v-else-if="controlPlane?.status === 'connected'" type="success" variant="tonal" class="mb-4">
      Connected to leader {{ controlPlane.leader || 'unknown' }}.
    </v-alert>

    <div class="summary-strip mb-4">
      <v-card variant="outlined" class="summary-card"><div class="text-caption text-medium-emphasis">Nodes</div><div class="text-h5 font-weight-bold font-tabular">{{ nodes.length }}</div></v-card>
      <v-card variant="outlined" class="summary-card"><div class="text-caption text-medium-emphasis">Ready</div><div class="text-h5 font-weight-bold font-tabular">{{ readyCount }}</div></v-card>
      <v-card variant="outlined" class="summary-card"><div class="text-caption text-medium-emphasis">Managed</div><div class="text-h5 font-weight-bold font-tabular">{{ managedCount }}</div></v-card>
      <v-card variant="outlined" class="summary-card"><div class="text-caption text-medium-emphasis">Pending</div><div class="text-h5 font-weight-bold font-tabular">{{ pendingCount }}</div></v-card>
    </div>

    <v-card v-if="activeTaskId" class="mb-4 pa-4" variant="outlined">
      <v-card-title class="px-0 pt-0 text-subtitle-1 font-weight-bold">Nomad Join Task</v-card-title>
      <TaskLogPanel :task-id="activeTaskId" :server-name="activeTaskServerName" compact @finished="handleTaskFinished" />
    </v-card>

    <v-card variant="outlined" :loading="loading">
      <v-table>
        <thead><tr><th>Name</th><th>Node ID</th><th>Host</th><th>Role</th><th>Status</th><th>Source</th><th>Task</th></tr></thead>
        <tbody>
          <tr v-for="node in nodes" :key="node.nodeId || node.serverId || node.name">
            <td class="font-weight-bold">{{ node.name || '-' }}</td>
            <td class="mono">{{ node.nodeId || '-' }}</td>
            <td>{{ node.host || '-' }}</td>
            <td><v-chip size="small" variant="tonal" label>{{ node.role }}</v-chip></td>
            <td>
              <v-chip :color="statusColor(node.status)" size="small" variant="tonal" label>{{ node.status || 'unknown' }}</v-chip>
              <div v-if="node.error" class="text-caption text-error mt-1">{{ node.error }}</div>
            </td>
            <td>
              <v-chip :color="kindColor(node.kind)" size="small" variant="tonal" label>{{ node.kind }}</v-chip>
              <div v-if="node.serverId" class="text-caption text-medium-emphasis mt-1">{{ node.serverId }}</div>
            </td>
            <td>
              <v-btn v-if="node.taskId" size="small" variant="text" color="primary" class="text-none" @click="activeTaskId = node.taskId; activeTaskServerName = taskServerName(node)">Task log</v-btn>
              <span v-else class="text-medium-emphasis">-</span>
            </td>
          </tr>
          <tr v-if="nodes.length === 0"><td colspan="7" class="text-center py-8 text-medium-emphasis">No projected Nomad nodes</td></tr>
        </tbody>
      </v-table>
    </v-card>

    <v-dialog v-model="joinDialog" width="520">
      <v-card class="join-dialog">
        <v-card-title class="d-flex align-center justify-space-between">
          <span>Join Node</span>
          <v-btn icon="mdi-close" variant="text" @click="joinDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text>
          <v-alert v-if="candidateServers.length === 0" type="info" variant="tonal" class="mb-4">
            All SSH servers are already managed, pending, or unavailable as join candidates.
          </v-alert>
          <v-select
            v-model="selectedServerId"
            :items="candidateOptions"
            item-title="label"
            item-value="value"
            label="SSH Server"
            variant="outlined"
            density="comfortable"
            class="mb-4"
          />
          <div v-if="selectedServer" class="server-preview">
            <div class="font-weight-bold">{{ selectedServer.name }}</div>
            <div class="text-caption text-medium-emphasis">{{ selectedServer.host }}:{{ selectedServer.port }}</div>
          </div>
        </v-card-text>
        <v-divider />
        <v-card-actions class="justify-end">
          <v-btn variant="text" class="text-none" @click="joinDialog = false">Cancel</v-btn>
          <v-btn color="primary" variant="flat" class="text-none" :loading="joining" :disabled="!selectedServerId" @click="joinSelectedServer">Join Node</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>

<style scoped>
.page-heading { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; }
.page-actions { display: flex; gap: 10px; align-items: center; }
.eyebrow { margin-bottom: 4px; color: rgb(var(--v-theme-primary)); font-size: 0.72rem; font-weight: 700; letter-spacing: 0; text-transform: uppercase; }
.summary-strip { display: grid; grid-template-columns: repeat(4, minmax(0, 180px)); gap: 12px; }
.summary-card { padding: 14px; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 0.8rem; }
.join-dialog { border-radius: 8px; }
.server-preview { border-radius: 8px; padding: 12px; background: rgba(var(--v-theme-primary), 0.06); }
@media (max-width: 900px) {
  .page-heading { flex-direction: column; }
  .summary-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
</style>
