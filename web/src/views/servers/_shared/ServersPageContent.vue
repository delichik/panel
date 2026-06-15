<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import { useI18n } from '@/i18n';
import { serversApi, type CredentialInput, type ServerInput } from '@/api/servers';
import { tasksApi } from '@/api/tasks';
import type { CredentialDto, ServerDto, TaskDto } from '@/types/api';
import AppPagination from '@/components/AppPagination.vue';
import PageLoadingState from '@/components/PageLoadingState.vue';
import { usePagination } from '@/composables/usePagination';

const route = useRoute();
const servers = ref<ServerDto[]>([]);
const credentials = ref<CredentialDto[]>([]);
const selectedServerId = ref('');
const loading = ref(false);
const error = ref('');
const serverDialog = ref(false);
const credentialDialog = ref(false);
const editing = ref<ServerDto | null>(null);
const editingCredential = ref<CredentialDto | null>(null);
const serverSaving = ref(false);
const creatingServerTaskId = ref('');
const ufwInstalling = ref<Record<string, boolean>>({});
const restarting = ref<Record<string, boolean>>({});
const agentDeploying = ref<Record<string, boolean>>({});
const defaultDockerHost = 'unix:///var/run/docker.sock';

const activeTab = computed(() => (route.name === 'credentials' ? 'credentials' : 'servers'));

const snackbar = ref(false);
const snackbarText = ref('');
const snackbarColor = ref('success');
const snackbarTaskId = ref('');
const { t, formatDateTime } = useI18n();

const confirmDialog = ref(false);
const confirmTitle = ref(t('common.confirm'));
const confirmMessage = ref('');
const confirmAction = ref<(() => Promise<void>) | null>(null);
const confirmLoading = ref(false);

const serverForm = reactive({
  name: '',
  host: '',
  port: 22,
  sshUsername: '',
  credentialId: '',
  dockerHost: defaultDockerHost,
  traitsRaw: [] as string[],
  notes: '',
});

const credentialForm = reactive<CredentialInput>({
  name: '',
  type: 'password',
  username: '',
  password: '',
  privateKey: '',
  passphrase: '',
});

const selectedServer = computed(() => servers.value.find((server) => server.id === selectedServerId.value) ?? null);
const reachableCount = computed(() => servers.value.filter((server) => server.reachable).length);
const agentReadyCount = computed(() => servers.value.filter((server) => server.traits?.['agent.status'] === 'compatible').length);
const credentialRows = computed(() => credentials.value ?? []);
const serverCredentialMissing = computed(() => !serverForm.credentialId);
const serverDockerHostMissing = computed(() => !serverForm.dockerHost.trim());
const {
  page: serverPage,
  pageSize: serverPageSize,
  total: serverTotal,
  pageItems: pagedServers,
} = usePagination(servers);
const {
  page: credentialPage,
  pageSize: credentialPageSize,
  total: credentialTotal,
  pageItems: pagedCredentialRows,
} = usePagination(credentialRows);

interface NetworkAddress {
  family: string;
  address: string;
}

interface NetworkInterface {
  name: string;
  addresses: NetworkAddress[];
}

const credentialOptions = computed(() =>
  credentialRows.value.map((credential) => ({
    label: `${credential.name} (${credential.username}, ${credential.type})`,
    value: credential.id,
  })),
);

watch(activeTab, () => {
  if (activeTab.value === 'servers' && !selectedServerId.value && servers.value.length) {
    selectedServerId.value = servers.value[0].id;
  }
});

function showMessage(text: string, color = 'success', taskId = '') {
  snackbarText.value = text;
  snackbarColor.value = color;
  snackbarTaskId.value = taskId;
  snackbar.value = true;
}

function taskRoute(taskId = snackbarTaskId.value) {
  return taskId ? { path: '/tasks', query: { task: taskId } } : '/tasks';
}

function confirm(title: string, message: string, action: () => Promise<void>) {
  confirmTitle.value = title;
  confirmMessage.value = message;
  confirmAction.value = action;
  confirmDialog.value = true;
}

async function executeConfirm() {
  if (!confirmAction.value || confirmLoading.value) return;
  confirmLoading.value = true;
  try {
      await confirmAction.value();
  } catch (err) {
    showMessage(err instanceof Error ? err.message : t('serversPage.actionFailed'), 'error');
  } finally {
    confirmLoading.value = false;
    confirmDialog.value = false;
  }
}

function credentialById(id?: string | null) {
  return credentialRows.value.find((credential) => credential.id === id);
}

function resetServerForm(server?: ServerDto) {
  editing.value = server ?? null;
  creatingServerTaskId.value = '';

  const traitsRaw: string[] = [];
  if (server?.traits) {
    for (const [key, value] of Object.entries(server.traits)) {
      if (key.startsWith('custom.')) traitsRaw.push(`${key.substring(7)}=${value}`);
      else if (!key.startsWith('sys.')) traitsRaw.push(`${key}=${value}`);
    }
  }

  Object.assign(serverForm, {
    name: server?.name ?? '',
    host: server?.host ?? '',
    port: server?.port ?? 22,
    sshUsername: server?.sshUsername ?? '',
    credentialId: server?.credentialId || credentialRows.value[0]?.id || '',
    dockerHost: server?.dockerHost || defaultDockerHost,
    traitsRaw,
    notes: server?.notes ?? '',
  });
  serverDialog.value = true;
}

