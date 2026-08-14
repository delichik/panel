<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { useRouter } from 'vue-router';
import { Download, HardDrive, Plus, RefreshCcw, Rocket, Save, Trash2, Wrench } from '@lucide/vue';
import { storageShareFacilityApi } from '@/api/facilityApps';
import { saveBlobDownload } from '@/api/download';
import type { StorageServerSetting, StorageShareConfig, StorageSharePartition } from '@/types/facilityApps';
import type { ServerDto } from '@/types/servers';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue';
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
const loading = ref(false);
const saving = ref(false);
const pending = ref('');
const error = ref('');
const form = reactive({ servers: [] as StorageServerSetting[] });
const uninstallOpen = ref(false);
const deletePartitionTarget = ref<StorageSharePartition | null>(null);

const isConfigMode = computed(() => props.mode === 'facilityConfig');
const serverOptions = computed(() => props.servers.map((server) => ({ label: server.name || server.id, value: server.id })));
const uninstallBlocked = computed(() => (config.value?.references?.length ?? 0) > 0);

function serverName(id: string) {
  return props.servers.find((server) => server.id === id)?.name ?? id;
}

function rowOptions(index: number) {
  const selected = new Set(form.servers.map((item) => item.serverId).filter(Boolean));
  return serverOptions.value.map((option) => ({ ...option, disabled: index !== form.servers.length - 1 ? selected.has(option.value) : selected.has(option.value) && option.value !== form.servers[index]?.serverId }));
}

