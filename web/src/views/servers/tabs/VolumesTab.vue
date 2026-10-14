<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import { RefreshCcw, Trash2 } from '@lucide/vue';
import { containersApi } from '@/api/containers';
import { waitForTask } from '@/api/taskWait';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import Table from '@/components/ui/Table.vue';
import Tooltip from '@/components/ui/Tooltip.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { VolumeDto } from '@/types/resources';
import type { ServerDto } from '@/types/servers';
import { formatDateTime } from '@/utils/datetime';
import { createLatestRequestGuard, isAbortError } from '@/views/_shared/requestState';
import { volumeTone } from './resourceModel';

const props = defineProps<{ server: ServerDto }>();
const { t } = useI18n();
const notifyError = useErrorToast();
const notifySuccess = useSuccessToast();

const volumes = ref<VolumeDto[]>([]);
const observedAt = ref('');
const loading = ref(false);
const error = ref('');
const pending = ref('');
const confirmTarget = ref<{ kind: 'volume'; name: string } | { kind: 'prune' } | null>(null);
const blocked = computed(() => !canUseDocker.value);
const canUseDocker = computed(() => props.server.traits?.['agent.status'] === 'compatible' && Boolean(props.server.traits?.['agent.url']));

const listGuard = createLatestRequestGuard();
let listController: AbortController | null = null;
let taskWaitController: AbortController | null = null;
const autoSnapshotKeys = new Set<string>();

const columns = computed(() => [
  { key: 'name', label: t('common.name') },
  { key: 'state', label: t('common.status'), nowrap: true },
  { key: 'containers', label: t('resourcesPage.attachedContainers'), nowrap: true },
  { key: 'size', label: t('resourcesPage.size'), nowrap: true },
  { key: 'actions', label: t('common.actions'), align: 'right' as const, nowrap: true },
]);

async function load() {
  listController?.abort();
  const requestId = listGuard.begin();
  const controller = new AbortController();
  listController = controller;
  loading.value = true;
  error.value = '';
  let needsSnapshot = false;
  try {
    const result = await containersApi.volumes(props.server.id, { signal: controller.signal });
    if (!listGuard.isCurrent(requestId)) return;
    volumes.value = result.items;
    observedAt.value = result.observedAt ?? '';
    needsSnapshot = Boolean(result.stale && !result.observedAt);
  } catch (err) {
    if (isAbortError(err)) return;
    error.value = err instanceof Error ? err.message : t('resourcesPage.loadResourceFailed');
    notifyError(err instanceof Error ? err.message : t('resourcesPage.loadResourceFailed'), err);
  } finally {
    if (listGuard.isCurrent(requestId)) loading.value = false;
  }
  if (needsSnapshot) await ensureSnapshot();
}

/** 首次打开且本地无快照时自动提交一次刷新任务，避免把“尚未刷新”误报成空列表。 */
async function ensureSnapshot() {
  const key = `${props.server.id}:volumes`;
  if (autoSnapshotKeys.has(key)) return;
  autoSnapshotKeys.add(key);
  try {
    const accepted = await containersApi.refreshVolumes(props.server.id);
    if (!taskWaitController || taskWaitController.signal.aborted) taskWaitController = new AbortController();
    await waitForTask(accepted.taskId, 90_000, taskWaitController.signal);
    await load();
  } catch (err) {
    if (!isAbortError(err)) {
      const message = err instanceof Error ? err.message : t('resourcesPage.loadResourceFailed');
      notifyError(message, err);
    }
  } finally {
    autoSnapshotKeys.delete(key);
  }
}

async function refresh() {
  if (blocked.value) return;
  pending.value = 'refresh';
  try {
    const accepted = await containersApi.refreshVolumes(props.server.id);
    notifySuccess(t('resourcesPage.taskAccepted', { taskId: accepted.taskId }), accepted);
    if (!taskWaitController || taskWaitController.signal.aborted) taskWaitController = new AbortController();
    await waitForTask(accepted.taskId, 90_000, taskWaitController.signal);
    await load();
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    pending.value = '';
  }
}

function askDelete(volume: VolumeDto) {
  if (blocked.value || volume.inUse) return;
  confirmTarget.value = { kind: 'volume', name: volume.name };
}

