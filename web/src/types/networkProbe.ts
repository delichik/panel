/** 网络探测设施：探测节点之间的延迟与丢包；配置与结果分属两个后端契约。 */
export type NetworkProbeMode = 'all' | 'selected';

export interface NetworkProbeNode {
  serverId: string;
  name: string;
  address: string;
}

export interface NetworkProbeHistoryPoint {
  time: string;
  latencyMs: number;
  packetLossPercent: number;
}

export interface NetworkProbeResult {
  sourceServerId: string;
  targetServerId: string;
  targetAddress: string;
  /** 毫秒；探测失败时为 0。 */
  latencyMs: number;
  packetLossPercent: number;
  sent: number;
  received: number;
  /** ISO 时间；服务端截断到秒。 */
  sampledAt: string;
  /** 稳定错误文本；空串表示本轮探测成功。 */
  error: string;
  /** 采样时间超出新鲜窗口；预计算值，客户端不重算。 */
  stale: boolean;
  /** 最近 24 小时历史（仅 `GET /servers/{id}/network-probes?history=1` 返回）。 */
  history?: NetworkProbeHistoryPoint[];
}

export interface NetworkProbeConfig extends NetworkProbeSettings {
  mode: NetworkProbeMode;
  serverIds: string[];
  version: number;
  updatedAt: string;
}

/** 探测节拍参数；旧 payload 缺省时由服务端按默认值补齐（interval=60、count=3、timeout=2）。 */
export interface NetworkProbeSettings {
  /** 探测周期（秒），合法区间 10..3600。 */
  intervalSeconds: number;
  /** 每次探测发送的 ping 包数，合法区间 1..10。 */
  pingCount: number;
  /** 单包超时（秒），合法区间 1..10。 */
  pingTimeoutSeconds: number;
}

export interface NetworkProbeSaveInput {
  mode: NetworkProbeMode;
  serverIds: string[];
  /** 旧 payload 可缺省，服务端按默认值补齐。 */
  intervalSeconds?: number;
  pingCount?: number;
  pingTimeoutSeconds?: number;
}

export interface NetworkProbeMatrix {
  mode: NetworkProbeMode;
  nodes: NetworkProbeNode[];
  results: NetworkProbeResult[];
  updatedAt: string | null;
}

export interface NetworkProbeServerResults {
  serverId: string;
  /** 该节点当前是否参与探测；未参与时 nodes/results 为空。 */
  enabled: boolean;
  /** 该节点的探测目标。 */
  nodes: NetworkProbeNode[];
  /** source = 该节点的最新结果。 */
  results: NetworkProbeResult[];
  updatedAt: string | null;
}

/** 概览 networkProbe 卡片的数据切片。 */
export interface NetworkProbeCardData {
  nodes: NetworkProbeNode[];
  results: NetworkProbeResult[];
  updatedAt: string | null;
}
