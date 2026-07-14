<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import { onBeforeRouteLeave, useRouter } from 'vue-router';
import { facilityAppsApi } from '@/api/facilityApps';
import { serversApi } from '@/api/servers';
import AppActionButton from '@/components/AppActionButton.vue';
import AppActionGroup from '@/components/AppActionGroup.vue';
import PageLoadingState from '@/components/PageLoadingState.vue';
import RoutePathAdvancedFields from '@/components/RoutePathAdvancedFields.vue';
import type { FacilityDomainPolicyDto, FacilityPanelEntryDto, FacilityReverseProxyConfigDto, FacilityReverseProxySaveDto, FacilityStaticAssetDto, FacilityStaticSiteDto, ServerDto } from '@/types/api';
import { useI18n } from '@/i18n';

type FacilityStaticSiteForm = FacilityStaticSiteDto & { localGroupId: string };
type FacilityDomainPolicyForm = FacilityDomainPolicyDto & { localGroupId: string };
type PendingAssetUpload = { localId: string; name: string; kind: string; file: File };

const router = useRouter();
const { t, formatDateTime, translateLifecycleStage, translateRuntimeDesiredState, translateRuntimeStatus } = useI18n();
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
const uploadingAsset = ref(false);
const uploadForm = reactive({
  name: '',
  kind: 'uploaded_file',
  file: null as File | File[] | null,
});
const form = reactive({
  deploymentServers: [] as string[],
  image: 'nginx:1.27-alpine',
  panelEntry: { enabled: false, serverId: '', domain: '' } as FacilityPanelEntryDto,
  staticSites: [] as FacilityStaticSiteForm[],
  domainPolicies: [] as FacilityDomainPolicyForm[],
});
const routeDialog = reactive({
  open: false,
  index: -1,
  draft: null as FacilityStaticSiteForm | null,
});
const confirmDialog = reactive({
  open: false,
  title: '',
  message: '',
  action: null as null | (() => void),
});
const baselinePayload = ref('');
let localGroupSequence = 0;

const serverOptions = computed(() => servers.value.map((server) => ({
  title: `${server.name} - ${server.host}`,
  value: server.id,
})));
const gatewayServerOptions = computed(() => serverOptions.value.filter((option) => form.deploymentServers.includes(option.value)));
const enabledServerNames = computed(() => form.deploymentServers
  .map((id) => servers.value.find((server) => server.id === id)?.name ?? id)
  .join(', '));
const staticAssetOptions = computed(() => staticAssets.value.map((asset) => ({
  title: `${asset.name} (${asset.filename})`,
  value: asset.id,
})));
const uploadKindOptions = computed(() => [
  { title: t('facilityAppsPage.uploadedFile'), value: 'uploaded_file' },
  { title: t('facilityAppsPage.uploadedBundle'), value: 'uploaded_bundle' },
]);
const sourceOptions = computed(() => [
  { title: t('facilityAppsPage.serverDirectory'), value: 'host_path' },
  { title: t('facilityAppsPage.uploadedFile'), value: 'uploaded_file' },
  { title: t('facilityAppsPage.uploadedBundle'), value: 'uploaded_bundle' },
]);
const routeTypeOptions = computed(() => [
  { title: t('facilityAppsPage.staticContent'), value: 'static' },
  { title: t('facilityAppsPage.redirect'), value: 'redirect' },
  { title: t('facilityAppsPage.proxyPass'), value: 'proxy_pass' },
]);
const redirectCodeOptions = [301, 302, 307, 308];
const proxySourceModeOptions = computed(() => [
  { title: t('facilityAppsPage.preserveSource'), value: 'preserve_source' },
  { title: t('facilityAppsPage.hideSource'), value: 'hide_source' },
]);
const upstreamStrategyOptions = computed(() => [
  { title: t('facilityAppsPage.roundRobin'), value: 'round_robin' },
  { title: t('facilityAppsPage.primaryBackup'), value: 'primary_backup' },
  { title: t('facilityAppsPage.ipHash'), value: 'ip_hash' },
]);
const panelRouteSummary = computed(() => (config.value?.routeSummaries ?? []).find((item) => item.source === 'system_panel'));
const lifecycleTargets = computed(() => config.value?.operation?.targets ?? []);
const staticSiteGroups = computed(() => {
  const groups: Array<{ key: string; domain: string; indexes: number[]; deploymentServers: string[] | null; policy: FacilityDomainPolicyForm | null; sites: Array<{ site: FacilityStaticSiteForm; index: number }> }> = [];
  const byGroupId = new Map<string, number>();
  form.staticSites.forEach((site, index) => {
    const key = site.localGroupId;
    let groupIndex = byGroupId.get(key);
    if (groupIndex === undefined) {
      groupIndex = groups.length;
      byGroupId.set(key, groupIndex);
      groups.push({ key, domain: site.domain?.trim() ?? '', indexes: [], deploymentServers: null, policy: form.domainPolicies.find((item) => item.localGroupId === key) ?? null, sites: [] });
    }
    const group = groups[groupIndex];
    if (!group.domain && site.domain?.trim()) {
      group.domain = site.domain.trim();
    }
    group.indexes.push(index);
    group.sites.push({ site, index });
    group.deploymentServers = mergeDomainServers(group.deploymentServers, site.deploymentServers ?? []);
  });
  return groups.map((group) => ({ ...group, deploymentServers: group.policy?.entryServerIds ?? group.deploymentServers ?? [] }));
});
const dirty = computed(() => pendingAssetUploads.value.length > 0 || deletedAssetIds.value.length > 0 || JSON.stringify(savePayload()) !== baselinePayload.value);

function nextLocalGroupId() {
  localGroupSequence += 1;
  return `draft-${localGroupSequence}`;
}

function mergeDomainServers(current: string[] | null, next: string[]) {
  if (current === null) return [...next].sort();
  if (!current.length || !next.length) return [];
  return [...new Set([...current, ...next])].sort();
}

