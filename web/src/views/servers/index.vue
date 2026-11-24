<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Cable, Eye, MoreHorizontal, PlayCircle, Plus, RefreshCcw, Trash2, Wrench } from '@/icons';
import { credentialsApi } from '@/api/credentials';
import { serversApi } from '@/api/servers';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue';
import Dropdown from '@/components/ui/Dropdown.vue';
import DropdownItem from '@/components/ui/DropdownItem.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import IconButton from '@/components/ui/IconButton.vue';
import PaginationBar from '@/components/ui/PaginationBar.vue';
import SearchInput from '@/components/ui/SearchInput.vue';
import Skeleton from '@/components/ui/Skeleton.vue';
import Table from '@/components/ui/Table.vue';
import Tabs from '@/components/ui/Tabs.vue';
import Select from '@/components/ui/Select.vue';
import Tooltip from '@/components/ui/Tooltip.vue';
import { useErrorDialog } from '@/components/ui/errorDialog';
import { useSuccessToast } from '@/components/ui/toast';
import TableDetailName from '@/components/patterns/TableDetailName.vue';
import ListCommandBar from '@/components/patterns/ListCommandBar.vue';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import ListPage from '@/components/templates/ListPage.vue';
import TableDetailLayout from '@/components/templates/TableDetailLayout.vue';
import { useOverlayBehavior } from '@/composables/useOverlayBehavior';
import { useI18n } from '@/i18n';
import type { CredentialDto } from '@/types/credentials';
import type { ServerDto } from '@/types/servers';
import { formatDateTime } from '@/utils/datetime';
import { createLatestRequestGuard, isAbortError, normalizePage } from '@/views/_shared/requestState';
import ServerFormDialog from './ServerFormDialog.vue';
import AgentTab from './tabs/AgentTab.vue';
import ContainersTab from './tabs/ContainersTab.vue';
import FirewallTab from './tabs/FirewallTab.vue';
import SshAccessTab from './tabs/SshAccessTab.vue';
import ImagesTab from './tabs/ImagesTab.vue';
import NetworksTab from './tabs/NetworksTab.vue';
import OverviewTab from './tabs/OverviewTab.vue';
import PackagesTab from './tabs/PackagesTab.vue';
import VolumesTab from './tabs/VolumesTab.vue';
import {
  isLegacyNetworkTab,
  isLegacyResourceTab,
  isLegacySystemTab,
  normalizeServerDetailTab,
  normalizeServerNetworkSubtab,
  normalizeServerResourceSubtab,
  normalizeServerSystemSubtab,
  canRunPrivilegedOperation,
  proxyListHintKey,
  proxyShortLabelKey,
  serverDetailTabs,
  serverNetworkSubtabs,
  serverResourceSubtabs,
  serverSystemSubtabs,
  reportStabilityStatusKey,
  reportStabilityTone,
  serverSummaryStatusKey,
  serverSummaryTone,
  shouldShowListReportStability,
  type ServerDetailTab,
  type ServerNetworkSubtab,
  type ServerResourceSubtab,
  type ServerSystemSubtab,
} from './model';

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const notifyError = useErrorDialog();
const notifySuccess = useSuccessToast();
const pageSize = 50;
const servers = ref<ServerDto[]>([]);
const serverDetails = ref<Record<string, ServerDto>>({});
const credentials = ref<CredentialDto[]>([]);
const selectedId = ref(String(route.query.server ?? ''));
const selectedTab = ref<ServerDetailTab>(normalizeServerDetailTab(route.query.tab));
const systemSubtab = ref<ServerSystemSubtab>(normalizeServerSystemSubtab(route.query.subtab, route.query.tab));
const resourceSubtab = ref<ServerResourceSubtab>(normalizeServerResourceSubtab(route.query.subtab, route.query.tab));
const networkSubtab = ref<ServerNetworkSubtab>(normalizeServerNetworkSubtab(route.query.subtab, route.query.tab));
const search = ref(String(route.query.search ?? ''));
const page = ref(normalizePage(route.query.page));
const totalServers = ref(0);
const loading = ref(false);
const detailLoading = ref(false);
const detailError = ref('');
const error = ref('');
const credentialError = ref('');
const serverDialog = ref(false);
const editing = ref<ServerDto | null>(null);
const confirmTarget = ref<ServerDto | null>(null);
const deleting = ref(false);
const rowBusy = ref<{ id: string; kind: 'test' | 'edit' } | null>(null);
const restartOpen = ref(false);
const restarting = ref(false);
const detailRoot = ref<HTMLElement | null>(null);

