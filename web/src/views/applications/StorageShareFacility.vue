<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { onBeforeRouteLeave, useRouter } from 'vue-router';
import { Download, HardDrive, Plus, RefreshCcw, Rocket, Save, Trash2, Users, Wrench } from '@lucide/vue';
import { storageShareFacilityApi } from '@/api/facilityApps';
import { saveBlobDownload } from '@/api/download';
import type { StoragePartitionStatus, StorageServerSetting, StorageShareConfig, StorageSharePartition, StorageShareStatus } from '@/types/facilityApps';
import type { ServerDto } from '@/types/servers';
import Button from '@/components/ui/Button.vue';
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue';
import Dialog from '@/components/ui/Dialog.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import Input from '@/components/ui/Input.vue';
import Select from '@/components/ui/Select.vue';
import Skeleton from '@/components/ui/Skeleton.vue';
import StatusBadge from '@/components/ui/StatusBadge.vue';
import Tabs from '@/components/ui/Tabs.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import { formatDateTime } from '@/utils/datetime';

const props = defineProps<{
  mode: 'facilityDetail' | 'facilityConfig';
  servers: ServerDto[];
}>();

const { t } = useI18n();
const router = useRouter();
const notifyError = useErrorToast();
const notifySuccess = useSuccessToast();

type DetailView = 'health' | 'partitions';

const config = ref<StorageShareConfig | null>(null);
const status = ref<StorageShareStatus | null>(null);
const loading = ref(false);
const statusLoading = ref(false);
const saving = ref(false);
const pending = ref('');
const error = ref('');
const detailView = ref<DetailView>('health');
const requestedDetailView = ref<DetailView | null>(null);
const form = reactive({ servers: [] as StorageServerSetting[] });
const formErrors = ref<Record<number, string>>({});
const savedServerIds = ref<string[]>([]);
let savedSnapshot = '';
let allowLeave = false;
const uninstallOpen = ref(false);
const deletePartitionTarget = ref<StorageSharePartition | null>(null);
const leaveOpen = ref(false);
let pendingLeave: (() => void) | null = null;
let statusTimer: ReturnType<typeof setInterval> | null = null;

const isSettingsMode = computed(() => props.mode === 'facilityConfig');
const activeView = computed(() => isSettingsMode.value ? 'settings' : detailView.value);
const serverOptions = computed(() => props.servers.map((server) => ({ label: server.name || server.id, value: server.id })));
const uninstallBlocked = computed(() => (config.value?.references?.length ?? 0) > 0);
const dirty = computed(() => JSON.stringify(form.servers) !== savedSnapshot);
const liveExports = computed(() => (status.value?.servers ?? []).filter((server) => server.agentOnline && server.exportLive).length);
const healthyPartitions = computed(() => (config.value?.partitions ?? []).filter((partition) => {
  const partitionStatus = statusPartitionById(partition.id);
  return Boolean(partitionStatus && partitionStatus.volumeExists && partitionStatus.mounted && partitionStatus.writable);
}).length);

const summaryState = computed(() => {
  if (!config.value?.enabled) return 'unconfigured';
  if (!status.value?.servers.length) return 'unknown';
  return status.value.servers.every((server) => server.agentOnline && server.exportLive) ? 'ok' : 'degraded';
});

function serverName(id: string) {
  return props.servers.find((server) => server.id === id)?.name ?? id;
}

function rowOptions(index: number) {
  const own = form.servers[index]?.serverId;
  return serverOptions.value.map((option) => ({
    ...option,
    disabled: option.value !== own && form.servers.some((item, i) => i !== index && item.serverId === option.value),
  }));
}

// 设施启用后，已有存储服务器的根目录不可修改（只能卸载后重新启用）。
function rowRootDisabled(index: number) {
  const item = form.servers[index];
  return Boolean(config.value?.enabled && item && savedServerIds.value.includes(item.serverId));
}

