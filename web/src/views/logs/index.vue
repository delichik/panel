<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Download, Eye, RefreshCcw } from '@lucide/vue';
import { activityApi } from '@/api/activity';
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
import { translateEventSummary, useI18n } from '@/i18n';
import type { ActivityEvent, ActivityQuery } from '@/types/activity';
import { formatDateTime } from '@/utils/datetime';
import { createLatestRequestGuard } from '@/views/_shared/requestState';
import { initializeActivityList } from '@/views/activity/initialLoad';
import { eventDisplayMessage, eventTone } from '@/views/activity/model';

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const notifyError = useErrorToast();
const read = (key: string) => typeof route.query[key] === 'string' ? route.query[key] as string : '';
const initialFrom = new Date(Date.now() - 86400000).toISOString();
const filterKeys = ['q', 'domain', 'level', 'resourceType', 'resourceId', 'executionId', 'operationId', 'from', 'to'] as const;
const query = computed<ActivityQuery>(() => {
  // Default to warning+error so the list is useful for diagnosis; widen via the level filter.
  const value: ActivityQuery = { limit: 100, from: read('from') || initialFrom, level: read('level') || 'warning,error' };
  for (const key of filterKeys) if (read(key)) value[key] = read(key);
  if (!read('level')) value.level = 'warning,error';
  return value;
});
const range = computed(() => ({ from: read('from') || initialFrom, to: read('to') }));
const events = ref<ActivityEvent[]>([]);
const loading = ref(false);
const exporting = ref(false);
const error = ref('');
const hasMore = ref(false);
const nextCursor = ref('');
const snapshotSeq = ref(0);
const indexState = ref('');
const listGuard = createLatestRequestGuard();
let searchTimer: ReturnType<typeof setTimeout> | undefined;
const search = ref(read('q'));
const selected = ref<ActivityEvent | null>(null);
const detailOpen = computed({
  get: () => Boolean(selected.value),
  set: (open) => {
    if (!open) {
      selected.value = null;
      if (read('eventId')) updateQuery({ eventId: undefined }, false);
    }
  },
});

const levelOptions = computed(() => [
  // Empty value keeps URL clean; query builder still sends warning,error.
  { value: '', label: t('logsPage.filter.diagnoseLevels') },
  { value: 'error', label: t('activity.level.error') },
  { value: 'info,warning,error', label: t('activity.filter.defaultLevels') },
  { value: 'debug,info,warning,error', label: t('activity.filter.allLevels') },
]);
const domainOptions = computed(() => [
  { value: '', label: t('activity.filter.allDomains') },
  ...['application', 'server', 'container', 'network', 'certificate', 'key_asset', 'package', 'system', 'security'].map((value) => ({ value, label: t(`activity.domain.${value}`) })),
]);
const columns = computed(() => [
  { key: 'occurredAt', label: t('logsPage.time'), nowrap: true },
  { key: 'level', label: t('activity.level'), nowrap: true },
  { key: 'source', label: t('logsPage.source'), nowrap: true },
  { key: 'message', label: t('logsPage.message') },
  { key: 'actions', label: t('common.actions'), align: 'right' as const, nowrap: true },
]);

function message(event: ActivityEvent) {
  return translateEventSummary(t, eventDisplayMessage(event, t));
}
function sourceLabel(event: ActivityEvent) {
  return event.actor?.name || event.actor?.id || event.sourceId || t('activity.system');
}
function dataString(event: ActivityEvent, key: string) {
  const value = event.data?.[key];
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
      ...(reset ? { cursor: undefined, snapshotSeq: undefined, cursorTrail: undefined } : {}),
      ...values,
    },
  });
}
function changeFilter(key: string, value: string) {
  updateQuery({ [key]: value || undefined });
}
function openDetail(event: ActivityEvent) {
  selected.value = event;
  updateQuery({ eventId: event.eventId }, false);
}

