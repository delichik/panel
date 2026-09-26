<script setup lang="ts">
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { AlertTriangle, Cable, KeyRound, Pencil, PlayCircle, Plus, RefreshCcw, ServerCog, ShieldPlus, Trash2, Wrench } from '@lucide/vue';
import { credentialsApi } from '@/api/credentials';
import { serversApi, type ServerMetricsRange, type ServerMetricsSeries } from '@/api/servers';
import { tasksApi } from '@/api/tasks';
import { activityApi } from '@/api/activity';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue';
import Dialog from '@/components/ui/Dialog.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import Input from '@/components/ui/Input.vue';
import PaginationBar from '@/components/ui/PaginationBar.vue';
import SearchInput from '@/components/ui/SearchInput.vue';
import Select from '@/components/ui/Select.vue';
import Skeleton from '@/components/ui/Skeleton.vue';
import StatusBadge from '@/components/ui/StatusBadge.vue';
import Textarea from '@/components/ui/Textarea.vue';
import LoadingOverlay from '@/components/ui/LoadingOverlay.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import { useCompactViewport } from '@/composables/useCompactViewport';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import AutoRefreshControl from '@/components/patterns/AutoRefreshControl.vue';
import MasterDetailLayout from '@/components/templates/MasterDetailLayout.vue';
import { useAutoRefresh } from '@/composables/useAutoRefresh';
import { useI18n } from '@/i18n';
import type { CredentialDto } from '@/types/credentials';
import type { ServerDto, NatPortConfig, NatPortMapping } from '@/types/servers';
import type { TaskDto, TaskLog } from '@/types/tasks';
import { agentTone, canInstallUfw, canRunPrivilegedOperation, credentialLabel, serverReachabilityTone } from './model';
import ServerFormDialog from './ServerFormDialog.vue';
import { createLatestRequestGuard } from '@/views/_shared/requestState';
import { formatDateTime } from '@/utils/datetime';

const { t } = useI18n();
const MetricLineChart = defineAsyncComponent(() => import('@/views/overview/MetricLineChart.vue'));
const route = useRoute();
const router = useRouter();
const notifyError = useErrorToast();
const notifySuccess = useSuccessToast();
const { mode: autoRefreshMode, enabled: autoRefreshEnabled, intervalMs: autoRefreshIntervalMs } = useAutoRefresh();

const servers = ref<ServerDto[]>([]);
const serverDetails = ref<Record<string, ServerDto>>({});
const credentials = ref<CredentialDto[]>([]);
const selectedId = ref('');
const search = ref(String(route.query.search ?? ''));
const page = ref(1);
const pageSize = 50;
const totalServers = ref(0);
let searchTimer: ReturnType<typeof setTimeout> | null = null;
let detailController: AbortController | null = null;
let metricsController: AbortController | null = null;
let detailRequestId = 0;
let metricsRequestId = 0;
let metricsInFlight = false;
let metricsAutoRefreshTimer: number | undefined;
let agentTaskPollTimer: number | undefined;
let agentTaskInFlight = false;
let initialTaskPollTimer: number | undefined;
let initialTaskInFlight = false;
const listRequests = createLatestRequestGuard();
const agentTaskRequests = createLatestRequestGuard();
const initialTaskRequests = createLatestRequestGuard();
const loading = ref(false);
const detailLoading = ref(false);
const error = ref('');
const credentialError = ref('');
const actionError = ref('');
const serverDialog = ref(false);
const confirmDialog = ref(false);
const testing = ref(false);
const openingEdit = ref(false);
const editing = ref<ServerDto | null>(null);
const confirmTarget = ref<ServerDto | null>(null);
const confirmOperation = ref<'restart' | 'ufw' | 'trustHostKey' | null>(null);
const pendingOperation = ref('');
const metrics = ref<ServerMetricsSeries | null>(null);
const metricsServerId = ref('');
const metricsLoading = ref(false);
const metricsError = ref('');
const metricsRange = ref<ServerMetricsRange>('1h');
const agentTask = ref<TaskDto | null>(null);
const agentTaskLogs = ref<TaskLog[]>([]);
const agentTaskLogCursor = ref(0);
const agentTaskServerId = ref('');
const agentTaskLoading = ref(false);
const agentTaskError = ref('');
const initialTask = ref<TaskDto | null>(null);
const initialTaskServerId = ref('');
const initialTaskLoading = ref(false);
const initialTaskError = ref('');
const metricsRangeOptions = [
  { value: '1h', label: '1h' },
  { value: '6h', label: '6h' },
  { value: '1d', label: '1d' },
  { value: '7d', label: '7d' },
];
const natConfig = ref<NatPortConfig | null>(null);
const natLoading = ref(false);
const natError = ref('');
const natDialogOpen = ref(false);
const natEditingId = ref('');
const natForm = reactive({ appId: '', hostPort: '', publicPort: '', protocol: 'tcp', label: '', notes: '' });
const natFormError = ref('');
const natSaving = ref(false);
let natRequestId = 0;
const natProtocolOptions = [
  { value: 'tcp', label: 'tcp' },
  { value: 'udp', label: 'udp' },
];


const selectedServer = computed(() => serverDetails.value[selectedId.value] ?? servers.value.find((item) => item.id === selectedId.value) ?? null);
/** 紧凑视口（xl 以下）不自动选中首条服务器：窄屏应停在列表，由用户进入详情。 */
const compactViewport = useCompactViewport();

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
const visibleAgentTaskLogs = computed(() => agentTaskLogs.value.slice(-20));
const agentTaskActive = computed(() => isActiveTask(agentTask.value));
const initialTaskActive = computed(() => isActiveTask(initialTask.value));

