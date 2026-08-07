<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Box, Boxes, Container, Database, DownloadCloud, FileText, Package, Play, RefreshCcw, Router, Search, Square, Trash2 } from '@lucide/vue';
import { containersApi } from '@/api/containers';
import { waitForTask } from '@/api/taskWait';
import { packagesApi } from '@/api/packages';
import { serversApi } from '@/api/servers';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import Dialog from '@/components/ui/Dialog.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import Input from '@/components/ui/Input.vue';
import Textarea from '@/components/ui/Textarea.vue';
import ServerContextSelector from '@/components/patterns/ServerContextSelector.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import MasterDetailLayout from '@/components/templates/MasterDetailLayout.vue';
import { useI18n } from '@/i18n';
import type { ServerDto } from '@/types/servers';
import type { ContainerDto, ImageDto, ImageList, NetworkDto, PackageUpdateList, VolumeDto } from '@/types/resources';
import {
  canMaintainPackages,
  canUseDockerResources,
  containerActionDisabled,
  containerTone,
  dockerBlockReason,
  filterPackages,
  imageLabel,
  imageTone,
  packageBlockReason,
  resourceTabFromPath,
  selectedPackageNames,
  volumeTone,
  type ResourceTab,
} from './model';

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const notifyError = useErrorToast();
const notifySuccess = useSuccessToast();

const resourceTabs: ResourceTab[] = ['packages', 'containers', 'images', 'networks', 'volumes'];
let serversController: AbortController | null = null;
let resourceController: AbortController | null = null;
let serversRequestId = 0;
let resourceRequestId = 0;
const autoRefreshing = new Set<string>();

const servers = ref<ServerDto[]>([]);
const selectedId = ref(String(route.query.server ?? ''));
const activeTab = computed<ResourceTab>(() => resourceTabFromPath(route.path));
const search = ref(String(route.query.search ?? ''));
const loadingServers = ref(false);
const loadingResource = ref(false);
const error = ref('');
const actionError = ref('');
const pending = ref('');

const packageList = ref<PackageUpdateList | null>(null);
const packageSelection = reactive<Record<string, boolean>>({});
const containers = ref<ContainerDto[]>([]);
const images = ref<ImageList | null>(null);
const networks = ref<NetworkDto[]>([]);
const volumes = ref<VolumeDto[]>([]);
const logsDialog = ref(false);
const logsText = ref('');
const logsTitle = ref('');
const pullDialog = ref(false);
const confirmDialog = ref(false);
const confirmTarget = ref<{ kind: 'container' | 'image' | 'image-prune' | 'volume' | 'volume-prune' | 'network'; id?: string; label?: string } | null>(null);
const pullForm = reactive({ reference: 'nginx:1.28-alpine' });

const selectedServer = computed(() => servers.value.find((item) => item.id === selectedId.value) ?? null);
const serverContextOptions = computed(() => servers.value.map((server) => {
  const packagesReady = canMaintainPackages(server);
  const dockerReady = canUseDockerResources(server);
  return {
    id: server.id,
    name: server.name,
    description: server.host,
    status: packagesReady || dockerReady ? 'online' : server.reachable ? 'warning' : 'offline',
    statusLabel: dockerReady ? 'Docker' : packagesReady ? 'APT' : t(server.reachable ? 'resourcesPage.limited' : 'resourcesPage.blockUnreachable'),
    statusTone: dockerReady || packagesReady ? 'success' as const : server.reachable ? 'warning' as const : 'danger' as const,
    capabilities: [
      packagesReady ? 'APT' : '',
      dockerReady ? 'Docker' : '',
    ].filter(Boolean),
    disabledReason: server.reachable ? '' : t('resourcesPage.blockUnreachable'),
  };
}));
const resourceTitleKey = computed(() => `routes.${activeTab.value}.title`);
const resourceDescriptionKey = computed(() => `routes.${activeTab.value}.description`);
const packageRows = computed(() => filterPackages(packageList.value?.updates ?? [], search.value));
const selectedPackages = computed(() => selectedPackageNames(packageSelection));
const imageUpgradeIds = computed(() => Array.from(new Set((images.value?.items ?? []).filter((item) => item.updateAvailable && item.upgradeable).flatMap((item) => item.applicationIds))));

watch(() => route.path, () => {
  clearAllResources();
  void loadResource();
});

watch(selectedId, (value) => {
  void router.replace({ query: { ...route.query, server: value || undefined } });
  clearAllResources();
  void loadResource();
});

watch(search, (value) => {
  void router.replace({ query: { ...route.query, search: value || undefined } });
});

