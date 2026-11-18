<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router';
import { Activity, AlertTriangle, ClipboardList, Eye, Globe2, HardDrive, History, MoreHorizontal, Plus, Radar, RefreshCcw, Rocket, Save, Square, Trash2, UploadCloud, Wrench } from '@/icons';
import { applicationsApi } from '@/api/applications';
import { ApiError } from '@/api/client';
import { serversApi } from '@/api/servers';
import { saveBlobDownload } from '@/api/download';
import { reverseProxyFacilityApi, storageShareFacilityApi } from '@/api/facilityApps';
import { metricsFacilityApi } from '@/api/metricsFacility';
import { networkProbeFacilityApi } from '@/api/networkProbe';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue';
import Dialog from '@/components/ui/Dialog.vue';
import Dropdown from '@/components/ui/Dropdown.vue';
import DropdownItem from '@/components/ui/DropdownItem.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import IconButton from '@/components/ui/IconButton.vue';
import Input from '@/components/ui/Input.vue';
import NumberBox from '@/components/ui/NumberBox.vue';
import SearchInput from '@/components/ui/SearchInput.vue';
import PaginationBar from '@/components/ui/PaginationBar.vue';
import Select from '@/components/ui/Select.vue';
import StatusBadge from '@/components/ui/StatusBadge.vue';
import Switch from '@/components/ui/Switch.vue';
import Table from '@/components/ui/Table.vue';
import Tabs from '@/components/ui/Tabs.vue';
import LoadingOverlay from '@/components/ui/LoadingOverlay.vue';
import { useErrorToast, useSuccessToast, useToast } from '@/components/ui/toast';
import ServerMultiPicker from '@/components/patterns/ServerMultiPicker.vue';
import TemplateVariableInput from '@/components/patterns/TemplateVariableInput.vue';
import type { TemplateVariableOption } from '@/components/patterns/templateVariable';
import StorageShareFacility from './StorageShareFacility.vue';
import NetworkProbeFacility from './NetworkProbeFacility.vue';
import MetricsFacility from './MetricsFacility.vue';
import FacilityCatalog from './FacilityCatalog.vue';
import FacilityDiagnostics from './FacilityDiagnostics.vue';
import ApplicationLogsDialog from './ApplicationLogsDialog.vue';
import ApplicationDetailPane from './components/ApplicationDetailPane.vue';
import ApplicationDiagnosticsList from './components/ApplicationDiagnosticsList.vue';
import { applicationDetailTrail, applicationEditorTrail, facilityTrail } from './breadcrumbs';
import AssetFileManager from '@/components/patterns/AssetFileManager.vue';
import DetailSection from '@/components/patterns/DetailSection.vue';
import FormField from '@/components/patterns/FormField.vue';
import InfoBar from '@/components/patterns/InfoBar.vue';
import ListCommandBar from '@/components/patterns/ListCommandBar.vue';
import TableDetailName from '@/components/patterns/TableDetailName.vue';
import type { AssetFileAdapter, AssetFileItem } from '@/components/patterns/assetFileManager';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import ListPage from '@/components/templates/ListPage.vue';
import EditorPage from '@/components/templates/EditorPage.vue';
import TableDetailLayout from '@/components/templates/TableDetailLayout.vue';
import ActivityLink from '@/components/activity/ActivityLink.vue';
import { useI18n } from '@/i18n';
import type { ApplicationDto, ApplicationEditPreviewResult, ApplicationEditSession, ApplicationFile, ApplicationRuntime, ApplicationSummaryDto, Diagnostic, PanelFileDefinition, ReverseProxyRule, TemplateVariableDefinition } from '@/types/applications';
import type { ConfigRecord, FacilityEditPreviewResult, FacilityEditSession, FacilityRouteDomain, FacilityRoutePath, ProxyPlaneState, ReverseProxyConfig, StaticAsset, StorageShareConfig } from '@/types/facilityApps';
import type { MetricsFacilityConfig } from '@/types/metricsFacility';
import type { NetworkProbeConfig } from '@/types/networkProbe';
import type { ServerDto } from '@/types/servers';
import { formatDateTime } from '@/utils/datetime';
import {
  applicationFileMountOptions as makeApplicationFileMountOptions,
  applicationRuntimePollDelay,
  applicationNodeCounts,
  applicationPlanningError,
  applicationStatus,
  cloneFacilityDomain,
  cloneFacilityPath,
  cloneProxyPath,
  cloneProxyRules,
  defaultRouteOptions,
  draftFromApplication,
  facilityRecordDraftFromRecord,
  hasBlockingDiagnostic,
  makeFacilityPath,
  makeKeyValueRow,
  makeMountRow,
  makeNewFacilityDomainRecord,
  makePortRow,
  makeProxyPath,
  makeProxyRule,
  panelFileMountOptions as makePanelFileMountOptions,
  routeMode,
  routeSummary,
  saveInputFromDraft,
  shouldPollApplicationRuntime,
  statusTone,
  validateApplicationDraft,
  validateFacilityPathFields,
  validateFacilityRecordDraft,
  validateFileName,
  type ApplicationDraftUi,
  type AppDetailTab,
  type FacilityDetailTab,
  type FieldErrors,
  type KeyValueRow,
  type MountRow,
  type PortRow,
  type ProxyRuleDraft,
  type SaveStage,
} from './model';
import { inferTemplateLanguage, type TemplateLanguage } from './templateLanguage';
import { archiveName } from './archivePath';
import {
  deleteLocalAsset,
  emptyLocalAssetState,
  findPendingFile,
  findPendingText,
  localAssetNames,
  localAssetsAreDirty,
  planLocalAssetFlush,
  resetLocalAssetState,
  upsertLocalAsset,
  visibleLocalAssets,
  type LocalAssetBaseline,
  type LocalAssetState,
} from './localAssets';
import { normalizePage } from '@/views/_shared/requestState';

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const notifyError = useErrorToast();
const notifySuccess = useSuccessToast();
const notify = useToast();

let pageLoadController: AbortController | null = null;
let runtimeController: AbortController | null = null;
let applicationDetailController: AbortController | null = null;
let editorQueryController: AbortController | null = null;
let pageLoadRequestId = 0;
let runtimeRequestId = 0;
let applicationDetailRequestId = 0;
let editorQueryRequestId = 0;
let searchTimer: ReturnType<typeof setTimeout> | null = null;
let dnsPollTimer: ReturnType<typeof setTimeout> | null = null;
let runtimePollTimer: ReturnType<typeof setTimeout> | null = null;
let runtimePollApplicationId = '';
let runtimePollDeadline = 0;
let runtimePollAttempts = 0;
let runtimePollInFlight = false;
const runtimePollMaxAttempts = 24;
const runtimePollMaxDurationMs = 120000;

const applications = ref<ApplicationSummaryDto[]>([]);
const templateVariables = ref<TemplateVariableDefinition[]>([]);
const panelFiles = ref<PanelFileDefinition[]>([]);
const applicationDetails = ref<Record<string, ApplicationDto>>({});
const applicationFiles = ref<Record<string, ApplicationFile[]>>({});
const runtimes = ref<Record<string, ApplicationRuntime>>({});
const rowImageLoading = ref<Record<string, boolean>>({});
const rowImageControllers = new Map<string, AbortController>();
/** 行图片懒加载缓存/去重：已解析的 imageReference、已尝试过的 id（失败也不重试）、延迟队列。 */
const rowImageRefs = ref<Record<string, string>>({});
const rowImageAttempted = new Set<string>();
const rowImageQueue: string[] = [];
let rowImageQueueTimer: ReturnType<typeof setTimeout> | undefined;
/** 服务器列表：按 mode 需要时加载一次，并由 ensureServers() 去重并发请求。 */
const serversLoaded = ref(false);
let serversRequest: Promise<void> | null = null;
/** 已成功加载的列表查询键（page + q）；返回列表命中时不再重复请求，显式刷新仍会重取。 */
let applicationsLoadedKey = '';
/** 同查询的列表请求去重：mode 切换与显式刷新并发时复用同一个 Promise。 */
let applicationsLoadPromise: Promise<void> | null = null;
let applicationsLoadPromiseKey = '';
const facility = ref<ReverseProxyConfig | null>(null);
/** Proxy plane: gateway membership + container deployments only (no route content). */
const proxyPlane = ref<ProxyPlaneState | null>(null);
const proxyPlaneDraft = ref<string[]>([]);
const proxyPlaneDraftTouched = ref(false);
const proxyPlaneSaving = ref(false);
const proxyPlaneError = ref('');
const proxyPlaneBlockedError = ref('');
/** Independent configuration records (facility domains + application reverse proxies). */
const configRecords = ref<ConfigRecord[]>([]);
const configRecordsError = ref('');
const configRecordActionKey = ref('');
const deleteRecordTarget = ref<ConfigRecord | null>(null);
const deleteRecordOpen = ref(false);
/** Single-record editor: committed baseline + local draft (never more than one record). */
const facilityRecord = ref<ConfigRecord | null>(null);
const facilityRecordDraft = reactive<FacilityRouteDomain>(makeNewFacilityDomainRecord());
const facilityRecordRelayScope = ref<'all' | 'selected'>('all');
const facilityRecordId = computed(() => String(route.query.recordId ?? '').trim().toLowerCase());
const facilityRecordIsNew = computed(() => route.query.new === '1' || route.query.new === 'true');
const storageShareOptions = ref<{ label: string; value: string }[]>([]);
const storageShareAvailable = ref(false);
const storageShareLabelBySource = ref<Record<string, string>>({});
const storageShareConfig = ref<StorageShareConfig | null>(null);
const networkProbeConfig = ref<NetworkProbeConfig | null>(null);
const metricsConfig = ref<MetricsFacilityConfig | null>(null);
const selectedId = ref(String(route.query.application ?? ''));
const search = ref(String(route.query.search ?? ''));
const page = ref(normalizePage(route.query.page));
const pageSize = 50;
const totalApplications = ref(0);
const loading = ref(false);
const detailLoading = ref(false);
const detailError = ref('');
const runtimeLoading = ref(false);
const runtimeError = ref('');
const editorLoading = ref(false);
const facilitiesLoaded = ref(true);
const facilities = [
  { kind: 'reverse-proxy', icon: Globe2, titleKey: 'applicationsPage.entranceProxyFacility', descriptionKey: 'applicationsPage.entranceProxyFacilityDescription', categoryKey: 'applicationsPage.facilityCategoryTraffic', status: 'available' },
  { kind: 'storage-share', icon: HardDrive, titleKey: 'applicationsPage.storageShareFacility', descriptionKey: 'applicationsPage.storageShareFacilityDescription', categoryKey: 'applicationsPage.facilityCategoryStorage', status: 'available' },
  { kind: 'network-probe', icon: Radar, titleKey: 'applicationsPage.networkProbeFacility', descriptionKey: 'applicationsPage.networkProbeFacilityDescription', categoryKey: 'applicationsPage.facilityCategoryNetwork', status: 'available' },
  { kind: 'metrics', icon: Activity, titleKey: 'applicationsPage.metricsFacility', descriptionKey: 'applicationsPage.metricsFacilityDescription', categoryKey: 'applicationsPage.facilityCategoryObservability', status: 'available' },
];
const error = ref('');
const actionError = ref('');
const pending = ref('');
const rowPendingId = ref('');
const logsOpen = ref(false);
const logsTargetId = ref('');
const confirmOpen = ref(false);
const confirmKind = ref<'delete' | 'stop'>('delete');
const confirmTarget = ref('');
const restoreConfirmOpen = ref(false);
const persistentServerId = ref('');
let restoreFile: File | null = null;
const discardDialogOpen = ref(false);
let pendingLeaveTarget: string | null = null;
let pendingFacilityCancel = false;
let allowLeave = false;
function normalizeAppDetailTab(value: unknown): AppDetailTab {
  if (value === 'routes' || value === 'files' || value === 'data') return value;
  return 'runtime';
}
function normalizeFacilityDetailTab(value: unknown): FacilityDetailTab {
  if (value === 'configs' || value === 'dns') return value;
  return 'proxy';
}
const appTab = ref<AppDetailTab>(normalizeAppDetailTab(route.query.tab));
const facilityTab = ref<FacilityDetailTab>(normalizeFacilityDetailTab(route.query.tab));
const activeAppTab = computed({
  get: () => appTab.value,
  set: (value: AppDetailTab) => selectAppTab(value),
});
const activeFacilityTab = computed({
  get: () => facilityTab.value,
  set: (value: FacilityDetailTab) => selectFacilityTab(value),
});
const appDetailTabs = computed<Array<{ label: string; value: AppDetailTab }>>(() => [
  { label: t('applicationsPage.runtime'), value: 'runtime' },
  { label: t('applicationsPage.routes'), value: 'routes' },
  { label: t('applicationsPage.files'), value: 'files' },
  { label: t('applicationsPage.persistentData'), value: 'data' },
]);
const facilityDetailTabs = computed(() => [
  { label: t('applicationsPage.facilityTabProxy'), value: 'proxy' },
  { label: t('applicationsPage.facilityTabConfigs'), value: 'configs' },
  { label: t('applicationsPage.facilityTabDns'), value: 'dns' },
]);
const fileActionErrors = ref<Record<string, string>>({});
const fileActionPending = ref('');
const diagnostics = ref<Diagnostic[]>([]);
const preview = ref<ApplicationEditPreviewResult | null>(null);
/** Temporary save-pipeline session only; null while the user is editing locally. */
const editSession = ref<ApplicationEditSession | null>(null);
const facilitySession = ref<FacilityEditSession | null>(null);
const facilityPreview = ref<FacilityEditPreviewResult | null>(null);
const facilityDiagnostics = ref<Diagnostic[]>([]);
/**
 * 保存失败（preview 返回 blocking diagnostics）时的检查结果弹窗：复用共享 Dialog +
 * ApplicationDiagnosticsList；关闭后停留编辑器，不再在右侧常驻摘要/检查面板。
 */
const editorDiagnosticsOpen = ref(false);
const editorDiagnostics = computed(() => (isFacilityEditor.value ? facilityDiagnostics.value : diagnostics.value));
const appLocalAssets = reactive<LocalAssetState>(emptyLocalAssetState());
const facilityLocalAssets = reactive<LocalAssetState>(emptyLocalAssetState());
const editorReady = ref(false);
const isDirty = ref(false);
const saveStage = ref<SaveStage>('idle');
const dialogOpen = ref(false);
const dialogKind = ref<'env' | 'port' | 'mount' | 'proxy' | 'proxyPath' | 'facilityPath'>('env');
const dialogIndex = ref(-1);
const dialogParentIndex = ref(-1);
const rowDraft = reactive<KeyValueRow>(makeKeyValueRow());
const portDraft = reactive<PortRow>(makePortRow());
const mountDraft = reactive<MountRow>(makeMountRow());
const proxyDraft = reactive<ProxyRuleDraft>({
  ...makeProxyRule(),
});
const proxyPathDraft = reactive(makeProxyPath());
const facilityPathDraft = reactive<FacilityRoutePath>(makeFacilityPath());
const facilityPathErrors = reactive<FieldErrors>({});
const facilityRequestHeaders = ref<KeyValueRow[]>([]);
const facilityResponseHeaders = ref<KeyValueRow[]>([]);
const proxyRequestHeaders = ref<KeyValueRow[]>([]);
const proxyResponseHeaders = ref<KeyValueRow[]>([]);

const mode = computed(() => routeMode(route.path, route.params));
const facilityKind = computed(() => String(route.params.facilityKind ?? ''));
const isAppEditor = computed(() => mode.value === 'create' || mode.value === 'edit');
const isCreateMode = computed(() => mode.value === 'create');
const isReverseProxyFacility = computed(() => !facilityKind.value || facilityKind.value === 'reverse-proxy');
const isStorageShareFacility = computed(() => facilityKind.value === 'storage-share');
const isNetworkProbeFacility = computed(() => facilityKind.value === 'network-probe');
const isMetricsFacility = computed(() => facilityKind.value === 'metrics');
const currentFacilitySummary = computed(() => facilities.find((item) => item.kind === (facilityKind.value || 'reverse-proxy')) ?? null);
const isFacilityEditor = computed(() => mode.value === 'facilityConfig' && isReverseProxyFacility.value);
const facilityPageTitle = computed(() => {
  if (mode.value === 'facilityConfig' && isReverseProxyFacility.value) {
    return facilityRecordIsNew.value ? t('applicationsPage.facilityRecordCreateTitle') : t('applicationsPage.facilityRecordEditTitle');
  }
  return currentFacilitySummary.value ? t(currentFacilitySummary.value.titleKey) : t('applicationsPage.facilityUnavailable');
});
const facilityPageDescription = computed(() => {
  if (mode.value === 'facilityConfig' && isReverseProxyFacility.value) return t('applicationsPage.facilityRecordEditorDescription');
  return currentFacilitySummary.value ? t(currentFacilitySummary.value.descriptionKey) : t('applicationsPage.facilityUnavailableDescription', { kind: facilityKind.value });
});
const currentApplicationSummary = computed(() => applications.value.find((item) => item.id === selectedId.value) ?? null);
const currentApplication = computed(() => selectedId.value ? applicationDetails.value[selectedId.value] ?? null : null);
const selectedApplication = computed(() => currentApplication.value ?? applicationFromSummary(currentApplicationSummary.value) ?? emptyApplication());
const persistentServerOptions = computed(() => (selectedApplication.value.persistentServers ?? []).map((serverId) => ({ label: serverDisplayName(serverId), value: serverId })));
const currentRuntime = computed(() => selectedId.value ? runtimes.value[selectedId.value] : null);
const planningError = computed(() => applicationPlanningError(selectedApplication.value, currentRuntime.value));
const appStatus = computed(() => currentApplicationSummary.value ? applicationStatus(selectedApplication.value, currentRuntime.value) : 'unknown');
const selectedNodeCounts = computed(() => applicationNodeCounts(selectedApplication.value, currentRuntime.value));
/**
 * 面包屑：仅详情/编辑器/设施详情/配置使用；列表页与设施目录不加。
 * 末项不带 `to`，由共享 BreadcrumbBar 渲染 aria-current="page"；back 行为保持不变。
 */
const applicationDetailBreadcrumbs = computed(() => applicationDetailTrail(t, selectedApplication.value.name));
const applicationEditorBreadcrumbs = computed(() => applicationEditorTrail(t, { create: isCreateMode.value, name: selectedApplication.value.name }));
const facilityBreadcrumbs = computed(() => facilityTrail(
  t,
  currentFacilitySummary.value ? t(currentFacilitySummary.value.titleKey) : null,
  mode.value === 'facilityConfig',
));
const appDraft = reactive<ApplicationDraftUi>(draftFromApplication());
const appErrors = computed(() => validateApplicationDraft(appDraft));
const appRows = computed(() => applications.value.map((app) => ({
  app,
  routes: routeSummary(applicationDetails.value[app.id] ?? emptyApplication()),
  counts: applicationNodeCounts(app, runtimes.value[app.id]),
})));
/**
 * ListPage 状态槽契约：首屏 loading / 加载失败 / “无任何筛选且无数据”走页级槽，
 * 搜索无结果仍留在 Table 的 inset 空态（保留现有文案与行为）。
 */
const applicationListState = computed<'ready' | 'loading' | 'error' | 'empty'>(() => {
  if (error.value && !appRows.value.length) return 'error';
  if (loading.value && !applications.value.length) return 'loading';
  if (!appRows.value.length && !search.value.trim()) return 'empty';
  return 'ready';
});
const detailOpen = computed(() => Boolean(selectedId.value));
const applicationColumns = computed(() => [
  { key: 'name', label: t('common.name') },
  // 移动端（<sm）只保留名称+操作：状态/实例折入名称副行（见 #name 的 sm:hidden 行）。
  { key: 'status', label: t('common.status'), hideBelow: 'sm' as const },
  // 次级列按优先级在窄屏隐藏：实例摘要 <lg 隐藏，更新时间仅 >=wide(1239) 显示。
  { key: 'instances', label: t('applicationsPage.nodeHealthColumn'), hideBelow: 'lg' as const },
  { key: 'updatedAt', label: t('applicationsPage.updatedAtColumn'), hideBelow: 'wide' as const },
  { key: 'actions', label: t('common.actions'), align: 'right' as const },
]);
const logsApplication = computed(() => {
  const target = logsTargetId.value ? applications.value.find((item) => item.id === logsTargetId.value) : null;
  return target ?? currentApplicationSummary.value;
});
const confirmApplication = computed(() => confirmTarget.value ? applications.value.find((item) => item.id === confirmTarget.value) ?? null : null);
const confirmHasPersistentData = computed(() => confirmTarget.value ? Boolean(applicationDetails.value[confirmTarget.value]?.hasPersistentData) : false);
const servers = ref<ServerDto[]>([]);
const serverNameMap = computed(() => new Map(servers.value.map((server) => [server.id, server.name])));
const applicationTemplateVariableOptions = computed<TemplateVariableOption[]>(() => templateVariables.value
  .filter((variable) => variable.specExpression && (variable.category === 'application' || variable.category === 'application_reference'))
  .map((variable) => ({
    id: variable.key,
    expression: variable.specExpression,
    label: variable.category === 'application_reference'
      ? t('applicationsPage.applicationContainerVariable', { name: variable.resourceName || variable.resourceId || variable.key })
      : t('applicationsPage.currentApplicationVariable', { key: variable.key }),
    description: variable.category === 'application_reference' ? variable.resourceId : variable.specExpression,
  })));
