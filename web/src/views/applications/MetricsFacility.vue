<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import { RefreshCcw, Save } from '@/icons';
import { metricsFacilityApi } from '@/api/metricsFacility';
import Button from '@/components/ui/Button.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import NumberBox from '@/components/ui/NumberBox.vue';
import Select from '@/components/ui/Select.vue';
import Skeleton from '@/components/ui/Skeleton.vue';
import FacilityField from '@/components/patterns/FacilityField.vue';
import FacilitySection from '@/components/patterns/FacilitySection.vue';
import InfoBar from '@/components/patterns/InfoBar.vue';
import ServerMultiPicker from '@/components/patterns/ServerMultiPicker.vue';
import { useErrorDialog } from '@/components/ui/errorDialog';
import { useSuccessToast } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { MetricsCleanupSchedule, MetricsFacilityConfig, MetricsFacilityMode, MetricsWarningThresholds } from '@/types/metricsFacility';
import type { ServerDto } from '@/types/servers';
import {
  isMetricsCleanupSchedule,
  isValidMetricsCollectionInterval,
  isValidMetricsRetention,
  metricsCollectionIntervalMin,
  metricsFacilityDefaults,
  parseMetricsPositiveInteger,
} from '@/utils/metricsFacility';
import { createLatestRequestGuard, isAbortError } from '@/views/_shared/requestState';

const props = defineProps<{
  mode: 'facilityDetail' | 'facilityConfig';
  servers: ServerDto[];
}>();

const { t } = useI18n();
const notifyError = useErrorDialog();
const notifySuccess = useSuccessToast();

const config = ref<MetricsFacilityConfig | null>(null);
const loading = ref(false);
const initialLoading = computed(() => loading.value && !config.value);
const saving = ref(false);
const error = ref('');
const formError = ref('');
const draft = reactive<{
  mode: MetricsFacilityMode;
  serverIds: string[];
  retentionDays: string;
  collectionIntervalSeconds: string;
  cleanupSchedule: MetricsCleanupSchedule;
}>({
  mode: metricsFacilityDefaults.mode,
  serverIds: [],
  retentionDays: String(metricsFacilityDefaults.retentionDays),
  collectionIntervalSeconds: String(metricsFacilityDefaults.collectionIntervalSeconds),
  cleanupSchedule: metricsFacilityDefaults.cleanupSchedule,
});
const guard = createLatestRequestGuard();
let controller: AbortController | null = null;

const serverOptions = computed(() => props.servers.map((server) => ({
  id: server.id,
  name: server.name,
  description: [server.ipv4 || server.ipv6 || server.host].filter(Boolean).join(' / '),
  status: server.reachable ? 'reachable' : 'unreachable',
})));

const modeOptions = computed(() => [
  { value: 'all', label: t('applicationsPage.metricsModeAll') },
  { value: 'selected', label: t('applicationsPage.metricsModeSelected') },
]);

const cleanupOptions = computed(() => [
  { value: 'hourly', label: t('applicationsPage.metricsCleanupHourly') },
  { value: 'daily', label: t('applicationsPage.metricsCleanupDaily') },
  { value: 'weekly', label: t('applicationsPage.metricsCleanupWeekly') },
]);

const retentionValue = computed(() => parseMetricsPositiveInteger(draft.retentionDays));
const intervalValue = computed(() => parseMetricsPositiveInteger(draft.collectionIntervalSeconds));

type ThresholdKey = keyof MetricsWarningThresholds;
const thresholdFields: Array<{ key: ThresholdKey; labelKey: string; percent: boolean }> = [
  { key: 'cpuUsagePercent', labelKey: 'applicationsPage.metricsThresholdCpu', percent: true },
  { key: 'memoryUsagePercent', labelKey: 'applicationsPage.metricsThresholdMemory', percent: true },
  { key: 'diskReadBytesPerSecond', labelKey: 'applicationsPage.metricsThresholdDiskRead', percent: false },
  { key: 'diskWriteBytesPerSecond', labelKey: 'applicationsPage.metricsThresholdDiskWrite', percent: false },
  { key: 'networkRxBytesPerSecond', labelKey: 'applicationsPage.metricsThresholdNetworkRx', percent: false },
  { key: 'networkTxBytesPerSecond', labelKey: 'applicationsPage.metricsThresholdNetworkTx', percent: false },
];
const thresholdDraft = reactive<Record<ThresholdKey, number | null>>({
  cpuUsagePercent: null,
  memoryUsagePercent: null,
  diskReadBytesPerSecond: null,
  diskWriteBytesPerSecond: null,
  networkRxBytesPerSecond: null,
  networkTxBytesPerSecond: null,
});
function thresholdInvalid(key: ThresholdKey): boolean {
  const value = thresholdDraft[key];
  if (value === null) return false;
  if (!Number.isFinite(value) || value <= 0) return true;
  return thresholdFields.find((item) => item.key === key)?.percent === true && value > 100;
}
const thresholdsInvalid = computed(() => thresholdFields.some((item) => thresholdInvalid(item.key)));
/** NumberBox 适配：draft 仍是字符串（校验/保存沿用 parse），空值映射为 null。 */
const retentionNumber = computed<number | null>({
  get: () => retentionValue.value,
  set: (value: number | null) => { draft.retentionDays = value === null ? '' : String(value); },
});
const intervalNumber = computed<number | null>({
  get: () => intervalValue.value,
  set: (value: number | null) => { draft.collectionIntervalSeconds = value === null ? '' : String(value); },
});
const retentionInvalid = computed(() => retentionValue.value === null || !isValidMetricsRetention(retentionValue.value));
const intervalInvalid = computed(() => intervalValue.value === null || !isValidMetricsCollectionInterval(intervalValue.value));