function rowError(index: number): string {
  const item = form.servers[index];
  if (!item) return '';
  const duplicate = form.servers.some((other, i) => i !== index && other.serverId && other.serverId === item.serverId);
  if (duplicate) return t('applicationsPage.storageShareServerDuplicate');
  if (!item.root) return '';
  const root = item.root.trim();
  const valid = /^\/[A-Za-z0-9._/-]+$/.test(root) && !root.includes('//') && !root.endsWith('/') && !root.split('/').some((segment) => segment === '.' || segment === '..');
  if (!valid) return t('applicationsPage.storageShareRootInvalid');
  return '';
}

function addServerRow() {
  form.servers.push({ serverId: '', root: '/opt/panel-shared-storage' });
}

function removeServerRow(index: number) {
  form.servers.splice(index, 1);
}

function resetDraft() {
  form.servers = (config.value?.servers ?? []).map((item) => ({ serverId: item.serverId, root: item.root }));
  savedSnapshot = JSON.stringify(form.servers);
  savedServerIds.value = form.servers.map((item) => item.serverId);
}

async function load() {
  loading.value = true;
  error.value = '';
  try {
    config.value = await storageShareFacilityApi.get();
  } catch (err) {
    notifyError(errorText(err));
    error.value = errorText(err);
  } finally {
    loading.value = false;
  }
}

async function loadStatus() {
  if (!config.value?.enabled || statusLoading.value) return;
  statusLoading.value = true;
  try {
    status.value = await storageShareFacilityApi.status();
  } catch {
    status.value = null;
  } finally {
    statusLoading.value = false;
  }
}

async function refreshAll() {
  await load();
  await loadStatus();
}

function startStatusPoll() {
  stopStatusPoll();
  statusTimer = setInterval(() => { void loadStatus(); }, 15000);
}

function stopStatusPoll() {
  if (statusTimer) {
    clearInterval(statusTimer);
    statusTimer = null;
  }
}

function openSettings() {
  resetDraft();
  void router.push('/applications/facility-apps/storage-share/config');
}

function leaveSettings() {
  if (!dirty.value || allowLeave) {
    allowLeave = true;
    void router.replace('/applications/facility-apps/storage-share');
    return;
  }
  pendingLeave = () => router.replace('/applications/facility-apps/storage-share');
  leaveOpen.value = true;
}

function confirmLeave() {
  leaveOpen.value = false;
  allowLeave = true;
  const action = pendingLeave;
  pendingLeave = null;
  if (action) action();
}

async function save() {
  const servers = form.servers.filter((item) => item.serverId && item.root.trim());
  if (!servers.length) {
    notifyError(t('applicationsPage.storageShareConfigRequired'));
    return;
  }
  const invalid = Object.values(formErrors.value).some(Boolean);
  if (invalid) {
    notifyError(t('applicationsPage.storageShareConfigInvalid'));
    return;
  }
  saving.value = true;
  pending.value = 'save';
  try {
    config.value = await storageShareFacilityApi.save({
      servers: servers.map((item) => ({ serverId: item.serverId, root: item.root.trim() })),
      version: config.value?.version ?? 0,
    });
    resetDraft();
    notifySuccess(t('applicationsPage.storageShareConfigSaved'));
    if (config.value.lastError) notifyError(`${t('applicationsPage.storageShareLastError')}: ${config.value.lastError}`);
    await loadStatus();
  } catch (err) {
    notifyError(errorText(err));
  } finally {
    saving.value = false;
    pending.value = '';
  }
}

async function reconcile() {
  pending.value = 'reconcile';
  try {
    const result = await storageShareFacilityApi.reconcile();
    config.value = result.config;
    notifySuccess(t('applicationsPage.storageShareReconcileTaskAccepted'));
    await loadStatus();
  } catch (err) {
    notifyError(errorText(err));
  } finally {
    pending.value = '';
  }
}

async function uninstall() {
  pending.value = 'uninstall';
  try {
    const result = await storageShareFacilityApi.uninstall();
    uninstallOpen.value = false;
    config.value = result;
    status.value = null;
    notifySuccess(t('applicationsPage.storageShareUninstalled'));
    if (result.lastError) notifyError(`${t('applicationsPage.storageShareLastError')}: ${result.lastError}`);
  } catch (err) {
    notifyError(errorText(err));
  } finally {
    pending.value = '';
  }
}