const metricChartPanels = computed(() => {
  const series = metrics.value;
  if (!series) return [];
  const serverName = selectedServer.value?.name ?? '';
  const single = (values: number[]) => [{ id: selectedId.value, name: serverName, values }];
  const percentValues = (points: Array<{ usedBytes?: number; totalBytes?: number }>) => points.map((point) => (point.totalBytes ? ((point.usedBytes ?? 0) / point.totalBytes) * 100 : 0));
  return [
    { key: 'cpu', label: 'CPU', current: percent(latestMetrics.value.cpu?.usagePercent), valueKind: 'percent' as const, labels: series.cpu.map((point) => point.time), series: single(series.cpu.map((point) => point.usagePercent)) },
    { key: 'memory', label: t('serversPage.memory'), current: `${bytes(latestMetrics.value.memory?.usedBytes)} / ${bytes(latestMetrics.value.memory?.totalBytes)}`, valueKind: 'percent' as const, labels: series.memory.map((point) => point.time), series: single(percentValues(series.memory)) },
    { key: 'disk', label: t('serversPage.disk'), current: `${bytes(latestMetrics.value.disk?.usedBytes)} / ${bytes(latestMetrics.value.disk?.totalBytes)}`, valueKind: 'percent' as const, labels: series.disk.map((point) => point.time), series: single(percentValues(series.disk)) },
    { key: 'network', label: t('serversPage.network'), current: `${bytesPerSecond(latestMetrics.value.network?.rxBytesPerSecond)} / ${bytesPerSecond(latestMetrics.value.network?.txBytesPerSecond)}`, valueKind: 'bytes' as const, labels: series.network.map((point) => point.time), series: [
      { id: `${selectedId.value}-rx`, name: t('serversPage.networkRx'), values: series.network.map((point) => point.rxBytesPerSecond ?? 0) },
      { id: `${selectedId.value}-tx`, name: t('serversPage.networkTx'), values: series.network.map((point) => point.txBytesPerSecond ?? 0) },
    ] },
  ];
});

watch(search, (value) => {
  void router.replace({ query: { ...route.query, search: value || undefined } });
  if (searchTimer) clearTimeout(searchTimer);
  searchTimer = setTimeout(() => {
    if (page.value !== 1) page.value = 1;
    else void loadServers();
  }, 250);
});
watch(page, () => { void loadServers(); });
watch(selectedId, () => {
  void loadServerDetail();
  void loadMetrics(true);
  void loadAgentDeployment(true);
  void loadInitialTask(true);
  void loadNatConfig();
});

/** 窄屏单视图：返回列表并清掉 URL 里的选中项。 */
function backToList() {
  selectedId.value = '';
  void router.replace({ query: { ...route.query, server: undefined } });
}
watch(metricsRange, () => {
  void loadMetrics(true);
});

async function loadNatConfig() {
  const id = selectedId.value;
  const requestId = ++natRequestId;
  natConfig.value = null;
  natError.value = '';
  if (!id || selectedServer.value?.kind !== 'nat') return;
  natLoading.value = true;
  try {
    const cfg = await serversApi.natPorts(id);
    if (requestId !== natRequestId || selectedId.value !== id) return;
    natConfig.value = cfg;
  } catch (err) {
    if (isAbortError(err)) return;
    natError.value = err instanceof Error ? err.message : t('serversPage.natLoadFailed');
  } finally {
    if (requestId === natRequestId) natLoading.value = false;
  }
}

function openNatAdd() {
  natEditingId.value = '';
  Object.assign(natForm, { appId: '', hostPort: '', publicPort: '', protocol: 'tcp', label: '', notes: '' });
  natFormError.value = '';
  natDialogOpen.value = true;
}

function openNatEdit(mapping: NatPortMapping) {
  natEditingId.value = mapping.id;
  Object.assign(natForm, { appId: mapping.appId, hostPort: String(mapping.hostPort), publicPort: String(mapping.publicPort), protocol: mapping.protocol, label: mapping.label, notes: mapping.notes });
  natFormError.value = '';
  natDialogOpen.value = true;
}

function validateNatForm(): boolean {
  const hostPort = Number(natForm.hostPort);
  const publicPort = Number(natForm.publicPort);
  if (!Number.isInteger(hostPort) || hostPort < 1 || hostPort > 65535) { natFormError.value = t('serversPage.natValidationHostPort'); return false; }
  if (!Number.isInteger(publicPort) || publicPort < 1 || publicPort > 65535) { natFormError.value = t('serversPage.natValidationPublicPort'); return false; }
  return true;
}

async function saveNatPort() {
  if (!validateNatForm()) return;
  const id = selectedId.value;
  if (!id) return;
  natSaving.value = true;
  natFormError.value = '';
  const payload = {
    appId: natForm.appId.trim(),
    hostPort: Number(natForm.hostPort),
    publicPort: Number(natForm.publicPort),
    protocol: natForm.protocol,
    label: natForm.label.trim(),
    notes: natForm.notes.trim(),
  };
  try {
    if (natEditingId.value) {
      await serversApi.updateNatPort(id, natEditingId.value, payload);
      notifySuccess(t('serversPage.natUpdated'));
    } else {
      await serversApi.addNatPort(id, payload);
      notifySuccess(t('serversPage.natAdded'));
    }
    natDialogOpen.value = false;
    void loadNatConfig();
  } catch (err) {
    const message = err instanceof Error ? err.message : t('serversPage.natSaveFailed');
    natFormError.value = natErrorMessage(message);
    notifyError(message, err);
  } finally {
    natSaving.value = false;
  }
}

async function deleteNatPort(mapping: NatPortMapping) {
  const id = selectedId.value;
  if (!id) return;
  try {
    await serversApi.deleteNatPort(id, mapping.id);
    notifySuccess(t('serversPage.natDeleted'));
    void loadNatConfig();
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('serversPage.natSaveFailed'), err);
  }
}

function natErrorMessage(message: string): string {
  if (message.includes('Public port is already mapped')) return t('serversPage.natPublicConflict');
  if (message.includes('Host port is already mapped')) return t('serversPage.natHostConflict');
  return message;
}

async function loadServerDetail() {
  const id = selectedId.value;
  detailController?.abort();
  const requestId = ++detailRequestId;
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
  } catch (err) {
    if (isAbortError(err)) return;
    actionError.value = err instanceof Error ? err.message : t('serversPage.loadFailed');
    notifyError(err instanceof Error ? err.message : t('serversPage.loadFailed'), err);
  } finally {
    if (requestId === detailRequestId) detailLoading.value = false;
  }
}

