<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
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
import type {
  ApplicationDto,
  MetricsRange,
  MetricsSeriesDto,
  OverviewCardDataDto,
  OverviewCardDto,
  OverviewCardKind,
  OverviewCardNetworkDirection,
  OverviewDto,
  OverviewServerDto,
} from '@/types/api';
import AppActionButton from '@/components/AppActionButton.vue';
import AppActionGroup from '@/components/AppActionGroup.vue';
import PageLoadingState from '@/components/PageLoadingState.vue';

use([CanvasRenderer, LineChart, GridComponent, TooltipComponent, LegendComponent]);

type CardKind = OverviewCardKind;
type NetworkDirection = OverviewCardNetworkDirection;
type OverviewCardConfig = OverviewCardDto;

interface CardPreset {
  kind: CardKind;
  icon: string;
  color: string;
  category: 'metric' | 'message';
  width: number;
  height: number;
}

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
const cards = ref<OverviewCardConfig[]>([]);
const cardDataById = ref<Record<string, OverviewCardDataDto | null>>({});
const loading = ref(false);
const error = ref('');
const saveError = ref('');
const dialog = ref(false);
const editingCardId = ref('');
const editMode = ref(false);
const draggingCardId = ref('');
const dragOverCardId = ref('');
let refreshTimer: number | undefined;
let saveQueue = Promise.resolve();
let cardsLoaded = false;

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

const presetItems = computed(() => cardPresets.map((preset) => ({
  ...preset,
  title: cardTitle(preset.kind),
})));

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

async function loadData() {
  loading.value = true;
  try {
    const cardRequest = cardsLoaded ? Promise.resolve(null) : overviewApi.getCards();
    const [nextOverview, nextApplications, cardConfiguration] = await Promise.all([
      overviewApi.getOverview(),
      applicationsApi.list(),
      cardRequest,
    ]);
    overview.value = nextOverview;
    applications.value = nextApplications;
    if (cardConfiguration) {
      cards.value = cardConfiguration.cards;
      cardsLoaded = true;
    }
    error.value = '';
    await loadCardMetrics();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('overviewPage.loadFailed');
  } finally {
    loading.value = false;
  }
}

async function loadCardMetrics() {
  const metricCards = cards.value.filter(isMetricCard);
  const activeCardIds = new Set(metricCards.map((card) => card.id));
  cardDataById.value = Object.fromEntries(Object.entries(cardDataById.value).filter(([cardId]) => activeCardIds.has(cardId)));
  await Promise.all(metricCards.map(async (card) => {
    try {
      cardDataById.value[card.id] = await overviewApi.getCardData(card.id);
    } catch {
      cardDataById.value[card.id] = null;
    }
  }));
}

function persistCards() {
  const snapshot = cards.value.map((card) => ({ ...card, serverIds: [...card.serverIds] }));
  saveQueue = saveQueue.then(async () => {
    try {
      await overviewApi.updateCards({ cards: snapshot });
      saveError.value = '';
    } catch (err) {
      saveError.value = err instanceof Error ? err.message : t('overviewPage.saveFailed');
    }
  });
  return saveQueue;
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
  const series = cardDataById.value[card.id]?.metricsByServer[server.id] as MetricsSeriesDto | undefined;
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
  void loadCardMetrics();
  void persistCards();
}

function removeCard(cardId: string) {
  cards.value = cards.value.filter((card) => card.id !== cardId);
  void loadCardMetrics();
  void persistCards();
}

