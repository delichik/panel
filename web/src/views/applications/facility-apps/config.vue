<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import { onBeforeRouteLeave, useRouter } from 'vue-router';
import { facilityAppsApi } from '@/api/facilityApps';
import { serversApi } from '@/api/servers';
import AppActionButton from '@/components/AppActionButton.vue';
import AppActionGroup from '@/components/AppActionGroup.vue';
import PageLoadingState from '@/components/PageLoadingState.vue';
import RoutePathAdvancedFields from '@/components/RoutePathAdvancedFields.vue';
import type { FacilityPanelEntryDto, FacilityReverseProxyConfigDto, FacilityReverseProxySaveDto, FacilityRouteDomainDto, FacilityRoutePathDto, FacilityStaticAssetDto, ServerDto } from '@/types/api';
import { useI18n } from '@/i18n';

type PendingAssetUpload = { localId: string; name: string; kind: string; file: File };

const router = useRouter();
const { t } = useI18n();
const loading = ref(true);
const saving = ref(false);
const saveStep = ref('');
const error = ref('');
const snackbar = ref(false);
const message = ref('');
const servers = ref<ServerDto[]>([]);
const config = ref<FacilityReverseProxyConfigDto | null>(null);
const staticAssets = ref<FacilityStaticAssetDto[]>([]);
const pendingAssetUploads = ref<PendingAssetUpload[]>([]);
const deletedAssetIds = ref<string[]>([]);
const baselinePayload = ref('');
const scrollBody = ref<HTMLElement | { $el?: HTMLElement } | null>(null);
const activeSectionId = ref('facility-settings-general');
let sectionObserver: IntersectionObserver | null = null;

const form = reactive({
  deploymentServers: [] as string[],
  panelEntry: { enabled: false, serverId: '', domain: '' } as FacilityPanelEntryDto,
  domains: [] as FacilityRouteDomainDto[],
});
const domainDialog = reactive({ open: false, index: -1, draft: null as FacilityRouteDomainDto | null });
const pathDialog = reactive({ open: false, domainIndex: -1, pathIndex: -1, draft: null as FacilityRoutePathDto | null });
const confirmDialog = reactive({ open: false, title: '', message: '', action: null as null | (() => void) });
const uploadForm = reactive({ name: '', kind: 'uploaded_file', file: null as File | File[] | null });

const sections = computed(() => [
  { id: 'facility-settings-general', title: t('facilityAppsPage.basicSettings'), icon: 'mdi-server-network' },
  { id: 'facility-settings-routes', title: t('facilityAppsPage.facilityRoutes'), icon: 'mdi-routes' },
  { id: 'facility-settings-panel', title: t('facilityAppsPage.panelEntry'), icon: 'mdi-view-dashboard-outline' },
  { id: 'facility-settings-assets', title: t('facilityAppsPage.staticAssets'), icon: 'mdi-folder-multiple-image' },
]);
const serverOptions = computed(() => servers.value.map((server) => ({ title: `${server.name} (${server.host})`, value: server.id })));
const gatewayServerOptions = computed(() => serverOptions.value.filter((item) => form.deploymentServers.includes(item.value)));
const panelHostServerId = computed(() => config.value?.panelHostServerId || '');
const staticAssetOptions = computed(() => staticAssets.value.map((asset) => ({ title: `${asset.name} (${asset.filename})`, value: asset.id })));
const routeTypeOptions = computed(() => [
  { title: t('facilityAppsPage.staticContent'), value: 'static' },
  { title: t('facilityAppsPage.redirect'), value: 'redirect' },
  { title: t('facilityAppsPage.proxyPass'), value: 'proxy_pass' },
]);
const sourceOptions = computed(() => [
  { title: t('facilityAppsPage.serverDirectory'), value: 'host_path' },
  { title: t('facilityAppsPage.uploadedFile'), value: 'uploaded_file' },
  { title: t('facilityAppsPage.uploadedBundle'), value: 'uploaded_bundle' },
]);
const strategyOptions = computed(() => [
  { title: t('facilityAppsPage.roundRobin'), value: 'round_robin' },
  { title: t('facilityAppsPage.primaryBackup'), value: 'primary_backup' },
  { title: t('facilityAppsPage.ipHash'), value: 'ip_hash' },
]);
const uploadKindOptions = computed(() => sourceOptions.value.filter((item) => item.value !== 'host_path'));
const proxySourceModeOptions = computed(() => [
  { title: t('facilityAppsPage.preserveSource'), value: 'preserve_source' },
  { title: t('facilityAppsPage.hideSource'), value: 'hide_source' },
]);
const dirty = computed(() => pendingAssetUploads.value.length > 0 || deletedAssetIds.value.length > 0 || JSON.stringify(savePayload()) !== baselinePayload.value);

