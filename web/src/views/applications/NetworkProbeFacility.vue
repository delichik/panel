<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { RefreshCcw, Save } from '@lucide/vue';
import { networkProbeFacilityApi } from '@/api/networkProbe';
import Button from '@/components/ui/Button.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import LoadingOverlay from '@/components/ui/LoadingOverlay.vue';
import Select from '@/components/ui/Select.vue';
import ServerMultiPicker from '@/components/patterns/ServerMultiPicker.vue';
import Table from '@/components/ui/Table.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { NetworkProbeConfig, NetworkProbeMatrix, NetworkProbeMode, NetworkProbeResult } from '@/types/networkProbe';
import type { ServerDto } from '@/types/servers';
import { formatDateTime } from '@/utils/datetime';
import { createLatestRequestGuard, isAbortError } from '@/views/_shared/requestState';

const props = defineProps<{
  mode: 'facilityDetail' | 'facilityConfig';
  servers: ServerDto[];
}>();

const { t } = useI18n();
const notifyError = useErrorToast();
const notifySuccess = useSuccessToast();

const config = ref<NetworkProbeConfig | null>(null);
const matrix = ref<NetworkProbeMatrix | null>(null);
const loading = ref(false);
const saving = ref(false);
const error = ref('');
const formError = ref('');
const draft = reactive<{ mode: NetworkProbeMode; serverIds: string[] }>({ mode: 'all', serverIds: [] });
const guard = createLatestRequestGuard();
let controller: AbortController | null = null;

const serverOptions = computed(() => props.servers.map((server) => ({
  id: server.id,
  name: server.name,
  description: [server.ipv4 || server.ipv6 || server.host].filter(Boolean).join(' / '),
  status: server.reachable ? 'reachable' : 'unreachable',
})));

const modeOptions = computed(() => [
  { value: 'all', label: t('applicationsPage.networkProbeModeAll') },
  { value: 'selected', label: t('applicationsPage.networkProbeModeSelected') },
]);

const dirty = computed(() => {
  if (!config.value) return true;
  if (draft.mode !== config.value.mode) return true;
  return JSON.stringify([...draft.serverIds].sort()) !== JSON.stringify([...config.value.serverIds].sort());
});

const sourceNodes = computed(() => matrix.value?.nodes ?? []);

/** 列是所有被探测目标：selected 模式下非探测节点也会作为目标出现。 */
const targetNodes = computed(() => {
  const ids = new Set((matrix.value?.results ?? []).map((result) => result.targetServerId));
  return props.servers
    .filter((server) => ids.has(server.id))
    .map((server) => ({ serverId: server.id, name: server.name }));
});

const resultsByPair = computed(() => {
  const map = new Map<string, NetworkProbeResult>();
  for (const result of matrix.value?.results ?? []) map.set(`${result.sourceServerId}:${result.targetServerId}`, result);
  return map;
});

const matrixColumns = computed(() => [
  { key: 'source', label: t('applicationsPage.networkProbeSource'), nowrap: true },
  ...targetNodes.value.map((node) => ({ key: node.serverId, label: node.name, nowrap: true })),
]);

const matrixRows = computed<Array<Record<string, string>>>(() => sourceNodes.value.map((source) => {
  const row: Record<string, string> = { id: source.serverId, source: source.name };
  for (const target of targetNodes.value) {
    row[target.serverId] = matrixCell(source.serverId, target.serverId);
  }
  return row;
}));

function matrixCell(sourceServerId: string, targetServerId: string) {
  if (sourceServerId === targetServerId) return '—';
  const result = resultsByPair.value.get(`${sourceServerId}:${targetServerId}`);
  if (!result) return '—';
  if (result.error) return t('applicationsPage.networkProbeFailed');
  const base = `${result.latencyMs.toFixed(1)} ms · ${result.packetLossPercent.toFixed(0)}%`;
  return result.stale ? `${base} · ${t('applicationsPage.networkProbeStale')}` : base;
}

function resetDraft() {
  draft.mode = config.value?.mode ?? 'all';
  draft.serverIds = [...(config.value?.serverIds ?? [])];
  formError.value = '';
}

