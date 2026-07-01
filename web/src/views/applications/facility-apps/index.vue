<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { useRouter } from 'vue-router';
import { facilityAppsApi } from '@/api/facilityApps';
import { serversApi } from '@/api/servers';
import PageLoadingState from '@/components/PageLoadingState.vue';
import type { FacilityPanelEntryDto, FacilityReverseProxyConfigDto, FacilityStaticAssetDto, FacilityStaticSiteDto, ServerDto } from '@/types/api';
import { useI18n } from '@/i18n';

type FacilityStaticSiteForm = FacilityStaticSiteDto & { localGroupId: string };

const { t, formatDateTime, translateLifecycleStage, translateRuntimeDesiredState, translateRuntimeStatus } = useI18n();
const router = useRouter();
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
const applicationRouteSummaries = computed(() => (config.value?.routeSummaries ?? []).filter((item) => item.source === 'application'));
const panelRouteSummary = computed(() => (config.value?.routeSummaries ?? []).find((item) => item.source === 'system_panel'));
const lifecycleTargets = computed(() => config.value?.operation?.targets ?? []);
const applicationRouteGroups = computed(() => {
  const groups: Array<{ domain: string; httpsStatus: string; items: typeof applicationRouteSummaries.value }> = [];
  const byDomain = new Map<string, number>();
  applicationRouteSummaries.value.forEach((item) => {
    const groupIndex = byDomain.get(item.domain);
    if (groupIndex === undefined) {
      byDomain.set(item.domain, groups.length);
      groups.push({ domain: item.domain, httpsStatus: item.httpsStatus || 'disabled', items: [item] });
    } else {
      groups[groupIndex].items.push(item);
    }
  });
  return groups;
});
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
  form.staticSites.push(newStaticSite());
}

function addStaticRoute(domain: string, deploymentServers: string[], localGroupId: string) {
  form.staticSites.push(newStaticSite(domain, deploymentServers, localGroupId));
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
}

