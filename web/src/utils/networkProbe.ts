import type { NetworkProbeHistoryPoint, NetworkProbeSettings, NetworkProbeTargetResult } from '@/types/networkProbe';
import type { MetricSeriesBand, MetricSeriesData, MetricSeriesPoint } from './metrics';

/**
 * 把探测历史桶转成 MetricChart 折线数据。延迟为 0/缺测时用 null 断线，
 * 丢包始终是 0..100 的有效百分比；同一桶的最大/最小值转成阴影带。
 * 没有历史时返回空点集，由调用方展示空态。
 */
export function probeHistorySeries(
  history: NetworkProbeHistoryPoint[] | undefined,
  field: 'latency' | 'packetLoss',
  id: string,
  name: string,
): MetricSeriesData {
  const source = (history ?? []).filter((point) => point && typeof point.time === 'string' && point.time !== '');
  const points: MetricSeriesPoint[] = source.map((point) => ({
    time: point.time,
    value: field === 'latency'
      ? (point.latencyMs > 0 ? point.latencyMs : null)
      : point.packetLossPercent,
  }));
  const minimum: MetricSeriesPoint[] = [];
  const maximum: MetricSeriesPoint[] = [];
  let hasRange = false;
  for (const point of source) {
    const average = field === 'latency'
      ? (point.latencyMs > 0 ? point.latencyMs : null)
      : point.packetLossPercent;
    if (field === 'latency' && average === null) {
      minimum.push({ time: point.time, value: null });
      maximum.push({ time: point.time, value: null });
      continue;
    }
    const low = field === 'latency' ? (point.latencyMsMin ?? point.latencyMs) : (point.packetLossPercentMin ?? point.packetLossPercent);
    const high = field === 'latency' ? (point.latencyMsMax ?? point.latencyMs) : (point.packetLossPercentMax ?? point.packetLossPercent);
    // 延迟 0 表示失败/缺测而不是真实最小值，不进入区间；丢包 0 是有效值。
    const validRange = field === 'latency'
      ? typeof low === 'number' && low > 0 && typeof high === 'number' && high > 0
      : typeof low === 'number' && Number.isFinite(low) && typeof high === 'number' && Number.isFinite(high);
    if (validRange) {
      minimum.push({ time: point.time, value: Math.min(low, high) });
      maximum.push({ time: point.time, value: Math.max(low, high) });
      hasRange = true;
    } else {
      minimum.push({ time: point.time, value: null });
      maximum.push({ time: point.time, value: null });
    }
  }
  const band: MetricSeriesBand | undefined = hasRange ? { min: minimum, max: maximum } : undefined;
  return { id, name, points, band };
}

/** 按目标聚合结果的用户可见状态：无源/无结果为 noData，失败语义并入 stale/degraded。 */
export type NetworkProbeAggregateOutcome = 'ok' | 'degraded' | 'stale' | 'noData';

/**
 * 从按目标聚合摘要派生状态：没有探测源或没有采样 → noData；超过新鲜窗口 → stale；
 * 有丢包 → degraded；其余为 ok。状态只由真实字段派生，不推测健康。
 */
export function probeTargetOutcome(
  node: Pick<NetworkProbeTargetResult, 'sourceCount' | 'stale' | 'packetLossPercent'>,
): NetworkProbeAggregateOutcome {
  if (node.sourceCount <= 0) return 'noData';
  if (node.stale) return 'stale';
  return (node.packetLossPercent ?? 0) > 0 ? 'degraded' : 'ok';
}

export function probeAggregateOutcomeTone(outcome: NetworkProbeAggregateOutcome): 'success' | 'warning' | 'danger' | 'neutral' {
  if (outcome === 'ok') return 'success';
  if (outcome === 'degraded' || outcome === 'stale') return 'warning';
  if (outcome === 'noData') return 'neutral';
  return 'danger';
}

export function probeAggregateOutcomeStatusKey(outcome: NetworkProbeAggregateOutcome) {
  return `serversPage.probe.status.${outcome}` as const;
}


/**
 * 网络探测节拍参数契约：前端表单、Mock 与测试共用同一组默认值/区间/错误码，
 * 避免界面提示、Mock 校验与后端契约各自漂移。
 */
export type NetworkProbeSettingField = keyof NetworkProbeSettings;

export interface NetworkProbeSettingRule {
  min: number;
  max: number;
  step: number;
  defaultValue: number;
  /** 越界/非法值对应的稳定错误码。 */
  errorCode: string;
}