const visited = reactive<Record<ServerDetailTab, boolean>>(Object.fromEntries(serverDetailTabs.map((tab) => [tab, false])) as Record<ServerDetailTab, boolean>);
const visitedSystem = reactive<Record<ServerSystemSubtab, boolean>>(Object.fromEntries(serverSystemSubtabs.map((tab) => [tab, false])) as Record<ServerSystemSubtab, boolean>);
const visitedResources = reactive<Record<ServerResourceSubtab, boolean>>(Object.fromEntries(serverResourceSubtabs.map((tab) => [tab, false])) as Record<ServerResourceSubtab, boolean>);
const visitedNetwork = reactive<Record<ServerNetworkSubtab, boolean>>(Object.fromEntries(serverNetworkSubtabs.map((tab) => [tab, false])) as Record<ServerNetworkSubtab, boolean>);
let searchTimer: ReturnType<typeof setTimeout> | undefined;
let detailController: AbortController | null = null;
const listGuard = createLatestRequestGuard();
let detailRequestId = 0;

const selectedServer = computed(() => serverDetails.value[selectedId.value] ?? servers.value.find((item) => item.id === selectedId.value) ?? null);
const detailOpen = computed(() => Boolean(selectedId.value));
const activeTab = computed<ServerDetailTab>({
  get: () => selectedTab.value,
  set: (value) => selectTab(value),
});
const detailTabLabelKeys: Record<ServerDetailTab, string> = {
  overview: 'serversPage.tab.overview',
  system: 'serversPage.tab.system',
  resources: 'serversPage.tab.resources',
  'network-security': 'serversPage.tab.networkSecurity',
};
const tabItems = computed(() => serverDetailTabs.map((tab) => ({ value: tab, label: t(detailTabLabelKeys[tab]) })));
const systemSubtabItems = computed(() => serverSystemSubtabs.map((tab) => ({ value: tab, label: t(`serversPage.tab.${tab}`) })));
const resourceSubtabItems = computed(() => serverResourceSubtabs.map((tab) => ({ value: tab, label: t(`serversPage.tab.${tab}`) })));
const networkSubtabItems = computed(() => serverNetworkSubtabs
  .map((tab) => ({
    value: tab,
    label: tab === 'ssh-access' ? t('serversPage.tab.sshAccess') : t('serversPage.tab.firewall'),
  })));
const detailTitle = computed(() => selectedServer.value?.name ?? t('common.loading'));
const detailDescription = computed(() => selectedServer.value ? `${selectedServer.value.host}:${selectedServer.value.port} / ${selectedServer.value.os?.prettyName || t('common.notAvailable')}` : '');
/** 覆盖式详情从服务器列表入口建立面包屑；末项（当前服务器名）无 to，由 BreadcrumbBar 标记 aria-current。 */
const detailBreadcrumbs = computed(() => [
  { label: t('routes.servers.title'), to: '/servers' },
  { label: selectedServer.value?.name ?? t('common.loading') },
]);

// 覆盖式详情不是弹窗，但复用浮层键盘模型：Escape 关闭、焦点归还触发按钮、
// 与内层 Dialog/Dropdown 组成 LIFO 栈（内层优先）；详情是页面级视图，不锁背景滚动。
const { onKeydown: onDetailKeydown } = useOverlayBehavior({
  open: () => detailOpen.value,
  containerRef: detailRoot,
  onClose: closeDetail,
  lockScroll: false,
});

const columns = computed(() => [
  { key: 'name', label: t('common.name') },
  { key: 'status', label: t('common.status'), nowrap: true, width: 'w-36', hideBelow: 'sm' as const },
  { key: 'lastChecked', label: t('serversPage.lastChecked'), nowrap: true, width: 'w-40', hideBelow: 'wide' as const },
  { key: 'actions', label: t('common.actions'), align: 'right' as const, nowrap: true, width: 'w-52' },
]);

