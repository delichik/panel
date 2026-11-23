<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { AlertTriangle, RefreshCcw } from '@/icons';
import { serversApi } from '@/api/servers';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import Input from '@/components/ui/Input.vue';
import PaginationBar from '@/components/ui/PaginationBar.vue';
import Select from '@/components/ui/Select.vue';
import Table from '@/components/ui/Table.vue';
import { useI18n } from '@/i18n';
import type { ServerDto } from '@/types/servers';
import type { SshAccessAttempt, SshAccessCoverage, SshAccessSession } from '@/types/sshAccess';
import { formatDateTime } from '@/utils/datetime';
import { createLatestRequestGuard, isAbortError } from '@/views/_shared/requestState';

const props = defineProps<{ server: ServerDto; active?: boolean }>();
const { t } = useI18n();

type Mode = 'sessions' | 'attempts';
type RangeKey = '1' | '7' | '30' | 'custom';

const mode = ref<Mode>('sessions');
const range = ref<RangeKey>('30');
const customFrom = ref('');
const customTo = ref('');
const state = ref<'all' | 'open'>('all');
const user = ref('');
const source = ref('');
const page = ref(1);
const pageSize = ref(20);
const sessions = ref<SshAccessSession[]>([]);
const attempts = ref<SshAccessAttempt[]>([]);
const total = ref(0);
const coverage = ref<SshAccessCoverage | null>(null);
const loading = ref(false);
const error = ref('');
const guard = createLatestRequestGuard();
let controller: AbortController | null = null;
let timer: ReturnType<typeof setInterval> | null = null;

const rangeOptions = computed(() => [
  { value: '1', label: t('serversPage.sshAccess.rangeToday') },
  { value: '7', label: t('serversPage.sshAccess.range7') },
  { value: '30', label: t('serversPage.sshAccess.range30') },
  { value: 'custom', label: t('serversPage.sshAccess.rangeCustom') },
]);

const modeOptions = computed(() => [
  { value: 'sessions', label: t('serversPage.sshAccess.sessions') },
  { value: 'attempts', label: t('serversPage.sshAccess.attempts') },
]);

const stateOptions = computed(() => [
  { value: 'all', label: t('serversPage.sshAccess.stateAll') },
  { value: 'open', label: t('serversPage.sshAccess.stateOpen') },
]);

const sessionColumns = computed(() => [
  { key: 'user', label: t('serversPage.sshAccess.user'), nowrap: true },
  { key: 'source', label: t('serversPage.sshAccess.source') },
  { key: 'tty', label: t('serversPage.sshAccess.tty'), nowrap: true, hideBelow: 'md' as const },
  { key: 'loginAt', label: t('serversPage.sshAccess.loginAt'), nowrap: true },
  { key: 'logoutAt', label: t('serversPage.sshAccess.logoutAt'), nowrap: true },
  { key: 'duration', label: t('serversPage.sshAccess.duration'), nowrap: true, hideBelow: 'sm' as const },
  { key: 'status', label: t('serversPage.sshAccess.status'), nowrap: true },
]);

const attemptColumns = computed(() => [
  { key: 'attemptAt', label: t('serversPage.sshAccess.attemptAt'), nowrap: true },
  { key: 'user', label: t('serversPage.sshAccess.user'), nowrap: true },
  { key: 'source', label: t('serversPage.sshAccess.source') },
  { key: 'pid', label: t('serversPage.sshAccess.pid'), nowrap: true, hideBelow: 'md' as const },
]);

const columns = computed(() => (mode.value === 'sessions' ? sessionColumns.value : attemptColumns.value));
/** Table 的插槽按行取列；两个分段共用一张表，因此按并集读取字段。 */
type SshAccessRow = SshAccessSession & SshAccessAttempt;
const rows = computed<SshAccessRow[]>(() => (mode.value === 'sessions' ? sessions.value : attempts.value) as SshAccessRow[]);

