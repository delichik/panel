<script setup lang="ts">
import { computed, type Component } from 'vue';
import { Activity, CalendarDays, History, KeyRound, Router, ServerCog, ShieldCheck, UnlockKeyhole } from '@/icons';
import type { ServerMetricsSeries } from '@/api/servers';
import ArchitectureMark from '@/components/patterns/ArchitectureMark.vue';
import DiskUsageChart from '@/components/patterns/DiskUsageChart.vue';
import OsMark from '@/components/patterns/OsMark.vue';
import Skeleton from '@/components/ui/Skeleton.vue';
import { useI18n } from '@/i18n';
import type { ServerDto } from '@/types/servers';
import { formatDateTime } from '@/utils/datetime';
import { formatBytes, formatPercent } from '@/utils/metrics';
import { diskUsagePercent, diskUsageTone } from './metricsModel';
import {
  privilegeStatusKey,
  proxyErrorKey,
  proxyModeLabelKey,
  proxyTunnelDisplayName,
  reportDisconnectCount1h,
  reportStabilityGrade,
  reportStabilityStatusKey,
  reportStabilityTone,
  reportStreamStatusKey,
  reportStreamTone,
  serverProxyMode,
  serverSummaryStatusKey,
  serverSummaryTone,
} from '../model';

const props = withDefaults(defineProps<{
  server: ServerDto;
  metrics: ServerMetricsSeries | null;
  metricsLoading?: boolean;
}>(), { metricsLoading: false });
const { t } = useI18n();
const diskStatSkeletonItems = computed(() => [
  { key: 'used', label: t('serversPage.diskUsed') },
  { key: 'free', label: t('serversPage.diskFree') },
  { key: 'total', label: t('serversPage.diskTotal') },
]);

type StatusTone = 'neutral' | 'success' | 'info' | 'warning' | 'danger';

interface StatusItem {
  key: string;
  label: string;
  value: string;
  tone: StatusTone;
  muted?: boolean;
  icon: Component;
}

const SUMMARY_PILL_CLASSES: Record<'success' | 'warning' | 'danger', string> = {
  success: 'border-success-border bg-success-bg text-success',
  warning: 'border-warning-border bg-warning-bg text-warning',
  danger: 'border-danger-border bg-danger-bg text-danger',
};

const TONE_TEXT_CLASSES: Record<StatusTone, string> = {
  neutral: 'text-foreground',
  success: 'text-success',
  info: 'text-info',
  warning: 'text-warning',
  danger: 'text-danger',
};

const summaryTone = computed(() => serverSummaryTone(props.server));
const summaryPillClass = computed(() => SUMMARY_PILL_CLASSES[summaryTone.value]);
const osName = computed(() => props.server.os?.prettyName || t('common.notAvailable'));
const privilegeIcon = computed<Component>(() => {
  if (props.server.privilege?.privileged) return ShieldCheck;
  if (props.server.sudo?.passwordless) return KeyRound;
  return UnlockKeyhole;
});
const proxyMode = computed(() => serverProxyMode(props.server));
const proxyModeLabel = computed(() => t(proxyModeLabelKey(props.server.proxyMode)));
const proxyDetail = computed(() => {
  if (proxyMode.value === 'ssh_tunnel') return proxyTunnelDisplayName(props.server) || t('serversPage.proxy.tunnelUnknown');
  if (proxyMode.value === 'socks5') return props.server.socks5Address || t('common.notAvailable');
  return '';
});
const proxyInvalid = computed(() => Boolean(props.server.proxyInvalid));
const reportDisconnectCount = computed(() => reportDisconnectCount1h(props.server));
const hasReportStability = computed(() => Boolean(reportStabilityGrade(props.server)));
const agentVersion = computed(() => props.server.traits?.['agent.version'] ?? '');

