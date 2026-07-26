<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { AlertTriangle, Cable, PlayCircle, Plus, RefreshCcw, Search, ServerCog, ShieldPlus, Trash2, Wrench } from '@lucide/vue';
import { credentialsApi } from '@/api/credentials';
import { serversApi, type ServerMetricsSeries } from '@/api/servers';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import Dialog from '@/components/ui/Dialog.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import Input from '@/components/ui/Input.vue';
import Select from '@/components/ui/Select.vue';
import Skeleton from '@/components/ui/Skeleton.vue';
import Textarea from '@/components/ui/Textarea.vue';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import MasterDetailLayout from '@/components/templates/MasterDetailLayout.vue';
import { useI18n } from '@/i18n';
import type { CredentialDto } from '@/types/credentials';
import type { ServerDto, ServerProbeResult, ServerSaveInput } from '@/types/servers';
import { agentTone, canInstallUfw, canRunPrivilegedOperation, credentialLabel, serverReachabilityTone, validateServerInput } from './model';

const { t } = useI18n();
const route = useRoute();
const router = useRouter();

const servers = ref<ServerDto[]>([]);
const credentials = ref<CredentialDto[]>([]);
const selectedId = ref('');
const search = ref(String(route.query.search ?? ''));
const loading = ref(false);
const error = ref('');
const credentialError = ref('');
const feedback = ref('');
const actionError = ref('');
const serverDialog = ref(false);
const confirmDialog = ref(false);
const saving = ref(false);
const probing = ref(false);
const probeResult = ref<ServerProbeResult | null>(null);
const editing = ref<ServerDto | null>(null);
const confirmTarget = ref<ServerDto | null>(null);
const pendingOperation = ref('');
const metrics = ref<ServerMetricsSeries | null>(null);
const metricsLoading = ref(false);
const metricsError = ref('');

const form = reactive({
  name: '',
  host: '',
  port: '22',
  sshUsername: '',
  credentialId: '',
  dockerHost: 'unix:///var/run/docker.sock',
  traits: '',
  variables: '',
  notes: '',
});

const selectedServer = computed(() => servers.value.find((item) => item.id === selectedId.value) ?? null);
const filteredServers = computed(() => {
  const term = search.value.trim().toLowerCase();
  if (!term) return servers.value;
  return servers.value.filter((server) => [server.name, server.host, server.os?.prettyName, server.traits?.['agent.status']].some((value) => String(value ?? '').toLowerCase().includes(term)));
});

const credentialOptions = computed(() => credentials.value.map((item) => ({ label: `${item.name} / ${item.username}`, value: item.id })));
const formPayload = computed<ServerSaveInput>(() => ({
  name: form.name,
  host: form.host,
  port: Number(form.port),
  sshUsername: form.sshUsername,
  credentialId: form.credentialId,
  dockerHost: form.dockerHost,
  traits: parsePairs(form.traits),
  variables: parsePairs(form.variables),
  notes: form.notes,
}));
const validation = computed(() => validateServerInput(formPayload.value));
const latestMetrics = computed(() => {
  const series = metrics.value;
  return {
    cpu: series?.cpu.at(-1),
    memory: series?.memory.at(-1),
    disk: series?.disk.at(-1),
    network: series?.network.at(-1),
    load: series?.load.at(-1),
  };
});

watch(search, (value) => {
  void router.replace({ query: { ...route.query, search: value || undefined } });
});
watch(selectedId, () => {
  void loadMetrics();
});

async function load() {
  loading.value = true;
  error.value = '';
  credentialError.value = '';
  try {
    const [serversResult, credentialsResult] = await Promise.allSettled([serversApi.list(), credentialsApi.list()]);
    if (serversResult.status === 'fulfilled') {
      const nextServers = serversResult.value;
      servers.value = nextServers;
      const queryServer = String(route.query.server ?? '');
      selectedId.value = nextServers.some((item) => item.id === queryServer)
        ? queryServer
        : nextServers.some((item) => item.id === selectedId.value)
          ? selectedId.value
          : nextServers[0]?.id || '';
    } else {
      error.value = serversResult.reason instanceof Error ? serversResult.reason.message : t('serversPage.loadFailed');
    }
    if (credentialsResult.status === 'fulfilled') {
      credentials.value = credentialsResult.value;
    } else {
      credentials.value = [];
      credentialError.value = credentialsResult.reason instanceof Error ? credentialsResult.reason.message : t('serversPage.credentialsLoadFailed');
    }
  } finally {
    loading.value = false;
  }
}

