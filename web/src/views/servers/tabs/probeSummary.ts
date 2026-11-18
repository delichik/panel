import type { NetworkProbeTargetResult } from '@/types/networkProbe';
import { formatDateTime } from '@/utils/datetime';
import { formatLatency, formatPercent } from '@/utils/metrics';
import { probeAggregateOutcomeStatusKey, probeAggregateOutcomeTone, probeTargetOutcome } from '@/utils/networkProbe';

/** 概览右栏「探测」摘要：与 ProbeOverviewPanel 的聚合结果同源，供详情页宽屏两列布局使用。 */
export type ProbeOutcomeTone = 'success' | 'warning' | 'danger' | 'neutral';

export interface ProbeOverviewSummary {
  status: string;
  tone: ProbeOutcomeTone;
  latency: string;
  packetLoss: string;
  sampledAt: string;
  hasResult: boolean;
}

export function summarizeProbeOverview(
  target: NetworkProbeTargetResult | null,
  t: (key: string, params?: Record<string, string | number>) => string,
): ProbeOverviewSummary {
  const outcome = target ? probeTargetOutcome(target) : 'noData';
  const unavailable = t('common.notAvailable');
  return {
    status: t(probeAggregateOutcomeStatusKey(outcome)),
    tone: probeAggregateOutcomeTone(outcome),
    latency: target?.latencyMs === null || target?.latencyMs === undefined ? unavailable : formatLatency(target.latencyMs),
    packetLoss: target?.packetLossPercent === null || target?.packetLossPercent === undefined ? unavailable : formatPercent(target.packetLossPercent, 1),
    sampledAt: formatDateTime(target?.sampledAt, t('common.never')),
    hasResult: Boolean(target && target.sourceCount > 0),
  };
}
