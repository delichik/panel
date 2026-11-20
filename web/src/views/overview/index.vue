<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { CheckCircle2, History, RefreshCcw, Server as ServerIcon, XCircle } from '@/icons';
import { applicationsApi } from '@/api/applications';
import { containersApi } from '@/api/containers';
import { metricsFacilityApi } from '@/api/metricsFacility';
import { networkProbeFacilityApi } from '@/api/networkProbe';
import { packagesApi } from '@/api/packages';
import { serversApi, type ServerMetricsSeries } from '@/api/servers';
import Button from '@/components/ui/Button.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import Select from '@/components/ui/Select.vue';
import Skeleton from '@/components/ui/Skeleton.vue';
import DataRefreshControl from '@/components/patterns/DataRefreshControl.vue';
import InfoBar from '@/components/patterns/InfoBar.vue';
import RatioRing from '@/components/patterns/RatioRing.vue';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import { useAutoRefresh } from '@/composables/useAutoRefresh';
import { useI18n } from '@/i18n';
import type { NetworkProbeConfig, NetworkProbeServerResults } from '@/types/networkProbe';
import type { MetricsFacilityConfig } from '@/types/metricsFacility';
import type { ApplicationSummaryDto } from '@/types/applications';
import type { ServerDto } from '@/types/servers';
import { isAbortError } from '@/views/_shared/requestState';
import ServerCard from './ServerCard.vue';
import StatChip from './StatChip.vue';
import {
  applicationRatio,
  buildServerCards,
  emptyApplicationSummary,
  formatBytes,
  formatPercent,
  onlineRateTone,
  percentTone,
  summarizeApplications,
  summarizeCapacity,
  summarizeNodeCards,
} from './model';

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const autoRefresh = useAutoRefresh();

const servers = ref<ServerDto[]>([]);
const metricsById = ref<Record<string, ServerMetricsSeries | undefined>>({});
const metricsErrorIds = ref<Set<string>>(new Set());
const packageUpdatesById = ref<Record<string, number | null>>({});
const imageUpdatesById = ref<Record<string, number | null>>({});
const probeConfig = ref<NetworkProbeConfig | null>(null);
const metricsConfig = ref<MetricsFacilityConfig | null>(null);
const warningThresholds = computed(() => ({
  cpu: metricsConfig.value?.thresholds.cpuUsagePercent ?? null,
  latency: probeConfig.value?.thresholds.latencyMs ?? null,
  packetLoss: probeConfig.value?.thresholds.packetLossPercent ?? null,
}));
const probeSourceId = ref('');
const probeResults = ref<NetworkProbeServerResults | null>(null);
const applications = ref<ApplicationSummaryDto[]>([]);
const applicationsTotal = ref(0);
const applicationsError = ref(false);
const pageLoading = ref(true);
const refreshing = ref(false);
const pageError = ref('');

let loadSeq = 0;
let probeSeq = 0;
let loadInFlight = false;
let controller: AbortController | null = null;

const candidateIds = computed(() => probeCandidateIds(probeConfig.value, servers.value));
const probeSourceOptions = computed(() => candidateIds.value.map((id) => {
  const server = servers.value.find((item) => item.id === id);
  return { value: id, label: server ? `${server.name} · ${server.host}` : id };
}));

const cards = computed(() => buildServerCards({
  servers: servers.value,
  metricsById: metricsById.value,
  metricsErrorIds: metricsErrorIds.value,
  probeSourceId: probeSourceId.value,
  probeResults: probeResults.value?.results ?? [],
  packageUpdatesById: packageUpdatesById.value,
  imageUpdatesById: imageUpdatesById.value,
}));
const nodeSummary = computed(() => summarizeNodeCards(cards.value));
const onlinePercent = computed(() => (nodeSummary.value.total ? Math.round((nodeSummary.value.online / nodeSummary.value.total) * 100) : 0));
const applicationSummary = computed(() => (
  applicationsError.value ? emptyApplicationSummary() : summarizeApplications(applications.value, applicationsTotal.value || applications.value.length)
));
const capacity = computed(() => summarizeCapacity(cards.value));
const applicationRatioView = computed(() => applicationRatio(applicationSummary.value));

type RingTone = 'accent' | 'success' | 'warning' | 'danger' | 'info';

interface CapacityRingItem {
  key: string;
  label: string;
  percent: number | null;
  text: string;
  hint: string;
  tone: RingTone;
  labelText: string;
}

function capacityTone(percent: number | null): RingTone {
  return percent === null ? 'accent' : percentTone(percent) ?? 'accent';
}

