<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { facilityAppsApi } from '@/api/facilityApps';
import { serversApi } from '@/api/servers';
import AppActionButton from '@/components/AppActionButton.vue';
import AppActionGroup from '@/components/AppActionGroup.vue';
import AppMasterDetailWorkspace from '@/components/AppMasterDetailWorkspace.vue';
import AppSelectorPanel from '@/components/AppSelectorPanel.vue';
import AppSelectorSummaryItem from '@/components/AppSelectorSummaryItem.vue';
import PageLoadingState from '@/components/PageLoadingState.vue';
import type { FacilityPanelEntryDto, FacilityReverseProxyConfigDto, FacilityStaticAssetDto, FacilityStaticSiteDto, ServerDto } from '@/types/api';
import { useI18n } from '@/i18n';

type FacilityStaticSiteForm = FacilityStaticSiteDto & { localGroupId: string };

const { t, formatDateTime, translateLifecycleStage, translateRuntimeDesiredState, translateRuntimeStatus } = useI18n();
const loading = ref(true);
const saving = ref(false);
const reconciling = ref(false);
const error = ref('');
const snackbar = ref(false);
const message = ref('');
const servers = ref<ServerDto[]>([]);
const config = ref<FacilityReverseProxyConfigDto | null>(null);
const staticAssets = ref<FacilityStaticAssetDto[]>([]);
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
});
const routeDialog = reactive({
  open: false,
  index: -1,
});
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
const panelRouteSummary = computed(() => (config.value?.routeSummaries ?? []).find((item) => item.source === 'system_panel'));
const lifecycleTargets = computed(() => config.value?.operation?.targets ?? []);
const facilityApplications = computed(() => [
  {
    id: 'reverse-proxy',
    title: t('facilityAppsPage.reverseProxy'),
    hint: t('facilityAppsPage.reverseProxyHint'),
    enabled: form.deploymentServers.length > 0,
  },
]);
const staticSiteGroups = computed(() => {
  const groups: Array<{ key: string; domain: string; indexes: number[]; deploymentServers: string[] | null; sites: Array<{ site: FacilityStaticSiteForm; index: number }> }> = [];
  const byGroupId = new Map<string, number>();
  form.staticSites.forEach((site, index) => {
    const key = site.localGroupId;
    let groupIndex = byGroupId.get(key);
    if (groupIndex === undefined) {
      groupIndex = groups.length;
      byGroupId.set(key, groupIndex);
      groups.push({ key, domain: site.domain?.trim() ?? '', indexes: [], deploymentServers: null, sites: [] });
    }
    const group = groups[groupIndex];
    if (!group.domain && site.domain?.trim()) {
      group.domain = site.domain.trim();
    }
    group.indexes.push(index);
    group.sites.push({ site, index });
    group.deploymentServers = mergeDomainServers(group.deploymentServers, site.deploymentServers ?? []);
  });
  return groups.map((group) => ({ ...group, deploymentServers: group.deploymentServers ?? [] }));
});

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
  }));
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
  return { localGroupId, domain, path: '/', ruleType: 'static', rootPath: '', sourceType: 'host_path', assetId: '', redirectUrl: '', redirectCode: 302, proxyUrl: '', proxySourceMode: 'preserve_source', deploymentServers: [...deploymentServers] };
}

function addStaticDomain() {
  const route = newStaticSite();
  form.staticSites.push(route);
  openStaticRouteDialog(form.staticSites.length - 1);
}

function addStaticRoute(domain: string, deploymentServers: string[], localGroupId: string) {
  form.staticSites.push(newStaticSite(domain, deploymentServers, localGroupId));
  openStaticRouteDialog(form.staticSites.length - 1);
}

function updateStaticDomain(indexes: number[], domain: string, localGroupId: string) {
  indexes.forEach((index) => {
    form.staticSites[index].domain = domain;
    form.staticSites[index].localGroupId = localGroupId;
  });
}

function updateStaticDomainServers(indexes: number[], serverIds: string[]) {
  indexes.forEach((index) => {
    form.staticSites[index].deploymentServers = [...serverIds];
  });
}

