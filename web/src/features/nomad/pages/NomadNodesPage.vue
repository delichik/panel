<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { t, useI18n } from '@/i18n';
import { nomadApi } from '@/api/nomad';
import type { NomadControlPlaneDto, NomadReverseProxyStaticSiteDto, ProjectedNomadNodeDto } from '@/types/api';

const router = useRouter();
useI18n();
const controlPlane = ref<NomadControlPlaneDto | null>(null);
const loading = ref(false);
const joining = ref(false);
const actionLoading = ref('');
const error = ref('');
const joinDialog = ref(false);
const selectedServerId = ref('');
const proxyDialog = ref(false);
const editingNode = ref<ProjectedNomadNodeDto | null>(null);
const proxyForm = ref({
  enabled: false,
  staticSites: [] as NomadReverseProxyStaticSiteDto[],
});

const nodes = computed(() => controlPlane.value?.nodes ?? []);
const candidateServers = computed(() => controlPlane.value?.joinCandidates ?? []);
const readyCount = computed(() => nodes.value.filter((node) => node.status === 'ready').length);
const managedCount = computed(() => nodes.value.filter((node) => node.kind === 'managed').length);
const pendingCount = computed(() => nodes.value.filter((node) => node.kind === 'pending').length);
const selectedServer = computed(() => candidateServers.value.find((server) => server.id === selectedServerId.value) ?? null);
const candidateOptions = computed(() =>
  candidateServers.value.map((server) => ({
    label: `${server.name} (${server.host}:${server.port})`,
    value: server.id,
  })),
);

function statusColor(nodeStatus?: string) {
  if (nodeStatus === 'ready') return 'success';
  if (nodeStatus === 'down' || nodeStatus === 'failed') return 'error';
  if (nodeStatus === 'unmanaged') return 'grey';
  if (nodeStatus === 'registering') return 'info';
  if (nodeStatus === 'missing' || nodeStatus === 'nomad_unreachable') return 'error';
  if (nodeStatus === 'removing') return 'warning';
  return 'warning';
}

function kindColor(kind?: string) {
  if (kind === 'managed') return 'primary';
  if (kind === 'pending') return 'warning';
  if (kind === 'missing') return 'error';
  return 'grey';
}

function openJoinDialog() {
  selectedServerId.value = candidateServers.value[0]?.id ?? '';
  joinDialog.value = true;
}

function canJoinNode(node: ProjectedNomadNodeDto) {
  return Boolean(node.serverId) && ['missing', 'nomad_unreachable', 'failed'].includes(node.status || '');
}

function canRemoveNode(node: ProjectedNomadNodeDto) {
  return Boolean(node.serverId || node.nodeId) && !['removing', 'joining', 'bootstrapping'].includes(node.status || '');
}