const dirty = computed(() => {
  const current = config.value;
  if (!current) return true;
  if (draft.mode !== current.mode) return true;
  if (JSON.stringify([...draft.serverIds].sort()) !== JSON.stringify([...current.serverIds].sort())) return true;
  if (draft.cleanupSchedule !== current.cleanupSchedule) return true;
  if (thresholdFields.some((item) => (current.thresholds?.[item.key] ?? null) !== thresholdDraft[item.key])) return true;
  return retentionValue.value !== current.retentionDays
    || intervalValue.value !== current.collectionIntervalSeconds;
});

function resetDraft() {
  const current = config.value;
  draft.mode = current?.mode ?? metricsFacilityDefaults.mode;
  draft.serverIds = [...(current?.serverIds ?? [])];
  draft.retentionDays = String(current?.retentionDays ?? metricsFacilityDefaults.retentionDays);
  draft.collectionIntervalSeconds = String(current?.collectionIntervalSeconds ?? metricsFacilityDefaults.collectionIntervalSeconds);
  draft.cleanupSchedule = current?.cleanupSchedule ?? metricsFacilityDefaults.cleanupSchedule;
  for (const field of thresholdFields) {
    thresholdDraft[field.key] = current?.thresholds?.[field.key] ?? null;
  }
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
    const nextConfig = await metricsFacilityApi.config({ signal: next.signal });
    if (!guard.isCurrent(requestId)) return;
    config.value = nextConfig;
    resetDraft();
  } catch (err) {
    if (isAbortError(err)) return;
    if (!guard.isCurrent(requestId)) return;
    // 配置加载失败：EmptyState 就地展示并提供重试，不进弹窗/toast。
    error.value = err instanceof Error ? err.message : t('applicationsPage.metricsLoadFailed');
  } finally {
    if (guard.isCurrent(requestId)) loading.value = false;
  }
}

async function save() {
  if (saving.value) return;
  formError.value = '';
  if (draft.mode === 'selected' && draft.serverIds.length === 0) {
    formError.value = t('applicationsPage.metricsServersRequired');
    return;
  }
  if (retentionInvalid.value) {
    formError.value = t('applicationsPage.metricsRetentionInvalid');
    return;
  }
  if (intervalInvalid.value) {
    formError.value = t('applicationsPage.metricsIntervalInvalid');
    return;
  }
  if (!isMetricsCleanupSchedule(draft.cleanupSchedule)) return;
  if (thresholdsInvalid.value) {
    formError.value = t('applicationsPage.metricsThresholdInvalid');
    return;
  }
  const thresholds: MetricsWarningThresholds = {};
  for (const field of thresholdFields) {
    const value = thresholdDraft[field.key];
    if (value !== null) thresholds[field.key] = value;
  }
  saving.value = true;
  try {
    const saved = await metricsFacilityApi.saveConfig({
      mode: draft.mode,
      serverIds: [...draft.serverIds],
      retentionDays: retentionValue.value ?? metricsFacilityDefaults.retentionDays,
      collectionIntervalSeconds: intervalValue.value ?? metricsFacilityDefaults.collectionIntervalSeconds,
      cleanupSchedule: draft.cleanupSchedule,
      thresholds,
    });
    config.value = saved;
    resetDraft();
    notifySuccess(t('applicationsPage.metricsSaved'), saved);
    await load();
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    saving.value = false;
  }
}

onMounted(() => { void load(); });
onBeforeUnmount(() => {
  controller?.abort();
  guard.invalidate();
});
</script>

