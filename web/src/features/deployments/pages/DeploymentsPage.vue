<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { nomadApi } from '@/api/nomad';
import { translateNomadRuntimeStatus, useI18n } from '@/i18n';
import type { NomadDeploymentDto, NomadEvaluationDto, NomadServiceRegistrationDto } from '@/types/api';

const { t } = useI18n();
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
    error.value = err instanceof Error ? err.message : t('deploymentsPage.loadFailed');
  } finally {
    loading.value = false;
  }
}

onMounted(load);
</script>

<template>
  <div class="page-shell">
    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>
    <div class="deployments-grid">
      <v-card variant="outlined" :loading="loading">
        <v-card-title>{{ t('deploymentsPage.deployments') }}</v-card-title>
        <v-table><thead><tr><th>{{ t('deploymentsPage.id') }}</th><th>{{ t('deploymentsPage.job') }}</th><th>{{ t('common.status') }}</th><th>{{ t('deploymentsPage.description') }}</th></tr></thead><tbody>
          <tr v-for="deployment in deployments" :key="deployment.ID"><td class="mono">{{ deployment.ID }}</td><td>{{ deployment.JobID }}</td><td><v-chip size="small" variant="tonal" label>{{ translateNomadRuntimeStatus(deployment.Status) }}</v-chip></td><td>{{ deployment.StatusDescription || '-' }}</td></tr>
          <tr v-if="deployments.length === 0"><td colspan="4" class="text-center py-6 text-medium-emphasis">{{ t('deploymentsPage.noDeployments') }}</td></tr>
        </tbody></v-table>
      </v-card>
      <v-card variant="outlined" :loading="loading">
        <v-card-title>{{ t('deploymentsPage.evaluations') }}</v-card-title>
        <v-table><thead><tr><th>{{ t('deploymentsPage.id') }}</th><th>{{ t('deploymentsPage.job') }}</th><th>{{ t('common.type') }}</th><th>{{ t('common.status') }}</th></tr></thead><tbody>
          <tr v-for="evaluation in evaluations" :key="evaluation.ID"><td class="mono">{{ evaluation.ID }}</td><td>{{ evaluation.JobID }}</td><td>{{ evaluation.Type }}</td><td>{{ translateNomadRuntimeStatus(evaluation.Status) }}</td></tr>
          <tr v-if="evaluations.length === 0"><td colspan="4" class="text-center py-6 text-medium-emphasis">{{ t('deploymentsPage.noEvaluations') }}</td></tr>
        </tbody></v-table>
      </v-card>
      <v-card variant="outlined" :loading="loading">
        <v-card-title>{{ t('deploymentsPage.services') }}</v-card-title>
        <v-table><thead><tr><th>{{ t('common.name') }}</th><th>{{ t('applicationsPage.namespace') }}</th><th>{{ t('deploymentsPage.tags') }}</th></tr></thead><tbody>
          <tr v-for="service in services" :key="`${service.Namespace}:${service.ServiceName}:${service.ID}`"><td class="font-weight-bold">{{ service.ServiceName }}</td><td>{{ service.Namespace }}</td><td>{{ service.Tags?.join(', ') || '-' }}</td></tr>
          <tr v-if="services.length === 0"><td colspan="3" class="text-center py-6 text-medium-emphasis">{{ t('deploymentsPage.noServices') }}</td></tr>
        </tbody></v-table>
      </v-card>
    </div>
  </div>
</template>

<style scoped>
.deployments-grid { display: grid; gap: 16px; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 0.8rem; }
</style>