function resetCredentialForm() {
  editingCredential.value = null;
  Object.assign(credentialForm, {
    name: '',
    type: 'password',
    username: '',
    password: '',
    privateKey: '',
    passphrase: '',
  });
  credentialDialog.value = true;
}

function editCredential(credential: CredentialDto) {
  editingCredential.value = credential;
  Object.assign(credentialForm, {
    name: credential.name,
    type: credential.type,
    username: credential.username,
    password: '',
    privateKey: '',
    passphrase: '',
  });
  credentialDialog.value = true;
}

async function load() {
  loading.value = true;
  try {
    const [serverRows, credentialRows] = await Promise.all([
      serversApi.listServers(),
      serversApi.listCredentials(),
    ]);
    servers.value = serverRows;
    credentials.value = credentialRows;
    if (!selectedServerId.value && serverRows.length) selectedServerId.value = serverRows[0].id;
    if (selectedServerId.value && !serverRows.some((server) => server.id === selectedServerId.value)) {
      selectedServerId.value = serverRows[0]?.id ?? '';
    }
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('serversPage.unableToLoadServers');
  } finally {
    loading.value = false;
  }
}

function sleep(ms: number) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

function taskTerminal(task: TaskDto) {
  return ['completed', 'failed', 'failed_retryable', 'blocked', 'cancelled'].includes(task.status);
}

async function waitForInitialServerTask(taskId: string) {
  for (let attempt = 0; attempt < 90; attempt++) {
    const task = await tasksApi.get(taskId);
    if (taskTerminal(task)) return task;
    await sleep(1500);
  }
  throw new Error(t('serversPage.serverInfoTaskTimeout'));
}

function buildServerPayload(): ServerInput {
  const traits: Record<string, string> = {};
  for (const raw of serverForm.traitsRaw) {
    const parts = raw.split('=');
    if (parts.length >= 2) {
      let key = parts[0].trim();
      const value = parts.slice(1).join('=').trim();
      if (key) {
        if (!key.startsWith('custom.') && !key.startsWith('sys.') && !key.startsWith('agent.')) key = 'custom.' + key;
        traits[key] = value;
      }
    }
  }

  return {
    name: serverForm.name,
    host: serverForm.host,
    port: serverForm.port,
    sshUsername: serverForm.sshUsername,
    credentialId: serverForm.credentialId.trim(),
    dockerHost: serverForm.dockerHost.trim(),
    traits,
    notes: serverForm.notes,
  };
}

async function saveServer() {
  if (serverCredentialMissing.value) {
    showMessage(t('serversPage.credentialRequired'), 'error');
    return;
  }
  if (serverDockerHostMissing.value) {
    showMessage(t('serversPage.dockerHostRequired'), 'error');
    return;
  }
  serverSaving.value = true;
  try {
    const payload = buildServerPayload();
    if (editing.value) {
      const saved = await serversApi.updateServer(editing.value.id, payload);
      selectedServerId.value = saved.id;
      showMessage(t('serversPage.serverUpdated'));
      serverDialog.value = false;
      await load();
      return;
    }

    const saved = await serversApi.createServer(payload);
    selectedServerId.value = saved.id;
    if (!saved.initialTaskId) {
      showMessage(t('serversPage.serverCreated'));
      serverDialog.value = false;
      await load();
      return;
    }

    creatingServerTaskId.value = saved.initialTaskId;
    showMessage(t('serversPage.serverInfoTaskStarted'), 'success', saved.initialTaskId);
    const task = await waitForInitialServerTask(saved.initialTaskId);
    await load();
    if (task.status === 'completed') {
      showMessage(t('serversPage.serverCreated'), 'success', saved.initialTaskId);
      serverDialog.value = false;
    } else {
      showMessage(task.error ? t('serversPage.serverInfoTaskFailedWithError', { error: task.error }) : t('serversPage.serverInfoTaskFailed'), 'error', saved.initialTaskId);
    }
  } catch (err) {
    showMessage(err instanceof Error ? err.message : t('serversPage.saveServerFailed'), 'error');
  } finally {
    serverSaving.value = false;
    creatingServerTaskId.value = '';
  }
}

async function saveCredential() {
  try {
    const input = { ...credentialForm };
    if (input.type === 'password') {
      delete input.privateKey;
      delete input.passphrase;
    } else {
      delete input.password;
    }
    const credential = editingCredential.value
      ? await serversApi.updateCredential(editingCredential.value.id, input)
      : await serversApi.createCredential(input);
    serverForm.credentialId = credential.id;
    credentialDialog.value = false;
    showMessage(editingCredential.value ? t('serversPage.credentialUpdated') : t('serversPage.credentialCreated'));
    await load();
  } catch (err) {
    showMessage(err instanceof Error ? err.message : t('serversPage.saveCredentialFailed'), 'error');
  }
}

