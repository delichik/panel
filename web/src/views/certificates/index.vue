<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Download, FileArchive, KeyRound, Plus, RefreshCcw, RotateCcw, Trash2 } from '@lucide/vue';
import { certificatesApi } from '@/api/certificates';
import { dnsApi } from '@/api/dns';
import { keyAssetsApi } from '@/api/keyAssets';
import { saveBlobDownload } from '@/api/download';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import CodeEditor from '@/components/ui/CodeEditor.vue';
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue';
import Dialog from '@/components/ui/Dialog.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import FileUploadButton from '@/components/ui/FileUploadButton.vue';
import Input from '@/components/ui/Input.vue';
import PaginationBar from '@/components/ui/PaginationBar.vue';
import SearchInput from '@/components/ui/SearchInput.vue';
import Select from '@/components/ui/Select.vue';
import Skeleton from '@/components/ui/Skeleton.vue';
import Switch from '@/components/ui/Switch.vue';
import Tabs from '@/components/ui/Tabs.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import MasterDetailLayout from '@/components/templates/MasterDetailLayout.vue';
import { useI18n } from '@/i18n';
import type { DomainCertificateDto, SelfSignedCertificateDto } from '@/types/certificates';
import type { DnsDomainDto } from '@/types/dns';
import type { ImportPreflightDto, KeyAssetDto, KeyAssetType } from '@/types/keyAssets';
import { assetImportHasCertificate, initialAssetImportMaterialTab, type AssetImportMaterialTab } from './assetImportEditor';
import { assetTone, certificateState, certificateTone, selfSignedTone } from './model';
import { createLatestRequestGuard } from '@/views/_shared/requestState';
import { formatDateTime } from '@/utils/datetime';

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const notifyError = useErrorToast();
const notifySuccess = useSuccessToast();

const domains = ref<DnsDomainDto[]>([]);
const certs = ref<DomainCertificateDto[]>([]);
const selfSigned = ref<SelfSignedCertificateDto[]>([]);
const assets = ref<KeyAssetDto[]>([]);
const selectedId = ref(String(route.query.selected ?? ''));
const loading = ref(false);
const listRequests = createLatestRequestGuard();
const error = ref('');
const assetActionError = ref('');
const dialog = ref<'issue' | 'self-ca' | 'self-leaf' | 'asset-ca' | 'asset-tls' | 'asset-ssh' | 'asset-import' | 'asset-export' | 'asset-preflight' | ''>('');
const confirmDelete = ref(false);
const saving = ref(false);
const importPlan = ref<ImportPreflightDto | null>(null);
const editingCertificateId = ref('');
const page = ref(parsePageQuery(route.query.page));
const pageSize = 50;
const total = ref(0);
const search = ref(String(route.query.search ?? ''));
let searchTimer: ReturnType<typeof setTimeout> | null = null;
const importConfirmOpen = ref(false);

function parsePageQuery(value: unknown) {
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed > 0 ? parsed : 1;
}

const mode = computed(() => route.path.includes('/self-signed') ? 'self' : route.path.includes('/keys') ? 'keys' : 'domains');
const title = computed(() => mode.value === 'self' ? t('routes.selfSigned.title') : mode.value === 'keys' ? t('routes.keys.title') : t('routes.certificates.title'));
const description = computed(() => mode.value === 'self' ? t('routes.selfSigned.description') : mode.value === 'keys' ? t('routes.keys.description') : t('routes.certificates.description'));
const selectedCert = computed(() => certs.value.find((item) => item.id === selectedId.value) ?? null);
const selectedSelf = computed(() => selfSigned.value.find((item) => item.id === selectedId.value) ?? null);
const userAssets = computed(() => assets.value.filter((item) => !isSystemManagedAsset(item)));
const selectedAsset = computed(() => userAssets.value.find((item) => item.id === selectedId.value) ?? null);
const caAssets = computed(() => userAssets.value.filter((item) => item.type === 'ca_certificate').map((item) => ({ label: item.name, value: item.id })));
const selfCas = computed(() => selfSigned.value.filter((item) => item.kind === 'ca').map((item) => ({ label: item.name, value: item.id })));
const domainOptions = computed(() => domains.value.map((item) => ({ label: item.name, value: item.id })));
const issueForm = reactive({ name: '', domainId: '', includeRoot: true, includeWildcard: false, subdomains: [''] });
const selfForm = reactive({ name: '', caId: '', commonName: '', dnsNames: [''], ipAddresses: [''], years: '5', days: '90' });
const assetForm = reactive({ type: 'ssh_key_pair' as KeyAssetType, name: '', parentAssetId: '', commonName: '', dnsNames: [''], ipAddresses: [''], algorithm: 'ed25519', keySize: '2048', validityDays: '365', comment: '', certificatePem: '', privateKeyPem: '', publicKey: '', password: '' });
const assetImportMaterialTab = ref<AssetImportMaterialTab>(initialAssetImportMaterialTab());
const importFile = ref<File | null>(null);
const selectedIssueDomainName = computed(() => domains.value.find((item) => item.id === issueForm.domainId)?.name ?? '');
const issueCoverage = computed(() => issuePrefixes().map((prefix) => prefixToDomain(prefix, selectedIssueDomainName.value)).filter(Boolean));

const algorithmOptions = [{ label: 'ed25519', value: 'ed25519' }, { label: 'rsa', value: 'rsa' }];
const assetTypeOptions = [
  { label: t('certificatesPage.assetCa'), value: 'ca_certificate' },
  { label: t('certificatesPage.assetTls'), value: 'tls_certificate' },
  { label: t('certificatesPage.assetSsh'), value: 'ssh_key_pair' },
];
const assetImportMaterialTabs = computed(() => [
  { label: t('certificatesPage.privateKeyPem'), value: 'privateKey' },
  { label: t('certificatesPage.certificatePem'), value: 'certificate' },
]);

onMounted(load);
watch(() => route.path, () => { void load(); });

watch(search, (value) => {
  void router.replace({ query: { ...route.query, search: value || undefined, page: undefined } });
  if (searchTimer) clearTimeout(searchTimer);
  searchTimer = setTimeout(() => {
    if (page.value !== 1) page.value = 1;
    void load();
  }, 250);
});

watch(selectedId, (value) => {
  void router.replace({ query: { ...route.query, selected: value || undefined } });
});