/** 状态单元格 tooltip：单一综合徽标，链路稳定性/连接代理等次级信息并入提示，不再并排多枚。 */
function statusTooltip(server: ServerDto) {
  const parts = [statusText(server)];
  if (shouldShowListReportStability(server)) parts.push(t(reportStabilityStatusKey(server)));
  const proxy = proxyShortLabelKey(server);
  if (proxy) parts.push(`${t(proxy)} · ${t(proxyListHintKey(server))}`);
  return parts.join(' · ');
}

/**
 * 列表页级状态：首载/页级错误/无库存走 ListPage 状态槽；
 * 表格内的筛选空态仍由 Table #empty 负责（保留）。
 */
const listState = computed<'ready' | 'loading' | 'error' | 'empty'>(() => {
  if (loading.value && !servers.value.length) return 'loading';
  if (error.value && !servers.value.length) return 'error';
  if (!loading.value && !servers.value.length && !search.value.trim() && page.value === 1) return 'empty';
  return 'ready';
});

function statusText(server: ServerDto) {
  return t(serverSummaryStatusKey(server));
}

function detailSubtabQuery() {
  if (selectedTab.value === 'system') {
    return systemSubtab.value === 'agent' ? undefined : systemSubtab.value;
  }
  if (selectedTab.value === 'resources') {
    return resourceSubtab.value === 'containers' ? undefined : resourceSubtab.value;
  }
  if (selectedTab.value === 'network-security') {
    return networkSubtab.value === 'firewall' ? undefined : networkSubtab.value;
  }
  return undefined;
}

function detailQuery(serverId = selectedId.value) {
  return {
    ...route.query,
    server: serverId || undefined,
    tab: selectedTab.value === 'overview' ? undefined : selectedTab.value,
    subtab: detailSubtabQuery(),
  };
}

function openDetail(server: ServerDto) {
  if (selectedId.value !== server.id) {
    selectedId.value = server.id;
    selectedTab.value = 'overview';
    systemSubtab.value = 'agent';
    resourceSubtab.value = 'containers';
    networkSubtab.value = 'firewall';
    resetVisited();
  }
  if (route.query.server !== server.id) void router.push({ query: detailQuery(server.id) });
}

function closeDetail() {
  if (!detailOpen.value) return;
  selectedId.value = '';
  void router.replace({ query: { ...route.query, server: undefined, tab: undefined, subtab: undefined } });
}

function selectTab(value: ServerDetailTab) {
  selectedTab.value = value;
  visited[value] = true;
  if (value === 'system') visitedSystem[systemSubtab.value] = true;
  if (value === 'resources') visitedResources[resourceSubtab.value] = true;
  if (value === 'network-security') visitedNetwork[networkSubtab.value] = true;
  const expectedTab = value === 'overview' ? undefined : value;
  const expectedSubtab = detailSubtabQuery();
  if (selectedId.value && (route.query.tab !== expectedTab || route.query.subtab !== expectedSubtab)) {
    void router.replace({ query: detailQuery() });
  }
}

function selectSystemSubtab(value: string) {
  const next = value as ServerSystemSubtab;
  systemSubtab.value = next;
  visitedSystem[next] = true;
  if (selectedId.value && route.query.subtab !== (next === 'agent' ? undefined : next)) {
    void router.replace({ query: detailQuery() });
  }
}

function selectResourceSubtab(value: string) {
  const next = value as ServerResourceSubtab;
  resourceSubtab.value = next;
  visitedResources[next] = true;
  if (selectedId.value && route.query.subtab !== (next === 'containers' ? undefined : next)) {
    void router.replace({ query: detailQuery() });
  }
}

function selectNetworkSubtab(value: string) {
  const next = value as ServerNetworkSubtab;
  networkSubtab.value = next;
  visitedNetwork[next] = true;
  if (selectedId.value) void router.replace({ query: detailQuery() });
}

