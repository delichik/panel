<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router';
import { AlertTriangle, ClipboardList, Globe2, HardDrive, History, Plus, RefreshCcw, Rocket, Save, Square, Trash2, UploadCloud, Wrench } from '@lucide/vue';
import { applicationsApi } from '@/api/applications';
import { serversApi } from '@/api/servers';
import { saveBlobDownload } from '@/api/download';
import { reverseProxyFacilityApi, storageShareFacilityApi } from '@/api/facilityApps';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue';
import Dialog from '@/components/ui/Dialog.vue';
import DownloadButton from '@/components/ui/DownloadButton.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import FileUploadButton from '@/components/ui/FileUploadButton.vue';
import Input from '@/components/ui/Input.vue';
import SearchInput from '@/components/ui/SearchInput.vue';
import PaginationBar from '@/components/ui/PaginationBar.vue';
import Select from '@/components/ui/Select.vue';
import StatusBadge from '@/components/ui/StatusBadge.vue';
import Switch from '@/components/ui/Switch.vue';
import Tabs from '@/components/ui/Tabs.vue';
import LoadingOverlay from '@/components/ui/LoadingOverlay.vue';
import { useErrorToast, useSuccessToast, useToast } from '@/components/ui/toast';
import ServerMultiPicker from '@/components/patterns/ServerMultiPicker.vue';
import ServerContextSelector from '@/components/patterns/ServerContextSelector.vue';
import StorageShareFacility from './StorageShareFacility.vue';
import AssetFileManager from '@/components/patterns/AssetFileManager.vue';
import type { AssetFileAdapter, AssetFileItem } from '@/components/patterns/assetFileManager';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import EditorPage from '@/components/templates/EditorPage.vue';
import MasterDetailLayout from '@/components/templates/MasterDetailLayout.vue';
import { useI18n } from '@/i18n';
import type { ApplicationDto, ApplicationEditPreviewResult, ApplicationEditSession, ApplicationFile, ApplicationRuntime, ApplicationSummaryDto, Diagnostic, ReverseProxyRule } from '@/types/applications';
import type { FacilityEditPreviewResult, FacilityEditSession, FacilityRouteDomain, FacilityRoutePath, ReverseProxyConfig, StaticAsset, StorageShareConfig } from '@/types/facilityApps';
import type { ServerDto } from '@/types/servers';
import { formatDateTime } from '@/utils/datetime';
import {
  applicationStatus,
  cloneFacilityDomains,
  cloneFacilityPath,
  cloneProxyRules,
  defaultRouteOptions,
  diffApplications,
  diffFacility,
  draftFromApplication,
  facilityDraftFromConfig,
  facilitySaveInputFromDraft,
  hasBlockingDiagnostic,
  makeFacilityDomain,
  makeFacilityPath,
  makeKeyValueRow,
  makeMountRow,
  makePortRow,
  makeProxyPath,
  makeProxyRule,
  routeMode,
  routeSummary,
  runtimeSummary,
  saveInputFromDraft,
  statusTone,
  validateApplicationDraft,
  validateFileName,
  validateFacilityDraft,
  validateFacilityDomainFields,
  validateFacilityPathFields,
  type ApplicationDraftUi,
  type FacilityDraftUi,
  type FieldErrors,
  type KeyValueRow,
  type MountRow,
  type PortRow,
  type ProxyRuleDraft,
  type SaveStage,
} from './model';
import { inferTemplateLanguage, type TemplateLanguage } from './templateLanguage';
import { archiveName } from './archivePath';

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

const applications = ref<ApplicationSummaryDto[]>([]);
const applicationDetails = ref<Record<string, ApplicationDto>>({});
const applicationFiles = ref<Record<string, ApplicationFile[]>>({});
const runtimes = ref<Record<string, ApplicationRuntime>>({});
const rowImageLoading = ref<Record<string, boolean>>({});
const rowImageControllers = new Map<string, AbortController>();
const facility = ref<ReverseProxyConfig | null>(null);
const storageShareOptions = ref<{ label: string; value: string }[]>([]);
const storageShareAvailable = ref(false);
const storageShareLabelBySource = ref<Record<string, string>>({});
const storageShareConfig = ref<StorageShareConfig | null>(null);
const selectedId = ref(String(route.query.application ?? ''));
const search = ref(String(route.query.search ?? ''));
const page = ref(1);
const pageSize = 50;
const totalApplications = ref(0);
const loading = ref(false);
const detailLoading = ref(false);
const runtimeLoading = ref(false);
const editorLoading = ref(false);
const facilitiesLoaded = ref(true);
const facilities = [
  { kind: 'reverse-proxy', icon: Globe2, titleKey: 'applicationsPage.entranceProxyFacility', descriptionKey: 'applicationsPage.entranceProxyFacilityDescription', categoryKey: 'applicationsPage.facilityCategoryTraffic', status: 'available' },
  { kind: 'storage-share', icon: HardDrive, titleKey: 'applicationsPage.storageShareFacility', descriptionKey: 'applicationsPage.storageShareFacilityDescription', categoryKey: 'applicationsPage.facilityCategoryStorage', status: 'available' },
];
const error = ref('');
const actionError = ref('');
const pending = ref('');
const logsOpen = ref(false);
const logsText = ref('');
const logsLoading = ref(false);
const confirmOpen = ref(false);
const confirmKind = ref<'delete' | 'stop'>('delete');
const confirmTarget = ref('');
const restoreConfirmOpen = ref(false);
let restoreFile: File | null = null;
const discardDialogOpen = ref(false);
let pendingLeaveTarget: string | null = null;
let pendingFacilityCancel = false;
let allowLeave = false;
const appTab = ref('runtime');
const fileActionErrors = ref<Record<string, string>>({});
const fileActionPending = ref('');
const assetActionErrors = ref<Record<string, string>>({});
const assetActionPending = ref('');
const diagnostics = ref<Diagnostic[]>([]);
const preview = ref<ApplicationEditPreviewResult | null>(null);
const editSession = ref<ApplicationEditSession | null>(null);
const facilitySession = ref<FacilityEditSession | null>(null);
const facilityPreview = ref<FacilityEditPreviewResult | null>(null);
const facilityDiagnostics = ref<Diagnostic[]>([]);
const isDirty = ref(false);
const saveStage = ref<SaveStage>('idle');
const dialogOpen = ref(false);
const dialogKind = ref<'env' | 'port' | 'mount' | 'proxy' | 'proxyPath' | 'facilityDomain' | 'facilityPath'>('env');
const dialogIndex = ref(-1);
const dialogParentIndex = ref(-1);
const rowDraft = reactive<KeyValueRow>(makeKeyValueRow());
const portDraft = reactive<PortRow>(makePortRow());
const mountDraft = reactive<MountRow>(makeMountRow());
const proxyDraft = reactive<ProxyRuleDraft>({
  ...makeProxyRule(),
});
const proxyPathDraft = reactive(makeProxyPath());
const facilityDomainDraft = reactive<FacilityRouteDomain>(makeFacilityDomain());
const facilityPathDraft = reactive<FacilityRoutePath>(makeFacilityPath());
const facilityPathErrors = reactive<FieldErrors>({});
const facilityDomainErrors = reactive<FieldErrors>({});
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
const currentFacilitySummary = computed(() => facilities.find((item) => item.kind === (facilityKind.value || 'reverse-proxy')) ?? null);
const isFacilityEditor = computed(() => mode.value === 'facilityConfig' && isReverseProxyFacility.value);
const facilityEditing = ref(false);
const facilityEditingView = computed(() => (mode.value === 'facilityConfig' && isReverseProxyFacility.value) || facilityEditing.value);
const currentApplicationSummary = computed(() => applications.value.find((item) => item.id === selectedId.value) ?? null);
const currentApplication = computed(() => selectedId.value ? applicationDetails.value[selectedId.value] ?? null : null);
const selectedApplication = computed(() => currentApplication.value ?? applicationFromSummary(currentApplicationSummary.value) ?? emptyApplication());
const currentRuntime = computed(() => selectedId.value ? runtimes.value[selectedId.value] : null);
const appStatus = computed(() => currentApplicationSummary.value ? applicationStatus(selectedApplication.value, currentRuntime.value) : 'unknown');
const appDraft = reactive<ApplicationDraftUi>(draftFromApplication());
const facilityDraft = reactive<FacilityDraftUi>(facilityDraftFromConfig());
const appErrors = computed(() => validateApplicationDraft(appDraft));
const facilityErrors = computed(() => validateFacilityDraft(facilityDraft));
const appDiff = computed(() => diffApplications(mode.value === 'edit' ? currentApplication.value : null, appDraft));
const facilityDiff = computed(() => diffFacility(facility.value, facilityDraft));
const hasBlockingAppDiagnostics = computed(() => hasBlockingDiagnostic(diagnostics.value));
const hasBlockingFacilityDiagnostics = computed(() => hasBlockingDiagnostic(facilityDiagnostics.value));
const appRows = computed(() => applications.value.map((app) => ({ app, routes: routeSummary(applicationDetails.value[app.id] ?? emptyApplication()), summary: runtimeSummary(runtimes.value[app.id]) })));
const facilityConfigSummary = computed(() => ({
  gateways: facility.value?.deploymentServers.length ?? 0,
  routes: facility.value?.routes ?? 0,
  assets: facility.value?.staticAssets.length ?? 0,
  appRoutes: facility.value?.applicationRoutes.length ?? 0,
}));
const servers = ref<ServerDto[]>([]);
const serverNameMap = computed(() => new Map(servers.value.map((server) => [server.id, server.name])));
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
// 编辑器资产选项只来自编辑会话：beginEdit 已把全部已提交资产复制进会话，
// 会话内删除的资产会立即从选择器消失，避免把路由引用指向已删除资产
// （此前混入 facility.staticAssets 会让被删资产仍可选，保存时触发
// facility_static_asset_referenced_after_delete 且无法定位）。
const assetOptions = computed(() => (facilitySession.value?.assets ?? []).map((asset) => ({ value: asset.name, label: `${asset.name} / ${asset.filename}` })));
const fileLanguageOptions = computed(() => (['plain', 'yaml', 'json', 'shell', 'nginx', 'properties', 'dockerfile'] as TemplateLanguage[]).map((value) => ({
  value,
  label: t(`applicationsPage.templateLanguage.${value}`),
})));
const applicationAssetItems = computed<AssetFileItem[]>(() => (editSession.value?.files ?? []).map((file) => ({
  key: file.name,
  name: file.name,
  filename: file.kind === 'archive' ? file.contentType : file.name.split('/').at(-1) || file.name,
  kind: file.kind === 'template' ? 'text' : file.kind === 'archive' ? 'archive' : 'binary',
  size: file.size,
  sha256: file.sha256,
  editable: file.kind === 'template',
})));
const facilityAssetItems = computed<AssetFileItem[]>(() => (facilitySession.value?.assets ?? []).map((asset) => ({
  key: asset.name,
  name: asset.name,
  filename: asset.filename,
  kind: asset.contentMode === 'text' ? 'text' : asset.kind === 'uploaded_bundle' ? 'archive' : 'binary',
  size: asset.size,
  sha256: asset.sha256,
  editable: asset.contentMode === 'text',
})));
const mountTypeOptions = computed(() => ['persistent', 'volume', 'host', 'file', 'panel_file', 'storage_share'].map((value) => ({ label: t(`applicationsPage.mountType.${value}`), value })));
const routeTypeOptions = computed(() => ['static', 'redirect', 'proxy_pass'].map((value) => ({ label: t(`applicationsPage.routeType.${value}`), value })));
const sourceTypeOptions = computed(() => ['uploaded_file', 'uploaded_bundle'].map((value) => ({ label: t(`applicationsPage.sourceType.${value}`), value })));
const saving = computed(() => pending.value === 'preview' || pending.value === 'commit');
const proxyPathWebSocket = computed({
  get: () => Boolean(proxyPathDraft.webSocket),
  set: (value: boolean) => { proxyPathDraft.webSocket = value; },
});

const proxyAnyAccessModel = computed({
  get: () => Boolean(proxyDraft.anyAccess.enabled),
  set: (value: boolean) => { proxyDraft.anyAccess.enabled = value; },
});
const proxyRelayScope = ref<'all' | 'selected'>('all');
const facilityRelayScope = ref<'all' | 'selected'>('all');
const anyAccessRelayScopeOptions = computed(() => [
  { label: t('applicationsPage.anyAccessRelayAll'), value: 'all' },
  { label: t('applicationsPage.anyAccessRelaySelected'), value: 'selected' },
]);
const proxyRelayServerOptions = computed(() => {
  const origins = new Set(proxyAutoOriginIds.value);
  const gateways = new Set(facility.value?.deploymentServers ?? []);
  return serverOptions.value.filter((item) => gateways.has(item.id) && !origins.has(item.id));
});
const facilityRelayServerOptions = computed(() => {
  const origins = new Set(facilityDomainDraft.originServerIds);
  const gateways = new Set(facilityDraft.deploymentServers);
  return serverOptions.value.filter((item) => gateways.has(item.id) && !origins.has(item.id));
});
const proxyRelayServerIdsModel = computed({
  get: () => proxyDraft.anyAccess.relayServerIds ?? [],
  set: (value: string[]) => { proxyDraft.anyAccess.relayServerIds = value; },
});
const facilityRelayServerIdsModel = computed({
  get: () => facilityDomainDraft.anyAccess.relayServerIds ?? [],
  set: (value: string[]) => { facilityDomainDraft.anyAccess.relayServerIds = value; },
});
const facilityRedirectCode = computed({
  get: () => String(facilityPathDraft.redirectCode ?? 302),
  set: (value: string) => { facilityPathDraft.redirectCode = Number(value); },
});
const anyAccessStrategyOptions = computed(() => (['round_robin', 'ip_hash', 'primary_backup'] as const).map((value) => ({ label: t(`applicationsPage.loadBalancingStrategy.${value}`), value })));
const httpModeOptions = computed(() => (['inherit', 'on', 'off'] as const).map((value) => ({ label: t(`applicationsPage.httpMode.${value}`), value })));
const webSocketModeOptions = computed(() => (['auto', 'on', 'off'] as const).map((value) => ({ label: t(`applicationsPage.httpMode.${value}`), value })));
const proxySourceModeOptions = computed(() => (['preserve_source', 'hide_source'] as const).map((value) => ({ label: t(`applicationsPage.proxySourceMode.${value}`), value })));
const facilityDomainOriginOptions = computed(() => facilityDomainDraft.originServerIds.map((id) => ({ label: serverDisplayName(id), value: id })));
const facilityGatewayIdSet = computed(() => new Set(facilityDraft.deploymentServers));
const facilityDomainOriginServerOptions = computed(() => serverOptions.value.filter((item) => facilityGatewayIdSet.value.has(item.id)));

