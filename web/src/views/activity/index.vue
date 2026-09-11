<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Download, RefreshCcw } from '@lucide/vue';
import { apiClient } from '@/api/client';
import { idempotencyKey } from '@/api/assetRequests';
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue';
import Dialog from '@/components/ui/Dialog.vue';
import Textarea from '@/components/ui/Textarea.vue';
import { activityApi } from '@/api/activity';
import { saveBlobDownload } from '@/api/download';
import EventTimeline from '@/components/activity/EventTimeline.vue';
import MasterDetailLayout from '@/components/templates/MasterDetailLayout.vue';
import PageHeader from '@/components/shell/PageHeader.vue';
import AutoRefreshControl from '@/components/patterns/AutoRefreshControl.vue';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import DateTimeRangePicker from '@/components/ui/DateTimeRangePicker.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import LoadingOverlay from '@/components/ui/LoadingOverlay.vue';
import PaginationBar from '@/components/ui/PaginationBar.vue';
import SearchInput from '@/components/ui/SearchInput.vue';
import Select from '@/components/ui/Select.vue';
import Tabs from '@/components/ui/Tabs.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import { useAutoRefresh } from '@/composables/useAutoRefresh';
import { translateEventSummary, useI18n } from '@/i18n';
import type { ActivityEvent, ActivityOperation, ActivityOperationDetail, ActivityQuery, ActivitySummary } from '@/types/activity';
import { formatDateTime } from '@/utils/datetime';
import { createLatestRequestGuard } from '@/views/_shared/requestState';
import { emptyTimelineWindow, replaceTimelineWindow } from './timelineWindow';
import { eventDisplayMessage, eventMessage, eventStream, eventTone, mergeEvents, operationTone, manualResolution, capacityNotice } from './model';

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const notifyError = useErrorToast();
const notifySuccess = useSuccessToast();
const pendingCommand = ref<{ kind: string; executionId: string } | null>(null);
const commandLoading = ref(false);
const resolveOutcome = ref('');
const resolveReason = ref('');
function requestCommand(command: { kind: string; executionId: string }) { resolveOutcome.value = ''; resolveReason.value = ''; pendingCommand.value = command; }
async function executeCommand() {
  if (!pendingCommand.value || commandLoading.value) return;
  const command = pendingCommand.value;
  if (!['retry', 'run-now', 'resolve'].includes(command.kind)) return;
  const resolution = manualResolution(resolveOutcome.value, resolveReason.value);
  if (command.kind === 'resolve' && !resolution) return;
  commandLoading.value = true;
  try {
    const receipt = await apiClient.post(`/executions/${encodeURIComponent(command.executionId)}/${command.kind}`, command.kind === 'resolve' ? resolution : undefined, { headers: { 'Idempotency-Key': idempotencyKey() } });
    pendingCommand.value = null;
    notifySuccess(t(command.kind === 'resolve' ? 'activity.resolveRecorded' : 'activity.commandAccepted'), receipt);
    await loadDetail();
  } catch (err) { notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err); }
  finally { commandLoading.value = false; }
}
const autoRefresh = useAutoRefresh();
const read = (key: string) => typeof route.query[key] === 'string' ? route.query[key] as string : '';
const view = computed(() => read('view') === 'operations' ? 'operations' : 'events');
const initialFrom = new Date(Date.now() - 86400000).toISOString();
const filters = ['q', 'domain', 'level', 'trigger', 'resourceType', 'resourceId', 'executionId', 'action', 'phase', 'result', 'attention', 'actorId', 'kind', 'stepId', 'from', 'to'] as const;
const query = computed<ActivityQuery>(() => {
  const value: ActivityQuery = { limit: 100, from: read('from') || initialFrom };
  for (const key of filters) if (read(key)) value[key] = read(key);
  if (!read('level')) value.level = 'info,warning,error';
  if (view.value === 'events') { delete value.phase; delete value.result; delete value.attention; }
  return value;
});
const range = computed(() => ({ from: read('from') || initialFrom, to: read('to') }));
const events = ref<ActivityEvent[]>([]);
const operations = ref<ActivityOperation[]>([]);
const summary = ref<ActivitySummary | null>(null);
const capacityState = computed(() => capacityNotice(summary.value?.capacity));
const loading = ref(false);
const exporting = ref(false);
const error = ref('');
const hasMore = ref(false);
const nextCursor = ref('');
const snapshotSeq = ref(0);
const headSeq = ref(0);
const projectedThroughSeq = ref(0);
const indexState = ref('');
const newerAvailable = ref(false);
function readCursorTrail(): string[] {
  try { const value: unknown = JSON.parse(read('cursorTrail') || '[]'); return Array.isArray(value) && value.every(item => typeof item === 'string') ? value : []; } catch { return []; }
}
const listGuard = createLatestRequestGuard();
const detailGuard = createLatestRequestGuard();
const detail = ref<ActivityOperationDetail | null>(null);
const selectedEvent = ref<ActivityEvent | null>(null);
const timeline = ref<ActivityEvent[]>([]);
const detailLoading = ref(false);
const detailError = ref('');
const timelineWindow = ref(emptyTimelineWindow());
const timelineHasMore = computed(() => timelineWindow.value.hasMore);
const timelineSnapshot = ref(0);
const detailHead = ref(0);
const detailNewer = ref(false);
const timelineSearch = ref('');
const stream = ref('');
const onlyErrors = ref(false);
const contextMode = ref(false);
const contextEvents = ref<ActivityEvent[]>([]);
const contextLoading = ref(false);
const contextSelectedId = ref('');
const contextGuard = createLatestRequestGuard();
let searchTimer: ReturnType<typeof setTimeout> | undefined;
const search = ref(read('q'));
const selectedId = computed(() => read('operationId') || read('eventId'));
const timelineVisible = computed(() => (contextMode.value ? contextEvents.value : timeline.value).filter(event => (!stream.value || eventStream(event) === stream.value) && (!onlyErrors.value || ['error', 'warning'].includes(event.level)) && (!timelineSearch.value || eventMessage(event).toLowerCase().includes(timelineSearch.value.toLowerCase()))));
const levelOptions = computed(() => [{ value: '', label: t('activity.filter.defaultLevels') }, ...['debug', 'info', 'warning', 'error'].map(value => ({ value, label: t(`activity.level.${value}`) })), { value: 'warning,error', label: t('activity.warningsErrors') }, { value: 'debug,info,warning,error', label: t('activity.filter.allLevels') }]);
const domainOptions = computed(() => [{ value: '', label: t('activity.filter.allDomains') }, ...['application', 'server', 'container', 'network', 'certificate', 'key_asset', 'package', 'system', 'security'].map(value => ({ value, label: t(`activity.domain.${value}`) }))]);
const triggerOptions = computed(() => [{ value: '', label: t('activity.filter.allTriggers') }, ...['user', 'schedule', 'reconcile', 'startup', 'dependency'].map(value => ({ value, label: t(`activity.trigger.${value}`) }))]);
function stateLabel(value: string) { const key = `activity.state.${value}`; return t(key) === key ? value : t(key); }
function message(event: ActivityEvent) { return translateEventSummary(t, eventDisplayMessage(event, t)); }
function updateQuery(values: Record<string, string | undefined>, reset = true) {
  void router.replace({ query: { ...route.query, ...(reset ? { cursor: undefined, snapshotSeq: undefined, cursorTrail: undefined } : {}), ...values } });
}
function changeFilter(key: string, value: string) { updateQuery({ [key]: value || undefined }); }
function selectEvent(event: ActivityEvent) { updateQuery({ eventId: event.eventId, operationId: undefined }, false); }
function selectOperation(operationId: string) { updateQuery({ operationId, eventId: undefined }, false); }
function closeDetail() { updateQuery({ operationId: undefined, eventId: undefined }, false); }
async function load() {
  const request = listGuard.begin();
  loading.value = true;
  error.value = '';
  const params = { ...query.value, cursor: read('cursor') || undefined, snapshotSeq: read('snapshotSeq') || undefined };
  try {
    const page = view.value === 'operations' ? await activityApi.operations(params) : await activityApi.events(params);
    if (!listGuard.isCurrent(request)) return;
    if (view.value === 'operations') operations.value = page.items as ActivityOperation[];
    else events.value = page.items as ActivityEvent[];
    hasMore.value = page.hasMore;
    nextCursor.value = page.nextCursor || '';
    snapshotSeq.value = page.snapshotSeq;
    headSeq.value = page.headSeq;
    projectedThroughSeq.value = page.projectedThroughSeq;
    indexState.value = page.indexState;
    newerAvailable.value = false;
    const nextSummary = await activityApi.summary({ ...query.value, snapshotSeq: page.snapshotSeq });
    if (!listGuard.isCurrent(request)) return;
    summary.value = nextSummary;
  } catch (err) {
    if (!listGuard.isCurrent(request)) return;
    error.value = err instanceof Error ? err.message : t('common.loadFailed');
    notifyError(error.value);
  } finally { if (listGuard.isCurrent(request)) loading.value = false; }
}
function refresh() {
  if (read('cursor') || read('snapshotSeq')) updateQuery({ cursor: undefined, snapshotSeq: undefined, cursorTrail: undefined });
  else void load();
}
function nextPage() {
  if (!hasMore.value || !nextCursor.value) return;
  const trail = [...readCursorTrail(), read('cursor')];
  updateQuery({ cursor: nextCursor.value, snapshotSeq: String(snapshotSeq.value), cursorTrail: JSON.stringify(trail) }, false);
}
function previousPage() {
  const trail = readCursorTrail();
  const previous = trail.pop();
  updateQuery({ cursor: previous || undefined, snapshotSeq: String(snapshotSeq.value), cursorTrail: trail.length ? JSON.stringify(trail) : undefined }, false);
}
async function loadDetail() {
  const request = detailGuard.begin();
  detailLoading.value = true;
  detail.value = null;
  selectedEvent.value = null;
  timeline.value = [];
  timelineWindow.value = emptyTimelineWindow();
  contextMode.value = false;
  contextGuard.invalidate();
  contextLoading.value = false;
  detailError.value = '';
  detailNewer.value = false;
  timelineSearch.value = '';
  onlyErrors.value = false;
  stream.value = '';
  try {
    let operationId = read('operationId');
    if (read('eventId')) {
      const event = await activityApi.event(read('eventId'));
      if (!detailGuard.isCurrent(request)) return;
      selectedEvent.value = event;
      operationId = event.operationId || '';
      if (!operationId) {
        const context = await activityApi.context(event.eventId, { scope: 'resource', before: 30, after: 30 });
        if (!detailGuard.isCurrent(request)) return;
        timeline.value = mergeEvents([event], context.items);
        timelineSnapshot.value = context.snapshotSeq;
        detailHead.value = context.headSeq;
        timelineWindow.value = emptyTimelineWindow();
        return;
      }
    }
    if (!operationId) return;
    const result = await activityApi.operation(operationId);
    if (!detailGuard.isCurrent(request)) return;
    detail.value = result;
    timelineWindow.value = replaceTimelineWindow(emptyTimelineWindow(), { items: result.events, nextCursor: result.nextCursor, hasMore: result.hasMore }, 'initial');
    timeline.value = timelineWindow.value.events;
    timelineSnapshot.value = result.snapshotSeq;
    detailHead.value = result.headSeq;
    if (selectedEvent.value && !timeline.value.some(item => item.eventId === selectedEvent.value?.eventId)) await showContext(selectedEvent.value);
  } catch (err) {
    if (!detailGuard.isCurrent(request)) return;
    detailError.value = err instanceof Error ? err.message : t('common.loadFailed');
    notifyError(detailError.value);
  } finally { if (detailGuard.isCurrent(request)) detailLoading.value = false; }
}
async function moreTimeline(direction: 'older' | 'newer' = 'older') {
  if (!detail.value || detailLoading.value || (direction === 'older' ? !timelineHasMore.value : !timelineWindow.value.previousCursors.length)) return;
  const cursor = direction === 'older' ? timelineWindow.value.nextCursor : timelineWindow.value.previousCursors.at(-1) || '';
  const request = detailGuard.begin();
  detailLoading.value = true;
  try {
    const page = await activityApi.events({ operationId: detail.value.operation.operationId, cursor: cursor || undefined, snapshotSeq: timelineSnapshot.value, limit: 100 });
    if (!detailGuard.isCurrent(request)) return;
    timelineWindow.value = replaceTimelineWindow(timelineWindow.value, page, direction);
    timeline.value = timelineWindow.value.events;
    contextMode.value = false;
  } catch (err) { if (detailGuard.isCurrent(request)) notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err); }
  finally { if (detailGuard.isCurrent(request)) detailLoading.value = false; }
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
  } catch (err) { if (contextGuard.isCurrent(request)) notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err); }
  finally { if (contextGuard.isCurrent(request)) contextLoading.value = false; }
}
async function download(operation = false) {
  exporting.value = true;
  try { saveBlobDownload(await activityApi.export(operation && detail.value ? { operationId: detail.value.operation.operationId, scope: 'operation', snapshotSeq: timelineSnapshot.value } : { ...query.value, snapshotSeq: snapshotSeq.value })); }
  catch (err) { notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err); }
  finally { exporting.value = false; }
}
async function evidence(evidenceId: string) { try { saveBlobDownload(await activityApi.evidence(evidenceId)); } catch (err) { notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err); } }
async function poll() {
  const selection = selectedId.value;
  const filterKey = JSON.stringify(query.value);
  const snapshot = snapshotSeq.value;
  if (loading.value || detailLoading.value) return;
  try {
    const page = await activityApi.tail({ ...query.value, afterSeq: headSeq.value, limit: 1 });
    if (filterKey !== JSON.stringify(query.value)) return;
    if (page.items.length) newerAvailable.value = true;
    headSeq.value = page.headSeq;
    const currentSummary = await activityApi.summary({ ...query.value, snapshotSeq: snapshot });
    if (filterKey !== JSON.stringify(query.value) || snapshot !== snapshotSeq.value || loading.value) return;
    summary.value = currentSummary;
    const operationId = detail.value?.operation.operationId;
    if (operationId) {
      const updates = await activityApi.tail({ operationId, afterSeq: detailHead.value, limit: 1 });
      if (selection !== selectedId.value) return;
      if (updates.items.length) detailNewer.value = true;
      detailHead.value = updates.headSeq;
    }
  } catch (err) { notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err); }
}
watch(search, value => { clearTimeout(searchTimer); searchTimer = setTimeout(() => changeFilter('q', value), 250); });
watch(() => read('q'), value => { if (search.value !== value) search.value = value; });
watch(() => JSON.stringify([view.value, query.value, read('cursor'), read('snapshotSeq')]), () => { void load(); });
watch(selectedId, () => { if (selectedId.value) void loadDetail(); else { detailGuard.invalidate(); detail.value = null; timeline.value = []; } });
onMounted(() => { if (!read('from')) updateQuery({ from: initialFrom }, false); else void load(); if (selectedId.value) void loadDetail(); autoRefresh.start(poll); });
onBeforeUnmount(() => { clearTimeout(searchTimer); listGuard.invalidate(); detailGuard.invalidate(); contextGuard.invalidate(); });
</script>

