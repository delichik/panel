<script setup lang="ts">
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { Activity, AlertTriangle, BarChart3, Boxes, Check, Grip, Gauge, Pencil, Plus, RefreshCcw, ShieldCheck, Server, Trash2 } from '@lucide/vue';
import { overviewApi } from '@/api/overview';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import Dialog from '@/components/ui/Dialog.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import Select from '@/components/ui/Select.vue';
import Skeleton from '@/components/ui/Skeleton.vue';
import ServerMultiPicker from '@/components/patterns/ServerMultiPicker.vue';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import { useI18n } from '@/i18n';
import type { OverviewCardConfiguration, OverviewCardData, OverviewCardKind, OverviewCardRange, OverviewDto, OverviewMetricPoint, OverviewMetricsSeries } from '@/types/overview';
import { cardHasData, createOverviewCard, defaultOverviewCards, overviewRisks, summarizeOverview } from './model';

const MetricLineChart = defineAsyncComponent(() => import('./MetricLineChart.vue'));

const { t } = useI18n();
const router = useRouter();

const overview = ref<OverviewDto>({ servers: [] });
const cards = ref<OverviewCardConfiguration[]>([]);
const persistedCardIds = ref<Set<string>>(new Set());
const cardLoading = ref<Record<string, boolean>>({});
const cardErrors = ref<Record<string, string>>({});
const cardValues = ref<Record<string, string>>({});
const cardData = ref<Record<string, OverviewCardData>>({});
const loading = ref(false);
const pageError = ref('');
const saveError = ref('');
const editMode = ref(false);
const cardEditorOpen = ref(false);
const editingCardId = ref<string | null>(null);
const saving = ref(false);
const newCardKind = ref<OverviewCardKind>('cpu');
const draggingCardId = ref<string | null>(null);
const dragPreviewTargetId = ref<string | null>(null);
const overviewGrid = ref<HTMLElement | null>(null);
const resizeState = ref<{
  cardId: string;
  startX: number;
  startY: number;
  startWidth: number;
  startHeight: number;
  columnWidth: number;
  rowHeight: number;
} | null>(null);

const summary = computed(() => summarizeOverview(overview.value));
const risks = computed(() => overviewRisks(overview.value.servers));
const editingCard = computed(() => cards.value.find((card) => card.id === editingCardId.value));
const serverOptions = computed(() => overview.value.servers.map((server) => ({
  id: server.id,
  name: server.name,
  description: server.host,
  status: server.reachable ? 'reachable' : 'unreachable',
})));

const kindOptions = computed(() => [
  { value: 'cpu', label: t('overviewPage.cardCpu') },
  { value: 'memory', label: t('overviewPage.cardMemory') },
  { value: 'disk', label: t('overviewPage.cardDisk') },
  { value: 'network', label: t('overviewPage.cardNetwork') },
  { value: 'packageUpdates', label: t('overviewPage.cardPackages') },
  { value: 'containerUpdates', label: t('overviewPage.cardContainers') },
]);

const rangeOptions = [
  { value: '1h', label: '1h' },
  { value: '6h', label: '6h' },
  { value: '1d', label: '1d' },
  { value: '7d', label: '7d' },
];

const directionOptions = computed(() => [
  { value: 'both', label: t('overviewPage.rxTx') },
  { value: 'rx', label: t('overviewPage.rx') },
  { value: 'tx', label: t('overviewPage.tx') },
]);
const widthOptions = computed(() => Array.from({ length: 6 }, (_, index) => ({ value: String(index + 1), label: `${index + 1}` })));
const heightOptions = computed(() => Array.from({ length: 4 }, (_, index) => ({ value: String(index + 1), label: `${index + 1}` })));

async function load() {
  loading.value = true;
  pageError.value = '';
  try {
    const [nextOverview, nextCards] = await Promise.all([overviewApi.getOverview(), overviewApi.getCards()]);
    overview.value = nextOverview;
    cards.value = nextCards.cards.map(normalizeCardSize);
    persistedCardIds.value = new Set(cards.value.map((card) => card.id));
    await Promise.all(cards.value.map((card) => loadCard(card.id)));
  } catch (err) {
    pageError.value = err instanceof Error ? err.message : t('common.loadFailed');
  } finally {
    loading.value = false;
  }
}