async function loadMetrics() {
  metrics.value = null;
  metricsError.value = '';
  if (!selectedId.value) return;
  metricsLoading.value = true;
  try {
    metrics.value = await serversApi.metrics(selectedId.value, '1h');
  } catch (err) {
    metricsError.value = err instanceof Error ? err.message : t('serversPage.metricsFailed');
  } finally {
    metricsLoading.value = false;
  }
}

function openCreate() {
  editing.value = null;
  probeResult.value = null;
  Object.assign(form, {
    name: '',
    host: '',
    port: '22',
    sshUsername: '',
    credentialId: credentials.value[0]?.id ?? '',
    dockerHost: 'unix:///var/run/docker.sock',
    traits: '',
    variables: '',
    notes: '',
  });
  serverDialog.value = true;
}

function openEdit(server: ServerDto) {
  editing.value = server;
  probeResult.value = null;
  Object.assign(form, {
    name: server.name,
    host: server.host,
    port: String(server.port || 22),
    sshUsername: server.sshUsername ?? '',
    credentialId: server.credentialId,
    dockerHost: server.dockerHost || 'unix:///var/run/docker.sock',
    traits: stringifyPairs(server.traits),
    variables: stringifyPairs(server.variables),
    notes: server.notes ?? '',
  });
  serverDialog.value = true;
}

async function probe() {
  probing.value = true;
  probeResult.value = null;
  actionError.value = '';
  try {
    probeResult.value = await serversApi.probe(formPayload.value);
  } catch (err) {
    actionError.value = err instanceof Error ? err.message : t('serversPage.probeFailed');
  } finally {
    probing.value = false;
  }
}

async function saveServer() {
  if (Object.keys(validation.value).length) return;
  saving.value = true;
  actionError.value = '';
  try {
    const saved = editing.value ? await serversApi.update(editing.value.id, formPayload.value) : await serversApi.create(formPayload.value);
    selectedId.value = saved.id;
    feedback.value = saved.initialTaskId ? t('serversPage.createdWithTask', { taskId: saved.initialTaskId }) : t(editing.value ? 'serversPage.updated' : 'serversPage.created');
    serverDialog.value = false;
    await load();
  } catch (err) {
    actionError.value = err instanceof Error ? err.message : t('serversPage.saveFailed');
  } finally {
    saving.value = false;
  }
}

async function testConnection(server: ServerDto) {
  await runInline(async () => {
    const tested = await serversApi.test(server.id);
    feedback.value = t('serversPage.testSucceeded', { name: tested.name });
    await load();
  });
}

function confirmDelete(server: ServerDto) {
  confirmTarget.value = server;
  confirmDialog.value = true;
}

async function deleteSelected() {
  const target = confirmTarget.value;
  if (!target) return;
  await runInline(async () => {
    await serversApi.delete(target.id);
    feedback.value = t('serversPage.deleted', { name: target.name });
    confirmDialog.value = false;
    selectedId.value = '';
    await load();
  });
}

async function deployAgent(server: ServerDto) {
  await runInline(async () => {
    const accepted = await serversApi.deployAgent(server.id);
    feedback.value = t('serversPage.agentTaskAccepted', { taskId: accepted.taskId });
  }, 'agent');
}

async function restartServer(server: ServerDto) {
  if (!canRunPrivilegedOperation(server)) return;
  await runInline(async () => {
    const accepted = await serversApi.restart(server.id);
    feedback.value = t('serversPage.restartAccepted', { taskId: accepted.taskId });
  }, 'restart');
}

async function installUfw(server: ServerDto) {
  if (!canInstallUfw(server)) return;
  await runInline(async () => {
    const accepted = await serversApi.installUfw(server.id);
    feedback.value = t('serversPage.ufwAccepted', { taskId: accepted.taskId });
  }, 'ufw');
}

