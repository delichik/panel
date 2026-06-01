<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { nomadApi } from '@/api/nomad';
import type { NomadDeploymentDto, NomadEvaluationDto, NomadServiceRegistrationDto } from '@/types/api';

const deployments = ref<NomadDeploymentDto[]>([]);
const evaluations = ref<NomadEvaluationDto[]>([]);
const services = ref<NomadServiceRegistrationDto[]>([]);
const loading = ref(false);
const error = ref('');

async function load() {
  loading.value = true;
  try {
    [deployments.value, evaluations.value, services.value] = await Promise.all([
      nomadApi.deployments(),
      nomadApi.evaluations(),
      nomadApi.services(),
    ]);
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to load deployments';
  } finally {
    loading.value = false;
  }
}

onMounted(load);
</script>

<template>
  <div>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>
    <div class="deployments-grid">
      <v-card variant="outlined" :loading="loading">
        <v-card-title>Deployments</v-card-title>
        <v-table><thead><tr><th>ID</th><th>Job</th><th>Status</th><th>Description</th></tr></thead><tbody>
          <tr v-for="deployment in deployments" :key="deployment.ID"><td class="mono">{{ deployment.ID }}</td><td>{{ deployment.JobID }}</td><td><v-chip size="small" variant="tonal" label>{{ deployment.Status }}</v-chip></td><td>{{ deployment.StatusDescription || '-' }}</td></tr>
          <tr v-if="deployments.length === 0"><td colspan="4" class="text-center py-6 text-medium-emphasis">No deployments</td></tr>
        </tbody></v-table>
      </v-card>
      <v-card variant="outlined" :loading="loading">
        <v-card-title>Evaluations</v-card-title>
        <v-table><thead><tr><th>ID</th><th>Job</th><th>Type</th><th>Status</th></tr></thead><tbody>
          <tr v-for="evaluation in evaluations" :key="evaluation.ID"><td class="mono">{{ evaluation.ID }}</td><td>{{ evaluation.JobID }}</td><td>{{ evaluation.Type }}</td><td>{{ evaluation.Status }}</td></tr>
          <tr v-if="evaluations.length === 0"><td colspan="4" class="text-center py-6 text-medium-emphasis">No evaluations</td></tr>
        </tbody></v-table>
      </v-card>
      <v-card variant="outlined" :loading="loading">
        <v-card-title>Services</v-card-title>
        <v-table><thead><tr><th>Name</th><th>Namespace</th><th>Tags</th></tr></thead><tbody>
          <tr v-for="service in services" :key="`${service.Namespace}:${service.ServiceName}:${service.ID}`"><td class="font-weight-bold">{{ service.ServiceName }}</td><td>{{ service.Namespace }}</td><td>{{ service.Tags?.join(', ') || '-' }}</td></tr>
          <tr v-if="services.length === 0"><td colspan="3" class="text-center py-6 text-medium-emphasis">No services</td></tr>
        </tbody></v-table>
      </v-card>
    </div>
  </div>
</template>

<style scoped>
.deployments-grid { display: grid; gap: 16px; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 0.8rem; }
</style>