export const networkProbeSettingRules: Record<NetworkProbeSettingField, NetworkProbeSettingRule> = {
  // 后台契约以秒为单位：最小 1 分钟（60 秒）、最大 60 分钟（3600 秒）。
  intervalSeconds: { min: 60, max: 3600, step: 1, defaultValue: 60, errorCode: 'network_probe_interval_invalid' },
  pingCount: { min: 1, max: 10, step: 1, defaultValue: 3, errorCode: 'network_probe_ping_count_invalid' },
  pingTimeoutSeconds: { min: 1, max: 10, step: 1, defaultValue: 2, errorCode: 'network_probe_ping_timeout_invalid' },
};

/**
 * 探测间隔的界面口径：表单以整数分钟输入（1..60，默认 1），保存时换算为后台的
 * intervalSeconds = 分钟 × 60。这里集中定义分钟规则与换算 helper，避免表单、Mock
 * 与测试各自漂移。
 */
export const networkProbeIntervalMinutes = { min: 1, max: 60, step: 1, defaultValue: 1 } as const;

/** 分钟 → 秒（后台契约单位）。 */
export function networkProbeIntervalSecondsFromMinutes(minutes: number): number {
  return minutes * 60;
}

/** 秒 → 分钟：四舍五入取整，最小 1 分钟（兼容旧配置中的 <60 秒或非整分钟值）。 */
export function networkProbeIntervalMinutesFromSeconds(seconds: number | null | undefined): number {
  const value = typeof seconds === 'number' && Number.isFinite(seconds)
    ? seconds
    : networkProbeSettingRules.intervalSeconds.defaultValue;
  return Math.max(networkProbeIntervalMinutes.min, Math.round(value / 60));
}

/** 分钟输入合法值必须是 1..60 的整数。 */
export function isValidNetworkProbeIntervalMinutes(value: unknown): boolean {
  return typeof value === 'number'
    && Number.isInteger(value)
    && value >= networkProbeIntervalMinutes.min
    && value <= networkProbeIntervalMinutes.max;
}

/** 表单分钟原始输入（字符串）转数字；空串、小数、越界一律返回 null。 */
export function parseNetworkProbeIntervalMinutes(raw: string): number | null {
  const value = parseNetworkProbeSetting(raw);
  return value !== null && isValidNetworkProbeIntervalMinutes(value) ? value : null;
}

export const networkProbeSettingFields = Object.keys(networkProbeSettingRules) as NetworkProbeSettingField[];

/** 合法取值必须是区间内的整数；字符串、NaN、小数一律非法。 */
export function isValidNetworkProbeSetting(field: NetworkProbeSettingField, value: unknown): boolean {
  const rule = networkProbeSettingRules[field];
  return typeof value === 'number'
    && Number.isInteger(value)
    && value >= rule.min
    && value <= rule.max;
}

/**
 * 缺省（undefined/null）表示旧 payload，交由服务端补默认值，不算错误；
 * 显式给出但非法时返回稳定错误码。
 */
export function networkProbeSettingError(field: NetworkProbeSettingField, value: unknown): string | null {
  if (value === undefined || value === null || value === '') return null;
  return isValidNetworkProbeSetting(field, value) ? null : networkProbeSettingRules[field].errorCode;
}

/** 旧 payload 缺省参数按默认值补齐；非法值也回退默认值（调用方应先做校验/报错）。 */
export function normalizeNetworkProbeSettings(input: Partial<Record<NetworkProbeSettingField, unknown>>): NetworkProbeSettings {
  const settings = {} as NetworkProbeSettings;
  for (const field of networkProbeSettingFields) {
    const value = input[field];
    settings[field] = isValidNetworkProbeSetting(field, value)
      ? (value as number)
      : networkProbeSettingRules[field].defaultValue;
  }
  return settings;
}

/** 保存输入校验：返回首个非法字段及错误码；缺省字段视为合法（服务端补默认值）。 */
export function networkProbeSettingsError(
  input: Partial<Record<NetworkProbeSettingField, unknown>>,
): { field: NetworkProbeSettingField; code: string } | null {
  for (const field of networkProbeSettingFields) {
    const code = networkProbeSettingError(field, input[field]);
    if (code) return { field, code };
  }
  return null;
}

/** 表单原始输入（字符串）转数字；空串与小数返回 null，由调用方按非法处理。 */
export function parseNetworkProbeSetting(raw: string): number | null {
  const trimmed = raw.trim();
  if (!trimmed) return null;
  const value = Number(trimmed);
  return Number.isInteger(value) ? value : null;
}
