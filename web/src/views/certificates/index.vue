<script setup lang="ts">
import ActivityLink from '@/components/activity/ActivityLink.vue';
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { ChevronDown, Eye, FileArchive, KeyRound, MoreHorizontal, Plus, RefreshCcw, RotateCcw, Trash2, Wrench } from '@/icons';
import { certificatesApi } from '@/api/certificates';
import { waitForTask } from '@/api/taskWait';
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
import StatusBadge from '@/components/ui/StatusBadge.vue';
import DownloadButton from '@/components/ui/DownloadButton.vue';
import NumberBox from '@/components/ui/NumberBox.vue';
import Skeleton from '@/components/ui/Skeleton.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import DetailSection from '@/components/patterns/DetailSection.vue';
import FormField from '@/components/patterns/FormField.vue';
import InfoBar from '@/components/patterns/InfoBar.vue';
import ListCommandBar from '@/components/patterns/ListCommandBar.vue';
import PropertyList from '@/components/patterns/PropertyList.vue';
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
const assetActionError = ref('');
const dialog = ref<'issue' | 'self-ca' | 'self-leaf' | 'asset-ca' | 'asset-tls' | 'asset-ssh' | 'asset-import' | 'asset-export' | 'asset-preflight' | ''>('');
const saving = ref(false);
const importPlan = ref<ImportPreflightDto | null>(null);
const editingCertificateId = ref('');
const page = ref(parsePageQuery(route.query.page));
const pageSize = 50;
const search = ref(String(route.query.search ?? ''));
let searchTimer: ReturnType<typeof setTimeout> | undefined;
const importConfirmOpen = ref(false);
const exportTargetId = ref('');
const deleteTarget = ref<{ kind: 'domains' | 'self' | 'keys'; id: string; name: string } | null>(null);
let taskWaitController = new AbortController();
onBeforeUnmount(() => taskWaitController.abort());

function parsePageQuery(value: unknown) {
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed > 0 ? parsed : 1;
}

const mode = computed(() => route.path.includes('/self-signed') ? 'self' : route.path.includes('/keys') ? 'keys' : 'domains');
type CertificateMode = 'domains' | 'self' | 'keys';
/** 每个 tab 独立加载状态/缓存：首屏只取当前 mode，切回已加载且筛选未变的分区不再请求。 */
const modeLoaded = reactive<Record<CertificateMode, boolean>>({ domains: false, self: false, keys: false });
const modeLoadedSearch = reactive<Record<CertificateMode, string>>({ domains: '', self: '', keys: '' });
const modeLoadedPage = reactive<Record<CertificateMode, number>>({ domains: 1, self: 1, keys: 1 });
const modeErrors = reactive<Record<CertificateMode, string>>({ domains: '', self: '', keys: '' });
const totals = reactive<Record<CertificateMode, number>>({ domains: 0, self: 0, keys: 0 });
const error = computed({
  get: () => modeErrors[mode.value],
  set: (value: string) => { modeErrors[mode.value] = value; },
});
const total = computed(() => totals[mode.value]);
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
const breadcrumbs = computed(() => [
  { label: title.value, to: modePaths[mode.value] },
  { label: detailTitle.value },
]);
const currentRows = computed(() => mode.value === 'self' ? selfSigned.value : mode.value === 'keys' ? userAssets.value : certs.value);
/** ListPage 页级状态槽：首屏 loading / 页级 error / 真·空态走槽；筛选无结果仍由 Table #empty（inset）承担。 */
const listState = computed<'ready' | 'loading' | 'error' | 'empty'>(() => {
  if (!modeLoaded[mode.value] && !error.value) return 'loading';
  if (loading.value && !currentRows.value.length) return 'loading';
  if (error.value && !currentRows.value.length) return 'error';
  if (!loading.value && !error.value && !currentRows.value.length && !search.value.trim()) return 'empty';
  return 'ready';
});
const emptyTitle = computed(() => mode.value === 'self'
  ? t('certificatesPage.noSelf')
  : mode.value === 'keys' ? t('certificatesPage.noAssets') : t('certificatesPage.noCertificates'));
const emptyHint = computed(() => mode.value === 'self'
  ? t('certificatesPage.noSelfHint')
  : mode.value === 'keys' ? t('certificatesPage.noAssetsHint') : t('certificatesPage.noCertificatesHint'));
