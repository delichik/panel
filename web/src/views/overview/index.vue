<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { useRouter } from 'vue-router';
import { useTheme } from 'vuetify';
import VChart from 'vue-echarts';
import { use } from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import { LineChart } from 'echarts/charts';
import { GridComponent, LegendComponent, TooltipComponent } from 'echarts/components';
import { useI18n } from '@/i18n';
import { overviewApi } from '@/api/overview';
import { applicationsApi } from '@/api/applications';
import type { ApplicationDto, MetricsRange, MetricsSeriesDto, OverviewDto, OverviewServerDto } from '@/types/api';
import PageLoadingState from '@/components/PageLoadingState.vue';

use([CanvasRenderer, LineChart, GridComponent, TooltipComponent, LegendComponent]);

type CardKind = 'cpu' | 'memory' | 'disk' | 'network' | 'packageUpdates' | 'containerUpdates' | 'placeholder';
type NetworkDirection = 'rx' | 'tx' | 'both';

interface OverviewCardConfig {
  id: string;
  kind: CardKind;
  width: number;
  height: number;
  range: MetricsRange;
  networkDirection: NetworkDirection;
  serverIds: string[];
}

interface CardPreset {
  kind: CardKind;
  icon: string;
  color: string;
  category: 'metric' | 'message';
  width: number;
  height: number;
}

type StoredOverviewCard = Partial<OverviewCardConfig> & {
  title?: string;
};

const STORAGE_KEY = 'linux-panel-overview-cards';
const router = useRouter();
const theme = useTheme();
const { t, formatDateTime } = useI18n();
const isDark = computed(() => theme.global.current.value.dark);

const cardPresets: CardPreset[] = [
  { kind: 'cpu', icon: 'mdi-cpu-64-bit', color: 'primary', category: 'metric', width: 3, height: 2 },
  { kind: 'memory', icon: 'mdi-memory', color: 'success', category: 'metric', width: 3, height: 2 },
  { kind: 'disk', icon: 'mdi-harddisk', color: 'warning', category: 'metric', width: 3, height: 2 },
  { kind: 'network', icon: 'mdi-lan', color: 'info', category: 'metric', width: 3, height: 2 },
  { kind: 'packageUpdates', icon: 'mdi-package-variant-plus', color: 'warning', category: 'message', width: 3, height: 2 },
  { kind: 'containerUpdates', icon: 'mdi-docker', color: 'primary', category: 'message', width: 3, height: 2 },
  { kind: 'placeholder', icon: 'mdi-border-none-variant', color: 'secondary', category: 'message', width: 3, height: 2 },
];

const rangeItems = computed<Array<{ title: string; value: MetricsRange }>>(() => [
  { title: '1h', value: '1h' },
  { title: '6h', value: '6h' },
  { title: '1d', value: '1d' },
  { title: '7d', value: '7d' },
]);

const networkDirectionItems = computed<Array<{ title: string; value: NetworkDirection }>>(() => [
  { title: t('overviewPage.rxTx'), value: 'both' },
  { title: t('overviewPage.rx'), value: 'rx' },
  { title: t('overviewPage.tx'), value: 'tx' },
]);

const overview = ref<OverviewDto>({ servers: [] });
const applications = ref<ApplicationDto[]>([]);
const cards = ref<OverviewCardConfig[]>(loadCards());
const metricsByKey = ref<Record<string, MetricsSeriesDto | null>>({});
const loading = ref(false);
const error = ref('');
const dialog = ref(false);
const editingCardId = ref('');
const editMode = ref(false);
const draggingCardId = ref('');
const dragOverCardId = ref('');
let refreshTimer: number | undefined;

const form = reactive({
  kind: 'cpu' as CardKind,
  width: 3,
  height: 2,
  range: '1h' as MetricsRange,
  networkDirection: 'both' as NetworkDirection,
  serverIds: [] as string[],
});

const palette = computed(() => ({
  text: isDark.value ? '#b3beb6' : '#5f6f68',
  grid: isDark.value ? 'rgba(179, 190, 182, 0.16)' : 'rgba(95, 111, 104, 0.16)',
  tooltipBackground: isDark.value ? '#151a17' : '#ffffff',
  tooltipBorder: isDark.value ? 'rgba(179, 190, 182, 0.22)' : 'rgba(95, 111, 104, 0.18)',
  tooltipText: isDark.value ? '#e7ece6' : '#1f2724',
  series: isDark.value
    ? ['#2dd4bf', '#4ade80', '#f59e0b', '#60a5fa', '#fb7185', '#a78bfa']
    : ['#0f766e', '#16a34a', '#b45309', '#2563eb', '#dc2626', '#7c3aed'],
}));