async function load() {
  const requestId = listRequests.begin();
  loading.value = true;
  error.value = '';
  try {
    const q = search.value.trim() || undefined;
    const [domainsResult, certsResult, selfResult, assetsResult] = await Promise.allSettled([
      dnsApi.listDomains(),
      certificatesApi.list({ page: page.value, pageSize, q }),
      certificatesApi.listSelfSignedPage({ page: page.value, pageSize, q }),
      keyAssetsApi.listPage({ page: page.value, pageSize, q }),
    ]);
    if (!listRequests.isCurrent(requestId)) return;
    let firstError = '';
    if (domainsResult.status === 'fulfilled') {
      domains.value = Array.isArray(domainsResult.value) ? domainsResult.value : [];
    } else {
      firstError = domainsResult.reason instanceof Error ? domainsResult.reason.message : t('certificatesPage.loadFailed');
    }
    if (certsResult.status === 'fulfilled') {
      certs.value = certsResult.value.items.map(normalizeDomainCertificate);
    } else {
      if (!firstError) firstError = certsResult.reason instanceof Error ? certsResult.reason.message : t('certificatesPage.loadFailed');
    }
    if (selfResult.status === 'fulfilled') {
      selfSigned.value = selfResult.value.items.map(normalizeSelfSignedCertificate);
    } else {
      if (!firstError) firstError = selfResult.reason instanceof Error ? selfResult.reason.message : t('certificatesPage.loadFailed');
    }
    if (assetsResult.status === 'fulfilled') {
      assets.value = assetsResult.value.items.map(normalizeKeyAsset);
    } else {
      if (!firstError) firstError = assetsResult.reason instanceof Error ? assetsResult.reason.message : t('certificatesPage.loadFailed');
    }
    const nextTotal = mode.value === 'self'
      ? (selfResult.status === 'fulfilled' ? selfResult.value.total : total.value)
      : mode.value === 'keys'
        ? (assetsResult.status === 'fulfilled' ? assetsResult.value.total : total.value)
        : (certsResult.status === 'fulfilled' ? certsResult.value.total : total.value);
    total.value = nextTotal;
    const visibleAssets = assets.value.filter((item) => !isSystemManagedAsset(item));
    const list = mode.value === 'self' ? selfSigned.value : mode.value === 'keys' ? visibleAssets : certs.value;
    selectedId.value = list.some((item) => item.id === selectedId.value) ? selectedId.value : list[0]?.id ?? '';
    if (firstError) {
      error.value = firstError;
      notifyError(firstError);
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('certificatesPage.loadFailed');
    notifyError(err instanceof Error ? err.message : t('certificatesPage.loadFailed'));
  } finally {
    if (listRequests.isCurrent(requestId)) loading.value = false;
  }
}

function switchMode(path: string) {
  listRequests.invalidate();
  selectedId.value = '';
  page.value = 1;
  void router.replace({ query: { ...route.query, page: undefined, selected: undefined } });
  if (route.path === path) void load();
  else void router.push(path);
}

function changePage(value: number) {
  page.value = value;
  void router.replace({ query: { ...route.query, page: value > 1 ? String(value) : undefined } });
  void load();
}

function openIssue() {
  resetIssueForm();
  dialog.value = 'issue';
}

function openReissue(cert: DomainCertificateDto) {
  resetIssueForm(cert);
  dialog.value = 'issue';
}

async function issueCertificate() {
  saving.value = true;
  try {
    const prefixes = issuePrefixes();
    const input = { name: issueForm.name, domainId: issueForm.domainId, prefixes: prefixes.length ? prefixes : ['@'] };
    const result = editingCertificateId.value ? await certificatesApi.reissue(editingCertificateId.value, input) : await certificatesApi.issue(input);
    notifySuccess(result.taskId ? t('certificatesPage.issueTask', { taskId: result.taskId }) : t('certificatesPage.issued'));
    selectedId.value = result.certificate.id;
    editingCertificateId.value = '';
    dialog.value = '';
    await load();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.operationFailed');
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'));
  } finally {
    saving.value = false;
  }
}

async function renewCertificate(cert: DomainCertificateDto) {
  await run(async () => {
    await certificatesApi.renew(cert.id);
    notifySuccess(t('certificatesPage.renewAccepted'));
    await load();
  });
}

function openSelf(kind: 'self-ca' | 'self-leaf') {
  Object.assign(selfForm, { name: '', caId: selfCas.value[0]?.value ?? '', commonName: '', dnsNames: [''], ipAddresses: [''], years: '5', days: '90' });
  dialog.value = kind;
}

async function saveSelf() {
  saving.value = true;
  try {
    const saved = dialog.value === 'self-ca'
      ? await certificatesApi.createSelfSignedCa({ name: selfForm.name, commonName: selfForm.commonName, years: Number(selfForm.years) || 5 })
      : await certificatesApi.createSelfSignedLeaf({ name: selfForm.name, caId: selfForm.caId, commonName: selfForm.commonName, dnsNames: cleanEntries(selfForm.dnsNames), ipAddresses: cleanEntries(selfForm.ipAddresses), days: Number(selfForm.days) || 90 });
    selectedId.value = saved.id;
    notifySuccess(t('certificatesPage.selfSaved'));
    dialog.value = '';
    await load();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.operationFailed');
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'));
  } finally {
    saving.value = false;
  }
}

async function renewSelf(cert: SelfSignedCertificateDto) {
  await run(async () => {
    const renewed = await certificatesApi.renewSelfSigned(cert.id);
    selectedId.value = renewed.id;
    notifySuccess(t('certificatesPage.selfRenewed'));
    await load();
  });
}

function openAsset(next: typeof dialog.value) {
  Object.assign(assetForm, { type: 'ssh_key_pair', name: '', parentAssetId: caAssets.value[0]?.value ?? '', commonName: '', dnsNames: [''], ipAddresses: [''], algorithm: 'ed25519', keySize: '2048', validityDays: '365', comment: '', certificatePem: '', privateKeyPem: '', publicKey: '', password: '' });
  importPlan.value = null;
  importFile.value = null;
  assetActionError.value = '';
  assetImportMaterialTab.value = initialAssetImportMaterialTab();
  dialog.value = next;
}

async function saveAsset() {
  const assetDialog = dialog.value;
  if (assetDialog === 'asset-import') assetActionError.value = '';
  saving.value = true;
  try {
    let result;
    if (dialog.value === 'asset-ca') result = await keyAssetsApi.createCa({ name: assetForm.name, commonName: assetForm.commonName, algorithm: assetForm.algorithm as 'ed25519' | 'rsa', keySize: Number(assetForm.keySize) || 0, years: 0, validityDays: Number(assetForm.validityDays) || 365 });
    if (dialog.value === 'asset-tls') result = await keyAssetsApi.createTls({ name: assetForm.name, parentAssetId: assetForm.parentAssetId, caId: assetForm.parentAssetId, commonName: assetForm.commonName, algorithm: assetForm.algorithm as 'ed25519' | 'rsa', keySize: Number(assetForm.keySize) || 0, dnsNames: cleanEntries(assetForm.dnsNames), ipAddresses: cleanEntries(assetForm.ipAddresses), days: 0, validityDays: Number(assetForm.validityDays) || 365 });
    if (dialog.value === 'asset-ssh') result = await keyAssetsApi.generateSsh({ name: assetForm.name, algorithm: assetForm.algorithm as 'ed25519' | 'rsa', keySize: Number(assetForm.keySize) || 0, comment: assetForm.comment });
    if (dialog.value === 'asset-import') result = await keyAssetsApi.importOne({ type: assetForm.type, name: assetForm.name, parentAssetId: assetForm.parentAssetId, commonName: assetForm.commonName, algorithm: assetForm.algorithm, keySize: Number(assetForm.keySize) || 0, certificatePem: assetForm.certificatePem, privateKeyPem: assetForm.privateKeyPem, publicKey: assetForm.publicKey });
    selectedId.value = result?.asset?.id ?? selectedId.value;
    notifySuccess(t('certificatesPage.assetSaved'));
    dialog.value = '';
    await load();
  } catch (err) {
    const message = err instanceof Error ? err.message : t('common.operationFailed');
    notifyError(message);
    if (assetDialog === 'asset-import') assetActionError.value = message;
    else error.value = message;
  } finally {
    saving.value = false;
  }
}

async function reissueAsset(asset: KeyAssetDto) {
  await run(async () => {
    const result = asset.type === 'ssh_key_pair' ? await keyAssetsApi.regenerate(asset.id) : await keyAssetsApi.reissue(asset.id);
    notifySuccess(result.taskId ? t('certificatesPage.assetTask', { taskId: result.taskId }) : t('certificatesPage.assetSaved'));
    await load();
  });
}

async function createExport() {
  await run(async () => {
    const result = await keyAssetsApi.createExport({ assetIds: selectedAsset.value ? [selectedAsset.value.id] : userAssets.value.map((item) => item.id), password: assetForm.password });
    notifySuccess(t('certificatesPage.exportTask', { taskId: result.taskId }));
    saveBlobDownload(await keyAssetsApi.downloadExport(result.taskId));
    dialog.value = '';
  });
}

async function downloadAssetFile(asset: KeyAssetDto, kind: string) {
  await run(async () => {
    saveBlobDownload(await keyAssetsApi.downloadFile(asset.id, kind));
  });
}

async function preflightImport() {
  if (!importFile.value) return;
  await run(async () => {
    importPlan.value = await keyAssetsApi.preflightImport(importFile.value as File, assetForm.password);
  });
}

async function executeImport(confirmDanger = false) {
  if (!importPlan.value) return;
  await run(async () => {
    const result = await keyAssetsApi.executeImport(importPlan.value!.planId, { strategy: 'overwrite', confirmOverwriteInUse: confirmDanger, confirmDangerousOverwrite: confirmDanger, resolutions: [] });
    notifySuccess(t('certificatesPage.importTask', { taskId: result.taskId }));
    dialog.value = '';
    importConfirmOpen.value = false;
    await load();
  });
}

async function deleteSelected() {
  await run(async () => {
    if (mode.value === 'domains' && selectedCert.value) await certificatesApi.delete(selectedCert.value.id);
    if (mode.value === 'self' && selectedSelf.value) await certificatesApi.deleteSelfSigned(selectedSelf.value.id);
    if (mode.value === 'keys' && selectedAsset.value) await keyAssetsApi.delete(selectedAsset.value.id);
    notifySuccess(t('certificatesPage.deleted'));
    selectedId.value = '';
    confirmDelete.value = false;
    await load();
  });
}

async function run(action: () => Promise<void>) {
  saving.value = true;
  error.value = '';
  try {
    await action();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.operationFailed');
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'));
  } finally {
    saving.value = false;
  }
}

function resetIssueForm(cert?: DomainCertificateDto) {
  const prefixes = cert ? prefixesFromCertificate(cert) : ['@'];
  Object.assign(issueForm, {
    name: cert?.name ?? '',
    domainId: cert?.domainId || domains.value[0]?.id || '',
    includeRoot: prefixes.includes('@'),
    includeWildcard: prefixes.includes('*'),
    subdomains: prefixes.filter((prefix) => prefix !== '@' && prefix !== '*'),
  });
  if (!issueForm.subdomains.length) issueForm.subdomains = [''];
  editingCertificateId.value = cert?.id ?? '';
}

function normalizeDomainCertificate(cert: DomainCertificateDto): DomainCertificateDto {
  return { ...cert, domains: safeStringArray((cert as { domains?: unknown }).domains) };
}

function normalizeSelfSignedCertificate(cert: SelfSignedCertificateDto): SelfSignedCertificateDto {
  return {
    ...cert,
    dnsNames: safeStringArray((cert as { dnsNames?: unknown }).dnsNames),
    ipAddresses: safeStringArray((cert as { ipAddresses?: unknown }).ipAddresses),
  };
}

function normalizeKeyAsset(asset: KeyAssetDto): KeyAssetDto {
  return {
    ...asset,
    dnsNames: safeStringArray((asset as { dnsNames?: unknown }).dnsNames),
    ipAddresses: safeStringArray((asset as { ipAddresses?: unknown }).ipAddresses),
    downloadKinds: safeStringArray((asset as { downloadKinds?: unknown }).downloadKinds),
    references: Array.isArray(asset.references) ? asset.references : [],
    childCount: Number(asset.childCount) || 0,
    referenceCount: Number(asset.referenceCount) || 0,
  };
}

function safeStringArray(value: unknown) {
  return Array.isArray(value) ? value.map((item) => String(item).trim()).filter(Boolean) : [];
}

function isSystemManagedAsset(asset: KeyAssetDto) {
  const metadata = asset.metadata ?? {};
  return metadata.systemManaged === true || metadata.systemManaged === 'true' || metadata.systemScope === 'agent_tls';
}

function issuePrefixes() {
  const prefixes: string[] = [];
  if (issueForm.includeRoot) prefixes.push('@');
  if (issueForm.includeWildcard) prefixes.push('*');
  for (const value of cleanEntries(issueForm.subdomains)) {
    const prefix = prefixFromInput(value);
    if (prefix) prefixes.push(prefix);
  }
  return uniqueStrings(prefixes.length ? prefixes : ['@']);
}

function prefixesFromCertificate(cert: DomainCertificateDto) {
  const stored = safeStringArray(String(cert.prefix || '').split(','));
  if (stored.length) return stored;
  return safeStringArray(cert.domains.map((domain) => prefixFromCoveredDomain(domain, cert.domain)));
}

function prefixFromInput(raw: string) {
  const value = raw.trim().toLowerCase().replace(/^\.+|\.+$/g, '');
  const domain = selectedIssueDomainName.value.toLowerCase();
  if (!value) return '';
  if (domain && value === domain) return '@';
  if (domain && value.endsWith(`.${domain}`)) return value.slice(0, -(domain.length + 1));
  return value;
}

function prefixFromCoveredDomain(raw: string, managedDomain: string) {
  const value = raw.trim().toLowerCase();
  const domain = managedDomain.trim().toLowerCase();
  if (!value || !domain) return '';
  if (value === domain) return '@';
  if (value === `*.${domain}`) return '*';
  if (value.endsWith(`.${domain}`)) return value.slice(0, -(domain.length + 1));
  return value;
}

function prefixToDomain(prefix: string, domain: string) {
  if (!domain) return prefix;
  if (prefix === '@') return domain;
  if (prefix === '*') return `*.${domain}`;
  return `${prefix}.${domain}`;
}

function cleanEntries(entries: string[]) {
  return entries.flatMap((entry) => entry.split(/[\n,]+/)).map((entry) => entry.trim()).filter(Boolean);
}

function uniqueStrings(values: string[]) {
  return values.filter((value, index) => values.indexOf(value) === index);
}

function displayEntries(entries: string[]) {
  const next = cleanEntries(entries);
  return next.length ? next.join(', ') : t('common.notAvailable');
}

function selfSignedKindLabel(kind: string) {
  return kind === 'ca' ? t('certificatesPage.kindCa') : t('certificatesPage.kindLeaf');
}

function assetTypeLabel(type: string) {
  switch (type) {
    case 'ca_certificate': return t('certificatesPage.assetCa');
    case 'tls_certificate': return t('certificatesPage.assetTls');
    case 'ssh_key_pair': return t('certificatesPage.assetSsh');
    default: return type;
  }
}

function fileKindLabel(kind: string) {
  switch (kind) {
    case 'certificate': return t('certificatesPage.fileCertificate');
    case 'private_key': return t('certificatesPage.filePrivateKey');
    case 'public_key': return t('certificatesPage.filePublicKey');
    case 'ssh_public_key': return t('certificatesPage.fileSshPublicKey');
    default: return kind;
  }
}

function addEntry(entries: string[]) {
  entries.push('');
}

function removeEntry(entries: string[], index: number) {
  entries.splice(index, 1);
  if (!entries.length) entries.push('');
}

function openBatchImport() {
  Object.assign(assetForm, { password: '' });
  importPlan.value = null;
  importFile.value = null;
  assetActionError.value = '';
  dialog.value = 'asset-preflight';
}

function onFileChange(value: File | File[]) {
  importFile.value = Array.isArray(value) ? value[0] ?? null : value;
  importPlan.value = null;
}
</script>

<template>
  <ConsolePage :title="title" :description="description">
    <template #actions>
      <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
      <Button v-if="mode === 'domains'" size="sm" variant="primary" @click="openIssue"><Plus />{{ t('certificatesPage.issue') }}</Button>
      <Button v-else-if="mode === 'self'" size="sm" variant="primary" @click="openSelf('self-ca')"><Plus />{{ t('certificatesPage.generateCa') }}</Button>
      <Button v-if="mode === 'keys'" size="sm" @click="openBatchImport"><FileArchive />{{ t('certificatesPage.batchImport') }}</Button>
      <Button v-else size="sm" variant="primary" @click="openAsset('asset-ssh')"><KeyRound />{{ t('certificatesPage.generateSsh') }}</Button>
    </template>

    <div class="grid h-full min-h-[640px] grid-rows-[auto_minmax(0,1fr)] gap-4">
      <nav class="flex flex-wrap gap-2 rounded-2xl border border-border bg-card p-2">
        <Button :variant="mode === 'domains' ? 'primary' : 'ghost'" @click="switchMode('/certificates/domains')">{{ t('routes.certificates.title') }}</Button>
        <Button :variant="mode === 'self' ? 'primary' : 'ghost'" @click="switchMode('/certificates/self-signed')">{{ t('routes.selfSigned.title') }}</Button>
        <Button :variant="mode === 'keys' ? 'primary' : 'ghost'" @click="switchMode('/certificates/keys')">{{ t('routes.keys.title') }}</Button>
      </nav>

      <MasterDetailLayout class="min-h-0">
        <template #master>
        <aside class="grid min-h-0 min-w-0 grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden rounded-2xl border border-border bg-card">
          <div class="border-b border-border p-3">
            <SearchInput v-model="search" clearable :label="t('common.search')" :placeholder="t('certificatesPage.searchPlaceholder')" :clear-label="t('common.clearSearch')" />
          </div>
          <div class="motion-stagger min-h-0 overflow-auto p-2">
          <div v-if="loading && ((mode === 'domains' && !certs.length) || (mode === 'self' && !selfSigned.length) || (mode === 'keys' && !userAssets.length))" class="grid gap-2">
            <Skeleton v-for="item in 6" :key="item" class="h-16" />
          </div>
          <template v-else>
            <EmptyState v-if="error && ((mode === 'domains' && !certs.length) || (mode === 'self' && !selfSigned.length) || (mode === 'keys' && !userAssets.length))" :title="t('common.loadFailed')" :description="error">
              <template #actions>
                <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button>
              </template>
            </EmptyState>
            <EmptyState v-if="!error && mode === 'domains' && !certs.length" :title="t('certificatesPage.noCertificates')" :description="t('certificatesPage.noCertificatesHint')" />
            <button v-for="cert in mode === 'domains' ? certs : []" :key="cert.id" type="button" class="motion-list-item mb-2 grid w-full gap-2 rounded-xl border p-3 text-left hover:bg-accent" :class="selectedId === cert.id ? 'border-border-strong bg-background' : 'border-transparent'" :aria-current="selectedId === cert.id ? 'true' : undefined" @click="selectedId = cert.id">
              <div class="flex items-center justify-between gap-2"><strong class="truncate text-sm">{{ cert.name }}</strong><Badge :tone="certificateTone(cert)">{{ t(certificateState(cert)) }}</Badge></div>
              <span class="truncate text-xs text-muted-foreground">{{ cert.domains.join(', ') }}</span>
            </button>

            <EmptyState v-if="!error && mode === 'self' && !selfSigned.length" :title="t('certificatesPage.noSelf')" :description="t('certificatesPage.noSelfHint')" />
            <button v-for="cert in mode === 'self' ? selfSigned : []" :key="cert.id" type="button" class="motion-list-item mb-2 grid w-full gap-2 rounded-xl border p-3 text-left hover:bg-accent" :class="selectedId === cert.id ? 'border-border-strong bg-background' : 'border-transparent'" :aria-current="selectedId === cert.id ? 'true' : undefined" @click="selectedId = cert.id">
              <div class="flex items-center justify-between gap-2"><strong class="truncate text-sm">{{ cert.name }}</strong><Badge :tone="selfSignedTone(cert)">{{ selfSignedKindLabel(cert.kind) }}</Badge></div>
              <span class="truncate text-xs text-muted-foreground">{{ cert.commonName }}</span>
            </button>

            <EmptyState v-if="!error && mode === 'keys' && !userAssets.length" :title="t('certificatesPage.noAssets')" :description="t('certificatesPage.noAssetsHint')" />
            <button v-for="asset in mode === 'keys' ? userAssets : []" :key="asset.id" type="button" class="motion-list-item mb-2 grid w-full gap-2 rounded-xl border p-3 text-left hover:bg-accent" :class="selectedId === asset.id ? 'border-border-strong bg-background' : 'border-transparent'" :aria-current="selectedId === asset.id ? 'true' : undefined" @click="selectedId = asset.id">
              <div class="flex items-center justify-between gap-2"><strong class="truncate text-sm">{{ asset.name }}</strong><Badge :tone="assetTone(asset)">{{ assetTypeLabel(asset.type) }}</Badge></div>
              <span class="truncate text-xs text-muted-foreground">{{ asset.fingerprint || t('common.notAvailable') }}</span>
            </button>
          </template>
          </div>
          <PaginationBar :page="page" class="px-3" :page-size="pageSize" :total="total" :loading="loading" :previous-label="t('common.previous')" :next-label="t('common.next')" @update:page="changePage" />
        </aside>
        </template>

        <template #detail>
        <main class="grid min-h-0 min-w-0 overflow-hidden rounded-2xl border border-border bg-card">
          <EmptyState v-if="mode === 'domains' && !selectedCert" :title="t('certificatesPage.selectCertificate')" :description="t('certificatesPage.selectCertificateHint')" />
          <article v-else-if="mode === 'domains' && selectedCert" class="grid min-h-0 grid-rows-[auto_minmax(0,1fr)]">
            <header class="flex items-start justify-between gap-3 border-b border-border p-5 max-md:grid">
              <div><h2 class="m-0 text-xl font-semibold">{{ selectedCert.name }}</h2><p class="m-0 mt-1 text-sm text-muted-foreground">{{ selectedCert.domain }} / {{ selectedCert.issuer }}</p></div>
              <div class="flex flex-wrap gap-2">
                <Button size="sm" @click="openReissue(selectedCert)"><RotateCcw />{{ t('certificatesPage.adjustReissue') }}</Button>
                <Button size="sm" :loading="saving" @click="renewCertificate(selectedCert)"><RotateCcw />{{ t('certificatesPage.renew') }}</Button>
                <Button size="sm" variant="danger" :loading="saving" @click="confirmDelete = true"><Trash2 />{{ t('common.delete') }}</Button>
              </div>
            </header>
            <div class="min-h-0 overflow-auto p-5">
              <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
                <section class="rounded-2xl border border-border bg-background p-4">
                  <h3 class="m-0 text-sm font-semibold">{{ t('certificatesPage.coverage') }}</h3>
                  <div class="mt-3 flex flex-wrap gap-2"><Badge v-for="domain in selectedCert.domains" :key="domain" tone="info">{{ domain }}</Badge></div>
                  <p v-if="selectedCert.lastError" class="mt-4 rounded-xl border border-danger-border bg-danger-bg p-3 text-sm text-danger">{{ selectedCert.lastError }}</p>
                </section>
                <aside class="grid content-start gap-3">
                  <div class="rounded-2xl border border-border bg-background p-4 text-sm"><div class="text-muted-foreground">{{ t('common.status') }}</div><strong>{{ t(certificateState(selectedCert)) }}</strong></div>
                  <div class="rounded-2xl border border-border bg-background p-4 text-sm"><div class="text-muted-foreground">{{ t('certificatesPage.expiresAt') }}</div><strong>{{ formatDateTime(selectedCert.notAfter) || t('common.notAvailable') }}</strong></div>
                  <div class="rounded-2xl border border-border bg-background p-4 text-sm"><div class="text-muted-foreground">{{ t('certificatesPage.taskEntry') }}</div><strong>{{ selectedCert.status === 'issuing' ? t('certificatesPage.openTaskCenter') : t('certificatesPage.noActiveTask') }}</strong></div>
                </aside>
              </div>
            </div>
          </article>

          <EmptyState v-if="mode === 'self' && !selectedSelf" :title="t('certificatesPage.selectSelf')" :description="t('certificatesPage.selectSelfHint')" />
          <article v-else-if="mode === 'self' && selectedSelf" class="grid min-h-0 grid-rows-[auto_minmax(0,1fr)]">
            <header class="flex items-start justify-between gap-3 border-b border-border p-5 max-md:grid">
              <div><h2 class="m-0 text-xl font-semibold">{{ selectedSelf.name }}</h2><p class="m-0 mt-1 text-sm text-muted-foreground">{{ selectedSelf.commonName }}</p></div>
              <div class="flex flex-wrap gap-2">
                <Button size="sm" @click="openSelf('self-leaf')"><Plus />{{ t('certificatesPage.generateLeaf') }}</Button>
                <Button size="sm" :loading="saving" @click="renewSelf(selectedSelf)"><RotateCcw />{{ t('certificatesPage.reissue') }}</Button>
                <Button size="sm" variant="danger" :loading="saving" @click="confirmDelete = true"><Trash2 />{{ t('common.delete') }}</Button>
              </div>
            </header>
            <div class="min-h-0 overflow-auto p-5">
              <div class="grid gap-3 md:grid-cols-2">
                <div class="rounded-2xl border border-border bg-background p-4 text-sm"><div class="text-muted-foreground">{{ t('common.type') }}</div><strong>{{ selfSignedKindLabel(selectedSelf.kind) }}</strong></div>
                <div class="rounded-2xl border border-border bg-background p-4 text-sm"><div class="text-muted-foreground">{{ t('certificatesPage.fingerprint') }}</div><strong>{{ selectedSelf.fingerprint }}</strong></div>
                <div class="rounded-2xl border border-border bg-background p-4 text-sm"><div class="text-muted-foreground">{{ t('certificatesPage.expiresAt') }}</div><strong>{{ formatDateTime(selectedSelf.notAfter) }}</strong></div>
                <div class="rounded-2xl border border-border bg-background p-4 text-sm"><div class="text-muted-foreground">{{ t('certificatesPage.dnsNames') }}</div><strong>{{ displayEntries(selectedSelf.dnsNames) }}</strong></div>
              </div>
            </div>
          </article>

          <EmptyState v-if="mode === 'keys' && !selectedAsset" :title="t('certificatesPage.selectAsset')" :description="t('certificatesPage.selectAssetHint')" />
          <article v-else-if="mode === 'keys' && selectedAsset" class="grid min-h-0 grid-rows-[auto_minmax(0,1fr)]">
            <header class="flex items-start justify-between gap-3 border-b border-border p-5 max-md:grid">
              <div><h2 class="m-0 text-xl font-semibold">{{ selectedAsset.name }}</h2><p class="m-0 mt-1 text-sm text-muted-foreground">{{ assetTypeLabel(selectedAsset.type) }} / {{ selectedAsset.algorithm }}</p></div>
              <div class="flex flex-wrap gap-2">
                <Button size="sm" @click="openAsset('asset-ca')">{{ t('certificatesPage.generateCa') }}</Button>
                <Button size="sm" @click="openAsset('asset-tls')">{{ t('certificatesPage.generateTls') }}</Button>
                <Button size="sm" @click="openAsset('asset-import')">{{ t('certificatesPage.importAsset') }}</Button>
                <Button size="sm" @click="openAsset('asset-export')"><FileArchive />{{ t('certificatesPage.exportAsset') }}</Button>
                <Button size="sm" :disabled="!selectedAsset.canReissue && !selectedAsset.canRegenerate" :loading="saving" @click="reissueAsset(selectedAsset)"><RotateCcw />{{ selectedAsset.type === 'ssh_key_pair' ? t('certificatesPage.regenerate') : t('certificatesPage.reissue') }}</Button>
                <Button size="sm" variant="danger" :disabled="!selectedAsset.canDelete" :loading="saving" @click="confirmDelete = true"><Trash2 />{{ t('common.delete') }}</Button>
              </div>
            </header>
            <div class="min-h-0 overflow-auto p-5">
              <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
                <section class="grid gap-3 md:grid-cols-2">
                  <div class="rounded-2xl border border-border bg-background p-4 text-sm"><div class="text-muted-foreground">{{ t('certificatesPage.fingerprint') }}</div><strong>{{ selectedAsset.fingerprint }}</strong></div>
                  <div class="rounded-2xl border border-border bg-background p-4 text-sm"><div class="text-muted-foreground">{{ t('certificatesPage.references') }}</div><strong>{{ selectedAsset.referenceCount }}</strong></div>
                  <div class="rounded-2xl border border-border bg-background p-4 text-sm"><div class="text-muted-foreground">{{ t('certificatesPage.children') }}</div><strong>{{ selectedAsset.childCount }}</strong></div>
                  <div class="rounded-2xl border border-border bg-background p-4 text-sm"><div class="text-muted-foreground">{{ t('certificatesPage.expiresAt') }}</div><strong>{{ formatDateTime(selectedAsset.notAfter) || t('common.notAvailable') }}</strong></div>
                </section>
                <aside class="rounded-2xl border border-border bg-background p-4">
                  <h3 class="m-0 text-sm font-semibold">{{ t('certificatesPage.downloads') }}</h3>
                  <div class="mt-3 grid gap-2">
                    <Button v-for="kind in selectedAsset.downloadKinds" :key="kind" size="sm" :loading="saving" @click="downloadAssetFile(selectedAsset, kind)"><Download />{{ fileKindLabel(kind) }}</Button>
                  </div>
                </aside>
              </div>
            </div>
          </article>
        </main>
        </template>
      </MasterDetailLayout>
    </div>

    <Dialog :open="dialog === 'issue'" :title="editingCertificateId ? t('certificatesPage.adjustReissue') : t('certificatesPage.issue')" :close-label="t('common.close')" @update:open="(open: boolean) => { if (!open) dialog = '' }">
      <div class="grid gap-4">
        <label class="grid gap-1 text-sm">{{ t('common.name') }}<Input v-model="issueForm.name" /></label>
        <label class="grid gap-1 text-sm">{{ t('routes.dns.title') }}<Select v-model="issueForm.domainId" :options="domainOptions" /></label>
        <section class="grid gap-3 rounded-xl border border-border p-3">
          <label class="flex items-center justify-between gap-3 text-sm">{{ t('certificatesPage.rootDomain') }}<Switch v-model="issueForm.includeRoot" :label="t('certificatesPage.rootDomain')" /></label>
          <label class="flex items-center justify-between gap-3 text-sm">{{ t('certificatesPage.wildcardDomain') }}<Switch v-model="issueForm.includeWildcard" :label="t('certificatesPage.wildcardDomain')" /></label>
          <div class="grid gap-2">
            <div class="text-sm font-medium">{{ t('certificatesPage.subdomains') }}</div>
            <div v-for="(_, index) in issueForm.subdomains" :key="index" class="grid grid-cols-[minmax(0,1fr)_auto] gap-2">
              <Input v-model="issueForm.subdomains[index]" :placeholder="t('certificatesPage.subdomainPlaceholder')" />
              <Button size="sm" variant="ghost" @click="removeEntry(issueForm.subdomains, index)"><Trash2 />{{ t('common.delete') }}</Button>
            </div>
            <Button size="sm" @click="addEntry(issueForm.subdomains)"><Plus />{{ t('certificatesPage.addSubdomain') }}</Button>
          </div>
        </section>
        <section class="rounded-xl border border-border bg-background p-3">
          <div class="text-sm font-medium">{{ t('certificatesPage.coveragePreview') }}</div>
          <div class="mt-2 flex flex-wrap gap-2"><Badge v-for="domain in issueCoverage" :key="domain" tone="info">{{ domain }}</Badge></div>
        </section>
      </div>
      <template #footer><Button @click="dialog = ''">{{ t('common.cancel') }}</Button><Button variant="primary" :loading="saving" :disabled="!issueForm.name || !issueForm.domainId" @click="issueCertificate">{{ editingCertificateId ? t('certificatesPage.adjustReissue') : t('certificatesPage.issue') }}</Button></template>
    </Dialog>

    <Dialog :open="dialog === 'self-ca' || dialog === 'self-leaf'" :title="dialog === 'self-ca' ? t('certificatesPage.generateCa') : t('certificatesPage.generateLeaf')" :close-label="t('common.close')" @update:open="(open: boolean) => { if (!open) dialog = '' }">
      <div class="grid gap-3"><label class="grid gap-1 text-sm">{{ t('common.name') }}<Input v-model="selfForm.name" /></label><label v-if="dialog === 'self-leaf'" class="grid gap-1 text-sm">{{ t('certificatesPage.kindCa') }}<Select v-model="selfForm.caId" :options="selfCas" /></label><label class="grid gap-1 text-sm">{{ t('certificatesPage.commonName') }}<Input v-model="selfForm.commonName" /></label><div v-if="dialog === 'self-leaf'" class="grid gap-2"><div class="text-sm font-medium">{{ t('certificatesPage.dnsNames') }}</div><div v-for="(_, index) in selfForm.dnsNames" :key="index" class="grid grid-cols-[minmax(0,1fr)_auto] gap-2"><Input v-model="selfForm.dnsNames[index]" /><Button size="sm" variant="ghost" @click="removeEntry(selfForm.dnsNames, index)"><Trash2 />{{ t('common.delete') }}</Button></div><Button size="sm" @click="addEntry(selfForm.dnsNames)"><Plus />{{ t('certificatesPage.addDnsName') }}</Button></div><div v-if="dialog === 'self-leaf'" class="grid gap-2"><div class="text-sm font-medium">{{ t('certificatesPage.ipAddresses') }}</div><div v-for="(_, index) in selfForm.ipAddresses" :key="index" class="grid grid-cols-[minmax(0,1fr)_auto] gap-2"><Input v-model="selfForm.ipAddresses[index]" /><Button size="sm" variant="ghost" @click="removeEntry(selfForm.ipAddresses, index)"><Trash2 />{{ t('common.delete') }}</Button></div><Button size="sm" @click="addEntry(selfForm.ipAddresses)"><Plus />{{ t('certificatesPage.addIpAddress') }}</Button></div><label v-if="dialog === 'self-ca'" class="grid gap-1 text-sm">{{ t('certificatesPage.years') }}<Input v-model="selfForm.years" type="number" /></label><label v-else class="grid gap-1 text-sm">{{ t('certificatesPage.days') }}<Input v-model="selfForm.days" type="number" /></label></div>
      <template #footer><Button @click="dialog = ''">{{ t('common.cancel') }}</Button><Button variant="primary" :loading="saving" :disabled="!selfForm.name || !selfForm.commonName" @click="saveSelf">{{ t('common.create') }}</Button></template>
    </Dialog>

    <Dialog :open="dialog.startsWith('asset-') && !['asset-export','asset-preflight'].includes(dialog)" :size="dialog === 'asset-import' ? 'large' : 'default'" :title="t('certificatesPage.assetForm')" :close-label="t('common.close')" @update:open="(open: boolean) => { if (!open) dialog = '' }">
      <div class="grid gap-3" :class="dialog === 'asset-import' ? 'h-full min-h-0 grid-rows-[auto_minmax(0,1fr)]' : ''">
        <div :class="dialog === 'asset-import' ? 'grid gap-3 md:grid-cols-2' : 'contents'">
          <label v-if="dialog === 'asset-import'" class="grid gap-1 text-sm">{{ t('common.type') }}<Select v-model="assetForm.type" :options="assetTypeOptions" /></label>
          <label class="grid gap-1 text-sm">{{ t('common.name') }}<Input v-model="assetForm.name" /></label>
          <label v-if="dialog === 'asset-tls' || (dialog === 'asset-import' && assetForm.type === 'tls_certificate')" class="grid gap-1 text-sm">{{ t('certificatesPage.kindCa') }}<Select v-model="assetForm.parentAssetId" :options="caAssets" /></label>
          <label v-if="dialog !== 'asset-ssh'" class="grid gap-1 text-sm">{{ t('certificatesPage.commonName') }}<Input v-model="assetForm.commonName" /></label>
          <label class="grid gap-1 text-sm">{{ t('certificatesPage.algorithm') }}<Select v-model="assetForm.algorithm" :options="algorithmOptions" /></label>
          <label v-if="assetForm.algorithm === 'rsa'" class="grid gap-1 text-sm">{{ t('certificatesPage.keySize') }}<Input v-model="assetForm.keySize" type="number" /></label>
        </div>
        <div v-if="dialog === 'asset-import'" class="min-h-0">
          <label v-if="!assetImportHasCertificate(assetForm.type)" class="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-1 text-sm">
            {{ t('certificatesPage.privateKeyPem') }}
            <CodeEditor v-model="assetForm.privateKeyPem" language="plain" :editor-label="t('certificatesPage.privateKeyPem')" />
          </label>
          <Tabs v-else v-model="assetImportMaterialTab" class="h-full" :tabs="assetImportMaterialTabs">
            <label v-if="assetImportMaterialTab === 'privateKey'" class="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-1 text-sm">
              {{ t('certificatesPage.privateKeyPem') }}
              <CodeEditor v-model="assetForm.privateKeyPem" language="plain" :editor-label="t('certificatesPage.privateKeyPem')" />
            </label>
            <label v-else class="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-1 text-sm">
              {{ t('certificatesPage.certificatePem') }}
              <CodeEditor v-model="assetForm.certificatePem" language="plain" :editor-label="t('certificatesPage.certificatePem')" />
            </label>
          </Tabs>
        </div>
        <div v-if="dialog === 'asset-tls'" class="grid gap-2">
          <div class="text-sm font-medium">{{ t('certificatesPage.dnsNames') }}</div>
          <div v-for="(_, index) in assetForm.dnsNames" :key="index" class="grid grid-cols-[minmax(0,1fr)_auto] gap-2"><Input v-model="assetForm.dnsNames[index]" /><Button size="sm" variant="ghost" @click="removeEntry(assetForm.dnsNames, index)"><Trash2 />{{ t('common.delete') }}</Button></div>
          <Button size="sm" @click="addEntry(assetForm.dnsNames)"><Plus />{{ t('certificatesPage.addDnsName') }}</Button>
        </div>
        <div v-if="dialog === 'asset-tls'" class="grid gap-2">
          <div class="text-sm font-medium">{{ t('certificatesPage.ipAddresses') }}</div>
          <div v-for="(_, index) in assetForm.ipAddresses" :key="index" class="grid grid-cols-[minmax(0,1fr)_auto] gap-2"><Input v-model="assetForm.ipAddresses[index]" /><Button size="sm" variant="ghost" @click="removeEntry(assetForm.ipAddresses, index)"><Trash2 />{{ t('common.delete') }}</Button></div>
          <Button size="sm" @click="addEntry(assetForm.ipAddresses)"><Plus />{{ t('certificatesPage.addIpAddress') }}</Button>
        </div>
      </div>
      <template #footer><Button @click="dialog = ''">{{ t('common.cancel') }}</Button><Button variant="primary" :loading="saving" :disabled="!assetForm.name" @click="saveAsset">{{ t('common.save') }}</Button></template>
    </Dialog>

    <Dialog :open="dialog === 'asset-export'" :title="t('certificatesPage.exportAsset')" :close-label="t('common.close')" @update:open="(open: boolean) => { if (!open) dialog = '' }">
      <div class="grid gap-3"><p class="m-0 text-sm text-muted-foreground">{{ t('certificatesPage.exportHint') }}</p><label class="grid gap-1 text-sm">{{ t('certificatesPage.archivePassword') }}<Input v-model="assetForm.password" type="password" /></label></div>
      <template #footer><Button @click="dialog = ''">{{ t('common.cancel') }}</Button><Button :loading="saving" :disabled="!assetForm.password" @click="createExport">{{ t('certificatesPage.exportAsset') }}</Button></template>
    </Dialog>

    <Dialog :open="dialog === 'asset-preflight'" :title="t('certificatesPage.batchImport')" :description="t('certificatesPage.batchImportHint')" :close-label="t('common.close')" @update:open="(open: boolean) => { if (!open) dialog = '' }">
      <div class="grid gap-3">
        <div class="grid gap-1 text-sm">
          <span>{{ t('certificatesPage.importArchive') }}</span>
          <FileUploadButton accept=".zip,.tar,.tar.gz,.tgz,.panel-key-assets,application/zip,application/x-tar,application/gzip,application/octet-stream" :label="importFile ? importFile.name : t('certificatesPage.chooseImportArchive')" @change="onFileChange" />
        </div>
        <label class="grid gap-1 text-sm">{{ t('certificatesPage.archivePassword') }}<Input v-model="assetForm.password" type="password" /></label>
        <Button :disabled="!importFile || !assetForm.password" :loading="saving" @click="preflightImport">{{ t('certificatesPage.preflightImport') }}</Button>
        <div v-if="importPlan && importPlan.summary.conflictCount" class="rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning">{{ t('certificatesPage.importConflicts', { count: importPlan.summary.conflictCount }) }}</div>
        <div v-if="importPlan && !importPlan.requiresDangerConfirm" class="rounded-xl border border-info-border bg-info-bg p-3 text-sm text-info">{{ t('certificatesPage.importReady') }}</div>
      </div>
      <template #footer>
        <Button @click="dialog = ''">{{ t('common.cancel') }}</Button>
        <Button v-if="importPlan && !importPlan.requiresDangerConfirm" variant="primary" :loading="saving" @click="executeImport(false)">{{ t('certificatesPage.executeImport') }}</Button>
        <Button v-if="importPlan && importPlan.requiresDangerConfirm" variant="danger" :loading="saving" @click="importConfirmOpen = true">{{ t('certificatesPage.executeImport') }}</Button>
      </template>
    </Dialog>

    <ConfirmDialog
      :open="importConfirmOpen"
      :title="t('certificatesPage.executeImport')"
      :impact="t('certificatesPage.importConflictImpact')"
      tone="danger"
      :require-checkbox="true"
      :checkbox-label="t('certificatesPage.importConflictCheckbox')"
      :confirm-label="t('certificatesPage.executeImport')"
      :cancel-label="t('common.cancel')"
      :loading="saving"
      @confirm="executeImport(true)"
      @update:open="(open: boolean) => { if (!open) importConfirmOpen = false }"
    />

    <Dialog v-model:open="confirmDelete" :title="t('certificatesPage.deleteTitle')" :close-label="t('common.close')">
      <p class="m-0 text-sm text-muted-foreground">{{ t('certificatesPage.deleteHint') }}</p>
      <template #footer><Button @click="confirmDelete = false">{{ t('common.cancel') }}</Button><Button variant="danger" :loading="saving" @click="deleteSelected">{{ t('common.delete') }}</Button></template>
    </Dialog>
  </ConsolePage>
</template>