function serverDisplayName(id: string) {
  return serverNameMap.value.get(id) || id;
}
function facilityDnsStatus(domain: string) {
  return facility.value?.dnsSync?.[domain.toLowerCase()]?.state ?? '';
}
function facilityDnsError(domain: string) {
  return facility.value?.dnsSync?.[domain.toLowerCase()]?.error ?? '';
}
function facilityDnsTone(state: string): 'success' | 'warning' | 'danger' | 'neutral' {
  if (state === 'synced') return 'success';
  if (state === 'failed') return 'danger';
  if (state === 'pending' || state === 'skipped') return 'warning';
  return 'neutral';
}
type FacilityDnsDomainRow = { domain: string; source: 'facility' | 'application' };
const facilityDnsDomains = computed<FacilityDnsDomainRow[]>(() => {
  const rows: FacilityDnsDomainRow[] = [];
  const seen = new Set<string>();
  for (const domain of facility.value?.domains ?? []) {
    const name = domain.domain.trim().toLowerCase();
    if (!name || seen.has(name)) continue;
    seen.add(name);
    rows.push({ domain: name, source: 'facility' });
  }
  for (const app of facility.value?.applicationRoutes ?? []) {
    for (const route of app.routes ?? []) {
      const name = String(route.domain || '').trim().toLowerCase();
      if (!name || seen.has(name)) continue;
      seen.add(name);
      rows.push({ domain: name, source: 'application' });
    }
  }
  return rows;
});
const dnsSyncPendingDomain = ref('');
const dnsTakeoverDomain = ref('');
const dnsTakeoverOpen = ref(false);
function facilityDnsNeedsAction(domain: string) {
  const state = facilityDnsStatus(domain);
  // pending must be retryable: exclusive/queue races can leave domains stuck
  // "同步中" with no automatic follow-up until the user re-triggers sync.
  return state === 'pending' || state === 'failed' || state === 'skipped' || state === 'unknown' || state === '';
}
function facilityDnsConflict(domain: string) {
  const error = facilityDnsError(domain).toLowerCase();
  return error.includes('cname') || error.includes('conflict') || error.includes('already exists');
}
async function retryFacilityDns(domain: string) {
  if (dnsSyncPendingDomain.value) return;
  dnsSyncPendingDomain.value = domain;
  try {
    const result = await reverseProxyFacilityApi.syncDNS({ domains: [domain], takeover: false });
    notifySuccess(t('applicationsPage.dnsSyncRetryAccepted', { taskId: result.taskId }), result);
    await loadFacilityData();
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    dnsSyncPendingDomain.value = '';
  }
}
function askFacilityDnsTakeover(domain: string) {
  dnsTakeoverDomain.value = domain;
  dnsTakeoverOpen.value = true;
}
async function confirmFacilityDnsTakeover() {
  const domain = dnsTakeoverDomain.value;
  if (!domain || dnsSyncPendingDomain.value) return;
  dnsSyncPendingDomain.value = domain;
  dnsTakeoverOpen.value = false;
  try {
    const result = await reverseProxyFacilityApi.syncDNS({ domains: [domain], takeover: true, confirmTakeover: true });
    notifySuccess(t('applicationsPage.dnsSyncTakeoverAccepted', { taskId: result.taskId }), result);
    await loadFacilityData();
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    dnsSyncPendingDomain.value = '';
    dnsTakeoverDomain.value = '';
  }
}
const serverOptions = computed(() => {
  const byId = new Map(servers.value.map((server) => [server.id, server]));
  return servers.value.map((item) => {
    const id = item.id;
    const server = byId.get(id);
    return {
      id,
      label: server?.name ?? id,
      name: server?.name ?? id,
      description: server?.host ? server.host : t('applicationsPage.serverOptionDescription', { id }),
      status: server ? (server.reachable ? 'reachable' : 'unreachable') : undefined,
    };
  });
});
const deploymentDisabledIds = computed(() => servers.value.filter((server) => !serverCanDeploy(server)).map((server) => server.id));
const deploymentDisabledReasons = computed(() => {
  const out: Record<string, string> = {};
  servers.value.forEach((server) => {
    const reason = serverDeployBlockReason(server);
    if (reason) out[server.id] = reason;
  });
  return out;
});
function serverCanDeploy(server: ServerDto) {
  return server.traits?.['agent.status'] === 'compatible' && Boolean(server.traits?.['agent.url']);
}
function serverDeployBlockReason(server: ServerDto) {
  if (server.traits?.['agent.status'] === 'compatible' && server.traits?.['agent.url']) return '';
  if (!server.reachable) return t('resourcesPage.blockUnreachable');
  return t('resourcesPage.blockAgent');
}
// 路由资产选项来自本地可见列表（已提交基线 ⊕ pending），删除会立刻从选择器消失。
const assetOptions = computed(() => visibleLocalAssets(facilityLocalAssets).map((asset) => ({
  value: asset.name,
  label: `${asset.name} / ${asset.filename || asset.name}`,
})));
const fileLanguageOptions = computed(() => (['plain', 'yaml', 'json', 'shell', 'nginx', 'properties', 'dockerfile'] as TemplateLanguage[]).map((value) => ({
  value,
  label: t(`applicationsPage.templateLanguage.${value}`),
})));
const applicationAssetItems = computed<AssetFileItem[]>(() => visibleLocalAssets(appLocalAssets));
const applicationFileMountOptions = computed(() => makeApplicationFileMountOptions(
  localAssetNames(appLocalAssets).map((name) => ({ name })),
  mountDraft.type === 'file' ? mountDraft.source : '',
  (name) => t('applicationsPage.applicationFileMountMissing', { name }),
));
const applicationFileMountSourceValid = computed(() => mountDraft.type !== 'file'
  || applicationFileMountOptions.value.some((option) => option.value === mountDraft.source.trim() && !option.disabled));
function panelFileLabel(file: Pick<PanelFileDefinition, 'kind' | 'name' | 'resourceType'>) {
  const resourceKey = file.resourceType === 'certificate' ? 'certificate' : file.resourceType === 'key_asset' ? 'key_asset' : '';
  const kindKey = ['certificate', 'private_key', 'public_key', 'ssh_public_key'].includes(file.kind) ? file.kind : '';
  return t('applicationsPage.panelFileMountOption', {
    resource: resourceKey ? t(`applicationsPage.panelFileResource.${resourceKey}`) : file.resourceType,
    name: file.name,
    kind: kindKey ? t(`applicationsPage.panelFileKind.${kindKey}`) : file.kind,
  });
}
const panelFileMountOptions = computed(() => makePanelFileMountOptions(
  panelFiles.value,
  mountDraft.type === 'panel_file' ? mountDraft.source : '',
  panelFileLabel,
  (source) => t('applicationsPage.panelFileMountMissing', { source }),
));
const panelFileMountSourceValid = computed(() => mountDraft.type !== 'panel_file'
  || panelFileMountOptions.value.some((option) => option.value === mountDraft.source.trim() && !option.disabled));
const panelFileLabelBySource = computed(() => new Map(panelFiles.value.map((file) => [file.source, panelFileLabel(file)])));
const facilityAssetItems = computed<AssetFileItem[]>(() => visibleLocalAssets(facilityLocalAssets));
/** Committed asset download must be scoped to the record being edited. */
const facilityAssetRecordId = computed(() => facilityRecord.value?.recordId || facilityRecordId.value || facilityRecordDraft.domain.trim().toLowerCase());
const mountTypeOptions = computed(() => ['persistent', 'volume', 'host', 'file', 'panel_file', 'storage_share'].map((value) => ({ label: t(`applicationsPage.mountType.${value}`), value })));
const portProtocolOptions = computed(() => (['tcp', 'udp'] as const).map((value) => ({ label: value.toUpperCase(), value })));
const routeTypeOptions = computed(() => ['static', 'redirect', 'proxy_pass'].map((value) => ({ label: t(`applicationsPage.routeType.${value}`), value })));
const sourceTypeOptions = computed(() => ['uploaded_file', 'uploaded_bundle'].map((value) => ({ label: t(`applicationsPage.sourceType.${value}`), value })));
const saving = computed(() => pending.value === 'commit');
const editorDirty = computed(() => isDirty.value
  || localAssetsAreDirty(appLocalAssets)
  || localAssetsAreDirty(facilityLocalAssets));
const proxyAnyAccessModel = computed({
  get: () => Boolean(proxyDraft.anyAccess.enabled),
  set: (value: boolean) => { proxyDraft.anyAccess.enabled = value; },
});
const proxyRelayScope = ref<'all' | 'selected'>('all');
const anyAccessRelayScopeOptions = computed(() => [
  { label: t('applicationsPage.anyAccessRelayAll'), value: 'all' },
  { label: t('applicationsPage.anyAccessRelaySelected'), value: 'selected' },
]);
const proxyRelayServerOptions = computed(() => {
  const origins = new Set(proxyAutoOriginIds.value);
  const gateways = new Set(facility.value?.deploymentServers ?? []);
  return serverOptions.value.filter((item) => gateways.has(item.id) && !origins.has(item.id));
});
const proxyRelayServerIdsModel = computed({
  get: () => proxyDraft.anyAccess.relayServerIds ?? [],
  set: (value: string[]) => { proxyDraft.anyAccess.relayServerIds = value; },
});
const facilityRedirectCode = computed({
  get: () => String(facilityPathDraft.redirectCode ?? 302),
  set: (value: string) => { facilityPathDraft.redirectCode = Number(value); },
});
const anyAccessStrategyOptions = computed(() => (['round_robin', 'ip_hash', 'primary_backup'] as const).map((value) => ({ label: t(`applicationsPage.loadBalancingStrategy.${value}`), value })));
const httpModeOptions = computed(() => (['inherit', 'on', 'off'] as const).map((value) => ({ label: t(`applicationsPage.httpMode.${value}`), value })));
const webSocketModeOptions = computed(() => (['auto', 'on', 'off'] as const).map((value) => ({ label: t(`applicationsPage.httpMode.${value}`), value })));
const proxySourceModeOptions = computed(() => (['preserve_source', 'hide_source'] as const).map((value) => ({ label: t(`applicationsPage.proxySourceMode.${value}`), value })));

// Single-record editor derived state. Gateways come from the proxy plane, so an
// origin can never point at a server that is no longer a gateway.
const facilityGatewayIds = computed(() => proxyPlane.value?.deploymentServers ?? facility.value?.deploymentServers ?? []);
const facilityGatewayIdSet = computed(() => new Set(facilityGatewayIds.value));
const facilityGatewayServers = computed(() => serverOptions.value.filter((item) => facilityGatewayIdSet.value.has(item.id)));
const facilityRecordErrors = computed(() => {
  const errors = validateFacilityRecordDraft(facilityRecordDraft, facilityGatewayIds.value);
  if (facilityRecordIsNew.value && !errors.domain) {
    const value = (facilityRecordDraft.domain ?? '').trim().toLowerCase();
    if (value && configRecords.value.some((record) => record.recordId === value)) {
      errors.domain = 'applicationsPage.validationDomainDuplicate';
    }
  }
  return errors;
});
const facilityRecordRelayServers = computed(() => {
  const origins = new Set(facilityRecordDraft.originServerIds);
  return facilityGatewayServers.value.filter((item) => !origins.has(item.id));
});
const facilityRecordOriginModel = computed({
  get: () => facilityRecordDraft.originServerIds,
  set: (value: string[]) => {
    facilityRecordDraft.originServerIds = value;
    const primary = facilityRecordDraft.anyAccess.primaryOriginServerId ?? '';
    if (primary && !value.includes(primary)) facilityRecordDraft.anyAccess.primaryOriginServerId = '';
    facilityRecordDraft.anyAccess.relayServerIds = (facilityRecordDraft.anyAccess.relayServerIds ?? []).filter((id) => !value.includes(id));
    markDirty();
  },
});
const facilityRecordRelayServerIdsModel = computed({
  get: () => facilityRecordDraft.anyAccess.relayServerIds ?? [],
  set: (value: string[]) => {
    facilityRecordDraft.anyAccess.relayServerIds = value;
    markDirty();
  },
});
const facilityRecordAnyAccessModel = computed({
  get: () => Boolean(facilityRecordDraft.anyAccess.enabled),
  set: (value: boolean) => {
    facilityRecordDraft.anyAccess.enabled = value;
    markDirty();
  },
});
const facilityRecordPrimaryOptions = computed(() => facilityRecordDraft.originServerIds.map((id) => ({ label: serverDisplayName(id), value: id })));

// Proxy plane draft: members are edited locally and saved with PUT proxy-plane.
const proxyPlaneDraftModel = computed({
  get: () => proxyPlaneDraft.value,
  set: (value: string[]) => {
    proxyPlaneDraft.value = value;
    proxyPlaneDraftTouched.value = true;
    proxyPlaneBlockedError.value = '';
  },
});
const proxyPlaneDirty = computed(() => {
  const current = [...(proxyPlane.value?.deploymentServers ?? [])].sort();
  const next = [...proxyPlaneDraft.value].sort();
  return current.length !== next.length || current.some((id, index) => id !== next[index]);
});
type ProxyPlaneReference = { recordId: string; kind: ConfigRecord['kind']; serverIds: string[] };
const proxyPlaneRemovedServers = computed(() => {
  const next = new Set(proxyPlaneDraft.value);
  return (proxyPlane.value?.deploymentServers ?? []).filter((id) => !next.has(id));
});
const proxyPlaneBlockedReferences = computed<ProxyPlaneReference[]>(() => {
  const removed = new Set(proxyPlaneRemovedServers.value);
  if (!removed.size) return [];
  const rows: ProxyPlaneReference[] = [];
  for (const record of configRecords.value) {
    const serverIds = new Set<string>();
    const domain = record.domain;
    for (const serverId of domain?.originServerIds ?? []) if (removed.has(serverId)) serverIds.add(serverId);
    const primary = domain?.anyAccess?.primaryOriginServerId ?? '';
    if (primary && removed.has(primary)) serverIds.add(primary);
    for (const serverId of domain?.anyAccess?.relayServerIds ?? []) if (removed.has(serverId)) serverIds.add(serverId);
    if (serverIds.size) rows.push({ recordId: record.recordId, kind: record.kind, serverIds: [...serverIds] });
  }
  return rows;
});
const configRecordColumns = computed(() => [
  { key: 'domain', label: t('applicationsPage.domain') },
  // 设施记录表同样按优先级隐藏次级列：来源/版本 <lg 隐藏，DNS/更新时间仅 >=wide 显示。
  { key: 'source', label: t('applicationsPage.configRecordSource'), hideBelow: 'lg' as const },
  { key: 'version', label: t('applicationsPage.configRecordVersion'), nowrap: true, hideBelow: 'lg' as const },
  { key: 'dns', label: t('applicationsPage.configRecordDns'), nowrap: true, hideBelow: 'wide' as const },
  { key: 'updatedAt', label: t('applicationsPage.configRecordUpdatedAt'), nowrap: true, hideBelow: 'wide' as const },
  { key: 'actions', label: t('common.actions'), align: 'right' as const, nowrap: true },
]);
function configRecordSourceLabel(record: ConfigRecord) {
  if (record.kind === 'facility_domain') return t('applicationsPage.configRecordSourceFacility');
  return record.applicationName
    ? t('applicationsPage.configRecordSourceApplicationNamed', { name: record.applicationName })
    : t('applicationsPage.configRecordSourceApplication');
}
function openFacilityRecord(record: ConfigRecord) {
  void router.push({ path: `/applications/facility-apps/${facilityKind.value || 'reverse-proxy'}/config`, query: { recordId: record.recordId } });
}
function startNewFacilityRecord() {
  void router.push({ path: `/applications/facility-apps/${facilityKind.value || 'reverse-proxy'}/config`, query: { new: '1' } });
}
function openRecordApplication(record: ConfigRecord) {
  if (!record.applicationId) return;
  void router.push({ path: '/applications/apps', query: { application: record.applicationId } });
}

function facilityPathNumberOption(field: 'clientMaxBodySizeMb' | 'connectTimeoutSeconds' | 'readTimeoutSeconds' | 'sendTimeoutSeconds') {
  return computed<number | null>({
    get: () => {
      const value = facilityPathDraft.options?.[field];
      return typeof value === 'number' ? value : null;
    },
    set: (value: number | null) => {
      if (!facilityPathDraft.options) return;
      // 空值在旧 Input 时代落到 0；这里保持保存语义不变（NumberBox 负责 min=0 夹取）。
      facilityPathDraft.options[field] = value === null ? 0 : Math.max(0, value);
    },
  });
}
function facilityPathStringOption(field: 'gzipMode' | 'bufferingMode' | 'webSocketMode', fallback: string) {
  return computed({
    get: () => facilityPathDraft.options?.[field] ?? fallback,
    set: (value: string) => { if (facilityPathDraft.options) facilityPathDraft.options[field] = value; },
  });
}
const facilityGzipMode = facilityPathStringOption('gzipMode', 'inherit');
const facilityBufferingMode = facilityPathStringOption('bufferingMode', 'inherit');
const facilityWebSocketMode = facilityPathStringOption('webSocketMode', 'off');
const facilityClientMaxBodySizeMb = facilityPathNumberOption('clientMaxBodySizeMb');
const facilityConnectTimeoutSeconds = facilityPathNumberOption('connectTimeoutSeconds');
const facilityReadTimeoutSeconds = facilityPathNumberOption('readTimeoutSeconds');
const facilitySendTimeoutSeconds = facilityPathNumberOption('sendTimeoutSeconds');

function proxyPathNumberOption(field: 'clientMaxBodySizeMb' | 'connectTimeoutSeconds' | 'readTimeoutSeconds' | 'sendTimeoutSeconds') {
  return computed<number | null>({
    get: () => {
      const value = proxyPathDraft.options?.[field];
      return typeof value === 'number' ? value : null;
    },
    set: (value: number | null) => {
      if (!proxyPathDraft.options) return;
      // 保持旧 Input 语义：空值保存为 0，非法/负数回落 0。
      proxyPathDraft.options[field] = value === null ? 0 : Math.max(0, value);
    },
  });
}
function proxyPathStringOption(field: 'gzipMode' | 'bufferingMode' | 'webSocketMode') {
  return computed({
    get: () => proxyPathDraft.options?.[field] ?? '',
    set: (value: string) => { if (proxyPathDraft.options) proxyPathDraft.options[field] = value; },
  });
}
const proxyGzipMode = proxyPathStringOption('gzipMode');
const proxyBufferingMode = proxyPathStringOption('bufferingMode');
const proxyWebSocketMode = proxyPathStringOption('webSocketMode');
const proxyClientMaxBodySizeMb = proxyPathNumberOption('clientMaxBodySizeMb');
const proxyConnectTimeoutSeconds = proxyPathNumberOption('connectTimeoutSeconds');
const proxyReadTimeoutSeconds = proxyPathNumberOption('readTimeoutSeconds');
const proxySendTimeoutSeconds = proxyPathNumberOption('sendTimeoutSeconds');
const proxyOriginPriorityModel = computed({
  get: () => (proxyDraft.anyAccess.originPriority?.length ? proxyDraft.anyAccess.originPriority : proxyAutoOriginIds.value),
  set: (value: string[]) => { proxyDraft.anyAccess.originPriority = value; },
});
function moveProxyOriginPriority(index: number, delta: -1 | 1) {
  const list = [...proxyOriginPriorityModel.value];
  const target = index + delta;
  if (target < 0 || target >= list.length) return;
  [list[index], list[target]] = [list[target], list[index]];
  proxyOriginPriorityModel.value = list;
}
const proxyAutoOriginIds = computed(() => {
  const gateways = new Set(facility.value?.deploymentServers ?? []);
  const deployed = appDraft.deploymentMode === 'selected' ? appDraft.deploymentServers : (facility.value?.deploymentServers ?? []);
  return deployed.filter((id) => gateways.has(id));
});

function applicationFileBaseline(files: ApplicationFile[]): LocalAssetBaseline[] {
  return files.map((file) => ({
    key: file.name,
    name: file.name,
    filename: file.kind === 'archive' ? file.contentType : file.name.split('/').at(-1) || file.name,
    kind: file.kind === 'template' ? 'text' : file.kind === 'archive' ? 'archive' : 'binary',
    size: file.size,
    sha256: file.sha256,
    editable: file.kind === 'template',
  }));
}

function facilityAssetBaseline(assets: StaticAsset[]): LocalAssetBaseline[] {
  return assets.map((asset) => ({
    key: asset.name,
    name: asset.name,
    filename: asset.filename,
    kind: asset.contentMode === 'text' ? 'text' : asset.kind === 'uploaded_bundle' ? 'archive' : 'binary',
    size: asset.size,
    sha256: asset.sha256,
    editable: asset.contentMode === 'text',
  }));
}

const applicationAssetAdapter: AssetFileAdapter = {
  async upload({ file, kind }) {
    const name = kind === 'archive' ? archiveName(file.name) : file.name;
    const nameError = validateFileName(name);
    if (nameError) throw new Error(t(nameError));
    upsertLocalAsset(appLocalAssets, {
      type: 'upsert', key: name, name, filename: file.name, kind, file, size: file.size,
    });
    markDirty();
  },
  async replace(item, { file, kind }) {
    upsertLocalAsset(appLocalAssets, {
      type: 'upsert', key: item.key, name: item.name, filename: file.name, kind, file, size: file.size,
    });
    markDirty();
  },
  async download(item) {
    const pendingFile = findPendingFile(appLocalAssets, item.key);
    if (pendingFile) {
      saveBlobDownload({ blob: pendingFile, filename: item.filename || item.name });
      return;
    }
    const pendingText = findPendingText(appLocalAssets, item.key);
    if (pendingText !== undefined) {
      saveBlobDownload({ blob: new Blob([pendingText], { type: 'text/plain;charset=utf-8' }), filename: item.filename || item.name });
      return;
    }
    const appId = String(route.params.applicationId ?? '');
    if (!appId) throw new Error(t('applicationsPage.fileLoadFailed'));
    saveBlobDownload(await applicationsApi.downloadFile(appId, item.key, item.filename || item.name));
  },
  async delete(item) {
    deleteLocalAsset(appLocalAssets, item.key);
    markDirty();
  },
  async loadText(item) {
    const pendingText = findPendingText(appLocalAssets, item.key);
    if (pendingText !== undefined) {
      return { content: pendingText, name: item.name, language: inferTemplateLanguage(item.name) };
    }
    const appId = String(route.params.applicationId ?? '');
    if (!appId) throw new Error(t('applicationsPage.fileLoadFailed'));
    const downloaded = await applicationsApi.downloadFile(appId, item.key, item.filename || item.name);
    const content = await downloaded.blob.text();
    return { content, name: item.name, language: inferTemplateLanguage(item.name) };
  },
  async saveText({ key: fileName, name, content }) {
    const nameError = validateFileName(name);
    if (nameError) throw new Error(t(nameError));
    upsertLocalAsset(appLocalAssets, {
      type: 'upsert',
      key: fileName,
      name,
      kind: 'text',
      textContent: content,
      size: new Blob([content]).size,
    });
    markDirty();
  },
  reload: reloadApplicationEditor,
};

const facilityAssetAdapter: AssetFileAdapter = {
  async upload({ file, kind }) {
    const name = file.name.replace(/\.[^.]+$/, '') || file.name;
    const nameError = validateFileName(name);
    if (nameError) throw new Error(t(nameError));
    upsertLocalAsset(facilityLocalAssets, {
      type: 'upsert', key: name, name, filename: file.name, kind, file, size: file.size,
    });
    markDirty();
  },
  async replace(item, { file, kind }) {
    upsertLocalAsset(facilityLocalAssets, {
      type: 'upsert', key: item.key, name: item.name, filename: file.name, kind, file, size: file.size,
    });
    markDirty();
  },
  async download(item) {
    const pendingFile = findPendingFile(facilityLocalAssets, item.key);
    if (pendingFile) {
      saveBlobDownload({ blob: pendingFile, filename: item.filename || item.name });
      return;
    }
    const pendingText = findPendingText(facilityLocalAssets, item.key);
    if (pendingText !== undefined) {
      saveBlobDownload({ blob: new Blob([pendingText], { type: 'text/plain;charset=utf-8' }), filename: item.filename || item.name });
      return;
    }
    saveBlobDownload(await reverseProxyFacilityApi.downloadStaticAsset(facilityAssetRecordId.value, item.key, item.filename || item.name));
  },
  async delete(item) {
    deleteLocalAsset(facilityLocalAssets, item.key);
    markDirty();
  },
  async loadText(item) {
    const pendingText = findPendingText(facilityLocalAssets, item.key);
    if (pendingText !== undefined) {
      return { content: pendingText, name: item.name, filename: item.filename, language: 'plain' as const };
    }
    const result = await reverseProxyFacilityApi.downloadStaticAsset(facilityAssetRecordId.value, item.key, item.filename || item.name);
    return { content: await result.blob.text(), name: item.name, filename: item.filename, language: 'plain' as const };
  },
  async saveText({ key: assetName, name, filename, content }) {
    const nameError = validateFileName(name) ?? (filename ? validateFileName(filename) : null);
    if (nameError) throw new Error(t(nameError));
    const resolvedFilename = filename || name;
    upsertLocalAsset(facilityLocalAssets, {
      type: 'upsert',
      key: assetName,
      name: name || resolvedFilename,
      filename: resolvedFilename,
      kind: 'text',
      textContent: content,
      size: new Blob([content]).size,
    });
    markDirty();
  },
  reload: reloadFacilityEditor,
};
const appDeploymentServersModel = computed({
  get: () => appDraft.deploymentServers,
  set: (value: string[]) => {
    appDraft.deploymentServers = value;
    markAppStructuredDirty();
  },
});