async function load() {
  const requestId = listRequests.begin();
  loading.value = true;
  error.value = '';
  credentialError.value = '';
  try {
    const [serversResult, credentialsResult] = await Promise.allSettled([serversApi.listPage({ page: page.value, pageSize, q: search.value.trim() || undefined }), credentialsApi.list()]);
    if (!listRequests.isCurrent(requestId)) return;
    if (serversResult.status === 'fulfilled') {
      const nextServers = serversResult.value.items;
      totalServers.value = serversResult.value.total;
      servers.value = nextServers;
      const queryServer = String(route.query.server ?? '');
      selectedId.value = nextServers.some((item) => item.id === queryServer)
        ? queryServer
        : nextServers.some((item) => item.id === selectedId.value)
          ? selectedId.value
          : (compactViewport.value ? '' : nextServers[0]?.id || '');
    } else {
      error.value = serversResult.reason instanceof Error ? serversResult.reason.message : t('serversPage.loadFailed');
      notifyError(serversResult.reason instanceof Error ? serversResult.reason.message : t('serversPage.loadFailed'));
    }
    const detailId = selectedId.value;
    if (detailId && serverDetails.value[detailId]) {
      const nextDetails = { ...serverDetails.value };
      delete nextDetails[detailId];
      serverDetails.value = nextDetails;
      void loadServerDetail();
    }
    if (credentialsResult.status === 'fulfilled') {
      credentials.value = credentialsResult.value;
    } else {
      credentials.value = [];
      credentialError.value = credentialsResult.reason instanceof Error ? credentialsResult.reason.message : t('serversPage.credentialsLoadFailed');
      notifyError(credentialsResult.reason instanceof Error ? credentialsResult.reason.message : t('serversPage.credentialsLoadFailed'));
    }
  } finally {
    if (listRequests.isCurrent(requestId)) loading.value = false;
  }
}

async function loadServers() {
  const requestId = listRequests.begin();
  loading.value = true;
  error.value = '';
  try {
    const result = await serversApi.listPage({ page: page.value, pageSize, q: search.value.trim() || undefined });
    if (!listRequests.isCurrent(requestId)) return;
    servers.value = result.items;
    totalServers.value = result.total;
    selectedId.value = result.items.some((item) => item.id === selectedId.value) ? selectedId.value : result.items[0]?.id ?? '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('serversPage.loadFailed');
    notifyError(err instanceof Error ? err.message : t('serversPage.loadFailed'), err);
  } finally {
    if (listRequests.isCurrent(requestId)) loading.value = false;
  }
}

async function loadMetrics(force = false) {
  // 自动轮询期间已有在途请求时跳过本轮，避免自伤 abort；仅切换范围/服务器或手动刷新时强制中止旧请求。
  if (!force && metricsInFlight) return;
  if (force) metricsController?.abort();
  const requestId = ++metricsRequestId;
  const id = selectedId.value;
  metricsError.value = '';
  // 切换服务器时立即清空旧数据；同服务器刷新保留旧数据，避免闪跳
  if (metricsServerId.value !== id) {
    metrics.value = null;
    metricsServerId.value = '';
  }
  if (!id) {
    metricsLoading.value = false;
    metricsInFlight = false;
    return;
  }
  const controller = new AbortController();
  metricsController = controller;
  metricsLoading.value = true;
  metricsInFlight = true;
  try {
    const next = await serversApi.metrics(id, metricsRange.value, { signal: controller.signal });
    if (requestId !== metricsRequestId || selectedId.value !== id) return;
    metrics.value = next;
    metricsServerId.value = id;
  } catch (err) {
    if (isAbortError(err)) return;
    metricsError.value = err instanceof Error ? err.message : t('serversPage.metricsFailed');
    notifyError(err instanceof Error ? err.message : t('serversPage.metricsFailed'), err);
  } finally {
    if (requestId === metricsRequestId) {
      metricsLoading.value = false;
      metricsInFlight = false;
    }
  }
}

function startMetricsAutoRefresh() {
  stopMetricsAutoRefresh();
  if (!autoRefreshEnabled.value) return;
  metricsAutoRefreshTimer = window.setInterval(() => {
    if (document.visibilityState === 'visible') void loadMetrics();
  }, autoRefreshIntervalMs.value);
}

function stopMetricsAutoRefresh() {
  if (metricsAutoRefreshTimer !== undefined) {
    window.clearInterval(metricsAutoRefreshTimer);
    metricsAutoRefreshTimer = undefined;
  }
}

watch(autoRefreshMode, startMetricsAutoRefresh, { immediate: true });

function openCreate() {
  editing.value = null;
  serverDialog.value = true;
}

async function openEdit(server: ServerDto) {
  openingEdit.value = true;
  try {
    const detail = serverDetails.value[server.id] ?? await serversApi.get(server.id);
    serverDetails.value = { ...serverDetails.value, [server.id]: detail };
    editing.value = detail;
    serverDialog.value = true;
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('serversPage.loadFailed'), err);
  } finally {
    openingEdit.value = false;
  }
}

/** 创建/更新成功后的两阶段衔接：创建带 initialTaskId 时立即跟踪初始化任务。 */
async function handleSaved(saved: ServerDto) {
  selectedId.value = saved.id;
  invalidateServerDetail(saved.id);
  if (!editing.value && saved.initialTaskId) await loadInitialTask(true, saved.initialTaskId);
  await load();
}

async function loadInitialTask(reset = false, preferredTaskId = '') {
  if (reset && initialTaskInFlight) initialTaskRequests.invalidate();
  else if (!reset && initialTaskInFlight) return;
  const serverId = selectedId.value;
  if (!serverId) {
    clearInitialTask();
    return;
  }
  const requestId = initialTaskRequests.begin();
  initialTaskInFlight = true;
  initialTaskLoading.value = true;
  initialTaskError.value = '';
  if (reset || initialTaskServerId.value !== serverId) initialTaskServerId.value = serverId;
  try {
    const detail = serverDetails.value[serverId] ?? servers.value.find((item) => item.id === serverId);
    let taskId = preferredTaskId || detail?.initialTaskId || '';
    if (!taskId) {
      const result = await tasksApi.list({ serverId, type: 'server_info_collect', page: 1, pageSize: 1 });
      if (!initialTaskRequests.isCurrent(requestId) || selectedId.value !== serverId) return;
      taskId = result.items[0]?.id ?? '';
    }
    if (!taskId) {
      initialTask.value = null;
      initialTaskServerId.value = serverId;
      return;
    }
    const nextTask = await tasksApi.get(taskId);
    if (!initialTaskRequests.isCurrent(requestId) || selectedId.value !== serverId) return;
    const previous = initialTask.value;
    initialTask.value = nextTask;
    initialTaskServerId.value = serverId;
    // 任务进入终态（完成/失败）时刷新详情；服务器记录始终保留，失败态由后端标记
    // 到 reachable/last_error，详情侧栏据此展示登记失败与原因，供用户重试、编辑或删除。
    if (previous && isActiveTask(previous) && !isActiveTask(nextTask)) {
      invalidateServerDetail(serverId);
    }
  } catch (err) {
    if (!initialTaskRequests.isCurrent(requestId) || selectedId.value !== serverId) return;
    initialTaskError.value = err instanceof Error ? err.message : t('serversPage.initialTaskLoadFailed');
  } finally {
    if (initialTaskRequests.isCurrent(requestId)) {
      initialTaskInFlight = false;
      initialTaskLoading.value = false;
    }
  }
}