async function loadServers() {
  serversController?.abort();
  const requestId = ++serversRequestId;
  const controller = new AbortController();
  serversController = controller;
  loadingServers.value = true;
  error.value = '';
  try {
    const nextServers = await serversApi.list({ signal: controller.signal });
    if (requestId !== serversRequestId) return;
    servers.value = nextServers;
    if (!servers.value.some((item) => item.id === selectedId.value)) {
      selectedId.value = servers.value[0]?.id ?? '';
      if (!selectedId.value) {
        clearAllResources();
        resourceController?.abort();
        resourceRequestId += 1;
        loadingResource.value = false;
      }
    }
  } catch (err) {
    if (isAbortError(err)) return;
    error.value = err instanceof Error ? err.message : t('resourcesPage.loadServersFailed');
    notifyError(err instanceof Error ? err.message : t('resourcesPage.loadServersFailed'));
  } finally {
    if (requestId === serversRequestId) loadingServers.value = false;
  }
}

async function loadResource() {
  resourceController?.abort();
  const requestId = ++resourceRequestId;
  const server = selectedServer.value;
  if (!server) {
    loadingResource.value = false;
    clearAllResources();
    return;
  }
  const controller = new AbortController();
  resourceController = controller;
  const tab = activeTab.value;
  let snapshotNeedsRefresh: { serverId: string; tab: 'networks' | 'volumes' } | null = null;
  loadingResource.value = true;
  actionError.value = '';
  try {
    if (tab === 'packages') {
      const updates = await packagesApi.updates(server.id, { signal: controller.signal });
      if (requestId !== resourceRequestId || tab !== activeTab.value) return;
      packageList.value = updates;
      Object.keys(packageSelection).forEach((key) => delete packageSelection[key]);
    }
    if (tab === 'containers') {
      const result = await containersApi.containers(server.id, { signal: controller.signal });
      if (requestId !== resourceRequestId || tab !== activeTab.value) return;
      containers.value = result.items;
    }
    if (tab === 'images') {
      const result = await containersApi.images(server.id, { signal: controller.signal });
      if (requestId !== resourceRequestId || tab !== activeTab.value) return;
      images.value = result;
    }
    if (tab === 'networks') {
      const result = await containersApi.networks(server.id, { signal: controller.signal });
      if (requestId !== resourceRequestId || tab !== activeTab.value) return;
      networks.value = result.items;
      if (result.stale && !result.observedAt) snapshotNeedsRefresh = { serverId: server.id, tab: 'networks' };
    }
    if (tab === 'volumes') {
      const result = await containersApi.volumes(server.id, { signal: controller.signal });
      if (requestId !== resourceRequestId || tab !== activeTab.value) return;
      volumes.value = result.items;
      if (result.stale && !result.observedAt) snapshotNeedsRefresh = { serverId: server.id, tab: 'volumes' };
    }
  } catch (err) {
    if (isAbortError(err)) return;
    actionError.value = err instanceof Error ? err.message : t('resourcesPage.loadResourceFailed');
    notifyError(err instanceof Error ? err.message : t('resourcesPage.loadResourceFailed'));
    clearResource(tab);
  } finally {
    if (requestId === resourceRequestId) loadingResource.value = false;
  }
  if (snapshotNeedsRefresh) {
    await ensureSnapshot(snapshotNeedsRefresh.serverId, snapshotNeedsRefresh.tab);
  }
}

function clearResource(tab: ResourceTab) {
  if (tab === 'packages') {
    packageList.value = null;
    Object.keys(packageSelection).forEach((key) => delete packageSelection[key]);
  }
  if (tab === 'containers') containers.value = [];
  if (tab === 'images') images.value = null;
  if (tab === 'networks') networks.value = [];
  if (tab === 'volumes') volumes.value = [];
}

function clearAllResources() {
  resourceTabs.forEach((tab) => clearResource(tab));
}

async function refreshCurrent() {
  const server = selectedServer.value;
  if (!server) return;
  await run('refresh', async () => {
    if (activeTab.value === 'packages') {
      const result = await packagesApi.refresh(server.id);
      notifySuccess(result.taskId ? t('resourcesPage.taskAccepted', { taskId: result.taskId }) : t('resourcesPage.refreshing'));
      if (result.taskId) await waitForTask(result.taskId);
    } else if (activeTab.value === 'images') {
      const accepted = await containersApi.refreshImages(server.id);
      notifySuccess(t('resourcesPage.taskAccepted', { taskId: accepted.taskId }));
      await waitForTask(accepted.taskId);
    } else if (activeTab.value === 'networks') {
      const accepted = await containersApi.refreshNetworks(server.id);
      notifySuccess(t('resourcesPage.taskAccepted', { taskId: accepted.taskId }));
      await waitForTask(accepted.taskId);
    } else if (activeTab.value === 'volumes') {
      const accepted = await containersApi.refreshVolumes(server.id);
      notifySuccess(t('resourcesPage.taskAccepted', { taskId: accepted.taskId }));
      await waitForTask(accepted.taskId);
    } else {
      await loadResource();
      notifySuccess(t('resourcesPage.refreshed'));
    }
    await loadResource();
  });
}

