import type {
  NetworkProbeCardData,
  NetworkProbeConfig,
  NetworkProbeHistoryPoint,
  NetworkProbeMatrix,
  NetworkProbeNode,
  NetworkProbeResult,
  NetworkProbeSaveInput,
  NetworkProbeServerResults,
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
  version: 1,
  updatedAt: '2026-11-16T05:00:00.000Z',
};

function cloneConfig(): NetworkProbeConfig {
  return { ...config, serverIds: [...config.serverIds] };
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
  config = {
    mode: input.mode,
    serverIds,
    ...settings,
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
    return {
      time: new Date(currentBucket - (47 - index) * bucketMs).toISOString(),
      latencyMs: failed ? 0 : Math.round((result.latencyMs + ((offset % 9) - 4) * 4) * 10) / 10,
      packetLossPercent: failed ? 100 : Math.max(0, Math.round((result.packetLossPercent + (offset % 5) - 2) * 10) / 10),
    };
  });
}

export function getNetworkProbeCardData(): NetworkProbeCardData {
  const matrix = getNetworkProbeMatrix();
  return { nodes: matrix.nodes, results: matrix.results, updatedAt: matrix.updatedAt };
}