<template>
  <div class="relative grid content-start gap-4" data-testid="metrics-facility" :aria-busy="initialLoading || undefined">
    <EmptyState v-if="error && !config" variant="page" :title="t('common.loadFailed')" :description="error">
      <template #actions>
        <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button>
      </template>
    </EmptyState>

    <template v-else>
      <!-- 区域一：采集范围（模式 + 节点选择），与配置区上下堆叠。 -->
      <FacilitySection
        :title="t('applicationsPage.metricsScopeTitle')"
        :description="t('applicationsPage.metricsScopeHint')"
        data-testid="metrics-scope-section"
      >
        <div class="grid content-start gap-3">
          <FacilityField class="sm:max-w-xs" :label="t('applicationsPage.metricsMode')">
            <Skeleton v-if="initialLoading" class="h-8 w-full" />
            <Select v-else v-model="draft.mode" :options="modeOptions" :disabled="saving" />
          </FacilityField>

          <div v-if="!initialLoading && draft.mode === 'selected'" class="grid gap-2">
            <FacilityField :label="t('applicationsPage.metricsSelectServers')" :hint="t('applicationsPage.metricsSelectServersHint')">
              <div class="max-h-64 overflow-y-auto pr-1">
                <ServerMultiPicker v-model="draft.serverIds" variant="compact" :servers="serverOptions" :label="t('applicationsPage.metricsSelectServers')" />
              </div>
            </FacilityField>
          </div>
        </div>
      </FacilitySection>

      <!-- 区域二：采集配置（保留天数、采集间隔、清理计划 + 保存）。 -->
      <FacilitySection
        :title="t('applicationsPage.metricsConfigTitle')"
        :description="t('applicationsPage.metricsConfigHint')"
        data-testid="metrics-config-form"
      >
        <div class="grid content-start gap-3">
          <div class="grid gap-3 sm:grid-cols-2">
            <FacilityField :label="t('applicationsPage.metricsCleanupSchedule')">
              <Skeleton v-if="initialLoading" class="h-8 w-full" />
              <Select v-else v-model="draft.cleanupSchedule" :options="cleanupOptions" :disabled="saving" />
            </FacilityField>
            <FacilityField
              :label="t('applicationsPage.metricsRetention')"
              :error="!initialLoading && retentionInvalid ? t('applicationsPage.metricsRetentionInvalid') : ''"
            >
              <Skeleton v-if="initialLoading" class="h-8 w-full" />
              <NumberBox
                v-else
                v-model="retentionNumber"
                :min="1"
                :step="1"
                :invalid="retentionInvalid"
                :disabled="saving"
                data-testid="metrics-retention"
              />
            </FacilityField>
            <FacilityField
              :label="t('applicationsPage.metricsInterval')"
              :hint="t('applicationsPage.metricsIntervalHint', { min: metricsCollectionIntervalMin, default: metricsFacilityDefaults.collectionIntervalSeconds })"
              :error="!initialLoading && intervalInvalid ? t('applicationsPage.metricsIntervalInvalid') : ''"
            >
              <Skeleton v-if="initialLoading" class="h-8 w-full" />
              <NumberBox
                v-else
                v-model="intervalNumber"
                :min="metricsCollectionIntervalMin"
                :step="1"
                :invalid="intervalInvalid"
                :disabled="saving"
                data-testid="metrics-interval"
              />
            </FacilityField>
          </div>
          <div class="grid gap-3 border-t border-divider pt-3">
            <div class="grid gap-1">
              <h4 class="m-0 text-body-strong text-foreground">{{ t('applicationsPage.metricsThresholdsTitle') }}</h4>
              <p class="m-0 text-caption leading-5 text-muted-foreground">{{ t('applicationsPage.metricsThresholdsHint') }}</p>
            </div>
            <div class="grid gap-3 sm:grid-cols-2">
              <FacilityField
                v-for="field in thresholdFields"
                :key="field.key"
                :label="t(field.labelKey)"
                :hint="field.percent ? '%' : 'B/s'"
                :error="!initialLoading && thresholdInvalid(field.key) ? t('applicationsPage.metricsThresholdInvalid') : ''"
              >
                <Skeleton v-if="initialLoading" class="h-8 w-full" />
                <NumberBox
                  v-else
                  v-model="thresholdDraft[field.key]"
                  :min="0"
                  :step="1"
                  :invalid="thresholdInvalid(field.key)"
                  :disabled="saving"
                />
              </FacilityField>
            </div>
          </div>
          <InfoBar v-if="formError" tone="danger">{{ formError }}</InfoBar>
        </div>

        <div class="flex justify-end">
          <Button size="sm" variant="primary" :loading="saving" :disabled="initialLoading || !dirty" @click="save"><Save />{{ t('applicationsPage.metricsSave') }}</Button>
        </div>
      </FacilitySection>
    </template>
  </div>
</template>