watch(search, (value) => {
  void router.replace({ query: { ...route.query, search: value || undefined } });
  if (searchTimer) clearTimeout(searchTimer);
  searchTimer = setTimeout(() => {
    if (page.value !== 1) page.value = 1;
    else if (mode.value === 'apps') void loadApplications({ loadSelectedRuntime: true });
  }, 250);
});

watch(page, (value) => {
  void router.replace({ query: { ...route.query, page: value > 1 ? String(value) : undefined } });
  if (mode.value === 'apps') void loadApplications({ loadSelectedRuntime: true });
});

watch(() => route.query.application, (value) => {
  const next = String(value ?? '');
  if (next !== selectedId.value) selectedId.value = next;
});

watch(() => route.query.tab, (value) => {
  if (mode.value === 'apps') {
    const next = normalizeAppDetailTab(value);
    if (next !== appTab.value) appTab.value = next;
    return;
  }
  if (mode.value === 'facilityDetail') {
    const next = normalizeFacilityDetailTab(value);
    if (next !== facilityTab.value) facilityTab.value = next;
  }
});

watch(selectedId, async (value) => {
  persistentServerId.value = '';
  detailError.value = '';
  runtimeError.value = '';
  if (value) {
    // 详情/路由页签需要服务器名称；列表页本身不触发服务器请求。
    void ensureServers();
    void loadApplicationDetail(value);
    await loadRuntime(value);
  }
});

function applicationDetailQuery(applicationId = selectedId.value) {
  return {
    ...route.query,
    application: applicationId || undefined,
    tab: appTab.value === 'runtime' ? undefined : appTab.value,
  };
}

function facilityDetailQuery(tab = facilityTab.value) {
  return {
    ...route.query,
    tab: tab === 'proxy' ? undefined : tab,
  };
}

function selectAppTab(value: AppDetailTab) {
  appTab.value = value;
  if (selectedId.value && route.query.tab !== (value === 'runtime' ? undefined : value)) {
    void router.replace({ query: applicationDetailQuery() });
  }
}

function selectFacilityTab(value: FacilityDetailTab) {
  facilityTab.value = value;
  const expected = value === 'proxy' ? undefined : value;
  if (route.query.tab !== expected) {
    void router.replace({ query: facilityDetailQuery(value) });
  }
}

function openApplicationDetail(app: ApplicationSummaryDto) {
  if (selectedId.value !== app.id) {
    selectedId.value = app.id;
    appTab.value = 'runtime';
  }
  void router.push({ query: applicationDetailQuery(app.id) });
}

/** 关闭覆盖详情并清掉 URL 里的选中项与 tab。 */
function backToApplicationList() {
  if (!selectedId.value) return;
  selectedId.value = '';
  appTab.value = 'runtime';
  void router.replace({ query: { ...route.query, application: undefined, tab: undefined } });
}

/** 取消/返回编辑器：明确回到 apps 列表（编辑时保留选中），不依赖历史栈。 */
function leaveApplicationEditor() {
  if (saving.value) return;
  const appId = isCreateMode.value ? '' : String(route.params.applicationId ?? '');
  void router.push({
    path: '/applications/apps',
    query: appId ? { application: appId } : undefined,
  });
}

watch(() => selectedApplication.value.persistentServers, (serverIds) => {
  const locations = serverIds ?? [];
  if (!locations.includes(persistentServerId.value)) {
    persistentServerId.value = locations.length === 1 ? locations[0] : '';
  }
}, { immediate: true });

watch(() => route.path, async () => {
  cancelRuntimeLoad();
  cancelApplicationDetailLoad();
  cancelRowImageLoads();
  cancelEditorQuery();
  if (dnsPollTimer) {
    clearTimeout(dnsPollTimer);
    dnsPollTimer = null;
  }
  actionError.value = '';
  editorReady.value = false;
  editSession.value = null;
  facilitySession.value = null;
  facilityRecord.value = null;
  resetLocalAssetState(appLocalAssets, []);
  resetLocalAssetState(facilityLocalAssets, []);
  isDirty.value = false;
  dialogOpen.value = false;
  if (mode.value !== 'facilityDetail') {
    proxyPlaneDraftTouched.value = false;
  }
  await ensureModeData();
  if (isAppEditor.value) await startApplicationEditor();
  if (isFacilityEditor.value) await startFacilityEditor();
});

onBeforeRouteLeave((to) => {
  if (saving.value) return false;
  if (allowLeave || !editorDirty.value) return true;
  pendingFacilityCancel = false;
  pendingLeaveTarget = to.fullPath;
  discardDialogOpen.value = true;
  return false;
});

function handleBeforeUnload(event: BeforeUnloadEvent) {
  if (!editorDirty.value && !saving.value) return;
  event.preventDefault();
  event.returnValue = '';
}

/**
 * 显式刷新（页头刷新按钮 / 操作完成后的数据回填）：强制重取当前 mode 所需数据。
 * 进入页面/切换 mode 走 ensureModeData()，避免列表→详情/编辑器时重复拉列表。
 */
async function load() {
  serversLoaded.value = false;
  const currentMode = mode.value;
  if (currentMode !== 'apps') {
    cancelRuntimeLoad();
    cancelApplicationDetailLoad();
    cancelRowImageLoads();
  }
  if (currentMode === 'apps') {
    await loadApplications({ loadSelectedRuntime: true });
    void ensureServers();
    return;
  }
  await ensureServers();
  if (isAppEditor.value) return;
  if (currentMode === 'facilityCatalog') {
    await loadFacilityData();
    return;
  }
  if (currentMode === 'facilityDetail') {
    if (isReverseProxyFacility.value) await loadFacilityDetailData();
    else await loadFacilityData();
  }
}

/** 进入页面 / 切换 mode 时的按需加载：只取当前 mode 真正需要的数据。 */
async function ensureModeData() {
  const currentMode = mode.value;
  if (currentMode === 'apps') {
    await ensureApplicationsList();
    // 详情/路由页签需要服务器名称；列表本身不需要，故只在打开详情时补一次。
    if (selectedId.value) void ensureServers();
    return;
  }
  await ensureServers();
  if (isAppEditor.value) {
    // startApplicationEditor 内部按需补列表/应用详情/文件与模板目录，这里不预取列表。
    return;
  }
  if (currentMode === 'facilityCatalog') {
    if (!facilitiesLoaded.value || !facility.value) await loadFacilityData();
    return;
  }
  if (currentMode === 'facilityDetail') {
    if (isReverseProxyFacility.value) await loadFacilityDetailData();
    else await loadFacilityData();
    return;
  }
  // facilityConfig（含 storage-share/network-probe/metrics 配置）：startFacilityEditor / 组件自取数据。
}

function applicationsListQueryKey() {
  return `${page.value}\u0000${search.value.trim()}`;
}

/** 返回列表命中已有数据时不刷新；搜索/分页变化仍由各自的 watcher 强制重取。 */
async function ensureApplicationsList() {
  if (applicationsLoadedKey === applicationsListQueryKey()) return;
  await loadApplications({ loadSelectedRuntime: Boolean(selectedId.value) });
}

async function loadServers() {
  try {
    servers.value = await serversApi.list();
    serversLoaded.value = true;
  } catch {
    // Server names are a display enhancement; keep IDs when the list is unavailable.
  }
}

/** 服务器列表只加载一次；并发进入多个 mode 时复用同一个请求。 */
async function ensureServers() {
  if (serversLoaded.value) return;
  if (serversRequest) return serversRequest;
  serversRequest = loadServers().finally(() => {
    serversRequest = null;
  });
  return serversRequest;
}

function rowImageReference(app: ApplicationSummaryDto) {
  const detail = applicationDetails.value[app.id];
  return detail?.imageReference
    || detail?.imageUpdateTargets?.find((target) => target.reference)?.reference
    || rowImageRefs.value[app.id]
    || app.imageReference
    || '';
}

function cancelRowImageLoads() {
  if (rowImageQueueTimer) {
    clearTimeout(rowImageQueueTimer);
    rowImageQueueTimer = undefined;
  }
  rowImageQueue.length = 0;
  rowImageControllers.forEach((controller) => controller.abort());
  rowImageControllers.clear();
  rowImageLoading.value = {};
}

/**
 * 行图片详情按需、延迟、去重加载：列表首屏后排队延迟预取（串行），
 * 已解析/已尝试过的 id 不再发请求；切走列表或重新加载列表时取消队列。
 * 不清空 rowImageAttempted/rowImageRefs，避免翻页/刷新后重复 GET 或回退到“不可用”。
 */
function enqueueRowImagePrefetch(applicationIds: string[]) {
  if (!applicationIds.length) return;
  rowImageQueue.push(...applicationIds);
  if (rowImageQueueTimer) return;
  rowImageQueueTimer = setTimeout(() => {
    rowImageQueueTimer = undefined;
    void drainRowImageQueue();
  }, 250);
}

async function drainRowImageQueue() {
  while (rowImageQueue.length) {
    if (mode.value !== 'apps') {
      rowImageQueue.length = 0;
      return;
    }
    const applicationId = rowImageQueue.shift();
    if (!applicationId) return;
    await loadRowImageDetail(applicationId);
  }
}

async function loadRowImageDetail(applicationId: string) {
  if (rowImageRefs.value[applicationId]
    || applicationDetails.value[applicationId]?.imageReference
    || rowImageAttempted.has(applicationId)
    || rowImageControllers.has(applicationId)) return;
  rowImageAttempted.add(applicationId);
  const controller = new AbortController();
  rowImageControllers.set(applicationId, controller);
  try {
    const app = await applicationsApi.get(applicationId, { signal: controller.signal });
    if (rowImageControllers.get(applicationId) !== controller || mode.value !== 'apps') return;
    applicationDetails.value = { ...applicationDetails.value, [applicationId]: app };
    const reference = app.imageReference || app.imageUpdateTargets?.find((target) => target.reference)?.reference || '';
    if (reference) rowImageRefs.value = { ...rowImageRefs.value, [applicationId]: reference };
  } catch {
    // Leave the cell empty/unavailable when the image cannot be resolved; never fall back to jobId (container name).
    // 失败保持 attempted，刷新列表也不重试，避免把行图片变成请求风暴。
  } finally {
    if (rowImageControllers.get(applicationId) === controller) {
      rowImageControllers.delete(applicationId);
      rowImageLoading.value = { ...rowImageLoading.value, [applicationId]: false };
    }
  }
}

async function loadApplications(options: { loadSelectedRuntime?: boolean } = {}): Promise<void> {
  const key = applicationsListQueryKey();
  if (applicationsLoadPromise && applicationsLoadPromiseKey === key) return applicationsLoadPromise;
  applicationsLoadPromiseKey = key;
  const promise = runLoadApplications(options).finally(() => {
    if (applicationsLoadPromise === promise) {
      applicationsLoadPromise = null;
      applicationsLoadPromiseKey = '';
    }
  });
  applicationsLoadPromise = promise;
  return promise;
}

async function runLoadApplications(options: { loadSelectedRuntime?: boolean } = {}) {
  pageLoadController?.abort();
  cancelRowImageLoads();
  const requestId = ++pageLoadRequestId;
  const controller = new AbortController();
  pageLoadController = controller;
  const modeAtStart = mode.value;
  loading.value = true;
  error.value = '';
  try {
    const result = await applicationsApi.list({ page: page.value, pageSize, q: search.value.trim() || undefined }, { signal: controller.signal });
    if (requestId !== pageLoadRequestId || mode.value !== modeAtStart) return;
    const apps = result.items;
    applications.value = apps;
    totalApplications.value = result.total;
    applicationsLoadedKey = applicationsListQueryKey();
    if (modeAtStart === 'apps') {
      const missing: Record<string, boolean> = {};
      apps.forEach((app) => {
        if (!app.imageReference && !rowImageRefs.value[app.id] && !rowImageAttempted.has(app.id)) missing[app.id] = true;
      });
      rowImageLoading.value = missing;
      // 延迟串行预取，避免首屏对每一行立即发 GET（N+1 风暴）。
      enqueueRowImagePrefetch(Object.keys(missing));
    }
    const appIds = new Set(apps.map((item) => item.id));
    applicationDetails.value = Object.fromEntries(Object.entries(applicationDetails.value).filter(([id]) => appIds.has(id)));
    if (!route.query.application && selectedId.value && !apps.some((item) => item.id === selectedId.value)) {
      selectedId.value = '';
    }
    if (options.loadSelectedRuntime) {
      void loadApplicationDetail(selectedId.value);
      void loadRuntime(selectedId.value);
    }
  } catch (err) {
    if (isAbortError(err)) return;
    error.value = err instanceof Error ? err.message : t('applicationsPage.loadFailed');
    notifyError(err instanceof Error ? err.message : t('applicationsPage.loadFailed'), err);
  } finally {
    if (requestId === pageLoadRequestId) loading.value = false;
  }
}

async function loadFacilityData() {
  pageLoadController?.abort();
  if (dnsPollTimer) clearTimeout(dnsPollTimer);
  const requestId = ++pageLoadRequestId;
  const controller = new AbortController();
  pageLoadController = controller;
  const modeAtStart = mode.value;
  loading.value = true;
  error.value = '';
  try {
    // 各设施配置互不依赖：并行读取，单个失败只影响该设施卡片，不影响目录与其它设施。
    const [primaryResult, storageShareResult, networkProbeResult, metricsResult] = await Promise.allSettled([
      isReverseProxyFacility.value ? reverseProxyFacilityApi.getConfig({ signal: controller.signal }) : Promise.resolve(null),
      storageShareFacilityApi.get({ signal: controller.signal }),
      networkProbeFacilityApi.config({ signal: controller.signal }),
      metricsFacilityApi.config({ signal: controller.signal }),
    ]);
    if (requestId !== pageLoadRequestId || mode.value !== modeAtStart) return;
    if (primaryResult.status === 'fulfilled') {
      facility.value = primaryResult.value;
    } else if (!isAbortError(primaryResult.reason)) {
      error.value = errorMessage(primaryResult.reason, t('applicationsPage.loadFailed'));
      notifyError(error.value, primaryResult.reason);
    }
    storageShareConfig.value = storageShareResult.status === 'fulfilled' ? storageShareResult.value : null;
    networkProbeConfig.value = networkProbeResult.status === 'fulfilled' ? networkProbeResult.value : null;
    metricsConfig.value = metricsResult.status === 'fulfilled' ? metricsResult.value : null;
    facilitiesLoaded.value = true;
  } catch (err) {
    if (isAbortError(err)) return;
    error.value = errorMessage(err, t('applicationsPage.loadFailed'));
    notifyError(error.value, err);
  } finally {
    if (requestId === pageLoadRequestId) loading.value = false;
  }
}

/** Detail page: aggregate snapshot (DNS), proxy plane and the independent config records. */
async function loadFacilityDetailData() {
  pageLoadController?.abort();
  if (dnsPollTimer) clearTimeout(dnsPollTimer);
  const requestId = ++pageLoadRequestId;
  const controller = new AbortController();
  pageLoadController = controller;
  const modeAtStart = mode.value;
  loading.value = true;
  error.value = '';
  proxyPlaneError.value = '';
  configRecordsError.value = '';
  const [configResult, planeResult, recordsResult] = await Promise.allSettled([
    reverseProxyFacilityApi.getConfig({ signal: controller.signal }),
    reverseProxyFacilityApi.getProxyPlane({ signal: controller.signal }),
    reverseProxyFacilityApi.listConfigRecords({ signal: controller.signal }),
  ]);
  if (requestId !== pageLoadRequestId || mode.value !== modeAtStart) return;
  try {
    if (configResult.status === 'fulfilled') {
      facility.value = configResult.value;
    } else if (!isAbortError(configResult.reason)) {
      error.value = errorMessage(configResult.reason, t('applicationsPage.loadFailed'));
      notifyError(error.value, configResult.reason);
    }
    if (planeResult.status === 'fulfilled') {
      proxyPlane.value = planeResult.value;
      if (!proxyPlaneDraftTouched.value) proxyPlaneDraft.value = [...planeResult.value.deploymentServers];
    } else if (!isAbortError(planeResult.reason)) {
      proxyPlaneError.value = errorMessage(planeResult.reason, t('applicationsPage.proxyPlaneLoadFailed'));
    }
    if (recordsResult.status === 'fulfilled') {
      configRecords.value = recordsResult.value;
    } else if (!isAbortError(recordsResult.reason)) {
      configRecordsError.value = errorMessage(recordsResult.reason, t('applicationsPage.configRecordsLoadFailed'));
    }
    try {
      storageShareConfig.value = await storageShareFacilityApi.get({ signal: controller.signal });
    } catch {
      storageShareConfig.value = null;
    }
    facilitiesLoaded.value = true;
    const pendingDns = facility.value?.dnsSync ? Object.values(facility.value.dnsSync).some((state) => state.state === 'pending') : false;
    if (pendingDns && document.visibilityState === 'visible' && mode.value !== 'facilityConfig') {
      dnsPollTimer = setTimeout(() => { void loadFacilityDetailData(); }, 3000);
    }
  } finally {
    if (requestId === pageLoadRequestId) loading.value = false;
  }
}

function errorMessage(error: unknown, fallback: string) {
  return error instanceof Error && error.message ? error.message : fallback;
}

async function loadApplicationDetail(applicationId: string) {
  applicationDetailController?.abort();
  const requestId = ++applicationDetailRequestId;
  const modeAtStart = mode.value;
  if (!applicationId) {
    detailLoading.value = false;
    detailError.value = '';
    return;
  }
  if (modeAtStart !== 'apps') {
    detailLoading.value = false;
    return;
  }
  const controller = new AbortController();
  applicationDetailController = controller;
  detailLoading.value = true;
  detailError.value = '';
  try {
    const [app, files] = await Promise.all([
      applicationsApi.get(applicationId, { signal: controller.signal }),
      applicationsApi.listFiles(applicationId, { signal: controller.signal }),
    ]);
    if (requestId !== applicationDetailRequestId || mode.value !== modeAtStart || applicationId !== selectedId.value) return;
    applicationDetails.value = { ...applicationDetails.value, [applicationId]: app };
    applicationFiles.value = { ...applicationFiles.value, [applicationId]: files };
    detailError.value = '';
  } catch (err) {
    if (isAbortError(err)) return;
    if (applicationId === selectedId.value) {
      const message = err instanceof Error ? err.message : t('applicationsPage.loadFailed');
      detailError.value = message;
      notifyError(message, err);
    }
  } finally {
    if (requestId === applicationDetailRequestId) detailLoading.value = false;
  }
}

function clearRuntimePollTimer() {
  if (runtimePollTimer) clearTimeout(runtimePollTimer);
  runtimePollTimer = null;
}

function stopRuntimePolling() {
  clearRuntimePollTimer();
  runtimePollApplicationId = '';
  runtimePollDeadline = 0;
  runtimePollAttempts = 0;
}

function scheduleRuntimePoll(applicationId: string) {
  clearRuntimePollTimer();
  const runtime = runtimes.value[applicationId];
  if (
    mode.value !== 'apps'
    || selectedId.value !== applicationId
    || !shouldPollApplicationRuntime(runtime)
    || runtimePollAttempts >= runtimePollMaxAttempts
    || Date.now() >= runtimePollDeadline
    || document.visibilityState !== 'visible'
  ) return;

  const delay = Math.min(applicationRuntimePollDelay(runtime), Math.max(0, runtimePollDeadline - Date.now()));
  runtimePollTimer = setTimeout(async () => {
    runtimePollTimer = null;
    if (runtimePollInFlight || runtimeLoading.value || document.visibilityState !== 'visible') {
      scheduleRuntimePoll(applicationId);
      return;
    }
    runtimePollInFlight = true;
    runtimePollAttempts += 1;
    try {
      await loadRuntime(applicationId, { poll: true });
    } finally {
      runtimePollInFlight = false;
    }
  }, delay);
}

function startRuntimePolling(applicationId: string) {
  runtimePollApplicationId = applicationId;
  runtimePollDeadline = Date.now() + runtimePollMaxDurationMs;
  runtimePollAttempts = 0;
  scheduleRuntimePoll(applicationId);
}

function handleVisibilityChange() {
  if (document.visibilityState !== 'visible') {
    clearRuntimePollTimer();
    return;
  }
  if (runtimePollApplicationId && Date.now() < runtimePollDeadline) scheduleRuntimePoll(runtimePollApplicationId);
}

async function loadRuntime(applicationId: string, options: { poll?: boolean } = {}) {
  if (!options.poll) stopRuntimePolling();
  runtimeController?.abort();
  const requestId = ++runtimeRequestId;
  const modeAtStart = mode.value;
  if (!applicationId) {
    detailLoading.value = false;
    runtimeLoading.value = false;
    runtimeError.value = '';
    return;
  }
  if (modeAtStart !== 'apps') {
    detailLoading.value = false;
    runtimeLoading.value = false;
    return;
  }
  const controller = new AbortController();
  runtimeController = controller;
  detailLoading.value = true;
  runtimeLoading.value = true;
  if (!options.poll) runtimeError.value = '';
  try {
    const runtime = await applicationsApi.runtime(applicationId, { signal: controller.signal });
    if (requestId !== runtimeRequestId || mode.value !== modeAtStart || applicationId !== selectedId.value) return;
    runtimes.value = { ...runtimes.value, [applicationId]: runtime };
    runtimeError.value = '';
    if (shouldPollApplicationRuntime(runtime)) {
      if (!options.poll || runtimePollApplicationId !== applicationId) startRuntimePolling(applicationId);
      else scheduleRuntimePoll(applicationId);
    } else {
      stopRuntimePolling();
    }
  } catch (err) {
    if (isAbortError(err)) return;
    if (!options.poll && applicationId === selectedId.value) {
      const message = err instanceof Error ? err.message : t('applicationsPage.runtimeUnavailable');
      runtimeError.value = message;
      notifyError(message, err);
    }
    if (options.poll && runtimePollApplicationId === applicationId) scheduleRuntimePoll(applicationId);
  } finally {
    if (requestId === runtimeRequestId) {
      detailLoading.value = false;
      runtimeLoading.value = false;
    }
  }
}