const serverOptions = computed(() => overview.value.servers.map((server) => ({
  title: `${server.name} (${server.host})`,
  value: server.id,
})));

const configuredServerIds = computed(() => new Set(cards.value.flatMap((card) => resolveCardServerIds(card))));
const onlineCount = computed(() => overview.value.servers.filter((server) => server.reachable).length);
const issueCount = computed(() => packageRowsForServers(overview.value.servers).length + containerRowsForServers(overview.value.servers.map((server) => server.id)).length);
const overviewSignals = computed(() => [
  {
    key: 'online',
    icon: 'mdi-server-network',
    color: 'success',
    value: `${onlineCount.value}/${overview.value.servers.length}`,
    label: t('overviewPage.onlineSummary', { online: onlineCount.value, total: overview.value.servers.length }),
  },
  {
    key: 'dashboard',
    icon: 'mdi-view-dashboard-outline',
    color: 'primary',
    value: configuredServerIds.value.size || overview.value.servers.length,
    label: t('overviewPage.dashboardSummary', { count: configuredServerIds.value.size || overview.value.servers.length }),
  },
  {
    key: 'issues',
    icon: 'mdi-alert-circle-outline',
    color: issueCount.value > 0 ? 'warning' : 'success',
    value: issueCount.value,
    label: t('overviewPage.issueSummary', { count: issueCount.value }),
  },
]);
const presetItems = computed(() => cardPresets.map((preset) => ({
  ...preset,
  title: cardTitle(preset.kind),
})));

watch(cards, () => {
  saveCards();
  void loadCardMetrics();
}, { deep: true });

function loadCards(): OverviewCardConfig[] {
  if (typeof window === 'undefined') return defaultCards();
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return defaultCards();
    const parsed = JSON.parse(raw) as StoredOverviewCard[];
    return Array.isArray(parsed) && parsed.length ? parsed.map(normalizeCard).filter(Boolean) as OverviewCardConfig[] : defaultCards();
  } catch {
    return defaultCards();
  }
}

function saveCards() {
  if (typeof window === 'undefined') return;
  window.localStorage.setItem(STORAGE_KEY, JSON.stringify(cards.value));
}

function defaultCards(): OverviewCardConfig[] {
  return [
    createCard('cpu', 3, 2, '1h'),
    createCard('memory', 3, 2, '1h'),
    createCard('disk', 3, 2, '6h'),
    createCard('network', 3, 2, '1h'),
    createCard('packageUpdates', 3, 2, '1d'),
    createCard('containerUpdates', 3, 2, '1d'),
  ];
}

function normalizeCard(card: StoredOverviewCard): OverviewCardConfig | null {
  if (!card.kind) return null;
  const preset = presetFor(card.kind);
  const range: MetricsRange = rangeItems.value.some((item) => item.value === card.range) ? card.range as MetricsRange : '1h';
  const networkDirection = normalizeNetworkDirection(card.networkDirection);
  return {
    id: card.id || newId(),
    kind: preset.kind,
    width: clamp(Number(card.width) || preset.width, 1, 6),
    height: clamp(Number(card.height) || preset.height, 1, 4),
    range,
    networkDirection,
    serverIds: Array.isArray(card.serverIds) ? card.serverIds : [],
  };
}

function createCard(kind: CardKind, width?: number, height?: number, range: MetricsRange = '1h'): OverviewCardConfig {
  const preset = presetFor(kind);
  return {
    id: newId(),
    kind,
    width: width ?? preset.width,
    height: height ?? preset.height,
    range,
    networkDirection: 'both',
    serverIds: [],
  };
}

