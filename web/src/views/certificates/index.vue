<script setup lang="ts">
import ActivityLink from '@/components/activity/ActivityLink.vue';
import { computed, onMounted, reactive, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Download, Eye, FileArchive, KeyRound, MoreHorizontal, Plus, RefreshCcw, RotateCcw, Trash2, Wrench } from '@lucide/vue';
import { certificatesApi } from '@/api/certificates';
import { dnsApi } from '@/api/dns';
import { keyAssetsApi } from '@/api/keyAssets';
import { saveBlobDownload } from '@/api/download';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import CodeEditor from '@/components/ui/CodeEditor.vue';
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue';
import Dialog from '@/components/ui/Dialog.vue';
import Dropdown from '@/components/ui/Dropdown.vue';
import DropdownItem from '@/components/ui/DropdownItem.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import FileUploadButton from '@/components/ui/FileUploadButton.vue';
import IconButton from '@/components/ui/IconButton.vue';
import Input from '@/components/ui/Input.vue';
import PaginationBar from '@/components/ui/PaginationBar.vue';
import SearchInput from '@/components/ui/SearchInput.vue';
import Select from '@/components/ui/Select.vue';
import Switch from '@/components/ui/Switch.vue';
import Table from '@/components/ui/Table.vue';
import Tabs from '@/components/ui/Tabs.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import TableDetailName from '@/components/patterns/TableDetailName.vue';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import ListPage from '@/components/templates/ListPage.vue';
import TableDetailLayout from '@/components/templates/TableDetailLayout.vue';
import { useI18n } from '@/i18n';
import type { DomainCertificateDto, SelfSignedCertificateDto } from '@/types/certificates';
import type { DnsDomainDto } from '@/types/dns';
import type { ImportPreflightDto, KeyAssetDto, KeyAssetType } from '@/types/keyAssets';
import { assetImportHasCertificate, initialAssetImportMaterialTab, type AssetImportMaterialTab } from './assetImportEditor';
import {
  assetTone,
  certificateState,
  certificateTone,
  selfSignedTone,
  validateIssueCertificateForm,
  validateKeyAssetForm,
  validateSelfSignedForm,
  type FieldErrors,
} from './model';
import { createLatestRequestGuard, isAbortError } from '@/views/_shared/requestState';
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
const saving = ref(false);
const importPlan = ref<ImportPreflightDto | null>(null);
const editingCertificateId = ref('');
const page = ref(parsePageQuery(route.query.page));
const pageSize = 50;
const total = ref(0);
const search = ref(String(route.query.search ?? ''));
let searchTimer: ReturnType<typeof setTimeout> | undefined;
const importConfirmOpen = ref(false);
const exportTargetId = ref('');
const deleteTarget = ref<{ kind: 'domains' | 'self' | 'keys'; id: string; name: string } | null>(null);

function parsePageQuery(value: unknown) {
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed > 0 ? parsed : 1;
}

const mode = computed(() => route.path.includes('/self-signed') ? 'self' : route.path.includes('/keys') ? 'keys' : 'domains');
const modePaths = {
  domains: '/certificates/domains',
  self: '/certificates/self-signed',
  keys: '/certificates/keys',
} as const;
const modeTabs = computed(() => [
  { value: 'domains', label: t('routes.certificates.title') },
  { value: 'self', label: t('routes.selfSigned.title') },
  { value: 'keys', label: t('routes.keys.title') },
]);
const title = computed(() => mode.value === 'self' ? t('routes.selfSigned.title') : mode.value === 'keys' ? t('routes.keys.title') : t('routes.certificates.title'));
const description = computed(() => mode.value === 'self' ? t('routes.selfSigned.description') : mode.value === 'keys' ? t('routes.keys.description') : t('routes.certificates.description'));
const selectedCert = computed(() => certs.value.find((item) => item.id === selectedId.value) ?? null);
const selectedSelf = computed(() => selfSigned.value.find((item) => item.id === selectedId.value) ?? null);
const userAssets = computed(() => assets.value.filter((item) => !isSystemManagedAsset(item)));
const selectedAsset = computed(() => userAssets.value.find((item) => item.id === selectedId.value) ?? null);
const caAssets = computed(() => userAssets.value.filter((item) => item.type === 'ca_certificate').map((item) => ({ label: item.name, value: item.id })));
const selfCas = computed(() => selfSigned.value.filter((item) => item.kind === 'ca').map((item) => ({ label: item.name, value: item.id })));
const domainOptions = computed(() => domains.value.map((item) => ({ label: item.name, value: item.id })));
const detailOpen = computed(() => Boolean(selectedId.value));
const detailTitle = computed(() => selectedCert.value?.name ?? selectedSelf.value?.name ?? selectedAsset.value?.name ?? t('common.loading'));
const detailDescription = computed(() => {
  if (mode.value === 'domains' && selectedCert.value) return `${selectedCert.value.domain} / ${selectedCert.value.issuer}`;
  if (mode.value === 'self' && selectedSelf.value) return selectedSelf.value.commonName;
  if (mode.value === 'keys' && selectedAsset.value) return `${assetTypeLabel(selectedAsset.value.type)} / ${selectedAsset.value.algorithm}`;
  return '';
});
const issueForm = reactive({ name: '', domainId: '', includeRoot: true, includeWildcard: false, subdomains: [''] });
const selfForm = reactive({ name: '', caId: '', commonName: '', dnsNames: [''], ipAddresses: [''], years: '5', days: '90' });
const assetForm = reactive({ type: 'ssh_key_pair' as KeyAssetType, name: '', parentAssetId: '', commonName: '', dnsNames: [''], ipAddresses: [''], algorithm: 'ed25519', keySize: '2048', validityDays: '365', comment: '', certificatePem: '', privateKeyPem: '', publicKey: '', password: '' });
const issueFormErrors = reactive<FieldErrors>({});
const selfFormErrors = reactive<FieldErrors>({});
const assetFormErrors = reactive<FieldErrors>({});
const assetImportMaterialTab = ref<AssetImportMaterialTab>(initialAssetImportMaterialTab());
const importFile = ref<File | null>(null);
const selectedIssueDomainName = computed(() => domains.value.find((item) => item.id === issueForm.domainId)?.name ?? '');
const issueCoverage = computed(() => issuePrefixes().map((prefix) => prefixToDomain(prefix, selectedIssueDomainName.value)).filter(Boolean));
const issueFormValid = computed(() => Object.keys(validateIssueCertificateForm({
  name: issueForm.name,
  domainId: issueForm.domainId,
  coverage: selectedIssueDomainName.value ? issueCoverage.value : [],
})).length === 0);
const selfFormValid = computed(() => Object.keys(validateSelfSignedForm({
  name: selfForm.name,
  commonName: selfForm.commonName,
  caId: selfForm.caId,
  requireCa: dialog.value === 'self-leaf',
})).length === 0);
const assetFormValid = computed(() => Object.keys(validateKeyAssetForm({
  name: assetForm.name,
  commonName: assetForm.commonName,
  requireCommonName: dialog.value === 'asset-ca' || dialog.value === 'asset-tls' || (dialog.value === 'asset-import' && assetForm.type !== 'ssh_key_pair'),
  parentAssetId: assetForm.parentAssetId,
  requireParent: dialog.value === 'asset-tls' || (dialog.value === 'asset-import' && assetForm.type === 'tls_certificate'),
})).length === 0);