/** 供创建弹窗内的凭据快捷创建/重试使用，只刷新凭据列表。 */
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

function startInitialTaskPolling() {
  window.clearInterval(initialTaskPollTimer);
  initialTaskPollTimer = window.setInterval(() => {
    if (document.visibilityState === 'visible' && initialTaskActive.value) void loadInitialTask();
  }, 2000);
}

function clearInitialTask() {
  initialTaskRequests.invalidate();
  initialTask.value = null;
  initialTaskServerId.value = '';
  initialTaskError.value = '';
  initialTaskLoading.value = false;
  initialTaskInFlight = false;
}

async function testConnection(server: ServerDto) {
  testing.value = true;
  try {
    await runInline(async () => {
      const tested = await serversApi.test(server.id);
      invalidateServerDetail(tested.id);
      notifySuccess(t('serversPage.testSucceeded', { name: tested.name }), tested);
      await load();
    });
  } finally {
    testing.value = false;
  }
}

function confirmDelete(server: ServerDto) {
  confirmTarget.value = server;
  confirmDialog.value = true;
}

async function deleteSelected() {
  const target = confirmTarget.value;
  if (!target) return;
  await runInline(async () => {
    const result = await serversApi.delete(target.id);
    const nextDetails = { ...serverDetails.value };
    delete nextDetails[target.id];
    serverDetails.value = nextDetails;
    notifySuccess(t('serversPage.deleted', { name: target.name }), result);
    confirmDialog.value = false;
    selectedId.value = '';
    await load();
  });
}

async function deployAgent(server: ServerDto) {
  await runInline(async () => {
    const accepted = await serversApi.deployAgent(server.id);
    notifySuccess(t('serversPage.agentTaskAccepted', { taskId: accepted.taskId }), accepted);
    await loadAgentDeployment(true, accepted.taskId);
  }, 'agent');
}

async function loadAgentDeployment(reset = false, preferredTaskId = '') {
  if (!reset && agentTaskInFlight) return;
  const serverId = selectedId.value;
  if (!serverId) {
    clearAgentDeployment();
    return;
  }
  const requestId = agentTaskRequests.begin();
  agentTaskInFlight = true;
  agentTaskLoading.value = true;
  agentTaskError.value = '';
  if (reset || agentTaskServerId.value !== serverId) {
    agentTask.value = null;
    agentTaskLogs.value = [];
    agentTaskLogCursor.value = 0;
    agentTaskServerId.value = serverId;
  }
  try {
    let nextTask: TaskDto | null;
    if (preferredTaskId) {
      nextTask = await tasksApi.get(preferredTaskId);
    } else if (agentTask.value?.id && agentTaskServerId.value === serverId) {
      nextTask = await tasksApi.get(agentTask.value.id);
    } else {
      const result = await tasksApi.list({ serverId, type: 'server_agent_deploy', page: 1, pageSize: 1 });
      nextTask = result.items[0] ?? null;
    }
    if (!agentTaskRequests.isCurrent(requestId) || selectedId.value !== serverId) return;
    if (!nextTask) {
      agentTask.value = null;
      agentTaskLogs.value = [];
      agentTaskLogCursor.value = 0;
      return;
    }
    const previousWasActive = isActiveTask(agentTask.value);
    const taskChanged = agentTask.value?.id !== nextTask.id;
    agentTask.value = nextTask;
    if (taskChanged) {
      agentTaskLogs.value = [];
      agentTaskLogCursor.value = 0;
    }
    const nextLogs = await activityApi.events({ executionId: nextTask.id, kind: 'output', limit: 20 });
    if (!agentTaskRequests.isCurrent(requestId) || selectedId.value !== serverId) return;
    agentTaskLogs.value = [...nextLogs.items].sort((left, right) => left.seq - right.seq).map(event => ({ cursor: event.seq, time: event.occurredAt, stream: event.stream || 'stdout', line: event.text || '' }));
    agentTaskLogCursor.value = nextLogs.headSeq;
    if (previousWasActive && !isActiveTask(nextTask)) invalidateServerDetail(serverId);
  } catch (err) {
    if (!agentTaskRequests.isCurrent(requestId) || selectedId.value !== serverId) return;
    agentTaskError.value = err instanceof Error ? err.message : t('serversPage.agentTaskLoadFailed');
  } finally {
    if (agentTaskRequests.isCurrent(requestId)) {
      agentTaskInFlight = false;
      agentTaskLoading.value = false;
    }
  }
}

function clearAgentDeployment() {
  agentTaskRequests.invalidate();
  agentTask.value = null;
  agentTaskLogs.value = [];
  agentTaskLogCursor.value = 0;
  agentTaskServerId.value = '';
  agentTaskError.value = '';
  agentTaskLoading.value = false;
  agentTaskInFlight = false;
}

function isActiveTask(task: TaskDto | null) {
  return Boolean(task && ['queued', 'scheduled', 'running', 'failed_retryable'].includes(task.status));
}

function agentTaskStageLabel(stage: string) {
  const key = ({
    preparing: 'serversPage.agentTaskStagePreparing',
    checking: 'serversPage.agentTaskStageChecking',
    uploading: 'serversPage.agentTaskStageUploading',
    configuring: 'serversPage.agentTaskStageConfiguring',
    starting: 'serversPage.agentTaskStageStarting',
    restarting: 'serversPage.agentTaskStageRestarting',
    collecting: 'serversPage.agentTaskStageCollecting',
    completed: 'serversPage.agentTaskStageCompleted',
  } as Record<string, string>)[stage];
  return key ? t(key) : stage || t('common.notAvailable');
}

