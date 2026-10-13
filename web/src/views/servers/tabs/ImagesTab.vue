<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import { Boxes, DownloadCloud, RefreshCcw, Trash2 } from '@lucide/vue';
import { containersApi } from '@/api/containers';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue';
import Dialog from '@/components/ui/Dialog.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import Input from '@/components/ui/Input.vue';
import Table from '@/components/ui/Table.vue';
import Tooltip from '@/components/ui/Tooltip.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { ImageDto, ImageList } from '@/types/resources';
import type { ServerDto } from '@/types/servers';
import { formatDateTime } from '@/utils/datetime';
import { createLatestRequestGuard, isAbortError } from '@/views/_shared/requestState';
import { imageLabel, imageTone, imageUpgradeApplicationIds } from './resourceModel';

const props = defineProps<{ server: ServerDto }>();
const { t } = useI18n();
const notifyError = useErrorToast();
const notifySuccess = useSuccessToast();

const images = ref<ImageList | null>(null);
const loading = ref(false);
const error = ref('');
const pending = ref('');
const confirmTarget = ref<{ kind: 'image'; id: string; label: string } | { kind: 'prune' } | null>(null);
const pullDialog = ref(false);
const pullForm = ref('nginx:1.28-alpine');
const blocked = computed(() => !canUseDocker.value);
const canUseDocker = computed(() => props.server.traits?.['agent.status'] === 'compatible' && Boolean(props.server.traits?.['agent.url']));
const upgradeIds = computed(() => imageUpgradeApplicationIds(images.value?.items ?? []));

const listGuard = createLatestRequestGuard();
let listController: AbortController | null = null;

