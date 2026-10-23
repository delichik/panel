<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Cable, Eye, MoreHorizontal, Plus, RefreshCcw, Trash2, Wrench } from '@lucide/vue';
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
import Tooltip from '@/components/ui/Tooltip.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import TableDetailName from '@/components/patterns/TableDetailName.vue';
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
import ImagesTab from './tabs/ImagesTab.vue';
import MetricsTab from './tabs/MetricsTab.vue';
import NetworksTab from './tabs/NetworksTab.vue';
import OverviewTab from './tabs/OverviewTab.vue';
import PackagesTab from './tabs/PackagesTab.vue';
import TailscaleTab from './tabs/TailscaleTab.vue';
import VolumesTab from './tabs/VolumesTab.vue';
import {
  isLegacyResourceTab,
  normalizeFirewallView,
  normalizeServerDetailTab,
  normalizeServerResourceSubtab,
  serverDetailTabs,
  serverResourceSubtabs,
  serverSummaryStatusKey,
  serverSummaryTone,
  type FirewallView,
  type ServerDetailTab,
  type ServerResourceSubtab,
} from './model';

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const notifyError = useErrorToast();
const notifySuccess = useSuccessToast();
const isDev = import.meta.env.DEV;

const pageSize = 50;
const servers = ref<ServerDto[]>([]);
const serverDetails = ref<Record<string, ServerDto>>({});
const credentials = ref<CredentialDto[]>([]);
const selectedId = ref(String(route.query.server ?? ''));
const selectedTab = ref<ServerDetailTab>(normalizeServerDetailTab(route.query.tab));
const resourceSubtab = ref<ServerResourceSubtab>(normalizeServerResourceSubtab(route.query.subtab, route.query.tab));
const firewallView = ref<FirewallView>(isDev ? normalizeFirewallView(route.query.subtab) : 'ufw');
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
const tailscaleApplying = ref(false);
const detailRoot = ref<HTMLElement | null>(null);

const visited = reactive<Record<ServerDetailTab, boolean>>(Object.fromEntries(serverDetailTabs.map((tab) => [tab, false])) as Record<ServerDetailTab, boolean>);
const visitedResources = reactive<Record<ServerResourceSubtab, boolean>>(Object.fromEntries(serverResourceSubtabs.map((tab) => [tab, false])) as Record<ServerResourceSubtab, boolean>);
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
const activeResourceSubtab = computed<ServerResourceSubtab>({
  get: () => resourceSubtab.value,
  set: (value) => selectResourceSubtab(value),
});
const tabItems = computed(() => serverDetailTabs.map((tab) => ({ value: tab, label: t(`serversPage.tab.${tab}`) })));
const resourceSubtabItems = computed(() => serverResourceSubtabs.map((tab) => ({ value: tab, label: t(`serversPage.tab.${tab}`) })));
const detailTitle = computed(() => selectedServer.value?.name ?? t('common.loading'));
const detailDescription = computed(() => selectedServer.value ? `${selectedServer.value.host}:${selectedServer.value.port} / ${selectedServer.value.os?.prettyName || t('common.notAvailable')}` : '');

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
  { key: 'address', label: t('serversPage.columns.address') },
  { key: 'status', label: t('common.status'), nowrap: true },
  { key: 'lastChecked', label: t('serversPage.lastChecked'), nowrap: true },
  { key: 'actions', label: t('common.actions'), align: 'right' as const, nowrap: true },
]);

function statusText(server: ServerDto) {
  return t(serverSummaryStatusKey(server));
}

function detailSubtabQuery() {
  if (selectedTab.value === 'resources') {
    return resourceSubtab.value === 'containers' ? undefined : resourceSubtab.value;
  }
  if (selectedTab.value === 'firewall' && firewallView.value === 'fail2ban') return 'fail2ban';
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
    resourceSubtab.value = 'containers';
    firewallView.value = 'ufw';
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
  const expectedTab = value === 'overview' ? undefined : value;
  const expectedSubtab = detailSubtabQuery();
  if (selectedId.value && (route.query.tab !== expectedTab || route.query.subtab !== expectedSubtab)) {
    void router.replace({ query: detailQuery() });
  }
}

