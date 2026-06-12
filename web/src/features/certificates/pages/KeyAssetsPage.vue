<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { keyAssetsApi } from '@/api/keyAssets';
import { useI18n } from '@/i18n';
import type {
  KeyAssetFileKind,
  KeyAssetImportConflictAction,
  KeyAssetImportConflictDto,
  KeyAssetImportConflictStrategy,
  KeyAssetSummaryDto,
} from '@/types/api';
import PageLoadingState from '@/components/PageLoadingState.vue';

type AssetTab = 'ca' | 'tls' | 'ssh';

interface ConfirmDialogState {
  title: string;
  message: string;
  confirmLabel: string;
  color: string;
  acknowledgeRequired: boolean;
  acknowledged: boolean;
}

const { t, formatDateTime } = useI18n();

const items = ref<KeyAssetSummaryDto[]>([]);
const loading = ref(false);
const error = ref('');
const activeTab = ref<AssetTab>('ca');
const busy = ref('');
const snackbar = ref(false);
const snackbarText = ref('');
const snackbarColor = ref('success');
const lastTaskId = ref('');
const lastExportTaskId = ref('');

const selectedByTab = reactive<Record<AssetTab, string[]>>({
  ca: [],
  tls: [],
  ssh: [],
});

const editorDialog = ref(false);
const editorKind = ref<AssetTab>('ca');
const editorImportMode = ref(false);
const caForm = reactive({
  name: '',
  commonName: '',
  validityDays: 3650,
  certificatePem: '',
  privateKeyPem: '',
  publicKeyPem: '',
});
const tlsForm = reactive({
  name: '',
  caId: '',
  commonName: '',
  dnsNames: '',
  ipAddresses: '',
  validityDays: 365,
  certificatePem: '',
  privateKeyPem: '',
  publicKeyPem: '',
});
const sshForm = reactive({
  name: '',
  algorithm: 'ed25519' as 'ed25519' | 'rsa',
  keySize: 3072,
  privateKeyPem: '',
  publicKey: '',
});

const exportDialog = ref(false);
const exportForm = reactive({
  password: '',
  confirmPassword: '',
});

const archiveDialog = ref(false);
const archiveFile = ref<File | null>(null);
const archivePassword = ref('');
const archiveStrategy = ref<KeyAssetImportConflictStrategy>('skip_existing');
const archiveDangerAcknowledged = ref(false);
const archivePreflight = ref<null | {
  planId: string;
  expiresAt: string;
  summary: {
    totalAssets: number;
    caCount: number;
    tlsCount: number;
    sshCount: number;
    standaloneTlsCount: number;
    conflictCount: number;
  };
  assets: Array<{
    assetId: string;
    type: string;
    name: string;
    parentAssetId?: string | null;
    algorithm?: string | null;
    keySize?: number | null;
    commonName?: string | null;
    fingerprint?: string | null;
    standalone: boolean;
    conflictTypes: string[];
  }>;
  conflicts: KeyAssetImportConflictDto[];
  requiresDangerConfirm: boolean;
}>(null);
const archiveResolutions = reactive<Record<string, { action: KeyAssetImportConflictAction; targetAssetId: string }>>({});

const confirmDialog = ref(false);
const confirmState = reactive<ConfirmDialogState>({
  title: '',
  message: '',
  confirmLabel: '',
  color: 'error',
  acknowledgeRequired: false,
  acknowledged: false,
});
let confirmAction: (() => Promise<void>) | null = null;

const caAssets = computed(() => items.value.filter((item) => item.type === 'ca_certificate'));
const tlsAssets = computed(() => items.value.filter((item) => item.type === 'tls_certificate'));
const sshAssets = computed(() => items.value.filter((item) => item.type === 'ssh_key_pair'));
const visibleAssets = computed(() => {
  if (activeTab.value === 'ca') return caAssets.value;
  if (activeTab.value === 'tls') return tlsAssets.value;
  return sshAssets.value;
});
const selectedIds = computed({
  get: () => selectedByTab[activeTab.value],
  set: (value: string[]) => {
    selectedByTab[activeTab.value] = Array.from(new Set(value));
  },
});
const selectedAssets = computed(() => items.value.filter((item) => selectedByTab[activeTab.value].includes(item.id)));
const caOptions = computed(() => caAssets.value.map((item) => ({ title: item.name, value: item.id })));
const allVisibleSelected = computed(
  () => visibleAssets.value.length > 0 && visibleAssets.value.every((item) => selectedIds.value.includes(item.id)),
);
const exportAssets = computed(() => {
  const included = new Map<string, KeyAssetSummaryDto>();
  for (const item of selectedAssets.value) {
    included.set(item.id, item);
    if (item.type === 'ca_certificate') {
      for (const child of tlsAssets.value.filter((tls) => tls.parentAssetId === item.id)) {
        included.set(child.id, child);
      }
    }
  }
  return Array.from(included.values());
});
const editorTitle = computed(() => {
  if (editorKind.value === 'ca') return t('keyAssetsPage.createCaTitle');
  if (editorKind.value === 'tls') return t('keyAssetsPage.createTlsTitle');
  return t('keyAssetsPage.createSshTitle');
});
const archiveNeedsExplicitTargets = computed(
  () =>
    archiveStrategy.value === 'overwrite_existing'
    && archivePreflight.value?.conflicts.some((conflict) =>
      (conflict.overwriteCandidates?.length ?? 0) > 0 && !archiveResolutions[conflict.assetId]?.targetAssetId) === true,
);
const archiveRequiresDangerConfirm = computed(
  () =>
    Boolean(
      archivePreflight.value?.requiresDangerConfirm
    || (archiveStrategy.value === 'overwrite_existing'
      && archivePreflight.value?.conflicts.some((conflict) => (conflict.affectedReferences?.length ?? 0) > 0)),
    ),
);

