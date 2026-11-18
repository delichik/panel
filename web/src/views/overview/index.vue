<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { AppWindow, RefreshCcw, Server as ServerIcon } from '@/icons';
import { applicationsApi } from '@/api/applications';
import { containersApi } from '@/api/containers';
import { networkProbeFacilityApi } from '@/api/networkProbe';
import { packagesApi } from '@/api/packages';
import { serversApi, type ServerMetricsSeries } from '@/api/servers';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import Select from '@/components/ui/Select.vue';
import Skeleton from '@/components/ui/Skeleton.vue';
import { useErrorToast } from '@/components/ui/toast';
import DataRefreshControl from '@/components/patterns/DataRefreshControl.vue';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import { useAutoRefresh } from '@/composables/useAutoRefresh';
import { useI18n } from '@/i18n';
import type { NetworkProbeConfig, NetworkProbeServerResults } from '@/types/networkProbe';
import type { ApplicationSummaryDto } from '@/types/applications';
import type { ServerDto } from '@/types/servers';
import { isAbortError } from '@/views/_shared/requestState';
import ServerCard from './ServerCard.vue';
import { buildServerCards, emptyApplicationSummary, summarizeApplications, summarizeNodeCards } from './model';

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const notifyError = useErrorToast();
const autoRefresh = useAutoRefresh();

