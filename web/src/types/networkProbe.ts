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
  /** 同一 30 分钟桶内采样的最小值/最大值；旧数据缺省时等于平均值。 */
  latencyMsMin?: number;
  latencyMsMax?: number;
  packetLossPercentMin?: number;
  packetLossPercentMax?: number;
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
  /** 稳定错误文本；空串表示本轮探测成功；服务端空值会省略该字段。 */
  error?: string;
  /** 采样时间超出新鲜窗口；预计算值，客户端不重算。 */
  stale: boolean;
  /** 最近 24 小时历史（仅 `GET /servers/{id}/network-probes?history=1` 返回）。 */
  history?: NetworkProbeHistoryPoint[];
}

export interface NetworkProbeConfig extends NetworkProbeSettings {
  mode: NetworkProbeMode;
  serverIds: string[];
  thresholds: NetworkProbeThresholds;
  version: number;
  updatedAt: string;
}

/** 探测节拍参数；旧 payload 缺省时由服务端按默认值补齐（interval=60、count=3、timeout=2）。 */
export interface NetworkProbeSettings {
  /**
   * 探测周期（后台契约单位为秒），合法区间 60..3600（即 1..60 分钟）。
   * 界面以整数分钟输入（默认 1 分钟），保存时换算为 intervalSeconds = 分钟 × 60。
   */
  intervalSeconds: number;
  /** 每次探测发送的 ping 包数，合法区间 1..10。 */
  pingCount: number;
  /** 单包超时（秒），合法区间 1..10。 */
  pingTimeoutSeconds: number;
}

/** 网络探测图表警告阈值；缺省表示不启用。 */
export interface NetworkProbeThresholds {
  latencyMs?: number;
  packetLossPercent?: number;
}

export interface NetworkProbeSaveInput {
  mode: NetworkProbeMode;
  serverIds: string[];
  /** 旧 payload 可缺省，服务端按默认值补齐。 */
  intervalSeconds?: number;
  pingCount?: number;
  pingTimeoutSeconds?: number;
  thresholds?: NetworkProbeThresholds;
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

/** 按目标聚合的探测摘要：所有当前探测源对某台服务器的最近结果。 */
export interface NetworkProbeTargetResult {
  serverId: string;
  name: string;
  address: string;
  /** 当前该目标的有效探测源数量（自身不会探测自己）。 */
  sourceCount: number;
  /** 所有有效探测源延迟的平均值；无有效样本时为 null。 */
  latencyMs: number | null;
  /** 所有有结果源丢包率的平均值；无结果时为 null。 */
  packetLossPercent: number | null;
  /** 所有有结果源中的最大丢包率；无结果时为 null。 */
  worstPacketLossPercent: number | null;
  /** 该目标所有结果中的最新采样时间；无结果时为 null。 */
  sampledAt: string | null;
  /** 最新采样超出新鲜窗口，或完全没有结果。 */
  stale: boolean;
  /** 跨源聚合的最近 24 小时历史；仅 `history=1` 返回，无历史时缺省/空数组。 */
  history?: NetworkProbeHistoryPoint[];
}

/** 设施详情页的按目标聚合视图：所有服务器各一条（无结果也保留空摘要）。 */
export interface NetworkProbeTargetMatrix {
  nodes: NetworkProbeTargetResult[];
  updatedAt: string | null;
}

/** 概览 networkProbe 卡片的数据切片。 */
export interface NetworkProbeCardData {
  nodes: NetworkProbeNode[];
  results: NetworkProbeResult[];
  updatedAt: string | null;
}