async function loadCard(cardId: string) {
  const card = cards.value.find((item) => item.id === cardId);
  const nextErrors = { ...cardErrors.value };
  delete nextErrors[cardId];
  cardErrors.value = nextErrors;
  if (!card || !persistedCardIds.value.has(cardId) || card.kind === 'packageUpdates' || card.kind === 'containerUpdates' || card.kind === 'placeholder') {
    const nextData = { ...cardData.value };
    delete nextData[cardId];
    cardData.value = nextData;
    cardValues.value = { ...cardValues.value, [cardId]: derivedCardValue(card) };
    return;
  }
  cardLoading.value = { ...cardLoading.value, [cardId]: true };
  try {
    const data = await overviewApi.getCardData(cardId);
    cardData.value = { ...cardData.value, [cardId]: data };
    cardValues.value = { ...cardValues.value, [cardId]: metricValue(card, data) };
    if (!cardHasData(card, data)) cardErrors.value = { ...cardErrors.value, [cardId]: t('overviewPage.cardEmpty') };
  } catch (err) {
    cardErrors.value = { ...cardErrors.value, [cardId]: err instanceof Error ? err.message : t('overviewPage.cardFailed') };
  } finally {
    const nextLoading = { ...cardLoading.value };
    delete nextLoading[cardId];
    cardLoading.value = nextLoading;
  }
}

function updateCardRange(card: OverviewCardConfiguration, range: string) {
  card.range = range as OverviewCardRange;
  void loadCard(card.id);
}

function addCard() {
  const next = createOverviewCard(newCardKind.value, '1h', defaultWidth(newCardKind.value), defaultHeight(newCardKind.value));
  cards.value = [...cards.value, next];
  void loadCard(next.id);
}

function removeCard(cardId: string) {
  cards.value = cards.value.filter((card) => card.id !== cardId);
  if (editingCardId.value === cardId) closeCardEditor();
}

function resetCards() {
  cards.value = defaultOverviewCards();
  cardErrors.value = {};
  cardData.value = {};
  cardValues.value = Object.fromEntries(cards.value.map((card) => [card.id, derivedCardValue(card)]));
}

async function saveCards() {
  saving.value = true;
  saveError.value = '';
  try {
    const visibleCards = cards.value.map(normalizeCardSize);
    const saved = await overviewApi.updateCards({ cards: visibleCards });
    cards.value = saved.cards.map(normalizeCardSize);
    persistedCardIds.value = new Set(cards.value.map((card) => card.id));
    editMode.value = false;
    closeCardEditor();
    await Promise.all(cards.value.map((card) => loadCard(card.id)));
  } catch (err) {
    saveError.value = err instanceof Error ? err.message : t('overviewPage.saveFailed');
  } finally {
    saving.value = false;
  }
}

function toggleEditMode() {
  saveError.value = '';
  if (!editMode.value) {
    editMode.value = true;
    return;
  }
  void saveCards();
}

function openCardEditor(cardId: string) {
  editingCardId.value = cardId;
  cardEditorOpen.value = true;
}

function closeCardEditor() {
  cardEditorOpen.value = false;
  editingCardId.value = null;
}

function onCardDragStart(cardId: string, event: DragEvent) {
  if (!editMode.value) return;
  draggingCardId.value = cardId;
  dragPreviewTargetId.value = cardId;
  if (event.dataTransfer) {
    event.dataTransfer.effectAllowed = 'move';
    event.dataTransfer.setData('text/plain', cardId);
  }
}

function onCardDragEnd() {
  draggingCardId.value = null;
  dragPreviewTargetId.value = null;
}

function onCardDragOver(targetCardId: string, event: DragEvent) {
  if (!editMode.value || !draggingCardId.value) return;
  event.preventDefault();
  if (event.dataTransfer) event.dataTransfer.dropEffect = 'move';
  if (dragPreviewTargetId.value === targetCardId || draggingCardId.value === targetCardId) return;
  const fromIndex = cards.value.findIndex((card) => card.id === draggingCardId.value);
  const toIndex = cards.value.findIndex((card) => card.id === targetCardId);
  if (fromIndex < 0 || toIndex < 0) return;
  const next = [...cards.value];
  const [moved] = next.splice(fromIndex, 1);
  next.splice(toIndex, 0, moved);
  cards.value = next;
  dragPreviewTargetId.value = targetCardId;
}

