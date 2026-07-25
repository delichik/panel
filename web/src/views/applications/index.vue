<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router';
import { AlertTriangle, Boxes, ClipboardList, Code2, Globe2, HardDrive, History, Layers3, Network, PackageCheck, Plus, RefreshCw, RefreshCcw, Rocket, Save, Server, Square, Trash2, UploadCloud, Wrench } from '@lucide/vue';
import { applicationsApi } from '@/api/applications';
import { saveBlobDownload } from '@/api/download';
import { facilityAppsApi } from '@/api/facilityApps';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import Dialog from '@/components/ui/Dialog.vue';
import DownloadButton from '@/components/ui/DownloadButton.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import FileUploadButton from '@/components/ui/FileUploadButton.vue';
import Input from '@/components/ui/Input.vue';
import SearchInput from '@/components/ui/SearchInput.vue';
import Select from '@/components/ui/Select.vue';
import StatusBadge from '@/components/ui/StatusBadge.vue';
import Switch from '@/components/ui/Switch.vue';
import Tabs from '@/components/ui/Tabs.vue';
import Textarea from '@/components/ui/Textarea.vue';
import EditorSectionRail from '@/components/patterns/EditorSectionRail.vue';
import ServerMultiPicker from '@/components/patterns/ServerMultiPicker.vue';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import EditorPage from '@/components/templates/EditorPage.vue';
import { useI18n } from '@/i18n';
import type { ApplicationDto, ApplicationEditPreviewResult, ApplicationEditSession, ApplicationFile, ApplicationRuntime, Diagnostic, ReverseProxyRule } from '@/types/applications';
import type { FacilityAppSummary, FacilityEditPreviewResult, FacilityEditSession, FacilityRouteDomain, FacilityRoutePath, ReverseProxyConfig } from '@/types/facilityApps';
import {
  applicationSections,
  applicationStatus,
  applyYamlToDraft,
  cloneFacilityDomains,
  cloneFacilityPath,
  cloneProxyRules,
  defaultRouteOptions,
  diffApplications,
  diffFacility,
  draftFromApplication,
  facilityDraftFromConfig,
  facilitySaveInputFromDraft,
  facilitySections,
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
  specYamlFromDraft,
  statusTone,
  syncDraftToYaml,
  validateApplicationDraft,
  validateFacilityDraft,
  type ApplicationDraftUi,
  type FacilityDraftUi,
  type KeyValueRow,
  type MountRow,
  type PortRow,
  type SaveStage,
} from './model';

const { t } = useI18n();
const route = useRoute();
const router = useRouter();

const applications = ref<ApplicationDto[]>([]);
const runtimes = ref<Record<string, ApplicationRuntime>>({});
const facilities = ref<FacilityAppSummary[]>([]);
const facility = ref<ReverseProxyConfig | null>(null);
const selectedId = ref(String(route.query.application ?? ''));
const search = ref(String(route.query.search ?? ''));
const loading = ref(false);
const detailLoading = ref(false);
const error = ref('');
const feedback = ref('');
const actionError = ref('');
const pending = ref('');
const logsOpen = ref(false);
const logsText = ref('');
const confirmOpen = ref(false);
const confirmKind = ref<'delete' | 'stop' | 'file-delete' | 'facility-asset-delete'>('delete');
const confirmTarget = ref('');
const appTab = ref('runtime');
const editorMode = ref<'configure' | 'source'>('configure');
const activeAppSection = ref('identity');
const activeFacilitySection = ref('gateways');
const fileDialog = ref(false);
const fileEditing = ref<ApplicationFile | null>(null);
const diagnostics = ref<Diagnostic[]>([]);
const preview = ref<ApplicationEditPreviewResult | null>(null);
const editSession = ref<ApplicationEditSession | null>(null);
const facilitySession = ref<FacilityEditSession | null>(null);
const facilityPreview = ref<FacilityEditPreviewResult | null>(null);
const facilityDiagnostics = ref<Diagnostic[]>([]);
const isDirty = ref(false);
const saveStage = ref<SaveStage>('idle');
const dialogOpen = ref(false);
const dialogKind = ref<'variable' | 'env' | 'port' | 'mount' | 'proxy' | 'proxyPath' | 'facilityDomain' | 'facilityPath'>('variable');
const dialogIndex = ref(-1);
const dialogParentIndex = ref(-1);
const rowDraft = reactive<KeyValueRow>(makeKeyValueRow());
const portDraft = reactive<PortRow>(makePortRow());
const mountDraft = reactive<MountRow>(makeMountRow());
const proxyDraft = reactive<ReverseProxyRule>(makeProxyRule());
const proxyPathDraft = reactive(makeProxyPath());
const facilityDomainDraft = reactive<FacilityRouteDomain>(makeFacilityDomain());
const facilityPathDraft = reactive<FacilityRoutePath>(makeFacilityPath());
const fileForm = reactive({ path: '', kind: 'template', contentType: 'text/plain', content: '' });