const capacityItems = computed<CapacityRingItem[]>(() => {
  const value = capacity.value;
  const memoryHint = value.memoryUsedBytes !== null && value.memoryTotalBytes !== null
    ? `${formatBytes(value.memoryUsedBytes)} / ${formatBytes(value.memoryTotalBytes)}`
    : t('overviewPage.metricsUnavailable');
  const diskHint = value.diskUsedBytes !== null && value.diskTotalBytes !== null
    ? `${formatBytes(value.diskUsedBytes)} / ${formatBytes(value.diskTotalBytes)}`
    : t('overviewPage.metricsUnavailable');
  const rows: Array<Omit<CapacityRingItem, 'labelText'>> = [
    {
      key: 'cpu',
      label: t('overviewPage.metricCpu'),
      percent: value.cpuPercent,
      text: value.cpuPercent === null ? '—' : formatPercent(value.cpuPercent, 0),
      hint: '',
      tone: capacityTone(value.cpuPercent),
    },
    {
      key: 'memory',
      label: t('overviewPage.metricMemory'),
      percent: value.memoryPercent,
      text: value.memoryPercent === null ? '—' : formatPercent(value.memoryPercent, 0),
      hint: memoryHint,
      tone: capacityTone(value.memoryPercent),
    },
    {
      key: 'disk',
      label: t('overviewPage.metricDisk'),
      percent: value.diskPercent,
      text: value.diskPercent === null ? '—' : formatPercent(value.diskPercent, 0),
      hint: diskHint,
      tone: capacityTone(value.diskPercent),
    },
  ];
  return rows.map((row) => ({
    ...row,
    labelText: t('overviewPage.capacityStatLabel', { label: row.label, value: row.text }),
  }));
});

const onlineRateLabel = computed(() => t('overviewPage.onlineRateChart', { percent: `${onlinePercent.value}%` }));
const applicationRatioLabel = computed(() => t('overviewPage.applicationRatioChart', {
  enabled: applicationSummary.value.enabled,
  disabled: applicationSummary.value.disabled,
}));

function probeCandidateIds(config: NetworkProbeConfig | null, items: ServerDto[]): string[] {
  if (!config || config.mode === 'all') return items.map((item) => item.id);
  const selected = new Set(config.serverIds);
  return items.filter((item) => selected.has(item.id)).map((item) => item.id);
}

function syncProbeQuery(value: string) {
  const current = route.query.probe === undefined ? '' : String(route.query.probe);
  if (value && current !== value) {
    void router.replace({ query: { ...route.query, probe: value } });
    return;
  }
  if (!value && route.query.probe !== undefined) {
    const query = { ...route.query };
    delete query.probe;
    void router.replace({ query });
  }
}

async function load(options: { initial?: boolean; includeUpdates?: boolean } = {}) {
  // 自动刷新/手动刷新不得与在途加载并发：并发会互相 abort，导致长时间
  // 只有被中止的请求，页面像“全部消失”。在途时直接跳过下一拍。
  if (loadInFlight) return;
  loadInFlight = true;
  const seq = ++loadSeq;
  controller?.abort();
  const request = new AbortController();
  controller = request;
  const initial = Boolean(options.initial) || servers.value.length === 0;
  if (initial) {
    pageLoading.value = true;
    pageError.value = '';
  } else {
    refreshing.value = true;
  }
  try {
    const [serversResult, configResult, applicationsResult, metricsConfigResult] = await Promise.allSettled([
      serversApi.list({ signal: request.signal }),
      networkProbeFacilityApi.config({ signal: request.signal }),
      applicationsApi.list({ pageSize: 200 }, { signal: request.signal }),
      metricsFacilityApi.config({ signal: request.signal }),
    ]);
    if (seq !== loadSeq) return;
    if (serversResult.status === 'rejected') throw serversResult.reason;
    pageError.value = '';
    // 自动刷新偶发拿到空列表时保留现有服务器，避免整页瞬间变成“暂无服务器”。
    if (serversResult.value.length > 0 || servers.value.length === 0) {
      servers.value = serversResult.value;
    }
    probeConfig.value = configResult.status === 'fulfilled' ? configResult.value : null;
    metricsConfig.value = metricsConfigResult.status === 'fulfilled' ? metricsConfigResult.value : null;
    // 应用总览是独立摘要：失败只让卡片显示不可用，不能拖垮服务器/指标区域。
    if (applicationsResult.status === 'fulfilled') {
      applications.value = applicationsResult.value.items;
      applicationsTotal.value = applicationsResult.value.total;
      applicationsError.value = false;
    } else if (applications.value.length === 0) {
      applicationsError.value = true;
    }

    const candidates = probeCandidateIds(probeConfig.value, servers.value);
    const requested = route.query.probe === undefined ? '' : String(route.query.probe);
    const nextSource = candidates.includes(probeSourceId.value)
      ? probeSourceId.value
      : candidates.includes(requested)
        ? requested
        : candidates[0] ?? '';
    probeSourceId.value = nextSource;
    syncProbeQuery(nextSource);

    // 探测与指标相互独立：探测失败/无数据不得影响指标渲染。
    await Promise.allSettled([
      loadProbeResults(nextSource, request.signal),
      loadServerDetails(servers.value, { signal: request.signal, includeUpdates: Boolean(options.includeUpdates) }),
    ]);
  } catch (err) {
    if (isAbortError(err) || seq !== loadSeq) return;
    // 首次加载失败走页级错误空态；刷新失败（手动/自动）保留旧数据并在页面内标记陈旧，
    // 不再弹 toast/弹窗。
    pageError.value = err instanceof Error ? err.message : t('common.loadFailed');
  } finally {
    loadInFlight = false;
    if (seq === loadSeq) {
      pageLoading.value = false;
      refreshing.value = false;
    }
  }
}

