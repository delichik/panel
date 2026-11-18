<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import { RefreshCcw, Save } from '@/icons';
import { metricsFacilityApi } from '@/api/metricsFacility';
import Button from '@/components/ui/Button.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import LoadingOverlay from '@/components/ui/LoadingOverlay.vue';
import NumberBox from '@/components/ui/NumberBox.vue';
import Select from '@/components/ui/Select.vue';
import FacilityField from '@/components/patterns/FacilityField.vue';
import FacilitySection from '@/components/patterns/FacilitySection.vue';
import InfoBar from '@/components/patterns/InfoBar.vue';
import ServerMultiPicker from '@/components/patterns/ServerMultiPicker.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { MetricsCleanupSchedule, MetricsFacilityConfig, MetricsFacilityMode } from '@/types/metricsFacility';
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
const notifyError = useErrorToast();
const notifySuccess = useSuccessToast();

const config = ref<MetricsFacilityConfig | null>(null);
const loading = ref(false);
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
    error.value = err instanceof Error ? err.message : t('applicationsPage.metricsLoadFailed');
    notifyError(error.value, err);
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
  saving.value = true;
  try {
    const saved = await metricsFacilityApi.saveConfig({
      mode: draft.mode,
      serverIds: [...draft.serverIds],
      retentionDays: retentionValue.value ?? metricsFacilityDefaults.retentionDays,
      collectionIntervalSeconds: intervalValue.value ?? metricsFacilityDefaults.collectionIntervalSeconds,
      cleanupSchedule: draft.cleanupSchedule,
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
  <div class="relative grid content-start gap-4" data-testid="metrics-facility">
    <LoadingOverlay v-if="loading && !config" :label="t('applicationsPage.metricsLoading')" />

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
            <Select v-model="draft.mode" :options="modeOptions" :disabled="saving" />
          </FacilityField>

          <div v-if="draft.mode === 'selected'" class="grid gap-2">
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
              <Select v-model="draft.cleanupSchedule" :options="cleanupOptions" :disabled="saving" />
            </FacilityField>
            <FacilityField
              :label="t('applicationsPage.metricsRetention')"
              :error="retentionInvalid ? t('applicationsPage.metricsRetentionInvalid') : ''"
            >
              <NumberBox
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
              :error="intervalInvalid ? t('applicationsPage.metricsIntervalInvalid') : ''"
            >
              <NumberBox
                v-model="intervalNumber"
                :min="metricsCollectionIntervalMin"
                :step="1"
                :invalid="intervalInvalid"
                :disabled="saving"
                data-testid="metrics-interval"
              />
            </FacilityField>
          </div>
          <InfoBar v-if="formError" tone="danger">{{ formError }}</InfoBar>
        </div>

        <div class="flex justify-end">
          <Button size="sm" variant="primary" :loading="saving" :disabled="!dirty" @click="save"><Save />{{ t('applicationsPage.metricsSave') }}</Button>
        </div>
      </FacilitySection>
    </template>
  </div>
</template>
