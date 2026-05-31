<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { useI18n } from '@/i18n';
import { nomadApi } from '@/api/nomad';
import TaskLogPanel from '@/components/tasks/TaskLogPanel.vue';
import type { NomadControlPlaneDto } from '@/types/api';

const router = useRouter();
const controlPlane = ref<NomadControlPlaneDto | null>(null);
const loading = ref(false);
const bootstrapping = ref(false);
const error = ref('');
const selectedServerId = ref('');
const activeTaskId = ref('');
const activeTaskServerName = ref('');
const { t } = useI18n();

const bootstrapCandidates = computed(() => controlPlane.value?.bootstrapCandidates ?? []);
const selectedServer = computed(() => bootstrapCandidates.value.find((server) => server.id === selectedServerId.value) ?? null);
const candidateOptions = computed(() =>
  bootstrapCandidates.value.map((server) => ({
    label: `${server.name} (${server.host}:${server.port})`,
    value: server.id,
  })),
);
const pendingBootstrap = computed(() => controlPlane.value?.nodes.find((node) => node.status === 'bootstrapping' && node.taskId) ?? null);

async function load() {
  loading.value = true;
  try {
    const result = await nomadApi.controlPlane();
    controlPlane.value = result;
    selectedServerId.value ||= result.bootstrapCandidates[0]?.id ?? '';
    activeTaskId.value ||= pendingBootstrap.value?.taskId ?? '';
    activeTaskServerName.value ||= pendingBootstrap.value?.name ?? '';
    error.value = '';
    if (result.status !== 'unconfigured') {
      await router.replace('/nomad/nodes');
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('nomadSetupPage.loadFailed');
  } finally {
    loading.value = false;
  }
}

async function bootstrapSelectedServer() {
  if (!selectedServerId.value) return;
  bootstrapping.value = true;
  try {
    const server = selectedServer.value;
    const result = await nomadApi.bootstrapServer(selectedServerId.value);
    activeTaskId.value = result.taskId;
    activeTaskServerName.value = server?.name ?? '';
    error.value = '';
    await router.replace('/nomad/nodes');
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('nomadSetupPage.bootstrapFailed');
  } finally {
    bootstrapping.value = false;
  }
}

onMounted(load);
</script>

<template>
  <div>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>

    <v-card variant="outlined" class="setup-panel">
      <div class="setup-icon"><v-icon size="30">mdi-server-plus</v-icon></div>
      <div class="setup-copy">
        <div class="text-h6 font-weight-bold">{{ t('nomadSetupPage.selectServerTitle') }}</div>
        <div class="text-body-2 text-medium-emphasis">
          {{ t('nomadSetupPage.selectServerHint') }}
        </div>
      </div>

      <v-alert v-if="bootstrapCandidates.length === 0" type="info" variant="tonal">
        {{ t('nomadSetupPage.noServersHint') }}
      </v-alert>
      <div v-else class="setup-form">
        <v-select
          v-model="selectedServerId"
          :items="candidateOptions"
          item-title="label"
          item-value="value"
          :label="t('nomadSetupPage.sshServer')"
          variant="outlined"
          density="comfortable"
        />
        <v-btn color="primary" variant="flat" prepend-icon="mdi-server-plus" class="text-none" :loading="bootstrapping" :disabled="!selectedServerId" @click="bootstrapSelectedServer">
          {{ t('nomadSetupPage.bootstrapFirstServer') }}
        </v-btn>
      </div>

      <v-btn v-if="bootstrapCandidates.length === 0" to="/servers" color="primary" variant="flat" prepend-icon="mdi-plus" class="text-none add-server-btn">
        {{ t('nomadSetupPage.addSshServer') }}
      </v-btn>
    </v-card>

    <v-card v-if="activeTaskId" class="mt-4 pa-4" variant="outlined">
      <v-card-title class="px-0 pt-0 text-subtitle-1 font-weight-bold">{{ t('nomadSetupPage.bootstrapTask') }}</v-card-title>
      <TaskLogPanel :task-id="activeTaskId" :server-name="activeTaskServerName" compact />
    </v-card>
  </div>
</template>

<style scoped>
.setup-panel { display: grid; grid-template-columns: auto 1fr; gap: 16px; align-items: start; padding: 18px; max-width: 860px; }
.setup-icon { display: grid; place-items: center; width: 50px; height: 50px; border-radius: 8px; color: rgb(var(--v-theme-primary)); background: rgba(var(--v-theme-primary), 0.08); }
.setup-copy { min-width: 0; text-wrap: pretty; }
.setup-form { grid-column: 2; display: grid; grid-template-columns: minmax(260px, 1fr) auto; gap: 12px; align-items: start; }
.add-server-btn { grid-column: 2; justify-self: start; }
@media (max-width: 780px) {
  .setup-panel { grid-template-columns: 1fr; }
  .setup-form { grid-column: 1; grid-template-columns: 1fr; }
  .add-server-btn { grid-column: 1; }
}
</style>
