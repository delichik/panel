<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Download, Eye, RefreshCcw } from '@lucide/vue';
import { apiClient } from '@/api/client';
import { idempotencyKey } from '@/api/assetRequests';
import { activityApi } from '@/api/activity';
import { logsPath } from '@/api/activityReference';
import { saveBlobDownload } from '@/api/download';
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue';
import Dialog from '@/components/ui/Dialog.vue';
import Textarea from '@/components/ui/Textarea.vue';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import DateTimeRangePicker from '@/components/ui/DateTimeRangePicker.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import PaginationBar from '@/components/ui/PaginationBar.vue';
import SearchInput from '@/components/ui/SearchInput.vue';
import Select from '@/components/ui/Select.vue';
import Table from '@/components/ui/Table.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import ListPage from '@/components/templates/ListPage.vue';
import TableDetailLayout from '@/components/templates/TableDetailLayout.vue';
import { translateEventSummary, useI18n } from '@/i18n';
import type { ActivityOperation, ActivityOperationDetail, ActivityQuery } from '@/types/activity';
import { formatDateTime } from '@/utils/datetime';
import { createLatestRequestGuard } from '@/views/_shared/requestState';
import { initializeActivityList } from '@/views/activity/initialLoad';
import { manualResolution, operationTone } from '@/views/activity/model';

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
const notifySuccess = useSuccessToast();

const read = (key: string) => typeof route.query[key] === 'string' ? route.query[key] as string : '';
const initialFrom = new Date(Date.now() - 86400000).toISOString();
const filterKeys = ['q', 'resourceType', 'resourceId', 'phase', 'result', 'attention', 'from', 'to'] as const;
const query = computed<ActivityQuery>(() => {
  const value: ActivityQuery = { limit: 50, from: read('from') || initialFrom, origin: props.origin };
  for (const key of filterKeys) if (read(key)) value[key] = read(key);
  return value;
});
const range = computed(() => ({ from: read('from') || initialFrom, to: read('to') }));
const operations = ref<ActivityOperation[]>([]);
const loading = ref(false);
const exporting = ref(false);
const error = ref('');
const hasMore = ref(false);
const nextCursor = ref('');
const snapshotSeq = ref(0);
const indexState = ref('');
const listGuard = createLatestRequestGuard();
const detailGuard = createLatestRequestGuard();
const detail = ref<ActivityOperationDetail | null>(null);
const detailLoading = ref(false);
const detailError = ref('');
const selectedId = computed(() => read('operationId'));
const detailOpen = computed(() => Boolean(selectedId.value));
let searchTimer: ReturnType<typeof setTimeout> | undefined;
const search = ref(read('q'));
const pendingCommand = ref<{ kind: string; executionId: string } | null>(null);
const commandLoading = ref(false);
const resolveOutcome = ref('');
const resolveReason = ref('');

const columns = computed(() => [
  { key: 'updatedAt', label: t('intentRecords.time'), nowrap: true },
  { key: 'title', label: t('intentRecords.title') },
  { key: 'resources', label: t('intentRecords.resources') },
  { key: 'result', label: t('common.status'), nowrap: true },
  { key: 'actions', label: t('common.actions'), align: 'right' as const, nowrap: true },
]);

