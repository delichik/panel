<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import { RefreshCcw, Save } from '@lucide/vue';
import { metricsFacilityApi } from '@/api/metricsFacility';
import Button from '@/components/ui/Button.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import Input from '@/components/ui/Input.vue';
import LoadingOverlay from '@/components/ui/LoadingOverlay.vue';
import Select from '@/components/ui/Select.vue';
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
  <div class="relative grid h-full min-h-0 gap-4 max-lg:h-auto lg:grid-cols-2" data-testid="metrics-facility">
    <LoadingOverlay v-if="loading && !config" :label="t('applicationsPage.metricsLoading')" />

    <EmptyState v-if="error && !config" variant="page" class="lg:col-span-2" :title="t('common.loadFailed')" :description="error">
      <template #actions>
        <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button>
      </template>
    </EmptyState>

    <template v-else>
      <!-- 区域一：采集范围（模式 + 节点选择）。 -->
      <section class="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-3 rounded-xl border border-border bg-card p-4 max-lg:grid-rows-none" data-testid="metrics-scope-section">
        <div>
          <h2 class="m-0 text-base font-semibold text-foreground">{{ t('applicationsPage.metricsScopeTitle') }}</h2>
          <p class="m-0 mt-1 text-sm leading-6 text-muted-foreground">{{ t('applicationsPage.metricsScopeHint') }}</p>
        </div>

        <div class="grid min-h-0 content-start gap-3 max-lg:content-normal">
          <label class="grid gap-1 sm:max-w-xs">
            <span class="text-xs font-medium text-muted-foreground">{{ t('applicationsPage.metricsMode') }}</span>
            <Select v-model="draft.mode" :options="modeOptions" :disabled="saving" />
          </label>

          <div v-if="draft.mode === 'selected'" class="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-2 max-lg:grid-rows-none">
            <div>
              <span class="text-xs font-medium text-muted-foreground">{{ t('applicationsPage.metricsSelectServers') }}</span>
              <p class="m-0 mt-1 text-xs leading-5 text-muted-foreground">{{ t('applicationsPage.metricsSelectServersHint') }}</p>
            </div>
            <div class="min-h-0 lg:overflow-y-auto lg:pr-1">
              <ServerMultiPicker v-model="draft.serverIds" variant="compact" :servers="serverOptions" :label="t('applicationsPage.metricsSelectServers')" />
            </div>
          </div>
        </div>
      </section>

      <!-- 区域二：采集配置（保留天数、采集间隔、清理计划 + 保存）。 -->
      <section class="grid min-h-0 grid-rows-[auto_minmax(0,1fr)_auto] gap-3 rounded-xl border border-border bg-card p-4 max-lg:grid-rows-none" data-testid="metrics-config-form">
        <div>
          <h2 class="m-0 text-base font-semibold text-foreground">{{ t('applicationsPage.metricsConfigTitle') }}</h2>
          <p class="m-0 mt-1 text-sm leading-6 text-muted-foreground">{{ t('applicationsPage.metricsConfigHint') }}</p>
        </div>

        <div class="grid min-h-0 content-start gap-3 max-lg:content-normal">
          <div class="grid gap-3 sm:grid-cols-2">
            <label class="grid gap-1">
              <span class="text-xs font-medium text-muted-foreground">{{ t('applicationsPage.metricsCleanupSchedule') }}</span>
              <Select v-model="draft.cleanupSchedule" :options="cleanupOptions" :disabled="saving" />
            </label>
            <label class="grid gap-1">
              <span class="text-xs font-medium text-muted-foreground">{{ t('applicationsPage.metricsRetention') }}</span>
              <Input
                v-model="draft.retentionDays"
                type="number"
                inputmode="numeric"
                :min="1"
                step="1"
                :invalid="retentionInvalid"
                :disabled="saving"
                data-testid="metrics-retention"
              />
              <span v-if="retentionInvalid" class="text-xs leading-4 text-danger">{{ t('applicationsPage.metricsRetentionInvalid') }}</span>
            </label>
            <label class="grid gap-1">
              <span class="text-xs font-medium text-muted-foreground">{{ t('applicationsPage.metricsInterval') }}</span>
              <Input
                v-model="draft.collectionIntervalSeconds"
                type="number"
                inputmode="numeric"
                :min="metricsCollectionIntervalMin"
                step="1"
                :invalid="intervalInvalid"
                :disabled="saving"
                data-testid="metrics-interval"
              />
              <span class="text-xs leading-4 text-muted-foreground">{{ t('applicationsPage.metricsIntervalHint', { min: metricsCollectionIntervalMin, default: metricsFacilityDefaults.collectionIntervalSeconds }) }}</span>
              <span v-if="intervalInvalid" class="text-xs leading-4 text-danger">{{ t('applicationsPage.metricsIntervalInvalid') }}</span>
            </label>
          </div>
          <p v-if="formError" role="alert" class="m-0 text-sm text-danger">{{ formError }}</p>
        </div>

        <div class="flex justify-end">
          <Button size="sm" variant="primary" :loading="saving" :disabled="!dirty" @click="save"><Save />{{ t('applicationsPage.metricsSave') }}</Button>
        </div>
      </section>
    </template>
  </div>
</template>
