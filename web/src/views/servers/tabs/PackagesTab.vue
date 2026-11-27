<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import { Package, RefreshCcw } from '@/icons';
import { packagesApi } from '@/api/packages';
import { waitForTask } from '@/api/taskWait';
import Button from '@/components/ui/Button.vue';
import Badge from '@/components/ui/Badge.vue';
import Checkbox from '@/components/ui/Checkbox.vue';
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import SearchInput from '@/components/ui/SearchInput.vue';
import Select from '@/components/ui/Select.vue';
import Table from '@/components/ui/Table.vue';
import Tooltip from '@/components/ui/Tooltip.vue';
import { useErrorDialog } from '@/components/ui/errorDialog';
import { useSuccessToast } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { PackageUpdateList } from '@/types/resources';
import type { ServerDto } from '@/types/servers';
import { formatDateTime } from '@/utils/datetime';
import { createLatestRequestGuard, isAbortError } from '@/views/_shared/requestState';
import {
  canMaintainPackages,
  filterPackages,
  packageBlockReason,
  packageUpdateKind,
  selectedPackageNames,
  type PackageUpdateFilter,
} from './resourceModel';

const props = defineProps<{ server: ServerDto }>();
const { t } = useI18n();
const notifyError = useErrorDialog();
const notifySuccess = useSuccessToast();

const packageList = ref<PackageUpdateList | null>(null);
const selection = reactive<Record<string, boolean>>({});
const search = ref('');
const updateFilter = ref<PackageUpdateFilter>('all');
const loading = ref(false);
const error = ref('');
const pending = ref('');
const confirmUpgradeAll = ref(false);
const canMaintain = computed(() => canMaintainPackages(props.server));
const blockReason = computed(() => packageBlockReason(props.server));
const allUpdates = computed(() => packageList.value?.updates ?? []);
const updateCounts = computed(() => {
  let security = 0;
  let regular = 0;
  let unknown = 0;
  for (const item of allUpdates.value) {
    const kind = packageUpdateKind(item);
    if (kind === 'security') security += 1;
    else if (kind === 'unknown') unknown += 1;
    else regular += 1;
  }
  return { total: allUpdates.value.length, security, regular, unknown };
});
const filterOptions = computed(() => {
  const options = [
    { value: 'all', label: t('resourcesPage.filterAllUpdates', { count: updateCounts.value.total }) },
    { value: 'security', label: t('resourcesPage.filterSecurityUpdates', { count: updateCounts.value.security }) },
    { value: 'regular', label: t('resourcesPage.filterRegularUpdates', { count: updateCounts.value.regular }) },
  ];
  if (updateCounts.value.unknown > 0) {
    options.push({ value: 'unknown', label: t('resourcesPage.filterUnknownUpdates', { count: updateCounts.value.unknown }) });
  }
  return options;
});
const rows = computed(() => filterPackages(allUpdates.value, search.value, updateFilter.value));
const selectedNames = computed(() => selectedPackageNames(selection));
const allVisibleSelected = computed(() => rows.value.length > 0 && rows.value.every((item) => selection[item.name]));

const listGuard = createLatestRequestGuard();
let listController: AbortController | null = null;
let taskWaitController = new AbortController();

const columns = computed(() => [
  { key: 'select', label: '', width: 'w-10' },
  { key: 'name', label: t('common.name') },
  { key: 'installed', label: t('resourcesPage.installedVersion'), nowrap: true, width: 'w-40', hideBelow: 'wide' as const },
  { key: 'candidate', label: t('resourcesPage.candidateVersion'), nowrap: true, width: 'w-40', hideBelow: 'sm' as const },
  { key: 'security', label: t('resourcesPage.updateType'), nowrap: true, width: 'w-28', hideBelow: 'sm' as const },
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
  <div class="grid h-full min-h-0 grid-rows-page-footer gap-4">
    <div class="grid gap-3">
      <div v-if="blockReason" class="rounded-control border border-warning-border bg-warning-bg p-3 text-sm text-warning">{{ t(blockReason) }}</div>
      <div class="flex flex-wrap items-center justify-between gap-2">
        <div class="flex flex-1 flex-wrap items-center gap-2">
          <SearchInput v-model="search" class="w-full max-w-sm" clearable :label="t('common.search')" :placeholder="t('resourcesPage.searchPackages')" :clear-label="t('common.clearSearch')" />
          <Select
            v-model="updateFilter"
            class="w-44"
            :options="filterOptions"
            :aria-label="t('resourcesPage.updateType')"
          />
        </div>
        <div class="flex flex-wrap gap-2">
          <Button v-permission="'servers.execute'" size="sm" :disabled="!canMaintain" :loading="pending === 'refresh' || loading" @click="refreshMetadata"><RefreshCcw />{{ t('resourcesPage.refreshMetadata') }}</Button>
          <Button v-permission="'servers.execute'" v-if="selectedNames.length" size="sm" variant="primary" :disabled="!canMaintain" :loading="pending === 'upgrade-selected'" @click="upgradeSelected"><Package />{{ t('resourcesPage.upgradeSelected') }}</Button>
          <Button v-permission="'servers.execute'" size="sm" variant="danger" :disabled="!canMaintain || !packageList?.updates.length" :loading="pending === 'upgrade-all'" @click="confirmUpgradeAll = true"><Package />{{ t('resourcesPage.upgradeAll') }}</Button>
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
      row-height="auto"
      :loading="loading"
      :loading-label="t('common.loading')"
    >
      <template #head-select>
        <Checkbox
          :model-value="allVisibleSelected"
          :disabled="!canMaintain || !rows.length"
          :aria-label="t('common.actions')"
          @update:model-value="toggleAll"
        />
      </template>
      <template #select="{ row }">
        <Checkbox v-model="selection[row.name]" :disabled="!canMaintain" :aria-label="row.name" />
      </template>
      <template #name="{ row }">
        <div class="min-w-0">
          <strong class="block max-w-80 truncate text-foreground">{{ row.name }}</strong>
          <span class="block truncate text-caption text-muted-foreground">{{ row.source }}</span>
        </div>
      </template>
      <template #installed="{ row }">
        <span class="whitespace-nowrap text-muted-foreground">{{ row.installedVersion }}</span>
      </template>
      <template #candidate="{ row }">
        <strong class="whitespace-nowrap text-foreground">{{ row.candidateVersion }}</strong>
      </template>
      <template #security="{ row }">
        <Tooltip v-if="row.securityKnown === false" :text="t('resourcesPage.securityUnknownHint')">
          <Badge tone="neutral">{{ t('resourcesPage.securityUnknown') }}</Badge>
        </Tooltip>
        <Badge v-else-if="row.security" tone="warning">{{ t('resourcesPage.securityUpdate') }}</Badge>
        <Badge v-else tone="info">{{ t('resourcesPage.regularUpdate') }}</Badge>
      </template>
      <template #empty>
        <EmptyState variant="inset" :title="t('resourcesPage.noPackages')" :description="t('resourcesPage.noPackagesHint')" />
      </template>
    </Table>

    <footer class="flex flex-wrap items-center justify-between gap-2 border-t border-divider pt-3 text-sm text-muted-foreground">
      <div class="flex flex-wrap items-center gap-3">
        <span>{{ t('resourcesPage.lastRefresh') }}: {{ formatDateTime(packageList?.lastRefreshedAt) || t('common.never') }}</span>
        <span v-if="updateCounts.total > 0">
          {{ t('resourcesPage.packageSummary', { total: updateCounts.total, security: updateCounts.security, regular: updateCounts.regular }) }}
        </span>
      </div>
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