function clonePath(path?: FacilityRoutePathDto | null): FacilityRoutePathDto {
  return {
    path: path?.path || '/',
    ruleType: path?.ruleType || 'static',
    rootPath: path?.rootPath || '',
    sourceType: path?.sourceType || 'host_path',
    assetId: path?.assetId || '',
    redirectUrl: path?.redirectUrl || '',
    redirectCode: path?.redirectCode || 302,
    proxyUrl: path?.proxyUrl || '',
    proxySourceMode: path?.proxySourceMode || 'preserve_source',
    options: {
      gzipMode: path?.options?.gzipMode || 'inherit',
      clientMaxBodySizeMb: path?.options?.clientMaxBodySizeMb || 0,
      connectTimeoutSeconds: path?.options?.connectTimeoutSeconds || 0,
      readTimeoutSeconds: path?.options?.readTimeoutSeconds || 0,
      sendTimeoutSeconds: path?.options?.sendTimeoutSeconds || 0,
      bufferingMode: path?.options?.bufferingMode || 'inherit',
      webSocketMode: path?.options?.webSocketMode || ((path?.ruleType || 'static') === 'proxy_pass' ? 'auto' : 'off'),
      requestHeaders: (path?.options?.requestHeaders ?? []).map((header) => ({ ...header })),
      responseHeaders: (path?.options?.responseHeaders ?? []).map((header) => ({ ...header })),
    },
  };
}

function cloneDomain(domain?: FacilityRouteDomainDto | null): FacilityRouteDomainDto {
  return {
    domain: domain?.domain || '',
    originServerIds: [...(domain?.originServerIds ?? [])],
    anyAccess: {
      enabled: Boolean(domain?.anyAccess?.enabled),
      strategy: domain?.anyAccess?.strategy || 'round_robin',
      primaryOriginServerId: domain?.anyAccess?.primaryOriginServerId || '',
    },
    paths: (domain?.paths ?? []).map(clonePath),
  };
}

async function load() {
  loading.value = true;
  try {
    const [serverItems, next] = await Promise.all([serversApi.listServers(), facilityAppsApi.reverseProxy()]);
    servers.value = serverItems;
    applyConfig(next);
    staticAssets.value = next.staticAssets ?? await facilityAppsApi.staticAssets();
    error.value = '';
    await nextTick();
    syncSectionObserver();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('facilityAppsPage.loadFailed');
  } finally {
    loading.value = false;
  }
}

function applyConfig(next: FacilityReverseProxyConfigDto) {
  config.value = next;
  form.deploymentServers = [...(next.deploymentServers ?? [])];
  form.panelEntry = { enabled: Boolean(next.panelEntry?.enabled), serverId: next.panelHostServerId || next.panelEntry?.serverId || '', domain: next.panelEntry?.domain || '' };
  form.domains = (next.domains ?? []).map(cloneDomain);
  staticAssets.value = next.staticAssets ?? staticAssets.value;
  pendingAssetUploads.value = [];
  deletedAssetIds.value = [];
  baselinePayload.value = JSON.stringify(savePayload());
}

function savePayload(): FacilityReverseProxySaveDto {
  return {
    deploymentServers: [...form.deploymentServers],
    panelEntry: { enabled: Boolean(form.panelEntry.enabled), serverId: form.panelEntry.enabled ? panelHostServerId.value : '', domain: (form.panelEntry.domain || '').trim().toLowerCase() },
    domains: form.domains.map((domain) => ({
      ...cloneDomain(domain),
      domain: domain.domain.trim().toLowerCase(),
      anyAccess: {
        ...domain.anyAccess,
        primaryOriginServerId: domain.anyAccess.enabled && domain.anyAccess.strategy === 'primary_backup' ? (domain.anyAccess.primaryOriginServerId || '') : '',
      },
    })),
  };
}

function validatePayload(payload: FacilityReverseProxySaveDto) {
  const gateways = new Set(payload.deploymentServers);
  if (payload.panelEntry.enabled && !panelHostServerId.value) return t('facilityAppsPage.panelHostSetupRequired');
  if (payload.panelEntry.enabled && !gateways.has(panelHostServerId.value)) return t('facilityAppsPage.panelHostGatewayRequired');
  for (const domain of payload.domains) {
    if (!domain.domain) return t('facilityAppsPage.domainRequired');
    if (!domain.originServerIds.length) return t('facilityAppsPage.originServersRequired');
    if (domain.originServerIds.some((id) => !gateways.has(id))) return t('facilityAppsPage.originServersInvalid');
    if (!domain.paths.length) return t('facilityAppsPage.domainPathRequired');
    if (domain.anyAccess.enabled && domain.anyAccess.strategy === 'primary_backup' && !domain.originServerIds.includes(domain.anyAccess.primaryOriginServerId || '')) return t('facilityAppsPage.primaryOriginRequired');
  }
  return '';
}