function removeStaticSite(index: number) {
  form.staticSites.splice(index, 1);
  if (routeDialog.index === index) closeStaticRouteDialog();
  else if (routeDialog.index > index) routeDialog.index -= 1;
}

function removeStaticDomain(indexes: number[]) {
  [...indexes].sort((a, b) => b - a).forEach((index) => {
    form.staticSites.splice(index, 1);
  });
  closeStaticRouteDialog();
}

async function save() {
  saving.value = true;
  try {
    const next = await facilityAppsApi.saveReverseProxy({
      deploymentServers: form.deploymentServers,
      image: form.image,
      panelEntry: {
        enabled: Boolean(form.panelEntry.enabled),
        serverId: form.panelEntry.serverId || '',
        domain: form.panelEntry.domain || '',
      },
      staticSites: normalizedStaticSitesForSave(),
    });
    applyConfig(next);
    message.value = t('facilityAppsPage.saved');
    snackbar.value = true;
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('facilityAppsPage.saveFailed');
  } finally {
    saving.value = false;
  }
}

function normalizedStaticSitesForSave(): FacilityStaticSiteDto[] {
  return staticSiteGroups.value.flatMap((group) => group.sites.map(({ site }) => {
    const { localGroupId: _localGroupId, ...payload } = site;
    return { ...payload, deploymentServers: [...group.deploymentServers] };
  }));
}

async function reconcile() {
  reconciling.value = true;
  try {
    const result = await facilityAppsApi.reconcileReverseProxy();
    applyConfig(result.config);
    message.value = t('facilityAppsPage.syncRequested');
    snackbar.value = true;
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('facilityAppsPage.syncFailed');
  } finally {
    reconciling.value = false;
  }
}

function selectedUploadFile() {
  return Array.isArray(uploadForm.file) ? uploadForm.file[0] : uploadForm.file;
}