async function ensureSnapshot(serverId: string, tab: 'networks' | 'volumes') {
  const key = `${serverId}:${tab}`;
  if (autoRefreshing.has(key)) return;
  autoRefreshing.add(key);
  loadingResource.value = true;
  try {
    const accepted = tab === 'networks' ? await containersApi.refreshNetworks(serverId) : await containersApi.refreshVolumes(serverId);
    await waitForTask(accepted.taskId);
    await loadResource();
  } catch (err) {
    if (!isAbortError(err)) {
      const message = err instanceof Error ? err.message : t('resourcesPage.loadResourceFailed');
      actionError.value = message;
      notifyError(message);
    }
  } finally {
    autoRefreshing.delete(key);
    loadingResource.value = false;
  }
}

async function upgradeSelectedPackages() {
  if (!selectedServer.value || !selectedPackages.value.length) return;
  await run('upgrade-selected', async () => {
    const accepted = await packagesApi.upgradeSelected(selectedServer.value!.id, selectedPackages.value);
    notifySuccess(t('resourcesPage.taskAccepted', { taskId: accepted.taskId }));
    await loadResource();
  });
}

async function upgradeAllPackages() {
  if (!selectedServer.value) return;
  await run('upgrade-all', async () => {
    const accepted = await packagesApi.upgradeAll(selectedServer.value!.id);
    notifySuccess(t('resourcesPage.taskAccepted', { taskId: accepted.taskId }));
    await loadResource();
  });
}

async function containerAction(container: ContainerDto, action: 'start' | 'stop' | 'restart') {
  if (!selectedServer.value || containerActionDisabled(container, action)) return;
  await run(`${action}-${container.id}`, async () => {
    await containersApi.containerAction(selectedServer.value!.id, container.id, action);
    notifySuccess(t('resourcesPage.operationCompleted'));
    await loadResource();
  });
}

async function openLogs(container: ContainerDto) {
  if (!selectedServer.value) return;
  await run(`logs-${container.id}`, async () => {
    const result = await containersApi.containerLogs(selectedServer.value!.id, container.id, 500);
    logsTitle.value = containerName(container);
    logsText.value = result.logs;
    logsDialog.value = true;
  });
}

function confirm(kind: NonNullable<typeof confirmTarget.value>['kind'], id?: string, label?: string) {
  confirmTarget.value = { kind, id, label };
  confirmDialog.value = true;
}

async function confirmDanger() {
  const server = selectedServer.value;
  const target = confirmTarget.value;
  if (!server || !target) return;
  await run(`confirm-${target.kind}`, async () => {
    if (target.kind === 'container' && target.id) await containersApi.deleteContainer(server.id, target.id);
    if (target.kind === 'image' && target.id) await containersApi.deleteImage(server.id, target.id);
    if (target.kind === 'image-prune') await containersApi.deleteUnusedImages(server.id);
    if (target.kind === 'volume' && target.id) await containersApi.deleteVolume(server.id, target.id);
    if (target.kind === 'volume-prune') await containersApi.deleteUnusedVolumes(server.id);
    notifySuccess(t('resourcesPage.operationCompleted'));
    confirmDialog.value = false;
    await loadResource();
  });
}

async function pullImage() {
  if (!selectedServer.value || !pullForm.reference.trim()) return;
  await run('pull-image', async () => {
    await containersApi.pullImage(selectedServer.value!.id, pullForm.reference);
    notifySuccess(t('resourcesPage.imagePulled'));
    pullDialog.value = false;
    await loadResource();
  });
}

async function upgradeImages(selected: boolean) {
  await run(selected ? 'upgrade-images-selected' : 'upgrade-images-all', async () => {
    const accepted = selected ? await containersApi.upgradeSelectedImages(imageUpgradeIds.value) : await containersApi.upgradeAllImages();
    notifySuccess(t('resourcesPage.taskAccepted', { taskId: accepted.taskId }));
  });
}

async function run(operation: string, action: () => Promise<void>) {
  pending.value = operation;
  actionError.value = '';
  try {
    await action();
  } catch (err) {
    actionError.value = err instanceof Error ? err.message : t('common.operationFailed');
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'));
  } finally {
    pending.value = '';
  }
}

