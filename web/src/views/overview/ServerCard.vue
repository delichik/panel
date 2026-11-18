<script setup lang="ts">
import { computed, type Component } from 'vue';
import { useRouter } from 'vue-router';
import { Activity, ChevronRight, CircleGauge, Download, HardDrive, Layers, Network, Package, Radar, Server, UploadCloud } from '@/icons';
import Badge from '@/components/ui/Badge.vue';
import IconButton from '@/components/ui/IconButton.vue';
import { useI18n } from '@/i18n';
import type { ServerCardView } from './model';
import MetricCell from './MetricCell.vue';
import {
  diskPercent,
  formatBytes,
  formatLatency,
  formatLoad,
  formatPacketLoss,
  formatPercent,
  formatRate,
  memoryPercent,
  percentTone,
  valueTone,
  type HistoryKind,
  type MetricTone,
} from './model';

const props = defineProps<{ card: ServerCardView }>();

const { t } = useI18n();
const router = useRouter();

const statusLabel = computed(() => {
  const key = `overviewPage.node${props.card.status.charAt(0).toUpperCase()}${props.card.status.slice(1)}`;
  return t(key);
});

const metricsUsable = computed(() => props.card.status === 'online');
const memoryPct = computed(() => (metricsUsable.value ? memoryPercent(props.card.metrics) : null));
const diskPct = computed(() => (metricsUsable.value ? diskPercent(props.card.metrics) : null));
const probeStale = computed(() => props.card.probeStale);
const probeMissing = computed(() => !props.card.probeAvailable);

const statusDotClass: Record<MetricTone, string> = {
  success: 'bg-success',
  warning: 'bg-warning',
  danger: 'bg-danger',
};
const textToneClass: Record<MetricTone, string> = {
  success: 'text-success',
  warning: 'text-warning',
  danger: 'text-danger',
};

interface CellView {
  label: string;
  icon: Component;
  text: string;
  valueClass: string;
  muted: boolean;
  empty?: boolean;
  placeholder?: boolean;
  plain?: boolean;
  hint?: string;
  ariaLabel: string;
  kind?: HistoryKind;
  history?: Array<number | null>;
  percent?: number | null;
}

function historyCell(icon: Component, kind: HistoryKind, label: string, value: number | null, text: string, history: Array<number | null>, muted: boolean): CellView {
  const tone = muted || value === null ? null : valueTone(kind, value);
  return {
    label,
    icon,
    text,
    muted: muted || value === null,
    ariaLabel: `${label} · ${t('overviewPage.last24Hours')}`,
    kind,
    history,
    valueClass: tone ? textToneClass[tone] : 'text-muted-foreground',
  };
}

function plainCell(icon: Component, kind: HistoryKind, label: string, value: number | null, text: string, muted: boolean): CellView {
  const tone = muted || value === null ? null : valueTone(kind, value);
  return {
    label,
    icon,
    text,
    muted: muted || value === null,
    plain: true,
    ariaLabel: label,
    valueClass: tone ? textToneClass[tone] : 'text-muted-foreground',
  };
}

function meterCell(icon: Component, label: string, percent: number | null, hint: string): CellView {
  const tone = percent === null ? null : percentTone(percent);
  return {
    label,
    icon,
    text: percent === null ? '—' : formatPercent(percent, 0),
    muted: percent === null,
    hint,
    ariaLabel: `${label}: ${percent === null ? '—' : formatPercent(percent, 0)}`,
    percent,
    valueClass: tone ? textToneClass[tone] : 'text-muted-foreground',
  };
}

const cpuCell = computed<CellView>(() => historyCell(
  CircleGauge,
  'cpu',
  t('overviewPage.metricCpu'),
  metricsUsable.value ? props.card.metrics.cpuPercent : null,
  metricsUsable.value ? formatPercent(props.card.metrics.cpuPercent, 1) : '—',
  props.card.cpuHistory,
  !metricsUsable.value,
));

const loadCell = computed<CellView>(() => plainCell(
  Activity,
  'load',
  t('overviewPage.metricLoad'),
  metricsUsable.value ? props.card.metrics.load1 : null,
  metricsUsable.value ? formatLoad(props.card.metrics.load1) : '—',
  !metricsUsable.value,
));

const latencyCell = computed<CellView>(() => (
  props.card.isProbeSource
    ? {
      label: t('overviewPage.metricLatency'),
      icon: Radar,
      text: '—',
      valueClass: 'text-muted-foreground',
      muted: true,
      placeholder: true,
      ariaLabel: t('overviewPage.probeSourceMetricHint'),
    }
    : historyCell(
      Radar,
      'latency',
      t('overviewPage.metricLatency'),
      probeMissing.value ? null : props.card.probeLatencyMs,
      probeMissing.value ? '—' : formatLatency(props.card.probeLatencyMs),
      props.card.latencyHistory,
      probeStale.value,
    )
));

const lossCell = computed<CellView>(() => (
  props.card.isProbeSource
    ? {
      label: t('overviewPage.metricPacketLoss'),
      icon: Network,
      text: '—',
      valueClass: 'text-muted-foreground',
      muted: true,
      placeholder: true,
      ariaLabel: t('overviewPage.probeSourceMetricHint'),
    }
    : historyCell(
      Network,
      'packetLoss',
      t('overviewPage.metricPacketLoss'),
      probeMissing.value ? null : props.card.probePacketLossPercent,
      probeMissing.value ? '—' : formatPacketLoss(props.card.probePacketLossPercent),
      props.card.lossHistory,
      probeStale.value,
    )
));