function selectResourceSubtab(value: ServerResourceSubtab) {
  resourceSubtab.value = value;
  visitedResources[value] = true;
  if (selectedId.value && route.query.subtab !== (value === 'containers' ? undefined : value)) {
    void router.replace({ query: detailQuery() });
  }
}

function selectFirewallView(value: FirewallView) {
  firewallView.value = value;
  if (selectedId.value && route.query.subtab !== (value === 'fail2ban' ? 'fail2ban' : undefined)) void router.replace({ query: detailQuery() });
}

function resetVisited() {
  serverDetailTabs.forEach((tab) => { visited[tab] = false; });
  serverResourceSubtabs.forEach((tab) => { visitedResources[tab] = false; });
  visited[selectedTab.value] = true;
  if (selectedTab.value === 'resources') visitedResources[resourceSubtab.value] = true;
}

watch(activeTab, (tab) => {
  visited[tab] = true;
  if (tab === 'resources') visitedResources[resourceSubtab.value] = true;
});
watch(resourceSubtab, (subtab) => { visitedResources[subtab] = true; });
watch(selectedId, () => { resetVisited(); }, { immediate: true });

watch(() => route.query.server, (value) => {
  const next = String(value ?? '');
  if (next !== selectedId.value) selectedId.value = next;
});
watch(() => route.query.tab, (value) => {
  const next = normalizeServerDetailTab(value);
  if (next !== selectedTab.value) selectedTab.value = next;
  if (isLegacyResourceTab(value)) {
    const nextSub = value;
    if (nextSub !== resourceSubtab.value) resourceSubtab.value = nextSub;
  }
});
watch(() => route.query.subtab, (value) => {
  if (selectedTab.value === 'resources' || isLegacyResourceTab(route.query.tab)) {
    const next = normalizeServerResourceSubtab(value, route.query.tab);
    if (next !== resourceSubtab.value) resourceSubtab.value = next;
    return;
  }
  const next = isDev ? normalizeFirewallView(value) : 'ufw';
  if (next !== firewallView.value) firewallView.value = next;
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
      notifyError(message, err);
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
      notifyError(serversResult.reason instanceof Error ? serversResult.reason.message : t('serversPage.loadFailed'));
    }
    if (credentialsResult.status === 'fulfilled') {
      credentials.value = credentialsResult.value;
    } else {
      credentials.value = [];
      credentialError.value = credentialsResult.reason instanceof Error ? credentialsResult.reason.message : t('serversPage.credentialsLoadFailed');
      notifyError(credentialsResult.reason instanceof Error ? credentialsResult.reason.message : t('serversPage.credentialsLoadFailed'));
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
    notifyError(err instanceof Error ? err.message : t('serversPage.loadFailed'), err);
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
    notifyError(err instanceof Error ? err.message : t('serversPage.credentialsLoadFailed'), err);
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

async function applyTailscale() {
  const server = selectedServer.value;
  if (!server?.tailscaleEnabled || tailscaleApplying.value) return;
  tailscaleApplying.value = true;
  try {
    const accepted = await serversApi.applyTailscale(server.id);
    notifySuccess(t('serversPage.tailscale.applyAccepted', { taskId: accepted.taskId }), accepted);
    refreshDetail();
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    tailscaleApplying.value = false;
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
        <template #actions>
          <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
          <Button size="sm" variant="primary" @click="openCreate"><Plus />{{ t('serversPage.addServer') }}</Button>
        </template>

        <ListPage>
          <template #toolbar>
            <SearchInput v-model="search" clearable :placeholder="t('serversPage.searchPlaceholder')" :label="t('common.search')" :clear-label="t('common.clearSearch')" />
          </template>

          <EmptyState v-if="error && !servers.length" variant="page" :title="t('common.loadFailed')" :description="error">
            <template #actions>
              <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button>
            </template>
          </EmptyState>

          <Table
            v-else
            class="h-full max-lg:max-h-[60dvh]"
            :columns="columns"
            :rows="servers"
            row-key="id"
            :active-key="selectedId"
            :loading="loading && !servers.length"
            :loading-label="t('common.loading')"
          >
            <template #name="{ row }">
              <TableDetailName class="max-w-[14rem]" :name="row.name" :label="t('common.openDetails', { name: row.name })" @click="openDetail(row)" />
            </template>
            <template #address="{ row }">
              <Tooltip :text="`${row.host}:${row.port}`" trigger-class="max-w-[16rem]">
                <span class="panel-mono truncate text-xs text-muted-foreground">{{ row.host }}:{{ row.port }}</span>
              </Tooltip>
            </template>
            <template #status="{ row }">
              <Badge :tone="serverSummaryTone(row)">{{ statusText(row) }}</Badge>
            </template>
            <template #lastChecked="{ row }">
              <span class="text-muted-foreground">{{ formatDateTime(row.lastCheckedAt, t('common.never')) }}</span>
            </template>
            <template #actions="{ row }">
              <div class="flex items-center justify-end gap-1">
                <Button size="sm" variant="secondary" @click="openDetail(row)"><Eye />{{ t('common.details') }}</Button>
                <Button size="sm" variant="ghost" :loading="rowBusy?.id === row.id && rowBusy.kind === 'test'" :disabled="rowBusy?.id === row.id" @click="testConnection(row)"><Cable />{{ t('serversPage.testConnection') }}</Button>
                <Dropdown align="right">
                  <template #trigger>
                    <IconButton size="sm" :label="t('common.moreActions')"><MoreHorizontal /></IconButton>
                  </template>
                  <DropdownItem :disabled="rowBusy?.id === row.id" @click="openEdit(row)"><Wrench />{{ t('common.edit') }}</DropdownItem>
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
      <ConsolePage :title="detailTitle" :description="detailDescription" :back-label="t('common.backToList')" :body-scroll="false" @back="closeDetail">
        <template v-if="selectedServer && activeTab === 'tailscale'" #actions>
          <Button size="sm" variant="primary" :disabled="!selectedServer.tailscaleEnabled" :loading="tailscaleApplying" @click="applyTailscale"><RefreshCcw />{{ t('serversPage.tailscale.apply') }}</Button>
        </template>
        <div ref="detailRoot" data-testid="server-detail" class="h-full min-h-0" tabindex="-1" @keydown="onDetailKeydown">
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
              <MetricsTab v-if="visited.metrics" v-show="activeTab === 'metrics'" :active="activeTab === 'metrics'" :server="selectedServer" />
              <AgentTab v-if="visited.agent" v-show="activeTab === 'agent'" :active="activeTab === 'agent'" :server="selectedServer" />
              <TailscaleTab v-if="visited.tailscale" v-show="activeTab === 'tailscale'" :server="selectedServer" />
              <div v-if="visited.resources" v-show="activeTab === 'resources'" class="h-full min-h-0">
                <Tabs v-model="activeResourceSubtab" class="h-full min-h-0" :tabs="resourceSubtabItems" keep-panels>
                  <ContainersTab v-if="visitedResources.containers" v-show="activeResourceSubtab === 'containers'" :server="selectedServer" />
                  <ImagesTab v-if="visitedResources.images" v-show="activeResourceSubtab === 'images'" :server="selectedServer" />
                  <NetworksTab v-if="visitedResources.networks" v-show="activeResourceSubtab === 'networks'" :server="selectedServer" />
                  <VolumesTab v-if="visitedResources.volumes" v-show="activeResourceSubtab === 'volumes'" :server="selectedServer" />
                </Tabs>
              </div>
              <PackagesTab v-if="visited.packages" v-show="activeTab === 'packages'" :server="selectedServer" />
              <FirewallTab v-if="visited.firewall" v-show="activeTab === 'firewall'" :server="selectedServer" :view="firewallView" @update:view="selectFirewallView" />
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
</template>
