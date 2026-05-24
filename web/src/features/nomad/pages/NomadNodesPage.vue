<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { nomadApi } from '@/api/nomad';
import type { NomadNodeDto, NomadStatusDto } from '@/types/api';

const status = ref<NomadStatusDto | null>(null);
const nodes = ref<NomadNodeDto[]>([]);
const loading = ref(false);
const error = ref('');

const readyCount = computed(() => nodes.value.filter((node) => node.Status === 'ready').length);

function statusColor(nodeStatus?: string) {
  if (nodeStatus === 'ready') return 'success';
  if (nodeStatus === 'down') return 'error';
  return 'warning';
}

async function load() {
  loading.value = true;
  try {
    const [statusResult, nodeResult] = await Promise.all([nomadApi.status(), nomadApi.nodes()]);
    status.value = statusResult;
    nodes.value = nodeResult;
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to load Nomad nodes';
  } finally {
    loading.value = false;
  }
}

onMounted(load);
</script>

<template>
  <div>
    <div class="page-heading mb-5">
      <div><div class="eyebrow">Nomad runtime</div><h1 class="text-h4 font-weight-bold">Nomad Nodes</h1></div>
      <v-btn prepend-icon="mdi-refresh" color="primary" variant="flat" :loading="loading" class="text-none" @click="load">Refresh</v-btn>
    </div>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>
    <v-alert :type="status?.connected ? 'success' : 'warning'" variant="tonal" class="mb-4">
      {{ status?.connected ? `Connected to leader ${status.leader || 'unknown'}` : 'Nomad connection not verified' }}
    </v-alert>
    <div class="summary-strip mb-4">
      <v-card variant="outlined" class="summary-card"><div class="text-caption text-medium-emphasis">Nodes</div><div class="text-h5 font-weight-bold font-tabular">{{ nodes.length }}</div></v-card>
      <v-card variant="outlined" class="summary-card"><div class="text-caption text-medium-emphasis">Ready</div><div class="text-h5 font-weight-bold font-tabular">{{ readyCount }}</div></v-card>
    </div>
    <v-card variant="outlined" :loading="loading">
      <v-table>
        <thead><tr><th>Name</th><th>ID</th><th>Datacenter</th><th>Status</th><th>Scheduling</th></tr></thead>
        <tbody>
          <tr v-for="node in nodes" :key="node.ID">
            <td class="font-weight-bold">{{ node.Name || '-' }}</td>
            <td class="mono">{{ node.ID }}</td>
            <td>{{ node.Datacenter || '-' }}</td>
            <td><v-chip :color="statusColor(node.Status)" size="small" variant="tonal" label>{{ node.Status || 'unknown' }}</v-chip></td>
            <td>{{ node.SchedulingEligibility || node.Eligibility || '-' }}</td>
          </tr>
          <tr v-if="nodes.length === 0"><td colspan="5" class="text-center py-8 text-medium-emphasis">No nodes</td></tr>
        </tbody>
      </v-table>
    </v-card>
  </div>
</template>

<style scoped>
.page-heading { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; }
.eyebrow { margin-bottom: 4px; color: rgb(var(--v-theme-primary)); font-size: 0.72rem; font-weight: 700; letter-spacing: 0; text-transform: uppercase; }
.summary-strip { display: grid; grid-template-columns: repeat(2, minmax(0, 180px)); gap: 12px; }
.summary-card { padding: 14px; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 0.8rem; }
</style>
