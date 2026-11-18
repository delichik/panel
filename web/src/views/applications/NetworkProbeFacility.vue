<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import { RefreshCcw, Save } from '@/icons';
import { networkProbeFacilityApi } from '@/api/networkProbe';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import LoadingOverlay from '@/components/ui/LoadingOverlay.vue';
import NumberBox from '@/components/ui/NumberBox.vue';
import Select from '@/components/ui/Select.vue';
import DataRefreshControl from '@/components/patterns/DataRefreshControl.vue';
import FacilityField from '@/components/patterns/FacilityField.vue';
import FacilitySection from '@/components/patterns/FacilitySection.vue';
import FacilitySectionHeader from '@/components/patterns/FacilitySectionHeader.vue';
import InfoBar from '@/components/patterns/InfoBar.vue';
import ProbeHistoryCharts from '@/components/patterns/ProbeHistoryCharts.vue';
import ServerMultiPicker from '@/components/patterns/ServerMultiPicker.vue';
import { useAutoRefresh } from '@/composables/useAutoRefresh';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { NetworkProbeConfig, NetworkProbeMode, NetworkProbeTargetMatrix, NetworkProbeTargetResult } from '@/types/networkProbe';
import type { ServerDto } from '@/types/servers';
import { formatDateTime } from '@/utils/datetime';
import { probeHistorySeries, probeAggregateOutcomeStatusKey, probeAggregateOutcomeTone, probeTargetOutcome, type NetworkProbeAggregateOutcome } from '@/utils/networkProbe';
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
import { formatLatency, formatPacketLoss, valueTone, type MetricTone } from '@/views/overview/model';

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

const parameterFields = [
  { field: 'pingCount', labelKey: 'applicationsPage.networkProbePingCount', hintKey: 'applicationsPage.networkProbePingCountHint', invalidKey: 'applicationsPage.networkProbePingCountInvalid', rule: networkProbeSettingRules.pingCount },
  { field: 'pingTimeoutSeconds', labelKey: 'applicationsPage.networkProbePingTimeout', hintKey: 'applicationsPage.networkProbePingTimeoutHint', invalidKey: 'applicationsPage.networkProbePingTimeoutInvalid', rule: networkProbeSettingRules.pingTimeoutSeconds },
] as const;

const config = ref<NetworkProbeConfig | null>(null);
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

// 结果区直接展示全部服务器的按目标聚合摘要，不再让用户选择探测源/目标。
const targetMatrix = ref<NetworkProbeTargetMatrix | null>(null);
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

const parameterValues = computed<Record<ParameterField, number | null>>(() => ({
  pingCount: parseNetworkProbeSetting(draft.pingCount),
  pingTimeoutSeconds: parseNetworkProbeSetting(draft.pingTimeoutSeconds),
}));

const intervalMinutesValue = computed(() => parseNetworkProbeIntervalMinutes(draft.intervalMinutes));
const intervalInvalid = computed(() => intervalMinutesValue.value === null);
/** NumberBox 适配：draft 仍是字符串（换算/校验/保存沿用 parse），空值映射为 null。 */
const intervalNumber = computed<number | null>({
  get: () => intervalMinutesValue.value,
  set: (value: number | null) => { draft.intervalMinutes = value === null ? '' : String(value); },
});

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

interface TargetCardView {
  node: NetworkProbeTargetResult;
  outcome: NetworkProbeAggregateOutcome;
  latencySeries: ReturnType<typeof probeHistorySeries>;
  lossSeries: ReturnType<typeof probeHistorySeries>;
}

const targetCards = computed<TargetCardView[]>(() => (targetMatrix.value?.nodes ?? []).map((node) => ({
  node,
  outcome: probeTargetOutcome(node),
  latencySeries: probeHistorySeries(node.history, 'latency', `${node.serverId}-latency`, t('serversPage.probe.latency')),
  lossSeries: probeHistorySeries(node.history, 'packetLoss', `${node.serverId}-loss`, t('serversPage.probe.packetLoss')),
})));

const probedCount = computed(() => targetCards.value.filter((card) => card.node.sourceCount > 0).length);

function valueClass(kind: 'latency' | 'packetLoss', value: number | null) {
  const tone = valueTone(kind, value);
  return tone ? textToneClass[tone] : 'text-foreground';
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
    const nextConfig = await networkProbeFacilityApi.config({ signal: next.signal });
    if (!guard.isCurrent(requestId)) return;
    // 旧服务端可能不返回三个节拍参数：按默认值补齐，避免表单显示空白。
    config.value = { ...nextConfig, ...normalizeNetworkProbeSettings(nextConfig) };
    resetDraft();
    await loadTargets();
  } catch (err) {
    if (isAbortError(err)) return;
    if (!guard.isCurrent(requestId)) return;
    error.value = err instanceof Error ? err.message : t('applicationsPage.networkProbeLoadFailed');
    notifyError(error.value, err);
  } finally {
    if (guard.isCurrent(requestId)) loading.value = false;
  }
}

