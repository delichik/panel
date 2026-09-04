<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { RefreshCcw } from '@lucide/vue';
import { applicationOperationsApi } from '@/api/applicationOperations';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import LoadingOverlay from '@/components/ui/LoadingOverlay.vue';
import PaginationBar from '@/components/ui/PaginationBar.vue';
import SearchInput from '@/components/ui/SearchInput.vue';
import Select from '@/components/ui/Select.vue';
import Skeleton from '@/components/ui/Skeleton.vue';
import StatusBadge from '@/components/ui/StatusBadge.vue';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import MasterDetailLayout from '@/components/templates/MasterDetailLayout.vue';
import { useErrorToast } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { ApplicationOperationDetailDto, ApplicationOperationDto, ApplicationOperationTargetDto } from '@/types/applicationOperations';
import { useOverlayBehavior } from '@/composables/useOverlayBehavior';
import { createLatestRequestGuard, normalizePage } from '@/views/_shared/requestState';
import { formatDateTime } from '@/utils/datetime';

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const notifyError = useErrorToast();

const FACILITY_REVERSE_PROXY_APPLICATION_ID = 'facility-reverse-proxy';

function applicationLabel(operation: Pick<ApplicationOperationDto, 'applicationId' | 'applicationName'>): string {
  if (operation.applicationId === FACILITY_REVERSE_PROXY_APPLICATION_ID) {
    return t('applicationsPage.entranceProxyFacility');
  }
  return operation.applicationName || t('common.notAvailable');
}

const rows = ref<ApplicationOperationDto[]>([]);
const total = ref(0);
const page = ref(normalizePage(route.query.page));
const pageSize = 20;
const search = ref(String(route.query.applicationId || ''));
const status = ref(String(route.query.status || ''));
const source = ref(String(route.query.source || ''));
const loading = ref(false);
const error = ref('');

const selectedOperationId = ref(String(route.query.operationId || ''));
const detail = ref<ApplicationOperationDetailDto | null>(null);
const detailLoading = ref(false);
const detailError = ref('');
const drawerTarget = ref<ApplicationOperationTargetDto | null>(null);
const listRequests = createLatestRequestGuard();
const detailRequests = createLatestRequestGuard();
let filterTimer: ReturnType<typeof setTimeout> | null = null;
const drawer = ref<HTMLElement | null>(null);

function closeDrawer() {
  drawerTarget.value = null;
}

const { onKeydown: onDrawerKeydown } = useOverlayBehavior({
  open: () => Boolean(drawerTarget.value),
  containerRef: drawer,
  onClose: closeDrawer,
  lockScroll: true,
});

const selectedOperationMissing = computed(() => Boolean(selectedOperationId.value) && !rows.value.some((item) => item.operationId === selectedOperationId.value) && !detailLoading.value && !detailError.value);

function clearSelectedOperation() {
  selectedOperationId.value = '';
}

const statusOptions = computed(() => [
  { label: t('applicationOperationsPage.filter.allStatuses'), value: '' },
  { label: t('applicationOperationsPage.status.queued'), value: 'queued' },
  { label: t('applicationOperationsPage.status.running'), value: 'running' },
  { label: t('applicationOperationsPage.status.succeeded'), value: 'succeeded' },
  { label: t('applicationOperationsPage.status.failed'), value: 'failed' },
  { label: t('applicationOperationsPage.status.partial_failed'), value: 'partial_failed' },
  { label: t('applicationOperationsPage.status.cancelled'), value: 'cancelled' },
]);

const sourceOptions = computed(() => [
  { label: t('applicationOperationsPage.filter.allSources'), value: '' },
  { label: t('applicationOperationsPage.source.user'), value: 'user' },
  { label: t('applicationOperationsPage.source.system'), value: 'system' },
  { label: t('applicationOperationsPage.source.scheduler'), value: 'scheduler' },
]);