function addServerRow() {
  form.servers.push({ serverId: '', root: '/srv/panel-storage' });
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

function openConfig() {
  form.servers = (config.value?.servers ?? []).map((item) => ({ serverId: item.serverId, root: item.root }));
  void router.push('/applications/facility-apps/storage-share/config');
}

async function save() {
  const servers = form.servers.filter((item) => item.serverId && item.root.trim());
  if (!servers.length) {
    notifyError(t('applicationsPage.storageShareConfigRequired'));
    return;
  }
  saving.value = true;
  pending.value = 'save';
  try {
    config.value = await storageShareFacilityApi.save({ servers: servers.map((item) => ({ serverId: item.serverId, root: item.root.trim() })) });
    notifySuccess(t('applicationsPage.storageShareConfigSaved'));
    if (config.value.lastError) notifyError(t('applicationsPage.storageShareLastError') + ': ' + config.value.lastError);
    await router.replace('/applications/facility-apps/storage-share');
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
    config.value = await storageShareFacilityApi.reconcile();
    notifySuccess(t('applicationsPage.storageShareReconcileAccepted'));
    if (config.value.lastError) notifyError(t('applicationsPage.storageShareLastError') + ': ' + config.value.lastError);
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
  } catch (err) {
    notifyError(errorText(err));
  } finally {
    pending.value = '';
  }
}

function partitionDeleteBlocked(partition: StorageSharePartition) {
  const usage = config.value?.references?.find((item) => item.applicationId === partition.applicationId);
  return usage ? t('applicationsPage.storageShareDeletePartitionBlocked', { name: usage.applicationName }) : '';
}

function errorText(err: unknown) {
  return err instanceof Error ? err.message : String(err);
}

onMounted(load);
</script>

<template>
  <div class="grid h-full min-h-[640px] gap-4 xl:grid-cols-[minmax(0,1fr)_340px]">
    <section class="min-h-0 overflow-auto rounded-2xl border border-border bg-card p-5">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="section-copy">
          <h3>{{ t('applicationsPage.storageSharePartitions') }}</h3>
          <p>{{ t('applicationsPage.storageSharePartitionsHint') }}</p>
        </div>
        <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
      </div>
      <div v-if="loading && !config" class="relative grid min-h-64 place-items-center">
        <LoadingOverlay />
      </div>
      <div v-else-if="error && !config" class="grid max-w-md justify-items-center gap-3 py-10 text-center">
        <HardDrive class="size-6 text-danger" aria-hidden="true" />
        <p class="m-0 text-sm text-danger">{{ error }}</p>
        <Button size="sm" :loading="loading" @click="load">{{ t('common.retry') }}</Button>
      </div>
      <div v-else-if="config" class="mt-3 grid gap-3 motion-stagger">
        <div v-for="partition in config.partitions" :key="partition.id" class="motion-reveal grid gap-2 rounded-xl border border-border p-3 text-sm">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <strong>{{ partition.applicationName }}</strong>
            <div class="flex flex-wrap gap-2">
              <Badge tone="info">{{ serverName(partition.storageServerId || '') }}</Badge>
              <Badge tone="neutral">{{ partition.serverName }}</Badge>
            </div>
          </div>
          <code class="block overflow-x-auto whitespace-nowrap rounded-lg bg-muted/40 px-2 py-1 text-xs text-foreground/80">{{ partition.path }}</code>
          <div class="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
            <span>{{ formatDateTime(partition.updatedAt) }}</span>
            <div class="flex flex-wrap gap-2">
              <Button size="sm" :loading="pending === `download-${partition.id}`" @click="downloadPartition(partition)"><Download />{{ t('applicationsPage.storageShareDownload') }}</Button>
              <Button size="sm" variant="danger" :disabled="Boolean(partitionDeleteBlocked(partition))" :title="partitionDeleteBlocked(partition) || undefined" @click="deletePartitionTarget = partition"><Trash2 />{{ t('applicationsPage.storageShareDeletePartition') }}</Button>
            </div>
          </div>
          <p v-if="partitionDeleteBlocked(partition)" class="m-0 text-xs text-warning">{{ partitionDeleteBlocked(partition) }}</p>
        </div>
        <EmptyState v-if="!config.partitions.length" :title="t('applicationsPage.storageShareNoPartitions')" :description="t('applicationsPage.storageShareNoPartitionsHint')" />
      </div>
    </section>

    <aside class="min-h-0 overflow-auto rounded-2xl border border-border bg-card p-5">
      <div class="flex items-center justify-between gap-3">
        <div class="section-copy">
          <h3>{{ t('applicationsPage.storageShareConfigTitle') }}</h3>
          <p>{{ t('applicationsPage.storageShareConfigHint') }}</p>
        </div>
        <Button v-if="!isConfigMode" size="sm" variant="primary" @click="openConfig"><Wrench />{{ t('common.edit') }}</Button>
      </div>

      <div v-if="loading && !config" class="relative grid min-h-48 place-items-center">
        <LoadingOverlay />
      </div>
      <div v-else-if="config" class="mt-4 grid gap-4">
        <div v-if="!isConfigMode" class="grid gap-2">
          <span class="text-xs text-muted-foreground">{{ t('applicationsPage.storageShareServers') }}</span>
          <div v-for="item in config.servers" :key="item.serverId" class="grid gap-1 rounded-xl border border-border bg-background p-3 text-sm">
            <strong class="truncate">{{ serverName(item.serverId) }}</strong>
            <code class="block overflow-x-auto whitespace-nowrap rounded-lg bg-muted/40 px-2 py-1 text-xs text-foreground/80">{{ item.root }}</code>
          </div>
          <p v-if="!config.servers.length" class="m-0 text-sm text-foreground/60">{{ t('common.notAvailable') }}</p>
        </div>

        <div v-if="isConfigMode" class="grid gap-3">
          <div class="section-copy">
            <h3>{{ t('applicationsPage.storageShareServers') }}</h3>
            <p>{{ t('applicationsPage.storageShareServerRootHint') }}</p>
          </div>
          <div v-for="(item, index) in form.servers" :key="index" class="grid gap-2 rounded-xl border border-border p-3">
            <label class="field">{{ t('applicationsPage.storageShareServer') }}<Select v-model="item.serverId" :options="rowOptions(index)" :placeholder="t('applicationsPage.storageShareServerPlaceholder')" /></label>
            <label class="field">{{ t('applicationsPage.storageShareRoot') }}<Input v-model="item.root" :placeholder="t('applicationsPage.storageShareRootPlaceholder')" /></label>
            <Button size="sm" variant="danger" :disabled="form.servers.length <= 1" @click="removeServerRow(index)"><Trash2 />{{ t('common.delete') }}</Button>
          </div>
          <Button size="sm" @click="addServerRow"><Plus />{{ t('applicationsPage.storageShareAddServer') }}</Button>
        </div>

        <div v-if="config.enabled" class="flex flex-wrap items-center gap-2">
          <StatusBadge status="enabled" :label="t('applicationsPage.storageShareEnabled')" />
          <span v-if="config.lastError" class="text-xs text-danger">{{ t('applicationsPage.storageShareLastError') }}: {{ config.lastError }}</span>
        </div>
        <div v-else class="flex flex-wrap items-center gap-2">
          <StatusBadge status="disabled" :label="t('applicationsPage.storageShareDisabled')" />
        </div>

        <div class="grid gap-2">
          <Button v-if="isConfigMode" variant="primary" :loading="pending === 'save' || saving" :disabled="!form.servers.some((item) => item.serverId && item.root.trim())" @click="save"><Save />{{ t('applicationsPage.storageShareSave') }}</Button>
          <Button :disabled="!config.enabled" :loading="pending === 'reconcile'" @click="reconcile"><Rocket />{{ t('applicationsPage.storageShareReconcile') }}</Button>
        </div>

        <div v-if="config.references?.length" class="rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning">
          <p class="m-0 font-medium">{{ t('applicationsPage.storageShareUninstallBlocked', { names: config.references.map((item) => item.applicationName).join(', ') }) }}</p>
        </div>
        <Button v-if="!isConfigMode" variant="danger" :disabled="!config.enabled || uninstallBlocked" :loading="pending === 'uninstall'" @click="uninstallOpen = true"><Trash2 />{{ t('applicationsPage.storageShareUninstall') }}</Button>
        <p v-if="!isConfigMode && uninstallBlocked" class="m-0 text-xs text-warning">{{ t('applicationsPage.storageShareUninstallHint') }}</p>
      </div>
    </aside>

    <ConfirmDialog
      v-model:open="uninstallOpen"
      :title="t('applicationsPage.storageShareUninstallConfirmTitle')"
      :impact="t('applicationsPage.storageShareUninstallImpact')"
      tone="danger"
      :confirm-label="t('common.confirm')"
      :cancel-label="t('common.cancel')"
      :loading="pending === 'uninstall'"
      :checkbox-label="t('applicationsPage.storageShareUninstallConfirmCheckbox')"
      require-checkbox
      @confirm="uninstall"
    />
    <ConfirmDialog
      :open="deletePartitionTarget !== null"
      @update:open="(value: boolean) => { if (!value) deletePartitionTarget = null }"
      :title="t('applicationsPage.storageShareDeletePartitionConfirmTitle')"
      :impact="t('applicationsPage.storageShareDeletePartitionImpact')"
      tone="danger"
      :confirm-label="t('common.confirm')"
      :cancel-label="t('common.cancel')"
      :loading="Boolean(deletePartitionTarget && pending === `delete-${deletePartitionTarget.id}`)"
      :checkbox-label="t('applicationsPage.storageShareDeletePartitionCheckbox')"
      require-checkbox
      @confirm="deletePartition"
    />
  </div>
</template>