function onCardDrop() {
  draggingCardId.value = null;
  dragPreviewTargetId.value = null;
}

function startResize(card: OverviewCardConfiguration, event: PointerEvent) {
  if (!editMode.value) return;
  event.preventDefault();
  event.stopPropagation();
  const grid = overviewGrid.value;
  if (!grid) return;
  const styles = window.getComputedStyle(grid);
  const columns = styles.gridTemplateColumns.split(' ').filter(Boolean).length || 6;
  const gap = Number.parseFloat(styles.columnGap || '0') || 0;
  const rowHeight = Number.parseFloat(styles.gridAutoRows || '112') || 112;
  const columnWidth = (grid.clientWidth - gap * Math.max(0, columns - 1)) / columns + gap;
  resizeState.value = {
    cardId: card.id,
    startX: event.clientX,
    startY: event.clientY,
    startWidth: card.width,
    startHeight: card.height,
    columnWidth,
    rowHeight,
  };
  window.addEventListener('pointermove', onResizeMove);
  window.addEventListener('pointerup', stopResize, { once: true });
}

function onResizeMove(event: PointerEvent) {
  const state = resizeState.value;
  if (!state) return;
  const card = cards.value.find((item) => item.id === state.cardId);
  if (!card) return;
  card.width = clampInteger(state.startWidth + Math.round((event.clientX - state.startX) / state.columnWidth), 1, 6);
  card.height = clampInteger(state.startHeight + Math.round((event.clientY - state.startY) / state.rowHeight), 1, 4);
}

function stopResize() {
  window.removeEventListener('pointermove', onResizeMove);
  resizeState.value = null;
}

function cardTitle(card: OverviewCardConfiguration) {
  return t(`overviewPage.card.${card.kind}`);
}

function cardIcon(kind: OverviewCardKind) {
  if (kind === 'cpu') return Gauge;
  if (kind === 'memory') return Activity;
  if (kind === 'disk') return BarChart3;
  if (kind === 'network') return Boxes;
  return AlertTriangle;
}

function cardTone(card: OverviewCardConfiguration) {
  if (cardErrors.value[card.id]) return 'danger' as const;
  if (card.kind === 'packageUpdates' && summary.value.updates > 0) return 'warning' as const;
  return 'neutral' as const;
}

function derivedCardValue(card?: OverviewCardConfiguration) {
  if (!card) return '-';
  if (card.kind === 'packageUpdates') return String(summary.value.updates);
  if (card.kind === 'containerUpdates') return String(overview.value.servers.filter((server) => !server.metricsFresh).length);
  return '-';
}

function metricValue(card: OverviewCardConfiguration, data: Awaited<ReturnType<typeof overviewApi.getCardData>>) {
  const latest = metricSeries(card, data).at(-1);
  if (latest === undefined) return '-';
  if (card.kind === 'network') return formatBytes(latest);
  if (card.kind === 'cpu' || card.kind === 'memory' || card.kind === 'disk') return `${Math.round(latest)}%`;
  return derivedCardValue(card);
}

function cardSecondaryValue(card: OverviewCardConfiguration) {
  const data = cardData.value[card.id];
  if (!data || card.kind === 'packageUpdates' || card.kind === 'containerUpdates') return card.serverIds.length ? t('overviewPage.selectedServers') : t('overviewPage.allServers');
  const values = metricSeries(card, data);
  if (!values.length) return t('common.notAvailable');
  const max = Math.max(...values);
  if (card.kind === 'network') return t('overviewPage.cardPeakValue', { value: formatBytes(max) });
  return t('overviewPage.cardPeakValue', { value: `${Math.round(max)}%` });
}

function cardMeta(card: OverviewCardConfiguration) {
  const size = `${card.width}x${card.height}`;
  const scope = card.serverIds.length
    ? t('overviewPage.selectedServerCount', { count: card.serverIds.length })
    : t('overviewPage.allServerCount', { count: overview.value.servers.length });
  return `${size} / ${card.range} / ${scope}`;
}