async function load() {
  loading.value = true;
  try {
    const [serverItems, proxyConfig] = await Promise.all([
      serversApi.listServers(),
      facilityAppsApi.reverseProxy(),
    ]);
    servers.value = serverItems;
    applyConfig(proxyConfig);
    staticAssets.value = proxyConfig.staticAssets ?? await facilityAppsApi.staticAssets();
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('facilityAppsPage.loadFailed');
  } finally {
    loading.value = false;
  }
}

function applyConfig(next: FacilityReverseProxyConfigDto) {
  config.value = next;
  form.deploymentServers = [...(next.deploymentServers ?? [])];
  form.image = next.image || 'nginx:1.27-alpine';
  form.panelEntry = {
    enabled: Boolean(next.panelEntry?.enabled),
    serverId: next.panelEntry?.serverId ?? '',
    domain: next.panelEntry?.domain ?? '',
  };
  staticAssets.value = next.staticAssets ?? staticAssets.value;
  const groupIds = new Map<string, string>();
  form.staticSites = (next.staticSites ?? []).map((site) => ({
    ...site,
    localGroupId: groupIdForStaticSite(site, groupIds),
    ruleType: site.ruleType || 'static',
    sourceType: site.sourceType || 'host_path',
    redirectCode: site.redirectCode || 302,
    proxySourceMode: site.proxySourceMode || 'preserve_source',
    deploymentServers: [...(site.deploymentServers ?? [])],
    options: {
      gzipMode: site.options?.gzipMode || 'inherit',
      clientMaxBodySizeMb: site.options?.clientMaxBodySizeMb || 0,
      connectTimeoutSeconds: site.options?.connectTimeoutSeconds || 0,
      readTimeoutSeconds: site.options?.readTimeoutSeconds || 0,
      sendTimeoutSeconds: site.options?.sendTimeoutSeconds || 0,
      bufferingMode: site.options?.bufferingMode || 'inherit',
      webSocketMode: site.options?.webSocketMode || ((site.ruleType || 'static') === 'proxy_pass' ? 'auto' : 'off'),
      requestHeaders: (site.options?.requestHeaders ?? []).map((header) => ({ ...header })),
      responseHeaders: (site.options?.responseHeaders ?? []).map((header) => ({ ...header })),
    },
  }));
  form.domainPolicies = (next.domainPolicies ?? []).map((policy) => ({
    ...policy,
    localGroupId: groupIds.get(policy.domain) ?? nextLocalGroupId(),
    entryServerIds: [...(policy.entryServerIds ?? [])],
    strategy: policy.strategy || 'round_robin',
    primaryServerId: policy.primaryServerId || '',
  }));
  pendingAssetUploads.value = [];
  deletedAssetIds.value = [];
  baselinePayload.value = JSON.stringify(savePayload());
}

function groupIdForStaticSite(site: FacilityStaticSiteDto, groupIds: Map<string, string>) {
  const key = site.domain?.trim() || nextLocalGroupId();
  let groupId = groupIds.get(key);
  if (!groupId) {
    groupId = nextLocalGroupId();
    groupIds.set(key, groupId);
  }
  return groupId;
}

function newStaticSite(domain = '', deploymentServers: string[] = [], localGroupId = nextLocalGroupId()): FacilityStaticSiteForm {
  return { localGroupId, domain, path: '/', ruleType: 'static', rootPath: '', sourceType: 'host_path', assetId: '', redirectUrl: '', redirectCode: 302, proxyUrl: '', proxySourceMode: 'preserve_source', deploymentServers: [...deploymentServers], options: { gzipMode: 'inherit', bufferingMode: 'inherit', webSocketMode: 'off', requestHeaders: [], responseHeaders: [] } };
}

function addStaticDomain() {
  openNewStaticRouteDialog(newStaticSite());
}

function addStaticRoute(domain: string, deploymentServers: string[], localGroupId: string) {
  openNewStaticRouteDialog(newStaticSite(domain, deploymentServers, localGroupId));
}

function updateStaticDomain(indexes: number[], domain: string, localGroupId: string) {
  indexes.forEach((index) => {
    form.staticSites[index].domain = domain;
    form.staticSites[index].localGroupId = localGroupId;
  });
  const policy = form.domainPolicies.find((item) => item.localGroupId === localGroupId);
  if (policy) policy.domain = domain;
}

function updateStaticDomainServers(indexes: number[], serverIds: string[]) {
  indexes.forEach((index) => {
    form.staticSites[index].deploymentServers = [...serverIds];
  });
  const groupId = form.staticSites[indexes[0]]?.localGroupId;
  const policy = form.domainPolicies.find((item) => item.localGroupId === groupId);
  if (policy) {
    policy.entryServerIds = [...serverIds];
    if (policy.primaryServerId && !serverIds.includes(policy.primaryServerId)) policy.primaryServerId = '';
  }
}

function updateGatewayServers(serverIds: string[]) {
  const allowed = new Set(serverIds);
  form.deploymentServers = [...serverIds];
  form.domainPolicies.forEach((policy) => {
    policy.entryServerIds = policy.entryServerIds.filter((serverId) => allowed.has(serverId));
    if (policy.primaryServerId && !policy.entryServerIds.includes(policy.primaryServerId)) policy.primaryServerId = '';
  });
  form.staticSites.forEach((site) => {
    const policy = form.domainPolicies.find((item) => item.localGroupId === site.localGroupId);
    site.deploymentServers = [...(policy?.entryServerIds ?? site.deploymentServers ?? []).filter((serverId) => allowed.has(serverId))];
  });
  if (form.panelEntry.serverId && !allowed.has(form.panelEntry.serverId)) form.panelEntry.serverId = '';
}

function removeStaticSite(index: number) {
  form.staticSites.splice(index, 1);
  if (routeDialog.index === index) closeStaticRouteDialog();
  else if (routeDialog.index > index) routeDialog.index -= 1;
}

function removeStaticDomain(indexes: number[]) {
  const groupId = form.staticSites[indexes[0]]?.localGroupId;
  [...indexes].sort((a, b) => b - a).forEach((index) => {
    form.staticSites.splice(index, 1);
  });
  form.domainPolicies = form.domainPolicies.filter((item) => item.localGroupId !== groupId);
  closeStaticRouteDialog();
}

