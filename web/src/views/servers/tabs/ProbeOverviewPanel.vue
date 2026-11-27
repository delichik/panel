<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { RefreshCcw } from '@/icons';
import { networkProbeFacilityApi } from '@/api/networkProbe';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import Skeleton from '@/components/ui/Skeleton.vue';
import ChartCard from '@/components/patterns/ChartCard.vue';
import DataRefreshControl from '@/components/patterns/DataRefreshControl.vue';
import ProbeHistoryCharts from '@/components/patterns/ProbeHistoryCharts.vue';
import { useAutoRefresh } from '@/composables/useAutoRefresh';
import { useI18n } from '@/i18n';
import type { NetworkProbeTargetMatrix, NetworkProbeThresholds } from '@/types/networkProbe';
import type { ServerDto } from '@/types/servers';
import { formatDateTime } from '@/utils/datetime';
import { probeHistorySeries, probeAggregateOutcomeStatusKey, probeAggregateOutcomeTone, probeTargetOutcome } from '@/utils/networkProbe';
import { createLatestRequestGuard, isAbortError } from '@/views/_shared/requestState';
import { summarizeProbeOverview, type ProbeOverviewSummary } from './probeSummary';

const props = defineProps<{ server: ServerDto; active?: boolean }>();
const emit = defineEmits<{ summary: [value: ProbeOverviewSummary | null] }>();
const { t } = useI18n();
const autoRefresh = useAutoRefresh();

const matrix = ref<NetworkProbeTargetMatrix | null>(null);
const probeThresholds = ref<NetworkProbeThresholds>({});
const loading = ref(false);
const error = ref('');
const guard = createLatestRequestGuard();
let controller: AbortController | null = null;
let inFlight = false;

/** 当前服务器在所有探测源聚合结果中的最新摘要；矩阵里没有该服务器时为 null。 */
const target = computed(() => matrix.value?.nodes.find((node) => node.serverId === props.server.id) ?? null);
const outcome = computed(() => (target.value ? probeTargetOutcome(target.value) : 'noData'));
const latencySeries = computed(() => probeHistorySeries(target.value?.history, 'latency', 'latency', t('serversPage.probe.latency')));
const lossSeries = computed(() => probeHistorySeries(target.value?.history, 'packetLoss', 'loss', t('serversPage.probe.packetLoss')));
const hasResult = computed(() => Boolean(target.value && target.value.sourceCount > 0));

async function loadThresholds() {
  try {
    const config = await networkProbeFacilityApi.config();
    probeThresholds.value = config.thresholds ?? {};
  } catch {
    // 阈值是展示增强：读取失败时不画阈值线，不影响探测结果加载。
    probeThresholds.value = {};
  }
}

async function load() {
  if (inFlight) return;
  inFlight = true;
  const requestId = guard.begin();
  controller?.abort();
  const next = new AbortController();
  controller = next;
  loading.value = true;
  error.value = '';
  try {
    const nextMatrix = await networkProbeFacilityApi.targets({ signal: next.signal }, true);
    if (!guard.isCurrent(requestId)) return;
    matrix.value = nextMatrix;
    emit('summary', summarizeProbeOverview(target.value, t));
  } catch (err) {
    if (isAbortError(err) || !guard.isCurrent(requestId)) return;
    // 失败保留上次成功数据；首次加载失败时显示明确错误空态。
    error.value = err instanceof Error ? err.message : t('serversPage.probe.loadFailed');
  } finally {
    if (guard.isCurrent(requestId)) {
      loading.value = false;
      inFlight = false;
    }
  }
}

watch(() => props.server.id, () => {
  matrix.value = null;
  emit('summary', null);
  void load();
});

onMounted(() => {
  void load();
  void loadThresholds();
  autoRefresh.start(() => {
    if (props.active === false || inFlight) return;
    void load();
  });
});
onBeforeUnmount(() => {
  guard.invalidate();
  controller?.abort();
});
</script>

<template>
  <section class="grid min-w-0 gap-3" data-testid="server-probe-overview">
    <div class="flex flex-wrap items-start justify-between gap-2">
      <div class="min-w-0">
        <h3 class="m-0 text-body-strong font-semibold text-foreground">{{ t('serversPage.probe.historyTitle') }}</h3>
        <p class="m-0 mt-1 text-caption leading-5 text-muted-foreground">{{ t('serversPage.probe.sourceHint') }}</p>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <DataRefreshControl />
        <Button size="sm" variant="ghost" :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
      </div>
    </div>

    <template v-if="loading && !matrix">
      <div class="flex flex-wrap items-center justify-between gap-2 text-caption text-muted-foreground" aria-busy="true">
        <span class="inline-flex flex-wrap items-center gap-2">
          <Skeleton class="h-5 w-16 rounded-pill" />
          <span>
            {{ t('serversPage.probe.lastSample') }}:
            <Skeleton class="inline-block h-3.5 w-28 align-middle" />
          </span>
        </span>
      </div>
      <div class="grid min-w-0 gap-3 xl:grid-cols-2" aria-busy="true">
        <ChartCard :title="t('serversPage.probe.latencyHistory')" loading>
          <Skeleton class="h-44 w-full sm:h-48" />
          <template #footer>
            <Skeleton class="h-4 w-48 max-w-full" />
          </template>
        </ChartCard>
        <ChartCard :title="t('serversPage.probe.packetLossHistory')" loading>
          <Skeleton class="h-44 w-full sm:h-48" />
          <template #footer>
            <Skeleton class="h-4 w-48 max-w-full" />
          </template>
        </ChartCard>
      </div>
    </template>
    <EmptyState v-else-if="error && !matrix" variant="page" :title="t('common.loadFailed')" :description="error">
      <template #actions>
        <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button>
      </template>
    </EmptyState>
    <template v-else>
      <div class="flex flex-wrap items-center justify-between gap-2 text-caption text-muted-foreground">
        <span class="inline-flex flex-wrap items-center gap-2">
          <Badge :tone="probeAggregateOutcomeTone(outcome)">{{ t(probeAggregateOutcomeStatusKey(outcome)) }}</Badge>
          <span>{{ t('serversPage.probe.lastSample') }}: {{ formatDateTime(target?.sampledAt, t('common.never')) }}</span>
          <span v-if="!hasResult">{{ t('serversPage.probe.resultUnavailable') }}</span>
        </span>
        <span v-if="error" class="text-warning">{{ t('applicationsPage.networkProbeShowingStale') }}</span>
      </div>
      <!-- 无结果/无历史时两张图也保留卡片与空态，不隐藏。 -->
      <ProbeHistoryCharts
        :latency="latencySeries"
        :loss="lossSeries"
        :latency-current="target?.latencyMs ?? null"
        :loss-current="target?.packetLossPercent ?? null"
        :thresholds="probeThresholds"
      />
    </template>
  </section>
</template>
