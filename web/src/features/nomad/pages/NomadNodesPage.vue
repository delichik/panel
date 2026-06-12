<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import {
  t,
  translateNomadNodeKind,
  translateNomadNodeRole,
  translateNomadNodeStatus,
} from '@/i18n';
import { nomadApi } from '@/api/nomad';
import type { NomadControlPlaneDto, NomadReverseProxyStaticSiteDto, ProjectedNomadNodeDto } from '@/types/api';
import AppPagination from '@/components/AppPagination.vue';
import PageLoadingState from '@/components/PageLoadingState.vue';
import { usePagination } from '@/composables/usePagination';
import {
  buildNomadAddressOptions,
  type NomadAddressOption,
  type NomadAddressTarget,
} from '@/features/nomad/addressOptions';

const router = useRouter();
const route = useRoute();
const controlPlane = ref<NomadControlPlaneDto | null>(null);
const loading = ref(false);
const joining = ref(false);
const actionLoading = ref('');
const error = ref('');
const joinDialog = ref(false);
const redeployDialog = ref(false);
const rebuildDialog = ref(false);
const switchDialog = ref(false);
const removeDialog = ref(false);
const selectedServerId = ref('');
const selectedJoinAddress = ref('');
const selectedRebuildServerId = ref('');
const selectedRebuildAddress = ref('');
const selectedSwitchServerId = ref('');
const selectedSwitchAddress = ref('');
const redeployingNode = ref<ProjectedNomadNodeDto | null>(null);
const selectedRedeployAddress = ref('');
const removingNode = ref<ProjectedNomadNodeDto | null>(null);
const operationTaskId = ref('');
const operationMessage = ref('');
const proxyDialog = ref(false);
const editingNode = ref<ProjectedNomadNodeDto | null>(null);
const proxyForm = ref({
  enabled: false,
  staticSites: [] as NomadReverseProxyStaticSiteDto[],
});

const nodes = computed(() => controlPlane.value?.nodes ?? []);
const candidateServers = computed(() => controlPlane.value?.joinCandidates ?? []);
const bootstrapServers = computed(() => controlPlane.value?.bootstrapCandidates ?? []);
const readyCount = computed(() => nodes.value.filter((node) => node.status === 'ready').length);
const managedCount = computed(() => nodes.value.filter((node) => node.kind === 'managed').length);
const pendingCount = computed(() => nodes.value.filter((node) => node.kind === 'pending').length);
const {
  page,
  pageSize,
  total,
  pageItems: pagedNodes,
} = usePagination(nodes);
const selectedServer = computed(() => candidateServers.value.find((server) => server.id === selectedServerId.value) ?? null);
const selectedRebuildServer = computed(() => bootstrapServers.value.find((server) => server.id === selectedRebuildServerId.value) ?? null);
const candidateOptions = computed(() =>
  candidateServers.value.map((server) => ({
    label: `${server.name} (${server.host}:${server.port})`,
    value: server.id,
  })),
);
const rebuildServerOptions = computed(() =>
  bootstrapServers.value.map((server) => ({
    label: `${server.name} (${server.host}:${server.port})`,
    value: server.id,
  })),
);
const switchServerTargets = computed(() => {
  const seen = new Set<string>();
  const targets: Array<NomadAddressTarget & { id: string; name: string; port?: number }> = [];
  for (const server of bootstrapServers.value) {
    seen.add(server.id);
    targets.push(server);
  }
  for (const node of nodes.value) {
    if (!node.serverId || seen.has(node.serverId)) continue;
    seen.add(node.serverId);
    targets.push({
      id: node.serverId,
      name: node.name || node.serverId,
      host: node.host,
      traits: node.traits,
    });
  }
  return targets;
});
const switchServerOptions = computed(() =>
  switchServerTargets.value.map((server) => ({
    label: `${server.name} (${server.host || server.id}${server.port ? `:${server.port}` : ''})`,
    value: server.id,
  })),
);
const selectedSwitchServer = computed(() => switchServerTargets.value.find((server) => server.id === selectedSwitchServerId.value) ?? null);
const joinAddressOptions = computed(() => labeledAddressOptions(selectedServer.value));
const redeployAddressOptions = computed(() => labeledAddressOptions(redeployingNode.value));
const rebuildAddressOptions = computed(() => labeledAddressOptions(selectedRebuildServer.value));
const switchAddressOptions = computed(() => labeledAddressOptions(selectedSwitchServer.value));
const removingNodeName = computed(() => removingNode.value?.name || removingNode.value?.nodeId || removingNode.value?.serverId || t('common.unknown'));
const redeployingNodeName = computed(() => redeployingNode.value?.name || redeployingNode.value?.serverId || t('common.unknown'));