const mode = computed(() => routeMode(route.path, route.params));
const facilityKind = computed(() => String(route.params.facilityKind ?? ''));
const applicationFamilyTab = computed({
  get: () => (mode.value === 'facilityCatalog' || mode.value === 'facilityDetail' || mode.value === 'facilityConfig' ? 'facility' : 'apps'),
  set: (value: string) => { void router.push(value === 'facility' ? '/applications/facility-apps' : '/applications/apps'); },
});
const isAppEditor = computed(() => mode.value === 'create' || mode.value === 'edit');
const isCreateMode = computed(() => mode.value === 'create');
const currentFacilitySummary = computed(() => facilities.value.find((item) => item.kind === facilityKind.value) ?? null);
const isFacilityEditor = computed(() => mode.value === 'facilityConfig' && Boolean(currentFacilitySummary.value));
const isUnsupportedFacilityRoute = computed(() => (mode.value === 'facilityDetail' || mode.value === 'facilityConfig') && !currentFacilitySummary.value);
const currentApplication = computed(() => applications.value.find((item) => item.id === selectedId.value) ?? null);
const selectedApplication = computed(() => currentApplication.value ?? emptyApplication());
const currentRuntime = computed(() => selectedId.value ? runtimes.value[selectedId.value] : null);
const appStatus = computed(() => currentApplication.value ? applicationStatus(currentApplication.value, currentRuntime.value) : 'unknown');
const appDraft = reactive<ApplicationDraftUi>(draftFromApplication());
const facilityDraft = reactive<FacilityDraftUi>(facilityDraftFromConfig());
const appErrors = computed(() => validateApplicationDraft(appDraft));
const facilityErrors = computed(() => validateFacilityDraft(facilityDraft));
const appSectionStates = computed(() => applicationSections(appDraft, appErrors.value));
const facilitySectionStates = computed(() => facilitySections(facilityDraft, facilityErrors.value));
const appDiff = computed(() => diffApplications(mode.value === 'edit' ? currentApplication.value : null, appDraft));
const facilityDiff = computed(() => diffFacility(facility.value, facilityDraft));
const hasBlockingAppDiagnostics = computed(() => hasBlockingDiagnostic(diagnostics.value));
const hasBlockingFacilityDiagnostics = computed(() => hasBlockingDiagnostic(facilityDiagnostics.value));
const appRows = computed(() => filteredApplications.value.map((app) => ({ app, routes: routeSummary(app), summary: runtimeSummary(runtimes.value[app.id]) })));
const filteredApplications = computed(() => {
  const term = search.value.trim().toLowerCase();
  if (!term) return applications.value;
  return applications.value.filter((app) => [app.name, app.imageReference, app.namespace, app.runtimeStatus].some((value) => String(value ?? '').toLowerCase().includes(term)));
});
const facilityConfigSummary = computed(() => ({
  gateways: currentFacilitySummary.value?.metrics.deploymentServers ?? facility.value?.deploymentServers.length ?? 0,
  routes: currentFacilitySummary.value?.metrics.routes ?? facility.value?.routes ?? 0,
  assets: currentFacilitySummary.value?.metrics.staticAssets ?? facility.value?.staticAssets.length ?? 0,
  appRoutes: currentFacilitySummary.value?.metrics.applicationRoutes ?? facility.value?.applicationRoutes.length ?? 0,
}));
const appWorkspacePanels = computed(() => [
  { id: 'identity', label: t('applicationsPage.panelIdentity'), description: t('applicationsPage.panelIdentityHint'), icon: PackageCheck },
  { id: 'runtime', label: t('applicationsPage.panelRuntimeSource'), description: t('applicationsPage.panelRuntimeSourceHint'), icon: Boxes },
  { id: 'networking', label: t('applicationsPage.panelNetworking'), description: t('applicationsPage.panelNetworkingHint'), icon: Network },
  { id: 'storage', label: t('applicationsPage.panelStorage'), description: t('applicationsPage.panelStorageHint'), icon: HardDrive },
  { id: 'deployment', label: t('applicationsPage.panelDeployment'), description: t('applicationsPage.panelDeploymentHint'), icon: Server },
  { id: 'files', label: t('applicationsPage.panelFilesAssets'), description: t('applicationsPage.panelFilesAssetsHint'), icon: Layers3 },
]);
const facilitySectionNav = computed(() => [
  { id: 'gateways', label: t('applicationsPage.gatewayNodes') },
  { id: 'domains', label: t('applicationsPage.domainGroups') },
  { id: 'panel', label: t('applicationsPage.panelEntry') },
  { id: 'assets', label: t('applicationsPage.staticAssets') },
]);
const appRailSections = computed(() => appWorkspacePanels.value.map((section) => ({
  ...section,
  complete: false,
  error: false,
  dirty: false,
  ...(appSectionStates.value.find((item) => item.id === section.id) ?? {}),
})));
const facilityRailSections = computed(() => facilitySectionNav.value.map((section) => ({
  ...section,
  ...(facilitySectionStates.value.find((item) => item.id === section.id) ?? { complete: false }),
  children: section.id === 'domains'
    ? facilityDraft.domains.map((domain, index) => ({
      id: `${domain.domain || 'domain'}-${index}`,
      label: domain.domain || t('applicationsPage.unnamedDomain'),
      status: `${domain.paths.length} ${t('common.path')}`,
    }))
    : undefined,
})));
const serverOptions = computed(() => {
  const ids = new Set<string>();
  applications.value.forEach((app) => app.deploymentServers.forEach((id) => ids.add(id)));
  facility.value?.deploymentServers.forEach((id) => ids.add(id));
  facility.value?.enabledServers.forEach((id) => ids.add(id));
  return Array.from(ids).map((id) => ({ id, label: id, name: id, description: t('applicationsPage.serverOptionDescription', { id }) }));
});
const assetOptions = computed(() => [
  ...(facility.value?.staticAssets ?? []).map((asset) => ({ value: asset.id, label: `${asset.name} / ${asset.filename}` })),
  ...(facilitySession.value?.assets ?? []).map((asset) => ({ value: asset.assetKey, label: `${asset.name} / ${asset.filename}` })),
]);
const fileKindOptions = computed(() => [{ label: t('applicationsPage.fileKindTemplate'), value: 'template' }, { label: t('applicationsPage.fileKindBinary'), value: 'binary' }]);
const mountTypeOptions = computed(() => ['persistent', 'volume', 'host', 'file', 'panel_file'].map((value) => ({ label: t(`applicationsPage.mountType.${value}`), value })));
const routeTypeOptions = computed(() => ['static', 'redirect', 'proxy_pass'].map((value) => ({ label: t(`applicationsPage.routeType.${value}`), value })));
const sourceTypeOptions = computed(() => ['host_path', 'uploaded_file', 'uploaded_bundle'].map((value) => ({ label: t(`applicationsPage.sourceType.${value}`), value })));
const saving = computed(() => pending.value === 'validate' || pending.value === 'preview' || pending.value === 'commit');
const proxyPathWebSocket = computed({
  get: () => Boolean(proxyPathDraft.webSocket),
  set: (value: boolean) => { proxyPathDraft.webSocket = value; },
});
const facilityRedirectCode = computed({
  get: () => String(facilityPathDraft.redirectCode ?? 302),
  set: (value: string) => { facilityPathDraft.redirectCode = Number(value); },
});
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
    facilityDraft.deploymentServers = value;
    markDirty();
  },
});
const proxyOriginServersModel = computed({
  get: () => proxyDraft.originServerIds,
  set: (value: string[]) => {
    proxyDraft.originServerIds = value;
    markAppStructuredDirty();
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
});

watch(selectedId, async (value) => {
  if (value) {
    await router.replace({ query: { ...route.query, application: value } });
    await loadRuntime(value);
  }
});

watch(() => route.path, async () => {
  actionError.value = '';
  feedback.value = '';
  if (isAppEditor.value) await startApplicationEditor();
  if (isFacilityEditor.value) await startFacilityEditor();
});

onBeforeRouteLeave(() => {
  if (saving.value) return false;
  if (!isDirty.value) return true;
  return window.confirm(t('applicationsPage.leaveDirty'));
});

function handleBeforeUnload(event: BeforeUnloadEvent) {
  if (!isDirty.value && !saving.value) return;
  event.preventDefault();
  event.returnValue = '';
}

async function load() {
  loading.value = true;
  error.value = '';
  try {
    const [apps, facilityList] = await Promise.all([
      applicationsApi.list(),
      facilityAppsApi.listFacilities(),
    ]);
    const primaryFacilityKind = facilityList.find((item) => item.kind === facilityKind.value)?.kind ?? facilityList[0]?.kind;
    const primaryFacility = primaryFacilityKind ? await facilityAppsApi.getFacility(primaryFacilityKind) : null;
    applications.value = apps;
    facilities.value = facilityList;
    facility.value = primaryFacility?.reverseProxy ?? null;
    selectedId.value = apps.some((item) => item.id === selectedId.value) ? selectedId.value : apps[0]?.id ?? '';
    await Promise.all(apps.slice(0, 4).map((app) => loadRuntime(app.id).catch(() => undefined)));
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('applicationsPage.loadFailed');
  } finally {
    loading.value = false;
  }
}

async function loadRuntime(applicationId: string) {
  if (!applicationId) return;
  detailLoading.value = true;
  try {
    runtimes.value = { ...runtimes.value, [applicationId]: await applicationsApi.runtime(applicationId) };
  } catch (err) {
    actionError.value = err instanceof Error ? err.message : t('applicationsPage.runtimeUnavailable');
  } finally {
    detailLoading.value = false;
  }
}

async function runOperation(name: string, action: () => Promise<unknown>, successKey: string) {
  pending.value = name;
  actionError.value = '';
  try {
    const result = await action();
    feedback.value = t(successKey, taskParams(result));
    await load();
  } catch (err) {
    actionError.value = err instanceof Error ? err.message : t('common.operationFailed');
  } finally {
    pending.value = '';
  }
}

function taskParams(result: unknown) {
  const record = result as { taskId?: string; deploymentId?: string; evalId?: string };
  return { taskId: record?.taskId || record?.deploymentId || record?.evalId || t('common.notAvailable') };
}

async function showLogs(app: ApplicationDto) {
  logsOpen.value = true;
  logsText.value = t('applicationsPage.logsLoading');
  try {
    logsText.value = (await applicationsApi.logs(app.id, { tail: 240 })).logs;
  } catch (err) {
    logsText.value = err instanceof Error ? err.message : t('applicationsPage.logsFailed');
  }
}

function ask(kind: typeof confirmKind.value, target: string) {
  confirmKind.value = kind;
  confirmTarget.value = target;
  confirmOpen.value = true;
}

async function confirmAction() {
  if (confirmKind.value === 'file-delete' && editSession.value) {
    await runEditorAction(async () => {
      editSession.value = await applicationsApi.deleteEditSessionFile(editSession.value!.id, confirmTarget.value, editSession.value!.revision);
      markDirty();
    });
    confirmOpen.value = false;
    return;
  }
  if (confirmKind.value === 'facility-asset-delete' && facilitySession.value) {
    await runEditorAction(async () => {
      facilitySession.value = await facilityAppsApi.deleteFacilityEditAsset(facilityKind.value, facilitySession.value!.id, confirmTarget.value, facilitySession.value!.revision);
      markDirty();
    });
    confirmOpen.value = false;
    return;
  }
  const app = currentApplication.value;
  if (!app) return;
  if (confirmKind.value === 'delete') {
    await runOperation('delete', () => applicationsApi.delete(app.id), 'applicationsPage.deleted');
  } else {
    await runOperation('stop', () => applicationsApi.stop(app.id), 'applicationsPage.stopAccepted');
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
  if (!file || !currentApplication.value) return;
  await runOperation('persistent-restore', () => applicationsApi.restorePersistentData(currentApplication.value!.id, file), 'applicationsPage.restoreAccepted');
}

async function startApplicationEditor() {
  const appId = String(route.params.applicationId ?? '');
  await ensureApplicationsLoaded();
  const app = appId ? applications.value.find((item) => item.id === appId) : null;
  Object.assign(appDraft, draftFromApplication(app));
  diagnostics.value = [];
  preview.value = null;
  editSession.value = null;
  isDirty.value = false;
  activeAppSection.value = isCreateMode.value ? 'identity' : 'runtime';
  editorMode.value = 'configure';
  try {
    const recovered = await applicationsApi.recoverableEditSessions(appId || undefined);
    editSession.value = recovered[0] ?? await applicationsApi.beginEditSession(appId || undefined, saveInputFromDraft(appDraft));
    Object.assign(appDraft, draftFromApplication({ ...(app ?? emptyApplication()), ...editSession.value.draft }));
  } catch (err) {
    actionError.value = err instanceof Error ? err.message : t('applicationsPage.editorStartFailed');
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

async function validateApplication() {
  await patchApplicationDraft();
  if (!editSession.value) return;
  await runEditorAction(async () => {
    const result = await applicationsApi.validateEditSession(editSession.value!.id, editSession.value!.revision);
    diagnostics.value = result.diagnostics;
    feedback.value = result.valid ? t('applicationsPage.validationPassed') : t('applicationsPage.validationFound');
  }, 'validate');
}

async function previewApplication() {
  await patchApplicationDraft();
  if (!editSession.value) return;
  await runEditorAction(async () => {
    preview.value = await applicationsApi.previewEditSession(editSession.value!.id, editSession.value!.revision);
    diagnostics.value = preview.value.diagnostics;
  }, 'preview');
}

async function commitApplication() {
  await previewApplication();
  if (!editSession.value || !preview.value || hasBlockingAppDiagnostics.value) return;
  await runEditorAction(async () => {
    const result = await applicationsApi.commitEditSession(editSession.value!, preview.value!);
    feedback.value = result.applyRequested ? t('applicationsPage.committedAndApplied') : t('applicationsPage.committed');
    isDirty.value = false;
    await router.push({ path: '/applications/apps', query: { application: result.application.id } });
    await load();
  }, 'commit');
}

async function startFacilityEditor() {
  await ensureFacilityLoaded();
  if (!currentFacilitySummary.value) return;
  Object.assign(facilityDraft, facilityDraftFromConfig(facility.value));
  facilityDiagnostics.value = [];
  facilityPreview.value = null;
  facilitySession.value = null;
  isDirty.value = false;
  activeFacilitySection.value = 'gateways';
  try {
    const recovered = await facilityAppsApi.recoverableFacilityEditSessions(facilityKind.value);
    facilitySession.value = recovered[0] ?? await facilityAppsApi.beginFacilityEdit(facilityKind.value, facilitySaveInputFromDraft(facilityDraft));
    Object.assign(facilityDraft, facilityDraftFromConfig({ ...(facility.value ?? emptyFacility()), deploymentServers: facilitySession.value.draft.deploymentServers, panelEntry: facilitySession.value.draft.panelEntry, domains: facilitySession.value.draft.domains }));
  } catch (err) {
    actionError.value = err instanceof Error ? err.message : t('applicationsPage.editorStartFailed');
  }
}

async function patchFacilityDraft() {
  if (!facilitySession.value || Object.keys(facilityErrors.value).length) return;
  await runEditorAction(async () => {
    facilitySession.value = await facilityAppsApi.patchFacilityEdit(facilityKind.value, facilitySession.value!.id, facilitySession.value!.revision, facilitySession.value!.baseResourceVersion.value, facilitySaveInputFromDraft(facilityDraft));
    facilityPreview.value = null;
    isDirty.value = false;
  });
}

async function validateFacility() {
  await patchFacilityDraft();
  if (!facilitySession.value) return;
  await runEditorAction(async () => {
    const result = await facilityAppsApi.validateFacilityEdit(facilityKind.value, facilitySession.value!.id, facilitySession.value!.revision);
    facilityDiagnostics.value = result.diagnostics;
    feedback.value = result.valid ? t('applicationsPage.validationPassed') : t('applicationsPage.validationFound');
  }, 'validate');
}

async function previewFacilityConfig() {
  await patchFacilityDraft();
  if (!facilitySession.value) return;
  await runEditorAction(async () => {
    facilityPreview.value = await facilityAppsApi.previewFacilityEdit(facilityKind.value, facilitySession.value!.id, facilitySession.value!.revision);
    facilityDiagnostics.value = facilityPreview.value.diagnostics;
  }, 'preview');
}

async function commitFacilityConfig() {
  await previewFacilityConfig();
  if (!facilitySession.value || !facilityPreview.value || hasBlockingFacilityDiagnostics.value) return;
  await runEditorAction(async () => {
    const result = await facilityAppsApi.commitFacilityEdit(facilityKind.value, facilitySession.value!, facilityPreview.value!);
    facility.value = result.config;
    feedback.value = result.applyRequested ? t('applicationsPage.gatewayCommittedAndApplied') : t('applicationsPage.gatewayCommitted');
    isDirty.value = false;
    await router.push(`/applications/facility-apps/${facilityKind.value}`);
    await load();
  }, 'commit');
}

async function runEditorAction(action: () => Promise<void>, name = 'editor') {
  pending.value = name;
  saveStage.value = name as SaveStage;
  actionError.value = '';
  try {
    await action();
  } catch (err) {
    actionError.value = err instanceof Error ? err.message : t('common.operationFailed');
  } finally {
    pending.value = '';
    saveStage.value = 'idle';
  }
}

async function ensureApplicationsLoaded() {
  if (!applications.value.length) await load();
}

async function ensureFacilityLoaded() {
  if (!facilities.value.length || !facility.value) await load();
}

function markDirty() {
  isDirty.value = true;
  preview.value = null;
  facilityPreview.value = null;
}

function markAppStructuredDirty() {
  syncDraftToYaml(appDraft);
  markDirty();
}

function markSpecDirty() {
  appDraft.yamlDirty = true;
  markDirty();
}

function syncYamlFromForm() {
  syncDraftToYaml(appDraft);
  markDirty();
  feedback.value = t('applicationsPage.yamlSynced');
}

function applyYamlToForm() {
  const result = applyYamlToDraft(appDraft);
  if (!result.ok) {
    actionError.value = result.error || t('applicationsPage.validationYaml');
    editorMode.value = 'source';
    return;
  }
  editorMode.value = 'configure';
  activeAppSection.value = 'runtime';
  markDirty();
  feedback.value = t('applicationsPage.yamlApplied');
}

function openRowDialog(kind: 'variable' | 'env', index = -1) {
  dialogKind.value = kind;
  dialogIndex.value = index;
  Object.assign(rowDraft, index >= 0 ? (kind === 'variable' ? appDraft.variables[index] : appDraft.env[index]) : makeKeyValueRow());
  dialogOpen.value = true;
}

function saveRowDialog() {
  const rows = dialogKind.value === 'variable' ? appDraft.variables : appDraft.env;
  const next = { ...rowDraft };
  if (dialogIndex.value >= 0) rows[dialogIndex.value] = next;
  else rows.push(next);
  dialogOpen.value = false;
  markAppStructuredDirty();
}

function removeRow(kind: 'variable' | 'env', index: number) {
  (kind === 'variable' ? appDraft.variables : appDraft.env).splice(index, 1);
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

function openMountDialog(index = -1) {
  dialogKind.value = 'mount';
  dialogIndex.value = index;
  Object.assign(mountDraft, index >= 0 ? appDraft.mounts[index] : makeMountRow());
  dialogOpen.value = true;
}

function saveMountDialog() {
  const next = { ...mountDraft };
  if (dialogIndex.value >= 0) appDraft.mounts[dialogIndex.value] = next;
  else appDraft.mounts.push(next);
  dialogOpen.value = false;
  markAppStructuredDirty();
}

function openProxyDialog(index = -1) {
  dialogKind.value = 'proxy';
  dialogIndex.value = index;
  Object.assign(proxyDraft, index >= 0 ? cloneProxyRules([appDraft.reverseProxy[index]])[0] : makeProxyRule());
  dialogOpen.value = true;
}

function saveProxyDialog() {
  const next = cloneProxyRules([proxyDraft])[0];
  if (dialogIndex.value >= 0) appDraft.reverseProxy[dialogIndex.value] = next;
  else appDraft.reverseProxy.push(next);
  dialogOpen.value = false;
  markAppStructuredDirty();
}

function openProxyPathDialog(index = -1) {
  dialogKind.value = 'proxyPath';
  dialogParentIndex.value = dialogIndex.value;
  dialogIndex.value = index;
  Object.assign(proxyPathDraft, index >= 0 ? proxyDraft.paths[index] : makeProxyPath());
  dialogOpen.value = true;
}

function saveProxyPathDialog() {
  const next = { ...proxyPathDraft, options: { ...defaultRouteOptions(), ...(proxyPathDraft.options ?? {}) } };
  if (dialogIndex.value >= 0) proxyDraft.paths[dialogIndex.value] = next;
  else proxyDraft.paths.push(next);
  dialogKind.value = 'proxy';
  dialogIndex.value = dialogParentIndex.value;
}

function openFacilityDomainDialog(index = -1) {
  dialogKind.value = 'facilityDomain';
  dialogIndex.value = index;
  Object.assign(facilityDomainDraft, index >= 0 ? cloneFacilityDomains([facilityDraft.domains[index]])[0] : makeFacilityDomain());
  dialogOpen.value = true;
}

function saveFacilityDomainDialog() {
  const next = cloneFacilityDomains([facilityDomainDraft])[0];
  if (dialogIndex.value >= 0) facilityDraft.domains[dialogIndex.value] = next;
  else facilityDraft.domains.push(next);
  dialogOpen.value = false;
  markDirty();
}

function openFacilityPathDialog(domainIndex: number, pathIndex = -1) {
  dialogKind.value = 'facilityPath';
  dialogParentIndex.value = domainIndex;
  dialogIndex.value = pathIndex;
  Object.assign(facilityPathDraft, pathIndex >= 0 ? cloneFacilityPath(facilityDraft.domains[domainIndex].paths[pathIndex]) : makeFacilityPath());
  dialogOpen.value = true;
}

function saveFacilityPathDialog() {
  const paths = facilityDraft.domains[dialogParentIndex.value]?.paths;
  if (!paths) return;
  const next = cloneFacilityPath(facilityPathDraft);
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

function openFileDialog(file?: ApplicationFile) {
  fileEditing.value = file ?? null;
  Object.assign(fileForm, {
    path: file?.path ?? 'config/app.conf',
    kind: file?.kind ?? 'template',
    contentType: file?.contentType ?? 'text/plain',
    content: '',
  });
  fileDialog.value = true;
}

async function saveFile() {
  if (!editSession.value || !fileForm.path.trim() || !fileForm.content.trim()) return;
  await runEditorAction(async () => {
    const key = fileEditing.value?.fileKey || fileEditing.value?.id || `file-${Date.now()}`;
    editSession.value = await applicationsApi.putEditSessionFile(editSession.value!.id, key, editSession.value!.revision, {
      path: fileForm.path,
      kind: fileForm.kind,
      contentType: fileForm.contentType,
      contentBase64: btoa(unescape(encodeURIComponent(fileForm.content))),
    });
    markDirty();
    fileDialog.value = false;
  });
}

async function uploadArchive(fileOrFiles: File | File[]) {
  const file = Array.isArray(fileOrFiles) ? fileOrFiles[0] : fileOrFiles;
  if (!file || !editSession.value) return;
  await runEditorAction(async () => {
    editSession.value = await applicationsApi.uploadEditSessionArchive(editSession.value!.id, editSession.value!.revision, {
      file,
      fileKey: `archive-${Date.now()}`,
      basePath: '/',
      kind: 'archive',
    });
    markDirty();
  });
}

async function uploadFacilityAsset(fileOrFiles: File | File[]) {
  const file = Array.isArray(fileOrFiles) ? fileOrFiles[0] : fileOrFiles;
  if (!file || !facilitySession.value) return;
  await runEditorAction(async () => {
    facilitySession.value = await facilityAppsApi.putFacilityEditAsset(facilityKind.value, facilitySession.value!.id, `asset-${Date.now()}`, facilitySession.value!.revision, {
      file,
      name: file.name.replace(/\.[^.]+$/, '') || file.name,
      kind: file.name.endsWith('.zip') ? 'uploaded_bundle' : 'uploaded_file',
    });
    markDirty();
  });
}

function sourceSummary() {
  if (appDraft.yamlDirty) return t('applicationsPage.yamlDirtySummary');
  return appDraft.image || t('common.notAvailable');
}

function pathTarget(path: FacilityRoutePath) {
  if (path.ruleType === 'redirect') return path.redirectUrl || t('common.notAvailable');
  if (path.ruleType === 'proxy_pass') return path.proxyUrl || t('common.notAvailable');
  return path.sourceType === 'host_path' ? path.rootPath : path.assetId;
}

function emptyApplication(): ApplicationDto {
  return { id: '', version: 0, kind: 'application', name: '', enabled: true, specYaml: '', variables: {}, deploymentMode: 'all', deploymentServers: [], reverseProxy: [], generation: 0, specHash: '', imageUpdateAvailable: false, jobId: '', namespace: '', createdAt: '', updatedAt: '' };
}

function emptyFacility(): ReverseProxyConfig {
  return { id: 'reverse_proxy', version: 0, deploymentServers: [], panelEntry: { enabled: false }, domains: [], staticAssets: [], routeSummaries: [], applicationRoutes: [], updatedAt: '', routes: 0, enabledServers: [] };
}

onMounted(async () => {
  window.addEventListener('beforeunload', handleBeforeUnload);
  await load();
  if (isAppEditor.value) await startApplicationEditor();
  if (isFacilityEditor.value) await startFacilityEditor();
});

onBeforeUnmount(() => {
  window.removeEventListener('beforeunload', handleBeforeUnload);
});
</script>

<template>
  <ConsolePage v-if="mode === 'apps'" :title="t('routes.applications.title')" :description="t('routes.applications.description')">
    <template #actions>
      <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
      <Button size="sm" variant="primary" @click="router.push('/applications/apps/create')"><Plus />{{ t('applicationsPage.createApplication') }}</Button>
    </template>

    <Tabs v-model="applicationFamilyTab" :tabs="[{ label: t('routes.applications.title'), value: 'apps' }, { label: t('routes.facilityApps.title'), value: 'facility' }]">
    <div class="grid h-full min-h-[660px] grid-cols-[370px_minmax(0,1fr)] gap-4 max-xl:grid-cols-1">
      <aside class="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] rounded-2xl border border-border bg-card">
        <div class="border-b border-border p-4">
          <SearchInput v-model="search" clearable :placeholder="t('applicationsPage.searchPlaceholder')" :label="t('common.search')" :clear-label="t('common.clearSearch')" />
        </div>
        <div class="min-h-0 overflow-auto p-2">
          <EmptyState v-if="!appRows.length" :title="t('applicationsPage.emptyApplications')" :description="t('applicationsPage.emptyApplicationsHint')" />
          <button v-for="row in appRows" v-else :key="row.app.id" type="button" class="mb-2 grid w-full gap-2 rounded-xl border p-3 text-left transition-colors hover:bg-accent" :class="selectedId === row.app.id ? 'border-border-strong bg-background' : 'border-transparent bg-transparent'" @click="selectedId = row.app.id">
            <div class="flex items-center justify-between gap-2">
              <strong class="truncate text-sm text-foreground">{{ row.app.name }}</strong>
              <StatusBadge :status="applicationStatus(row.app, runtimes[row.app.id])" :tone="statusTone(applicationStatus(row.app, runtimes[row.app.id]))" :label="t(`applicationsPage.status.${applicationStatus(row.app, runtimes[row.app.id])}`)" />
            </div>
            <span class="truncate text-xs text-muted-foreground">{{ row.app.imageReference || row.app.jobId }}</span>
            <div class="flex flex-wrap gap-1.5">
              <Badge tone="info">{{ t('applicationsPage.instancesCount', { count: row.summary.total }) }}</Badge>
              <Badge :tone="row.app.imageUpdateAvailable ? 'warning' : 'neutral'">{{ row.app.imageUpdateAvailable ? t('applicationsPage.imageUpdate') : t('applicationsPage.imageCurrent') }}</Badge>
            </div>
          </button>
        </div>
      </aside>

      <main class="grid min-h-0">
        <section v-if="error" class="rounded-2xl border border-danger-border bg-danger-bg p-4 text-sm text-danger">{{ error }}</section>
        <EmptyState v-else-if="!currentApplication" :title="t('applicationsPage.selectApplication')" :description="t('applicationsPage.selectApplicationHint')" />
        <article v-else class="grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)] overflow-hidden rounded-2xl border border-border bg-card">
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
              <Button :loading="pending === 'deploy'" @click="runOperation('deploy', () => applicationsApi.deploy(selectedApplication.id), 'applicationsPage.deployAccepted')"><Rocket />{{ t('applicationsPage.sync') }}</Button>
              <Button variant="danger" :disabled="!selectedApplication.enabled" @click="ask('stop', selectedApplication.id)"><Square />{{ t('applicationsPage.disable') }}</Button>
            </div>
          </header>
          <div v-if="actionError" class="border-b border-danger-border bg-danger-bg px-5 py-3 text-sm text-danger">{{ actionError }}</div>
          <div v-if="feedback" class="border-b border-success-border bg-success-bg px-5 py-3 text-sm text-success">{{ feedback }}</div>
          <div class="min-h-0 overflow-auto p-5">
            <div class="grid gap-5 xl:grid-cols-[minmax(0,1fr)_320px]">
              <div class="min-w-0">
                <Tabs v-model="appTab" :tabs="[{ label: t('applicationsPage.runtime'), value: 'runtime' }, { label: t('applicationsPage.routes'), value: 'routes' }, { label: t('applicationsPage.files'), value: 'files' }]">
                  <section v-if="appTab === 'runtime'" class="grid gap-4">
                    <div class="grid grid-cols-3 gap-3 max-md:grid-cols-1">
                      <div class="rounded-2xl border border-border bg-background p-4"><span>{{ t('applicationsPage.instances') }}</span><strong>{{ runtimeSummary(currentRuntime).total }}</strong></div>
                      <div class="rounded-2xl border border-border bg-background p-4"><span>{{ t('applicationsPage.running') }}</span><strong>{{ runtimeSummary(currentRuntime).running }}</strong></div>
                      <div class="rounded-2xl border border-border bg-background p-4"><span>{{ t('applicationsPage.failed') }}</span><strong>{{ runtimeSummary(currentRuntime).failed }}</strong></div>
                    </div>
                    <div class="rounded-2xl border border-border bg-background p-4">
                      <h3>{{ t('applicationsPage.nodeInstances') }}</h3>
                      <div v-if="currentRuntime?.instances?.length" class="mt-3 grid gap-2">
                        <div v-for="instance in currentRuntime.instances" :key="instance.instanceId || instance.id" class="grid gap-1 rounded-xl border border-border p-3 text-sm">
                          <div class="flex items-center justify-between gap-2"><strong>{{ instance.serverName || instance.serverId || instance.instanceId }}</strong><StatusBadge :status="instance.status || instance.state || 'unknown'" :tone="statusTone(instance.status || instance.state || 'unknown')" :label="instance.status || instance.state || t('common.notAvailable')" /></div>
                          <span class="text-muted-foreground">{{ instance.containerName || instance.containerId || t('common.notAvailable') }}</span>
                          <span v-if="instance.error" class="text-danger">{{ instance.error }}</span>
                        </div>
                      </div>
                      <EmptyState v-else :title="t('applicationsPage.noRuntime')" :description="detailLoading ? t('applicationsPage.runtimeLoading') : t('applicationsPage.noRuntimeHint')" />
                    </div>
                  </section>
                  <section v-else-if="appTab === 'routes'" class="grid gap-3">
                    <div v-for="rule in selectedApplication.reverseProxy" :key="rule.domain" class="rounded-2xl border border-border bg-background p-4">
                      <div class="flex items-center justify-between gap-2"><strong>{{ rule.domain }}</strong><Badge tone="info">{{ rule.targetPort }}</Badge></div>
                      <p class="text-sm text-muted-foreground">{{ t('applicationsPage.originServers', { count: rule.originServerIds.length }) }}</p>
                      <div class="mt-2 flex flex-wrap gap-2"><Badge v-for="path in rule.paths" :key="path.path" tone="neutral">{{ path.path }}</Badge></div>
                    </div>
                    <EmptyState v-if="!selectedApplication.reverseProxy.length" :title="t('applicationsPage.noRoutes')" :description="t('applicationsPage.noRoutesHint')" />
                  </section>
                  <section v-else class="rounded-2xl border border-border bg-background p-4">
                    <p class="m-0 text-sm text-muted-foreground">{{ selectedApplication.persistentPath ? t('applicationsPage.persistentPath', { path: selectedApplication.persistentPath }) : t('applicationsPage.noPersistentData') }}</p>
                  </section>
                </Tabs>
              </div>
              <aside class="grid content-start gap-3">
                <section class="rounded-2xl border border-border bg-background p-4">
                  <h3>{{ t('applicationsPage.operations') }}</h3>
                  <div class="mt-3 grid gap-2">
                    <Button :loading="pending === 'image-check'" @click="runOperation('image-check', () => applicationsApi.checkImage(selectedApplication.id), 'applicationsPage.imageChecked')"><RefreshCcw />{{ t('applicationsPage.checkImage') }}</Button>
                    <Button :disabled="!selectedApplication.imageUpdateAvailable" :loading="pending === 'image-update'" @click="runOperation('image-update', () => applicationsApi.updateImage(selectedApplication.id), 'applicationsPage.imageUpdateAccepted')"><UploadCloud />{{ t('applicationsPage.updateImage') }}</Button>
                    <Button @click="router.push({ path: '/application-operations', query: { applicationId: selectedApplication.id } })"><ClipboardList />{{ t('applicationsPage.operationRecords') }}</Button>
                    <Button @click="showLogs(selectedApplication)"><History />{{ t('applicationsPage.logs') }}</Button>
                    <Button variant="danger" @click="ask('delete', selectedApplication.id)"><Trash2 />{{ t('common.delete') }}</Button>
                  </div>
                </section>
                <section class="rounded-2xl border border-border bg-background p-4">
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
    </div>
    </Tabs>
  </ConsolePage>

  <ConsolePage v-else-if="mode === 'facilityCatalog'" :title="t('routes.facilityApps.title')" :description="t('routes.facilityApps.description')">
    <template #actions>
      <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
    </template>
    <Tabs v-model="applicationFamilyTab" :tabs="[{ label: t('routes.applications.title'), value: 'apps' }, { label: t('routes.facilityApps.title'), value: 'facility' }]">
    <div class="grid h-full min-h-[640px] gap-4 xl:grid-cols-[minmax(0,1fr)_340px]">
      <section class="min-h-0 overflow-auto rounded-2xl border border-border bg-card p-5">
        <div class="mb-4">
          <h2 class="m-0 text-lg font-semibold text-foreground">{{ t('applicationsPage.facilityCatalogTitle') }}</h2>
          <p class="m-0 mt-1 text-sm text-muted-foreground">{{ t('applicationsPage.facilityCatalogHint') }}</p>
        </div>
        <div class="grid gap-3 [grid-template-columns:repeat(auto-fit,minmax(min(280px,100%),1fr))]">
          <article v-for="item in facilities" :key="item.kind" class="grid gap-4 rounded-2xl border border-border bg-background p-4">
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0">
                <div class="flex flex-wrap items-center gap-2">
                  <h3 class="m-0 text-base font-semibold text-foreground">{{ t(item.titleKey) }}</h3>
                  <Badge tone="info">{{ t(item.categoryKey) }}</Badge>
                </div>
                <p class="m-0 mt-2 text-sm leading-6 text-muted-foreground">{{ t(item.descriptionKey) }}</p>
              </div>
              <StatusBadge :status="item.status" :tone="item.status === 'degraded' ? 'danger' : 'success'" :label="t(`applicationsPage.facilityStatus.${item.status}`)" />
            </div>
            <div class="grid grid-cols-4 gap-2 text-sm max-lg:grid-cols-2 max-sm:grid-cols-1">
              <div class="rounded-xl border border-border p-3"><span>{{ t('applicationsPage.gatewayNodes') }}</span><strong>{{ item.metrics.deploymentServers }}</strong></div>
              <div class="rounded-xl border border-border p-3"><span>{{ t('applicationsPage.gatewayRoutes') }}</span><strong>{{ item.metrics.routes }}</strong></div>
              <div class="rounded-xl border border-border p-3"><span>{{ t('applicationsPage.staticAssets') }}</span><strong>{{ item.metrics.staticAssets }}</strong></div>
              <div class="rounded-xl border border-border p-3"><span>{{ t('applicationsPage.applicationRoutes') }}</span><strong>{{ item.metrics.applicationRoutes }}</strong></div>
            </div>
            <div class="flex flex-wrap gap-2">
              <Button size="sm" variant="primary" @click="router.push(`/applications/facility-apps/${item.kind}`)">{{ t('common.view') }}</Button>
              <Button size="sm" @click="runOperation(`facility-reconcile-${item.kind}`, () => facilityAppsApi.reconcileFacility(item.kind), 'applicationsPage.gatewayReconcileAccepted')"><Rocket />{{ t('applicationsPage.reconcileGateway') }}</Button>
              <Button size="sm" @click="router.push(`/applications/facility-apps/${item.kind}/config`)"><Wrench />{{ t('common.configure') }}</Button>
            </div>
          </article>
          <EmptyState v-if="!facilities.length" :title="t('applicationsPage.emptyFacilityCatalog')" :description="t('applicationsPage.emptyFacilityCatalogHint')" />
        </div>
      </section>
      <aside class="grid content-start gap-3 rounded-2xl border border-border bg-card p-5">
        <h3>{{ t('applicationsPage.facilityCatalogStatus') }}</h3>
        <div class="grid gap-3 text-sm">
          <div><span>{{ t('applicationsPage.availableFacilities') }}</span><strong>{{ facilities.length }}</strong></div>
          <div><span>{{ t('applicationsPage.panelEntry') }}</span><strong>{{ facility?.panelEntry.enabled ? facility.panelEntry.domain : t('applicationsPage.panelEntryDisabled') }}</strong></div>
          <div v-if="facility?.operation"><span>{{ t('applicationsPage.currentOperation') }}</span><StatusBadge :status="facility.operation.status" domain="operation" /></div>
        </div>
      </aside>
    </div>
    </Tabs>
  </ConsolePage>

  <ConsolePage v-else-if="mode === 'facilityDetail'" :title="currentFacilitySummary ? t(currentFacilitySummary.titleKey) : t('applicationsPage.facilityUnavailable')" :description="currentFacilitySummary ? t(currentFacilitySummary.descriptionKey) : t('applicationsPage.facilityUnavailableDescription', { kind: facilityKind })">
    <template #actions>
      <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
      <Button size="sm" @click="router.push('/applications/facility-apps')">{{ t('routes.facilityApps.title') }}</Button>
      <Button v-if="currentFacilitySummary" size="sm" @click="runOperation(`facility-reconcile-${facilityKind}`, () => facilityAppsApi.reconcileFacility(facilityKind), 'applicationsPage.gatewayReconcileAccepted')"><Rocket />{{ t('applicationsPage.reconcileGateway') }}</Button>
      <Button v-if="currentFacilitySummary" size="sm" variant="primary" @click="router.push(`/applications/facility-apps/${facilityKind}/config`)"><Wrench />{{ t('common.configure') }}</Button>
    </template>
    <Tabs v-model="applicationFamilyTab" :tabs="[{ label: t('routes.applications.title'), value: 'apps' }, { label: t('routes.facilityApps.title'), value: 'facility' }]">
    <div class="grid h-full min-h-[640px] gap-4 xl:grid-cols-[minmax(0,1fr)_340px]">
      <section class="min-h-0 overflow-auto rounded-2xl border border-border bg-card p-5">
        <EmptyState v-if="!currentFacilitySummary" :title="t('applicationsPage.facilityUnavailable')" :description="t('applicationsPage.facilityUnavailableDescription', { kind: facilityKind })" />
        <div v-else-if="facility" class="grid gap-4">
          <div class="grid grid-cols-4 gap-3 max-lg:grid-cols-2 max-sm:grid-cols-1">
            <div class="rounded-2xl border border-border bg-background p-4"><span>{{ t('applicationsPage.gatewayNodes') }}</span><strong>{{ facilityConfigSummary.gateways }}</strong></div>
            <div class="rounded-2xl border border-border bg-background p-4"><span>{{ t('applicationsPage.gatewayRoutes') }}</span><strong>{{ facilityConfigSummary.routes }}</strong></div>
            <div class="rounded-2xl border border-border bg-background p-4"><span>{{ t('applicationsPage.staticAssets') }}</span><strong>{{ facilityConfigSummary.assets }}</strong></div>
            <div class="rounded-2xl border border-border bg-background p-4"><span>{{ t('applicationsPage.applicationRoutes') }}</span><strong>{{ facilityConfigSummary.appRoutes }}</strong></div>
          </div>
          <section class="rounded-2xl border border-border bg-background p-4">
            <h3>{{ t('applicationsPage.routeSummaries') }}</h3>
            <div class="mt-3 grid gap-2">
              <div v-for="summary in facility.routeSummaries" :key="`${summary.domain}-${summary.path}-${summary.source}`" class="grid gap-1 rounded-xl border border-border p-3 text-sm">
                <div class="flex items-center justify-between"><strong>{{ summary.domain }}{{ summary.path }}</strong><StatusBadge :status="summary.httpsStatus" :tone="summary.httpsStatus === 'disabled' ? 'warning' : 'success'" /></div>
                <span class="text-muted-foreground">{{ summary.source }} / {{ summary.serverIds.join(', ') }}</span>
              </div>
              <EmptyState v-if="!facility.routeSummaries.length" :title="t('applicationsPage.noGatewayRoutes')" :description="t('applicationsPage.noGatewayRoutesHint')" />
            </div>
          </section>
        </div>
      </section>
      <aside class="grid content-start gap-3 rounded-2xl border border-border bg-card p-5">
        <h3>{{ t('applicationsPage.gatewayDetails') }}</h3>
        <div v-if="facility" class="grid gap-3 text-sm">
          <div><span>{{ t('applicationsPage.panelEntry') }}</span><strong>{{ facility.panelEntry.enabled ? facility.panelEntry.domain : t('applicationsPage.panelEntryDisabled') }}</strong></div>
          <div><span>{{ t('applicationsPage.lastUpdated') }}</span><strong>{{ facility.updatedAt || t('common.never') }}</strong></div>
          <div v-if="facility.operation"><span>{{ t('applicationsPage.currentOperation') }}</span><StatusBadge :status="facility.operation.status" domain="operation" /></div>
          <div v-if="facility.lastError" class="rounded-xl border border-danger-border bg-danger-bg p-3 text-danger">{{ facility.lastError }}</div>
        </div>
      </aside>
    </div>
    </Tabs>
  </ConsolePage>

  <ConsolePage v-else-if="isUnsupportedFacilityRoute" :title="t('applicationsPage.facilityUnavailable')" :description="t('applicationsPage.facilityUnavailableDescription', { kind: facilityKind })">
    <template #actions>
      <Button size="sm" @click="router.push('/applications/facility-apps')">{{ t('routes.facilityApps.title') }}</Button>
    </template>
    <Tabs v-model="applicationFamilyTab" :tabs="[{ label: t('routes.applications.title'), value: 'apps' }, { label: t('routes.facilityApps.title'), value: 'facility' }]">
      <EmptyState :title="t('applicationsPage.facilityUnavailable')" :description="t('applicationsPage.facilityUnavailableDescription', { kind: facilityKind })" />
    </Tabs>
  </ConsolePage>

  <ConsolePage v-else-if="isAppEditor || isFacilityEditor" :title="isFacilityEditor ? t('applicationsPage.gatewayEditor') : (isCreateMode ? t('applicationsPage.createApplication') : t('applicationsPage.applicationEditor'))" :description="isFacilityEditor ? t('applicationsPage.gatewayEditorDescription') : t('applicationsPage.applicationEditorDescription')">
    <EditorPage>
      <div v-if="saving" class="mb-4 rounded-xl border border-info-border bg-info-bg p-3 text-sm text-info">{{ t(`applicationsPage.saveStage.${saveStage}`) }}</div>
      <div v-if="actionError" class="mb-4 rounded-xl border border-danger-border bg-danger-bg p-3 text-sm text-danger">{{ actionError }}</div>
      <div v-if="feedback" class="mb-4 rounded-xl border border-success-border bg-success-bg p-3 text-sm text-success">{{ feedback }}</div>

      <div v-if="isAppEditor" class="app-editor-layout">
        <section class="app-editor-shell">
          <div class="app-editor-header">
            <div class="app-editor-title-row">
              <div class="section-copy">
                <h3>{{ t('applicationsPage.editorWorkspace') }}</h3>
                <p>{{ t('applicationsPage.editorWorkspaceHint') }}</p>
              </div>
              <div class="mode-switch" :aria-label="t('applicationsPage.applicationEditor')">
                <button class="mode-button" :class="{ active: editorMode === 'configure' }" type="button" @click="editorMode = 'configure'">
                  <Wrench class="size-4" />{{ t('applicationsPage.configureMode') }}
                </button>
                <button class="mode-button" :class="{ active: editorMode === 'source' }" type="button" @click="editorMode = 'source'">
                  <Code2 class="size-4" />{{ t('applicationsPage.sourceMode') }}
                </button>
              </div>
            </div>
            <div class="workspace-steps" role="tablist" :aria-label="t('applicationsPage.applicationEditor')">
              <button v-for="section in appRailSections" :key="section.id" class="workspace-step" :class="{ active: activeAppSection === section.id, error: section.error }" type="button" @click="activeAppSection = section.id; editorMode = 'configure'">
                <component :is="section.icon" class="size-4" />
                <span>{{ section.label }}</span>
                <StatusBadge :status="section.error ? 'error' : section.complete ? 'complete' : 'pending'" :tone="section.error ? 'danger' : section.complete ? 'success' : 'neutral'" :label="section.error ? t('common.error') : section.complete ? t('common.complete') : t('applicationsPage.pending')" />
              </button>
            </div>
          </div>

          <div v-if="editorMode === 'configure'" class="app-editor-body">
            <section v-show="activeAppSection === 'identity'" class="workspace-panel">
              <div class="section-copy"><h3>{{ t('applicationsPage.panelIdentity') }}</h3><p>{{ isCreateMode ? t('applicationsPage.createFastPathHint') : t('applicationsPage.editRuntimeHint') }}</p></div>
              <div class="form-grid">
                <label class="field">{{ t('common.name') }}<Input v-model="appDraft.name" :invalid="Boolean(appErrors.name)" @input="markAppStructuredDirty" /></label>
                <label class="field">{{ t('applicationsPage.enabled') }}<Switch v-model="appDraft.enabled" :label="t('applicationsPage.enabled')" @click="markAppStructuredDirty" /></label>
              </div>
            </section>

            <section v-show="activeAppSection === 'runtime'" class="workspace-panel">
              <div class="section-copy"><h3>{{ t('applicationsPage.panelRuntimeSource') }}</h3><p>{{ t('applicationsPage.sourceHint') }}</p></div>
              <div class="form-grid">
                <label class="field wide-field">{{ t('applicationsPage.image') }}<Input v-model="appDraft.image" :invalid="Boolean(appErrors.image)" @input="markAppStructuredDirty" /></label>
                <label class="field">{{ t('applicationsPage.networkMode') }}<Select v-model="appDraft.networkMode" :options="[{ label: 'bridge', value: 'bridge' }, { label: 'host', value: 'host' }]" @change="markAppStructuredDirty" /></label>
                <label class="field">{{ t('applicationsPage.cpu') }}<Input v-model="appDraft.cpu" placeholder="0.5" @input="markAppStructuredDirty" /></label>
                <label class="field">{{ t('applicationsPage.memoryMb') }}<Input v-model="appDraft.memoryMb" placeholder="512" @input="markAppStructuredDirty" /></label>
                <label class="field wide-field">{{ t('applicationsPage.command') }}<Textarea v-model="appDraft.commandText" :placeholder="t('applicationsPage.commandHint')" @input="markAppStructuredDirty" /></label>
                <label class="switch-field wide-field">{{ t('applicationsPage.privileged') }}<Switch v-model="appDraft.privileged" :label="t('applicationsPage.privileged')" @click="markAppStructuredDirty" /></label>
              </div>
            </section>

            <section v-show="activeAppSection === 'networking'" class="workspace-panel">
              <div class="section-heading"><div class="section-copy"><h3>{{ t('applicationsPage.panelNetworking') }}</h3><p>{{ t('applicationsPage.networkingHint') }}</p></div><div class="flex flex-wrap gap-2"><Button size="sm" @click="openPortDialog()"><Plus />{{ t('applicationsPage.addPort') }}</Button><Button size="sm" @click="openProxyDialog()"><Globe2 />{{ t('applicationsPage.addProxyRule') }}</Button></div></div>
              <div class="grid gap-3">
                <div v-for="(port, index) in appDraft.ports" :key="port.id" class="item-row"><div><strong>{{ port.label || 'port' }} · {{ port.to }}</strong><span>{{ port.staticPort ? t('applicationsPage.staticPort', { port: port.staticPort }) : t('applicationsPage.dynamicPort') }}</span></div><div class="row-actions"><Button size="sm" @click="openPortDialog(index)">{{ t('common.edit') }}</Button><Button size="sm" variant="danger" @click="removeAt(appDraft.ports, index)">{{ t('common.delete') }}</Button></div></div>
                <div v-for="(rule, index) in appDraft.reverseProxy" :key="`${rule.domain}-${index}`" class="item-row"><div><strong>{{ rule.domain || t('applicationsPage.unnamedDomain') }}</strong><span>{{ rule.targetType }}:{{ rule.targetPort }} · {{ rule.paths.map((path) => path.path).join(', ') }}</span></div><div class="row-actions"><Button size="sm" @click="openProxyDialog(index)">{{ t('common.edit') }}</Button><Button size="sm" variant="danger" @click="removeAt(appDraft.reverseProxy, index)">{{ t('common.delete') }}</Button></div></div>
                <EmptyState v-if="!appDraft.ports.length && !appDraft.reverseProxy.length" :title="t('applicationsPage.noRoutes')" :description="t('applicationsPage.networkingEmptyHint')" />
              </div>
            </section>

            <section v-show="activeAppSection === 'storage'" class="workspace-panel">
              <div class="section-heading"><div class="section-copy"><h3>{{ t('applicationsPage.panelStorage') }}</h3><p>{{ t('applicationsPage.storageHint') }}</p></div><Button size="sm" @click="openMountDialog()"><Plus />{{ t('applicationsPage.addMount') }}</Button></div>
              <div class="grid gap-3">
                <div class="flex items-center justify-between gap-3"><strong>{{ t('applicationsPage.variables') }}</strong><Button size="sm" @click="openRowDialog('variable')"><Plus />{{ t('common.add') }}</Button></div>
                <div v-for="(row, index) in appDraft.variables" :key="row.id" class="item-row"><div><strong>{{ row.key }}</strong><span>{{ row.value || t('common.empty') }}</span></div><div class="row-actions"><Button size="sm" @click="openRowDialog('variable', index)">{{ t('common.edit') }}</Button><Button size="sm" variant="danger" @click="removeRow('variable', index)">{{ t('common.delete') }}</Button></div></div>
                <div class="flex items-center justify-between gap-3"><strong>{{ t('applicationsPage.containerEnv') }}</strong><Button size="sm" @click="openRowDialog('env')"><Plus />{{ t('common.add') }}</Button></div>
                <div v-for="(row, index) in appDraft.env" :key="row.id" class="item-row"><div><strong>{{ row.key }}</strong><span>{{ row.value || t('common.empty') }}</span></div><div class="row-actions"><Button size="sm" @click="openRowDialog('env', index)">{{ t('common.edit') }}</Button><Button size="sm" variant="danger" @click="removeRow('env', index)">{{ t('common.delete') }}</Button></div></div>
                <div v-for="(mount, index) in appDraft.mounts" :key="mount.id" class="item-row"><div><strong>{{ mount.type }} · {{ mount.target }}</strong><span>{{ mount.source || t('applicationsPage.panelManagedSource') }}</span></div><div class="row-actions"><Button size="sm" @click="openMountDialog(index)">{{ t('common.edit') }}</Button><Button size="sm" variant="danger" @click="removeAt(appDraft.mounts, index)">{{ t('common.delete') }}</Button></div></div>
                <EmptyState v-if="!appDraft.variables.length && !appDraft.env.length && !appDraft.mounts.length" :title="t('applicationsPage.noStorageConfig')" :description="t('applicationsPage.noStorageConfigHint')" />
              </div>
            </section>

            <section v-show="activeAppSection === 'deployment'" class="workspace-panel">
              <div class="section-copy"><h3>{{ t('applicationsPage.panelDeployment') }}</h3><p>{{ t('applicationsPage.deployHint') }}</p></div>
              <label class="field">{{ t('applicationsPage.deploymentMode') }}<Select v-model="appDraft.deploymentMode" :options="[{ label: t('applicationsPage.allServers'), value: 'all' }, { label: t('applicationsPage.selectedServers'), value: 'selected' }]" @change="markAppStructuredDirty" /></label>
              <ServerMultiPicker v-if="appDraft.deploymentMode === 'selected'" v-model="appDeploymentServersModel" :servers="serverOptions" :label="t('applicationsPage.deploymentServers')" />
            </section>

            <section v-show="activeAppSection === 'files'" class="workspace-panel">
              <div class="section-heading">
                <div class="section-copy"><h3>{{ t('applicationsPage.panelFilesAssets') }}</h3><p>{{ t('applicationsPage.filesHint') }}</p></div>
                <div class="flex flex-wrap gap-2">
                  <Button size="sm" @click="openFileDialog()"><Plus />{{ t('applicationsPage.addFile') }}</Button>
                  <FileUploadButton size="sm" accept=".zip,application/zip" :loading="pending === 'editor'" :disabled="!editSession" :label="t('applicationsPage.uploadArchive')" @change="uploadArchive" />
                </div>
              </div>
              <div v-for="file in editSession?.files || []" :key="file.fileKey || file.id" class="item-row"><div><strong>{{ file.path }}</strong><span>{{ file.kind }} / {{ file.size }} bytes</span></div><div class="row-actions"><Button size="sm" @click="openFileDialog(file)">{{ t('common.edit') }}</Button><Button size="sm" variant="danger" @click="ask('file-delete', file.fileKey || file.id || '')">{{ t('common.delete') }}</Button></div></div>
              <EmptyState v-if="!(editSession?.files || []).length" :title="t('applicationsPage.noFiles')" :description="t('applicationsPage.noFilesHint')" />
            </section>
          </div>

          <div v-else class="app-editor-body">
            <section class="workspace-panel source-panel">
              <div class="section-heading">
                <div class="section-copy"><h3>{{ t('applicationsPage.sourceViewTitle') }}</h3><p>{{ t('applicationsPage.sourceViewHint') }}</p></div>
                <div class="flex flex-wrap gap-2">
                  <Button size="sm" @click="syncYamlFromForm"><RefreshCw />{{ t('applicationsPage.syncSource') }}</Button>
                  <Button size="sm" variant="primary" @click="applyYamlToForm"><Code2 />{{ t('applicationsPage.applySource') }}</Button>
                </div>
              </div>
              <div class="rounded-xl border border-info-border bg-info-bg p-3 text-sm text-info">{{ t('applicationsPage.sourceGuardHint') }}</div>
              <Textarea v-model="appDraft.specYaml" class="min-h-[520px] font-mono" :invalid="Boolean(appErrors.specYaml)" @input="markSpecDirty" />
            </section>
          </div>
        </section>

        <aside class="app-editor-summary">
          <section class="rounded-2xl border border-border bg-card p-4">
            <h3>{{ isCreateMode ? t('applicationsPage.createSummary') : t('applicationsPage.changeSummary') }}</h3>
            <div class="mt-3 grid gap-2 text-sm">
              <div><span>{{ t('applicationsPage.source') }}</span><strong>{{ sourceSummary() }}</strong></div>
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
              <div v-for="item in diagnostics" :key="`${item.code}-${item.field}`" class="rounded-xl border p-3 text-sm" :class="item.severity === 'error' ? 'border-danger-border bg-danger-bg text-danger' : 'border-warning-border bg-warning-bg text-warning'">{{ item.field ? `${item.field}: ` : '' }}{{ item.message }}</div>
              <p v-if="!diagnostics.length" class="m-0 text-sm text-muted-foreground">{{ t('applicationsPage.noDiagnostics') }}</p>
            </div>
          </section>
        </aside>
      </div>

      <div v-else class="grid min-h-0 gap-4 xl:grid-cols-[260px_minmax(0,1fr)_340px]">
        <aside class="sticky top-0 self-start rounded-2xl border border-border bg-card p-3">
          <EditorSectionRail
            v-model="activeFacilitySection"
            :sections="facilityRailSections"
            :label="t('applicationsPage.gatewayEditor')"
            :complete-label="t('common.complete')"
            :error-label="t('common.error')"
            :dirty-label="t('applicationsPage.dirty')"
          />
        </aside>

        <section class="grid max-h-[calc(100dvh-220px)] gap-5 overflow-auto rounded-2xl border border-border bg-card p-5">
          <section v-show="activeFacilitySection === 'gateways'" class="grid gap-4">
            <div class="section-copy"><h3>{{ t('applicationsPage.gatewayNodes') }}</h3><p>{{ t('applicationsPage.gatewayNodesHint') }}</p></div>
            <ServerMultiPicker v-model="facilityDeploymentServersModel" :servers="serverOptions" :label="t('applicationsPage.gatewayNodes')" />
          </section>

          <section v-show="activeFacilitySection === 'domains'" class="grid gap-4">
            <div class="section-heading"><div class="section-copy"><h3>{{ t('applicationsPage.domainGroups') }}</h3><p>{{ t('applicationsPage.domainGroupsHint') }}</p></div><Button size="sm" @click="openFacilityDomainDialog()"><Plus />{{ t('applicationsPage.addDomain') }}</Button></div>
            <div v-for="(domain, domainIndex) in facilityDraft.domains" :key="`${domain.domain}-${domainIndex}`" class="rounded-2xl border border-border bg-background p-4">
              <div class="flex items-start justify-between gap-3">
                <div><strong>{{ domain.domain || t('applicationsPage.unnamedDomain') }}</strong><span>{{ domain.originServerIds.join(', ') || t('common.notAvailable') }}</span></div>
                <div class="row-actions"><Button size="sm" @click="openFacilityDomainDialog(domainIndex)">{{ t('common.edit') }}</Button><Button size="sm" variant="danger" @click="removeAt(facilityDraft.domains, domainIndex, 'facility')">{{ t('common.delete') }}</Button></div>
              </div>
              <div class="mt-3 grid gap-2">
                <div v-for="(path, pathIndex) in domain.paths" :key="`${path.path}-${pathIndex}`" class="item-row"><div><strong>{{ path.path || '/' }} · {{ path.ruleType }}</strong><span>{{ pathTarget(path) }}</span></div><div class="row-actions"><Button size="sm" @click="openFacilityPathDialog(domainIndex, pathIndex)">{{ t('common.edit') }}</Button><Button size="sm" variant="danger" @click="removeAt(domain.paths, pathIndex, 'facility')">{{ t('common.delete') }}</Button></div></div>
                <Button size="sm" class="justify-self-start" @click="openFacilityPathDialog(domainIndex)"><Plus />{{ t('common.addPath') }}</Button>
              </div>
            </div>
          </section>

          <section v-show="activeFacilitySection === 'panel'" class="grid gap-4">
            <div class="section-copy"><h3>{{ t('applicationsPage.panelEntry') }}</h3><p>{{ t('applicationsPage.panelEntryHint') }}</p></div>
            <label class="flex items-center justify-between rounded-xl border border-border p-3 text-sm">{{ t('applicationsPage.panelEntry') }}<Switch v-model="facilityDraft.panelEnabled" :label="t('applicationsPage.panelEntry')" @click="markDirty" /></label>
            <div class="grid grid-cols-2 gap-3 max-md:grid-cols-1">
              <label class="field">{{ t('applicationsPage.panelServer') }}<Input v-model="facilityDraft.panelServerId" :invalid="Boolean(facilityErrors.panelServerId)" @input="markDirty" /></label>
              <label class="field">{{ t('applicationsPage.panelDomain') }}<Input v-model="facilityDraft.panelDomain" :invalid="Boolean(facilityErrors.panelDomain)" @input="markDirty" /></label>
            </div>
          </section>

          <section v-show="activeFacilitySection === 'assets'" class="grid gap-4">
            <div class="section-heading">
              <div class="section-copy"><h3>{{ t('applicationsPage.staticAssets') }}</h3><p>{{ t('applicationsPage.assetUploadLimit') }}</p></div>
              <div>
                <FileUploadButton size="sm" :loading="pending === 'editor'" :disabled="!facilitySession" :label="t('applicationsPage.uploadStaticAsset')" @change="uploadFacilityAsset" />
              </div>
            </div>
            <div v-for="asset in facilitySession?.assets || []" :key="asset.assetKey" class="item-row"><div><strong>{{ asset.name }}</strong><span>{{ asset.kind }} / {{ asset.filename }}</span></div><Button size="sm" variant="danger" @click="ask('facility-asset-delete', asset.assetKey)">{{ t('common.delete') }}</Button></div>
          </section>
        </section>

        <aside class="sticky top-0 grid content-start gap-3 self-start">
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
              <div v-for="item in facilityDiagnostics" :key="`${item.code}-${item.field}`" class="rounded-xl border p-3 text-sm" :class="item.severity === 'error' ? 'border-danger-border bg-danger-bg text-danger' : 'border-warning-border bg-warning-bg text-warning'">{{ item.field ? `${item.field}: ` : '' }}{{ item.message }}</div>
              <p v-if="!facilityDiagnostics.length" class="m-0 text-sm text-muted-foreground">{{ t('applicationsPage.noDiagnostics') }}</p>
            </div>
          </section>
        </aside>
      </div>

      <template #footer>
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div class="text-sm text-muted-foreground">{{ isDirty ? t('applicationsPage.unsavedChanges') : t('applicationsPage.readyToCommit') }}</div>
          <div class="flex flex-wrap gap-2">
            <Button variant="secondary" :disabled="saving" @click="router.back()">{{ t('common.cancel') }}</Button>
            <Button variant="secondary" :loading="pending === 'validate'" :disabled="saving" @click="isFacilityEditor ? validateFacility() : validateApplication()">{{ t('applicationsPage.validate') }}</Button>
            <Button variant="secondary" :loading="pending === 'preview'" :disabled="saving" @click="isFacilityEditor ? previewFacilityConfig() : previewApplication()">{{ t('applicationsPage.preview') }}</Button>
            <Button variant="primary" :loading="pending === 'commit'" :disabled="isFacilityEditor ? Boolean(Object.keys(facilityErrors).length || hasBlockingFacilityDiagnostics || saving) : Boolean(Object.keys(appErrors).length || hasBlockingAppDiagnostics || saving)" @click="isFacilityEditor ? commitFacilityConfig() : commitApplication()"><Save />{{ t('applicationsPage.commit') }}</Button>
          </div>
        </div>
      </template>
    </EditorPage>
  </ConsolePage>

  <Dialog v-model:open="logsOpen" :title="t('applicationsPage.logs')" :close-label="t('common.close')">
    <pre class="max-h-[520px] overflow-auto whitespace-pre-wrap rounded-xl border border-border bg-background p-3 text-xs text-foreground">{{ logsText }}</pre>
  </Dialog>

  <Dialog v-model:open="dialogOpen" :title="t(`applicationsPage.dialog.${dialogKind}`)" :close-label="t('common.close')">
    <div v-if="dialogKind === 'variable' || dialogKind === 'env'" class="grid gap-3">
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
      <label class="field">{{ t('applicationsPage.source') }}<Input v-model="mountDraft.source" /></label>
      <label class="field">{{ t('applicationsPage.target') }}<Input v-model="mountDraft.target" /></label>
      <label class="field">{{ t('applicationsPage.mode') }}<Input v-model="mountDraft.mode" placeholder="0755" /></label>
      <label class="flex items-center justify-between rounded-xl border border-border p-3 text-sm">{{ t('applicationsPage.readOnly') }}<Switch v-model="mountDraft.readOnly" :label="t('applicationsPage.readOnly')" /></label>
    </div>
    <div v-else-if="dialogKind === 'proxy'" class="grid gap-3">
      <label class="field">{{ t('applicationsPage.domain') }}<Input v-model="proxyDraft.domain" /></label>
      <label class="field">{{ t('applicationsPage.targetPort') }}<Input v-model="proxyDraft.targetPort" /></label>
      <label class="field">{{ t('applicationsPage.targetType') }}<Select v-model="proxyDraft.targetType" :options="[{ label: 'local', value: 'local' }, { label: 'container', value: 'container' }]" /></label>
      <ServerMultiPicker v-model="proxyOriginServersModel" :servers="serverOptions" :label="t('applicationsPage.originServers', { count: proxyDraft.originServerIds.length })" />
      <div class="section-heading"><strong>{{ t('common.path') }}</strong><Button size="sm" @click="openProxyPathDialog()"><Plus />{{ t('common.addPath') }}</Button></div>
      <div v-for="(path, index) in proxyDraft.paths" :key="`${path.path}-${index}`" class="item-row"><div><strong>{{ path.path }}</strong><span>WebSocket: {{ path.webSocket ? 'on' : 'off' }}</span></div><div class="row-actions"><Button size="sm" @click="openProxyPathDialog(index)">{{ t('common.edit') }}</Button><Button size="sm" variant="danger" @click="proxyDraft.paths.splice(index, 1)">{{ t('common.delete') }}</Button></div></div>
    </div>
    <div v-else-if="dialogKind === 'proxyPath'" class="grid gap-3">
      <label class="field">{{ t('common.path') }}<Input v-model="proxyPathDraft.path" /></label>
      <label class="field">{{ t('applicationsPage.webSocket') }}<Switch v-model="proxyPathWebSocket" :label="t('applicationsPage.webSocket')" /></label>
    </div>
    <div v-else-if="dialogKind === 'facilityDomain'" class="grid gap-3">
      <label class="field">{{ t('applicationsPage.domain') }}<Input v-model="facilityDomainDraft.domain" /></label>
      <ServerMultiPicker v-model="facilityDomainOriginServersModel" :servers="serverOptions" :label="t('applicationsPage.originServers', { count: facilityDomainDraft.originServerIds.length })" />
    </div>
    <div v-else-if="dialogKind === 'facilityPath'" class="grid gap-3">
      <label class="field">{{ t('common.path') }}<Input v-model="facilityPathDraft.path" /></label>
      <label class="field">{{ t('common.type') }}<Select v-model="facilityPathDraft.ruleType" :options="routeTypeOptions" /></label>
      <template v-if="facilityPathDraft.ruleType === 'static'">
        <label class="field">{{ t('applicationsPage.sourceType') }}<Select v-model="facilityPathDraft.sourceType" :options="sourceTypeOptions" /></label>
        <label v-if="facilityPathDraft.sourceType === 'host_path'" class="field">{{ t('applicationsPage.rootPath') }}<Input v-model="facilityPathDraft.rootPath" /></label>
        <label v-else class="field">{{ t('applicationsPage.staticAsset') }}<Select v-model="facilityPathDraft.assetId" :options="assetOptions" /></label>
      </template>
      <template v-else-if="facilityPathDraft.ruleType === 'redirect'">
        <label class="field">{{ t('applicationsPage.redirectUrl') }}<Input v-model="facilityPathDraft.redirectUrl" /></label>
        <label class="field">{{ t('applicationsPage.redirectCode') }}<Select v-model="facilityRedirectCode" :options="['301', '302', '307', '308'].map((value) => ({ label: value, value }))" /></label>
      </template>
      <template v-else>
        <label class="field">{{ t('applicationsPage.proxyUrl') }}<Input v-model="facilityPathDraft.proxyUrl" /></label>
      </template>
    </div>
    <template #footer>
      <Button variant="secondary" @click="dialogOpen = false">{{ t('common.cancel') }}</Button>
      <Button v-if="dialogKind === 'variable' || dialogKind === 'env'" variant="primary" @click="saveRowDialog">{{ t('common.save') }}</Button>
      <Button v-else-if="dialogKind === 'port'" variant="primary" @click="savePortDialog">{{ t('common.save') }}</Button>
      <Button v-else-if="dialogKind === 'mount'" variant="primary" @click="saveMountDialog">{{ t('common.save') }}</Button>
      <Button v-else-if="dialogKind === 'proxy'" variant="primary" @click="saveProxyDialog">{{ t('common.save') }}</Button>
      <Button v-else-if="dialogKind === 'proxyPath'" variant="primary" @click="saveProxyPathDialog">{{ t('common.save') }}</Button>
      <Button v-else-if="dialogKind === 'facilityDomain'" variant="primary" @click="saveFacilityDomainDialog">{{ t('common.save') }}</Button>
      <Button v-else-if="dialogKind === 'facilityPath'" variant="primary" @click="saveFacilityPathDialog">{{ t('common.save') }}</Button>
    </template>
  </Dialog>

  <Dialog v-model:open="fileDialog" :title="fileEditing ? t('applicationsPage.editFile') : t('applicationsPage.addFile')" :close-label="t('common.close')">
    <div class="grid gap-3">
      <label class="field">{{ t('applicationsPage.filePath') }}<Input v-model="fileForm.path" /></label>
      <label class="field">{{ t('applicationsPage.fileKind') }}<Select v-model="fileForm.kind" :options="fileKindOptions" /></label>
      <label class="field">{{ t('applicationsPage.contentType') }}<Input v-model="fileForm.contentType" /></label>
      <label class="field">{{ t('applicationsPage.fileContent') }}<Textarea v-model="fileForm.content" class="min-h-[220px] font-mono" /></label>
    </div>
    <template #footer>
      <Button variant="secondary" @click="fileDialog = false">{{ t('common.cancel') }}</Button>
      <Button variant="primary" :loading="pending === 'editor'" @click="saveFile">{{ t('common.save') }}</Button>
    </template>
  </Dialog>

  <Dialog v-model:open="confirmOpen" :title="t(`applicationsPage.confirm.${confirmKind}.title`)" :description="t(`applicationsPage.confirm.${confirmKind}.description`)" :close-label="t('common.close')">
    <div class="flex gap-3 rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning"><AlertTriangle class="size-4 shrink-0" />{{ t('applicationsPage.confirmImpact') }}</div>
    <template #footer>
      <Button variant="secondary" @click="confirmOpen = false">{{ t('common.cancel') }}</Button>
      <Button variant="danger" :loading="Boolean(pending)" @click="confirmAction">{{ t('common.apply') }}</Button>
    </template>
  </Dialog>
</template>

<style scoped>
h3 {
  margin: 0;
  color: hsl(var(--foreground));
  font-size: 14px;
  font-weight: 650;
}

span {
  color: hsl(var(--muted-foreground));
  font-size: 12px;
}

strong {
  display: block;
  color: hsl(var(--foreground));
  overflow-wrap: anywhere;
}

.field {
  display: grid;
  gap: 0.35rem;
  color: hsl(var(--foreground));
  font-size: 0.875rem;
}

.section-copy {
  display: grid;
  gap: 0.25rem;
  min-width: 0;
}

.section-copy p {
  margin: 0;
  color: hsl(var(--muted-foreground));
  font-size: 0.8125rem;
  line-height: 1.5;
}

.section-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 0.75rem;
}

.app-editor-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) clamp(280px, 24vw, 340px);
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
  border: 1px solid hsl(var(--border));
  border-radius: 1rem;
  background: hsl(var(--card));
}

.app-editor-header {
  display: grid;
  gap: 1rem;
  min-width: 0;
  border-bottom: 1px solid hsl(var(--border));
  padding: 1rem;
}

.app-editor-title-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: start;
  gap: 1rem;
  min-width: 0;
}

.mode-switch {
  display: inline-flex;
  width: max-content;
  max-width: 100%;
  border: 1px solid hsl(var(--border));
  border-radius: 0.875rem;
  background: hsl(var(--background));
  padding: 0.25rem;
}

.app-editor-body {
  display: grid;
  gap: 1.25rem;
  min-width: 0;
  min-height: 0;
  max-height: min(820px, calc(100dvh - 320px));
  overflow: auto;
  padding: 1.25rem;
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

.mode-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 0.4rem;
  min-height: 2rem;
  min-width: 0;
  border: 0;
  border-radius: 0.625rem;
  background: transparent;
  color: hsl(var(--muted-foreground));
  padding: 0 0.75rem;
  font-size: 0.8125rem;
  font-weight: 650;
  transition:
    background-color var(--panel-motion-duration-base) var(--panel-motion-ease-standard),
    color var(--panel-motion-duration-base) var(--panel-motion-ease-standard),
    box-shadow var(--panel-motion-duration-base) var(--panel-motion-ease-standard),
    transform var(--panel-motion-duration-base) var(--panel-motion-ease-standard);
}

.mode-button:hover {
  transform: translateY(var(--panel-motion-hover-y));
}

.mode-button:active {
  transform: translateY(0) scale(var(--panel-motion-press-scale));
}

.mode-button.active {
  background: hsl(var(--card));
  color: hsl(var(--foreground));
  box-shadow: 0 1px 2px hsl(var(--foreground) / 0.08);
}

.workspace-steps {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(10.5rem, 100%), 1fr));
  gap: 0.5rem;
  min-width: 0;
}