const failedTargets = computed<ApplicationOperationTargetDto[]>(() => {
  const targets = detail.value?.targets ?? [];
  return targets.filter((target) => target.status === 'failed' || target.errorCode || target.errorMessage || target.errorDetail);
});

function actionLabel(value: string): string {
  if (!value) return t('common.notAvailable');
  const key = `applicationOperationsPage.action.${value}`;
  const label = t(key);
  return label === key ? value : label;
}

function sourceLabel(value: string): string {
  const key = `applicationOperationsPage.source.${value}`;
  const label = t(key);
  return label === key ? value : label;
}

function statusLabel(value: string): string {
  const key = `applicationOperationsPage.status.${value}`;
  const label = t(key);
  return label === key ? value : label;
}

function serverLabel(target: Pick<ApplicationOperationTargetDto, 'serverName' | 'serverId' | 'id'>): string {
  return target.serverName || target.serverId || target.id || t('common.notAvailable');
}

function desiredStateLabel(value?: string): string {
  if (!value) return t('common.notAvailable');
  const key = `applicationOperationsPage.desiredState.${value}`;
  const label = t(key);
  return label === key ? value : label;
}

function stageLabel(value: string): string {
  if (!value) return t('common.notAvailable');
  const key = `applicationOperationsPage.stage.${value}`;
  const label = t(key);
  return label === key ? value : label;
}

function targetProgress(row: ApplicationOperationDto): string {
  if (!row.targetTotal) return t('common.notAvailable');
  return t('applicationOperationsPage.targetProgress', { succeeded: row.targetSucceeded, failed: row.targetFailed, total: row.targetTotal });
}

function isConsistent(target: ApplicationOperationTargetDto): boolean {
  return target.status === 'consistent';
}

function mismatchText(target: ApplicationOperationTargetDto): string {
  const parts: string[] = [];
  const desired = target.desiredState;
  const actual = target.observedState;
  if (desired === 'running' && (actual === 'stopped' || actual === 'missing' || actual === 'failed' || actual === '')) {
    if (actual === 'missing') {
      parts.push(t('applicationOperationsPage.mismatch.neverDeployed'));
    } else if (target.observedError) {
      parts.push(t('applicationOperationsPage.mismatch.unexpectedExit', { error: target.observedError }));
    } else {
      parts.push(t('applicationOperationsPage.mismatch.notRunning'));
    }
  } else if (desired === 'stopped' && actual === 'running') {
    parts.push(t('applicationOperationsPage.mismatch.shouldStop'));
  }
  if (target.desiredGeneration && target.observedGeneration && target.desiredGeneration !== target.observedGeneration) {
    parts.push(t('applicationOperationsPage.mismatch.generation', { desired: target.desiredGeneration, actual: target.observedGeneration }));
  } else if (target.desiredSpecHash && target.observedSpecHash && target.desiredSpecHash !== target.observedSpecHash) {
    parts.push(t('applicationOperationsPage.mismatch.config'));
  }
  return parts.join('；');
}

function desiredActualText(target: ApplicationOperationTargetDto): string {
  const desired = `${desiredStateLabel(target.desiredState)}${target.desiredGeneration ? t('applicationOperationsPage.mismatch.generationShort', { generation: target.desiredGeneration }) : ''}`;
  const actual = target.observedState
    ? `${desiredStateLabel(target.observedState)}${target.observedGeneration ? t('applicationOperationsPage.mismatch.generationShort', { generation: target.observedGeneration }) : ''}`
    : t('common.notAvailable');
  return `${t('applicationOperationsPage.desiredState')} ${desired} / ${t('applicationOperationsPage.mismatch.actual')} ${actual}`;
}

function targetErrorText(target: ApplicationOperationTargetDto): string {
  return [target.errorMessage, target.errorDetail].map((value) => value?.trim()).filter(Boolean).join('\n');
}

