<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { RefreshCcw } from '@lucide/vue';
import { eventsApi } from '@/api/events';
import { logsApi } from '@/api/logs';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import PaginationBar from '@/components/ui/PaginationBar.vue';
import SearchInput from '@/components/ui/SearchInput.vue';
import Select from '@/components/ui/Select.vue';
import Table from '@/components/ui/Table.vue';
import { useErrorToast } from '@/components/ui/toast';
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
const notifyError = useErrorToast();
const read = (key: string) => typeof route.query[key] === 'string' ? route.query[key] as string : '';
const eventId = computed(() => read('eventId') || read('streamId'));
const lineLevel = computed(() => read('level') || 'warning,error');
const lines = ref<LogLine[]>([]);
const eventMeta = ref<Event | null>(null);
const loading = ref(false);
const error = ref('');
const hasMore = ref(false);
const nextAfter = ref('');
const linesGuard = createLatestRequestGuard();
let resolveTimer: ReturnType<typeof setTimeout> | undefined;

const levelOptions = computed(() => [
  { value: '', label: t('logsPage.filter.diagnoseLevels') },
  { value: 'error', label: t('activity.level.error') },
  { value: 'info,warning,error', label: t('activity.filter.defaultLevels') },
  { value: 'debug,info,warning,error', label: t('activity.filter.allLevels') },
]);
const columns = computed(() => [
  { key: 'ts', label: t('logsPage.time'), nowrap: true },
  { key: 'level', label: t('activity.level'), nowrap: true },
  { key: 'eventId', label: t('logsPage.eventId'), nowrap: true },
  { key: 'msg', label: t('logsPage.message') },
]);

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
    // Leave the page in pick-an-event state when correlation lookup fails.
  }
}

async function loadLines() {
  if (!eventId.value) {
    lines.value = [];
    eventMeta.value = null;
    error.value = '';
    return;
  }
  const request = linesGuard.begin();
  loading.value = true;
  error.value = '';
  try {
    const [page, meta] = await Promise.all([
      logsApi.list({
        eventId: eventId.value,
        level: lineLevel.value,
        limit: 200,
        after: read('after') || undefined,
      }),
      eventMeta.value?.id === eventId.value ? Promise.resolve(eventMeta.value) : eventsApi.get(eventId.value).catch(() => null),
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
    notifyError(error.value, err);
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

watch(eventId, () => {
  if (!eventId.value && (read('operationId') || read('executionId'))) {
    clearTimeout(resolveTimer);
    resolveTimer = setTimeout(() => { void resolveEventIdFromCorrelation(); }, 0);
    return;
  }
  void loadLines();
});
watch(lineLevel, () => {
  if (eventId.value) updateQuery({}, true);
});
watch(() => read('after'), () => {
  if (eventId.value) void loadLines();
});
onMounted(() => {
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
      <Button size="sm" :loading="loading" :disabled="!eventId" @click="refresh"><RefreshCcw />{{ t('common.refresh') }}</Button>
    </template>

    <ListPage>
      <template #toolbar>
        <div class="grid min-w-0 gap-3">
          <div class="grid min-w-0 grid-cols-2 gap-3 xl:grid-cols-3">
            <SearchInput
              :model-value="eventId"
              :label="t('logsPage.eventId')"
              :placeholder="t('logsPage.eventId')"
              @update:model-value="changeFilter('eventId', $event)"
            />
            <Select :model-value="read('level')" :options="levelOptions" :aria-label="t('activity.level')" @update:model-value="changeFilter('level', $event)" />
          </div>
          <div v-if="eventMeta" class="text-sm text-muted-foreground">
            {{ eventMeta.title || eventMeta.summary || eventMeta.id }}
          </div>
          <div v-if="read('operationId') || read('executionId')" class="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
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

      <EmptyState
        v-if="!eventId"
        variant="page"
        :title="t('logsPage.pickEventTitle')"
        :description="t('logsPage.pickEventHint')"
      >
        <template #actions><Button size="sm" @click="openEvents">{{ t('eventsPage.openEvents') }}</Button></template>
      </EmptyState>
      <EmptyState v-else-if="error && !lines.length" variant="page" :title="t('common.loadFailed')" :description="error">
        <template #actions><Button size="sm" :loading="loading" @click="loadLines"><RefreshCcw />{{ t('common.retry') }}</Button></template>
      </EmptyState>
      <Table
        v-else
        class="h-full max-lg:max-h-[60dvh]"
        :columns="columns"
        :rows="lines"
        :loading="loading && !lines.length"
        :loading-label="t('common.loading')"
      >
        <template #ts="{ row }">
          <span class="text-muted-foreground">{{ formatDateTime(typeof row.ts === 'string' ? row.ts : undefined) }}</span>
        </template>
        <template #level="{ row }">
          <Badge v-if="row.level" :tone="eventTone(String(row.level))">{{ levelLabel(String(row.level)) }}</Badge>
        </template>
        <template #eventId="{ row }">
          <span class="font-mono text-xs text-muted-foreground">{{ row.eventId || eventId }}</span>
        </template>
        <template #msg="{ row }">
          <div class="grid min-w-0 gap-2">
            <dl v-if="lineField(row, 'errorCode') || lineField(row, 'error') || lineField(row, 'detail')" class="grid gap-2 text-sm">
              <div v-if="lineField(row, 'errorCode')"><dt class="text-muted-foreground">{{ t('logsPage.errorCode') }}</dt><dd class="font-mono">{{ lineField(row, 'errorCode') }}</dd></div>
              <div v-if="lineField(row, 'error')"><dt class="text-muted-foreground">{{ t('logsPage.error') }}</dt><dd class="break-words [overflow-wrap:anywhere]">{{ lineField(row, 'error') }}</dd></div>
              <div v-if="lineField(row, 'detail')"><dt class="text-muted-foreground">{{ t('logsPage.detail') }}</dt><dd class="break-words [overflow-wrap:anywhere]">{{ lineField(row, 'detail') }}</dd></div>
            </dl>
            <pre class="m-0 whitespace-pre-wrap break-words font-mono text-sm [overflow-wrap:anywhere]">{{ lineMessage(row) }}</pre>
          </div>
        </template>
        <template #empty>
          <EmptyState variant="inset" :title="t('logsPage.linesEmpty')" :description="t('logsPage.linesEmptyHint')" />
        </template>
      </Table>

      <template #pagination>
        <PaginationBar
          v-if="eventId"
          mode="cursor"
          :has-previous="Boolean(read('after'))"
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