function resetVisited() {
  serverDetailTabs.forEach((tab) => { visited[tab] = false; });
  serverSystemSubtabs.forEach((tab) => { visitedSystem[tab] = false; });
  serverResourceSubtabs.forEach((tab) => { visitedResources[tab] = false; });
  serverNetworkSubtabs.forEach((tab) => { visitedNetwork[tab] = false; });
  visited[selectedTab.value] = true;
  if (selectedTab.value === 'system') visitedSystem[systemSubtab.value] = true;
  if (selectedTab.value === 'resources') visitedResources[resourceSubtab.value] = true;
  if (selectedTab.value === 'network-security') visitedNetwork[networkSubtab.value] = true;
}

watch(activeTab, (tab) => {
  visited[tab] = true;
  if (tab === 'system') visitedSystem[systemSubtab.value] = true;
  if (tab === 'resources') visitedResources[resourceSubtab.value] = true;
  if (tab === 'network-security') visitedNetwork[networkSubtab.value] = true;
});
watch(systemSubtab, (subtab) => { visitedSystem[subtab] = true; });
watch(resourceSubtab, (subtab) => { visitedResources[subtab] = true; });
watch(networkSubtab, (subtab) => { visitedNetwork[subtab] = true; });
watch(selectedId, () => { resetVisited(); }, { immediate: true });

watch(() => route.query.server, (value) => {
  const next = String(value ?? '');
  if (next !== selectedId.value) selectedId.value = next;
});
watch(() => route.query.tab, (value) => {
  const next = normalizeServerDetailTab(value);
  if (next !== selectedTab.value) selectedTab.value = next;
  const legacy = route.query.tab;
  if (isLegacySystemTab(legacy)) {
    const nextSub = normalizeServerSystemSubtab(undefined, legacy);
    if (nextSub !== systemSubtab.value) systemSubtab.value = nextSub;
  }
  if (isLegacyResourceTab(legacy)) {
    if (legacy !== resourceSubtab.value) resourceSubtab.value = legacy;
  }
  if (isLegacyNetworkTab(legacy)) {
    const nextSub = normalizeServerNetworkSubtab(undefined, legacy);
    if (nextSub !== networkSubtab.value) networkSubtab.value = nextSub;
  }
});
watch(() => route.query.subtab, (value) => {
  if (selectedTab.value === 'system' || isLegacySystemTab(route.query.tab)) {
    const next = normalizeServerSystemSubtab(value, route.query.tab);
    if (next !== systemSubtab.value) systemSubtab.value = next;
    return;
  }
  if (selectedTab.value === 'resources' || isLegacyResourceTab(route.query.tab)) {
    const next = normalizeServerResourceSubtab(value, route.query.tab);
    if (next !== resourceSubtab.value) resourceSubtab.value = next;
    return;
  }
  if (selectedTab.value === 'network-security' || isLegacyNetworkTab(route.query.tab)) {
    const next = normalizeServerNetworkSubtab(value, route.query.tab);
    if (next !== networkSubtab.value) networkSubtab.value = next;
  }
});
watch(() => route.query.page, (value) => {
  const next = normalizePage(value);
  if (next !== page.value) page.value = next;
});

watch(search, (value) => {
  void router.replace({ query: { ...route.query, search: value || undefined } });
  if (searchTimer) clearTimeout(searchTimer);
  searchTimer = setTimeout(() => {
    if (page.value !== 1) page.value = 1;
    else void loadServers();
  }, 250);
});
watch(page, (value) => {
  void router.replace({ query: { ...route.query, page: value > 1 ? String(value) : undefined } });
  void loadServers();
});
watch(selectedId, () => { void loadServerDetail(); }, { immediate: true });

async function loadServerDetail() {
  const id = selectedId.value;
  detailController?.abort();
  const requestId = ++detailRequestId;
  detailError.value = '';
  if (!id || serverDetails.value[id]) {
    detailLoading.value = false;
    return;
  }
  const controller = new AbortController();
  detailController = controller;
  detailLoading.value = true;
  try {
    const detail = await serversApi.get(id, { signal: controller.signal });
    if (requestId !== detailRequestId || selectedId.value !== id) return;
    serverDetails.value = { ...serverDetails.value, [id]: detail };
    detailError.value = '';
  } catch (err) {
    if (isAbortError(err)) return;
    if (requestId === detailRequestId) {
      const message = err instanceof Error ? err.message : t('serversPage.loadFailed');
      detailError.value = message;
    }
  } finally {
    if (requestId === detailRequestId) detailLoading.value = false;
  }
}