function shouldShowChart(card: OverviewCardConfiguration) {
  return card.width >= 2 && card.height >= 2 && !['packageUpdates', 'containerUpdates', 'placeholder'].includes(card.kind);
}

function isMetricCard(card: OverviewCardConfiguration) {
  return !['packageUpdates', 'containerUpdates', 'placeholder'].includes(card.kind);
}

function shouldShowDetail(card: OverviewCardConfiguration) {
  return card.width >= 3 || card.height >= 3;
}

function metricSeries(card: OverviewCardConfiguration, data?: OverviewCardData) {
  const seriesByServer = Object.values(data?.metricsByServer ?? {}).map((series) => metricValues(card, series)).filter((series) => series.length > 0);
  const maxLength = Math.max(0, ...seriesByServer.map((series) => series.length));
  return Array.from({ length: maxLength }, (_, index) => {
    const values = seriesByServer.map((series) => series[index]).filter((value) => value !== undefined);
    return values.length ? values.reduce((sum, value) => sum + value, 0) / values.length : 0;
  });
}

function metricValues(card: OverviewCardConfiguration, series: OverviewMetricsSeries) {
  if (card.kind === 'cpu') return (series.cpu ?? []).map((point) => point.usagePercent ?? 0);
  if (card.kind === 'memory') return (series.memory ?? []).map(percentUsed);
  if (card.kind === 'disk') return (series.disk ?? []).map(percentUsed);
  if (card.kind === 'network') {
    return (series.network ?? []).map((point) => {
      if (card.networkDirection === 'rx') return point.rxBytesPerSecond ?? 0;
      if (card.networkDirection === 'tx') return point.txBytesPerSecond ?? 0;
      return (point.rxBytesPerSecond ?? 0) + (point.txBytesPerSecond ?? 0);
    });
  }
  return [];
}

function cardChartSeries(card: OverviewCardConfiguration) {
  const data = cardData.value[card.id];
  return Object.entries(data?.metricsByServer ?? {}).map(([serverId, series]) => ({
    id: serverId,
    name: overview.value.servers.find((server) => server.id === serverId)?.name ?? serverId,
    values: metricValues(card, series),
  })).filter((series) => series.values.length > 0);
}

function cardChartLabels(card: OverviewCardConfiguration) {
  const data = cardData.value[card.id];
  const first = Object.values(data?.metricsByServer ?? {})[0];
  if (!first) return [];
  if (card.kind === 'cpu') return (first.cpu ?? []).map((point) => point.time);
  if (card.kind === 'memory') return (first.memory ?? []).map((point) => point.time);
  if (card.kind === 'disk') return (first.disk ?? []).map((point) => point.time);
  if (card.kind === 'network') return (first.network ?? []).map((point) => point.time);
  return [];
}

function cardChartValueKind(card: OverviewCardConfiguration) {
  return card.kind === 'network' ? 'bytes' as const : 'percent' as const;
}

function percentUsed(point: OverviewMetricPoint) {
  return point.totalBytes ? ((point.usedBytes ?? 0) / point.totalBytes) * 100 : 0;
}

function normalizeCardSize(card: OverviewCardConfiguration): OverviewCardConfiguration {
  return {
    ...card,
    width: clampInteger(card.width, 1, 6),
    height: clampInteger(card.height, 1, 4),
    serverIds: [...card.serverIds],
  };
}

function clampInteger(value: number, min: number, max: number) {
  const next = Number.isFinite(value) ? Math.round(value) : min;
  return Math.min(max, Math.max(min, next));
}

function updateCardWidth(card: OverviewCardConfiguration, value: string) {
  card.width = clampInteger(Number(value), 1, 6);
}

function updateCardHeight(card: OverviewCardConfiguration, value: string) {
  card.height = clampInteger(Number(value), 1, 4);
}

function updateCardKind(card: OverviewCardConfiguration, value: string) {
  card.kind = value as OverviewCardKind;
  card.width = defaultWidth(card.kind);
  card.height = defaultHeight(card.kind);
  void loadCard(card.id);
}

function defaultWidth(kind: OverviewCardKind) {
  if (kind === 'network') return 6;
  if (kind === 'packageUpdates' || kind === 'containerUpdates') return 2;
  return 3;
}