function containerName(container: ContainerDto) {
  return (container.names[0] || container.id).replace(/^\//, '');
}

function formatBytes(value?: number) {
  if (!value) return '0 B';
  const units = ['B', 'KiB', 'MiB', 'GiB'];
  let amount = value;
  let index = 0;
  while (amount >= 1024 && index < units.length - 1) {
    amount /= 1024;
    index += 1;
  }
  return `${amount.toFixed(index ? 1 : 0)} ${units[index]}`;
}

function isAbortError(error: unknown) {
  return Boolean(error && typeof error === 'object' && 'code' in error && (error as { code?: string }).code === 'request_aborted');
}

onMounted(async () => {
  await loadServers();
  await loadResource();
});

onBeforeUnmount(() => {
  serversController?.abort();
  resourceController?.abort();
});
</script>

<template>
  <ConsolePage :title="t(resourceTitleKey)" :description="t(resourceDescriptionKey)">
    <template #actions>
      <Button size="sm" :loading="loadingServers || loadingResource || pending === 'refresh'" @click="refreshCurrent"><RefreshCcw />{{ t('common.refresh') }}</Button>
    </template>

    <MasterDetailLayout class="h-full min-h-[640px]">
      <template #master>
      <aside class="grid min-h-0 min-w-0 grid-rows-[auto_auto_minmax(0,1fr)] rounded-2xl border border-border bg-card">
        <div class="border-b border-border p-4">
          <h2 class="m-0 text-sm font-semibold text-foreground">{{ t('resourcesPage.serverContext') }}</h2>
          <p class="m-0 mt-1 text-xs text-muted-foreground">{{ t('resourcesPage.selectServerHint') }}</p>
        </div>
        <div class="grid grid-cols-2 gap-2 border-b border-border p-4 text-center text-xs">
          <div class="rounded-xl border border-border bg-background p-3">
            <strong class="block text-lg text-foreground">{{ servers.filter(canMaintainPackages).length }}</strong>
            <span class="text-muted-foreground">{{ t('resourcesPage.packageReady') }}</span>
          </div>
          <div class="rounded-xl border border-border bg-background p-3">
            <strong class="block text-lg text-foreground">{{ servers.filter(canUseDockerResources).length }}</strong>
            <span class="text-muted-foreground">{{ t('resourcesPage.dockerReady') }}</span>
          </div>
        </div>
        <div class="min-h-0 overflow-auto p-3">
          <ServerContextSelector
            v-model="selectedId"
            :servers="serverContextOptions"
            :label="t('resourcesPage.serverContext')"
            :loading="loadingServers"
            :disabled="loadingServers"
          />
        </div>
      </aside>
      </template>

      <template #detail>
      <main class="grid min-h-0 min-w-0">
        <EmptyState v-if="!selectedServer" :title="t('resourcesPage.selectServer')" :description="t('resourcesPage.selectServerHint')" />
        <article v-else class="grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)] overflow-hidden rounded-2xl border border-border bg-card">
          <header class="flex items-start justify-between gap-4 border-b border-border p-5 max-lg:grid">
            <div>
              <div class="flex flex-wrap items-center gap-2">
                <h2 class="m-0 text-xl font-semibold text-foreground">{{ selectedServer.name }}</h2>
                <Badge :tone="canMaintainPackages(selectedServer) ? 'success' : 'warning'">APT</Badge>
                <Badge :tone="canUseDockerResources(selectedServer) ? 'success' : 'warning'">Docker</Badge>
              </div>
              <p class="m-0 mt-1 text-sm text-muted-foreground">{{ selectedServer.host }} · {{ selectedServer.dockerHost || t('common.notAvailable') }}</p>
            </div>
            <div class="flex flex-wrap gap-2">
              <Button v-if="activeTab === 'packages' && selectedPackages.length" size="sm" variant="primary" :disabled="!canMaintainPackages(selectedServer)" :loading="pending === 'upgrade-selected'" @click="upgradeSelectedPackages"><Package />{{ t('resourcesPage.upgradeSelected') }}</Button>
              <Button v-if="activeTab === 'images'" size="sm" variant="primary" :disabled="!canUseDockerResources(selectedServer)" @click="pullDialog = true"><DownloadCloud />{{ t('resourcesPage.pullImage') }}</Button>
            </div>
          </header>

          <div v-if="(activeTab === 'packages' && packageBlockReason(selectedServer)) || (activeTab !== 'packages' && dockerBlockReason(selectedServer))" class="grid gap-2 border-b border-border p-4">
            <div v-if="activeTab === 'packages' && packageBlockReason(selectedServer)" class="rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning">{{ t(packageBlockReason(selectedServer)) }}</div>
            <div v-if="activeTab !== 'packages' && dockerBlockReason(selectedServer)" class="rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning">{{ t(dockerBlockReason(selectedServer)) }}</div>
          </div>

          <div class="min-h-0 p-5">
            <section v-if="activeTab === 'packages'" class="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)_auto] rounded-2xl border border-border bg-background">
              <div class="flex flex-wrap items-center justify-between gap-3 border-b border-border p-4">
                <label class="relative block w-full max-w-sm">
                  <Search class="pointer-events-none absolute left-3 top-2.5 size-4 text-muted-foreground" />
                  <Input v-model="search" class="pl-9" :placeholder="t('resourcesPage.searchPackages')" />
                </label>
                <div class="flex flex-wrap gap-2">
                  <Button size="sm" :disabled="!canMaintainPackages(selectedServer)" :loading="pending === 'refresh'" @click="refreshCurrent"><RefreshCcw />{{ t('resourcesPage.refreshMetadata') }}</Button>
                  <Button size="sm" :disabled="!canMaintainPackages(selectedServer) || !packageList?.updates.length" :loading="pending === 'upgrade-all'" @click="upgradeAllPackages"><Package />{{ t('resourcesPage.upgradeAll') }}</Button>
                </div>
              </div>
              <div class="min-h-0 overflow-auto p-3">
                <div v-if="loadingResource && !packageList" class="grid gap-2" aria-hidden="true">
                  <div v-for="item in 8" :key="item" class="grid grid-cols-[auto_minmax(0,1fr)_minmax(160px,auto)] items-center gap-3 rounded-xl border border-border p-3">
                    <div class="motion-skeleton size-4 rounded bg-muted animate-pulse" />
                    <div class="min-w-0">
                      <div class="motion-skeleton h-4 w-40 rounded bg-muted animate-pulse" />
                      <div class="motion-skeleton mt-2 h-3 w-56 max-w-full rounded bg-muted animate-pulse" />
                    </div>
                    <div class="motion-skeleton h-4 w-36 rounded bg-muted animate-pulse" />
                  </div>
                </div>
                <EmptyState v-else-if="error" :title="t('common.loadFailed')" :description="error">
                  <template #actions>
                    <Button size="sm" :loading="loadingResource" @click="loadResource"><RefreshCcw />{{ t('common.retry') }}</Button>
                  </template>
                </EmptyState>
                <EmptyState v-else-if="!packageRows.length" :title="t('resourcesPage.noPackages')" :description="t('resourcesPage.noPackagesHint')" />
                <label v-for="item in packageRows" v-else :key="item.name" class="mb-2 grid grid-cols-[auto_minmax(0,1fr)_minmax(160px,auto)] items-center gap-3 rounded-xl border border-border p-3">
                  <input v-model="packageSelection[item.name]" type="checkbox" :disabled="!canMaintainPackages(selectedServer)" />
                  <span class="min-w-0">
                    <strong class="block truncate text-sm text-foreground">{{ item.name }}</strong>
                    <span class="block truncate text-xs text-muted-foreground">{{ item.source }}</span>
                  </span>
                  <span class="text-right text-xs text-muted-foreground">{{ item.installedVersion }} -> <strong class="text-foreground">{{ item.candidateVersion }}</strong></span>
                </label>
              </div>
              <footer class="flex items-center justify-between border-t border-border p-3 text-sm text-muted-foreground">
                <span>{{ t('resourcesPage.lastRefresh') }}: {{ packageList?.lastRefreshedAt || t('common.never') }}</span>
                <span v-if="selectedPackages.length">{{ selectedPackages.length }} {{ t('resourcesPage.selectedCount') }}</span>
              </footer>
            </section>

            <section v-else-if="activeTab === 'containers'" class="grid h-full min-h-0 grid-cols-3 gap-3 overflow-y-auto overflow-x-hidden max-2xl:grid-cols-2 max-lg:grid-cols-1">
              <template v-if="loadingResource && !containers.length">
                <article v-for="item in 6" :key="item" class="grid min-h-[220px] grid-rows-[auto_minmax(0,1fr)_auto] rounded-2xl border border-border bg-background" aria-hidden="true">
                  <header class="border-b border-border p-4">
                    <div class="motion-skeleton h-4 w-36 rounded bg-muted animate-pulse" />
                    <div class="motion-skeleton mt-2 h-3 w-56 max-w-full rounded bg-muted animate-pulse" />
                  </header>
                  <div class="p-4">
                    <div class="motion-skeleton h-4 w-full rounded bg-muted animate-pulse" />
                    <div class="motion-skeleton mt-3 h-4 w-2/3 rounded bg-muted animate-pulse" />
                  </div>
                  <footer class="flex gap-2 border-t border-border p-3">
                    <div v-for="button in 3" :key="button" class="motion-skeleton h-8 w-20 rounded bg-muted animate-pulse" />
                  </footer>
                </article>
              </template>
              <EmptyState v-else-if="error" :title="t('common.loadFailed')" :description="error">
                <template #actions>
                  <Button size="sm" :loading="loadingResource" @click="loadResource"><RefreshCcw />{{ t('common.retry') }}</Button>
                </template>
              </EmptyState>
              <EmptyState v-else-if="!containers.length" :title="t('resourcesPage.noContainers')" :description="t('resourcesPage.noContainersHint')" />
              <article v-for="item in containers" v-else :key="item.id" class="grid grid-rows-[auto_auto_auto] rounded-2xl border border-border bg-background">
                <header class="border-b border-border p-4">
                  <div class="flex items-start justify-between gap-3">
                    <div class="min-w-0">
                      <h3 class="m-0 truncate text-sm font-semibold">{{ containerName(item) }}</h3>
                      <p class="m-0 mt-1 truncate text-xs text-muted-foreground">{{ item.image }}</p>
                    </div>
                    <Badge :tone="containerTone(item)">{{ item.managed ? t('resourcesPage.managed') : item.state }}</Badge>
                  </div>
                </header>
                <div class="p-4 text-sm text-muted-foreground">
                  <p class="m-0">{{ item.status }}</p>
                  <p class="m-0 mt-2 truncate">{{ item.ports.map((port) => `${port.publicPort || port.privatePort}:${port.privatePort}/${port.type}`).join(', ') || t('common.notAvailable') }}</p>
                  <p v-if="item.managed" class="mt-3 rounded-xl border border-info-border bg-info-bg p-2 text-xs text-info">{{ t('resourcesPage.managedContainerHint') }}</p>
                </div>
                <footer class="flex flex-wrap gap-2 border-t border-border p-3">
                  <Button size="sm" :disabled="Boolean(containerActionDisabled(item, 'start'))" :loading="pending === `start-${item.id}`" @click="containerAction(item, 'start')"><Play />{{ t('resourcesPage.start') }}</Button>
                  <Button size="sm" :disabled="Boolean(containerActionDisabled(item, 'stop'))" :loading="pending === `stop-${item.id}`" @click="containerAction(item, 'stop')"><Square />{{ t('resourcesPage.stop') }}</Button>
                  <Button size="sm" :disabled="Boolean(containerActionDisabled(item, 'restart'))" :loading="pending === `restart-${item.id}`" @click="containerAction(item, 'restart')"><RefreshCcw />{{ t('resourcesPage.restart') }}</Button>
                  <Button size="sm" :loading="pending === `logs-${item.id}`" @click="openLogs(item)"><FileText />{{ t('resourcesPage.logs') }}</Button>
                  <Button size="sm" variant="danger" :disabled="Boolean(containerActionDisabled(item, 'delete'))" @click="confirm('container', item.id, containerName(item))"><Trash2 />{{ t('common.delete') }}</Button>
                </footer>
              </article>
            </section>

            <section v-else-if="activeTab === 'images'" class="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] rounded-2xl border border-border bg-background">
              <div class="flex flex-wrap items-center justify-between gap-2 border-b border-border p-4">
                <div class="flex items-center gap-2 text-sm text-muted-foreground"><Boxes class="size-4" />{{ images?.observedAt || t('common.never') }}</div>
                <div class="flex flex-wrap gap-2">
                  <Button size="sm" :disabled="!imageUpgradeIds.length" :loading="pending === 'upgrade-images-selected'" @click="upgradeImages(true)">{{ t('resourcesPage.upgradeApplications') }}</Button>
                  <Button size="sm" :loading="pending === 'upgrade-images-all'" @click="upgradeImages(false)">{{ t('resourcesPage.upgradeAllApplications') }}</Button>
                  <Button size="sm" variant="danger" @click="confirm('image-prune')"><Trash2 />{{ t('resourcesPage.pruneUnused') }}</Button>
                </div>
              </div>
              <div class="min-h-0 overflow-auto p-3">
                <div v-if="loadingResource && !images" class="grid gap-2" aria-hidden="true">
                  <div v-for="item in 8" :key="item" class="grid grid-cols-[minmax(0,1fr)_80px] gap-3 rounded-xl border border-border p-3">
                    <div class="min-w-0">
                      <div class="motion-skeleton h-4 w-48 rounded bg-muted animate-pulse" />
                      <div class="motion-skeleton mt-2 h-3 w-72 max-w-full rounded bg-muted animate-pulse" />
                    </div>
                    <div class="motion-skeleton h-8 w-20 rounded bg-muted animate-pulse" />
                  </div>
                </div>
                <EmptyState v-else-if="error" :title="t('common.loadFailed')" :description="error">
                  <template #actions>
                    <Button size="sm" :loading="loadingResource" @click="loadResource"><RefreshCcw />{{ t('common.retry') }}</Button>
                  </template>
                </EmptyState>
                <EmptyState v-else-if="!images?.items.length" :title="t('resourcesPage.noImages')" :description="t('resourcesPage.noImagesHint')" />
                <div v-for="item in images?.items" v-else :key="item.id" class="mb-2 grid grid-cols-[minmax(0,1fr)_auto] gap-3 rounded-xl border border-border p-3">
                  <div class="min-w-0">
                    <div class="flex flex-wrap items-center gap-2">
                      <strong class="truncate text-sm text-foreground">{{ imageLabel(item) }}</strong>
                      <Badge :tone="imageTone(item)">{{ item.updateAvailable ? t('resourcesPage.updateAvailable') : item.inUse ? t('resourcesPage.inUse') : t('resourcesPage.unused') }}</Badge>
                    </div>
                    <p class="m-0 mt-1 truncate text-xs text-muted-foreground">{{ item.id }} · {{ formatBytes(item.size) }}</p>
                    <p v-if="item.lastError" class="m-0 mt-1 text-xs text-warning">{{ item.lastError }}</p>
                  </div>
                  <Button size="sm" variant="danger" :disabled="item.inUse" @click="confirm('image', item.id, imageLabel(item))"><Trash2 />{{ t('common.delete') }}</Button>
                </div>
              </div>
            </section>

            <section v-else-if="activeTab === 'networks'" class="grid h-full min-h-0 grid-cols-[minmax(0,1fr)_300px] grid-rows-[minmax(0,1fr)] gap-4 max-xl:grid-cols-1">
              <div class="min-h-0 overflow-auto rounded-2xl border border-border bg-background p-3">
                <div v-if="loadingResource && !networks.length" class="grid gap-2" aria-hidden="true">
                  <article v-for="item in 6" :key="item" class="rounded-xl border border-border p-4">
                    <div class="motion-skeleton h-4 w-40 rounded bg-muted animate-pulse" />
                    <div class="motion-skeleton mt-2 h-3 w-56 max-w-full rounded bg-muted animate-pulse" />
                  </article>
                </div>
                <EmptyState v-else-if="error" :title="t('common.loadFailed')" :description="error">
                  <template #actions>
                    <Button size="sm" :loading="loadingResource" @click="loadResource"><RefreshCcw />{{ t('common.retry') }}</Button>
                  </template>
                </EmptyState>
                <EmptyState v-else-if="!networks.length" :title="t('resourcesPage.noNetworks')" :description="t('resourcesPage.noNetworksHint')">
                  <template #actions>
                    <Button size="sm" :loading="pending === 'refresh'" @click="refreshCurrent"><RefreshCcw />{{ t('common.refresh') }}</Button>
                  </template>
                </EmptyState>
                <article v-for="item in networks" v-else :key="item.id" class="mb-2 rounded-xl border border-border p-4">
                  <div class="flex items-start justify-between gap-3">
                    <div>
                      <h3 class="m-0 text-sm font-semibold">{{ item.name }}</h3>
                      <p class="m-0 mt-1 text-xs text-muted-foreground">{{ item.driver }} / {{ item.scope }}</p>
                    </div>
                    <Badge :tone="item.internal ? 'warning' : item.labels?.['panel.managed'] ? 'success' : 'neutral'">{{ item.internal ? t('resourcesPage.internal') : item.labels?.['panel.managed'] ? t('resourcesPage.managed') : t('resourcesPage.external') }}</Badge>
                  </div>
                </article>
              </div>
              <aside class="rounded-2xl border border-info-border bg-info-bg p-4 text-sm text-info">
                <Router class="mb-3 size-5" />
                <h3 class="m-0 text-sm font-semibold">{{ t('resourcesPage.networkReadonly') }}</h3>
                <p class="m-0 mt-2">{{ t('resourcesPage.networkDeleteUnavailable') }}</p>
                <Button class="mt-3 w-full" disabled variant="danger" @click="confirm('network')"><Trash2 />{{ t('common.delete') }}</Button>
              </aside>
            </section>

            <section v-else class="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] rounded-2xl border border-border bg-background">
              <div class="flex flex-wrap items-center justify-between gap-2 border-b border-border p-4">
                <div class="flex items-center gap-2 text-sm text-muted-foreground"><Database class="size-4" />{{ t('resourcesPage.volumeSafety') }}</div>
                <Button size="sm" variant="danger" @click="confirm('volume-prune')"><Trash2 />{{ t('resourcesPage.pruneUnusedVolumes') }}</Button>
              </div>
              <div class="min-h-0 overflow-auto p-3">
                <div v-if="loadingResource && !volumes.length" class="grid gap-2" aria-hidden="true">
                  <div v-for="item in 8" :key="item" class="grid grid-cols-[minmax(0,1fr)_80px] gap-3 rounded-xl border border-border p-3">
                    <div class="min-w-0">
                      <div class="motion-skeleton h-4 w-44 rounded bg-muted animate-pulse" />
                      <div class="motion-skeleton mt-2 h-3 w-72 max-w-full rounded bg-muted animate-pulse" />
                    </div>
                    <div class="motion-skeleton h-8 w-20 rounded bg-muted animate-pulse" />
                  </div>
                </div>
                <EmptyState v-else-if="error" :title="t('common.loadFailed')" :description="error">
                  <template #actions>
                    <Button size="sm" :loading="loadingResource" @click="loadResource"><RefreshCcw />{{ t('common.retry') }}</Button>
                  </template>
                </EmptyState>
                <EmptyState v-else-if="!volumes.length" :title="t('resourcesPage.noVolumes')" :description="t('resourcesPage.noVolumesHint')">
                  <template #actions>
                    <Button size="sm" :loading="pending === 'refresh'" @click="refreshCurrent"><RefreshCcw />{{ t('common.refresh') }}</Button>
                  </template>
                </EmptyState>
                <div v-for="item in volumes" v-else :key="item.name" class="mb-2 grid grid-cols-[minmax(0,1fr)_auto] gap-3 rounded-xl border border-border p-3">
                  <div class="min-w-0">
                    <div class="flex flex-wrap items-center gap-2">
                      <strong class="truncate text-sm text-foreground">{{ item.name }}</strong>
                      <Badge :tone="volumeTone(item)">{{ item.inUse ? t('resourcesPage.inUse') : t('resourcesPage.unused') }}</Badge>
                    </div>
                    <p class="m-0 mt-1 truncate text-xs text-muted-foreground">{{ item.mountpoint }}</p>
                    <p class="m-0 mt-1 text-xs text-muted-foreground">{{ item.containerCount }} {{ t('resourcesPage.containersAttached') }} · {{ formatBytes(item.usageData?.size) }}</p>
                  </div>
                  <Button size="sm" variant="danger" :disabled="item.inUse" @click="confirm('volume', item.name, item.name)"><Trash2 />{{ t('common.delete') }}</Button>
                </div>
              </div>
            </section>
          </div>
        </article>
      </main>
      </template>
    </MasterDetailLayout>

    <Dialog v-model:open="logsDialog" :title="logsTitle" :description="t('resourcesPage.logsDescription')" :close-label="t('common.close')">
      <pre class="max-h-[520px] overflow-auto rounded-xl border border-border bg-muted p-3 text-xs leading-5 text-foreground">{{ logsText }}</pre>
      <template #footer>
        <Button @click="logsDialog = false">{{ t('common.close') }}</Button>
      </template>
    </Dialog>

    <Dialog v-model:open="pullDialog" :title="t('resourcesPage.pullImage')" :description="t('resourcesPage.pullImageDescription')" :close-label="t('common.close')">
      <label class="grid gap-1 text-sm">{{ t('resourcesPage.imageReference') }}<Input v-model="pullForm.reference" /></label>
      <template #footer>
        <Button @click="pullDialog = false">{{ t('common.cancel') }}</Button>
        <Button variant="primary" :loading="pending === 'pull-image'" :disabled="!pullForm.reference.trim()" @click="pullImage"><DownloadCloud />{{ t('resourcesPage.pullImage') }}</Button>
      </template>
    </Dialog>

    <Dialog v-model:open="confirmDialog" :title="t('resourcesPage.confirmDanger')" :description="confirmTarget?.label || t('resourcesPage.confirmDangerDescription')" :close-label="t('common.close')">
      <div class="rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning">{{ t('resourcesPage.confirmDangerImpact') }}</div>
      <template #footer>
        <Button @click="confirmDialog = false">{{ t('common.cancel') }}</Button>
        <Button variant="danger" :loading="pending.startsWith('confirm-')" @click="confirmDanger"><Trash2 />{{ t('common.delete') }}</Button>
      </template>
    </Dialog>
  </ConsolePage>
</template>