async function loadProbeResults(sourceId: string, signal: AbortSignal) {
  const seq = ++probeSeq;
  if (!sourceId) {
    probeResults.value = null;
    return;
  }
  try {
    const result = await serversApi.networkProbes(sourceId, { signal }, true);
    if (seq !== probeSeq) return;
    probeResults.value = result;
  } catch (err) {
    // 探测结果失败只让相关卡片显示占位，不阻断页面其它数据。
    if (isAbortError(err) || seq !== probeSeq) return;
  }
}

async function loadServerDetails(items: ServerDto[], options: { signal: AbortSignal; includeUpdates: boolean }) {
  // 以旧数据为基线：单台指标请求失败时保留上一次的值，而不是把该卡片清空。
  const nextMetrics: Record<string, ServerMetricsSeries | undefined> = { ...metricsById.value };
  const nextErrors = new Set<string>();
  const nextPackages = { ...packageUpdatesById.value };
  const nextImages = { ...imageUpdatesById.value };
  await forEachWithConcurrency(items, 6, async (server) => {
    if (options.signal.aborted) return;
    const jobs: Promise<unknown>[] = [
      serversApi.metricsBucketed(server.id, '1d', 1800, { signal: options.signal })
        .then((series) => { nextMetrics[server.id] = series; })
        .catch((err) => { if (!isAbortError(err)) nextErrors.add(server.id); }),
    ];
    if (options.includeUpdates) {
      jobs.push(packagesApi.updates(server.id, { signal: options.signal })
        .then((list) => { nextPackages[server.id] = list.updates.length; })
        .catch((err) => { if (!isAbortError(err)) nextPackages[server.id] = null; }));
      jobs.push(containersApi.images(server.id, { signal: options.signal })
        .then((list) => { nextImages[server.id] = list.items.filter((item) => item.updateAvailable).length; })
        .catch((err) => { if (!isAbortError(err)) nextImages[server.id] = null; }));
    }
    await Promise.all(jobs);
  });
  if (options.signal.aborted) return;
  metricsById.value = nextMetrics;
  metricsErrorIds.value = nextErrors;
  if (options.includeUpdates) {
    packageUpdatesById.value = nextPackages;
    imageUpdatesById.value = nextImages;
  }
}

async function forEachWithConcurrency<T>(items: T[], limit: number, worker: (item: T) => Promise<void>) {
  const queue = [...items];
  const runners = Array.from({ length: Math.min(limit, queue.length) }, async () => {
    for (;;) {
      const item = queue.shift();
      if (item === undefined) return;
      await worker(item);
    }
  });
  await Promise.all(runners);
}

function changeProbeSource(value: string) {
  probeSourceId.value = value;
  syncProbeQuery(value);
  const request = new AbortController();
  void loadProbeResults(value, request.signal);
}

function manualRefresh() {
  void load({ includeUpdates: true });
}

onMounted(() => {
  void load({ initial: true, includeUpdates: true });
  autoRefresh.start(() => load({ includeUpdates: false }));
});
</script>