watch(selectedServerId, () => {
  selectedJoinAddress.value = joinAddressOptions.value[0]?.value ?? '';
});

watch(selectedSwitchServerId, () => {
  selectedSwitchAddress.value = switchAddressOptions.value[0]?.value ?? '';
});

watch(selectedRebuildServerId, () => {
  selectedRebuildAddress.value = rebuildAddressOptions.value[0]?.value ?? '';
});

function taskRoute(taskId = operationTaskId.value) {
  return taskId ? { path: '/tasks', query: { task: taskId } } : '/tasks';
}

function routeTaskId() {
  return typeof route.query.task === 'string' ? route.query.task : '';
}

function showTaskMessage(taskId: string | undefined, message: string) {
  operationTaskId.value = taskId || '';
  operationMessage.value = message;
}

function clearTaskMessage() {
  operationTaskId.value = '';
  operationMessage.value = '';
}

function statusColor(nodeStatus?: string) {
  if (nodeStatus === 'ready') return 'success';
  if (nodeStatus === 'down' || nodeStatus === 'failed') return 'error';
  if (nodeStatus === 'unmanaged') return 'grey';
  if (nodeStatus === 'registering') return 'info';
  if (nodeStatus === 'missing' || nodeStatus === 'nomad_unreachable') return 'error';
  if (nodeStatus === 'rebuilding' || nodeStatus === 'removing') return 'warning';
  return 'warning';
}

function kindColor(kind?: string) {
  if (kind === 'managed') return 'primary';
  if (kind === 'pending') return 'warning';
  if (kind === 'missing') return 'error';
  return 'grey';
}

function openJoinDialog(serverId = '') {
  selectedServerId.value = serverId || (candidateServers.value[0]?.id ?? '');
  selectedJoinAddress.value = joinAddressOptions.value[0]?.value ?? '';
  joinDialog.value = true;
}

function openRedeployDialog(node: ProjectedNomadNodeDto) {
  if (!node.serverId) return;
  redeployingNode.value = node;
  selectedRedeployAddress.value = redeployAddressOptions.value[0]?.value ?? '';
  redeployDialog.value = true;
}

function openRebuildDialog() {
  selectedRebuildServerId.value = rebuildServerOptions.value[0]?.value ?? '';
  selectedRebuildAddress.value = rebuildAddressOptions.value[0]?.value ?? '';
  rebuildDialog.value = true;
}

function openSwitchDialog() {
  selectedSwitchServerId.value = switchServerOptions.value[0]?.value ?? '';
  selectedSwitchAddress.value = switchAddressOptions.value[0]?.value ?? '';
  switchDialog.value = true;
}

function labeledAddressOptions(target?: NomadAddressTarget | null) {
  return buildNomadAddressOptions(target).map((option) => ({
    ...option,
    label: nomadAddressOptionLabel(option),
  }));
}

function nomadAddressOptionLabel(option: NomadAddressOption) {
  if (option.source === 'current') {
    return t('nomadSetupPage.nomadAddressSourceCurrent', { address: option.value });
  }
  if (option.source === 'ssh') {
    return t('nomadSetupPage.nomadAddressSourceSsh', { address: option.value });
  }
  return t('nomadSetupPage.nomadAddressSourceInterface', { name: option.name || '-', address: option.value });
}

function canJoinNode(node: ProjectedNomadNodeDto) {
  return Boolean(node.serverId && node.joinEligible) && ['missing', 'nomad_unreachable', 'failed'].includes(node.status || '');
}

function canRedeployNode(node: ProjectedNomadNodeDto) {
  return Boolean(node.serverId) && ['server', 'client'].includes(node.role || '') && !['bootstrapping', 'joining', 'registering', 'rebuilding', 'removing'].includes(node.status || '');
}

function canRemoveNode(node: ProjectedNomadNodeDto) {
  return Boolean(node.serverId || node.nodeId) && !['removing', 'joining', 'bootstrapping', 'rebuilding'].includes(node.status || '');
}