.workspace-step {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  align-items: center;
  gap: 0.5rem;
  min-width: 0;
  border: 1px solid hsl(var(--border));
  border-radius: 0.875rem;
  background: hsl(var(--background));
  color: hsl(var(--foreground));
  padding: 0.65rem;
  text-align: left;
  transition:
    background-color var(--panel-motion-duration-base) var(--panel-motion-ease-standard),
    border-color var(--panel-motion-duration-base) var(--panel-motion-ease-standard),
    box-shadow var(--panel-motion-duration-base) var(--panel-motion-ease-standard),
    transform var(--panel-motion-duration-base) var(--panel-motion-ease-standard);
}

.workspace-step:hover {
  transform: translateY(var(--panel-motion-hover-y));
  box-shadow: var(--panel-motion-shadow-raised);
}

.workspace-step:active {
  transform: translateY(0) scale(var(--panel-motion-press-scale));
}

.workspace-step span {
  min-width: 0;
  overflow-wrap: anywhere;
}

.workspace-step :deep(.status-badge) {
  grid-column: 1 / -1;
  justify-self: start;
}

.workspace-step.active {
  border-color: hsl(var(--primary));
  background: hsl(var(--primary) / 0.08);
  box-shadow: var(--panel-motion-shadow-raised);
}

.workspace-step.error {
  border-color: hsl(var(--danger-border));
  background: hsl(var(--danger-bg));
}