function facilityPathNumberOption(field: 'clientMaxBodySizeMb' | 'connectTimeoutSeconds' | 'readTimeoutSeconds' | 'sendTimeoutSeconds') {
  return computed({
    get: () => String(facilityPathDraft.options?.[field] ?? 0),
    set: (value: string) => {
      if (!facilityPathDraft.options) return;
      const parsed = Number(value);
      facilityPathDraft.options[field] = Number.isFinite(parsed) && parsed >= 0 ? parsed : 0;
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
  return computed({
    get: () => String(proxyPathDraft.options?.[field] ?? ''),
    set: (value: string) => {
      if (!proxyPathDraft.options) return;
      const parsed = Number(value);
      proxyPathDraft.options[field] = value.trim() === '' ? 0 : Number.isFinite(parsed) && parsed >= 0 ? parsed : 0;
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

const applicationAssetAdapter: AssetFileAdapter = {
  async upload({ file, kind }) {
    if (!editSession.value) return;
    if (kind === 'archive') {
      editSession.value = await applicationsApi.uploadEditSessionArchive(editSession.value.id, editSession.value.revision, {
        file,
        name: archiveName(file.name),
        kind: 'archive',
      });
    } else {
      editSession.value = await applicationsApi.uploadEditSessionFile(editSession.value.id, file.name, editSession.value.revision, { file, name: file.name });
    }
    markDirty();
  },
  async replace(item, { file, kind }) {
    if (!editSession.value) return;
    const source = editSession.value.files.find((entry) => entry.name === item.key);
    if (!source) throw new Error(t('applicationsPage.fileLoadFailed'));
    if (kind === 'archive') {
      editSession.value = await applicationsApi.uploadEditSessionArchive(editSession.value.id, editSession.value.revision, {
        file,
        name: source.name,
        kind: 'archive',
      });
    } else {
      editSession.value = await applicationsApi.uploadEditSessionFile(editSession.value.id, source.name, editSession.value.revision, { file, name: source.name });
    }
    markDirty();
  },
  async download(item) {
    if (!editSession.value) return;
    const source = editSession.value.files.find((entry) => entry.name === item.key);
    if (!source) throw new Error(t('applicationsPage.fileLoadFailed'));
    saveBlobDownload(await applicationsApi.downloadEditSessionFile(editSession.value.id, source.name, applicationFileDownloadName(source)));
  },
  async delete(item) {
    if (!editSession.value) return;
    editSession.value = await applicationsApi.deleteEditSessionFile(editSession.value.id, item.key, editSession.value.revision);
    markDirty();
  },
  async loadText(item) {
    if (!editSession.value) throw new Error(t('applicationsPage.fileLoadFailed'));
    const source = editSession.value.files.find((entry) => entry.name === item.key);
    const loaded = await applicationsApi.getEditSessionFile(editSession.value.id, item.key);
    return { content: decodeBase64Utf8(loaded.contentBase64), name: source?.name ?? loaded.name, language: inferTemplateLanguage(source?.name ?? loaded.name, loaded.contentType) };
  },
  async saveText({ key: fileName, name, content }) {
    if (!editSession.value) return;
    const nameError = validateFileName(name);
    if (nameError) throw new Error(t(nameError));
    editSession.value = await applicationsApi.putEditSessionFile(editSession.value.id, fileName, editSession.value.revision, { name, contentBase64: encodeBase64Utf8(content) });
    markDirty();
  },
  reload: reloadApplicationEditor,
};

const facilityAssetAdapter: AssetFileAdapter = {
  async upload({ file, kind }) {
    if (!facilitySession.value) return;
    const name = file.name.replace(/\.[^.]+$/, '') || file.name;
    facilitySession.value = await reverseProxyFacilityApi.putEditAsset(facilitySession.value.id, name, facilitySession.value.revision, {
      file,
      name,
      kind: kind === 'archive' ? 'uploaded_bundle' : 'uploaded_file',
      contentMode: 'binary',
    });
    markDirty();
  },
  async replace(item, { file, kind }) {
    if (!facilitySession.value) return;
    const source = facilitySession.value.assets.find((entry) => entry.name === item.key);
    if (!source) throw new Error(t('applicationsPage.fileLoadFailed'));
    facilitySession.value = await reverseProxyFacilityApi.putEditAsset(facilitySession.value.id, source.name, facilitySession.value.revision, {
      file,
      name: source.name,
      kind: kind === 'archive' ? 'uploaded_bundle' : 'uploaded_file',
      contentMode: 'binary',
    });
    markDirty();
  },
  async download(item) {
    if (!facilitySession.value) return;
    const source = facilitySession.value.assets.find((entry) => entry.name === item.key);
    if (!source) throw new Error(t('applicationsPage.fileLoadFailed'));
    saveBlobDownload(await reverseProxyFacilityApi.downloadEditAsset(facilitySession.value.id, item.key, source.filename));
  },
  async delete(item) {
    if (!facilitySession.value) return;
    facilitySession.value = await reverseProxyFacilityApi.deleteEditAsset(facilitySession.value.id, item.key, facilitySession.value.revision);
    markDirty();
  },
  async loadText(item) {
    if (!facilitySession.value) throw new Error(t('applicationsPage.fileLoadFailed'));
    const source = facilitySession.value.assets.find((entry) => entry.name === item.key);
    if (!source) throw new Error(t('applicationsPage.fileLoadFailed'));
    const result = await reverseProxyFacilityApi.downloadEditAsset(facilitySession.value.id, source.name, source.filename);
    return { content: await result.blob.text(), name: source.name, filename: source.filename, language: 'plain' as const };
  },
  async saveText({ key: assetName, name, filename, content }) {
    if (!facilitySession.value || !filename) return;
    const nameError = validateFileName(name) ?? validateFileName(filename);
    if (nameError) throw new Error(t(nameError));
    const file = new File([content], filename, { type: 'text/plain;charset=utf-8' });
    facilitySession.value = await reverseProxyFacilityApi.putEditAsset(facilitySession.value.id, assetName, facilitySession.value.revision, { file, name: name || filename, kind: 'uploaded_file', contentMode: 'text' });
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
const facilityDeploymentServersModel = computed({
  get: () => facilityDraft.deploymentServers,
  set: (value: string[]) => {
    const removed = facilityDraft.deploymentServers.filter((id) => !value.includes(id));
    facilityDraft.deploymentServers = value;
    if (removed.length) {
      const gatewaySet = new Set(value);
      let pruned = 0;
      facilityDraft.domains.forEach((domain) => {
        const before = domain.originServerIds.length;
        domain.originServerIds = domain.originServerIds.filter((id) => gatewaySet.has(id));
        pruned += before - domain.originServerIds.length;
        if (domain.anyAccess.primaryOriginServerId && !gatewaySet.has(domain.anyAccess.primaryOriginServerId)) {
          domain.anyAccess.primaryOriginServerId = '';
        }
        domain.anyAccess.relayServerIds = (domain.anyAccess.relayServerIds ?? []).filter((id) => gatewaySet.has(id));
      });
      if (pruned > 0) notify.push({ title: t('applicationsPage.gatewayPrunedOrigins', { count: pruned }), tone: 'warning' });
    }
    markDirty();
  },
});

const facilityDomainOriginServersModel = computed({
  get: () => facilityDomainDraft.originServerIds,
  set: (value: string[]) => {
    facilityDomainDraft.originServerIds = value;
    markDirty();
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

watch(page, () => {
  if (mode.value === 'apps') void loadApplications({ loadSelectedRuntime: true });
});

watch(selectedId, async (value) => {
  if (value) {
    await router.replace({ query: { ...route.query, application: value } });
    void loadApplicationDetail(value);
    await loadRuntime(value);
  }
});

watch(facilityEditingView, (editing) => {
  if (editing && dnsPollTimer) {
    clearTimeout(dnsPollTimer);
    dnsPollTimer = null;
  }
});

watch(() => route.path, async () => {
  cancelRuntimeLoad();
  cancelApplicationDetailLoad();
  cancelRowImageLoads();
  cancelEditorQuery();
  actionError.value = '';
  facilityEditing.value = false;
  isDirty.value = false;
  dialogOpen.value = false;
  await load();
  if (isAppEditor.value) await startApplicationEditor();
  if (isFacilityEditor.value) await startFacilityEditor();
});

onBeforeRouteLeave((to) => {
  if (saving.value) return false;
  if (allowLeave || !isDirty.value) return true;
  pendingFacilityCancel = false;
  pendingLeaveTarget = to.fullPath;
  discardDialogOpen.value = true;
  return false;
});

function handleBeforeUnload(event: BeforeUnloadEvent) {
  if (!isDirty.value && !saving.value) return;
  event.preventDefault();
  event.returnValue = '';
}

async function load() {
  void loadServers();
  const currentMode = mode.value;
  if (currentMode !== 'apps') {
    cancelRuntimeLoad();
    cancelApplicationDetailLoad();
    cancelRowImageLoads();
  }
  if (currentMode === 'apps') {
    await loadApplications({ loadSelectedRuntime: true });
    return;
  }
  if (isAppEditor.value) {
    await loadApplications({ loadSelectedRuntime: false });
    return;
  }
  if (currentMode === 'facilityCatalog' || currentMode === 'facilityDetail' || currentMode === 'facilityConfig') {
    await loadFacilityData();
  }
}

async function loadServers() {
  try {
    servers.value = await serversApi.list();
  } catch {
    // Server names are a display enhancement; keep IDs when the list is unavailable.
  }
}

function rowImageReference(app: ApplicationSummaryDto) {
  return applicationDetails.value[app.id]?.imageReference || app.imageReference || '';
}

function cancelRowImageLoads() {
  rowImageControllers.forEach((controller) => controller.abort());
  rowImageControllers.clear();
  rowImageLoading.value = {};
}

async function loadRowImageDetail(applicationId: string) {
  const controller = new AbortController();
  rowImageControllers.set(applicationId, controller);
  try {
    const app = await applicationsApi.get(applicationId, { signal: controller.signal });
    if (rowImageControllers.get(applicationId) !== controller || mode.value !== 'apps') return;
    applicationDetails.value = { ...applicationDetails.value, [applicationId]: app };
  } catch {
    // Keep the summary or job fallback when the image cannot be resolved.
  } finally {
    if (rowImageControllers.get(applicationId) === controller) {
      rowImageControllers.delete(applicationId);
      rowImageLoading.value = { ...rowImageLoading.value, [applicationId]: false };
    }
  }
}

async function loadApplications(options: { loadSelectedRuntime?: boolean } = {}) {
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
    if (modeAtStart === 'apps') {
      const missing: Record<string, boolean> = {};
      apps.forEach((app) => {
        if (!app.imageReference) missing[app.id] = true;
      });
      rowImageLoading.value = missing;
      Object.keys(missing).forEach((id) => { void loadRowImageDetail(id); });
    }
    const appIds = new Set(apps.map((item) => item.id));
    applicationDetails.value = Object.fromEntries(Object.entries(applicationDetails.value).filter(([id]) => appIds.has(id)));
    selectedId.value = apps.some((item) => item.id === selectedId.value) ? selectedId.value : apps[0]?.id ?? '';
    if (options.loadSelectedRuntime) {
      void loadApplicationDetail(selectedId.value);
      void loadRuntime(selectedId.value);
    }
  } catch (err) {
    if (isAbortError(err)) return;
    error.value = err instanceof Error ? err.message : t('applicationsPage.loadFailed');
    notifyError(err instanceof Error ? err.message : t('applicationsPage.loadFailed'));
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
    const primaryFacility = isReverseProxyFacility.value ? await reverseProxyFacilityApi.getConfig({ signal: controller.signal }) : null;
    if (requestId !== pageLoadRequestId || mode.value !== modeAtStart) return;
    facility.value = primaryFacility;
    try {
      storageShareConfig.value = await storageShareFacilityApi.get({ signal: controller.signal });
    } catch {
      storageShareConfig.value = null;
    }
    facilitiesLoaded.value = true;
    const pendingDns = primaryFacility?.dnsSync ? Object.values(primaryFacility.dnsSync).some((state) => state.state === 'pending') : false;
    if (pendingDns && !facilityEditingView.value && document.visibilityState === 'visible') {
      dnsPollTimer = setTimeout(() => { void loadFacilityData(); }, 3000);
    }
  } catch (err) {
    if (isAbortError(err)) return;
    error.value = err instanceof Error ? err.message : t('applicationsPage.loadFailed');
    notifyError(err instanceof Error ? err.message : t('applicationsPage.loadFailed'));
  } finally {
    if (requestId === pageLoadRequestId) loading.value = false;
  }
}

async function loadApplicationDetail(applicationId: string) {
  applicationDetailController?.abort();
  const requestId = ++applicationDetailRequestId;
  const modeAtStart = mode.value;
  if (!applicationId) {
    detailLoading.value = false;
    return;
  }
  if (modeAtStart !== 'apps') {
    detailLoading.value = false;
    return;
  }
  const controller = new AbortController();
  applicationDetailController = controller;
  detailLoading.value = true;
  try {
    const [app, files] = await Promise.all([
      applicationsApi.get(applicationId, { signal: controller.signal }),
      applicationsApi.listFiles(applicationId, { signal: controller.signal }),
    ]);
    if (requestId !== applicationDetailRequestId || mode.value !== modeAtStart || applicationId !== selectedId.value) return;
    applicationDetails.value = { ...applicationDetails.value, [applicationId]: app };
    applicationFiles.value = { ...applicationFiles.value, [applicationId]: files };
  } catch (err) {
    if (isAbortError(err)) return;
    notifyError(err instanceof Error ? err.message : t('applicationsPage.loadFailed'));
  } finally {
    if (requestId === applicationDetailRequestId) detailLoading.value = false;
  }
}

async function loadRuntime(applicationId: string) {
  runtimeController?.abort();
  const requestId = ++runtimeRequestId;
  const modeAtStart = mode.value;
  if (!applicationId) {
    detailLoading.value = false;
    runtimeLoading.value = false;
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
  try {
    const runtime = await applicationsApi.runtime(applicationId, { signal: controller.signal });
    if (requestId !== runtimeRequestId || mode.value !== modeAtStart || applicationId !== selectedId.value) return;
    runtimes.value = { ...runtimes.value, [applicationId]: runtime };
  } catch (err) {
    if (isAbortError(err)) return;
    notifyError(err instanceof Error ? err.message : t('applicationsPage.runtimeUnavailable'));
  } finally {
    if (requestId === runtimeRequestId) {
      detailLoading.value = false;
      runtimeLoading.value = false;
    }
  }
}

async function runOperation(name: string, action: () => Promise<unknown>, successKey: string, successKeyWithoutId = '') {
  pending.value = name;
  actionError.value = '';
  try {
    const result = await action();
    const params = taskParams(result);
    notifySuccess(params ? t(successKey, params) : t(successKeyWithoutId || successKey));
    await load();
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'));
  } finally {
    pending.value = '';
  }
}

function taskParams(result: unknown) {
  const record = result as { taskId?: string; deploymentId?: string; evalId?: string };
  const taskId = record?.taskId || record?.deploymentId || record?.evalId;
  return taskId ? { taskId } : null;
}

async function showLogs(app: ApplicationDto) {
  logsOpen.value = true;
  logsText.value = '';
  logsLoading.value = true;
  try {
    logsText.value = (await applicationsApi.logs(app.id, { tail: 240 })).logs;
  } catch (err) {
    const message = err instanceof Error ? err.message : t('applicationsPage.logsFailed');
    notifyError(message);
    logsText.value = t('applicationsPage.logsFailed');
  } finally {
    logsLoading.value = false;
  }
}

function ask(kind: typeof confirmKind.value, target: string) {
  confirmKind.value = kind;
  confirmTarget.value = target;
  confirmOpen.value = true;
}

async function confirmAction() {
  const app = selectedApplication.value;
  if (!currentApplicationSummary.value) return;
  if (confirmKind.value === 'delete') {
    await runOperation('delete', () => applicationsApi.delete(app.id), 'applicationsPage.deleted');
  } else {
    await runOperation('stop', () => applicationsApi.stop(app.id), 'applicationsPage.stopAccepted', 'applicationsPage.stopAcceptedWithoutId');
  }
  confirmOpen.value = false;
}

async function downloadPersistentData(app: ApplicationDto) {
  await runOperation('persistent-download', async () => {
    saveBlobDownload(await applicationsApi.downloadPersistentData(app.id));
    return { taskId: 'download' };
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
  await ensureApplicationsLoaded();
  await ensureFacilityLoaded();
  if (requestId !== editorQueryRequestId || mode.value !== modeAtStart || String(route.params.applicationId ?? '') !== appId) return;
  let app: ApplicationDto | null = null;
  if (appId) {
    selectedId.value = appId;
    app = applicationDetails.value[appId] ?? await applicationsApi.get(appId, { signal: controller.signal });
    if (requestId !== editorQueryRequestId || mode.value !== modeAtStart || String(route.params.applicationId ?? '') !== appId) return;
    applicationDetails.value = { ...applicationDetails.value, [appId]: app };
  }
  Object.assign(appDraft, draftFromApplication(app));
  diagnostics.value = [];
  preview.value = null;
  editSession.value = null;
  isDirty.value = false;
  try {
    editSession.value = await applicationsApi.beginEditSession(appId || undefined, saveInputFromDraft(appDraft));
    if (requestId !== editorQueryRequestId || mode.value !== modeAtStart || String(route.params.applicationId ?? '') !== appId) return;
    Object.assign(appDraft, draftFromApplication({ ...(app ?? emptyApplication()), ...editSession.value.draft }));
  } catch (err) {
    if (isAbortError(err)) return;
    const message = err instanceof Error ? err.message : t('applicationsPage.editorStartFailed');
    actionError.value = message;
    notifyError(message);
  }
}

async function patchApplicationDraft() {
  if (!editSession.value || Object.keys(appErrors.value).length) return;
  await runEditorAction(async () => {
    editSession.value = await applicationsApi.patchEditSession(editSession.value!.id, editSession.value!.revision, saveInputFromDraft(appDraft));
    preview.value = null;
    isDirty.value = false;
  });
}

async function previewApplication() {
  const localErrorKeys = Object.keys(appErrors.value);
  if (localErrorKeys.length) {
    actionError.value = t(appErrors.value[localErrorKeys[0]]);
    return;
  }
  await patchApplicationDraft();
  if (!editSession.value) return;
  await runEditorAction(async () => {
    preview.value = await applicationsApi.previewEditSession(editSession.value!.id, editSession.value!.revision);
    diagnostics.value = preview.value.diagnostics ?? [];
  }, 'preview');
}

async function commitApplication() {
  const localErrorKeys = Object.keys(appErrors.value);
  if (localErrorKeys.length) {
    actionError.value = t(appErrors.value[localErrorKeys[0]]);
    return;
  }
  await previewApplication();
  if (!editSession.value || !preview.value) return;
  if (hasBlockingAppDiagnostics.value) {
    notify.push({ title: t('applicationsPage.validationFound'), tone: 'warning' });
    return;
  }
  await runEditorAction(async () => {
    const result = await applicationsApi.commitEditSession(editSession.value!, preview.value!);
    notifySuccess(result.applyRequested ? t('applicationsPage.committedAndApplied') : t('applicationsPage.committed'));
    isDirty.value = false;
    await router.push({ path: '/applications/apps', query: { application: result.application.id } });
    await load();
  }, 'commit');
}

async function startFacilityEditor() {
  editorLoading.value = true;
  try {
    await startFacilityEditorCore();
  } finally {
    editorLoading.value = false;
  }
}

async function startFacilityEditorCore() {
  editorQueryController?.abort();
  const requestId = ++editorQueryRequestId;
  const controller = new AbortController();
  editorQueryController = controller;
  const modeAtStart = mode.value;
  const kindAtStart = facilityKind.value;
  await ensureFacilityLoaded();
  if (requestId !== editorQueryRequestId || mode.value !== modeAtStart || facilityKind.value !== kindAtStart) return;
  if (!isReverseProxyFacility.value) return;
  Object.assign(facilityDraft, facilityDraftFromConfig(facility.value));
  facilityDiagnostics.value = [];
  facilityPreview.value = null;
  facilitySession.value = null;
  isDirty.value = false;
  try {
    facilitySession.value = await reverseProxyFacilityApi.beginEdit(facilitySaveInputFromDraft(facilityDraft));
    if (requestId !== editorQueryRequestId || mode.value !== modeAtStart || facilityKind.value !== kindAtStart) return;
    Object.assign(facilityDraft, facilityDraftFromConfig({ ...(facility.value ?? emptyFacility()), deploymentServers: facilitySession.value.draft.deploymentServers, domains: facilitySession.value.draft.domains }));
  } catch (err) {
    if (isAbortError(err)) return;
    const message = err instanceof Error ? err.message : t('applicationsPage.editorStartFailed');
    actionError.value = message;
    notifyError(message);
  }
}

async function startInPlaceFacilityEdit() {
  if (facilityEditingView.value) return;
  facilityEditing.value = true;
  actionError.value = '';
  await startFacilityEditor();
}

function cancelFacilityEdit() {
  if (!isDirty.value) {
    performCancelFacilityEdit();
    return;
  }
  pendingLeaveTarget = null;
  pendingFacilityCancel = true;
  discardDialogOpen.value = true;
}

function performCancelFacilityEdit() {
  facilitySession.value = null;
  facilityPreview.value = null;
  facilityDiagnostics.value = [];
  actionError.value = '';
  isDirty.value = false;
  if (mode.value === 'facilityConfig') {
    void router.push(`/applications/facility-apps/${facilityKind.value}`);
    return;
  }
  facilityEditing.value = false;
}

function confirmDiscard() {
  discardDialogOpen.value = false;
  if (pendingFacilityCancel) {
    pendingFacilityCancel = false;
    performCancelFacilityEdit();
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
  if (facilityEditingView.value) {
    cancelFacilityEdit();
    return;
  }
  if (isStorageShareFacility.value && mode.value === 'facilityConfig') {
    void router.replace('/applications/facility-apps/storage-share');
    return;
  }
  void router.push('/applications/facility-apps');
}

async function reloadApplicationEditor() {
  pending.value = 'editor-reload';
  const currentSession = editSession.value;
  try {
    if (currentSession) await applicationsApi.discardEditSession(currentSession.id);
    editSession.value = null;
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
  const currentSession = facilitySession.value;
  try {
    if (currentSession) await reverseProxyFacilityApi.discardEdit(currentSession.id);
    facilitySession.value = null;
    actionError.value = '';
    await loadFacilityData();
    await startFacilityEditor();
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'));
  } finally {
    pending.value = '';
  }
}

async function patchFacilityDraft() {
  if (!facilitySession.value || Object.keys(facilityErrors.value).length) return;
  await runEditorAction(async () => {
    facilitySession.value = await reverseProxyFacilityApi.patchEdit(facilitySession.value!.id, facilitySession.value!.revision, facilitySession.value!.baseResourceVersion.value, facilitySaveInputFromDraft(facilityDraft));
    facilityPreview.value = null;
    isDirty.value = false;
  });
}

async function previewFacilityConfig() {
  const localErrorKeys = Object.keys(facilityErrors.value);
  if (localErrorKeys.length) {
    actionError.value = t(facilityErrors.value[localErrorKeys[0]]);
    return;
  }
  await patchFacilityDraft();
  if (!facilitySession.value) return;
  await runEditorAction(async () => {
    facilityPreview.value = await reverseProxyFacilityApi.previewEdit(facilitySession.value!.id, facilitySession.value!.revision);
    facilityDiagnostics.value = facilityPreview.value.diagnostics ?? [];
  }, 'preview');
}

async function commitFacilityConfig() {
  const localErrorKeys = Object.keys(facilityErrors.value);
  if (localErrorKeys.length) {
    actionError.value = t(facilityErrors.value[localErrorKeys[0]]);
    return;
  }
  await previewFacilityConfig();
  if (!facilitySession.value || !facilityPreview.value) return;
  if (hasBlockingFacilityDiagnostics.value) {
    notify.push({ title: t('applicationsPage.validationFound'), tone: 'warning' });
    return;
  }
  await runEditorAction(async () => {
    const result = await reverseProxyFacilityApi.commitEdit(facilitySession.value!, facilityPreview.value!);
    facility.value = result.config;
    notifySuccess(result.applyRequested ? t('applicationsPage.gatewayCommittedAndApplied') : t('applicationsPage.gatewayCommitted'));
    isDirty.value = false;
    if (mode.value === 'facilityConfig') {
      await router.replace(`/applications/facility-apps/${facilityKind.value}`);
      await load();
    } else {
      facilityEditing.value = false;
      await load();
    }
  }, 'commit');
}

async function runEditorAction(action: () => Promise<void>, name = 'editor') {
  pending.value = name;
  saveStage.value = name as SaveStage;
  actionError.value = '';
  try {
    await action();
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'));
  } finally {
    pending.value = '';
    saveStage.value = 'idle';
  }
}

async function ensureApplicationsLoaded() {
  if (!applications.value.length) await loadApplications({ loadSelectedRuntime: false });
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
  dialogOpen.value = true;
}

function savePortDialog() {
  const next = { ...portDraft };
  if (dialogIndex.value >= 0) appDraft.ports[dialogIndex.value] = next;
  else appDraft.ports.push(next);
  dialogOpen.value = false;
  markAppStructuredDirty();
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
      webSocket: Boolean(path.webSocket),
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
  Object.assign(proxyPathDraft, index >= 0 ? structuredClone(proxyDraft.paths[index]) : makeProxyPath());
  if (!proxyPathDraft.options) Object.assign(proxyPathDraft, { options: defaultRouteOptions() });
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

function openFacilityDomainDialog(index = -1) {
  dialogKind.value = 'facilityDomain';
  dialogIndex.value = index;
  Object.keys(facilityDomainErrors).forEach((key) => delete facilityDomainErrors[key]);
  Object.assign(facilityDomainDraft, index >= 0 ? cloneFacilityDomains([facilityDraft.domains[index]])[0] : makeFacilityDomain());
  const domainGatewaySet = new Set(facilityDraft.deploymentServers);
  facilityDomainDraft.originServerIds = facilityDomainDraft.originServerIds.filter((id) => domainGatewaySet.has(id));
  const domainPrimaryOrigin = facilityDomainDraft.anyAccess.primaryOriginServerId || '';
  facilityDomainDraft.anyAccess.primaryOriginServerId = domainPrimaryOrigin && facilityDomainDraft.originServerIds.includes(domainPrimaryOrigin) ? domainPrimaryOrigin : '';
  facilityDomainDraft.anyAccess.relayServerIds = (facilityDomainDraft.anyAccess.relayServerIds ?? []).filter((id) => domainGatewaySet.has(id));
  facilityRelayScope.value = facilityDomainDraft.anyAccess?.relayServerIds?.length ? 'selected' : 'all';
  dialogOpen.value = true;
}

function clearFacilityDomainError(field: string) {
  delete facilityDomainErrors[field];
}

function saveFacilityDomainDialog() {
  const nextErrors = validateFacilityDomainFields(facilityDomainDraft, facilityDraft.domains, dialogIndex.value);
  Object.keys(facilityDomainErrors).forEach((key) => delete facilityDomainErrors[key]);
  Object.assign(facilityDomainErrors, nextErrors);
  if (Object.keys(facilityDomainErrors).length) return;
  const next = cloneFacilityDomains([facilityDomainDraft])[0];
  next.anyAccess.relayServerIds = facilityDomainDraft.anyAccess.enabled && facilityRelayScope.value === 'selected' ? [...(facilityDomainDraft.anyAccess.relayServerIds ?? [])] : [];
  if (dialogIndex.value >= 0) facilityDraft.domains[dialogIndex.value] = next;
  else facilityDraft.domains.push(next);
  dialogOpen.value = false;
  markDirty();
}

function openFacilityPathDialog(domainIndex: number, pathIndex = -1) {
  dialogKind.value = 'facilityPath';
  dialogParentIndex.value = domainIndex;
  dialogIndex.value = pathIndex;
  Object.keys(facilityPathErrors).forEach((key) => delete facilityPathErrors[key]);
  Object.assign(facilityPathDraft, pathIndex >= 0 ? cloneFacilityPath(facilityDraft.domains[domainIndex].paths[pathIndex]) : makeFacilityPath());
  if (!facilityPathDraft.options) Object.assign(facilityPathDraft, { options: defaultRouteOptions() });
  facilityRequestHeaders.value = (facilityPathDraft.options?.requestHeaders ?? []).map((header) => makeKeyValueRow(header.name, header.value));
  facilityResponseHeaders.value = (facilityPathDraft.options?.responseHeaders ?? []).map((header) => makeKeyValueRow(header.name, header.value));
  dialogOpen.value = true;
}

function clearFacilityPathError(field: string) {
  delete facilityPathErrors[field];
}

function saveFacilityPathDialog() {
  const paths = facilityDraft.domains[dialogParentIndex.value]?.paths;
  if (!paths) return;
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
  if (dialogIndex.value >= 0) paths[dialogIndex.value] = next;
  else paths.push(next);
  dialogOpen.value = false;
  markDirty();
}

function removeAt<T>(items: T[], index: number, domain: 'app' | 'facility' = 'app') {
  items.splice(index, 1);
  if (domain === 'app') markAppStructuredDirty();
  else markDirty();
}

function decodeBase64Utf8(value: string) {
  const bytes = Uint8Array.from(atob(value), (character) => character.charCodeAt(0));
  return new TextDecoder('utf-8', { fatal: true }).decode(bytes);
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
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'));
  } finally {
    fileActionPending.value = '';
  }
}

async function downloadCommittedFacilityAsset(asset: StaticAsset) {
  await runAssetAction(`committed:${asset.name}`, async () => {
    saveBlobDownload(await reverseProxyFacilityApi.downloadStaticAsset(asset.name, asset.filename));
  });
}

async function runAssetAction(key: string, action: () => Promise<void>) {
  assetActionPending.value = key;
  assetActionErrors.value = { ...assetActionErrors.value, [key]: '' };
  try {
    await action();
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'));
  } finally {
    assetActionPending.value = '';
  }
}

function instanceStatusLabel(status: string) {
  if (!status) return t('common.notAvailable');
  const key = `applicationsPage.instanceStatus.${status}`;
  const label = t(key);
  return label === key ? status : label;
}

// 诊断详情（domain/path/assetName 等）只放在 details 里，页面必须展示出来，
// 否则像“仍有路由引用了已删除的静态资产”这类错误无法定位到具体域名与路径。
function diagnosticDetailText(item: Diagnostic): string {
  const details = item.details ?? {};
  const parts: string[] = [];
  const domain = typeof details.domain === 'string' ? details.domain : '';
  const path = typeof details.path === 'string' ? details.path : '';
  const assetName = typeof details.assetName === 'string' ? details.assetName : '';
  const serverId = typeof details.serverId === 'string' ? details.serverId : '';
  if (domain) parts.push(`${t('applicationsPage.domain')}: ${domain}`);
  if (path) parts.push(`${t('common.path')}: ${path}`);
  if (assetName) parts.push(`${t('applicationsPage.assetReferenceName')}: ${assetName}`);
  if (serverId) parts.push(`${t('applicationsPage.server')}: ${serverId}`);
  return parts.length ? ` — ${parts.join(' · ')}` : '';
}

function sourceSummary() {
  return appDraft.image || t('common.notAvailable');
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
  await load();
  if (isAppEditor.value) await startApplicationEditor();
  if (isFacilityEditor.value) await startFacilityEditor();
});

onBeforeUnmount(() => {
  if (searchTimer) clearTimeout(searchTimer);
  if (dnsPollTimer) clearTimeout(dnsPollTimer);
  window.removeEventListener('beforeunload', handleBeforeUnload);
  pageLoadController?.abort();
  cancelRuntimeLoad();
  cancelApplicationDetailLoad();
  cancelRowImageLoads();
  cancelEditorQuery();
});
</script>

<template>
  <ConsolePage v-if="mode === 'apps'" :title="t('routes.applications.title')" :description="t('routes.applications.description')">
    <template #actions>
      <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
      <Button size="sm" variant="primary" @click="router.push('/applications/apps/create')"><Plus />{{ t('applicationsPage.createApplication') }}</Button>
    </template>

    <MasterDetailLayout class="h-full min-h-[660px]">
      <template #master>
      <aside class="grid min-h-0 min-w-0 grid-rows-[auto_minmax(0,1fr)_auto] rounded-2xl border border-border bg-card">
        <div class="border-b border-border p-4">
          <SearchInput v-model="search" clearable :placeholder="t('applicationsPage.searchPlaceholder')" :label="t('common.search')" :clear-label="t('common.clearSearch')" />
        </div>
        <div class="motion-stagger min-h-0 overflow-auto p-2">
          <div v-if="loading && !applications.length" class="grid gap-2" aria-hidden="true">
            <div v-for="item in 7" :key="item" class="grid gap-2 rounded-xl border border-border bg-muted p-3">
              <div class="flex items-center justify-between gap-2">
                <div class="motion-skeleton h-4 w-36 rounded bg-muted animate-pulse" />
                <div class="motion-skeleton h-6 w-20 rounded-full bg-muted animate-pulse" />
              </div>
              <div class="motion-skeleton h-3 w-56 max-w-full rounded bg-muted animate-pulse" />
              <div class="flex gap-1.5">
                <div class="motion-skeleton h-5 w-20 rounded-full bg-muted animate-pulse" />
                <div class="motion-skeleton h-5 w-24 rounded-full bg-muted animate-pulse" />
              </div>
            </div>
          </div>
          <EmptyState v-else-if="error && !appRows.length" :title="t('common.loadFailed')" :description="error">
            <template #actions>
              <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button>
            </template>
          </EmptyState>
          <EmptyState v-else-if="!appRows.length" :title="t('applicationsPage.emptyApplications')" :description="t('applicationsPage.emptyApplicationsHint')" />
          <button v-for="row in appRows" v-else :key="row.app.id" type="button" class="motion-list-item mb-2 grid w-full gap-2 rounded-xl border p-3 text-left transition-colors hover:bg-accent" :class="selectedId === row.app.id ? 'border-border-strong bg-muted' : 'border-transparent bg-transparent'" @click="selectedId = row.app.id">
            <div class="flex items-center justify-between gap-2">
              <strong class="truncate text-sm text-foreground">{{ row.app.name }}</strong>
              <StatusBadge :status="applicationStatus(row.app, runtimes[row.app.id])" :tone="statusTone(applicationStatus(row.app, runtimes[row.app.id]))" :label="t(`applicationsPage.status.${applicationStatus(row.app, runtimes[row.app.id])}`)" />
            </div>
            <span v-if="!loading && !(rowImageLoading[row.app.id] && !rowImageReference(row.app))" class="truncate text-xs text-muted-foreground">{{ rowImageReference(row.app) || row.app.jobId }}</span>
            <div v-else class="motion-skeleton h-3 w-56 max-w-full rounded bg-muted animate-pulse" aria-hidden="true" />
            <div class="flex flex-wrap gap-1.5">
              <Badge v-if="!loading" tone="info">{{ t('applicationsPage.instancesCount', { count: row.app.instanceCount ?? row.summary.total }) }}</Badge>
              <div v-else class="motion-skeleton h-5 w-16 rounded-full bg-muted animate-pulse" aria-hidden="true" />
              <Badge :tone="row.app.imageUpdateAvailable ? 'warning' : 'neutral'">{{ row.app.imageUpdateAvailable ? t('applicationsPage.imageUpdate') : t('applicationsPage.imageCurrent') }}</Badge>
            </div>
          </button>
        </div>
        <PaginationBar v-model:page="page" class="px-3" :page-size="pageSize" :total="totalApplications" :loading="loading" :previous-label="t('common.previous')" :next-label="t('common.next')" />
      </aside>
      </template>

      <template #detail>
      <main class="grid min-h-0 min-w-0">
        <article v-if="loading && !applications.length" class="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-2xl border border-border bg-card">
          <header class="border-b border-border p-5">
            <div class="motion-skeleton h-7 w-48 rounded bg-muted animate-pulse" />
            <div class="motion-skeleton mt-3 h-4 w-80 max-w-full rounded bg-muted animate-pulse" />
          </header>
          <div class="min-h-0 overflow-auto p-5">
            <div class="grid gap-5 xl:grid-cols-[minmax(0,1fr)_320px]">
              <div class="grid gap-4">
                <div class="grid grid-cols-3 gap-3 max-md:grid-cols-1">
                  <div v-for="item in 3" :key="item" class="motion-skeleton h-24 rounded-2xl bg-muted animate-pulse" />
                </div>
                <div class="motion-skeleton h-72 rounded-2xl bg-muted animate-pulse" />
              </div>
              <aside class="grid content-start gap-3">
                <div v-for="item in 2" :key="item" class="motion-skeleton h-44 rounded-2xl bg-muted animate-pulse" />
              </aside>
            </div>
          </div>
        </article>
        <EmptyState v-else-if="!currentApplicationSummary" :title="t('applicationsPage.selectApplication')" :description="t('applicationsPage.selectApplicationHint')" />
        <article v-else class="relative grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)] overflow-hidden rounded-2xl border border-border bg-card">
          <LoadingOverlay v-if="detailLoading && !applicationDetails[selectedId]" />
          <header class="flex items-start justify-between gap-4 border-b border-border p-5 max-md:grid">
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-2">
                <h2 class="m-0 truncate text-xl font-semibold text-foreground">{{ selectedApplication.name }}</h2>
                <StatusBadge :status="appStatus" :tone="statusTone(appStatus)" :label="t(`applicationsPage.status.${appStatus}`)" />
                <Badge :tone="selectedApplication.imageUpdateAvailable ? 'warning' : 'neutral'">{{ selectedApplication.imageUpdateAvailable ? t('applicationsPage.imageUpdate') : t('applicationsPage.imageCurrent') }}</Badge>
              </div>
              <p class="m-0 mt-1 text-sm text-muted-foreground">{{ selectedApplication.imageReference || selectedApplication.jobId }}</p>
            </div>
            <div class="flex flex-wrap gap-2">
              <Button @click="router.push(`/applications/apps/${encodeURIComponent(selectedApplication.id)}/edit`)"><Wrench />{{ t('common.edit') }}</Button>
              <Button :loading="pending === 'deploy'" @click="runOperation('deploy', () => applicationsApi.deploy(selectedApplication.id), 'applicationsPage.deployAccepted', 'applicationsPage.deployAcceptedWithoutId')"><Rocket />{{ t('applicationsPage.sync') }}</Button>
              <Button variant="danger" :disabled="!selectedApplication.enabled" @click="ask('stop', selectedApplication.id)"><Square />{{ t('applicationsPage.disable') }}</Button>
            </div>
          </header>
          <div class="min-h-0 overflow-auto p-5">
            <div class="grid gap-5 xl:grid-cols-[minmax(0,1fr)_320px]">
              <div class="min-w-0">
                <Tabs v-model="appTab" :tabs="[{ label: t('applicationsPage.runtime'), value: 'runtime' }, { label: t('applicationsPage.routes'), value: 'routes' }, { label: t('applicationsPage.files'), value: 'files' }]">
                  <section v-if="appTab === 'runtime'" class="grid gap-4">
                    <div class="grid grid-cols-3 gap-3 max-md:grid-cols-1">
                      <div class="rounded-2xl border border-border bg-muted p-4"><span>{{ t('applicationsPage.instances') }}</span><strong>{{ runtimeSummary(currentRuntime).total }}</strong></div>
                      <div class="rounded-2xl border border-border bg-muted p-4"><span>{{ t('applicationsPage.running') }}</span><strong>{{ runtimeSummary(currentRuntime).running }}</strong></div>
                      <div class="rounded-2xl border border-border bg-muted p-4"><span>{{ t('applicationsPage.failed') }}</span><strong>{{ runtimeSummary(currentRuntime).failed }}</strong></div>
                    </div>
                    <div class="rounded-2xl border border-border bg-muted p-4">
                      <h3>{{ t('applicationsPage.nodeInstances') }}</h3>
                      <div v-if="!runtimeLoading && currentRuntime?.instances?.length" class="mt-3 grid gap-2">
                        <div v-for="instance in currentRuntime.instances" :key="instance.instanceId || instance.id" class="grid gap-1 rounded-xl border border-border p-3 text-sm">
                          <div class="flex items-center justify-between gap-2"><strong>{{ instance.serverName || instance.serverId || instance.instanceId }}</strong><StatusBadge :status="instance.status || instance.state || 'unknown'" :tone="statusTone(instance.status || instance.state || 'unknown')" :label="instanceStatusLabel(instance.status || instance.state || '')" /></div>
                          <span class="text-muted-foreground">{{ instance.containerName || instance.containerId || t('common.notAvailable') }}</span>
                          <span v-if="instance.error" class="text-danger">{{ instance.error }}</span>
                        </div>
                      </div>
                      <div v-else-if="runtimeLoading" class="mt-3 grid gap-2" aria-hidden="true">
                        <div v-for="item in 4" :key="item" class="grid gap-2 rounded-xl border border-border p-3">
                          <div class="flex items-center justify-between gap-2">
                            <div class="motion-skeleton h-4 w-36 rounded bg-muted animate-pulse" />
                            <div class="motion-skeleton h-6 w-20 rounded-full bg-muted animate-pulse" />
                          </div>
                          <div class="motion-skeleton h-3 w-56 max-w-full rounded bg-muted animate-pulse" />
                        </div>
                      </div>
                      <EmptyState v-else :title="t('applicationsPage.noRuntime')" :description="t('applicationsPage.noRuntimeHint')" />
                    </div>
                  </section>
                  <section v-else-if="appTab === 'routes'" class="grid gap-3 motion-stagger">
                    <div v-for="rule in selectedApplication.reverseProxy" :key="rule.domain" class="motion-reveal rounded-2xl border border-border bg-muted p-4">
                      <div class="flex items-center justify-between gap-2"><strong>{{ rule.domain }}</strong><Badge tone="info">{{ rule.targetPort }}</Badge></div>
                      <p class="text-sm text-muted-foreground">{{ t('applicationsPage.originServers', { count: rule.originServerIds.length }) }} · {{ rule.originServerIds.map((id) => serverDisplayName(id)).join(', ') || t('common.notAvailable') }}</p>
                      <div class="mt-2 flex flex-wrap gap-2"><Badge v-for="path in rule.paths" :key="path.path" tone="neutral">{{ path.path }}</Badge></div>
                    </div>
                    <EmptyState v-if="!selectedApplication.reverseProxy.length" :title="t('applicationsPage.noRoutes')" :description="t('applicationsPage.noRoutesHint')" />
                  </section>
                  <section v-else class="rounded-2xl border border-border bg-muted p-4">
                    <div class="grid gap-3 motion-stagger">
                      <div v-for="file in applicationFiles[selectedApplication.id] || []" :key="file.name" class="item-row motion-reveal">
                        <div><strong>{{ file.name }}</strong><span>{{ file.size }} {{ t('applicationsPage.bytes') }}</span></div>
                        <div class="row-actions">
                          <DownloadButton size="sm" :loading="fileActionPending === `committed:${file.name}`" :label="t('common.download')" @click="downloadCommittedApplicationFile(file)" />
                        </div>
                      </div>
                      <EmptyState v-if="!(applicationFiles[selectedApplication.id] || []).length" :title="t('applicationsPage.noFiles')" :description="t('applicationsPage.noCommittedFilesHint')" />
                    </div>
                  </section>
                </Tabs>
              </div>
              <aside class="grid content-start gap-3">
                <section class="rounded-2xl border border-border bg-muted p-4">
                  <h3>{{ t('applicationsPage.operations') }}</h3>
                  <div class="mt-3 grid gap-2">
                    <Button :disabled="!selectedApplication.imageUpdateAvailable" :loading="pending === 'image-update'" @click="runOperation('image-update', () => applicationsApi.updateImage(selectedApplication.id), 'applicationsPage.imageUpdateAccepted', 'applicationsPage.imageUpdateAcceptedWithoutId')"><UploadCloud />{{ t('applicationsPage.updateImage') }}</Button>
                    <Button @click="router.push({ path: '/application-operations', query: { applicationId: selectedApplication.id } })"><ClipboardList />{{ t('applicationsPage.operationRecords') }}</Button>
                    <Button :loading="logsLoading" @click="showLogs(selectedApplication)"><History />{{ t('applicationsPage.logs') }}</Button>
                    <Button variant="danger" @click="ask('delete', selectedApplication.id)"><Trash2 />{{ t('common.delete') }}</Button>
                  </div>
                </section>
                <section class="rounded-2xl border border-border bg-muted p-4">
                  <h3>{{ t('applicationsPage.persistentData') }}</h3>
                  <p class="text-sm text-muted-foreground">{{ selectedApplication.persistentPath ? t('applicationsPage.persistentDataHint') : t('applicationsPage.persistentDataUnavailable') }}</p>
                  <div class="mt-3 flex flex-wrap gap-2">
                    <DownloadButton size="sm" :disabled="!selectedApplication.persistentPath" :loading="pending === 'persistent-download'" :label="t('applicationsPage.downloadPersistentData')" @click="downloadPersistentData(selectedApplication)" />
                    <FileUploadButton size="sm" accept=".zip,application/zip" :disabled="!selectedApplication.persistentPath" :loading="pending === 'persistent-restore'" :label="t('applicationsPage.restorePersistentData')" @change="restorePersistentData" />
                  </div>
                </section>
              </aside>
            </div>
          </div>
        </article>
      </main>
      </template>
    </MasterDetailLayout>
  </ConsolePage>

  <ConsolePage v-else-if="mode === 'facilityCatalog'" :title="t('routes.facilityApps.title')" :description="t('routes.facilityApps.description')">
    <template #actions>
      <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
    </template>
    <div class="grid gap-4">
      <div>
        <h2 class="m-0 text-lg font-semibold text-foreground">{{ t('applicationsPage.facilityCatalogTitle') }}</h2>
        <p class="m-0 mt-1 text-sm text-muted-foreground">{{ t('applicationsPage.facilityCatalogHint') }}</p>
      </div>
            <div class="grid gap-3 motion-stagger">
        <article
          v-for="item in facilities"
          :key="item.kind"
          class="motion-list-item grid gap-5 rounded-2xl border border-border bg-card p-5 lg:grid-cols-[auto_minmax(0,1fr)_auto]"
        >
          <div class="grid size-14 shrink-0 place-items-center rounded-2xl border border-border bg-muted/40 text-foreground">
            <component :is="item.icon" class="size-6" aria-hidden="true" />
          </div>
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-2">
              <h3 class="m-0 text-base font-semibold text-foreground">{{ t(item.titleKey) }}</h3>
              <Badge tone="info">{{ t(item.categoryKey) }}</Badge>
              <StatusBadge :status="item.status" :tone="item.status === 'degraded' ? 'danger' : 'success'" :label="t(`applicationsPage.facilityStatus.${item.status}`)" />
            </div>
            <p class="m-0 mt-1 text-sm leading-6 text-muted-foreground">{{ t(item.descriptionKey) }}</p>
            <div class="facility-card-stats mt-4">
              <template v-if="loading">
                <div v-for="stat in 3" :key="stat" class="facility-card-stat" aria-hidden="true">
                  <div class="motion-skeleton h-5 w-10 rounded bg-muted animate-pulse" />
                  <div class="motion-skeleton mt-1 h-3 w-16 rounded bg-muted animate-pulse" />
                </div>
              </template>
              <template v-else-if="item.kind === 'storage-share'">
                <div class="facility-card-stat"><strong>{{ storageShareConfig?.partitions.length ?? '—' }}</strong><span>{{ t('applicationsPage.storageSharePartitions') }}</span></div>
                <div class="facility-card-stat"><strong>{{ storageShareConfig?.servers.length ?? '—' }}</strong><span>{{ t('applicationsPage.storageShareServers') }}</span></div>
                <div class="facility-card-stat"><strong>{{ storageShareConfig?.servers.filter((server) => server.root).length ?? '—' }}</strong><span>{{ t('applicationsPage.storageShareRoot') }}</span></div>
              </template>
              <template v-else>
                <div class="facility-card-stat"><strong>{{ facility?.routes ?? '—' }}</strong><span>{{ t('applicationsPage.gatewayRoutes') }}</span></div>
                <div class="facility-card-stat"><strong>{{ facility?.deploymentServers.length ?? '—' }}</strong><span>{{ t('applicationsPage.gatewayNodes') }}</span></div>
                <div class="facility-card-stat"><strong>{{ facility?.staticAssets.length ?? '—' }}</strong><span>{{ t('applicationsPage.staticAssets') }}</span></div>
              </template>
            </div>
          </div>
          <div class="flex flex-wrap items-start gap-2 lg:justify-end">
            <Button size="sm" variant="primary" @click="router.push(`/applications/facility-apps/${item.kind}`)"><Wrench />{{ t('applicationsPage.manageFacility') }}</Button>
            <Button size="sm" variant="secondary" :disabled="item.kind === 'storage-share' && !storageShareConfig?.enabled" :loading="pending === `facility-reconcile-${item.kind}`" @click="runOperation(`facility-reconcile-${item.kind}`, () => item.kind === 'storage-share' ? storageShareFacilityApi.reconcile() : reverseProxyFacilityApi.reconcile(), item.kind === 'storage-share' ? 'applicationsPage.storageShareReconcileAccepted' : 'applicationsPage.gatewayReconcileAccepted', 'applicationsPage.gatewayReconcileAcceptedWithoutId')"><Rocket />{{ item.kind === 'storage-share' ? t('applicationsPage.storageShareReconcile') : t('applicationsPage.reconcileGateway') }}</Button>
          </div>
        </article>
        <EmptyState v-if="!facilities.length" :title="t('applicationsPage.emptyFacilityCatalog')" :description="t('applicationsPage.emptyFacilityCatalogHint')" />
      </div>
    </div>
  </ConsolePage>

  <ConsolePage v-else-if="mode === 'facilityDetail' || mode === 'facilityConfig'" :back-label="t('common.back')" @back="goBackFromFacilityPage" :title="facilityEditingView ? t('applicationsPage.gatewayEditor') : (currentFacilitySummary ? t(currentFacilitySummary.titleKey) : t('applicationsPage.facilityUnavailable'))" :description="facilityEditingView ? t('applicationsPage.gatewayEditorDescription') : (currentFacilitySummary ? t(currentFacilitySummary.descriptionKey) : t('applicationsPage.facilityUnavailableDescription', { kind: facilityKind }))">
    <template #actions>
      <template v-if="!facilityEditingView">
        <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
        <Button v-if="currentFacilitySummary && isReverseProxyFacility" size="sm" @click="runOperation(`facility-reconcile-${facilityKind}`, () => reverseProxyFacilityApi.reconcile(), 'applicationsPage.gatewayReconcileAccepted', 'applicationsPage.gatewayReconcileAcceptedWithoutId')"><Rocket />{{ t('applicationsPage.reconcileGateway') }}</Button>
        <Button v-if="currentFacilitySummary && isReverseProxyFacility" size="sm" variant="primary" @click="startInPlaceFacilityEdit"><Wrench />{{ t('common.edit') }}</Button>
      </template>
    </template>
    <template v-if="facilityEditingView">
      <EditorPage>
        <div v-if="!facilitySession" class="relative grid min-h-64 place-items-center">
          <LoadingOverlay v-if="editorLoading || !actionError" />
          <div v-else class="grid max-w-md justify-items-center gap-3 text-center">
            <AlertTriangle class="size-6 text-danger" aria-hidden="true" />
            <p class="m-0 text-sm text-danger">{{ actionError }}</p>
            <Button size="sm" :loading="pending === 'editor-reload'" @click="reloadFacilityEditor">{{ t('common.retry') }}</Button>
          </div>
        </div>
        <div v-if="saving" class="mb-4 rounded-xl border border-info-border bg-info-bg p-3 text-sm text-info">{{ t(`applicationsPage.saveStage.${saveStage}`) }}</div>
        <div v-if="facilitySession && actionError" class="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-danger-border bg-danger-bg p-3 text-sm text-danger">
          <span class="text-danger">{{ actionError }}</span>
          <Button size="sm" :loading="pending === 'editor-reload'" @click="reloadFacilityEditor">{{ t('applicationsPage.reloadFacilityDraft') }}</Button>
        </div>
        <div v-if="facilitySession" class="relative app-editor-layout">
          <LoadingOverlay v-if="saving" :label="t(`applicationsPage.saveStage.${saveStage}`)" />
          <section class="app-editor-shell">
            <div class="app-editor-header">
              <p class="editor-flow-hint">{{ t('applicationsPage.gatewayEditorFlowHint') }}</p>
            </div>
  
            <div class="app-editor-body">
              <section class="workspace-panel">
                <div class="section-copy"><h3>{{ t('applicationsPage.gatewayNodes') }}</h3><p>{{ t('applicationsPage.gatewayNodesHint') }}</p></div>
                <div class="server-picker-grid">
                  <ServerMultiPicker v-model="facilityDeploymentServersModel" :servers="serverOptions" :label="t('applicationsPage.gatewayNodes')" />
                </div>
              </section>
  
              <section class="workspace-panel">
                <div class="section-heading"><div class="section-copy"><h3>{{ t('applicationsPage.domainGroups') }}</h3><p>{{ t('applicationsPage.domainGroupsHint') }}</p></div><Button size="sm" @click="openFacilityDomainDialog()"><Plus />{{ t('applicationsPage.addDomain') }}</Button></div>
                <EmptyState v-if="!facilityDraft.domains.length" :title="t('applicationsPage.noDomains')" :description="t('applicationsPage.noDomainsHint')" />
                <div v-for="(domain, domainIndex) in facilityDraft.domains" :key="`${domain.domain}-${domainIndex}`" class="facility-domain-card">
                  <div class="facility-domain-header">
                    <div class="min-w-0">
                      <div class="flex flex-wrap items-center gap-2">
                        <Globe2 class="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
                        <strong class="facility-domain-name">{{ domain.domain || t('applicationsPage.unnamedDomain') }}</strong>
                        <Badge v-if="domain.anyAccess?.enabled" tone="info">{{ t(`applicationsPage.loadBalancingStrategy.${domain.anyAccess?.strategy || 'round_robin'}`) }}</Badge>
                        <Badge :tone="facilityDnsTone(facilityDnsStatus(domain.domain))" :title="facilityDnsError(domain.domain) || undefined">{{ t(`applicationsPage.dnsSync.${facilityDnsStatus(domain.domain) || 'unknown'}`) }}</Badge>
                      </div>
                      <span class="facility-domain-servers">{{ t('applicationsPage.originServers', { count: domain.originServerIds.length }) }} · {{ domain.originServerIds.map((id) => serverDisplayName(id)).join(', ') || t('common.notAvailable') }}</span>
                    </div>
                    <div class="row-actions">
                      <Button size="sm" @click="openFacilityDomainDialog(domainIndex)">{{ t('common.edit') }}</Button>
                      <Button size="sm" variant="danger" @click="removeAt(facilityDraft.domains, domainIndex, 'facility')">{{ t('common.delete') }}</Button>
                    </div>
                  </div>
                  <div class="facility-paths">
                    <div v-for="(path, pathIndex) in domain.paths" :key="pathIndex" class="facility-path-row">
                      <div class="min-w-0">
                        <div class="flex flex-wrap items-center gap-2">
                          <Badge tone="neutral" class="panel-mono">{{ path.path || '/' }}</Badge>
                          <Badge tone="info">{{ path.ruleType ? t(`applicationsPage.routeType.${path.ruleType}`) : t('common.notAvailable') }}</Badge>
                        </div>
                        <span class="facility-path-target">{{ facilityPathTargetLabel(path) }}</span>
                      </div>
                      <div class="row-actions">
                        <Button size="sm" @click="openFacilityPathDialog(domainIndex, pathIndex)">{{ t('common.edit') }}</Button>
                        <Button size="sm" variant="danger" @click="removeAt(domain.paths, pathIndex, 'facility')">{{ t('common.delete') }}</Button>
                      </div>
                    </div>
                    <p v-if="!domain.paths.length" class="facility-paths-empty">{{ t('applicationsPage.noPathsHint') }}</p>
                    <Button size="sm" class="justify-self-start" @click="openFacilityPathDialog(domainIndex)"><Plus />{{ t('common.addPath') }}</Button>
                  </div>
                </div>
              </section>
  
              <section class="workspace-panel">
                <AssetFileManager
                  :items="facilityAssetItems" :adapter="facilityAssetAdapter" :disabled="!facilitySession"
                  :labels="{ title: t('applicationsPage.staticAssets'), hint: t('applicationsPage.assetUploadLimit'), uploadAsset: t('applicationsPage.uploadAsset'), uploadAssetTitle: t('applicationsPage.uploadAssetTitle'), uploadType: t('applicationsPage.uploadType'), uploadTypeText: t('applicationsPage.uploadTypeText'), uploadTypeBinary: t('applicationsPage.uploadTypeBinary'), uploadTypeArchive: t('applicationsPage.uploadTypeArchive'), uploadFile: t('applicationsPage.uploadFile'), uploadArchive: t('applicationsPage.uploadArchive'), operationFailed: t('applicationsPage.operationFailed'), edit: t('common.edit'), replace: t('common.replace'), download: t('common.download'), delete: t('common.delete'), bytes: t('applicationsPage.bytes'), noAssets: t('applicationsPage.noAssets'), noAssetsHint: t('applicationsPage.noAssetsHint'), textTitle: t('applicationsPage.editTextFile'), newTextTitle: t('applicationsPage.newTextFile'), name: t('applicationsPage.assetReferenceName'), nameHint: t('applicationsPage.assetReferenceNameHint'), filename: t('applicationsPage.assetDownloadFilename'), filenameHint: t('applicationsPage.assetDownloadFilenameHint'), language: t('applicationsPage.highlightLanguage'), content: t('applicationsPage.fileContent'), loading: t('applicationsPage.fileLoading'), loadFailed: t('applicationsPage.fileLoadFailed'), cancel: t('common.cancel'), save: t('common.save'), close: t('common.close'), reload: t('applicationsPage.discardTextAndReload'), deleteTitle: t('applicationsPage.confirm.facility-asset-delete.title'), deleteDescription: t('applicationsPage.confirm.facility-asset-delete.description'), confirmDelete: t('common.delete') }"
                />
              </section>
            </div>
          </section>
  
          <aside class="app-editor-summary">
            <section class="rounded-2xl border border-border bg-card p-4">
              <h3>{{ t('applicationsPage.gatewayChangeSummary') }}</h3>
              <div class="mt-3 grid gap-2 text-sm">
                <div><span>{{ t('applicationsPage.gatewayNodes') }}</span><strong>{{ facilityDraft.deploymentServers.length }}</strong></div>
                <div><span>{{ t('applicationsPage.domainGroups') }}</span><strong>{{ facilityDraft.domains.length }}</strong></div>
                <div><span>{{ t('applicationsPage.previewAdded') }}</span><strong>{{ facilityDiff.added }}</strong></div>
                <div><span>{{ t('applicationsPage.previewChanged') }}</span><strong>{{ facilityDiff.changed }}</strong></div>
                <div><span>{{ t('applicationsPage.previewRemoved') }}</span><strong>{{ facilityDiff.removed }}</strong></div>
              </div>
            </section>
            <section class="rounded-2xl border border-border bg-card p-4">
              <h3>{{ t('applicationsPage.diagnostics') }}</h3>
              <div class="mt-3 grid gap-2">
                <div v-for="item in facilityDiagnostics" :key="`${item.code}-${item.field}`" class="rounded-xl border p-3 text-sm" :class="item.severity === 'error' ? 'border-danger-border bg-danger-bg text-danger' : 'border-warning-border bg-warning-bg text-warning'">{{ item.field ? `${item.field}: ` : '' }}{{ item.message }}{{ diagnosticDetailText(item) }}</div>
                <p v-if="!facilityDiagnostics.length" class="m-0 text-sm text-muted-foreground">{{ t('applicationsPage.noDiagnostics') }}</p>
              </div>
            </section>
          </aside>
        </div>
        <template #footer>
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div class="text-sm text-muted-foreground">{{ isDirty ? t('applicationsPage.unsavedChanges') : t('applicationsPage.readyToCommit') }}</div>
            <div class="flex flex-wrap gap-2">
              <Button variant="secondary" :disabled="saving" @click="cancelFacilityEdit">{{ t('common.cancel') }}</Button>
              <Button variant="secondary" :loading="pending === 'preview'" :disabled="!facilitySession || saving" @click="previewFacilityConfig">{{ t('applicationsPage.preview') }}</Button>
              <Button variant="primary" :loading="pending === 'commit'" :disabled="!facilitySession || saving" @click="commitFacilityConfig"><Save />{{ t('applicationsPage.commit') }}</Button>
            </div>
          </div>
        </template>
      </EditorPage>
    </template>
    <template v-else-if="isStorageShareFacility">
      <StorageShareFacility :mode="mode" :servers="servers" />
    </template>
    <div v-else class="grid items-start gap-4 xl:grid-cols-[minmax(0,1fr)_360px]">
      <section class="rounded-2xl border border-border bg-card p-5">
        <EmptyState v-if="!currentFacilitySummary" :title="t('applicationsPage.facilityUnavailable')" :description="t('applicationsPage.facilityUnavailableDescription', { kind: facilityKind })" />
        <div v-else-if="loading && !facility" class="relative grid min-h-64 place-items-center">
          <LoadingOverlay />
        </div>
        <div v-else-if="facility" class="grid gap-4">
          <div class="grid grid-cols-4 gap-3 max-lg:grid-cols-2 max-sm:grid-cols-1">
            <div class="rounded-2xl border border-border bg-muted p-4"><span>{{ t('applicationsPage.gatewayNodes') }}</span><strong>{{ facilityConfigSummary.gateways }}</strong></div>
            <div class="rounded-2xl border border-border bg-muted p-4"><span>{{ t('applicationsPage.gatewayRoutes') }}</span><strong>{{ facilityConfigSummary.routes }}</strong></div>
            <div class="rounded-2xl border border-border bg-muted p-4"><span>{{ t('applicationsPage.staticAssets') }}</span><strong>{{ facilityConfigSummary.assets }}</strong></div>
            <div class="rounded-2xl border border-border bg-muted p-4"><span>{{ t('applicationsPage.applicationRoutes') }}</span><strong>{{ facilityConfigSummary.appRoutes }}</strong></div>
          </div>
          <section class="rounded-2xl border border-border bg-muted p-4">
            <h3>{{ t('applicationsPage.routeSummaries') }}</h3>
            <div class="mt-3 grid gap-2 motion-stagger">
              <div v-for="summary in facility.routeSummaries" :key="`${summary.domain}-${summary.path}-${summary.source}`" class="motion-reveal grid gap-1 rounded-xl border border-border p-3 text-sm">
                <div class="flex items-center justify-between"><strong>{{ summary.domain }}{{ summary.path }}</strong><StatusBadge :status="summary.httpsStatus" :tone="summary.httpsStatus === 'disabled' ? 'warning' : 'success'" /></div>
                <span class="text-muted-foreground">{{ summary.source }} / {{ summary.serverIds.map((id) => serverDisplayName(id)).join(', ') }}</span>
              </div>
              <EmptyState v-if="!facility.routeSummaries.length" :title="t('applicationsPage.noGatewayRoutes')" :description="t('applicationsPage.noGatewayRoutesHint')" />
            </div>
          </section>
          <section class="rounded-2xl border border-border bg-muted p-4">
            <h3>{{ t('applicationsPage.dnsSyncTitle') }}</h3>
            <div class="mt-3 grid gap-2 motion-stagger">
              <div v-for="domain in facility.domains" :key="`dns-${domain.domain}`" class="motion-reveal flex items-center justify-between gap-3 rounded-xl border border-border p-3 text-sm">
                <strong>{{ domain.domain }}</strong>
                <Badge :tone="facilityDnsTone(facilityDnsStatus(domain.domain))" :title="facilityDnsError(domain.domain) || undefined">{{ t(`applicationsPage.dnsSync.${facilityDnsStatus(domain.domain) || 'unknown'}`) }}</Badge>
              </div>
              <EmptyState v-if="!facility.domains.length" :title="t('applicationsPage.noDomains')" :description="t('applicationsPage.noDomainsHint')" />
            </div>
          </section>
          <section class="rounded-2xl border border-border bg-muted p-4">
            <h3>{{ t('applicationsPage.staticAssets') }}</h3>
            <div class="mt-3 grid gap-2 motion-stagger">
              <div v-for="asset in facility.staticAssets" :key="asset.name" class="item-row motion-reveal">
                <div><strong>{{ asset.name }}</strong><span>{{ asset.filename }} · {{ asset.size }} {{ t('applicationsPage.bytes') }}</span></div>
                <DownloadButton size="sm" :loading="assetActionPending === `committed:${asset.name}`" :label="t('common.download')" @click="downloadCommittedFacilityAsset(asset)" />
              </div>
              <EmptyState v-if="!facility.staticAssets.length" :title="t('applicationsPage.noAssets')" :description="t('applicationsPage.noAssetsHint')" />
            </div>
          </section>
        </div>
      </section>
      <aside class="grid content-start gap-3 rounded-2xl border border-border bg-card p-5">
        <h3>{{ t('applicationsPage.gatewayDetails') }}</h3>
        <div v-if="facility" class="grid gap-3 text-sm">
          <div><span>{{ t('applicationsPage.lastUpdated') }}</span><strong>{{ formatDateTime(facility.updatedAt) || t('common.never') }}</strong></div>
          <div v-if="facility.operation"><span>{{ t('applicationsPage.currentOperation') }}</span><StatusBadge :status="facility.operation.status" domain="operation" /></div>
          <div v-if="facility.reconcileStopped"><StatusBadge :status="'needs_attention'" domain="operation" :label="t('applicationsPage.status.attention')" /></div>
          <div v-if="facility.lastError" class="rounded-xl border border-danger-border bg-danger-bg p-3 text-danger">{{ facility.lastError }}</div>
        </div>
      </aside>
    </div>
  </ConsolePage>

  <ConsolePage v-else-if="isAppEditor" :back-label="t('common.back')" @back="router.push('/applications/apps')" :title="isCreateMode ? t('applicationsPage.createApplication') : t('applicationsPage.applicationEditor')" :description="t('applicationsPage.applicationEditorDescription')">
    <EditorPage>
      <div v-if="!editSession" class="relative grid min-h-64 place-items-center">
        <LoadingOverlay v-if="editorLoading || !actionError" />
        <div v-else class="grid max-w-md justify-items-center gap-3 text-center">
          <AlertTriangle class="size-6 text-danger" aria-hidden="true" />
          <p class="m-0 text-sm text-danger">{{ actionError }}</p>
          <Button size="sm" :loading="pending === 'editor-reload'" @click="reloadApplicationEditor">{{ t('common.retry') }}</Button>
        </div>
      </div>
      <div v-if="saving" class="mb-4 rounded-xl border border-info-border bg-info-bg p-3 text-sm text-info">{{ t(`applicationsPage.saveStage.${saveStage}`) }}</div>
      <div v-if="editSession && actionError" class="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-danger-border bg-danger-bg p-3 text-sm text-danger">
        <span class="text-danger">{{ actionError }}</span>
      </div>
      <div v-if="editSession" class="relative app-editor-layout">
        <LoadingOverlay v-if="saving" :label="t(`applicationsPage.saveStage.${saveStage}`)" />
        <section class="app-editor-shell">
          <div class="app-editor-header">
            <div class="app-editor-title-row">
              <p class="editor-flow-hint">{{ t('applicationsPage.editorFlowHint') }}</p>
            </div>
          </div>

          <div class="app-editor-body">
            <section class="workspace-panel">
              <div class="section-copy"><h3>{{ t('applicationsPage.panelIdentity') }}</h3><p>{{ isCreateMode ? t('applicationsPage.createFastPathHint') : t('applicationsPage.editRuntimeHint') }}</p></div>
              <div class="form-grid">
                <label class="field">{{ t('common.name') }}<Input v-model="appDraft.name" :invalid="Boolean(appErrors.name)" @input="markAppStructuredDirty" /></label>
                <label class="field">{{ t('applicationsPage.enabled') }}<Switch v-model="appDraft.enabled" :label="t('applicationsPage.enabled')" @click="markAppStructuredDirty" /></label>
              </div>
            </section>

            <section class="workspace-panel">
              <div class="section-copy"><h3>{{ t('applicationsPage.panelRuntimeSource') }}</h3><p>{{ t('applicationsPage.sourceHint') }}</p></div>
              <div class="form-grid">
                <label class="field wide-field">{{ t('applicationsPage.image') }}<Input v-model="appDraft.image" :invalid="Boolean(appErrors.image)" @input="markAppStructuredDirty" /></label>
                <label class="field">{{ t('applicationsPage.cpu') }}<Input v-model="appDraft.cpu" placeholder="0.5" :invalid="Boolean(appErrors.cpu)" @input="markAppStructuredDirty" /></label>
                <label class="field">{{ t('applicationsPage.memoryMb') }}<Input v-model="appDraft.memoryMb" placeholder="512" :invalid="Boolean(appErrors.memoryMb)" @input="markAppStructuredDirty" /></label>
                <div class="field wide-field">
                  <span>{{ t('applicationsPage.command') }}</span>
                  <div class="grid gap-2">
                    <div v-for="row in appDraft.commandRows" :key="row.id" class="header-row">
                      <Input v-model="row.value" :placeholder="t('applicationsPage.commandArgument')" @input="markAppStructuredDirty" />
                      <Button size="sm" variant="danger" class="shrink-0" @click="removeCommandRow(row.id)"><Trash2 /></Button>
                    </div>
                    <Button size="sm" class="justify-self-start" @click="addCommandRow"><Plus />{{ t('common.add') }}</Button>
                  </div>
                </div>
                <label class="switch-field wide-field">{{ t('applicationsPage.privileged') }}<Switch v-model="appDraft.privileged" :label="t('applicationsPage.privileged')" @click="markAppStructuredDirty" /></label>
              </div>
            </section>

            <section class="workspace-panel">
              <div class="section-heading"><div class="section-copy"><h3>{{ t('applicationsPage.panelNetworking') }}</h3><p>{{ t('applicationsPage.networkingHint') }}</p></div><div class="flex flex-wrap gap-2"><Button size="sm" @click="openPortDialog()"><Plus />{{ t('applicationsPage.addPort') }}</Button><Button size="sm" @click="openProxyDialog()"><Globe2 />{{ t('applicationsPage.addProxyRule') }}</Button></div></div>
              <div class="grid gap-3">
                <div v-for="(port, index) in appDraft.ports" :key="port.id" class="item-row"><div><strong>{{ port.label || t('applicationsPage.unnamedPort') }}</strong><span>{{ t('applicationsPage.containerPortSummary', { port: port.to }) }} · {{ port.staticPort ? t('applicationsPage.staticPort', { port: port.staticPort }) : t('applicationsPage.dynamicPort') }}</span></div><div class="row-actions"><Button size="sm" @click="openPortDialog(index)">{{ t('common.edit') }}</Button><Button size="sm" variant="danger" @click="removeAt(appDraft.ports, index)">{{ t('common.delete') }}</Button></div></div>
                <div v-for="(rule, index) in appDraft.reverseProxy" :key="index" class="item-row"><div><strong>{{ rule.domain || t('applicationsPage.unnamedDomain') }}</strong><span>{{ t('applicationsPage.routeTargetSummary', { port: rule.targetPort, paths: rule.paths.map((path) => path.path).join(', ') }) }}</span></div><div class="row-actions"><Button size="sm" @click="openProxyDialog(index)">{{ t('common.edit') }}</Button><Button size="sm" variant="danger" @click="removeAt(appDraft.reverseProxy, index)">{{ t('common.delete') }}</Button></div></div>
                <EmptyState v-if="!appDraft.ports.length && !appDraft.reverseProxy.length" :title="t('applicationsPage.noRoutes')" :description="t('applicationsPage.networkingEmptyHint')" />
              </div>
            </section>

            <section class="workspace-panel">
              <div class="section-heading"><div class="section-copy"><h3>{{ t('applicationsPage.panelStorage') }}</h3><p>{{ t('applicationsPage.storageHint') }}</p></div><Button size="sm" @click="openMountDialog()"><Plus />{{ t('applicationsPage.addMount') }}</Button></div>
              <div class="grid gap-3">
                <div class="flex items-center justify-between gap-3"><strong>{{ t('applicationsPage.containerEnv') }}</strong><Button size="sm" @click="openRowDialog()"><Plus />{{ t('common.add') }}</Button></div>
                <div v-for="(row, index) in appDraft.env" :key="row.id" class="item-row"><div><strong>{{ row.key }}</strong><span>{{ row.value || t('common.empty') }}</span></div><div class="row-actions"><Button size="sm" @click="openRowDialog(index)">{{ t('common.edit') }}</Button><Button size="sm" variant="danger" @click="removeRow(index)">{{ t('common.delete') }}</Button></div></div>
                <div v-for="(mount, index) in appDraft.mounts" :key="mount.id" class="item-row"><div><strong>{{ t('applicationsPage.mountSummary', { type: mount.type, target: mount.target }) }}</strong><span>{{ mountSourceLabel(mount) }}</span></div><div class="row-actions"><Button size="sm" @click="openMountDialog(index)">{{ t('common.edit') }}</Button><Button size="sm" variant="danger" @click="removeAt(appDraft.mounts, index)">{{ t('common.delete') }}</Button></div></div>
                <EmptyState v-if="!appDraft.env.length && !appDraft.mounts.length" :title="t('applicationsPage.noStorageConfig')" :description="t('applicationsPage.noStorageConfigHint')" />
              </div>
            </section>

            <section class="workspace-panel">
              <div class="section-copy"><h3>{{ t('applicationsPage.panelDeployment') }}</h3><p>{{ t('applicationsPage.deployHint') }}</p></div>
              <label class="field">{{ t('applicationsPage.deploymentMode') }}<Select v-model="appDraft.deploymentMode" :options="[{ label: t('applicationsPage.allServers'), value: 'all' }, { label: t('applicationsPage.selectedServers'), value: 'selected' }]" @change="markAppStructuredDirty" /></label>
              <div v-if="appDraft.deploymentMode === 'selected'" class="server-picker-grid">
                <ServerMultiPicker v-model="appDeploymentServersModel" :servers="serverOptions" :disabled-ids="deploymentDisabledIds" :disabled-reasons="deploymentDisabledReasons" :label="t('applicationsPage.deploymentServers')" />
              </div>
            </section>

            <section class="workspace-panel">
              <AssetFileManager
                :items="applicationAssetItems" :adapter="applicationAssetAdapter" :disabled="!editSession" :show-filename="false"
                :language-options="fileLanguageOptions" :infer-language="inferTemplateLanguage"
                :labels="{ title: t('applicationsPage.panelFilesAssets'), hint: t('applicationsPage.filesHint'), uploadAsset: t('applicationsPage.uploadAsset'), uploadAssetTitle: t('applicationsPage.uploadAssetTitle'), uploadType: t('applicationsPage.uploadType'), uploadTypeText: t('applicationsPage.uploadTypeText'), uploadTypeBinary: t('applicationsPage.uploadTypeBinary'), uploadTypeArchive: t('applicationsPage.uploadTypeArchive'), uploadFile: t('applicationsPage.uploadFile'), uploadArchive: t('applicationsPage.uploadFolderArchive'), operationFailed: t('applicationsPage.operationFailed'), edit: t('common.edit'), replace: t('common.replace'), download: t('common.download'), delete: t('common.delete'), bytes: t('applicationsPage.bytes'), noAssets: t('applicationsPage.noFiles'), noAssetsHint: t('applicationsPage.noFilesHint'), textTitle: t('applicationsPage.editTextFile'), newTextTitle: t('applicationsPage.newTextFile'), name: t('applicationsPage.fileName'), filename: t('applicationsPage.assetDownloadFilename'), language: t('applicationsPage.highlightLanguage'), content: t('applicationsPage.fileContent'), loading: t('applicationsPage.fileLoading'), loadFailed: t('applicationsPage.fileLoadFailed'), cancel: t('common.cancel'), save: t('common.save'), close: t('common.close'), reload: t('applicationsPage.discardTextAndReload'), deleteTitle: t('applicationsPage.confirm.file-delete.title'), deleteDescription: t('applicationsPage.confirm.file-delete.description'), confirmDelete: t('common.delete') }"
              />
            </section>
          </div>

        </section>

        <aside class="app-editor-summary">
          <section class="rounded-2xl border border-border bg-card p-4">
            <h3>{{ isCreateMode ? t('applicationsPage.createSummary') : t('applicationsPage.changeSummary') }}</h3>
            <div class="mt-3 grid gap-2 text-sm">
              <div><span>{{ t('applicationsPage.configuration') }}</span><strong>{{ sourceSummary() }}</strong></div>
              <div><span>{{ t('applicationsPage.routes') }}</span><strong>{{ appDraft.reverseProxy.length }}</strong></div>
              <div><span>{{ t('applicationsPage.mounts') }}</span><strong>{{ appDraft.mounts.length }}</strong></div>
              <div><span>{{ t('applicationsPage.previewAdded') }}</span><strong>{{ appDiff.added }}</strong></div>
              <div><span>{{ t('applicationsPage.previewChanged') }}</span><strong>{{ appDiff.changed }}</strong></div>
              <Badge :tone="isDirty ? 'warning' : 'success'">{{ isDirty ? t('applicationsPage.dirty') : t('applicationsPage.clean') }}</Badge>
            </div>
          </section>
          <section class="rounded-2xl border border-border bg-card p-4">
            <h3>{{ t('applicationsPage.diagnostics') }}</h3>
            <div class="mt-3 grid gap-2">
              <div v-for="item in diagnostics" :key="`${item.code}-${item.field}`" class="rounded-xl border p-3 text-sm" :class="item.severity === 'error' ? 'border-danger-border bg-danger-bg text-danger' : 'border-warning-border bg-warning-bg text-warning'">{{ item.field ? `${item.field}: ` : '' }}{{ item.message }}{{ diagnosticDetailText(item) }}</div>
              <p v-if="!diagnostics.length" class="m-0 text-sm text-muted-foreground">{{ t('applicationsPage.noDiagnostics') }}</p>
            </div>
          </section>
        </aside>
      </div>


      <template #footer>
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div class="text-sm text-muted-foreground">{{ isDirty ? t('applicationsPage.unsavedChanges') : t('applicationsPage.readyToCommit') }}</div>
          <div class="flex flex-wrap gap-2">
            <Button variant="secondary" :disabled="saving" @click="router.back()">{{ t('common.cancel') }}</Button>
            <Button variant="secondary" :loading="pending === 'preview'" :disabled="!editSession || saving" @click="previewApplication()">{{ t('applicationsPage.preview') }}</Button>
            <Button variant="primary" :loading="pending === 'commit'" :disabled="!editSession || saving" @click="commitApplication()"><Save />{{ t('applicationsPage.commit') }}</Button>
          </div>
        </div>
      </template>
    </EditorPage>
  </ConsolePage>

  <Dialog v-model:open="logsOpen" :title="t('applicationsPage.logs')" :close-label="t('common.close')">
    <div class="relative">
      <pre class="max-h-[520px] overflow-auto whitespace-pre-wrap rounded-xl border border-border bg-muted p-3 text-xs text-foreground">{{ logsText }}</pre>
      <LoadingOverlay v-if="logsLoading" :label="t('applicationsPage.logsLoading')" />
    </div>
  </Dialog>

  <Dialog v-model:open="dialogOpen" :title="t(`applicationsPage.dialog.${dialogKind}`)" :close-label="t('common.close')">
    <div v-if="dialogKind === 'env'" class="grid gap-3">
      <label class="field">{{ t('common.name') }}<Input v-model="rowDraft.key" /></label>
      <label class="field">{{ t('common.value') }}<Input v-model="rowDraft.value" /></label>
    </div>
    <div v-else-if="dialogKind === 'port'" class="grid gap-3">
      <label class="field">{{ t('common.name') }}<Input v-model="portDraft.label" /></label>
      <label class="field">{{ t('applicationsPage.containerPort') }}<Input v-model="portDraft.to" /></label>
      <label class="field">{{ t('applicationsPage.hostPort') }}<Input v-model="portDraft.staticPort" /></label>
    </div>
    <div v-else-if="dialogKind === 'mount'" class="grid gap-3">
      <label class="field">{{ t('common.type') }}<Select v-model="mountDraft.type" :options="mountTypeOptions" /></label>
      <label class="field">{{ t('applicationsPage.source') }}<Select v-if="mountDraft.type === 'storage_share'" v-model="mountDraft.source" :options="storageShareOptions" :placeholder="t('applicationsPage.storageShareMountSourcePlaceholder')" :disabled="!storageShareAvailable" /><Input v-else v-model="mountDraft.source" /></label>
      <p v-if="mountDraft.type === 'storage_share'" class="m-0 text-xs text-muted-foreground">{{ t('applicationsPage.storageShareMountHint') }}</p>
      <p v-if="mountDraft.type === 'storage_share' && !storageShareAvailable" class="m-0 text-xs text-warning">{{ t('applicationsPage.storageShareMountUnconfigured') }} <Button size="sm" variant="ghost" @click="router.push('/applications/facility-apps/storage-share/config')">{{ t('applicationsPage.storageShareGoConfigure') }}</Button></p>
      <label class="field">{{ t('applicationsPage.target') }}<Input v-model="mountDraft.target" /></label>
      <label v-if="mountDraft.type !== 'storage_share'" class="field">{{ t('applicationsPage.mode') }}<Input v-model="mountDraft.mode" placeholder="0755" /></label>
      <label class="flex items-center justify-between rounded-xl border border-border p-3 text-sm">{{ t('applicationsPage.readOnly') }}<Switch v-model="mountDraft.readOnly" :label="t('applicationsPage.readOnly')" /></label>
    </div>
    <div v-else-if="dialogKind === 'proxy'" class="grid gap-3">
      <label class="field">{{ t('applicationsPage.domain') }}<Input v-model="proxyDraft.domain" /></label>
      <label class="field">{{ t('applicationsPage.targetPort') }}<Input v-model="proxyDraft.targetPort" /></label>
      <div class="options-block">
        <div class="section-copy"><h3>{{ t('applicationsPage.anyAccess') }}</h3><p>{{ t('applicationsPage.anyAccessHint') }}</p></div>
        <label class="switch-field">{{ t('applicationsPage.anyAccess') }}<Switch v-model="proxyAnyAccessModel" :label="t('applicationsPage.anyAccess')" /></label>
        <div v-if="proxyDraft.anyAccess.enabled" class="grid gap-3">
          <label class="field">{{ t('applicationsPage.anyAccessRelayScope') }}<Select v-model="proxyRelayScope" :options="anyAccessRelayScopeOptions" /></label>
          <ServerMultiPicker v-if="proxyRelayScope === 'selected'" v-model="proxyRelayServerIdsModel" :servers="proxyRelayServerOptions" :label="t('applicationsPage.anyAccessRelayServers', { count: proxyDraft.anyAccess.relayServerIds?.length ?? 0 })" />
          <p v-if="proxyRelayScope === 'selected' && !proxyRelayServerOptions.length" class="m-0 text-sm text-muted-foreground">{{ t('applicationsPage.anyAccessNoRelayServers') }}</p>
          <label class="field">{{ t('applicationsPage.loadBalancingStrategy') }}<Select v-model="proxyDraft.anyAccess.strategy" :options="anyAccessStrategyOptions" /></label>
          <div v-if="proxyDraft.anyAccess.strategy === 'primary_backup'" class="options-block">
            <div class="section-copy"><h3>{{ t('applicationsPage.originPriority') }}</h3><p>{{ t('applicationsPage.originPriorityHint') }}</p></div>
            <div v-for="(serverId, index) in proxyOriginPriorityModel" :key="serverId" class="item-row">
              <div><strong>{{ index + 1 }}. {{ serverDisplayName(serverId) }}</strong><span v-if="index === 0" class="text-xs text-muted-foreground">{{ t('applicationsPage.primaryOriginServer') }}</span></div>
              <div class="row-actions">
                <Button size="sm" :disabled="index === 0" @click="moveProxyOriginPriority(index, -1)">{{ t('applicationsPage.moveUp') }}</Button>
                <Button size="sm" :disabled="index === proxyOriginPriorityModel.length - 1" @click="moveProxyOriginPriority(index, 1)">{{ t('applicationsPage.moveDown') }}</Button>
              </div>
            </div>
            <p v-if="!proxyOriginPriorityModel.length" class="m-0 text-sm text-danger">{{ t('applicationsPage.proxyOriginEmptyHint') }}</p>
          </div>
        </div>
      </div>
      <div class="section-heading"><strong>{{ t('common.path') }}</strong><Button size="sm" @click="openProxyPathDialog()"><Plus />{{ t('common.addPath') }}</Button></div>
      <div v-for="(path, index) in proxyDraft.paths" :key="`${path.path}-${index}`" class="item-row"><div><strong>{{ path.path }}</strong><span>WebSocket: {{ path.webSocket ? 'on' : 'off' }}</span></div><div class="row-actions"><Button size="sm" @click="openProxyPathDialog(index)">{{ t('common.edit') }}</Button><Button size="sm" variant="danger" @click="proxyDraft.paths.splice(index, 1)">{{ t('common.delete') }}</Button></div></div>
    </div>
    <div v-else-if="dialogKind === 'proxyPath'" class="grid gap-3">
      <label class="field">{{ t('common.path') }}<Input v-model="proxyPathDraft.path" /></label>
      <label class="field">{{ t('applicationsPage.webSocket') }}<Switch v-model="proxyPathWebSocket" :label="t('applicationsPage.webSocket')" /></label>
      <div class="options-block">
        <div class="section-copy"><h3>{{ t('applicationsPage.advancedOptions') }}</h3><p>{{ t('applicationsPage.advancedOptionsHint') }}</p></div>
        <label class="field">{{ t('applicationsPage.gzipMode') }}<Select v-model="proxyGzipMode" :options="httpModeOptions" /></label>
        <div class="form-grid">
          <label class="field">{{ t('applicationsPage.clientMaxBodySizeMb') }}<Input v-model="proxyClientMaxBodySizeMb" type="number" min="0" /></label>
          <label class="field">{{ t('applicationsPage.connectTimeoutSeconds') }}<Input v-model="proxyConnectTimeoutSeconds" type="number" min="0" /></label>
          <label class="field">{{ t('applicationsPage.readTimeoutSeconds') }}<Input v-model="proxyReadTimeoutSeconds" type="number" min="0" /></label>
          <label class="field">{{ t('applicationsPage.sendTimeoutSeconds') }}<Input v-model="proxySendTimeoutSeconds" type="number" min="0" /></label>
          <label class="field">{{ t('applicationsPage.bufferingMode') }}<Select v-model="proxyBufferingMode" :options="httpModeOptions" /></label>
          <label class="field">{{ t('applicationsPage.webSocketMode') }}<Select v-model="proxyWebSocketMode" :options="webSocketModeOptions" /></label>
        </div>
        <div class="options-subheading">{{ t('applicationsPage.requestHeaders') }}</div>
        <div v-for="(row, index) in proxyRequestHeaders" :key="row.id" class="header-row">
          <Input v-model="row.key" :placeholder="t('applicationsPage.headerName')" />
          <Input v-model="row.value" :placeholder="t('applicationsPage.headerValue')" />
          <Button size="sm" variant="danger" class="shrink-0" @click="proxyRequestHeaders.splice(index, 1)"><Trash2 /></Button>
        </div>
        <Button size="sm" class="justify-self-start" @click="proxyRequestHeaders.push(makeKeyValueRow())"><Plus />{{ t('common.add') }}</Button>
        <div class="options-subheading">{{ t('applicationsPage.responseHeaders') }}</div>
        <div v-for="(row, index) in proxyResponseHeaders" :key="row.id" class="header-row">
          <Input v-model="row.key" :placeholder="t('applicationsPage.headerName')" />
          <Input v-model="row.value" :placeholder="t('applicationsPage.headerValue')" />
          <Button size="sm" variant="danger" class="shrink-0" @click="proxyResponseHeaders.splice(index, 1)"><Trash2 /></Button>
        </div>
        <Button size="sm" class="justify-self-start" @click="proxyResponseHeaders.push(makeKeyValueRow())"><Plus />{{ t('common.add') }}</Button>
      </div>
    </div>
    <div v-else-if="dialogKind === 'facilityDomain'" class="grid gap-3">
      <label class="field">{{ t('applicationsPage.domain') }}<Input v-model="facilityDomainDraft.domain" :invalid="Boolean(facilityDomainErrors.domain)" @input="clearFacilityDomainError('domain')" /></label>
      <p v-if="facilityDomainErrors.domain" class="m-0 text-sm text-danger">{{ t(facilityDomainErrors.domain) }}</p>
      <ServerMultiPicker v-model="facilityDomainOriginServersModel" :servers="facilityDomainOriginServerOptions" :label="t('applicationsPage.originServers', { count: facilityDomainDraft.originServerIds.length })" @update:model-value="clearFacilityDomainError('originServers')" />
      <p v-if="facilityDomainErrors.originServers" class="m-0 text-sm text-danger">{{ t(facilityDomainErrors.originServers) }}</p>
      <div class="options-block">
        <div class="section-copy"><h3>{{ t('applicationsPage.anyAccess') }}</h3><p>{{ t('applicationsPage.anyAccessHint') }}</p></div>
        <label class="switch-field">{{ t('applicationsPage.anyAccess') }}<Switch v-model="facilityDomainDraft.anyAccess.enabled" :label="t('applicationsPage.anyAccess')" /></label>
        <div v-if="facilityDomainDraft.anyAccess.enabled" class="grid gap-3">
          <label class="field">{{ t('applicationsPage.anyAccessRelayScope') }}<Select v-model="facilityRelayScope" :options="anyAccessRelayScopeOptions" /></label>
          <ServerMultiPicker v-if="facilityRelayScope === 'selected'" v-model="facilityRelayServerIdsModel" :servers="facilityRelayServerOptions" :label="t('applicationsPage.anyAccessRelayServers', { count: facilityDomainDraft.anyAccess.relayServerIds?.length ?? 0 })" />
          <p v-if="facilityRelayScope === 'selected' && !facilityRelayServerOptions.length" class="m-0 text-sm text-muted-foreground">{{ t('applicationsPage.anyAccessNoRelayServers') }}</p>
          <label class="field">{{ t('applicationsPage.loadBalancingStrategy') }}<Select v-model="facilityDomainDraft.anyAccess.strategy" :options="anyAccessStrategyOptions" /></label>
          <label v-if="facilityDomainDraft.anyAccess.strategy === 'primary_backup'" class="field">{{ t('applicationsPage.primaryOriginServer') }}<Select v-model="facilityDomainDraft.anyAccess.primaryOriginServerId" :options="facilityDomainOriginOptions" /></label>
        </div>
      </div>
    </div>
    <div v-else-if="dialogKind === 'facilityPath'" class="grid gap-3">
      <label class="field">{{ t('common.path') }}<Input v-model="facilityPathDraft.path" :invalid="Boolean(facilityPathErrors.path)" @input="clearFacilityPathError('path')" /></label>
      <p v-if="facilityPathErrors.path" class="m-0 text-sm text-danger">{{ t(facilityPathErrors.path) }}</p>
      <label class="field">{{ t('common.type') }}<Select v-model="facilityPathDraft.ruleType" :options="routeTypeOptions" /></label>
      <template v-if="facilityPathDraft.ruleType === 'static'">
        <label class="field">{{ t('applicationsPage.sourceType') }}<Select v-model="facilityPathDraft.sourceType" :options="sourceTypeOptions" /></label>
        <label class="field">{{ t('applicationsPage.staticAsset') }}<Select v-model="facilityPathDraft.assetName" :options="assetOptions" @change="clearFacilityPathError('asset')" /></label>
        <p v-if="facilityPathErrors.asset" class="m-0 text-sm text-danger">{{ t(facilityPathErrors.asset) }}</p>
      </template>
      <template v-else-if="facilityPathDraft.ruleType === 'redirect'">
        <label class="field">{{ t('applicationsPage.redirectUrl') }}<Input v-model="facilityPathDraft.redirectUrl" :invalid="Boolean(facilityPathErrors.redirectUrl)" @input="clearFacilityPathError('redirectUrl')" /></label>
        <p v-if="facilityPathErrors.redirectUrl" class="m-0 text-sm text-danger">{{ t(facilityPathErrors.redirectUrl) }}</p>
        <label class="field">{{ t('applicationsPage.redirectCode') }}<Select v-model="facilityRedirectCode" :options="['301', '302', '307', '308'].map((value) => ({ label: value, value }))" /></label>
      </template>
      <template v-else>
        <label class="field">{{ t('applicationsPage.proxyUrl') }}<Input v-model="facilityPathDraft.proxyUrl" :invalid="Boolean(facilityPathErrors.proxyUrl)" @input="clearFacilityPathError('proxyUrl')" /></label>
        <p v-if="facilityPathErrors.proxyUrl" class="m-0 text-sm text-danger">{{ t(facilityPathErrors.proxyUrl) }}</p>
        <label class="field">{{ t('applicationsPage.proxySourceMode') }}<Select v-model="facilityPathDraft.proxySourceMode" :options="proxySourceModeOptions" /></label>
      </template>

      <div class="options-block">
        <div class="section-copy"><h3>{{ t('applicationsPage.advancedOptions') }}</h3><p>{{ t('applicationsPage.advancedOptionsHint') }}</p></div>
        <label v-if="facilityPathDraft.ruleType !== 'redirect'" class="field">{{ t('applicationsPage.gzipMode') }}<Select v-model="facilityGzipMode" :options="httpModeOptions" /></label>
        <template v-if="facilityPathDraft.ruleType === 'proxy_pass'">
          <div class="form-grid">
            <label class="field">{{ t('applicationsPage.clientMaxBodySizeMb') }}<Input v-model="facilityClientMaxBodySizeMb" type="number" min="0" /></label>
            <label class="field">{{ t('applicationsPage.connectTimeoutSeconds') }}<Input v-model="facilityConnectTimeoutSeconds" type="number" min="0" /></label>
            <label class="field">{{ t('applicationsPage.readTimeoutSeconds') }}<Input v-model="facilityReadTimeoutSeconds" type="number" min="0" /></label>
            <label class="field">{{ t('applicationsPage.sendTimeoutSeconds') }}<Input v-model="facilitySendTimeoutSeconds" type="number" min="0" /></label>
            <label class="field">{{ t('applicationsPage.bufferingMode') }}<Select v-model="facilityBufferingMode" :options="httpModeOptions" /></label>
            <label class="field">{{ t('applicationsPage.webSocketMode') }}<Select v-model="facilityWebSocketMode" :options="webSocketModeOptions" /></label>
          </div>
          <div class="options-subheading">{{ t('applicationsPage.requestHeaders') }}</div>
          <div v-for="(row, index) in facilityRequestHeaders" :key="row.id" class="header-row">
            <Input v-model="row.key" :placeholder="t('applicationsPage.headerName')" />
            <Input v-model="row.value" :placeholder="t('applicationsPage.headerValue')" />
            <Button size="sm" variant="danger" class="shrink-0" @click="facilityRequestHeaders.splice(index, 1)"><Trash2 /></Button>
          </div>
          <Button size="sm" class="justify-self-start" @click="facilityRequestHeaders.push(makeKeyValueRow())"><Plus />{{ t('common.add') }}</Button>
        </template>
        <div class="options-subheading">{{ t('applicationsPage.responseHeaders') }}</div>
        <div v-for="(row, index) in facilityResponseHeaders" :key="row.id" class="header-row">
          <Input v-model="row.key" :placeholder="t('applicationsPage.headerName')" />
          <Input v-model="row.value" :placeholder="t('applicationsPage.headerValue')" />
          <Button size="sm" variant="danger" class="shrink-0" @click="facilityResponseHeaders.splice(index, 1)"><Trash2 /></Button>
        </div>
        <Button size="sm" class="justify-self-start" @click="facilityResponseHeaders.push(makeKeyValueRow())"><Plus />{{ t('common.add') }}</Button>
      </div>
    </div>
    <template #footer>
      <Button variant="secondary" @click="dialogOpen = false">{{ t('common.cancel') }}</Button>
      <Button v-if="dialogKind === 'env'" variant="primary" @click="saveRowDialog">{{ t('common.save') }}</Button>
      <Button v-else-if="dialogKind === 'port'" variant="primary" @click="savePortDialog">{{ t('common.save') }}</Button>
      <Button v-else-if="dialogKind === 'mount'" variant="primary" @click="saveMountDialog">{{ t('common.save') }}</Button>
      <Button v-else-if="dialogKind === 'proxy'" variant="primary" @click="saveProxyDialog">{{ t('common.save') }}</Button>
      <Button v-else-if="dialogKind === 'proxyPath'" variant="primary" @click="saveProxyPathDialog">{{ t('common.save') }}</Button>
      <Button v-else-if="dialogKind === 'facilityDomain'" variant="primary" @click="saveFacilityDomainDialog">{{ t('common.save') }}</Button>
      <Button v-else-if="dialogKind === 'facilityPath'" variant="primary" @click="saveFacilityPathDialog">{{ t('common.save') }}</Button>
    </template>
  </Dialog>

  <Dialog v-model:open="confirmOpen" :title="t(`applicationsPage.confirm.${confirmKind}.title`)" :description="t(`applicationsPage.confirm.${confirmKind}.description`)" :close-label="t('common.close')">
    <div class="flex gap-3 rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning"><AlertTriangle class="size-4 shrink-0" />{{ t('applicationsPage.confirmImpact') }}</div>
    <template #footer>
      <Button variant="secondary" @click="confirmOpen = false">{{ t('common.cancel') }}</Button>
            <Button variant="danger" :loading="Boolean(pending)" @click="confirmAction">{{ t('common.confirm') }}</Button>
    </template>
  </Dialog>

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

<style scoped>
h3 {
  margin: 0;
  color: var(--panel-text);
  font-size: 14px;
  font-weight: 650;
}

span {
  color: var(--panel-text-muted);
  font-size: 12px;
}

strong {
  display: block;
  color: var(--panel-text);
  overflow-wrap: anywhere;
}

.field {
  display: grid;
  gap: 0.35rem;
  color: var(--panel-text);
  font-size: 0.875rem;
}

.section-copy {
  display: grid;
  gap: 0.25rem;
  min-width: 0;
}

.section-copy p {
  margin: 0;
  color: var(--panel-text-muted);
  font-size: 0.8125rem;
  line-height: 1.5;
}

.section-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 0.75rem;
}

.editor-flow-hint {
  margin: 0;
  align-self: center;
  color: var(--panel-text-muted);
  font-size: 0.8125rem;
  line-height: 1.5;
}

.app-editor-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) clamp(272px, 23vw, 320px);
  gap: 1rem;
  height: 100%;
  min-height: 0;
  min-width: 0;
}

.app-editor-shell {
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
  min-width: 0;
  min-height: 0;
  overflow: hidden;
  border: 1px solid var(--panel-border);
  border-radius: 1rem;
  background: var(--panel-surface);
}

.app-editor-header {
  display: grid;
  gap: 1rem;
  min-width: 0;
  border-bottom: 1px solid var(--panel-border);
  background: var(--panel-surface);
  padding: 1rem 1.125rem;
}

.app-editor-title-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: start;
  gap: 1rem;
  min-width: 0;
}

.app-editor-body {
  display: grid;
  align-content: start;
  gap: 1.25rem;
  min-width: 0;
  min-height: 0;
  overflow: auto;
  padding: 1.25rem;
}

.editor-flow-hint {
  margin: 0;
  align-self: center;
  color: var(--panel-text-muted);
  font-size: 0.8125rem;
  line-height: 1.5;
}

.app-editor-summary {
  position: sticky;
  top: 0;
  display: grid;
  align-content: start;
  gap: 0.75rem;
  min-width: 0;
  align-self: start;
}

.workspace-panel {
  display: grid;
  gap: 1rem;
  min-width: 0;
  width: 100%;
  max-width: 70rem;
  border: 1px solid var(--panel-border);
  border-radius: 1rem;
  background: var(--panel-muted);
  padding: 1rem;
  animation: panel-motion-enter var(--panel-motion-duration-slow) var(--panel-motion-ease-emphasized) both;
}

.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.75rem;
  width: 100%;
  max-width: 58rem;
  min-width: 0;
}

.wide-field {
  grid-column: 1 / -1;
}

.switch-field {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  min-width: 0;
  border: 1px solid var(--panel-border);
  border-radius: 0.75rem;
  padding: 0.75rem;
  color: var(--panel-text);
  font-size: 0.875rem;
}

.server-picker-grid {
  width: 100%;
  max-width: 58rem;
}

.server-picker-grid :deep(section) {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.75rem;
}

.item-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 0.75rem;
  align-items: center;
  min-width: 0;
  border: 1px solid var(--panel-border);
  border-radius: 0.875rem;
  background: var(--panel-surface);
  padding: 0.75rem;
  transition:
    background-color var(--panel-motion-duration-base) var(--panel-motion-ease-standard),
    border-color var(--panel-motion-duration-base) var(--panel-motion-ease-standard),
    box-shadow var(--panel-motion-duration-base) var(--panel-motion-ease-standard),
    transform var(--panel-motion-duration-base) var(--panel-motion-ease-standard);
}

.item-row:hover {
  border-color: color-mix(in srgb, var(--panel-border) 92%, transparent);
  background: color-mix(in srgb, var(--panel-muted) 34%, transparent);
  transform: translateY(var(--panel-motion-hover-y));
  box-shadow: var(--panel-motion-shadow-raised);
}

.row-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  justify-content: flex-end;
}

.row-error {
  grid-column: 1 / -1;
  min-width: 0;
  border: 1px solid var(--panel-danger-border);
  border-radius: 0.5rem;
  background: var(--panel-danger-bg);
  color: var(--panel-danger);
  padding: 0.5rem 0.625rem;
  font-size: 0.75rem;
  overflow-wrap: anywhere;
}


.facility-domain-card {
  display: grid;
  gap: 0.875rem;
  min-width: 0;
  border: 1px solid var(--panel-border);
  border-radius: 0.875rem;
  background: var(--panel-surface);
  padding: 0.875rem;
}

.facility-domain-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 0.75rem;
  min-width: 0;
}

.facility-domain-name {
  display: inline;
  color: var(--panel-text);
  font-size: 0.9375rem;
  font-weight: 700;
  overflow-wrap: anywhere;
}

.facility-domain-servers {
  display: block;
  margin-top: 0.25rem;
  color: var(--panel-text-muted);
  font-size: 0.8125rem;
  line-height: 1.5;
  overflow-wrap: anywhere;
}

.facility-paths {
  display: grid;
  gap: 0.5rem;
  min-width: 0;
  padding-top: 0.875rem;
  border-top: 1px dashed var(--panel-border);
}

.facility-path-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 0.5rem 0.75rem;
  align-items: center;
  min-width: 0;
  border: 1px solid var(--panel-border);
  border-radius: 0.75rem;
  background: var(--panel-surface);
  padding: 0.625rem 0.75rem;
}

.facility-path-target {
  display: block;
  margin-top: 0.25rem;
  color: var(--panel-text-muted);
  font-size: 0.8125rem;
  line-height: 1.5;
  overflow-wrap: anywhere;
}

.facility-paths-empty {
  margin: 0;
  border: 1px dashed var(--panel-border);
  border-radius: 0.625rem;
  padding: 0.5rem 0.75rem;
  color: var(--panel-text-muted);
  font-size: 0.8125rem;
  line-height: 1.5;
}

.options-block {
  display: grid;
  gap: 0.75rem;
  min-width: 0;
  border: 1px solid var(--panel-border);
  border-radius: 0.875rem;
  background: color-mix(in srgb, var(--panel-muted) 30%, transparent);
  padding: 0.875rem;
}

.options-subheading {
  margin: 0;
  color: var(--panel-text);
  font-size: 0.8125rem;
  font-weight: 650;
}

.header-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto;
  gap: 0.5rem;
  align-items: center;
  min-width: 0;
}

.facility-card-stats {
  display: flex;
  flex-wrap: wrap;
  gap: 0.75rem 1.25rem;
}

.facility-card-stat {
  display: grid;
  gap: 0.125rem;
  min-width: 0;
}

.facility-card-stat-truncate { display: block; max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.facility-card-stat strong {
  display: block;
  color: var(--panel-text);
  font-size: 1.125rem;
  font-weight: 700;
  line-height: 1.25;
}

.facility-card-stat span {
  color: var(--panel-text-muted);
  font-size: 0.75rem;
  line-height: 1.4;
}

@media (max-width: 640px) {
  .facility-path-row {
    grid-template-columns: 1fr;
    align-items: start;
  }

  .facility-path-row .row-actions {
    justify-content: flex-start;
  }
}

@media (max-width: 760px) {
  .item-row {
    grid-template-columns: 1fr;
    align-items: start;
  }

  .item-row .row-actions {
    justify-content: flex-start;
  }

  .section-heading {
    align-items: stretch;
    flex-direction: column;
  }
}

@media (max-width: 1279px) {
  .app-editor-layout {
    grid-template-columns: minmax(0, 1fr);
    height: auto;
  }

  .app-editor-title-row {
    grid-template-columns: 1fr;
    gap: 0.75rem;
  }

  .app-editor-summary {
    position: static;
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 1023px) {
  .app-editor-shell {
    overflow: visible;
  }

  .app-editor-body {
    overflow: visible;
  }
}

@media (max-width: 900px) {
  .form-grid {
    grid-template-columns: 1fr;
  }

  .server-picker-grid :deep(section) {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 640px) {
  .app-editor-title-row,
  .app-editor-summary {
    grid-template-columns: 1fr;
  }

  .app-editor-body {
    padding: 0.875rem;
  }

  .workspace-panel {
    padding: 0.875rem;
  }
}

@media (max-width: 420px) {
  .form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