function stateLabel(value: string) {
  const key = `activity.state.${value}`;
  return t(key) === key ? value : t(key);
}
function resourceLabel(item: ActivityOperation) {
  return item.resources?.map((resource) => resource.nameSnapshot || resource.resourceId).filter(Boolean).join(' · ') || t('activity.system');
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
function openDetail(operationId: string) {
  updateQuery({ operationId }, false);
}
function closeDetail() {
  updateQuery({ operationId: undefined }, false);
}
function openLogs(operationId: string, diagnose = true) {
  void router.push(logsPath({
    operationId,
    // Jump into diagnostic lines first; the logs page can widen the level filter.
    level: diagnose ? 'warning,error' : undefined,
    from: read('from') || undefined,
    to: read('to') || undefined,
  }));
}

async function load() {
  const request = listGuard.begin();
  loading.value = true;
  error.value = '';
  const params = { ...query.value, cursor: read('cursor') || undefined, snapshotSeq: read('snapshotSeq') || undefined };
  try {
    const page = await activityApi.operations(params);
    if (!listGuard.isCurrent(request)) return;
    operations.value = page.items;
    hasMore.value = page.hasMore;
    nextCursor.value = page.nextCursor || '';
    snapshotSeq.value = page.snapshotSeq;
    indexState.value = page.indexState;
  } catch (err) {
    if (!listGuard.isCurrent(request)) return;
    operations.value = [];
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
    const value = await activityApi.operation(selectedId.value, { snapshotSeq: snapshotSeq.value || undefined });
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
  if (selectedId.value) void loadDetail();
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
function requestCommand(command: { kind: string; executionId: string }) {
  resolveOutcome.value = '';
  resolveReason.value = '';
  pendingCommand.value = command;
}
async function executeCommand() {
  if (!pendingCommand.value || commandLoading.value) return;
  const command = pendingCommand.value;
  if (!['retry', 'run-now', 'resolve'].includes(command.kind)) return;
  const resolution = manualResolution(resolveOutcome.value, resolveReason.value);
  if (command.kind === 'resolve' && !resolution) return;
  commandLoading.value = true;
  try {
    const receipt = await apiClient.post(
      `/executions/${encodeURIComponent(command.executionId)}/${command.kind}`,
      command.kind === 'resolve' ? resolution : undefined,
      { headers: { 'Idempotency-Key': idempotencyKey() } },
    );
    pendingCommand.value = null;
    notifySuccess(t(command.kind === 'resolve' ? 'activity.resolveRecorded' : 'activity.commandAccepted'), receipt);
    await loadDetail();
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    commandLoading.value = false;
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
          <Button size="sm" :loading="exporting" :disabled="!snapshotSeq" @click="download"><Download />{{ t('activity.export') }}</Button>
        </template>
        <ListPage>
          <template #toolbar>
            <div class="grid min-w-0 gap-3">
              <div class="grid min-w-0 gap-3 lg:grid-cols-2">
                <SearchInput v-model="search" clearable :label="t('common.search')" :placeholder="t('intentRecords.search')" :clear-label="t('common.clearSearch')" />
                <DateTimeRangePicker :model-value="range" @update:model-value="value => updateQuery({ from: value.from, to: value.to || undefined })" />
              </div>
              <div class="flex flex-wrap gap-2">
                <Button size="sm" :variant="!read('attention') && !read('phase') ? 'primary' : 'ghost'" @click="updateQuery({ attention: undefined, phase: undefined })">{{ t('activity.all') }}</Button>
                <Button size="sm" :variant="read('attention') ? 'primary' : 'ghost'" @click="updateQuery({ attention: 'true', phase: undefined })">{{ t('activity.attention') }}</Button>
                <Button size="sm" :variant="read('phase') ? 'primary' : 'ghost'" @click="updateQuery({ phase: 'running', attention: undefined })">{{ t('activity.running') }}</Button>
                <Badge v-if="indexState && indexState !== 'ready'" tone="warning">{{ t('activity.indexState') }}</Badge>
              </div>
              <div v-if="read('resourceId') || read('resourceType')" class="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                <span>{{ t('intentRecords.resourceFilterHint') }}</span>
                <Badge v-if="read('resourceType')" tone="neutral">{{ read('resourceType') }}</Badge>
                <Badge v-if="read('resourceId')" tone="neutral">{{ read('resourceId') }}</Badge>
                <Button size="sm" variant="ghost" @click="updateQuery({ resourceId: undefined, resourceType: undefined })">{{ t('activity.clearResource') }}</Button>
              </div>
            </div>
          </template>

          <EmptyState v-if="error && !operations.length" variant="page" :title="t('common.loadFailed')" :description="error">
            <template #actions><Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button></template>
          </EmptyState>
          <Table
            v-else
            class="h-full max-lg:max-h-[60dvh]"
            :columns="columns"
            :rows="operations"
            row-key="operationId"
            :active-key="selectedId"
            :loading="loading && !operations.length"
            :loading-label="t('common.loading')"
          >
            <template #updatedAt="{ row }">
              <span class="text-muted-foreground">{{ formatDateTime(row.updatedAt) }}</span>
            </template>
            <template #title="{ row }">
              <strong class="block max-w-[24rem] truncate text-foreground">{{ translateEventSummary(t, row.title || row.action) }}</strong>
              <span v-if="row.failureSummary" class="mt-1 block max-w-[24rem] truncate text-xs text-danger">{{ row.failureSummary }}</span>
            </template>
            <template #resources="{ row }">
              <span class="text-muted-foreground">{{ resourceLabel(row) }}</span>
            </template>
            <template #result="{ row }">
              <Badge :tone="operationTone(row.result)">{{ stateLabel(row.result || row.phase) }}</Badge>
            </template>
            <template #actions="{ row }">
              <div class="flex items-center justify-end gap-1">
                <Button size="sm" variant="secondary" @click="openDetail(row.operationId)"><Eye />{{ t('common.details') }}</Button>
                <Button size="sm" variant="ghost" @click="openLogs(row.operationId)">{{ t('intentRecords.viewLogs') }}</Button>
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
        :title="detail ? translateEventSummary(t, detail.operation.title || detail.operation.action) : t('common.loading')"
        :back-label="t('common.backToList')"
        @back="closeDetail"
      >
        <template #actions>
          <Button
            v-for="command in detail?.availableCommands.filter(item => ['retry', 'run-now', 'resolve'].includes(item.kind)) || []"
            :key="`${command.executionId}:${command.kind}`"
            size="sm"
            :disabled="commandLoading"
            @click="requestCommand(command)"
          >
            {{ t(command.kind === 'resolve' ? 'activity.resolve' : command.kind === 'retry' ? 'common.retry' : 'common.runNow') }}
          </Button>
          <Button v-if="detail" size="sm" variant="primary" @click="openLogs(detail.operation.operationId)">{{ t('intentRecords.viewLogs') }}</Button>
        </template>

        <EmptyState v-if="detailError" :title="t('common.loadFailed')" :description="detailError">
          <template #actions><Button size="sm" :loading="detailLoading" @click="loadDetail"><RefreshCcw />{{ t('common.retry') }}</Button></template>
        </EmptyState>
        <div v-else-if="detail" class="grid min-w-0 gap-4">
          <div class="flex flex-wrap gap-2">
            <Badge :tone="operationTone(detail.operation.result)">{{ stateLabel(detail.operation.result || detail.operation.phase) }}</Badge>
            <Badge v-if="detail.operation.hadError" tone="danger">{{ t('activity.hadError') }}</Badge>
            <Badge v-if="detail.operation.uncertainty" tone="warning">{{ t('activity.uncertain') }}</Badge>
          </div>
          <p v-if="detail.operation.failureSummary" class="m-0 break-words rounded-xl border border-danger-border bg-danger-bg p-3 text-sm text-danger">{{ detail.operation.failureSummary }}</p>
          <p v-else class="m-0 text-sm text-muted-foreground">{{ t('intentRecords.noFailureSummary') }}</p>
          <dl class="grid gap-2 text-sm sm:grid-cols-2">
            <div><dt class="text-muted-foreground">{{ t('intentRecords.resources') }}</dt><dd>{{ resourceLabel(detail.operation) }}</dd></div>
            <div><dt class="text-muted-foreground">{{ t('intentRecords.actor') }}</dt><dd>{{ detail.operation.actor?.name || detail.operation.actor?.id || t('activity.system') }}</dd></div>
            <div><dt class="text-muted-foreground">{{ t('intentRecords.time') }}</dt><dd>{{ formatDateTime(detail.operation.updatedAt) }}</dd></div>
            <div><dt class="text-muted-foreground">{{ t('intentRecords.attempts') }}</dt><dd>{{ detail.operation.attemptCount }}</dd></div>
          </dl>
          <p class="m-0 text-sm text-muted-foreground">{{ t('intentRecords.diagnoseHint') }}</p>
        </div>
      </ConsolePage>
    </template>
  </TableDetailLayout>

  <Dialog
    :open="pendingCommand?.kind === 'resolve'"
    :title="t('activity.resolveTitle')"
    :description="t('activity.resolveDescription')"
    @update:open="value => { if (!value && !commandLoading) pendingCommand = null; }"
  >
    <div class="grid gap-4">
      <p class="m-0 rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning">{{ t('activity.resolveImpact') }}</p>
      <label class="grid gap-2 text-sm">
        <span>{{ t('activity.resolveOutcome') }}</span>
        <Select
          v-model="resolveOutcome"
          :options="[{ value: '', label: t('activity.resolveChoose') }, { value: 'succeeded', label: t('activity.resolveSucceeded') }, { value: 'failed', label: t('activity.resolveFailed') }]"
          :disabled="commandLoading"
        />
      </label>
      <label class="grid gap-2 text-sm">
        <span>{{ t('activity.resolveReason') }}</span>
        <Textarea v-model="resolveReason" :placeholder="t('activity.resolveReasonHint')" :disabled="commandLoading" required />
      </label>
    </div>
    <template #footer>
      <Button :disabled="commandLoading" @click="pendingCommand = null">{{ t('common.cancel') }}</Button>
      <Button variant="primary" :loading="commandLoading" :disabled="!resolveOutcome || !resolveReason.trim()" @click="executeCommand">{{ t('activity.resolveConfirm') }}</Button>
    </template>
  </Dialog>
  <ConfirmDialog
    :open="Boolean(pendingCommand && pendingCommand.kind !== 'resolve')"
    :title="t('activity.commandTitle')"
    :impact="t('activity.commandImpact')"
    :checkbox-label="t('common.confirm')"
    :confirm-label="t('common.confirm')"
    :cancel-label="t('common.cancel')"
    :loading="commandLoading"
    @update:open="value => { if (!value) pendingCommand = null; }"
    @confirm="executeCommand"
  />
</template>