async function deleteServer(server: ServerDto) {
  confirm(t('serversPage.confirmDelete'), t('serversPage.deleteServerConfirm', { name: server.name }), async () => {
    await serversApi.deleteServer(server.id);
    showMessage(t('serversPage.serverDeleted'));
    await load();
  });
}

async function deleteCredential(credential: CredentialDto) {
  confirm(t('serversPage.confirmDelete'), t('serversPage.deleteCredentialConfirm', { name: credential.name }), async () => {
    await serversApi.deleteCredential(credential.id);
    showMessage(t('serversPage.credentialDeleted'));
    await load();
  });
}

function canRestart(server: ServerDto | null) {
  return Boolean(server?.reachable && server.sudo?.passwordless);
}

function restartServer(server: ServerDto) {
  confirm(t('serversPage.confirmRestart'), t('serversPage.restartServerConfirm', { name: server.name }), async () => {
    restarting.value = { ...restarting.value, [server.id]: true };
    try {
      const result = await serversApi.restartServer(server.id);
      showMessage(t('serversPage.restartStarted'), 'success', result.taskId);
    } finally {
      const next = { ...restarting.value };
      delete next[server.id];
      restarting.value = next;
    }
  });
}

function formatDate(value?: string | null) {
  return value ? formatDateTime(value) : t('common.never');
}

function traitValue(server: ServerDto | null, key: string) {
  return server?.traits?.[key] || t('common.notAvailable');
}

function agentStatusForServer(server: ServerDto | null) {
  const traits = server?.traits;
  if (traits?.['agent.enabled'] !== 'true' || !traits?.['agent.url']) {
    return { label: t('serversPage.agentNotDeployed'), color: 'secondary' };
  }
  if (traits['agent.status'] === 'compatible') {
    return { label: t('serversPage.agentCompatible'), color: 'success' };
  }
  if (traits['agent.status'] === 'unavailable') {
    return { label: t('serversPage.agentUnavailable'), color: 'error' };
  }
  return { label: t('serversPage.agentIncompatible'), color: 'warning' };
}

function shouldDeployAgent(server: ServerDto | null) {
  return agentStatusForServer(server).color !== 'success';
}

function agentDeployActionLabel(server: ServerDto | null) {
  const traits = server?.traits;
  if (traits?.['agent.enabled'] !== 'true' || !traits?.['agent.url']) return t('serversPage.installAgent');
  return t('serversPage.reinstallAgent');
}

async function deployAgent(server: ServerDto) {
  agentDeploying.value = { ...agentDeploying.value, [server.id]: true };
  try {
    const result = await serversApi.deployAgent(server.id);
    showMessage(t('serversPage.agentDeployStarted'), 'success', result.taskId);
    await load();
  } catch (err) {
    showMessage(err instanceof Error ? err.message : t('serversPage.agentDeployFailed'), 'error');
  } finally {
    const next = { ...agentDeploying.value };
    delete next[server.id];
    agentDeploying.value = next;
  }
}

function networkInterfaces(traits?: Record<string, string> | null): NetworkInterface[] {
  const grouped = new Map<string, NetworkAddress[]>();
  for (const raw of (traits?.['sys.network_interfaces'] || '').split(', ')) {
    const [name, family = '', address = ''] = raw.split('|');
    if (!name?.trim()) continue;
    const addresses = grouped.get(name) ?? [];
    if (family || address) addresses.push({ family, address });
    grouped.set(name, addresses);
  }
  return Array.from(grouped, ([name, addresses]) => ({ name, addresses }));
}

function networkFamilyLabel(family: string) {
  if (family === 'inet') return 'IPv4';
  if (family === 'inet6') return 'IPv6';
  return family || t('serversPage.linkOnly');
}

function ufwStatusFromTraits(traits?: Record<string, string> | null) {
  const supported = traits?.['sys.ufw_supported'] === 'true';
  if (traits?.['sys.ufw_supported'] === 'false') {
    return { label: t('serversPage.ufwUnsupported'), color: 'secondary', installed: false, supported: false };
  }
  if (traits?.['sys.ufw_installed'] === 'true') {
    if (traits?.['sys.ufw_active'] === 'true') return { label: t('serversPage.ufwActive'), color: 'success', installed: true, supported };
    return { label: t('serversPage.ufwInstalled'), color: 'primary', installed: true, supported };
  }
  if (traits?.['sys.ufw_installed'] === 'false' && supported) {
    return { label: t('serversPage.ufwNotInstalled'), color: 'warning', installed: false, supported: true };
  }
  return { label: t('common.unknown'), color: 'secondary', installed: false, supported: false };
}

function ufwStatusForServer(server: ServerDto | null) {
  return ufwStatusFromTraits(server?.traits);
}

function canInstallUFW(server: ServerDto | null) {
  if (!server) return false;
  const status = ufwStatusForServer(server);
  return server.reachable && server.sudo?.passwordless === true && status.supported && !status.installed;
}

async function installUFW(server: ServerDto) {
  ufwInstalling.value = { ...ufwInstalling.value, [server.id]: true };
  try {
    const result = await serversApi.installUFW(server.id);
    showMessage(t('serversPage.ufwInstallStarted'), 'success', result.taskId);
    await load();
  } catch (err) {
    showMessage(err instanceof Error ? err.message : t('serversPage.ufwInstallFailed'), 'error');
  } finally {
    const next = { ...ufwInstalling.value };
    delete next[server.id];
    ufwInstalling.value = next;
  }
}