function taskRoute(taskId = lastTaskId.value) {
  return taskId ? { path: '/tasks', query: { task: taskId } } : '/tasks';
}

function showMessage(text: string, color = 'success') {
  snackbarText.value = text;
  snackbarColor.value = color;
  snackbar.value = true;
}

function resetCaForm() {
  Object.assign(caForm, {
    name: '',
    commonName: '',
    validityDays: 3650,
    certificatePem: '',
    privateKeyPem: '',
    publicKeyPem: '',
  });
}

function resetTlsForm() {
  Object.assign(tlsForm, {
    name: '',
    caId: caAssets.value[0]?.id ?? '',
    commonName: '',
    dnsNames: '',
    ipAddresses: '',
    validityDays: 365,
    certificatePem: '',
    privateKeyPem: '',
    publicKeyPem: '',
  });
}

function resetSshForm() {
  Object.assign(sshForm, {
    name: '',
    algorithm: 'ed25519',
    keySize: 3072,
    privateKeyPem: '',
    publicKey: '',
  });
}

function resetArchiveState() {
  archiveFile.value = null;
  archivePassword.value = '';
  archiveStrategy.value = 'skip_existing';
  archiveDangerAcknowledged.value = false;
  archivePreflight.value = null;
  for (const key of Object.keys(archiveResolutions)) delete archiveResolutions[key];
}

function splitValues(value: string) {
  return value
    .split(/[\n,]+/)
    .map((item) => item.trim())
    .filter(Boolean);
}

function formatOptionalDate(value?: string | null) {
  return value ? formatDateTime(value) : t('common.notAvailable');
}

function assetTypeLabel(value: string) {
  if (value === 'ca_certificate') return t('keyAssetsPage.typeCa');
  if (value === 'tls_certificate') return t('keyAssetsPage.typeTls');
  if (value === 'ssh_key_pair') return t('keyAssetsPage.typeSsh');
  return value;
}

function fileKindLabel(kind: KeyAssetFileKind) {
  if (kind === 'certificate') return t('keyAssetsPage.fileCertificate');
  if (kind === 'private_key') return t('keyAssetsPage.filePrivateKey');
  if (kind === 'public_key') return t('keyAssetsPage.filePublicKey');
  return t('keyAssetsPage.fileSshPublicKey');
}

function conflictTypeLabel(value: string) {
  if (value === 'id_conflict') return t('keyAssetsPage.conflictId');
  if (value === 'name_conflict') return t('keyAssetsPage.conflictName');
  if (value === 'overwrite_in_use') return t('keyAssetsPage.conflictOverwriteInUse');
  if (value === 'missing_parent_ca') return t('keyAssetsPage.missingParentCa');
  return value;
}

function referenceText(asset: KeyAssetSummaryDto) {
  if (asset.referenceCount === 0) return t('keyAssetsPage.noReferences');
  return asset.references.map((item) => `${item.resourceName} (${item.relation})`).join(', ');
}

function toggleSelection(assetId: string, checked: boolean | null) {
  if (checked) {
    selectedIds.value = [...selectedIds.value, assetId];
    return;
  }
  selectedIds.value = selectedIds.value.filter((id) => id !== assetId);
}

function toggleSelectAll(checked: boolean | null) {
  if (checked) {
    selectedIds.value = visibleAssets.value.map((item) => item.id);
    return;
  }
  selectedIds.value = [];
}

function openEditor(kind: AssetTab, importMode = false) {
  editorKind.value = kind;
  editorImportMode.value = importMode;
  if (kind === 'ca') resetCaForm();
  if (kind === 'tls') resetTlsForm();
  if (kind === 'ssh') resetSshForm();
  editorDialog.value = true;
}

async function load() {
  loading.value = true;
  try {
    items.value = await keyAssetsApi.list();
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('keyAssetsPage.loadFailed');
  } finally {
    loading.value = false;
  }
}

async function afterMutation(taskId?: string, successText = t('keyAssetsPage.operationQueued'), preserveExportTask = false) {
  lastTaskId.value = taskId || '';
  if (!preserveExportTask) lastExportTaskId.value = '';
  await load();
  showMessage(successText, 'info');
}

async function submitEditor() {
  busy.value = 'editor';
  try {
    if (editorKind.value === 'ca') {
      const result = editorImportMode.value
        ? await keyAssetsApi.importAsset({
          type: 'ca_certificate',
          name: caForm.name,
          certificatePem: caForm.certificatePem,
          privateKeyPem: caForm.privateKeyPem,
          publicKeyPem: caForm.publicKeyPem || undefined,
        })
        : await keyAssetsApi.createCa({
          name: caForm.name,
          commonName: caForm.commonName,
          validityDays: caForm.validityDays,
        });
      editorDialog.value = false;
      await afterMutation(result.taskId);
      return;
    }

    if (editorKind.value === 'tls') {
      const result = editorImportMode.value
        ? await keyAssetsApi.importAsset({
          type: 'tls_certificate',
          name: tlsForm.name,
          parentAssetId: tlsForm.caId || null,
          certificatePem: tlsForm.certificatePem,
          privateKeyPem: tlsForm.privateKeyPem,
          publicKeyPem: tlsForm.publicKeyPem || undefined,
        })
        : await keyAssetsApi.createTls({
          name: tlsForm.name,
          caId: tlsForm.caId,
          commonName: tlsForm.commonName,
          dnsNames: splitValues(tlsForm.dnsNames),
          ipAddresses: splitValues(tlsForm.ipAddresses),
          validityDays: tlsForm.validityDays,
        });
      editorDialog.value = false;
      await afterMutation(result.taskId);
      return;
    }

    const result = editorImportMode.value
      ? await keyAssetsApi.importAsset({
        type: 'ssh_key_pair',
        name: sshForm.name,
        privateKeyPem: sshForm.privateKeyPem,
        publicKey: sshForm.publicKey || undefined,
      })
      : await keyAssetsApi.generateSsh({
        name: sshForm.name,
        algorithm: sshForm.algorithm,
        keySize: sshForm.algorithm === 'rsa' ? sshForm.keySize : null,
      });
    editorDialog.value = false;
    await afterMutation(result.taskId);
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('keyAssetsPage.createFailed');
  } finally {
    busy.value = '';
  }
}