/** Status summary as compact tiles: icon + label + tone value, never a bare text row. */
const statusItems = computed<StatusItem[]>(() => {
  const streamTone = reportStreamTone(props.server);
  const stabilityTone = reportStabilityTone(props.server);
  const stabilityText = [
    t(reportStabilityStatusKey(props.server)),
    hasReportStability.value && reportDisconnectCount.value != null
      ? t('serversPage.reportStability.disconnects1h', { count: reportDisconnectCount.value })
      : '',
  ].filter(Boolean).join(' · ');
  return [
    {
      key: 'stream',
      label: t('serversPage.reportStream.label'),
      value: t(reportStreamStatusKey(props.server)),
      tone: streamTone,
      muted: streamTone === 'neutral',
      icon: Activity,
    },
    {
      key: 'stability',
      label: t('serversPage.reportStability.label'),
      value: stabilityText,
      tone: stabilityTone,
      muted: stabilityTone === 'neutral',
      icon: History,
    },
    {
      key: 'proxy',
      label: t('serversPage.proxy.label'),
      value: proxyInvalid.value
        ? `${proxyModeLabel.value} · ${t(proxyErrorKey(props.server))}`
        : `${proxyModeLabel.value}${proxyDetail.value ? ` · ${proxyDetail.value}` : ''}`,
      tone: proxyInvalid.value ? 'warning' : 'neutral',
      muted: !proxyInvalid.value,
      icon: Router,
    },
    {
      key: 'agent',
      label: t('serversPage.agent'),
      value: agentVersion.value || t('common.notAvailable'),
      tone: 'neutral',
      muted: !agentVersion.value,
      icon: ServerCog,
    },
  ];
});

const diskPercent = computed(() => diskUsagePercent(props.metrics));
const diskPoint = computed(() => props.metrics?.diskCurrent ?? null);
const diskPercentText = computed(() => (diskPercent.value === null ? t('common.notAvailable') : formatPercent(diskPercent.value, 1)));
/** Ring center stays compact; the legend still spells out “not available”. */
const diskChartValueText = computed(() => (diskPercent.value === null ? '—' : diskPercentText.value));
const diskTone = computed(() => diskUsageTone(diskPercent.value));
const diskChartTone = computed(() => (diskTone.value === 'neutral' ? 'accent' : diskTone.value) as 'accent' | 'success' | 'warning' | 'danger');
const DISK_TONE_TEXT_CLASSES: Record<'success' | 'warning' | 'danger' | 'neutral', string> = {
  success: 'text-success',
  warning: 'text-warning',
  danger: 'text-danger',
  neutral: 'text-muted-foreground',
};
const diskToneText = computed(() => DISK_TONE_TEXT_CLASSES[diskTone.value]);
const diskChartLabel = computed(() => t('serversPage.diskUsageChart', { percent: diskChartValueText.value }));
const diskStats = computed(() => {
  const point = diskPoint.value;
  if (!point) return [];
  return [
    { key: 'used', label: t('serversPage.diskUsed'), value: formatBytes(point.usedBytes) },
    { key: 'free', label: t('serversPage.diskFree'), value: formatBytes(Math.max(0, point.totalBytes - point.usedBytes)) },
    { key: 'total', label: t('serversPage.diskTotal'), value: formatBytes(point.totalBytes) },
  ];
});
const diskSampledText = computed(() => {
  const point = diskPoint.value;
  return point
    ? `${t('serversPage.diskSampledAt')} ${formatDateTime(point.time, t('common.never'))}`
    : t('serversPage.metrics.waitingSample');
});

function toneClass(item: StatusItem) {
  return item.muted ? 'text-muted-foreground' : TONE_TEXT_CLASSES[item.tone];
}
</script>

