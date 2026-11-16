<script setup lang="ts">
import { computed } from 'vue';
import type { ServerMetricsSeries } from '@/api/servers';
import { useI18n } from '@/i18n';
import type { ServerDto } from '@/types/servers';
import { formatDateTime } from '@/utils/datetime';
import { formatBytes, formatPercent } from '@/utils/metrics';
import { diskUsagePercent } from './metricsModel';
import {
  architectureLabel,
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
  tailscaleStatusKey,
  tailscaleTone,
} from '../model';

const props = defineProps<{ server: ServerDto; metrics: ServerMetricsSeries | null }>();
const { t } = useI18n();

type StatusTone = 'neutral' | 'success' | 'info' | 'warning' | 'danger';

interface StatusItem {
  key: string;
  label: string;
  value: string;
  tone: StatusTone;
  muted?: boolean;
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
const architecture = computed(() => architectureLabel(props.server.architecture));
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
const hasTailscale = computed(() => props.server.tailscaleEnabled);

const statusItems = computed<StatusItem[]>(() => {
  const streamTone = reportStreamTone(props.server);
  const stabilityTone = reportStabilityTone(props.server);
  const tailscaleStateTone = tailscaleTone(props.server);
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
    },
    {
      key: 'stability',
      label: t('serversPage.reportStability.label'),
      value: stabilityText,
      tone: stabilityTone,
      muted: stabilityTone === 'neutral',
    },
    {
      key: 'proxy',
      label: t('serversPage.proxy.label'),
      value: proxyInvalid.value
        ? `${proxyModeLabel.value} · ${t(proxyErrorKey(props.server))}`
        : `${proxyModeLabel.value}${proxyDetail.value ? ` · ${proxyDetail.value}` : ''}`,
      tone: proxyInvalid.value ? 'warning' : 'neutral',
    },
    {
      key: 'tailscale',
      label: t('serversPage.tailscale.title'),
      value: hasTailscale.value ? t(tailscaleStatusKey(props.server)) : t('serversPage.tailscale.notEnabled'),
      tone: hasTailscale.value ? tailscaleStateTone : 'neutral',
      muted: !hasTailscale.value || tailscaleStateTone === 'neutral',
    },
    {
      key: 'agent',
      label: t('serversPage.agent'),
      value: agentVersion.value || t('common.notAvailable'),
      tone: 'neutral',
      muted: !agentVersion.value,
    },
  ];
});

const diskPercent = computed(() => diskUsagePercent(props.metrics));
const diskPoint = computed(() => props.metrics?.diskCurrent ?? null);
const diskPercentText = computed(() => (diskPercent.value === null ? t('common.notAvailable') : formatPercent(diskPercent.value, 1)));
const diskTone = computed<'success' | 'warning' | 'danger' | 'neutral'>(() => {
  const percent = diskPercent.value;
  if (percent === null) return 'neutral';
  if (percent >= 95) return 'danger';
  if (percent >= 85) return 'warning';
  return 'success';
});
const DISK_TONE_TEXT_CLASSES: Record<'success' | 'warning' | 'danger' | 'neutral', string> = {
  success: 'text-success',
  warning: 'text-warning',
  danger: 'text-danger',
  neutral: 'text-muted-foreground',
};
const DISK_TONE_BG_CLASSES: Record<'success' | 'warning' | 'danger' | 'neutral', string> = {
  success: 'bg-success',
  warning: 'bg-warning',
  danger: 'bg-danger',
  neutral: 'bg-border-strong',
};
const diskToneText = computed(() => DISK_TONE_TEXT_CLASSES[diskTone.value]);
const diskToneBg = computed(() => DISK_TONE_BG_CLASSES[diskTone.value]);
const diskWidth = computed(() => `${Math.min(100, Math.max(0, diskPercent.value ?? 0))}%`);
</script>

<template>
  <section class="rounded-2xl border border-border bg-card">
    <div class="flex flex-wrap items-center justify-between gap-x-6 gap-y-2 px-4 py-2.5">
      <div class="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1.5">
        <span class="inline-flex items-center gap-2 rounded-full border px-2.5 py-1 text-xs font-semibold" :class="summaryPillClass">
          <span class="size-1.5 rounded-full bg-current" aria-hidden="true" />
          {{ t(serverSummaryStatusKey(server)) }}
        </span>
        <span class="min-w-0 text-xs text-muted-foreground">
          {{ server.os?.prettyName || t('common.notAvailable') }}
          <template v-if="architecture"> · {{ architecture }}</template>
          · {{ t(privilegeStatusKey(server)) }}
        </span>
      </div>
      <span class="text-[11.5px] text-muted-foreground">
        {{ t('serversPage.lastChecked') }} <strong class="font-medium text-secondary-foreground">{{ formatDateTime(server.lastCheckedAt, t('common.never')) }}</strong>
        · {{ t('serversPage.updatedAt') }} <strong class="font-medium text-secondary-foreground">{{ formatDateTime(server.updatedAt, t('common.never')) }}</strong>
      </span>
    </div>

    <dl class="flex flex-wrap items-center gap-x-4 gap-y-1.5 border-t border-border px-4 py-2">
      <div v-for="(item, index) in statusItems" :key="item.key" class="inline-flex min-w-0 items-baseline gap-1.5">
        <span v-if="index > 0" class="text-border-strong" aria-hidden="true">·</span>
        <dt class="text-[11px] text-muted-foreground">{{ item.label }}</dt>
        <dd class="m-0 max-w-[18rem] truncate text-xs font-medium" :class="item.muted ? 'text-muted-foreground' : TONE_TEXT_CLASSES[item.tone]" :title="item.value">{{ item.value }}</dd>
      </div>
    </dl>

    <div class="flex flex-wrap items-center gap-x-3 gap-y-1.5 border-t border-border px-4 py-2">
      <span class="text-xs text-muted-foreground">{{ t('serversPage.disk') }}</span>
      <strong class="text-base font-semibold tabular-nums" :class="diskToneText">{{ diskPercentText }}</strong>
      <div
        class="h-2 w-[180px] overflow-hidden rounded-full bg-border/60 sm:w-[240px]"
        role="progressbar"
        :aria-valuenow="diskPercent ?? undefined"
        aria-valuemin="0"
        aria-valuemax="100"
        :aria-label="t('serversPage.disk')"
      >
        <div class="h-full rounded-full transition-[width]" :class="diskToneBg" :style="{ width: diskWidth }" />
      </div>
      <span v-if="diskPoint" class="text-[11.5px] text-muted-foreground">{{ formatBytes(diskPoint.usedBytes) }} / {{ formatBytes(diskPoint.totalBytes) }}</span>
      <span class="text-[11.5px] text-muted-foreground sm:ml-auto">
        <template v-if="diskPoint">{{ t('serversPage.diskSampledAt') }} {{ formatDateTime(diskPoint.time, t('common.never')) }}</template>
        <template v-else>{{ t('serversPage.metrics.waitingSample') }}</template>
      </span>
    </div>
  </section>
</template>