function openConfirm(config: Omit<ConfirmDialogState, 'acknowledged'>, action: () => Promise<void>) {
  confirmState.title = config.title;
  confirmState.message = config.message;
  confirmState.confirmLabel = config.confirmLabel;
  confirmState.color = config.color;
  confirmState.acknowledgeRequired = config.acknowledgeRequired;
  confirmState.acknowledged = false;
  confirmAction = action;
  confirmDialog.value = true;
}

async function runConfirmAction() {
  if (!confirmAction) return;
  busy.value = 'confirm';
  try {
    await confirmAction();
    confirmDialog.value = false;
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('keyAssetsPage.createFailed');
  } finally {
    busy.value = '';
  }
}

async function triggerReissue(asset: KeyAssetSummaryDto) {
  openConfirm(
    {
      title: t('keyAssetsPage.reissueConfirmTitle'),
      message: t('keyAssetsPage.reissueConfirmMessage'),
      confirmLabel: t('keyAssetsPage.reissue'),
      color: 'warning',
      acknowledgeRequired: true,
    },
    async () => {
      const result = await keyAssetsApi.reissue(asset.id);
      await afterMutation(result.taskId);
    },
  );
}

async function triggerRegenerate(asset: KeyAssetSummaryDto) {
  openConfirm(
    {
      title: t('keyAssetsPage.regenerateConfirmTitle'),
      message: t('keyAssetsPage.regenerateConfirmMessage'),
      confirmLabel: t('keyAssetsPage.regenerate'),
      color: 'error',
      acknowledgeRequired: true,
    },
    async () => {
      const result = await keyAssetsApi.regenerate(asset.id);
      await afterMutation(result.taskId);
    },
  );
}

function triggerDelete(asset: KeyAssetSummaryDto) {
  openConfirm(
    {
      title: t('keyAssetsPage.deleteAsset'),
      message: t('keyAssetsPage.deleteAssetConfirm', { name: asset.name }),
      confirmLabel: t('common.delete'),
      color: 'error',
      acknowledgeRequired: false,
    },
    async () => {
      await keyAssetsApi.delete(asset.id);
      lastTaskId.value = '';
      lastExportTaskId.value = '';
      await load();
      showMessage(t('keyAssetsPage.operationQueued'));
    },
  );
}

