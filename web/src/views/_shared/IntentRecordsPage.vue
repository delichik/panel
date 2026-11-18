<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Eye, MoreHorizontal, RefreshCcw } from '@/icons';
import { eventsApi } from '@/api/events';
import { DIAGNOSE_LOG_LEVELS, logsPath } from '@/api/activityReference';
import { saveBlobDownload } from '@/api/download';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import DateTimeRangePicker from '@/components/ui/DateTimeRangePicker.vue';
import Dialog from '@/components/ui/Dialog.vue';
import DownloadButton from '@/components/ui/DownloadButton.vue';
import Dropdown from '@/components/ui/Dropdown.vue';
import DropdownItem from '@/components/ui/DropdownItem.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import IconButton from '@/components/ui/IconButton.vue';
import LoadingOverlay from '@/components/ui/LoadingOverlay.vue';
import PaginationBar from '@/components/ui/PaginationBar.vue';
import SearchInput from '@/components/ui/SearchInput.vue';
import Select from '@/components/ui/Select.vue';
import Table from '@/components/ui/Table.vue';
import ToggleButton from '@/components/ui/ToggleButton.vue';
import { useErrorToast } from '@/components/ui/toast';
import InfoBar from '@/components/patterns/InfoBar.vue';
import ListCommandBar from '@/components/patterns/ListCommandBar.vue';
import PropertyList, { type PropertyListItem } from '@/components/patterns/PropertyList.vue';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import ListPage from '@/components/templates/ListPage.vue';
import { translateEventSummary, useI18n } from '@/i18n';
import type { Event, EventQuery } from '@/types/events';
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
const filterKeys = ['from', 'to', 'hadError', 'origin', 'module', 'state', 'level'] as const;
const query = computed<EventQuery>(() => {
  const value: EventQuery = { limit: 50, from: read('from') || initialFrom };
  for (const key of filterKeys) if (read(key)) value[key] = read(key);
  return value;
});
const range = computed(() => ({ from: read('from') || initialFrom, to: read('to') }));
const events = ref<Event[]>([]);
const loading = ref(false);
const exporting = ref(false);
const error = ref('');
const hasMore = ref(false);
const nextCursor = ref('');
const listGuard = createLatestRequestGuard();
const detailGuard = createLatestRequestGuard();
const detail = ref<Event | null>(null);
const detailLoading = ref(false);
const detailError = ref('');
const selectedId = computed(() => read('eventId') || read('streamId'));
const detailOpen = computed(() => Boolean(selectedId.value));
let searchTimer: ReturnType<typeof setTimeout> | undefined;
const search = ref(read('q'));

const originOptions = computed(() => [
  { value: '', label: t('eventsPage.filter.allOrigins') },
  { value: 'user', label: t('activity.trigger.user') },
  { value: 'system', label: t('activity.system') },
  { value: 'process', label: t('eventsPage.filter.process') },
]);
const levelOptions = computed(() => [
  { value: '', label: t('eventsPage.filter.allLevels') },
  { value: 'debug', label: t('activity.level.debug') },
  { value: 'info', label: t('activity.level.info') },
  { value: 'warning', label: t('activity.level.warning') },
  { value: 'error', label: t('activity.level.error') },
]);

/**
 * 列精简（≤5）：时间列合并级别徽标（时间为主行、级别为副行），不再单列 level；
 * 模块、标题（title+summary 两行）、状态、操作。次级列（module/state）在 <wide
 * 隐藏（Table hideBelow），窄屏只保留时间+级别、标题与操作；标题列吃剩余宽度。
 */
const columns = computed(() => [
  { key: 'updatedAt', label: t('eventsPage.time'), width: 'w-36 wide:w-40', nowrap: true },
  { key: 'module', label: t('eventsPage.module'), width: 'w-28', nowrap: true, hideBelow: 'wide' as const },
  { key: 'title', label: t('eventsPage.title') },
  { key: 'state', label: t('common.status'), width: 'w-24', nowrap: true, hideBelow: 'wide' as const },
  { key: 'actions', label: t('common.actions'), align: 'right' as const, width: 'w-36 wide:w-52', nowrap: true },
]);

