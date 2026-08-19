<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { onBeforeRouteLeave, useRouter } from 'vue-router';
import { ArrowLeft, Download, HardDrive, Plus, RefreshCcw, Rocket, Save, Trash2, Users, Wrench } from '@lucide/vue';
import { storageShareFacilityApi } from '@/api/facilityApps';
import { saveBlobDownload } from '@/api/download';
import type { StoragePartitionStatus, StorageServerSetting, StorageShareConfig, StorageSharePartition, StorageShareStatus } from '@/types/facilityApps';
import type { ServerDto } from '@/types/servers';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import Dialog from '@/components/ui/Dialog.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import Input from '@/components/ui/Input.vue';
import LoadingOverlay from '@/components/ui/LoadingOverlay.vue';
import Select from '@/components/ui/Select.vue';
import StatusBadge from '@/components/ui/StatusBadge.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import { formatDateTime } from '@/utils/datetime';

const props = defineProps<{
  mode: 'facilityDetail' | 'facilityConfig';
  servers: ServerDto[];
}>();

const emit = defineEmits<{ back: [] }>();

const { t } = useI18n();
const router = useRouter();
const notifyError = useErrorToast();
const notifySuccess = useSuccessToast();

const config = ref<StorageShareConfig | null>(null);
const status = ref<StorageShareStatus | null>(null);
const loading = ref(false);
const statusLoading = ref(false);
const saving = ref(false);
const pending = ref('');
const error = ref('');
const form = reactive({ servers: [] as StorageServerSetting[] });
const formErrors = ref<Record<number, string>>({});
const savedServerIds = ref<string[]>([]);
let savedSnapshot = '';
let allowLeave = false;
const uninstallOpen = ref(false);
const deletePartitionTarget = ref<StorageSharePartition | null>(null);
const associationsOpen = ref(false);
const leaveOpen = ref(false);
let pendingLeave: (() => void) | null = null;
let statusTimer: ReturnType<typeof setInterval> | null = null;

const isConfigMode = computed(() => props.mode === 'facilityConfig');
const serverOptions = computed(() => props.servers.map((server) => ({ label: server.name || server.id, value: server.id })));
const uninstallBlocked = computed(() => (config.value?.references?.length ?? 0) > 0);
const dirty = computed(() => JSON.stringify(form.servers) !== savedSnapshot);