const windowStart = computed(() => {
  if (range.value === 'custom') return customFrom.value ? new Date(`${customFrom.value}T00:00:00`).toISOString() : undefined;
  const days = Number(range.value);
  return new Date(Date.now() - days * 24 * 60 * 60 * 1000).toISOString();
});
const windowEnd = computed(() => {
  if (range.value !== 'custom') return undefined;
  return customTo.value ? new Date(`${customTo.value}T23:59:59.999`).toISOString() : undefined;
});

function formatDuration(seconds: number | undefined): string {
  if (seconds === undefined || seconds < 0) return t('common.notAvailable');
  const total = Math.floor(seconds);
  const days = Math.floor(total / 86400);
  const hours = Math.floor((total % 86400) / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  if (days > 0) return `${days}d ${hours}h`;
  if (hours > 0) return `${hours}h ${minutes}m`;
  return `${minutes}m`;
}

function sourceLabel(item: SshAccessSession | SshAccessAttempt): string {
  return item.sourceHost || item.sourceAddress || t('common.notAvailable');
}

function coverageText(): string {
  const value = coverage.value;
  if (!value || value.state === 'unknown') return t('serversPage.sshAccess.coverageUnknown');
  if (value.state === 'stale') return t('serversPage.sshAccess.coverageStale').replace('{time}', value.lastLiveAt ? formatDateTime(value.lastLiveAt) : '-');
  return t('serversPage.sshAccess.coverageLive');
}

function coverageTone(): 'success' | 'warning' | 'neutral' {
  if (!coverage.value || coverage.value.state === 'unknown') return 'neutral';
  if (coverage.value.state === 'stale') return 'warning';
  return 'success';
}

function query() {
  const result: Record<string, string | number> = { page: page.value, pageSize: pageSize.value };
  if (windowStart.value) result.from = windowStart.value;
  if (windowEnd.value) result.to = windowEnd.value;
  if (user.value.trim()) result.user = user.value.trim();
  if (source.value.trim()) result.source = source.value.trim();
  if (mode.value === 'sessions' && state.value === 'open') result.state = 'open';
  return result;
}

async function load() {
  const requestId = guard.begin();
  controller?.abort();
  controller = new AbortController();
  loading.value = true;
  try {
    if (mode.value === 'sessions') {
      const result = await serversApi.sshAccessSessions(props.server.id, query(), { signal: controller.signal });
      if (!guard.isCurrent(requestId)) return;
      sessions.value = result.items ?? [];
      total.value = result.total ?? 0;
      coverage.value = result.coverage ?? null;
    } else {
      const result = await serversApi.sshAccessAttempts(props.server.id, query(), { signal: controller.signal });
      if (!guard.isCurrent(requestId)) return;
      attempts.value = result.items ?? [];
      total.value = result.total ?? 0;
      coverage.value = result.coverage ?? null;
    }
    error.value = '';
  } catch (err) {
    if (isAbortError(err)) return;
    // 保留旧数据；错误就地展示并可重试。
    error.value = err instanceof Error ? err.message : t('serversPage.sshAccess.loadFailed');
  } finally {
    if (guard.isCurrent(requestId)) loading.value = false;
  }
}

function restartPolling() {
  if (timer) clearInterval(timer);
  timer = null;
  if (!props.active || document.hidden) return;
  timer = setInterval(() => { void load(); }, mode.value === 'sessions' ? 5000 : 30000);
}

function onVisibilityChange() {
  restartPolling();
  if (props.active && !document.hidden) void load();
}

function reload() {
  page.value = 1;
  void load();
}

watch([mode, range, state, customFrom, customTo], reload);
watch(page, () => { void load(); });
watch([user, source], () => {
  page.value = 1;
  void load();
});
watch(() => props.active, (active) => { if (active) void load(); restartPolling(); });
watch(() => props.server.id, reload);

onMounted(() => {
  document.addEventListener('visibilitychange', onVisibilityChange);
  void load();
  restartPolling();
});
onBeforeUnmount(() => {
  document.removeEventListener('visibilitychange', onVisibilityChange);
  controller?.abort();
  if (timer) clearInterval(timer);
});
</script>

<template>
  <div class="flex h-full min-h-0 flex-col gap-3">
    <div class="flex shrink-0 flex-wrap items-center gap-2">
      <Select class="w-40" :model-value="mode" :options="modeOptions" :aria-label="t('serversPage.sshAccess.view')" @update:model-value="mode = $event as Mode" />
      <Select class="w-36" :model-value="range" :options="rangeOptions" :aria-label="t('serversPage.sshAccess.range')" @update:model-value="range = $event as RangeKey" />
      <template v-if="range === 'custom'">
        <Input v-model="customFrom" type="date" class="w-40" :aria-label="t('serversPage.sshAccess.from')" />
        <Input v-model="customTo" type="date" class="w-40" :aria-label="t('serversPage.sshAccess.to')" />
      </template>
      <Select v-if="mode === 'sessions'" class="w-36" :model-value="state" :options="stateOptions" :aria-label="t('serversPage.sshAccess.state')" @update:model-value="state = $event as 'all' | 'open'" />
      <Input v-model="user" class="w-40" :placeholder="t('serversPage.sshAccess.filterUser')" :aria-label="t('serversPage.sshAccess.filterUser')" />
      <Input v-model="source" class="w-48" :placeholder="t('serversPage.sshAccess.filterSource')" :aria-label="t('serversPage.sshAccess.filterSource')" />
      <Button variant="secondary" :loading="loading" @click="reload"><RefreshCcw />{{ t('common.refresh') }}</Button>
      <Badge class="ml-auto" :tone="coverageTone()">{{ coverageText() }}</Badge>
    </div>

    <div v-if="error" class="flex shrink-0 items-center gap-2 rounded-md border border-danger/30 bg-danger/5 px-3 py-2 text-caption text-danger">
      <AlertTriangle class="size-4 shrink-0" />
      <span class="min-w-0 flex-1">{{ error }}</span>
      <Button variant="secondary" size="sm" @click="load">{{ t('common.retry') }}</Button>
    </div>

    <div class="min-h-0 flex-1">
      <Table
        :columns="columns"
        :rows="rows"
        row-key="id"
        :loading="loading"
        :loading-label="t('common.loading')"
      >
        <template #user="{ row }">
          <span class="flex items-center gap-1.5">
            <span class="font-medium">{{ row.user || t('common.notAvailable') }}</span>
          </span>
        </template>
        <template #source="{ row }">
          <div class="flex min-w-0 flex-col">
            <span class="truncate">{{ sourceLabel(row) }}</span>
            <span v-if="row.sourceHost && row.sourceAddress" class="text-caption text-muted-foreground">{{ row.sourceAddress }}</span>
          </div>
        </template>
        <template #tty="{ row }">{{ row.tty || '-' }}</template>
        <template #loginAt="{ row }">{{ formatDateTime(row.loginAt) }}</template>
        <template #logoutAt="{ row }">
          <span v-if="row.logoutAt" :title="row.logoutOrigin === 'wtmp' ? t('serversPage.sshAccess.logoutWtmp') : t('serversPage.sshAccess.logoutLive')">{{ formatDateTime(row.logoutAt) }}</span>
          <span v-else class="text-muted-foreground">{{ t('serversPage.sshAccess.stillOnline') }}</span>
        </template>
        <template #duration="{ row }">{{ formatDuration(row.durationSeconds) }}</template>
        <template #status="{ row }">
          <Badge :tone="row.logoutAt ? 'neutral' : 'success'">{{ row.logoutAt ? t('serversPage.sshAccess.ended') : t('serversPage.sshAccess.online') }}</Badge>
        </template>
        <template #attemptAt="{ row }">{{ formatDateTime(row.attemptAt) }}</template>
        <template #pid="{ row }">{{ row.pid }}</template>
      </Table>
      <EmptyState v-if="!loading && !rows.length && !error" class="py-10" :title="t('serversPage.sshAccess.empty')" :description="t('serversPage.sshAccess.emptyHint')" />
    </div>

    <PaginationBar
      v-if="total > pageSize"
      v-model:page="page"
      :page-size="pageSize"
      :total="total"
      :loading="loading"
      :previous-label="t('common.previous')"
      :next-label="t('common.next')"
    />
  </div>
</template>