async function save() {
  const payload = savePayload();
  const validation = validatePayload(payload);
  if (validation) { error.value = validation; return; }
  saving.value = true;
  saveStep.value = t('facilityAppsPage.saveStepStartingSession');
  let sessionId = '';
  try {
    const session = await facilityAppsApi.beginSaveSession(config.value?.updatedAt || '0001-01-01T00:00:00Z');
    sessionId = session.id;
    for (const [index, assetId] of deletedAssetIds.value.entries()) {
      saveStep.value = t('facilityAppsPage.saveStepDeletingAsset', { current: index + 1, total: deletedAssetIds.value.length });
      await facilityAppsApi.deleteSaveSessionAsset(session.id, assetId);
    }
    const assetIds = new Map<string, string>();
    for (const [index, pending] of pendingAssetUploads.value.entries()) {
      saveStep.value = t('facilityAppsPage.saveStepUploadingAsset', { current: index + 1, total: pendingAssetUploads.value.length });
      const uploaded = await facilityAppsApi.uploadSaveSessionAsset(session.id, { name: pending.name, kind: pending.kind, file: pending.file });
      assetIds.set(pending.localId, uploaded.id);
    }
    payload.domains = payload.domains.map((domain) => ({ ...domain, paths: domain.paths.map((path) => ({ ...path, assetId: path.assetId ? (assetIds.get(path.assetId) ?? path.assetId) : '' })) }));
    saveStep.value = t('facilityAppsPage.saveStepCommitting');
    const result = await facilityAppsApi.commitSaveSession(session.id, payload);
    sessionId = '';
    applyConfig(result.config);
    message.value = result.applyRequested ? t('facilityAppsPage.savedAndApplying') : t('facilityAppsPage.savedApplyFailed');
    snackbar.value = true;
    error.value = '';
  } catch (err) {
    if (sessionId) void facilityAppsApi.discardSaveSession(sessionId).catch(() => undefined);
    error.value = err instanceof Error ? err.message : t('facilityAppsPage.saveFailed');
  } finally {
    saving.value = false;
    saveStep.value = '';
  }
}

function updateGatewayServers(ids: string[]) {
  if (form.panelEntry.enabled && panelHostServerId.value && !ids.includes(panelHostServerId.value)) ids = [...ids, panelHostServerId.value];
  const allowed = new Set(ids);
  form.deploymentServers = [...ids];
  for (const domain of form.domains) {
    domain.originServerIds = domain.originServerIds.filter((id) => allowed.has(id));
    if (domain.anyAccess.primaryOriginServerId && !domain.originServerIds.includes(domain.anyAccess.primaryOriginServerId)) domain.anyAccess.primaryOriginServerId = '';
  }
  form.panelEntry.serverId = form.panelEntry.enabled ? panelHostServerId.value : '';
}

function openDomainDialog(index = -1) {
  domainDialog.index = index;
  domainDialog.draft = index >= 0 ? cloneDomain(form.domains[index]) : cloneDomain({ domain: '', originServerIds: [...form.deploymentServers], anyAccess: { enabled: false, strategy: 'round_robin' }, paths: [] });
  domainDialog.open = true;
}
function closeDomainDialog() { domainDialog.open = false; domainDialog.index = -1; domainDialog.draft = null; }
function saveDomainDialog() {
  if (!domainDialog.draft) return;
  const next = cloneDomain(domainDialog.draft);
  if (domainDialog.index >= 0) form.domains[domainDialog.index] = next;
  else form.domains.push(next);
  closeDomainDialog();
}
function openPathDialog(domainIndex: number, pathIndex = -1) {
  pathDialog.domainIndex = domainIndex;
  pathDialog.pathIndex = pathIndex;
  pathDialog.draft = pathIndex >= 0 ? clonePath(form.domains[domainIndex].paths[pathIndex]) : clonePath();
  pathDialog.open = true;
}
function closePathDialog() { pathDialog.open = false; pathDialog.domainIndex = -1; pathDialog.pathIndex = -1; pathDialog.draft = null; }
function savePathDialog() {
  if (!pathDialog.draft || pathDialog.domainIndex < 0) return;
  const paths = form.domains[pathDialog.domainIndex].paths;
  const next = clonePath(pathDialog.draft);
  if (pathDialog.pathIndex >= 0) paths[pathDialog.pathIndex] = next;
  else paths.push(next);
  closePathDialog();
}

function openConfirm(title: string, body: string, action: () => void) { confirmDialog.title = title; confirmDialog.message = body; confirmDialog.action = action; confirmDialog.open = true; }
function closeConfirm() { confirmDialog.open = false; confirmDialog.action = null; }
function confirmAction() { const action = confirmDialog.action; closeConfirm(); action?.(); }
function removeDomain(index: number) { form.domains.splice(index, 1); }
function removePath(domainIndex: number, pathIndex: number) { form.domains[domainIndex].paths.splice(pathIndex, 1); }