const summaryState = computed(() => {
  if (!config.value?.enabled) return 'unconfigured';
  if (!status.value?.servers.length) return 'unknown';
  return status.value.servers.every((server) => server.exportLive) ? 'ok' : 'degraded';
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

function openConfig() {
  form.servers = (config.value?.servers ?? []).map((item) => ({ serverId: item.serverId, root: item.root }));
  savedSnapshot = JSON.stringify(form.servers);
  savedServerIds.value = form.servers.map((item) => item.serverId);
  void router.push('/applications/facility-apps/storage-share/config');
}

function backToDetail() {
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
    config.value = await storageShareFacilityApi.save({ servers: servers.map((item) => ({ serverId: item.serverId, root: item.root.trim() })), version: config.value?.version ?? 0 });
    form.servers = config.value.servers.map((item) => ({ serverId: item.serverId, root: item.root }));
    savedSnapshot = JSON.stringify(form.servers);
    notifySuccess(t('applicationsPage.storageShareConfigSaved'));
    if (config.value.lastError) notifyError(t('applicationsPage.storageShareLastError') + ': ' + config.value.lastError);
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
    if (result.lastError) notifyError(t('applicationsPage.storageShareLastError') + ': ' + result.lastError);
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

function statusPartitionById(id: string) {
  return status.value?.partitions.find((item) => item.id === id);
}

function partitionDeleteBlocked(partition: StorageSharePartition) {
  const usage = config.value?.references?.find((item) => item.applicationId === partition.applicationId);
  return usage ? t('applicationsPage.storageShareDeletePartitionBlocked', { name: usage.applicationName }) : '';
}

function mountState(partition: StoragePartitionStatus) {
  if (!partition.volumeExists) return { tone: 'warning' as const, label: t('applicationsPage.storageShareMountMissing') };
  if (!partition.mounted) return { tone: 'neutral' as const, label: t('applicationsPage.storageShareMountNotMounted') };
  if (partition.writable) return { tone: 'success' as const, label: t('applicationsPage.storageShareMountOk') };
  return { tone: 'warning' as const, label: t('applicationsPage.storageShareMountRo') };
}

function serverState(server: { agentOnline: boolean; exportLive: boolean }) {
  if (!server.agentOnline) return { tone: 'danger' as const, label: t('applicationsPage.storageShareStateAgentOffline') };
  if (server.exportLive) return { tone: 'success' as const, label: t('applicationsPage.storageShareStateExportLive') };
  return { tone: 'danger' as const, label: t('applicationsPage.storageShareStateExportDown') };
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

onBeforeRouteLeave((_to, _from, next) => {
  if (!dirty.value || allowLeave) return next();
  pendingLeave = () => next(true);
  leaveOpen.value = true;
  return next(false);
});

onMounted(async () => {
  await load();
  savedSnapshot = JSON.stringify(form.servers);
  if (isConfigMode.value && config.value) {
    form.servers = config.value.servers.map((item) => ({ serverId: item.serverId, root: item.root }));
    savedSnapshot = JSON.stringify(form.servers);
    savedServerIds.value = form.servers.map((item) => item.serverId);
  }
  await loadStatus();
  startStatusPoll();
});

onBeforeUnmount(() => {
  stopStatusPoll();
});
</script>

<template>
  <div class="grid gap-4">
    <div v-if="isConfigMode" class="grid items-start gap-4 xl:grid-cols-[minmax(0,1fr)_360px]">
      <section class="rounded-2xl border border-border bg-card p-5">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div class="section-copy">
            <h3>{{ t('applicationsPage.storageShareConfigTitle') }}</h3>
            <p>{{ t('applicationsPage.storageShareServerRootHint') }}</p>
          </div>
          <Button size="sm" variant="ghost" @click="backToDetail"><ArrowLeft />{{ t('applicationsPage.storageShareBackToDetail') }}</Button>
        </div>

        <div class="mt-4 grid gap-3">
          <div v-for="(item, index) in form.servers" :key="index" class="grid gap-2 rounded-xl border border-border p-3">
            <label class="field">{{ t('applicationsPage.storageShareServer') }}<Select v-model="item.serverId" :options="rowOptions(index)" :placeholder="t('applicationsPage.storageShareServerPlaceholder')" /></label>
            <label class="field">{{ t('applicationsPage.storageShareRoot') }}<Input v-model="item.root" :placeholder="t('applicationsPage.storageShareRootPlaceholder')" :invalid="Boolean(formErrors[index])" :disabled="rowRootDisabled(index)" /></label>
            <p v-if="rowRootDisabled(index)" class="m-0 text-xs text-muted-foreground">{{ t('applicationsPage.storageShareRootImmutableHint') }}</p>
            <p v-if="formErrors[index]" class="m-0 text-xs text-danger">{{ formErrors[index] }}</p>
            <Button size="sm" variant="danger" :disabled="form.servers.length <= 1" @click="removeServerRow(index)"><Trash2 />{{ t('common.delete') }}</Button>
          </div>
          <Button size="sm" @click="addServerRow"><Plus />{{ t('applicationsPage.storageShareAddServer') }}</Button>
        </div>

        <div class="mt-4 flex flex-wrap gap-2">
          <Button variant="primary" :loading="pending === 'save' || saving" :disabled="!form.servers.some((item) => item.serverId && item.root.trim())" @click="save"><Save />{{ t('applicationsPage.storageShareSave') }}</Button>
        </div>
      </section>

      <aside class="grid content-start gap-3 rounded-2xl border border-border bg-card p-5">
        <div class="section-copy">
          <h3>{{ t('applicationsPage.storageShareStateTitle') }}</h3>
          <p>{{ t('applicationsPage.storageShareStateHint') }}</p>
        </div>
        <div class="mt-4 grid gap-2">
          <div v-for="server in status?.servers ?? []" :key="server.serverId" class="grid gap-1 rounded-xl border border-border p-3 text-sm">
            <div class="flex items-center justify-between gap-2">
              <strong class="truncate">{{ serverName(server.serverId) }}</strong>
              <StatusBadge :status="serverState(server).label" :label="serverState(server).label" :tone="serverState(server).tone" />
            </div>
            <code class="block overflow-x-auto whitespace-nowrap rounded-lg bg-muted/40 px-2 py-1 text-xs text-foreground/80">{{ server.root }}</code>
            <span v-if="server.detail" class="text-xs text-muted-foreground">{{ server.detail }}</span>
          </div>
          <p v-if="!status?.servers.length" class="m-0 text-sm text-muted-foreground">{{ t('applicationsPage.storageShareStateNoServers') }}</p>
        </div>
      </aside>
    </div>

    <template v-else>
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="flex flex-wrap items-center gap-2">
          <StatusBadge v-if="summaryState === 'ok'" status="ok" :label="t('applicationsPage.storageShareSummaryOk')" tone="success" />
          <StatusBadge v-else-if="summaryState === 'degraded'" status="degraded" :label="t('applicationsPage.storageShareSummaryDegraded')" tone="danger" />
          <StatusBadge v-else status="unknown" :label="t(`applicationsPage.storageShareSummary.${summaryState}`)" tone="warning" />
          <span v-if="config?.lastError" class="text-xs text-danger">{{ t('applicationsPage.storageShareLastError') }}: {{ config.lastError }}</span>
        </div>
        <div class="flex flex-wrap gap-2">
          <Button size="sm" :loading="statusLoading" @click="loadStatus"><RefreshCcw />{{ t('common.refresh') }}</Button>
          <Button size="sm" :disabled="!config?.enabled" :loading="pending === 'reconcile'" @click="reconcile"><Rocket />{{ t('applicationsPage.storageShareReconcile') }}</Button>
          <Button size="sm" @click="associationsOpen = true"><Users />{{ t('applicationsPage.storageShareAssociatedApps', { count: config?.partitions.length ?? 0 }) }}</Button>
          <Button size="sm" variant="primary" @click="openConfig"><Wrench />{{ t('common.edit') }}</Button>
          <Button size="sm" variant="danger" :disabled="!config?.enabled" :loading="pending === 'uninstall'" @click="uninstallOpen = true"><Trash2 />{{ t('applicationsPage.storageShareUninstall') }}</Button>
        </div>
      </div>

      <div v-if="loading && !config" class="relative grid min-h-64 place-items-center">
        <LoadingOverlay />
      </div>
      <div v-else-if="error && !config" class="grid max-w-md justify-items-center gap-3 py-10 text-center">
        <HardDrive class="size-6 text-danger" aria-hidden="true" />
        <p class="m-0 text-sm text-danger">{{ error }}</p>
        <Button size="sm" :loading="loading" @click="load">{{ t('common.retry') }}</Button>
      </div>
      <div v-else class="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <div v-for="server in status?.servers ?? []" :key="server.serverId" class="grid gap-2 rounded-2xl border border-border bg-card p-4">
          <div class="flex items-center justify-between gap-2">
            <strong class="truncate">{{ serverName(server.serverId) }}</strong>
            <StatusBadge :status="serverState(server).label" :label="serverState(server).label" :tone="serverState(server).tone" />
          </div>
          <div class="grid gap-1 text-sm">
            <span class="text-muted-foreground">{{ t('applicationsPage.storageShareRoot') }}</span>
            <code class="block overflow-x-auto whitespace-nowrap rounded-lg bg-muted/40 px-2 py-1 text-xs text-foreground/80">{{ server.root }}</code>
          </div>
          <p v-if="server.detail" class="m-0 text-xs text-muted-foreground">{{ server.detail }}</p>
          <p v-if="!server.exportLive && server.detail" class="m-0 text-xs text-danger">{{ server.detail }}</p>
        </div>
        <div v-if="!status?.servers.length" class="rounded-2xl border border-border bg-card p-6">
          <EmptyState :title="t('applicationsPage.storageShareStateNoServers')" :description="t('applicationsPage.storageShareStateNoServersHint')">
            <template #actions>
              <Button size="sm" variant="primary" @click="openConfig"><Wrench />{{ t('applicationsPage.storageShareGoConfigure') }}</Button>
            </template>
          </EmptyState>
        </div>
      </div>
    </template>

    <Dialog v-model:open="associationsOpen" :title="t('applicationsPage.storageShareAssociatedAppsTitle')" :close-label="t('common.close')">
      <div class="grid max-h-[70vh] gap-2 overflow-y-auto">
        <div v-for="partition in config?.partitions ?? []" :key="partition.id" class="grid gap-1 rounded-xl border border-border p-3 text-sm">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <strong>{{ partition.applicationName }}</strong>
            <div class="flex flex-wrap gap-2">
              <Badge tone="info">{{ serverName(partition.storageServerId || '') }}</Badge>
              <Badge tone="neutral">{{ partition.serverName }}</Badge>
              <StatusBadge :status="statusPartitionById(partition.id) ? mountState(statusPartitionById(partition.id)!).label : t('applicationsPage.storageShareMountUnknown')" :label="statusPartitionById(partition.id) ? mountState(statusPartitionById(partition.id)!).label : t('applicationsPage.storageShareMountUnknown')" :tone="statusPartitionById(partition.id) ? mountState(statusPartitionById(partition.id)!).tone : 'neutral'" />
            </div>
          </div>
          <code class="block overflow-x-auto whitespace-nowrap rounded-lg bg-muted/40 px-2 py-1 text-xs text-foreground/80">{{ partition.path }} → {{ partition.target }}</code>
          <div class="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
            <span>{{ formatDateTime(partition.updatedAt) }}</span>
            <div class="flex flex-wrap gap-2">
              <Button size="sm" :loading="pending === `download-${partition.id}`" @click="downloadPartition(partition)"><Download />{{ t('applicationsPage.storageShareDownload') }}</Button>
              <Button size="sm" variant="danger" :disabled="Boolean(partitionDeleteBlocked(partition))" :title="partitionDeleteBlocked(partition) || undefined" @click="deletePartitionTarget = partition"><Trash2 />{{ t('applicationsPage.storageShareDeletePartition') }}</Button>
            </div>
          </div>
          <p v-if="statusPartitionById(partition.id)?.mountDetail" class="m-0 text-xs text-muted-foreground">{{ statusPartitionById(partition.id)?.mountDetail }}</p>
          <p v-if="partitionDeleteBlocked(partition)" class="m-0 text-xs text-warning">{{ partitionDeleteBlocked(partition) }}</p>
        </div>
        <EmptyState v-if="!(config?.partitions.length)" :title="t('applicationsPage.storageShareNoPartitions')" :description="t('applicationsPage.storageShareNoPartitionsHint')" />
      </div>
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

    <Dialog :open="deletePartitionTarget !== null" @update:open="(value: boolean) => { if (!value) deletePartitionTarget = null }" :title="t('applicationsPage.storageShareDeletePartitionConfirmTitle')" :close-label="t('common.close')">
      <div class="grid gap-3">
        <p v-if="deletePartitionTarget && partitionDeleteBlocked(deletePartitionTarget)" class="m-0 text-sm text-warning">{{ t('applicationsPage.storageSharePartitionUnlinkHint') }}</p>
        <p v-else class="m-0 text-sm text-foreground/80">{{ t('applicationsPage.storageShareDeletePartitionImpact') }}</p>
        <div v-if="deletePartitionTarget && partitionDeleteBlocked(deletePartitionTarget)" class="flex items-center justify-between gap-2 rounded-xl border border-border p-3 text-sm">
          <span class="min-w-0 truncate">{{ deletePartitionTarget.applicationName }}</span>
          <Button size="sm" @click="goEditApplication(deletePartitionTarget.applicationId)">{{ t('applicationsPage.storageShareUnlink') }}</Button>
        </div>
      </div>
      <template #footer>
        <Button variant="secondary" @click="deletePartitionTarget = null">{{ t('common.cancel') }}</Button>
        <Button v-if="!deletePartitionTarget || !partitionDeleteBlocked(deletePartitionTarget)" variant="danger" :loading="Boolean(deletePartitionTarget && pending === `delete-${deletePartitionTarget.id}`)" @click="deletePartition">{{ t('applicationsPage.storageShareDeletePartition') }}</Button>
        <Button v-else variant="primary" @click="goEditApplication(deletePartitionTarget.applicationId)">{{ t('applicationsPage.storageShareUnlink') }}</Button>
      </template>
    </Dialog>
  </div>
</template>