<template>
  <section data-testid="server-overview-summary" class="surface-card">
    <div class="grid gap-4 p-3 sm:p-4 wide:grid-cols-[minmax(0,1fr)_minmax(0,34rem)] wide:items-start">
      <!-- 身份区：发行版徽标 + 名称 + 综合状态 + 架构/特权/时间元信息。 -->
      <div class="flex min-w-0 items-start gap-3">
        <OsMark :os="server.os" size="lg" />
        <div class="grid min-w-0 flex-1 content-start gap-1.5">
          <div class="flex min-w-0 flex-wrap items-center gap-2">
            <h2 class="m-0 min-w-0 truncate text-subtitle text-foreground" :title="osName">{{ osName }}</h2>
            <span class="inline-flex shrink-0 items-center gap-1.5 rounded-pill border px-2 py-0.5 text-caption font-semibold" :class="summaryPillClass">
              <span class="size-1.5 rounded-full bg-current" aria-hidden="true" />
              {{ t(serverSummaryStatusKey(server)) }}
            </span>
          </div>
          <div class="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1.5 text-caption text-muted-foreground">
            <ArchitectureMark :architecture="server.architecture" />
            <span class="inline-flex min-w-0 items-center gap-1">
              <component :is="privilegeIcon" class="size-3.5 shrink-0" aria-hidden="true" />
              {{ t(privilegeStatusKey(server)) }}
            </span>
            <span class="inline-flex min-w-0 items-center gap-1">
              <CalendarDays class="size-3.5 shrink-0" aria-hidden="true" />
              {{ t('serversPage.lastChecked') }}
              <strong class="font-medium text-secondary-foreground">{{ formatDateTime(server.lastCheckedAt, t('common.never')) }}</strong>
            </span>
            <span class="inline-flex min-w-0 items-center gap-1">
              <History class="size-3.5 shrink-0" aria-hidden="true" />
              {{ t('serversPage.updatedAt') }}
              <strong class="font-medium text-secondary-foreground">{{ formatDateTime(server.updatedAt, t('common.never')) }}</strong>
            </span>
          </div>
        </div>
      </div>

      <!-- 状态瓦片：不再用一行行「标签 · 值」文字，窄屏两列、宽屏三列。 -->
      <dl class="grid min-w-0 content-start grid-cols-2 gap-2 sm:grid-cols-3">
        <div
          v-for="item in statusItems"
          :key="item.key"
          class="grid min-w-0 content-start gap-0.5 rounded-control border border-divider bg-muted px-2 py-1.5"
          :class="item.key === 'proxy' ? 'max-sm:col-span-2' : undefined"
        >
          <dt class="flex min-w-0 items-center gap-1 text-caption text-muted-foreground">
            <component :is="item.icon" class="size-3.5 shrink-0" aria-hidden="true" />
            <span class="truncate">{{ item.label }}</span>
          </dt>
          <dd class="m-0 min-w-0 truncate text-caption font-medium" :class="toneClass(item)" :title="item.value">{{ item.value }}</dd>
        </div>
      </dl>
    </div>

    <!-- 磁盘当前值：环形图 + 已用/可用/总量 + 采样时间（只消费 metrics.diskCurrent）。 -->
    <div class="border-t border-divider p-3 sm:p-4">
      <div class="flex min-w-0 flex-wrap items-center gap-4">
        <DiskUsageChart
          :percent="diskPercent"
          :value-text="diskChartValueText"
          :label="diskChartLabel"
          :tone="diskChartTone"
          :loading="metricsLoading && !metrics"
          size="lg"
        />
        <div class="grid min-w-0 flex-1 content-start gap-2">
          <div class="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-1">
            <span class="text-body-strong text-foreground">{{ t('serversPage.disk') }}</span>
            <Skeleton v-if="metricsLoading && !metrics" class="h-4 w-12" />
            <strong v-else class="text-body font-semibold tabular-nums" :class="diskToneText">{{ diskPercentText }}</strong>
          </div>
          <dl v-if="metricsLoading && !metrics" class="grid min-w-0 grid-cols-2 gap-x-4 gap-y-1 sm:grid-cols-3">
            <div v-for="stat in diskStatSkeletonItems" :key="stat.key" class="grid min-w-0 gap-0.5">
              <dt class="text-caption text-muted-foreground">{{ stat.label }}</dt>
              <dd class="m-0"><Skeleton class="h-5 w-16" /></dd>
            </div>
          </dl>
          <dl v-else-if="diskStats.length" class="grid min-w-0 grid-cols-2 gap-x-4 gap-y-1 sm:grid-cols-3">
            <div v-for="stat in diskStats" :key="stat.key" class="grid min-w-0 gap-0.5">
              <dt class="text-caption text-muted-foreground">{{ stat.label }}</dt>
              <dd class="m-0 truncate text-body tabular-nums text-foreground" :title="stat.value">{{ stat.value }}</dd>
            </div>
          </dl>
          <Skeleton v-if="metricsLoading && !metrics" class="h-3.5 w-36 max-w-full" />
          <p v-else class="m-0 text-caption text-muted-foreground">{{ diskSampledText }}</p>
        </div>
      </div>
    </div>
  </section>
</template>
