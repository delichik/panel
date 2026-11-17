<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import { RefreshCcw, Save } from '@lucide/vue';
import { networkProbeFacilityApi } from '@/api/networkProbe';
import { serversApi } from '@/api/servers';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import Input from '@/components/ui/Input.vue';
import LoadingOverlay from '@/components/ui/LoadingOverlay.vue';
import SearchInput from '@/components/ui/SearchInput.vue';
import Select from '@/components/ui/Select.vue';
import DataRefreshControl from '@/components/patterns/DataRefreshControl.vue';
import ServerMultiPicker from '@/components/patterns/ServerMultiPicker.vue';
import { useAutoRefresh } from '@/composables/useAutoRefresh';
import HistoryStrip from '@/views/overview/HistoryStrip.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { NetworkProbeConfig, NetworkProbeMatrix, NetworkProbeMode, NetworkProbeResult, NetworkProbeServerResults } from '@/types/networkProbe';
import type { ServerDto } from '@/types/servers';
import { formatDateTime } from '@/utils/datetime';
import {
  isValidNetworkProbeSetting,
  networkProbeIntervalMinutes,
  networkProbeIntervalMinutesFromSeconds,
  networkProbeIntervalSecondsFromMinutes,
  networkProbeSettingRules,
  normalizeNetworkProbeSettings,
  parseNetworkProbeIntervalMinutes,
  parseNetworkProbeSetting,
} from '@/utils/networkProbe';
import { createLatestRequestGuard, isAbortError } from '@/views/_shared/requestState';
import { formatLatency, formatPacketLoss, probeLatencyHistory, probeLossHistory, probeTargetView, valueTone, type MetricTone } from '@/views/overview/model';

const props = defineProps<{
  mode: 'facilityDetail' | 'facilityConfig';
  servers: ServerDto[];
}>();

const { t } = useI18n();
const notifyError = useErrorToast();
const notifySuccess = useSuccessToast();
const autoRefresh = useAutoRefresh();

// 探测间隔在界面上按分钟输入（1..60，默认 1），保存时换算成后台的 intervalSeconds。
type ParameterField = 'pingCount' | 'pingTimeoutSeconds';
type ProbeOutcome = 'ok' | 'degraded' | 'stale' | 'failed';

const parameterFields = [
  { field: 'pingCount', labelKey: 'applicationsPage.networkProbePingCount', hintKey: 'applicationsPage.networkProbePingCountHint', invalidKey: 'applicationsPage.networkProbePingCountInvalid', rule: networkProbeSettingRules.pingCount },
  { field: 'pingTimeoutSeconds', labelKey: 'applicationsPage.networkProbePingTimeout', hintKey: 'applicationsPage.networkProbePingTimeoutHint', invalidKey: 'applicationsPage.networkProbePingTimeoutInvalid', rule: networkProbeSettingRules.pingTimeoutSeconds },
] as const;

const config = ref<NetworkProbeConfig | null>(null);
const matrix = ref<NetworkProbeMatrix | null>(null);
const loading = ref(false);
const saving = ref(false);
const error = ref('');
const formError = ref('');
const draft = reactive<{
  mode: NetworkProbeMode;
  serverIds: string[];
  intervalMinutes: string;
  pingCount: string;
  pingTimeoutSeconds: string;
}>({
  mode: 'all',
  serverIds: [],
  intervalMinutes: String(networkProbeIntervalMinutes.defaultValue),
  pingCount: String(networkProbeSettingRules.pingCount.defaultValue),
  pingTimeoutSeconds: String(networkProbeSettingRules.pingTimeoutSeconds.defaultValue),
});
const guard = createLatestRequestGuard();
let controller: AbortController | null = null;

// 结果区：探测源 + 搜索 + 纵向目标列表（含 24h 历史），不再使用横向源×目标矩阵。
const sourceId = ref('');
const targetSearch = ref('');
const sourceResults = ref<NetworkProbeServerResults | null>(null);
const resultsLoading = ref(false);
const resultsError = ref('');
const resultsGuard = createLatestRequestGuard();
let resultsController: AbortController | null = null;