async function runInline(action: () => Promise<void>, operation = 'default') {
  pendingOperation.value = operation;
  actionError.value = '';
  try {
    await action();
  } catch (err) {
    actionError.value = err instanceof Error ? err.message : t('common.operationFailed');
  } finally {
    pendingOperation.value = '';
  }
}

function statusText(server: ServerDto) {
  return server.reachable ? t('serversPage.reachable') : t('serversPage.unreachable');
}

function agentText(server: ServerDto) {
  const status = server.traits?.['agent.status'];
  if (status === 'compatible') return t('serversPage.agentCompatible');
  if (status === 'unavailable') return t('serversPage.agentUnavailable');
  if (status === 'undeployable') return t('serversPage.agentUndeployable');
  return server.traits?.['agent.enabled'] === 'true' ? t('serversPage.agentIncompatible') : t('serversPage.agentNotInstalled');
}

function privilegeText(server: ServerDto) {
  if (server.privilege?.privileged) return t('serversPage.privileged');
  if (server.sudo?.passwordless) return t('serversPage.passwordlessSudo');
  return t('serversPage.noPrivilege');
}

function parsePairs(raw: string) {
  return Object.fromEntries(raw.split('\n').map((line) => line.trim()).filter(Boolean).map((line) => {
    const [key, ...rest] = line.split('=');
    return [key.trim(), rest.join('=').trim()];
  }).filter(([key]) => key));
}

function stringifyPairs(value?: Record<string, string>) {
  return Object.entries(value ?? {}).map(([key, val]) => `${key}=${val}`).join('\n');
}

function percent(value?: number) {
  return typeof value === 'number' ? `${value.toFixed(1)}%` : t('common.notAvailable');
}

function bytes(value?: number) {
  if (typeof value !== 'number') return t('common.notAvailable');
  if (value >= 1024 ** 3) return `${(value / 1024 ** 3).toFixed(1)} GiB`;
  if (value >= 1024 ** 2) return `${(value / 1024 ** 2).toFixed(1)} MiB`;
  return `${value} B`;
}

function bytesPerSecond(value?: number) {
  return typeof value === 'number' ? `${bytes(value)}/s` : t('common.notAvailable');
}

onMounted(load);
</script>