const diskReadCell = computed<CellView>(() => plainCell(
  Download,
  'diskRead',
  t('overviewPage.metricDiskRead'),
  metricsUsable.value ? props.card.metrics.diskReadBytesPerSecond : null,
  metricsUsable.value ? formatRate(props.card.metrics.diskReadBytesPerSecond) : '—',
  !metricsUsable.value,
));

const diskWriteCell = computed<CellView>(() => plainCell(
  UploadCloud,
  'diskWrite',
  t('overviewPage.metricDiskWrite'),
  metricsUsable.value ? props.card.metrics.diskWriteBytesPerSecond : null,
  metricsUsable.value ? formatRate(props.card.metrics.diskWriteBytesPerSecond) : '—',
  !metricsUsable.value,
));

const memoryCell = computed<CellView>(() => meterCell(
  Server,
  t('overviewPage.metricMemory'),
  memoryPct.value,
  `${formatBytes(props.card.metrics.memoryUsedBytes)} / ${formatBytes(props.card.metrics.memoryTotalBytes)}`,
));

const diskCell = computed<CellView>(() => meterCell(
  HardDrive,
  t('overviewPage.metricDisk'),
  diskPct.value,
  `${formatBytes(props.card.metrics.diskUsedBytes)} / ${formatBytes(props.card.metrics.diskTotalBytes)}`,
));

const networkDownCell = computed<CellView>(() => plainCell(
  Download,
  'load',
  t('overviewPage.metricNetworkDown'),
  metricsUsable.value ? props.card.metrics.rxBytesPerSecond : null,
  metricsUsable.value ? formatRate(props.card.metrics.rxBytesPerSecond) : '—',
  !metricsUsable.value,
));

const networkUpCell = computed<CellView>(() => plainCell(
  UploadCloud,
  'load',
  t('overviewPage.metricNetworkUp'),
  metricsUsable.value ? props.card.metrics.txBytesPerSecond : null,
  metricsUsable.value ? formatRate(props.card.metrics.txBytesPerSecond) : '—',
  !metricsUsable.value,
));

const rowPairs = computed<Array<[CellView, CellView]>>(() => [
  [cpuCell.value, loadCell.value],
  [memoryCell.value, diskCell.value],
  [diskReadCell.value, diskWriteCell.value],
  [networkDownCell.value, networkUpCell.value],
  [latencyCell.value, lossCell.value],
]);

function openDetails() {
  void router.push({ path: '/servers', query: { server: props.card.server.id } });
}

function openPackages() {
  void router.push({ path: '/servers', query: { server: props.card.server.id, tab: 'packages' } });
}

function openImages() {
  void router.push({ path: '/servers', query: { server: props.card.server.id, tab: 'resources', subtab: 'images' } });
}
</script>

<template>
  <article
    class="motion-reveal grid min-w-0 content-start gap-3 surface-card p-4"
    data-testid="server-card"
  >
    <header class="flex min-w-0 items-start justify-between gap-2">
      <div class="flex min-w-0 items-start gap-2">
        <span class="mt-1.5 size-2 shrink-0 rounded-full" :class="statusDotClass[card.statusTone]" :title="statusLabel" aria-hidden="true" />
        <span class="sr-only">{{ statusLabel }}</span>
        <div class="flex min-w-0 items-baseline gap-2">
          <h3 class="m-0 max-w-full truncate text-body-strong font-semibold text-foreground">{{ card.server.name }}</h3>
          <span v-if="card.isProbeSource" class="shrink-0 text-caption font-medium uppercase tracking-wide text-muted-foreground">{{ t('overviewPage.probeSourceBadge') }}</span>
        </div>
      </div>
      <div class="flex shrink-0 items-center gap-1">
        <button
          v-if="card.packageUpdates"
          type="button"
          class="inline-flex min-h-8 items-center justify-center rounded-control px-1 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring max-lg:min-h-10"
          :title="t('overviewPage.packageUpdatesHint', { count: card.packageUpdates })"
          data-testid="package-updates-badge"
          @click.stop="openPackages"
        >
          <Badge tone="warning"><Package class="size-3" aria-hidden="true" />{{ card.packageUpdates }}</Badge>
        </button>
        <button
          v-if="card.imageUpdates"
          type="button"
          class="inline-flex min-h-8 items-center justify-center rounded-control px-1 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring max-lg:min-h-10"
          :title="t('overviewPage.imageUpdatesHint', { count: card.imageUpdates })"
          data-testid="image-updates-badge"
          @click.stop="openImages"
        >
          <!-- 容器镜像更新：用 Layers（镜像层）语义图标，避免与图片/相册图标或 Package 软件包徽标混淆。 -->
          <Badge tone="info"><Layers class="size-3" aria-hidden="true" />{{ card.imageUpdates }}</Badge>
        </button>
        <IconButton size="sm" :label="t('common.details')" data-testid="server-card-details" @click.stop="openDetails">
          <ChevronRight aria-hidden="true" />
        </IconButton>
      </div>
    </header>

    <div class="grid min-w-0 gap-3">
      <div v-for="(pair, index) in rowPairs" :key="index" class="grid min-w-0 grid-cols-2 gap-x-5">
        <MetricCell v-bind="pair[0]" />
        <MetricCell v-bind="pair[1]" />
      </div>
    </div>
  </article>
</template>