.workspace-panel {
  display: grid;
  gap: 1rem;
  min-width: 0;
  border: 1px solid hsl(var(--border));
  border-radius: 1rem;
  background: hsl(var(--background));
  padding: 1rem;
  animation: panel-motion-enter var(--panel-motion-duration-slow) var(--panel-motion-ease-emphasized) both;
}

.form-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(17rem, 100%), 1fr));
  gap: 0.75rem;
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
  border: 1px solid hsl(var(--border));
  border-radius: 0.75rem;
  padding: 0.75rem;
  color: hsl(var(--foreground));
  font-size: 0.875rem;
}

.source-panel {
  background: linear-gradient(180deg, hsl(var(--background)), hsl(var(--muted) / 0.35));
}

.item-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 0.75rem;
  align-items: center;
  min-width: 0;
  border: 1px solid hsl(var(--border));
  border-radius: 0.875rem;
  background: hsl(var(--background));
  padding: 0.75rem;
  transition:
    background-color var(--panel-motion-duration-base) var(--panel-motion-ease-standard),
    border-color var(--panel-motion-duration-base) var(--panel-motion-ease-standard),
    box-shadow var(--panel-motion-duration-base) var(--panel-motion-ease-standard),
    transform var(--panel-motion-duration-base) var(--panel-motion-ease-standard);
}

.item-row:hover {
  border-color: hsl(var(--border) / 0.92);
  background: hsl(var(--muted) / 0.34);
  transform: translateY(var(--panel-motion-hover-y));
  box-shadow: var(--panel-motion-shadow-raised);
}

.row-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  justify-content: flex-end;
}

@media (max-width: 760px) {
  .item-row,
  .section-heading {
    grid-template-columns: 1fr;
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

  .workspace-steps {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }

  .app-editor-body {
    max-height: none;
  }
}

@media (max-width: 1023px) {
  .app-editor-shell {
    overflow: visible;
  }

  .app-editor-body {
    max-height: none;
    overflow: visible;
  }
}

@media (max-width: 900px) {
  .workspace-steps {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .form-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 640px) {
  .app-editor-title-row,
  .app-editor-summary {
    grid-template-columns: 1fr;
  }

  .mode-switch {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    width: 100%;
  }

  .mode-button {
    padding: 0 0.5rem;
  }

  .workspace-steps {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 420px) {
  .workspace-steps,
  .form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
