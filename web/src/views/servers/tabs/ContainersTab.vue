<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import { FileText, ListTree, MoreHorizontal, Play, RefreshCcw, Square, Trash2 } from '@lucide/vue';
import { containersApi } from '@/api/containers';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue';
import Dialog from '@/components/ui/Dialog.vue';
import Dropdown from '@/components/ui/Dropdown.vue';
import DropdownItem from '@/components/ui/DropdownItem.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import IconButton from '@/components/ui/IconButton.vue';
import Table from '@/components/ui/Table.vue';
import Tooltip from '@/components/ui/Tooltip.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { ContainerDto } from '@/types/resources';
import type { ServerDto } from '@/types/servers';
import { createLatestRequestGuard, isAbortError } from '@/views/_shared/requestState';
import { containerActionDisabled, containerTone } from './resourceModel';

const props = defineProps<{ server: ServerDto }>();
const { t } = useI18n();
const notifyError = useErrorToast();
const notifySuccess = useSuccessToast();

const containers = ref<ContainerDto[]>([]);
const loading = ref(false);
const error = ref('');
const pending = ref('');
const confirmTarget = ref<{ container: ContainerDto; action: 'stop' | 'restart' | 'delete' } | null>(null);
const logsDialog = ref(false);
const logsText = ref('');
const logsTitle = ref('');
const canUseDocker = computed(() => props.server.traits?.['agent.status'] === 'compatible' && Boolean(props.server.traits?.['agent.url']));
const blocked = computed(() => !canUseDocker.value);

const listGuard = createLatestRequestGuard();
let listController: AbortController | null = null;

const columns = computed(() => [
  { key: 'name', label: t('common.name') },
  { key: 'image', label: t('resourcesPage.image') },
  { key: 'state', label: t('common.status'), nowrap: true },
  { key: 'ports', label: t('resourcesPage.ports') },
  { key: 'actions', label: t('common.actions'), align: 'right' as const, nowrap: true },
]);