async function load() {
  loading.value = true;
  try {
    const result = await nomadApi.controlPlane();
    controlPlane.value = result;
    error.value = '';
    if (result.status === 'unconfigured') {
      await router.replace('/nomad/setup');
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('nomadNodesPage.loadFailed');
  } finally {
    loading.value = false;
  }
}

async function joinSelectedServer() {
  if (!selectedServerId.value) return;
  joining.value = true;
  try {
    await nomadApi.joinServer(selectedServerId.value);
    joinDialog.value = false;
    error.value = '';
    await load();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('nomadNodesPage.joinFailed');
  } finally {
    joining.value = false;
  }
}

async function joinNode(node: ProjectedNomadNodeDto) {
  if (!node.serverId) return;
  actionLoading.value = `join:${node.serverId}`;
  try {
    await nomadApi.joinServer(node.serverId);
    error.value = '';
    await load();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('nomadNodesPage.joinFailed');
  } finally {
    actionLoading.value = '';
  }
}

async function removeNode(node: ProjectedNomadNodeDto) {
  actionLoading.value = `remove:${node.serverId || node.nodeId}`;
  try {
    await nomadApi.removeNode({ serverId: node.serverId, nodeId: node.nodeId });
    error.value = '';
    await load();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('nomadNodesPage.removeFailed');
  } finally {
    actionLoading.value = '';
  }
}

function openProxyDialog(node: ProjectedNomadNodeDto) {
  editingNode.value = node;
  proxyForm.value = {
    enabled: node.reverseProxy,
    staticSites: cloneStaticSites(node.reverseProxyStaticSites ?? []),
  };
  proxyDialog.value = true;
}

function cloneStaticSites(sites: NomadReverseProxyStaticSiteDto[]) {
  return sites.map((site) => ({ domain: site.domain, root: site.root, index: site.index || 'index.html' }));
}

function addStaticSite() {
  proxyForm.value.staticSites.push({ domain: '', root: '/var/www/html', index: 'index.html' });
}

function removeAt<T>(items: T[], index: number) {
  items.splice(index, 1);
}

async function saveProxyConfig() {
  const node = editingNode.value;
  if (!node?.serverId) return;
  actionLoading.value = `proxy:${node.serverId}`;
  try {
    const staticSites = proxyForm.value.staticSites
      .filter((site) => site.domain.trim() || site.root.trim())
      .map((site) => ({ domain: site.domain.trim(), root: site.root.trim(), index: site.index.trim() || 'index.html' }));
    await nomadApi.updateReverseProxy({
      serverId: node.serverId,
      enabled: proxyForm.value.enabled,
      staticFiles: staticSites.length > 0,
      staticSites,
    });
    error.value = '';
    proxyDialog.value = false;
    await load();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('nomadNodesPage.saveProxyFailed');
  } finally {
    actionLoading.value = '';
  }
}

onMounted(load);
</script>

<template>
  <div class="page-shell">
    <v-alert v-if="error" type="error" variant="tonal">{{ error }}</v-alert>
    <v-alert v-else-if="controlPlane?.status === 'bootstrapping'" type="info" variant="tonal">
      {{ t('nomadNodesPage.bootstrappingHint') }}
    </v-alert>
    <v-alert v-else-if="controlPlane?.status === 'degraded'" type="warning" variant="tonal">
      {{ t('nomadNodesPage.degradedHint') }}
    </v-alert>
    <v-alert v-else-if="controlPlane?.status === 'connected'" type="success" variant="tonal">
      {{ t('nomadNodesPage.connectedLeader', { leader: controlPlane.leader || t('common.unknown') }) }}
    </v-alert>

    <div class="summary-strip">
      <v-card variant="outlined" class="summary-card"><div class="text-caption text-medium-emphasis">{{ t('nomadNodesPage.nodes') }}</div><div class="text-h5 font-weight-bold font-tabular">{{ nodes.length }}</div></v-card>
      <v-card variant="outlined" class="summary-card"><div class="text-caption text-medium-emphasis">{{ t('nomadNodesPage.ready') }}</div><div class="text-h5 font-weight-bold font-tabular">{{ readyCount }}</div></v-card>
      <v-card variant="outlined" class="summary-card"><div class="text-caption text-medium-emphasis">{{ t('nomadNodesPage.managed') }}</div><div class="text-h5 font-weight-bold font-tabular">{{ managedCount }}</div></v-card>
      <v-card variant="outlined" class="summary-card"><div class="text-caption text-medium-emphasis">{{ t('nomadNodesPage.pending') }}</div><div class="text-h5 font-weight-bold font-tabular">{{ pendingCount }}</div></v-card>
    </div>

    <v-card variant="outlined" :loading="loading">
      <div class="app-card-header">
        <v-btn prepend-icon="mdi-account-network" color="primary" variant="flat" class="text-none action-btn" :disabled="candidateServers.length === 0" @click="openJoinDialog">{{ t('nomadNodesPage.joinNode') }}</v-btn>
      </div>
      <v-table>
        <thead><tr><th>{{ t('common.name') }}</th><th>Node ID</th><th>{{ t('serversPage.host') }}</th><th>{{ t('nomadNodesPage.role') }}</th><th>{{ t('common.status') }}</th><th>{{ t('nomadNodesPage.reverseProxy') }}</th><th>{{ t('packagesPage.source') }}</th><th class="text-right">{{ t('common.actions') }}</th></tr></thead>
        <tbody>
          <tr v-for="node in nodes" :key="node.nodeId || node.serverId || node.name">
            <td class="font-weight-bold">{{ node.name || '-' }}</td>
            <td class="mono">{{ node.nodeId || '-' }}</td>
            <td>{{ node.host || '-' }}</td>
            <td><v-chip size="small" variant="tonal" label>{{ node.role }}</v-chip></td>
            <td>
              <v-chip :color="statusColor(node.status)" size="small" variant="tonal" label>{{ node.status || t('common.unknown') }}</v-chip>
              <div v-if="node.error" class="text-caption text-error mt-1">{{ node.error }}</div>
            </td>
            <td>
              <div v-if="node.serverId" class="proxy-summary">
                <v-chip :color="node.reverseProxy ? 'success' : 'grey'" size="small" variant="tonal" label>{{ node.reverseProxy ? t('nomadNodesPage.enabled') : t('nomadNodesPage.disabled') }}</v-chip>
                <span class="text-caption text-medium-emphasis">{{ t('nomadNodesPage.staticSitesCount', { count: node.reverseProxyStaticSites?.length ?? 0 }) }}</span>
              </div>
              <span v-else class="text-medium-emphasis">-</span>
            </td>
            <td>
              <v-chip :color="kindColor(node.kind)" size="small" variant="tonal" label>{{ node.kind }}</v-chip>
              <div v-if="node.serverId" class="text-caption text-medium-emphasis mt-1">{{ node.serverId }}</div>
            </td>
            <td class="text-right">
              <div class="row-actions">
                <v-btn
                  v-if="node.serverId"
                  size="small"
                  variant="text"
                  color="primary"
                  prepend-icon="mdi-tune"
                  class="text-none"
                  :loading="actionLoading === `proxy:${node.serverId}`"
                  @click="openProxyDialog(node)"
                >
                  {{ t('nomadNodesPage.proxy') }}
                </v-btn>
                <v-btn
                  v-if="canJoinNode(node)"
                  size="small"
                  variant="text"
                  color="primary"
                  prepend-icon="mdi-account-network"
                  class="text-none"
                  :loading="actionLoading === `join:${node.serverId}`"
                  @click="joinNode(node)"
                >
                  {{ t('nomadNodesPage.join') }}
                </v-btn>
                <v-btn
                  v-if="canRemoveNode(node)"
                  size="small"
                  variant="text"
                  color="error"
                  prepend-icon="mdi-delete"
                  class="text-none"
                  :loading="actionLoading === `remove:${node.serverId || node.nodeId}`"
                  @click="removeNode(node)"
                >
                  {{ t('nomadNodesPage.remove') }}
                </v-btn>
              </div>
            </td>
          </tr>
          <tr v-if="nodes.length === 0"><td colspan="8" class="text-center py-8 text-medium-emphasis">{{ t('nomadNodesPage.noProjectedNodes') }}</td></tr>
        </tbody>
      </v-table>
    </v-card>

    <v-dialog v-model="joinDialog" width="520">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('nomadNodesPage.joinNodeTitle') }}</span>
          <v-btn icon="mdi-close" variant="text" @click="joinDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          <v-alert v-if="candidateServers.length === 0" type="info" variant="tonal" class="mb-4">
            {{ t('nomadNodesPage.noJoinCandidates') }}
          </v-alert>
          <v-select
            v-model="selectedServerId"
            :items="candidateOptions"
            item-title="label"
            item-value="value"
            :label="t('nomadNodesPage.sshServer')"
            variant="outlined"
            density="comfortable"
            class="mb-4"
          />
          <div v-if="selectedServer" class="server-preview">
            <div class="font-weight-bold">{{ selectedServer.name }}</div>
            <div class="text-caption text-medium-emphasis">{{ selectedServer.host }}:{{ selectedServer.port }}</div>
          </div>
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <v-btn variant="text" class="text-none" @click="joinDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="primary" variant="flat" class="text-none" :loading="joining" :disabled="!selectedServerId" @click="joinSelectedServer">{{ t('nomadNodesPage.joinNode') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="proxyDialog" width="920">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('nomadNodesPage.reverseProxyTitle', { name: editingNode?.name ?? '' }) }}</span>
          <v-btn icon="mdi-close" variant="text" @click="proxyDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          <v-switch v-model="proxyForm.enabled" color="primary" density="compact" :label="t('nomadNodesPage.enableReverseProxy')" hide-details class="mb-4" />

          <div class="section-title">{{ t('nomadNodesPage.staticSites') }}</div>
          <div v-for="(site, index) in proxyForm.staticSites" :key="index" class="repeat-row static-site-row">
            <v-text-field v-model="site.domain" :label="t('domainsPage.domain')" density="compact" variant="outlined" hide-details />
            <v-text-field v-model="site.root" :label="t('nomadNodesPage.staticRoot')" density="compact" variant="outlined" hide-details />
            <v-text-field v-model="site.index" :label="t('nomadNodesPage.index')" density="compact" variant="outlined" hide-details />
            <v-btn icon="mdi-delete" variant="text" color="error" @click="removeAt(proxyForm.staticSites, index)" />
          </div>
          <v-btn size="small" variant="outlined" prepend-icon="mdi-plus" class="text-none" @click="addStaticSite">{{ t('common.addStaticSite') }}</v-btn>
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <v-btn variant="text" class="text-none" @click="proxyDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="primary" variant="flat" class="text-none" :loading="actionLoading === `proxy:${editingNode?.serverId}`" @click="saveProxyConfig">{{ t('nomadNodesPage.saveProxyConfig') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>

<style scoped>
.summary-strip { grid-template-columns: repeat(4, minmax(0, 180px)); }
.mono { font-size: 0.8rem; }
.row-actions { display: inline-flex; justify-content: flex-end; gap: 4px; min-width: 0; }
.proxy-summary { display: flex; gap: 8px; align-items: center; min-width: 180px; }
.section-title { margin-top: 10px; }
.repeat-row { display: grid; gap: 8px; align-items: center; margin-bottom: 8px; }
.static-site-row { grid-template-columns: minmax(0, 1fr) minmax(0, 1.2fr) 180px 40px; }
.server-preview { border-radius: 8px; padding: 12px; background: rgba(var(--v-theme-primary), 0.06); }
@media (max-width: 900px) {
  .summary-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .static-site-row { grid-template-columns: 1fr; }
}
</style>