const columns = computed(() => [
  { key: 'image', label: t('resourcesPage.image') },
  { key: 'size', label: t('resourcesPage.size'), nowrap: true },
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
  try {
    const result = await containersApi.images(props.server.id, { signal: controller.signal });
    if (!listGuard.isCurrent(requestId)) return;
    images.value = result;
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

async function pull() {
  if (blocked.value || !pullForm.value.trim()) return;
  await run('pull', async () => {
    const result = await containersApi.pullImage(props.server.id, pullForm.value.trim());
    notifySuccess(t('resourcesPage.imagePulled'), result);
    pullDialog.value = false;
    await load();
  });
}

async function upgrade(selected: boolean) {
  if (blocked.value) return;
  await run(selected ? 'upgrade-selected' : 'upgrade-all', async () => {
    const accepted = selected ? await containersApi.upgradeSelectedImages(upgradeIds.value) : await containersApi.upgradeAllImages();
    notifySuccess(t('resourcesPage.taskAccepted', { taskId: accepted.taskId }), accepted);
  });
}

function askDelete(image: ImageDto) {
  if (blocked.value || image.inUse) return;
  confirmTarget.value = { kind: 'image', id: image.id, label: imageLabel(image) };
}

function askPrune() {
  if (blocked.value) return;
  confirmTarget.value = { kind: 'prune' };
}

async function confirmAction() {
  const target = confirmTarget.value;
  if (!target) return;
  await run(`confirm-${target.kind}`, async () => {
    const result = target.kind === 'image' ? await containersApi.deleteImage(props.server.id, target.id) : await containersApi.deleteUnusedImages(props.server.id);
    notifySuccess(t('resourcesPage.operationCompleted'), result);
    confirmTarget.value = null;
    await load();
  });
}

const confirmTitle = computed(() => confirmTarget.value?.kind === 'prune' ? t('resourcesPage.pruneUnused') : t('common.delete'));
const confirmDescription = computed(() => confirmTarget.value?.kind === 'image' ? confirmTarget.value.label : '');

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
onBeforeUnmount(() => listController?.abort());
</script>

<template>
  <div class="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-4">
    <div class="grid gap-3">
      <div v-if="blocked" class="rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning">{{ t('resourcesPage.blockAgent') }}</div>
      <div class="flex flex-wrap items-center justify-between gap-2">
        <p class="m-0 flex items-center gap-2 text-sm text-muted-foreground"><Boxes class="size-4" />{{ formatDateTime(images?.observedAt) || t('common.never') }}</p>
        <div class="flex flex-wrap gap-2">
          <Button size="sm" variant="primary" :disabled="blocked" @click="pullDialog = true"><DownloadCloud />{{ t('resourcesPage.pullImage') }}</Button>
          <Button size="sm" :disabled="blocked || !upgradeIds.length" :loading="pending === 'upgrade-selected'" @click="upgrade(true)">{{ t('resourcesPage.upgradeApplications') }}</Button>
          <Button size="sm" :disabled="blocked" :loading="pending === 'upgrade-all'" @click="upgrade(false)">{{ t('resourcesPage.upgradeAllApplications') }}</Button>
          <Button size="sm" variant="danger" :disabled="blocked" @click="askPrune"><Trash2 />{{ t('resourcesPage.pruneUnused') }}</Button>
          <Button size="sm" :loading="loading" :disabled="blocked" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
        </div>
      </div>
    </div>

    <EmptyState v-if="error && !images?.items.length" class="self-start" :title="t('common.loadFailed')" :description="error">
      <template #actions>
        <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button>
      </template>
    </EmptyState>

    <Table
      v-else
      :columns="columns"
      :rows="images?.items ?? []"
      row-key="id"
      :loading="loading"
      :loading-label="t('common.loading')"
    >
      <template #image="{ row }">
        <div class="min-w-0">
          <Tooltip :text="imageLabel(row)" trigger-class="max-w-[22rem]">
            <strong class="truncate text-foreground">{{ imageLabel(row) }}</strong>
          </Tooltip>
          <span class="panel-mono block truncate text-xs text-muted-foreground">{{ row.id }}</span>
        </div>
      </template>
      <template #size="{ row }">
        <span class="text-muted-foreground">{{ formatBytes(row.size) }}</span>
      </template>
      <template #state="{ row }">
        <div class="grid justify-start gap-1">
          <Badge :tone="imageTone(row)">{{ row.updateAvailable ? t('resourcesPage.updateAvailable') : row.inUse ? t('resourcesPage.inUse') : t('resourcesPage.unused') }}</Badge>
          <span v-if="row.lastError" class="max-w-[14rem] truncate text-xs text-warning">{{ row.lastError }}</span>
        </div>
      </template>
      <template #actions="{ row }">
        <Button size="sm" variant="danger" :disabled="blocked || row.inUse" @click="askDelete(row)"><Trash2 />{{ t('common.delete') }}</Button>
      </template>
      <template #empty>
        <EmptyState :title="t('resourcesPage.noImages')" :description="t('resourcesPage.noImagesHint')" />
      </template>
    </Table>

    <Dialog v-model:open="pullDialog" :title="t('resourcesPage.pullImage')" :description="t('resourcesPage.pullImageDescription')" :close-label="t('common.close')">
      <label class="grid gap-1 text-sm">{{ t('resourcesPage.image') }}<Input v-model="pullForm" placeholder="nginx:latest" /></label>
      <template #footer>
        <Button variant="secondary" @click="pullDialog = false">{{ t('common.cancel') }}</Button>
        <Button variant="primary" :disabled="!pullForm.trim()" :loading="pending === 'pull'" @click="pull">{{ t('resourcesPage.pullImage') }}</Button>
      </template>
    </Dialog>

    <ConfirmDialog
      :open="Boolean(confirmTarget)"
      :title="confirmTitle"
      :description="confirmDescription"
      :impact="t('resourcesPage.confirmDangerImpact')"
      tone="danger"
      :loading="Boolean(pending)"
      :confirm-label="confirmTitle"
      :cancel-label="t('common.cancel')"
      require-checkbox
      :checkbox-label="t('serversPage.confirmCheckbox')"
      @update:open="(value) => { if (!value) confirmTarget = null; }"
      @confirm="confirmAction"
      @cancel="confirmTarget = null"
    />
  </div>
</template>