function retryApplicationDetail() {
  if (!selectedId.value) return;
  void loadApplicationDetail(selectedId.value);
}

function retryApplicationRuntime() {
  if (!selectedId.value) return;
  void loadRuntime(selectedId.value);
}

async function runOperation(name: string, action: () => Promise<unknown>, successKey: string, successKeyWithoutId = '', noChangeKey = '') {
  pending.value = name;
  actionError.value = '';
  try {
    const result = await action();
    const params = taskParams(result);
    const noChange = Boolean((result as { noChange?: boolean } | null)?.noChange);
    notifySuccess(noChange && noChangeKey ? t(noChangeKey) : params ? t(successKey, params) : t(successKeyWithoutId || successKey), result);
    await load();
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
    // Planning can fail before a Job exists. Reload the durable diagnostic so
    // the reason remains visible after the error toast disappears.
    await load();
  } finally {
    pending.value = '';
  }
}

function taskParams(result: unknown) {
  const record = result as { taskId?: string; operationId?: string; deploymentId?: string; evalId?: string };
  const taskId = record?.operationId || record?.taskId || record?.deploymentId || record?.evalId;
  return taskId ? { taskId } : null;
}

function ask(kind: typeof confirmKind.value, target: string) {
  confirmKind.value = kind;
  confirmTarget.value = target;
  confirmOpen.value = true;
}

/** 行内删除：先补载完整详情以确认是否存在持久化数据，再进入危险确认。 */
async function askDelete(app: ApplicationSummaryDto) {
  if (rowPendingId.value) return;
  if (!applicationDetails.value[app.id]) {
    rowPendingId.value = app.id;
    try {
      const detail = await applicationsApi.get(app.id);
      applicationDetails.value = { ...applicationDetails.value, [app.id]: detail };
    } catch (err) {
      notifyError(err instanceof Error ? err.message : t('applicationsPage.loadFailed'), err);
      return;
    } finally {
      rowPendingId.value = '';
    }
  }
  ask('delete', app.id);
}

function openLogsFor(app: ApplicationSummaryDto) {
  logsTargetId.value = app.id;
  logsOpen.value = true;
}

/** 行内高频操作的单项 loading：只锁定当前行按钮，不与 `pending` 的对话框操作互相转圈。 */
const rowActionKey = ref('');
async function runRowOperation(key: string, action: () => Promise<void>) {
  if (rowActionKey.value) return;
  rowActionKey.value = key;
  try {
    await action();
  } finally {
    rowActionKey.value = '';
  }
}

async function updateImageFor(app: ApplicationSummaryDto) {
  await runRowOperation(`image:${app.id}`, () => runOperation('image-update', () => applicationsApi.updateImage(app.id), 'applicationsPage.imageUpdateAccepted', 'applicationsPage.imageUpdateAcceptedWithoutId'));
}

async function deployFor(app: ApplicationSummaryDto) {
  await runRowOperation(`deploy:${app.id}`, () => runOperation('deploy', () => applicationsApi.deploy(app.id), 'applicationsPage.deployAccepted', 'applicationsPage.deployAcceptedWithoutId', 'applicationsPage.deployNoChange'));
}

async function confirmAction() {
  const app = confirmApplication.value;
  if (!app) return;
  const hasPersistentData = Boolean(applicationDetails.value[app.id]?.hasPersistentData);
  if (confirmKind.value === 'delete') {
    await runOperation('delete', () => applicationsApi.delete(app.id, hasPersistentData), 'applicationsPage.deleted');
    if (selectedId.value === app.id) backToApplicationList();
  } else {
    await runOperation('stop', () => applicationsApi.stop(app.id), 'applicationsPage.stopAccepted', 'applicationsPage.stopAcceptedWithoutId');
  }
  confirmOpen.value = false;
}

async function downloadPersistentData(app: ApplicationDto) {
	if (!persistentServerId.value) return;
  await runOperation('persistent-download', async () => {
    saveBlobDownload(await applicationsApi.downloadPersistentData(app.id, persistentServerId.value));
    return undefined;
  }, 'applicationsPage.downloadStarted');
}

async function restorePersistentData(fileOrFiles: File | File[]) {
  const file = Array.isArray(fileOrFiles) ? fileOrFiles[0] : fileOrFiles;
  if (!file || !currentApplicationSummary.value) return;
  restoreFile = file;
  restoreConfirmOpen.value = true;
}

async function confirmRestorePersistentData() {
  const file = restoreFile;
  restoreFile = null;
  restoreConfirmOpen.value = false;
  if (!file || !currentApplicationSummary.value) return;
  await runOperation('persistent-restore', () => applicationsApi.restorePersistentData(selectedApplication.value.id, file), 'applicationsPage.restoreAccepted', 'applicationsPage.restoreAcceptedWithoutId');
}

async function startApplicationEditor() {
  editorLoading.value = true;
  try {
    await startApplicationEditorCore();
  } finally {
    editorLoading.value = false;
  }
}

async function startApplicationEditorCore() {
  const appId = String(route.params.applicationId ?? '');
  editorQueryController?.abort();
  const requestId = ++editorQueryRequestId;
  const controller = new AbortController();
  editorQueryController = controller;
  const modeAtStart = mode.value;
  editorReady.value = false;
  actionError.value = '';
  await ensureApplicationsLoaded();
  await ensureFacilityLoaded();
  try {
    const catalog = await applicationsApi.templateCatalog({ signal: controller.signal });
    templateVariables.value = catalog.variables ?? [];
    panelFiles.value = catalog.panelFiles ?? [];
  } catch (err) {
    if (isAbortError(err)) return;
    templateVariables.value = [];
    panelFiles.value = [];
  }
  if (requestId !== editorQueryRequestId || mode.value !== modeAtStart || String(route.params.applicationId ?? '') !== appId) return;
  let app: ApplicationDto | null = null;
  let files: ApplicationFile[] = [];
  try {
    if (appId) {
      selectedId.value = appId;
      app = applicationDetails.value[appId] ?? await applicationsApi.get(appId, { signal: controller.signal });
      if (requestId !== editorQueryRequestId || mode.value !== modeAtStart || String(route.params.applicationId ?? '') !== appId) return;
      applicationDetails.value = { ...applicationDetails.value, [appId]: app };
      files = await applicationsApi.listFiles(appId, { signal: controller.signal });
      if (requestId !== editorQueryRequestId || mode.value !== modeAtStart || String(route.params.applicationId ?? '') !== appId) return;
      applicationFiles.value = { ...applicationFiles.value, [appId]: files };
    }
    Object.assign(appDraft, draftFromApplication(app));
    resetLocalAssetState(appLocalAssets, applicationFileBaseline(files));
    diagnostics.value = [];
    preview.value = null;
    editSession.value = null;
    isDirty.value = false;
    editorReady.value = true;
  } catch (err) {
    if (isAbortError(err)) return;
    const message = err instanceof Error ? err.message : t('applicationsPage.editorStartFailed');
    actionError.value = message;
    editorReady.value = false;
    notifyError(message, err);
  }
}

async function discardApplicationSaveSession() {
  const session = editSession.value;
  editSession.value = null;
  preview.value = null;
  if (!session) return;
  try {
    await applicationsApi.discardEditSession(session.id);
  } catch {
    // TTL cleanup is the safety net for abandoned mid-save sessions.
  }
}

async function flushApplicationLocalAssets() {
  if (!editSession.value) return;
  const plan = planLocalAssetFlush(appLocalAssets);
  for (const key of plan.deletes) {
    editSession.value = await applicationsApi.deleteEditSessionFile(editSession.value.id, key, editSession.value.revision);
  }
  for (const item of plan.upserts) {
    if (item.kind === 'text') {
      const content = item.textContent ?? '';
      editSession.value = await applicationsApi.putEditSessionFile(editSession.value.id, item.key, editSession.value.revision, {
        name: item.name,
        contentBase64: encodeBase64Utf8(content),
      });
      continue;
    }
    const file = item.file;
    if (!file) throw new Error(t('applicationsPage.fileLoadFailed'));
    if (item.kind === 'archive') {
      editSession.value = await applicationsApi.uploadEditSessionArchive(editSession.value.id, editSession.value.revision, {
        file,
        name: item.name,
        kind: 'archive',
      });
    } else {
      editSession.value = await applicationsApi.uploadEditSessionFile(editSession.value.id, item.key, editSession.value.revision, {
        file,
        name: item.name,
      });
    }
  }
}

async function commitApplication() {
  const localErrorKeys = Object.keys(appErrors.value);
  if (localErrorKeys.length) {
    actionError.value = t(appErrors.value[localErrorKeys[0]]);
    return;
  }
  const appId = String(route.params.applicationId ?? '');
  await runEditorAction(async () => {
    let succeeded = false;
    try {
      editSession.value = await applicationsApi.beginEditSession(
        appId || undefined,
        appId ? undefined : saveInputFromDraft(appDraft),
      );
      editSession.value = await applicationsApi.patchEditSession(
        editSession.value.id,
        editSession.value.revision,
        saveInputFromDraft(appDraft),
      );
      await flushApplicationLocalAssets();
      if (!editSession.value) return;
      preview.value = await applicationsApi.previewEditSession(editSession.value.id, editSession.value.revision);
      diagnostics.value = preview.value.diagnostics ?? [];
      if (hasBlockingDiagnostic(diagnostics.value)) {
        editorDiagnosticsOpen.value = true;
        return;
      }
      const result = await applicationsApi.commitEditSession(editSession.value, preview.value);
      let applied = false;
      if (result.applyNeeded) {
        if (result.applyAction === 'stop') {
          await applicationsApi.stop(result.application.id);
        } else {
          await applicationsApi.deploy(result.application.id);
        }
        applied = true;
      }
      succeeded = true;
      editSession.value = null;
      preview.value = null;
      resetLocalAssetState(appLocalAssets, []);
      notifySuccess(applied || result.applyRequested ? t('applicationsPage.committedAndApplied') : t('applicationsPage.committed'), result);
      isDirty.value = false;
      editorReady.value = false;
      await router.push({ path: '/applications/apps', query: { application: result.application.id } });
      await load();
    } finally {
      if (!succeeded) await discardApplicationSaveSession();
    }
  }, 'commit');
}

async function startFacilityEditor() {
  editorLoading.value = true;
  try {
    await startFacilityRecordEditorCore();
  } finally {
    editorLoading.value = false;
  }
}

/**
 * Enter the single-record editor. It never creates an edit session: the
 * committed record (or an empty draft for a new one) is loaded locally, and a
 * session only exists inside the save pipeline.
 */
async function startFacilityRecordEditorCore() {
  editorQueryController?.abort();
  const requestId = ++editorQueryRequestId;
  const controller = new AbortController();
  editorQueryController = controller;
  const modeAtStart = mode.value;
  const kindAtStart = facilityKind.value;
  const isNew = facilityRecordIsNew.value;
  const recordId = facilityRecordId.value;
  editorReady.value = false;
  actionError.value = '';
  facilityRecord.value = null;
  await ensureServers();
  if (requestId !== editorQueryRequestId || mode.value !== modeAtStart || facilityKind.value !== kindAtStart) return;
  if (!isReverseProxyFacility.value) return;
  if (!isNew && !recordId) {
    actionError.value = t('applicationsPage.facilityRecordMissing');
    return;
  }
  try {
    const plane = await reverseProxyFacilityApi.getProxyPlane({ signal: controller.signal });
    if (requestId !== editorQueryRequestId || mode.value !== modeAtStart) return;
    proxyPlane.value = plane;
    proxyPlaneDraft.value = [...plane.deploymentServers];
    proxyPlaneDraftTouched.value = false;
    if (isNew) {
      // Load the record index so a new domain cannot silently overwrite an existing record.
      try {
        const records = await reverseProxyFacilityApi.listConfigRecords({ signal: controller.signal });
        if (requestId !== editorQueryRequestId || mode.value !== modeAtStart) return;
        configRecords.value = records;
      } catch {
        // Fall through: the save pipeline re-checks uniqueness before begin.
      }
      Object.assign(facilityRecordDraft, makeNewFacilityDomainRecord());
      facilityRecordRelayScope.value = 'all';
      resetLocalAssetState(facilityLocalAssets, []);
    } else {
      const record = await reverseProxyFacilityApi.getConfigRecord(recordId, { signal: controller.signal });
      if (requestId !== editorQueryRequestId || mode.value !== modeAtStart) return;
      if (record.kind !== 'facility_domain') {
        actionError.value = t('applicationsPage.facilityRecordNotEditable');
        return;
      }
      facilityRecord.value = record;
      Object.assign(facilityRecordDraft, facilityRecordDraftFromRecord(record));
      facilityRecordRelayScope.value = (record.domain.anyAccess?.relayServerIds ?? []).length ? 'selected' : 'all';
      resetLocalAssetState(facilityLocalAssets, facilityAssetBaseline(record.staticAssets ?? []));
    }
    facilitySession.value = null;
    facilityPreview.value = null;
    facilityDiagnostics.value = [];
    isDirty.value = false;
    editorReady.value = true;
  } catch (err) {
    if (isAbortError(err)) return;
    actionError.value = errorMessage(err, t('applicationsPage.editorStartFailed'));
    notifyError(actionError.value, err);
  }
}

function facilityDetailPath(tab: FacilityDetailTab = 'configs') {
  return {
    path: `/applications/facility-apps/${facilityKind.value || 'reverse-proxy'}`,
    query: tab === 'proxy' ? undefined : { tab },
  };
}

function cancelFacilityRecordEdit() {
  if (saving.value) return;
  if (!editorDirty.value) {
    performCancelFacilityRecordEdit();
    return;
  }
  pendingLeaveTarget = null;
  pendingFacilityCancel = true;
  discardDialogOpen.value = true;
}

function performCancelFacilityRecordEdit() {
  facilitySession.value = null;
  facilityPreview.value = null;
  facilityDiagnostics.value = [];
  resetLocalAssetState(facilityLocalAssets, []);
  editorReady.value = false;
  actionError.value = '';
  isDirty.value = false;
  void router.push(facilityDetailPath());
}

function confirmDiscard() {
  discardDialogOpen.value = false;
  if (pendingFacilityCancel) {
    pendingFacilityCancel = false;
    performCancelFacilityRecordEdit();
    return;
  }
  const target = pendingLeaveTarget;
  pendingLeaveTarget = null;
  if (!target) return;
  allowLeave = true;
  void router.push(target).finally(() => {
    allowLeave = false;
  });
}

function handleDiscardOpenChange(open: boolean) {
  discardDialogOpen.value = open;
  if (!open) {
    pendingFacilityCancel = false;
    pendingLeaveTarget = null;
  }
}

function goBackFromFacilityPage() {
  if (mode.value === 'facilityConfig') {
    if (isStorageShareFacility.value) {
      void router.replace('/applications/facility-apps/storage-share');
      return;
    }
    // 指标设施详情与配置共用同一界面；返回详情时不要残留配置页的 tab 参数。
    if (isMetricsFacility.value) {
      void router.replace('/applications/facility-apps/metrics');
      return;
    }
    cancelFacilityRecordEdit();
    return;
  }
  void router.push('/applications/facility-apps');
}

async function reloadApplicationEditor() {
  pending.value = 'editor-reload';
  try {
    await discardApplicationSaveSession();
    actionError.value = '';
    await startApplicationEditor();
  } catch (err) {
    throw err;
  } finally {
    pending.value = '';
  }
}

async function reloadFacilityEditor() {
  pending.value = 'editor-reload';
  try {
    await discardFacilitySaveSession();
    actionError.value = '';
    await startFacilityEditor();
  } catch (err) {
    notifyError(errorMessage(err, t('common.operationFailed')), err);
  } finally {
    pending.value = '';
  }
}

async function discardFacilitySaveSession() {
  const session = facilitySession.value;
  facilitySession.value = null;
  facilityPreview.value = null;
  if (!session) return;
  try {
    await reverseProxyFacilityApi.discardEdit(session.id);
  } catch {
    // TTL cleanup covers abandoned mid-save sessions.
  }
}

async function flushFacilityLocalAssets() {
  if (!facilitySession.value) return;
  const plan = planLocalAssetFlush(facilityLocalAssets);
  for (const key of plan.deletes) {
    facilitySession.value = await reverseProxyFacilityApi.deleteEditAsset(
      facilitySession.value.id,
      key,
      facilitySession.value.revision,
    );
  }
  for (const item of plan.upserts) {
    const filename = item.filename || item.name;
    const file = item.file
      ?? (item.textContent !== undefined
        ? new File([item.textContent], filename, { type: 'text/plain;charset=utf-8' })
        : null);
    if (!file) throw new Error(t('applicationsPage.fileLoadFailed'));
    const uploadKind = item.kind === 'archive' ? 'uploaded_bundle' : 'uploaded_file';
    const contentMode = item.kind === 'text' ? 'text' : 'binary';
    facilitySession.value = await reverseProxyFacilityApi.putEditAsset(
      facilitySession.value.id,
      item.key,
      facilitySession.value.revision,
      {
        file,
        name: item.name,
        kind: uploadKind,
        contentMode,
      },
    );
  }
}

/** Normalized payload for this one record: relays/primary are pruned to the current gateways. */
function facilityRecordSaveDraft(): FacilityRouteDomain {
  const draft = cloneFacilityDomain(facilityRecordDraft);
  draft.domain = (facilityRecordIsNew.value ? draft.domain : facilityRecordId.value).trim().toLowerCase();
  draft.originServerIds = (draft.originServerIds ?? []).filter((serverId) => facilityGatewayIdSet.value.has(serverId));
  const enabled = Boolean(draft.anyAccess?.enabled);
  const strategy = enabled ? (draft.anyAccess?.strategy || 'round_robin') : '';
  const primary = enabled && strategy === 'primary_backup' ? (draft.anyAccess?.primaryOriginServerId ?? '') : '';
  draft.anyAccess = {
    enabled,
    strategy,
    primaryOriginServerId: primary && draft.originServerIds.includes(primary) ? primary : '',
    relayServerIds: enabled && facilityRecordRelayScope.value === 'selected'
      ? (draft.anyAccess?.relayServerIds ?? []).filter((serverId) => facilityGatewayIdSet.value.has(serverId) && !draft.originServerIds.includes(serverId))
      : [],
  };
  draft.paths = (draft.paths ?? []).map((path) => cloneFacilityPath(path));
  return draft;
}

function isConflictError(error: unknown) {
  if (error instanceof ApiError) {
    return error.status === 409 || error.code === 'resource_version_conflict' || error.code === 'conflict';
  }
  return false;
}

async function commitFacilityRecord() {
  if (saving.value) return;
  const localErrorKeys = Object.keys(facilityRecordErrors.value);
  if (localErrorKeys.length) {
    actionError.value = t(facilityRecordErrors.value[localErrorKeys[0]]);
    return;
  }
  const draft = facilityRecordSaveDraft();
  const recordId = facilityRecordIsNew.value ? draft.domain : facilityRecordId.value;
  if (!recordId) {
    actionError.value = t('applicationsPage.facilityRecordMissing');
    return;
  }
  if (facilityRecordIsNew.value) {
    try {
      const records = await reverseProxyFacilityApi.listConfigRecords();
      configRecords.value = records;
      if (records.some((record) => record.recordId === recordId)) {
        actionError.value = t('applicationsPage.validationDomainDuplicate');
        return;
      }
    } catch {
      // The server still rejects a duplicate record atomically; continue the pipeline.
    }
  }
  pending.value = 'commit';
  saveStage.value = 'commit';
  actionError.value = '';
  let committed = false;
  try {
    facilitySession.value = await reverseProxyFacilityApi.beginEdit({
      recordId,
      clientDraftKey: `facility:${recordId}`,
      ...(facilityRecordIsNew.value ? { draft } : {}),
    });
    facilitySession.value = await reverseProxyFacilityApi.patchEdit(
      facilitySession.value.id,
      facilitySession.value.revision,
      facilitySession.value.baseResourceVersion.value,
      draft,
    );
    await flushFacilityLocalAssets();
    if (!facilitySession.value) return;
    facilityPreview.value = await reverseProxyFacilityApi.previewEdit(
      facilitySession.value.id,
      facilitySession.value.revision,
    );
    facilityDiagnostics.value = facilityPreview.value.diagnostics ?? [];
    if (hasBlockingDiagnostic(facilityDiagnostics.value)) {
      editorDiagnosticsOpen.value = true;
      return;
    }
    const result = await reverseProxyFacilityApi.commitEdit(facilitySession.value, facilityPreview.value);
    // The record is persisted from here on. The follow-up apply is only a
    // config-plane reload request and must never roll back or negate the commit.
    committed = true;
    facilitySession.value = null;
    facilityPreview.value = null;
    facilityDiagnostics.value = [];
    let applyFailed = false;
    if (result.applyNeeded) {
      try {
        await reverseProxyFacilityApi.applyConfigRecord(result.record.recordId);
      } catch (err) {
        applyFailed = true;
        notify.push({
          title: t('applicationsPage.facilityRecordCommittedApplyFailed'),
          description: errorMessage(err, ''),
          tone: 'warning',
        });
      }
    }
    if (!applyFailed) {
      notifySuccess(
        result.applyNeeded
          ? t('applicationsPage.facilityRecordCommittedAndReloadRequested')
          : t('applicationsPage.facilityRecordCommitted'),
        result,
      );
    }
    isDirty.value = false;
    editorReady.value = false;
    resetLocalAssetState(facilityLocalAssets, []);
    // Release the save lock before navigating: the leave guard refuses to leave
    // while a commit is in flight, and the record is already persisted.
    pending.value = '';
    saveStage.value = 'idle';
    await router.push(facilityDetailPath('configs'));
  } catch (err) {
    if (isAbortError(err)) return;
    if (isConflictError(err)) {
      await discardFacilitySaveSession();
      actionError.value = t('applicationsPage.facilityRecordConflictHint');
      notify.push({ title: t('applicationsPage.facilityRecordConflict'), tone: 'warning' });
      return;
    }
    notifyError(errorMessage(err, t('common.operationFailed')), err);
  } finally {
    if (!committed) await discardFacilitySaveSession();
    pending.value = '';
    saveStage.value = 'idle';
  }
}

async function runEditorAction(action: () => Promise<void>, name = 'editor') {
  pending.value = name;
  saveStage.value = name as SaveStage;
  actionError.value = '';
  try {
    await action();
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    pending.value = '';
    saveStage.value = 'idle';
  }
}

async function ensureApplicationsLoaded() {
  if (applicationsLoadedKey === applicationsListQueryKey()) return;
  await loadApplications({ loadSelectedRuntime: false });
}

async function ensureFacilityLoaded() {
  if (!facilitiesLoaded.value || !facility.value) await loadFacilityData();
}

function markDirty() {
  isDirty.value = true;
  preview.value = null;
  facilityPreview.value = null;
  diagnostics.value = [];
  facilityDiagnostics.value = [];
}

function markAppStructuredDirty() {
  markDirty();
}

