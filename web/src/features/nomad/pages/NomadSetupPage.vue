<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useRouter } from 'vue-router';
import { useI18n } from '@/i18n';
import { nomadApi } from '@/api/nomad';
import type { NomadControlPlaneDto } from '@/types/api';
import PageLoadingState from '@/components/PageLoadingState.vue';
import { buildNomadAddressOptions, type NomadAddressOption } from '@/features/nomad/addressOptions';

const router = useRouter();
const controlPlane = ref<NomadControlPlaneDto | null>(null);
const loading = ref(false);
const bootstrapping = ref(false);
const error = ref('');
const selectedServerId = ref('');
const selectedAdvertiseAddress = ref('');
const { t } = useI18n();

const bootstrapCandidates = computed(() => controlPlane.value?.bootstrapCandidates ?? []);
const candidateOptions = computed(() =>
  bootstrapCandidates.value.map((server) => ({
    label: `${server.name} (${server.host}:${server.port})`,
    value: server.id,
  })),
);
const selectedServer = computed(() => bootstrapCandidates.value.find((server) => server.id === selectedServerId.value) ?? null);
const addressOptions = computed(() => {
  return buildNomadAddressOptions(selectedServer.value).map((option) => ({
    ...option,
    label: nomadAddressOptionLabel(option),
  }));
});
const migrationRequired = computed(() => controlPlane.value?.status === 'migration_required');

watch(selectedServerId, () => {
  selectedAdvertiseAddress.value = addressOptions.value[0]?.value ?? '';
});

function nomadAddressOptionLabel(option: NomadAddressOption) {
  if (option.source === 'current') {
    return t('nomadSetupPage.nomadAddressSourceCurrent', { address: option.value });
  }
  if (option.source === 'ssh') {
    return t('nomadSetupPage.nomadAddressSourceSsh', { address: option.value });
  }
  return t('nomadSetupPage.nomadAddressSourceInterface', { name: option.name || '-', address: option.value });
}

async function load() {
  loading.value = true;
  try {
    const result = await nomadApi.controlPlane();
    const bootstrapCandidates = result.bootstrapCandidates ?? [];
    controlPlane.value = result;
    selectedServerId.value ||= bootstrapCandidates[0]?.id ?? '';
    selectedAdvertiseAddress.value ||= addressOptions.value[0]?.value ?? '';
    error.value = '';
    if (!['unconfigured', 'migration_required'].includes(result.status)) {
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
    const input = { serverId: selectedServerId.value, advertiseAddress: selectedAdvertiseAddress.value };
    const result = migrationRequired.value
      ? await nomadApi.rebuildCluster(input)
      : await nomadApi.bootstrapServer(input);
    error.value = '';
    await router.replace({ path: '/nomad/nodes', query: { task: result.taskId } });
  } catch (err) {
    error.value = err instanceof Error
      ? err.message
      : (migrationRequired.value ? t('nomadSetupPage.rebuildFailed') : t('nomadSetupPage.bootstrapFailed'));
  } finally {
    bootstrapping.value = false;
  }
}

onMounted(load);
</script>

<template>
  <div class="page-shell">
    <v-alert v-if="error" type="error" variant="tonal">{{ error }}</v-alert>

    <PageLoadingState v-if="loading && !controlPlane" min-height="320px" />

    <v-card v-else variant="outlined" class="setup-panel">
      <div class="setup-icon"><v-icon size="30">mdi-server-plus</v-icon></div>
      <div class="setup-copy">
        <div class="text-h6 font-weight-bold">{{ migrationRequired ? t('nomadSetupPage.migrationTitle') : t('nomadSetupPage.selectServerTitle') }}</div>
        <div class="text-body-2 text-medium-emphasis">
          {{ migrationRequired ? t('nomadSetupPage.migrationHint') : t('nomadSetupPage.selectServerHint') }}
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
        <v-text-field
          :model-value="selectedServer?.host || ''"
          :label="t('nomadSetupPage.sshAddress')"
          variant="outlined"
          density="comfortable"
          readonly
        />
        <v-select
          v-model="selectedAdvertiseAddress"
          :items="addressOptions"
          item-title="label"
          item-value="value"
          :label="t('nomadSetupPage.nomadAddress')"
          :hint="t('nomadSetupPage.nomadAddressHint')"
          persistent-hint
          variant="outlined"
          density="comfortable"
        />
        <v-alert v-if="selectedServer && addressOptions.length === 0" type="warning" variant="tonal">
          {{ t('nomadSetupPage.noNetworkAddresses') }}
        </v-alert>
        <v-btn color="primary" variant="flat" prepend-icon="mdi-server-plus" class="text-none" :loading="bootstrapping" :disabled="!selectedServerId || !selectedAdvertiseAddress" @click="bootstrapSelectedServer">
          {{ migrationRequired ? t('nomadSetupPage.rebuildCluster') : t('nomadSetupPage.bootstrapFirstServer') }}
        </v-btn>
      </div>

      <v-btn v-if="bootstrapCandidates.length === 0" to="/servers" color="primary" variant="flat" prepend-icon="mdi-plus" class="text-none add-server-btn">
        {{ t('nomadSetupPage.addSshServer') }}
      </v-btn>
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