function clearFieldErrors(target: FieldErrors) {
  Object.keys(target).forEach((key) => { delete target[key]; });
}

function assignFieldErrors(target: FieldErrors, next: FieldErrors) {
  clearFieldErrors(target);
  Object.assign(target, next);
}

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
const domainColumns = computed(() => [
  { key: 'name', label: t('common.name') },
  { key: 'coverage', label: t('certificatesPage.coverage') },
  { key: 'status', label: t('common.status'), nowrap: true },
  { key: 'expiresAt', label: t('certificatesPage.expiresAt'), nowrap: true },
  { key: 'actions', label: t('common.actions'), align: 'right' as const, nowrap: true },
]);
const selfColumns = computed(() => [
  { key: 'name', label: t('common.name') },
  { key: 'kind', label: t('common.type'), nowrap: true },
  { key: 'commonName', label: t('certificatesPage.commonName') },
  { key: 'expiresAt', label: t('certificatesPage.expiresAt'), nowrap: true },
  { key: 'actions', label: t('common.actions'), align: 'right' as const, nowrap: true },
]);
const assetColumns = computed(() => [
  { key: 'name', label: t('common.name') },
  { key: 'type', label: t('common.type'), nowrap: true },
  { key: 'algorithm', label: t('certificatesPage.algorithm'), nowrap: true },
  { key: 'fingerprint', label: t('certificatesPage.fingerprint') },
  { key: 'expiresAt', label: t('certificatesPage.expiresAt'), nowrap: true },
  { key: 'actions', label: t('common.actions'), align: 'right' as const, nowrap: true },
]);

onMounted(load);
watch(() => route.path, () => { void load(); });

watch(() => route.query.selected, (value) => {
  const next = String(value ?? '');
  if (next !== selectedId.value) selectedId.value = next;
});

function openDetail(id: string) {
  selectedId.value = id;
  void router.push({ query: { ...route.query, selected: id } });
}

function closeDetail() {
  if (!selectedId.value) return;
  selectedId.value = '';
  void router.replace({ query: { ...route.query, selected: undefined } });
}

