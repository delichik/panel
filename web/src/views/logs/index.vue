<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { RefreshCcw, Eye } from '@/icons';
import { eventsApi } from '@/api/events';
import { logsApi } from '@/api/logs';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import Dialog from '@/components/ui/Dialog.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import PaginationBar from '@/components/ui/PaginationBar.vue';
import SearchInput from '@/components/ui/SearchInput.vue';
import Select from '@/components/ui/Select.vue';
import Table from '@/components/ui/Table.vue';
import ListCommandBar from '@/components/patterns/ListCommandBar.vue';
import PropertyList, { type PropertyListItem } from '@/components/patterns/PropertyList.vue';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import ListPage from '@/components/templates/ListPage.vue';
import { useI18n } from '@/i18n';
import type { Event, LogLine } from '@/types/events';
import { formatDateTime } from '@/utils/datetime';
import { createLatestRequestGuard } from '@/views/_shared/requestState';
import { eventTone } from '@/views/activity/model';

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const read = (key: string) => typeof route.query[key] === 'string' ? route.query[key] as string : '';
const eventId = computed(() => read('eventId') || read('streamId'));
const lineLevel = computed(() => read('level') || 'info,warning,error');
const lines = ref<LogLine[]>([]);
const eventMeta = ref<Event | null>(null);
const loading = ref(false);
const error = ref('');
const hasMore = ref(false);
const nextAfter = ref('');
const linesGuard = createLatestRequestGuard();
let resolveTimer: ReturnType<typeof setTimeout> | undefined;
let levelBootstrapped = false;

/**
 * ListPage 状态槽契约：首屏 loading / 加载失败 / 无筛选且无日志走页级槽；
 * 筛选（event/operation/execution/非默认等级）无结果仍由 Table 的 inset 空态承接。
 */
const logsListState = computed<'ready' | 'loading' | 'error' | 'empty'>(() => {
  if (error.value && !lines.value.length) return 'error';
  if (loading.value && !lines.value.length) return 'loading';
  const filtered = Boolean(eventId.value || read('operationId') || read('executionId'))
    || Boolean(read('level') && read('level') !== 'info,warning,error');
  if (!lines.value.length && !filtered) return 'empty';
  return 'ready';
});

const levelOptions = computed(() => [
  { value: '', label: t('activity.filter.defaultLevels') },
  { value: 'warning,error', label: t('logsPage.filter.diagnoseLevels') },
  { value: 'error', label: t('activity.level.error') },
  { value: 'debug,info,warning,error', label: t('activity.filter.allLevels') },
]);
/**
 * 列精简（4 列）：时间列合并级别徽标（时间主行、级别副行）；eventId 在 <wide
 * 隐藏（详情 Dialog 仍可看完整 eventId）；时间列在 <sm 再隐藏，手机只保留 msg
 * 与操作；msg 保留结构化错误与原文多行；操作列固定宽度。
 */
const columns = computed(() => [
  { key: 'ts', label: t('logsPage.time'), width: 'w-40', nowrap: true, hideBelow: 'sm' as const },
  { key: 'eventId', label: t('logsPage.eventId'), width: 'w-40', nowrap: true, hideBelow: 'wide' as const },
  { key: 'msg', label: t('logsPage.message') },
  { key: 'actions', label: t('common.actions'), align: 'right' as const, width: 'w-28', nowrap: true },
]);

const selectedLine = ref<LogLine | null>(null);
const lineDialogOpen = ref(false);
function openLine(line: LogLine) {
  selectedLine.value = line;
  lineDialogOpen.value = true;
}
function closeLine() {
  lineDialogOpen.value = false;
  selectedLine.value = null;
}

const lineProperties = computed<PropertyListItem[]>(() => {
  const line = selectedLine.value;
  if (!line) return [];
  const items: PropertyListItem[] = [
    { key: 'time', label: t('logsPage.time'), value: formatDateTime(typeof line.ts === 'string' ? line.ts : undefined) },
  ];
  if (line.level) items.push({ key: 'level', label: t('activity.level'), value: levelLabel(String(line.level)) });
  if (line.eventId || eventId.value) items.push({ key: 'eventId', label: t('logsPage.eventId'), value: line.eventId || eventId.value, mono: true });
  if (lineField(line, 'operationId')) items.push({ key: 'operationId', label: t('logsPage.operationId'), value: lineField(line, 'operationId'), mono: true });
  if (lineField(line, 'executionId')) items.push({ key: 'executionId', label: t('logsPage.executionId'), value: lineField(line, 'executionId'), mono: true });
  if (lineField(line, 'errorCode')) items.push({ key: 'errorCode', label: t('logsPage.errorCode'), value: lineField(line, 'errorCode'), mono: true });
  if (lineField(line, 'error')) items.push({ key: 'error', label: t('logsPage.error'), value: lineField(line, 'error') });
  if (lineField(line, 'detail')) items.push({ key: 'detail', label: t('logsPage.detail'), value: lineField(line, 'detail') });
  items.push({ key: 'message', label: t('logsPage.message'), value: lineMessage(line) });
  return items;
});