/** 加载全部服务器的按目标聚合摘要；失败时保留上次成功数据不清空。 */
async function loadTargets() {
  resultsController?.abort();
  const requestId = resultsGuard.begin();
  const controller = new AbortController();
  resultsController = controller;
  resultsLoading.value = true;
  resultsError.value = '';
  try {
    const next = await networkProbeFacilityApi.targets({ signal: controller.signal }, true);
    if (!resultsGuard.isCurrent(requestId)) return;
    targetMatrix.value = next;
  } catch (err) {
    if (isAbortError(err)) return;
    if (!resultsGuard.isCurrent(requestId)) return;
    resultsError.value = err instanceof Error ? err.message : t('applicationsPage.networkProbeLoadFailed');
    notifyError(resultsError.value, err);
  } finally {
    if (resultsGuard.isCurrent(requestId)) resultsLoading.value = false;
  }
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
  // 自动刷新只重取聚合结果；配置表单有未保存修改时不打断。
  autoRefresh.start(() => {
    if (dirty.value || resultsLoading.value) return;
    void loadTargets();
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
    class="relative grid h-full min-h-0 grid-rows-page gap-4 max-lg:h-auto max-lg:grid-rows-none"
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
        <FacilitySection
          :title="t('applicationsPage.networkProbeScopeTitle')"
          :description="t('applicationsPage.networkProbeScopeHint')"
          data-testid="network-probe-scope-section"
        >
          <div class="grid content-start gap-3">
            <FacilityField class="sm:max-w-xs" :label="t('applicationsPage.networkProbeMode')">
              <Select v-model="draft.mode" :options="modeOptions" :disabled="saving" />
            </FacilityField>

            <div v-if="draft.mode === 'selected'" class="grid gap-2">
              <FacilityField :label="t('applicationsPage.networkProbeSelectServers')" :hint="t('applicationsPage.networkProbeSelectServersHint')">
                <div class="max-h-64 overflow-y-auto pr-1">
                  <ServerMultiPicker v-model="draft.serverIds" variant="compact" :servers="serverOptions" :label="t('applicationsPage.networkProbeSelectServers')" />
                </div>
              </FacilityField>
            </div>
          </div>
        </FacilitySection>

        <!-- 区域二：探测配置（间隔、包数、单包超时 + 保存）。 -->
        <FacilitySection
          :title="t('applicationsPage.networkProbeConfigTitle')"
          :description="t('applicationsPage.networkProbeConfigHint')"
          data-testid="network-probe-config-section"
        >
          <div class="grid gap-3 sm:grid-cols-3">
            <FacilityField
              :label="t('applicationsPage.networkProbeInterval')"
              :hint="t('applicationsPage.networkProbeIntervalHint', { min: networkProbeIntervalMinutes.min, max: networkProbeIntervalMinutes.max, default: networkProbeIntervalMinutes.defaultValue })"
              :error="intervalInvalid ? t('applicationsPage.networkProbeIntervalInvalid', { min: networkProbeIntervalMinutes.min, max: networkProbeIntervalMinutes.max }) : ''"
            >
              <NumberBox
                v-model="intervalNumber"
                :min="networkProbeIntervalMinutes.min"
                :max="networkProbeIntervalMinutes.max"
                :step="1"
                :invalid="intervalInvalid"
                :disabled="saving"
                data-testid="network-probe-interval-minutes"
              />
            </FacilityField>
            <FacilityField
              v-for="item in parameterFields"
              :key="item.field"
              :label="t(item.labelKey)"
              :hint="t(item.hintKey, { min: item.rule.min, max: item.rule.max, default: item.rule.defaultValue })"
              :error="parameterInvalid(item.field) ? t(item.invalidKey, { min: item.rule.min, max: item.rule.max }) : ''"
            >
              <NumberBox
                :model-value="parameterValues[item.field]"
                :min="item.rule.min"
                :max="item.rule.max"
                :step="item.rule.step"
                :invalid="parameterInvalid(item.field)"
                :disabled="saving"
                @update:model-value="setParameter(item.field, $event === null ? '' : String($event))"
              />
            </FacilityField>
          </div>

          <InfoBar v-if="formError" tone="danger">{{ formError }}</InfoBar>
          <div class="flex justify-end">
            <Button size="sm" variant="primary" :loading="saving" :disabled="!dirty" @click="save"><Save />{{ t('applicationsPage.networkProbeSave') }}</Button>
          </div>
        </FacilitySection>
      </div>

      <!-- 区域三：全部服务器结果（每台服务器直接展示聚合值与两张 24h 折线图）。
           结果区不再画第二层卡片：图表自身是 ChartCard 语义的 surface-card，外层只负责分区与滚动。 -->
      <section
        class="relative grid min-h-0 gap-3 p-4 max-lg:grid-rows-none lg:grid-rows-page-top"
        data-testid="network-probe-results-section"
      >
        <FacilitySectionHeader
          :title="t('applicationsPage.networkProbeResultsTitle')"
          :description="t('applicationsPage.networkProbeResultsHint')"
        >
          <template #actions>
            <DataRefreshControl />
            <Button size="sm" variant="ghost" :loading="resultsLoading" @click="loadTargets"><RefreshCcw />{{ t('common.refresh') }}</Button>
          </template>
        </FacilitySectionHeader>

        <LoadingOverlay v-if="resultsLoading && !targetMatrix" :label="t('applicationsPage.networkProbeLoading')" />
        <EmptyState v-else-if="resultsError && !targetMatrix" variant="page" :title="t('common.loadFailed')" :description="resultsError">
          <template #actions>
            <Button size="sm" :loading="resultsLoading" @click="loadTargets"><RefreshCcw />{{ t('common.retry') }}</Button>
          </template>
        </EmptyState>
        <template v-else>
          <div class="grid min-w-0 gap-2">
            <InfoBar v-if="resultsError" tone="warning">{{ t('applicationsPage.networkProbeShowingStale') }}</InfoBar>
            <div class="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
              <span>{{ t('applicationsPage.networkProbeTargetsSummary', { count: targetCards.length, probed: probedCount }) }}</span>
              <span>{{ t('serversPage.probe.lastSample') }}: {{ formatDateTime(targetMatrix?.updatedAt, t('common.never')) }}</span>
            </div>
            <p v-if="targetCards.length && probedCount === 0" class="m-0 text-xs text-muted-foreground">{{ t('applicationsPage.networkProbeNoSourcesHint') }}</p>
          </div>

          <EmptyState
            v-if="!targetCards.length"
            variant="inset"
            :title="t('applicationsPage.networkProbeNoTargets')"
            :description="t('applicationsPage.networkProbeNoTargetsHint')"
          />
          <ul
            v-else
            class="m-0 grid min-h-0 list-none gap-3 overflow-x-hidden overflow-y-auto p-0 lg:pr-1"
            data-testid="probe-target-cards"
          >
            <li
              v-for="card in targetCards"
              :key="card.node.serverId"
              class="grid min-w-0 content-start gap-3 border-t border-divider pt-3 first:border-t-0 first:pt-0"
              data-testid="probe-target-card"
            >
              <div class="flex min-w-0 flex-wrap items-start justify-between gap-2">
                <div class="min-w-0">
                  <strong class="block truncate text-sm text-foreground">{{ card.node.name || card.node.serverId }}</strong>
                  <span class="panel-mono block truncate text-xs text-muted-foreground">{{ card.node.address || t('common.notAvailable') }}</span>
                </div>
                <Badge :tone="probeAggregateOutcomeTone(card.outcome)">{{ t(probeAggregateOutcomeStatusKey(card.outcome)) }}</Badge>
              </div>

              <dl class="m-0 grid min-w-0 grid-cols-2 gap-2 sm:grid-cols-4">
                <div class="min-w-0">
                  <dt class="truncate text-xs text-muted-foreground">{{ t('applicationsPage.networkProbeSourceCountLabel') }}</dt>
                  <dd class="m-0 text-sm font-semibold tabular-nums text-muted-foreground">{{ card.node.sourceCount }}</dd>
                </div>
                <div class="min-w-0">
                  <dt class="truncate text-xs text-muted-foreground">{{ t('serversPage.probe.latency') }}</dt>
                  <dd class="m-0 text-sm font-semibold tabular-nums" :class="valueClass('latency', card.node.latencyMs)">{{ formatLatency(card.node.latencyMs) }}</dd>
                </div>
                <div class="min-w-0">
                  <dt class="truncate text-xs text-muted-foreground">{{ t('serversPage.probe.packetLoss') }}</dt>
                  <dd class="m-0 text-sm font-semibold tabular-nums" :class="valueClass('packetLoss', card.node.packetLossPercent)">{{ formatPacketLoss(card.node.packetLossPercent) }}</dd>
                </div>
                <div class="min-w-0">
                  <dt class="truncate text-xs text-muted-foreground">{{ t('serversPage.probe.sampledAt') }}</dt>
                  <dd class="m-0 truncate text-xs text-secondary-foreground">{{ formatDateTime(card.node.sampledAt, t('common.never')) }}</dd>
                </div>
              </dl>

              <ProbeHistoryCharts
                :latency="card.latencySeries"
                :loss="card.lossSeries"
                :latency-current="card.node.latencyMs"
                :loss-current="card.node.packetLossPercent"
              />
            </li>
          </ul>
        </template>
      </section>
    </template>
  </div>
</template>