function openConfirmDialog(title: string, messageText: string, action: () => void) {
  confirmDialog.title = title;
  confirmDialog.message = messageText;
  confirmDialog.action = action;
  confirmDialog.open = true;
}

function closeConfirmDialog() {
  confirmDialog.open = false;
  confirmDialog.title = '';
  confirmDialog.message = '';
  confirmDialog.action = null;
}

function confirmPendingAction() {
  const action = confirmDialog.action;
  closeConfirmDialog();
  action?.();
}

function requestRemoveStaticSite(index: number) {
  const site = form.staticSites[index];
  openConfirmDialog(t('facilityAppsPage.deleteRouteTitle'), t('facilityAppsPage.deleteRouteConfirm', { path: site?.path || '/' }), () => removeStaticSite(index));
}

function requestRemoveStaticDomain(indexes: number[], domain: string) {
  openConfirmDialog(t('facilityAppsPage.deleteDomainTitle'), t('facilityAppsPage.deleteDomainConfirm', { domain: domain || t('common.notAvailable') }), () => removeStaticDomain(indexes));
}

async function save() {
  const payload = savePayload();
  const validationError = validateSavePayload(payload);
  if (validationError) {
    error.value = validationError;
    return;
  }
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
    const assetIdMap = new Map<string, string>();
    for (const [index, pending] of pendingAssetUploads.value.entries()) {
      saveStep.value = t('facilityAppsPage.saveStepUploadingAsset', { current: index + 1, total: pendingAssetUploads.value.length });
      const uploaded = await facilityAppsApi.uploadSaveSessionAsset(session.id, { name: pending.name, kind: pending.kind, file: pending.file });
      assetIdMap.set(pending.localId, uploaded.id);
    }
    payload.staticSites = payload.staticSites.map((site) => ({ ...site, assetId: site.assetId ? (assetIdMap.get(site.assetId) ?? site.assetId) : '' }));
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

function validateSavePayload(payload: FacilityReverseProxySaveDto) {
  const gatewayNodes = new Set(payload.deploymentServers);
  for (const policy of payload.domainPolicies) {
    if (!policy.domain.trim()) return t('facilityAppsPage.domainRequired');
    if (!policy.entryServerIds.length) return t('facilityAppsPage.selectDomainGatewayNodes');
    if (policy.entryServerIds.some((serverId) => !gatewayNodes.has(serverId))) return t('facilityAppsPage.domainGatewayNodesInvalid');
    if (policy.upstreamMode && policy.strategy === 'primary_backup' && !policy.entryServerIds.includes(policy.primaryServerId || '')) {
      return t('facilityAppsPage.primaryServerRequired');
    }
  }
  return '';
}

function normalizedStaticSitesForSave(): FacilityStaticSiteDto[] {
  return staticSiteGroups.value.flatMap((group) => group.sites.map(({ site }) => {
    const { localGroupId: _localGroupId, ...payload } = site;
    return { ...payload, deploymentServers: [...group.deploymentServers] };
  }));
}

function normalizedDomainPoliciesForSave(): FacilityDomainPolicyDto[] {
  return staticSiteGroups.value.map((group) => ({
    domain: group.domain.trim().toLowerCase(),
    entryServerIds: [...(group.policy?.entryServerIds ?? group.deploymentServers)],
    upstreamMode: Boolean(group.policy?.upstreamMode),
    strategy: group.policy?.strategy || 'round_robin',
    primaryServerId: group.policy?.strategy === 'primary_backup' ? (group.policy.primaryServerId || '') : '',
  }));
}

function savePayload(): FacilityReverseProxySaveDto {
  return {
    deploymentServers: [...form.deploymentServers],
    image: form.image,
    panelEntry: { enabled: Boolean(form.panelEntry.enabled), serverId: form.panelEntry.serverId || '', domain: form.panelEntry.domain || '' },
    staticSites: normalizedStaticSitesForSave(),
    domainPolicies: normalizedDomainPoliciesForSave(),
  };
}

function selectedUploadFile() {
  return Array.isArray(uploadForm.file) ? uploadForm.file[0] : uploadForm.file;
}

async function uploadAsset() {
  const file = selectedUploadFile();
  if (!file) return;
  uploadingAsset.value = true;
  try {
    const localId = `draft-asset-${Date.now()}-${pendingAssetUploads.value.length + 1}`;
    const now = new Date().toISOString();
    const asset = { id: localId, name: uploadForm.name || file.name, kind: uploadForm.kind, filename: file.name, size: file.size, sha256: '', createdAt: now, updatedAt: now } as FacilityStaticAssetDto;
    pendingAssetUploads.value.push({ localId, name: asset.name, kind: asset.kind, file });
    staticAssets.value = [asset, ...staticAssets.value.filter((item) => item.id !== asset.id)];
    uploadForm.name = '';
    uploadForm.file = null;
    message.value = t('facilityAppsPage.assetUploaded');
    snackbar.value = true;
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('facilityAppsPage.assetUploadFailed');
  } finally {
    uploadingAsset.value = false;
  }
}

function deleteAsset(assetId: string) {
  if (assetId.startsWith('draft-asset-')) {
    pendingAssetUploads.value = pendingAssetUploads.value.filter((item) => item.localId !== assetId);
  } else if (!deletedAssetIds.value.includes(assetId)) {
    deletedAssetIds.value.push(assetId);
  }
  staticAssets.value = staticAssets.value.filter((asset) => asset.id !== assetId);
  form.staticSites.forEach((site) => {
    if (site.assetId === assetId) site.assetId = '';
  });
}

function requestDeleteAsset(asset: FacilityStaticAssetDto) {
  openConfirmDialog(t('facilityAppsPage.deleteAssetTitle'), t('facilityAppsPage.deleteAssetConfirm', { name: asset.name }), () => deleteAsset(asset.id));
}

function domainHttpsStatus(domain: string) {
  const summary = (config.value?.routeSummaries ?? []).find((item) => item.domain === domain);
  return summary?.httpsStatus || 'disabled';
}

function routeTypeLabel(site: FacilityStaticSiteDto) {
  const value = site.ruleType || 'static';
  if (value === 'redirect') return t('facilityAppsPage.redirect');
  if (value === 'proxy_pass') return t('facilityAppsPage.proxyPass');
  return t('facilityAppsPage.staticContent');
}

function openStaticRouteDialog(index: number) {
  routeDialog.index = index;
  routeDialog.draft = structuredClone(form.staticSites[index]);
  routeDialog.open = true;
}

function openNewStaticRouteDialog(route: FacilityStaticSiteForm) {
  routeDialog.index = -1;
  routeDialog.draft = structuredClone(route);
  routeDialog.open = true;
}

function closeStaticRouteDialog() {
  routeDialog.open = false;
  routeDialog.index = -1;
  routeDialog.draft = null;
}

function saveStaticRouteDialog() {
  if (!routeDialog.draft) return;
  const draft = structuredClone(routeDialog.draft);
  const groupIndexes = form.staticSites.map((site, index) => (site.localGroupId === draft.localGroupId ? index : -1)).filter((index) => index >= 0);
  groupIndexes.forEach((index) => { form.staticSites[index].domain = draft.domain; });
  let policy = form.domainPolicies.find((item) => item.localGroupId === draft.localGroupId);
  if (!policy) {
    policy = { localGroupId: draft.localGroupId, domain: draft.domain, entryServerIds: [...(draft.deploymentServers ?? [])], upstreamMode: false, strategy: 'round_robin', primaryServerId: '' };
    form.domainPolicies.push(policy);
  } else {
    policy.domain = draft.domain;
  }
  draft.deploymentServers = [...policy.entryServerIds];
  if (routeDialog.index >= 0) form.staticSites[routeDialog.index] = draft;
  else form.staticSites.push(draft);
  closeStaticRouteDialog();
}

function httpsStatusColor(status: string) {
  if (status === 'domain_certificate') return 'success';
  if (status === 'self_signed_certificate') return 'warning';
  return undefined;
}

function runtimeStatusColor(status?: string | null) {
  switch (status) {
    case 'running':
    case 'deployed':
      return 'success';
    case 'failed':
    case 'partially_deployed':
      return 'error';
    case 'deploying':
    case 'pending':
    case 'preparing':
    case 'missing':
      return 'warning';
    case 'stopped':
      return 'secondary';
    default:
      return 'primary';
  }
}

function httpsStatusLabel(status: string) {
  if (status === 'domain_certificate') return t('facilityAppsPage.validCertificate');
  if (status === 'self_signed_certificate') return t('facilityAppsPage.selfSignedCertificate');
  return t('facilityAppsPage.httpsDisabled');
}

function backToFacility() {
  void router.push('/applications/facility-apps');
}

function formatBytes(value: number) {
  if (value >= 1024 * 1024) return `${(value / 1024 / 1024).toFixed(1)} MB`;
  if (value >= 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${value} B`;
}

function handleBeforeUnload(event: BeforeUnloadEvent) {
  if (!dirty.value && !saving.value) return;
  event.preventDefault();
  event.returnValue = '';
}

onMounted(() => {
  window.addEventListener('beforeunload', handleBeforeUnload);
  void load();
});
onBeforeUnmount(() => window.removeEventListener('beforeunload', handleBeforeUnload));

onBeforeRouteLeave(() => {
  if (saving.value) return false;
  if (!dirty.value) return true;
  return window.confirm(t('facilityAppsPage.discardChangesConfirm'));
});
</script>

<template>
  <div class="page-shell facility-config-page">
    <v-alert v-if="error" type="error" variant="tonal">{{ error }}</v-alert>
    <PageLoadingState v-if="loading" />
    <template v-else-if="config">
      <div class="config-page-toolbar">
        <AppActionButton kind="plain" icon="mdi-arrow-left" :label="t('facilityAppsPage.backToFacility')" :disabled="saving" @click="backToFacility" />
        <v-chip v-if="dirty" color="warning" size="small" variant="tonal" label>{{ t('facilityAppsPage.unsavedChanges') }}</v-chip>
      </div>
      <v-card variant="outlined" class="facility-detail">
        <div class="facility-detail__header">
          <div class="min-width-0">
            <div class="text-h6 font-weight-bold text-truncate">{{ t('facilityAppsPage.reverseProxy') }}</div>
            <div class="text-body-2 text-medium-emphasis text-truncate">
              {{ enabledServerNames || t('facilityAppsPage.noEnabledServers') }}
            </div>
            <div class="facility-statuses">
              <v-chip :color="form.deploymentServers.length ? 'success' : 'grey'" size="small" variant="tonal" label>
                {{ form.deploymentServers.length ? t('common.enabled') : t('common.disabled') }}
              </v-chip>
              <v-chip size="small" variant="tonal" label>{{ t('facilityAppsPage.gatewayNodes') }} {{ form.deploymentServers.length }}</v-chip>
            </div>
          </div>
          <AppActionGroup context="detail">
            <AppActionButton kind="primary" icon="mdi-content-save" :label="t('facilityAppsPage.saveAndApply')" :loading="saving" :disabled="!dirty || saving" @click="save" />
          </AppActionGroup>
        </div>
        <div class="facility-detail__body">
          <v-alert v-if="config?.lastError" type="warning" variant="tonal" density="compact" class="facility-alert">
            {{ config.lastError }}
          </v-alert>
          <div class="facility-summary">
            <div>
              <span>{{ t('facilityAppsPage.enabledServers') }}</span>
              <strong>{{ form.deploymentServers.length }}</strong>
            </div>
            <div>
              <span>{{ t('facilityAppsPage.proxyRoutes') }}</span>
              <strong>{{ config?.routes ?? 0 }}</strong>
            </div>
            <div>
              <span>{{ t('common.updatedAt') }}</span>
              <strong>{{ config ? formatDateTime(config.updatedAt) : t('common.never') }}</strong>
            </div>
          </div>

          <div class="facility-content-grid">
          <section class="facility-section facility-section--routes">
            <div class="section-heading">
              <div class="section-title">{{ t('facilityAppsPage.routes') }}</div>
              <AppActionButton icon="mdi-plus" :label="t('facilityAppsPage.addDomain')" @click="addStaticDomain" />
            </div>
            <div v-if="!form.staticSites.length" class="empty-inline">
              {{ t('facilityAppsPage.noStaticSites') }}
            </div>
            <div v-for="group in staticSiteGroups" :key="group.key" class="static-domain-card">
              <div class="static-domain-card__header">
                <v-text-field
                  :model-value="group.domain"
                  :label="t('facilityAppsPage.domain')"
                  variant="outlined"
                  density="compact"
                  hide-details="auto"
                  @update:model-value="updateStaticDomain(group.indexes, String($event ?? ''), group.key)"
                />
                <v-select
                  :model-value="group.deploymentServers"
                  :items="gatewayServerOptions"
                  :label="t('facilityAppsPage.domainGatewayNodes')"
                  multiple
                  chips
                  closable-chips
                  variant="outlined"
                  density="compact"
                  hide-details="auto"
                  :placeholder="t('facilityAppsPage.selectDomainGatewayNodes')"
                  @update:model-value="updateStaticDomainServers(group.indexes, $event as string[])"
                />
                <div class="domain-https-status">
                  <v-chip
                    size="small"
                    :color="httpsStatusColor(domainHttpsStatus(group.domain))"
                    variant="tonal"
                    label
                  >
                    {{ httpsStatusLabel(domainHttpsStatus(group.domain)) }}
                  </v-chip>
                </div>
                <AppActionButton kind="danger" icon="mdi-delete-outline" :label="t('common.remove')" @click="requestRemoveStaticDomain(group.indexes, group.domain)" />
              </div>
              <div v-if="group.policy" class="domain-upstream-settings">
                <v-switch v-model="group.policy.upstreamMode" :label="t('facilityAppsPage.upstreamMode')" color="primary" hide-details />
                <v-select
                  v-if="group.policy.upstreamMode"
                  v-model="group.policy.strategy"
                  :items="upstreamStrategyOptions"
                  :label="t('facilityAppsPage.upstreamStrategy')"
                  variant="outlined"
                  density="compact"
                  hide-details="auto"
                />
                <v-select
                  v-if="group.policy.upstreamMode && group.policy.strategy === 'primary_backup'"
                  v-model="group.policy.primaryServerId"
                  :items="gatewayServerOptions.filter((item) => group.policy?.entryServerIds.includes(item.value))"
                  :label="t('facilityAppsPage.primaryServer')"
                  variant="outlined"
                  density="compact"
                  hide-details="auto"
                />
                <div v-if="group.policy.upstreamMode" class="text-caption text-medium-emphasis">
                  {{ t('facilityAppsPage.relayNodesHint', { count: Math.max(0, form.deploymentServers.length - group.policy.entryServerIds.length) }) }}
                </div>
              </div>
              <div class="static-route-list">
                <div
                  v-for="{ site, index } in group.sites"
                  :key="index"
                  class="static-route-list-row"
                >
                  <button type="button" class="static-route-list-row__main" @click="openStaticRouteDialog(index)">
                    <span class="static-route-path text-truncate">{{ site.path || '/' }}</span>
                    <v-chip size="small" variant="tonal" label>{{ routeTypeLabel(site) }}</v-chip>
                    <span class="static-route-target text-truncate">
                      <template v-if="(site.ruleType || 'static') === 'static'">
                        {{ (site.sourceType || 'host_path') === 'host_path' ? (site.rootPath || t('facilityAppsPage.serverDirectory')) : (staticAssets.find((asset) => asset.id === site.assetId)?.name || t('facilityAppsPage.siteContent')) }}
                      </template>
                      <template v-else-if="site.ruleType === 'redirect'">
                        {{ site.redirectUrl || t('facilityAppsPage.redirectTarget') }}
                      </template>
                      <template v-else>
                        {{ site.proxyUrl || t('facilityAppsPage.upstreamUrl') }}
                      </template>
                    </span>
                  </button>
                  <AppActionGroup context="table" class="static-route-list-row__actions">
                    <AppActionButton icon="mdi-pencil-outline" :label="t('common.edit')" @click="openStaticRouteDialog(index)" />
                    <AppActionButton kind="danger" icon="mdi-delete-outline" :label="t('common.delete')" @click="requestRemoveStaticSite(index)" />
                  </AppActionGroup>
                </div>
              </div>
              <AppActionGroup context="section" align="start" class="static-domain-card__actions">
                <AppActionButton icon="mdi-plus" :label="t('facilityAppsPage.addRoute')" @click="addStaticRoute(group.domain, group.deploymentServers, group.key)" />
              </AppActionGroup>
            </div>
          </section>

          <section class="facility-section facility-section--setup">
            <div class="facility-setup-grid">
              <div class="facility-setup-card">
                <div class="section-title">{{ t('facilityAppsPage.gatewayNodes') }}</div>
                <v-select
                  :model-value="form.deploymentServers"
                  :items="serverOptions"
                  :label="t('facilityAppsPage.gatewayNodes')"
                  multiple
                  chips
                  closable-chips
                  variant="outlined"
                  density="comfortable"
                  hide-details="auto"
                  @update:model-value="updateGatewayServers($event as string[])"
                />
                <v-text-field
                  v-model="form.image"
                  :label="t('facilityAppsPage.nginxImage')"
                  variant="outlined"
                  density="comfortable"
                  hide-details="auto"
                />
              </div>

              <div class="facility-setup-card">
                <div class="section-title">{{ t('facilityAppsPage.panelEntry') }}</div>
                <v-switch
                  v-model="form.panelEntry.enabled"
                  :label="t('facilityAppsPage.enablePanelEntry')"
                  color="primary"
                  hide-details
                />
                <div class="panel-entry-grid">
                  <v-select
                    v-model="form.panelEntry.serverId"
                    :items="gatewayServerOptions"
                    :label="t('facilityAppsPage.panelHost')"
                    variant="outlined"
                    density="comfortable"
                    hide-details="auto"
                    :disabled="!form.panelEntry.enabled"
                  />
                  <v-text-field
                    v-model="form.panelEntry.domain"
                    :label="t('facilityAppsPage.panelDomain')"
                    variant="outlined"
                    density="comfortable"
                    hide-details="auto"
                    :disabled="!form.panelEntry.enabled"
                  />
                  <div class="domain-https-status">
                    <v-chip
                      size="small"
                      :color="httpsStatusColor(panelRouteSummary?.httpsStatus || 'disabled')"
                      variant="tonal"
                      label
                    >
                      {{ httpsStatusLabel(panelRouteSummary?.httpsStatus || 'disabled') }}
                    </v-chip>
                  </div>
                </div>
                <div class="text-caption text-medium-emphasis">
                  {{ t('facilityAppsPage.panelEntryHint') }}
                </div>
              </div>
            </div>
          </section>

          <section class="facility-section facility-section--assets">
            <div class="section-title">{{ t('facilityAppsPage.siteContent') }}</div>
            <div class="asset-upload-row">
              <v-text-field v-model="uploadForm.name" :label="t('common.name')" variant="outlined" density="compact" hide-details="auto" />
              <v-select v-model="uploadForm.kind" :items="uploadKindOptions" :label="t('common.type')" variant="outlined" density="compact" hide-details="auto" />
              <v-file-input v-model="uploadForm.file" :label="uploadForm.kind === 'uploaded_bundle' ? t('facilityAppsPage.bundleFile') : t('facilityAppsPage.singleFile')" variant="outlined" density="compact" hide-details="auto" />
              <AppActionButton kind="primary" icon="mdi-upload" :label="t('facilityAppsPage.upload')" :loading="uploadingAsset" :disabled="!selectedUploadFile()" @click="uploadAsset" />
            </div>
            <div v-if="staticAssets.length" class="asset-list">
              <div v-for="asset in staticAssets" :key="asset.id" class="asset-item">
                <span class="text-truncate">{{ asset.name }}</span>
                <v-chip size="small" variant="tonal" label>{{ asset.kind === 'uploaded_bundle' ? t('facilityAppsPage.uploadedBundle') : t('facilityAppsPage.uploadedFile') }}</v-chip>
                <span class="text-caption text-medium-emphasis">{{ formatBytes(asset.size) }}</span>
                <AppActionButton kind="danger" icon="mdi-delete-outline" :label="t('common.delete')" @click="requestDeleteAsset(asset)" />
              </div>
            </div>
          </section>

          <section class="facility-section facility-section--deployment">
            <div class="section-heading">
              <div class="min-width-0">
                <div class="section-title">{{ t('facilityAppsPage.deploymentRecords') }}</div>
                <div v-if="config?.operation" class="section-subtitle text-truncate mono">{{ config.operation.id }}</div>
              </div>
              <v-chip v-if="config?.operation" :color="runtimeStatusColor(config.operation.status)" size="small" variant="tonal" label>
                {{ translateRuntimeStatus(config.operation.status) }}
              </v-chip>
            </div>
            <div v-if="!config?.operation" class="empty-inline">
              {{ t('facilityAppsPage.noDeploymentRecords') }}
            </div>
            <div v-else class="deployment-record">
              <div class="deployment-record__updated">
                {{ formatDateTime(config.operation.finishedAt || config.operation.updatedAt) }}
              </div>
              <div v-if="lifecycleTargets.length" class="deployment-target-list">
                <div v-for="target in lifecycleTargets" :key="target.id" class="deployment-target-row">
                  <div class="deployment-target-row__top">
                    <div class="min-width-0">
                      <div class="font-weight-bold text-truncate">{{ target.serverName || target.serverId }}</div>
                      <div v-if="target.serverName" class="text-caption text-medium-emphasis mono text-truncate">{{ target.serverId }}</div>
                    </div>
                    <v-chip :color="runtimeStatusColor(target.status)" size="small" variant="tonal" label>
                      {{ translateRuntimeStatus(target.status) }}
                    </v-chip>
                  </div>
                  <div class="deployment-target-row__meta">
                    <span>{{ translateRuntimeDesiredState(target.desiredState) }}</span>
                    <span>{{ target.stage ? translateLifecycleStage(target.stage) : t('common.notAvailable') }}</span>
                    <span class="mono text-truncate">{{ target.containerName || target.instanceId || t('common.notAvailable') }}</span>
                    <span>{{ formatDateTime(target.finishedAt || target.updatedAt) }}</span>
                  </div>
                  <div v-if="target.error" class="text-caption text-error">{{ target.error }}</div>
                </div>
              </div>
              <div v-else class="empty-inline">{{ t('facilityAppsPage.noDeploymentTargets') }}</div>
            </div>
          </section>

          </div>
        </div>
        <v-overlay :model-value="saving" contained persistent class="facility-save-overlay">
          <div class="facility-save-progress">
            <v-progress-circular indeterminate color="primary" size="38" />
            <div class="font-weight-medium">{{ saveStep || t('facilityAppsPage.saveStepCommitting') }}</div>
          </div>
        </v-overlay>
      </v-card>
    </template>
    <v-dialog v-model="routeDialog.open" width="min(880px, calc(100vw - 32px))" :persistent="saving">
      <v-card v-if="routeDialog.draft" class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('facilityAppsPage.routes') }}</span>
          <AppActionButton kind="tool" icon="mdi-close" :label="t('common.cancel')" @click="closeStaticRouteDialog" />
        </v-card-title>
        <v-card-text class="app-dialog-body">
          <div class="static-route-dialog-form">
            <v-text-field v-model="routeDialog.draft.domain" :label="t('facilityAppsPage.domain')" variant="outlined" density="comfortable" hide-details="auto" />
            <v-text-field v-model="routeDialog.draft.path" :label="t('common.path')" variant="outlined" density="comfortable" hide-details="auto" />
            <v-select v-model="routeDialog.draft.ruleType" :items="routeTypeOptions" :label="t('facilityAppsPage.ruleType')" variant="outlined" density="comfortable" hide-details="auto" />
            <template v-if="(routeDialog.draft.ruleType || 'static') === 'static'">
              <v-select v-model="routeDialog.draft.sourceType" :items="sourceOptions" :label="t('facilityAppsPage.contentSource')" variant="outlined" density="comfortable" hide-details="auto" />
              <v-text-field v-if="(routeDialog.draft.sourceType || 'host_path') === 'host_path'" v-model="routeDialog.draft.rootPath" :label="t('facilityAppsPage.serverDirectory')" variant="outlined" density="comfortable" hide-details="auto" />
              <v-select v-else v-model="routeDialog.draft.assetId" :items="staticAssetOptions" :label="t('facilityAppsPage.siteContent')" variant="outlined" density="comfortable" hide-details="auto" />
            </template>
            <template v-else-if="routeDialog.draft.ruleType === 'redirect'">
              <v-text-field v-model="routeDialog.draft.redirectUrl" :label="t('facilityAppsPage.redirectTarget')" variant="outlined" density="comfortable" hide-details="auto" />
              <v-select v-model="routeDialog.draft.redirectCode" :items="redirectCodeOptions" :label="t('facilityAppsPage.redirectCode')" variant="outlined" density="comfortable" hide-details="auto" />
            </template>
            <template v-else>
              <v-text-field v-model="routeDialog.draft.proxyUrl" :label="t('facilityAppsPage.upstreamUrl')" variant="outlined" density="comfortable" hide-details="auto" />
              <v-select v-model="routeDialog.draft.proxySourceMode" :items="proxySourceModeOptions" :label="t('facilityAppsPage.requestInfo')" variant="outlined" density="comfortable" hide-details="auto" />
            </template>
          </div>
          <RoutePathAdvancedFields
            v-model="routeDialog.draft.options"
            :proxy="routeDialog.draft.ruleType === 'proxy_pass'"
            :gzip="routeDialog.draft.ruleType === 'static' || routeDialog.draft.ruleType === 'proxy_pass'"
          />
        </v-card-text>
        <v-card-actions class="app-dialog-actions">
          <AppActionGroup context="dialog">
            <AppActionButton kind="plain" :label="t('common.cancel')" @click="closeStaticRouteDialog" />
            <AppActionButton kind="primary" :label="t('common.save')" @click="saveStaticRouteDialog" />
          </AppActionGroup>
        </v-card-actions>
      </v-card>
    </v-dialog>
    <v-dialog v-model="confirmDialog.open" width="420" :persistent="saving">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ confirmDialog.title }}</span>
          <AppActionButton kind="tool" icon="mdi-close" :label="t('common.cancel')" :disabled="saving" @click="closeConfirmDialog" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          <p>{{ confirmDialog.message }}</p>
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <AppActionGroup context="dialog">
            <AppActionButton kind="plain" :label="t('common.cancel')" :disabled="saving" @click="closeConfirmDialog" />
            <AppActionButton kind="danger-primary" :label="t('common.delete')" :disabled="saving" @click="confirmPendingAction" />
          </AppActionGroup>
        </v-card-actions>
      </v-card>
    </v-dialog>
    <v-snackbar v-model="snackbar">{{ message }}</v-snackbar>
  </div>
</template>

<style scoped>
.facility-config-page { gap: 12px; overflow: hidden; }
.config-page-toolbar { display: flex; flex: 0 0 auto; align-items: center; justify-content: space-between; gap: 10px; padding: 2px 4px; }
.facility-list,
.facility-detail {
  position: relative;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
}

.facility-save-overlay {
  align-items: center;
  justify-content: center;
  backdrop-filter: blur(2px);
}

.facility-save-progress {
  display: grid;
  justify-items: center;
  gap: 12px;
  max-width: min(420px, calc(100vw - 48px));
  padding: 20px 24px;
  border: 1px solid color-mix(in srgb, var(--lp-border), transparent 20%);
  border-radius: var(--lp-radius-md);
  background: rgb(var(--v-theme-surface));
  box-shadow: var(--lp-shadow-md);
  text-align: center;
}

.facility-detail {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  border-color: color-mix(in srgb, var(--lp-border), transparent 28%) !important;
  background:
    radial-gradient(circle at 18% 0%, color-mix(in srgb, rgb(var(--v-theme-primary)), transparent 90%), transparent 26rem),
    linear-gradient(180deg, color-mix(in srgb, var(--lp-surface), transparent 2%), color-mix(in srgb, var(--lp-surface-container), transparent 14%)) !important;
  box-shadow: 0 18px 48px color-mix(in srgb, var(--lp-background), transparent 24%) !important;
}

.section-heading {
  display: flex;
  align-items: center;
  gap: 8px;
}

.facility-detail__header {
  display: flex;
  flex: 0 0 auto;
  align-items: flex-start;
  justify-content: space-between;
  gap: 14px;
  padding: 18px 22px 16px;
  border-bottom: 1px solid color-mix(in srgb, var(--lp-border), transparent 18%);
  background: color-mix(in srgb, var(--lp-surface), transparent 8%);
}

.facility-statuses {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 8px;
}

.facility-detail__body {
  display: grid;
  flex: 1 1 auto;
  gap: 22px;
  align-content: start;
  justify-content: stretch;
  min-height: 0;
  padding: 16px 18px 18px;
  overflow: auto;
  scroll-behavior: smooth;
}

.facility-alert,
.facility-summary,
.facility-content-grid {
  width: 100%;
  justify-self: stretch;
}

.facility-summary {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
}

.facility-content-grid {
  display: grid;
  gap: 22px;
  grid-template-columns: minmax(0, 1fr);
  align-content: start;
  min-width: 0;
}

.facility-summary > div {
  display: grid;
  gap: 4px;
  min-width: 0;
  padding: 12px 14px;
  border: 0;
  border-radius: 6px;
  background: color-mix(in srgb, var(--lp-surface-container), transparent 62%);
}

.facility-summary span {
  color: var(--lp-text-muted);
  font-size: 0.76rem;
}

.facility-summary strong {
  min-width: 0;
  overflow: hidden;
  font-size: 0.95rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.facility-section {
  position: relative;
  display: grid;
  gap: 14px;
  align-content: start;
  min-width: 0;
  overflow: visible;
  padding: 0 0 22px;
  border-bottom: 1px solid color-mix(in srgb, var(--lp-border), transparent 34%);
}

.facility-section--routes {
  gap: 18px;
  padding-bottom: 24px;
}

.facility-section--setup,
.facility-section--assets,
.facility-section--deployment {
  background: transparent;
  box-shadow: none;
}

.facility-section::before {
  content: none;
}

.facility-content-grid > .facility-section:last-child {
  padding-bottom: 0;
  border-bottom: 0;
}

.deployment-record {
  display: grid;
  gap: 8px;
}

.deployment-record__updated {
  color: var(--lp-text-muted);
  font-size: 0.78rem;
  font-variant-numeric: tabular-nums;
}

.deployment-target-list {
  display: grid;
  gap: 8px;
}

.deployment-target-row {
  display: grid;
  gap: 7px;
  min-width: 0;
  padding: 10px 12px;
  border: 0;
  border-radius: 6px;
  background: color-mix(in srgb, var(--lp-surface-container), transparent 62%);
}

.deployment-target-row__top {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
  align-items: center;
}

.deployment-target-row__meta {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 5px 8px;
  color: var(--lp-text-muted);
  font-size: 0.74rem;
}

.deployment-target-row__meta > span {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.section-heading {
  justify-content: space-between;
}

.section-title {
  min-width: 0;
  font-size: 1rem;
  font-weight: 750;
}

.section-subtitle {
  margin-top: 2px;
  color: var(--lp-text-muted);
  font-size: 0.78rem;
}

.static-domain-card {
  display: grid;
  gap: 18px;
  padding: 16px 0 0;
  border-top: 1px solid color-mix(in srgb, var(--lp-border), transparent 42%);
  background: transparent;
}

.static-domain-card:first-of-type {
  padding-top: 0;
  border-top: 0;
}

.facility-setup-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
  align-items: start;
}

.facility-setup-card {
  display: grid;
  gap: 12px;
  padding: 12px;
  border: 0;
  border-radius: 6px;
  background: color-mix(in srgb, var(--lp-surface-container), transparent 62%);
}

.panel-entry-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 8px;
  align-items: start;
}

.static-domain-card__header {
  display: grid;
  grid-template-columns: minmax(220px, 1fr) minmax(260px, 1.25fr) auto 40px;
  gap: 12px;
  align-items: start;
}

.domain-upstream-settings {
  display: grid;
  grid-template-columns: minmax(220px, .7fr) minmax(220px, 1fr) minmax(220px, 1fr);
  gap: 10px;
  align-items: center;
  padding: 10px 12px;
  border-radius: var(--lp-radius-sm);
  background: color-mix(in srgb, var(--lp-surface-container), transparent 58%);
}

.domain-https-status {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  min-height: 40px;
  align-items: center;
}

.static-domain-card__actions {
  display: flex;
  align-items: center;
  justify-content: flex-start;
  gap: 8px;
  padding-top: 2px;
}

.static-route-list {
  display: grid;
  gap: 8px;
}

.static-route-list-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 12px;
  align-items: center;
  min-width: 0;
  padding: 8px 10px;
  border: 1px solid color-mix(in srgb, var(--lp-border), transparent 28%);
  border-radius: var(--lp-radius-sm);
  background: color-mix(in srgb, var(--lp-surface), transparent 38%);
}

.static-route-list-row__main {
  display: grid;
  grid-template-columns: minmax(110px, 0.38fr) auto minmax(0, 1fr);
  gap: 10px;
  align-items: center;
  min-width: 0;
  border: 0;
  background: transparent;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
}

.static-route-list-row__main:focus-visible {
  outline: 2px solid color-mix(in srgb, rgb(var(--v-theme-primary)), transparent 42%);
  outline-offset: 3px;
  border-radius: var(--lp-radius-sm);
}

.static-route-path {
  min-width: 0;
  font-weight: 760;
}

.static-route-target {
  min-width: 0;
  color: var(--lp-text-muted);
  font-size: 0.82rem;
}

.static-route-dialog-form {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
  align-items: start;
}

.static-route-dialog-form > *:last-child:nth-child(odd) {
  grid-column: 1 / -1;
}

.asset-upload-row {
  display: grid;
  grid-template-columns: minmax(140px, 1fr) minmax(150px, 0.8fr) minmax(220px, 1.2fr) auto;
  gap: 8px;
  align-items: start;
}

.asset-list {
  display: grid;
  gap: 8px;
}

.asset-item {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto auto auto;
  align-items: center;
  gap: 10px;
  min-width: 0;
  padding: 8px 10px;
  border: 0;
  border-radius: 6px;
  background: color-mix(in srgb, var(--lp-surface-container), transparent 66%);
}

.empty-inline {
  padding: 14px;
  border: 1px dashed var(--lp-border);
  border-radius: 8px;
  color: var(--lp-text-muted);
  font-size: 0.88rem;
}

.min-width-0 {
  min-width: 0;
}

.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 0.78rem;
}

@media (max-width: 1280px) {
  .static-route-list-row,
  .static-route-list-row__main,
  .asset-upload-row {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 1080px) {
  .facility-content-grid {
    grid-template-columns: minmax(0, 1fr);
  }

  .facility-setup-grid,
  .static-domain-card__header,
  .domain-upstream-settings {
    grid-template-columns: minmax(0, 1fr);
  }
}

@media (max-width: 760px) {
  .facility-list,
  .facility-detail,
  .facility-detail__body {
    overflow: visible;
  }

  .facility-detail__header,
  .facility-detail__body {
    padding: 16px;
  }

  .facility-section {
    padding: 0 0 18px;
  }

  .facility-summary,
  .facility-setup-grid,
  .panel-entry-grid,
  .static-domain-card__header,
  .static-route-dialog-form,
  .static-route-list-row,
  .static-route-list-row__main,
  .asset-upload-row,
  .asset-item {
    grid-template-columns: 1fr;
  }

  .section-heading,
  .static-domain-card__actions {
    align-items: stretch;
    flex-direction: column;
  }
}
</style>