function lineMessage(line: LogLine) {
  return typeof line.msg === 'string' ? line.msg : JSON.stringify(line);
}
function levelLabel(level: string) {
  const key = `activity.level.${level}`;
  return t(key) === key ? level : t(key);
}
function lineField(line: LogLine, key: string) {
  const value = line[key];
  return typeof value === 'string' ? value.trim() : '';
}
function updateQuery(values: Record<string, string | undefined>, reset = true) {
  void router.replace({
    query: {
      ...route.query,
      ...(reset ? { after: undefined } : {}),
      ...values,
    },
  });
}
function changeFilter(key: string, value: string) {
  updateQuery({ [key]: value || undefined });
}

async function resolveEventIdFromCorrelation() {
  if (eventId.value || (!read('operationId') && !read('executionId'))) return;
  try {
    const page = await eventsApi.list({
      limit: 1,
      operationId: read('operationId') || undefined,
      executionId: read('executionId') || undefined,
    });
    const match = page.items[0];
    if (match) {
      updateQuery({ eventId: match.id, streamId: undefined }, true);
    }
  } catch {
    // Fall through to global load when correlation lookup fails.
  }
}

async function loadLines() {
  const request = linesGuard.begin();
  loading.value = true;
  error.value = '';
  try {
    const [page, meta] = await Promise.all([
      logsApi.list({
        eventId: eventId.value || undefined,
        level: lineLevel.value,
        limit: 200,
        after: read('after') || undefined,
      }),
      eventId.value
        ? (eventMeta.value?.id === eventId.value ? Promise.resolve(eventMeta.value) : eventsApi.get(eventId.value).catch(() => null))
        : Promise.resolve(null),
    ]);
    if (!linesGuard.isCurrent(request)) return;
    lines.value = page.items;
    hasMore.value = page.hasMore;
    nextAfter.value = page.nextCursor || '';
    eventMeta.value = meta;
  } catch (err) {
    if (!linesGuard.isCurrent(request)) return;
    lines.value = [];
    error.value = err instanceof Error ? err.message : t('common.loadFailed');
  } finally {
    if (linesGuard.isCurrent(request)) loading.value = false;
  }
}

function previousPage() {
  updateQuery({ after: undefined }, false);
}
function nextPage() {
  if (!nextAfter.value) return;
  updateQuery({ after: nextAfter.value }, false);
}
function refresh() {
  updateQuery({});
  void loadLines();
}
function openEvents() {
  void router.push({ path: '/events', query: { from: read('from') || undefined } });
}
function filterByEvent(id: string) {
  const value = id.trim();
  if (!value) return;
  updateQuery({ eventId: value, streamId: undefined });
}

watch(eventId, () => {
  if (!eventId.value && (read('operationId') || read('executionId'))) {
    clearTimeout(resolveTimer);
    resolveTimer = setTimeout(() => { void resolveEventIdFromCorrelation(); }, 0);
    return;
  }
  void loadLines();
});
watch(lineLevel, () => {
  if (!levelBootstrapped) {
    levelBootstrapped = true;
    return;
  }
  updateQuery({}, true);
});
watch(() => read('after'), () => {
  void loadLines();
});
onMounted(() => {
  if (!read('level')) {
    updateQuery({ level: 'info,warning,error' }, false);
  } else {
    levelBootstrapped = true;
  }
  if (!eventId.value && (read('operationId') || read('executionId'))) {
    void resolveEventIdFromCorrelation();
  } else {
    void loadLines();
  }
});
onBeforeUnmount(() => {
  clearTimeout(resolveTimer);
  linesGuard.invalidate();
});
</script>