<template>
  <ConsolePage :title="t('routes.overview.title')" :description="t('routes.overview.description')">
    <template #actions>
      <div class="flex min-w-0 items-center gap-1.5 max-sm:w-full sm:flex-none">
        <span class="shrink-0 text-caption text-muted-foreground">{{ t('overviewPage.probeSource') }}</span>
        <Select
          class="min-w-0 flex-1 sm:w-44 sm:flex-none"
          :model-value="probeSourceId"
          :options="probeSourceOptions"
          :disabled="probeSourceOptions.length === 0"
          :placeholder="t('overviewPage.probeSourcePlaceholder')"
          :aria-label="t('overviewPage.probeSource')"
          @update:model-value="changeProbeSource"
        />
      </div>
      <DataRefreshControl />
      <Button class="max-sm:w-full" :loading="refreshing" @click="manualRefresh">
        <RefreshCcw />
        {{ t('common.refresh') }}
      </Button>
    </template>

    <div class="grid min-w-0 gap-4">
      <template v-if="pageLoading && servers.length === 0">
        <div
          class="grid min-w-0 gap-6 wide:grid-cols-2 wider:grid-cols-3 wide:items-start"
          data-testid="overview-summary-skeleton"
          aria-hidden="true"
        >
          <div class="grid content-start gap-3">
            <Skeleton class="h-3 w-20" />
            <div class="flex items-center gap-4">
              <Skeleton class="size-24 rounded-pill" />
              <div class="flex flex-wrap gap-2">
                <Skeleton v-for="item in 4" :key="item" class="h-7 w-24 rounded-pill" />
              </div>
            </div>
          </div>
          <div class="grid content-start gap-3">
            <Skeleton class="h-3 w-16" />
            <div class="flex items-end gap-4">
              <Skeleton class="h-7 w-12" />
              <div class="flex flex-wrap gap-2">
                <Skeleton v-for="item in 4" :key="item" class="h-7 w-24 rounded-pill" />
              </div>
            </div>
            <Skeleton class="h-2 w-full rounded-pill" />
          </div>
          <div class="grid content-start gap-3">
            <Skeleton class="h-3 w-24" />
            <div class="flex gap-4">
              <Skeleton v-for="item in 3" :key="item" class="size-14 rounded-pill" />
            </div>
          </div>
        </div>
        <div class="grid gap-3 sm:grid-cols-2 wide:grid-cols-3">
          <Skeleton v-for="item in 8" :key="item" class="h-52" />
        </div>
      </template>

      <template v-else>
        <!--
          状态总览头：不套 surface-card，直接位于页面层（Mica 透出）。
          用环形占比 + 胶囊 KPI + 胶囊分布条降低“方块感”；三组之间只用留白，
          不画通长分隔线。服务器卡片网格保持既有卡片语言。
        -->
        <section
          class="motion-reveal grid min-w-0 gap-6 wide:grid-cols-2 wider:grid-cols-3 wide:items-start"
          data-testid="overview-summary"
        >
          <section data-testid="node-summary" class="grid min-w-0 content-start gap-3">
            <h2 class="m-0 text-caption font-semibold uppercase tracking-wide text-muted-foreground">
              {{ t('overviewPage.nodeStatus') }}
            </h2>
            <div class="flex min-w-0 flex-wrap items-center gap-4">
              <div class="grid justify-items-center gap-1">
                <RatioRing
                  :percent="nodeSummary.total ? onlinePercent : null"
                  :value-text="nodeSummary.total ? `${onlinePercent}%` : '—'"
                  :label="onlineRateLabel"
                  :tone="onlineRateTone(nodeSummary)"
                  size="lg"
                  test-id="online-rate-ring"
                />
                <span class="text-caption text-muted-foreground">{{ t('overviewPage.onlineRate') }}</span>
              </div>
              <div class="flex min-w-0 flex-wrap gap-2">
                <StatChip :label="t('overviewPage.totalNodes')" :value="nodeSummary.total" :icon="ServerIcon" />
                <StatChip :label="t('overviewPage.onlineNodes')" :value="nodeSummary.online" tone="success" :icon="CheckCircle2" />
                <StatChip
                  :label="t('overviewPage.offlineNodes')"
                  :value="nodeSummary.offline"
                  :tone="nodeSummary.offline ? 'danger' : 'neutral'"
                  :icon="XCircle"
                />
                <StatChip
                  :label="t('overviewPage.freshMetrics')"
                  :value="`${nodeSummary.fresh}/${nodeSummary.total}`"
                  :tone="nodeSummary.total === 0 ? 'neutral' : nodeSummary.fresh < nodeSummary.total ? 'warning' : 'success'"
                  :icon="History"
                />
              </div>
            </div>
          </section>

          <section data-testid="application-summary" class="grid min-w-0 content-start gap-3">
            <h2 class="m-0 text-caption font-semibold uppercase tracking-wide text-muted-foreground">
              {{ t('overviewPage.applications') }}
            </h2>
            <div class="flex min-w-0 flex-wrap items-end gap-4">
              <div class="grid content-start gap-0.5">
                <strong class="block text-title font-semibold leading-none tabular-nums text-foreground">
                  {{ applicationSummary.available ? applicationSummary.total : '—' }}
                </strong>
                <span class="text-caption text-muted-foreground">{{ t('overviewPage.totalApplications') }}</span>
              </div>
              <div class="flex min-w-0 flex-wrap gap-2">
                <StatChip
                  :label="t('applicationsPage.status.enabled')"
                  :value="applicationSummary.available ? applicationSummary.enabled : '—'"
                  tone="success"
                />
                <StatChip
                  :label="t('applicationsPage.status.deploying')"
                  :value="applicationSummary.available ? applicationSummary.deploying : '—'"
                  tone="info"
                />
                <StatChip
                  :label="t('applicationsPage.status.attention')"
                  :value="applicationSummary.available ? applicationSummary.attention : '—'"
                  :tone="applicationSummary.attention ? 'warning' : 'neutral'"
                />
                <StatChip
                  :label="t('applicationsPage.status.disabled')"
                  :value="applicationSummary.available ? applicationSummary.disabled : '—'"
                />
              </div>
            </div>
            <div
              v-if="applicationSummary.available && applicationRatioView.enabledPercent !== null"
              class="flex h-2 w-full min-w-0 overflow-hidden rounded-pill bg-muted"
              role="img"
              :aria-label="applicationRatioLabel"
              data-testid="application-ratio-bar"
            >
              <span class="h-full bg-success" :style="{ width: `${applicationRatioView.enabledPercent}%` }" />
            </div>
            <p v-if="!applicationSummary.available" class="m-0 text-caption text-muted-foreground">
              {{ t('overviewPage.applicationsUnavailable') }}
            </p>
          </section>

          <section data-testid="capacity-summary" class="grid min-w-0 content-start gap-3 wide:col-span-2 wider:col-span-1">
            <h2 class="m-0 flex min-w-0 flex-wrap items-baseline gap-1.5 text-caption font-semibold uppercase tracking-wide text-muted-foreground">
              <span>{{ t('overviewPage.capacity') }}</span>
              <span class="font-normal normal-case tracking-normal">
                {{ t('overviewPage.capacityScope', { count: nodeSummary.online }) }}
              </span>
            </h2>
            <div class="flex min-w-0 flex-wrap items-start gap-4">
              <div v-for="item in capacityItems" :key="item.key" class="grid justify-items-center gap-0.5">
                <RatioRing
                  :percent="item.percent"
                  :value-text="item.text"
                  :label="item.labelText"
                  :tone="item.tone"
                  size="sm"
                  :test-id="`capacity-${item.key}`"
                />
                <span class="text-caption text-muted-foreground">{{ item.label }}</span>
                <span v-if="item.hint" class="text-caption text-muted-foreground">{{ item.hint }}</span>
              </div>
            </div>
          </section>
        </section>

        <InfoBar v-if="pageError && servers.length > 0" tone="warning" data-testid="overview-stale">
          <div class="flex min-w-0 flex-wrap items-center gap-2">
            <span class="min-w-0 break-words">{{ pageError }}</span>
            <Button size="sm" variant="secondary" :loading="refreshing" @click="manualRefresh">{{ t('common.retry') }}</Button>
          </div>
        </InfoBar>

        <EmptyState v-if="pageError && servers.length === 0" :title="t('common.loadFailed')" :description="pageError">
          <template #actions>
            <Button size="sm" :loading="pageLoading" @click="load({ initial: true, includeUpdates: true })">{{ t('common.retry') }}</Button>
          </template>
        </EmptyState>
        <EmptyState v-else-if="servers.length === 0" :title="t('overviewPage.noServersConnected')" :description="t('overviewPage.addServerHint')">
          <template #actions>
            <Button variant="primary" @click="router.push('/servers')">
              <ServerIcon />
              {{ t('common.add') }}
            </Button>
          </template>
        </EmptyState>
        <div v-else class="grid gap-3 sm:grid-cols-2 wide:grid-cols-3">
          <ServerCard v-for="card in cards" :key="card.server.id" :card="card" :thresholds="warningThresholds" />
        </div>
      </template>
    </div>
  </ConsolePage>
</template>
