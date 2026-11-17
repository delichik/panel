<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { Radar, RefreshCcw } from '@lucide/vue';
import { serversApi } from '@/api/servers';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import Table from '@/components/ui/Table.vue';
import DataRefreshControl from '@/components/patterns/DataRefreshControl.vue';
import { useAutoRefresh } from '@/composables/useAutoRefresh';
import { useErrorToast } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { NetworkProbeResult, NetworkProbeServerResults } from '@/types/networkProbe';
import type { ServerDto } from '@/types/servers';
import { formatDateTime } from '@/utils/datetime';
import { createLatestRequestGuard, isAbortError } from '@/views/_shared/requestState';
import { probeOutcome, probeOutcomeStatusKey, probeOutcomeTone } from '../model';

const props = defineProps<{ server: ServerDto; active?: boolean }>();
const { t } = useI18n();
const notifyError = useErrorToast();
const router = useRouter();
const autoRefresh = useAutoRefresh();

const data = ref<NetworkProbeServerResults | null>(null);
const loading = ref(false);
const error = ref('');
const guard = createLatestRequestGuard();
let controller: AbortController | null = null;

const columns = computed(() => [
  { key: 'target', label: t('serversPage.probe.target') },
  { key: 'address', label: t('serversPage.probe.address') },
  { key: 'latency', label: t('serversPage.probe.latency'), nowrap: true },
  { key: 'packetLoss', label: t('serversPage.probe.packetLoss'), nowrap: true },
  { key: 'sent', label: t('serversPage.probe.sent'), nowrap: true },
  { key: 'received', label: t('serversPage.probe.received'), nowrap: true },
  { key: 'sampledAt', label: t('serversPage.probe.sampledAt'), nowrap: true },
  { key: 'status', label: t('common.status'), nowrap: true },
]);

/** 非探测节点返回 enabled=false：空态引导到设施配置，而不是伪造健康结果。 */
const isProbeNode = computed(() => Boolean(data.value?.enabled && (data.value.nodes.length || data.value.results.length)));
const rows = computed<NetworkProbeResult[]>(() => data.value?.results ?? []);

function targetName(row: NetworkProbeResult) {
  return data.value?.nodes.find((node) => node.serverId === row.targetServerId)?.name || row.targetAddress || row.targetServerId;
}

function latencyText(row: NetworkProbeResult) {
  if (probeOutcome(row) === 'failed') return t('common.notAvailable');
  return `${row.latencyMs.toFixed(1)} ms`;
}

function lossText(row: NetworkProbeResult) {
  return `${row.packetLossPercent.toFixed(0)}%`;
}

async function load() {
  controller?.abort();
  const requestId = guard.begin();
  const next = new AbortController();
  controller = next;
  loading.value = true;
  error.value = '';
  try {
    const result = await serversApi.networkProbes(props.server.id, { signal: next.signal });
    if (!guard.isCurrent(requestId)) return;
    data.value = result;
  } catch (err) {
    if (isAbortError(err)) return;
    if (!guard.isCurrent(requestId)) return;
    error.value = err instanceof Error ? err.message : t('serversPage.probe.loadFailed');
    notifyError(error.value, err);
  } finally {
    if (guard.isCurrent(requestId)) loading.value = false;
  }
}

function openFacility() {
  void router.push('/applications/facility-apps/network-probe');
}

onMounted(() => {
  void load();
  // 探测页签复用全局自动刷新档位；非当前 tab / 加载中时跳过这一拍。
  autoRefresh.start(() => {
    if (props.active === false || loading.value) return;
    void load();
  });
});
</script>

<template>
  <div class="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-4">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <span class="text-xs text-muted-foreground">
        {{ t('serversPage.probe.lastSample') }}: {{ formatDateTime(data?.updatedAt, t('common.never')) }}
      </span>
      <div class="flex flex-wrap items-center gap-2">
        <DataRefreshControl />
        <Button size="sm" variant="ghost" :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
      </div>
    </div>

    <EmptyState v-if="error && !data" variant="page" :title="t('common.loadFailed')" :description="error">
      <template #actions>
        <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button>
      </template>
    </EmptyState>

    <EmptyState v-else-if="data && !isProbeNode" variant="page" :title="t('serversPage.probe.notProbeNode')" :description="t('serversPage.probe.notProbeNodeHint')">
      <template #actions>
        <Button size="sm" variant="primary" @click="openFacility"><Radar />{{ t('serversPage.probe.openFacility') }}</Button>
      </template>
    </EmptyState>

    <Table
      v-else
      :columns="columns"
      :rows="rows"
      row-key="targetServerId"
      :loading="loading && !data"
      :loading-label="t('common.loading')"
    >
      <template #target="{ row }">
        <strong class="block max-w-[16rem] truncate text-foreground">{{ targetName(row) }}</strong>
      </template>
      <template #address="{ row }">
        <span class="panel-mono text-xs text-muted-foreground">{{ row.targetAddress || t('common.notAvailable') }}</span>
      </template>
      <template #latency="{ row }">
        <span class="tabular-nums">{{ latencyText(row) }}</span>
      </template>
      <template #packetLoss="{ row }">
        <span class="tabular-nums">{{ lossText(row) }}</span>
      </template>
      <template #sent="{ row }">
        <span class="tabular-nums text-muted-foreground">{{ row.sent }}</span>
      </template>
      <template #received="{ row }">
        <span class="tabular-nums text-muted-foreground">{{ row.received }}</span>
      </template>
      <template #sampledAt="{ row }">
        <span class="text-muted-foreground">{{ formatDateTime(row.sampledAt, t('common.never')) }}</span>
      </template>
      <template #status="{ row }">
        <Badge :tone="probeOutcomeTone(probeOutcome(row))">{{ t(probeOutcomeStatusKey(probeOutcome(row))) }}</Badge>
      </template>
      <template #empty>
        <EmptyState variant="inset" :title="t('serversPage.probe.noResults')" :description="t('serversPage.probe.noResultsHint')" />
      </template>
    </Table>
  </div>
</template>