function stageDuration(stage: { startedAt?: string; finishedAt?: string }): string {
  if (!stage.startedAt || !stage.finishedAt) return '';
  const seconds = Math.max(0, Math.round((Date.parse(stage.finishedAt) - Date.parse(stage.startedAt)) / 1000));
  if (seconds < 60) return t('applicationOperationsPage.durationSeconds', { seconds });
  const minutes = Math.floor(seconds / 60);
  const rest = seconds % 60;
  return t('applicationOperationsPage.durationMinutes', { minutes, seconds: rest });
}

watch([search, status, source], () => {
  selectedOperationId.value = '';
  detail.value = null;
  drawerTarget.value = null;
  if (filterTimer) clearTimeout(filterTimer);
  filterTimer = setTimeout(() => {
    if (page.value !== 1) {
      page.value = 1;
      return;
    }
    syncQueryAndLoad();
  }, 250);
});

watch(page, () => {
  selectedOperationId.value = '';
  detail.value = null;
  drawerTarget.value = null;
  syncQueryAndLoad();
});

watch(selectedOperationId, (operationId) => {
  drawerTarget.value = null;
  if (!operationId) {
    detail.value = null;
    detailError.value = '';
    return;
  }
  const row = rows.value.find((item) => item.operationId === operationId);
  if (row) void openDetail(row);
});

function syncQueryAndLoad() {
  void router.replace({
    query: {
      ...route.query,
      applicationId: search.value || undefined,
      status: status.value || undefined,
      source: source.value || undefined,
      operationId: selectedOperationId.value || undefined,
      page: page.value > 1 ? String(page.value) : undefined,
    },
  });
  void load();
}

async function load() {
  const requestId = listRequests.begin();
  loading.value = true;
  error.value = '';
  try {
    const result = await applicationOperationsApi.list({
      applicationId: search.value,
      status: status.value,
      source: source.value,
      page: page.value,
      pageSize,
    });
    if (!listRequests.isCurrent(requestId)) return;
    rows.value = result.items;
    total.value = result.total;
    if (result.page && result.page !== page.value) page.value = result.page;
    const selected = rows.value.find((item) => item.operationId === selectedOperationId.value);
    if (selected && (!detail.value || detail.value.operation.operationId !== selected.operationId)) {
      void openDetail(selected);
    } else if (!selected) {
      detail.value = null;
    }
  } catch (err) {
    if (!listRequests.isCurrent(requestId)) return;
    error.value = err instanceof Error ? err.message : t('applicationOperationsPage.loadFailed');
    notifyError(error.value);
    rows.value = [];
    total.value = 0;
  } finally {
    if (listRequests.isCurrent(requestId)) loading.value = false;
  }
}

async function openDetail(row: ApplicationOperationDto) {
  const requestId = detailRequests.begin();
  detailLoading.value = true;
  detailError.value = '';
  detail.value = null;
  drawerTarget.value = null;
  try {
    const result = await applicationOperationsApi.get(row.operationId);
    if (!detailRequests.isCurrent(requestId)) return;
    detail.value = result;
  } catch (err) {
    if (!detailRequests.isCurrent(requestId)) return;
    detailError.value = err instanceof Error ? err.message : t('applicationOperationsPage.detailLoadFailed');
    notifyError(detailError.value);
  } finally {
    if (detailRequests.isCurrent(requestId)) detailLoading.value = false;
  }
}

function retryDetail() {
  const row = rows.value.find((item) => item.operationId === selectedOperationId.value);
  if (row) void openDetail(row);
}

onMounted(load);

onBeforeUnmount(() => {
  if (filterTimer) clearTimeout(filterTimer);
});
</script>