function containerName(container: ContainerDto) {
  return (container.names[0] || container.id).replace(/^\//, '');
}

function portsText(container: ContainerDto) {
  return container.ports.map((port) => `${port.publicPort || port.privatePort}:${port.privatePort}/${port.type}`).join(', ') || t('resourcesPage.noPortsMapped');
}

async function load() {
  listController?.abort();
  const requestId = listGuard.begin();
  const controller = new AbortController();
  listController = controller;
  loading.value = true;
  error.value = '';
  try {
    const result = await containersApi.containers(props.server.id, { signal: controller.signal });
    if (!listGuard.isCurrent(requestId)) return;
    containers.value = result.items;
  } catch (err) {
    if (isAbortError(err)) return;
    error.value = err instanceof Error ? err.message : t('resourcesPage.loadResourceFailed');
    notifyError(err instanceof Error ? err.message : t('resourcesPage.loadResourceFailed'), err);
  } finally {
    if (listGuard.isCurrent(requestId)) loading.value = false;
  }
}

async function run(operation: string, action: () => Promise<void>) {
  pending.value = operation;
  try {
    await action();
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    pending.value = '';
  }
}

async function start(container: ContainerDto) {
  if (blocked.value || containerActionDisabled(container, 'start')) return;
  await run(`start-${container.id}`, async () => {
    const result = await containersApi.containerAction(props.server.id, container.id, 'start');
    notifySuccess(t('resourcesPage.operationCompleted'), result);
    await load();
  });
}

function ask(container: ContainerDto, action: 'stop' | 'restart' | 'delete') {
  if (blocked.value || containerActionDisabled(container, action)) return;
  confirmTarget.value = { container, action };
}

async function confirmAction() {
  const target = confirmTarget.value;
  if (!target) return;
  await run(`confirm-${target.container.id}`, async () => {
    const result = target.action === 'delete'
      ? await containersApi.deleteContainer(props.server.id, target.container.id)
      : await containersApi.containerAction(props.server.id, target.container.id, target.action);
    notifySuccess(t('resourcesPage.operationCompleted'), result);
    confirmTarget.value = null;
    await load();
  });
}

async function openLogs(container: ContainerDto) {
  await run(`logs-${container.id}`, async () => {
    const result = await containersApi.containerLogs(props.server.id, container.id, 500);
    logsTitle.value = containerName(container);
    logsText.value = result.logs;
    logsDialog.value = true;
  });
}

const confirmActionLabel = computed(() => {
  const action = confirmTarget.value?.action;
  if (action === 'stop') return t('resourcesPage.stop');
  if (action === 'restart') return t('resourcesPage.restart');
  return t('common.delete');
});

const confirmImpact = computed(() => {
  const target = confirmTarget.value;
  if (!target) return '';
  const base = target.action === 'delete' ? t('resourcesPage.confirmContainerDeleteImpact') : t('resourcesPage.confirmContainerActionImpact');
  return target.container.managed ? `${base} ${t('resourcesPage.managedContainerConfirmImpact')}` : base;
});

onMounted(load);
onBeforeUnmount(() => listController?.abort());
</script>

<template>
  <div class="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-4">
    <div class="grid gap-3">
      <div v-if="blocked" class="rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning">{{ t('resourcesPage.blockAgent') }}</div>
      <div class="flex flex-wrap items-center justify-between gap-2">
        <p class="m-0 flex items-center gap-2 text-sm text-muted-foreground"><ListTree class="size-4" />{{ t('serversPage.tab.containers') }}</p>
        <Button size="sm" :loading="loading" :disabled="blocked" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
      </div>
    </div>

    <EmptyState v-if="error && !containers.length" class="self-start" :title="t('common.loadFailed')" :description="error">
      <template #actions>
        <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button>
      </template>
    </EmptyState>

    <Table
      v-else
      :columns="columns"
      :rows="containers"
      row-key="id"
      :loading="loading"
      :loading-label="t('common.loading')"
    >
      <template #name="{ row }">
        <strong class="block max-w-[16rem] truncate text-foreground">{{ containerName(row) }}</strong>
      </template>
      <template #image="{ row }">
        <Tooltip :text="row.image" trigger-class="max-w-[18rem]">
          <span class="truncate">{{ row.image }}</span>
        </Tooltip>
      </template>
      <template #state="{ row }">
        <Badge :tone="containerTone(row)">{{ row.managed ? t('resourcesPage.managed') : row.state }}</Badge>
      </template>
      <template #ports="{ row }">
        <span class="text-muted-foreground">{{ portsText(row) }}</span>
      </template>
      <template #actions="{ row }">
        <div class="flex items-center justify-end gap-1">
          <Button size="sm" variant="ghost" :disabled="blocked" :loading="pending === `logs-${row.id}`" @click="openLogs(row)"><FileText />{{ t('resourcesPage.logs') }}</Button>
          <Dropdown align="right">
            <template #trigger>
              <IconButton size="sm" :label="t('common.moreActions')"><MoreHorizontal /></IconButton>
            </template>
            <DropdownItem :disabled="blocked || Boolean(containerActionDisabled(row, 'start'))" @click="start(row)"><Play />{{ t('resourcesPage.start') }}</DropdownItem>
            <DropdownItem :disabled="blocked || Boolean(containerActionDisabled(row, 'stop'))" @click="ask(row, 'stop')"><Square />{{ t('resourcesPage.stop') }}</DropdownItem>
            <DropdownItem :disabled="blocked || Boolean(containerActionDisabled(row, 'restart'))" @click="ask(row, 'restart')"><RefreshCcw />{{ t('resourcesPage.restart') }}</DropdownItem>
            <DropdownItem tone="danger" :disabled="blocked || Boolean(containerActionDisabled(row, 'delete'))" @click="ask(row, 'delete')"><Trash2 />{{ t('common.delete') }}</DropdownItem>
          </Dropdown>
        </div>
      </template>
      <template #empty>
        <EmptyState :title="t('resourcesPage.noContainers')" :description="t('resourcesPage.noContainersHint')" />
      </template>
    </Table>

    <Dialog v-model:open="logsDialog" :title="logsTitle" :description="t('resourcesPage.logsDescription')" :close-label="t('common.close')">
      <pre class="max-h-[520px] overflow-auto rounded-xl border border-border bg-muted p-3 text-xs leading-5 text-foreground">{{ logsText }}</pre>
      <template #footer>
        <Button @click="logsDialog = false">{{ t('common.close') }}</Button>
      </template>
    </Dialog>

    <ConfirmDialog
      :open="Boolean(confirmTarget)"
      :title="confirmActionLabel"
      :description="confirmTarget ? containerName(confirmTarget.container) : ''"
      :impact="confirmImpact"
      tone="danger"
      :loading="Boolean(pending)"
      :confirm-label="confirmActionLabel"
      :cancel-label="t('common.cancel')"
      :require-checkbox="confirmTarget?.action === 'delete'"
      :checkbox-label="t('serversPage.confirmCheckbox')"
      @update:open="(value) => { if (!value) confirmTarget = null; }"
      @confirm="confirmAction"
      @cancel="confirmTarget = null"
    />
  </div>
</template>
