<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Download, Eye, RefreshCcw } from '@lucide/vue';
import { activityApi } from '@/api/activity';
import { saveBlobDownload } from '@/api/download';
import EventTimeline from '@/components/activity/EventTimeline.vue';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import DateTimeRangePicker from '@/components/ui/DateTimeRangePicker.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import PaginationBar from '@/components/ui/PaginationBar.vue';
import SearchInput from '@/components/ui/SearchInput.vue';
import Select from '@/components/ui/Select.vue';
import Table from '@/components/ui/Table.vue';
import { useErrorToast } from '@/components/ui/toast';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import ListPage from '@/components/templates/ListPage.vue';
import TableDetailLayout from '@/components/templates/TableDetailLayout.vue';
import { translateEventSummary, useI18n } from '@/i18n';
import type { ActivityEvent, ActivityOperationDetail, ActivityQuery } from '@/types/activity';
import { formatDateTime } from '@/utils/datetime';
import { createLatestRequestGuard } from '@/views/_shared/requestState';
import { initializeActivityList } from '@/views/activity/initialLoad';
import { emptyTimelineWindow, replaceTimelineWindow } from '@/views/activity/timelineWindow';
import { eventDisplayMessage, eventStream, eventTone, mergeEvents } from '@/views/activity/model';

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const notifyError = useErrorToast();
const read = (key: string) => typeof route.query[key] === 'string' ? route.query[key] as string : '';
const initialFrom = new Date(Date.now() - 86400000).toISOString();
const filterKeys = ['q', 'domain', 'level', 'resourceType', 'resourceId', 'executionId', 'operationId', 'from', 'to'] as const;
const query = computed<ActivityQuery>(() => {
  const value: ActivityQuery = { limit: 100, from: read('from') || initialFrom, level: read('level') || 'info,warning,error' };
  for (const key of filterKeys) if (read(key)) value[key] = read(key);
  if (!read('level')) value.level = 'info,warning,error';
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
const detailGuard = createLatestRequestGuard();
const contextGuard = createLatestRequestGuard();
const detail = ref<ActivityOperationDetail | null>(null);
const selectedEvent = ref<ActivityEvent | null>(null);
const timeline = ref<ActivityEvent[]>([]);
const detailLoading = ref(false);
const detailError = ref('');
const timelineWindow = ref(emptyTimelineWindow());
const timelineHasMore = computed(() => timelineWindow.value.hasMore);
const timelineSnapshot = ref(0);
const timelineSearch = ref('');
const stream = ref('');
const onlyErrors = ref(false);
const contextMode = ref(false);
const contextEvents = ref<ActivityEvent[]>([]);
const contextLoading = ref(false);
const contextSelectedId = ref('');
let searchTimer: ReturnType<typeof setTimeout> | undefined;
const search = ref(read('q'));
const selectedOperationId = computed(() => read('operationId'));
const selectedEventId = computed(() => read('eventId'));
const detailOpen = computed(() => Boolean(selectedOperationId.value || selectedEventId.value));
const timelineVisible = computed(() => (contextMode.value ? contextEvents.value : timeline.value).filter((event) => (
  (!stream.value || eventStream(event) === stream.value)
  && (!onlyErrors.value || ['error', 'warning'].includes(event.level))
  && (!timelineSearch.value || eventDisplayMessage(event, t).toLowerCase().includes(timelineSearch.value.toLowerCase()))
)));
const levelOptions = computed(() => [
  { value: '', label: t('activity.filter.defaultLevels') },
  ...['debug', 'info', 'warning', 'error'].map((value) => ({ value, label: t(`activity.level.${value}`) })),
  { value: 'warning,error', label: t('activity.warningsErrors') },
  { value: 'debug,info,warning,error', label: t('activity.filter.allLevels') },
]);
const domainOptions = computed(() => [
  { value: '', label: t('activity.filter.allDomains') },
  ...['application', 'server', 'container', 'network', 'certificate', 'key_asset', 'package', 'system', 'security'].map((value) => ({ value, label: t(`activity.domain.${value}`) })),
]);
const columns = computed(() => [
  { key: 'occurredAt', label: t('logsPage.time'), nowrap: true },
  { key: 'level', label: t('activity.level'), nowrap: true },
  { key: 'message', label: t('logsPage.message') },
  { key: 'resources', label: t('intentRecords.resources') },
  { key: 'actions', label: t('common.actions'), align: 'right' as const, nowrap: true },
]);

function message(event: ActivityEvent) {
  return translateEventSummary(t, eventDisplayMessage(event, t));
}
function resourceLabel(event: ActivityEvent) {
  return event.resources?.map((resource) => resource.nameSnapshot || resource.resourceId).filter(Boolean).join(' · ') || t('activity.system');
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
function selectEvent(event: ActivityEvent) {
  updateQuery({ eventId: event.eventId, operationId: event.operationId || undefined }, false);
}
function closeDetail() {
  updateQuery({ operationId: undefined, eventId: undefined }, false);
}

async function load() {
  const request = listGuard.begin();
  loading.value = true;
  error.value = '';
  try {
    const page = await activityApi.events({ ...query.value, cursor: read('cursor') || undefined, snapshotSeq: read('snapshotSeq') || undefined });
    if (!listGuard.isCurrent(request)) return;
    events.value = page.items;
    hasMore.value = page.hasMore;
    nextCursor.value = page.nextCursor || '';
    snapshotSeq.value = page.snapshotSeq;
    indexState.value = page.indexState;
  } catch (err) {
    if (!listGuard.isCurrent(request)) return;
    events.value = [];
    error.value = err instanceof Error ? err.message : t('common.loadFailed');
    notifyError(error.value, err);
  } finally {
    if (listGuard.isCurrent(request)) loading.value = false;
  }
}

async function loadDetail() {
  const operationId = selectedOperationId.value;
  const eventId = selectedEventId.value;
  if (!operationId && !eventId) {
    detail.value = null;
    selectedEvent.value = null;
    timeline.value = [];
    return;
  }
  const request = detailGuard.begin();
  detailLoading.value = true;
  detailError.value = '';
  contextMode.value = false;
  try {
    if (operationId) {
      const value = await activityApi.operation(operationId, { snapshotSeq: snapshotSeq.value || undefined });
      if (!detailGuard.isCurrent(request)) return;
      detail.value = value;
      selectedEvent.value = null;
      timelineWindow.value = replaceTimelineWindow(emptyTimelineWindow(), {
        items: value.events,
        hasMore: value.hasMore,
        nextCursor: value.nextCursor || '',
      }, 'initial');
      timeline.value = timelineWindow.value.events;
      timelineSnapshot.value = value.snapshotSeq;
    } else if (eventId) {
      const event = await activityApi.event(eventId);
      if (!detailGuard.isCurrent(request)) return;
      selectedEvent.value = event;
      detail.value = null;
      if (event.operationId) {
        updateQuery({ operationId: event.operationId, eventId }, false);
        return;
      }
      const page = await activityApi.context(eventId, { scope: 'resource', before: 40, after: 40 });
      if (!detailGuard.isCurrent(request)) return;
      timeline.value = mergeEvents([event], page.items);
      timelineWindow.value = emptyTimelineWindow();
    }
  } catch (err) {
    if (!detailGuard.isCurrent(request)) return;
    detailError.value = err instanceof Error ? err.message : t('common.loadFailed');
    detail.value = null;
    timeline.value = [];
  } finally {
    if (detailGuard.isCurrent(request)) detailLoading.value = false;
  }
}

async function moreTimeline(direction: 'older' | 'newer') {
  if (!detail.value) return;
  const cursor = direction === 'older' ? timelineWindow.value.nextCursor : timelineWindow.value.previousCursors.at(-1);
  if (direction === 'older' && !timelineWindow.value.hasMore) return;
  if (direction === 'newer' && !timelineWindow.value.previousCursors.length) return;
  const request = detailGuard.begin();
  detailLoading.value = true;
  try {
    const page = await activityApi.events({
      operationId: detail.value.operation.operationId,
      cursor: cursor || undefined,
      snapshotSeq: timelineSnapshot.value,
      limit: 100,
    });
    if (!detailGuard.isCurrent(request)) return;
    timelineWindow.value = replaceTimelineWindow(timelineWindow.value, page, direction);
    timeline.value = timelineWindow.value.events;
    contextMode.value = false;
  } catch (err) {
    if (detailGuard.isCurrent(request)) notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    if (detailGuard.isCurrent(request)) detailLoading.value = false;
  }
}

async function showContext(event: ActivityEvent) {
  const request = contextGuard.begin();
  contextLoading.value = true;
  contextSelectedId.value = event.eventId;
  try {
    const page = await activityApi.context(event.eventId, { scope: event.operationId ? 'operation' : 'resource', before: 30, after: 30 });
    if (!contextGuard.isCurrent(request)) return;
    contextEvents.value = mergeEvents([event], page.items);
    contextMode.value = true;
    onlyErrors.value = false;
    stream.value = '';
    timelineSearch.value = '';
  } catch (err) {
    if (contextGuard.isCurrent(request)) notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    if (contextGuard.isCurrent(request)) contextLoading.value = false;
  }
}

async function evidence(evidenceId: string) {
  try {
    saveBlobDownload(await activityApi.evidence(evidenceId));
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
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
  if (detailOpen.value) void loadDetail();
}
async function download() {
  exporting.value = true;
  try {
    saveBlobDownload(await activityApi.export({
      ...(detail.value ? { operationId: detail.value.operation.operationId, scope: 'operation', snapshotSeq: timelineSnapshot.value } : { ...query.value, snapshotSeq: snapshotSeq.value }),
    }));
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
watch([selectedOperationId, selectedEventId], () => {
  if (detailOpen.value) void loadDetail();
  else {
    detailGuard.invalidate();
    contextGuard.invalidate();
    detail.value = null;
    selectedEvent.value = null;
    timeline.value = [];
  }
});
onMounted(() => {
  initializeActivityList(Boolean(read('from')), initialFrom, updateQuery, load);
  if (detailOpen.value) void loadDetail();
});
onBeforeUnmount(() => {
  clearTimeout(searchTimer);
  listGuard.invalidate();
  detailGuard.invalidate();
  contextGuard.invalidate();
});
</script>

<template>
  <TableDetailLayout class="h-full" :has-detail="detailOpen">
    <template #list>
      <ConsolePage :title="t('routes.logs.title')" :description="t('routes.logs.description')">
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
              <div v-if="read('executionId') || read('resourceId') || (indexState && indexState !== 'ready')" class="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                <span v-if="read('executionId')">{{ t('activity.executionFilterHint') }}</span>
                <Badge v-if="indexState && indexState !== 'ready'" tone="warning">{{ t('activity.indexState') }}</Badge>
                <Button v-if="read('resourceId') || read('executionId')" size="sm" variant="ghost" @click="updateQuery({ resourceId: undefined, resourceType: undefined, executionId: undefined })">{{ t('activity.clearResource') }}</Button>
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
            :active-key="selectedEventId"
            :loading="loading && !events.length"
            :loading-label="t('common.loading')"
          >
            <template #occurredAt="{ row }">
              <span class="text-muted-foreground">{{ formatDateTime(row.occurredAt) }}</span>
            </template>
            <template #level="{ row }">
              <Badge :tone="eventTone(row.level)">{{ t(`activity.level.${row.level}`) }}</Badge>
            </template>
            <template #message="{ row }">
              <span class="block max-w-[28rem] truncate text-foreground">{{ message(row) }}</span>
            </template>
            <template #resources="{ row }">
              <span class="text-muted-foreground">{{ resourceLabel(row) }}</span>
            </template>
            <template #actions="{ row }">
              <Button size="sm" variant="secondary" @click="selectEvent(row)"><Eye />{{ t('common.details') }}</Button>
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
    </template>

    <template #detail>
      <ConsolePage
        :title="detail ? translateEventSummary(t, detail.operation.title || detail.operation.action) : (selectedEvent ? message(selectedEvent) : t('activity.timeline'))"
        :back-label="t('common.backToList')"
        @back="closeDetail"
      >
        <template #actions>
          <Button v-if="detail" size="sm" :loading="exporting" @click="download">{{ t('activity.exportOperation') }}</Button>
        </template>
        <EmptyState v-if="detailError" :title="t('common.loadFailed')" :description="detailError">
          <template #actions><Button size="sm" :loading="detailLoading" @click="loadDetail"><RefreshCcw />{{ t('common.retry') }}</Button></template>
        </EmptyState>
        <div v-else class="grid min-w-0 gap-4">
          <div class="flex flex-wrap items-center gap-2">
            <Button v-if="contextMode" size="sm" @click="contextMode = false">{{ t('activity.backTimeline') }}</Button>
            <Button size="sm" :variant="onlyErrors ? 'primary' : 'secondary'" @click="onlyErrors = !onlyErrors">{{ t('activity.warningsErrors') }}</Button>
            <Button size="sm" :loading="detailLoading" @click="loadDetail">{{ t('activity.latestBatch') }}</Button>
          </div>
          <div class="grid gap-2 md:grid-cols-2">
            <SearchInput v-model="timelineSearch" :label="t('activity.searchBatch')" :placeholder="t('activity.searchBatch')" />
            <Select
              v-model="stream"
              :options="[{ value: '', label: t('activity.allStreams') }, { value: 'stdout', label: 'stdout' }, { value: 'stderr', label: 'stderr' }]"
              :aria-label="t('activity.stream')"
            />
          </div>
          <p v-if="contextMode" class="m-0 text-xs text-muted-foreground">{{ t('activity.contextHint') }}</p>
          <EventTimeline
            :events="timelineVisible"
            :selected-event-id="contextMode ? contextSelectedId : selectedEventId"
            @context="showContext"
            @evidence="evidence"
          />
          <p v-if="!detailLoading && !contextLoading && !timelineVisible.length" class="text-sm text-muted-foreground">{{ t('activity.noEventsInBatch') }}</p>
          <div v-if="!contextMode && detail" class="flex flex-wrap gap-2">
            <Button v-if="timelineWindow.previousCursors.length" :loading="detailLoading" @click="moreTimeline('newer')">{{ t('activity.newerBatch') }}</Button>
            <Button v-if="timelineHasMore" :loading="detailLoading" @click="moreTimeline('older')">{{ t('activity.olderBatch') }}</Button>
          </div>
        </div>
      </ConsolePage>
    </template>
  </TableDetailLayout>
</template>
