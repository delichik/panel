<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import { RefreshCcw, Router, Trash2 } from '@lucide/vue';
import { containersApi } from '@/api/containers';
import { waitForTask } from '@/api/taskWait';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import IconButton from '@/components/ui/IconButton.vue';
import Table from '@/components/ui/Table.vue';
import Tooltip from '@/components/ui/Tooltip.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { NetworkDto } from '@/types/resources';
import type { ServerDto } from '@/types/servers';
import { formatDateTime } from '@/utils/datetime';
import { createLatestRequestGuard, isAbortError } from '@/views/_shared/requestState';

const props = defineProps<{ server: ServerDto }>();
const { t } = useI18n();
const notifyError = useErrorToast();
const notifySuccess = useSuccessToast();

const networks = ref<NetworkDto[]>([]);
const observedAt = ref('');
const loading = ref(false);
const error = ref('');
const pending = ref('');
const blocked = computed(() => !canUseDocker.value);
const canUseDocker = computed(() => props.server.traits?.['agent.status'] === 'compatible' && Boolean(props.server.traits?.['agent.url']));

const listGuard = createLatestRequestGuard();
let listController: AbortController | null = null;
let taskWaitController: AbortController | null = null;
const autoSnapshotKeys = new Set<string>();

const columns = computed(() => [
  { key: 'name', label: t('common.name') },
  { key: 'driver', label: t('resourcesPage.driver'), nowrap: true },
  { key: 'scope', label: t('resourcesPage.scope'), nowrap: true },
  { key: 'state', label: t('common.status'), nowrap: true },
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
    const result = await containersApi.networks(props.server.id, { signal: controller.signal });
    if (!listGuard.isCurrent(requestId)) return;
    networks.value = result.items;
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
  const key = `${props.server.id}:networks`;
  if (autoSnapshotKeys.has(key)) return;
  autoSnapshotKeys.add(key);
  try {
    const accepted = await containersApi.refreshNetworks(props.server.id);
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
    const accepted = await containersApi.refreshNetworks(props.server.id);
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

function networkState(network: NetworkDto): { label: string; tone: 'warning' | 'success' | 'neutral' } {
  if (network.internal) return { label: t('resourcesPage.internal'), tone: 'warning' };
  if (network.labels?.['panel.managed']) return { label: t('resourcesPage.managed'), tone: 'success' };
  return { label: t('resourcesPage.external'), tone: 'neutral' };
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
      <div class="flex flex-wrap items-start justify-between gap-2">
        <p class="m-0 flex items-center gap-2 text-sm text-muted-foreground">
          <Router class="size-4 shrink-0" />
          <span><strong class="text-foreground">{{ t('resourcesPage.networkReadonly') }}</strong> — {{ t('resourcesPage.networkDeleteUnavailable') }}</span>
        </p>
        <div class="flex items-center gap-2">
          <span class="text-xs text-muted-foreground">{{ formatDateTime(observedAt) || t('common.never') }}</span>
          <Button size="sm" :loading="loading || pending === 'refresh'" :disabled="blocked" @click="refresh"><RefreshCcw />{{ t('common.refresh') }}</Button>
        </div>
      </div>
    </div>

    <EmptyState v-if="error && !networks.length" class="self-start" :title="t('common.loadFailed')" :description="error">
      <template #actions>
        <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button>
      </template>
    </EmptyState>

    <Table
      v-else
      :columns="columns"
      :rows="networks"
      row-key="id"
      :loading="loading || pending === 'refresh'"
      :loading-label="t('common.loading')"
    >
      <template #name="{ row }">
        <strong class="block max-w-[20rem] truncate text-foreground">{{ row.name }}</strong>
      </template>
      <template #driver="{ row }">
        <span class="text-muted-foreground">{{ row.driver }}</span>
      </template>
      <template #scope="{ row }">
        <span class="text-muted-foreground">{{ row.scope }}</span>
      </template>
      <template #state="{ row }">
        <Badge :tone="networkState(row).tone">{{ networkState(row).label }}</Badge>
      </template>
      <template #actions>
        <Tooltip :text="t('resourcesPage.networkDeleteUnavailable')">
          <IconButton size="sm" variant="danger" disabled :label="t('common.delete')"><Trash2 /></IconButton>
        </Tooltip>
      </template>
      <template #empty>
        <EmptyState :title="t('resourcesPage.noNetworks')" :description="t('resourcesPage.noNetworksHint')" />
      </template>
    </Table>
  </div>
</template>