function invalidateServerDetail(id?: string) {
  const target = id || selectedId.value;
  if (!target) return;
  const next = { ...serverDetails.value };
  delete next[target];
  serverDetails.value = next;
  if (target === selectedId.value) void loadServerDetail();
}

async function load() {
  const requestId = listGuard.begin();
  loading.value = true;
  error.value = '';
  credentialError.value = '';
  try {
    const [serversResult, credentialsResult] = await Promise.allSettled([
      serversApi.listPage({ page: page.value, pageSize, q: search.value.trim() || undefined }),
      credentialsApi.list(),
    ]);
    if (!listGuard.isCurrent(requestId)) return;
    if (serversResult.status === 'fulfilled') {
      servers.value = serversResult.value.items;
      totalServers.value = serversResult.value.total;
      const queryServer = String(route.query.server ?? '');
      if (!queryServer && selectedId.value && !serversResult.value.items.some((item) => item.id === selectedId.value)) {
        selectedId.value = '';
      }
    } else {
      error.value = serversResult.reason instanceof Error ? serversResult.reason.message : t('serversPage.loadFailed');
    }
    if (credentialsResult.status === 'fulfilled') {
      credentials.value = credentialsResult.value;
    } else {
      credentials.value = [];
      credentialError.value = credentialsResult.reason instanceof Error ? credentialsResult.reason.message : t('serversPage.credentialsLoadFailed');
    }
    if (selectedId.value) invalidateServerDetail(selectedId.value);
  } finally {
    if (listGuard.isCurrent(requestId)) loading.value = false;
  }
}

async function loadServers() {
  const requestId = listGuard.begin();
  loading.value = true;
  error.value = '';
  try {
    const result = await serversApi.listPage({ page: page.value, pageSize, q: search.value.trim() || undefined });
    if (!listGuard.isCurrent(requestId)) return;
    servers.value = result.items;
    totalServers.value = result.total;
    // 翻页/搜索后选中项不在当前页时收起详情，避免覆盖一个已不在列表中的对象。
    if (selectedId.value && !result.items.some((item) => item.id === selectedId.value)) {
      selectedId.value = '';
      void router.replace({ query: { ...route.query, server: undefined, tab: undefined, subtab: undefined } });
    }
  } catch (err) {
    if (isAbortError(err)) return;
    error.value = err instanceof Error ? err.message : t('serversPage.loadFailed');
  } finally {
    if (listGuard.isCurrent(requestId)) loading.value = false;
  }
}

async function testConnection(server: ServerDto) {
  if (rowBusy.value) return;
  rowBusy.value = { id: server.id, kind: 'test' };
  try {
    const tested = await serversApi.test(server.id);
    invalidateServerDetail(tested.id);
    notifySuccess(t('serversPage.testSucceeded', { name: tested.name }), tested);
    await load();
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    rowBusy.value = null;
  }
}

async function openEdit(server: ServerDto) {
  if (rowBusy.value) return;
  rowBusy.value = { id: server.id, kind: 'edit' };
  try {
    const detail = serverDetails.value[server.id] ?? await serversApi.get(server.id);
    serverDetails.value = { ...serverDetails.value, [server.id]: detail };
    editing.value = detail;
    serverDialog.value = true;
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('serversPage.loadFailed'), err);
  } finally {
    rowBusy.value = null;
  }
}

function openCreate() {
  editing.value = null;
  serverDialog.value = true;
}

async function handleSaved(saved: ServerDto) {
  selectedId.value = saved.id;
  selectedTab.value = 'overview';
  invalidateServerDetail(saved.id);
  await router.push({ query: { ...route.query, server: saved.id, tab: undefined, subtab: undefined } });
  await load();
}

async function loadCredentials() {
  try {
    credentials.value = await credentialsApi.list();
    credentialError.value = '';
  } catch (err) {
    credentials.value = [];
    credentialError.value = err instanceof Error ? err.message : t('serversPage.credentialsLoadFailed');
  }
}