async function uploadAsset() {
  const file = selectedUploadFile();
  if (!file) return;
  uploadingAsset.value = true;
  try {
    const asset = await facilityAppsApi.uploadStaticAsset({
      name: uploadForm.name || file.name,
      kind: uploadForm.kind,
      file,
    });
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
  routeDialog.open = true;
}

function closeStaticRouteDialog() {
  routeDialog.open = false;
  routeDialog.index = -1;
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

function selectFacilityApp(_id: string) {
  // Reserved for future facility apps; the current page only exposes the entrance gateway.
}

function formatBytes(value: number) {
  if (value >= 1024 * 1024) return `${(value / 1024 / 1024).toFixed(1)} MB`;
  if (value >= 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${value} B`;
}

onMounted(load);
</script>

<template>
  <div class="page-shell">
    <v-alert v-if="error" type="error" variant="tonal">{{ error }}</v-alert>
    <PageLoadingState v-if="loading" />
    <AppMasterDetailWorkspace v-else>
      <template #aside>
        <AppSelectorPanel
          class="facility-list"
          :title="t('facilityAppsPage.title')"
          :loading="false"
          :empty="facilityApplications.length === 0"
          empty-icon="mdi-application-cog-outline"
          :empty-text="t('facilityAppsPage.noRouteSummaries')"
        >
          <AppSelectorSummaryItem
            v-for="item in facilityApplications"
            :key="item.id"
            selected
            :title="item.title"
            :subtitle="item.hint"
            :status="item.enabled ? 'success' : 'grey'"
            @select="selectFacilityApp(item.id)"
          />
        </AppSelectorPanel>
      </template>

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
            <AppActionButton icon="mdi-refresh" :label="t('facilityAppsPage.syncNow')" :loading="reconciling" @click="reconcile" />
            <AppActionButton kind="primary" icon="mdi-content-save" :label="t('common.save')" :loading="saving" @click="save" />
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
                  :items="serverOptions"
                  :label="t('facilityAppsPage.domainGatewayNodes')"
                  multiple
                  chips
                  closable-chips
                  variant="outlined"
                  density="compact"
                  hide-details="auto"
                  :placeholder="t('facilityAppsPage.inheritGatewayNodes')"
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
                <AppActionButton kind="danger" icon="mdi-delete-outline" :label="t('common.remove')" @click="removeStaticDomain(group.indexes)" />
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
                    <AppActionButton kind="danger" icon="mdi-delete-outline" :label="t('common.delete')" @click="removeStaticSite(index)" />
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
                  v-model="form.deploymentServers"
                  :items="serverOptions"
                  :label="t('facilityAppsPage.gatewayNodes')"
                  multiple
                  chips
                  closable-chips
                  variant="outlined"
                  density="comfortable"
                  hide-details="auto"
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
      </v-card>
    </AppMasterDetailWorkspace>
    <v-dialog v-model="routeDialog.open" width="min(720px, calc(100vw - 32px))">
      <v-card v-if="routeDialog.index >= 0 && form.staticSites[routeDialog.index]" class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('facilityAppsPage.routes') }}</span>
          <AppActionButton kind="tool" icon="mdi-close" :label="t('common.cancel')" @click="closeStaticRouteDialog" />
        </v-card-title>
        <v-card-text class="app-dialog-body">
          <div class="static-route-dialog-form">
            <v-text-field v-model="form.staticSites[routeDialog.index].path" :label="t('common.path')" variant="outlined" density="comfortable" hide-details="auto" />
            <v-select v-model="form.staticSites[routeDialog.index].ruleType" :items="routeTypeOptions" :label="t('facilityAppsPage.ruleType')" variant="outlined" density="comfortable" hide-details="auto" />
            <template v-if="(form.staticSites[routeDialog.index].ruleType || 'static') === 'static'">
              <v-select v-model="form.staticSites[routeDialog.index].sourceType" :items="sourceOptions" :label="t('facilityAppsPage.contentSource')" variant="outlined" density="comfortable" hide-details="auto" />
              <v-text-field v-if="(form.staticSites[routeDialog.index].sourceType || 'host_path') === 'host_path'" v-model="form.staticSites[routeDialog.index].rootPath" :label="t('facilityAppsPage.serverDirectory')" variant="outlined" density="comfortable" hide-details="auto" />
              <v-select v-else v-model="form.staticSites[routeDialog.index].assetId" :items="staticAssetOptions" :label="t('facilityAppsPage.siteContent')" variant="outlined" density="comfortable" hide-details="auto" />
            </template>
            <template v-else-if="form.staticSites[routeDialog.index].ruleType === 'redirect'">
              <v-text-field v-model="form.staticSites[routeDialog.index].redirectUrl" :label="t('facilityAppsPage.redirectTarget')" variant="outlined" density="comfortable" hide-details="auto" />
              <v-select v-model="form.staticSites[routeDialog.index].redirectCode" :items="redirectCodeOptions" :label="t('facilityAppsPage.redirectCode')" variant="outlined" density="comfortable" hide-details="auto" />
            </template>
            <template v-else>
              <v-text-field v-model="form.staticSites[routeDialog.index].proxyUrl" :label="t('facilityAppsPage.upstreamUrl')" variant="outlined" density="comfortable" hide-details="auto" />
              <v-select v-model="form.staticSites[routeDialog.index].proxySourceMode" :items="proxySourceModeOptions" :label="t('facilityAppsPage.requestInfo')" variant="outlined" density="comfortable" hide-details="auto" />
            </template>
          </div>
        </v-card-text>
        <v-card-actions class="app-dialog-actions">
          <AppActionGroup context="dialog">
            <AppActionButton kind="plain" :label="t('common.cancel')" @click="closeStaticRouteDialog" />
            <AppActionButton kind="primary" :label="t('common.save')" @click="closeStaticRouteDialog" />
          </AppActionGroup>
        </v-card-actions>
      </v-card>
    </v-dialog>
    <v-snackbar v-model="snackbar">{{ message }}</v-snackbar>
  </div>
</template>

<style scoped>
.facility-list,
.facility-detail {
  min-width: 0;
  min-height: 0;
  overflow: hidden;
}

.facility-detail {
  display: flex;
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
  grid-template-columns: minmax(0, 1fr) auto auto;
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
  .static-domain-card__header {
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