const detailTitle = computed(() => selectedCert.value?.name ?? selectedSelf.value?.name ?? selectedAsset.value?.name ?? t('common.loading'));
const detailDescription = computed(() => {
  if (mode.value === 'domains' && selectedCert.value) return selectedCert.value.domain;
  if (mode.value === 'self' && selectedSelf.value) return selectedSelf.value.commonName;
  if (mode.value === 'keys' && selectedAsset.value) return `${assetTypeLabel(selectedAsset.value.type)} / ${selectedAsset.value.algorithm}`;
  return '';
});
/** 三种模式详情统一为只读属性行；issuer 只在正文属性中出现，PageHeader 仅保留域名身份。 */
const domainDetailItems = computed(() => {
  const certificate = selectedCert.value;
  if (!certificate) return [];
  return [
    { key: 'status', label: t('common.status'), value: t(certificateState(certificate)) },
    { key: 'expiresAt', label: t('certificatesPage.expiresAt'), value: formatDateTime(certificate.notAfter, t('common.notAvailable')) },
    { key: 'issuer', label: t('certificatesPage.issuer'), value: certificate.issuer },
    { key: 'coverage', label: t('certificatesPage.coverage'), value: certificate.domains.join(', ') },
  ];
});
const selfDetailItems = computed(() => {
  const certificate = selectedSelf.value;
  if (!certificate) return [];
  return [
    { key: 'type', label: t('common.type'), value: selfSignedKindLabel(certificate.kind) },
    { key: 'fingerprint', label: t('certificatesPage.fingerprint'), value: certificate.fingerprint, mono: true },
    { key: 'expiresAt', label: t('certificatesPage.expiresAt'), value: formatDateTime(certificate.notAfter, t('common.notAvailable')) },
    { key: 'dnsNames', label: t('certificatesPage.dnsNames'), value: displayEntries(certificate.dnsNames) },
    { key: 'ipAddresses', label: t('certificatesPage.ipAddresses'), value: displayEntries(certificate.ipAddresses) },
  ];
});
/** 详情 ≥wide 两列：coverage 与属性/状态并排；短属性 PropertyList 单列以免半列内再拆列。 */
const coverageDetailItems = computed(() => domainDetailItems.value.filter((item) => item.key === 'coverage'));
const certificatePropertyItems = computed(() => domainDetailItems.value.filter((item) => item.key !== 'coverage'));
const selfIdentityItems = computed(() => selfDetailItems.value.filter((item) => ['type', 'fingerprint', 'expiresAt'].includes(item.key ?? '')));
const selfNameItems = computed(() => selfDetailItems.value.filter((item) => !['type', 'fingerprint', 'expiresAt'].includes(item.key ?? '')));
const assetDetailItems = computed(() => {
  const asset = selectedAsset.value;
  if (!asset) return [];
  return [
    { key: 'type', label: t('common.type'), value: assetTypeLabel(asset.type) },
    { key: 'algorithm', label: t('certificatesPage.algorithm'), value: asset.algorithm || t('common.notAvailable') },
    { key: 'fingerprint', label: t('certificatesPage.fingerprint'), value: asset.fingerprint || t('common.notAvailable'), mono: true },
    { key: 'references', label: t('certificatesPage.references'), value: asset.referenceCount },
    { key: 'children', label: t('certificatesPage.children'), value: asset.childCount },
    { key: 'expiresAt', label: t('certificatesPage.expiresAt'), value: formatDateTime(asset.notAfter, t('common.notAvailable')) },
  ];
});
const issueForm = reactive({ name: '', domainId: '', includeRoot: true, includeWildcard: false, subdomains: [''] });
const selfForm = reactive({ name: '', caId: '', commonName: '', dnsNames: [''], ipAddresses: [''], years: '5', days: '90' });
const assetForm = reactive({ type: 'ssh_key_pair' as KeyAssetType, name: '', parentAssetId: '', commonName: '', dnsNames: [''], ipAddresses: [''], algorithm: 'ed25519', keySize: '2048', validityDays: '365', comment: '', certificatePem: '', privateKeyPem: '', publicKey: '', password: '' });
const issueFormErrors = reactive<FieldErrors>({});
const selfFormErrors = reactive<FieldErrors>({});
const assetFormErrors = reactive<FieldErrors>({});
/** NumberBox 使用 number|null；表格单仍是字符串，保持校验/负载逻辑。 */
function toNumber(value: string): number | null {
  const raw = value.trim();
  if (!raw) return null;
  const parsed = Number(raw);
  return Number.isFinite(parsed) ? parsed : null;
}
function fromNumber(value: number | null): string {
  return value === null ? '' : String(value);
}
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
  { key: 'name', label: t('common.name'), width: 'w-72' },
  { key: 'status', label: t('common.status'), nowrap: true },
  { key: 'expiresAt', label: t('certificatesPage.expiresAt'), nowrap: true, width: 'w-36', hideBelow: 'wide' as const },
  { key: 'actions', label: t('common.actions'), align: 'right' as const, nowrap: true },
]);
const selfColumns = computed(() => [
  { key: 'name', label: t('common.name'), width: 'w-72' },
  { key: 'kind', label: t('common.type'), nowrap: true },
  { key: 'expiresAt', label: t('certificatesPage.expiresAt'), nowrap: true, width: 'w-36', hideBelow: 'wide' as const },
  { key: 'actions', label: t('common.actions'), align: 'right' as const, nowrap: true },
]);
const assetColumns = computed(() => [
  { key: 'name', label: t('common.name'), width: 'w-72' },
  { key: 'type', label: t('common.type'), nowrap: true },
  { key: 'expiresAt', label: t('certificatesPage.expiresAt'), nowrap: true, width: 'w-36', hideBelow: 'wide' as const },
  { key: 'actions', label: t('common.actions'), align: 'right' as const, nowrap: true },
]);

onMounted(() => { void ensureModeLoaded(); });
watch(() => route.path, () => { void ensureModeLoaded(); });