async function deleteSelected() {
  const target = confirmTarget.value;
  if (!target) return;
  deleting.value = true;
  try {
    const result = await serversApi.delete(target.id);
    const next = { ...serverDetails.value };
    delete next[target.id];
    serverDetails.value = next;
    notifySuccess(t('serversPage.deleted', { name: target.name }), result);
    confirmTarget.value = null;
    closeDetail();
    await load();
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    deleting.value = false;
  }
}

function refreshDetail() {
  invalidateServerDetail();
  void load();
}

async function restartHost() {
  const server = selectedServer.value;
  if (!server || restarting.value) return;
  restarting.value = true;
  try {
    const accepted = await serversApi.restart(server.id);
    notifySuccess(t('serversPage.restartAccepted', { taskId: accepted.taskId }), accepted);
    restartOpen.value = false;
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    restarting.value = false;
  }
}

onMounted(() => {
  void load();
});
onBeforeUnmount(() => {
  if (searchTimer) clearTimeout(searchTimer);
  detailController?.abort();
  listGuard.invalidate();
});
</script>

<template>
  <TableDetailLayout class="h-full" :has-detail="detailOpen">
    <template #list>
      <ConsolePage :title="t('routes.servers.title')" :description="t('routes.servers.description')">
        <ListPage :state="listState">
          <template #toolbar>
            <ListCommandBar :label="t('routes.servers.title')">
              <template #search>
                <SearchInput v-model="search" clearable :placeholder="t('serversPage.searchPlaceholder')" :label="t('common.search')" :clear-label="t('common.clearSearch')" />
              </template>
              <template #actions>
                <Button class="max-sm:w-full" :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
                <Button v-permission="'servers.manage'" data-perm-anchor="servers.manage.create" class="max-sm:w-full" variant="primary" @click="openCreate"><Plus />{{ t('common.add') }}</Button>
              </template>
            </ListCommandBar>
          </template>

          <template #loading>
            <div class="grid content-start gap-2 rounded-overlay border border-card-stroke bg-card p-3" aria-busy="true">
              <Skeleton v-for="item in 6" :key="item" class="h-9" />
            </div>
          </template>

          <template #error>
            <div class="grid h-full min-h-0 place-items-center">
              <EmptyState variant="page" :title="t('common.loadFailed')" :description="error">
                <template #actions>
                  <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button>
                </template>
              </EmptyState>
            </div>
          </template>

          <template #empty>
            <div class="grid h-full min-h-0 place-items-center">
              <EmptyState variant="page" :title="t('serversPage.noServers')" :description="t('serversPage.noServersHint')">
                <template #actions>
                  <Button v-permission="'servers.manage'" size="sm" variant="primary" @click="openCreate"><Plus />{{ t('common.add') }}</Button>
                </template>
              </EmptyState>
            </div>
          </template>

          <Table
            class="h-full max-lg:max-h-list"
            :columns="columns"
            :rows="servers"
            row-key="id"
            row-height="auto"
            :active-key="selectedId"
            :loading-label="t('common.loading')"
          >
            <template #name="{ row }">
              <div class="grid min-w-0 content-start">
                <TableDetailName class="max-w-64" :name="row.name" :label="t('common.openDetails', { name: row.name })" @click="openDetail(row)" />
                <span class="truncate panel-mono text-caption text-muted-foreground max-sm:hidden">{{ row.host }}:{{ row.port }}</span>
                <!-- <sm：状态列隐藏后折入名称副行，保留单一综合徽标语义。 -->
                <span class="sm:hidden"><Badge class="h-5 shrink-0" :tone="serverSummaryTone(row)">{{ statusText(row) }}</Badge></span>
              </div>
            </template>
            <template #status="{ row }">
              <div class="flex items-center whitespace-nowrap">
                <Tooltip :text="statusTooltip(row)">
                  <Badge class="h-5 shrink-0" :tone="serverSummaryTone(row)">{{ statusText(row) }}</Badge>
                </Tooltip>
              </div>
            </template>
            <template #lastChecked="{ row }">
              <span class="whitespace-nowrap text-muted-foreground">{{ formatDateTime(row.lastCheckedAt, t('common.never')) }}</span>
            </template>
            <template #actions="{ row }">
              <div class="flex items-center justify-end gap-1">
                <Button size="sm" variant="secondary" @click="openDetail(row)"><Eye />{{ t('common.details') }}</Button>
                <Button class="max-wide:hidden" size="sm" variant="ghost" :loading="rowBusy?.id === row.id && rowBusy.kind === 'test'" :disabled="rowBusy?.id === row.id" @click="testConnection(row)"><Cable />{{ t('serversPage.testConnection') }}</Button>
                <Dropdown align="right">
                  <template #trigger>
                    <IconButton size="sm" :label="t('common.moreActions')"><MoreHorizontal /></IconButton>
                  </template>
                  <DropdownItem class="wide:hidden" :disabled="rowBusy?.id === row.id" @click="testConnection(row)"><Cable />{{ t('serversPage.testConnection') }}</DropdownItem>
                  <DropdownItem v-permission="'servers.manage'" :disabled="rowBusy?.id === row.id" @click="openEdit(row)"><Wrench />{{ t('common.edit') }}</DropdownItem>
                  <DropdownItem tone="danger" @click="confirmTarget = row"><Trash2 />{{ t('common.delete') }}</DropdownItem>
                </Dropdown>
              </div>
            </template>
            <template #empty>
              <EmptyState
                variant="inset"
                :title="search ? t('common.filterEmpty') : t('serversPage.noServers')"
                :description="search ? t('common.filterEmptyHint') : t('serversPage.noServersHint')"
              />
            </template>
          </Table>

          <template #pagination>
            <PaginationBar v-model:page="page" :page-size="pageSize" :total="totalServers" :loading="loading" :previous-label="t('common.previous')" :next-label="t('common.next')" />
          </template>
        </ListPage>
      </ConsolePage>
    </template>

    <template #detail>
      <ConsolePage :title="detailTitle" :description="detailDescription" :breadcrumbs="detailBreadcrumbs" :back-label="t('common.backToList')" :body-scroll="false" @back="closeDetail">
        <template v-if="selectedServer" #actions>
          <Button v-permission="'servers.manage'" class="max-sm:w-full" variant="secondary" :disabled="rowBusy?.id === selectedServer.id" :loading="rowBusy?.id === selectedServer.id && rowBusy?.kind === 'edit'" @click="openEdit(selectedServer)"><Wrench />{{ t('common.edit') }}</Button>
          <Button v-permission="'servers.execute'" data-perm-anchor="servers.execute.restart" class="max-sm:w-full" variant="danger" :disabled="!canRunPrivilegedOperation(selectedServer)" :loading="restarting" @click="restartOpen = true"><PlayCircle />{{ t('serversPage.restart') }}</Button>
        </template>
        <div ref="detailRoot" data-testid="server-detail" class="h-full min-h-0 outline-none" tabindex="-1" @keydown="onDetailKeydown">
          <EmptyState v-if="detailError && !serverDetails[selectedId]" variant="page" :title="t('common.loadFailed')" :description="detailError">
            <template #actions>
              <Button size="sm" :loading="detailLoading" @click="invalidateServerDetail()"><RefreshCcw />{{ t('common.retry') }}</Button>
            </template>
          </EmptyState>
          <div v-else-if="!selectedServer" class="grid content-start gap-4">
            <Skeleton class="h-7 w-48" />
            <Skeleton class="h-4 w-72 max-w-full" />
            <div class="mt-4 grid gap-3">
              <Skeleton v-for="item in 3" :key="item" class="h-32" />
            </div>
          </div>
          <Tabs v-else v-model="activeTab" class="h-full min-h-0" :tabs="tabItems" keep-panels>
            <div :key="selectedId" class="h-full min-h-0">
              <OverviewTab v-if="visited.overview" v-show="activeTab === 'overview'" :active="activeTab === 'overview'" :server="selectedServer" :credentials="credentials" @refresh="refreshDetail" />
              <div v-if="visited.system" v-show="activeTab === 'system'" class="flex h-full min-h-0 flex-col gap-3">
                <div class="flex shrink-0 items-center gap-2">
                  <span class="text-caption text-muted-foreground" aria-hidden="true">{{ t('serversPage.tab.system') }}</span>
                  <Select class="w-44" :model-value="systemSubtab" :options="systemSubtabItems" :aria-label="t('serversPage.tab.system')" @update:model-value="selectSystemSubtab" />
                </div>
                <div class="min-h-0 flex-1">
                  <AgentTab v-if="visitedSystem.agent" v-show="systemSubtab === 'agent'" :active="activeTab === 'system' && systemSubtab === 'agent'" :server="selectedServer" />
                  <PackagesTab v-if="visitedSystem.packages" v-show="systemSubtab === 'packages'" :server="selectedServer" />
                </div>
              </div>
              <div v-if="visited.resources" v-show="activeTab === 'resources'" class="flex h-full min-h-0 flex-col gap-3">
                <div class="flex shrink-0 items-center gap-2">
                  <span class="text-caption text-muted-foreground" aria-hidden="true">{{ t('serversPage.tab.resources') }}</span>
                  <Select class="w-44" :model-value="resourceSubtab" :options="resourceSubtabItems" :aria-label="t('serversPage.tab.resources')" @update:model-value="selectResourceSubtab" />
                </div>
                <div class="min-h-0 flex-1">
                  <ContainersTab v-if="visitedResources.containers" v-show="resourceSubtab === 'containers'" :server="selectedServer" />
                  <ImagesTab v-if="visitedResources.images" v-show="resourceSubtab === 'images'" :server="selectedServer" />
                  <NetworksTab v-if="visitedResources.networks" v-show="resourceSubtab === 'networks'" :server="selectedServer" />
                  <VolumesTab v-if="visitedResources.volumes" v-show="resourceSubtab === 'volumes'" :server="selectedServer" />
                </div>
              </div>
              <div v-if="visited['network-security']" v-show="activeTab === 'network-security'" class="flex h-full min-h-0 flex-col gap-3">
                <div v-if="networkSubtabItems.length > 1" class="flex shrink-0 items-center gap-2">
                  <span class="text-caption text-muted-foreground" aria-hidden="true">{{ t('serversPage.tab.networkSecurity') }}</span>
                  <Select class="w-44" :model-value="networkSubtab" :options="networkSubtabItems" :aria-label="t('serversPage.tab.networkSecurity')" @update:model-value="selectNetworkSubtab" />
                </div>
                <div class="min-h-0 flex-1">
                  <FirewallTab v-if="visitedNetwork.firewall" v-show="networkSubtab === 'firewall'" :server="selectedServer" />
                  <SshAccessTab v-if="visitedNetwork['ssh-access']" v-show="networkSubtab === 'ssh-access'" :server="selectedServer" :active="activeTab === 'network-security' && networkSubtab === 'ssh-access'" />
                </div>
              </div>
            </div>
          </Tabs>
        </div>
      </ConsolePage>
    </template>
  </TableDetailLayout>

  <ServerFormDialog
    v-model:open="serverDialog"
    :editing="editing"
    :credentials="credentials"
    :credential-error="credentialError"
    :servers="servers"
    @saved="handleSaved"
    @refresh-credentials="loadCredentials"
  />

  <ConfirmDialog
    :open="Boolean(confirmTarget)"
    :title="t('serversPage.deleteServer')"
    :description="confirmTarget ? t('serversPage.deleteServerDescription', { name: confirmTarget.name }) : ''"
    :impact="t('serversPage.deleteServerImpact')"
    tone="danger"
    :loading="deleting"
    :confirm-label="t('common.delete')"
    :cancel-label="t('common.cancel')"
    :checkbox-label="t('serversPage.confirmCheckbox')"
    @update:open="(value) => { if (!value) confirmTarget = null; }"
    @confirm="deleteSelected"
    @cancel="confirmTarget = null"
  />

  <ConfirmDialog
    :open="restartOpen"
    :title="t('serversPage.confirmRestartTitle')"
    :description="selectedServer ? t('serversPage.confirmRestartDescription', { name: selectedServer.name }) : ''"
    :impact="t('serversPage.confirmRestartImpact')"
    tone="danger"
    :loading="restarting"
    :confirm-label="t('common.confirm')"
    :cancel-label="t('common.cancel')"
    :checkbox-label="t('serversPage.confirmCheckbox')"
    @update:open="(value) => { restartOpen = value; }"
    @confirm="restartHost"
    @cancel="restartOpen = false"
  />
</template>