<template>
  <ConsolePage :title="t('routes.applicationOperations.title')" :description="t('routes.applicationOperations.description')">
    <MasterDetailLayout class="h-full min-h-[640px]">
      <template #master>
        <aside class="grid min-h-0 min-w-0 grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden rounded-2xl border border-border bg-card">
          <div class="grid min-w-0 gap-3 border-b border-border p-4">
            <SearchInput v-model="search" clearable :label="t('applicationOperationsPage.applicationFilter')" :placeholder="t('applicationOperationsPage.applicationFilterPlaceholder')" :clear-label="t('common.clearSearch')" />
            <div class="grid grid-cols-2 gap-3">
              <Select v-model="status" :options="statusOptions" :placeholder="t('common.status')" />
              <Select v-model="source" :options="sourceOptions" :placeholder="t('applicationOperationsPage.column.source')" />
            </div>
            <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
          </div>
          <div class="motion-stagger min-h-0 min-w-0 overflow-y-auto overflow-x-hidden p-2">
            <div v-if="loading && !rows.length" class="grid gap-2">
              <Skeleton v-for="item in 6" :key="item" class="h-24" />
            </div>
            <EmptyState v-else-if="error && !rows.length" :title="t('common.loadFailed')" :description="error">
              <template #actions>
                <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button>
              </template>
            </EmptyState>
            <EmptyState v-else-if="!rows.length" :title="t('applicationOperationsPage.empty')" :description="t('applicationOperationsPage.emptyHint')" />
            <button v-for="row in rows" v-else :key="row.operationId" type="button" class="motion-list-item mb-2 grid w-full min-w-0 gap-2 overflow-hidden rounded-xl border p-3 text-left text-sm hover:bg-accent" :class="selectedOperationId === row.operationId ? 'border-border-strong bg-muted' : 'border-transparent'" :aria-current="selectedOperationId === row.operationId ? 'true' : undefined" @click="selectedOperationId = row.operationId">
              <div class="grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-start gap-2 max-[420px]:grid-cols-1">
                <div class="grid min-w-0 gap-0.5">
                  <strong class="min-w-0 truncate text-foreground">{{ applicationLabel(row) }}</strong>
                  <span class="min-w-0 truncate text-xs text-muted-foreground">{{ actionLabel(row.action) }}</span>
                </div>
                <StatusBadge class="max-w-full shrink-0 justify-self-start whitespace-nowrap" :status="row.status" domain="operation" :label="statusLabel(row.status)" />
              </div>
              <div v-if="row.targetServers?.length" class="flex min-w-0 flex-wrap gap-1">
                <Badge v-for="server in row.targetServers" :key="server" class="max-w-full shrink-0 whitespace-nowrap">{{ server }}</Badge>
              </div>
              <div v-else class="min-w-0 truncate text-xs text-muted-foreground">{{ targetProgress(row) }}</div>
              <p v-if="row.failureSummary" class="m-0 min-w-0 truncate text-xs text-danger">{{ row.failureSummary }}</p>
              <div class="flex min-w-0 items-center justify-between gap-2 text-xs text-muted-foreground">
                <span class="min-w-0 truncate">{{ sourceLabel(row.source) }}</span>
                <span class="shrink-0">{{ formatDateTime(row.latestAt, '') }}</span>
              </div>
            </button>
          </div>
          <PaginationBar v-model:page="page" class="min-w-0 overflow-hidden px-3" :page-size="pageSize" :total="total" :loading="loading" :previous-label="t('common.previous')" :next-label="t('common.next')">
            <template #summary="{ page: currentPage, pageCount }">
              {{ t('applicationOperationsPage.paginationSummary', { page: currentPage, pages: pageCount, total }) }}
            </template>
          </PaginationBar>
        </aside>
      </template>

      <template #detail>
        <main class="grid min-h-0 min-w-0 overflow-hidden">
          <EmptyState v-if="!selectedOperationId" :title="t('applicationOperationsPage.selectRecord')" :description="t('applicationOperationsPage.selectRecordHint')" />
          <EmptyState v-else-if="selectedOperationMissing" :title="t('applicationOperationsPage.operationNotOnPage')" :description="t('applicationOperationsPage.operationNotOnPageHint')">
            <template #actions>
              <Button size="sm" @click="clearSelectedOperation">{{ t('applicationOperationsPage.clearSelection') }}</Button>
            </template>
          </EmptyState>
          <EmptyState v-else-if="detailError" :title="t('applicationOperationsPage.detailLoadFailed')" :description="detailError">
            <template #actions>
              <Button size="sm" :loading="detailLoading" @click="retryDetail"><RefreshCcw />{{ t('common.retry') }}</Button>
            </template>
          </EmptyState>
          <div v-else-if="detailLoading" class="relative grid min-h-64 place-items-center">
            <LoadingOverlay :label="t('applicationOperationsPage.loadingDetail')" />
          </div>
          <article v-else-if="detail" class="grid min-h-0 min-w-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-2xl border border-border bg-card">
            <header class="grid min-w-0 gap-3 border-b border-border p-5">
              <div class="grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-start gap-2 max-[420px]:grid-cols-1">
                <h2 class="m-0 min-w-0 truncate text-xl font-semibold">{{ applicationLabel(detail.operation) }}</h2>
                <StatusBadge class="max-w-full shrink-0 justify-self-start whitespace-nowrap" :status="detail.operation.status" domain="operation" :label="statusLabel(detail.operation.status)" />
              </div>
              <p class="m-0 min-w-0 truncate text-sm text-muted-foreground">{{ actionLabel(detail.operation.action) }}，{{ sourceLabel(detail.operation.source) }}</p>
              <p class="m-0 min-w-0 truncate text-xs text-muted-foreground">
                {{ t('applicationOperationsPage.startedAt') }}：{{ formatDateTime(detail.operation.startedAt, t('common.notAvailable')) }}
                <template v-if="detail.operation.finishedAt">，{{ t('applicationOperationsPage.finishedAt') }}：{{ formatDateTime(detail.operation.finishedAt, t('common.notAvailable')) }}</template>
              </p>
              <p class="m-0 min-w-0 truncate text-xs text-muted-foreground">{{ t('applicationOperationsPage.resultLine', { total: detail.operation.targetTotal, succeeded: detail.operation.targetSucceeded, failed: detail.operation.targetFailed }) }}</p>
              <div v-if="failedTargets.length" class="grid min-w-0 gap-2 rounded-xl border border-danger-border bg-danger-bg p-3 text-sm text-danger">
                <strong>{{ t('applicationOperationsPage.failedTargets') }}</strong>
                <div v-for="target in failedTargets" :key="target.id" class="grid min-w-0 gap-0.5">
                  <span class="font-medium">{{ serverLabel(target) }}</span>
                  <span v-if="target.errorCode" class="text-xs">{{ t('applicationOperationsPage.errorCode') }}：{{ target.errorCode }}</span>
                  <span v-if="target.errorMessage" class="[overflow-wrap:anywhere]">{{ target.errorMessage }}</span>
                  <span v-if="target.errorDetail" class="text-xs [overflow-wrap:anywhere]">{{ target.errorDetail }}</span>
                </div>
              </div>
            </header>

            <div class="motion-stagger min-h-0 min-w-0 overflow-y-auto overflow-x-hidden p-4">
              <section class="motion-reveal rounded-2xl border border-border bg-muted p-4">
                <h3 class="m-0 mb-3 text-sm font-semibold">{{ t('applicationOperationsPage.servers') }}</h3>
                <div v-if="!detail.targets.length" class="grid gap-2">
                  <EmptyState :title="t('applicationOperationsPage.noTargets')" :description="t('applicationOperationsPage.noTargetsHint')" />
                </div>
                <div v-for="target in detail.targets" :key="target.id" class="grid min-w-0 gap-2 border-t border-border py-3 text-sm first:border-t-0 first:pt-0" >
                  <div class="grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-start gap-2">
                    <strong class="min-w-0 truncate">{{ serverLabel(target) }}</strong>
                    <StatusBadge class="max-w-full shrink-0 justify-self-start whitespace-nowrap" :status="target.status" domain="operation" :label="statusLabel(target.status)" />
                  </div>
                  <p v-if="mismatchText(target)" class="m-0 min-w-0 text-xs text-foreground [overflow-wrap:anywhere]">{{ mismatchText(target) }}</p>
                  <p v-if="!isConsistent(target)" class="m-0 min-w-0 text-xs text-muted-foreground [overflow-wrap:anywhere]">{{ desiredActualText(target) }}</p>
                  <p v-else class="m-0 min-w-0 text-xs text-success">{{ t('applicationOperationsPage.consistentHint', { generation: target.desiredGeneration || '' }) }}</p>
                  <p v-if="!isConsistent(target)" class="m-0 min-w-0 text-xs text-muted-foreground">
                    {{ t('applicationOperationsPage.stage') }}：{{ target.stage || t('common.notAvailable') }}
                    <template v-if="target.attempt">，{{ t('applicationOperationsPage.attempt') }}：{{ target.attempt }}</template>
                    <template v-if="target.nextRunAt">，{{ t('applicationOperationsPage.retryAt') }}：{{ formatDateTime(target.nextRunAt, '') }}</template>
                  </p>
                  <div v-if="targetErrorText(target)" class="min-w-0 whitespace-pre-wrap break-words rounded-lg border border-danger-border bg-danger-bg p-2 text-xs text-danger [overflow-wrap:anywhere]">
                    <span v-if="target.errorCode" class="block font-medium">{{ t('applicationOperationsPage.errorCode') }}：{{ target.errorCode }}</span>
                    {{ targetErrorText(target) }}
                  </div>
                  <Button v-if="!isConsistent(target)" size="sm" variant="secondary" class="justify-self-start" @click="drawerTarget = target">{{ t('applicationOperationsPage.stageLog') }}</Button>
                </div>
              </section>
            </div>
          </article>
        </main>
      </template>
    </MasterDetailLayout>

    <Teleport to="body">
      <Transition name="drawer">
        <div v-if="drawerTarget" class="fixed inset-0 z-50 bg-overlay">