<template>
  <ConsolePage :title="t('routes.servers.title')" :description="t('routes.servers.description')">
    <template #actions>
      <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
      <Button size="sm" variant="primary" @click="openCreate"><Plus />{{ t('serversPage.addServer') }}</Button>
    </template>

    <MasterDetailLayout class="h-full min-h-[640px]">
      <template #master>
      <aside class="grid min-h-0 min-w-0 grid-rows-[auto_minmax(0,1fr)] rounded-2xl border border-border bg-card">
        <div class="border-b border-border p-4">
          <label class="relative block">
            <Search class="pointer-events-none absolute left-3 top-2.5 size-4 text-muted-foreground" />
            <Input v-model="search" class="pl-9" :placeholder="t('serversPage.searchPlaceholder')" />
          </label>
        </div>
        <div class="min-h-0 overflow-auto p-2">
          <div v-if="loading && !servers.length" class="grid gap-2">
            <Skeleton v-for="item in 6" :key="item" class="h-20" />
          </div>
          <EmptyState v-else-if="!filteredServers.length" :title="t('serversPage.noServers')" :description="t('serversPage.noServersHint')" />
          <button
            v-for="server in filteredServers"
            v-else
            :key="server.id"
            type="button"
            class="motion-list-item mb-2 grid w-full gap-2 rounded-xl border p-3 text-left hover:bg-accent"
            :class="selectedId === server.id ? 'border-border-strong bg-background' : 'border-transparent bg-transparent'"
            :aria-current="selectedId === server.id ? 'true' : undefined"
            @click="selectedId = server.id; router.replace({ query: { ...route.query, server: server.id } })"
          >
            <div class="flex items-center justify-between gap-2">
              <strong class="truncate text-sm text-foreground">{{ server.name }}</strong>
              <Badge :tone="serverReachabilityTone(server)">{{ statusText(server) }}</Badge>
            </div>
            <span class="truncate text-xs text-muted-foreground">{{ server.host }}:{{ server.port }}</span>
            <div class="flex flex-wrap gap-1.5">
              <Badge :tone="agentTone(server)">{{ agentText(server) }}</Badge>
              <Badge :tone="canRunPrivilegedOperation(server) ? 'success' : 'warning'">{{ privilegeText(server) }}</Badge>
            </div>
          </button>
        </div>
      </aside>
      </template>

      <template #detail>
      <main class="grid min-h-0 min-w-0">
        <section v-if="error" class="rounded-2xl border border-danger-border bg-danger-bg p-4 text-sm text-danger">{{ error }}</section>
        <article v-else-if="loading && !servers.length" class="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-2xl border border-border bg-card">
          <header class="border-b border-border p-5">
            <Skeleton class="h-7 w-48" />
            <Skeleton class="mt-3 h-4 w-72 max-w-full" />
          </header>
          <div class="min-h-0 overflow-auto p-5">
            <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
              <div class="grid gap-4">
                <section v-for="item in 3" :key="item" class="rounded-2xl border border-border bg-background p-4">
                  <Skeleton class="h-4 w-32" />
                  <div class="mt-4 grid grid-cols-2 gap-3 max-md:grid-cols-1">
                    <Skeleton v-for="line in 4" :key="line" class="h-10" />
                  </div>
                </section>
              </div>
              <aside class="grid content-start gap-3">
                <Skeleton v-for="item in 2" :key="item" class="h-36" />
              </aside>
            </div>
          </div>
        </article>
        <EmptyState v-else-if="!selectedServer" :title="t('serversPage.selectServer')" :description="t('serversPage.selectServerHint')" />
        <article v-else class="grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)] overflow-hidden rounded-2xl border border-border bg-card">
          <header class="flex items-start justify-between gap-4 border-b border-border p-5 max-md:grid">
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-2">
                <h2 class="m-0 truncate text-xl font-semibold text-foreground">{{ selectedServer.name }}</h2>
                <Badge :tone="serverReachabilityTone(selectedServer)">{{ statusText(selectedServer) }}</Badge>
                <Badge :tone="agentTone(selectedServer)">{{ agentText(selectedServer) }}</Badge>
              </div>
              <p class="m-0 mt-1 text-sm text-muted-foreground">{{ selectedServer.host }}:{{ selectedServer.port }} / {{ selectedServer.os?.prettyName || t('common.notAvailable') }}</p>
            </div>
            <div class="flex flex-wrap justify-end gap-2">
              <Button size="sm" @click="testConnection(selectedServer)"><Cable />{{ t('serversPage.testConnection') }}</Button>
              <Button size="sm" @click="openEdit(selectedServer)"><Wrench />{{ t('common.edit') }}</Button>
              <Button size="sm" variant="danger" @click="confirmDelete(selectedServer)"><Trash2 />{{ t('common.delete') }}</Button>
            </div>
          </header>

          <div v-if="feedback || actionError || credentialError || selectedServer.lastError" class="grid gap-2 border-b border-border p-4">
            <div v-if="feedback" class="rounded-xl border border-success-border bg-success-bg p-3 text-sm text-success">{{ feedback }}</div>
            <div v-if="actionError" class="rounded-xl border border-danger-border bg-danger-bg p-3 text-sm text-danger">{{ actionError }}</div>
            <div v-if="credentialError" class="rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning">{{ t('serversPage.credentialsLoadFailed') }} {{ credentialError }}</div>
            <div v-if="selectedServer.lastError" class="rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning">{{ selectedServer.lastError }}</div>
          </div>

          <div class="min-h-0 overflow-auto p-5">
            <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
              <div class="grid gap-4">
                <section class="rounded-2xl border border-border bg-background p-4">
                  <h3 class="m-0 text-sm font-semibold text-foreground">{{ t('serversPage.connection') }}</h3>
                  <dl class="mt-3 grid grid-cols-2 gap-3 text-sm max-md:grid-cols-1">
                    <div><dt>{{ t('serversPage.host') }}</dt><dd>{{ selectedServer.host }}</dd></div>
                    <div><dt>{{ t('serversPage.port') }}</dt><dd>{{ selectedServer.port }}</dd></div>
                    <div><dt>{{ t('serversPage.credential') }}</dt><dd>{{ credentialLabel(selectedServer.credentialId, credentials) || t('common.notAvailable') }}</dd></div>
                    <div><dt>{{ t('serversPage.dockerHost') }}</dt><dd>{{ selectedServer.dockerHost || t('common.notAvailable') }}</dd></div>
                  </dl>
                </section>
                <section class="rounded-2xl border border-border bg-background p-4">
                  <div class="flex items-center justify-between gap-3">
                    <h3 class="m-0 text-sm font-semibold text-foreground">{{ t('serversPage.metrics') }}</h3>
                    <Button size="sm" variant="ghost" :loading="metricsLoading" @click="loadMetrics"><RefreshCcw />{{ t('common.refresh') }}</Button>
                  </div>
                  <div v-if="metricsError" class="mt-3 rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning">{{ metricsError }}</div>
                  <dl class="mt-3 grid grid-cols-3 gap-3 text-sm max-md:grid-cols-1">
                    <div><dt>CPU</dt><dd>{{ percent(latestMetrics.cpu?.usagePercent) }}</dd></div>
                    <div><dt>{{ t('serversPage.memory') }}</dt><dd>{{ bytes(latestMetrics.memory?.usedBytes) }} / {{ bytes(latestMetrics.memory?.totalBytes) }}</dd></div>
                    <div><dt>{{ t('serversPage.disk') }}</dt><dd>{{ bytes(latestMetrics.disk?.usedBytes) }} / {{ bytes(latestMetrics.disk?.totalBytes) }}</dd></div>
                    <div><dt>{{ t('serversPage.networkRx') }}</dt><dd>{{ bytesPerSecond(latestMetrics.network?.rxBytesPerSecond) }}</dd></div>
                    <div><dt>{{ t('serversPage.networkTx') }}</dt><dd>{{ bytesPerSecond(latestMetrics.network?.txBytesPerSecond) }}</dd></div>
                    <div><dt>{{ t('serversPage.loadAverage') }}</dt><dd>{{ latestMetrics.load ? `${latestMetrics.load.load1.toFixed(2)} ${latestMetrics.load.load5.toFixed(2)} ${latestMetrics.load.load15.toFixed(2)}` : selectedServer.loadAverage || t('common.notAvailable') }}</dd></div>
                  </dl>
                </section>
                <section class="rounded-2xl border border-border bg-background p-4">
                  <h3 class="m-0 text-sm font-semibold text-foreground">{{ t('serversPage.recentOperations') }}</h3>
                  <div class="mt-3 grid gap-2 text-sm text-muted-foreground">
                    <span>{{ t('serversPage.lastChecked') }}: {{ selectedServer.lastCheckedAt || t('common.never') }}</span>
                    <span>{{ t('serversPage.updatedAt') }}: {{ selectedServer.updatedAt || t('common.never') }}</span>
                    <span v-if="selectedServer.initialTaskId">{{ t('serversPage.initialTask') }}: {{ selectedServer.initialTaskId }}</span>
                  </div>
                </section>
              </div>
              <aside class="grid content-start gap-3">
                <section class="rounded-2xl border border-border bg-background p-4">
                  <h3 class="m-0 text-sm font-semibold text-foreground">{{ t('serversPage.agent') }}</h3>
                  <p class="mt-2 text-sm text-muted-foreground">{{ agentText(selectedServer) }}</p>
                  <div class="mt-3 grid gap-2">
                    <Button :loading="pendingOperation === 'agent'" @click="deployAgent(selectedServer)"><ServerCog />{{ t('serversPage.deployAgent') }}</Button>
                    <Button :disabled="!canRunPrivilegedOperation(selectedServer)" :loading="pendingOperation === 'restart'" @click="restartServer(selectedServer)"><PlayCircle />{{ t('serversPage.restart') }}</Button>
                  </div>
                </section>
                <section class="rounded-2xl border border-border bg-background p-4">
                  <h3 class="m-0 text-sm font-semibold text-foreground">{{ t('serversPage.privilegeAndSecurity') }}</h3>
                  <p class="mt-2 text-sm text-muted-foreground">{{ privilegeText(selectedServer) }}</p>
                  <Button class="mt-3 w-full" :disabled="!canInstallUfw(selectedServer)" :loading="pendingOperation === 'ufw'" @click="installUfw(selectedServer)">
                    <ShieldPlus />{{ t('serversPage.installUfw') }}
                  </Button>
                </section>
              </aside>
            </div>
          </div>
        </article>
      </main>
      </template>
    </MasterDetailLayout>

    <Dialog v-model:open="serverDialog" :title="editing ? t('serversPage.editServer') : t('serversPage.createServer')" :description="t('serversPage.formDescription')" :close-label="t('common.close')">
      <div class="grid gap-4">
        <div v-if="actionError" class="rounded-xl border border-danger-border bg-danger-bg p-3 text-sm text-danger">{{ actionError }}</div>
        <div class="grid grid-cols-2 gap-3 max-sm:grid-cols-1">
          <label class="grid gap-1 text-sm">{{ t('serversPage.name') }}<Input v-model="form.name" :invalid="Boolean(validation.name)" /></label>
          <label class="grid gap-1 text-sm">{{ t('serversPage.host') }}<Input v-model="form.host" :invalid="Boolean(validation.host)" /></label>
          <label class="grid gap-1 text-sm">{{ t('serversPage.port') }}<Input v-model="form.port" type="number" :invalid="Boolean(validation.port)" /></label>
          <label class="grid gap-1 text-sm">{{ t('serversPage.credential') }}<Select v-model="form.credentialId" :options="credentialOptions" :placeholder="t('serversPage.selectCredential')" /></label>
          <label class="col-span-2 grid gap-1 text-sm max-sm:col-span-1">{{ t('serversPage.sshUsername') }}<Input v-model="form.sshUsername" :placeholder="t('serversPage.sshUsernameHint')" /></label>
          <label class="col-span-2 grid gap-1 text-sm max-sm:col-span-1">{{ t('serversPage.dockerHost') }}<Input v-model="form.dockerHost" :invalid="Boolean(validation.dockerHost)" /></label>
          <label class="col-span-2 grid gap-1 text-sm max-sm:col-span-1">{{ t('serversPage.traits') }}<Textarea v-model="form.traits" :placeholder="t('serversPage.pairsHint')" /></label>
          <label class="col-span-2 grid gap-1 text-sm max-sm:col-span-1">{{ t('serversPage.notes') }}<Textarea v-model="form.notes" /></label>
        </div>
        <div v-if="Object.values(validation).length" class="rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning">
          {{ t(Object.values(validation)[0] || 'serversPage.validationGeneric') }}
        </div>
        <div v-if="probeResult" class="rounded-xl border border-info-border bg-info-bg p-3 text-sm text-info">
          {{ probeResult.reachable ? t('serversPage.probeReachable') : t('serversPage.probeUnreachable') }}
          <span v-if="probeResult.error"> {{ probeResult.error }}</span>
        </div>
      </div>
      <template #footer>
        <Button variant="secondary" :loading="probing" @click="probe"><Cable />{{ t('serversPage.probe') }}</Button>
        <Button variant="secondary" @click="serverDialog = false">{{ t('common.cancel') }}</Button>
        <Button variant="primary" :loading="saving" :disabled="Boolean(Object.keys(validation).length)" @click="saveServer">{{ editing ? t('common.save') : t('common.create') }}</Button>
      </template>
    </Dialog>

    <Dialog v-model:open="confirmDialog" :title="t('serversPage.deleteServer')" :description="confirmTarget ? t('serversPage.deleteServerDescription', { name: confirmTarget.name }) : ''" :close-label="t('common.close')">
      <div class="flex gap-3 rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning">
        <AlertTriangle class="size-4 shrink-0" />
        <span>{{ t('serversPage.deleteServerImpact') }}</span>
      </div>
      <template #footer>
        <Button variant="secondary" @click="confirmDialog = false">{{ t('common.cancel') }}</Button>
        <Button variant="danger" :loading="pendingOperation === 'default'" @click="deleteSelected">{{ t('common.delete') }}</Button>
      </template>
    </Dialog>
  </ConsolePage>
</template>

<style scoped>
dt {
  margin: 0 0 4px;
  color: hsl(var(--muted-foreground));
  font-size: 12px;
}

dd {
  margin: 0;
  overflow-wrap: anywhere;
  color: hsl(var(--foreground));
  font-weight: 600;
}
</style>
