<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Download, Eye, RefreshCcw } from '@lucide/vue';
import { logContextsApi } from '@/api/logContexts';
import { logsPath } from '@/api/activityReference';
import { saveBlobDownload } from '@/api/download';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import DateTimeRangePicker from '@/components/ui/DateTimeRangePicker.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import PaginationBar from '@/components/ui/PaginationBar.vue';
import SearchInput from '@/components/ui/SearchInput.vue';
import Table from '@/components/ui/Table.vue';
import { useErrorToast } from '@/components/ui/toast';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import ListPage from '@/components/templates/ListPage.vue';
import TableDetailLayout from '@/components/templates/TableDetailLayout.vue';
import { translateEventSummary, useI18n } from '@/i18n';
import type { LogContext, LogContextQuery } from '@/types/logContexts';
import { formatDateTime } from '@/utils/datetime';
import { createLatestRequestGuard } from '@/views/_shared/requestState';
import { initializeActivityList } from '@/views/activity/initialLoad';
import { operationTone } from '@/views/activity/model';

const props = defineProps<{
  origin: 'user' | 'system';
  titleKey: string;
  descriptionKey: string;
  emptyKey: string;
  emptyHintKey: string;
}>();

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const notifyError = useErrorToast();

const read = (key: string) => typeof route.query[key] === 'string' ? route.query[key] as string : '';
const initialFrom = new Date(Date.now() - 86400000).toISOString();
const filterKeys = ['from', 'to', 'hadError'] as const;
const query = computed<LogContextQuery>(() => {
  const value: LogContextQuery = { limit: 50, from: read('from') || initialFrom, origin: props.origin };
  for (const key of filterKeys) if (read(key)) value[key] = read(key);
  return value;
});
const range = computed(() => ({ from: read('from') || initialFrom, to: read('to') }));
const contexts = ref<LogContext[]>([]);
const loading = ref(false);
const exporting = ref(false);
const error = ref('');
const hasMore = ref(false);
const nextCursor = ref('');
const listGuard = createLatestRequestGuard();
const detailGuard = createLatestRequestGuard();
const detail = ref<LogContext | null>(null);
const detailLoading = ref(false);
const detailError = ref('');
const selectedId = computed(() => read('streamId'));
const detailOpen = computed(() => Boolean(selectedId.value));
let searchTimer: ReturnType<typeof setTimeout> | undefined;
const search = ref(read('q'));

const columns = computed(() => [
  { key: 'updatedAt', label: t('intentRecords.time'), nowrap: true },
  { key: 'title', label: t('intentRecords.title') },
  { key: 'resources', label: t('intentRecords.resources') },
  { key: 'result', label: t('common.status'), nowrap: true },
  { key: 'actions', label: t('common.actions'), align: 'right' as const, nowrap: true },
]);

const visibleContexts = computed(() => {
  const q = search.value.trim().toLowerCase();
  if (!q) return contexts.value;
  return contexts.value.filter((item) => JSON.stringify(item).toLowerCase().includes(q));
});

function stateLabel(value: string) {
  const key = `activity.state.${value}`;
  return t(key) === key ? value : t(key);
}
function resourceLabel(item: LogContext) {
  return item.resources?.map((resource) => resource.nameSnapshot || resource.resourceId).filter(Boolean).join(' · ')
    || item.module
    || t('activity.system');
}
function titleLabel(item: LogContext) {
  return item.title || item.summary || item.operationId || item.streamId;
}
function readCursorTrail(): string[] {
  try {
    const value: unknown = JSON.parse(read('cursorTrail') || '[]');
    return Array.isArray(value) && value.every((item) => typeof item === 'string') ? value : [];
  } catch {
    return [];
  }
}
function updateQuery(values: Record<string, string | undefined>, reset = true) {
  void router.replace({
    query: {
      ...route.query,
      ...(reset ? { cursor: undefined, cursorTrail: undefined } : {}),
      ...values,
    },
  });
}
function changeFilter(key: string, value: string) {
  updateQuery({ [key]: value || undefined });
}
function openDetail(streamId: string) {
  updateQuery({ streamId }, false);
}
function closeDetail() {
  updateQuery({ streamId: undefined }, false);
}
function openLogs(item: LogContext, diagnose = true) {
  void router.push(logsPath({
    streamId: item.streamId,
    operationId: item.operationId,
    level: diagnose ? 'warning,error' : undefined,
    from: read('from') || undefined,
    to: read('to') || undefined,
  }));
}

