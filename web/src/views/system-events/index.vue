<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { RefreshCcw } from '@lucide/vue';
import { systemEventsApi } from '@/api/systemEvents';
import Button from '@/components/ui/Button.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import PaginationBar from '@/components/ui/PaginationBar.vue';
import SearchInput from '@/components/ui/SearchInput.vue';
import Select from '@/components/ui/Select.vue';
import StatusBadge from '@/components/ui/StatusBadge.vue';
import Table from '@/components/ui/Table.vue';
import Tooltip from '@/components/ui/Tooltip.vue';
import { useErrorToast } from '@/components/ui/toast';
import ListPage from '@/components/templates/ListPage.vue';
import { translateRuntimeEventType, useI18n } from '@/i18n';
import type { SystemEventDto } from '@/types/systemEvents';
import { createLatestRequestGuard, normalizePage } from '@/views/_shared/requestState';
import { formatDateTime } from '@/utils/datetime';

type EventRow = SystemEventDto & Record<string, unknown>;

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const notifyError = useErrorToast();

const rows = ref<SystemEventDto[]>([]);
const total = ref(0);
const page = ref(normalizePage(route.query.page));
const pageSize = 20;
const eventType = ref(String(route.query.eventType || ''));
const severity = ref(String(route.query.severity || ''));
const fromValue = ref(toLocalInput(String(route.query.from || '')) || toLocalInput(new Date(Date.now() - 24 * 3600 * 1000).toISOString()));
const toValue = ref(toLocalInput(String(route.query.to || '')) || toLocalInput(new Date().toISOString()));
const loading = ref(false);
const error = ref('');
const listRequests = createLatestRequestGuard();

const columns = computed<Array<{ key: keyof EventRow & string; label: string; align?: 'left' | 'right'; width?: string; nowrap?: boolean }>>(() => [
  { key: 'occurredAt', label: t('systemEventsPage.column.time'), width: 'w-40', nowrap: true },
  { key: 'severity', label: t('systemEventsPage.column.severity'), width: 'w-24', nowrap: true },
  { key: 'eventType', label: t('systemEventsPage.column.type'), width: 'w-36' },
  { key: 'summary', label: t('systemEventsPage.column.content'), width: 'w-[40%]' },
  { key: 'source', label: t('systemEventsPage.column.source'), width: 'w-28' },
]);

const severityOptions = computed(() => [
  { label: t('systemEventsPage.filter.allSeverities'), value: '' },
  { label: t('systemEventsPage.severity.info'), value: 'info' },
  { label: t('systemEventsPage.severity.warning'), value: 'warning' },
  { label: t('systemEventsPage.severity.error'), value: 'error' },
]);

watch([eventType, severity, fromValue, toValue], () => {
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
      eventType: eventType.value || undefined,
      severity: severity.value || undefined,
      from: localInputToIso(fromValue.value) || undefined,
      to: localInputToIso(toValue.value) || undefined,
      page: page.value > 1 ? String(page.value) : undefined,
    },
  });
  void load();
}

function toLocalInput(iso?: string): string {
  if (!iso) return '';
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return '';
  const pad = (value: number) => String(value).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function localInputToIso(value: string): string | undefined {
  if (!value) return undefined;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString();
}

async function load() {
  const requestId = listRequests.begin();
  loading.value = true;
  error.value = '';
  try {
    const result = await systemEventsApi.list({
      eventType: eventType.value,
      severity: severity.value,
      from: localInputToIso(fromValue.value),
      to: localInputToIso(toValue.value),
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

function severityLabel(value: string) {
  const key = `systemEventsPage.severity.${value}`;
  const label = t(key);
  return label === key ? value : label;
}

function eventTypeLabel(value: string) {
  return translateRuntimeEventType(t, value);
}

function sourceLabel(row: SystemEventDto) {
  return row.sourceModule || row.source || t('common.notAvailable');
}

onMounted(load);
</script>

<template>
  <ListPage>
    <template #toolbar>
      <div class="grid gap-3 lg:grid-cols-[minmax(220px,1fr)_180px_220px_220px_auto]">
        <SearchInput v-model="eventType" clearable :label="t('systemEventsPage.typeFilter')" :placeholder="t('systemEventsPage.typeFilterPlaceholder')" :clear-label="t('common.clearSearch')" />
        <Select v-model="severity" :options="severityOptions" :placeholder="t('systemEventsPage.column.severity')" />
        <label class="grid gap-1 text-xs text-muted-foreground">{{ t('systemEventsPage.range.from') }}<input v-model="fromValue" type="datetime-local" class="motion-field h-9 w-full rounded-xl border border-input bg-background px-3 text-sm text-foreground transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 focus-visible:ring-offset-2 focus-visible:ring-offset-background" /></label>
        <label class="grid gap-1 text-xs text-muted-foreground">{{ t('systemEventsPage.range.to') }}<input v-model="toValue" type="datetime-local" class="motion-field h-9 w-full rounded-xl border border-input bg-background px-3 text-sm text-foreground transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 focus-visible:ring-offset-2 focus-visible:ring-offset-background" /></label>
        <Button :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
      </div>
    </template>

    <div class="grid min-h-full gap-3">
      <Table v-if="rows.length || loading" :columns="columns" :rows="rows as EventRow[]" row-key="id" fixed :loading="loading" :loading-label="t('systemEventsPage.loading')">
        <template #occurredAt="{ row }"><span class="block whitespace-nowrap">{{ formatDateTime(row.occurredAt, t('common.notAvailable')) }}</span></template>
        <template #severity="{ row }"><StatusBadge :status="row.severity" :tone="row.severity === 'error' ? 'danger' : row.severity === 'warning' ? 'warning' : 'info'" :label="severityLabel(row.severity)" /></template>
        <template #eventType="{ row }"><span class="block truncate">{{ eventTypeLabel(row.eventType) }}</span></template>
        <template #summary="{ row }">
          <Tooltip trigger-class="block min-w-0 w-full" :text="row.summary">
            <span class="block min-w-0 max-w-full truncate">{{ row.summary }}</span>
          </Tooltip>
        </template>
        <template #source="{ row }"><span class="block min-w-0 truncate">{{ sourceLabel(row) }}</span></template>
      </Table>
      <EmptyState v-else-if="error" :title="t('common.loadFailed')" :description="error">
        <template #actions>
          <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button>
        </template>
      </EmptyState>
      <EmptyState v-else :title="t('systemEventsPage.empty')" :description="t('systemEventsPage.emptyHint')" />
    </div>

    <template #pagination>
      <PaginationBar v-model:page="page" :page-size="pageSize" :total="total" :loading="loading" :previous-label="t('common.previous')" :next-label="t('common.next')">
        <template #summary="{ page: currentPage, pageCount, total: totalCount }">{{ t('systemEventsPage.paginationSummary', { page: currentPage, pages: pageCount, total: totalCount }) }}</template>
      </PaginationBar>
    </template>
  </ListPage>
</template>