async function confirmAction() {
  const target = confirmTarget.value;
  if (!target) return;
  pending.value = `confirm-${target.kind}`;
  try {
    const result = target.kind === 'volume'
      ? await containersApi.deleteVolume(props.server.id, target.name)
      : await containersApi.deleteUnusedVolumes(props.server.id);
    notifySuccess(t('resourcesPage.operationCompleted'), result);
    confirmTarget.value = null;
    await load();
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    pending.value = '';
  }
}

function formatBytes(value?: number) {
  if (!value) return '0 B';
  const units = ['B', 'KiB', 'MiB', 'GiB'];
  let amount = value;
  let index = 0;
  while (amount >= 1024 && index < units.length - 1) {
    amount /= 1024;
    index += 1;
  }
  return `${amount.toFixed(index ? 1 : 0)} ${units[index]}`;
}

onMounted(load);
onBeforeUnmount(() => {
  listController?.abort();
  taskWaitController?.abort();
});
</script>

<template>
  <div class="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-4">
    <div class="grid gap-3">
      <div v-if="blocked" class="rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning">{{ t('resourcesPage.blockAgent') }}</div>
      <div class="flex flex-wrap items-center justify-between gap-2">
        <p class="m-0 text-sm text-muted-foreground">{{ t('resourcesPage.volumeSafety') }}</p>
        <div class="flex flex-wrap items-center gap-2">
          <span class="text-xs text-muted-foreground">{{ formatDateTime(observedAt) || t('common.never') }}</span>
          <Button size="sm" variant="danger" :disabled="blocked" @click="confirmTarget = { kind: 'prune' }"><Trash2 />{{ t('resourcesPage.pruneUnusedVolumes') }}</Button>
          <Button size="sm" :loading="loading || pending === 'refresh'" :disabled="blocked" @click="refresh"><RefreshCcw />{{ t('common.refresh') }}</Button>
        </div>
      </div>
    </div>

    <EmptyState v-if="error && !volumes.length" class="self-start" :title="t('common.loadFailed')" :description="error">
      <template #actions>
        <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button>
      </template>
    </EmptyState>

    <Table
      v-else
      :columns="columns"
      :rows="volumes"
      row-key="name"
      :loading="loading || pending === 'refresh'"
      :loading-label="t('common.loading')"
    >
      <template #name="{ row }">
        <div class="min-w-0">
          <strong class="block max-w-[20rem] truncate text-foreground">{{ row.name }}</strong>
          <Tooltip :text="row.mountpoint" trigger-class="max-w-[20rem]">
            <span class="panel-mono block truncate text-xs text-muted-foreground">{{ row.mountpoint }}</span>
          </Tooltip>
        </div>
      </template>
      <template #state="{ row }">
        <Badge :tone="volumeTone(row)">{{ row.inUse ? t('resourcesPage.inUse') : t('resourcesPage.unused') }}</Badge>
      </template>
      <template #containers="{ row }">
        <span class="text-muted-foreground">{{ row.containerCount }}</span>
      </template>
      <template #size="{ row }">
        <span class="text-muted-foreground">{{ formatBytes(row.usageData?.size) }}</span>
      </template>
      <template #actions="{ row }">
        <Button size="sm" variant="danger" :disabled="blocked || row.inUse" @click="askDelete(row)"><Trash2 />{{ t('common.delete') }}</Button>
      </template>
      <template #empty>
        <EmptyState :title="t('resourcesPage.noVolumes')" :description="t('resourcesPage.noVolumesHint')" />
      </template>
    </Table>

    <ConfirmDialog
      :open="Boolean(confirmTarget)"
      :title="confirmTarget?.kind === 'prune' ? t('resourcesPage.pruneUnusedVolumes') : t('common.delete')"
      :description="confirmTarget?.kind === 'volume' ? confirmTarget.name : ''"
      :impact="t('resourcesPage.confirmDangerImpact')"
      tone="danger"
      :loading="Boolean(pending)"
      :confirm-label="confirmTarget?.kind === 'prune' ? t('resourcesPage.pruneUnusedVolumes') : t('common.delete')"
      :cancel-label="t('common.cancel')"
      require-checkbox
      :checkbox-label="t('serversPage.confirmCheckbox')"
      @update:open="(value) => { if (!value) confirmTarget = null; }"
      @confirm="confirmAction"
      @cancel="confirmTarget = null"
    />
  </div>
</template>