watch(() => route.query.selected, (value) => {
  const next = String(value ?? '');
  if (next !== selectedId.value) selectedId.value = next;
});

function setAssetFormType(value: string) {
  assetForm.type = value as KeyAssetType;
}

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

async function ensureModeLoaded(target: CertificateMode = mode.value) {
  const query = search.value.trim();
  if (modeLoaded[target] && modeLoadedSearch[target] === query && modeLoadedPage[target] === page.value) return;
  await loadMode(target);
}

/**
 * 按 tab 懒加载：每个 mode 只请求自己需要的列表（domains 额外取 DNS 域名用于签发），
 * 首屏加载一次；切回筛选/页码未变的分区直接复用缓存，不再重取全部 4 个列表。
 */
async function loadMode(target: CertificateMode = mode.value) {
  const requestId = listRequests.begin();
  loading.value = true;
  modeErrors[target] = '';
  const q = search.value.trim() || undefined;
  try {
    let firstError = '';
    if (target === 'domains') {
      const [domainsResult, certsResult] = await Promise.allSettled([
        dnsApi.listDomains(),
        certificatesApi.list({ page: page.value, pageSize, q }),
      ]);
      if (!listRequests.isCurrent(requestId)) return;
      if (domainsResult.status === 'fulfilled') {
        domains.value = Array.isArray(domainsResult.value) ? domainsResult.value : [];
      } else {
        firstError = domainsResult.reason instanceof Error ? domainsResult.reason.message : t('certificatesPage.loadFailed');
      }
      if (certsResult.status === 'fulfilled') {
        certs.value = certsResult.value.items.map(normalizeDomainCertificate);
        totals.domains = certsResult.value.total;
      } else if (!firstError) {
        firstError = certsResult.reason instanceof Error ? certsResult.reason.message : t('certificatesPage.loadFailed');
      }
    } else if (target === 'self') {
      const [selfResult] = await Promise.allSettled([
        certificatesApi.listSelfSignedPage({ page: page.value, pageSize, q }),
      ]);
      if (!listRequests.isCurrent(requestId)) return;
      if (selfResult.status === 'fulfilled') {
        selfSigned.value = selfResult.value.items.map(normalizeSelfSignedCertificate);
        totals.self = selfResult.value.total;
      } else {
        firstError = selfResult.reason instanceof Error ? selfResult.reason.message : t('certificatesPage.loadFailed');
      }
    } else {
      const [assetsResult] = await Promise.allSettled([
        keyAssetsApi.listPage({ page: page.value, pageSize, q }),
      ]);
      if (!listRequests.isCurrent(requestId)) return;
      if (assetsResult.status === 'fulfilled') {
        assets.value = assetsResult.value.items.map(normalizeKeyAsset);
        totals.keys = assetsResult.value.total;
      } else {
        firstError = assetsResult.reason instanceof Error ? assetsResult.reason.message : t('certificatesPage.loadFailed');
      }
    }

    modeLoaded[target] = true;
    modeLoadedSearch[target] = search.value.trim();
    modeLoadedPage[target] = page.value;
    if (target === mode.value) {
      const visibleAssets = assets.value.filter((item) => !isSystemManagedAsset(item));
      const list = target === 'self' ? selfSigned.value : target === 'keys' ? visibleAssets : certs.value;
      if (!route.query.selected && selectedId.value && !list.some((item) => item.id === selectedId.value)) {
        selectedId.value = '';
      }
    }
    if (firstError) {
      modeErrors[target] = firstError;
      notifyError(firstError);
    }
  } catch (err) {
    modeErrors[target] = err instanceof Error ? err.message : t('certificatesPage.loadFailed');
    notifyError(err instanceof Error ? err.message : t('certificatesPage.loadFailed'), err);
  } finally {
    if (listRequests.isCurrent(requestId)) loading.value = false;
  }
}

