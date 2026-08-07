<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Eye, RefreshCcw } from '@lucide/vue';
import { applicationOperationsApi } from '@/api/applicationOperations';
import Button from '@/components/ui/Button.vue';
import Dialog from '@/components/ui/Dialog.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import PaginationBar from '@/components/ui/PaginationBar.vue';
import SearchInput from '@/components/ui/SearchInput.vue';
import Select from '@/components/ui/Select.vue';
import StatusBadge from '@/components/ui/StatusBadge.vue';
import Table from '@/components/ui/Table.vue';
import Tooltip from '@/components/ui/Tooltip.vue';
import LoadingOverlay from '@/components/ui/LoadingOverlay.vue';
import { useErrorToast } from '@/components/ui/toast';
import ListPage from '@/components/templates/ListPage.vue';
import { translateRuntimeEventType, useI18n } from '@/i18n';
import type { ApplicationOperationDetailDto, ApplicationOperationDto } from '@/types/applicationOperations';
import { createLatestRequestGuard, normalizePage } from '@/views/_shared/requestState';

type OperationRow = ApplicationOperationDto & Record<string, unknown>;

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const notifyError = useErrorToast();

const rows = ref<ApplicationOperationDto[]>([]);
const total = ref(0);
const page = ref(normalizePage(route.query.page));
const pageSize = 20;
const search = ref(String(route.query.applicationId || ''));
const status = ref(String(route.query.status || ''));
const source = ref(String(route.query.source || ''));
const loading = ref(false);
const detailLoadingId = ref('');
const error = ref('');
const detail = ref<ApplicationOperationDetailDto | null>(null);
const detailOpen = ref(false);
const listRequests = createLatestRequestGuard();
const detailRequests = createLatestRequestGuard();

const columns = computed<Array<{ key: keyof OperationRow & string; label: string; align?: 'left' | 'right' }>>(() => [
  { key: 'applicationNameSnapshot', label: t('applicationOperationsPage.column.application') },
  { key: 'action', label: t('applicationOperationsPage.column.action') },
  { key: 'source', label: t('applicationOperationsPage.column.source') },
  { key: 'status', label: t('common.status') },
  { key: 'targetTotal', label: t('applicationOperationsPage.column.targets') },
  { key: 'latestEventAt', label: t('applicationOperationsPage.column.latest') },
  { key: 'operationId', label: t('common.actions'), align: 'right' },
]);

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

watch([search, status, source], () => {
  if (page.value !== 1) {
    page.value = 1;
    return;
  }
  syncQueryAndLoad();
});

watch(page, syncQueryAndLoad);