async function startDownload(asset: KeyAssetSummaryDto, kind: KeyAssetFileKind) {
  busy.value = `download:${asset.id}:${kind}`;
  try {
    const result = await keyAssetsApi.downloadFile(asset.id, kind);
    const url = URL.createObjectURL(result.blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = result.filename;
    link.click();
    URL.revokeObjectURL(url);
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('keyAssetsPage.downloadFailed');
  } finally {
    busy.value = '';
  }
}

function downloadAssetFile(asset: KeyAssetSummaryDto, kind: KeyAssetFileKind) {
  if (kind !== 'private_key') {
    void startDownload(asset, kind);
    return;
  }
  openConfirm(
    {
      title: t('keyAssetsPage.privateKeyDownloadTitle'),
      message: t('keyAssetsPage.privateKeyDownloadConfirm'),
      confirmLabel: t('keyAssetsPage.downloadPrivateKey'),
      color: 'error',
      acknowledgeRequired: true,
    },
    () => startDownload(asset, kind),
  );
}

function openExportDialog() {
  exportForm.password = '';
  exportForm.confirmPassword = '';
  exportDialog.value = true;
}

async function submitExport() {
  if (exportForm.password.length < 12) {
    error.value = t('keyAssetsPage.exportPasswordMinLength');
    return;
  }
  if (exportForm.password !== exportForm.confirmPassword) {
    error.value = t('keyAssetsPage.exportPasswordMismatch');
    return;
  }

  busy.value = 'export';
  try {
    const result = await keyAssetsApi.exportSelected({
      assetIds: exportAssets.value.map((item) => item.id),
      password: exportForm.password,
    });
    lastTaskId.value = result.taskId;
    lastExportTaskId.value = result.taskId;
    exportDialog.value = false;
    showMessage(t('keyAssetsPage.archiveQueued'), 'info');
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('keyAssetsPage.exportFailed');
  } finally {
    busy.value = '';
  }
}

async function downloadExportArchive() {
  if (!lastExportTaskId.value) return;
  busy.value = 'download-export';
  try {
    const result = await keyAssetsApi.downloadExport(lastExportTaskId.value);
    const url = URL.createObjectURL(result.blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = result.filename;
    link.click();
    URL.revokeObjectURL(url);
    showMessage(t('keyAssetsPage.exportedArchiveReady'));
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('keyAssetsPage.downloadExportFailed');
  } finally {
    busy.value = '';
  }
}

function openArchiveDialog() {
  resetArchiveState();
  archiveDialog.value = true;
}

function updateConflictDefaults(conflict: KeyAssetImportConflictDto) {
  archiveResolutions[conflict.assetId] = {
    action: archiveStrategy.value,
    targetAssetId: archiveResolutions[conflict.assetId]?.targetAssetId ?? '',
  };
}

async function runArchivePreflight() {
  if (!archiveFile.value) return;
  busy.value = 'preflight';
  try {
    const result = await keyAssetsApi.preflightImportArchive(archiveFile.value, archivePassword.value);
    archivePreflight.value = result;
    archiveDangerAcknowledged.value = false;
    for (const key of Object.keys(archiveResolutions)) delete archiveResolutions[key];
    for (const conflict of result.conflicts) updateConflictDefaults(conflict);
    error.value = '';
  } catch (err) {
    archivePreflight.value = null;
    error.value = err instanceof Error ? err.message : t('keyAssetsPage.preflightFailed');
  } finally {
    busy.value = '';
  }
}

function changeArchiveStrategy(value: KeyAssetImportConflictStrategy) {
  archiveStrategy.value = value;
  for (const conflict of archivePreflight.value?.conflicts ?? []) updateConflictDefaults(conflict);
}

async function executeArchiveImport() {
  if (!archivePreflight.value) {
    error.value = t('keyAssetsPage.preflightFailed');
    return;
  }
  if (archiveNeedsExplicitTargets.value) {
    error.value = t('keyAssetsPage.requiresTargetSelection');
    return;
  }
  if (archiveRequiresDangerConfirm.value && !archiveDangerAcknowledged.value) {
    error.value = t('keyAssetsPage.confirmOverwriteMessage');
    return;
  }

  busy.value = 'execute-import';
  try {
    const result = await keyAssetsApi.executeImport(archivePreflight.value.planId, {
      strategy: archiveStrategy.value,
      confirmDangerousOverwrite: archiveRequiresDangerConfirm.value,
      resolutions: archivePreflight.value.conflicts.map((conflict) => ({
        assetId: conflict.assetId,
        action: archiveResolutions[conflict.assetId]?.action ?? archiveStrategy.value,
        targetAssetId: archiveResolutions[conflict.assetId]?.targetAssetId || undefined,
      })),
    });
    archiveDialog.value = false;
    lastTaskId.value = result.taskId;
    lastExportTaskId.value = '';
    showMessage(t('keyAssetsPage.importQueued'), 'info');
    await load();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('keyAssetsPage.importFailed');
  } finally {
    busy.value = '';
  }
}

onMounted(load);
</script>

<template>
  <div class="page-shell">
    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>
    <v-card variant="outlined" class="mb-4">
      <div class="app-card-header">
        <div class="min-width-0">
          <div class="text-h6">{{ t('routes.keyAssets.title') }}</div>
          <div class="text-body-2 text-medium-emphasis">{{ t('keyAssetsPage.subtitle') }}</div>
        </div>
        <div class="toolbar-actions">
          <v-btn
            color="primary"
            prepend-icon="mdi-shield-plus-outline"
            class="text-none"
            @click="openEditor(activeTab, false)"
          >
            {{
              activeTab === 'ca'
                ? t('keyAssetsPage.createCa')
                : activeTab === 'tls'
                  ? t('keyAssetsPage.createTls')
                  : t('keyAssetsPage.generateSsh')
            }}
          </v-btn>
          <v-btn variant="outlined" prepend-icon="mdi-import" class="text-none" @click="openEditor(activeTab, true)">
            {{ t('keyAssetsPage.importAsset') }}
          </v-btn>
          <v-btn variant="outlined" prepend-icon="mdi-archive-arrow-up-outline" class="text-none" @click="openArchiveDialog">
            {{ t('keyAssetsPage.importArchive') }}
          </v-btn>
          <v-btn
            variant="outlined"
            prepend-icon="mdi-archive-arrow-down-outline"
            class="text-none"
            :disabled="selectedAssets.length === 0"
            @click="openExportDialog"
          >
            {{ t('keyAssetsPage.exportSelected') }}
          </v-btn>
        </div>
      </div>
      <v-alert type="info" variant="tonal" class="mx-4 mb-4">
        {{ t('keyAssetsPage.privateKeysHiddenHint') }}
      </v-alert>
      <v-tabs v-model="activeTab" density="comfortable" class="px-4">
        <v-tab value="ca">{{ t('keyAssetsPage.tabs.ca') }}</v-tab>
        <v-tab value="tls">{{ t('keyAssetsPage.tabs.tls') }}</v-tab>
        <v-tab value="ssh">{{ t('keyAssetsPage.tabs.ssh') }}</v-tab>
      </v-tabs>
    </v-card>

    <v-card variant="outlined" class="table-card">
      <div class="selection-bar">
        <div class="text-body-2 text-medium-emphasis">
          {{ t('keyAssetsPage.selectionSummary', { count: selectedAssets.length }) }}
        </div>
        <v-btn
          v-if="lastExportTaskId"
          size="small"
          variant="text"
          prepend-icon="mdi-download"
          class="text-none"
          :loading="busy === 'download-export'"
          @click="downloadExportArchive"
        >
          {{ t('keyAssetsPage.downloadArchive') }}
        </v-btn>
      </div>
      <PageLoadingState v-if="loading && items.length === 0" min-height="360px" />
      <v-window v-else v-model="activeTab">
        <v-window-item value="ca">
          <v-table density="comfortable">
            <thead>
              <tr>
                <th class="checkbox-col">
                  <v-checkbox-btn :model-value="allVisibleSelected" @update:model-value="toggleSelectAll" />
                </th>
                <th>{{ t('common.name') }}</th>
                <th>{{ t('keyAssetsPage.commonName') }}</th>
                <th>{{ t('common.algorithm') }}</th>
                <th>{{ t('common.fingerprint') }}</th>
                <th>{{ t('keyAssetsPage.children') }}</th>
                <th>{{ t('keyAssetsPage.references') }}</th>
                <th>{{ t('common.validUntil') }}</th>
                <th class="text-right">{{ t('common.actions') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="caAssets.length === 0">
                <td colspan="9" class="text-center py-8 text-medium-emphasis">{{ t('keyAssetsPage.noAssets') }}</td>
              </tr>
              <tr v-for="asset in caAssets" :key="asset.id">
                <td class="checkbox-col">
                  <v-checkbox-btn :model-value="selectedIds.includes(asset.id)" @update:model-value="toggleSelection(asset.id, $event)" />
                </td>
                <td>
                  <div class="font-weight-medium">{{ asset.name }}</div>
                  <div class="text-caption text-medium-emphasis mono">{{ asset.id }}</div>
                </td>
                <td>{{ asset.commonName || t('common.notAvailable') }}</td>
                <td>{{ asset.algorithm || t('common.notAvailable') }}</td>
                <td class="mono">{{ asset.fingerprint }}</td>
                <td>{{ asset.childCount }}</td>
                <td class="cell-wrap">{{ referenceText(asset) }}</td>
                <td>{{ formatOptionalDate(asset.notAfter) }}</td>
                <td class="text-right">
                  <div class="row-actions">
                    <v-menu>
                      <template #activator="{ props }">
                        <v-btn v-bind="props" size="small" variant="outlined" prepend-icon="mdi-download" class="text-none">
                          {{ t('common.download') }}
                        </v-btn>
                      </template>
                      <v-list density="compact">
                        <v-list-item
                          v-for="kind in asset.downloadKinds"
                          :key="kind"
                          :title="fileKindLabel(kind)"
                          @click="downloadAssetFile(asset, kind)"
                        />
                      </v-list>
                    </v-menu>
                    <v-btn size="small" color="error" variant="outlined" class="text-none" @click="triggerDelete(asset)">
                      {{ t('common.delete') }}
                    </v-btn>
                  </div>
                </td>
              </tr>
            </tbody>
          </v-table>
        </v-window-item>

        <v-window-item value="tls">
          <v-table density="comfortable">
            <thead>
              <tr>
                <th class="checkbox-col">
                  <v-checkbox-btn :model-value="allVisibleSelected" @update:model-value="toggleSelectAll" />
                </th>
                <th>{{ t('common.name') }}</th>
                <th>{{ t('keyAssetsPage.parentCa') }}</th>
                <th>{{ t('keyAssetsPage.commonName') }}</th>
                <th>{{ t('keyAssetsPage.dnsNames') }}</th>
                <th>{{ t('common.fingerprint') }}</th>
                <th>{{ t('keyAssetsPage.references') }}</th>
                <th>{{ t('common.validUntil') }}</th>
                <th class="text-right">{{ t('common.actions') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="tlsAssets.length === 0">
                <td colspan="9" class="text-center py-8 text-medium-emphasis">{{ t('keyAssetsPage.noAssets') }}</td>
              </tr>
              <tr v-for="asset in tlsAssets" :key="asset.id">
                <td class="checkbox-col">
                  <v-checkbox-btn :model-value="selectedIds.includes(asset.id)" @update:model-value="toggleSelection(asset.id, $event)" />
                </td>
                <td>
                  <div class="font-weight-medium">{{ asset.name }}</div>
                  <div v-if="!asset.parentAssetId" class="text-caption text-warning">{{ t('keyAssetsPage.standaloneTls') }}</div>
                </td>
                <td class="mono">{{ asset.parentAssetId || t('common.notAvailable') }}</td>
                <td>{{ asset.commonName || t('common.notAvailable') }}</td>
                <td class="cell-wrap">{{ asset.dnsNames.join(', ') || t('common.notAvailable') }}</td>
                <td class="mono">{{ asset.fingerprint }}</td>
                <td class="cell-wrap">{{ referenceText(asset) }}</td>
                <td>{{ formatOptionalDate(asset.notAfter) }}</td>
                <td class="text-right">
                  <div class="row-actions">
                    <v-menu>
                      <template #activator="{ props }">
                        <v-btn v-bind="props" size="small" variant="outlined" prepend-icon="mdi-download" class="text-none">
                          {{ t('common.download') }}
                        </v-btn>
                      </template>
                      <v-list density="compact">
                        <v-list-item
                          v-for="kind in asset.downloadKinds"
                          :key="kind"
                          :title="fileKindLabel(kind)"
                          @click="downloadAssetFile(asset, kind)"
                        />
                      </v-list>
                    </v-menu>
                    <v-btn
                      size="small"
                      variant="outlined"
                      class="text-none"
                      :disabled="!asset.canReissue"
                      @click="triggerReissue(asset)"
                    >
                      {{ t('keyAssetsPage.reissue') }}
                    </v-btn>
                    <v-btn size="small" color="error" variant="outlined" class="text-none" @click="triggerDelete(asset)">
                      {{ t('common.delete') }}
                    </v-btn>
                  </div>
                </td>
              </tr>
            </tbody>
          </v-table>
        </v-window-item>

        <v-window-item value="ssh">
          <v-table density="comfortable">
            <thead>
              <tr>
                <th class="checkbox-col">
                  <v-checkbox-btn :model-value="allVisibleSelected" @update:model-value="toggleSelectAll" />
                </th>
                <th>{{ t('common.name') }}</th>
                <th>{{ t('common.algorithm') }}</th>
                <th>{{ t('keyAssetsPage.keySize') }}</th>
                <th>{{ t('common.fingerprint') }}</th>
                <th>{{ t('keyAssetsPage.references') }}</th>
                <th>{{ t('common.updatedAt') }}</th>
                <th class="text-right">{{ t('common.actions') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="sshAssets.length === 0">
                <td colspan="8" class="text-center py-8 text-medium-emphasis">{{ t('keyAssetsPage.noAssets') }}</td>
              </tr>
              <tr v-for="asset in sshAssets" :key="asset.id">
                <td class="checkbox-col">
                  <v-checkbox-btn :model-value="selectedIds.includes(asset.id)" @update:model-value="toggleSelection(asset.id, $event)" />
                </td>
                <td>{{ asset.name }}</td>
                <td>{{ asset.algorithm || t('common.notAvailable') }}</td>
                <td>{{ asset.keySize || t('common.notAvailable') }}</td>
                <td class="mono">{{ asset.fingerprint }}</td>
                <td class="cell-wrap">{{ referenceText(asset) }}</td>
                <td>{{ formatOptionalDate(asset.updatedAt) }}</td>
                <td class="text-right">
                  <div class="row-actions">
                    <v-menu>
                      <template #activator="{ props }">
                        <v-btn v-bind="props" size="small" variant="outlined" prepend-icon="mdi-download" class="text-none">
                          {{ t('common.download') }}
                        </v-btn>
                      </template>
                      <v-list density="compact">
                        <v-list-item
                          v-for="kind in asset.downloadKinds"
                          :key="kind"
                          :title="fileKindLabel(kind)"
                          @click="downloadAssetFile(asset, kind)"
                        />
                      </v-list>
                    </v-menu>
                    <v-btn
                      size="small"
                      variant="outlined"
                      class="text-none"
                      :disabled="!asset.canRegenerate"
                      @click="triggerRegenerate(asset)"
                    >
                      {{ t('keyAssetsPage.regenerate') }}
                    </v-btn>
                    <v-btn size="small" color="error" variant="outlined" class="text-none" @click="triggerDelete(asset)">
                      {{ t('common.delete') }}
                    </v-btn>
                  </div>
                </td>
              </tr>
            </tbody>
          </v-table>
        </v-window-item>
      </v-window>
    </v-card>

    <v-dialog v-model="editorDialog" max-width="760">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ editorTitle }}</span>
          <v-btn icon="mdi-close" variant="text" @click="editorDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          <v-switch v-model="editorImportMode" color="primary" :label="editorImportMode ? t('keyAssetsPage.importMode') : t('keyAssetsPage.generateMode')" />

          <template v-if="editorKind === 'ca'">
            <v-text-field v-model="caForm.name" :label="t('common.name')" :placeholder="t('keyAssetsPage.assetNameHint')" />
            <template v-if="!editorImportMode">
              <v-text-field v-model="caForm.commonName" :label="t('keyAssetsPage.commonName')" />
              <v-text-field v-model.number="caForm.validityDays" type="number" min="1" :label="t('keyAssetsPage.validityDays')" />
            </template>
            <template v-else>
              <v-textarea v-model="caForm.certificatePem" :label="t('keyAssetsPage.certificatePem')" rows="5" />
              <v-textarea v-model="caForm.privateKeyPem" :label="t('keyAssetsPage.privateKeyPem')" rows="5" />
              <v-textarea v-model="caForm.publicKeyPem" :label="t('keyAssetsPage.publicKeyPem')" rows="3" />
            </template>
          </template>

          <template v-else-if="editorKind === 'tls'">
            <v-text-field v-model="tlsForm.name" :label="t('common.name')" :placeholder="t('keyAssetsPage.assetNameHint')" />
            <v-select v-model="tlsForm.caId" :items="caOptions" item-title="title" item-value="value" :label="t('keyAssetsPage.parentCa')" clearable />
            <template v-if="!editorImportMode">
              <v-text-field v-model="tlsForm.commonName" :label="t('keyAssetsPage.commonName')" />
              <v-textarea v-model="tlsForm.dnsNames" :label="t('keyAssetsPage.dnsNames')" :hint="t('keyAssetsPage.dnsNamesHint')" persistent-hint rows="3" />
              <v-textarea v-model="tlsForm.ipAddresses" :label="t('keyAssetsPage.ipAddresses')" :hint="t('keyAssetsPage.ipAddressesHint')" persistent-hint rows="3" />
              <v-text-field v-model.number="tlsForm.validityDays" type="number" min="1" :label="t('keyAssetsPage.validityDays')" />
            </template>
            <template v-else>
              <v-alert type="info" variant="tonal" class="mb-3">{{ t('keyAssetsPage.standaloneTlsHint') }}</v-alert>
              <v-textarea v-model="tlsForm.certificatePem" :label="t('keyAssetsPage.certificatePem')" rows="5" />
              <v-textarea v-model="tlsForm.privateKeyPem" :label="t('keyAssetsPage.privateKeyPem')" rows="5" />
              <v-textarea v-model="tlsForm.publicKeyPem" :label="t('keyAssetsPage.publicKeyPem')" rows="3" />
            </template>
          </template>

          <template v-else>
            <v-text-field v-model="sshForm.name" :label="t('common.name')" :placeholder="t('keyAssetsPage.assetNameHint')" />
            <template v-if="!editorImportMode">
              <v-select
                v-model="sshForm.algorithm"
                :items="[
                  { title: t('keyAssetsPage.algorithmEd25519'), value: 'ed25519' },
                  { title: t('keyAssetsPage.algorithmRsa'), value: 'rsa' },
                ]"
                item-title="title"
                item-value="value"
                :label="t('common.algorithm')"
              />
              <v-select
                v-if="sshForm.algorithm === 'rsa'"
                v-model="sshForm.keySize"
                :items="[2048, 3072, 4096]"
                :label="t('keyAssetsPage.keySize')"
              />
            </template>
            <template v-else>
              <v-alert type="warning" variant="tonal" class="mb-3">{{ t('keyAssetsPage.sshImportHint') }}</v-alert>
              <v-textarea v-model="sshForm.privateKeyPem" :label="t('keyAssetsPage.privateKeyPem')" rows="6" />
              <v-textarea v-model="sshForm.publicKey" :label="t('keyAssetsPage.publicKeyPem')" :hint="t('keyAssetsPage.publicKeyOptionalHint')" persistent-hint rows="3" />
            </template>
          </template>
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <v-btn variant="text" class="text-none" @click="editorDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="primary" variant="flat" class="text-none" :loading="busy === 'editor'" @click="submitEditor">
            {{ editorImportMode ? t('common.import') : t('common.generate') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="exportDialog" max-width="640">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('keyAssetsPage.exportTitle') }}</span>
          <v-btn icon="mdi-close" variant="text" @click="exportDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          <v-alert type="info" variant="tonal" class="mb-4">{{ t('keyAssetsPage.exportHint') }}</v-alert>
          <div class="text-subtitle-2 mb-2">{{ t('keyAssetsPage.exportIncludes') }}</div>
          <v-list density="compact" lines="two" class="export-list">
            <v-list-item v-for="asset in exportAssets" :key="asset.id" :title="asset.name" :subtitle="assetTypeLabel(asset.type)" />
          </v-list>
          <v-text-field v-model="exportForm.password" type="password" :label="t('common.password')" class="mt-3" />
          <v-text-field v-model="exportForm.confirmPassword" type="password" :label="t('common.confirmPassword')" />
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <v-btn variant="text" class="text-none" @click="exportDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="primary" variant="flat" class="text-none" :loading="busy === 'export'" @click="submitExport">
            {{ t('common.export') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="archiveDialog" max-width="980">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('keyAssetsPage.archiveImportTitle') }}</span>
          <v-btn icon="mdi-close" variant="text" @click="archiveDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body archive-dialog">
          <div class="archive-inputs">
            <v-file-input
              v-model="archiveFile"
              :label="t('keyAssetsPage.archiveFile')"
              accept=".panel-key-assets"
              show-size
              prepend-icon="mdi-paperclip"
            />
            <v-text-field v-model="archivePassword" type="password" :label="t('keyAssetsPage.archivePassword')" />
            <v-btn color="primary" variant="outlined" class="text-none" :loading="busy === 'preflight'" :disabled="!archiveFile" @click="runArchivePreflight">
              {{ t('keyAssetsPage.preflight') }}
            </v-btn>
          </div>

          <template v-if="archivePreflight">
            <v-card variant="tonal" color="info">
              <v-card-text class="summary-grid">
                <div><span>{{ t('keyAssetsPage.preflightSummary') }}</span><strong>{{ archivePreflight.summary.totalAssets }}</strong></div>
                <div><span>{{ t('keyAssetsPage.tabs.ca') }}</span><strong>{{ archivePreflight.summary.caCount }}</strong></div>
                <div><span>{{ t('keyAssetsPage.tabs.tls') }}</span><strong>{{ archivePreflight.summary.tlsCount }}</strong></div>
                <div><span>{{ t('keyAssetsPage.tabs.ssh') }}</span><strong>{{ archivePreflight.summary.sshCount }}</strong></div>
                <div><span>{{ t('keyAssetsPage.standaloneTls') }}</span><strong>{{ archivePreflight.summary.standaloneTlsCount }}</strong></div>
                <div><span>{{ t('keyAssetsPage.conflicts') }}</span><strong>{{ archivePreflight.summary.conflictCount }}</strong></div>
              </v-card-text>
            </v-card>

            <div class="d-flex align-center justify-space-between flex-wrap ga-3 mt-4">
              <div class="text-body-2 text-medium-emphasis">{{ t('keyAssetsPage.planExpiresAt', { value: formatOptionalDate(archivePreflight.expiresAt) }) }}</div>
              <v-select
                :model-value="archiveStrategy"
                :items="[
                  { title: t('keyAssetsPage.strategySkip'), value: 'skip_existing' },
                  { title: t('keyAssetsPage.strategyGenerateNewId'), value: 'generate_new_id' },
                  { title: t('keyAssetsPage.strategyOverwrite'), value: 'overwrite_existing' },
                ]"
                item-title="title"
                item-value="value"
                :label="t('keyAssetsPage.conflictStrategy')"
                class="strategy-select"
                @update:model-value="changeArchiveStrategy"
              />
            </div>

            <div class="section-title mt-4">{{ t('keyAssetsPage.planAssets') }}</div>
            <v-table density="compact">
              <thead>
                <tr>
                  <th>{{ t('common.name') }}</th>
                  <th>{{ t('common.type') }}</th>
                  <th>{{ t('common.algorithm') }}</th>
                  <th>{{ t('common.fingerprint') }}</th>
                  <th>{{ t('keyAssetsPage.conflicts') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="asset in archivePreflight.assets" :key="asset.assetId">
                  <td>
                    <div>{{ asset.name }}</div>
                    <div v-if="asset.standalone" class="text-caption text-warning">{{ t('keyAssetsPage.standaloneTls') }}</div>
                  </td>
                  <td>{{ assetTypeLabel(asset.type) }}</td>
                  <td>{{ asset.algorithm || t('common.notAvailable') }}</td>
                  <td class="mono">{{ asset.fingerprint || t('common.notAvailable') }}</td>
                  <td class="cell-wrap">{{ asset.conflictTypes.map(conflictTypeLabel).join(', ') || t('keyAssetsPage.noConflicts') }}</td>
                </tr>
              </tbody>
            </v-table>

            <div class="section-title mt-4">{{ t('keyAssetsPage.conflicts') }}</div>
            <v-list v-if="archivePreflight.conflicts.length" density="compact" lines="three" class="conflict-list">
              <v-list-item v-for="conflict in archivePreflight.conflicts" :key="`${conflict.assetId}:${conflict.conflictType}`">
                <v-list-item-title>{{ conflict.assetName }} · {{ conflictTypeLabel(conflict.conflictType) }}</v-list-item-title>
                <v-list-item-subtitle class="conflict-subtitle">
                  <div v-if="conflict.existingAssetName">{{ conflict.existingAssetName }} ({{ conflict.existingAssetId }})</div>
                  <div v-if="conflict.affectedReferences?.length" class="text-warning">{{ t('keyAssetsPage.overwriteAffectsReferences') }}</div>
                  <v-select
                    v-if="archiveStrategy === 'overwrite_existing' && (conflict.overwriteCandidates?.length ?? 0) > 0"
                    v-model="archiveResolutions[conflict.assetId].targetAssetId"
                    :items="conflict.overwriteCandidates?.map((candidate) => ({ title: `${candidate.name} (${candidate.assetId})`, value: candidate.assetId }))"
                    item-title="title"
                    item-value="value"
                    :label="t('keyAssetsPage.targetAsset')"
                    density="compact"
                    class="mt-2"
                  />
                </v-list-item-subtitle>
              </v-list-item>
            </v-list>
            <div v-else class="text-body-2 text-medium-emphasis">{{ t('keyAssetsPage.noConflicts') }}</div>

            <v-checkbox
              v-if="archiveRequiresDangerConfirm"
              v-model="archiveDangerAcknowledged"
              color="error"
              :label="t('keyAssetsPage.dangerAcknowledge')"
              class="mt-4"
            />
          </template>
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <v-btn variant="text" class="text-none" @click="archiveDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn
            color="primary"
            variant="flat"
            class="text-none"
            :disabled="!archivePreflight"
            :loading="busy === 'execute-import'"
            @click="executeArchiveImport"
          >
            {{ t('keyAssetsPage.executeImport') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="confirmDialog" max-width="560">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ confirmState.title }}</span>
          <v-btn icon="mdi-close" variant="text" @click="confirmDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          <v-alert :type="confirmState.color === 'error' ? 'error' : 'warning'" variant="tonal">{{ confirmState.message }}</v-alert>
          <v-checkbox
            v-if="confirmState.acknowledgeRequired"
            v-model="confirmState.acknowledged"
            color="error"
            :label="t('keyAssetsPage.dangerAcknowledge')"
            class="mt-4"
          />
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <v-btn variant="text" class="text-none" @click="confirmDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn
            :color="confirmState.color"
            variant="flat"
            class="text-none"
            :disabled="confirmState.acknowledgeRequired && !confirmState.acknowledged"
            :loading="busy === 'confirm'"
            @click="runConfirmAction"
          >
            {{ confirmState.confirmLabel }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar" :color="snackbarColor" timeout="5000">
      {{ snackbarText }}
      <template #actions>
        <v-btn v-if="lastTaskId" color="white" variant="text" :to="taskRoute()">{{ t('common.viewTask') }}</v-btn>
        <v-btn
          v-if="lastExportTaskId"
          color="white"
          variant="text"
          :loading="busy === 'download-export'"
          @click="downloadExportArchive"
        >
          {{ t('keyAssetsPage.downloadArchive') }}
        </v-btn>
        <v-btn color="white" variant="text" @click="snackbar = false">{{ t('common.close') }}</v-btn>
      </template>
    </v-snackbar>
  </div>
</template>

<style scoped>
.toolbar-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  justify-content: flex-end;
}

.selection-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 16px 0;
}

.table-card {
  overflow: hidden;
}

.checkbox-col {
  width: 44px;
}

.row-actions {
  display: inline-flex;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.cell-wrap {
  max-width: 280px;
  white-space: normal;
  overflow-wrap: anywhere;
}

.mono {
  font-family: "Cascadia Code", "SFMono-Regular", Consolas, monospace;
}

.min-width-0 {
  min-width: 0;
}

.export-list {
  border: 1px solid var(--lp-border);
  border-radius: 8px;
}

.archive-dialog {
  display: grid;
  gap: 16px;
}

.archive-inputs {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(220px, 280px) auto;
  gap: 12px;
  align-items: start;
}

.summary-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}

.summary-grid span {
  display: block;
  color: var(--lp-text-muted);
  font-size: 12px;
  margin-bottom: 4px;
}

.summary-grid strong {
  display: block;
  font-size: 1rem;
}

.strategy-select {
  max-width: 320px;
}

.section-title {
  font-size: 0.92rem;
  font-weight: 700;
}

.conflict-list {
  border: 1px solid var(--lp-border);
  border-radius: 8px;
}

.conflict-subtitle {
  white-space: normal;
}

@media (max-width: 960px) {
  .archive-inputs,
  .summary-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 720px) {
  .toolbar-actions,
  .selection-bar {
    align-items: stretch;
    flex-direction: column;
  }

  .row-actions {
    display: flex;
    width: 100%;
  }

  .row-actions :deep(.v-btn) {
    flex: 1 1 auto;
  }
}
</style>