function defaultHeight(kind: OverviewCardKind) {
  if (kind === 'packageUpdates' || kind === 'containerUpdates') return 1;
  return 2;
}

function formatBytes(value: number) {
  if (!value) return '0 B/s';
  if (value < 1024 * 1024) return `${Math.round(value / 1024)} KB/s`;
  return `${(value / 1024 / 1024).toFixed(1)} MB/s`;
}

onMounted(load);
onBeforeUnmount(stopResize);
</script>

<template>
  <ConsolePage :title="t('routes.overview.title')" :description="t('routes.overview.description')">
    <template #actions>
      <Button size="sm" :loading="loading" @click="load">
        <RefreshCcw />
        {{ t('common.refresh') }}
      </Button>
      <Button size="sm" variant="primary" :loading="saving" @click="toggleEditMode">
        <Check v-if="editMode" />
        <Pencil v-else />
        {{ editMode ? t('overviewPage.saveDashboard') : t('overviewPage.editDashboard') }}
      </Button>
    </template>

    <div class="overview-layout grid h-full min-h-0 min-w-0 gap-4 overflow-hidden max-xl:overflow-x-hidden max-xl:overflow-y-auto">
      <main class="grid min-h-0 min-w-0 grid-rows-[auto_minmax(0,1fr)] gap-4">
        <div v-if="pageError" class="rounded-2xl border border-danger-border bg-danger-bg p-4 text-sm text-danger">
          {{ pageError }}
        </div>

        <section class="grid min-w-0 gap-3 [grid-template-columns:repeat(auto-fit,minmax(min(180px,100%),1fr))]">
          <article class="motion-card rounded-2xl border border-border bg-card p-4">
            <span class="text-xs font-medium text-muted-foreground">{{ t('overviewPage.healthSummary') }}</span>
            <strong class="mt-2 block text-2xl text-foreground">{{ summary.reachable }}/{{ summary.total }}</strong>
            <p class="m-0 mt-1 text-xs text-muted-foreground">{{ t('overviewPage.reachableHosts') }}</p>
          </article>
          <article class="motion-card rounded-2xl border border-border bg-card p-4">
            <span class="text-xs font-medium text-muted-foreground">{{ t('overviewPage.capacitySummary') }}</span>
            <strong class="mt-2 block text-2xl text-foreground">{{ summary.fresh }}/{{ summary.total }}</strong>
            <p class="m-0 mt-1 text-xs text-muted-foreground">{{ t('overviewPage.freshMetrics') }}</p>
          </article>
          <article class="motion-card rounded-2xl border border-border bg-card p-4">
            <span class="text-xs font-medium text-muted-foreground">{{ t('overviewPage.taskSummary') }}</span>
            <strong class="mt-2 block text-2xl text-foreground">{{ summary.updates }}</strong>
            <p class="m-0 mt-1 text-xs text-muted-foreground">{{ t('overviewPage.pendingUpdates') }}</p>
          </article>
          <article class="motion-card rounded-2xl border border-border bg-card p-4">
            <span class="text-xs font-medium text-muted-foreground">{{ t('overviewPage.securitySummary') }}</span>
            <strong class="mt-2 block text-2xl text-foreground">{{ summary.supported }}/{{ summary.total }}</strong>
            <p class="m-0 mt-1 text-xs text-muted-foreground">{{ t('overviewPage.supportedHosts') }}</p>
          </article>
        </section>

        <section class="min-h-0 min-w-0 overflow-y-auto overflow-x-hidden rounded-2xl border border-border bg-card p-4">
          <div v-if="loading && cards.length === 0" class="grid min-w-0 gap-3 [grid-template-columns:repeat(auto-fit,minmax(min(240px,100%),1fr))]">
            <Skeleton v-for="item in 6" :key="item" class="h-40" />
          </div>
          <EmptyState v-else-if="overview.servers.length === 0" :title="t('overviewPage.noServersConnected')" :description="t('overviewPage.addServerHint')">
            <template #actions>
              <Button variant="primary" @click="router.push('/servers')">
                <Server />
                {{ t('overviewPage.addServer') }}
              </Button>
            </template>
          </EmptyState>
          <div v-else-if="editMode" class="mb-3 grid grid-cols-[minmax(0,1fr)_auto_auto] gap-2 max-sm:grid-cols-1">
            <Select v-model="newCardKind" :options="kindOptions" />
            <Button @click="addCard"><Plus />{{ t('overviewPage.addCard') }}</Button>
            <Button variant="ghost" @click="resetCards">{{ t('overviewPage.resetCards') }}</Button>
          </div>
          <div v-if="saveError" class="mb-3 rounded-xl border border-danger-border bg-danger-bg p-3 text-sm text-danger">{{ saveError }}</div>
          <div v-if="overview.servers.length > 0" ref="overviewGrid" class="overview-card-grid grid min-w-0 gap-3" :class="editMode ? 'is-editing' : undefined">
            <article
              v-for="card in cards"
              :key="card.id"
              class="motion-card overview-card relative grid min-h-0 grid-rows-[auto_minmax(0,1fr)] rounded-2xl border border-border bg-background p-4"
              :class="[
                card.kind === 'placeholder' ? 'border-dashed bg-muted/30' : '',
                editMode ? 'cursor-move ring-1 ring-primary/20' : '',
                draggingCardId === card.id ? 'opacity-45' : '',
                dragPreviewTargetId === card.id && draggingCardId !== card.id ? 'border-primary ring-2 ring-primary/30' : '',
              ]"
              :style="{ '--overview-card-w': String(card.width), '--overview-card-w-tablet': String(Math.min(card.width, 3)), '--overview-card-h': String(card.height) }"
              :draggable="editMode"
              @dragstart="onCardDragStart(card.id, $event)"
              @dragend="onCardDragEnd"
              @dragover="onCardDragOver(card.id, $event)"
              @drop.prevent="onCardDrop"
            >
              <div v-if="editMode" class="absolute right-3 top-3 z-10 flex items-center gap-1 rounded-xl border border-border bg-popover/95 p-1 shadow-sm">
                <button type="button" class="grid size-7 place-items-center rounded-lg text-muted-foreground hover:bg-accent hover:text-foreground" :title="t('overviewPage.moveCard')">
                  <Grip class="size-4" aria-hidden="true" />
                </button>
                <button type="button" class="grid size-7 place-items-center rounded-lg text-muted-foreground hover:bg-accent hover:text-foreground" :title="t('overviewPage.editCard')" @click.stop="openCardEditor(card.id)">
                  <Pencil class="size-4" aria-hidden="true" />
                </button>
                <button type="button" class="grid size-7 place-items-center rounded-lg text-danger hover:bg-danger-bg" :title="t('overviewPage.deleteCard')" @click.stop="removeCard(card.id)">
                  <Trash2 class="size-4" aria-hidden="true" />
                </button>
              </div>
              <div class="flex items-start justify-between gap-3">
                <div class="flex min-w-0 items-center gap-3">
                  <span class="grid size-9 place-items-center rounded-xl border border-border bg-muted text-muted-foreground">
                    <component :is="cardIcon(card.kind)" class="size-4" aria-hidden="true" />
                  </span>
                  <div class="min-w-0">
                    <h3 class="m-0 truncate text-sm font-semibold text-foreground">{{ cardTitle(card) }}</h3>
                    <p class="m-0 mt-1 text-xs text-muted-foreground">{{ cardMeta(card) }}</p>
                  </div>
                </div>
                <Badge v-if="!editMode" :tone="cardTone(card)">{{ cardErrors[card.id] ? t('overviewPage.partial') : t('overviewPage.live') }}</Badge>
              </div>
              <div class="overview-card-body grid min-h-0 gap-3 overflow-hidden" :class="shouldShowChart(card) ? 'is-chart-primary' : 'is-value-primary'">
                <span v-if="cardLoading[card.id]" class="text-sm text-muted-foreground">{{ t('overviewPage.loadingCard') }}</span>
                <span v-else-if="cardErrors[card.id]" class="line-clamp-2 text-sm leading-6 text-danger">{{ cardErrors[card.id] }}</span>
                <template v-else>
                  <div v-if="shouldShowChart(card) && cardChartSeries(card).length" class="overview-chart-stage min-h-0 min-w-0">
                    <MetricLineChart
                      :labels="cardChartLabels(card)"
                      :series="cardChartSeries(card)"
                      :value-kind="cardChartValueKind(card)"
                    />
                  </div>
                  <div class="overview-card-value min-w-0" :class="shouldShowChart(card) ? 'is-supporting' : 'is-primary'">
                    <span v-if="shouldShowChart(card)" class="text-xs font-medium uppercase text-muted-foreground">{{ t('overviewPage.live') }}</span>
                    <strong class="block truncate text-foreground" :class="shouldShowChart(card) ? 'text-lg' : card.width <= 1 ? 'text-2xl' : 'text-3xl'">{{ cardValues[card.id] || derivedCardValue(card) }}</strong>
                    <span v-if="shouldShowDetail(card) || (isMetricCard(card) && shouldShowChart(card))" class="block truncate text-xs text-muted-foreground">{{ cardSecondaryValue(card) }}</span>
                  </div>
                </template>
              </div>
              <button
                v-if="editMode"
                type="button"
                class="absolute bottom-2 right-2 grid size-8 touch-none place-items-center rounded-xl border border-border bg-popover text-muted-foreground shadow-sm hover:bg-accent hover:text-foreground"
                :title="t('overviewPage.resizeCard')"
                @pointerdown="startResize(card, $event)"
              >
                <Grip class="size-4 rotate-45" aria-hidden="true" />
              </button>
            </article>
          </div>
        </section>
      </main>

      <aside class="min-h-0 min-w-0 overflow-hidden xl:sticky xl:top-0">
        <div class="grid content-start gap-4">
          <section class="min-w-0 rounded-2xl border border-border bg-card p-4">
            <div class="mb-3 flex items-center justify-between">
              <h2 class="m-0 text-sm font-semibold text-foreground">{{ t('overviewPage.riskQueue') }}</h2>
              <Badge :tone="risks.some((item) => item.tone === 'danger') ? 'danger' : risks.length ? 'warning' : 'success'">{{ risks.length }}</Badge>
            </div>
            <div v-if="risks.length" class="grid gap-2">
              <button v-for="risk in risks" :key="risk.id" type="button" class="motion-list-item grid min-w-0 rounded-xl border border-border bg-background p-3 text-left hover:bg-accent" @click="router.push(risk.to)">
                <div class="flex min-w-0 items-center justify-between gap-2">
                  <strong class="truncate text-sm text-foreground">{{ risk.title }}</strong>
                  <Badge class="shrink-0" :tone="risk.tone">{{ t(`overviewPage.risk.${risk.tone}`) }}</Badge>
                </div>
                <span class="mt-1 min-w-0 break-words text-xs leading-5 text-muted-foreground">{{ t(risk.description) }}</span>
              </button>
            </div>
            <p v-else class="m-0 text-sm text-muted-foreground">{{ t('overviewPage.noRisks') }}</p>
          </section>

          <section class="min-w-0 rounded-2xl border border-border bg-card p-4">
            <h2 class="m-0 text-sm font-semibold text-foreground">{{ t('overviewPage.quickEntry') }}</h2>
            <div class="mt-3 grid min-w-0 grid-cols-2 gap-2">
              <Button class="w-full min-w-0 overflow-hidden px-2" variant="secondary" @click="router.push('/servers')"><Server class="shrink-0" /><span class="min-w-0 truncate">{{ t('routes.servers.title') }}</span></Button>
              <Button class="w-full min-w-0 overflow-hidden px-2" variant="secondary" @click="router.push('/credentials')"><ShieldCheck class="shrink-0" /><span class="min-w-0 truncate">{{ t('routes.credentials.title') }}</span></Button>
              <Button class="w-full min-w-0 overflow-hidden px-2" variant="secondary" @click="router.push('/resources/packages')"><Boxes class="shrink-0" /><span class="min-w-0 truncate">{{ t('routes.packages.title') }}</span></Button>
              <Button class="w-full min-w-0 overflow-hidden px-2" variant="secondary" @click="router.push('/application-operations')"><Activity class="shrink-0" /><span class="min-w-0 truncate">{{ t('routes.applicationOperations.title') }}</span></Button>
            </div>
          </section>
        </div>
      </aside>
    </div>

    <Dialog v-model:open="cardEditorOpen" :title="t('overviewPage.editCard')" :description="t('overviewPage.cardEditDescription')" :close-label="t('common.close')">
      <div v-if="editingCard" class="grid gap-3">
        <label class="grid gap-1">
          <span class="text-xs font-medium text-muted-foreground">{{ t('common.type') }}</span>
          <Select :model-value="editingCard.kind" :options="kindOptions" @update:model-value="updateCardKind(editingCard, $event)" />
        </label>
        <div class="grid grid-cols-2 gap-2">
          <label class="grid gap-1">
            <span class="text-xs font-medium text-muted-foreground">{{ t('overviewPage.cardWidth') }}</span>
            <Select :model-value="String(editingCard.width)" :options="widthOptions" @update:model-value="updateCardWidth(editingCard, $event)" />
          </label>
          <label class="grid gap-1">
            <span class="text-xs font-medium text-muted-foreground">{{ t('overviewPage.cardHeight') }}</span>
            <Select :model-value="String(editingCard.height)" :options="heightOptions" @update:model-value="updateCardHeight(editingCard, $event)" />
          </label>
        </div>
        <div class="grid gap-2" :class="editingCard.kind === 'network' ? 'grid-cols-2' : 'grid-cols-1'">
          <label class="grid gap-1">
            <span class="text-xs font-medium text-muted-foreground">{{ t('overviewPage.cardRange') }}</span>
            <Select :model-value="editingCard.range" :options="rangeOptions" @update:model-value="updateCardRange(editingCard, $event)" />
          </label>
          <label v-if="editingCard.kind === 'network'" class="grid gap-1">
            <span class="text-xs font-medium text-muted-foreground">{{ t('overviewPage.cardDirection') }}</span>
            <Select :model-value="editingCard.networkDirection" :options="directionOptions" @update:model-value="editingCard.networkDirection = $event as any" />
          </label>
        </div>
        <div class="grid gap-2">
          <div>
            <span class="text-xs font-medium text-muted-foreground">{{ t('overviewPage.cardServers') }}</span>
            <p class="m-0 mt-1 text-xs leading-5 text-muted-foreground">{{ t('overviewPage.cardServersHint') }}</p>
          </div>
          <ServerMultiPicker v-model="editingCard.serverIds" :servers="serverOptions" :label="t('overviewPage.cardServers')" />
        </div>
      </div>
      <template #footer>
        <Button variant="secondary" @click="closeCardEditor">{{ t('common.close') }}</Button>
      </template>
    </Dialog>
  </ConsolePage>