const textToneClass: Record<MetricTone, string> = {
  success: 'text-success',
  warning: 'text-warning',
  danger: 'text-danger',
};

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

const sourceOptions = computed(() => (matrix.value?.nodes ?? []).map((node) => ({
  value: node.serverId,
  label: node.address ? `${node.name} · ${node.address}` : node.name,
})));

const parameterValues = computed<Record<ParameterField, number | null>>(() => ({
  pingCount: parseNetworkProbeSetting(draft.pingCount),
  pingTimeoutSeconds: parseNetworkProbeSetting(draft.pingTimeoutSeconds),
}));

const intervalMinutesValue = computed(() => parseNetworkProbeIntervalMinutes(draft.intervalMinutes));
const intervalInvalid = computed(() => intervalMinutesValue.value === null);

function parameterInvalid(field: ParameterField) {
  const value = parameterValues.value[field];
  return value === null || !isValidNetworkProbeSetting(field, value);
}

function setParameter(field: ParameterField, value: string) {
  draft[field] = value;
}

const dirty = computed(() => {
  const current = config.value;
  if (!current) return true;
  if (draft.mode !== current.mode) return true;
  if (JSON.stringify([...draft.serverIds].sort()) !== JSON.stringify([...current.serverIds].sort())) return true;
  // 秒为单位保存/加载，比较时把分钟草稿换算回秒，避免出现“1 分钟 vs 60 秒”的伪脏状态。
  if (intervalMinutesValue.value !== networkProbeIntervalMinutesFromSeconds(current.intervalSeconds)) return true;
  return parameterFields.some((item) => parameterValues.value[item.field] !== current[item.field]);
});

interface TargetRow {
  result: NetworkProbeResult;
  targetServerId: string;
  name: string;
  address: string;
  latencyMs: number | null;
  packetLossPercent: number | null;
  sampledAt: string;
  outcome: ProbeOutcome;
  latencyHistory: Array<number | null>;
  lossHistory: Array<number | null>;
}

const allTargetRows = computed<TargetRow[]>(() => {
  const data = sourceResults.value;
  if (!data) return [];
  const nodes = data.nodes ?? [];
  const results = data.results ?? [];
  const nodesById = new Map(nodes.map((node) => [node.serverId, node]));
  return results
    .filter((result) => result.targetServerId !== data.serverId)
    .map((result) => {
      const node = nodesById.get(result.targetServerId);
      const server = props.servers.find((item) => item.id === result.targetServerId);
      const view = probeTargetView(result);
      const outcome: ProbeOutcome = view.failed
        ? 'failed'
        : view.stale
          ? 'stale'
          : (view.packetLossPercent ?? 0) > 0 ? 'degraded' : 'ok';
      return {
        result,
        targetServerId: result.targetServerId,
        name: node?.name || server?.name || result.targetServerId,
        address: node?.address || result.targetAddress || '',
        latencyMs: view.latencyMs,
        packetLossPercent: view.packetLossPercent,
        sampledAt: view.sampledAt ?? '',
        outcome,
        latencyHistory: probeLatencyHistory(result.history),
        lossHistory: probeLossHistory(result.history),
      };
    });
});

const targetRows = computed(() => {
  const query = targetSearch.value.trim().toLowerCase();
  if (!query) return allTargetRows.value;
  return allTargetRows.value.filter((row) => [row.name, row.address, row.result.targetServerId].some((value) => value.toLowerCase().includes(query)));
});

const failedTargetCount = computed(() => allTargetRows.value.filter((row) => row.outcome === 'failed').length);

function outcomeTone(outcome: ProbeOutcome): 'success' | 'warning' | 'danger' | 'neutral' {
  if (outcome === 'ok') return 'success';
  if (outcome === 'degraded' || outcome === 'stale') return 'warning';
  if (outcome === 'failed') return 'danger';
  return 'neutral';
}

function outcomeKey(outcome: ProbeOutcome) {
  return `serversPage.probe.status.${outcome}` as const;
}

function valueClass(kind: 'latency' | 'packetLoss', value: number | null) {
  const tone = valueTone(kind, value);
  return tone ? textToneClass[tone] : 'text-foreground';
}