const servers = ref<ServerDto[]>([]);
const metricsById = ref<Record<string, ServerMetricsSeries | undefined>>({});
const metricsErrorIds = ref<Set<string>>(new Set());
const packageUpdatesById = ref<Record<string, number | null>>({});
const imageUpdatesById = ref<Record<string, number | null>>({});
const probeConfig = ref<NetworkProbeConfig | null>(null);
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
    const [serversResult, configResult, applicationsResult] = await Promise.allSettled([
      serversApi.list({ signal: request.signal }),
      networkProbeFacilityApi.config({ signal: request.signal }),
      applicationsApi.list({ pageSize: 200 }, { signal: request.signal }),
    ]);
    if (seq !== loadSeq) return;
    if (serversResult.status === 'rejected') throw serversResult.reason;
    // 自动刷新偶发拿到空列表时保留现有服务器，避免整页瞬间变成“暂无服务器”。
    if (serversResult.value.length > 0 || servers.value.length === 0) {
      servers.value = serversResult.value;
    }
    probeConfig.value = configResult.status === 'fulfilled' ? configResult.value : null;
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
    const message = err instanceof Error ? err.message : t('common.loadFailed');
    if (initial) pageError.value = message;
    else notifyError(message, err);
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
      <div class="flex min-w-0 items-center gap-1.5">
        <span class="shrink-0 text-caption text-muted-foreground">{{ t('overviewPage.probeSource') }}</span>
        <Select
          class="min-w-36"
          :model-value="probeSourceId"
          :options="probeSourceOptions"
          :disabled="probeSourceOptions.length === 0"
          :placeholder="t('overviewPage.probeSourcePlaceholder')"
          :aria-label="t('overviewPage.probeSource')"
          @update:model-value="changeProbeSource"
        />
      </div>
      <DataRefreshControl />
      <Button :loading="refreshing" @click="manualRefresh">
        <RefreshCcw />
        {{ t('common.refresh') }}
      </Button>
    </template>

    <div class="grid h-full min-h-0 grid-rows-page gap-3 max-lg:block max-lg:h-auto">
      <div class="grid min-w-0 gap-3 wide:grid-cols-2" data-testid="overview-summary">
      <section class="min-w-0 surface-card p-4" data-testid="node-summary">
        <div class="flex items-center justify-between gap-2">
          <h2 class="m-0 flex items-center gap-1.5 text-body-strong font-semibold text-foreground">
            <ServerIcon class="size-3.5 text-secondary-foreground" aria-hidden="true" />
            {{ t('overviewPage.nodeStatus') }}
          </h2>
          <Badge :tone="nodeSummary.offline ? 'danger' : 'success'">{{ onlinePercent }}%</Badge>
        </div>
        <div class="mt-3 grid grid-cols-1 gap-2 sm:grid-cols-3 sm:gap-0 sm:divide-x sm:divide-divider">
          <div class="min-w-0 sm:pr-3">
            <span class="block text-caption text-muted-foreground">{{ t('overviewPage.totalNodes') }}</span>
            <strong class="mt-1 block text-title font-semibold leading-none text-foreground">{{ nodeSummary.total }}</strong>
          </div>
          <div class="min-w-0 sm:px-3">
            <span class="block text-caption text-muted-foreground">{{ t('overviewPage.onlineNodes') }}</span>
            <strong class="mt-1 block text-title font-semibold leading-none text-success">{{ nodeSummary.online }}</strong>
          </div>
          <div class="min-w-0 sm:pl-3">
            <span class="block text-caption text-muted-foreground">{{ t('overviewPage.offlineNodes') }}</span>
            <strong class="mt-1 block text-title font-semibold leading-none" :class="nodeSummary.offline ? 'text-danger' : 'text-foreground'">{{ nodeSummary.offline }}</strong>
          </div>
        </div>
        <div class="mt-3 flex h-1 overflow-hidden rounded-control bg-muted" role="img" :aria-label="`${t('overviewPage.nodeStatus')}: ${onlinePercent}%`">
          <span class="bg-success" :style="{ width: `${nodeSummary.total ? (nodeSummary.online / nodeSummary.total) * 100 : 0}%` }" />
          <span class="bg-danger" :style="{ width: `${nodeSummary.total ? (nodeSummary.offline / nodeSummary.total) * 100 : 0}%` }" />
        </div>
        <p class="m-0 mt-2 text-caption text-muted-foreground">
          {{ t('overviewPage.freshMetrics') }} {{ nodeSummary.fresh }}/{{ nodeSummary.total }}
        </p>
      </section>

      <section class="min-w-0 surface-card p-4" data-testid="application-summary">
        <div class="flex items-center justify-between gap-2">
          <h2 class="m-0 flex items-center gap-1.5 text-body-strong font-semibold text-foreground">
            <AppWindow class="size-3.5 text-secondary-foreground" aria-hidden="true" />
            {{ t('overviewPage.applications') }}
          </h2>
          <Badge :tone="applicationSummary.available ? (applicationSummary.attention ? 'warning' : 'success') : 'neutral'">
            {{ applicationSummary.available ? applicationSummary.total : '—' }}
          </Badge>
        </div>
        <div class="mt-3 grid grid-cols-2 gap-2 sm:grid-cols-4 sm:gap-0 sm:divide-x sm:divide-divider">
          <div class="min-w-0 sm:pr-3">
            <span class="block text-caption text-muted-foreground">{{ t('overviewPage.totalApplications') }}</span>
            <strong class="mt-1 block text-title font-semibold leading-none text-foreground">{{ applicationSummary.total ?? '—' }}</strong>
          </div>
          <div class="min-w-0 sm:px-3">
            <span class="block text-caption text-muted-foreground">{{ t('applicationsPage.status.enabled') }}</span>
            <strong class="mt-1 block text-title font-semibold leading-none text-success">{{ applicationSummary.available ? applicationSummary.enabled : '—' }}</strong>
          </div>
          <div class="min-w-0 sm:px-3">
            <span class="block text-caption text-muted-foreground">{{ t('applicationsPage.status.deploying') }}</span>
            <strong class="mt-1 block text-title font-semibold leading-none text-foreground">{{ applicationSummary.available ? applicationSummary.deploying : '—' }}</strong>
          </div>
          <div class="min-w-0 sm:pl-3">
            <span class="block text-caption text-muted-foreground">{{ t('applicationsPage.status.attention') }}</span>
            <strong class="mt-1 block text-title font-semibold leading-none" :class="applicationSummary.attention ? 'text-warning' : 'text-foreground'">{{ applicationSummary.available ? applicationSummary.attention : '—' }}</strong>
          </div>
        </div>
        <p v-if="!applicationSummary.available" class="m-0 mt-2 text-caption text-muted-foreground">{{ t('overviewPage.applicationsUnavailable') }}</p>
        <p v-else class="m-0 mt-2 text-caption text-muted-foreground">
          {{ t('applicationsPage.status.disabled') }} {{ applicationSummary.disabled }}
        </p>
      </section>
      </div>

      <div class="min-h-0 min-w-0 overflow-y-auto overflow-x-hidden max-lg:overflow-visible">
        <div v-if="pageLoading && servers.length === 0" class="grid gap-3 sm:grid-cols-2 wide:grid-cols-3">
          <Skeleton v-for="item in 8" :key="item" class="h-52" />
        </div>
        <EmptyState v-else-if="pageError && servers.length === 0" :title="t('common.loadFailed')" :description="pageError">
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
          <ServerCard v-for="card in cards" :key="card.server.id" :card="card" />
        </div>
      </div>
    </div>
  </ConsolePage>
</template>
