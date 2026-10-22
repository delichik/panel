<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Download, Eye, RefreshCcw } from '@lucide/vue';
import { logContextsApi } from '@/api/logContexts';
import { saveBlobDownload } from '@/api/download';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import DateTimeRangePicker from '@/components/ui/DateTimeRangePicker.vue';
import Dialog from '@/components/ui/Dialog.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import PaginationBar from '@/components/ui/PaginationBar.vue';
import SearchInput from '@/components/ui/SearchInput.vue';
import Select from '@/components/ui/Select.vue';
import Table from '@/components/ui/Table.vue';
import { useErrorToast } from '@/components/ui/toast';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import ListPage from '@/components/templates/ListPage.vue';
import { useI18n } from '@/i18n';
import type { LogContext, LogContextLine, LogContextQuery } from '@/types/logContexts';
import { formatDateTime } from '@/utils/datetime';
import { createLatestRequestGuard } from '@/views/_shared/requestState';
import { initializeActivityList } from '@/views/activity/initialLoad';
import { eventTone, operationTone } from '@/views/activity/model';

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const notifyError = useErrorToast();
const read = (key: string) => typeof route.query[key] === 'string' ? route.query[key] as string : '';
const initialFrom = new Date(Date.now() - 86400000).toISOString();
const filterKeys = ['operationId', 'executionId', 'module', 'from', 'to', 'hadError'] as const;
const query = computed<LogContextQuery>(() => {
  const value: LogContextQuery = { limit: 100, from: read('from') || initialFrom };
  for (const key of filterKeys) if (read(key)) value[key] = read(key);
  return value;
});
const lineLevel = computed(() => read('level') || 'warning,error');
const range = computed(() => ({ from: read('from') || initialFrom, to: read('to') }));
const contexts = ref<LogContext[]>([]);
const loading = ref(false);
const exporting = ref(false);
const error = ref('');
const hasMore = ref(false);
const nextCursor = ref('');
const listGuard = createLatestRequestGuard();
const linesGuard = createLatestRequestGuard();
let searchTimer: ReturnType<typeof setTimeout> | undefined;
const search = ref(read('q'));
const selected = ref<LogContext | null>(null);
const lines = ref<LogContextLine[]>([]);
const linesLoading = ref(false);
const linesError = ref('');
const detailOpen = computed({
  get: () => Boolean(selected.value),
  set: (open) => {
    if (!open) {
      selected.value = null;
      lines.value = [];
      linesError.value = '';
      if (read('streamId')) updateQuery({ streamId: undefined }, false);
    }
  },
});

const levelOptions = computed(() => [
  { value: '', label: t('logsPage.filter.diagnoseLevels') },
  { value: 'error', label: t('activity.level.error') },
  { value: 'info,warning,error', label: t('activity.filter.defaultLevels') },
  { value: 'debug,info,warning,error', label: t('activity.filter.allLevels') },
]);
const columns = computed(() => [
  { key: 'updatedAt', label: t('logsPage.time'), nowrap: true },
  { key: 'status', label: t('common.status'), nowrap: true },
  { key: 'source', label: t('logsPage.source'), nowrap: true },
  { key: 'title', label: t('logsPage.message') },
  { key: 'actions', label: t('common.actions'), align: 'right' as const, nowrap: true },
]);

const visibleContexts = computed(() => {
  const q = search.value.trim().toLowerCase();
  if (!q) return contexts.value;
  return contexts.value.filter((item) => JSON.stringify(item).toLowerCase().includes(q));
});