async function load() {
  loading.value = true;
  try {
    const result = await nomadApi.controlPlane();
    controlPlane.value = result;
    error.value = '';
    const linkedTaskId = routeTaskId();
    if (linkedTaskId && !operationTaskId.value) {
      showTaskMessage(linkedTaskId, t('nomadNodesPage.bootstrapStarted'));
    }
    if (result.status === 'unconfigured' || result.status === 'migration_required') {
      await router.replace('/nomad/setup');
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('nomadNodesPage.loadFailed');
  } finally {
    loading.value = false;
  }
}

async function joinSelectedServer() {
  if (!selectedServerId.value || !selectedJoinAddress.value) return;
  joining.value = true;
  try {
    const result = await nomadApi.joinServer({
      serverId: selectedServerId.value,
      advertiseAddress: selectedJoinAddress.value,
    });
    showTaskMessage(result.taskId, t('nomadNodesPage.joinStarted'));
    joinDialog.value = false;
    error.value = '';
    await load();
  } catch (err) {
    clearTaskMessage();
    error.value = err instanceof Error ? err.message : t('nomadNodesPage.joinFailed');
  } finally {
    joining.value = false;
  }
}

async function redeploySelectedNode() {
  const node = redeployingNode.value;
  if (!node?.serverId || !selectedRedeployAddress.value) return;
  actionLoading.value = `redeploy:${node.serverId}`;
  try {
    const result = await nomadApi.redeployNode({
      serverId: node.serverId,
      role: node.role,
      advertiseAddress: selectedRedeployAddress.value,
    });
    showTaskMessage(result.taskId, t('nomadNodesPage.redeployStarted'));
    redeployDialog.value = false;
    redeployingNode.value = null;
    error.value = '';
    await load();
  } catch (err) {
    clearTaskMessage();
    error.value = err instanceof Error ? err.message : t('nomadNodesPage.redeployFailed');
  } finally {
    actionLoading.value = '';
  }
}

async function rebuildSelectedCluster() {
  if (!selectedRebuildServerId.value || !selectedRebuildAddress.value) return;
  actionLoading.value = 'rebuild-cluster';
  try {
    const result = await nomadApi.rebuildCluster({
      serverId: selectedRebuildServerId.value,
      advertiseAddress: selectedRebuildAddress.value,
    });
    showTaskMessage(result.taskId, t('nomadNodesPage.rebuildStarted'));
    rebuildDialog.value = false;
    error.value = '';
    await load();
  } catch (err) {
    clearTaskMessage();
    error.value = err instanceof Error ? err.message : t('nomadNodesPage.rebuildFailed');
  } finally {
    actionLoading.value = '';
  }
}

async function switchSelectedServer() {
  if (!selectedSwitchServerId.value) return;
  actionLoading.value = 'switch-server';
  try {
    const result = await nomadApi.switchServer({
      serverId: selectedSwitchServerId.value,
      advertiseAddress: selectedSwitchAddress.value,
    });
    showTaskMessage(result.taskId, t('nomadNodesPage.switchStarted'));
    switchDialog.value = false;
    error.value = '';
    await load();
  } catch (err) {
    clearTaskMessage();
    error.value = err instanceof Error ? err.message : t('nomadNodesPage.switchFailed');
  } finally {
    actionLoading.value = '';
  }
}

function askRemoveNode(node: ProjectedNomadNodeDto) {
  removingNode.value = node;
  removeDialog.value = true;
}

async function removeSelectedNode() {
  const node = removingNode.value;
  if (!node) return;
  actionLoading.value = `remove:${node.serverId || node.nodeId}`;
  try {
    const result = await nomadApi.removeNode({ serverId: node.serverId, nodeId: node.nodeId });
    showTaskMessage(result.taskId, t('nomadNodesPage.removeStarted'));
    removeDialog.value = false;
    removingNode.value = null;
    error.value = '';
    await load();
  } catch (err) {
    clearTaskMessage();
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
    const result = await nomadApi.updateReverseProxy({
      serverId: node.serverId,
      enabled: proxyForm.value.enabled,
      staticFiles: staticSites.length > 0,
      staticSites,
    });
    showTaskMessage(result.taskId, t('nomadNodesPage.proxySyncStarted'));
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
    <v-alert v-else-if="operationMessage" type="info" variant="tonal" closable @click:close="operationMessage = ''">
      <div class="task-alert">
        <span>{{ operationMessage }}</span>
        <v-btn v-if="operationTaskId" size="small" variant="text" :to="taskRoute()" class="text-none">{{ t('taskCenter.task') }}</v-btn>
      </div>
    </v-alert>
    <v-alert v-else-if="controlPlane?.status === 'bootstrapping'" type="info" variant="tonal">
      {{ t('nomadNodesPage.bootstrappingHint') }}
    </v-alert>
    <v-alert v-else-if="controlPlane?.status === 'degraded'" type="warning" variant="tonal">
      {{ t('nomadNodesPage.degradedHint') }}
    </v-alert>
    <PageLoadingState v-if="loading && nodes.length === 0" min-height="340px" />

    <template v-else>
    <div class="summary-strip">
      <v-card variant="outlined" class="summary-card"><div class="text-caption text-medium-emphasis">{{ t('nomadNodesPage.nodes') }}</div><div class="text-h5 font-weight-bold font-tabular">{{ nodes.length }}</div></v-card>
      <v-card variant="outlined" class="summary-card"><div class="text-caption text-medium-emphasis">{{ t('nomadNodesPage.ready') }}</div><div class="text-h5 font-weight-bold font-tabular">{{ readyCount }}</div></v-card>
      <v-card variant="outlined" class="summary-card"><div class="text-caption text-medium-emphasis">{{ t('nomadNodesPage.managed') }}</div><div class="text-h5 font-weight-bold font-tabular">{{ managedCount }}</div></v-card>
      <v-card variant="outlined" class="summary-card"><div class="text-caption text-medium-emphasis">{{ t('nomadNodesPage.pending') }}</div><div class="text-h5 font-weight-bold font-tabular">{{ pendingCount }}</div></v-card>
    </div>

    <v-card variant="outlined" :loading="loading">
      <div class="app-card-header">
        <v-btn prepend-icon="mdi-account-network" color="primary" variant="flat" class="text-none action-btn" :disabled="candidateServers.length === 0" @click="openJoinDialog">{{ t('nomadNodesPage.joinNode') }}</v-btn>
        <v-btn prepend-icon="mdi-swap-horizontal" variant="outlined" class="text-none action-btn" :disabled="switchServerOptions.length === 0" :loading="actionLoading === 'switch-server'" @click="openSwitchDialog">{{ t('nomadNodesPage.switchServer') }}</v-btn>
        <v-btn prepend-icon="mdi-database-refresh" color="warning" variant="outlined" class="text-none action-btn" :disabled="rebuildServerOptions.length === 0" :loading="actionLoading === 'rebuild-cluster'" @click="openRebuildDialog">{{ t('nomadNodesPage.rebuildCluster') }}</v-btn>
      </div>
      <v-table>
        <thead><tr><th>{{ t('common.name') }}</th><th>{{ t('nomadNodesPage.nodeId') }}</th><th>{{ t('serversPage.host') }}</th><th>{{ t('nomadNodesPage.role') }}</th><th>{{ t('common.status') }}</th><th>{{ t('nomadNodesPage.reverseProxy') }}</th><th>{{ t('packagesPage.source') }}</th><th class="text-right">{{ t('common.actions') }}</th></tr></thead>
        <tbody>
          <tr v-for="node in pagedNodes" :key="node.nodeId || node.serverId || node.name">
            <td class="font-weight-bold">{{ node.name || '-' }}</td>
            <td class="mono">{{ node.nodeId || '-' }}</td>
            <td>{{ node.host || '-' }}</td>
            <td><v-chip size="small" variant="tonal" label>{{ translateNomadNodeRole(node.role) }}</v-chip></td>
            <td>
              <v-chip :color="statusColor(node.status)" size="small" variant="tonal" label>{{ translateNomadNodeStatus(node.status) }}</v-chip>
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
              <v-chip :color="kindColor(node.kind)" size="small" variant="tonal" label>{{ translateNomadNodeKind(node.kind) }}</v-chip>
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
                  @click="openJoinDialog(node.serverId)"
                >
                  {{ t('nomadNodesPage.join') }}
                </v-btn>
                <v-btn
                  v-if="canRedeployNode(node)"
                  size="small"
                  variant="text"
                  color="primary"
                  prepend-icon="mdi-refresh"
                  class="text-none"
                  :loading="actionLoading === `redeploy:${node.serverId}`"
                  @click="openRedeployDialog(node)"
                >
                  {{ t('nomadNodesPage.redeploy') }}
                </v-btn>
                <v-btn
                  v-if="canRemoveNode(node)"
                  size="small"
                  variant="text"
                  color="error"
                  prepend-icon="mdi-delete"
                  class="text-none"
                  :loading="actionLoading === `remove:${node.serverId || node.nodeId}`"
                  @click="askRemoveNode(node)"
                >
                  {{ t('nomadNodesPage.remove') }}
                </v-btn>
              </div>
            </td>
          </tr>
          <tr v-if="nodes.length === 0"><td colspan="8" class="text-center py-8 text-medium-emphasis">{{ t('nomadNodesPage.noProjectedNodes') }}</td></tr>
        </tbody>
      </v-table>
      <AppPagination v-model:page="page" v-model:page-size="pageSize" :total="total" />
    </v-card>
    </template>

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
          <v-select
            v-model="selectedJoinAddress"
            :items="joinAddressOptions"
            item-title="label"
            item-value="value"
            :label="t('nomadSetupPage.nomadAddress')"
            :hint="t('nomadSetupPage.nomadAddressHint')"
            persistent-hint
            variant="outlined"
            density="comfortable"
            class="mb-4"
          />
          <v-alert v-if="selectedServer && joinAddressOptions.length === 0" type="warning" variant="tonal" density="compact" class="mb-4">
            {{ t('nomadSetupPage.noNetworkAddresses') }}
          </v-alert>
          <div v-if="selectedServer" class="server-preview">
            <div class="font-weight-bold">{{ selectedServer.name }}</div>
            <div class="text-caption text-medium-emphasis">{{ selectedServer.host }}:{{ selectedServer.port }}</div>
          </div>
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <v-btn variant="text" class="text-none" @click="joinDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="primary" variant="flat" class="text-none" :loading="joining" :disabled="!selectedServerId || !selectedJoinAddress" @click="joinSelectedServer">{{ t('nomadNodesPage.joinNode') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="switchDialog" width="560">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('nomadNodesPage.switchServerTitle') }}</span>
          <v-btn icon="mdi-close" variant="text" @click="switchDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          <v-alert v-if="switchServerOptions.length === 0" type="info" variant="tonal" class="mb-4">
            {{ t('nomadNodesPage.noSwitchCandidates') }}
          </v-alert>
          <v-select
            v-model="selectedSwitchServerId"
            :items="switchServerOptions"
            item-title="label"
            item-value="value"
            :label="t('nomadNodesPage.sshServer')"
            variant="outlined"
            density="comfortable"
            class="mb-4"
          />
          <v-select
            v-model="selectedSwitchAddress"
            :items="switchAddressOptions"
            item-title="label"
            item-value="value"
            :label="t('nomadSetupPage.nomadAddress')"
            variant="outlined"
            density="comfortable"
            class="mb-4"
          />
          <v-alert v-if="selectedSwitchServer && switchAddressOptions.length === 0" type="warning" variant="tonal" density="compact" class="mb-4">
            {{ t('nomadSetupPage.noNetworkAddresses') }}
          </v-alert>
          <v-alert type="info" variant="tonal" density="compact">
            {{ t('nomadNodesPage.switchServerHint') }}
          </v-alert>
          <div v-if="selectedSwitchServer" class="server-preview mt-4">
            <div class="font-weight-bold">{{ selectedSwitchServer.name }}</div>
            <div class="text-caption text-medium-emphasis">{{ selectedSwitchServer.host || selectedSwitchServer.id }}</div>
          </div>
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <v-btn variant="text" class="text-none" @click="switchDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="primary" variant="flat" class="text-none" :loading="actionLoading === 'switch-server'" :disabled="!selectedSwitchServerId || !selectedSwitchAddress" @click="switchSelectedServer">{{ t('nomadNodesPage.switchServer') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="rebuildDialog" width="620">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('nomadNodesPage.rebuildClusterTitle') }}</span>
          <v-btn icon="mdi-close" variant="text" @click="rebuildDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          <v-alert v-if="rebuildServerOptions.length === 0" type="info" variant="tonal" class="mb-4">
            {{ t('nomadNodesPage.noRebuildCandidates') }}
          </v-alert>
          <v-alert type="warning" variant="tonal" density="compact" class="mb-4">
            {{ t('nomadNodesPage.rebuildClusterHint') }}
          </v-alert>
          <v-select
            v-model="selectedRebuildServerId"
            :items="rebuildServerOptions"
            item-title="label"
            item-value="value"
            :label="t('nomadNodesPage.sshServer')"
            variant="outlined"
            density="comfortable"
            class="mb-4"
          />
          <v-select
            v-model="selectedRebuildAddress"
            :items="rebuildAddressOptions"
            item-title="label"
            item-value="value"
            :label="t('nomadSetupPage.nomadAddress')"
            :hint="t('nomadSetupPage.nomadAddressHint')"
            persistent-hint
            variant="outlined"
            density="comfortable"
            class="mb-4"
          />
          <v-alert v-if="selectedRebuildServer && rebuildAddressOptions.length === 0" type="error" variant="tonal" density="compact" class="mb-4">
            {{ t('nomadSetupPage.noNetworkAddresses') }}
          </v-alert>
          <div v-if="selectedRebuildServer" class="server-preview">
            <div class="font-weight-bold">{{ selectedRebuildServer.name }}</div>
            <div class="text-caption text-medium-emphasis">{{ selectedRebuildServer.host }}:{{ selectedRebuildServer.port }}</div>
          </div>
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <v-btn variant="text" class="text-none" @click="rebuildDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="warning" variant="flat" class="text-none" :loading="actionLoading === 'rebuild-cluster'" :disabled="!selectedRebuildServerId || !selectedRebuildAddress" @click="rebuildSelectedCluster">{{ t('nomadNodesPage.rebuildCluster') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="redeployDialog" width="560">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('nomadNodesPage.redeployNodeTitle') }}</span>
          <v-btn icon="mdi-close" variant="text" @click="redeployDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          <v-alert type="info" variant="tonal" density="compact" class="mb-4">
            {{ t('nomadNodesPage.redeployNodeHint') }}
          </v-alert>
          <div v-if="redeployingNode" class="server-preview mb-4">
            <div class="font-weight-bold">{{ redeployingNodeName }}</div>
            <div class="text-caption text-medium-emphasis">{{ redeployingNode.host || redeployingNode.serverId }}</div>
          </div>
          <v-select
            v-model="selectedRedeployAddress"
            :items="redeployAddressOptions"
            item-title="label"
            item-value="value"
            :label="t('nomadSetupPage.nomadAddress')"
            :hint="t('nomadSetupPage.nomadAddressHint')"
            persistent-hint
            variant="outlined"
            density="comfortable"
            class="mb-4"
          />
          <v-alert v-if="redeployingNode && redeployAddressOptions.length === 0" type="warning" variant="tonal" density="compact">
            {{ t('nomadSetupPage.noNetworkAddresses') }}
          </v-alert>
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <v-btn variant="text" class="text-none" @click="redeployDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="primary" variant="flat" class="text-none" :loading="actionLoading === `redeploy:${redeployingNode?.serverId}`" :disabled="!redeployingNode?.serverId || !selectedRedeployAddress" @click="redeploySelectedNode">{{ t('nomadNodesPage.redeploy') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="removeDialog" width="480">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('nomadNodesPage.removeNodeTitle') }}</span>
          <v-btn icon="mdi-close" variant="text" @click="removeDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          {{ t('nomadNodesPage.removeNodeConfirm', { name: removingNodeName }) }}
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <v-btn variant="text" class="text-none" @click="removeDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="error" variant="flat" class="text-none" :loading="actionLoading.startsWith('remove:')" @click="removeSelectedNode">{{ t('nomadNodesPage.remove') }}</v-btn>
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
.app-card-header { display: flex; justify-content: flex-end; gap: 8px; flex-wrap: wrap; }
.mono { font-size: 0.8rem; }
.row-actions { display: inline-flex; justify-content: flex-end; gap: 4px; min-width: 0; flex-wrap: wrap; }
.proxy-summary { display: flex; gap: 8px; align-items: center; min-width: 180px; }
.task-alert { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.section-title { margin-top: 10px; }
.repeat-row { display: grid; gap: 8px; align-items: center; margin-bottom: 8px; }
.static-site-row { grid-template-columns: minmax(0, 1fr) minmax(0, 1.2fr) 180px 40px; }
.server-preview { border-radius: 8px; padding: 12px; background: rgba(var(--v-theme-primary), 0.06); }
@media (max-width: 900px) {
  .summary-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .static-site-row { grid-template-columns: 1fr; }
}
</style>