function sentReceived(row: TargetRow) {
  return `${row.result.sent} / ${row.result.received}`;
}

function resetDraft() {
  const current = config.value;
  draft.mode = current?.mode ?? 'all';
  draft.serverIds = [...(current?.serverIds ?? [])];
  draft.intervalMinutes = String(networkProbeIntervalMinutesFromSeconds(current?.intervalSeconds));
  draft.pingCount = String(current?.pingCount ?? networkProbeSettingRules.pingCount.defaultValue);
  draft.pingTimeoutSeconds = String(current?.pingTimeoutSeconds ?? networkProbeSettingRules.pingTimeoutSeconds.defaultValue);
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
    // 旧服务端可能不返回三个节拍参数：按默认值补齐，避免表单显示空白。
    config.value = { ...nextConfig, ...normalizeNetworkProbeSettings(nextConfig) };
    matrix.value = nextMatrix;
    resetDraft();
    const nodes = nextMatrix.nodes ?? [];
    const nextSource = nodes.some((node) => node.serverId === sourceId.value)
      ? sourceId.value
      : nodes[0]?.serverId ?? '';
    sourceId.value = nextSource;
    await loadSourceResults(nextSource, { clear: true });
  } catch (err) {
    if (isAbortError(err)) return;
    if (!guard.isCurrent(requestId)) return;
    error.value = err instanceof Error ? err.message : t('applicationsPage.networkProbeLoadFailed');
    notifyError(error.value, err);
  } finally {
    if (guard.isCurrent(requestId)) loading.value = false;
  }
}

/** 加载单个探测源的当前结果与 24h 历史；失败时保留上次成功数据不清空。 */
async function loadSourceResults(sourceServerId: string, options: { clear?: boolean } = {}) {
  if (!sourceServerId) {
    sourceResults.value = null;
    resultsError.value = '';
    return;
  }
  if (options.clear) {
    sourceResults.value = null;
    resultsError.value = '';
  }
  resultsController?.abort();
  const requestId = resultsGuard.begin();
  const controller = new AbortController();
  resultsController = controller;
  resultsLoading.value = true;
  resultsError.value = '';
  try {
    const next = await serversApi.networkProbes(sourceServerId, { signal: controller.signal }, true);
    if (!resultsGuard.isCurrent(requestId)) return;
    sourceResults.value = next;
  } catch (err) {
    if (isAbortError(err)) return;
    if (!resultsGuard.isCurrent(requestId)) return;
    resultsError.value = err instanceof Error ? err.message : t('applicationsPage.networkProbeLoadFailed');
    notifyError(resultsError.value, err);
  } finally {
    if (resultsGuard.isCurrent(requestId)) resultsLoading.value = false;
  }
}

function changeSource(value: string) {
  sourceId.value = value;
  void loadSourceResults(value, { clear: true });
}

