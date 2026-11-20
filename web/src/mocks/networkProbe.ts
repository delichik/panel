import type {
  NetworkProbeCardData,
  NetworkProbeConfig,
  NetworkProbeHistoryPoint,
  NetworkProbeMatrix,
  NetworkProbeNode,
  NetworkProbeResult,
  NetworkProbeSaveInput,
  NetworkProbeServerResults,
  NetworkProbeTargetMatrix,
  NetworkProbeTargetResult,
} from '@/types/networkProbe';
import type { ServerDto } from '@/types/servers';
import { networkProbeSettingRules, normalizeNetworkProbeSettings, networkProbeSettingsError } from '@/utils/networkProbe';
import { mockServers } from './servers';

/** 网络探测设施配置/结果校验错误：browser mock 捕获后按契约错误码返回。 */
export class MockNetworkProbeError extends Error {
  readonly code: string;
  readonly status: number;

  constructor(code: string, message: string, status = 422) {
    super(message);
    this.name = 'MockNetworkProbeError';
    this.code = code;
    this.status = status;
  }
}

let config: NetworkProbeConfig = {
  mode: 'all',
  serverIds: [],
  intervalSeconds: networkProbeSettingRules.intervalSeconds.defaultValue,
  pingCount: networkProbeSettingRules.pingCount.defaultValue,
  pingTimeoutSeconds: networkProbeSettingRules.pingTimeoutSeconds.defaultValue,
  thresholds: {},
  version: 1,
  updatedAt: '2026-11-16T05:00:00.000Z',
};

function cloneConfig(): NetworkProbeConfig {
  return { ...config, serverIds: [...config.serverIds], thresholds: { ...config.thresholds } };
}

function toNode(server: ServerDto): NetworkProbeNode {
  return { serverId: server.id, name: server.name, address: server.ipv4 || server.ipv6 || server.host };
}

function activeNodeIds(): string[] {
  if (config.mode === 'all') return mockServers.map((server) => server.id);
  const known = new Set(mockServers.map((server) => server.id));
  return config.serverIds.filter((id) => known.has(id));
}

function activeNodes(): NetworkProbeNode[] {
  const ids = new Set(activeNodeIds());
  return mockServers.filter((server) => ids.has(server.id)).map(toNode);
}

/** 探测目标恒为“所有其他服务器”，与 mode 无关；服务端强制跳过探测节点自己。 */
function targetsFor(sourceId: string): ServerDto[] {
  return mockServers.filter((server) => server.id !== sourceId);
}

function hashValue(text: string): number {
  let hash = 0;
  for (let index = 0; index < text.length; index += 1) {
    hash = (hash * 31 + text.charCodeAt(index)) >>> 0;
  }
  return hash;
}

/**
 * 生成稳定的代表性结果：多数正常、少量高丢包、少量探测失败、少量过期样本，
 * 便于设施矩阵、服务器页签与概览卡片同时展示各种状态。
 */
function resultFor(source: ServerDto, target: ServerDto, sampledAt: number): NetworkProbeResult {
  const hash = hashValue(`${source.id}|${target.id}`);
  const failed = hash % 17 === 0;
  const lossy = !failed && hash % 11 === 0;
  const stale = hash % 13 === 0;
  const packetLossPercent = failed ? 100 : lossy ? ((hash % 9) + 1) * 10 : 0;
  const sent = 3;
  const received = failed ? 0 : Math.max(1, Math.round(sent * (1 - packetLossPercent / 100)));
  const latencyMs = failed ? 0 : Math.round((2.5 + (hash % 7_500) / 1_000) * 10) / 10;
  return {
    sourceServerId: source.id,
    targetServerId: target.id,
    targetAddress: target.ipv4 || target.ipv6 || target.host,
    latencyMs,
    packetLossPercent,
    sent,
    received,
    sampledAt: new Date(sampledAt - (stale ? 12 * 60 * 1000 : (hash % 4) * 30 * 1000)).toISOString(),
    error: failed ? 'ping: sendto: no route to host' : '',
    stale,
  };
}

function resultsForSource(source: ServerDto, now: number): NetworkProbeResult[] {
  return targetsFor(source.id).map((target) => resultFor(source, target, now));
}

function latestSampleAt(results: NetworkProbeResult[]): string | null {
  return results.reduce<string | null>((latest, result) => (!latest || result.sampledAt > latest ? result.sampledAt : latest), null);
}

export function getNetworkProbeConfig(): NetworkProbeConfig {
  return cloneConfig();
}