function formatAgentTaskLog(line: string) {
  if (line === 'waiting for agent restart readiness') return t('serversPage.agentTaskLogWaitingReadiness');
  if (line === 'agent restart readiness confirmed (state=ready)') return t('serversPage.agentTaskLogReadinessConfirmed');
  const restartDelay = line.match(/^agent requested a restart delay \(state=holdon; (.+) elapsed\); the protocol did not provide a reason$/);
  if (restartDelay) return t('serversPage.agentTaskLogRestartDelay', { elapsed: restartDelay[1] });
  const maintenanceWait = line.match(/^panel agent is still running package maintenance; waiting to restart \((.+) elapsed\)$/);
  if (maintenanceWait) return t('serversPage.agentTaskLogRestartDelay', { elapsed: maintenanceWait[1] });
  const readinessTimeout = line.match(/^agent restart readiness wait timed out after (.+); deployment will continue$/);
  if (readinessTimeout) return t('serversPage.agentTaskLogReadinessTimeout', { timeout: readinessTimeout[1] });
  return line;
}

function startAgentTaskPolling() {
  window.clearInterval(agentTaskPollTimer);
  agentTaskPollTimer = window.setInterval(() => {
    if (document.visibilityState === 'visible' && Boolean(agentTask.value)) void loadAgentDeployment();
  }, 2000);
}

function invalidateServerDetail(id?: string) {
  const target = id || selectedId.value;
  if (!target) return;
  const next = { ...serverDetails.value };
  delete next[target];
  serverDetails.value = next;
  if (target === selectedId.value) void loadServerDetail();
}

function confirmRestart(server: ServerDto) {
  if (!canRunPrivilegedOperation(server)) return;
  confirmTarget.value = server;
  confirmOperation.value = 'restart';
}

function confirmInstallUfw(server: ServerDto) {
  if (!canInstallUfw(server)) return;
  confirmTarget.value = server;
  confirmOperation.value = 'ufw';
}
function confirmTrustHostKey(server: ServerDto) {
  confirmTarget.value = server;
  confirmOperation.value = 'trustHostKey';
}

async function runConfirmedOperation() {
  const server = confirmTarget.value;
  const operation = confirmOperation.value;
  if (!server || !operation) return;
  confirmOperation.value = null;
  if (operation === 'restart') {
    await runInline(async () => {
      const accepted = await serversApi.restart(server.id);
      notifySuccess(t('serversPage.restartAccepted', { taskId: accepted.taskId }), accepted);
    }, 'restart');
  } else if (operation === 'ufw') {
    await runInline(async () => {
      const accepted = await serversApi.installUfw(server.id);
      notifySuccess(t('serversPage.ufwAccepted', { taskId: accepted.taskId }), accepted);
    }, 'ufw');
  } else {
    await runInline(async () => {
      const trusted = await serversApi.trustHostKey(server.id);
      invalidateServerDetail(trusted.id);
      notifySuccess(t('serversPage.trustHostKeySucceeded', { name: trusted.name }), trusted);
      await load();
    }, 'trustHostKey');
  }
  confirmTarget.value = null;
}