async function load() {
  controller?.abort();
  const requestId = guard.begin();
  const next = new AbortController();
  controller = next;
  loading.value = true;
  error.value = '';
  try {
    const [nextConfig, nextMatrix] = await Promise.all([
      networkProbeFacilityApi.config({ signal: next.signal }),
      networkProbeFacilityApi.results({ signal: next.signal }),
    ]);
    if (!guard.isCurrent(requestId)) return;
    config.value = nextConfig;
    matrix.value = nextMatrix;
    resetDraft();
  } catch (err) {
    if (isAbortError(err)) return;
    if (!guard.isCurrent(requestId)) return;
    error.value = err instanceof Error ? err.message : t('applicationsPage.networkProbeLoadFailed');
    notifyError(error.value, err);
  } finally {
    if (guard.isCurrent(requestId)) loading.value = false;
  }
}

async function save() {
  if (saving.value) return;
  formError.value = '';
  if (draft.mode === 'selected' && draft.serverIds.length === 0) {
    formError.value = t('applicationsPage.networkProbeServersRequired');
    return;
  }
  saving.value = true;
  try {
    const saved = await networkProbeFacilityApi.saveConfig({ mode: draft.mode, serverIds: [...draft.serverIds] });
    config.value = saved;
    resetDraft();
    notifySuccess(t('applicationsPage.networkProbeSaved'), saved);
    await load();
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    saving.value = false;
  }
}

onMounted(() => { void load(); });
</script>

<template>
  <div class="grid gap-4">
    <section class="relative grid gap-3 rounded-xl border border-border bg-card p-4">
      <LoadingOverlay v-if="loading && !config" :label="t('applicationsPage.networkProbeLoading')" />
      <div>
        <h2 class="m-0 text-base font-semibold text-foreground">{{ t('applicationsPage.networkProbeConfigTitle') }}</h2>
        <p class="m-0 mt-1 text-sm leading-6 text-muted-foreground">{{ t('applicationsPage.networkProbeConfigHint') }}</p>
      </div>

      <EmptyState v-if="error && !config" variant="page" :title="t('common.loadFailed')" :description="error">
        <template #actions>
          <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button>
        </template>
      </EmptyState>

      <template v-else>
        <div class="grid gap-3 sm:grid-cols-2">
          <label class="grid gap-1">
            <span class="text-xs font-medium text-muted-foreground">{{ t('applicationsPage.networkProbeMode') }}</span>
            <Select v-model="draft.mode" :options="modeOptions" :disabled="saving" />
          </label>
        </div>
        <div v-if="draft.mode === 'selected'" class="grid gap-2">
          <div>
            <span class="text-xs font-medium text-muted-foreground">{{ t('applicationsPage.networkProbeSelectServers') }}</span>
            <p class="m-0 mt-1 text-xs leading-5 text-muted-foreground">{{ t('applicationsPage.networkProbeSelectServersHint') }}</p>
          </div>
          <ServerMultiPicker v-model="draft.serverIds" :servers="serverOptions" :label="t('applicationsPage.networkProbeSelectServers')" />
        </div>
        <p v-if="formError" class="m-0 text-sm text-danger">{{ formError }}</p>
        <div class="flex justify-end">
          <Button size="sm" variant="primary" :loading="saving" :disabled="!dirty" @click="save"><Save />{{ t('applicationsPage.networkProbeSave') }}</Button>
        </div>
      </template>
    </section>

    <section class="relative grid gap-3 rounded-xl border border-border bg-card p-4">
      <div class="flex flex-wrap items-start justify-between gap-2">
        <div class="min-w-0">
          <h2 class="m-0 text-base font-semibold text-foreground">{{ t('applicationsPage.networkProbeResultsTitle') }}</h2>
          <p class="m-0 mt-1 text-sm leading-6 text-muted-foreground">{{ t('applicationsPage.networkProbeResultsHint') }}</p>
          <p class="m-0 mt-1 text-xs text-muted-foreground">{{ t('applicationsPage.networkProbeUpdatedAt') }}: {{ formatDateTime(matrix?.updatedAt, t('common.never')) }}</p>
        </div>
        <Button size="sm" variant="ghost" :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
      </div>

      <LoadingOverlay v-if="loading && !matrix" :label="t('applicationsPage.networkProbeLoading')" />
      <EmptyState
        v-else-if="!sourceNodes.length || !targetNodes.length"
        variant="inset"
        :title="t('applicationsPage.networkProbeNoResults')"
        :description="t('applicationsPage.networkProbeNoResultsHint')"
      />
      <Table
        v-else
        class="max-h-[32rem]"
        :columns="matrixColumns"
        :rows="matrixRows"
        row-key="id"
      >
        <template #source="{ row }">
          <strong class="whitespace-nowrap text-foreground">{{ row.source }}</strong>
        </template>
      </Table>
    </section>
  </div>
</template>