function memoryLabel(server: ServerDto | null) {
  const mb = Number(server?.traits?.['sys.memory_total_mb'] || 0);
  return mb > 0 ? `${(mb / 1024).toFixed(1)} GB` : t('common.notAvailable');
}

function diskLabel(server: ServerDto | null) {
  const gb = Number(server?.traits?.['sys.disk_total_gb'] || 0);
  return gb > 0 ? `${gb} GB` : t('common.notAvailable');
}

function customTraits(server: ServerDto | null) {
  if (!server?.traits) return [];
  return Object.entries(server.traits).filter(([key]) => key.startsWith('custom.'));
}

onMounted(load);
</script>

<template>
  <div class="page-shell">
    <v-alert v-if="error" type="error" variant="tonal">{{ error }}</v-alert>

    <template v-if="activeTab === 'servers'">
      <PageLoadingState v-if="loading && servers.length === 0" min-height="340px" />

      <template v-else>
      <div class="summary-strip">
        <v-card variant="outlined" class="summary-card">
          <div class="summary-icon surface-primary"><v-icon size="18">mdi-server</v-icon></div>
          <div><div class="text-caption text-medium-emphasis">{{ t('serversPage.servers') }}</div><div class="text-h5 font-weight-bold font-tabular">{{ servers.length }}</div></div>
        </v-card>
        <v-card variant="outlined" class="summary-card">
          <div class="summary-icon surface-success"><v-icon size="18">mdi-lan-connect</v-icon></div>
          <div><div class="text-caption text-medium-emphasis">{{ t('serversPage.reachable') }}</div><div class="text-h5 font-weight-bold font-tabular">{{ reachableCount }}</div></div>
        </v-card>
        <v-card variant="outlined" class="summary-card">
          <div class="summary-icon surface-warning"><v-icon size="18">mdi-docker</v-icon></div>
          <div><div class="text-caption text-medium-emphasis">{{ t('serversPage.agentReady') }}</div><div class="text-h5 font-weight-bold font-tabular">{{ agentReadyCount }}</div></div>
        </v-card>
      </div>

      <div class="servers-workspace">
        <v-card variant="outlined" :loading="loading" class="server-list">
          <div class="list-header">
            <div class="list-header-main">
              <v-btn
                color="primary"
                prepend-icon="mdi-plus"
                variant="flat"
                class="text-none font-weight-bold action-btn"
                @click="resetServerForm()"
              >
                {{ t('serversPage.addServer') }}
              </v-btn>
              <div class="text-subtitle-1 font-weight-bold">{{ t('serversPage.registeredServers') }}</div>
            </div>
            <v-chip size="small" variant="tonal" color="primary" label>{{ t('common.total', { count: servers.length }) }}</v-chip>
          </div>

          <div class="server-list-body">
            <button
              v-for="server in pagedServers"
              :key="server.id"
              class="server-row"
              :class="{ selected: selectedServerId === server.id }"
              type="button"
              @click="selectedServerId = server.id"
            >
              <span class="min-width-0">
                <span class="server-name text-truncate">{{ server.name }}</span>
                <span class="server-meta text-truncate">{{ server.host }}:{{ server.port }}</span>
              </span>
              <v-chip :color="agentStatusForServer(server).color" size="x-small" variant="tonal" label>
                {{ agentStatusForServer(server).label }}
              </v-chip>
            </button>
            <div v-if="servers.length === 0" class="empty-list">
              <v-icon size="32" color="medium-emphasis">mdi-server-off</v-icon>
              <div class="text-body-2 text-medium-emphasis">{{ t('serversPage.noServers') }}</div>
            </div>
          </div>
          <AppPagination v-model:page="serverPage" v-model:page-size="serverPageSize" :total="serverTotal" />
        </v-card>

        <div class="detail-column">
          <v-card v-if="selectedServer" variant="outlined" class="detail-card">
            <div class="detail-header">
              <div class="min-width-0">
                <div class="d-flex align-center ga-2 mb-1">
                  <span class="status-dot" :class="selectedServer.reachable ? 'success' : 'warning'" />
                  <div class="text-h6 font-weight-bold text-truncate">{{ selectedServer.name }}</div>
                </div>
                <div class="text-body-2 text-medium-emphasis">{{ selectedServer.host }}:{{ selectedServer.port }}</div>
              </div>
              <div class="detail-actions">
                <v-btn
                  size="small"
                  color="warning"
                  variant="outlined"
                  prepend-icon="mdi-restart"
                  class="text-none"
                  :loading="restarting[selectedServer.id]"
                  :disabled="!canRestart(selectedServer)"
                  @click="restartServer(selectedServer)"
                >
                  {{ t('serversPage.restart') }}
                </v-btn>
                <v-btn size="small" variant="outlined" prepend-icon="mdi-pencil" class="text-none" @click="resetServerForm(selectedServer)">{{ t('common.edit') }}</v-btn>
                <v-btn size="small" color="error" variant="outlined" prepend-icon="mdi-delete" class="text-none" @click="deleteServer(selectedServer)">{{ t('common.delete') }}</v-btn>
              </div>
            </div>

            <v-alert v-if="selectedServer.lastError" type="warning" variant="tonal" class="mb-4">{{ selectedServer.lastError }}</v-alert>

            <div class="metric-grid mb-4">
              <div class="metric-tile">
                <v-icon color="primary">mdi-cpu-64-bit</v-icon>
                <div><div class="text-caption text-medium-emphasis">{{ t('serversPage.cpuCores') }}</div><div class="font-weight-bold">{{ traitValue(selectedServer, 'sys.cpu_cores') }}</div></div>
              </div>
              <div class="metric-tile">
                <v-icon color="success">mdi-memory</v-icon>
                <div><div class="text-caption text-medium-emphasis">{{ t('serversPage.memory') }}</div><div class="font-weight-bold">{{ memoryLabel(selectedServer) }}</div></div>
              </div>
              <div class="metric-tile">
                <v-icon color="warning">mdi-harddisk</v-icon>
                <div><div class="text-caption text-medium-emphasis">{{ t('serversPage.disk') }}</div><div class="font-weight-bold">{{ diskLabel(selectedServer) }}</div></div>
              </div>
              <div class="metric-tile">
                <v-icon color="info">mdi-chip</v-icon>
                <div><div class="text-caption text-medium-emphasis">{{ t('serversPage.architecture') }}</div><div class="font-weight-bold">{{ traitValue(selectedServer, 'sys.architecture') }}</div></div>
              </div>
            </div>

            <div class="detail-sections">
              <section>
                <div class="section-title">{{ t('serversPage.runtime') }}</div>
                <div class="property-grid">
                  <div>
                    <span>{{ t('serversPage.agent') }}</span>
                    <div class="ufw-actions">
                      <v-chip :color="agentStatusForServer(selectedServer).color" size="small" variant="tonal" label>{{ agentStatusForServer(selectedServer).label }}</v-chip>
                      <v-btn
                        v-if="shouldDeployAgent(selectedServer)"
                        size="small"
                        color="primary"
                        variant="outlined"
                        prepend-icon="mdi-server-network"
                        class="text-none"
                        :loading="agentDeploying[selectedServer.id]"
                        @click="deployAgent(selectedServer)"
                      >
                        {{ agentDeployActionLabel(selectedServer) }}
                      </v-btn>
                    </div>
                  </div>
                  <div><span>{{ t('serversPage.dockerHost') }}</span><strong>{{ selectedServer.dockerHost || defaultDockerHost }}</strong></div>
                  <div><span>{{ t('serversPage.distro') }}</span><v-chip :color="selectedServer.os?.supported ? 'success' : 'warning'" size="small" variant="tonal" label>{{ selectedServer.os?.prettyName || t('common.unknown') }}</v-chip></div>
                  <div>
                    <span>{{ t('serversPage.ufw') }}</span>
                    <div class="ufw-actions">
                      <v-chip :color="ufwStatusForServer(selectedServer).color" size="small" variant="tonal" label>{{ ufwStatusForServer(selectedServer).label }}</v-chip>
                      <v-btn
                        v-if="canInstallUFW(selectedServer)"
                        size="small"
                        color="primary"
                        variant="outlined"
                        prepend-icon="mdi-shield-plus"
                        class="text-none"
                        :loading="ufwInstalling[selectedServer.id]"
                        @click.stop="installUFW(selectedServer)"
                      >
                        {{ t('serversPage.installUfw') }}
                      </v-btn>
                    </div>
                  </div>
                  <div><span>{{ t('serversPage.kernelHost') }}</span><strong>{{ traitValue(selectedServer, 'sys.hostname') }}</strong></div>
                  <div><span>{{ t('serversPage.loadAverage') }}</span><strong>{{ selectedServer.loadAverage || t('common.notAvailable') }}</strong></div>
                  <div><span>{{ t('serversPage.cpuModel') }}</span><strong>{{ traitValue(selectedServer, 'sys.cpu_model') }}</strong></div>
                </div>
              </section>

              <section>
                <div class="section-title">{{ t('serversPage.networkInterfaces') }}</div>
                <div v-if="networkInterfaces(selectedServer.traits).length" class="network-grid">
                  <div v-for="network in networkInterfaces(selectedServer.traits)" :key="network.name" class="network-card">
                    <div class="network-card-title">
                      <v-icon size="18" color="primary">mdi-ethernet</v-icon>
                      <strong>{{ network.name }}</strong>
                    </div>
                    <div v-if="network.addresses.length" class="network-addresses">
                      <div v-for="item in network.addresses" :key="`${item.family}:${item.address}`" class="network-address">
                        <v-chip size="x-small" variant="tonal" color="primary" label>{{ networkFamilyLabel(item.family) }}</v-chip>
                        <code>{{ item.address || t('common.notAvailable') }}</code>
                      </div>
                    </div>
                    <div v-else class="text-caption text-medium-emphasis">{{ t('serversPage.linkOnly') }}</div>
                  </div>
                </div>
                <div v-else class="text-body-2 text-medium-emphasis">{{ t('common.notAvailable') }}</div>
              </section>

              <section>
                <div class="section-title">{{ t('serversPage.access') }}</div>
                <div class="property-grid">
                  <div><span>{{ t('serversPage.sshUser') }}</span><strong>{{ selectedServer.sshUsername || credentialById(selectedServer.credentialId)?.username || t('serversPage.credential') }}</strong></div>
                  <div><span>{{ t('serversPage.credential') }}</span><strong>{{ credentialById(selectedServer.credentialId)?.name || t('common.notAvailable') }}</strong></div>
                  <div><span>{{ t('serversPage.lastChecked') }}</span><strong>{{ formatDate(selectedServer.lastCheckedAt) }}</strong></div>
                  <div><span>{{ t('serversPage.updated') }}</span><strong>{{ formatDate(selectedServer.updatedAt) }}</strong></div>
                  <div v-if="selectedServer.traits?.['agent.version']"><span>{{ t('serversPage.agentVersion') }}</span><strong>{{ selectedServer.traits['agent.version'] }}</strong></div>
                  <div v-if="selectedServer.traits?.['agent.last_checked_at']"><span>{{ t('serversPage.agentLastChecked') }}</span><strong>{{ formatDate(selectedServer.traits['agent.last_checked_at']) }}</strong></div>
                </div>
                <v-alert
                  v-if="selectedServer.traits?.['agent.last_error']"
                  :type="selectedServer.traits?.['agent.status'] === 'unavailable' ? 'error' : 'warning'"
                  variant="tonal"
                  density="compact"
                  class="mt-3"
                >
                  {{ selectedServer.traits['agent.last_error'] }}
                </v-alert>
              </section>

              <section v-if="customTraits(selectedServer).length">
                <div class="section-title">{{ t('serversPage.customTraits') }}</div>
                <div class="trait-list">
                  <v-chip v-for="[key, value] in customTraits(selectedServer)" :key="key" size="small" color="success" variant="tonal" label>
                    {{ key.substring(7) }}={{ value }}
                  </v-chip>
                </div>
              </section>

              <section v-if="selectedServer.notes">
                <div class="section-title">{{ t('serversPage.notes') }}</div>
                <p class="notes">{{ selectedServer.notes }}</p>
              </section>
            </div>
          </v-card>

          <v-card v-else variant="outlined" class="empty-detail">
            <v-icon size="28" color="medium-emphasis">mdi-server-search</v-icon>
            <div class="text-body-2 text-medium-emphasis">{{ t('serversPage.selectServerHint') }}</div>
          </v-card>
        </div>
      </div>
      </template>
    </template>

    <PageLoadingState v-else-if="loading && credentialRows.length === 0" min-height="320px" />

    <v-card v-else variant="outlined" class="credential-table-card">
      <div class="app-card-header">
        <v-btn
          color="primary"
          prepend-icon="mdi-plus"
          variant="flat"
          class="text-none font-weight-bold action-btn"
          @click="resetCredentialForm()"
        >
          {{ t('serversPage.addCredential') }}
        </v-btn>
      </div>
      <v-table class="text-left">
        <thead>
          <tr>
            <th class="font-weight-bold">{{ t('serversPage.name') }}</th>
            <th class="font-weight-bold">{{ t('serversPage.username') }}</th>
            <th class="font-weight-bold">{{ t('common.type') }}</th>
            <th class="font-weight-bold text-right" style="width: 180px;">{{ t('common.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="credentialRows.length === 0">
            <td colspan="4" class="text-center py-6 text-medium-emphasis">{{ t('serversPage.noCredentials') }}</td>
          </tr>
          <tr v-for="row in pagedCredentialRows" :key="row.id">
            <td class="font-weight-bold">{{ row.name }}</td>
            <td>{{ row.username }}</td>
            <td><v-chip size="small" label color="secondary" variant="tonal">{{ row.type }}</v-chip></td>
            <td class="text-right">
              <div class="app-table-actions">
                <v-btn size="small" variant="outlined" prepend-icon="mdi-pencil" @click="editCredential(row)">{{ t('common.edit') }}</v-btn>
                <v-btn size="small" color="error" variant="outlined" prepend-icon="mdi-delete" @click="deleteCredential(row)">{{ t('common.delete') }}</v-btn>
              </div>
            </td>
          </tr>
        </tbody>
      </v-table>
      <AppPagination v-model:page="credentialPage" v-model:page-size="credentialPageSize" :total="credentialTotal" />
    </v-card>

    <v-dialog v-model="serverDialog" width="640">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ editing ? t('serversPage.editServer') : t('serversPage.createServer') }}</span>
          <v-btn icon="mdi-close" variant="text" :disabled="serverSaving" @click="serverDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          <v-form :disabled="serverSaving" @submit.prevent="saveServer">
            <v-alert v-if="credentialRows.length === 0" type="info" variant="tonal" density="compact" class="credential-required-alert mb-3">
              <span>{{ t('serversPage.credentialRequired') }}</span>
              <v-btn size="small" variant="text" class="text-none" @click="resetCredentialForm">{{ t('serversPage.addCredential') }}</v-btn>
            </v-alert>
            <div class="form-grid">
              <v-text-field v-model="serverForm.name" :label="t('serversPage.name')" variant="outlined" density="comfortable" />
              <v-text-field v-model="serverForm.host" :label="t('serversPage.host')" variant="outlined" density="comfortable" />
              <v-text-field v-model.number="serverForm.port" type="number" :label="t('serversPage.port')" variant="outlined" density="comfortable" />
              <v-select
                v-model="serverForm.credentialId"
                :items="credentialOptions"
                item-title="label"
                item-value="value"
                :label="t('serversPage.selectCredential')"
                :placeholder="t('serversPage.selectCredential')"
                variant="outlined"
                density="comfortable"
                :disabled="credentialOptions.length === 0"
                :rules="[value => Boolean(value) || t('serversPage.credentialRequired')]"
              />
              <v-text-field
                v-model="serverForm.sshUsername"
                :label="t('serversPage.sshUsernameOverride')"
                :placeholder="t('serversPage.sshUsernameOverrideHint')"
                variant="outlined"
                density="comfortable"
                class="span-all"
              />
              <v-text-field
                v-model="serverForm.dockerHost"
                :label="t('serversPage.dockerHost')"
                :hint="t('serversPage.dockerHostHint')"
                :rules="[value => Boolean(String(value || '').trim()) || t('serversPage.dockerHostRequired')]"
                variant="outlined"
                density="comfortable"
                persistent-hint
                class="span-all"
              />
              <v-combobox
                v-model="serverForm.traitsRaw"
                :label="t('serversPage.customTraitsLabel')"
                multiple
                chips
                closable-chips
                variant="outlined"
                density="comfortable"
                class="span-all"
                :placeholder="t('serversPage.customTraitsHint')"
              />
              <v-textarea v-model="serverForm.notes" :label="t('serversPage.notesLabel')" variant="outlined" density="comfortable" rows="3" class="span-all" />
            </div>

            <v-alert v-if="creatingServerTaskId" type="info" variant="tonal" density="compact" class="mt-3">
              <div class="server-task-alert">
                <span>{{ t('serversPage.serverInfoTaskRunning') }}</span>
                <v-btn size="small" variant="text" class="text-none" :to="taskRoute(creatingServerTaskId)">{{ t('taskCenter.task') }}</v-btn>
              </div>
            </v-alert>
          </v-form>
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <v-btn variant="text" class="text-none" :disabled="serverSaving" @click="serverDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="primary" variant="flat" class="text-none" :loading="serverSaving" :disabled="serverCredentialMissing || serverDockerHostMissing" @click="saveServer">{{ editing ? t('common.save') : t('common.create') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="credentialDialog" width="680">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ editingCredential ? t('serversPage.editCredential') : t('serversPage.createCredential') }}</span>
          <v-btn icon="mdi-close" variant="text" @click="credentialDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          <v-form @submit.prevent="saveCredential">
            <v-text-field v-model="credentialForm.name" :label="t('serversPage.name')" variant="outlined" density="comfortable" class="mb-3" />
            <div class="text-subtitle-2 mb-2">{{ t('serversPage.credentialType') }}</div>
            <v-btn-toggle v-model="credentialForm.type" mandatory color="primary" density="compact" class="mb-4">
              <v-btn value="password" class="text-none">{{ t('serversPage.password') }}</v-btn>
              <v-btn value="private_key" class="text-none">{{ t('serversPage.privateKey') }}</v-btn>
            </v-btn-toggle>
            <v-text-field v-model="credentialForm.username" :label="t('serversPage.username')" variant="outlined" density="comfortable" class="mb-3" />
            <v-text-field
              v-if="credentialForm.type === 'password'"
              v-model="credentialForm.password"
              type="password"
              :label="t('serversPage.password')"
              :placeholder="t('serversPage.password')"
              variant="outlined"
              density="comfortable"
              class="mb-3"
            />
            <template v-else>
              <v-textarea v-model="credentialForm.privateKey" :label="t('serversPage.privateKey')" :placeholder="t('serversPage.privateKeyHint')" variant="outlined" density="comfortable" rows="8" class="mb-3 font-mono" />
              <v-text-field v-model="credentialForm.passphrase" type="password" :label="t('serversPage.passphrase')" :placeholder="t('serversPage.passphraseHint')" variant="outlined" density="comfortable" class="mb-3" />
            </template>
          </v-form>
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <v-btn variant="text" class="text-none" @click="credentialDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="primary" variant="flat" class="text-none" @click="saveCredential">{{ editingCredential ? t('common.save') : t('common.create') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="confirmDialog" width="420">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ confirmTitle }}</span>
          <v-btn icon="mdi-close" variant="text" @click="confirmDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body text-body-1">{{ confirmMessage }}</v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <v-btn variant="text" class="text-none" @click="confirmDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="error" variant="flat" class="text-none" :loading="confirmLoading" @click="executeConfirm">{{ t('common.confirm') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar" :color="snackbarColor" timeout="3000">
      {{ snackbarText }}
      <template #actions>
        <v-btn v-if="snackbarTaskId" color="white" variant="text" :to="taskRoute()">{{ t('taskCenter.task') }}</v-btn>
        <v-btn color="white" variant="text" @click="snackbar = false">{{ t('common.close') }}</v-btn>
      </template>
    </v-snackbar>
  </div>
</template>

<style scoped>
.summary-strip { max-width: 780px; }
.servers-workspace { display: grid; grid-template-columns: clamp(300px, 26vw, 340px) minmax(0, 1fr); gap: 18px; flex: 1 1 auto; min-height: 0; align-items: stretch; }
.server-list, .detail-column { min-width: 0; min-height: 0; }
.server-list { display: flex; flex-direction: column; overflow: hidden; }
.list-header-main { display: grid; gap: 10px; justify-items: start; }
.server-list-body { display: grid; gap: 8px; min-height: 0; padding: 10px; overflow: auto; }
.server-row { display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: center; gap: 10px; width: 100%; padding: 11px 12px; border: 1px solid transparent; border-radius: 8px; background: transparent; color: inherit; text-align: left; cursor: pointer; transition: background-color 0.16s ease, border-color 0.16s ease; }
.server-row:hover { background: rgba(var(--v-theme-on-surface), 0.025); }
.server-row.selected { border-color: rgba(var(--v-theme-primary), 0.26); background: rgba(var(--v-theme-primary), 0.06); }
.server-name, .server-meta { display: block; }
.server-name { font-weight: 700; font-size: 0.9rem; }
.server-meta { color: var(--lp-text-muted); font-size: 0.76rem; margin-top: 2px; }
.status-dot { flex: 0 0 auto; width: 9px; height: 9px; border-radius: 999px; background: rgb(var(--v-theme-info)); box-shadow: 0 0 0 4px rgba(var(--v-theme-info), 0.12); }
.status-dot.success { background: rgb(var(--v-theme-success)); box-shadow: 0 0 0 4px rgba(var(--v-theme-success), 0.12); }
.status-dot.warning { background: rgb(var(--v-theme-warning)); box-shadow: 0 0 0 4px rgba(var(--v-theme-warning), 0.14); }
.empty-list { display: grid; place-items: center; gap: 10px; min-height: 220px; padding: 32px; text-align: center; }
.detail-column { display: flex; }
.detail-card { display: flex; flex: 1 1 auto; flex-direction: column; min-height: 0; padding: 16px; overflow: hidden; }
.detail-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 14px; margin-bottom: 16px; }
.detail-actions { display: flex; justify-content: flex-end; gap: 8px; flex-wrap: wrap; }
.metric-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 10px; }
.metric-tile { display: flex; align-items: center; gap: 10px; min-width: 0; padding: 12px; border: 1px solid var(--lp-border); border-radius: 8px; background: color-mix(in srgb, var(--lp-surface-container), transparent 28%); }
.detail-sections { display: grid; gap: 18px; min-height: 0; overflow: auto; }
.property-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
.property-grid > div { display: flex; align-items: center; justify-content: space-between; gap: 10px; min-width: 0; padding: 10px 12px; border: 1px solid var(--lp-border); border-radius: 8px; }
.property-grid span { color: var(--lp-text-muted); font-size: 0.78rem; }
.property-grid strong { min-width: 0; overflow-wrap: anywhere; text-align: right; font-size: 0.86rem; }
.ufw-actions { display: flex; align-items: center; justify-content: flex-end; gap: 6px; flex-wrap: wrap; min-width: 0; }
.network-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
.network-card { min-width: 0; padding: 12px; border: 1px solid var(--lp-border); border-radius: 8px; background: color-mix(in srgb, var(--lp-surface-container), transparent 28%); }
.network-card-title { display: flex; align-items: center; gap: 8px; margin-bottom: 9px; }
.network-addresses { display: grid; gap: 7px; }
.network-address { display: flex; align-items: center; gap: 8px; min-width: 0; }
.network-address code { min-width: 0; overflow-wrap: anywhere; font-size: 0.78rem; color: var(--lp-text-muted); }
.trait-list { display: flex; flex-wrap: wrap; gap: 6px; }
.notes { margin: 0; color: var(--lp-text-muted); white-space: pre-wrap; }
.form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.span-all { grid-column: 1 / -1; }
.server-task-alert { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.credential-required-alert :deep(.v-alert__content) { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.credential-table-card { overflow: hidden; }
.font-tabular { font-variant-numeric: tabular-nums; }
.min-width-0 { min-width: 0; }
@media (min-width: 761px) { .detail-column { min-height: 0; overflow: auto; } }
@media (max-width: 1080px) { .servers-workspace { grid-template-columns: 1fr; } }
@media (max-width: 760px) {
  .summary-strip, .metric-grid, .property-grid, .network-grid, .form-grid { grid-template-columns: 1fr; max-width: none; }
  .detail-header, .server-task-alert { flex-direction: column; align-items: stretch; }
  .servers-workspace, .detail-card { flex: none; min-height: auto; }
  .server-list, .detail-card { overflow: visible; }
  .server-list-body, .detail-sections { overflow: visible; }
  .detail-column { display: block; }
}
</style>