function sourceLabel(item: LogContext) {
  return item.module || item.origin || t('activity.system');
}
function titleLabel(item: LogContext) {
  return item.title || item.summary || item.streamId;
}
function lineMessage(line: LogContextLine) {
  return typeof line.msg === 'string' ? line.msg : JSON.stringify(line);
}
function levelLabel(level: string) {
  const key = `activity.level.${level}`;
  return t(key) === key ? level : t(key);
}
function lineField(line: LogContextLine, key: string) {
  const value = line[key];
  return typeof value === 'string' ? value.trim() : '';
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
function openDetail(item: LogContext) {
  selected.value = item;
  updateQuery({ streamId: item.streamId }, false);
  void loadLines(item.streamId);
}

async function load() {
  const request = listGuard.begin();
  loading.value = true;
  error.value = '';
  try {
    const page = await logContextsApi.list({
      ...query.value,
      cursor: read('cursor') || undefined,
    });
    if (!listGuard.isCurrent(request)) return;
    contexts.value = page.items;
    hasMore.value = page.hasMore;
    nextCursor.value = page.nextCursor || '';
    const streamId = read('streamId');
    if (streamId) {
      const match = page.items.find((item) => item.streamId === streamId);
      if (match) {
        selected.value = match;
        void loadLines(streamId);
      } else if (!selected.value || selected.value.streamId !== streamId) {
        try {
          selected.value = await logContextsApi.get(streamId);
          void loadLines(streamId);
        } catch {
          selected.value = null;
          lines.value = [];
        }
      }
    }
  } catch (err) {
    if (!listGuard.isCurrent(request)) return;
    contexts.value = [];
    error.value = err instanceof Error ? err.message : t('common.loadFailed');
    notifyError(error.value, err);
  } finally {
    if (listGuard.isCurrent(request)) loading.value = false;
  }
}

async function loadLines(streamId: string) {
  const request = linesGuard.begin();
  linesLoading.value = true;
  linesError.value = '';
  try {
    const page = await logContextsApi.lines(streamId, { level: lineLevel.value, limit: 200 });
    if (!linesGuard.isCurrent(request)) return;
    lines.value = page.items;
  } catch (err) {
    if (!linesGuard.isCurrent(request)) return;
    lines.value = [];
    linesError.value = err instanceof Error ? err.message : t('common.loadFailed');
  } finally {
    if (linesGuard.isCurrent(request)) linesLoading.value = false;
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
watch(lineLevel, () => {
  if (selected.value) void loadLines(selected.value.streamId);
});
onMounted(() => {
  initializeActivityList(Boolean(read('from')), initialFrom, updateQuery, load);
});
onBeforeUnmount(() => {
  clearTimeout(searchTimer);
  listGuard.invalidate();
  linesGuard.invalidate();
});
</script>

<template>
  <ConsolePage class="h-full" :title="t('routes.logs.title')" :description="t('routes.logs.description')">
    <template #actions>
      <Button size="sm" :loading="loading" @click="refresh"><RefreshCcw />{{ t('common.refresh') }}</Button>
      <Button size="sm" :loading="exporting" @click="download"><Download />{{ t('activity.export') }}</Button>
    </template>

    <ListPage>
      <template #toolbar>
        <div class="grid min-w-0 gap-3">
          <div class="grid min-w-0 gap-3 lg:grid-cols-2">
            <SearchInput v-model="search" clearable :label="t('common.search')" :placeholder="t('logsPage.search')" :clear-label="t('common.clearSearch')" />
            <DateTimeRangePicker :model-value="range" @update:model-value="value => updateQuery({ from: value.from, to: value.to || undefined })" />
          </div>
          <div class="grid min-w-0 grid-cols-2 gap-3 xl:grid-cols-3">
            <Select :model-value="read('level')" :options="levelOptions" :aria-label="t('activity.level')" @update:model-value="changeFilter('level', $event)" />
            <SearchInput :model-value="read('operationId')" :label="t('logsPage.operationId')" :placeholder="t('logsPage.operationId')" @update:model-value="changeFilter('operationId', $event)" />
            <SearchInput :model-value="read('executionId')" :label="t('logsPage.executionId')" :placeholder="t('logsPage.executionId')" @update:model-value="changeFilter('executionId', $event)" />
          </div>
          <div v-if="read('executionId') || read('operationId') || read('streamId')" class="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <span v-if="read('operationId')">{{ t('logsPage.operationFilterHint') }}</span>
            <span v-else-if="read('executionId')">{{ t('activity.executionFilterHint') }}</span>
            <span v-else-if="read('streamId')">{{ t('logsPage.streamFilterHint') }}</span>
            <Button
              v-if="read('executionId') || read('operationId') || read('streamId')"
              size="sm"
              variant="ghost"
              @click="updateQuery({ executionId: undefined, operationId: undefined, streamId: undefined })"
            >
              {{ t('activity.clearResource') }}
            </Button>
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
        :active-key="selected?.streamId || ''"
        :loading="loading && !contexts.length"
        :loading-label="t('common.loading')"
      >
        <template #updatedAt="{ row }">
          <span class="text-muted-foreground">{{ formatDateTime(row.updatedAt || row.startedAt) }}</span>
        </template>
        <template #status="{ row }">
          <Badge v-if="row.hadError" tone="danger">{{ t('activity.hadError') }}</Badge>
          <Badge v-else-if="row.result" :tone="operationTone(row.result)">{{ row.result }}</Badge>
          <Badge v-else tone="neutral">{{ t('activity.running') }}</Badge>
        </template>
        <template #source="{ row }">
          <span class="text-muted-foreground">{{ sourceLabel(row) }}</span>
        </template>
        <template #title="{ row }">
          <span class="block max-w-[36rem] truncate text-foreground">{{ titleLabel(row) }}</span>
          <span v-if="row.summary && row.summary !== row.title" class="mt-1 block max-w-[36rem] truncate text-xs text-muted-foreground">{{ row.summary }}</span>
        </template>
        <template #actions="{ row }">
          <Button size="sm" variant="secondary" @click="openDetail(row)"><Eye />{{ t('common.details') }}</Button>
        </template>
        <template #empty>
          <EmptyState variant="inset" :title="t('logsPage.empty')" :description="t('logsPage.emptyHint')" />
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

  <Dialog
    :open="detailOpen"
    size="large"
    :title="selected ? titleLabel(selected) : t('logsPage.detailTitle')"
    :description="selected ? formatDateTime(selected.updatedAt || selected.startedAt) : undefined"
    @update:open="detailOpen = $event"
  >
    <div v-if="selected" class="grid min-w-0 gap-4">
      <div class="flex flex-wrap gap-2">
        <Badge v-if="selected.hadError" tone="danger">{{ t('activity.hadError') }}</Badge>
        <Badge v-if="selected.result" :tone="operationTone(selected.result)">{{ selected.result }}</Badge>
        <span class="text-sm text-muted-foreground">{{ sourceLabel(selected) }}</span>
        <span v-if="selected.origin" class="text-sm text-muted-foreground">{{ selected.origin }}</span>
      </div>
      <p v-if="selected.summary" class="m-0 text-sm text-muted-foreground">{{ selected.summary }}</p>
      <EmptyState v-if="linesError" variant="inset" :title="t('common.loadFailed')" :description="linesError">
        <template #actions><Button size="sm" :loading="linesLoading" @click="loadLines(selected.streamId)"><RefreshCcw />{{ t('common.retry') }}</Button></template>
      </EmptyState>
      <div v-else-if="linesLoading && !lines.length" class="text-sm text-muted-foreground">{{ t('common.loading') }}</div>
      <EmptyState v-else-if="!lines.length" variant="inset" :title="t('logsPage.linesEmpty')" :description="t('logsPage.linesEmptyHint')" />
      <div v-else class="grid max-h-[60dvh] gap-3 overflow-auto">
        <div v-for="(line, index) in lines" :key="`${line.ts || ''}-${index}`" class="rounded-xl border border-border p-3">
          <div class="mb-2 flex flex-wrap items-center gap-2">
            <Badge v-if="line.level" :tone="eventTone(String(line.level))">{{ levelLabel(String(line.level)) }}</Badge>
            <span class="text-xs text-muted-foreground">{{ formatDateTime(typeof line.ts === 'string' ? line.ts : undefined) }}</span>
          </div>
          <dl v-if="lineField(line, 'errorCode') || lineField(line, 'error') || lineField(line, 'detail')" class="mb-2 grid gap-2 text-sm">
            <div v-if="lineField(line, 'errorCode')"><dt class="text-muted-foreground">{{ t('logsPage.errorCode') }}</dt><dd class="font-mono">{{ lineField(line, 'errorCode') }}</dd></div>
            <div v-if="lineField(line, 'error')"><dt class="text-muted-foreground">{{ t('logsPage.error') }}</dt><dd class="break-words [overflow-wrap:anywhere]">{{ lineField(line, 'error') }}</dd></div>
            <div v-if="lineField(line, 'detail')"><dt class="text-muted-foreground">{{ t('logsPage.detail') }}</dt><dd class="break-words [overflow-wrap:anywhere]">{{ lineField(line, 'detail') }}</dd></div>
          </dl>
          <pre class="m-0 whitespace-pre-wrap break-words font-mono text-sm [overflow-wrap:anywhere]">{{ lineMessage(line) }}</pre>
        </div>
      </div>
    </div>
  </Dialog>
</template>
