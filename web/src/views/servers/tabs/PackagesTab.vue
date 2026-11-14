<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import { Package, RefreshCcw } from '@lucide/vue';
import { packagesApi } from '@/api/packages';
import { waitForTask } from '@/api/taskWait';
import Button from '@/components/ui/Button.vue';
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import SearchInput from '@/components/ui/SearchInput.vue';
import Table from '@/components/ui/Table.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { PackageUpdateList } from '@/types/resources';
import type { ServerDto } from '@/types/servers';
import { formatDateTime } from '@/utils/datetime';
import { createLatestRequestGuard, isAbortError } from '@/views/_shared/requestState';
import { canMaintainPackages, filterPackages, packageBlockReason, selectedPackageNames } from './resourceModel';

const props = defineProps<{ server: ServerDto }>();
const { t } = useI18n();
const notifyError = useErrorToast();
const notifySuccess = useSuccessToast();

const packageList = ref<PackageUpdateList | null>(null);
const selection = reactive<Record<string, boolean>>({});
const search = ref('');
const loading = ref(false);
const error = ref('');
const pending = ref('');
const confirmUpgradeAll = ref(false);
const canMaintain = computed(() => canMaintainPackages(props.server));
const blockReason = computed(() => packageBlockReason(props.server));
const rows = computed(() => filterPackages(packageList.value?.updates ?? [], search.value));
const selectedNames = computed(() => selectedPackageNames(selection));
const allVisibleSelected = computed(() => rows.value.length > 0 && rows.value.every((item) => selection[item.name]));

const listGuard = createLatestRequestGuard();
let listController: AbortController | null = null;
let taskWaitController = new AbortController();

const columns = computed(() => [
  { key: 'select', label: '', width: 'w-10' },
  { key: 'name', label: t('common.name') },
  { key: 'installed', label: t('resourcesPage.installedVersion'), nowrap: true },
  { key: 'candidate', label: t('resourcesPage.candidateVersion'), nowrap: true },
  { key: 'source', label: t('resourcesPage.source'), nowrap: true },
]);

function clearSelection() {
  Object.keys(selection).forEach((key) => delete selection[key]);
}

function toggleAll(checked: boolean) {
  rows.value.forEach((item) => { selection[item.name] = checked; });
}

async function load() {
  listController?.abort();
  const requestId = listGuard.begin();
  const controller = new AbortController();
  listController = controller;
  loading.value = true;
  error.value = '';
  try {
    const updates = await packagesApi.updates(props.server.id, { signal: controller.signal });
    if (!listGuard.isCurrent(requestId)) return;
    packageList.value = updates;
    // 勾选只对当前快照有效：重载后清空选择。
    clearSelection();
  } catch (err) {
    if (isAbortError(err)) return;
    error.value = err instanceof Error ? err.message : t('resourcesPage.loadResourceFailed');
    notifyError(err instanceof Error ? err.message : t('resourcesPage.loadResourceFailed'), err);
  } finally {
    if (listGuard.isCurrent(requestId)) loading.value = false;
  }
}

async function refreshMetadata() {
  if (!canMaintain.value) return;
  pending.value = 'refresh';
  try {
    const result = await packagesApi.refresh(props.server.id);
    notifySuccess(result.taskId ? t('resourcesPage.taskAccepted', { taskId: result.taskId }) : t('resourcesPage.refreshing'), result);
    if (result.taskId) await waitForTask(result.taskId, 90_000, taskWaitController.signal);
    await load();
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    pending.value = '';
  }
}

async function upgradeSelected() {
  if (!canMaintain.value || !selectedNames.value.length) return;
  pending.value = 'upgrade-selected';
  try {
    const accepted = await packagesApi.upgradeSelected(props.server.id, selectedNames.value);
    notifySuccess(t('resourcesPage.taskAccepted', { taskId: accepted.taskId }), accepted);
    await load();
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    pending.value = '';
  }
}