async function save() {
  if (saving.value) return;
  formError.value = '';
  if (draft.mode === 'selected' && draft.serverIds.length === 0) {
    formError.value = t('applicationsPage.networkProbeServersRequired');
    return;
  }
  if (intervalInvalid.value) {
    formError.value = t('applicationsPage.networkProbeIntervalInvalid', { min: networkProbeIntervalMinutes.min, max: networkProbeIntervalMinutes.max });
    return;
  }
  if (parameterFields.some((item) => parameterInvalid(item.field))) return;
  const settings = parameterValues.value;
  const intervalMinutes = intervalMinutesValue.value ?? networkProbeIntervalMinutes.defaultValue;
  saving.value = true;
  try {
    const saved = await networkProbeFacilityApi.saveConfig({
      mode: draft.mode,
      serverIds: [...draft.serverIds],
      intervalSeconds: networkProbeIntervalSecondsFromMinutes(intervalMinutes),
      pingCount: settings.pingCount ?? networkProbeSettingRules.pingCount.defaultValue,
      pingTimeoutSeconds: settings.pingTimeoutSeconds ?? networkProbeSettingRules.pingTimeoutSeconds.defaultValue,
    });
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

onMounted(() => {
  void load();
  // 自动刷新只重取当前探测源结果与历史；配置表单有未保存修改时不打断。
  autoRefresh.start(() => {
    if (dirty.value || resultsLoading.value || !sourceId.value) return;
    void loadSourceResults(sourceId.value);
  });
});
onBeforeUnmount(() => {
  controller?.abort();
  resultsController?.abort();
  guard.invalidate();
  resultsGuard.invalidate();
});
</script>

<template>
  <div
    class="relative grid h-full min-h-0 grid-rows-[auto_auto_minmax(0,1fr)] gap-4 max-lg:h-auto max-lg:grid-rows-none"
    data-testid="network-probe-facility"
  >
    <LoadingOverlay v-if="loading && !config" :label="t('applicationsPage.networkProbeLoading')" />

    <EmptyState v-if="error && !config" variant="page" :title="t('common.loadFailed')" :description="error">
      <template #actions>
        <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button>
      </template>
    </EmptyState>

    <template v-else>
      <div class="grid gap-4">
        <!-- 区域一：探测范围（模式 + 探测节点），与配置区上下堆叠。 -->
        <section
          class="grid gap-3 rounded-xl border border-border bg-card p-4"
          data-testid="network-probe-scope-section"
        >
          <div>
            <h2 class="m-0 text-base font-semibold text-foreground">{{ t('applicationsPage.networkProbeScopeTitle') }}</h2>
            <p class="m-0 mt-1 text-sm leading-6 text-muted-foreground">{{ t('applicationsPage.networkProbeScopeHint') }}</p>
          </div>

          <div class="grid content-start gap-3">
            <label class="grid gap-1 sm:max-w-xs">
              <span class="text-xs font-medium text-muted-foreground">{{ t('applicationsPage.networkProbeMode') }}</span>
              <Select v-model="draft.mode" :options="modeOptions" :disabled="saving" />
            </label>

            <div v-if="draft.mode === 'selected'" class="grid gap-2">
              <div>
                <span class="text-xs font-medium text-muted-foreground">{{ t('applicationsPage.networkProbeSelectServers') }}</span>
                <p class="m-0 mt-1 text-xs leading-5 text-muted-foreground">{{ t('applicationsPage.networkProbeSelectServersHint') }}</p>
              </div>
              <div class="max-h-64 overflow-y-auto pr-1">
                <ServerMultiPicker v-model="draft.serverIds" variant="compact" :servers="serverOptions" :label="t('applicationsPage.networkProbeSelectServers')" />
              </div>
            </div>
          </div>
        </section>

        <!-- 区域二：探测配置（间隔、包数、单包超时 + 保存）。 -->
        <section class="grid gap-3 rounded-xl border border-border bg-card p-4" data-testid="network-probe-config-section">
          <div>
            <h2 class="m-0 text-base font-semibold text-foreground">{{ t('applicationsPage.networkProbeConfigTitle') }}</h2>
            <p class="m-0 mt-1 text-sm leading-6 text-muted-foreground">{{ t('applicationsPage.networkProbeConfigHint') }}</p>
          </div>

          <div class="grid gap-3 sm:grid-cols-3">
            <label class="grid min-w-0 gap-1">
              <span class="text-xs font-medium text-muted-foreground">{{ t('applicationsPage.networkProbeInterval') }}</span>
              <Input
                v-model="draft.intervalMinutes"
                type="number"
                inputmode="numeric"
                :min="networkProbeIntervalMinutes.min"
                :max="networkProbeIntervalMinutes.max"
                step="1"
                :invalid="intervalInvalid"
                :disabled="saving"
                data-testid="network-probe-interval-minutes"
              />
              <span class="text-xs leading-4 text-muted-foreground">{{ t('applicationsPage.networkProbeIntervalHint', { min: networkProbeIntervalMinutes.min, max: networkProbeIntervalMinutes.max, default: networkProbeIntervalMinutes.defaultValue }) }}</span>
              <span v-if="intervalInvalid" class="text-xs leading-4 text-danger">{{ t('applicationsPage.networkProbeIntervalInvalid', { min: networkProbeIntervalMinutes.min, max: networkProbeIntervalMinutes.max }) }}</span>
            </label>
            <label v-for="item in parameterFields" :key="item.field" class="grid min-w-0 gap-1">
              <span class="text-xs font-medium text-muted-foreground">{{ t(item.labelKey) }}</span>
              <Input
                :model-value="draft[item.field]"
                type="number"
                inputmode="numeric"
                :min="item.rule.min"
                :max="item.rule.max"
                :step="item.rule.step"
                :invalid="parameterInvalid(item.field)"
                :disabled="saving"
                @update:model-value="setParameter(item.field, $event)"
              />
              <span class="text-xs leading-4 text-muted-foreground">{{ t(item.hintKey, { min: item.rule.min, max: item.rule.max, default: item.rule.defaultValue }) }}</span>
              <span v-if="parameterInvalid(item.field)" class="text-xs leading-4 text-danger">{{ t(item.invalidKey, { min: item.rule.min, max: item.rule.max }) }}</span>
            </label>
          </div>

          <p v-if="formError" role="alert" class="m-0 text-sm text-danger">{{ formError }}</p>
          <div class="flex justify-end">
            <Button size="sm" variant="primary" :loading="saving" :disabled="!dirty" @click="save"><Save />{{ t('applicationsPage.networkProbeSave') }}</Button>
          </div>
        </section>
      </div>

      <!-- 区域三：结果（保持独立，卡片内部滚动）。 -->
      <section
        class="relative grid min-h-0 gap-3 rounded-xl border border-border bg-card p-4 max-lg:grid-rows-none lg:grid-rows-[auto_auto_minmax(0,1fr)]"
        data-testid="network-probe-results-section"
      >
        <div class="min-w-0">
          <h2 class="m-0 text-base font-semibold text-foreground">{{ t('applicationsPage.networkProbeResultsTitle') }}</h2>
          <p class="m-0 mt-1 text-sm leading-6 text-muted-foreground">{{ t('applicationsPage.networkProbeResultsHint') }}</p>
        </div>

        <div class="grid min-w-0 gap-2 sm:grid-cols-[minmax(0,17rem)_minmax(0,1fr)_auto]">
          <Select
            :model-value="sourceId"
            :options="sourceOptions"
            :disabled="!sourceOptions.length"
            :placeholder="t('applicationsPage.networkProbeSourcePlaceholder')"
            :aria-label="t('applicationsPage.networkProbeSource')"
            @update:model-value="changeSource"
          />
          <SearchInput
            v-model="targetSearch"
            clearable
            :disabled="!sourceOptions.length"
            :placeholder="t('applicationsPage.networkProbeSearchTargets')"
            :label="t('applicationsPage.networkProbeSearchTargets')"
            :clear-label="t('common.clearSearch')"
          />
          <div class="flex flex-wrap items-center justify-end gap-2">
            <DataRefreshControl />
            <Button size="sm" variant="ghost" :loading="resultsLoading" :disabled="!sourceId" @click="loadSourceResults(sourceId)"><RefreshCcw />{{ t('common.refresh') }}</Button>
          </div>
        </div>

        <LoadingOverlay v-if="resultsLoading && !sourceResults" :label="t('applicationsPage.networkProbeLoading')" />
        <EmptyState
          v-else-if="!sourceOptions.length"
          variant="inset"
          :title="t('applicationsPage.networkProbeNoProbeNodes')"
          :description="t('applicationsPage.networkProbeNoProbeNodesHint')"
        />
        <EmptyState v-else-if="resultsError && !sourceResults" variant="page" :title="t('common.loadFailed')" :description="resultsError">
          <template #actions>
            <Button size="sm" :loading="resultsLoading" @click="loadSourceResults(sourceId)"><RefreshCcw />{{ t('common.retry') }}</Button>
          </template>
        </EmptyState>
        <div v-else class="grid min-h-0 content-start gap-2 lg:overflow-y-auto lg:pr-1">
          <p v-if="resultsError" role="alert" class="m-0 rounded-xl border border-warning-border bg-warning-bg p-2 text-xs text-warning">{{ t('applicationsPage.networkProbeShowingStale') }}</p>
          <div class="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
            <span>{{ t('applicationsPage.networkProbeTargetsSummary', { count: allTargetRows.length, failed: failedTargetCount }) }}</span>
            <span>{{ t('serversPage.probe.lastSample') }}: {{ formatDateTime(sourceResults?.updatedAt, t('common.never')) }}</span>
          </div>

          <EmptyState
            v-if="!allTargetRows.length"
            variant="inset"
            :title="t('applicationsPage.networkProbeNoTargets')"
            :description="t('applicationsPage.networkProbeNoTargetsHint')"
          />
          <EmptyState
            v-else-if="!targetRows.length"
            variant="inset"
            :title="t('applicationsPage.networkProbeSearchEmpty')"
            :description="t('applicationsPage.networkProbeSearchEmptyHint')"
          />
          <ul v-else class="m-0 list-none overflow-x-hidden p-0 max-lg:max-h-[36rem] max-lg:overflow-y-auto" data-testid="probe-target-list">
            <li
              v-for="row in targetRows"
              :key="row.targetServerId"
              class="grid min-w-0 gap-2 border-t border-border py-3 first:border-t-0"
              data-testid="probe-target"
            >
              <div class="flex min-w-0 flex-wrap items-start justify-between gap-2">
                <div class="min-w-0">
                  <strong class="block truncate text-sm text-foreground">{{ row.name }}</strong>
                  <span class="panel-mono block truncate text-xs text-muted-foreground">{{ row.address || t('common.notAvailable') }}</span>
                </div>
                <Badge :tone="outcomeTone(row.outcome)">{{ t(outcomeKey(row.outcome)) }}</Badge>
              </div>

              <dl class="m-0 grid min-w-0 grid-cols-2 gap-2 sm:grid-cols-4">
                <div class="min-w-0">
                  <dt class="truncate text-xs text-muted-foreground">{{ t('serversPage.probe.latency') }}</dt>
                  <dd class="m-0 text-sm font-semibold tabular-nums" :class="valueClass('latency', row.latencyMs)">{{ formatLatency(row.latencyMs) }}</dd>
                </div>
                <div class="min-w-0">
                  <dt class="truncate text-xs text-muted-foreground">{{ t('serversPage.probe.packetLoss') }}</dt>
                  <dd class="m-0 text-sm font-semibold tabular-nums" :class="valueClass('packetLoss', row.packetLossPercent)">{{ formatPacketLoss(row.packetLossPercent) }}</dd>
                </div>
                <div class="min-w-0">
                  <dt class="truncate text-xs text-muted-foreground">{{ t('applicationsPage.networkProbeSentReceived') }}</dt>
                  <dd class="m-0 text-sm font-semibold tabular-nums text-muted-foreground">{{ sentReceived(row) }}</dd>
                </div>
                <div class="min-w-0">
                  <dt class="truncate text-xs text-muted-foreground">{{ t('serversPage.probe.sampledAt') }}</dt>
                  <dd class="m-0 truncate text-xs text-secondary-foreground">{{ formatDateTime(row.sampledAt, t('common.never')) }}</dd>
                </div>
              </dl>

              <div class="grid min-w-0 gap-2 sm:grid-cols-2">
                <div class="grid min-w-0 gap-1">
                  <span class="truncate text-xs text-muted-foreground">{{ t('applicationsPage.networkProbeLatencyHistory') }}</span>
                  <HistoryStrip kind="latency" :values="row.latencyHistory" :label="`${row.name} · ${t('applicationsPage.networkProbeLatencyHistory')}`" />
                </div>
                <div class="grid min-w-0 gap-1">
                  <span class="truncate text-xs text-muted-foreground">{{ t('applicationsPage.networkProbeLossHistory') }}</span>
                  <HistoryStrip kind="packetLoss" :values="row.lossHistory" :label="`${row.name} · ${t('applicationsPage.networkProbeLossHistory')}`" />
                </div>
              </div>
            </li>
          </ul>
        </div>
      </section>
    </template>
  </div>
</template>