async function downloadPartition(partition: StorageSharePartition) {
  pending.value = `download-${partition.id}`;
  try {
    saveBlobDownload(await storageShareFacilityApi.downloadPartition(partition.id, `${partition.applicationName}-${partition.serverName}-storage.tgz`));
  } catch (err) {
    notifyError(errorText(err));
  } finally {
    pending.value = '';
  }
}

async function deletePartition() {
  const partition = deletePartitionTarget.value;
  if (!partition) return;
  pending.value = `delete-${partition.id}`;
  try {
    await storageShareFacilityApi.deletePartition(partition.id);
    notifySuccess(t('applicationsPage.storageSharePartitionDeleted'));
    deletePartitionTarget.value = null;
    await load();
    await loadStatus();
  } catch (err) {
    notifyError(errorText(err));
  } finally {
    pending.value = '';
  }
}

function goEditApplication(applicationId: string) {
  void router.push(`/applications/apps/${applicationId}/edit`);
}

function statusPartitionById(id: string): StoragePartitionStatus | undefined {
  return status.value?.partitions.find((item) => item.id === id);
}

function partitionDeleteBlocked(partition: StorageSharePartition) {
  const usage = config.value?.references?.find((item) => item.applicationId === partition.applicationId);
  return usage ? t('applicationsPage.storageShareDeletePartitionBlocked', { name: usage.applicationName }) : '';
}

function partitionStatus(partition: StorageSharePartition) {
  const partitionStatus = statusPartitionById(partition.id);
  if (!partitionStatus) return { tone: 'neutral' as const, label: t('applicationsPage.storageShareMountUnknown') };
  if (!partitionStatus.volumeExists) return { tone: 'warning' as const, label: t('applicationsPage.storageShareMountMissing') };
  if (!partitionStatus.mounted) return { tone: 'neutral' as const, label: t('applicationsPage.storageShareMountNotMounted') };
  if (partitionStatus.writable) return { tone: 'success' as const, label: t('applicationsPage.storageShareMountOk') };
  return { tone: 'warning' as const, label: t('applicationsPage.storageShareMountRo') };
}

function serverChecks(server: StorageShareStatus['servers'][number]) {
  return [
    {
      tone: server.agentOnline ? ('success' as const) : ('danger' as const),
      label: server.agentOnline ? t('applicationsPage.storageShareStateAgentOnline') : t('applicationsPage.storageShareStateAgentOffline'),
    },
    {
      tone: server.rootExists ? ('success' as const) : ('warning' as const),
      label: server.rootExists ? t('applicationsPage.storageShareRootReady') : t('applicationsPage.storageShareRootMissing'),
    },
    {
      tone: server.serverInstalled ? ('success' as const) : ('warning' as const),
      label: server.serverInstalled ? t('applicationsPage.storageShareNfsInstalled') : t('applicationsPage.storageShareNfsMissing'),
    },
    {
      tone: server.exportLive ? ('success' as const) : ('danger' as const),
      label: server.exportLive ? t('applicationsPage.storageShareStateExportLive') : t('applicationsPage.storageShareStateExportDown'),
    },
  ];
}

function selectView(value: string | number) {
  if (value === 'settings') {
    openSettings();
    return;
  }
  if (isSettingsMode.value) {
    requestedDetailView.value = value as DetailView;
    leaveSettings();
    return;
  }
  detailView.value = value as DetailView;
}

function handleBeforeUnload(event: BeforeUnloadEvent) {
  if (!dirty.value || saving.value) return;
  event.preventDefault();
  event.returnValue = '';
}

function errorText(err: unknown) {
  return err instanceof Error ? err.message : String(err);
}

watch(() => form.servers, () => {
  const errors: Record<number, string> = {};
  form.servers.forEach((_, index) => {
    const message = rowError(index);
    if (message) errors[index] = message;
  });
  formErrors.value = errors;
}, { deep: true });