function routeTypeLabel(path: FacilityRoutePathDto) { return routeTypeOptions.value.find((item) => item.value === path.ruleType)?.title ?? path.ruleType; }
function serverNames(ids: string[]) { return ids.map((id) => servers.value.find((server) => server.id === id)?.name ?? id).join(', ') || t('common.notAvailable'); }
function strategyLabel(domain: FacilityRouteDomainDto) { return domain.anyAccess.enabled ? (strategyOptions.value.find((item) => item.value === domain.anyAccess.strategy)?.title ?? domain.anyAccess.strategy) : t('facilityAppsPage.anyAccessDisabled'); }
function pathSummary(path: FacilityRoutePathDto) {
  const options = path.options;
  return `${t('facilityAppsPage.gzip')}: ${options?.gzipMode || 'inherit'} · ${t('facilityAppsPage.bodyLimit')}: ${options?.clientMaxBodySizeMb || 0} MB · ${t('facilityAppsPage.timeouts')}: ${options?.connectTimeoutSeconds || 0}/${options?.readTimeoutSeconds || 0}/${options?.sendTimeoutSeconds || 0}s · ${t('facilityAppsPage.buffering')}: ${options?.bufferingMode || 'inherit'} · WebSocket: ${options?.webSocketMode || 'off'} · ${t('facilityAppsPage.headers')}: ${options?.requestHeaders?.length || 0}/${options?.responseHeaders?.length || 0}`;
}

function selectedUploadFile() { return Array.isArray(uploadForm.file) ? uploadForm.file[0] : uploadForm.file; }
function uploadAsset() {
  const file = selectedUploadFile();
  if (!file) return;
  const localId = `draft-asset-${Date.now()}-${pendingAssetUploads.value.length + 1}`;
  const now = new Date().toISOString();
  const asset = { id: localId, name: uploadForm.name || file.name, kind: uploadForm.kind, filename: file.name, size: file.size, sha256: '', createdAt: now, updatedAt: now } as FacilityStaticAssetDto;
  pendingAssetUploads.value.push({ localId, name: asset.name, kind: asset.kind, file });
  staticAssets.value = [asset, ...staticAssets.value];
  uploadForm.name = '';
  uploadForm.file = null;
}
function deleteAsset(asset: FacilityStaticAssetDto) {
  if (asset.id.startsWith('draft-asset-')) pendingAssetUploads.value = pendingAssetUploads.value.filter((item) => item.localId !== asset.id);
  else if (!deletedAssetIds.value.includes(asset.id)) deletedAssetIds.value.push(asset.id);
  staticAssets.value = staticAssets.value.filter((item) => item.id !== asset.id);
  for (const domain of form.domains) for (const path of domain.paths) if (path.assetId === asset.id) path.assetId = '';
}
function formatBytes(value: number) { if (value >= 1048576) return `${(value / 1048576).toFixed(1)} MB`; if (value >= 1024) return `${(value / 1024).toFixed(1)} KB`; return `${value} B`; }

function scrollToSection(id: string) {
  document.getElementById(id)?.scrollIntoView({ behavior: 'smooth', block: 'start' });
}
function syncSectionObserver() {
  sectionObserver?.disconnect();
  const root = scrollBody.value instanceof HTMLElement ? scrollBody.value : scrollBody.value?.$el ?? null;
  if (!root) return;
  sectionObserver = new IntersectionObserver((entries) => {
    const visible = entries.filter((entry) => entry.isIntersecting).sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top);
    if (visible[0]) activeSectionId.value = visible[0].target.id;
  }, { root, rootMargin: '-12px 0px -65% 0px', threshold: [0, 0.1] });
  for (const section of sections.value) { const element = document.getElementById(section.id); if (element) sectionObserver.observe(element); }
}
function backToFacility() { void router.push('/applications/facility-apps'); }
function handleBeforeUnload(event: BeforeUnloadEvent) { if (!dirty.value && !saving.value) return; event.preventDefault(); event.returnValue = ''; }

onMounted(() => { window.addEventListener('beforeunload', handleBeforeUnload); void load(); });
onBeforeUnmount(() => { window.removeEventListener('beforeunload', handleBeforeUnload); sectionObserver?.disconnect(); });
onBeforeRouteLeave(() => { if (saving.value) return false; if (!dirty.value) return true; return window.confirm(t('facilityAppsPage.discardChangesConfirm')); });
</script>