function openRowDialog(index = -1) {
  dialogKind.value = 'env';
  dialogIndex.value = index;
  Object.assign(rowDraft, index >= 0 ? appDraft.env[index] : makeKeyValueRow());
  dialogOpen.value = true;
}

function saveRowDialog() {
  const rows = appDraft.env;
  const next = { ...rowDraft };
  if (dialogIndex.value >= 0) rows[dialogIndex.value] = next;
  else rows.push(next);
  dialogOpen.value = false;
  markAppStructuredDirty();
}

function removeRow(index: number) {
  appDraft.env.splice(index, 1);
  markAppStructuredDirty();
}

function openPortDialog(index = -1) {
  dialogKind.value = 'port';
  dialogIndex.value = index;
  Object.assign(portDraft, index >= 0 ? appDraft.ports[index] : makePortRow());
  if (!portDraft.staticPort.trim()) portDraft.openFirewall = false;
  dialogOpen.value = true;
}

function savePortDialog() {
  const next = { ...portDraft };
  if (dialogIndex.value >= 0) appDraft.ports[dialogIndex.value] = next;
  else appDraft.ports.push(next);
  dialogOpen.value = false;
  markAppStructuredDirty();
}

function onPortStaticPortInput() {
  if (!portDraft.staticPort.trim()) portDraft.openFirewall = false;
}

async function loadStorageShareOptions() {
  try {
    const config = await storageShareFacilityApi.get();
    storageShareAvailable.value = config.enabled;
    const options = config.enabled ? config.servers.map((server) => {
      const label = t('applicationsPage.storageShareMountOption', { server: serverNameMap.value.get(server.serverId) || server.serverId, root: server.root });
      storageShareLabelBySource.value[`storage-share:${server.serverId}`] = label;
      return { label, value: `storage-share:${server.serverId}` };
    }) : [];
    if (options.length) storageShareLabelBySource.value['storage-share'] = options[0].label;
    storageShareOptions.value = options;
  } catch {
    storageShareAvailable.value = false;
    storageShareOptions.value = [];
  }
}

function mountSourceLabel(mount: MountRow) {
  if (mount.type === 'storage_share') {
    return storageShareLabelBySource.value[mount.source] || mount.source || t('applicationsPage.panelManagedSource');
  }
  if (mount.type === 'panel_file') {
    return panelFileLabelBySource.value.get(mount.source) || mount.source || t('applicationsPage.panelManagedSource');
  }
  return mount.source || t('applicationsPage.panelManagedSource');
}

watch(() => mountDraft.type, (type) => {
  if (type === 'storage_share') void loadStorageShareOptions();
});

function openMountDialog(index = -1) {
  dialogKind.value = 'mount';
  dialogIndex.value = index;
  Object.assign(mountDraft, index >= 0 ? appDraft.mounts[index] : makeMountRow());
  if (mountDraft.type === 'storage_share') void loadStorageShareOptions();
  dialogOpen.value = true;
}

function saveMountDialog() {
  if (mountDraft.type === 'file' && !applicationFileMountSourceValid.value) {
    notifyError(t('applicationsPage.applicationFileMountSourceRequired'));
    return;
  }
  if (mountDraft.type === 'panel_file' && !panelFileMountSourceValid.value) {
    notifyError(t('applicationsPage.panelFileMountSourceRequired'));
    return;
  }
  if (mountDraft.type === 'storage_share' && !mountDraft.source) {
    notifyError(t('applicationsPage.storageShareMountSourceRequired'));
    return;
  }
  const next = { ...mountDraft };
  if (dialogIndex.value >= 0) appDraft.mounts[dialogIndex.value] = next;
  else appDraft.mounts.push(next);
  dialogOpen.value = false;
  markAppStructuredDirty();
}

function openProxyDialog(index = -1) {
  dialogKind.value = 'proxy';
  dialogIndex.value = index;
  const base = index >= 0 ? cloneProxyRules([appDraft.reverseProxy[index]])[0] : makeProxyRule();
  Object.assign(proxyDraft, { ...base });
  proxyRelayScope.value = base.anyAccess?.relayServerIds?.length ? 'selected' : 'all';
  dialogOpen.value = true;
}

function addCommandRow() {
  appDraft.commandRows.push({ id: `cmd-${Date.now()}-${appDraft.commandRows.length}`, value: '' });
  markAppStructuredDirty();
}

function removeCommandRow(id: string) {
  const index = appDraft.commandRows.findIndex((row) => row.id === id);
  if (index >= 0) {
    appDraft.commandRows.splice(index, 1);
    markAppStructuredDirty();
  }
}

function saveProxyDialog() {
  const port = Number(proxyDraft.targetPort);
  const next: ReverseProxyRule = {
    domain: proxyDraft.domain.trim().toLowerCase(),
    targetPort: Number.isFinite(port) && port > 0 ? port : 0,
    originServerIds: [],
    anyAccess: {
      enabled: Boolean(proxyDraft.anyAccess.enabled),
      strategy: proxyDraft.anyAccess.enabled ? (proxyDraft.anyAccess.strategy || 'round_robin') : '',
      originPriority: proxyDraft.anyAccess.enabled && proxyDraft.anyAccess.strategy === 'primary_backup' ? [...proxyOriginPriorityModel.value] : [],
      relayServerIds: proxyDraft.anyAccess.enabled && proxyRelayScope.value === 'selected' ? [...(proxyDraft.anyAccess.relayServerIds ?? [])] : [],
    },
    paths: proxyDraft.paths.map((path) => ({
      path: path.path.trim() || '/',
      options: {
        ...defaultRouteOptions(),
        ...(path.options ?? {}),
      },
    })),
  };
  if (dialogIndex.value >= 0) appDraft.reverseProxy[dialogIndex.value] = next;
  else appDraft.reverseProxy.push(next);
  dialogOpen.value = false;
  markAppStructuredDirty();
}

function openProxyPathDialog(index = -1) {
  dialogKind.value = 'proxyPath';
  dialogParentIndex.value = dialogIndex.value;
  dialogIndex.value = index;
  Object.assign(proxyPathDraft, index >= 0 ? cloneProxyPath(proxyDraft.paths[index]) : makeProxyPath());
  proxyRequestHeaders.value = (proxyPathDraft.options?.requestHeaders ?? []).map((header) => makeKeyValueRow(header.name, header.value));
  proxyResponseHeaders.value = (proxyPathDraft.options?.responseHeaders ?? []).map((header) => makeKeyValueRow(header.name, header.value));
  dialogOpen.value = true;
}

function saveProxyPathDialog() {
  const next = {
    ...proxyPathDraft,
    options: {
      ...defaultRouteOptions(),
      ...(proxyPathDraft.options ?? {}),
      requestHeaders: proxyRequestHeaders.value.filter((row) => row.key.trim() || row.value.trim()).map((row) => ({ name: row.key.trim(), value: row.value })),
      responseHeaders: proxyResponseHeaders.value.filter((row) => row.key.trim() || row.value.trim()).map((row) => ({ name: row.key.trim(), value: row.value })),
    },
  };
  if (dialogIndex.value >= 0) proxyDraft.paths[dialogIndex.value] = next;
  else proxyDraft.paths.push(next);
  dialogKind.value = 'proxy';
  dialogIndex.value = dialogParentIndex.value;
}

function cancelDialog() {
  if (dialogKind.value === 'proxyPath') {
    dialogKind.value = 'proxy';
    dialogIndex.value = dialogParentIndex.value;
    return;
  }
  dialogOpen.value = false;
}

function openFacilityPathDialog(pathIndex = -1) {
  dialogKind.value = 'facilityPath';
  dialogParentIndex.value = -1;
  dialogIndex.value = pathIndex;
  Object.keys(facilityPathErrors).forEach((key) => delete facilityPathErrors[key]);
  Object.assign(facilityPathDraft, pathIndex >= 0 ? cloneFacilityPath(facilityRecordDraft.paths[pathIndex]) : makeFacilityPath());
  if (!facilityPathDraft.options) Object.assign(facilityPathDraft, { options: defaultRouteOptions() });
  facilityRequestHeaders.value = (facilityPathDraft.options?.requestHeaders ?? []).map((header) => makeKeyValueRow(header.name, header.value));
  facilityResponseHeaders.value = (facilityPathDraft.options?.responseHeaders ?? []).map((header) => makeKeyValueRow(header.name, header.value));
  dialogOpen.value = true;
}

function clearFacilityPathError(field: string) {
  delete facilityPathErrors[field];
}

function saveFacilityPathDialog() {
  const nextErrors = validateFacilityPathFields(facilityPathDraft);
  Object.keys(facilityPathErrors).forEach((key) => delete facilityPathErrors[key]);
  Object.assign(facilityPathErrors, nextErrors);
  if (Object.keys(facilityPathErrors).length) return;
  const next = cloneFacilityPath(facilityPathDraft);
  next.options = {
    ...defaultRouteOptions(),
    ...(next.options ?? {}),
    requestHeaders: facilityRequestHeaders.value.filter((row) => row.key.trim() || row.value.trim()).map((row) => ({ name: row.key.trim(), value: row.value })),
    responseHeaders: facilityResponseHeaders.value.filter((row) => row.key.trim() || row.value.trim()).map((row) => ({ name: row.key.trim(), value: row.value })),
  };
  if (dialogIndex.value >= 0) facilityRecordDraft.paths[dialogIndex.value] = next;
  else facilityRecordDraft.paths.push(next);
  dialogOpen.value = false;
  markDirty();
}

function removeFacilityRecordPath(index: number) {
  facilityRecordDraft.paths.splice(index, 1);
  markDirty();
}

function removeAt<T>(items: T[], index: number) {
  items.splice(index, 1);
  markAppStructuredDirty();
}

async function saveProxyPlaneDraft() {
  if (proxyPlaneSaving.value || !proxyPlaneDirty.value) return;
  if (proxyPlaneBlockedReferences.value.length) {
    notify.push({ title: t('applicationsPage.gatewayRemovalBlocked'), tone: 'warning' });
    return;
  }
  proxyPlaneSaving.value = true;
  proxyPlaneError.value = '';
  proxyPlaneBlockedError.value = '';
  try {
    const saved = await reverseProxyFacilityApi.saveProxyPlane({ deploymentServers: [...proxyPlaneDraft.value] });
    proxyPlane.value = saved;
    proxyPlaneDraft.value = [...saved.deploymentServers];
    proxyPlaneDraftTouched.value = false;
    notifySuccess(t('applicationsPage.gatewaySaved'));
    await loadFacilityDetailData();
  } catch (err) {
    const blocked = proxyPlaneConflictDomains(err);
    if (blocked.length) {
      proxyPlaneBlockedError.value = blocked.join(', ');
      notify.push({ title: t('applicationsPage.gatewayRemovalBlocked'), tone: 'warning' });
      return;
    }
    proxyPlaneError.value = errorMessage(err, t('common.operationFailed'));
    notifyError(proxyPlaneError.value, err);
  } finally {
    proxyPlaneSaving.value = false;
  }
}

/** Server-side FAC-RP-006 guard fallback: list the records that still reference removed gateways. */
function proxyPlaneConflictDomains(error: unknown): string[] {
  if (!(error instanceof ApiError) || error.code !== 'facility_proxy_plane_gateway_in_use') return [];
  const details = error.details as { records?: Array<{ recordId?: string }> } | undefined;
  return (details?.records ?? []).map((item) => item.recordId ?? '').filter(Boolean);
}

async function ensureProxyRunning() {
  await runOperation(
    'facility-reconcile',
    () => reverseProxyFacilityApi.reconcile(),
    'applicationsPage.ensureProxyAccepted',
    'applicationsPage.ensureProxyAcceptedWithoutId',
  );
}

async function applyFacilityRecord(record: ConfigRecord) {
  if (configRecordActionKey.value) return;
  configRecordActionKey.value = `apply:${record.recordId}`;
  try {
    await reverseProxyFacilityApi.applyConfigRecord(record.recordId);
    notifySuccess(t('applicationsPage.configRecordApplyRequested', { domain: record.recordId }));
  } catch (err) {
    notifyError(errorMessage(err, t('common.operationFailed')), err);
  } finally {
    configRecordActionKey.value = '';
  }
}

function askDeleteFacilityRecord(record: ConfigRecord) {
  deleteRecordTarget.value = record;
  deleteRecordOpen.value = true;
}

async function confirmDeleteFacilityRecord() {
  const record = deleteRecordTarget.value;
  if (!record) return;
  configRecordActionKey.value = `delete:${record.recordId}`;
  try {
    await reverseProxyFacilityApi.deleteConfigRecord(record.recordId, record.version);
    notifySuccess(t('applicationsPage.configRecordDeleted'));
    deleteRecordOpen.value = false;
    deleteRecordTarget.value = null;
    await loadFacilityDetailData();
  } catch (err) {
    if (isConflictError(err)) {
      notify.push({ title: t('applicationsPage.configRecordDeleteConflict'), tone: 'warning' });
      deleteRecordOpen.value = false;
      deleteRecordTarget.value = null;
      await loadFacilityDetailData();
      return;
    }
    notifyError(errorMessage(err, t('common.operationFailed')), err);
  } finally {
    configRecordActionKey.value = '';
  }
}

function encodeBase64Utf8(value: string) {
  const bytes = new TextEncoder().encode(value);
  let binary = '';
  bytes.forEach((byte) => { binary += String.fromCharCode(byte); });
  return btoa(binary);
}

async function downloadCommittedApplicationFile(file: ApplicationFile) {
  if (!selectedApplication.value.id || !file.name) return;
  await runFileAction(`committed:${file.name}`, async () => {
    saveBlobDownload(await applicationsApi.downloadFile(selectedApplication.value.id, file.name, applicationFileDownloadName(file)));
  });
}

function applicationFileDownloadName(file: ApplicationFile) {
  if (file.kind === 'archive' && file.contentType) return file.contentType;
  return file.name.split('/').filter(Boolean).at(-1) || 'application-file';
}

async function runFileAction(key: string, action: () => Promise<void>) {
  fileActionPending.value = key;
  fileActionErrors.value = { ...fileActionErrors.value, [key]: '' };
  try {
    await action();
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    fileActionPending.value = '';
  }
}

function openApplicationTasks(operationId = '', executionId = '') {
  void router.push({
    path: '/events',
    query: {
      operationId: operationId || undefined,
      executionId: executionId || undefined,
      resourceType: 'application',
      resourceId: selectedApplication.value.id,
    },
  });
}

function openFacilityTasks() {
  void router.push({ path: '/events', query: { resourceType: 'facility_app', resourceId: facilityKind.value } });
}

function pathTarget(path: FacilityRoutePath) {
  if (path.ruleType === 'redirect') return path.redirectUrl || t('common.notAvailable');
  if (path.ruleType === 'proxy_pass') return path.proxyUrl || t('common.notAvailable');
  return path.assetName;
}

function facilityPathTargetLabel(path: FacilityRoutePath) {
  if (path.ruleType === 'static') {
    const source = path.sourceType === 'uploaded_file'
      ? t('applicationsPage.sourceType.uploaded_file')
      : t('applicationsPage.sourceType.uploaded_bundle');
    return `${source}: ${pathTarget(path) || t('common.notAvailable')}`;
  }
  return pathTarget(path) || t('common.notAvailable');
}

function emptyApplication(): ApplicationDto {
  return { id: '', version: 0, kind: 'application', name: '', enabled: true, specYaml: '', deploymentMode: 'all', deploymentServers: [], reverseProxy: [], generation: 0, specHash: '', imageUpdateAvailable: false, jobId: '', namespace: '', createdAt: '', updatedAt: '' };
}

function applicationFromSummary(app?: ApplicationSummaryDto | null): ApplicationDto | null {
  if (!app) return null;
  return {
    ...emptyApplication(),
    id: app.id,
    name: app.name,
    enabled: app.enabled,
    imageReference: app.imageReference,
    imageUpdateAvailable: app.imageUpdateAvailable,
    jobId: app.jobId,
    namespace: app.namespace,
    runtimeStatus: app.runtimeStatus,
    lastError: app.lastError,
    planningError: app.planningError,
    updatedAt: app.updatedAt,
  };
}

function emptyFacility(): ReverseProxyConfig {
  return { id: 'reverse_proxy', version: 0, deploymentServers: [], domains: [], staticAssets: [], routeSummaries: [], applicationRoutes: [], updatedAt: '', routes: 0, enabledServers: [] };
}

function isAbortError(error: unknown) {
  return Boolean(error && typeof error === 'object' && 'code' in error && (error as { code?: string }).code === 'request_aborted');
}

function cancelRuntimeLoad() {
  stopRuntimePolling();
  runtimeController?.abort();
  runtimeRequestId += 1;
  detailLoading.value = false;
  runtimeLoading.value = false;
}

function cancelApplicationDetailLoad() {
  applicationDetailController?.abort();
  applicationDetailRequestId += 1;
  detailLoading.value = false;
}

function cancelEditorQuery() {
  editorQueryController?.abort();
  editorQueryRequestId += 1;
}

onMounted(async () => {
  window.addEventListener('beforeunload', handleBeforeUnload);
  document.addEventListener('visibilitychange', handleVisibilityChange);
  await ensureModeData();
  if (isAppEditor.value) await startApplicationEditor();
  if (isFacilityEditor.value) await startFacilityEditor();
});

onBeforeUnmount(() => {
  if (searchTimer) clearTimeout(searchTimer);
  if (dnsPollTimer) clearTimeout(dnsPollTimer);
  window.removeEventListener('beforeunload', handleBeforeUnload);
  document.removeEventListener('visibilitychange', handleVisibilityChange);
  pageLoadController?.abort();
  cancelRuntimeLoad();
  cancelApplicationDetailLoad();
  cancelRowImageLoads();
  cancelEditorQuery();
});
</script>