const detailProperties = computed<PropertyListItem[]>(() => {
  const item = detail.value;
  if (!item) return [];
  const items: PropertyListItem[] = [
    { key: 'module', label: t('eventsPage.module'), value: item.module },
    { key: 'origin', label: t('eventsPage.origin'), value: item.origin },
    { key: 'time', label: t('eventsPage.time'), value: formatDateTime(item.updatedAt || item.startedAt) },
  ];
  if (item.operationId) items.push({ key: 'operationId', label: t('logsPage.operationId'), value: item.operationId, mono: true });
  if (item.executionId) items.push({ key: 'executionId', label: t('logsPage.executionId'), value: item.executionId, mono: true });
  return items;
});

function setAttention(value: boolean) {
  updateQuery({ hadError: value ? 'true' : undefined });
}

const visibleEvents = computed(() => {
  const q = search.value.trim().toLowerCase();
  if (!q) return events.value;
  return events.value.filter((item) => JSON.stringify(item).toLowerCase().includes(q));
});

/**
 * ListPage 状态槽契约：首屏 loading / 加载失败 / 无筛选且无事件走页级槽；
 * 有关键字或查询筛选时空结果仍由 Table 的 inset 空态承接，保持既有文案。
 */
const recordsListState = computed<'ready' | 'loading' | 'error' | 'empty'>(() => {
  if (error.value && !events.value.length) return 'error';
  if (loading.value && !events.value.length) return 'loading';
  const filtered = Boolean(search.value.trim() || filterKeys.some((key) => read(key)));
  if (!visibleEvents.value.length && !filtered) return 'empty';
  return 'ready';
});