<template>
  <div class="page-shell facility-config-page">
    <PageLoadingState v-if="loading" />
    <template v-else-if="config">
      <div class="config-toolbar">
        <div class="config-toolbar__leading">
          <AppActionButton kind="plain" icon="mdi-arrow-left" :label="t('facilityAppsPage.backToFacility')" :disabled="saving" @click="backToFacility" />
          <div class="min-width-0">
            <div class="text-h6 font-weight-bold text-truncate">{{ t('facilityAppsPage.reverseProxySettings') }}</div>
            <div class="text-caption text-medium-emphasis text-truncate">{{ t('facilityAppsPage.settingsHint') }}</div>
          </div>
          <v-chip v-if="dirty" color="warning" size="small" variant="tonal" label>{{ t('facilityAppsPage.unsavedChanges') }}</v-chip>
        </div>
        <AppActionButton kind="primary" icon="mdi-content-save-outline" :label="t('common.saveAndDeploy')" :loading="saving" :disabled="!dirty" @click="save" />
      </div>
      <v-alert v-if="error" type="error" variant="tonal" density="compact" closable @click:close="error = ''">{{ error }}</v-alert>

      <v-card variant="outlined" class="settings-card">
        <v-card-text ref="scrollBody" class="settings-body">
          <div class="settings-layout">
            <nav class="editor-section-nav" :aria-label="t('facilityAppsPage.settingsSections')">
              <button v-for="section in sections" :key="section.id" type="button" class="editor-section-link" :class="{ 'editor-section-link--active': activeSectionId === section.id }" @click="scrollToSection(section.id)">
                <v-icon :icon="section.icon" size="18" /><span>{{ section.title }}</span>
              </button>
            </nav>
            <div class="settings-flow">
              <section id="facility-settings-general" class="editor-section">
                <div><div class="section-title">{{ t('facilityAppsPage.basicSettings') }}</div><div class="section-hint">{{ t('facilityAppsPage.globalGatewayHint') }}</div></div>
                <v-select :model-value="form.deploymentServers" :items="serverOptions" item-title="title" item-value="value" :label="t('facilityAppsPage.globalGatewayNodes')" multiple chips closable-chips variant="outlined" density="comfortable" @update:model-value="updateGatewayServers" />
              </section>

              <section id="facility-settings-routes" class="editor-section">
                <div class="section-heading"><div><div class="section-title">{{ t('facilityAppsPage.facilityRoutes') }}</div><div class="section-hint">{{ t('facilityAppsPage.facilityRoutesHint') }}</div></div><AppActionButton icon="mdi-plus" :label="t('facilityAppsPage.addDomain')" @click="openDomainDialog()" /></div>
                <div v-if="form.domains.length" class="domain-list">
                  <div v-for="(domain, domainIndex) in form.domains" :key="`${domain.domain}-${domainIndex}`" class="domain-item">
                    <div class="domain-heading">
                      <button type="button" class="domain-main" @click="openDomainDialog(domainIndex)">
                        <strong>{{ domain.domain || t('facilityAppsPage.domain') }}</strong>
                        <span>{{ t('facilityAppsPage.originServers') }}: {{ serverNames(domain.originServerIds) }}</span>
                        <span>AnyAccess · {{ strategyLabel(domain) }}</span>
                      </button>
                      <AppActionGroup context="table"><AppActionButton icon="mdi-pencil-outline" :label="t('common.edit')" @click="openDomainDialog(domainIndex)" /><AppActionButton kind="danger" icon="mdi-delete-outline" :label="t('common.delete')" @click="openConfirm(t('facilityAppsPage.deleteDomainTitle'), t('facilityAppsPage.deleteDomainConfirm', { domain: domain.domain }), () => removeDomain(domainIndex))" /></AppActionGroup>
                    </div>
                    <div class="path-list">
                      <div v-for="(path, pathIndex) in domain.paths" :key="`${path.path}-${pathIndex}`" class="path-item">
                        <button type="button" class="path-copy" @click="openPathDialog(domainIndex, pathIndex)"><strong class="mono">{{ path.path || '/' }}</strong><span>{{ routeTypeLabel(path) }}</span><small>{{ pathSummary(path) }}</small></button>
                        <AppActionGroup context="table"><AppActionButton icon="mdi-pencil-outline" :label="t('common.edit')" @click.stop="openPathDialog(domainIndex, pathIndex)" /><AppActionButton kind="danger" icon="mdi-delete-outline" :label="t('common.delete')" @click.stop="openConfirm(t('facilityAppsPage.deleteRouteTitle'), t('facilityAppsPage.deleteRouteConfirm', { path: path.path || '/' }), () => removePath(domainIndex, pathIndex))" /></AppActionGroup>
                      </div>
                      <AppActionButton icon="mdi-plus" :label="t('common.addPath')" @click="openPathDialog(domainIndex)" />
                    </div>
                  </div>
                </div>
                <div v-else class="empty-section">{{ t('facilityAppsPage.noFacilityRoutes') }}</div>
              </section>

              <section id="facility-settings-panel" class="editor-section">
                <div><div class="section-title">{{ t('facilityAppsPage.panelEntry') }}</div><div class="section-hint">{{ t('facilityAppsPage.panelEntryHint') }}</div></div>
                <v-switch v-model="form.panelEntry.enabled" :label="t('facilityAppsPage.enablePanelEntry')" color="primary" :disabled="!panelHostServerId" @update:model-value="form.panelEntry.serverId = panelHostServerId" />
                <v-alert v-if="!panelHostServerId" type="info" variant="tonal" density="compact">{{ t('facilityAppsPage.panelHostSetupRequired') }}</v-alert>
                <div v-if="form.panelEntry.enabled" class="field-grid">
                  <v-select v-model="form.panelEntry.serverId" :items="gatewayServerOptions" item-title="title" item-value="value" :label="t('facilityAppsPage.panelHostNode')" variant="outlined" density="comfortable" disabled />
                  <v-text-field v-model="form.panelEntry.domain" :label="t('facilityAppsPage.domain')" variant="outlined" density="comfortable" />
                </div>
              </section>

              <section id="facility-settings-assets" class="editor-section">
                <div><div class="section-title">{{ t('facilityAppsPage.staticAssets') }}</div><div class="section-hint">{{ t('facilityAppsPage.staticAssetsHint') }}</div></div>
                <div class="asset-upload-grid">
                  <v-text-field v-model="uploadForm.name" :label="t('common.name')" variant="outlined" density="comfortable" />
                  <v-select v-model="uploadForm.kind" :items="uploadKindOptions" item-title="title" item-value="value" :label="t('facilityAppsPage.sourceType')" variant="outlined" density="comfortable" />
                  <v-file-input v-model="uploadForm.file" :label="t('common.file')" variant="outlined" density="comfortable" />
                  <AppActionButton icon="mdi-upload" :label="t('facilityAppsPage.upload')" :disabled="!selectedUploadFile()" @click="uploadAsset" />
                </div>
                <div class="asset-list"><div v-for="asset in staticAssets" :key="asset.id" class="asset-item"><div class="min-width-0"><strong class="text-truncate">{{ asset.name }}</strong><div class="section-hint text-truncate">{{ asset.filename }} · {{ formatBytes(asset.size) }}</div></div><AppActionButton kind="danger" icon="mdi-delete-outline" :label="t('common.delete')" @click="openConfirm(t('facilityAppsPage.deleteAssetTitle'), t('facilityAppsPage.deleteAssetConfirm', { name: asset.name }), () => deleteAsset(asset))" /></div></div>
              </section>
            </div>
          </div>
        </v-card-text>
      </v-card>
    </template>

    <v-dialog v-model="domainDialog.open" width="720" :persistent="saving">
      <v-card v-if="domainDialog.draft" class="app-dialog-card"><v-card-title class="app-dialog-title"><span class="app-dialog-title-text">{{ t('facilityAppsPage.domainSettings') }}</span><AppActionButton kind="tool" icon="mdi-close" :label="t('common.close')" @click="closeDomainDialog" /></v-card-title><v-divider /><v-card-text class="app-dialog-body domain-form">
        <v-text-field v-model="domainDialog.draft.domain" :label="t('facilityAppsPage.domain')" variant="outlined" density="comfortable" />
        <v-select v-model="domainDialog.draft.originServerIds" :items="gatewayServerOptions" item-title="title" item-value="value" :label="t('facilityAppsPage.originServers')" :hint="t('facilityAppsPage.originServersHint')" persistent-hint multiple chips closable-chips variant="outlined" density="comfortable" />
        <v-switch v-model="domainDialog.draft.anyAccess.enabled" label="AnyAccess" :hint="t('facilityAppsPage.anyAccessHint')" persistent-hint color="primary" />
        <v-select v-if="domainDialog.draft.anyAccess.enabled" v-model="domainDialog.draft.anyAccess.strategy" :items="strategyOptions" item-title="title" item-value="value" :label="t('facilityAppsPage.trafficStrategy')" variant="outlined" density="comfortable" />
        <v-select v-if="domainDialog.draft.anyAccess.enabled && domainDialog.draft.anyAccess.strategy === 'primary_backup'" v-model="domainDialog.draft.anyAccess.primaryOriginServerId" :items="gatewayServerOptions.filter(item => domainDialog.draft?.originServerIds.includes(item.value))" item-title="title" item-value="value" :label="t('facilityAppsPage.primaryOrigin')" variant="outlined" density="comfortable" />
      </v-card-text><v-divider /><v-card-actions class="app-dialog-actions"><AppActionGroup context="dialog"><AppActionButton kind="plain" :label="t('common.cancel')" @click="closeDomainDialog" /><AppActionButton kind="primary" :label="t('common.save')" @click="saveDomainDialog" /></AppActionGroup></v-card-actions></v-card>
    </v-dialog>

    <v-dialog v-model="pathDialog.open" width="860" :persistent="saving">
      <v-card v-if="pathDialog.draft" class="app-dialog-card"><v-card-title class="app-dialog-title"><span class="app-dialog-title-text">{{ t('facilityAppsPage.pathSettings') }}</span><AppActionButton kind="tool" icon="mdi-close" :label="t('common.close')" @click="closePathDialog" /></v-card-title><v-divider /><v-card-text class="app-dialog-body path-form">
        <v-text-field v-model="pathDialog.draft.path" :label="t('common.path')" variant="outlined" density="comfortable" />
        <v-select v-model="pathDialog.draft.ruleType" :items="routeTypeOptions" item-title="title" item-value="value" :label="t('common.type')" variant="outlined" density="comfortable" />
        <template v-if="pathDialog.draft.ruleType === 'static'"><v-select v-model="pathDialog.draft.sourceType" :items="sourceOptions" item-title="title" item-value="value" :label="t('facilityAppsPage.sourceType')" variant="outlined" density="comfortable" /><v-text-field v-if="pathDialog.draft.sourceType === 'host_path'" v-model="pathDialog.draft.rootPath" :label="t('facilityAppsPage.rootPath')" variant="outlined" density="comfortable" /><v-select v-else v-model="pathDialog.draft.assetId" :items="staticAssetOptions" item-title="title" item-value="value" :label="t('facilityAppsPage.staticAsset')" variant="outlined" density="comfortable" /></template>
        <template v-else-if="pathDialog.draft.ruleType === 'redirect'"><v-text-field v-model="pathDialog.draft.redirectUrl" :label="t('facilityAppsPage.redirectUrl')" variant="outlined" density="comfortable" /><v-select v-model="pathDialog.draft.redirectCode" :items="[301,302,307,308]" :label="t('facilityAppsPage.redirectCode')" variant="outlined" density="comfortable" /></template>
        <template v-else><v-text-field v-model="pathDialog.draft.proxyUrl" :label="t('facilityAppsPage.proxyUrl')" variant="outlined" density="comfortable" /><v-select v-model="pathDialog.draft.proxySourceMode" :items="proxySourceModeOptions" item-title="title" item-value="value" :label="t('facilityAppsPage.proxySourceMode')" variant="outlined" density="comfortable" /></template>
        <RoutePathAdvancedFields v-model="pathDialog.draft.options" :proxy="pathDialog.draft.ruleType === 'proxy_pass'" :gzip="pathDialog.draft.ruleType !== 'redirect'" class="span-all" />
      </v-card-text><v-divider /><v-card-actions class="app-dialog-actions"><AppActionGroup context="dialog"><AppActionButton kind="plain" :label="t('common.cancel')" @click="closePathDialog" /><AppActionButton kind="primary" :label="t('common.save')" @click="savePathDialog" /></AppActionGroup></v-card-actions></v-card>
    </v-dialog>

    <v-dialog v-model="confirmDialog.open" width="440"><v-card class="app-dialog-card"><v-card-title class="app-dialog-title"><span class="app-dialog-title-text">{{ confirmDialog.title }}</span></v-card-title><v-divider /><v-card-text class="app-dialog-body">{{ confirmDialog.message }}</v-card-text><v-divider /><v-card-actions class="app-dialog-actions"><AppActionGroup context="dialog"><AppActionButton kind="plain" :label="t('common.cancel')" @click="closeConfirm" /><AppActionButton kind="danger-primary" :label="t('common.delete')" @click="confirmAction" /></AppActionGroup></v-card-actions></v-card></v-dialog>
    <v-overlay :model-value="saving" contained persistent class="saving-overlay"><v-progress-circular indeterminate color="primary" /><strong>{{ t('facilityAppsPage.saving') }}</strong><span>{{ saveStep }}</span></v-overlay>
    <v-snackbar v-model="snackbar" color="success">{{ message }}</v-snackbar>
  </div>
