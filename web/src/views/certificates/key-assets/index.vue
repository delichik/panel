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
import AppActionButton from '@/components/AppActionButton.vue';
import AppActionGroup from '@/components/AppActionGroup.vue';
import AppDetailPanel from '@/components/AppDetailPanel.vue';
import AppMasterDetailWorkspace from '@/components/AppMasterDetailWorkspace.vue';
import AppSelectorPanel from '@/components/AppSelectorPanel.vue';
import AppSelectorSummaryItem from '@/components/AppSelectorSummaryItem.vue';

type AssetTab = 'ca' | 'tls' | 'ssh';
type PageMode = 'certificates' | 'keys';

const props = withDefaults(defineProps<{ mode?: PageMode }>(), {
  mode: 'certificates',
});

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
const activeTab = ref<AssetTab>(props.mode === 'keys' ? 'ssh' : 'ca');
const busy = ref('');
const snackbar = ref(false);
const snackbarText = ref('');
const snackbarColor = ref('success');
const lastTaskId = ref('');
const lastExportTaskId = ref('');
const dialogFieldDefaults = {
  VTextField: { variant: 'outlined', density: 'comfortable' },
  VTextarea: { variant: 'outlined', density: 'comfortable' },
  VSelect: { variant: 'outlined', density: 'comfortable' },
  VFileInput: { variant: 'outlined', density: 'comfortable' },
  VSwitch: { density: 'comfortable' },
  VCheckbox: { density: 'comfortable' },
} as const;

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
const confirmActionKind = computed<'danger-primary' | 'warning-primary'>(() => (confirmState.color === 'warning' ? 'warning-primary' : 'danger-primary'));
let confirmAction: (() => Promise<void>) | null = null;

const caAssets = computed(() => items.value.filter((item) => item.type === 'ca_certificate'));
const tlsAssets = computed(() => items.value.filter((item) => item.type === 'tls_certificate'));
const sshAssets = computed(() => items.value.filter((item) => item.type === 'ssh_key_pair'));
type SelectorAsset =
  { key: string; source: 'user'; type: AssetTab; user: KeyAssetSummaryDto };
const selectorAssets = computed<SelectorAsset[]>(() => {
  if (props.mode === 'keys') {
    return sshAssets.value.map((user) => ({ key: `user:${user.id}`, source: 'user' as const, type: 'ssh' as const, user }));
  }
  return [
    ...caAssets.value.map((user) => ({ key: `user:${user.id}`, source: 'user' as const, type: 'ca' as const, user })),
    ...tlsAssets.value.map((user) => ({ key: `user:${user.id}`, source: 'user' as const, type: 'tls' as const, user })),
  ];
});
const selectedAssetKey = ref('');
const selectedAsset = computed(() => selectorAssets.value.find((item) => item.key === selectedAssetKey.value) ?? null);
const selectorTitle = computed(() => props.mode === 'keys' ? t('routes.keys.title') : t('routes.selfSignedCertificates.title'));
const selectableUserAssets = computed(() => selectorAssets.value);
const selectedAssetIds = computed(() => new Set(Object.values(selectedByTab).flat()));
const allSelectableSelected = computed(
  () => selectableUserAssets.value.length > 0 && selectableUserAssets.value.every((item) => selectedAssetIds.value.has(item.user.id)),
);
const someSelectableSelected = computed(
  () => selectableUserAssets.value.some((item) => selectedAssetIds.value.has(item.user.id)) && !allSelectableSelected.value,
);
const selectedIds = computed({
  get: () => selectedByTab[activeTab.value],
  set: (value: string[]) => {
    selectedByTab[activeTab.value] = Array.from(new Set(value));
  },
});
const selectedAssets = computed(() => items.value.filter((item) => selectedAssetIds.value.has(item.id)));
const caOptions = computed(() => caAssets.value.map((item) => ({ title: item.name, value: item.id })));
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

function toggleSelectionFor(type: AssetTab, assetId: string, checked: boolean | null) {
  activeTab.value = type;
  const current = selectedByTab[type];
  selectedByTab[type] = checked ? Array.from(new Set([...current, assetId])) : current.filter((id) => id !== assetId);
}

function toggleSelectAllAssets(checked: boolean | null) {
  if (!checked) {
    selectedByTab.ca = [];
    selectedByTab.tls = [];
    selectedByTab.ssh = [];
    return;
  }
  selectedByTab.ca = caAssets.value.map((item) => item.id);
  selectedByTab.tls = tlsAssets.value.map((item) => item.id);
  selectedByTab.ssh = sshAssets.value.map((item) => item.id);
}