/** 刷新动作只重取当前 tab（不再一次拉全部 4 个列表）。 */
function load() {
  return loadMode(mode.value);
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
    if (result.taskId) await waitForTask(result.taskId, 90_000, taskWaitController.signal);
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
    if (result.taskId) await waitForTask(result.taskId, 90_000, taskWaitController.signal);
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
    if (result.taskId) await waitForTask(result.taskId, 90_000, taskWaitController.signal);
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
    if (result.taskId) await waitForTask(result.taskId, 90_000, taskWaitController.signal);
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
      <ConsolePage width="content" :title="title" :description="description">
        <Tabs
          class="h-full min-h-0"
          :model-value="mode"
          :tabs="modeTabs"
          keep-panels
          @update:model-value="switchMode"
        >
          <ListPage :state="listState">
            <template #toolbar>
              <ListCommandBar :label="title">
                <template #search>
                  <SearchInput v-model="search" clearable :label="t('common.search')" :placeholder="t('certificatesPage.searchPlaceholder')" :clear-label="t('common.clearSearch')" />
                </template>
                <template #actions>
                  <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
                  <template v-if="mode === 'domains'">
                    <Button size="sm" variant="primary" @click="openIssue"><Plus />{{ t('certificatesPage.issue') }}</Button>
                  </template>
                  <template v-else-if="mode === 'self'">
                    <Button size="sm" variant="primary" @click="openSelf('self-ca')"><Plus />{{ t('certificatesPage.generateCa') }}</Button>
                  </template>
                  <template v-else>
                    <Button size="sm" variant="primary" @click="openAsset('asset-ssh')"><KeyRound />{{ t('certificatesPage.generateSsh') }}</Button>
                    <Dropdown align="right">
                      <template #trigger>
                        <Button size="sm"><Plus />{{ t('certificatesPage.new') }}<ChevronDown /></Button>
                      </template>
                      <DropdownItem @click="openAsset('asset-ca')"><Plus />{{ t('certificatesPage.generateCa') }}</DropdownItem>
                      <DropdownItem @click="openAsset('asset-tls')"><Plus />{{ t('certificatesPage.generateTls') }}</DropdownItem>
                      <DropdownItem @click="openBatchImport"><FileArchive />{{ t('certificatesPage.batchImport') }}</DropdownItem>
                    </Dropdown>
                  </template>
                </template>
              </ListCommandBar>
            </template>

            <template #loading>
              <div class="grid gap-2" aria-busy="true">
                <Skeleton v-for="row in 6" :key="row" class="h-10 w-full" />
              </div>
            </template>

            <template #error>
              <EmptyState variant="page" :title="t('common.loadFailed')" :description="error">
                <template #actions>
                  <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button>
                </template>
              </EmptyState>
            </template>

            <template #empty>
              <EmptyState variant="page" :title="emptyTitle" :description="emptyHint" />
            </template>

            <Table
              v-if="mode === 'domains'"
              class="h-full max-lg:max-h-list"
              :columns="domainColumns"
              :rows="certs"
              row-key="id"
              :active-key="selectedId"
              :loading="loading && !certs.length"
              :loading-label="t('common.loading')"
            >
              <template #name="{ row }">
                <TableDetailName class="max-w-64" :name="row.name" :label="t('common.openDetails', { name: row.name })" @click="openDetail(row.id)" />
                <span class="block max-w-64 truncate text-xs text-muted-foreground" :title="`${row.domains.join(', ')} · ${row.issuer}`">
                  {{ row.domains.join(', ') }}<template v-if="row.issuer"> · {{ row.issuer }}</template>
                </span>
              </template>
              <template #status="{ row }">
                <StatusBadge :status="certificateState(row)" :tone="certificateTone(row)" :label="t(certificateState(row))" />
              </template>
              <template #expiresAt="{ row }">
                <span class="text-muted-foreground">{{ formatDateTime(row.notAfter, t('common.never')) }}</span>
              </template>
              <template #actions="{ row }">
                <div class="flex items-center justify-end gap-1">
                  <Button size="sm" variant="secondary" @click="openDetail(row.id)"><Eye />{{ t('common.details') }}</Button>
                  <Button class="max-wide:hidden" size="sm" variant="ghost" :loading="rowPending === `domains:${row.id}`" :disabled="Boolean(rowPending)" @click="renewDomainRow(row)"><RotateCcw />{{ t('certificatesPage.renew') }}</Button>
                  <Dropdown align="right">
                    <template #trigger>
                      <IconButton size="sm" :label="t('common.moreActions')"><MoreHorizontal /></IconButton>
                    </template>
                    <DropdownItem @click="renewDomainRow(row)"><RotateCcw />{{ t('certificatesPage.renew') }}</DropdownItem>
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
              class="h-full max-lg:max-h-list"
              :columns="selfColumns"
              :rows="selfSigned"
              row-key="id"
              :active-key="selectedId"
              :loading="loading && !selfSigned.length"
              :loading-label="t('common.loading')"
            >
              <template #name="{ row }">
                <TableDetailName class="max-w-64" :name="row.name" :label="t('common.openDetails', { name: row.name })" @click="openDetail(row.id)" />
                <span class="block max-w-64 truncate text-xs text-muted-foreground" :title="row.commonName">{{ row.commonName }}</span>
              </template>
              <template #kind="{ row }">
                <Badge :tone="selfSignedTone(row)">{{ selfSignedKindLabel(row.kind) }}</Badge>
              </template>
              <template #expiresAt="{ row }">
                <span class="text-muted-foreground">{{ formatDateTime(row.notAfter, t('common.never')) }}</span>
              </template>
              <template #actions="{ row }">
                <div class="flex items-center justify-end gap-1">
                  <Button size="sm" variant="secondary" @click="openDetail(row.id)"><Eye />{{ t('common.details') }}</Button>
                  <Button class="max-wide:hidden" size="sm" variant="ghost" :loading="rowPending === `self:${row.id}`" :disabled="Boolean(rowPending)" @click="renewSelfRow(row)"><RotateCcw />{{ t('certificatesPage.reissue') }}</Button>
                  <Dropdown align="right">
                    <template #trigger>
                      <IconButton size="sm" :label="t('common.moreActions')"><MoreHorizontal /></IconButton>
                    </template>
                    <DropdownItem @click="renewSelfRow(row)"><RotateCcw />{{ t('certificatesPage.reissue') }}</DropdownItem>
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
              class="h-full max-lg:max-h-list"
              :columns="assetColumns"
              :rows="userAssets"
              row-key="id"
              :active-key="selectedId"
              :loading="loading && !userAssets.length"
              :loading-label="t('common.loading')"
            >
              <template #name="{ row }">
                <TableDetailName class="max-w-64" :name="row.name" :label="t('common.openDetails', { name: row.name })" @click="openDetail(row.id)" />
                <span class="block max-w-72 truncate text-xs text-muted-foreground" :title="row.fingerprint">
                  {{ row.algorithm || t('common.notAvailable') }}<template v-if="row.fingerprint"> · {{ row.fingerprint }}</template>
                </span>
              </template>
              <template #type="{ row }">
                <Badge :tone="assetTone(row)">{{ assetTypeLabel(row.type) }}</Badge>
              </template>
              <template #expiresAt="{ row }">
                <span class="text-muted-foreground">{{ formatDateTime(row.notAfter, t('common.never')) }}</span>
              </template>
              <template #actions="{ row }">
                <div class="flex items-center justify-end gap-1">
                  <Button size="sm" variant="secondary" @click="openDetail(row.id)"><Eye />{{ t('common.details') }}</Button>
                  <Button class="max-wide:hidden" size="sm" variant="ghost" :disabled="(!row.canReissue && !row.canRegenerate) || Boolean(rowPending)" :loading="rowPending === `keys:${row.id}`" @click="reissueAssetRow(row)"><RotateCcw />{{ row.type === 'ssh_key_pair' ? t('certificatesPage.regenerate') : t('certificatesPage.reissue') }}</Button>
                  <Dropdown align="right">
                    <template #trigger>
                      <IconButton size="sm" :label="t('common.moreActions')"><MoreHorizontal /></IconButton>
                    </template>
                    <DropdownItem :disabled="(!row.canReissue && !row.canRegenerate) || Boolean(rowPending)" @click="reissueAssetRow(row)"><RotateCcw />{{ row.type === 'ssh_key_pair' ? t('certificatesPage.regenerate') : t('certificatesPage.reissue') }}</DropdownItem>
                    <DropdownItem @click="openExport(row)"><FileArchive />{{ t('certificatesPage.exportAsset') }}</DropdownItem>
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
      <ConsolePage width="content" :title="detailTitle" :description="detailDescription" :breadcrumbs="breadcrumbs" :back-label="t('common.backToList')" :body-scroll="false" @back="closeDetail">
        <div class="grid h-full min-h-0 content-start gap-4 overflow-y-auto pr-1">
          <div v-if="mode === 'domains' && selectedCert" class="grid content-start gap-4 lg:grid-cols-2">
            <DetailSection :title="t('certificatesPage.coverage')">
              <template #actions>
                <ActivityLink resource-type="certificate" :resource-id="selectedCert.id" />
              </template>
              <PropertyList :items="coverageDetailItems" :columns="1" />
              <template v-if="selectedCert.lastError" #footer>
                <InfoBar tone="danger">{{ selectedCert.lastError }}</InfoBar>
              </template>
            </DetailSection>
            <DetailSection :title="t('common.details')">
              <PropertyList :items="certificatePropertyItems" :columns="1">
                <template #value="{ item, text }">
                  <StatusBadge
                    v-if="item.key === 'status'"
                    :status="certificateState(selectedCert)"
                    :tone="certificateTone(selectedCert)"
                    :label="text"
                  />
                  <span v-else class="min-w-0 break-all">{{ text }}</span>
                </template>
              </PropertyList>
            </DetailSection>
          </div>

          <div v-else-if="mode === 'self' && selectedSelf" class="grid content-start gap-4 lg:grid-cols-2">
            <DetailSection :title="t('common.details')">
              <template #actions>
                <ActivityLink resource-type="certificate" :resource-id="selectedSelf.id" />
              </template>
              <PropertyList :items="selfIdentityItems" :columns="1" />
            </DetailSection>
            <DetailSection :title="t('certificatesPage.dnsNames')">
              <PropertyList :items="selfNameItems" :columns="1" />
            </DetailSection>
          </div>

          <div v-else-if="mode === 'keys' && selectedAsset" class="grid content-start gap-4 lg:grid-cols-2">
            <DetailSection :title="t('common.details')">
              <template #actions>
                <ActivityLink resource-type="key_asset" :resource-id="selectedAsset.id" />
              </template>
              <PropertyList :items="assetDetailItems" :columns="1" />
            </DetailSection>
            <DetailSection :title="t('certificatesPage.downloads')">
              <div class="flex flex-wrap gap-2 px-4 py-3">
                <DownloadButton
                  v-for="kind in selectedAsset.downloadKinds"
                  :key="kind"
                  size="sm"
                  :loading="saving"
                  :label="fileKindLabel(kind)"
                  @click="downloadAssetFile(selectedAsset, kind)"
                />
              </div>
            </DetailSection>
          </div>
        </div>
      </ConsolePage>
    </template>
  </TableDetailLayout>

  <Dialog :open="dialog === 'issue'" :title="editingCertificateId ? t('certificatesPage.adjustReissue') : t('certificatesPage.issue')" :close-label="t('common.close')" @update:open="(open: boolean) => { if (!open) dialog = '' }">
    <div class="grid gap-4">
      <div class="grid gap-3 sm:grid-cols-2">
        <FormField :label="t('common.name')" required :error="issueFormErrors.name ? t(issueFormErrors.name) : ''">
          <template #default="{ id, describedBy, invalid }">
            <Input :id="id" :aria-describedby="describedBy" :invalid="invalid" :model-value="issueForm.name" @update:model-value="issueForm.name = $event; delete issueFormErrors.name" />
          </template>
        </FormField>
        <FormField :label="t('routes.dns.title')" required :error="issueFormErrors.domainId ? t(issueFormErrors.domainId) : ''">
          <template #default="{ id, describedBy, invalid }">
            <Select :id="id" :aria-describedby="describedBy" :aria-invalid="invalid || undefined" :invalid="invalid" :model-value="issueForm.domainId" :options="domainOptions" @update:model-value="issueForm.domainId = $event; delete issueFormErrors.domainId" />
          </template>
        </FormField>
      </div>
      <div class="grid gap-3">
        <FormField :label="t('certificatesPage.rootDomain')">
          <template #default="{ id }">
            <Switch :id="id" :model-value="issueForm.includeRoot" :label="t('certificatesPage.rootDomain')" @update:model-value="issueForm.includeRoot = $event" />
          </template>
        </FormField>
        <FormField :label="t('certificatesPage.wildcardDomain')">
          <template #default="{ id }">
            <Switch :id="id" :model-value="issueForm.includeWildcard" :label="t('certificatesPage.wildcardDomain')" @update:model-value="issueForm.includeWildcard = $event" />
          </template>
        </FormField>
        <FormField :label="t('certificatesPage.subdomains')" :error="issueFormErrors.coverage ? t(issueFormErrors.coverage) : ''">
          <div class="grid gap-2">
            <div v-for="(_, index) in issueForm.subdomains" :key="index" class="grid grid-cols-field gap-2">
              <Input v-model="issueForm.subdomains[index]" :aria-label="t('certificatesPage.subdomains')" :placeholder="t('certificatesPage.subdomainPlaceholder')" />
              <Button size="sm" variant="ghost" @click="removeEntry(issueForm.subdomains, index)"><Trash2 />{{ t('common.delete') }}</Button>
            </div>
            <Button size="sm" @click="addEntry(issueForm.subdomains)"><Plus />{{ t('certificatesPage.addSubdomain') }}</Button>
          </div>
        </FormField>
      </div>
      <InfoBar tone="info" :title="t('certificatesPage.coveragePreview')">
        <div class="flex flex-wrap gap-2"><Badge v-for="domain in issueCoverage" :key="domain" tone="info">{{ domain }}</Badge></div>
      </InfoBar>
    </div>
    <template #footer><Button @click="dialog = ''">{{ t('common.cancel') }}</Button><Button variant="primary" :loading="saving" :disabled="!issueFormValid" @click="issueCertificate">{{ editingCertificateId ? t('certificatesPage.adjustReissue') : t('certificatesPage.issue') }}</Button></template>
  </Dialog>

  <Dialog :open="dialog === 'self-ca' || dialog === 'self-leaf'" :title="dialog === 'self-ca' ? t('certificatesPage.generateCa') : t('certificatesPage.generateLeaf')" :close-label="t('common.close')" @update:open="(open: boolean) => { if (!open) dialog = '' }">
    <div class="grid gap-3">
      <div class="grid gap-3 sm:grid-cols-2">
        <FormField :label="t('common.name')" required :error="selfFormErrors.name ? t(selfFormErrors.name) : ''">
          <template #default="{ id, describedBy, invalid }">
            <Input :id="id" :aria-describedby="describedBy" :invalid="invalid" :model-value="selfForm.name" @update:model-value="selfForm.name = $event; delete selfFormErrors.name" />
          </template>
        </FormField>
        <FormField v-if="dialog === 'self-leaf'" :label="t('certificatesPage.kindCa')" required :error="selfFormErrors.caId ? t(selfFormErrors.caId) : ''">
          <template #default="{ id, describedBy, invalid }">
            <Select :id="id" :aria-describedby="describedBy" :aria-invalid="invalid || undefined" :invalid="invalid" :model-value="selfForm.caId" :options="selfCas" @update:model-value="selfForm.caId = $event; delete selfFormErrors.caId" />
          </template>
        </FormField>
        <FormField :label="t('certificatesPage.commonName')" required :error="selfFormErrors.commonName ? t(selfFormErrors.commonName) : ''">
          <template #default="{ id, describedBy, invalid }">
            <Input :id="id" :aria-describedby="describedBy" :invalid="invalid" :model-value="selfForm.commonName" @update:model-value="selfForm.commonName = $event; delete selfFormErrors.commonName" />
          </template>
        </FormField>
      </div>
      <FormField v-if="dialog === 'self-leaf'" :label="t('certificatesPage.dnsNames')">
        <div class="grid gap-2">
          <div v-for="(_, index) in selfForm.dnsNames" :key="index" class="grid grid-cols-field gap-2">
            <Input v-model="selfForm.dnsNames[index]" :aria-label="t('certificatesPage.dnsNames')" />
            <Button size="sm" variant="ghost" @click="removeEntry(selfForm.dnsNames, index)"><Trash2 />{{ t('common.delete') }}</Button>
          </div>
          <Button size="sm" @click="addEntry(selfForm.dnsNames)"><Plus />{{ t('certificatesPage.addDnsName') }}</Button>
        </div>
      </FormField>
      <FormField v-if="dialog === 'self-leaf'" :label="t('certificatesPage.ipAddresses')">
        <div class="grid gap-2">
          <div v-for="(_, index) in selfForm.ipAddresses" :key="index" class="grid grid-cols-field gap-2">
            <Input v-model="selfForm.ipAddresses[index]" :aria-label="t('certificatesPage.ipAddresses')" />
            <Button size="sm" variant="ghost" @click="removeEntry(selfForm.ipAddresses, index)"><Trash2 />{{ t('common.delete') }}</Button>
          </div>
          <Button size="sm" @click="addEntry(selfForm.ipAddresses)"><Plus />{{ t('certificatesPage.addIpAddress') }}</Button>
        </div>
      </FormField>
      <FormField v-if="dialog === 'self-ca'" :label="t('certificatesPage.years')">
        <template #default="{ id, describedBy, invalid }">
          <NumberBox :id="id" :aria-describedby="describedBy" :invalid="invalid" :min="1" :step="1" :model-value="toNumber(selfForm.years)" @update:model-value="selfForm.years = fromNumber($event)" />
        </template>
      </FormField>
      <FormField v-else :label="t('certificatesPage.days')">
        <template #default="{ id, describedBy, invalid }">
          <NumberBox :id="id" :aria-describedby="describedBy" :invalid="invalid" :min="1" :step="1" :model-value="toNumber(selfForm.days)" @update:model-value="selfForm.days = fromNumber($event)" />
        </template>
      </FormField>
    </div>
    <template #footer><Button @click="dialog = ''">{{ t('common.cancel') }}</Button><Button variant="primary" :loading="saving" :disabled="!selfFormValid" @click="saveSelf">{{ t('common.create') }}</Button></template>
  </Dialog>

  <Dialog :open="dialog.startsWith('asset-') && !['asset-export','asset-preflight'].includes(dialog)" :size="dialog === 'asset-import' ? 'large' : 'default'" :title="t('certificatesPage.assetForm')" :close-label="t('common.close')" @update:open="(open: boolean) => { if (!open) dialog = '' }">
    <div class="grid gap-3" :class="dialog === 'asset-import' ? 'h-full min-h-0 grid-rows-page grid-cols-safe' : ''">
      <InfoBar v-if="assetActionError" tone="danger">{{ assetActionError }}</InfoBar>
      <div :class="dialog === 'asset-import' ? 'grid gap-3 md:grid-cols-2' : 'grid gap-3 sm:grid-cols-2'">
        <FormField v-if="dialog === 'asset-import'" :label="t('common.type')">
          <template #default="{ id, describedBy }">
            <Select :id="id" :aria-describedby="describedBy" :model-value="assetForm.type" :options="assetTypeOptions" @update:model-value="setAssetFormType" />
          </template>
        </FormField>
        <FormField :label="t('common.name')" required :error="assetFormErrors.name ? t(assetFormErrors.name) : ''">
          <template #default="{ id, describedBy, invalid }">
            <Input :id="id" :aria-describedby="describedBy" :invalid="invalid" :model-value="assetForm.name" @update:model-value="assetForm.name = $event; delete assetFormErrors.name" />
          </template>
        </FormField>
        <FormField v-if="dialog === 'asset-tls' || (dialog === 'asset-import' && assetForm.type === 'tls_certificate')" :label="t('certificatesPage.kindCa')" :error="assetFormErrors.parentAssetId ? t(assetFormErrors.parentAssetId) : ''">
          <template #default="{ id, describedBy, invalid }">
            <Select :id="id" :aria-describedby="describedBy" :aria-invalid="invalid || undefined" :invalid="invalid" :model-value="assetForm.parentAssetId" :options="caAssets" @update:model-value="assetForm.parentAssetId = $event; delete assetFormErrors.parentAssetId" />
          </template>
        </FormField>
        <FormField v-if="dialog !== 'asset-ssh'" :label="t('certificatesPage.commonName')" :error="assetFormErrors.commonName ? t(assetFormErrors.commonName) : ''">
          <template #default="{ id, describedBy, invalid }">
            <Input :id="id" :aria-describedby="describedBy" :invalid="invalid" :model-value="assetForm.commonName" @update:model-value="assetForm.commonName = $event; delete assetFormErrors.commonName" />
          </template>
        </FormField>
        <FormField :label="t('certificatesPage.algorithm')">
          <template #default="{ id, describedBy }">
            <Select :id="id" :aria-describedby="describedBy" :model-value="assetForm.algorithm" :options="algorithmOptions" @update:model-value="assetForm.algorithm = $event" />
          </template>
        </FormField>
        <FormField v-if="assetForm.algorithm === 'rsa'" :label="t('certificatesPage.keySize')">
          <template #default="{ id, describedBy, invalid }">
            <NumberBox :id="id" :aria-describedby="describedBy" :invalid="invalid" :min="1024" :max="8192" :step="1024" :model-value="toNumber(assetForm.keySize)" @update:model-value="assetForm.keySize = fromNumber($event)" />
          </template>
        </FormField>
      </div>
      <div v-if="dialog === 'asset-import'" class="min-h-0">
        <FormField v-if="!assetImportHasCertificate(assetForm.type)" :label="t('certificatesPage.privateKeyPem')" class="h-full min-h-0 grid-rows-page">
          <CodeEditor v-model="assetForm.privateKeyPem" language="plain" :editor-label="t('certificatesPage.privateKeyPem')" />
        </FormField>
        <Tabs v-else v-model="assetImportMaterialTab" class="h-full" :tabs="assetImportMaterialTabs">
          <FormField v-if="assetImportMaterialTab === 'privateKey'" :label="t('certificatesPage.privateKeyPem')" class="h-full min-h-0 grid-rows-page">
            <CodeEditor v-model="assetForm.privateKeyPem" language="plain" :editor-label="t('certificatesPage.privateKeyPem')" />
          </FormField>
          <FormField v-else :label="t('certificatesPage.certificatePem')" class="h-full min-h-0 grid-rows-page">
            <CodeEditor v-model="assetForm.certificatePem" language="plain" :editor-label="t('certificatesPage.certificatePem')" />
          </FormField>
        </Tabs>
      </div>
      <FormField v-if="dialog === 'asset-tls'" :label="t('certificatesPage.dnsNames')">
        <div class="grid gap-2">
          <div v-for="(_, index) in assetForm.dnsNames" :key="index" class="grid grid-cols-field gap-2">
            <Input v-model="assetForm.dnsNames[index]" :aria-label="t('certificatesPage.dnsNames')" />
            <Button size="sm" variant="ghost" @click="removeEntry(assetForm.dnsNames, index)"><Trash2 />{{ t('common.delete') }}</Button>
          </div>
          <Button size="sm" @click="addEntry(assetForm.dnsNames)"><Plus />{{ t('certificatesPage.addDnsName') }}</Button>
        </div>
      </FormField>
      <FormField v-if="dialog === 'asset-tls'" :label="t('certificatesPage.ipAddresses')">
        <div class="grid gap-2">
          <div v-for="(_, index) in assetForm.ipAddresses" :key="index" class="grid grid-cols-field gap-2">
            <Input v-model="assetForm.ipAddresses[index]" :aria-label="t('certificatesPage.ipAddresses')" />
            <Button size="sm" variant="ghost" @click="removeEntry(assetForm.ipAddresses, index)"><Trash2 />{{ t('common.delete') }}</Button>
          </div>
          <Button size="sm" @click="addEntry(assetForm.ipAddresses)"><Plus />{{ t('certificatesPage.addIpAddress') }}</Button>
        </div>
      </FormField>
    </div>
    <template #footer><Button @click="dialog = ''">{{ t('common.cancel') }}</Button><Button variant="primary" :loading="saving" :disabled="!assetFormValid" @click="saveAsset">{{ t('common.save') }}</Button></template>
  </Dialog>

  <Dialog :open="dialog === 'asset-export'" :title="t('certificatesPage.exportAsset')" :close-label="t('common.close')" @update:open="(open: boolean) => { if (!open) dialog = '' }">
    <div class="grid gap-3">
      <FormField :label="t('certificatesPage.archivePassword')" :hint="t('certificatesPage.exportHint')">
        <template #default="{ id, describedBy }">
          <Input :id="id" :aria-describedby="describedBy" v-model="assetForm.password" type="password" />
        </template>
      </FormField>
    </div>
    <template #footer><Button @click="dialog = ''">{{ t('common.cancel') }}</Button><Button :loading="saving" :disabled="!assetForm.password" @click="createExport">{{ t('certificatesPage.exportAsset') }}</Button></template>
  </Dialog>

  <Dialog :open="dialog === 'asset-preflight'" :title="t('certificatesPage.batchImport')" :description="t('certificatesPage.batchImportHint')" :close-label="t('common.close')" @update:open="(open: boolean) => { if (!open) dialog = '' }">
    <div class="grid gap-3">
      <FormField :label="t('certificatesPage.importArchive')">
        <FileUploadButton accept=".zip,.tar,.tar.gz,.tgz,.panel-key-assets,application/zip,application/x-tar,application/gzip,application/octet-stream" :label="importFile ? importFile.name : t('certificatesPage.chooseImportArchive')" @change="onFileChange" />
      </FormField>
      <FormField :label="t('certificatesPage.archivePassword')">
        <template #default="{ id }">
          <Input :id="id" v-model="assetForm.password" type="password" />
        </template>
      </FormField>
      <Button :disabled="!importFile || !assetForm.password" :loading="saving" @click="preflightImport">{{ t('certificatesPage.preflightImport') }}</Button>
      <InfoBar v-if="importPlan && importPlan.summary.conflictCount" tone="warning">{{ t('certificatesPage.importConflicts', { count: importPlan.summary.conflictCount }) }}</InfoBar>
      <InfoBar v-if="importPlan && !importPlan.requiresDangerConfirm" tone="info">{{ t('certificatesPage.importReady') }}</InfoBar>
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