function resetCards() {
  cards.value = defaultCards();
  void loadCardMetrics();
  void persistCards();
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
  const shouldPersist = Boolean(draggingCardId.value);
  draggingCardId.value = '';
  dragOverCardId.value = '';
  if (shouldPersist) void persistCards();
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
  const currentInsertAfter = sourceIndex > targetIndex;
  if (Math.abs(sourceIndex - targetIndex) === 1 && currentInsertAfter === insertAfter) return;
  const [movingCard] = next.splice(sourceIndex, 1);
  const nextTargetIndex = next.findIndex((card) => card.id === targetId);
  const insertIndex = nextTargetIndex + (insertAfter ? 1 : 0);
  if (sourceIndex === insertIndex || sourceIndex + 1 === insertIndex) return;
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
      <AppActionGroup context="page" class="overview-actions">
        <template v-if="editMode">
          <AppActionButton icon="mdi-restore" :label="t('overviewPage.reset')" @click="resetCards" />
          <AppActionButton kind="primary" icon="mdi-view-grid-plus" :label="t('overviewPage.addCard')" @click="openAddDialog()" />
        </template>
        <AppActionButton
          :kind="editMode ? 'primary' : 'secondary'"
          :icon="editMode ? 'mdi-check' : 'mdi-pencil'"
          :label="editMode ? t('common.done') : t('common.edit')"
          @click="toggleEditMode"
        />
      </AppActionGroup>
    </div>

    <v-alert v-if="error || saveError" type="error" variant="tonal">{{ error || saveError }}</v-alert>

    <PageLoadingState v-if="loading && overview.servers.length === 0" min-height="360px" />

    <div v-else-if="overview.servers.length === 0" class="empty-state">
      <v-icon size="44" color="primary">mdi-server-network-off</v-icon>
      <div>
        <div class="text-subtitle-1 font-weight-bold">{{ t('overviewPage.noServersConnected') }}</div>
        <div class="text-body-2 text-medium-emphasis">{{ t('overviewPage.addServerHint') }}</div>
      </div>
      <AppActionButton kind="primary" icon="mdi-plus" :label="t('common.addServer')" @click="router.push('/servers')" />
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
            <AppActionButton
              class="card-drag-handle"
              kind="tool"
              icon="mdi-drag"
              :label="t('overviewPage.moveCard')"
              draggable="true"
              @dragstart="startCardDrag(card.id, $event)"
              @dragend="endCardDrag"
            />
            <v-menu location="bottom end">
              <template #activator="{ props }">
                <AppActionButton v-bind="props" kind="tool" icon="mdi-dots-vertical" :label="t('common.more')" />
              </template>
              <v-list density="compact">
                <v-list-item prepend-icon="mdi-pencil" :title="t('common.edit')" @click="openEditDialog(card)" />
                <v-list-item prepend-icon="mdi-delete" :title="t('common.remove')" class="text-error" @click="removeCard(card.id)" />
              </v-list>
            </v-menu>
          </div>
        </div>

          <div v-if="card.kind === 'placeholder' && editMode" class="placeholder-tools">
            <AppActionButton
              class="card-drag-handle"
              kind="tool"
              icon="mdi-drag"
              :label="t('overviewPage.moveCard')"
              draggable="true"
              @dragstart="startCardDrag(card.id, $event)"
              @dragend="endCardDrag"
            />
            <v-menu location="bottom end">
              <template #activator="{ props }">
                <AppActionButton v-bind="props" kind="tool" icon="mdi-dots-vertical" :label="t('common.more')" />
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
            @click="router.push('/resources/packages')"
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
          <AppActionButton kind="tool" icon="mdi-close" :label="t('common.close')" @click="dialog = false" />
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
          <AppActionGroup context="dialog">
            <AppActionButton kind="plain" :label="t('common.cancel')" @click="dialog = false" />
            <AppActionButton kind="primary" :label="t('common.save')" @click="saveCard" />
          </AppActionGroup>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>

<style scoped>
.overview-workspace {
  flex: 1 1 auto;
  min-height: 0;
  gap: 14px;
}

.overview-actions-row {
  justify-content: flex-end;
  min-height: 0;
}

.dashboard-grid {
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
  grid-auto-rows: 124px;
  grid-auto-flow: dense;
  flex: 1 1 auto;
  gap: 14px;
  min-height: 0;
  align-items: stretch;
  align-content: start;
  overflow: auto;
  padding: 16px;
  border: 1px solid color-mix(in srgb, var(--lp-border), transparent 8%);
  border-radius: var(--lp-radius-lg);
  background:
    linear-gradient(180deg, color-mix(in srgb, var(--lp-surface-container), transparent 42%), transparent 52%),
    color-mix(in srgb, var(--lp-surface), transparent 8%);
  box-shadow: var(--lp-shadow-sm);
}

.dashboard-card {
  position: relative;
  display: flex;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
  flex-direction: column;
  background:
    linear-gradient(180deg, color-mix(in srgb, var(--lp-surface-container), transparent 34%), var(--lp-surface) 72%) !important;
  border-color: color-mix(in srgb, var(--lp-border), transparent 6%) !important;
  transition: border-color 0.18s ease, box-shadow 0.18s ease, transform 0.18s ease !important;
}

.dashboard-card:hover {
  transform: translateY(-1px);
  border-color: color-mix(in srgb, rgb(var(--v-theme-primary)), var(--lp-border) 80%) !important;
}

.card-accent {
  position: absolute;
  inset: 14px auto 14px 0;
  width: 3px;
  height: auto;
  border-radius: 0 99px 99px 0;
  opacity: 0.92;
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
  background: color-mix(in srgb, var(--lp-surface), transparent 12%) !important;
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
  background: rgba(var(--v-theme-primary), 0.06) !important;
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
  padding: 13px 14px 8px 16px;
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
  width: 32px;
  height: 32px;
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
  padding: 0 14px 14px 16px;
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