function removeStaticDomain(indexes: number[]) {
  [...indexes].sort((a, b) => b - a).forEach((index) => {
    form.staticSites.splice(index, 1);
  });
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

function serverNames(ids: string[] = []) {
  if (!ids.length) return t('facilityAppsPage.allProxyServers');
  return ids.map((id) => servers.value.find((server) => server.id === id)?.name ?? id).join(', ');
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

function openApplications() {
  router.push({ name: 'applications' });
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
    <div v-else class="facility-workspace">
      <v-card variant="outlined" class="facility-list">
        <div class="facility-list__header">
          <div class="text-subtitle-1 font-weight-bold">{{ t('facilityAppsPage.title') }}</div>
        </div>
        <div class="facility-list__body">
          <button type="button" class="facility-app-item facility-app-item--selected">
            <span>
              <strong>{{ t('facilityAppsPage.reverseProxy') }}</strong>
              <span>{{ t('facilityAppsPage.reverseProxyHint') }}</span>
            </span>
            <v-chip size="small" color="primary" variant="tonal">
              {{ form.deploymentServers.length }}
            </v-chip>
          </button>
        </div>
      </v-card>

      <v-card variant="outlined" class="facility-detail">
        <div class="app-card-header">
          <div class="min-width-0">
            <strong>{{ t('facilityAppsPage.reverseProxy') }}</strong>
            <div class="text-caption text-medium-emphasis text-truncate">
              {{ enabledServerNames || t('facilityAppsPage.noEnabledServers') }}
            </div>
          </div>
          <div class="facility-actions">
            <v-btn size="small" variant="outlined" prepend-icon="mdi-refresh" :loading="reconciling" @click="reconcile">
              {{ t('facilityAppsPage.syncNow') }}
            </v-btn>
            <v-btn size="small" color="primary" variant="flat" prepend-icon="mdi-content-save" :loading="saving" @click="save">
              {{ t('common.save') }}
            </v-btn>
          </div>
        </div>
        <v-divider />
        <div class="facility-detail__body">
          <v-alert v-if="config?.lastError" type="warning" variant="tonal" density="compact">
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

          <section class="facility-section">
            <div class="section-title">{{ t('facilityAppsPage.deploymentRecords') }}</div>
            <div v-if="!config?.operation" class="empty-inline">
              {{ t('facilityAppsPage.noDeploymentRecords') }}
            </div>
            <div v-else class="deployment-record">
              <div class="deployment-record__summary">
                <v-chip :color="runtimeStatusColor(config.operation.status)" size="small" variant="tonal" label>
                  {{ translateRuntimeStatus(config.operation.status) }}
                </v-chip>
                <span class="text-caption text-medium-emphasis mono">{{ config.operation.id }}</span>
                <span class="text-caption text-medium-emphasis">
                  {{ formatDateTime(config.operation.finishedAt || config.operation.updatedAt) }}
                </span>
              </div>
              <div class="deployment-target-table">
                <v-table density="compact">
                  <thead>
                    <tr>
                      <th>{{ t('applicationRuntime.server') }}</th>
                      <th>{{ t('common.status') }}</th>
                      <th>{{ t('applicationRuntime.desired') }}</th>
                      <th>{{ t('applicationRuntime.stage') }}</th>
                      <th>{{ t('applicationRuntime.container') }}</th>
                      <th>{{ t('common.updatedAt') }}</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="target in lifecycleTargets" :key="target.id">
                      <td>
                        <div class="font-weight-medium">{{ target.serverName || target.serverId }}</div>
                        <div v-if="target.serverName" class="text-caption text-medium-emphasis mono">{{ target.serverId }}</div>
                      </td>
                      <td>
                        <v-chip :color="runtimeStatusColor(target.status)" size="small" variant="tonal" label>
                          {{ translateRuntimeStatus(target.status) }}
                        </v-chip>
                        <div v-if="target.error" class="text-caption text-error">{{ target.error }}</div>
                      </td>
                      <td>{{ translateRuntimeDesiredState(target.desiredState) }}</td>
                      <td>{{ target.stage ? translateLifecycleStage(target.stage) : t('common.notAvailable') }}</td>
                      <td class="mono">{{ target.containerName || target.instanceId || t('common.notAvailable') }}</td>
                      <td>{{ formatDateTime(target.finishedAt || target.updatedAt) }}</td>
                    </tr>
                    <tr v-if="!lifecycleTargets.length">
                      <td colspan="6" class="text-center text-medium-emphasis py-4">{{ t('facilityAppsPage.noDeploymentTargets') }}</td>
                    </tr>
                  </tbody>
                </v-table>
              </div>
            </div>
          </section>

          <section class="facility-section">
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
          </section>

          <section class="facility-section">
            <div class="section-title">{{ t('facilityAppsPage.panelEntry') }}</div>
            <div class="panel-entry-card">
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
          </section>

          <section class="facility-section">
            <div class="section-title">{{ t('facilityAppsPage.siteContent') }}</div>
            <div class="asset-upload-row">
              <v-text-field v-model="uploadForm.name" :label="t('common.name')" variant="outlined" density="compact" hide-details="auto" />
              <v-select v-model="uploadForm.kind" :items="uploadKindOptions" :label="t('common.type')" variant="outlined" density="compact" hide-details="auto" />
              <v-file-input v-model="uploadForm.file" :label="uploadForm.kind === 'uploaded_bundle' ? t('facilityAppsPage.bundleFile') : t('facilityAppsPage.singleFile')" variant="outlined" density="compact" hide-details="auto" />
              <v-btn color="primary" variant="flat" :loading="uploadingAsset" :disabled="!selectedUploadFile()" @click="uploadAsset">
                {{ t('facilityAppsPage.upload') }}
              </v-btn>
            </div>
            <div v-if="staticAssets.length" class="asset-list">
              <div v-for="asset in staticAssets" :key="asset.id" class="asset-item">
                <span class="text-truncate">{{ asset.name }}</span>
                <v-chip size="small" variant="tonal" label>{{ asset.kind === 'uploaded_bundle' ? t('facilityAppsPage.uploadedBundle') : t('facilityAppsPage.uploadedFile') }}</v-chip>
                <span class="text-caption text-medium-emphasis">{{ formatBytes(asset.size) }}</span>
              </div>
            </div>
          </section>

          <section class="facility-section">
            <div class="section-heading">
              <div class="section-title">{{ t('facilityAppsPage.routes') }}</div>
              <v-btn size="small" variant="outlined" prepend-icon="mdi-plus" @click="addStaticDomain">
                {{ t('facilityAppsPage.addDomain') }}
              </v-btn>
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
                <div class="static-domain-card__actions">
                  <v-btn size="small" variant="outlined" prepend-icon="mdi-plus" @click="addStaticRoute(group.domain, group.deploymentServers, group.key)">
                    {{ t('facilityAppsPage.addRoute') }}
                  </v-btn>
                  <v-btn icon variant="text" color="error" :aria-label="t('common.remove')" @click="removeStaticDomain(group.indexes)">
                    <v-icon>mdi-delete-outline</v-icon>
                  </v-btn>
                </div>
              </div>
              <div class="static-route-list">
                <div v-for="{ site, index } in group.sites" :key="index" class="static-route-row">
                  <div class="static-route-row__main">
                    <v-text-field v-model="site.path" :label="t('common.path')" variant="outlined" density="compact" hide-details="auto" />
                    <v-select v-model="site.ruleType" :items="routeTypeOptions" :label="t('facilityAppsPage.ruleType')" variant="outlined" density="compact" hide-details="auto" />
                    <template v-if="(site.ruleType || 'static') === 'static'">
                      <v-select v-model="site.sourceType" :items="sourceOptions" :label="t('facilityAppsPage.contentSource')" variant="outlined" density="compact" hide-details="auto" />
                      <v-text-field v-if="(site.sourceType || 'host_path') === 'host_path'" v-model="site.rootPath" :label="t('facilityAppsPage.serverDirectory')" variant="outlined" density="compact" hide-details="auto" />
                      <v-select v-else v-model="site.assetId" :items="staticAssetOptions" :label="t('facilityAppsPage.siteContent')" variant="outlined" density="compact" hide-details="auto" />
                    </template>
                    <template v-else-if="site.ruleType === 'redirect'">
                      <v-text-field v-model="site.redirectUrl" :label="t('facilityAppsPage.redirectTarget')" variant="outlined" density="compact" hide-details="auto" />
                      <v-select v-model="site.redirectCode" :items="redirectCodeOptions" :label="t('facilityAppsPage.redirectCode')" variant="outlined" density="compact" hide-details="auto" />
                    </template>
                    <template v-else>
                      <v-text-field v-model="site.proxyUrl" :label="t('facilityAppsPage.upstreamUrl')" variant="outlined" density="compact" hide-details="auto" />
                      <v-select v-model="site.proxySourceMode" :items="proxySourceModeOptions" :label="t('facilityAppsPage.requestInfo')" variant="outlined" density="compact" hide-details="auto" />
                    </template>
                    <v-chip size="small" variant="tonal" label>{{ routeTypeLabel(site) }}</v-chip>
                    <v-btn icon variant="text" color="error" :aria-label="t('common.remove')" @click="removeStaticSite(index)">
                      <v-icon>mdi-close</v-icon>
                    </v-btn>
                  </div>
                </div>
              </div>
            </div>
          </section>

          <section class="facility-section">
            <div class="section-heading">
              <div class="section-title">{{ t('facilityAppsPage.applicationRoutes') }}</div>
              <v-btn size="small" variant="outlined" prepend-icon="mdi-application-cog-outline" @click="openApplications">
                {{ t('facilityAppsPage.openApplications') }}
              </v-btn>
            </div>
            <div v-if="!applicationRouteGroups.length" class="empty-inline">
              {{ t('facilityAppsPage.noRouteSummaries') }}
            </div>
            <div v-else class="application-route-domain-list">
              <div v-for="group in applicationRouteGroups" :key="group.domain" class="application-route-domain">
                <div class="application-route-domain__header">
                  <strong class="text-truncate">{{ group.domain }}</strong>
                  <v-chip size="small" :color="httpsStatusColor(group.httpsStatus)" variant="tonal" label>
                    {{ httpsStatusLabel(group.httpsStatus) }}
                  </v-chip>
                </div>
                <div class="route-summary-list">
                  <div v-for="item in group.items" :key="`${item.domain}:${item.path}:${item.serverIds.join(',')}`" class="route-summary-item">
                    <div class="min-width-0">
                      <strong class="text-truncate">{{ item.path }}</strong>
                      <span class="text-caption text-medium-emphasis">{{ serverNames(item.serverIds) }}</span>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </section>
        </div>
      </v-card>
    </div>
    <v-snackbar v-model="snackbar">{{ message }}</v-snackbar>
  </div>
</template>

<style scoped>
.facility-workspace {
  display: grid;
  grid-template-columns: clamp(300px, 26vw, 340px) minmax(0, 1fr);
  flex: 1 1 auto;
  gap: 18px;
  min-height: 0;
}

.facility-list,
.facility-detail {
  min-height: 0;
  overflow: hidden;
}

.facility-list {
  display: flex;
  flex-direction: column;
}

.facility-list__header {
  flex: 0 0 auto;
  padding: 12px 16px;
  background: rgb(var(--v-theme-surface-variant));
}

.facility-list__body,
.facility-detail__body {
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
}

.facility-list__body {
  padding: 10px;
}

.facility-app-item {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  width: 100%;
  min-width: 0;
  padding: 12px;
  border: 1px solid rgba(var(--v-theme-primary), 0.26);
  border-radius: 8px;
  background: rgba(var(--v-theme-primary), 0.06);
  color: inherit;
  font: inherit;
  text-align: left;
}

.facility-app-item span {
  display: grid;
  gap: 3px;
  min-width: 0;
}

.facility-app-item span span {
  color: var(--lp-text-muted);
  font-size: 0.78rem;
}

.facility-detail {
  display: flex;
  flex-direction: column;
}

.facility-actions,
.section-heading {
  display: flex;
  align-items: center;
  gap: 8px;
}

.facility-detail__body {
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: 16px;
}

.facility-summary {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
}

.facility-summary > div {
  display: grid;
  gap: 4px;
  min-width: 0;
  padding: 10px 12px;
  border: 1px solid var(--lp-border);
  border-radius: 8px;
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
  display: grid;
  gap: 12px;
}

.deployment-record {
  display: grid;
  gap: 10px;
}

.deployment-record__summary {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
  min-width: 0;
}

.deployment-target-table {
  min-width: 0;
  overflow: auto;
  border: 1px solid var(--lp-border);
  border-radius: 8px;
}

.section-heading {
  justify-content: space-between;
}

.section-title {
  font-weight: 700;
}

.static-domain-card {
  display: grid;
  gap: 12px;
  padding: 12px;
  border: 1px solid var(--lp-border);
  border-radius: 8px;
}

.panel-entry-card {
  display: grid;
  gap: 10px;
  padding: 12px;
  border: 1px solid var(--lp-border);
  border-radius: 8px;
}

.panel-entry-grid {
  display: grid;
  grid-template-columns: minmax(220px, 0.9fr) minmax(220px, 1.1fr) auto;
  gap: 8px;
  align-items: start;
}

.static-domain-card__header {
  display: grid;
  grid-template-columns: minmax(180px, 0.9fr) minmax(220px, 1.1fr) minmax(180px, auto) auto;
  gap: 8px;
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
  gap: 8px;
}

.static-route-list {
  display: grid;
  gap: 8px;
}

.static-route-row {
  padding: 8px;
  border: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  border-radius: 8px;
}

.static-route-row__main {
  display: grid;
  grid-template-columns: minmax(90px, 0.7fr) minmax(130px, 0.8fr) minmax(150px, 0.9fr) minmax(220px, 1.2fr) auto 40px;
  gap: 8px;
  align-items: start;
}

.asset-upload-row {
  display: grid;
  grid-template-columns: minmax(140px, 1fr) 160px minmax(220px, 1.4fr) auto;
  gap: 8px;
  align-items: start;
}

.asset-list,
.route-summary-list,
.application-route-domain-list {
  display: grid;
  gap: 8px;
}

.application-route-domain {
  display: grid;
  gap: 8px;
  padding: 10px;
  border: 1px solid var(--lp-border);
  border-radius: 8px;
}

.application-route-domain__header {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
  align-items: center;
}

.asset-item,
.route-summary-item {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto auto;
  align-items: center;
  gap: 10px;
  min-width: 0;
  padding: 8px 10px;
  border: 1px solid var(--lp-border);
  border-radius: 8px;
}

.route-summary-item {
  grid-template-columns: minmax(0, 1fr) auto;
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

@media (max-width: 1080px) {
  .facility-workspace {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 760px) {
  .facility-workspace {
    flex: none;
    min-height: auto;
  }

  .facility-list,
  .facility-detail,
  .facility-list__body,
  .facility-detail__body {
    overflow: visible;
  }

  .facility-summary,
  .panel-entry-grid,
  .static-domain-card__header,
  .static-route-row__main,
  .asset-upload-row,
  .asset-item,
  .application-route-domain__header,
  .route-summary-item {
    grid-template-columns: 1fr;
  }

  .facility-actions,
  .section-heading,
  .static-domain-card__actions {
    align-items: stretch;
    flex-direction: column;
  }
}
</style>