function syncQueryAndLoad() {
  void router.replace({
    query: {
      ...route.query,
      applicationId: search.value || undefined,
      status: status.value || undefined,
      source: source.value || undefined,
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
  } catch (err) {
    if (!listRequests.isCurrent(requestId)) return;
    error.value = err instanceof Error ? err.message : t('applicationOperationsPage.loadFailed');
    notifyError(err instanceof Error ? err.message : t('applicationOperationsPage.loadFailed'));
    rows.value = [];
    total.value = 0;
  } finally {
    if (listRequests.isCurrent(requestId)) loading.value = false;
  }
}

async function openDetail(row: ApplicationOperationDto) {
  if (!row.detailAvailable) return;
  const requestId = detailRequests.begin();
  detailLoadingId.value = row.operationId;
  detailOpen.value = true;
  detail.value = null;
  try {
    const result = await applicationOperationsApi.get(row.operationId);
    if (!detailRequests.isCurrent(requestId)) return;
    detail.value = result;
  } catch (err) {
    if (!detailRequests.isCurrent(requestId)) return;
    error.value = err instanceof Error ? err.message : t('applicationOperationsPage.detailLoadFailed');
    notifyError(err instanceof Error ? err.message : t('applicationOperationsPage.detailLoadFailed'));
    detailOpen.value = false;
  } finally {
    if (detailRequests.isCurrent(requestId)) detailLoadingId.value = '';
  }
}

function formatDateTime(value?: string) {
  if (!value) return t('common.notAvailable');
  const time = new Date(value);
  return Number.isNaN(time.getTime()) ? value : time.toLocaleString();
}

function statusLabel(value: string) {
  return t(`applicationOperationsPage.status.${value}`);
}

function sourceLabel(value: string) {
  const key = `applicationOperationsPage.source.${value}`;
  const label = t(key);
  return label === key ? value : label;
}

function eventTypeLabel(value: string) {
  return translateRuntimeEventType(t, value);
}

function targetProgress(row: ApplicationOperationDto) {
  if (!row.targetTotal) return t('common.notAvailable');
  return t('applicationOperationsPage.targetProgress', { succeeded: row.targetSucceeded, failed: row.targetFailed, total: row.targetTotal });
}

onMounted(load);
</script>

<template>
  <ListPage>
    <template #toolbar>
      <div class="grid gap-3 lg:grid-cols-[minmax(220px,1fr)_180px_180px_auto]">
        <SearchInput v-model="search" clearable :label="t('applicationOperationsPage.applicationFilter')" :placeholder="t('applicationOperationsPage.applicationFilterPlaceholder')" :clear-label="t('common.clearSearch')" />
        <Select v-model="status" :options="statusOptions" :placeholder="t('common.status')" />
        <Select v-model="source" :options="sourceOptions" :placeholder="t('applicationOperationsPage.column.source')" />
        <Button :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
      </div>
    </template>

    <div class="grid min-h-full gap-3">
      <Table v-if="rows.length || loading" :columns="columns" :rows="rows as OperationRow[]" row-key="operationId" :loading="loading" :loading-label="t('applicationOperationsPage.loading')">
        <template #applicationNameSnapshot="{ row }">
          <div class="grid min-w-0 gap-1">
            <strong class="truncate text-foreground">{{ row.applicationNameSnapshot || t('common.notAvailable') }}</strong>
          </div>
        </template>
        <template #action="{ row }">{{ t(`applicationOperationsPage.action.${row.action}`) }}</template>
        <template #source="{ row }">{{ sourceLabel(row.source) }}</template>
        <template #status="{ row }">
          <StatusBadge class="shrink-0 justify-self-start" :status="row.status" domain="operation" :label="statusLabel(row.status)" />
        </template>
        <template #targetTotal="{ row }">{{ targetProgress(row) }}</template>
        <template #latestEventAt="{ row }">{{ formatDateTime(row.latestEventAt) }}</template>
        <template #operationId="{ row }">
          <Tooltip v-if="!row.detailAvailable" :text="t('applicationOperationsPage.detailPruned')">
            <Button size="sm" disabled><Eye />{{ t('common.view') }}</Button>
          </Tooltip>
          <Button v-else size="sm" :loading="detailLoadingId === row.operationId" @click="openDetail(row)"><Eye />{{ t('common.view') }}</Button>
        </template>
      </Table>
      <EmptyState v-else :title="t('applicationOperationsPage.empty')" :description="t('applicationOperationsPage.emptyHint')" />
    </div>

    <template #pagination>
      <PaginationBar v-model:page="page" :page-size="pageSize" :total="total" :loading="loading" :previous-label="t('common.previous')" :next-label="t('common.next')">
        <template #summary="{ page: currentPage, pageCount, total: totalCount }">{{ t('applicationOperationsPage.paginationSummary', { page: currentPage, pages: pageCount, total: totalCount }) }}</template>
      </PaginationBar>
    </template>
  </ListPage>

  <Dialog v-model:open="detailOpen" size="large" :title="detail?.operation.applicationNameSnapshot || t('applicationOperationsPage.detailTitle')" :close-label="t('common.close')">
    <div v-if="detailLoadingId !== ''" class="relative grid min-h-64 place-items-center">
      <LoadingOverlay :label="t('applicationOperationsPage.loadingDetail')" />
    </div>
    <div v-else-if="detail" class="grid gap-4">
      <section class="grid gap-2 rounded-xl border border-border p-3 text-sm">
        <div><span class="text-muted-foreground">{{ t('common.status') }}</span> <StatusBadge :status="detail.operation.status" domain="operation" :label="statusLabel(detail.operation.status)" /></div>
        <div><span class="text-muted-foreground">{{ t('applicationOperationsPage.column.targets') }}</span> <strong>{{ targetProgress(detail.operation) }}</strong></div>
        <div v-if="detail.operation.failureSummary" class="whitespace-pre-wrap break-words rounded-lg border border-danger-border bg-danger-bg p-2 text-danger">{{ detail.operation.failureSummary }}</div>
      </section>
      <section>
        <h3 class="m-0 mb-2 text-sm font-semibold">{{ t('applicationOperationsPage.targets') }}</h3>
        <div class="grid gap-2">
          <EmptyState v-if="!detail.targets.length" :title="t('applicationOperationsPage.noTargets')" :description="t('applicationOperationsPage.noTargetsHint')" />
          <div v-for="target in detail.targets" :key="target.id" class="grid gap-1 rounded-xl border border-border p-3 text-sm">
            <div class="flex items-center justify-between gap-2"><strong>{{ target.serverName || target.serverId || target.id }}</strong><StatusBadge :status="target.status" domain="operation" :label="target.status" /></div>
            <span class="text-muted-foreground">{{ target.stage || target.action }}</span>
            <span v-if="target.error" class="text-danger">{{ target.error }}</span>
          </div>
        </div>
      </section>
      <section>
        <h3 class="m-0 mb-2 text-sm font-semibold">{{ t('applicationOperationsPage.events') }}</h3>
        <div class="grid gap-2">
          <div v-for="event in detail.events" :key="event.id" class="grid gap-1 rounded-xl border border-border p-3 text-sm">
            <div class="flex items-center justify-between gap-2"><strong>{{ eventTypeLabel(event.eventType) }}</strong><span class="text-xs text-muted-foreground">{{ formatDateTime(event.occurredAt) }}</span></div>
            <p class="m-0 text-muted-foreground">{{ event.summary }}</p>
          </div>
        </div>
      </section>
    </div>
  </Dialog>
</template>