export function saveNetworkProbeConfig(input: NetworkProbeSaveInput): NetworkProbeConfig {
  if (input.mode !== 'all' && input.mode !== 'selected') {
    throw new MockNetworkProbeError('network_probe_mode_invalid', 'Probe mode must be "all" or "selected".');
  }
  const known = new Set(mockServers.map((server) => server.id));
  const serverIds = [...new Set(input.serverIds ?? [])].filter((id) => id.trim());
  if (input.mode === 'selected' && serverIds.length === 0) {
    throw new MockNetworkProbeError('network_probe_servers_required', 'Select at least one probe node.');
  }
  const missing = serverIds.find((id) => !known.has(id));
  if (missing) {
    throw new MockNetworkProbeError('network_probe_server_not_found', `Server ${missing} was not found.`, 404);
  }
  // 旧 payload 缺省三个节拍参数时按默认值补齐；显式非法值按契约错误码拒绝。
  const settingsError = networkProbeSettingsError(input);
  if (settingsError) {
    throw new MockNetworkProbeError(settingsError.code, `Probe setting ${settingsError.field} is out of range.`);
  }
  const settings = normalizeNetworkProbeSettings(input);
  const thresholds = input.thresholds ?? {};
  if (thresholds.latencyMs !== undefined && (!Number.isFinite(thresholds.latencyMs) || thresholds.latencyMs <= 0)) {
    throw new MockNetworkProbeError('network_probe_threshold_invalid', 'Latency threshold must be greater than 0.');
  }
  if (thresholds.packetLossPercent !== undefined && (!Number.isFinite(thresholds.packetLossPercent) || thresholds.packetLossPercent <= 0 || thresholds.packetLossPercent > 100)) {
    throw new MockNetworkProbeError('network_probe_threshold_invalid', 'Packet loss threshold must be greater than 0 and at most 100.');
  }
  config = {
    mode: input.mode,
    serverIds,
    ...settings,
    thresholds: { ...thresholds },
    version: config.version + 1,
    updatedAt: new Date().toISOString(),
  };
  return cloneConfig();
}

export function getNetworkProbeMatrix(): NetworkProbeMatrix {
  const now = Date.now();
  const nodes = activeNodes();
  const results = nodes.flatMap((node) => {
    const source = mockServers.find((server) => server.id === node.serverId);
    return source ? resultsForSource(source, now) : [];
  });
  return { mode: config.mode, nodes, results, updatedAt: latestSampleAt(results) };
}

export function getServerNetworkProbes(serverId: string, includeHistory = false): NetworkProbeServerResults | null {
  const source = mockServers.find((server) => server.id === serverId);
  if (!source) return null;
  const enabled = activeNodeIds().includes(serverId);
  const nodes = enabled ? targetsFor(serverId).map(toNode) : [];
  const results = enabled
    ? resultsForSource(source, Date.now()).map((result) => (
      includeHistory ? { ...result, history: historyFor(source, result, Date.now()) } : result
    ))
    : [];
  return { serverId, enabled, nodes, results, updatedAt: latestSampleAt(results) };
}

/** 24h / 30 分钟桶历史，供首页历史条在 Mock 模式下演示。 */
function historyFor(source: ServerDto, result: NetworkProbeResult, now: number): NetworkProbeHistoryPoint[] {
  const hash = hashValue(`${source.id}|${result.targetServerId}|history`);
  const bucketMs = 30 * 60 * 1000;
  const currentBucket = Math.floor(now / bucketMs) * bucketMs;
  return Array.from({ length: 48 }, (_, index) => {
    const offset = hash + index * 7;
    const failed = offset % 23 === 0;
    const latency = failed ? 0 : Math.round((result.latencyMs + ((offset % 9) - 4) * 4) * 10) / 10;
    const loss = failed ? 100 : Math.max(0, Math.round((result.packetLossPercent + (offset % 5) - 2) * 10) / 10);
    // 同一 30 分钟桶内的采样区间：平均保持不变，min/max 用于图表阴影带。
    const latencySpread = failed ? 0 : 1 + (offset % 4);
    const lossSpread = failed ? 0 : (offset % 3);
    return {
      time: new Date(currentBucket - (47 - index) * bucketMs).toISOString(),
      latencyMs: latency,
      latencyMsMin: Math.max(0, Math.round((latency - latencySpread) * 10) / 10),
      latencyMsMax: Math.round((latency + latencySpread) * 10) / 10,
      packetLossPercent: loss,
      packetLossPercentMin: Math.max(0, Math.round((loss - lossSpread) * 10) / 10),
      packetLossPercentMax: Math.min(100, Math.round((loss + lossSpread) * 10) / 10),
    };
  });
}

export function getNetworkProbeCardData(): NetworkProbeCardData {
  const matrix = getNetworkProbeMatrix();
  return { nodes: matrix.nodes, results: matrix.results, updatedAt: matrix.updatedAt };
}

/** 5 分钟新鲜度窗口，与后端 `freshnessFor` / ResultFreshFor 对齐。 */
const TARGET_FRESH_WINDOW_MS = 5 * 60 * 1000;

function roundTenth(value: number): number {
  return Math.round(value * 10) / 10;
}