async function load() {
  const request = listGuard.begin();
  loading.value = true;
  error.value = '';
  try {
    const page = await activityApi.events({
      ...query.value,
      cursor: read('cursor') || undefined,
      snapshotSeq: read('snapshotSeq') || undefined,
    });
    if (!listGuard.isCurrent(request)) return;
    events.value = page.items;
    hasMore.value = page.hasMore;
    nextCursor.value = page.nextCursor || '';
    snapshotSeq.value = page.snapshotSeq;
    indexState.value = page.indexState;
    const eventId = read('eventId');
    if (eventId) {
      const match = page.items.find((item) => item.eventId === eventId);
      if (match) selected.value = match;
      else if (!selected.value || selected.value.eventId !== eventId) {
        try {
          selected.value = await activityApi.event(eventId);
        } catch {
          selected.value = null;
        }
      }
    }
  } catch (err) {
    if (!listGuard.isCurrent(request)) return;
    events.value = [];
    error.value = err instanceof Error ? err.message : t('common.loadFailed');
    notifyError(error.value, err);
  } finally {
    if (listGuard.isCurrent(request)) loading.value = false;
  }
}

function previousPage() {
  const trail = readCursorTrail();
  const previous = trail.pop();
  updateQuery({
    cursor: previous,
    snapshotSeq: previous ? String(snapshotSeq.value) : undefined,
    cursorTrail: trail.length ? JSON.stringify(trail) : undefined,
  }, false);
}
function nextPage() {
  if (!nextCursor.value) return;
  const trail = readCursorTrail();
  if (read('cursor')) trail.push(read('cursor'));
  updateQuery({ cursor: nextCursor.value, snapshotSeq: String(snapshotSeq.value), cursorTrail: JSON.stringify(trail) }, false);
}
function refresh() {
  updateQuery({});
  void load();
}
async function download() {
  exporting.value = true;
  try {
    saveBlobDownload(await activityApi.export({ ...query.value, snapshotSeq: snapshotSeq.value }));
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
watch(() => JSON.stringify([query.value, read('cursor'), read('snapshotSeq')]), () => {
  void load();
});
onMounted(() => {
  initializeActivityList(Boolean(read('from')), initialFrom, updateQuery, load);
});
onBeforeUnmount(() => {
  clearTimeout(searchTimer);
  listGuard.invalidate();
});
</script>

<template>
  <ConsolePage class="h-full" :title="t('routes.logs.title')" :description="t('routes.logs.description')">
    <template #actions>
      <Button size="sm" :loading="loading" @click="refresh"><RefreshCcw />{{ t('common.refresh') }}</Button>
      <Button size="sm" :loading="exporting" :disabled="!snapshotSeq" @click="download"><Download />{{ t('activity.export') }}</Button>
    </template>

    <ListPage>
      <template #toolbar>
        <div class="grid min-w-0 gap-3">
          <div class="grid min-w-0 gap-3 lg:grid-cols-2">
            <SearchInput v-model="search" clearable :label="t('common.search')" :placeholder="t('activity.search')" :clear-label="t('common.clearSearch')" />
            <DateTimeRangePicker :model-value="range" @update:model-value="value => updateQuery({ from: value.from, to: value.to || undefined })" />
          </div>
          <div class="grid min-w-0 grid-cols-2 gap-3 xl:grid-cols-3">
            <Select :model-value="read('domain')" :options="domainOptions" :aria-label="t('activity.domain')" @update:model-value="changeFilter('domain', $event)" />
            <Select :model-value="read('level')" :options="levelOptions" :aria-label="t('activity.level')" @update:model-value="changeFilter('level', $event)" />
            <SearchInput :model-value="read('resourceId')" :label="t('activity.resource')" :placeholder="t('activity.resource')" @update:model-value="changeFilter('resourceId', $event)" />
          </div>
          <div v-if="read('executionId') || read('operationId') || read('resourceId') || (indexState && indexState !== 'ready')" class="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <span v-if="read('operationId')">{{ t('logsPage.operationFilterHint') }}</span>
            <span v-else-if="read('executionId')">{{ t('activity.executionFilterHint') }}</span>
            <Badge v-if="indexState && indexState !== 'ready'" tone="warning">{{ t('activity.indexState') }}</Badge>
            <Button
              v-if="read('resourceId') || read('executionId') || read('operationId')"
              size="sm"
              variant="ghost"
              @click="updateQuery({ resourceId: undefined, resourceType: undefined, executionId: undefined, operationId: undefined })"
            >
              {{ t('activity.clearResource') }}
            </Button>
          </div>
        </div>
      </template>

      <EmptyState v-if="error && !events.length" variant="page" :title="t('common.loadFailed')" :description="error">
        <template #actions><Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button></template>
      </EmptyState>
      <Table
        v-else
        class="h-full max-lg:max-h-[60dvh]"
        :columns="columns"
        :rows="events"
        row-key="eventId"
        :active-key="selected?.eventId || ''"
        :loading="loading && !events.length"
        :loading-label="t('common.loading')"
      >
        <template #occurredAt="{ row }">
          <span class="text-muted-foreground">{{ formatDateTime(row.occurredAt) }}</span>
        </template>
        <template #level="{ row }">
          <Badge :tone="eventTone(row.level)">{{ t(`activity.level.${row.level}`) }}</Badge>
        </template>
        <template #source="{ row }">
          <span class="text-muted-foreground">{{ sourceLabel(row) }}</span>
        </template>
        <template #message="{ row }">
          <span class="block max-w-[36rem] truncate text-foreground">{{ message(row) }}</span>
        </template>
        <template #actions="{ row }">
          <Button size="sm" variant="secondary" @click="openDetail(row)"><Eye />{{ t('common.details') }}</Button>
        </template>
        <template #empty>
          <EmptyState variant="inset" :title="t('activity.empty')" :description="t('activity.emptyHint')" />
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
    :title="t('logsPage.detailTitle')"
    :description="selected ? formatDateTime(selected.occurredAt) : undefined"
    @update:open="detailOpen = $event"
  >
    <div v-if="selected" class="grid min-w-0 gap-4">
      <div class="flex flex-wrap gap-2">
        <Badge :tone="eventTone(selected.level)">{{ t(`activity.level.${selected.level}`) }}</Badge>
        <span class="text-sm text-muted-foreground">{{ sourceLabel(selected) }}</span>
        <span v-if="selected.domain" class="text-sm text-muted-foreground">{{ t(`activity.domain.${selected.domain}`) }}</span>
      </div>
      <dl v-if="dataString(selected, 'errorCode') || dataString(selected, 'error') || dataString(selected, 'detail')" class="grid gap-2 rounded-xl border border-border p-3 text-sm">
        <div v-if="dataString(selected, 'errorCode')"><dt class="text-muted-foreground">{{ t('logsPage.errorCode') }}</dt><dd class="font-mono">{{ dataString(selected, 'errorCode') }}</dd></div>
        <div v-if="dataString(selected, 'error')"><dt class="text-muted-foreground">{{ t('logsPage.error') }}</dt><dd class="break-words [overflow-wrap:anywhere]">{{ dataString(selected, 'error') }}</dd></div>
        <div v-if="dataString(selected, 'detail')"><dt class="text-muted-foreground">{{ t('logsPage.detail') }}</dt><dd class="break-words [overflow-wrap:anywhere]">{{ dataString(selected, 'detail') }}</dd></div>
      </dl>
      <pre class="m-0 max-h-[60dvh] overflow-auto whitespace-pre-wrap break-words rounded-xl bg-muted p-3 font-mono text-sm [overflow-wrap:anywhere]">{{ message(selected) }}</pre>
    </div>
  </Dialog>
</template>