async function load() {
  const request = listGuard.begin();
  loading.value = true;
  error.value = '';
  const params = { ...query.value, cursor: read('cursor') || undefined };
  try {
    const page = await logContextsApi.list(params);
    if (!listGuard.isCurrent(request)) return;
    contexts.value = page.items;
    hasMore.value = page.hasMore;
    nextCursor.value = page.nextCursor || '';
  } catch (err) {
    if (!listGuard.isCurrent(request)) return;
    contexts.value = [];
    error.value = err instanceof Error ? err.message : t('common.loadFailed');
    notifyError(error.value, err);
  } finally {
    if (listGuard.isCurrent(request)) loading.value = false;
  }
}

async function loadDetail() {
  if (!selectedId.value) {
    detail.value = null;
    return;
  }
  const request = detailGuard.begin();
  detailLoading.value = true;
  detailError.value = '';
  try {
    const match = contexts.value.find((item) => item.streamId === selectedId.value);
    const value = match || await logContextsApi.get(selectedId.value);
    if (!detailGuard.isCurrent(request)) return;
    detail.value = value;
  } catch (err) {
    if (!detailGuard.isCurrent(request)) return;
    detail.value = null;
    detailError.value = err instanceof Error ? err.message : t('common.loadFailed');
  } finally {
    if (detailGuard.isCurrent(request)) detailLoading.value = false;
  }
}

function previousPage() {
  const trail = readCursorTrail();
  const previous = trail.pop();
  updateQuery({
    cursor: previous,
    cursorTrail: trail.length ? JSON.stringify(trail) : undefined,
  }, false);
}
function nextPage() {
  if (!nextCursor.value) return;
  const trail = readCursorTrail();
  if (read('cursor')) trail.push(read('cursor'));
  updateQuery({ cursor: nextCursor.value, cursorTrail: JSON.stringify(trail) }, false);
}
function refresh() {
  updateQuery({});
  void load();
  if (selectedId.value) void loadDetail();
}
async function download() {
  exporting.value = true;
  try {
    saveBlobDownload(await logContextsApi.export({ ...query.value }));
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    exporting.value = false;
  }
}

watch(search, (value) => {
  clearTimeout(searchTimer);
  searchTimer = setTimeout(() => changeFilter('q', value), 250);
});
watch(() => read('q'), (value) => {
  if (search.value !== value) search.value = value;
});
watch(() => JSON.stringify([query.value, read('cursor')]), () => {
  void load();
});
watch(selectedId, () => {
  if (selectedId.value) void loadDetail();
  else {
    detailGuard.invalidate();
    detail.value = null;
  }
});
onMounted(() => {
  initializeActivityList(Boolean(read('from')), initialFrom, updateQuery, load);
  if (selectedId.value) void loadDetail();
});
onBeforeUnmount(() => {
  clearTimeout(searchTimer);
  listGuard.invalidate();
  detailGuard.invalidate();
});
</script>