async function upgradeAll() {
  if (!canMaintain.value) return;
  pending.value = 'upgrade-all';
  try {
    const accepted = await packagesApi.upgradeAll(props.server.id);
    notifySuccess(t('resourcesPage.taskAccepted', { taskId: accepted.taskId }), accepted);
    confirmUpgradeAll.value = false;
    await load();
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    pending.value = '';
  }
}

onMounted(load);
onBeforeUnmount(() => {
  listController?.abort();
  taskWaitController.abort();
});
</script>

<template>
  <div class="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)_auto] gap-4">
    <div class="grid gap-3">
      <div v-if="blockReason" class="rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning">{{ t(blockReason) }}</div>
      <div class="flex flex-wrap items-center justify-between gap-2">
        <SearchInput v-model="search" class="w-full max-w-sm" clearable :label="t('common.search')" :placeholder="t('resourcesPage.searchPackages')" :clear-label="t('common.clearSearch')" />
        <div class="flex flex-wrap gap-2">
          <Button size="sm" :disabled="!canMaintain" :loading="pending === 'refresh' || loading" @click="refreshMetadata"><RefreshCcw />{{ t('resourcesPage.refreshMetadata') }}</Button>
          <Button v-if="selectedNames.length" size="sm" variant="primary" :disabled="!canMaintain" :loading="pending === 'upgrade-selected'" @click="upgradeSelected"><Package />{{ t('resourcesPage.upgradeSelected') }}</Button>
          <Button size="sm" variant="danger" :disabled="!canMaintain || !packageList?.updates.length" :loading="pending === 'upgrade-all'" @click="confirmUpgradeAll = true"><Package />{{ t('resourcesPage.upgradeAll') }}</Button>
        </div>
      </div>
    </div>

    <EmptyState v-if="error && !packageList" class="self-start" :title="t('common.loadFailed')" :description="error">
      <template #actions>
        <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button>
      </template>
    </EmptyState>

    <Table
      v-else
      :columns="columns"
      :rows="rows"
      row-key="name"
      :loading="loading"
      :loading-label="t('common.loading')"
    >
      <template #head-select>
        <input
          type="checkbox"
          :checked="allVisibleSelected"
          :disabled="!canMaintain || !rows.length"
          :aria-label="t('common.actions')"
          @change="toggleAll(($event.target as HTMLInputElement).checked)"
        />
      </template>
      <template #select="{ row }">
        <input v-model="selection[row.name]" type="checkbox" :disabled="!canMaintain" :aria-label="row.name" />
      </template>
      <template #name="{ row }">
        <strong class="block max-w-[20rem] truncate text-foreground">{{ row.name }}</strong>
      </template>
      <template #installed="{ row }">
        <span class="text-muted-foreground">{{ row.installedVersion }}</span>
      </template>
      <template #candidate="{ row }">
        <strong class="text-foreground">{{ row.candidateVersion }}</strong>
      </template>
      <template #source="{ row }">
        <span class="text-muted-foreground">{{ row.source }}</span>
      </template>
      <template #empty>
        <EmptyState :title="t('resourcesPage.noPackages')" :description="t('resourcesPage.noPackagesHint')" />
      </template>
    </Table>

    <footer class="flex items-center justify-between border-t border-border pt-3 text-sm text-muted-foreground">
      <span>{{ t('resourcesPage.lastRefresh') }}: {{ formatDateTime(packageList?.lastRefreshedAt) || t('common.never') }}</span>
      <span v-if="selectedNames.length">{{ selectedNames.length }} {{ t('resourcesPage.selectedCount') }}</span>
    </footer>

    <ConfirmDialog
      :open="confirmUpgradeAll"
      :title="t('resourcesPage.upgradeAll')"
      :impact="t('resourcesPage.confirmUpgradeAllImpact')"
      tone="danger"
      :loading="pending === 'upgrade-all'"
      :confirm-label="t('resourcesPage.upgradeAll')"
      :cancel-label="t('common.cancel')"
      :checkbox-label="t('serversPage.confirmCheckbox')"
      @update:open="(value) => { confirmUpgradeAll = value; }"
      @confirm="upgradeAll"
      @cancel="confirmUpgradeAll = false"
    />
  </div>
</template>