async function runInline(action: () => Promise<void>, operation = 'default') {
  pendingOperation.value = operation;
  actionError.value = '';
  try {
    await action();
  } catch (err) {
    actionError.value = err instanceof Error ? err.message : t('common.operationFailed');
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
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
  if (typeof value !== 'number') return t('common.notAvailable');
  if (!value) return '0 B/s';
  const units = ['B/s', 'KB/s', 'MB/s', 'GB/s', 'TB/s'];
  let unitIndex = 0;
  let scaled = value;
  while (scaled >= 1024 && unitIndex < units.length - 1) {
    scaled /= 1024;
    unitIndex += 1;
  }
  return `${scaled >= 100 ? scaled.toFixed(0) : scaled.toFixed(1)} ${units[unitIndex]}`;
}

function isAbortError(error: unknown) {
  return Boolean(error && typeof error === 'object' && 'code' in error && (error as { code?: string }).code === 'request_aborted');
}

onMounted(async () => {
  await load();
  startAgentTaskPolling();
  startInitialTaskPolling();
});
onBeforeUnmount(() => {
  if (searchTimer) clearTimeout(searchTimer);
  detailController?.abort();
  metricsController?.abort();
  stopMetricsAutoRefresh();
  window.clearInterval(agentTaskPollTimer);
  window.clearInterval(initialTaskPollTimer);
  agentTaskRequests.invalidate();
  initialTaskRequests.invalidate();
});
</script>

<template>
  <ConsolePage :title="t('routes.servers.title')" :description="t('routes.servers.description')">
    <template #actions>
      <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
      <Button size="sm" variant="primary" @click="openCreate"><Plus />{{ t('serversPage.addServer') }}</Button>
    </template>

    <MasterDetailLayout class="h-full" :detail-key="selectedId" :has-detail="!!selectedServer" :back-label="t('common.backToList')" @back="backToList">
      <template #master>
      <aside class="grid min-h-0 min-w-0 grid-rows-[auto_minmax(0,1fr)_auto] rounded-2xl border border-border bg-card">
        <div class="border-b border-border p-4">
          <SearchInput v-model="search" clearable :placeholder="t('serversPage.searchPlaceholder')" :label="t('common.search')" :clear-label="t('common.clearSearch')" />
        </div>
        <div class="motion-stagger min-h-0 overflow-auto p-2 max-lg:max-h-[60dvh]">
          <div v-if="loading && !servers.length" class="grid gap-2">
            <Skeleton v-for="item in 6" :key="item" class="h-20" />
          </div>
          <EmptyState v-else-if="error && !servers.length" :title="t('common.loadFailed')" :description="error">
            <template #actions>
              <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button>
            </template>
          </EmptyState>
          <EmptyState v-else-if="!servers.length" :title="t('serversPage.noServers')" :description="t('serversPage.noServersHint')" />
          <button
            v-for="server in servers"
            v-else
            :key="server.id"
            type="button"
            class="motion-list-item mb-2 grid w-full gap-2 rounded-xl border p-3 text-left hover:bg-accent"
            :class="selectedId === server.id ? 'border-border-strong bg-muted' : 'border-transparent bg-transparent'"
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
        <PaginationBar v-model:page="page" class="px-3" :page-size="pageSize" :total="totalServers" :loading="loading" :previous-label="t('common.previous')" :next-label="t('common.next')" />
      </aside>
      </template>

      <template #detail>
      <main class="grid min-h-0 min-w-0">
        <article v-if="loading && !servers.length" class="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-2xl border border-border bg-card">
          <header class="border-b border-border p-5">
            <Skeleton class="h-7 w-48" />
            <Skeleton class="mt-3 h-4 w-72 max-w-full" />
          </header>
          <div class="min-h-0 overflow-auto p-5">
            <div class="grid gap-4 2xl:grid-cols-[minmax(0,1fr)_320px]">
              <div class="grid gap-4">
                <section v-for="item in 3" :key="item" class="rounded-2xl border border-border bg-muted p-4">
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
        <article v-else class="relative grid min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-2xl border border-border bg-card">
          <LoadingOverlay v-if="detailLoading && !serverDetails[selectedId]" :label="t('common.loading')" />
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
              <Button size="sm" :loading="testing" @click="testConnection(selectedServer)"><Cable />{{ t('serversPage.testConnection') }}</Button>
              <Button size="sm" :loading="openingEdit" @click="openEdit(selectedServer)"><Wrench />{{ t('common.edit') }}</Button>
              <Button size="sm" variant="danger" @click="confirmDelete(selectedServer)"><Trash2 />{{ t('common.delete') }}</Button>
            </div>
          </header>

          <div class="min-h-0 overflow-auto p-5">
            <div v-if="selectedServer.hostKeyMismatch || selectedServer.lastError" class="mb-4 grid gap-2">
              <div v-if="selectedServer.hostKeyMismatch" class="rounded-xl border border-danger-border bg-danger-bg p-3 text-sm text-danger">
                <div class="flex flex-wrap items-start justify-between gap-3">
                  <div class="grid min-w-0 gap-1">
                    <strong>{{ t('serversPage.hostKeyMismatchTitle') }}</strong>
                    <p class="m-0">{{ t('serversPage.hostKeyMismatchDescription') }}</p>
                  </div>
                  <Button size="sm" variant="danger" :loading="pendingOperation === 'trustHostKey'" @click="confirmTrustHostKey(selectedServer)"><KeyRound />{{ t('serversPage.trustHostKey') }}</Button>
                </div>
              </div>
              <div v-if="selectedServer.lastError" class="rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning">{{ selectedServer.lastError }}</div>
            </div>

            <div class="grid gap-4 2xl:grid-cols-[minmax(0,1fr)_320px]">
              <div class="grid gap-4">
                <section class="rounded-2xl border border-border bg-muted p-4">
                  <h3 class="m-0 text-sm font-semibold text-foreground">{{ t('serversPage.connection') }}</h3>
                  <dl class="mt-3 grid grid-cols-2 gap-3 text-sm max-md:grid-cols-1">
                    <div><dt>{{ t('serversPage.host') }}</dt><dd>{{ selectedServer.host }}</dd></div>
                    <div><dt>{{ t('serversPage.port') }}</dt><dd>{{ selectedServer.port }}</dd></div>
                    <div><dt>{{ t('serversPage.kind') }}</dt><dd>{{ selectedServer.kind === 'nat' ? t('serversPage.kindNat') : t('serversPage.kindNormal') }}</dd></div>
                    <div><dt>{{ t('serversPage.credential') }}</dt><dd>{{ credentialLabel(selectedServer.credentialId, credentials) || t('common.notAvailable') }}</dd></div>
                    <div><dt>{{ t('serversPage.dockerHost') }}</dt><dd>{{ selectedServer.dockerHost || t('common.notAvailable') }}</dd></div>
                  </dl>
                </section>

                <section v-if="selectedServer.kind === 'nat'" class="rounded-2xl border border-border bg-muted p-4">
                  <div class="flex flex-wrap items-center justify-between gap-3">
                    <h3 class="m-0 text-sm font-semibold text-foreground">{{ t('serversPage.natPorts') }}</h3>
                    <Button size="sm" variant="secondary" @click="openNatAdd"><Plus />{{ t('serversPage.natAddMapping') }}</Button>
                  </div>
                  <p class="mt-1 text-xs text-muted-foreground">{{ t('serversPage.natPortsHint') }}</p>

                  <div class="mt-3 grid gap-2">
                    <h4 class="m-0 text-xs font-semibold text-foreground">{{ t('serversPage.natNeedOpen') }}</h4>
                    <p class="m-0 text-xs text-muted-foreground">{{ t('serversPage.natNeedOpenHint') }}</p>
                    <LoadingOverlay v-if="natLoading && !natConfig" />
                    <p v-if="natError && !natConfig" class="m-0 text-sm text-danger">{{ natError }}</p>
                    <div v-else class="flex flex-wrap gap-2">
                      <span v-for="item in natConfig?.needOpen ?? []" :key="`${item.kind}-${item.port}`" class="inline-flex items-center gap-1 rounded-lg border border-border bg-card px-2 py-1 text-xs">
                        <span class="text-muted-foreground">{{ item.label }}</span>
                        <span class="panel-mono font-semibold text-foreground">{{ item.port }}</span>
                        <span v-if="item.target" class="panel-mono text-muted-foreground">→ {{ item.target }}</span>
                      </span>
                    </div>
                  </div>

                  <div class="mt-3 overflow-x-auto">
                    <table class="w-full text-left text-sm">
                      <thead>
                        <tr class="text-xs text-muted-foreground">
                          <th class="py-1 pr-2">{{ t('serversPage.natHostPort') }}</th>
                          <th class="py-1 pr-2">{{ t('serversPage.natPublicPort') }}</th>
                          <th class="py-1 pr-2">{{ t('serversPage.natApp') }}</th>
                          <th class="py-1 pr-2">{{ t('serversPage.natLabel') }}</th>
                          <th class="py-1 pr-2">{{ t('serversPage.natAccess') }}</th>
                          <th class="py-1" />
                        </tr>
                      </thead>
                      <tbody>
                        <tr v-for="m in natConfig?.mappings ?? []" :key="m.id" class="border-t border-border">
                          <td class="py-2 pr-2"><span class="panel-mono">{{ m.hostPort }}</span></td>
                          <td class="py-2 pr-2"><span class="panel-mono">{{ m.publicPort }}/{{ m.protocol }}</span></td>
                          <td class="py-2 pr-2">{{ m.appName || m.appId || t('common.notAvailable') }}</td>
                          <td class="py-2 pr-2">{{ m.label }}</td>
                          <td class="py-2 pr-2"><span class="panel-mono">{{ natConfig?.serverHost }}:{{ m.publicPort }}</span></td>
                          <td class="py-2 text-right">
                            <div class="flex justify-end gap-1">
                              <Button size="sm" variant="ghost" @click="openNatEdit(m)"><Pencil /></Button>
                              <Button size="sm" variant="ghost" @click="deleteNatPort(m)"><Trash2 /></Button>
                            </div>
                          </td>
                        </tr>
                      </tbody>
                    </table>
                    <p v-if="(natConfig?.mappings?.length ?? 0) === 0 && !natLoading" class="m-0 mt-2 text-sm text-muted-foreground">{{ t('serversPage.natEmpty') }}</p>
                  </div>
                </section>
                <section class="rounded-2xl border border-border bg-muted p-4">
                  <div class="flex flex-wrap items-center justify-between gap-3">
                    <h3 class="m-0 text-sm font-semibold text-foreground">{{ t('serversPage.metrics') }}</h3>
                    <div class="flex flex-wrap items-center gap-2">
                      <Select v-model="metricsRange" :options="metricsRangeOptions" class="w-24" />
                      <AutoRefreshControl
                        :off-label="t('autoRefresh.off')"
                        :short-label="t('autoRefresh.5s')"
                        :long-label="t('autoRefresh.10s')"
                        :hint-label="t('autoRefresh.hint')"
                      />
                      <Button size="sm" variant="ghost" :loading="metricsLoading" @click="loadMetrics(true)"><RefreshCcw />{{ t('common.refresh') }}</Button>
                    </div>
                  </div>
                  <LoadingOverlay v-if="metricsLoading && !metrics" />
                  <p v-else-if="metricsError && !metrics" class="mt-3 text-sm text-danger">{{ metricsError }}</p>
                  <template v-else-if="metrics">

                    <div class="mt-4 grid gap-3 xl:grid-cols-2">
                      <div v-for="panel in metricChartPanels" :key="panel.key" class="rounded-xl border border-border bg-card p-3">
                        <div class="flex items-center justify-between gap-3">
                          <span class="text-xs font-medium text-muted-foreground">{{ panel.label }}</span>
                          <span class="truncate text-sm font-semibold text-foreground">{{ panel.current }}</span>
                        </div>
                        <div class="mt-2 h-28 min-w-0">
                          <MetricLineChart :labels="panel.labels" :series="panel.series" :value-kind="panel.valueKind" />
                        </div>
                      </div>
                    </div>
                  </template>
                </section>
                <section class="rounded-2xl border border-border bg-muted p-4">
                  <div class="flex flex-wrap items-center justify-between gap-2"><h3 class="m-0 text-sm font-semibold text-foreground">{{ t('serversPage.recentOperations') }}</h3><Button size="sm" variant="ghost" @click="router.push({ path: '/activity', query: { resourceType: 'server', resourceId: selectedServer.id } })">{{ t('activity.relatedLogs') }}</Button></div>
                  <div class="mt-3 grid gap-2 text-sm text-muted-foreground">
                    <span>{{ t('serversPage.lastChecked') }}: {{ formatDateTime(selectedServer.lastCheckedAt) || t('common.never') }}</span>
                    <span>{{ t('serversPage.updatedAt') }}: {{ formatDateTime(selectedServer.updatedAt) || t('common.never') }}</span>
                    <div v-if="selectedServer.initialTaskId" class="grid gap-1">
                      <div class="flex flex-wrap items-center justify-between gap-2">
                        <span>{{ t('serversPage.initialTask') }}</span>
                        <span class="flex items-center gap-2">
                          <StatusBadge v-if="initialTask" :status="initialTask.status" domain="task" :label="t(`tasksPage.status.${initialTask.status}`)" />
                          <Button size="sm" variant="ghost" @click="router.push({ path: '/activity', query: { executionId: selectedServer.initialTaskId } })">{{ t('activity.relatedLogs') }}</Button>
                        </span>
                      </div>
                      <span class="panel-mono text-xs">{{ selectedServer.initialTaskId }}</span>
                      <p v-if="initialTaskLoading && !initialTask" class="m-0 text-xs">{{ t('serversPage.agentTaskLoading') }}</p>
                      <p v-if="initialTaskError" class="m-0 break-words text-danger">{{ initialTaskError }}</p>
                      <p v-else-if="initialTask?.error" class="m-0 break-words text-danger">{{ initialTask.error }}</p>
                    </div>
                  </div>
                </section>
              </div>
              <aside class="grid content-start gap-3">
                <section class="relative rounded-2xl border border-border bg-muted p-4">
                  <div class="flex min-w-0 flex-wrap items-center justify-between gap-2">
                    <h3 class="m-0 text-sm font-semibold text-foreground">{{ t('serversPage.agent') }}</h3>
                    <StatusBadge v-if="agentTask" :status="agentTask.status" domain="task" :label="t(`tasksPage.status.${agentTask.status}`)" />
                  </div>
                  <p class="mt-2 text-sm text-muted-foreground">{{ agentText(selectedServer) }}</p>
                  <LoadingOverlay v-if="agentTaskLoading && !agentTask" :label="t('serversPage.agentTaskLoading')" />
                  <div v-if="agentTask" class="mt-3 grid min-w-0 gap-2 rounded-xl border border-border bg-card p-3 text-xs">
                    <div class="flex min-w-0 flex-wrap items-center justify-between gap-2">
                      <strong class="text-foreground">{{ t('serversPage.agentTaskCurrentStage') }}</strong>
                      <span class="text-muted-foreground">{{ agentTaskStageLabel(agentTask.stage) }}</span>
                    </div>
                    <p v-if="agentTask.error" class="m-0 break-words text-danger">{{ agentTask.error }}</p>
                    <div class="grid min-w-0 gap-1">
                      <div class="flex flex-wrap items-center justify-between gap-2"><strong class="text-foreground">{{ t('serversPage.agentTaskRecentLogs') }}</strong><Button size="sm" variant="ghost" @click="router.push({ path: '/activity', query: { executionId: agentTask.id } })">{{ t('activity.relatedLogs') }}</Button></div>
                      <pre aria-live="polite" class="m-0 max-h-48 min-w-0 overflow-y-auto overflow-x-hidden whitespace-pre-wrap break-words rounded-lg bg-muted p-2 text-[11px] text-muted-foreground [overflow-wrap:anywhere]">{{ visibleAgentTaskLogs.map((line) => `[${line.stream}] ${formatAgentTaskLog(line.line)}`).join('\n') || t('serversPage.agentTaskNoLogs') }}</pre>
                    </div>
                  </div>
                  <div v-if="agentTaskError" class="mt-3 grid gap-2 rounded-xl border border-danger-border bg-danger-bg p-3 text-xs text-danger">
                    <span class="break-words">{{ agentTaskError }}</span>
                    <Button size="sm" variant="secondary" :loading="agentTaskLoading" @click="loadAgentDeployment(true)"><RefreshCcw />{{ t('common.retry') }}</Button>
                  </div>
                  <p v-else-if="!agentTask && !agentTaskLoading" class="mt-3 text-xs text-muted-foreground">{{ t('serversPage.agentTaskNoHistory') }}</p>
                  <div class="mt-3 grid gap-2">
                    <Button :loading="pendingOperation === 'agent'" @click="deployAgent(selectedServer)"><ServerCog />{{ t('serversPage.deployAgent') }}</Button>
                    <Button :disabled="!canRunPrivilegedOperation(selectedServer)" :loading="pendingOperation === 'restart'" @click="confirmRestart(selectedServer)"><PlayCircle />{{ t('serversPage.restart') }}</Button>
                  </div>
                </section>
                <section class="rounded-2xl border border-border bg-muted p-4">
                  <h3 class="m-0 text-sm font-semibold text-foreground">{{ t('serversPage.privilegeAndSecurity') }}</h3>
                  <p class="mt-2 text-sm text-muted-foreground">{{ privilegeText(selectedServer) }}</p>
                  <Button class="mt-3 w-full" :disabled="!canInstallUfw(selectedServer)" :loading="pendingOperation === 'ufw'" @click="confirmInstallUfw(selectedServer)">
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

    <ServerFormDialog
      v-model:open="serverDialog"
      :editing="editing"
      :credentials="credentials"
      :credential-error="credentialError"
      @saved="handleSaved"
      @refresh-credentials="loadCredentials"
    />

    <Dialog v-model:open="natDialogOpen" :title="t(natEditingId ? 'serversPage.natEditMapping' : 'serversPage.natAddMapping')" :close-label="t('common.cancel')">
      <div class="grid gap-4">
        <div class="grid gap-1">
          <label class="grid gap-1 text-sm">{{ t('serversPage.natHostPort') }}<Input id="nat-form-host-port" v-model="natForm.hostPort" type="number" /></label>
          <p class="m-0 text-xs text-muted-foreground">{{ t('serversPage.natHostPortHint') }}</p>
        </div>
        <div class="grid gap-1">
          <label class="grid gap-1 text-sm">{{ t('serversPage.natPublicPort') }}<Input id="nat-form-public-port" v-model="natForm.publicPort" type="number" /></label>
          <p class="m-0 text-xs text-muted-foreground">{{ t('serversPage.natPublicPortHint') }}</p>
        </div>
        <div class="grid gap-1">
          <label class="grid gap-1 text-sm">{{ t('serversPage.natProtocol') }}<Select v-model="natForm.protocol" :options="natProtocolOptions" /></label>
        </div>
        <div class="grid gap-1">
          <label class="grid gap-1 text-sm">{{ t('serversPage.natApp') }}<Input v-model="natForm.appId" :placeholder="t('serversPage.natAppIdHint')" /></label>
        </div>
        <div class="grid gap-1">
          <label class="grid gap-1 text-sm">{{ t('serversPage.natLabel') }}<Input v-model="natForm.label" /></label>
        </div>
        <div class="grid gap-1">
          <label class="grid gap-1 text-sm">{{ t('serversPage.natNotes') }}<Textarea v-model="natForm.notes" /></label>
        </div>
        <p v-if="natFormError" class="m-0 text-sm text-danger">{{ natFormError }}</p>
      </div>
      <template #footer>
        <Button variant="secondary" @click="natDialogOpen = false">{{ t('common.cancel') }}</Button>
        <Button variant="primary" :loading="natSaving" :disabled="false" @click="saveNatPort">{{ natEditingId ? t('common.save') : t('common.create') }}</Button>
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

    <ConfirmDialog
      :open="Boolean(confirmOperation)"
      :title="confirmOperation === 'restart' ? t('serversPage.confirmRestartTitle') : confirmOperation === 'ufw' ? t('serversPage.confirmUfwTitle') : t('serversPage.confirmTrustHostKeyTitle')"
      :description="confirmTarget ? (confirmOperation === 'restart' ? t('serversPage.confirmRestartDescription', { name: confirmTarget.name }) : confirmOperation === 'ufw' ? t('serversPage.confirmUfwDescription', { name: confirmTarget.name }) : t('serversPage.confirmTrustHostKeyDescription', { name: confirmTarget.name })) : ''"
      :impact="confirmOperation === 'restart' ? t('serversPage.confirmRestartImpact') : confirmOperation === 'ufw' ? t('serversPage.confirmUfwImpact') : t('serversPage.confirmTrustHostKeyImpact')"
      tone="danger"
      :loading="Boolean(pendingOperation)"
      :confirm-label="t('common.confirm')"
      :cancel-label="t('common.cancel')"
      :require-checkbox="confirmOperation === 'trustHostKey'"
      :checkbox-label="confirmOperation === 'trustHostKey' ? t('serversPage.trustHostKeyCheckbox') : t('serversPage.confirmCheckbox')"
      @update:open="(value) => { if (!value) confirmOperation = null }"
      @confirm="runConfirmedOperation"
      @cancel="confirmOperation = null"
    />
  </ConsolePage>
</template>

<style scoped>
dt {
  margin: 0 0 4px;
  color: var(--panel-text-muted);
  font-size: 12px;
}

dd {
  margin: 0;
  overflow-wrap: anywhere;
  color: var(--panel-text);
  font-weight: 600;
}
</style>