<template>
  <TableDetailLayout class="h-full" :has-detail="detailOpen">
    <template #list>
      <ConsolePage :title="t(titleKey)" :description="t(descriptionKey)">
        <template #actions>
          <Button size="sm" :loading="loading" @click="refresh"><RefreshCcw />{{ t('common.refresh') }}</Button>
          <Button size="sm" :loading="exporting" @click="download"><Download />{{ t('activity.export') }}</Button>
        </template>
        <ListPage>
          <template #toolbar>
            <div class="grid min-w-0 gap-3">
              <div class="grid min-w-0 gap-3 lg:grid-cols-2">
                <SearchInput v-model="search" clearable :label="t('common.search')" :placeholder="t('intentRecords.search')" :clear-label="t('common.clearSearch')" />
                <DateTimeRangePicker :model-value="range" @update:model-value="value => updateQuery({ from: value.from, to: value.to || undefined })" />
              </div>
              <div class="flex flex-wrap gap-2">
                <Button size="sm" :variant="!read('hadError') ? 'primary' : 'ghost'" @click="updateQuery({ hadError: undefined })">{{ t('activity.all') }}</Button>
                <Button size="sm" :variant="read('hadError') === 'true' ? 'primary' : 'ghost'" @click="updateQuery({ hadError: 'true' })">{{ t('activity.attention') }}</Button>
              </div>
            </div>
          </template>

          <EmptyState v-if="error && !contexts.length" variant="page" :title="t('common.loadFailed')" :description="error">
            <template #actions><Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button></template>
          </EmptyState>
          <Table
            v-else
            class="h-full max-lg:max-h-[60dvh]"
            :columns="columns"
            :rows="visibleContexts"
            row-key="streamId"
            :active-key="selectedId"
            :loading="loading && !contexts.length"
            :loading-label="t('common.loading')"
          >
            <template #updatedAt="{ row }">
              <span class="text-muted-foreground">{{ formatDateTime(row.updatedAt || row.startedAt) }}</span>
            </template>
            <template #title="{ row }">
              <strong class="block max-w-[24rem] truncate text-foreground">{{ translateEventSummary(t, titleLabel(row)) }}</strong>
              <span v-if="row.summary && row.summary !== row.title" class="mt-1 block max-w-[24rem] truncate text-xs text-danger">{{ row.summary }}</span>
            </template>
            <template #resources="{ row }">
              <span class="text-muted-foreground">{{ resourceLabel(row) }}</span>
            </template>
            <template #result="{ row }">
              <Badge :tone="operationTone(row.result)">{{ stateLabel(row.result || (row.finishedAt ? 'ended' : 'running')) }}</Badge>
            </template>
            <template #actions="{ row }">
              <div class="flex items-center justify-end gap-1">
                <Button size="sm" variant="secondary" @click="openDetail(row.streamId)"><Eye />{{ t('common.details') }}</Button>
                <Button size="sm" variant="ghost" @click="openLogs(row)">{{ t('intentRecords.viewLogs') }}</Button>
              </div>
            </template>
            <template #empty>
              <EmptyState variant="inset" :title="t(emptyKey)" :description="t(emptyHintKey)" />
            </template>
          </Table>
          <template #pagination>
            <PaginationBar
              mode="cursor"
              :has-previous="Boolean(read('cursor'))"
              :has-next="hasMore"
              :loading="loading"
              :previous-label="t('common.previous')"
              :next-label="t('common.next')"
              @previous="previousPage"
              @next="nextPage"
            >
              <template #summary />
            </PaginationBar>
          </template>
        </ListPage>
      </ConsolePage>
    </template>

    <template #detail>
      <ConsolePage
        :title="detail ? translateEventSummary(t, titleLabel(detail)) : t('common.loading')"
        :back-label="t('common.backToList')"
        @back="closeDetail"
      >
        <template #actions>
          <Button v-if="detail" size="sm" variant="primary" @click="openLogs(detail)">{{ t('intentRecords.viewLogs') }}</Button>
        </template>

        <EmptyState v-if="detailError" :title="t('common.loadFailed')" :description="detailError">
          <template #actions><Button size="sm" :loading="detailLoading" @click="loadDetail"><RefreshCcw />{{ t('common.retry') }}</Button></template>
        </EmptyState>
        <div v-else-if="detail" class="grid min-w-0 gap-4">
          <div class="flex flex-wrap gap-2">
            <Badge :tone="operationTone(detail.result)">{{ stateLabel(detail.result || (detail.finishedAt ? 'ended' : 'running')) }}</Badge>
            <Badge v-if="detail.hadError" tone="danger">{{ t('activity.hadError') }}</Badge>
          </div>
          <p v-if="detail.summary" class="m-0 break-words rounded-xl border border-danger-border bg-danger-bg p-3 text-sm text-danger">{{ detail.summary }}</p>
          <p v-else class="m-0 text-sm text-muted-foreground">{{ t('intentRecords.noFailureSummary') }}</p>
          <dl class="grid gap-2 text-sm sm:grid-cols-2">
            <div><dt class="text-muted-foreground">{{ t('intentRecords.resources') }}</dt><dd>{{ resourceLabel(detail) }}</dd></div>
            <div><dt class="text-muted-foreground">{{ t('intentRecords.actor') }}</dt><dd>{{ detail.actor?.name || detail.actor?.id || t('activity.system') }}</dd></div>
            <div><dt class="text-muted-foreground">{{ t('intentRecords.time') }}</dt><dd>{{ formatDateTime(detail.updatedAt || detail.startedAt) }}</dd></div>
            <div><dt class="text-muted-foreground">{{ t('logsPage.module') }}</dt><dd>{{ detail.module }}</dd></div>
          </dl>
          <p class="m-0 text-sm text-muted-foreground">{{ t('intentRecords.diagnoseHint') }}</p>
        </div>
      </ConsolePage>
    </template>
  </TableDetailLayout>
</template>
