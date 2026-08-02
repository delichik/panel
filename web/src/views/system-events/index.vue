<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Eye, RefreshCcw } from '@lucide/vue';
import { systemEventsApi } from '@/api/systemEvents';
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
import type { SystemEventDetailDto, SystemEventDto } from '@/types/systemEvents';
import { createLatestRequestGuard, normalizePage } from '@/views/_shared/requestState';

type EventRow = SystemEventDto & Record<string, unknown>;

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const notifyError = useErrorToast();

const rows = ref<SystemEventDto[]>([]);
const total = ref(0);
const page = ref(normalizePage(route.query.page));
const pageSize = 20;
const subjectId = ref(String(route.query.subjectId || ''));
const eventType = ref(String(route.query.eventType || ''));
const severity = ref(String(route.query.severity || ''));
const category = ref(String(route.query.category || ''));
const loading = ref(false);
const detailLoading = ref(false);
const error = ref('');
const detail = ref<SystemEventDetailDto | null>(null);
const detailOpen = ref(false);
const listRequests = createLatestRequestGuard();
const detailRequests = createLatestRequestGuard();

const columns = computed<Array<{ key: keyof EventRow & string; label: string; align?: 'left' | 'right' }>>(() => [
  { key: 'occurredAt', label: t('systemEventsPage.column.time') },
  { key: 'severity', label: t('systemEventsPage.column.severity') },
  { key: 'eventType', label: t('common.type') },
  { key: 'summary', label: t('systemEventsPage.column.summary') },
  { key: 'subjectId', label: t('systemEventsPage.column.subject') },
  { key: 'source', label: t('systemEventsPage.column.source') },
  { key: 'id', label: t('common.actions'), align: 'right' },
]);

const severityOptions = computed(() => [
  { label: t('systemEventsPage.filter.allSeverities'), value: '' },
  { label: t('systemEventsPage.severity.info'), value: 'info' },
  { label: t('systemEventsPage.severity.warning'), value: 'warning' },
  { label: t('systemEventsPage.severity.error'), value: 'error' },
  { label: t('systemEventsPage.severity.critical'), value: 'critical' },
]);

const categoryOptions = computed(() => [
  { label: t('systemEventsPage.filter.allCategories'), value: '' },
  { label: t('systemEventsPage.category.application'), value: 'application' },
  { label: t('systemEventsPage.category.task'), value: 'task' },
  { label: t('systemEventsPage.category.alert'), value: 'alert' },
  { label: t('systemEventsPage.category.log'), value: 'log' },
  { label: t('systemEventsPage.category.runtime'), value: 'runtime' },
  { label: t('systemEventsPage.category.system'), value: 'system' },
]);

watch([subjectId, eventType, severity, category], () => {
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
      subjectId: subjectId.value || undefined,
      eventType: eventType.value || undefined,
      severity: severity.value || undefined,
      category: category.value || undefined,
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
    const result = await systemEventsApi.list({
      subjectId: subjectId.value,
      eventType: eventType.value,
      severity: severity.value,
      category: category.value,
      page: page.value,
      pageSize,
    });
    if (!listRequests.isCurrent(requestId)) return;
    rows.value = result.items;
    total.value = result.total;
    if (result.page && result.page !== page.value) page.value = result.page;
  } catch (err) {
    if (!listRequests.isCurrent(requestId)) return;
    error.value = err instanceof Error ? err.message : t('systemEventsPage.loadFailed');
    notifyError(err instanceof Error ? err.message : t('systemEventsPage.loadFailed'));
    rows.value = [];
    total.value = 0;
  } finally {
    if (listRequests.isCurrent(requestId)) loading.value = false;
  }
}

async function openDetail(row: SystemEventDto) {
  if (!row.detailAvailable) return;
  const requestId = detailRequests.begin();
  detailLoading.value = true;
  detailOpen.value = true;
  detail.value = null;
  try {
    const result = await systemEventsApi.get(row.id);
    if (!detailRequests.isCurrent(requestId)) return;
    detail.value = result;
  } catch (err) {
    if (!detailRequests.isCurrent(requestId)) return;
    error.value = err instanceof Error ? err.message : t('systemEventsPage.detailLoadFailed');
    notifyError(err instanceof Error ? err.message : t('systemEventsPage.detailLoadFailed'));
    detailOpen.value = false;
  } finally {
    if (detailRequests.isCurrent(requestId)) detailLoading.value = false;
  }
}

function formatDateTime(value?: string) {
  if (!value) return t('common.notAvailable');
  const time = new Date(value);
  return Number.isNaN(time.getTime()) ? value : time.toLocaleString();
}

function severityLabel(value: string) {
  const key = `systemEventsPage.severity.${value}`;
  const label = t(key);
  return label === key ? value : label;
}

function categoryLabel(value: string) {
  const key = `systemEventsPage.category.${value}`;
  const label = t(key);
  return label === key ? value : label;
}

function eventTypeLabel(value: string) {
  return translateRuntimeEventType(t, value);
}

function subjectLabel(row: SystemEventDto) {
  return row.subjectType && row.subjectId ? `${row.subjectType}:${row.subjectId}` : t('common.notAvailable');
}

onMounted(load);
</script>