function stateLabel(value: string) {
  const key = `activity.state.${value}`;
  return t(key) === key ? value : t(key);
}
function levelLabel(level: string) {
  const key = `activity.level.${level}`;
  return t(key) === key ? level : t(key);
}
function titleLabel(item: Event) {
  return item.title || item.summary || item.operationId || item.id;
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
function openDetail(eventId: string) {
  updateQuery({ eventId, streamId: undefined }, false);
}
function closeDetail() {
  updateQuery({ eventId: undefined, streamId: undefined }, false);
}
function openLogs(item: Event, diagnose = true) {
  void router.push(logsPath({
    eventId: item.id,
    operationId: item.operationId,
    executionId: item.executionId,
    level: diagnose ? DIAGNOSE_LOG_LEVELS : undefined,
    from: read('from') || undefined,
    to: read('to') || undefined,
  }));
}

function openPairedLogs(item: Event, kind: 'operation' | 'execution') {
  const eventId = kind === 'execution' ? item.executionId : item.operationId;
  if (!eventId) return;
  void router.push(logsPath({
    eventId,
    operationId: item.operationId,
    executionId: item.executionId,
    level: DIAGNOSE_LOG_LEVELS,
    from: read('from') || undefined,
    to: read('to') || undefined,
  }));
}

function hasPairedLogs(item: Event | null) {
  if (!item?.operationId || !item.executionId) return false;
  return item.operationId !== item.id || item.executionId !== item.id;
}

async function load() {
  const request = listGuard.begin();
  loading.value = true;
  error.value = '';
  const params = { ...query.value, cursor: read('cursor') || undefined };
  try {
    const page = await eventsApi.list(params);
    if (!listGuard.isCurrent(request)) return;
    events.value = page.items;
    hasMore.value = page.hasMore;
    nextCursor.value = page.nextCursor || '';
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
  if (!selectedId.value) {
    detail.value = null;
    return;
  }
  const request = detailGuard.begin();
  detailLoading.value = true;
  detailError.value = '';
  try {
    const match = events.value.find((item) => item.id === selectedId.value);
    const value = match || await eventsApi.get(selectedId.value);
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
    saveBlobDownload(await eventsApi.export({ ...query.value }));
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
  <ConsolePage :title="t('routes.events.title')" :description="t('routes.events.description')">
        <template #actions>
          <Button size="sm" :loading="loading" @click="refresh"><RefreshCcw />{{ t('common.refresh') }}</Button>
          <DownloadButton size="sm" :loading="exporting" :label="t('activity.export')" @click="download" />
        </template>
        <ListPage :state="recordsListState">
          <template #toolbar>
            <div class="grid min-w-0 gap-2">
              <ListCommandBar :label="t('routes.events.title')">
                <template #search>
                  <SearchInput v-model="search" clearable class="w-full" :label="t('common.search')" :placeholder="t('eventsPage.search')" :clear-label="t('common.clearSearch')" />
                </template>
                <template #filters>
                  <div class="w-full min-w-0 sm:w-56">
                    <DateTimeRangePicker :model-value="range" @update:model-value="value => updateQuery({ from: value.from, to: value.to || undefined })" />
                  </div>
                  <Select class="w-32" :model-value="read('origin')" :options="originOptions" :aria-label="t('eventsPage.origin')" @update:model-value="changeFilter('origin', $event)" />
                  <Select class="w-28" :model-value="read('level')" :options="levelOptions" :aria-label="t('activity.level')" @update:model-value="changeFilter('level', $event)" />
                </template>
                <template #actions>
                  <ToggleButton
                    size="sm"
                    :model-value="read('hadError') === 'true'"
                    :label="t('activity.attention')"
                    @update:model-value="setAttention"
                  >
                    {{ t('activity.attention') }}
                  </ToggleButton>
                </template>
              </ListCommandBar>
              <div v-if="read('operationId') || read('executionId')" class="flex flex-wrap items-center gap-2 text-caption text-muted-foreground">
                <span v-if="read('operationId')">{{ t('logsPage.operationFilterHint') }}</span>
                <span v-else-if="read('executionId')">{{ t('activity.executionFilterHint') }}</span>
                <Button
                  size="sm"
                  variant="ghost"
                  @click="updateQuery({ executionId: undefined, operationId: undefined, eventId: undefined, streamId: undefined })"
                >
                  {{ t('activity.clearResource') }}
                </Button>
              </div>
              <div v-if="read('module') || read('state')" class="flex flex-wrap items-center gap-2 text-caption text-muted-foreground">
                <span v-if="read('module')">{{ t('eventsPage.module') }}: {{ read('module') }}</span>
                <span v-if="read('state')">{{ t('eventsPage.state') }}: {{ read('state') }}</span>
                <Button size="sm" variant="ghost" @click="updateQuery({ module: undefined, state: undefined })">{{ t('activity.clearResource') }}</Button>
              </div>
            </div>
          </template>

          <template #loading>
            <Table class="h-full max-lg:max-h-list" row-height="auto" :columns="columns" :rows="[]" :loading="true" :loading-label="t('common.loading')" />
          </template>
          <template #error>
            <EmptyState variant="page" :title="t('common.loadFailed')" :description="error">
              <template #actions><Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button></template>
            </EmptyState>
          </template>
          <template #empty>
            <EmptyState variant="inset" :title="t('eventsPage.empty')" :description="t('eventsPage.emptyHint')" />
          </template>
          <Table
            class="h-full max-lg:max-h-list"
            row-height="auto"
            :columns="columns"
            :rows="visibleEvents"
            row-key="id"
            :active-key="selectedId"
            :loading="loading && !events.length"
            :loading-label="t('common.loading')"
          >
            <template #updatedAt="{ row }">
              <div class="flex min-w-0 flex-col items-start gap-1">
                <span class="whitespace-nowrap text-muted-foreground">{{ formatDateTime(row.updatedAt || row.startedAt) }}</span>
                <Badge v-if="row.level" :tone="eventTone(row.level)">{{ levelLabel(row.level) }}</Badge>
              </div>
            </template>
            <template #module="{ row }">
              <span class="whitespace-nowrap text-muted-foreground">{{ row.module }}</span>
            </template>
            <template #title="{ row }">
              <div class="grid min-w-0 gap-0.5">
                <strong class="block truncate text-body-strong font-semibold text-foreground">{{ translateEventSummary(t, titleLabel(row)) }}</strong>
                <span
                  v-if="row.summary && row.summary !== row.title"
                  class="line-clamp-2 text-caption"
                  :class="row.hadError || row.state === 'failed' ? 'text-danger' : 'text-muted-foreground'"
                >{{ translateEventSummary(t, row.summary) }}</span>
              </div>
            </template>
            <template #state="{ row }">
              <Badge :tone="operationTone(row.state)">{{ stateLabel(row.state || (row.finishedAt ? 'ended' : 'running')) }}</Badge>
            </template>
            <template #actions="{ row }">
              <div class="flex items-center justify-end gap-1">
                <Button size="sm" variant="secondary" @click="openDetail(row.id)"><Eye />{{ t('common.details') }}</Button>
                <!-- 次级行操作在 <wide 收进「更多」，窄屏只保留详情主入口。 -->
                <Button class="max-wide:hidden" size="sm" variant="ghost" @click="openLogs(row)">{{ t('eventsPage.viewLogs') }}</Button>
                <Dropdown class="wide:hidden" align="right">
                  <template #trigger>
                    <IconButton size="sm" :label="t('common.moreActions')"><MoreHorizontal /></IconButton>
                  </template>
                  <DropdownItem @click="openLogs(row)">{{ t('eventsPage.viewLogs') }}</DropdownItem>
                </Dropdown>
              </div>
            </template>
            <template #empty>
              <EmptyState
                variant="inset"
                :title="search ? t('common.filterEmpty') : t('eventsPage.empty')"
                :description="search ? t('common.filterEmptyHint') : t('eventsPage.emptyHint')"
              />
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
            />
          </template>
        </ListPage>

    <!--
      Event detail is a WinUI ContentDialog instead of a second page: the list
      stays visible (URL `eventId` still deep-links the dialog), Escape/close
      clears the selection, and focus returns to the row action.
    -->
    <Dialog
      :open="detailOpen"
      :title="detail ? translateEventSummary(t, titleLabel(detail)) : t('common.loading')"
      @update:open="value => { if (!value) closeDetail(); }"
    >
      <template #footer>
        <Button variant="secondary" @click="closeDetail">{{ t('common.close') }}</Button>
        <Button v-if="detail" variant="primary" @click="openLogs(detail)">{{ t('eventsPage.viewLogs') }}</Button>
      </template>

      <EmptyState v-if="detailError" variant="inset" :title="t('common.loadFailed')" :description="detailError">
        <template #actions><Button size="sm" :loading="detailLoading" @click="loadDetail"><RefreshCcw />{{ t('common.retry') }}</Button></template>
      </EmptyState>
      <div v-else-if="!detail" class="relative grid min-h-40 place-items-center">
        <LoadingOverlay :label="t('common.loading')" />
      </div>
      <div v-else class="grid min-w-0 gap-4">
        <div class="flex flex-wrap gap-2">
          <Badge :tone="operationTone(detail.state)">{{ stateLabel(detail.state || (detail.finishedAt ? 'ended' : 'running')) }}</Badge>
          <Badge v-if="detail.hadError" tone="danger">{{ t('activity.hadError') }}</Badge>
          <Badge v-if="detail.level" :tone="eventTone(detail.level)">{{ levelLabel(detail.level) }}</Badge>
        </div>
        <InfoBar v-if="detail.summary" :tone="detail.hadError || detail.state === 'failed' ? 'danger' : 'info'">
          {{ translateEventSummary(t, detail.summary) }}
        </InfoBar>
        <p v-else class="m-0 text-sm text-muted-foreground">{{ t('eventsPage.noFailureSummary') }}</p>
        <div class="-mx-4">
          <PropertyList :items="detailProperties" :columns="1" />
        </div>
        <div v-if="hasPairedLogs(detail)" class="flex flex-wrap gap-2">
          <Button v-if="detail.operationId && detail.operationId !== detail.id" size="sm" variant="secondary" @click="openPairedLogs(detail, 'operation')">{{ t('eventsPage.viewOperationLogs') }}</Button>
          <Button v-if="detail.executionId && detail.executionId !== detail.id" size="sm" variant="secondary" @click="openPairedLogs(detail, 'execution')">{{ t('eventsPage.viewExecutionLogs') }}</Button>
        </div>
        <p class="m-0 text-sm text-muted-foreground">{{ t('eventsPage.diagnoseHint') }}</p>
      </div>
    </Dialog>
  </ConsolePage>
</template>