function newId() {
  return `card-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
}

function presetFor(kind: CardKind) {
  return cardPresets.find((preset) => preset.kind === kind) ?? cardPresets[0];
}

function cardTitle(kind: CardKind) {
  return t(`overviewPage.${kind}`);
}

function isMetricCard(card: OverviewCardConfig) {
  return presetFor(card.kind).category === 'metric';
}

function normalizeNetworkDirection(value: unknown): NetworkDirection {
  return value === 'rx' || value === 'tx' || value === 'both' ? value : 'both';
}

function clamp(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value));
}

function resolveCardServers(card: OverviewCardConfig) {
  const selected = new Set(card.serverIds);
  return overview.value.servers.filter((server) => selected.size === 0 || selected.has(server.id));
}

function resolveCardServerIds(card: OverviewCardConfig) {
  return resolveCardServers(card).map((server) => server.id);
}

function metricKey(serverId: string, range: MetricsRange) {
  return `${serverId}:${range}`;
}

async function loadData() {
  loading.value = true;
  try {
    const [nextOverview, nextApplications] = await Promise.all([
      overviewApi.getOverview(),
      applicationsApi.list(),
    ]);
    overview.value = nextOverview;
    applications.value = nextApplications;
    error.value = '';
    await loadCardMetrics();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('overviewPage.loadFailed');
  } finally {
    loading.value = false;
  }
}

async function loadCardMetrics() {
  const pairs = new Map<string, { serverId: string; range: MetricsRange }>();
  for (const card of cards.value) {
    if (!isMetricCard(card)) continue;
    for (const serverId of resolveCardServerIds(card)) {
      pairs.set(metricKey(serverId, card.range), { serverId, range: card.range });
    }
  }
  await Promise.all([...pairs.entries()].map(async ([key, pair]) => {
    try {
      metricsByKey.value[key] = await overviewApi.getMetrics(pair.serverId, pair.range);
    } catch {
      metricsByKey.value[key] = null;
    }
  }));
}

function chartOption(card: OverviewCardConfig) {
  const servers = resolveCardServers(card);
  const series = servers.flatMap((server, index) => metricSeriesForCard(card, server, index));
  return {
    color: palette.value.series,
    tooltip: {
      trigger: 'axis',
      appendToBody: true,
      backgroundColor: palette.value.tooltipBackground,
      borderColor: palette.value.tooltipBorder,
      textStyle: { color: palette.value.tooltipText },
      valueFormatter: (value: number) => formatMetricValue(card.kind, value),
    },
    legend: {
      top: 0,
      type: 'scroll',
      textStyle: { color: palette.value.text, fontSize: 11 },
    },
    grid: { left: 46, right: 20, top: 36, bottom: 28 },
    xAxis: {
      type: 'time',
      axisLabel: {
        color: palette.value.text,
        fontSize: 11,
        hideOverlap: true,
        showMinLabel: false,
        showMaxLabel: false,
      },
      axisLine: { lineStyle: { color: palette.value.grid } },
    },
    yAxis: {
      type: 'value',
      axisLabel: {
        color: palette.value.text,
        fontSize: 11,
        formatter: (value: number) => formatAxisValue(card.kind, value),
      },
      splitLine: { lineStyle: { color: palette.value.grid } },
    },
    series,
  };
}

function metricSeriesForCard(card: OverviewCardConfig, server: OverviewServerDto, index: number) {
  const series = metricsByKey.value[metricKey(server.id, card.range)];
  const color = palette.value.series[index % palette.value.series.length];
  if (!series) return [];
  if (card.kind === 'cpu') return [lineSeries(server.name, color, series.cpu.map((point) => [point.time, point.usagePercent]))];
  if (card.kind === 'memory') return [lineSeries(server.name, color, series.memory.map((point) => [point.time, percent(point.usedBytes, point.totalBytes)]))];
  if (card.kind === 'disk') return [lineSeries(server.name, color, series.disk.map((point) => [point.time, percent(point.usedBytes, point.totalBytes)]))];
  if (card.kind === 'network') {
    const items = [];
    if (card.networkDirection === 'rx' || card.networkDirection === 'both') {
      items.push(lineSeries(`${server.name} RX`, color, series.network.map((point) => [point.time, point.rxBytesPerSecond])));
    }
    if (card.networkDirection === 'tx' || card.networkDirection === 'both') {
      items.push(lineSeries(`${server.name} TX`, color, series.network.map((point) => [point.time, point.txBytesPerSecond]), card.networkDirection === 'both' ? 'dashed' : 'solid'));
    }
    return items;
  }
  return [];
}

function lineSeries(name: string, color: string, data: Array<[string, number]>, type: 'solid' | 'dashed' = 'solid') {
  return {
    name,
    type: 'line',
    smooth: true,
    symbol: 'none',
    data,
    lineStyle: { color, type, width: 2 },
  };
}

function percent(used: number, total: number) {
  return total > 0 ? Math.round((used / total) * 1000) / 10 : 0;
}

function formatAxisValue(kind: CardKind, value: number) {
  return kind === 'network' ? formatBytesPerSecond(value) : `${Number(value).toFixed(0)}%`;
}

function formatMetricValue(kind: CardKind, value: number) {
  return kind === 'network' ? formatBytesPerSecond(value) : `${Number(value).toFixed(1)}%`;
}

function formatBytesPerSecond(bytesPerSecond: number): string {
  if (!Number.isFinite(bytesPerSecond) || bytesPerSecond === 0) return '0 B/s';
  const sizes = ['B/s', 'KB/s', 'MB/s', 'GB/s', 'TB/s'];
  const index = Math.min(Math.floor(Math.log(bytesPerSecond) / Math.log(1024)), sizes.length - 1);
  return `${(bytesPerSecond / Math.pow(1024, index)).toFixed(1)} ${sizes[index]}`;
}

function packageRows(card: OverviewCardConfig) {
  return packageRowsForServers(resolveCardServers(card));
}

function packageRowsForServers(servers: OverviewServerDto[]) {
  return servers
    .filter((server) => server.packageUpdateCount > 0)
    .sort((a, b) => b.packageUpdateCount - a.packageUpdateCount);
}

function containerRows(card: OverviewCardConfig) {
  return containerRowsForServers(resolveCardServerIds(card));
}

function containerRowsForServers(serverIds: string[]) {
  const selected = new Set(serverIds);
  return applications.value.filter((app) => {
  if (!app.imageUpdateAvailable && !app.imageLastError) return false;
    const targets = applicationServerIds(app);
    return selected.size === 0 || targets.some((serverId) => selected.has(serverId));
  });
}

function applicationServerIds(app: ApplicationDto) {
  if (app.deploymentMode === 'selected') return app.deploymentServers ?? [];
  return overview.value.servers.map((server) => server.id);
}

function targetLabel(app: ApplicationDto) {
  if (app.deploymentMode !== 'selected') return t('overviewPage.allServers');
  const ids = new Set(app.deploymentServers ?? []);
  const names = overview.value.servers.filter((server) => ids.has(server.id)).map((server) => server.name);
  return names.length ? names.join(', ') : t('overviewPage.noSelectedServers');
}

function cardServerLabel(card: OverviewCardConfig) {
  const servers = resolveCardServers(card);
  if (card.serverIds.length === 0) return t('overviewPage.allServers');
  if (servers.length === 0) return t('overviewPage.noServers');
  return t('overviewPage.serverCount', { count: servers.length });
}

function metricCardLabel(card: OverviewCardConfig) {
  if (card.kind !== 'network') return card.range;
  const direction = networkDirectionItems.value.find((item) => item.value === card.networkDirection)?.title ?? t('overviewPage.rxTx');
  return `${card.range} / ${direction}`;
}

function openAddDialog(kind?: CardKind) {
  const preset = presetFor(kind ?? 'cpu');
  editingCardId.value = '';
  Object.assign(form, {
    kind: preset.kind,
    width: preset.width,
    height: preset.height,
    range: '1h' as MetricsRange,
    networkDirection: 'both' as NetworkDirection,
    serverIds: [],
  });
  dialog.value = true;
}

function openEditDialog(card: OverviewCardConfig) {
  editingCardId.value = card.id;
  Object.assign(form, {
    kind: card.kind,
    width: card.width,
    height: card.height,
    range: card.range,
    networkDirection: normalizeNetworkDirection(card.networkDirection),
    serverIds: [...card.serverIds],
  });
  dialog.value = true;
}

function saveCard() {
  const next: OverviewCardConfig = {
    id: editingCardId.value || newId(),
    kind: form.kind,
    width: clamp(form.width, 1, 6),
    height: clamp(form.height, 1, 4),
    range: form.range,
    networkDirection: normalizeNetworkDirection(form.networkDirection),
    serverIds: [...form.serverIds],
  };
  if (editingCardId.value) {
    cards.value = cards.value.map((card) => card.id === editingCardId.value ? next : card);
  } else {
    cards.value = [...cards.value, next];
  }
  dialog.value = false;
}

function removeCard(cardId: string) {
  cards.value = cards.value.filter((card) => card.id !== cardId);
}

function resetCards() {
  cards.value = defaultCards();
}

function toggleEditMode() {
  editMode.value = !editMode.value;
  if (!editMode.value) {
    draggingCardId.value = '';
    dragOverCardId.value = '';
  }
}

function startCardDrag(cardId: string, event: DragEvent) {
  if (!editMode.value) return;
  draggingCardId.value = cardId;
  if (event.dataTransfer) {
    event.dataTransfer.effectAllowed = 'move';
    event.dataTransfer.setData('text/plain', cardId);
  }
}

function handleCardDragOver(cardId: string, event: DragEvent) {
  if (!editMode.value || !draggingCardId.value || draggingCardId.value === cardId) return;
  event.preventDefault();
  dragOverCardId.value = cardId;
  if (event.dataTransfer) event.dataTransfer.dropEffect = 'move';
  reorderCard(draggingCardId.value, cardId, shouldInsertAfter(event));
}

function dropCard(cardId: string, event: DragEvent) {
  if (!editMode.value) return;
  event.preventDefault();
  const sourceId = draggingCardId.value || event.dataTransfer?.getData('text/plain') || '';
  reorderCard(sourceId, cardId, shouldInsertAfter(event));
  endCardDrag();
}

function endCardDrag() {
  draggingCardId.value = '';
  dragOverCardId.value = '';
}

function shouldInsertAfter(event: DragEvent) {
  const element = event.currentTarget instanceof HTMLElement ? event.currentTarget : null;
  if (!element) return false;
  const rect = element.getBoundingClientRect();
  const xRatio = (event.clientX - rect.left) / rect.width;
  const yRatio = (event.clientY - rect.top) / rect.height;
  if (yRatio < 0.35) return false;
  if (yRatio > 0.65) return true;
  return xRatio > 0.5;
}

function reorderCard(sourceId: string, targetId: string, insertAfter = false) {
  if (!sourceId || sourceId === targetId) return;
  const next = [...cards.value];
  const sourceIndex = next.findIndex((card) => card.id === sourceId);
  const targetIndex = next.findIndex((card) => card.id === targetId);
  if (sourceIndex < 0 || targetIndex < 0) return;
  const [movingCard] = next.splice(sourceIndex, 1);
  const nextTargetIndex = next.findIndex((card) => card.id === targetId);
  const insertIndex = nextTargetIndex + (insertAfter ? 1 : 0);
  next.splice(insertIndex, 0, movingCard);
  cards.value = next;
}

function cardStyle(card: OverviewCardConfig) {
  return {
    gridColumn: `span ${clamp(card.width, 1, 6)}`,
    gridRow: `span ${clamp(card.height, 1, 4)}`,
  };
}

onMounted(async () => {
  await loadData();
  refreshTimer = window.setInterval(loadData, 15000);
});

onBeforeUnmount(() => {
  if (refreshTimer) window.clearInterval(refreshTimer);
});
</script>

<template>
  <div class="overview-workspace page-shell">
    <div class="overview-actions-row page-toolbar">
      <div class="overview-signals">
        <div v-for="signal in overviewSignals" :key="signal.key" class="overview-signal">
          <span class="overview-signal__icon" :class="`surface-${signal.color}`">
            <v-icon size="18">{{ signal.icon }}</v-icon>
          </span>
          <span class="min-width-0">
            <span class="overview-signal__value font-tabular">{{ signal.value }}</span>
            <span class="overview-signal__label text-truncate">{{ signal.label }}</span>
          </span>
        </div>
      </div>
      <div class="overview-actions">
        <template v-if="editMode">
          <v-btn variant="outlined" prepend-icon="mdi-restore" class="text-none" @click="resetCards">{{ t('overviewPage.reset') }}</v-btn>
          <v-btn color="primary" variant="flat" prepend-icon="mdi-view-grid-plus" class="text-none font-weight-bold" @click="openAddDialog()">{{ t('overviewPage.addCard') }}</v-btn>
        </template>
        <v-btn
          :color="editMode ? 'primary' : undefined"
          :variant="editMode ? 'flat' : 'outlined'"
          :prepend-icon="editMode ? 'mdi-check' : 'mdi-pencil'"
          class="text-none font-weight-bold"
          @click="toggleEditMode"
        >
          {{ editMode ? t('common.done') : t('common.edit') }}
        </v-btn>
      </div>
    </div>

    <v-alert v-if="error" type="error" variant="tonal">{{ error }}</v-alert>

    <PageLoadingState v-if="loading && overview.servers.length === 0" min-height="360px" />

    <div v-else-if="overview.servers.length === 0" class="empty-state">
      <v-icon size="44" color="primary">mdi-server-network-off</v-icon>
      <div>
        <div class="text-subtitle-1 font-weight-bold">{{ t('overviewPage.noServersConnected') }}</div>
        <div class="text-body-2 text-medium-emphasis">{{ t('overviewPage.addServerHint') }}</div>
      </div>
      <v-btn color="primary" variant="flat" prepend-icon="mdi-plus" class="text-none" @click="router.push('/servers')">{{ t('common.addServer') }}</v-btn>
    </div>

    <div v-else class="dashboard-grid">
      <v-card
        v-for="card in cards"
        :key="card.id"
        class="dashboard-card"
        :class="{
          'dashboard-card--placeholder': card.kind === 'placeholder',
          'dashboard-card--editing': editMode,
          'dashboard-card--dragging': draggingCardId === card.id,
          'dashboard-card--drag-over': dragOverCardId === card.id,
        }"
        variant="outlined"
        :style="cardStyle(card)"
        @dragover="handleCardDragOver(card.id, $event)"
        @drop="dropCard(card.id, $event)"
      >
        <span v-if="card.kind !== 'placeholder'" class="card-accent" :class="`card-accent--${presetFor(card.kind).color}`" />
        <div v-if="card.kind !== 'placeholder'" class="card-header">
          <div class="card-title">
            <span class="card-icon" :class="`surface-${presetFor(card.kind).color}`">
              <v-icon size="18">{{ presetFor(card.kind).icon }}</v-icon>
            </span>
            <div class="min-width-0">
              <div class="text-subtitle-2 font-weight-bold text-truncate">{{ cardTitle(card.kind) }}</div>
              <div class="text-caption text-medium-emphasis text-truncate">
                {{ cardServerLabel(card) }}<template v-if="isMetricCard(card)"> / {{ metricCardLabel(card) }}</template>
              </div>
            </div>
          </div>
          <div v-if="editMode" class="card-tools">
            <v-btn
              class="card-drag-handle"
              icon="mdi-drag"
              variant="text"
              size="small"
              draggable="true"
              @dragstart="startCardDrag(card.id, $event)"
              @dragend="endCardDrag"
            />
            <v-menu location="bottom end">
              <template #activator="{ props }">
                <v-btn v-bind="props" icon="mdi-dots-vertical" variant="text" size="small" />
              </template>
              <v-list density="compact">
                <v-list-item prepend-icon="mdi-pencil" :title="t('common.edit')" @click="openEditDialog(card)" />
                <v-list-item prepend-icon="mdi-delete" :title="t('common.remove')" class="text-error" @click="removeCard(card.id)" />
              </v-list>
            </v-menu>
          </div>
        </div>

          <div v-else-if="editMode" class="placeholder-tools">
            <v-btn
              class="card-drag-handle"
              icon="mdi-drag"
              variant="text"
              size="small"
              draggable="true"
              @dragstart="startCardDrag(card.id, $event)"
              @dragend="endCardDrag"
            />
            <v-menu location="bottom end">
              <template #activator="{ props }">
                <v-btn v-bind="props" icon="mdi-dots-vertical" variant="text" size="small" />
              </template>
              <v-list density="compact">
                <v-list-item prepend-icon="mdi-pencil" :title="t('common.edit')" @click="openEditDialog(card)" />
                <v-list-item prepend-icon="mdi-delete" :title="t('common.remove')" class="text-error" @click="removeCard(card.id)" />
              </v-list>
            </v-menu>
          </div>

        <div v-if="isMetricCard(card)" class="card-body chart-body">
          <VChart class="chart" :option="chartOption(card)" autoresize />
        </div>

        <div v-else-if="card.kind === 'packageUpdates'" class="card-body message-list">
          <div
            v-for="server in packageRows(card)"
            :key="server.id"
            class="message-row"
            @click="router.push('/servers/packages')"
          >
            <div>
              <div class="font-weight-bold text-body-2">{{ server.name }}</div>
              <div class="text-caption text-medium-emphasis">{{ t('overviewPage.lastChecked', { value: server.lastPackageRefreshAt ? formatDateTime(server.lastPackageRefreshAt) : t('common.never') }) }}</div>
            </div>
            <v-chip color="warning" size="small" variant="tonal" label>{{ server.packageUpdateCount }}</v-chip>
          </div>
          <div v-if="packageRows(card).length === 0" class="empty-card-text">{{ t('overviewPage.noPackageUpdates') }}</div>
        </div>

        <div v-else-if="card.kind === 'containerUpdates'" class="card-body message-list">
          <div
            v-for="app in containerRows(card)"
            :key="app.id"
            class="message-row"
            @click="router.push({ path: '/applications', query: { application: app.id } })"
          >
            <div class="min-width-0">
              <div class="font-weight-bold text-body-2 text-truncate">{{ app.name }}</div>
              <div class="text-caption text-medium-emphasis text-truncate">{{ targetLabel(app) }}</div>
            </div>
            <v-chip :color="app.imageLastError ? 'error' : 'warning'" size="small" variant="tonal" label>
              {{ app.imageLastError ? t('overviewPage.errorBadge') : t('overviewPage.updateBadge') }}
            </v-chip>
          </div>
          <div v-if="containerRows(card).length === 0" class="empty-card-text">{{ t('overviewPage.noContainerUpdates') }}</div>
        </div>

        <div v-else class="placeholder-card-body"></div>
      </v-card>
    </div>

    <v-dialog v-model="dialog" width="640">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ editingCardId ? t('overviewPage.editCard') : t('overviewPage.createCard') }}</span>
          <v-btn icon="mdi-close" variant="text" @click="dialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          <v-select
            v-model="form.kind"
            :items="presetItems"
            item-title="title"
            item-value="kind"
            :label="t('common.preset')"
            variant="outlined"
            density="comfortable"
            class="mb-3"
          />
          <div class="size-grid mb-3">
            <v-text-field v-model.number="form.width" type="number" :label="t('common.width')" min="1" max="6" variant="outlined" density="comfortable" hide-details />
            <v-text-field v-model.number="form.height" type="number" :label="t('common.height')" min="1" max="4" variant="outlined" density="comfortable" hide-details />
          </div>
          <v-select
            v-if="presetFor(form.kind).category === 'metric'"
            v-model="form.range"
            :items="rangeItems"
            :label="t('common.range')"
            variant="outlined"
            density="comfortable"
            class="mb-3"
          />
          <div v-if="form.kind === 'network'" class="mb-3">
            <div class="text-caption text-medium-emphasis mb-2">{{ t('common.traffic') }}</div>
            <v-btn-toggle v-model="form.networkDirection" mandatory divided density="comfortable" class="traffic-toggle">
              <v-btn
                v-for="item in networkDirectionItems"
                :key="item.value"
                :value="item.value"
                class="text-none"
              >
                {{ item.title }}
              </v-btn>
            </v-btn-toggle>
          </div>
          <v-select
            v-if="form.kind !== 'placeholder'"
            v-model="form.serverIds"
            :items="serverOptions"
            :label="t('serversPage.servers')"
            :placeholder="t('overviewPage.allServers')"
            variant="outlined"
            density="comfortable"
            multiple
            chips
            closable-chips
            clearable
            :hint="t('overviewPage.leaveEmptyServers')"
            persistent-hint
          />
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <v-btn variant="text" class="text-none" @click="dialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="primary" variant="flat" class="text-none" @click="saveCard">{{ t('common.save') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>

<style scoped>
.overview-workspace { flex: 1 1 auto; min-height: 0; }

.overview-actions-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 14px;
  align-items: start;
}

.overview-signals {
  display: grid;
  grid-template-columns: repeat(3, minmax(160px, 1fr));
  gap: 10px;
  min-width: 0;
}

.overview-signal {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
  min-height: 58px;
  padding: 10px 12px;
  border: 1px solid color-mix(in srgb, var(--lp-border), transparent 10%);
  border-radius: 8px;
  background: color-mix(in srgb, var(--lp-surface), transparent 10%);
  box-shadow: var(--lp-shadow-sm);
}

.overview-signal__icon {
  display: grid;
  place-items: center;
  width: 34px;
  height: 34px;
  border-radius: 8px;
  flex: 0 0 auto;
}

.overview-signal__value,
.overview-signal__label {
  display: block;
}

.overview-signal__value {
  color: var(--lp-text);
  font-size: 1.05rem;
  font-weight: 760;
  line-height: 1.1;
}

.overview-signal__label {
  margin-top: 3px;
  color: var(--lp-text-muted);
  font-size: 0.78rem;
}

.overview-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  flex-wrap: wrap;
}

.dashboard-grid {
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
  grid-auto-rows: 132px;
  grid-auto-flow: dense;
  flex: 1 1 auto;
  gap: 16px;
  min-height: 0;
  align-items: stretch;
  align-content: start;
  overflow: auto;
  padding-right: 4px;
}

.dashboard-card {
  position: relative;
  display: flex;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
  flex-direction: column;
  background:
    linear-gradient(180deg, color-mix(in srgb, var(--lp-surface-container), transparent 36%), var(--lp-surface) 70%) !important;
  transition: border-color 0.18s ease, box-shadow 0.18s ease, transform 0.18s ease !important;
}

.dashboard-card:hover {
  transform: translateY(-1px);
}

.card-accent {
  position: absolute;
  inset: 0 0 auto;
  height: 3px;
  opacity: 0.82;
}

.card-accent--primary { background: rgb(var(--v-theme-primary)); }
.card-accent--success { background: rgb(var(--v-theme-success)); }
.card-accent--warning { background: rgb(var(--v-theme-warning)); }
.card-accent--info { background: rgb(var(--v-theme-info)); }
.card-accent--secondary { background: rgb(var(--v-theme-secondary)); }

.dashboard-card--placeholder {
  background: transparent !important;
  border-color: transparent !important;
  box-shadow: none !important;
}

.dashboard-card--editing {
  border: 1px dashed rgba(var(--v-theme-primary), 0.55) !important;
  background: color-mix(in srgb, var(--lp-surface), transparent 18%);
}

.dashboard-card--placeholder.dashboard-card--editing {
  background: transparent !important;
}

.dashboard-card--dragging {
  opacity: 0.48;
  transform: scale(0.99);
}

.dashboard-card--drag-over {
  border-color: rgb(var(--v-theme-primary)) !important;
  background: rgba(var(--v-theme-primary), 0.06);
}

.card-tools,
.placeholder-tools {
  display: flex;
  align-items: center;
  gap: 2px;
}

.placeholder-tools {
  position: absolute;
  top: 8px;
  right: 8px;
}

.card-drag-handle {
  cursor: grab;
}

.card-drag-handle:active {
  cursor: grabbing;
}

.placeholder-card-body {
  flex: 1 1 auto;
}

.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 12px 14px 8px;
  flex: 0 0 auto;
}

.card-title {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.card-icon {
  display: grid;
  place-items: center;
  width: 34px;
  height: 34px;
  border-radius: 8px;
  flex: 0 0 auto;
  transition: transform 0.18s ease;
}

.dashboard-card:hover .card-icon {
  transform: translateY(-1px);
}

.surface-primary { color: rgb(var(--v-theme-primary)); background: rgba(var(--v-theme-primary), 0.1); }
.surface-success { color: rgb(var(--v-theme-success)); background: rgba(var(--v-theme-success), 0.1); }
.surface-warning { color: rgb(var(--v-theme-warning)); background: rgba(var(--v-theme-warning), 0.12); }
.surface-info { color: rgb(var(--v-theme-info)); background: rgba(var(--v-theme-info), 0.1); }

.card-body {
  min-height: 0;
  flex: 1 1 auto;
  padding: 0 14px 14px;
  overflow: auto;
}

.chart-body {
  display: flex;
  flex-direction: column;
}

.chart {
  width: 100%;
  min-height: 0;
  flex: 1 1 auto;
}

.message-list {
  display: grid;
  align-content: start;
  gap: 8px;
}

.message-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 10px;
  align-items: center;
  padding: 10px;
  border: 1px solid var(--lp-border);
  border-radius: 8px;
  background: color-mix(in srgb, var(--lp-surface-container), transparent 30%);
  cursor: pointer;
  transition: background-color 0.16s ease, border-color 0.16s ease, transform 0.16s ease;
}

.message-row:hover {
  border-color: rgba(var(--v-theme-primary), 0.24);
  background: rgba(var(--v-theme-primary), 0.05);
  transform: translateX(2px);
}

.empty-card-text {
  display: grid;
  place-items: center;
  min-height: 84px;
  color: var(--lp-text-muted);
  font-size: 0.88rem;
}

.empty-state {
  flex: 1 1 auto;
  min-height: 360px;
  border: 1px solid var(--lp-border);
  background:
    linear-gradient(180deg, color-mix(in srgb, var(--lp-surface-container), transparent 24%), var(--lp-surface));
  box-shadow: var(--lp-shadow-sm);
}

.size-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.traffic-toggle {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  width: 100%;
}

.min-width-0 {
  min-width: 0;
}

@media (max-width: 1180px) {
  .overview-actions-row {
    grid-template-columns: 1fr;
  }

  .dashboard-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }

  .dashboard-card {
    grid-column: span 3 !important;
  }
}

@media (max-width: 760px) {
  .overview-workspace,
  .dashboard-grid {
    flex: none;
    min-height: auto;
  }

  .overview-signals {
    grid-template-columns: 1fr;
  }

  .overview-actions,
  .overview-actions .v-btn {
    width: 100%;
  }

  .dashboard-grid {
    grid-template-columns: 1fr;
    grid-auto-rows: 150px;
    overflow: visible;
    padding-right: 0;
  }

  .dashboard-card {
    grid-column: span 1 !important;
  }

  .size-grid {
    grid-template-columns: 1fr;
  }
}
</style>
