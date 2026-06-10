<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { nomadApi } from '@/api/nomad';
import { useI18n } from '@/i18n';
import type { NomadJobDto } from '@/types/api';

const { t } = useI18n();
const jobs = ref<NomadJobDto[]>([]);
const prefix = ref('');
const loading = ref(false);
const error = ref('');
const filteredJobs = computed(() => jobs.value.filter((job) => !prefix.value || (job.ID || '').includes(prefix.value)));

async function load() {
  loading.value = true;
  try {
    jobs.value = await nomadApi.jobs();
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('nomadJobsPage.loadFailed');
  } finally {
    loading.value = false;
  }
}

onMounted(load);
</script>

<template>
  <div class="page-shell">
    <div class="actions mb-4"><v-text-field v-model="prefix" :label="t('nomadJobsPage.filterPrefix')" density="compact" variant="outlined" hide-details /></div>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>
    <v-card variant="outlined" :loading="loading">
      <v-table>
        <thead><tr><th>{{ t('nomadJobsPage.jobId') }}</th><th>{{ t('common.name') }}</th><th>{{ t('common.type') }}</th><th>{{ t('common.status') }}</th><th>{{ t('applicationsPage.namespace') }}</th><th>{{ t('nomadJobsPage.datacenters') }}</th></tr></thead>
        <tbody>
          <tr v-for="job in filteredJobs" :key="job.ID">
            <td class="mono">{{ job.ID }}</td><td class="font-weight-bold">{{ job.Name }}</td><td>{{ job.Type }}</td><td><v-chip size="small" variant="tonal" label>{{ job.Status }}</v-chip></td><td>{{ job.Namespace }}</td><td>{{ job.Datacenters?.join(', ') || '-' }}</td>
          </tr>
          <tr v-if="filteredJobs.length === 0"><td colspan="6" class="text-center py-8 text-medium-emphasis">{{ t('nomadJobsPage.noJobs') }}</td></tr>
        </tbody>
      </v-table>
    </v-card>
  </div>
</template>

<style scoped>
.actions { display: flex; gap: 10px; align-items: center; justify-content: flex-end; }
.actions :deep(.v-input) { width: 220px; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 0.8rem; }
</style>