watch(search, (value) => {
  void router.replace({ query: { ...route.query, search: value || undefined, page: undefined } });
  if (searchTimer) clearTimeout(searchTimer);
  searchTimer = setTimeout(() => {
    if (page.value !== 1) page.value = 1;
    void load();
  }, 250);
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
    if (!route.query.selected && selectedId.value && !list.some((item) => item.id === selectedId.value)) {
      selectedId.value = '';
    }
    if (firstError) {
      error.value = firstError;
      notifyError(firstError);
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('certificatesPage.loadFailed');
    notifyError(err instanceof Error ? err.message : t('certificatesPage.loadFailed'), err);
  } finally {
    if (listRequests.isCurrent(requestId)) loading.value = false;
  }
}

function switchMode(nextMode: string) {
  const path = modePaths[nextMode as keyof typeof modePaths];
  if (!path) return;
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
  const localErrors = validateIssueCertificateForm({
    name: issueForm.name,
    domainId: issueForm.domainId,
    coverage: selectedIssueDomainName.value ? issueCoverage.value : [],
  });
  assignFieldErrors(issueFormErrors, localErrors);
  if (Object.keys(localErrors).length) return;
  saving.value = true;
  try {
    const prefixes = issuePrefixes();
    const input = { name: issueForm.name.trim(), domainId: issueForm.domainId, prefixes: prefixes.length ? prefixes : ['@'] };
    const result = editingCertificateId.value ? await certificatesApi.reissue(editingCertificateId.value, input) : await certificatesApi.issue(input);
    notifySuccess(result.taskId ? t('certificatesPage.issueTask', { taskId: result.taskId }) : t('certificatesPage.issued'), result);
    selectedId.value = result.certificate.id;
    editingCertificateId.value = '';
    dialog.value = '';
    await router.push({ query: { ...route.query, selected: result.certificate.id } });
    await load();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.operationFailed');
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    saving.value = false;
  }
}

async function renewCertificate(cert: DomainCertificateDto) {
  await run(async () => {
    const result = await certificatesApi.renew(cert.id);
    notifySuccess(t('certificatesPage.renewAccepted'), result);
    await load();
  });
}

function openSelf(kind: 'self-ca' | 'self-leaf') {
  Object.assign(selfForm, { name: '', caId: selfCas.value[0]?.value ?? '', commonName: '', dnsNames: [''], ipAddresses: [''], years: '5', days: '90' });
  clearFieldErrors(selfFormErrors);
  dialog.value = kind;
}

async function saveSelf() {
  const localErrors = validateSelfSignedForm({
    name: selfForm.name,
    commonName: selfForm.commonName,
    caId: selfForm.caId,
    requireCa: dialog.value === 'self-leaf',
  });
  assignFieldErrors(selfFormErrors, localErrors);
  if (Object.keys(localErrors).length) return;
  saving.value = true;
  try {
    const saved = dialog.value === 'self-ca'
      ? await certificatesApi.createSelfSignedCa({ name: selfForm.name.trim(), commonName: selfForm.commonName.trim(), years: Number(selfForm.years) || 5 })
      : await certificatesApi.createSelfSignedLeaf({ name: selfForm.name.trim(), caId: selfForm.caId, commonName: selfForm.commonName.trim(), dnsNames: cleanEntries(selfForm.dnsNames), ipAddresses: cleanEntries(selfForm.ipAddresses), days: Number(selfForm.days) || 90 });
    selectedId.value = saved.id;
    notifySuccess(t('certificatesPage.selfSaved'), saved);
    dialog.value = '';
    await router.push({ query: { ...route.query, selected: saved.id } });
    await load();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.operationFailed');
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    saving.value = false;
  }
}

async function renewSelf(cert: SelfSignedCertificateDto) {
  await run(async () => {
    const renewed = await certificatesApi.renewSelfSigned(cert.id);
    selectedId.value = renewed.id;
    notifySuccess(t('certificatesPage.selfRenewed'), renewed);
    await load();
  });
}

function openAsset(next: typeof dialog.value) {
  Object.assign(assetForm, { type: 'ssh_key_pair', name: '', parentAssetId: caAssets.value[0]?.value ?? '', commonName: '', dnsNames: [''], ipAddresses: [''], algorithm: 'ed25519', keySize: '2048', validityDays: '365', comment: '', certificatePem: '', privateKeyPem: '', publicKey: '', password: '' });
  importPlan.value = null;
  importFile.value = null;
  assetActionError.value = '';
  clearFieldErrors(assetFormErrors);
  assetImportMaterialTab.value = initialAssetImportMaterialTab();
  dialog.value = next;
}

async function saveAsset() {
  const assetDialog = dialog.value;
  const localErrors = validateKeyAssetForm({
    name: assetForm.name,
    commonName: assetForm.commonName,
    requireCommonName: assetDialog === 'asset-ca' || assetDialog === 'asset-tls' || (assetDialog === 'asset-import' && assetForm.type !== 'ssh_key_pair'),
    parentAssetId: assetForm.parentAssetId,
    requireParent: assetDialog === 'asset-tls' || (assetDialog === 'asset-import' && assetForm.type === 'tls_certificate'),
  });
  assignFieldErrors(assetFormErrors, localErrors);
  if (Object.keys(localErrors).length) return;
  if (assetDialog === 'asset-import') assetActionError.value = '';
  saving.value = true;
  try {
    let result;
    if (dialog.value === 'asset-ca') result = await keyAssetsApi.createCa({ name: assetForm.name, commonName: assetForm.commonName, algorithm: assetForm.algorithm as 'ed25519' | 'rsa', keySize: Number(assetForm.keySize) || 0, years: 0, validityDays: Number(assetForm.validityDays) || 365 });
    if (dialog.value === 'asset-tls') result = await keyAssetsApi.createTls({ name: assetForm.name, parentAssetId: assetForm.parentAssetId, caId: assetForm.parentAssetId, commonName: assetForm.commonName, algorithm: assetForm.algorithm as 'ed25519' | 'rsa', keySize: Number(assetForm.keySize) || 0, dnsNames: cleanEntries(assetForm.dnsNames), ipAddresses: cleanEntries(assetForm.ipAddresses), days: 0, validityDays: Number(assetForm.validityDays) || 365 });
    if (dialog.value === 'asset-ssh') result = await keyAssetsApi.generateSsh({ name: assetForm.name, algorithm: assetForm.algorithm as 'ed25519' | 'rsa', keySize: Number(assetForm.keySize) || 0, comment: assetForm.comment });
    if (dialog.value === 'asset-import') result = await keyAssetsApi.importOne({ type: assetForm.type, name: assetForm.name, parentAssetId: assetForm.parentAssetId, commonName: assetForm.commonName, algorithm: assetForm.algorithm, keySize: Number(assetForm.keySize) || 0, certificatePem: assetForm.certificatePem, privateKeyPem: assetForm.privateKeyPem, publicKey: assetForm.publicKey });
    selectedId.value = result?.asset?.id ?? selectedId.value;
    notifySuccess(t('certificatesPage.assetSaved'), result);
    dialog.value = '';
    await load();
  } catch (err) {
    const message = err instanceof Error ? err.message : t('common.operationFailed');
    notifyError(message, err);
    if (assetDialog === 'asset-import') assetActionError.value = message;
    else error.value = message;
  } finally {
    saving.value = false;
  }
}

async function reissueAsset(asset: KeyAssetDto) {
  await run(async () => {
    const result = asset.type === 'ssh_key_pair' ? await keyAssetsApi.regenerate(asset.id) : await keyAssetsApi.reissue(asset.id);
    notifySuccess(result.taskId ? t('certificatesPage.assetTask', { taskId: result.taskId }) : t('certificatesPage.assetSaved'), result);
    await load();
  });
}

/**
 * 行内高频操作（续签/重签/重生成）的单项 loading：只锁定当前行按钮，
 * 不全表转圈（`saving` 仍用于对话框与详情内的操作）。
 */
const rowPending = ref('');
async function runRowAction(key: string, action: () => Promise<void>) {
  if (rowPending.value) return;
  rowPending.value = key;
  try {
    await action();
  } finally {
    rowPending.value = '';
  }
}

function renewDomainRow(cert: DomainCertificateDto) {
  return runRowAction(`domains:${cert.id}`, () => renewCertificate(cert));
}

function renewSelfRow(cert: SelfSignedCertificateDto) {
  return runRowAction(`self:${cert.id}`, () => renewSelf(cert));
}

function reissueAssetRow(asset: KeyAssetDto) {
  return runRowAction(`keys:${asset.id}`, () => reissueAsset(asset));
}

function openExport(asset?: KeyAssetDto) {
  exportTargetId.value = asset?.id ?? selectedAsset.value?.id ?? '';
  assetForm.password = '';
  dialog.value = 'asset-export';
}

async function createExport() {
  await run(async () => {
    const result = await keyAssetsApi.createExport({ assetIds: exportTargetId.value ? [exportTargetId.value] : userAssets.value.map((item) => item.id), password: assetForm.password });
    notifySuccess(t('certificatesPage.exportTask', { taskId: result.taskId }), result);
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
    notifySuccess(t('certificatesPage.importTask', { taskId: result.taskId }), result);
    dialog.value = '';
    importConfirmOpen.value = false;
    await load();
  });
}

function askDelete(kind: 'domains' | 'self' | 'keys', id: string, name: string, canDelete = true) {
  if (!canDelete) return;
  deleteTarget.value = { kind, id, name };
}

async function confirmDeleteAction() {
  const target = deleteTarget.value;
  if (!target) return;
  await run(async () => {
    let result: unknown;
    if (target.kind === 'domains') result = await certificatesApi.delete(target.id);
    if (target.kind === 'self') result = await certificatesApi.deleteSelfSigned(target.id);
    if (target.kind === 'keys') result = await keyAssetsApi.delete(target.id);
    notifySuccess(t('certificatesPage.deleted'), result);
    deleteTarget.value = null;
    if (selectedId.value === target.id) closeDetail();
    await load();
  });
}

async function run(action: () => Promise<void>) {
  saving.value = true;
  error.value = '';
  try {
    await action();
  } catch (err) {
    if (isAbortError(err)) return;
    error.value = err instanceof Error ? err.message : t('common.operationFailed');
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
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
  clearFieldErrors(issueFormErrors);
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
  <TableDetailLayout class="h-full" :has-detail="detailOpen">
    <template #list>
      <ConsolePage :title="title" :description="description">
        <template #actions>
          <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
          <template v-if="mode === 'domains'">
            <Button size="sm" variant="primary" @click="openIssue"><Plus />{{ t('certificatesPage.issue') }}</Button>
          </template>
          <template v-else-if="mode === 'self'">
            <Button size="sm" variant="primary" @click="openSelf('self-ca')"><Plus />{{ t('certificatesPage.generateCa') }}</Button>
          </template>
          <template v-else>
            <Button size="sm" @click="openBatchImport"><FileArchive />{{ t('certificatesPage.batchImport') }}</Button>
            <Button size="sm" @click="openAsset('asset-ca')"><Plus />{{ t('certificatesPage.generateCa') }}</Button>
            <Button size="sm" @click="openAsset('asset-tls')"><Plus />{{ t('certificatesPage.generateTls') }}</Button>
            <Button size="sm" variant="primary" @click="openAsset('asset-ssh')"><KeyRound />{{ t('certificatesPage.generateSsh') }}</Button>
          </template>
        </template>

        <Tabs
          class="h-full min-h-0"
          :model-value="mode"
          :tabs="modeTabs"
          keep-panels
          @update:model-value="switchMode"
        >
          <ListPage>
            <template #toolbar>
              <SearchInput v-model="search" clearable :label="t('common.search')" :placeholder="t('certificatesPage.searchPlaceholder')" :clear-label="t('common.clearSearch')" />
            </template>

            <EmptyState v-if="error && ((mode === 'domains' && !certs.length) || (mode === 'self' && !selfSigned.length) || (mode === 'keys' && !userAssets.length))" variant="page" :title="t('common.loadFailed')" :description="error">
              <template #actions>
                <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button>
              </template>
            </EmptyState>

            <Table
              v-else-if="mode === 'domains'"
              class="h-full max-lg:max-h-[60dvh]"
              :columns="domainColumns"
              :rows="certs"
              row-key="id"
              :active-key="selectedId"
              :loading="loading && !certs.length"
              :loading-label="t('common.loading')"
            >
              <template #name="{ row }">
                <TableDetailName class="max-w-[16rem]" :name="row.name" :label="t('common.openDetails', { name: row.name })" @click="openDetail(row.id)" />
              </template>
              <template #coverage="{ row }">
                <span class="block max-w-[20rem] truncate text-muted-foreground">{{ row.domains.join(', ') }}</span>
              </template>
              <template #status="{ row }">
                <Badge :tone="certificateTone(row)">{{ t(certificateState(row)) }}</Badge>
              </template>
              <template #expiresAt="{ row }">
                <span class="text-muted-foreground">{{ formatDateTime(row.notAfter, t('common.never')) }}</span>
              </template>
              <template #actions="{ row }">
                <div class="flex items-center justify-end gap-1">
                  <Button size="sm" variant="secondary" @click="openDetail(row.id)"><Eye />{{ t('common.details') }}</Button>
                  <Button size="sm" variant="ghost" :loading="rowPending === `domains:${row.id}`" :disabled="Boolean(rowPending)" @click="renewDomainRow(row)"><RotateCcw />{{ t('certificatesPage.renew') }}</Button>
                  <Dropdown align="right">
                    <template #trigger>
                      <IconButton size="sm" :label="t('common.moreActions')"><MoreHorizontal /></IconButton>
                    </template>
                    <DropdownItem @click="openReissue(row)"><Wrench />{{ t('certificatesPage.adjustReissue') }}</DropdownItem>
                    <DropdownItem tone="danger" @click="askDelete('domains', row.id, row.name)"><Trash2 />{{ t('common.delete') }}</DropdownItem>
                  </Dropdown>
                </div>
              </template>
              <template #empty>
                <EmptyState
                  variant="inset"
                  :title="search ? t('common.filterEmpty') : t('certificatesPage.noCertificates')"
                  :description="search ? t('common.filterEmptyHint') : t('certificatesPage.noCertificatesHint')"
                />
              </template>
            </Table>

            <Table
              v-else-if="mode === 'self'"
              class="h-full max-lg:max-h-[60dvh]"
              :columns="selfColumns"
              :rows="selfSigned"
              row-key="id"
              :active-key="selectedId"
              :loading="loading && !selfSigned.length"
              :loading-label="t('common.loading')"
            >
              <template #name="{ row }">
                <TableDetailName class="max-w-[16rem]" :name="row.name" :label="t('common.openDetails', { name: row.name })" @click="openDetail(row.id)" />
              </template>
              <template #kind="{ row }">
                <Badge :tone="selfSignedTone(row)">{{ selfSignedKindLabel(row.kind) }}</Badge>
              </template>
              <template #commonName="{ row }">
                <span class="text-muted-foreground">{{ row.commonName }}</span>
              </template>
              <template #expiresAt="{ row }">
                <span class="text-muted-foreground">{{ formatDateTime(row.notAfter, t('common.never')) }}</span>
              </template>
              <template #actions="{ row }">
                <div class="flex items-center justify-end gap-1">
                  <Button size="sm" variant="secondary" @click="openDetail(row.id)"><Eye />{{ t('common.details') }}</Button>
                  <Button size="sm" variant="ghost" :loading="rowPending === `self:${row.id}`" :disabled="Boolean(rowPending)" @click="renewSelfRow(row)"><RotateCcw />{{ t('certificatesPage.reissue') }}</Button>
                  <Dropdown align="right">
                    <template #trigger>
                      <IconButton size="sm" :label="t('common.moreActions')"><MoreHorizontal /></IconButton>
                    </template>
                    <DropdownItem @click="openSelf('self-leaf')"><Plus />{{ t('certificatesPage.generateLeaf') }}</DropdownItem>
                    <DropdownItem tone="danger" @click="askDelete('self', row.id, row.name)"><Trash2 />{{ t('common.delete') }}</DropdownItem>
                  </Dropdown>
                </div>
              </template>
              <template #empty>
                <EmptyState
                  variant="inset"
                  :title="search ? t('common.filterEmpty') : t('certificatesPage.noSelf')"
                  :description="search ? t('common.filterEmptyHint') : t('certificatesPage.noSelfHint')"
                />
              </template>
            </Table>

            <Table
              v-else
              class="h-full max-lg:max-h-[60dvh]"
              :columns="assetColumns"
              :rows="userAssets"
              row-key="id"
              :active-key="selectedId"
              :loading="loading && !userAssets.length"
              :loading-label="t('common.loading')"
            >
              <template #name="{ row }">
                <TableDetailName class="max-w-[16rem]" :name="row.name" :label="t('common.openDetails', { name: row.name })" @click="openDetail(row.id)" />
              </template>
              <template #type="{ row }">
                <Badge :tone="assetTone(row)">{{ assetTypeLabel(row.type) }}</Badge>
              </template>
              <template #algorithm="{ row }">
                <span class="text-muted-foreground">{{ row.algorithm || t('common.notAvailable') }}</span>
              </template>
              <template #fingerprint="{ row }">
                <span class="panel-mono block max-w-[18rem] truncate text-xs text-muted-foreground">{{ row.fingerprint || t('common.notAvailable') }}</span>
              </template>
              <template #expiresAt="{ row }">
                <span class="text-muted-foreground">{{ formatDateTime(row.notAfter, t('common.never')) }}</span>
              </template>
              <template #actions="{ row }">
                <div class="flex items-center justify-end gap-1">
                  <Button size="sm" variant="secondary" @click="openDetail(row.id)"><Eye />{{ t('common.details') }}</Button>
                  <Button size="sm" variant="ghost" :disabled="(!row.canReissue && !row.canRegenerate) || Boolean(rowPending)" :loading="rowPending === `keys:${row.id}`" @click="reissueAssetRow(row)"><RotateCcw />{{ row.type === 'ssh_key_pair' ? t('certificatesPage.regenerate') : t('certificatesPage.reissue') }}</Button>
                  <Button size="sm" variant="ghost" @click="openExport(row)"><FileArchive />{{ t('certificatesPage.exportAsset') }}</Button>
                  <Dropdown align="right">
                    <template #trigger>
                      <IconButton size="sm" :label="t('common.moreActions')"><MoreHorizontal /></IconButton>
                    </template>
                    <DropdownItem @click="openAsset('asset-import')"><KeyRound />{{ t('certificatesPage.importAsset') }}</DropdownItem>
                    <DropdownItem tone="danger" :disabled="!row.canDelete" @click="askDelete('keys', row.id, row.name, row.canDelete)"><Trash2 />{{ t('common.delete') }}</DropdownItem>
                  </Dropdown>
                </div>
              </template>
              <template #empty>
                <EmptyState
                  variant="inset"
                  :title="search ? t('common.filterEmpty') : t('certificatesPage.noAssets')"
                  :description="search ? t('common.filterEmptyHint') : t('certificatesPage.noAssetsHint')"
                />
              </template>
            </Table>

            <template #pagination>
              <PaginationBar :page="page" :page-size="pageSize" :total="total" :loading="loading" :previous-label="t('common.previous')" :next-label="t('common.next')" @update:page="changePage" />
            </template>
          </ListPage>
        </Tabs>
      </ConsolePage>
    </template>

    <template #detail>
      <ConsolePage :title="detailTitle" :description="detailDescription" :back-label="t('common.backToList')" @back="closeDetail">
        <article v-if="mode === 'domains' && selectedCert" class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
          <section class="rounded-2xl border border-border bg-muted p-4">
            <div class="flex flex-wrap items-center justify-between gap-2">
              <h3 class="m-0 text-sm font-semibold">{{ t('certificatesPage.coverage') }}</h3>
              <ActivityLink resource-type="certificate" :resource-id="selectedCert.id" />
            </div>
            <div class="mt-3 flex flex-wrap gap-2"><Badge v-for="domain in selectedCert.domains" :key="domain" tone="info">{{ domain }}</Badge></div>
            <p v-if="selectedCert.lastError" class="mt-4 rounded-xl border border-danger-border bg-danger-bg p-3 text-sm text-danger">{{ selectedCert.lastError }}</p>
          </section>
          <aside class="grid content-start gap-3">
            <div class="rounded-2xl border border-border bg-muted p-4 text-sm"><div class="text-muted-foreground">{{ t('common.status') }}</div><strong>{{ t(certificateState(selectedCert)) }}</strong></div>
            <div class="rounded-2xl border border-border bg-muted p-4 text-sm"><div class="text-muted-foreground">{{ t('certificatesPage.expiresAt') }}</div><strong>{{ formatDateTime(selectedCert.notAfter) || t('common.notAvailable') }}</strong></div>
            <div class="rounded-2xl border border-border bg-muted p-4 text-sm"><div class="text-muted-foreground">{{ t('certificatesPage.taskEntry') }}</div><ActivityLink resource-type="certificate" :resource-id="selectedCert.id" /></div>
          </aside>
        </article>

        <article v-else-if="mode === 'self' && selectedSelf" class="grid gap-3 md:grid-cols-2">
          <div class="rounded-2xl border border-border bg-muted p-4 text-sm md:col-span-2"><div class="flex flex-wrap items-center justify-between gap-2"><span class="text-muted-foreground">{{ t('common.status') }}</span><ActivityLink resource-type="certificate" :resource-id="selectedSelf.id" /></div></div>
          <div class="rounded-2xl border border-border bg-muted p-4 text-sm"><div class="text-muted-foreground">{{ t('common.type') }}</div><strong>{{ selfSignedKindLabel(selectedSelf.kind) }}</strong></div>
          <div class="rounded-2xl border border-border bg-muted p-4 text-sm"><div class="text-muted-foreground">{{ t('certificatesPage.fingerprint') }}</div><strong class="break-all">{{ selectedSelf.fingerprint }}</strong></div>
          <div class="rounded-2xl border border-border bg-muted p-4 text-sm"><div class="text-muted-foreground">{{ t('certificatesPage.expiresAt') }}</div><strong>{{ formatDateTime(selectedSelf.notAfter) }}</strong></div>
          <div class="rounded-2xl border border-border bg-muted p-4 text-sm"><div class="text-muted-foreground">{{ t('certificatesPage.dnsNames') }}</div><strong>{{ displayEntries(selectedSelf.dnsNames) }}</strong></div>
        </article>

        <article v-else-if="mode === 'keys' && selectedAsset" class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
          <section class="grid content-start gap-3 md:grid-cols-2">
            <div class="rounded-2xl border border-border bg-muted p-4 text-sm md:col-span-2"><div class="flex flex-wrap items-center justify-between gap-2"><span class="text-muted-foreground">{{ t('common.status') }}</span><ActivityLink resource-type="key_asset" :resource-id="selectedAsset.id" /></div></div>
            <div class="rounded-2xl border border-border bg-muted p-4 text-sm"><div class="text-muted-foreground">{{ t('certificatesPage.fingerprint') }}</div><strong class="break-all">{{ selectedAsset.fingerprint }}</strong></div>
            <div class="rounded-2xl border border-border bg-muted p-4 text-sm"><div class="text-muted-foreground">{{ t('certificatesPage.references') }}</div><strong>{{ selectedAsset.referenceCount }}</strong></div>
            <div class="rounded-2xl border border-border bg-muted p-4 text-sm"><div class="text-muted-foreground">{{ t('certificatesPage.children') }}</div><strong>{{ selectedAsset.childCount }}</strong></div>
            <div class="rounded-2xl border border-border bg-muted p-4 text-sm"><div class="text-muted-foreground">{{ t('certificatesPage.expiresAt') }}</div><strong>{{ formatDateTime(selectedAsset.notAfter) || t('common.notAvailable') }}</strong></div>
          </section>
          <aside class="rounded-2xl border border-border bg-muted p-4">
            <h3 class="m-0 text-sm font-semibold">{{ t('certificatesPage.downloads') }}</h3>
            <div class="mt-3 grid gap-2">
              <Button v-for="kind in selectedAsset.downloadKinds" :key="kind" size="sm" :loading="saving" @click="downloadAssetFile(selectedAsset, kind)"><Download />{{ fileKindLabel(kind) }}</Button>
            </div>
          </aside>
        </article>
      </ConsolePage>
    </template>
  </TableDetailLayout>

  <Dialog :open="dialog === 'issue'" :title="editingCertificateId ? t('certificatesPage.adjustReissue') : t('certificatesPage.issue')" :close-label="t('common.close')" @update:open="(open: boolean) => { if (!open) dialog = '' }">
    <div class="grid gap-4">
      <label class="grid gap-1 text-sm">
        {{ t('common.name') }}
        <Input v-model="issueForm.name" :invalid="Boolean(issueFormErrors.name)" @input="delete issueFormErrors.name" />
        <span v-if="issueFormErrors.name" class="text-xs text-danger">{{ t(issueFormErrors.name) }}</span>
      </label>
      <label class="grid gap-1 text-sm">
        {{ t('routes.dns.title') }}
        <Select v-model="issueForm.domainId" :options="domainOptions" />
        <span v-if="issueFormErrors.domainId" class="text-xs text-danger">{{ t(issueFormErrors.domainId) }}</span>
      </label>
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
        <span v-if="issueFormErrors.coverage" class="text-xs text-danger">{{ t(issueFormErrors.coverage) }}</span>
      </section>
      <section class="rounded-xl border border-border bg-muted p-3">
        <div class="text-sm font-medium">{{ t('certificatesPage.coveragePreview') }}</div>
        <div class="mt-2 flex flex-wrap gap-2"><Badge v-for="domain in issueCoverage" :key="domain" tone="info">{{ domain }}</Badge></div>
      </section>
    </div>
    <template #footer><Button @click="dialog = ''">{{ t('common.cancel') }}</Button><Button variant="primary" :loading="saving" :disabled="!issueFormValid" @click="issueCertificate">{{ editingCertificateId ? t('certificatesPage.adjustReissue') : t('certificatesPage.issue') }}</Button></template>
  </Dialog>

  <Dialog :open="dialog === 'self-ca' || dialog === 'self-leaf'" :title="dialog === 'self-ca' ? t('certificatesPage.generateCa') : t('certificatesPage.generateLeaf')" :close-label="t('common.close')" @update:open="(open: boolean) => { if (!open) dialog = '' }">
    <div class="grid gap-3">
      <label class="grid gap-1 text-sm">
        {{ t('common.name') }}
        <Input v-model="selfForm.name" :invalid="Boolean(selfFormErrors.name)" @input="delete selfFormErrors.name" />
        <span v-if="selfFormErrors.name" class="text-xs text-danger">{{ t(selfFormErrors.name) }}</span>
      </label>
      <label v-if="dialog === 'self-leaf'" class="grid gap-1 text-sm">
        {{ t('certificatesPage.kindCa') }}
        <Select v-model="selfForm.caId" :options="selfCas" />
        <span v-if="selfFormErrors.caId" class="text-xs text-danger">{{ t(selfFormErrors.caId) }}</span>
      </label>
      <label class="grid gap-1 text-sm">
        {{ t('certificatesPage.commonName') }}
        <Input v-model="selfForm.commonName" :invalid="Boolean(selfFormErrors.commonName)" @input="delete selfFormErrors.commonName" />
        <span v-if="selfFormErrors.commonName" class="text-xs text-danger">{{ t(selfFormErrors.commonName) }}</span>
      </label>
      <div v-if="dialog === 'self-leaf'" class="grid gap-2"><div class="text-sm font-medium">{{ t('certificatesPage.dnsNames') }}</div><div v-for="(_, index) in selfForm.dnsNames" :key="index" class="grid grid-cols-[minmax(0,1fr)_auto] gap-2"><Input v-model="selfForm.dnsNames[index]" /><Button size="sm" variant="ghost" @click="removeEntry(selfForm.dnsNames, index)"><Trash2 />{{ t('common.delete') }}</Button></div><Button size="sm" @click="addEntry(selfForm.dnsNames)"><Plus />{{ t('certificatesPage.addDnsName') }}</Button></div>
      <div v-if="dialog === 'self-leaf'" class="grid gap-2"><div class="text-sm font-medium">{{ t('certificatesPage.ipAddresses') }}</div><div v-for="(_, index) in selfForm.ipAddresses" :key="index" class="grid grid-cols-[minmax(0,1fr)_auto] gap-2"><Input v-model="selfForm.ipAddresses[index]" /><Button size="sm" variant="ghost" @click="removeEntry(selfForm.ipAddresses, index)"><Trash2 />{{ t('common.delete') }}</Button></div><Button size="sm" @click="addEntry(selfForm.ipAddresses)"><Plus />{{ t('certificatesPage.addIpAddress') }}</Button></div>
      <label v-if="dialog === 'self-ca'" class="grid gap-1 text-sm">{{ t('certificatesPage.years') }}<Input v-model="selfForm.years" type="number" /></label>
      <label v-else class="grid gap-1 text-sm">{{ t('certificatesPage.days') }}<Input v-model="selfForm.days" type="number" /></label>
    </div>
    <template #footer><Button @click="dialog = ''">{{ t('common.cancel') }}</Button><Button variant="primary" :loading="saving" :disabled="!selfFormValid" @click="saveSelf">{{ t('common.create') }}</Button></template>
  </Dialog>

  <Dialog :open="dialog.startsWith('asset-') && !['asset-export','asset-preflight'].includes(dialog)" :size="dialog === 'asset-import' ? 'large' : 'default'" :title="t('certificatesPage.assetForm')" :close-label="t('common.close')" @update:open="(open: boolean) => { if (!open) dialog = '' }">
    <div class="grid gap-3" :class="dialog === 'asset-import' ? 'h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] grid-cols-[minmax(0,1fr)]' : ''">
      <div :class="dialog === 'asset-import' ? 'grid gap-3 md:grid-cols-2' : 'contents'">
        <label v-if="dialog === 'asset-import'" class="grid gap-1 text-sm">{{ t('common.type') }}<Select v-model="assetForm.type" :options="assetTypeOptions" /></label>
        <label class="grid gap-1 text-sm">
          {{ t('common.name') }}
          <Input v-model="assetForm.name" :invalid="Boolean(assetFormErrors.name)" @input="delete assetFormErrors.name" />
          <span v-if="assetFormErrors.name" class="text-xs text-danger">{{ t(assetFormErrors.name) }}</span>
        </label>
        <label v-if="dialog === 'asset-tls' || (dialog === 'asset-import' && assetForm.type === 'tls_certificate')" class="grid gap-1 text-sm">
          {{ t('certificatesPage.kindCa') }}
          <Select v-model="assetForm.parentAssetId" :options="caAssets" />
          <span v-if="assetFormErrors.parentAssetId" class="text-xs text-danger">{{ t(assetFormErrors.parentAssetId) }}</span>
        </label>
        <label v-if="dialog !== 'asset-ssh'" class="grid gap-1 text-sm">
          {{ t('certificatesPage.commonName') }}
          <Input v-model="assetForm.commonName" :invalid="Boolean(assetFormErrors.commonName)" @input="delete assetFormErrors.commonName" />
          <span v-if="assetFormErrors.commonName" class="text-xs text-danger">{{ t(assetFormErrors.commonName) }}</span>
        </label>
        <label class="grid gap-1 text-sm">{{ t('certificatesPage.algorithm') }}<Select v-model="assetForm.algorithm" :options="algorithmOptions" /></label>
        <label v-if="assetForm.algorithm === 'rsa'" class="grid gap-1 text-sm">{{ t('certificatesPage.keySize') }}<Input v-model="assetForm.keySize" type="number" /></label>
      </div>
      <div v-if="dialog === 'asset-import'" class="min-h-0">
        <label v-if="!assetImportHasCertificate(assetForm.type)" class="grid h-full min-h-0 grid-cols-[minmax(0,1fr)] grid-rows-[auto_minmax(0,1fr)] gap-1 text-sm">
          {{ t('certificatesPage.privateKeyPem') }}
          <CodeEditor v-model="assetForm.privateKeyPem" language="plain" :editor-label="t('certificatesPage.privateKeyPem')" />
        </label>
        <Tabs v-else v-model="assetImportMaterialTab" class="h-full" :tabs="assetImportMaterialTabs">
          <label v-if="assetImportMaterialTab === 'privateKey'" class="grid h-full min-h-0 grid-cols-[minmax(0,1fr)] grid-rows-[auto_minmax(0,1fr)] gap-1 text-sm">
            {{ t('certificatesPage.privateKeyPem') }}
            <CodeEditor v-model="assetForm.privateKeyPem" language="plain" :editor-label="t('certificatesPage.privateKeyPem')" />
          </label>
          <label v-else class="grid h-full min-h-0 grid-cols-[minmax(0,1fr)] grid-rows-[auto_minmax(0,1fr)] gap-1 text-sm">
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
    <template #footer><Button @click="dialog = ''">{{ t('common.cancel') }}</Button><Button variant="primary" :loading="saving" :disabled="!assetFormValid" @click="saveAsset">{{ t('common.save') }}</Button></template>
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

  <ConfirmDialog
    :open="Boolean(deleteTarget)"
    :title="t('certificatesPage.deleteTitle')"
    :description="deleteTarget?.name ?? ''"
    :impact="t('certificatesPage.deleteHint')"
    tone="danger"
    :loading="saving"
    :confirm-label="t('common.delete')"
    :cancel-label="t('common.cancel')"
    :checkbox-label="t('serversPage.confirmCheckbox')"
    @update:open="(value) => { if (!value) deleteTarget = null; }"
    @confirm="confirmDeleteAction"
    @cancel="deleteTarget = null"
  />
</template>