<template>
  <div class="grid h-full min-h-0 min-w-0 grid-rows-[auto_auto_minmax(0,1fr)] overflow-hidden max-lg:h-auto max-lg:min-h-full max-lg:overflow-visible">
    <PageHeader :title="t('routes.activity.title')" :description="t('activity.description')">
      <template #actions><AutoRefreshControl :off-label="t('activity.paused')" :short-label="t('activity.every5')" :long-label="t('activity.every10')" :hint-label="t('activity.liveHint')" /><Button :loading="loading" @click="refresh"><RefreshCcw />{{ t('common.refresh') }}</Button><Button :loading="exporting" :disabled="!snapshotSeq" @click="download()"><Download />{{ t('activity.export') }}</Button></template>
    </PageHeader>
    <div class="grid min-w-0 gap-3 border-b border-border px-6 pb-4 max-sm:px-4">
      <div class="grid min-w-0 gap-3 lg:grid-cols-2"><SearchInput v-model="search" clearable :label="t('common.search')" :placeholder="t('activity.search')" :clear-label="t('common.clearSearch')" /><DateTimeRangePicker :model-value="range" @update:model-value="value => updateQuery({ from: value.from, to: value.to || undefined })" /></div>
      <div class="grid min-w-0 grid-cols-2 gap-3 xl:grid-cols-4"><Select :model-value="read('domain')" :options="domainOptions" :aria-label="t('activity.domain')" @update:model-value="changeFilter('domain', $event)" /><Select :model-value="read('level')" :options="levelOptions" :aria-label="t('activity.level')" @update:model-value="changeFilter('level', $event)" /><Select :model-value="read('trigger')" :options="triggerOptions" :aria-label="t('activity.trigger')" @update:model-value="changeFilter('trigger', $event)" /><SearchInput :model-value="read('resourceId')" :label="t('activity.resource')" :placeholder="t('activity.resource')" @update:model-value="changeFilter('resourceId', $event)" /></div>
      <div v-if="capacityState" :role="capacityState === 'blocked' ? 'alert' : 'status'" class="rounded-xl border p-3 text-sm" :class="capacityState === 'blocked' ? 'border-danger-border bg-danger-bg text-danger' : 'border-warning-border bg-warning-bg text-warning'">
        <strong>{{ t(`activity.capacity.${capacityState}`) }}</strong>
        <p class="m-0 mt-1">{{ t(`activity.capacity.${capacityState}Hint`) }}</p>
        <p v-if="summary?.capacity && summary.capacity.availableBytes >= 0" class="m-0 mt-1 text-xs">{{ t('activity.capacity.remaining', { size: Math.floor(summary.capacity.availableBytes / 1048576) }) }}</p>
      </div>
      <div class="flex flex-wrap items-center gap-2 text-xs text-muted-foreground"><span v-if="read('executionId')">{{ t('activity.executionFilterHint') }}</span><span v-if="summary">{{ t('activity.counts', { total: summary.total, errors: summary.byLevel.error || 0, warnings: summary.byLevel.warning || 0 }) }}</span><Badge v-if="indexState && indexState !== 'ready'" tone="warning">{{ t('activity.indexState', { seq: projectedThroughSeq, head: headSeq }) }}</Badge><Button v-if="newerAvailable" size="sm" @click="refresh">{{ t('activity.newEvents') }}</Button><Button v-if="read('resourceId') || read('executionId')" size="sm" variant="ghost" @click="updateQuery({ resourceId: undefined, resourceType: undefined, executionId: undefined })">{{ t('activity.clearResource') }}</Button></div>
    </div>
    <Tabs class="min-h-0 min-w-0 p-6 max-sm:p-4" :model-value="view" :tabs="[{ value: 'events', label: t('activity.events') }, { value: 'operations', label: t('activity.operations') }]" @update:model-value="updateQuery({ view: $event, operationId: undefined, eventId: undefined })">
      <MasterDetailLayout class="h-full min-h-0 max-lg:h-auto">
        <template #master>
          <section class="grid min-h-0 min-w-0 grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden rounded-xl border border-border bg-card max-lg:min-h-96" :class="selectedId ? 'hidden xl:grid' : ''">
            <div v-if="view === 'operations'" class="flex flex-wrap gap-2 border-b border-border p-3"><Button size="sm" :variant="!read('attention') && !read('phase') ? 'primary' : 'ghost'" @click="updateQuery({ attention: undefined, phase: undefined })">{{ t('activity.all') }}</Button><Button size="sm" :variant="read('attention') ? 'primary' : 'ghost'" @click="updateQuery({ attention: 'true', phase: undefined })">{{ t('activity.attention') }}</Button><Button size="sm" :variant="read('phase') ? 'primary' : 'ghost'" @click="updateQuery({ phase: 'running', attention: undefined })">{{ t('activity.running') }}</Button></div><div v-else class="border-b border-border p-3 text-xs text-muted-foreground">{{ t('activity.receiveOrder') }}</div>
            <div class="relative min-h-0 min-w-0 overflow-y-auto max-lg:max-h-[60dvh]" :aria-busy="loading"><LoadingOverlay v-if="loading && !(events.length || operations.length)" :label="t('activity.loading')" /><EmptyState v-if="error" :title="t('common.loadFailed')" :description="error"><template #actions><Button @click="load">{{ t('common.retry') }}</Button></template></EmptyState><EmptyState v-else-if="!loading && !(view === 'events' ? events.length : operations.length)" :title="t('activity.empty')" :description="t('activity.emptyHint')" />
              <div v-else-if="view === 'events'" class="divide-y divide-border"><button v-for="event in events" :key="event.eventId" type="button" class="motion-list-item block w-full min-w-0 p-3 text-left hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring/40" :class="read('eventId') === event.eventId ? 'bg-accent' : ''" @click="selectEvent(event)"><span class="flex flex-wrap items-center justify-between gap-2"><time class="text-xs text-muted-foreground">{{ formatDateTime(event.occurredAt) }}</time><Badge :tone="eventTone(event.level)">{{ t(`activity.level.${event.level}`) }}</Badge></span><span class="my-2 block break-words text-sm font-medium [overflow-wrap:anywhere]">{{ message(event) }}</span><span class="block truncate text-xs text-muted-foreground">{{ event.resources?.map(resource => resource.nameSnapshot || resource.resourceId).join(' · ') || t('activity.system') }} · {{ event.actor?.name || event.sourceId }}</span></button></div>
              <div v-else class="divide-y divide-border"><button v-for="operation in operations" :key="operation.operationId" type="button" class="motion-list-item block w-full min-w-0 p-3 text-left hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring/40" :class="read('operationId') === operation.operationId ? 'bg-accent' : ''" @click="selectOperation(operation.operationId)"><span class="flex flex-wrap items-center justify-between gap-2"><time class="text-xs text-muted-foreground">{{ formatDateTime(operation.updatedAt) }}</time><Badge :tone="operationTone(operation.result)">{{ stateLabel(operation.result || operation.phase) }}</Badge></span><span class="my-2 block break-words text-sm font-medium">{{ translateEventSummary(t, operation.title || operation.action) }}</span><span class="block truncate text-xs text-muted-foreground">{{ operation.resources?.map(resource => resource.nameSnapshot || resource.resourceId).join(' · ') }}</span><span v-if="operation.failureSummary" class="mt-2 block break-words text-xs text-danger">{{ operation.failureSummary }}</span><span class="mt-2 block text-xs text-muted-foreground">{{ operation.actor?.name || operation.actor?.id }} · {{ t('activity.attempts', { count: operation.attemptCount }) }}</span></button></div>
            </div>
            <PaginationBar mode="cursor" class="px-3" :has-previous="Boolean(read('cursor'))" :has-next="hasMore" :loading="loading" :previous-label="t('common.previous')" :next-label="t('common.next')" :summary-label="t('activity.snapshot', { seq: snapshotSeq })" @previous="previousPage" @next="nextPage" />
          </section>
        </template>
        <template #detail>
          <section class="relative grid min-h-0 min-w-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-xl border border-border bg-card max-lg:min-h-96" :class="!selectedId ? 'hidden xl:grid' : ''">
            <div class="flex flex-wrap items-center justify-between gap-2 border-b border-border p-3"><h2 class="m-0 text-sm font-semibold">{{ t('activity.timeline') }}</h2><div class="flex flex-wrap gap-2"><Button v-for="command in detail?.availableCommands.filter(item => ['retry', 'run-now', 'resolve'].includes(item.kind)) || []" :key="`${command.executionId}:${command.kind}`" size="sm" :disabled="commandLoading" @click="requestCommand(command)">{{ t(command.kind === 'resolve' ? 'activity.resolve' : command.kind === 'retry' ? 'common.retry' : 'common.runNow') }}</Button><Button v-if="detail" size="sm" :loading="exporting" @click="download(true)">{{ t('activity.exportOperation') }}</Button><Button v-if="selectedId" size="sm" variant="ghost" @click="closeDetail">{{ t('common.close') }}</Button></div></div>
            <div class="relative min-h-0 min-w-0 overflow-y-auto p-4"><LoadingOverlay v-if="(detailLoading && !timeline.length) || contextLoading" :label="t('activity.loading')" /><EmptyState v-if="detailError" :title="t('common.loadFailed')" :description="detailError"><template #actions><Button @click="loadDetail">{{ t('common.retry') }}</Button></template></EmptyState><EmptyState v-else-if="!selectedId" :title="t('activity.select')" :description="t('activity.selectHint')" />
              <div v-else class="grid min-w-0 gap-4">
                <div v-if="detail" class="grid gap-2"><h3 class="m-0 break-words text-lg font-semibold">{{ translateEventSummary(t, detail.operation.title || detail.operation.action) }}</h3><div class="flex flex-wrap gap-2"><Badge :tone="operationTone(detail.operation.result)">{{ stateLabel(detail.operation.result || detail.operation.phase) }}</Badge><Badge v-if="detail.operation.hadError" tone="warning">{{ t('activity.hadError') }}</Badge><Badge v-if="!detail.operation.evidenceComplete" tone="warning">{{ t('activity.incomplete') }}</Badge></div><p v-if="detail.operation.failureSummary" class="m-0 break-words text-sm text-danger">{{ detail.operation.failureSummary }}</p><p v-if="detail.operation.uncertainty" class="m-0 text-sm text-warning">{{ t('activity.uncertain') }}</p><details v-if="detail.executions.length"><summary class="cursor-pointer text-sm font-medium">{{ t('activity.executions') }}</summary><div v-for="execution in detail.executions" :key="execution.executionId" class="mt-2 rounded-lg border border-border p-3 text-sm"><div class="flex flex-wrap gap-2"><Badge :tone="operationTone(execution.result)">{{ stateLabel(execution.result || execution.phase) }}</Badge><span class="text-xs text-muted-foreground">{{ formatDateTime(execution.startedAt) }}</span></div><div v-for="step in detail.steps.filter(item => item.executionId === execution.executionId)" :key="step.stepId" class="mt-2 flex flex-wrap items-center justify-between gap-2"><span>{{ step.name }}</span><Badge :tone="operationTone(step.result)">{{ stateLabel(step.result || step.phase) }}</Badge></div></div></details><div v-if="detail.relatedOperations.length" class="flex flex-wrap gap-2"><Button v-for="operationId in detail.relatedOperations" :key="operationId" size="sm" @click="selectOperation(operationId)">{{ t('activity.relatedOperation') }}</Button></div></div>
                <div class="flex flex-wrap items-center gap-2"><Button v-if="detailNewer" size="sm" @click="loadDetail">{{ t('activity.newEvidence') }}</Button><Button v-if="contextMode" size="sm" @click="contextMode = false">{{ t('activity.backTimeline') }}</Button><Button size="sm" :variant="onlyErrors ? 'primary' : 'secondary'" @click="onlyErrors = !onlyErrors">{{ t('activity.warningsErrors') }}</Button><Button size="sm" :loading="detailLoading" @click="loadDetail">{{ t('activity.latestBatch') }}</Button></div>
                <div class="grid gap-2 md:grid-cols-2"><SearchInput v-model="timelineSearch" :label="t('activity.searchBatch')" :placeholder="t('activity.searchBatch')" /><Select v-model="stream" :options="[{ value: '', label: t('activity.allStreams') }, { value: 'stdout', label: 'stdout' }, { value: 'stderr', label: 'stderr' }]" :aria-label="t('activity.stream')" /></div>
                <p v-if="contextMode" class="m-0 text-xs text-muted-foreground">{{ t('activity.contextHint') }}</p><EventTimeline :events="timelineVisible" :selected-event-id="contextMode ? contextSelectedId : read('eventId')" @context="showContext" @evidence="evidence" /><p v-if="!detailLoading && !timelineVisible.length" class="text-sm text-muted-foreground">{{ t('activity.noEventsInBatch') }}</p><div v-if="!contextMode" class="flex flex-wrap gap-2"><Button v-if="timelineWindow.previousCursors.length" :loading="detailLoading" @click="moreTimeline('newer')">{{ t('activity.newerBatch') }}</Button><Button v-if="timelineHasMore" :loading="detailLoading" @click="moreTimeline('older')">{{ t('activity.olderBatch') }}</Button></div><p v-if="!detailLoading" class="m-0 text-xs text-muted-foreground">{{ t('activity.batchHint') }}</p>
              </div>
            </div>
          </section>
        </template>
      </MasterDetailLayout>
    </Tabs>
    <Dialog :open="pendingCommand?.kind === 'resolve'" :title="t('activity.resolveTitle')" :description="t('activity.resolveDescription')" @update:open="value => { if (!value && !commandLoading) pendingCommand = null; }">
      <div class="grid gap-4">
        <p class="m-0 rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning">{{ t('activity.resolveImpact') }}</p>
        <label class="grid gap-2 text-sm"><span>{{ t('activity.resolveOutcome') }}</span><Select v-model="resolveOutcome" :options="[{ value: '', label: t('activity.resolveChoose') }, { value: 'succeeded', label: t('activity.resolveSucceeded') }, { value: 'failed', label: t('activity.resolveFailed') }]" :disabled="commandLoading" /></label>
        <label class="grid gap-2 text-sm"><span>{{ t('activity.resolveReason') }}</span><Textarea v-model="resolveReason" :placeholder="t('activity.resolveReasonHint')" :disabled="commandLoading" required /></label>
      </div>
      <template #footer><Button :disabled="commandLoading" @click="pendingCommand = null">{{ t('common.cancel') }}</Button><Button variant="primary" :loading="commandLoading" :disabled="!resolveOutcome || !resolveReason.trim()" @click="executeCommand">{{ t('activity.resolveConfirm') }}</Button></template>
    </Dialog>
    <ConfirmDialog :open="Boolean(pendingCommand && pendingCommand.kind !== 'resolve')" :title="t('activity.commandTitle')" :impact="t('activity.commandImpact')" :checkbox-label="t('common.confirm')" :confirm-label="t('common.confirm')" :cancel-label="t('common.cancel')" :loading="commandLoading" @update:open="value => { if (!value) pendingCommand = null; }" @confirm="executeCommand" />
  </div>
</template>
