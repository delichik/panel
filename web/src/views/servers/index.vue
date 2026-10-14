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
  agentStatusKey,
  agentTone,
  normalizeFirewallView,
  normalizeServerDetailTab,
  privilegeStatusKey,
  serverDetailTabs,
  serverReachabilityTone,
  tailscaleStatusKey,
  tailscaleTone,
  type FirewallView,
  type ServerDetailTab,
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
const detailRoot = ref<HTMLElement | null>(null);

const visited = reactive<Record<ServerDetailTab, boolean>>(Object.fromEntries(serverDetailTabs.map((tab) => [tab, false])) as Record<ServerDetailTab, boolean>);
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
const tabItems = computed(() => serverDetailTabs.map((tab) => ({ value: tab, label: t(`serversPage.tab.${tab}`) })));
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
  { key: 'agent', label: t('serversPage.agent'), nowrap: true },
  { key: 'privilege', label: t('serversPage.columns.privilege'), nowrap: true },
  { key: 'tailscale', label: t('serversPage.tailscale.title'), nowrap: true },
  { key: 'lastChecked', label: t('serversPage.lastChecked'), nowrap: true },
  { key: 'actions', label: t('common.actions'), align: 'right' as const, nowrap: true },
]);

function statusText(server: ServerDto) {
  return server.reachable ? t('serversPage.reachable') : t('serversPage.unreachable');
}

function agentText(server: ServerDto) {
  return t(agentStatusKey(server));
}

function privilegeText(server: ServerDto) {
  return t(privilegeStatusKey(server));
}

function detailQuery(serverId = selectedId.value) {
  return {
    ...route.query,
    server: serverId || undefined,
    tab: selectedTab.value === 'overview' ? undefined : selectedTab.value,
    subtab: selectedTab.value === 'firewall' && firewallView.value === 'fail2ban' ? 'fail2ban' : undefined,
  };
}

function openDetail(server: ServerDto) {
  if (selectedId.value !== server.id) {
    selectedId.value = server.id;
    selectedTab.value = 'overview';
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
  if (selectedId.value && route.query.tab !== (value === 'overview' ? undefined : value)) void router.replace({ query: detailQuery() });
}

function selectFirewallView(value: FirewallView) {
  firewallView.value = value;
  if (selectedId.value && route.query.subtab !== (value === 'fail2ban' ? 'fail2ban' : undefined)) void router.replace({ query: detailQuery() });
}

function resetVisited() {
  serverDetailTabs.forEach((tab) => { visited[tab] = false; });
  visited[selectedTab.value] = true;
}

watch(activeTab, (tab) => { visited[tab] = true; });
watch(selectedId, () => { resetVisited(); }, { immediate: true });

watch(() => route.query.server, (value) => {
  const next = String(value ?? '');
  if (next !== selectedId.value) selectedId.value = next;
});
watch(() => route.query.tab, (value) => {
  const next = normalizeServerDetailTab(value);
  if (next !== selectedTab.value) selectedTab.value = next;
});
watch(() => route.query.subtab, (value) => {
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
              <strong class="block max-w-[14rem] truncate text-foreground">{{ row.name }}</strong>
            </template>
            <template #address="{ row }">
              <Tooltip :text="`${row.host}:${row.port}`" trigger-class="max-w-[16rem]">
                <span class="panel-mono truncate text-xs text-muted-foreground">{{ row.host }}:{{ row.port }}</span>
              </Tooltip>
            </template>
            <template #status="{ row }">
              <Badge :tone="serverReachabilityTone(row)">{{ statusText(row) }}</Badge>
            </template>
            <template #agent="{ row }">
              <Badge :tone="agentTone(row)">{{ agentText(row) }}</Badge>
            </template>
            <template #privilege="{ row }">
              <Badge :tone="row.privilege?.privileged || row.sudo?.passwordless ? 'success' : 'warning'">{{ privilegeText(row) }}</Badge>
            </template>
            <template #tailscale="{ row }">
              <Badge v-if="row.tailscaleEnabled" :tone="tailscaleTone(row)">{{ t(tailscaleStatusKey(row)) }}</Badge>
              <span v-else class="text-muted-foreground">{{ t('common.notAvailable') }}</span>
            </template>
            <template #lastChecked="{ row }">
              <span class="text-muted-foreground">{{ formatDateTime(row.lastCheckedAt, t('common.never')) }}</span>
            </template>
            <template #actions="{ row }">
              <div class="flex items-center justify-end gap-1">
                <Button size="sm" variant="secondary" @click="openDetail(row)"><Eye />{{ t('common.details') }}</Button>
                <Button size="sm" variant="ghost" :loading="rowBusy?.id === row.id && rowBusy.kind === 'edit'" :disabled="rowBusy?.id === row.id" @click="openEdit(row)"><Wrench />{{ t('common.edit') }}</Button>
                <Button size="sm" variant="ghost" :loading="rowBusy?.id === row.id && rowBusy.kind === 'test'" :disabled="rowBusy?.id === row.id" @click="testConnection(row)"><Cable />{{ t('serversPage.testConnection') }}</Button>
                <Dropdown align="right">
                  <template #trigger>
                    <IconButton size="sm" :label="t('common.moreActions')"><MoreHorizontal /></IconButton>
                  </template>
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
        <div ref="detailRoot" data-testid="server-detail" class="h-full min-h-0" tabindex="-1" @keydown="onDetailKeydown">
          <EmptyState v-if="detailError && !serverDetails[selectedId]" variant="page" :title="t('common.loadFailed')" :description="detailError">
            <template #actions>
              <Button size="sm" :loading="detailLoading" @click="invalidateServerDetail()"><RefreshCcw />{{ t('common.retry') }}</Button>
            </template>
          </EmptyState>
          <div v-else-if="!selectedServer" class="grid content-start gap-4">
            <Skeleton class="h-7 w-48" />
            <Skeleton class="h-4 w-72 max-w-full" />
            <div class="mt-4 grid gap-3 xl:grid-cols-2">
              <Skeleton v-for="item in 4" :key="item" class="h-40" />
            </div>
          </div>
          <Tabs v-else v-model="activeTab" class="h-full min-h-0" :tabs="tabItems" keep-panels>
            <div :key="selectedId" class="h-full min-h-0">
              <OverviewTab v-if="visited.overview" v-show="activeTab === 'overview'" :active="activeTab === 'overview'" :server="selectedServer" :credentials="credentials" @refresh="refreshDetail" />
              <MetricsTab v-if="visited.metrics" v-show="activeTab === 'metrics'" :active="activeTab === 'metrics'" :server="selectedServer" />
              <AgentTab v-if="visited.agent" v-show="activeTab === 'agent'" :active="activeTab === 'agent'" :server="selectedServer" />
              <TailscaleTab v-if="visited.tailscale" v-show="activeTab === 'tailscale'" :server="selectedServer" @refresh="refreshDetail" />
              <ContainersTab v-if="visited.containers" v-show="activeTab === 'containers'" :server="selectedServer" />
              <ImagesTab v-if="visited.images" v-show="activeTab === 'images'" :server="selectedServer" />
              <NetworksTab v-if="visited.networks" v-show="activeTab === 'networks'" :server="selectedServer" />
              <VolumesTab v-if="visited.volumes" v-show="activeTab === 'volumes'" :server="selectedServer" />
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