function selectAsset(item: SelectorAsset) {
  selectedAssetKey.value = item.key;
  activeTab.value = item.type;
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
    const userAssets = await keyAssetsApi.list();
    items.value = userAssets;
    const availableKeys = new Set([
      ...userAssets.filter((item) => props.mode === 'keys' ? item.type === 'ssh_key_pair' : item.type !== 'ssh_key_pair').map((item) => `user:${item.id}`),
    ]);
    if (!availableKeys.has(selectedAssetKey.value)) selectedAssetKey.value = availableKeys.values().next().value ?? '';
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
    <AppMasterDetailWorkspace>
      <template #aside>
      <AppSelectorPanel
        :title="selectorTitle"
        :loading="loading"
        :empty="selectorAssets.length === 0"
        :empty-icon="props.mode === 'keys' ? 'mdi-key-outline' : 'mdi-certificate-outline'"
        :empty-text="t('keyAssetsPage.noAssets')"
      >
        <template #leading>
          <v-checkbox-btn
            :model-value="allSelectableSelected"
            :indeterminate="someSelectableSelected"
            :aria-label="t('keyAssetsPage.selectionSummary', { count: selectedAssets.length })"
            @update:model-value="toggleSelectAllAssets"
          />
        </template>
        <template #subtitle>
          <div class="asset-selector-summary">
            <span class="text-caption text-medium-emphasis">{{ t('keyAssetsPage.selectionSummary', { count: selectedAssets.length }) }}</span>
          </div>
        </template>
        <template #actions>
          <AppActionButton kind="tool" icon="mdi-plus" :label="activeTab === 'ca' ? t('keyAssetsPage.createCa') : activeTab === 'tls' ? t('keyAssetsPage.createTls') : t('keyAssetsPage.generateSsh')" @click="openEditor(activeTab, false)" />
          <v-menu location="bottom end">
            <template #activator="{ props: menuProps }">
              <AppActionButton v-bind="menuProps" kind="tool" icon="mdi-dots-vertical" :label="t('common.more')" />
            </template>
            <v-list density="compact">
              <v-list-item prepend-icon="mdi-import" :title="t('keyAssetsPage.importAsset')" @click="openEditor(activeTab, true)" />
              <v-list-item prepend-icon="mdi-archive-arrow-up-outline" :title="t('keyAssetsPage.importArchive')" @click="openArchiveDialog" />
              <v-list-item prepend-icon="mdi-archive-arrow-down-outline" :title="t('keyAssetsPage.exportSelected')" :disabled="selectedAssets.length === 0" @click="openExportDialog" />
            </v-list>
          </v-menu>
        </template>
        <AppSelectorSummaryItem
          v-for="item in selectorAssets"
          :key="item.key"
          as="div"
          :selected="item.key === selectedAssetKey"
          :title="item.user.name"
          :subtitle="item.user.commonName || item.user.fingerprint"
          :status="false"
          @select="selectAsset(item)"
        >
          <template #leading>
            <v-checkbox-btn
              class="asset-selector-checkbox"
              :model-value="selectedByTab[item.type].includes(item.user.id)"
              @click.stop
              @update:model-value="toggleSelectionFor(item.type, item.user.id, $event)"
            />
          </template>
          <v-chip size="x-small" variant="tonal">{{ assetTypeLabel(item.user.type) }}</v-chip>
        </AppSelectorSummaryItem>
      </AppSelectorPanel>
      </template>

      <AppDetailPanel
        class="asset-detail-card"
        :loading="loading && !selectedAsset"
        :empty="!selectedAsset"
        :empty-text="t('keyAssetsPage.noAssets')"
      >
        <template v-if="selectedAsset" #header>
            <div class="min-width-0">
              <div class="text-h6 font-weight-bold text-truncate">{{ selectedAsset.user.name }}</div>
              <div class="text-body-2 text-medium-emphasis">{{ assetTypeLabel(selectedAsset.user.type) }}</div>
            </div>
            <AppActionGroup context="detail" class="app-detail-actions">
              <v-menu>
                <template #activator="{ props: menuProps }"><AppActionButton v-bind="menuProps" icon="mdi-download" :label="t('common.download')" /></template>
                <v-list density="compact"><v-list-item v-for="kind in selectedAsset.user.downloadKinds" :key="kind" :title="fileKindLabel(kind)" @click="downloadAssetFile(selectedAsset.user, kind)" /></v-list>
              </v-menu>
              <AppActionButton v-if="selectedAsset.user.type === 'tls_certificate'" icon="mdi-autorenew" :label="t('keyAssetsPage.reissue')" :disabled="!selectedAsset.user.canReissue" @click="triggerReissue(selectedAsset.user)" />
              <AppActionButton v-if="selectedAsset.user.type === 'ssh_key_pair'" icon="mdi-reload" :label="t('keyAssetsPage.regenerate')" :disabled="!selectedAsset.user.canRegenerate" @click="triggerRegenerate(selectedAsset.user)" />
              <AppActionButton kind="danger" icon="mdi-delete" :label="t('common.delete')" @click="triggerDelete(selectedAsset.user)" />
            </AppActionGroup>
        </template>
        <template v-if="selectedAsset" #body>
            <div class="asset-detail-grid">
              <div><span>{{ t('keyAssetsPage.commonName') }}</span><strong>{{ selectedAsset.user.commonName || t('common.notAvailable') }}</strong></div>
              <div><span>{{ t('common.fingerprint') }}</span><strong class="mono">{{ selectedAsset.user.fingerprint }}</strong></div>
              <div><span>{{ t('common.algorithm') }}</span><strong>{{ selectedAsset.user.algorithm || t('common.notAvailable') }}</strong></div>
              <div><span>{{ t('common.validUntil') }}</span><strong>{{ formatOptionalDate(selectedAsset.user.notAfter) }}</strong></div>
              <div><span>{{ t('keyAssetsPage.references') }}</span><strong>{{ referenceText(selectedAsset.user) }}</strong></div>
              <div v-if="selectedAsset.user.type === 'tls_certificate'"><span>{{ t('keyAssetsPage.dnsNames') }}</span><strong>{{ selectedAsset.user.dnsNames.join(', ') || t('common.notAvailable') }}</strong></div>
            </div>
        </template>
      </AppDetailPanel>
    </AppMasterDetailWorkspace>

    <v-dialog v-model="editorDialog" max-width="760">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ editorTitle }}</span>
          <AppActionButton kind="tool" icon="mdi-close" :label="t('common.close')" @click="editorDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          <v-defaults-provider :defaults="dialogFieldDefaults">
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
              <v-alert type="info" variant="tonal" density="compact" class="mb-3">{{ t('keyAssetsPage.standaloneTlsHint') }}</v-alert>
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
              <v-alert type="warning" variant="tonal" density="compact" class="mb-3">{{ t('keyAssetsPage.sshImportHint') }}</v-alert>
              <v-textarea v-model="sshForm.privateKeyPem" :label="t('keyAssetsPage.privateKeyPem')" rows="6" />
              <v-textarea v-model="sshForm.publicKey" :label="t('keyAssetsPage.publicKeyPem')" :hint="t('keyAssetsPage.publicKeyOptionalHint')" persistent-hint rows="3" />
            </template>
          </template>
          </v-defaults-provider>
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <AppActionGroup context="dialog">
            <AppActionButton kind="plain" :label="t('common.cancel')" @click="editorDialog = false" />
            <AppActionButton kind="primary" :label="editorImportMode ? t('common.import') : t('common.generate')" :loading="busy === 'editor'" @click="submitEditor" />
          </AppActionGroup>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="exportDialog" max-width="640">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('keyAssetsPage.exportTitle') }}</span>
          <AppActionButton kind="tool" icon="mdi-close" :label="t('common.close')" @click="exportDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          <v-defaults-provider :defaults="dialogFieldDefaults">
          <v-alert type="info" variant="tonal" density="compact" class="mb-4">{{ t('keyAssetsPage.exportHint') }}</v-alert>
          <div class="text-subtitle-2 mb-2">{{ t('keyAssetsPage.exportIncludes') }}</div>
          <v-list density="compact" lines="two" class="export-list">
            <v-list-item v-for="asset in exportAssets" :key="asset.id" :title="asset.name" :subtitle="assetTypeLabel(asset.type)" />
          </v-list>
          <v-text-field v-model="exportForm.password" type="password" :label="t('common.password')" class="mt-3" />
          <v-text-field v-model="exportForm.confirmPassword" type="password" :label="t('common.confirmPassword')" />
          </v-defaults-provider>
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <AppActionGroup context="dialog">
            <AppActionButton kind="plain" :label="t('common.cancel')" @click="exportDialog = false" />
            <AppActionButton kind="primary" :label="t('common.export')" :loading="busy === 'export'" @click="submitExport" />
          </AppActionGroup>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="archiveDialog" max-width="980">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('keyAssetsPage.archiveImportTitle') }}</span>
          <AppActionButton kind="tool" icon="mdi-close" :label="t('common.close')" @click="archiveDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body archive-dialog">
          <v-defaults-provider :defaults="dialogFieldDefaults">
          <div class="archive-inputs">
            <v-file-input
              v-model="archiveFile"
              :label="t('keyAssetsPage.archiveFile')"
              accept=".panel-key-assets"
              show-size
              prepend-icon="mdi-paperclip"
            />
            <v-text-field v-model="archivePassword" type="password" :label="t('keyAssetsPage.archivePassword')" />
            <AppActionButton icon="mdi-shield-search" :label="t('keyAssetsPage.preflight')" :loading="busy === 'preflight'" :disabled="!archiveFile" @click="runArchivePreflight" />
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
          </v-defaults-provider>
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <AppActionGroup context="dialog">
            <AppActionButton kind="plain" :label="t('common.cancel')" @click="archiveDialog = false" />
            <AppActionButton
              kind="primary"
              :label="t('keyAssetsPage.executeImport')"
              :disabled="!archivePreflight"
              :loading="busy === 'execute-import'"
              @click="executeArchiveImport"
            />
          </AppActionGroup>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="confirmDialog" max-width="560">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ confirmState.title }}</span>
          <AppActionButton kind="tool" icon="mdi-close" :label="t('common.close')" @click="confirmDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          <v-defaults-provider :defaults="dialogFieldDefaults">
          <p class="confirm-message">{{ confirmState.message }}</p>
          <v-checkbox
            v-if="confirmState.acknowledgeRequired"
            v-model="confirmState.acknowledged"
            color="error"
            :label="t('keyAssetsPage.dangerAcknowledge')"
            class="mt-4"
          />
          </v-defaults-provider>
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <AppActionGroup context="dialog">
            <AppActionButton kind="plain" :label="t('common.cancel')" @click="confirmDialog = false" />
            <AppActionButton
              :kind="confirmActionKind"
              :label="confirmState.confirmLabel"
              :disabled="confirmState.acknowledgeRequired && !confirmState.acknowledged"
              :loading="busy === 'confirm'"
              @click="runConfirmAction"
            />
          </AppActionGroup>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar" :color="snackbarColor" timeout="5000">
      {{ snackbarText }}
      <template #actions>
        <AppActionGroup context="snackbar">
        <AppActionButton v-if="lastTaskId" kind="snackbar" :label="t('common.viewTask')" :to="taskRoute()" />
        <AppActionButton
          v-if="lastExportTaskId"
          kind="snackbar"
          :label="t('keyAssetsPage.downloadArchive')"
          :loading="busy === 'download-export'"
          @click="downloadExportArchive"
        />
        <AppActionButton kind="snackbar" :label="t('common.close')" @click="snackbar = false" />
        </AppActionGroup>
      </template>
    </v-snackbar>
  </div>
</template>

<style scoped>
.asset-detail-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.asset-detail-grid > div {
  display: grid;
  gap: 4px;
  min-width: 0;
  padding: 12px;
  border: 1px solid var(--lp-border);
  border-radius: 8px;
}

.asset-detail-grid span {
  color: var(--lp-text-muted);
  font-size: 0.76rem;
}

.asset-detail-grid strong { overflow-wrap: anywhere; font-size: 0.88rem; }
.asset-selector-summary { min-height: 20px; }
.asset-selector-checkbox { flex: 0 0 auto; }

.table-pane {
  min-height: 0;
  overflow: auto;
}

.checkbox-col {
  width: 44px;
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

.confirm-message {
  margin: 0;
  color: var(--lp-text);
  line-height: 1.55;
  white-space: pre-wrap;
}

@media (max-width: 1080px) {
  .archive-inputs,
  .summary-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 720px) {
  .asset-detail-grid { grid-template-columns: 1fr; }
  .table-pane {
    overflow-x: auto;
    overflow-y: visible;
  }
}
</style>