/** 跨源聚合 24h 历史：时间桶按当前时间对齐，延迟/丢包取有效均值，区间取所有源的最小/最大值。 */
function aggregateTargetHistory(histories: NetworkProbeHistoryPoint[][]): NetworkProbeHistoryPoint[] {
  if (!histories.length) return [];
  const length = Math.max(...histories.map((history) => history.length));
  return Array.from({ length }, (_, index) => {
    const points = histories
      .map((history) => history[index])
      .filter((point): point is NetworkProbeHistoryPoint => Boolean(point));
    const validLatencyPoints = points.filter((point) => point.latencyMs > 0);
    const latencies = validLatencyPoints.map((point) => point.latencyMs);
    const losses = points.map((point) => point.packetLossPercent);
    const latencyMins = validLatencyPoints.map((point) => point.latencyMsMin ?? point.latencyMs);
    const latencyMaxes = validLatencyPoints.map((point) => point.latencyMsMax ?? point.latencyMs);
    const lossMins = points.map((point) => point.packetLossPercentMin ?? point.packetLossPercent);
    const lossMaxes = points.map((point) => point.packetLossPercentMax ?? point.packetLossPercent);
    return {
      time: points[0]?.time ?? new Date().toISOString(),
      latencyMs: latencies.length ? roundTenth(latencies.reduce((sum, value) => sum + value, 0) / latencies.length) : 0,
      latencyMsMin: latencyMins.length ? roundTenth(Math.min(...latencyMins)) : 0,
      latencyMsMax: latencyMaxes.length ? roundTenth(Math.max(...latencyMaxes)) : 0,
      packetLossPercent: losses.length ? roundTenth(losses.reduce((sum, value) => sum + value, 0) / losses.length) : 0,
      packetLossPercentMin: lossMins.length ? roundTenth(Math.min(...lossMins)) : 0,
      packetLossPercentMax: lossMaxes.length ? roundTenth(Math.max(...lossMaxes)) : 0,
    };
  });
}

/**
 * 按目标聚合视图（设施详情 / 服务器概览）：nodes 覆盖全部服务器，没有结果也保留
 * 一条空摘要；只聚合当前探测节点的结果，自身不会探测自己。
 */
export function getNetworkProbeTargets(includeHistory = false): NetworkProbeTargetMatrix {
  const now = Date.now();
  const sources = activeNodes()
    .map((node) => mockServers.find((server) => server.id === node.serverId))
    .filter((server): server is ServerDto => Boolean(server));

  interface Aggregate {
    sourceCount: number;
    latencySum: number;
    latencyCount: number;
    lossSum: number;
    lossCount: number;
    worstLoss: number | null;
    latestSampledAt: number;
    histories: NetworkProbeHistoryPoint[][];
  }

  const aggregates = new Map<string, Aggregate>();
  for (const source of sources) {
    for (const target of targetsFor(source.id)) {
      const result = resultFor(source, target, now);
      const entry = aggregates.get(target.id) ?? {
        sourceCount: 0,
        latencySum: 0,
        latencyCount: 0,
        lossSum: 0,
        lossCount: 0,
        worstLoss: null,
        latestSampledAt: 0,
        histories: [],
      };
      entry.sourceCount += 1;
      if (!result.error && result.received > 0 && result.latencyMs > 0) {
        entry.latencySum += result.latencyMs;
        entry.latencyCount += 1;
      }
      entry.lossSum += result.packetLossPercent;
      entry.lossCount += 1;
      if (entry.worstLoss === null || result.packetLossPercent > entry.worstLoss) entry.worstLoss = result.packetLossPercent;
      const sampledAt = Date.parse(result.sampledAt);
      if (sampledAt > entry.latestSampledAt) entry.latestSampledAt = sampledAt;
      if (includeHistory) entry.histories.push(historyFor(source, result, now));
      aggregates.set(target.id, entry);
    }
  }

  const nodes: NetworkProbeTargetResult[] = mockServers.map((server) => {
    const entry = aggregates.get(server.id);
    const sampledAtMs = entry?.latestSampledAt ?? 0;
    return {
      serverId: server.id,
      name: server.name,
      address: server.ipv4 || server.ipv6 || server.host,
      sourceCount: entry?.sourceCount ?? 0,
      latencyMs: entry && entry.latencyCount ? roundTenth(entry.latencySum / entry.latencyCount) : null,
      packetLossPercent: entry && entry.lossCount ? roundTenth(entry.lossSum / entry.lossCount) : null,
      worstPacketLossPercent: entry?.worstLoss ?? null,
      sampledAt: sampledAtMs ? new Date(sampledAtMs).toISOString() : null,
      stale: !sampledAtMs || now - sampledAtMs > TARGET_FRESH_WINDOW_MS,
      ...(includeHistory ? { history: aggregateTargetHistory(entry?.histories ?? []) } : {}),
    };
  });

  let updatedAt: string | null = null;
  for (const node of nodes) {
    if (node.sampledAt && (!updatedAt || node.sampledAt > updatedAt)) updatedAt = node.sampledAt;
  }
  return { nodes, updatedAt };
}