</template>

<style scoped>
.facility-config-page { display: flex; flex-direction: column; gap: 12px; height: calc(100dvh - var(--lp-header-height, 64px)); min-height: 0; overflow: hidden; position: relative; }
.config-toolbar { display: flex; flex: 0 0 auto; align-items: center; justify-content: space-between; gap: 14px; }
.config-toolbar__leading { display: flex; align-items: center; gap: 12px; min-width: 0; }
.settings-card { display: flex; flex: 1 1 auto; min-height: 0; overflow: hidden; border-color: color-mix(in srgb, var(--lp-border), transparent 28%) !important; background: linear-gradient(180deg, color-mix(in srgb, var(--lp-surface), transparent 2%), color-mix(in srgb, var(--lp-surface-container), transparent 14%)) !important; }
.settings-body { flex: 1 1 auto; min-height: 0; max-height: none; overflow: auto; scroll-behavior: smooth; padding: 18px !important; }
.settings-layout { display: grid; grid-template-columns: 208px minmax(0, 1fr); gap: 18px; align-items: start; }
.editor-section-nav { position: sticky; top: 4px; display: grid; gap: 6px; padding: 10px; border: 1px solid color-mix(in srgb, var(--lp-border), transparent 16%); border-radius: var(--lp-radius-md); background: color-mix(in srgb, var(--lp-surface), transparent 8%); box-shadow: var(--lp-shadow-sm); }
.editor-section-link { display: grid; grid-template-columns: 24px minmax(0, 1fr); gap: 9px; align-items: center; min-height: 40px; padding: 8px 10px; border: 1px solid transparent; border-radius: var(--lp-radius-sm); background: transparent; color: var(--lp-text-muted); text-align: left; cursor: pointer; }
.editor-section-link:hover, .editor-section-link:focus-visible { background: color-mix(in srgb, rgb(var(--v-theme-primary)), transparent 90%); color: rgb(var(--v-theme-on-surface)); outline: none; }
.editor-section-link--active { border-color: color-mix(in srgb, rgb(var(--v-theme-primary)), transparent 58%); background: linear-gradient(90deg, color-mix(in srgb, rgb(var(--v-theme-primary)), transparent 82%), transparent 120%); color: rgb(var(--v-theme-primary)); font-weight: 700; box-shadow: inset 3px 0 0 rgb(var(--v-theme-primary)); }
.settings-flow { display: grid; gap: 22px; min-width: 0; }
.editor-section { display: grid; gap: 14px; min-width: 0; scroll-margin-top: 18px; padding-bottom: 22px; border-bottom: 1px solid color-mix(in srgb, var(--lp-border), transparent 34%); }
.editor-section:last-child { border-bottom: 0; padding-bottom: 0; }
.section-title { font-size: 1rem; font-weight: 750; }
.section-hint { color: var(--lp-text-muted); font-size: .8rem; }
.section-heading, .domain-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
.field-grid, .domain-form, .path-form, .asset-upload-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; align-items: start; }
.domain-form > *, .path-form > .span-all { grid-column: 1 / -1; }
.domain-list, .path-list, .asset-list { display: grid; gap: 10px; }
.domain-item { display: grid; gap: 12px; padding: 12px; border: 1px solid color-mix(in srgb, var(--lp-border), transparent 28%); border-radius: var(--lp-radius-sm); background: color-mix(in srgb, var(--lp-surface), transparent 38%); }
.domain-main { display: grid; gap: 3px; min-width: 0; padding: 0; border: 0; background: transparent; color: inherit; text-align: left; cursor: pointer; }
.domain-main span, .path-copy span, .path-copy small { color: var(--lp-text-muted); font-size: .8rem; overflow-wrap: anywhere; }
.path-item { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 12px; align-items: center; width: 100%; padding: 10px; border-radius: 6px; background: color-mix(in srgb, var(--lp-surface-container), transparent 58%); }
.path-copy { display: grid; gap: 3px; min-width: 0; padding: 0; border: 0; background: transparent; color: inherit; text-align: left; cursor: pointer; }
.asset-upload-grid { grid-template-columns: minmax(0, 1fr) minmax(180px, .6fr) minmax(0, 1fr) auto; }
.asset-item { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 12px; align-items: center; padding: 10px 12px; border-radius: 6px; background: color-mix(in srgb, var(--lp-surface-container), transparent 58%); }
.empty-section { padding: 24px; border: 1px dashed color-mix(in srgb, var(--lp-border), transparent 18%); border-radius: var(--lp-radius-sm); color: var(--lp-text-muted); text-align: center; }
.min-width-0 { min-width: 0; }.mono { font-family: "Cascadia Code", Consolas, monospace; }.saving-overlay :deep(.v-overlay__content) { display: grid; justify-items: center; gap: 10px; padding: 24px; border-radius: var(--lp-radius-md); background: rgb(var(--v-theme-surface)); box-shadow: var(--lp-shadow-2); }.saving-overlay span { color: var(--lp-text-muted); }
@media (max-width: 1180px) { .settings-layout { grid-template-columns: 1fr; }.editor-section-nav { position: static; grid-auto-flow: column; grid-auto-columns: max-content; overflow-x: auto; }.asset-upload-grid { grid-template-columns: 1fr 1fr; } }
@media (max-width: 760px) { .facility-config-page { height: auto; overflow: visible; }.settings-card, .settings-body { overflow: visible; }.config-toolbar, .config-toolbar__leading, .section-heading, .domain-heading { align-items: stretch; flex-direction: column; }.field-grid, .domain-form, .path-form, .asset-upload-grid { grid-template-columns: 1fr; }.path-item { grid-template-columns: 1fr; } }
</style>