<template>
  <ListPage>
    <template #toolbar>
      <div class="grid gap-3 lg:grid-cols-[minmax(220px,1fr)_minmax(180px,0.7fr)_180px_180px_auto]">
        <SearchInput v-model="subjectId" clearable :label="t('systemEventsPage.subjectFilter')" :placeholder="t('systemEventsPage.subjectFilterPlaceholder')" :clear-label="t('common.clearSearch')" />
        <SearchInput v-model="eventType" clearable :label="t('systemEventsPage.eventTypeFilter')" :placeholder="t('systemEventsPage.eventTypeFilterPlaceholder')" :clear-label="t('common.clearSearch')" />
        <Select v-model="severity" :options="severityOptions" :placeholder="t('systemEventsPage.column.severity')" />
        <Select v-model="category" :options="categoryOptions" :placeholder="t('systemEventsPage.column.category')" />
        <Button :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
      </div>
    </template>

    <div class="grid min-h-full gap-3">
      <Table v-if="rows.length || loading" :columns="columns" :rows="rows as EventRow[]" row-key="id" :loading="loading" :loading-label="t('systemEventsPage.loading')">
        <template #occurredAt="{ row }">{{ formatDateTime(row.occurredAt) }}</template>
        <template #severity="{ row }"><StatusBadge :status="row.severity" :tone="row.severity === 'error' || row.severity === 'critical' ? 'danger' : row.severity === 'warning' ? 'warning' : 'info'" :label="severityLabel(row.severity)" /></template>
        <template #eventType="{ row }">
          <div class="grid min-w-0 gap-1">
            <strong class="truncate text-foreground">{{ eventTypeLabel(row.eventType) }}</strong>
            <span class="text-xs text-muted-foreground">{{ categoryLabel(row.category) }}</span>
          </div>
        </template>
        <template #summary="{ row }"><span class="line-clamp-2">{{ row.summary }}</span></template>
        <template #subjectId="{ row }">{{ subjectLabel(row) }}</template>
        <template #source="{ row }">{{ row.sourceModule || row.source || t('common.notAvailable') }}</template>
        <template #id="{ row }">
          <Tooltip v-if="!row.detailAvailable" :text="t('systemEventsPage.detailPruned')">
            <Button size="sm" disabled><Eye />{{ t('common.view') }}</Button>
          </Tooltip>
          <Button v-else size="sm" :loading="detailLoading" @click="openDetail(row)"><Eye />{{ t('common.view') }}</Button>
        </template>
      </Table>
      <EmptyState v-else :title="t('systemEventsPage.empty')" :description="t('systemEventsPage.emptyHint')" />
    </div>

    <template #pagination>
      <PaginationBar v-model:page="page" :page-size="pageSize" :total="total" :loading="loading" :previous-label="t('common.previous')" :next-label="t('common.next')">
        <template #summary="{ page: currentPage, pageCount, total: totalCount }">{{ t('systemEventsPage.paginationSummary', { page: currentPage, pages: pageCount, total: totalCount }) }}</template>
      </PaginationBar>
    </template>
  </ListPage>

  <Dialog v-model:open="detailOpen" :title="detail ? eventTypeLabel(detail.event.eventType) : t('systemEventsPage.detailTitle')" :description="detail?.event.id" :close-label="t('common.close')">
    <div v-if="detailLoading" class="relative grid min-h-64 place-items-center">
      <LoadingOverlay :label="t('systemEventsPage.loadingDetail')" />
    </div>
    <div v-else-if="detail" class="grid gap-4">
      <section class="grid gap-2 rounded-xl border border-border p-3 text-sm">
        <div><span class="text-muted-foreground">{{ t('systemEventsPage.column.time') }}</span> <strong>{{ formatDateTime(detail.event.occurredAt) }}</strong></div>
        <div><span class="text-muted-foreground">{{ t('systemEventsPage.column.severity') }}</span> <StatusBadge :status="detail.event.severity" :label="severityLabel(detail.event.severity)" /></div>
        <div><span class="text-muted-foreground">{{ t('systemEventsPage.column.subject') }}</span> <strong>{{ subjectLabel(detail.event) }}</strong></div>
        <p class="m-0 text-muted-foreground">{{ detail.event.summary }}</p>
      </section>
      <section v-if="detail.logRefs?.length">
        <h3 class="m-0 mb-2 text-sm font-semibold">{{ t('systemEventsPage.logRefs') }}</h3>
        <div class="grid gap-2">
          <div v-for="logRef in detail.logRefs" :key="`${logRef.label}-${logRef.source}`" class="rounded-xl border border-border p-3 text-sm">
            <strong>{{ logRef.label }}</strong>
            <p class="m-0 text-muted-foreground">{{ logRef.source || t('common.notAvailable') }}</p>
          </div>
        </div>
      </section>
      <section v-if="detail.taskRefs?.length || detail.targetRefs?.length" class="grid gap-2 rounded-xl border border-border p-3 text-sm">
        <div v-if="detail.taskRefs?.length"><span class="text-muted-foreground">{{ t('systemEventsPage.taskRefs') }}</span> <strong>{{ detail.taskRefs.join(', ') }}</strong></div>
        <div v-if="detail.targetRefs?.length"><span class="text-muted-foreground">{{ t('systemEventsPage.targetRefs') }}</span> <strong>{{ detail.targetRefs.join(', ') }}</strong></div>
      </section>
      <pre v-if="detail.payload" class="max-h-64 overflow-auto rounded-xl border border-border bg-muted p-3 text-xs">{{ JSON.stringify(detail.payload, null, 2) }}</pre>
    </div>
  </Dialog>
</template>