watch(() => props.mode, (mode) => {
  if (mode !== 'facilityConfig') {
    if (requestedDetailView.value) {
      detailView.value = requestedDetailView.value;
      requestedDetailView.value = null;
    }
    return;
  }
  if (!form.servers.length) resetDraft();
});

onBeforeRouteLeave((_to, _from, next) => {
  if (!dirty.value || allowLeave) return next();
  pendingLeave = () => next(true);
  leaveOpen.value = true;
  return next(false);
});

onMounted(async () => {
  window.addEventListener('beforeunload', handleBeforeUnload);
  await load();
  resetDraft();
  await loadStatus();
  startStatusPoll();
});

onBeforeUnmount(() => {
  window.removeEventListener('beforeunload', handleBeforeUnload);
  stopStatusPoll();
});
</script>

<template>
  <div class="grid gap-5">
    <div v-if="loading && !config" class="grid gap-5" aria-busy="true">
      <div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <div v-for="item in 4" :key="item" class="rounded-2xl border border-border bg-card p-4">
          <Skeleton class="h-6 w-12" />
          <Skeleton class="mt-2 h-4 w-24 max-w-full" />
        </div>
      </div>
      <Skeleton class="h-10 w-fit rounded-xl" />
      <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_360px]">
        <div class="grid gap-4">
          <div v-for="item in 2" :key="item" class="rounded-2xl border border-border bg-card p-4">
            <Skeleton class="h-5 w-32 max-w-full" />
            <Skeleton class="mt-3 h-4 w-full" />
            <Skeleton class="mt-2 h-4 w-2/3" />
          </div>
        </div>
        <div class="grid content-start gap-3 rounded-2xl border border-border bg-card p-5">
          <Skeleton class="h-5 w-28 max-w-full" />
          <Skeleton class="h-16 w-full" />
        </div>
      </div>
    </div>

    <div v-else-if="error && !config" class="grid max-w-md justify-items-center gap-3 py-14 text-center">
      <HardDrive class="size-7 text-danger" aria-hidden="true" />
      <p class="m-0 text-sm text-danger">{{ error }}</p>
      <Button size="sm" :loading="loading" @click="refreshAll"><RefreshCcw />{{ t('common.retry') }}</Button>
    </div>

    <template v-else>
      <section class="grid gap-4 rounded-2xl border border-border bg-card p-5">
        <div class="flex flex-wrap items-start justify-between gap-4">
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-2">
              <StatusBadge
                v-if="summaryState === 'ok'"
                status="ok"
                :label="t('applicationsPage.storageShareSummaryOk')"
                tone="success"
              />
              <StatusBadge
                v-else-if="summaryState === 'degraded'"
                status="degraded"
                :label="t('applicationsPage.storageShareSummaryDegraded')"
                tone="danger"
              />
              <StatusBadge
                v-else
                status="unknown"
                :label="t(`applicationsPage.storageShareSummary.${summaryState}`)"
                tone="warning"
              />
              <span v-if="config?.lastError" class="text-xs text-danger">{{ t('applicationsPage.storageShareLastError') }}: {{ config.lastError }}</span>
            </div>
            <p class="m-0 mt-2 text-sm leading-6 text-muted-foreground">{{ t('applicationsPage.storageShareDetailHint') }}</p>
          </div>
          <div v-if="!isSettingsMode" class="flex flex-wrap justify-end gap-2">
            <Button size="sm" :loading="statusLoading" @click="refreshAll"><RefreshCcw />{{ t('common.refresh') }}</Button>
            <Button size="sm" :disabled="!config?.enabled" :loading="pending === 'reconcile'" @click="reconcile"><Rocket />{{ t('applicationsPage.storageShareReconcile') }}</Button>
            <Button size="sm" variant="primary" @click="openSettings"><Wrench />{{ t('common.edit') }}</Button>
            <Button size="sm" variant="danger" :disabled="!config?.enabled" :loading="pending === 'uninstall'" @click="uninstallOpen = true"><Trash2 />{{ t('applicationsPage.storageShareUninstall') }}</Button>
          </div>
        </div>
        <div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
          <div class="rounded-xl border border-border bg-muted/35 p-3"><strong class="block text-lg">{{ config?.servers.length ?? 0 }}</strong><span class="text-xs text-muted-foreground">{{ t('applicationsPage.storageShareServers') }}</span></div>
          <div class="rounded-xl border border-border bg-muted/35 p-3"><strong class="block text-lg">{{ liveExports }}/{{ status?.servers.length ?? 0 }}</strong><span class="text-xs text-muted-foreground">{{ t('applicationsPage.storageShareLiveExports') }}</span></div>
          <div class="rounded-xl border border-border bg-muted/35 p-3"><strong class="block text-lg">{{ config?.partitions.length ?? 0 }}</strong><span class="text-xs text-muted-foreground">{{ t('applicationsPage.storageSharePartitions') }}</span></div>
          <div class="rounded-xl border border-border bg-muted/35 p-3"><strong class="block text-lg">{{ healthyPartitions }}/{{ config?.partitions.length ?? 0 }}</strong><span class="text-xs text-muted-foreground">{{ t('applicationsPage.storageShareHealthyMounts') }}</span></div>
        </div>
      </section>

      <Tabs :model-value="activeView" :tabs="[
        { value: 'health', label: t('applicationsPage.storageShareTabHealth') },
        { value: 'partitions', label: t('applicationsPage.storageShareTabPartitions') },
        { value: 'settings', label: t('applicationsPage.storageShareTabSettings') },
      ]" @update:model-value="selectView($event)">
        <!-- 运行健康 -->
        <div v-if="activeView === 'health'" class="grid items-start gap-4 xl:grid-cols-[minmax(0,1fr)_360px]">
          <div class="grid content-start gap-4 motion-stagger">
            <article v-for="server in status?.servers ?? []" :key="server.serverId" class="motion-reveal grid min-w-0 gap-3 rounded-2xl border border-border bg-card p-4">
              <div class="flex flex-wrap items-center justify-between gap-3">
                <strong class="truncate">{{ serverName(server.serverId) }}</strong>
                <code class="min-w-0 overflow-x-auto whitespace-nowrap rounded-lg bg-muted/40 px-2 py-1 text-xs text-foreground/80">{{ server.root }}</code>
              </div>
              <div class="flex flex-wrap gap-2">
                <StatusBadge
                  v-for="check in serverChecks(server)"
                  :key="check.label"
                  :status="check.label"
                  :label="check.label"
                  :tone="check.tone"
                />
              </div>
              <p v-if="server.detail" class="m-0 break-words text-xs" :class="server.exportLive ? 'text-muted-foreground' : 'text-danger'">{{ server.detail }}</p>
            </article>
            <div v-if="!status?.servers.length" class="rounded-2xl border border-border bg-card p-6">
              <EmptyState :title="t('applicationsPage.storageShareStateNoServers')" :description="t('applicationsPage.storageShareStateNoServersHint')">
                <template #actions>
                  <Button size="sm" variant="primary" @click="openSettings"><Wrench />{{ t('applicationsPage.storageShareGoConfigure') }}</Button>
                </template>
              </EmptyState>
            </div>
          </div>

          <aside class="grid content-start gap-3 rounded-2xl border border-border bg-card p-5">
            <div>
              <h3 class="m-0 text-base font-semibold">{{ t('applicationsPage.storageShareAssociatedAppsTitle') }}</h3>
              <p class="m-0 mt-1 text-sm text-muted-foreground">{{ t('applicationsPage.storageShareAssociationsHint', { count: config?.references?.length ?? 0 }) }}</p>
            </div>
            <ul class="m-0 grid list-none gap-2 p-0">
              <li v-for="usage in config?.references ?? []" :key="usage.applicationId" class="flex min-w-0 items-center justify-between gap-2 rounded-xl border border-border p-3 text-sm">
                <span class="min-w-0 truncate font-medium">{{ usage.applicationName }}</span>
                <Button size="sm" @click="goEditApplication(usage.applicationId)"><Users />{{ t('common.edit') }}</Button>
              </li>
            </ul>
            <EmptyState v-if="!(config?.references?.length)" :title="t('applicationsPage.storageShareNoAssociatedApps')" :description="t('applicationsPage.storageShareNoPartitionsHint')" />
          </aside>
        </div>

        <!-- 分区资产 -->
        <div v-else-if="activeView === 'partitions'" class="motion-stagger grid gap-3">
          <p v-if="statusLoading" class="m-0 text-xs text-muted-foreground">{{ t('applicationsPage.storageShareRefreshingMounts') }}</p>
          <article v-for="partition in config?.partitions ?? []" :key="partition.id" class="motion-reveal grid min-w-0 gap-3 rounded-2xl border border-border bg-card p-4">
            <div class="flex flex-wrap items-start justify-between gap-3">
              <div class="min-w-0">
                <div class="flex flex-wrap items-center gap-2">
                  <strong class="min-w-0 truncate">{{ partition.applicationName }}</strong>
                  <StatusBadge
                    :status="partitionStatus(partition).label"
                    :label="partitionStatus(partition).label"
                    :tone="partitionStatus(partition).tone"
                  />
                </div>
                <span class="mt-1 block text-xs text-muted-foreground">{{ t('applicationsPage.storageShareStorageSource') }}: {{ partition.storageServerName || serverName(partition.storageServerId || '') }} · {{ t('applicationsPage.storageShareAppNode') }}: {{ partition.serverName }}</span>
              </div>
              <div class="flex shrink-0 flex-wrap justify-end gap-2">
                <Button size="sm" :loading="pending === `download-${partition.id}`" @click="downloadPartition(partition)"><Download />{{ t('applicationsPage.storageShareDownload') }}</Button>
                <Button size="sm" variant="danger" :disabled="Boolean(partitionDeleteBlocked(partition))" :title="partitionDeleteBlocked(partition) || undefined" @click="deletePartitionTarget = partition"><Trash2 />{{ t('applicationsPage.storageShareDeletePartition') }}</Button>
              </div>
            </div>
            <dl class="m-0 grid gap-2 sm:grid-cols-2">
              <div class="min-w-0 rounded-xl bg-muted/40 px-3 py-2">
                <dt class="text-xs text-muted-foreground">{{ t('applicationsPage.storageShareHostPath') }}</dt>
                <dd class="m-0 block overflow-x-auto whitespace-nowrap font-mono text-xs">{{ partition.path }}</dd>
              </div>
              <div class="min-w-0 rounded-xl bg-muted/40 px-3 py-2">
                <dt class="text-xs text-muted-foreground">{{ t('applicationsPage.storageShareContainerTarget') }}</dt>
                <dd class="m-0 block overflow-x-auto whitespace-nowrap font-mono text-xs">{{ partition.target || '—' }}</dd>
              </div>
            </dl>
            <div class="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
              <span>{{ t('applicationsPage.storageShareVolumeName') }}: {{ partition.volumeName || '—' }}</span>
              <span>{{ formatDateTime(partition.updatedAt) }}</span>
            </div>
            <p v-if="statusPartitionById(partition.id)?.mountDetail" class="m-0 break-words text-xs text-muted-foreground">{{ statusPartitionById(partition.id)?.mountDetail }}</p>
            <p v-if="partitionDeleteBlocked(partition)" class="m-0 text-xs text-warning">{{ partitionDeleteBlocked(partition) }}</p>
          </article>
          <div v-if="!(config?.partitions.length)" class="rounded-2xl border border-border bg-card p-6">
            <EmptyState :title="t('applicationsPage.storageShareNoPartitions')" :description="t('applicationsPage.storageShareNoPartitionsHint')" />
          </div>
        </div>

        <!-- 设置 -->
        <div v-else class="grid items-start gap-4 xl:grid-cols-[minmax(0,1fr)_360px]">
          <section class="grid min-w-0 gap-4 rounded-2xl border border-border bg-card p-5">
            <div>
              <h3 class="m-0 text-base font-semibold">{{ t('applicationsPage.storageShareConfigTitle') }}</h3>
              <p class="m-0 mt-1 text-sm leading-6 text-muted-foreground">{{ t('applicationsPage.storageShareServerRootHint') }}</p>
            </div>
            <div class="grid gap-3">
              <div v-for="(item, index) in form.servers" :key="index" class="grid gap-3 rounded-xl border border-border p-4">
                <div class="flex items-center justify-between gap-2">
                  <strong class="text-sm">{{ t('applicationsPage.storageShareServerItem', { index: index + 1 }) }}</strong>
                  <Button size="sm" variant="danger" :disabled="form.servers.length <= 1" @click="removeServerRow(index)"><Trash2 />{{ t('common.delete') }}</Button>
                </div>
                <label class="field">{{ t('applicationsPage.storageShareServer') }}
                  <Select v-model="item.serverId" :options="rowOptions(index)" :placeholder="t('applicationsPage.storageShareServerPlaceholder')" />
                </label>
                <label class="field">{{ t('applicationsPage.storageShareRoot') }}
                  <Input v-model="item.root" :placeholder="t('applicationsPage.storageShareRootPlaceholder')" :invalid="Boolean(formErrors[index])" :disabled="rowRootDisabled(index)" />
                </label>
                <p v-if="rowRootDisabled(index)" class="m-0 text-xs text-muted-foreground">{{ t('applicationsPage.storageShareRootImmutableHint') }}</p>
                <p v-if="formErrors[index]" class="m-0 text-xs text-danger">{{ formErrors[index] }}</p>
              </div>
              <Button class="justify-self-start" size="sm" @click="addServerRow"><Plus />{{ t('applicationsPage.storageShareAddServer') }}</Button>
            </div>
            <div class="flex flex-wrap items-center justify-between gap-3 border-t border-border pt-4">
              <span class="text-sm text-muted-foreground">{{ dirty ? t('applicationsPage.unsavedChanges') : t('applicationsPage.readyToCommit') }}</span>
              <div class="flex flex-wrap gap-2">
                <Button variant="secondary" :disabled="saving || !dirty" @click="resetDraft">{{ t('common.cancel') }}</Button>
                <Button variant="primary" :loading="pending === 'save' || saving" :disabled="!form.servers.some((item) => item.serverId && item.root.trim())" @click="save"><Save />{{ t('applicationsPage.storageShareSave') }}</Button>
              </div>
            </div>
          </section>

          <aside class="grid content-start gap-3 rounded-2xl border border-border bg-card p-5">
            <div>
              <h3 class="m-0 text-base font-semibold">{{ t('applicationsPage.storageShareStateTitle') }}</h3>
              <p class="m-0 mt-1 text-sm leading-6 text-muted-foreground">{{ t('applicationsPage.storageShareStateHint') }}</p>
            </div>
            <div class="grid gap-2">
              <div v-for="server in status?.servers ?? []" :key="server.serverId" class="grid gap-2 rounded-xl border border-border p-3 text-sm">
                <div class="flex min-w-0 items-center justify-between gap-2">
                  <strong class="truncate">{{ serverName(server.serverId) }}</strong>
                  <StatusBadge
                    :status="server.exportLive ? t('applicationsPage.storageShareStateExportLive') : t('applicationsPage.storageShareStateExportDown')"
                    :label="server.exportLive ? t('applicationsPage.storageShareStateExportLive') : t('applicationsPage.storageShareStateExportDown')"
                    :tone="server.exportLive ? 'success' : 'danger'"
                  />
                </div>
                <code class="block min-w-0 overflow-x-auto whitespace-nowrap rounded-lg bg-muted/40 px-2 py-1 text-xs text-foreground/80">{{ server.root }}</code>
                <span v-if="server.detail" class="break-words text-xs text-muted-foreground">{{ server.detail }}</span>
              </div>
              <p v-if="!status?.servers.length" class="m-0 text-sm text-muted-foreground">{{ t('applicationsPage.storageShareStateNoServers') }}</p>
            </div>
            <div class="border-t border-border pt-3">
              <Button size="sm" variant="ghost" @click="leaveSettings">{{ t('applicationsPage.storageShareBackToDetail') }}</Button>
            </div>
          </aside>
        </div>
      </Tabs>

      <ConfirmDialog
        v-if="deletePartitionTarget && !partitionDeleteBlocked(deletePartitionTarget)"
        :open="true"
        :title="t('applicationsPage.storageShareDeletePartitionConfirmTitle')"
        :impact="t('applicationsPage.storageShareDeletePartitionImpact')"
        tone="danger"
        :confirm-label="t('applicationsPage.storageShareDeletePartition')"
        :cancel-label="t('common.cancel')"
        :loading="pending === `delete-${deletePartitionTarget.id}`"
        require-checkbox
        :checkbox-label="t('applicationsPage.storageShareDeletePartitionConfirmCheck')"
        @update:open="(value: boolean) => { if (!value) deletePartitionTarget = null }"
        @confirm="deletePartition"
      />

      <Dialog
        v-else
        :open="deletePartitionTarget !== null"
        :title="t('applicationsPage.storageShareDeletePartitionConfirmTitle')"
        :close-label="t('common.close')"
      >
        <div class="grid gap-3">
          <p class="m-0 text-sm text-warning">{{ t('applicationsPage.storageSharePartitionUnlinkHint') }}</p>
          <div v-if="deletePartitionTarget" class="flex items-center justify-between gap-2 rounded-xl border border-border p-3 text-sm">
            <span class="min-w-0 truncate">{{ deletePartitionTarget.applicationName }}</span>
            <Button size="sm" @click="goEditApplication(deletePartitionTarget.applicationId)">{{ t('applicationsPage.storageShareUnlink') }}</Button>
          </div>
        </div>
        <template #footer>
          <Button variant="secondary" @click="deletePartitionTarget = null">{{ t('common.close') }}</Button>
          <Button variant="primary" @click="deletePartitionTarget && goEditApplication(deletePartitionTarget.applicationId)">{{ t('applicationsPage.storageShareUnlink') }}</Button>
        </template>
      </Dialog>

      <Dialog v-model:open="leaveOpen" :title="t('applicationsPage.storageShareUnsavedTitle')" :close-label="t('common.cancel')">
        <p class="m-0 text-sm text-foreground/80">{{ t('applicationsPage.storageShareUnsavedConfirm') }}</p>
        <template #footer>
          <Button variant="secondary" @click="leaveOpen = false">{{ t('common.cancel') }}</Button>
          <Button variant="primary" @click="confirmLeave">{{ t('applicationsPage.storageShareLeave') }}</Button>
        </template>
      </Dialog>

      <Dialog v-model:open="uninstallOpen" :title="t('applicationsPage.storageShareUninstallConfirmTitle')" :close-label="t('common.close')">
        <div class="grid gap-3">
          <template v-if="uninstallBlocked">
            <p class="m-0 text-sm text-warning">{{ t('applicationsPage.storageShareUnlinkHint') }}</p>
            <div v-for="usage in config?.references ?? []" :key="usage.applicationId" class="flex items-center justify-between gap-2 rounded-xl border border-border p-3 text-sm">
              <span class="min-w-0 truncate">{{ usage.applicationName }}</span>
              <Button size="sm" @click="goEditApplication(usage.applicationId)">{{ t('applicationsPage.storageShareUnlink') }}</Button>
            </div>
          </template>
          <template v-else>
            <p class="m-0 text-sm text-foreground/80">{{ t('applicationsPage.storageShareUninstallImpact') }}</p>
          </template>
        </div>
        <template #footer>
          <Button variant="secondary" @click="uninstallOpen = false">{{ t('common.cancel') }}</Button>
          <Button v-if="!uninstallBlocked" variant="danger" :loading="pending === 'uninstall'" @click="uninstall">{{ t('applicationsPage.storageShareUninstall') }}</Button>
        </template>
      </Dialog>
    </template>
  </div>
</template>