<aside ref="drawer" tabindex="-1" class="drawer-panel absolute inset-y-0 right-0 grid w-full max-w-2xl grid-rows-[auto_minmax(0,1fr)_auto] border-l border-border bg-card shadow-2xl" @keydown="onDrawerKeydown">
            <header class="flex items-center justify-between gap-3 border-b border-border p-4">
              <h4 class="m-0 min-w-0 truncate text-sm font-semibold">{{ t('applicationOperationsPage.stageLogTitle', { server: serverLabel(drawerTarget) }) }}</h4>
              <Button size="sm" variant="secondary" @click="drawerTarget = null">{{ t('common.close') }}</Button>
            </header>
            <div class="min-h-0 overflow-y-auto px-4 py-3">
              <div v-if="!drawerTarget.stages?.length" class="grid h-full min-h-40 place-items-center">
                <EmptyState :title="t('applicationOperationsPage.noStages')" :description="t('applicationOperationsPage.noStagesHint')" />
              </div>
              <div v-else class="grid gap-1">
                <div v-for="stage in drawerTarget.stages" :key="stage.id" class="grid grid-cols-[130px_110px_minmax(0,1fr)] gap-3 border-b border-border py-3 text-xs last:border-b-0 max-sm:grid-cols-1" :class="stage.status === 'failed' ? 'text-danger' : ''">
                  <span class="text-muted-foreground">{{ formatDateTime(stage.startedAt, '') }}</span>
                  <span class="font-semibold">{{ stageLabel(stage.stage) }}（{{ statusLabel(stage.status) }}）<span v-if="stageDuration(stage)" class="block font-normal text-muted-foreground">{{ stageDuration(stage) }}</span></span>
                  <span class="whitespace-pre-wrap text-muted-foreground [overflow-wrap:anywhere]">{{ stage.detail || t('applicationOperationsPage.noStageDetail') }}</span>
                </div>
              </div>
            </div>
            <footer class="flex justify-end border-t border-border p-4">
              <Button size="sm" variant="secondary" @click="drawerTarget = null">{{ t('common.close') }}</Button>
            </footer>
          </aside>
        </div>
      </Transition>
    </Teleport>
  </ConsolePage>
</template>