<template>
  <TableDetailLayout v-if="mode === 'apps'" class="h-full" :has-detail="detailOpen">
    <template #list>
      <ConsolePage :title="t('routes.applications.title')" :description="t('routes.applications.description')">
        <template #actions>
          <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
          <Button size="sm" variant="primary" @click="router.push('/applications/apps/create')"><Plus />{{ t('common.create') }}</Button>
        </template>

        <ListPage :state="applicationListState">
          <template #toolbar>
            <ListCommandBar :label="t('routes.applications.title')">
              <template #search>
                <SearchInput v-model="search" clearable :placeholder="t('applicationsPage.searchPlaceholder')" :label="t('common.search')" :clear-label="t('common.clearSearch')" />
              </template>
            </ListCommandBar>
          </template>

          <template #loading>
            <Table class="h-full max-lg:max-h-list" :columns="applicationColumns" :rows="[]" :loading="true" :loading-label="t('common.loading')" />
          </template>
          <template #error>
            <EmptyState variant="page" :title="t('common.loadFailed')" :description="error">
              <template #actions>
                <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button>
              </template>
            </EmptyState>
          </template>
          <template #empty>
            <EmptyState variant="inset" :title="t('applicationsPage.emptyApplications')" :description="t('applicationsPage.emptyApplicationsHint')" />
          </template>

          <Table
            class="h-full max-lg:max-h-list"
            row-height="auto"
            :columns="applicationColumns"
            :rows="appRows"
            row-key="id"
            :active-key="selectedId"
            :loading="loading && !applications.length"
            :loading-label="t('common.loading')"
          >
            <template #name="{ row }">
              <div class="grid min-w-0 gap-0.5">
                <TableDetailName class="max-w-44 sm:max-w-72" :name="row.app.name" :label="t('common.openDetails', { name: row.app.name })" @click="openApplicationDetail(row.app)" />
                <!-- 副行合并 namespace + image reference：列表不再单列 image，避免重复信息列。 -->
                <span class="flex min-w-0 items-center gap-1 text-caption text-muted-foreground">
                  <span v-if="row.app.namespace" class="shrink-0">{{ row.app.namespace }}</span>
                  <span v-if="row.app.namespace" class="shrink-0" aria-hidden="true">·</span>
                  <span v-if="!loading && !(rowImageLoading[row.app.id] && !rowImageReference(row.app))" class="min-w-0 truncate">{{ rowImageReference(row.app) || t('common.notAvailable') }}</span>
                  <span v-else class="motion-skeleton h-3 w-32 rounded bg-muted animate-pulse" aria-hidden="true" />
                </span>
                <!-- <sm：状态/实例列被隐藏，折入名称副行的紧凑摘要，避免移动端丢信息。 -->
                <div class="flex flex-wrap items-center gap-1 sm:hidden">
                  <StatusBadge :status="applicationStatus(row.app)" :tone="statusTone(applicationStatus(row.app))" :label="t(`applicationsPage.status.${applicationStatus(row.app)}`)" />
                  <template v-if="row.counts.healthy || row.counts.abnormal || row.counts.deploying">
                    <Badge v-if="row.counts.healthy > 0" tone="success">{{ t('applicationsPage.nodeBucket.healthy', { count: row.counts.healthy }) }}</Badge>
                    <Badge v-if="row.counts.abnormal > 0" tone="danger">{{ t('applicationsPage.nodeBucket.abnormal', { count: row.counts.abnormal }) }}</Badge>
                    <Badge v-if="row.counts.deploying > 0" tone="warning">{{ t('applicationsPage.nodeBucket.deploying', { count: row.counts.deploying }) }}</Badge>
                  </template>
                  <Badge v-else-if="row.app.imageUpdateAvailable" tone="warning">{{ t('applicationsPage.imageUpdate') }}</Badge>
                </div>
              </div>
            </template>
            <template #status="{ row }">
              <div class="flex flex-nowrap items-center gap-1">
                <StatusBadge :status="applicationStatus(row.app)" :tone="statusTone(applicationStatus(row.app))" :label="t(`applicationsPage.status.${applicationStatus(row.app)}`)" />
                <!-- 单枚综合徽标之外只保留一枚紧凑次级标记：规划失败 > 需关注 > 有镜像更新。 -->
                <Badge v-if="row.app.planningError" tone="danger" class="whitespace-nowrap">{{ t('applicationsPage.status.planning_failed') }}</Badge>
                <Badge v-else-if="row.app.reconcileStopped" tone="warning" class="whitespace-nowrap">{{ t('applicationsPage.status.attention') }}</Badge>
                <Badge v-else-if="row.app.imageUpdateAvailable" tone="warning" class="whitespace-nowrap">{{ t('applicationsPage.imageUpdate') }}</Badge>
              </div>
            </template>
            <template #instances="{ row }">
              <!-- 实例摘要合并为一格单行：健康 / 异常 / 部署中；全 0 时显示不可用。 -->
              <div class="flex flex-nowrap items-center gap-1 whitespace-nowrap">
                <template v-if="row.counts.healthy || row.counts.abnormal || row.counts.deploying">
                  <Badge v-if="row.counts.healthy > 0" tone="success">{{ t('applicationsPage.nodeBucket.healthy', { count: row.counts.healthy }) }}</Badge>
                  <Badge v-if="row.counts.abnormal > 0" tone="danger">{{ t('applicationsPage.nodeBucket.abnormal', { count: row.counts.abnormal }) }}</Badge>
                  <Badge v-if="row.counts.deploying > 0" tone="warning">{{ t('applicationsPage.nodeBucket.deploying', { count: row.counts.deploying }) }}</Badge>
                </template>
                <span v-else class="text-caption text-muted-foreground">{{ t('common.notAvailable') }}</span>
              </div>
            </template>
            <template #updatedAt="{ row }">
              <span class="whitespace-nowrap text-muted-foreground">{{ formatDateTime(row.app.updatedAt, t('common.never')) }}</span>
            </template>
            <template #actions="{ row }">
              <div class="flex flex-wrap items-center justify-end gap-1">
                <Button size="sm" variant="secondary" @click="openApplicationDetail(row.app)"><Eye />{{ t('common.details') }}</Button>
                <!-- <wide：行内编辑/同步收进行内 More 菜单，只保留详情 + 更多。 -->
                <div class="hidden items-center gap-1 wide:flex">
                  <Button size="sm" variant="ghost" @click="router.push(`/applications/apps/${encodeURIComponent(row.app.id)}/edit`)"><Wrench />{{ t('common.edit') }}</Button>
                  <Button size="sm" variant="ghost" :loading="rowActionKey === `deploy:${row.app.id}`" :disabled="Boolean(rowActionKey)" @click="deployFor(row.app)"><Rocket />{{ t('applicationsPage.sync') }}</Button>
                </div>
                <Dropdown align="right">
                  <template #trigger>
                    <IconButton size="sm" :label="t('common.moreActions')"><MoreHorizontal /></IconButton>
                  </template>
                  <!-- More 必须始终包含窄屏被隐藏的行内主操作。 -->
                  <DropdownItem @click="router.push(`/applications/apps/${encodeURIComponent(row.app.id)}/edit`)"><Wrench />{{ t('common.edit') }}</DropdownItem>
                  <DropdownItem :disabled="Boolean(rowActionKey)" @click="deployFor(row.app)"><Rocket />{{ t('applicationsPage.sync') }}</DropdownItem>
                  <DropdownItem :disabled="!row.app.enabled" @click="ask('stop', row.app.id)"><Square />{{ t('applicationsPage.disable') }}</DropdownItem>
                  <DropdownItem :disabled="!row.app.imageUpdateAvailable || Boolean(rowActionKey)" @click="updateImageFor(row.app)"><UploadCloud />{{ t('applicationsPage.updateImage') }}</DropdownItem>
                  <DropdownItem @click="openLogsFor(row.app)"><History />{{ t('applicationsPage.logs') }}</DropdownItem>
                  <DropdownItem tone="danger" :disabled="rowPendingId === row.app.id" @click="askDelete(row.app)"><Trash2 />{{ t('common.delete') }}</DropdownItem>
                </Dropdown>
              </div>
            </template>
            <template #empty>
              <EmptyState
                variant="inset"
                :title="search ? t('common.filterEmpty') : t('applicationsPage.emptyApplications')"
                :description="search ? t('common.filterEmptyHint') : t('applicationsPage.emptyApplicationsHint')"
              />
            </template>
          </Table>

          <template #pagination>
            <PaginationBar v-model:page="page" :page-size="pageSize" :total="totalApplications" :loading="loading" :previous-label="t('common.previous')" :next-label="t('common.next')" />
          </template>
        </ListPage>
      </ConsolePage>
    </template>

    <template #detail>
      <ConsolePage
        :title="selectedApplication.name"
        :description="selectedApplication.imageReference || selectedApplication.imageUpdateTargets?.find((target) => target.reference)?.reference || ''"
        :back-label="t('common.backToList')"
        :breadcrumbs="applicationDetailBreadcrumbs"
        :body-scroll="false"
        @back="backToApplicationList"
      >
        <template #actions>
          <Button size="sm" variant="secondary" @click="router.push(`/applications/apps/${encodeURIComponent(selectedApplication.id)}/edit`)"><Wrench />{{ t('common.edit') }}</Button>
          <ActivityLink resource-type="application" :resource-id="selectedApplication.id" />
        </template>

        <EmptyState v-if="detailError" variant="page" :title="t('common.loadFailed')" :description="detailError">
          <template #actions>
            <Button size="sm" :loading="detailLoading" @click="retryApplicationDetail"><RefreshCcw />{{ t('common.retry') }}</Button>
          </template>
        </EmptyState>
        <div v-else-if="!currentApplicationSummary && !currentApplication" class="grid content-start gap-4">
          <div class="motion-skeleton h-7 w-48 rounded bg-muted animate-pulse" />
          <div class="motion-skeleton h-4 w-80 max-w-full rounded bg-muted animate-pulse" />
          <div class="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <div v-for="item in 3" :key="item" class="motion-skeleton h-24 rounded-overlay bg-muted animate-pulse" />
          </div>
          <div class="motion-skeleton h-72 rounded-overlay bg-muted animate-pulse" />
        </div>
        <ApplicationDetailPane
          v-else
          v-model:persistent-server-id="persistentServerId"
          :application="selectedApplication"
          :app-status="appStatus"
          :planning-error="planningError"
          :node-counts="selectedNodeCounts"
          :runtime="currentRuntime"
          :runtime-loading="runtimeLoading"
          :runtime-error="runtimeError"
          :files="applicationFiles[selectedApplication.id] || []"
          :file-action-pending="fileActionPending"
          :persistent-server-options="persistentServerOptions"
          :pending="pending"
          :tabs="appDetailTabs"
          :active-tab="appTab"
          :show-loading="detailLoading && !applicationDetails[selectedId]"
          :server-name="serverDisplayName"
          @update:active-tab="selectAppTab"
          @retry-runtime="retryApplicationRuntime"
          @open-operation="openApplicationTasks"
          @download-file="downloadCommittedApplicationFile"
          @download-persistent-data="downloadPersistentData(selectedApplication)"
          @restore-persistent-data="restorePersistentData"
        />
      </ConsolePage>
    </template>
  </TableDetailLayout>

  <ConsolePage v-else-if="mode === 'facilityCatalog'" :title="t('routes.facilityApps.title')" :description="t('routes.facilityApps.description')">
    <template #actions>
      <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
    </template>
    <FacilityCatalog
      :facilities="facilities"
      :loading="loading"
      :pending="pending"
      :facility="facility"
      :storage-share-config="storageShareConfig"
      :network-probe-config="networkProbeConfig"
      :metrics-config="metricsConfig"
      :server-total="servers.length"
      @open="(kind) => router.push(`/applications/facility-apps/${kind}`)"
      @reconcile="(kind) => runOperation(`facility-reconcile-${kind}`, () => kind === 'storage-share' ? storageShareFacilityApi.reconcile() : reverseProxyFacilityApi.reconcile(), kind === 'storage-share' ? 'applicationsPage.storageShareReconcileAccepted' : 'applicationsPage.gatewayReconcileAccepted', 'applicationsPage.gatewayReconcileAcceptedWithoutId')"
    />
  </ConsolePage>

  <ConsolePage
    v-else-if="mode === 'facilityDetail' || mode === 'facilityConfig'"
    :back-label="t('common.back')"
    @back="goBackFromFacilityPage"
    :title="facilityPageTitle"
    :description="facilityPageDescription"
    :breadcrumbs="facilityBreadcrumbs"
    :body-scroll="isStorageShareFacility || isMetricsFacility"
    width="content"
  >
    <template #actions>
      <template v-if="mode === 'facilityDetail'">
        <Button v-if="currentFacilitySummary" size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
        <!-- 网络探测/指标设施没有协调任务/活动事件，不展示任务入口避免死链。 -->
        <Button v-if="currentFacilitySummary && !isNetworkProbeFacility && !isMetricsFacility" size="sm" variant="ghost" @click="openFacilityTasks"><ClipboardList />{{ t('applicationsPage.viewTasks') }}</Button>
      </template>
    </template>

    <template v-if="isStorageShareFacility">
      <StorageShareFacility :mode="mode" :servers="servers" />
    </template>

    <template v-else-if="isNetworkProbeFacility">
      <NetworkProbeFacility :mode="mode" :servers="servers" />
    </template>

    <template v-else-if="isMetricsFacility">
      <MetricsFacility :mode="mode" :servers="servers" />
    </template>

    <template v-else-if="mode === 'facilityConfig'">
      <EditorPage>
        <div v-if="!editorReady" class="relative grid min-h-64 place-items-center">
          <LoadingOverlay v-if="editorLoading || !actionError" />
          <div v-else class="grid max-w-md justify-items-center gap-3 text-center">
            <AlertTriangle class="size-6 text-danger" aria-hidden="true" />
            <p class="m-0 text-body text-danger">{{ actionError }}</p>
            <div class="flex flex-wrap justify-center gap-2">
              <Button size="sm" :loading="pending === 'editor-reload'" @click="reloadFacilityEditor">{{ t('common.retry') }}</Button>
              <Button size="sm" variant="secondary" :disabled="saving" @click="cancelFacilityRecordEdit">{{ t('common.cancel') }}</Button>
            </div>
          </div>
        </div>
        <div class="grid gap-3">
          <InfoBar v-if="saving" tone="info">{{ t(`applicationsPage.saveStage.${saveStage}`) }}</InfoBar>
          <InfoBar v-if="editorReady && actionError" tone="danger">
            <div class="flex flex-wrap items-center justify-between gap-3">
              <span class="min-w-0">{{ actionError }}</span>
              <Button size="sm" :loading="pending === 'editor-reload'" @click="reloadFacilityEditor">{{ t('applicationsPage.reloadFacilityDraft') }}</Button>
            </div>
          </InfoBar>
        </div>

        <!--
          编辑器主区单列全宽（不再保留右侧摘要/检查结果栏）：保存前检查仍在内部 preview 阶段完成，
          仅当出现 blocking diagnostics 时用共享 Dialog 展示检查结果。
        -->
        <div v-if="editorReady" class="relative grid min-w-0 content-start gap-4">
          <LoadingOverlay v-if="saving" :label="t(`applicationsPage.saveStage.${saveStage}`)" />
          <div class="grid min-w-0 content-start gap-4">
            <p class="m-0 text-caption text-muted-foreground">{{ t('applicationsPage.facilityRecordEditorFlowHint') }}</p>

            <DetailSection :title="t('applicationsPage.facilityRecordIdentity')" :description="t('applicationsPage.facilityRecordIdentityHint')">
              <div class="grid gap-3 p-4 sm:grid-cols-2">
                <FormField :label="t('applicationsPage.domain')" :error="facilityRecordErrors.domain ? t(facilityRecordErrors.domain) : ''">
                  <template #default="{ id, describedBy, invalid }">
                    <Input :id="id" :aria-describedby="describedBy" v-model="facilityRecordDraft.domain" :invalid="invalid" :disabled="!facilityRecordIsNew" @input="markDirty" />
                  </template>
                </FormField>
                <div class="grid content-start gap-0.5">
                  <span class="text-caption text-muted-foreground">{{ t('applicationsPage.facilityRecordVersion') }}</span>
                  <div class="flex items-center gap-2"><Badge tone="neutral">{{ facilityRecord ? facilityRecord.version : t('applicationsPage.facilityRecordNewVersion') }}</Badge></div>
                </div>
              </div>
              <p v-if="!facilityRecordIsNew" class="m-0 px-4 pb-4 text-caption text-muted-foreground">{{ t('applicationsPage.facilityRecordDomainReadonly') }}</p>
            </DetailSection>

            <DetailSection :title="t('applicationsPage.facilityRecordOrigins')" :description="t('applicationsPage.facilityRecordOriginsHint')">
              <EmptyState v-if="!facilityGatewayServers.length" variant="inset" :title="t('applicationsPage.facilityRecordNoGateways')" :description="t('applicationsPage.facilityRecordNoGatewaysHint')" />
              <div v-else class="p-4">
                <ServerMultiPicker v-model="facilityRecordOriginModel" :servers="facilityGatewayServers" :label="t('applicationsPage.facilityRecordOrigins')" />
              </div>
              <div v-if="facilityRecordErrors.originServers" class="px-4 pb-4">
                <InfoBar tone="danger">{{ t(facilityRecordErrors.originServers) }}</InfoBar>
              </div>
            </DetailSection>

            <DetailSection :title="t('applicationsPage.anyAccess')" :description="t('applicationsPage.anyAccessHint')">
              <div class="grid gap-3 p-4">
                <FormField :label="t('applicationsPage.anyAccess')">
                  <template #default="{ id }">
                    <Switch :id="id" v-model="facilityRecordAnyAccessModel" :label="t('applicationsPage.anyAccess')" />
                  </template>
                </FormField>
                <div v-if="facilityRecordDraft.anyAccess.enabled" class="grid gap-3">
                  <FormField :label="t('applicationsPage.loadBalancingStrategy')">
                    <template #default="{ id }">
                      <Select :id="id" v-model="facilityRecordDraft.anyAccess.strategy" :options="anyAccessStrategyOptions" @change="markDirty" />
                    </template>
                  </FormField>
                  <FormField v-if="facilityRecordDraft.anyAccess.strategy === 'primary_backup'" :label="t('applicationsPage.primaryOriginServer')" :error="facilityRecordErrors.primaryOrigin ? t(facilityRecordErrors.primaryOrigin) : ''">
                    <template #default="{ id, describedBy, invalid }">
                      <Select :id="id" :aria-describedby="describedBy" v-model="facilityRecordDraft.anyAccess.primaryOriginServerId" :options="facilityRecordPrimaryOptions" :placeholder="t('applicationsPage.selectPrimaryOrigin')" @change="markDirty" />
                    </template>
                  </FormField>
                  <FormField :label="t('applicationsPage.anyAccessRelayScope')">
                    <template #default="{ id }">
                      <Select :id="id" v-model="facilityRecordRelayScope" :options="anyAccessRelayScopeOptions" @change="markDirty" />
                    </template>
                  </FormField>
                  <ServerMultiPicker v-if="facilityRecordRelayScope === 'selected'" v-model="facilityRecordRelayServerIdsModel" :servers="facilityRecordRelayServers" :label="t('applicationsPage.anyAccessRelayServers', { count: facilityRecordDraft.anyAccess.relayServerIds?.length ?? 0 })" />
                  <p v-if="facilityRecordRelayScope === 'selected' && !facilityRecordRelayServers.length" class="m-0 text-caption text-muted-foreground">{{ t('applicationsPage.anyAccessNoRelayServers') }}</p>
                </div>
              </div>
            </DetailSection>

            <DetailSection :title="t('applicationsPage.facilityRecordPaths')" :description="t('applicationsPage.facilityRecordPathsHint')">
              <template #actions>
                <Button size="sm" @click="openFacilityPathDialog()"><Plus />{{ t('common.addPath') }}</Button>
              </template>
              <div v-for="(path, pathIndex) in facilityRecordDraft.paths" :key="`${path.path}-${pathIndex}`" class="grid gap-3 p-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
                <div class="min-w-0">
                  <div class="flex flex-wrap items-center gap-2">
                    <Badge tone="neutral" class="panel-mono">{{ path.path || '/' }}</Badge>
                    <Badge tone="info">{{ path.ruleType ? t(`applicationsPage.routeType.${path.ruleType}`) : t('common.notAvailable') }}</Badge>
                  </div>
                  <span class="mt-1 block text-caption text-muted-foreground">{{ facilityPathTargetLabel(path) }}</span>
                </div>
                <div class="flex flex-wrap justify-start gap-2 sm:justify-end">
                  <Button size="sm" @click="openFacilityPathDialog(pathIndex)">{{ t('common.edit') }}</Button>
                  <Button size="sm" variant="danger" @click="removeFacilityRecordPath(pathIndex)">{{ t('common.delete') }}</Button>
                </div>
              </div>
              <EmptyState v-if="!facilityRecordDraft.paths.length" variant="inset" :title="t('applicationsPage.facilityRecordNoPaths')" :description="t('applicationsPage.noPathsHint')" />
              <div v-if="facilityRecordErrors.paths" class="px-4 pb-4">
                <InfoBar tone="danger">{{ t(facilityRecordErrors.paths) }}</InfoBar>
              </div>
            </DetailSection>
          </div>

        </div>

        <!-- 资产/代码编辑器全宽：不放进 xl 双列的窄主列，避免 YAML/模板被压窄。 -->
        <div class="mt-4 surface-card p-4">
          <AssetFileManager
            :items="facilityAssetItems" :adapter="facilityAssetAdapter" :disabled="!editorReady || saving"
            :labels="{ title: t('applicationsPage.facilityRecordAssets'), hint: t('applicationsPage.facilityRecordAssetsHint'), uploadAsset: t('applicationsPage.uploadAsset'), uploadAssetTitle: t('applicationsPage.uploadAssetTitle'), uploadType: t('applicationsPage.uploadType'), uploadTypeText: t('applicationsPage.uploadTypeText'), uploadTypeBinary: t('applicationsPage.uploadTypeBinary'), uploadTypeArchive: t('applicationsPage.uploadTypeArchive'), uploadFile: t('applicationsPage.uploadFile'), uploadArchive: t('applicationsPage.uploadArchive'), operationFailed: t('applicationsPage.operationFailed'), edit: t('common.edit'), replace: t('common.replace'), download: t('common.download'), delete: t('common.delete'), bytes: t('applicationsPage.bytes'), noAssets: t('applicationsPage.noAssets'), noAssetsHint: t('applicationsPage.noAssetsHint'), textTitle: t('applicationsPage.editTextFile'), newTextTitle: t('applicationsPage.newTextFile'), name: t('applicationsPage.assetReferenceName'), nameHint: t('applicationsPage.assetReferenceNameHint'), filename: t('applicationsPage.assetDownloadFilename'), filenameHint: t('applicationsPage.assetDownloadFilenameHint'), language: t('applicationsPage.highlightLanguage'), content: t('applicationsPage.fileContent'), loading: t('applicationsPage.fileLoading'), loadFailed: t('applicationsPage.fileLoadFailed'), cancel: t('common.cancel'), save: t('common.save'), close: t('common.close'), reload: t('applicationsPage.discardTextAndReload'), deleteTitle: t('applicationsPage.confirm.facility-asset-delete.title'), deleteDescription: t('applicationsPage.confirm.facility-asset-delete.description'), confirmDelete: t('common.delete') }"
          />
        </div>

        <template #footer>
          <div class="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
            <div class="text-caption text-muted-foreground">{{ editorDirty ? t('applicationsPage.unsavedChanges') : t('applicationsPage.readyToCommit') }}</div>
            <div class="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap">
              <Button variant="secondary" class="w-full sm:w-auto" :disabled="saving" @click="cancelFacilityRecordEdit">{{ t('common.cancel') }}</Button>
              <Button variant="primary" class="w-full sm:w-auto" :loading="pending === 'commit'" :disabled="!editorReady || saving || !editorDirty" @click="commitFacilityRecord"><Save />{{ t('applicationsPage.facilityRecordSave') }}</Button>
            </div>
          </div>
        </template>
      </EditorPage>
    </template>

    <div v-else class="grid h-full min-h-0 min-w-0 content-start gap-4">
      <EmptyState v-if="!currentFacilitySummary" variant="page" :title="t('applicationsPage.facilityUnavailable')" :description="t('applicationsPage.facilityUnavailableDescription', { kind: facilityKind })" />
      <EmptyState v-else-if="error && !facility" variant="page" :title="t('common.loadFailed')" :description="error">
        <template #actions>
          <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button>
        </template>
      </EmptyState>
      <div v-else-if="loading && !facility" class="relative grid h-full min-h-64 place-items-center">
        <LoadingOverlay />
      </div>
      <Tabs v-else-if="facility" v-model="activeFacilityTab" :tabs="facilityDetailTabs" class="h-full min-h-0">
        <section v-if="facilityTab === 'proxy'" class="grid h-full min-h-0 content-start gap-4 overflow-y-auto pr-1">
          <DetailSection :title="t('applicationsPage.proxyPlaneTitle')" :description="t('applicationsPage.proxyPlaneHint')">
            <template #actions>
              <Button size="sm" variant="secondary" :loading="pending === 'facility-reconcile'" :disabled="Boolean(pending)" @click="ensureProxyRunning"><Rocket />{{ t('applicationsPage.ensureProxyRunning') }}</Button>
              <Button size="sm" variant="primary" :loading="proxyPlaneSaving" :disabled="proxyPlaneSaving || !proxyPlaneDirty || Boolean(proxyPlaneBlockedReferences.length)" @click="saveProxyPlaneDraft"><Save />{{ t('applicationsPage.gatewaySave') }}</Button>
            </template>
            <div class="grid gap-3 p-4">
              <p class="m-0 text-caption text-muted-foreground">{{ t('applicationsPage.gatewayEditorFlowHint') }}</p>
              <EmptyState v-if="proxyPlaneError" variant="inset" :title="t('common.loadFailed')" :description="proxyPlaneError">
                <template #actions>
                  <Button size="sm" :loading="loading" @click="loadFacilityDetailData"><RefreshCcw />{{ t('common.retry') }}</Button>
                </template>
              </EmptyState>
              <div v-else-if="!proxyPlane" class="relative grid min-h-32 place-items-center"><LoadingOverlay /></div>
              <template v-else>
                <div class="grid gap-1">
                  <span class="text-caption text-muted-foreground">{{ t('applicationsPage.gatewayNodes') }}</span>
                  <ServerMultiPicker v-model="proxyPlaneDraftModel" :servers="serverOptions" :label="t('applicationsPage.gatewayNodes')" />
                  <p class="m-0 text-caption text-secondary-foreground">{{ t('applicationsPage.gatewayNodesHint') }}</p>
                </div>
                <InfoBar v-if="proxyPlaneBlockedReferences.length || proxyPlaneBlockedError" tone="danger" :title="t('applicationsPage.gatewayRemovalBlocked')">
                  <p class="m-0">{{ t('applicationsPage.gatewayRemovalBlockedHint') }}</p>
                  <p class="m-0 break-words">{{ t('applicationsPage.gatewayRemovalBlockedRecords', { domains: proxyPlaneBlockedReferences.map((item) => item.recordId).join(', ') || proxyPlaneBlockedError }) }}</p>
                </InfoBar>
              </template>
            </div>
          </DetailSection>

          <div class="surface-card p-4">
            <FacilityDiagnostics v-if="proxyPlane" :state="proxyPlane" />
            <div v-else class="relative grid min-h-32 place-items-center"><LoadingOverlay /></div>
          </div>
        </section>

        <section v-else-if="facilityTab === 'configs'" class="grid h-full min-h-0 content-start gap-4 overflow-y-auto pr-1">
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div class="grid min-w-0 gap-0.5">
              <h2 class="m-0 text-body-strong font-semibold text-foreground">{{ t('applicationsPage.configRecordsTitle') }}</h2>
              <p class="m-0 text-caption text-muted-foreground">{{ t('applicationsPage.configRecordsHint') }}</p>
            </div>
            <Button size="sm" variant="primary" :disabled="Boolean(pending)" @click="startNewFacilityRecord"><Plus />{{ t('applicationsPage.addDomainRecord') }}</Button>
          </div>
          <EmptyState v-if="configRecordsError" variant="page" :title="t('common.loadFailed')" :description="configRecordsError">
            <template #actions>
              <Button size="sm" :loading="loading" @click="loadFacilityDetailData"><RefreshCcw />{{ t('common.retry') }}</Button>
            </template>
          </EmptyState>
          <Table
            v-else
            :columns="configRecordColumns"
            :rows="configRecords"
            row-key="recordId"
            :loading="loading && !configRecords.length"
            :loading-label="t('common.loading')"
          >
            <template #domain="{ row }"><strong class="break-all">{{ row.recordId }}</strong></template>
            <template #source="{ row }"><Badge :tone="row.kind === 'facility_domain' ? 'neutral' : 'info'">{{ configRecordSourceLabel(row) }}</Badge></template>
            <template #version="{ row }"><Badge tone="neutral">{{ row.version }}</Badge></template>
            <template #dns="{ row }"><Badge :tone="facilityDnsTone(facilityDnsStatus(row.recordId))" :title="facilityDnsError(row.recordId) || undefined">{{ t(`applicationsPage.dnsSync.${facilityDnsStatus(row.recordId) || 'unknown'}`) }}</Badge></template>
            <template #updatedAt="{ row }"><span class="text-muted-foreground">{{ formatDateTime(row.updatedAt, t('common.never')) }}</span></template>
            <template #actions="{ row }">
              <div class="flex flex-wrap items-center justify-end gap-1">
                <!-- <wide：设施记录操作全部收进 More；>=wide 保持行内按钮。 -->
                <div v-if="row.kind === 'facility_domain'" class="hidden items-center gap-1 wide:flex">
                  <Button size="sm" variant="secondary" @click="openFacilityRecord(row)"><Wrench />{{ t('common.edit') }}</Button>
                  <Button size="sm" variant="ghost" :loading="configRecordActionKey === `apply:${row.recordId}`" :disabled="Boolean(configRecordActionKey)" @click="applyFacilityRecord(row)"><RefreshCcw />{{ t('common.apply') }}</Button>
                  <Button size="sm" variant="danger" :disabled="Boolean(configRecordActionKey)" @click="askDeleteFacilityRecord(row)"><Trash2 />{{ t('common.delete') }}</Button>
                </div>
                <Button v-else size="sm" variant="secondary" class="hidden wide:inline-flex" @click="openRecordApplication(row)"><Globe2 />{{ t('applicationsPage.configRecordOpenApplication') }}</Button>
                <Dropdown align="right">
                  <template #trigger>
                    <IconButton size="sm" :label="t('common.moreActions')"><MoreHorizontal /></IconButton>
                  </template>
                  <template v-if="row.kind === 'facility_domain'">
                    <DropdownItem @click="openFacilityRecord(row)"><Wrench />{{ t('common.edit') }}</DropdownItem>
                    <DropdownItem :disabled="Boolean(configRecordActionKey)" @click="applyFacilityRecord(row)"><RefreshCcw />{{ t('common.apply') }}</DropdownItem>
                    <DropdownItem tone="danger" :disabled="Boolean(configRecordActionKey)" @click="askDeleteFacilityRecord(row)"><Trash2 />{{ t('common.delete') }}</DropdownItem>
                  </template>
                  <DropdownItem v-else @click="openRecordApplication(row)"><Globe2 />{{ t('applicationsPage.configRecordOpenApplication') }}</DropdownItem>
                </Dropdown>
              </div>
            </template>
            <template #empty>
              <EmptyState variant="inset" :title="t('applicationsPage.configRecordEmpty')" :description="t('applicationsPage.configRecordEmptyHint')" />
            </template>
          </Table>
        </section>

        <section v-else class="grid h-full min-h-0 content-start gap-4 overflow-y-auto pr-1">
          <DetailSection :title="t('applicationsPage.dnsSyncTitle')" :description="t('applicationsPage.dnsTabHint')">
            <div v-for="row in facilityDnsDomains" :key="`dns-${row.domain}`" class="grid gap-2 p-4">
              <div class="flex flex-wrap items-center justify-between gap-3">
                <div class="grid gap-0.5">
                  <strong class="break-all text-body-strong font-semibold text-foreground">{{ row.domain }}</strong>
                  <span class="text-caption text-muted-foreground">{{ t(`applicationsPage.dnsSyncSource.${row.source}`) }}</span>
                </div>
                <div class="flex flex-wrap items-center gap-2">
                  <Badge :tone="facilityDnsTone(facilityDnsStatus(row.domain))" :title="facilityDnsError(row.domain) || undefined">{{ t(`applicationsPage.dnsSync.${facilityDnsStatus(row.domain) || 'unknown'}`) }}</Badge>
                  <Button
                    v-if="facilityDnsNeedsAction(row.domain)"
                    size="sm"
                    variant="secondary"
                    :loading="dnsSyncPendingDomain === row.domain"
                    :disabled="Boolean(dnsSyncPendingDomain) && dnsSyncPendingDomain !== row.domain"
                    @click="retryFacilityDns(row.domain)"
                  >{{ t('applicationsPage.dnsSyncRetry') }}</Button>
                  <Button
                    v-if="facilityDnsStatus(row.domain) === 'failed' || facilityDnsConflict(row.domain)"
                    size="sm"
                    variant="danger"
                    :loading="dnsSyncPendingDomain === row.domain"
                    :disabled="Boolean(dnsSyncPendingDomain) && dnsSyncPendingDomain !== row.domain"
                    @click="askFacilityDnsTakeover(row.domain)"
                  >{{ t('applicationsPage.dnsSyncTakeover') }}</Button>
                </div>
              </div>
              <p v-if="facilityDnsError(row.domain)" class="m-0 text-caption font-semibold text-danger">{{ facilityDnsError(row.domain) }}</p>
            </div>
            <EmptyState v-if="!facilityDnsDomains.length" variant="inset" :title="t('applicationsPage.configRecordEmpty')" :description="t('applicationsPage.configRecordEmptyHint')" />
          </DetailSection>
        </section>
      </Tabs>
    </div>
  </ConsolePage>

  <ConsolePage
    v-else-if="isAppEditor"
    :back-label="t('common.back')"
    @back="leaveApplicationEditor"
    :title="isCreateMode ? t('applicationsPage.createApplication') : t('applicationsPage.applicationEditor')"
    :description="t('applicationsPage.applicationEditorDescription')"
    :breadcrumbs="applicationEditorBreadcrumbs"
    :body-scroll="false"
    width="content"
  >
    <EditorPage>
      <div v-if="!editorReady" class="relative grid min-h-64 place-items-center">
        <LoadingOverlay v-if="editorLoading || !actionError" />
        <div v-else class="grid max-w-md justify-items-center gap-3 text-center">
          <AlertTriangle class="size-6 text-danger" aria-hidden="true" />
          <p class="m-0 text-body text-danger">{{ actionError }}</p>
          <Button size="sm" :loading="pending === 'editor-reload'" @click="reloadApplicationEditor">{{ t('common.retry') }}</Button>
        </div>
      </div>
      <div class="grid gap-3">
        <InfoBar v-if="saving" tone="info">{{ t(`applicationsPage.saveStage.${saveStage}`) }}</InfoBar>
        <InfoBar v-if="editorReady && actionError" tone="danger">
          <span class="min-w-0 break-words">{{ actionError }}</span>
        </InfoBar>
      </div>

      <!-- 同设施记录编辑器：主区单列全宽；检查结果只在保存失败时弹窗展示。 -->
      <div v-if="editorReady" class="relative grid min-w-0 content-start gap-4">
        <LoadingOverlay v-if="saving" :label="t(`applicationsPage.saveStage.${saveStage}`)" />
        <div class="grid min-w-0 content-start gap-4">
          <p class="m-0 text-caption text-muted-foreground">{{ t('applicationsPage.editorFlowHint') }}</p>

          <DetailSection :title="t('applicationsPage.panelIdentity')" :description="isCreateMode ? t('applicationsPage.createFastPathHint') : t('applicationsPage.editRuntimeHint')">
            <div class="grid gap-3 p-4 sm:grid-cols-2">
              <FormField :label="t('common.name')" :invalid="Boolean(appErrors.name)">
                <template #default="{ id, describedBy, invalid }">
                  <Input :id="id" :aria-describedby="describedBy" v-model="appDraft.name" :invalid="invalid" @input="markAppStructuredDirty" />
                </template>
              </FormField>
              <FormField :label="t('applicationsPage.enabled')">
                <template #default="{ id }">
                  <Switch :id="id" v-model="appDraft.enabled" :label="t('applicationsPage.enabled')" @click="markAppStructuredDirty" />
                </template>
              </FormField>
            </div>
          </DetailSection>

          <DetailSection :title="t('applicationsPage.panelRuntimeSource')" :description="t('applicationsPage.sourceHint')">
            <div class="grid gap-3 p-4 sm:grid-cols-2">
              <div class="sm:col-span-2">
                <FormField :label="t('applicationsPage.image')" :invalid="Boolean(appErrors.image)">
                  <template #default="{ id, describedBy, invalid }">
                    <Input :id="id" :aria-describedby="describedBy" v-model="appDraft.image" :invalid="invalid" @input="markAppStructuredDirty" />
                  </template>
                </FormField>
              </div>
              <FormField :label="t('applicationsPage.cpu')" :invalid="Boolean(appErrors.cpu)">
                <template #default="{ id, describedBy, invalid }">
                  <Input :id="id" :aria-describedby="describedBy" v-model="appDraft.cpu" placeholder="0.5" :invalid="invalid" @input="markAppStructuredDirty" />
                </template>
              </FormField>
              <FormField :label="t('applicationsPage.memoryMb')" :invalid="Boolean(appErrors.memoryMb)">
                <template #default="{ id, describedBy, invalid }">
                  <Input :id="id" :aria-describedby="describedBy" v-model="appDraft.memoryMb" placeholder="512" :invalid="invalid" @input="markAppStructuredDirty" />
                </template>
              </FormField>
              <div class="grid content-start gap-1 sm:col-span-2">
                <span class="text-caption text-muted-foreground">{{ t('applicationsPage.command') }}</span>
                <div class="grid gap-2">
                  <div v-for="row in appDraft.commandRows" :key="row.id" class="grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
                    <Input v-model="row.value" :placeholder="t('applicationsPage.commandArgument')" @input="markAppStructuredDirty" />
                    <Button size="sm" variant="danger" class="shrink-0" @click="removeCommandRow(row.id)"><Trash2 /></Button>
                  </div>
                  <Button size="sm" variant="secondary" class="justify-self-start" @click="addCommandRow"><Plus />{{ t('common.add') }}</Button>
                </div>
              </div>
              <FormField :label="t('applicationsPage.privileged')" class="sm:col-span-2">
                <template #default="{ id }">
                  <Switch :id="id" v-model="appDraft.privileged" :label="t('applicationsPage.privileged')" @click="markAppStructuredDirty" />
                </template>
              </FormField>
            </div>
          </DetailSection>

          <DetailSection :title="t('applicationsPage.panelNetworking')" :description="t('applicationsPage.networkingHint')">
            <template #actions>
              <Button size="sm" variant="secondary" @click="openPortDialog()"><Plus />{{ t('applicationsPage.addPort') }}</Button>
              <Button size="sm" variant="secondary" @click="openProxyDialog()"><Globe2 />{{ t('applicationsPage.addProxyRule') }}</Button>
            </template>
            <div v-for="(port, index) in appDraft.ports" :key="port.id" class="grid gap-3 p-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
              <div class="min-w-0">
                <strong class="block break-all text-body-strong font-semibold text-foreground">{{ port.label || t('applicationsPage.unnamedPort') }}</strong>
                <span class="mt-0.5 block text-caption text-muted-foreground">{{ t('applicationsPage.containerPortSummary', { port: port.to }) }} · {{ port.staticPort ? t('applicationsPage.staticPort', { port: port.staticPort }) : t('applicationsPage.dynamicPort') }} · {{ t('applicationsPage.protocolSummary', { protocol: port.protocol.toUpperCase() }) }} · {{ port.openFirewall ? t('applicationsPage.firewallManaged') : t('applicationsPage.firewallUnchanged') }}</span>
              </div>
              <div class="flex flex-wrap justify-start gap-2 sm:justify-end">
                <Button size="sm" @click="openPortDialog(index)">{{ t('common.edit') }}</Button>
                <Button size="sm" variant="danger" @click="removeAt(appDraft.ports, index)">{{ t('common.delete') }}</Button>
              </div>
            </div>
            <div v-for="(rule, index) in appDraft.reverseProxy" :key="index" class="grid gap-3 p-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
              <div class="min-w-0">
                <strong class="block break-all text-body-strong font-semibold text-foreground">{{ rule.domain || t('applicationsPage.unnamedDomain') }}</strong>
                <span class="mt-0.5 block text-caption text-muted-foreground">{{ t('applicationsPage.routeTargetSummary', { port: rule.targetPort, paths: rule.paths.map((path) => path.path).join(', ') }) }}</span>
              </div>
              <div class="flex flex-wrap justify-start gap-2 sm:justify-end">
                <Button size="sm" @click="openProxyDialog(index)">{{ t('common.edit') }}</Button>
                <Button size="sm" variant="danger" @click="removeAt(appDraft.reverseProxy, index)">{{ t('common.delete') }}</Button>
              </div>
            </div>
            <EmptyState v-if="!appDraft.ports.length && !appDraft.reverseProxy.length" variant="inset" :title="t('applicationsPage.noRoutes')" :description="t('applicationsPage.networkingEmptyHint')" />
          </DetailSection>

          <DetailSection :title="t('applicationsPage.containerEnv')" :description="t('applicationsPage.environmentHint')">
            <template #actions>
              <Button size="sm" variant="secondary" @click="openRowDialog()"><Plus />{{ t('common.add') }}</Button>
            </template>
            <div v-for="(row, index) in appDraft.env" :key="row.id" class="grid gap-3 p-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
              <div class="min-w-0">
                <strong class="block break-all text-body-strong font-semibold text-foreground">{{ row.key }}</strong>
                <span class="mt-0.5 block break-all text-caption text-muted-foreground">{{ row.value || t('common.empty') }}</span>
              </div>
              <div class="flex flex-wrap justify-start gap-2 sm:justify-end">
                <Button size="sm" @click="openRowDialog(index)">{{ t('common.edit') }}</Button>
                <Button size="sm" variant="danger" @click="removeRow(index)">{{ t('common.delete') }}</Button>
              </div>
            </div>
            <EmptyState v-if="!appDraft.env.length" variant="inset" :title="t('applicationsPage.noEnvironment')" :description="t('applicationsPage.noEnvironmentHint')" />
          </DetailSection>

          <DetailSection :title="t('applicationsPage.panelStorage')" :description="t('applicationsPage.storageHint')">
            <template #actions>
              <Button size="sm" variant="secondary" @click="openMountDialog()"><Plus />{{ t('applicationsPage.addMount') }}</Button>
            </template>
            <div v-for="(mount, index) in appDraft.mounts" :key="mount.id" class="grid gap-3 p-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
              <div class="min-w-0">
                <strong class="block break-all text-body-strong font-semibold text-foreground">{{ t('applicationsPage.mountSummary', { type: mount.type, target: mount.target }) }}</strong>
                <span class="mt-0.5 block break-all text-caption text-muted-foreground">{{ mountSourceLabel(mount) }}</span>
              </div>
              <div class="flex flex-wrap justify-start gap-2 sm:justify-end">
                <Button size="sm" @click="openMountDialog(index)">{{ t('common.edit') }}</Button>
                <Button size="sm" variant="danger" @click="removeAt(appDraft.mounts, index)">{{ t('common.delete') }}</Button>
              </div>
            </div>
            <EmptyState v-if="!appDraft.mounts.length" variant="inset" :title="t('applicationsPage.noMounts')" :description="t('applicationsPage.noMountsHint')" />
          </DetailSection>

          <DetailSection :title="t('applicationsPage.panelDeployment')" :description="t('applicationsPage.deployHint')">
            <div class="grid gap-3 p-4">
              <FormField :label="t('applicationsPage.deploymentMode')">
                <template #default="{ id }">
                  <Select :id="id" v-model="appDraft.deploymentMode" :options="[{ label: t('applicationsPage.allServers'), value: 'all' }, { label: t('applicationsPage.selectedServers'), value: 'selected' }]" @change="markAppStructuredDirty" />
                </template>
              </FormField>
              <div v-if="appDraft.deploymentMode === 'selected'">
                <ServerMultiPicker v-model="appDeploymentServersModel" :servers="serverOptions" :disabled-ids="deploymentDisabledIds" :disabled-reasons="deploymentDisabledReasons" :label="t('applicationsPage.deploymentServers')" />
              </div>
            </div>
          </DetailSection>
        </div>

        </div>

      <!-- 资产/代码编辑器全宽：不放进 xl 双列的窄主列，避免 YAML/模板被压窄。 -->
      <div class="mt-4 surface-card p-4">
        <AssetFileManager
          :items="applicationAssetItems" :adapter="applicationAssetAdapter" :disabled="!editorReady || saving" :show-filename="false"
          :language-options="fileLanguageOptions" :infer-language="inferTemplateLanguage"
          :labels="{ title: t('applicationsPage.panelFilesAssets'), hint: t('applicationsPage.filesHint'), uploadAsset: t('applicationsPage.uploadAsset'), uploadAssetTitle: t('applicationsPage.uploadAssetTitle'), uploadType: t('applicationsPage.uploadType'), uploadTypeText: t('applicationsPage.uploadTypeText'), uploadTypeBinary: t('applicationsPage.uploadTypeBinary'), uploadTypeArchive: t('applicationsPage.uploadTypeArchive'), uploadFile: t('applicationsPage.uploadFile'), uploadArchive: t('applicationsPage.uploadFolderArchive'), operationFailed: t('applicationsPage.operationFailed'), edit: t('common.edit'), replace: t('common.replace'), download: t('common.download'), delete: t('common.delete'), bytes: t('applicationsPage.bytes'), noAssets: t('applicationsPage.noFiles'), noAssetsHint: t('applicationsPage.noFilesHint'), textTitle: t('applicationsPage.editTextFile'), newTextTitle: t('applicationsPage.newTextFile'), name: t('applicationsPage.fileName'), filename: t('applicationsPage.assetDownloadFilename'), language: t('applicationsPage.highlightLanguage'), content: t('applicationsPage.fileContent'), loading: t('applicationsPage.fileLoading'), loadFailed: t('applicationsPage.fileLoadFailed'), cancel: t('common.cancel'), save: t('common.save'), close: t('common.close'), reload: t('applicationsPage.discardTextAndReload'), deleteTitle: t('applicationsPage.confirm.file-delete.title'), deleteDescription: t('applicationsPage.confirm.file-delete.description'), confirmDelete: t('common.delete') }"
        />
      </div>

      <template #footer>
        <div class="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
          <div class="text-caption text-muted-foreground">{{ editorDirty ? t('applicationsPage.unsavedChanges') : t('applicationsPage.readyToCommit') }}</div>
          <div class="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap">
            <Button variant="secondary" class="w-full sm:w-auto" :disabled="saving" @click="leaveApplicationEditor">{{ t('common.cancel') }}</Button>
            <Button variant="primary" class="w-full sm:w-auto" :loading="pending === 'commit'" :disabled="!editorReady || saving" @click="commitApplication()"><Save />{{ t('applicationsPage.commit') }}</Button>
          </div>
        </div>
      </template>
    </EditorPage>
  </ConsolePage>

  <!-- 保存失败（preview 返回 blocking diagnostics）时的检查结果；关闭后停留编辑器。 -->
  <Dialog
    :open="editorDiagnosticsOpen"
    :title="t('applicationsPage.diagnostics')"
    :description="t('applicationsPage.validationFoundDialog')"
    :close-label="t('common.close')"
    @update:open="(open: boolean) => { if (!open) editorDiagnosticsOpen = false }"
  >
    <ApplicationDiagnosticsList :items="editorDiagnostics" />
    <template #footer>
      <Button variant="primary" class="w-full sm:w-auto" @click="editorDiagnosticsOpen = false">{{ t('common.close') }}</Button>
    </template>
  </Dialog>

  <ApplicationLogsDialog v-model:open="logsOpen" :application-id="logsApplication?.id ?? ''" :application-name="logsApplication?.name ?? ''" />

  <Dialog v-model:open="dialogOpen" :title="t(`applicationsPage.dialog.${dialogKind}`)" :close-label="t('common.close')">
    <div v-if="dialogKind === 'env'" class="grid gap-3">
      <FormField :label="t('common.name')">
        <template #default="{ id }"><Input :id="id" v-model="rowDraft.key" /></template>
      </FormField>
      <div class="grid gap-1">
        <span class="text-caption text-muted-foreground">{{ t('common.value') }}</span>
        <TemplateVariableInput
          v-model="rowDraft.value"
          :options="applicationTemplateVariableOptions"
          :insert-label="t('applicationsPage.insertVariable')"
          :empty-label="t('applicationsPage.noInsertableVariables')"
        />
        <span class="text-caption text-secondary-foreground">{{ t('applicationsPage.variableReferenceHint') }}</span>
      </div>
    </div>
    <div v-else-if="dialogKind === 'port'" class="grid gap-3">
      <FormField :label="t('common.name')">
        <template #default="{ id }"><Input :id="id" v-model="portDraft.label" /></template>
      </FormField>
      <FormField :label="t('applicationsPage.containerPort')">
        <template #default="{ id }"><Input :id="id" v-model="portDraft.to" /></template>
      </FormField>
      <FormField :label="t('applicationsPage.hostPort')">
        <template #default="{ id }"><Input :id="id" v-model="portDraft.staticPort" @input="onPortStaticPortInput" /></template>
      </FormField>
      <FormField :label="t('applicationsPage.protocol')">
        <template #default="{ id }"><Select :id="id" v-model="portDraft.protocol" :options="portProtocolOptions" /></template>
      </FormField>
      <FormField :label="t('applicationsPage.openFirewall')" :hint="t('applicationsPage.openFirewallHint')">
        <template #default="{ id }"><Switch :id="id" v-model="portDraft.openFirewall" :label="t('applicationsPage.openFirewall')" :disabled="!portDraft.staticPort.trim()" /></template>
      </FormField>
    </div>
    <div v-else-if="dialogKind === 'mount'" class="grid gap-3">
      <FormField :label="t('common.type')">
        <template #default="{ id }"><Select :id="id" v-model="mountDraft.type" :options="mountTypeOptions" /></template>
      </FormField>
      <FormField :label="t('applicationsPage.source')">
        <template #default="{ id }">
          <Select v-if="mountDraft.type === 'file'" :id="id" v-model="mountDraft.source" :options="applicationFileMountOptions" :placeholder="t('applicationsPage.applicationFileMountSourcePlaceholder')" />
          <Select v-else-if="mountDraft.type === 'panel_file'" :id="id" v-model="mountDraft.source" :options="panelFileMountOptions" :placeholder="t('applicationsPage.panelFileMountSourcePlaceholder')" />
          <Select v-else-if="mountDraft.type === 'storage_share'" :id="id" v-model="mountDraft.source" :options="storageShareOptions" :placeholder="t('applicationsPage.storageShareMountSourcePlaceholder')" :disabled="!storageShareAvailable" />
          <Input v-else :id="id" v-model="mountDraft.source" />
        </template>
      </FormField>
      <p v-if="mountDraft.type === 'file' && !applicationAssetItems.length" class="m-0 text-caption text-muted-foreground">{{ t('applicationsPage.applicationFileMountEmpty') }}</p>
      <p v-else-if="mountDraft.type === 'file' && mountDraft.source && !applicationFileMountSourceValid" class="m-0 text-caption font-semibold text-danger">{{ t('applicationsPage.applicationFileMountMissing', { name: mountDraft.source }) }}</p>
      <p v-if="mountDraft.type === 'panel_file' && !panelFiles.length" class="m-0 text-caption text-muted-foreground">{{ t('applicationsPage.panelFileMountEmpty') }}</p>
      <p v-else-if="mountDraft.type === 'panel_file' && mountDraft.source && !panelFileMountSourceValid" class="m-0 text-caption font-semibold text-danger">{{ t('applicationsPage.panelFileMountMissing', { source: mountDraft.source }) }}</p>
      <p v-if="mountDraft.type === 'storage_share'" class="m-0 text-caption text-muted-foreground">{{ t('applicationsPage.storageShareMountHint') }}</p>
      <p v-if="mountDraft.type === 'storage_share' && !storageShareAvailable" class="m-0 text-caption text-warning">{{ t('applicationsPage.storageShareMountUnconfigured') }} <Button size="sm" variant="ghost" @click="router.push('/applications/facility-apps/storage-share/config')">{{ t('applicationsPage.storageShareGoConfigure') }}</Button></p>
      <FormField :label="t('applicationsPage.target')">
        <template #default="{ id }"><Input :id="id" v-model="mountDraft.target" /></template>
      </FormField>
      <FormField v-if="mountDraft.type !== 'storage_share'" :label="t('applicationsPage.mode')">
        <template #default="{ id }"><Input :id="id" v-model="mountDraft.mode" placeholder="0755" /></template>
      </FormField>
      <FormField :label="t('applicationsPage.readOnly')">
        <template #default="{ id }"><Switch :id="id" v-model="mountDraft.readOnly" :label="t('applicationsPage.readOnly')" /></template>
      </FormField>
    </div>
    <div v-else-if="dialogKind === 'proxy'" class="grid gap-3">
      <FormField :label="t('applicationsPage.domain')">
        <template #default="{ id }"><Input :id="id" v-model="proxyDraft.domain" /></template>
      </FormField>
      <FormField :label="t('applicationsPage.targetPort')">
        <template #default="{ id }"><Input :id="id" v-model="proxyDraft.targetPort" /></template>
      </FormField>
      <div class="grid gap-3 rounded-control border border-divider bg-control p-3">
        <div class="grid gap-0.5">
          <h3 class="m-0 text-body-strong font-semibold text-foreground">{{ t('applicationsPage.anyAccess') }}</h3>
          <p class="m-0 text-caption text-muted-foreground">{{ t('applicationsPage.anyAccessHint') }}</p>
        </div>
        <FormField :label="t('applicationsPage.anyAccess')">
          <template #default="{ id }"><Switch :id="id" v-model="proxyAnyAccessModel" :label="t('applicationsPage.anyAccess')" /></template>
        </FormField>
        <div v-if="proxyDraft.anyAccess.enabled" class="grid gap-3">
          <FormField :label="t('applicationsPage.anyAccessRelayScope')">
            <template #default="{ id }"><Select :id="id" v-model="proxyRelayScope" :options="anyAccessRelayScopeOptions" /></template>
          </FormField>
          <ServerMultiPicker v-if="proxyRelayScope === 'selected'" v-model="proxyRelayServerIdsModel" :servers="proxyRelayServerOptions" :label="t('applicationsPage.anyAccessRelayServers', { count: proxyDraft.anyAccess.relayServerIds?.length ?? 0 })" />
          <p v-if="proxyRelayScope === 'selected' && !proxyRelayServerOptions.length" class="m-0 text-caption text-muted-foreground">{{ t('applicationsPage.anyAccessNoRelayServers') }}</p>
          <FormField :label="t('applicationsPage.loadBalancingStrategy')">
            <template #default="{ id }"><Select :id="id" v-model="proxyDraft.anyAccess.strategy" :options="anyAccessStrategyOptions" /></template>
          </FormField>
          <div v-if="proxyDraft.anyAccess.strategy === 'primary_backup'" class="grid gap-3 rounded-control border border-divider bg-control p-3">
            <div class="grid gap-0.5">
              <h3 class="m-0 text-body-strong font-semibold text-foreground">{{ t('applicationsPage.originPriority') }}</h3>
              <p class="m-0 text-caption text-muted-foreground">{{ t('applicationsPage.originPriorityHint') }}</p>
            </div>
            <div v-for="(serverId, index) in proxyOriginPriorityModel" :key="serverId" class="grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
              <div class="min-w-0">
                <strong class="block break-all text-body-strong font-semibold text-foreground">{{ index + 1 }}. {{ serverDisplayName(serverId) }}</strong>
                <span v-if="index === 0" class="text-caption text-muted-foreground">{{ t('applicationsPage.primaryOriginServer') }}</span>
              </div>
              <div class="flex flex-wrap gap-2 sm:justify-end">
                <Button size="sm" :disabled="index === 0" @click="moveProxyOriginPriority(index, -1)">{{ t('applicationsPage.moveUp') }}</Button>
                <Button size="sm" :disabled="index === proxyOriginPriorityModel.length - 1" @click="moveProxyOriginPriority(index, 1)">{{ t('applicationsPage.moveDown') }}</Button>
              </div>
            </div>
            <InfoBar v-if="!proxyOriginPriorityModel.length" tone="danger">{{ t('applicationsPage.proxyOriginEmptyHint') }}</InfoBar>
          </div>
        </div>
      </div>
      <div class="flex flex-wrap items-center justify-between gap-2">
        <strong class="text-body-strong font-semibold text-foreground">{{ t('common.path') }}</strong>
        <Button size="sm" variant="secondary" @click="openProxyPathDialog()"><Plus />{{ t('common.addPath') }}</Button>
      </div>
      <div v-for="(path, index) in proxyDraft.paths" :key="`${path.path}-${index}`" class="grid gap-2 rounded-control border border-divider p-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
        <div class="min-w-0">
          <strong class="block break-all text-body-strong font-semibold text-foreground">{{ path.path }}</strong>
          <span class="block text-caption text-muted-foreground">{{ t('applicationsPage.webSocketMode') }}: {{ t(`applicationsPage.httpMode.${path.options?.webSocketMode || 'off'}`) }}</span>
        </div>
        <div class="flex flex-wrap gap-2 sm:justify-end">
          <Button size="sm" @click="openProxyPathDialog(index)">{{ t('common.edit') }}</Button>
          <Button size="sm" variant="danger" @click="proxyDraft.paths.splice(index, 1)">{{ t('common.delete') }}</Button>
        </div>
      </div>
    </div>
    <div v-else-if="dialogKind === 'proxyPath'" class="grid gap-3">
      <FormField :label="t('common.path')">
        <template #default="{ id }"><Input :id="id" v-model="proxyPathDraft.path" /></template>
      </FormField>
      <div class="grid gap-3 rounded-control border border-divider bg-control p-3">
        <div class="grid gap-0.5">
          <h3 class="m-0 text-body-strong font-semibold text-foreground">{{ t('applicationsPage.advancedOptions') }}</h3>
          <p class="m-0 text-caption text-muted-foreground">{{ t('applicationsPage.advancedOptionsHint') }}</p>
        </div>
        <FormField :label="t('applicationsPage.gzipMode')">
          <template #default="{ id }"><Select :id="id" v-model="proxyGzipMode" :options="httpModeOptions" /></template>
        </FormField>
        <div class="grid gap-3 sm:grid-cols-2">
          <FormField :label="t('applicationsPage.clientMaxBodySizeMb')">
            <template #default="{ id, describedBy, invalid }"><NumberBox :id="id" v-model="proxyClientMaxBodySizeMb" :aria-describedby="describedBy" :invalid="invalid" :min="0" :step="1" /></template>
          </FormField>
          <FormField :label="t('applicationsPage.connectTimeoutSeconds')">
            <template #default="{ id, describedBy, invalid }"><NumberBox :id="id" v-model="proxyConnectTimeoutSeconds" :aria-describedby="describedBy" :invalid="invalid" :min="0" :step="1" /></template>
          </FormField>
          <FormField :label="t('applicationsPage.readTimeoutSeconds')">
            <template #default="{ id, describedBy, invalid }"><NumberBox :id="id" v-model="proxyReadTimeoutSeconds" :aria-describedby="describedBy" :invalid="invalid" :min="0" :step="1" /></template>
          </FormField>
          <FormField :label="t('applicationsPage.sendTimeoutSeconds')">
            <template #default="{ id, describedBy, invalid }"><NumberBox :id="id" v-model="proxySendTimeoutSeconds" :aria-describedby="describedBy" :invalid="invalid" :min="0" :step="1" /></template>
          </FormField>
          <FormField :label="t('applicationsPage.bufferingMode')">
            <template #default="{ id }"><Select :id="id" v-model="proxyBufferingMode" :options="httpModeOptions" /></template>
          </FormField>
          <FormField :label="t('applicationsPage.webSocketMode')">
            <template #default="{ id }"><Select :id="id" v-model="proxyWebSocketMode" :options="webSocketModeOptions" /></template>
          </FormField>
        </div>
        <div class="grid gap-1">
          <span class="text-caption font-semibold text-foreground">{{ t('applicationsPage.requestHeaders') }}</span>
          <div v-for="(row, index) in proxyRequestHeaders" :key="row.id" class="grid gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-center">
            <Input v-model="row.key" :placeholder="t('applicationsPage.headerName')" />
            <Input v-model="row.value" :placeholder="t('applicationsPage.headerValue')" />
            <Button size="sm" variant="danger" class="shrink-0" @click="proxyRequestHeaders.splice(index, 1)"><Trash2 /></Button>
          </div>
          <Button size="sm" variant="secondary" class="justify-self-start" @click="proxyRequestHeaders.push(makeKeyValueRow())"><Plus />{{ t('common.add') }}</Button>
        </div>
        <div class="grid gap-1">
          <span class="text-caption font-semibold text-foreground">{{ t('applicationsPage.responseHeaders') }}</span>
          <div v-for="(row, index) in proxyResponseHeaders" :key="row.id" class="grid gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-center">
            <Input v-model="row.key" :placeholder="t('applicationsPage.headerName')" />
            <Input v-model="row.value" :placeholder="t('applicationsPage.headerValue')" />
            <Button size="sm" variant="danger" class="shrink-0" @click="proxyResponseHeaders.splice(index, 1)"><Trash2 /></Button>
          </div>
          <Button size="sm" variant="secondary" class="justify-self-start" @click="proxyResponseHeaders.push(makeKeyValueRow())"><Plus />{{ t('common.add') }}</Button>
        </div>
      </div>
    </div>
    <div v-else-if="dialogKind === 'facilityPath'" class="grid gap-3">
      <FormField :label="t('common.path')" :error="facilityPathErrors.path ? t(facilityPathErrors.path) : ''" :invalid="Boolean(facilityPathErrors.path)">
        <template #default="{ id, describedBy, invalid }">
          <Input :id="id" :aria-describedby="describedBy" v-model="facilityPathDraft.path" :invalid="invalid" @input="clearFacilityPathError('path')" />
        </template>
      </FormField>
      <FormField :label="t('common.type')">
        <template #default="{ id }"><Select :id="id" v-model="facilityPathDraft.ruleType" :options="routeTypeOptions" /></template>
      </FormField>
      <template v-if="facilityPathDraft.ruleType === 'static'">
        <FormField :label="t('applicationsPage.sourceType')">
          <template #default="{ id }"><Select :id="id" v-model="facilityPathDraft.sourceType" :options="sourceTypeOptions" /></template>
        </FormField>
        <FormField :label="t('applicationsPage.staticAsset')" :error="facilityPathErrors.asset ? t(facilityPathErrors.asset) : ''" :invalid="Boolean(facilityPathErrors.asset)">
          <template #default="{ id, describedBy, invalid }">
            <Select :id="id" :aria-describedby="describedBy" v-model="facilityPathDraft.assetName" :options="assetOptions" @change="clearFacilityPathError('asset')" />
          </template>
        </FormField>
      </template>
      <template v-else-if="facilityPathDraft.ruleType === 'redirect'">
        <FormField :label="t('applicationsPage.redirectUrl')" :error="facilityPathErrors.redirectUrl ? t(facilityPathErrors.redirectUrl) : ''" :invalid="Boolean(facilityPathErrors.redirectUrl)">
          <template #default="{ id, describedBy, invalid }">
            <Input :id="id" :aria-describedby="describedBy" v-model="facilityPathDraft.redirectUrl" :invalid="invalid" @input="clearFacilityPathError('redirectUrl')" />
          </template>
        </FormField>
        <FormField :label="t('applicationsPage.redirectCode')">
          <template #default="{ id }"><Select :id="id" v-model="facilityRedirectCode" :options="['301', '302', '307', '308'].map((value) => ({ label: value, value }))" /></template>
        </FormField>
      </template>
      <template v-else>
        <FormField :label="t('applicationsPage.proxyUrl')" :error="facilityPathErrors.proxyUrl ? t(facilityPathErrors.proxyUrl) : ''" :invalid="Boolean(facilityPathErrors.proxyUrl)">
          <template #default="{ id, describedBy, invalid }">
            <Input :id="id" :aria-describedby="describedBy" v-model="facilityPathDraft.proxyUrl" :invalid="invalid" @input="clearFacilityPathError('proxyUrl')" />
          </template>
        </FormField>
        <FormField :label="t('applicationsPage.proxySourceMode')">
          <template #default="{ id }"><Select :id="id" v-model="facilityPathDraft.proxySourceMode" :options="proxySourceModeOptions" /></template>
        </FormField>
      </template>

      <div class="grid gap-3 rounded-control border border-divider bg-control p-3">
        <div class="grid gap-0.5">
          <h3 class="m-0 text-body-strong font-semibold text-foreground">{{ t('applicationsPage.advancedOptions') }}</h3>
          <p class="m-0 text-caption text-muted-foreground">{{ t('applicationsPage.advancedOptionsHint') }}</p>
        </div>
        <FormField v-if="facilityPathDraft.ruleType !== 'redirect'" :label="t('applicationsPage.gzipMode')">
          <template #default="{ id }"><Select :id="id" v-model="facilityGzipMode" :options="httpModeOptions" /></template>
        </FormField>
        <template v-if="facilityPathDraft.ruleType === 'proxy_pass'">
          <div class="grid gap-3 sm:grid-cols-2">
            <FormField :label="t('applicationsPage.clientMaxBodySizeMb')">
              <template #default="{ id, describedBy, invalid }"><NumberBox :id="id" v-model="facilityClientMaxBodySizeMb" :aria-describedby="describedBy" :invalid="invalid" :min="0" :step="1" /></template>
            </FormField>
            <FormField :label="t('applicationsPage.connectTimeoutSeconds')">
              <template #default="{ id, describedBy, invalid }"><NumberBox :id="id" v-model="facilityConnectTimeoutSeconds" :aria-describedby="describedBy" :invalid="invalid" :min="0" :step="1" /></template>
            </FormField>
            <FormField :label="t('applicationsPage.readTimeoutSeconds')">
              <template #default="{ id, describedBy, invalid }"><NumberBox :id="id" v-model="facilityReadTimeoutSeconds" :aria-describedby="describedBy" :invalid="invalid" :min="0" :step="1" /></template>
            </FormField>
            <FormField :label="t('applicationsPage.sendTimeoutSeconds')">
              <template #default="{ id, describedBy, invalid }"><NumberBox :id="id" v-model="facilitySendTimeoutSeconds" :aria-describedby="describedBy" :invalid="invalid" :min="0" :step="1" /></template>
            </FormField>
            <FormField :label="t('applicationsPage.bufferingMode')">
              <template #default="{ id }"><Select :id="id" v-model="facilityBufferingMode" :options="httpModeOptions" /></template>
            </FormField>
            <FormField :label="t('applicationsPage.webSocketMode')">
              <template #default="{ id }"><Select :id="id" v-model="facilityWebSocketMode" :options="webSocketModeOptions" /></template>
            </FormField>
          </div>
          <div class="grid gap-1">
            <span class="text-caption font-semibold text-foreground">{{ t('applicationsPage.requestHeaders') }}</span>
            <div v-for="(row, index) in facilityRequestHeaders" :key="row.id" class="grid gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-center">
              <Input v-model="row.key" :placeholder="t('applicationsPage.headerName')" />
              <Input v-model="row.value" :placeholder="t('applicationsPage.headerValue')" />
              <Button size="sm" variant="danger" class="shrink-0" @click="facilityRequestHeaders.splice(index, 1)"><Trash2 /></Button>
            </div>
            <Button size="sm" variant="secondary" class="justify-self-start" @click="facilityRequestHeaders.push(makeKeyValueRow())"><Plus />{{ t('common.add') }}</Button>
          </div>
        </template>
        <div class="grid gap-1">
          <span class="text-caption font-semibold text-foreground">{{ t('applicationsPage.responseHeaders') }}</span>
          <div v-for="(row, index) in facilityResponseHeaders" :key="row.id" class="grid gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-center">
            <Input v-model="row.key" :placeholder="t('applicationsPage.headerName')" />
            <Input v-model="row.value" :placeholder="t('applicationsPage.headerValue')" />
            <Button size="sm" variant="danger" class="shrink-0" @click="facilityResponseHeaders.splice(index, 1)"><Trash2 /></Button>
          </div>
          <Button size="sm" variant="secondary" class="justify-self-start" @click="facilityResponseHeaders.push(makeKeyValueRow())"><Plus />{{ t('common.add') }}</Button>
        </div>
      </div>
    </div>
    <template #footer>
      <div class="grid w-full grid-cols-1 gap-2 sm:flex sm:flex-wrap sm:justify-end">
        <Button variant="secondary" class="w-full sm:w-auto" @click="cancelDialog">{{ t('common.cancel') }}</Button>
        <Button v-if="dialogKind === 'env'" variant="primary" class="w-full sm:w-auto" @click="saveRowDialog">{{ t('common.save') }}</Button>
        <Button v-else-if="dialogKind === 'port'" variant="primary" class="w-full sm:w-auto" @click="savePortDialog">{{ t('common.save') }}</Button>
        <Button v-else-if="dialogKind === 'mount'" variant="primary" class="w-full sm:w-auto" @click="saveMountDialog">{{ t('common.save') }}</Button>
        <Button v-else-if="dialogKind === 'proxy'" variant="primary" class="w-full sm:w-auto" @click="saveProxyDialog">{{ t('common.save') }}</Button>
        <Button v-else-if="dialogKind === 'proxyPath'" variant="primary" class="w-full sm:w-auto" @click="saveProxyPathDialog">{{ t('common.save') }}</Button>
        <Button v-else-if="dialogKind === 'facilityPath'" variant="primary" class="w-full sm:w-auto" @click="saveFacilityPathDialog">{{ t('common.save') }}</Button>
      </div>
    </template>
  </Dialog>

  <ConfirmDialog
    v-if="confirmKind === 'delete' && confirmHasPersistentData"
    :open="confirmOpen"
    :title="t('applicationsPage.confirm.deletePersistent.title')"
    :impact="t('applicationsPage.confirm.deletePersistent.impact')"
    tone="danger"
    :confirm-label="t('common.confirm')"
    :cancel-label="t('common.cancel')"
    :require-checkbox="true"
    :checkbox-label="t('applicationsPage.confirm.deletePersistent.checkbox')"
    @confirm="confirmAction"
    @update:open="(open: boolean) => { confirmOpen = open }"
  />

  <ConfirmDialog
    v-else
    :open="confirmOpen"
    :title="t(`applicationsPage.confirm.${confirmKind}.title`)"
    :description="t(`applicationsPage.confirm.${confirmKind}.description`)"
    :impact="t('applicationsPage.confirmImpact')"
    tone="danger"
    :loading="Boolean(pending)"
    :confirm-label="t('common.confirm')"
    :cancel-label="t('common.cancel')"
    :checkbox-label="t('serversPage.confirmCheckbox')"
    @confirm="confirmAction"
    @update:open="(open: boolean) => { confirmOpen = open }"
  />

  <ConfirmDialog
    :open="restoreConfirmOpen"
    :title="t('applicationsPage.restorePersistentData')"
    :impact="t('applicationsPage.restorePersistentDataImpact')"
    tone="danger"
    :confirm-label="t('common.confirm')"
    :cancel-label="t('common.cancel')"
    :require-checkbox="true"
    :checkbox-label="t('applicationsPage.restorePersistentDataCheckbox')"
    @confirm="confirmRestorePersistentData"
    @update:open="(open: boolean) => { if (!open) restoreConfirmOpen = false }"
  />

  <ConfirmDialog
    :open="dnsTakeoverOpen"
    :title="t('applicationsPage.confirm.dnsTakeover.title')"
    :description="t('applicationsPage.confirm.dnsTakeover.description', { domain: dnsTakeoverDomain })"
    :impact="t('applicationsPage.confirm.dnsTakeover.impact', { domain: dnsTakeoverDomain })"
    tone="danger"
    :loading="Boolean(dnsSyncPendingDomain)"
    :confirm-label="t('applicationsPage.dnsSyncTakeover')"
    :cancel-label="t('common.cancel')"
    :require-checkbox="true"
    :checkbox-label="t('applicationsPage.confirm.dnsTakeover.checkbox')"
    @confirm="confirmFacilityDnsTakeover"
    @update:open="(open: boolean) => { if (!open) { dnsTakeoverOpen = false; dnsTakeoverDomain = ''; } }"
  />

  <ConfirmDialog
    :open="deleteRecordOpen"
    :title="t('applicationsPage.configRecordDeleteTitle')"
    :description="deleteRecordTarget ? t('applicationsPage.configRecordDeleteDescription', { domain: deleteRecordTarget.recordId, version: deleteRecordTarget.version }) : ''"
    :impact="t('applicationsPage.configRecordDeleteImpact')"
    tone="danger"
    :loading="Boolean(configRecordActionKey)"
    :confirm-label="t('common.delete')"
    :cancel-label="t('common.cancel')"
    checkbox-label=""
    @confirm="confirmDeleteFacilityRecord"
    @update:open="(open: boolean) => { deleteRecordOpen = open; if (!open) deleteRecordTarget = null; }"
  />

  <ConfirmDialog
    :open="discardDialogOpen"
    :title="t('applicationsPage.unsavedChanges')"
    :impact="t('applicationsPage.leaveDirty')"
    :confirm-label="t('common.discard')"
    :cancel-label="t('common.cancel')"
    checkbox-label=""
    @confirm="confirmDiscard"
    @update:open="handleDiscardOpenChange"
  />
</template>