</template>

<style scoped>
.overview-layout {
  grid-template-columns: minmax(0, 1fr);
}

.overview-card-grid {
  container-type: inline-size;
  grid-auto-flow: dense;
  grid-auto-rows: 112px;
  grid-template-columns: repeat(6, minmax(0, 1fr));
}

.overview-card {
  grid-column: span var(--overview-card-w);
  grid-row: span var(--overview-card-h);
}

.overview-card-body.is-chart-primary {
  grid-template-rows: minmax(0, 1fr) auto;
  align-content: stretch;
}

.overview-card-body.is-value-primary {
  align-content: center;
}

.overview-chart-stage {
  min-height: 96px;
}

.overview-card-value.is-supporting {
  display: grid;
  grid-template-columns: auto minmax(0, max-content) minmax(0, 1fr);
  align-items: baseline;
  gap: 8px;
}

.overview-card-value.is-supporting strong {
  line-height: 1.1;
}

.overview-card-value.is-supporting span:last-child {
  min-width: 0;
}

.overview-card-value.is-primary {
  display: block;
}

@media (max-width: 1024px) {
  .overview-card-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }

  .overview-card {
    grid-column: span var(--overview-card-w-tablet);
  }
}

@media (min-width: 1280px) {
  .overview-layout {
    grid-template-columns: minmax(0, 1fr) minmax(220px, min(28%, 320px));
  }
}

@media (max-width: 640px) {
  .overview-card-grid {
    grid-auto-rows: 108px;
    grid-template-columns: 1fr;
  }

  .overview-card {
    grid-column: span 1;
  }

  .overview-card-value.is-supporting {
    grid-template-columns: minmax(0, 1fr);
    gap: 2px;
  }
}
</style>