<template>
  <ConsolePage class="h-full" :title="t('routes.logs.title')" :description="t('routes.logs.description')">
    <template #actions>
      <Button size="sm" variant="ghost" @click="openEvents">{{ t('eventsPage.openEvents') }}</Button>
      <Button size="sm" :loading="loading" @click="refresh"><RefreshCcw />{{ t('common.refresh') }}</Button>
    </template>

    <ListPage :state="logsListState">
      <template #toolbar>
        <div class="grid min-w-0 gap-2">
          <ListCommandBar :label="t('routes.logs.title')">
            <template #search>
              <SearchInput
                :model-value="eventId"
                class="w-full"
                :label="t('logsPage.eventId')"
                :placeholder="t('logsPage.eventIdOptional')"
                @update:model-value="changeFilter('eventId', $event)"
              />
            </template>
            <template #filters>
              <Select class="w-44 max-sm:w-full" :model-value="read('level')" :options="levelOptions" :aria-label="t('activity.level')" @update:model-value="changeFilter('level', $event)" />
            </template>
          </ListCommandBar>
          <div v-if="eventMeta" class="truncate text-caption text-muted-foreground">
            {{ eventMeta.title || eventMeta.summary || eventMeta.id }}
          </div>
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
        </div>
      </template>

      <template #loading>
        <Table class="h-full max-lg:max-h-list" row-height="auto" :columns="columns" :rows="[]" :loading="true" :loading-label="t('common.loading')" />
      </template>
      <template #error>
        <EmptyState variant="page" :title="t('common.loadFailed')" :description="error">
          <template #actions><Button size="sm" :loading="loading" @click="loadLines"><RefreshCcw />{{ t('common.retry') }}</Button></template>
        </EmptyState>
      </template>
      <template #empty>
        <EmptyState variant="inset" :title="t('logsPage.linesEmpty')" :description="t('logsPage.linesEmptyHint')" />
      </template>
      <Table
        class="h-full max-lg:max-h-list"
        row-height="auto"
        :columns="columns"
        :rows="lines"
        :loading="loading && !lines.length"
        :loading-label="t('common.loading')"
      >
        <template #ts="{ row }">
          <div class="flex min-w-0 flex-col items-start gap-1">
            <span class="whitespace-nowrap text-muted-foreground">{{ formatDateTime(typeof row.ts === 'string' ? row.ts : undefined) }}</span>
            <Badge v-if="row.level" :tone="eventTone(String(row.level))">{{ levelLabel(String(row.level)) }}</Badge>
          </div>
        </template>
        <template #eventId="{ row }">
          <button
            v-if="row.eventId || eventId"
            type="button"
            class="block w-full truncate text-left font-mono text-xs text-muted-foreground underline-offset-2 hover:underline"
            :title="String(row.eventId || eventId)"
            @click="filterByEvent(String(row.eventId || eventId))"
          >
            {{ row.eventId || eventId }}
          </button>
        </template>
        <template #msg="{ row }">
          <div class="grid min-w-0 gap-1.5">
            <dl v-if="lineField(row, 'errorCode') || lineField(row, 'error') || lineField(row, 'detail')" class="grid min-w-0 gap-1 text-body">
              <div v-if="lineField(row, 'errorCode')" class="flex min-w-0 flex-wrap items-baseline gap-x-2">
                <dt class="shrink-0 text-caption text-muted-foreground">{{ t('logsPage.errorCode') }}</dt>
                <dd class="m-0 min-w-0 break-all font-mono">{{ lineField(row, 'errorCode') }}</dd>
              </div>
              <div v-if="lineField(row, 'error')" class="flex min-w-0 flex-wrap items-baseline gap-x-2">
                <dt class="shrink-0 text-caption text-muted-foreground">{{ t('logsPage.error') }}</dt>
                <dd class="m-0 min-w-0 break-words [overflow-wrap:anywhere]">{{ lineField(row, 'error') }}</dd>
              </div>
              <div v-if="lineField(row, 'detail')" class="flex min-w-0 flex-wrap items-baseline gap-x-2">
                <dt class="shrink-0 text-caption text-muted-foreground">{{ t('logsPage.detail') }}</dt>
                <dd class="m-0 min-w-0 break-words [overflow-wrap:anywhere]">{{ lineField(row, 'detail') }}</dd>
              </div>
            </dl>
            <pre class="m-0 whitespace-pre-wrap break-words font-mono text-body [overflow-wrap:anywhere]">{{ lineMessage(row) }}</pre>
          </div>
        </template>
        <template #actions="{ row }">
          <Button size="sm" variant="secondary" @click="openLine(row)"><Eye />{{ t('common.details') }}</Button>
        </template>
        <template #empty>
          <EmptyState variant="inset" :title="t('logsPage.linesEmpty')" :description="t('logsPage.linesEmptyHint')" />
        </template>
      </Table>

      <template #pagination>
        <PaginationBar
          mode="cursor"
          :has-previous="Boolean(read('after'))"
          :has-next="hasMore"
          :loading="loading"
          :previous-label="t('common.previous')"
          :next-label="t('common.next')"
          @previous="previousPage"
          @next="nextPage"
        />
      </template>
    </ListPage>

    <!-- Log detail is a WinUI ContentDialog; the list stays visible and its filters/cursor state is untouched. -->
    <Dialog
      :open="lineDialogOpen"
      :title="t('logsPage.detailTitle')"
      @update:open="value => { if (!value) closeLine(); }"
    >
      <template #footer>
        <div class="flex w-full flex-col gap-2 sm:flex-row sm:justify-end">
          <Button class="max-sm:w-full" variant="secondary" @click="closeLine">{{ t('common.close') }}</Button>
          <Button
            v-if="selectedLine && (selectedLine.eventId || eventId)"
            class="max-sm:w-full"
            variant="primary"
            @click="filterByEvent(String(selectedLine.eventId || eventId)); closeLine()"
          >
            {{ t('eventsPage.viewLogs') }}
          </Button>
        </div>
      </template>

      <div v-if="selectedLine" class="-mx-4">
        <PropertyList :items="lineProperties" :columns="1">
          <template #value="{ item, text }">
            <Badge v-if="item.key === 'level'" :tone="eventTone(String(selectedLine?.level ?? ''))">{{ text }}</Badge>
            <pre v-else-if="item.key === 'message'" class="m-0 whitespace-pre-wrap break-words font-mono text-body [overflow-wrap:anywhere]">{{ text }}</pre>
            <span v-else class="min-w-0 break-all" :class="item.mono ? 'panel-mono' : undefined" :title="text">{{ text }}</span>
          </template>
        </PropertyList>
      </div>
    </Dialog>
  </ConsolePage>
</template>
