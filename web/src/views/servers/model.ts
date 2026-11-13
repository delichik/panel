import type { ServerDto, ServerProbeInput, ServerProxyMode, ServerSaveInput } from '@/types/servers';
import type { CredentialDto } from '@/types/credentials';
import { isSocks5Address, normalizeServerProxyMode } from '@/utils/serverProxy';

export type { ServerProbeInput };

/**
 * 服务器详情的一级 tab。Docker 资源合并为 `resources`，再用 URL `subtab`
 * 区分 containers/images/networks/volumes；旧一级 tab 名仍作深链别名。
 * 未知取值一律回落到 overview。
 */
export const serverDetailTabs = ['overview', 'metrics', 'agent', 'tailscale', 'resources', 'packages', 'firewall'] as const;

export type ServerDetailTab = (typeof serverDetailTabs)[number];

/** Docker 资源在 Resources 一级 tab 内的分段；也是旧 `tab=containers|…` 别名的目标。 */
export const serverResourceSubtabs = ['containers', 'images', 'networks', 'volumes'] as const;

export type ServerResourceSubtab = (typeof serverResourceSubtabs)[number];

const legacyResourceTabSet = new Set<string>(serverResourceSubtabs);

export function isLegacyResourceTab(value: unknown): value is ServerResourceSubtab {
  return legacyResourceTabSet.has(String(value ?? ''));
}

export function normalizeServerDetailTab(value: unknown): ServerDetailTab {
  const raw = String(value ?? '');
  if (isLegacyResourceTab(raw)) return 'resources';
  return (serverDetailTabs as readonly string[]).includes(raw) ? (raw as ServerDetailTab) : 'overview';
}

export function normalizeServerResourceSubtab(value: unknown, legacyTab?: unknown): ServerResourceSubtab {
  const fromSubtab = String(value ?? '');
  if ((serverResourceSubtabs as readonly string[]).includes(fromSubtab)) return fromSubtab as ServerResourceSubtab;
  if (isLegacyResourceTab(legacyTab)) return legacyTab;
  return 'containers';
}

/** 防火墙 tab 内部的 UFW / Fail2Ban 分段（Fail2Ban 仅 dev 构建可见）。 */
export type FirewallView = 'ufw' | 'fail2ban';

export function normalizeFirewallView(value: unknown): FirewallView {
  return String(value ?? '') === 'fail2ban' ? 'fail2ban' : 'ufw';
}


export function agentTone(server: ServerDto): 'success' | 'warning' | 'danger' | 'neutral' {
  const status = server.traits?.['agent.status'];
  if (server.traits?.['agent.enabled'] !== 'true') return 'neutral';
  if (status === 'compatible') return 'success';
  if (status === 'unavailable' || status === 'undeployable') return 'danger';
  return 'warning';
}

export function serverReachabilityTone(server: ServerDto): 'success' | 'danger' {
  return server.reachable ? 'success' : 'danger';
}

/**
 * 列表/概况对外只暴露这一枚综合状态，避免「可达」掩盖 Agent 受阻等更严重问题。
 * 优先级：不可达 → 主机密钥不匹配 → Agent 受阻/不可用/不匹配/未安装 → 正常。
 */
export type ServerSummaryKind =
  | 'unreachable'
  | 'hostKeyMismatch'
  | 'agentBlocked'
  | 'agentUnavailable'
  | 'agentMismatch'
  | 'agentNotInstalled'
  | 'ready';

export function serverSummaryKind(server: ServerDto): ServerSummaryKind {
  if (!server.reachable) return 'unreachable';
  if (server.hostKeyMismatch) return 'hostKeyMismatch';
  const enabled = server.traits?.['agent.enabled'] === 'true';
  const status = server.traits?.['agent.status'];
  if (status === 'undeployable') return 'agentBlocked';
  if (status === 'unavailable') return 'agentUnavailable';
  if (status === 'compatible' && enabled) return 'ready';
  if (enabled || status === 'incompatible') return 'agentMismatch';
  return 'agentNotInstalled';
}

export function serverSummaryStatusKey(server: ServerDto) {
  return `serversPage.summary.${serverSummaryKind(server)}` as const;
}

export function serverSummaryTone(server: ServerDto): 'success' | 'warning' | 'danger' {
  switch (serverSummaryKind(server)) {
    case 'ready':
      return 'success';
    case 'agentMismatch':
    case 'agentNotInstalled':
      return 'warning';
    default:
      return 'danger';
  }
}

/** Agent 能力的用户可见状态文案 key；Agent tab 任务区旁的细粒度标签仍可复用。 */
export function agentStatusKey(server: ServerDto) {
  const status = server.traits?.['agent.status'];
  if (status === 'compatible') return 'serversPage.agentCompatible';
  if (status === 'unavailable') return 'serversPage.agentUnavailable';
  if (status === 'undeployable') return 'serversPage.agentUndeployable';
  return server.traits?.['agent.enabled'] === 'true' ? 'serversPage.agentIncompatible' : 'serversPage.agentNotInstalled';
}

/** Report stream 稳定性等级；来源固定为 traits 的 `agent.report.stability`。 */
export const reportStabilityGrades = ['excellent', 'good', 'fair', 'poor', 'critical'] as const;

export type ReportStabilityGrade = (typeof reportStabilityGrades)[number];

export type ReportStabilityTone = 'success' | 'info' | 'warning' | 'danger' | 'neutral';

export function reportStreamStatus(server: ServerDto): 'connected' | 'disconnected' | '' {
  const raw = server.traits?.['agent.report.status'] ?? '';
  if (raw === 'connected' || raw === 'disconnected') return raw;
  return '';
}

export function reportStabilityGrade(server: ServerDto): ReportStabilityGrade | '' {
  const raw = server.traits?.['agent.report.stability'] ?? '';
  return (reportStabilityGrades as readonly string[]).includes(raw) ? (raw as ReportStabilityGrade) : '';
}

export function reportDisconnectCount1h(server: ServerDto): number | null {
  const raw = server.traits?.['agent.report.disconnect_count_1h'];
  if (raw == null || raw === '') return null;
  const count = Number(raw);
  return Number.isFinite(count) && count >= 0 ? count : null;
}

export function reportStabilityStatusKey(server: ServerDto) {
  const grade = reportStabilityGrade(server);
  return grade ? (`serversPage.reportStability.${grade}` as const) : ('serversPage.reportStability.unknown' as const);
}

export function reportStabilityTone(server: ServerDto): ReportStabilityTone {
  switch (reportStabilityGrade(server)) {
    case 'excellent':
      return 'success';
    case 'good':
      return 'info';
    case 'fair':
      return 'warning';
    case 'poor':
    case 'critical':
      return 'danger';
    default:
      return 'neutral';
  }
}

export function reportStreamStatusKey(server: ServerDto) {
  const status = reportStreamStatus(server);
  if (status === 'connected') return 'serversPage.reportStream.connected' as const;
  if (status === 'disconnected') return 'serversPage.reportStream.disconnected' as const;
  return 'serversPage.reportStream.unknown' as const;
}

export function reportStreamTone(server: ServerDto): ReportStabilityTone {
  const status = reportStreamStatus(server);
  if (status === 'connected') return 'success';
  if (status === 'disconnected') return 'warning';
  return 'neutral';
}

/** 列表仅在非优时提示抖动，避免与综合状态徽标抢视线。 */
export function shouldShowListReportStability(server: ServerDto) {
  const grade = reportStabilityGrade(server);
  return Boolean(grade && grade !== 'excellent');
}

/** 特权能力文案 key；列表行与概况 tab 共用。 */
export function privilegeStatusKey(server: ServerDto) {
  if (server.privilege?.privileged) return 'serversPage.privileged';
  if (server.sudo?.passwordless) return 'serversPage.passwordlessSudo';
  return 'serversPage.noPrivilege';
}

export function canRunPrivilegedOperation(server: ServerDto | null) {
  return Boolean(server?.reachable && (server.privilege?.privileged || server.sudo?.passwordless));
}

/** 节点侧 Tailscale 观测状态取值；来源固定为 traits 的 `tailscale.*`。 */
export const tailscaleStatuses = ['disabled', 'pending', 'installing', 'running', 'degraded', 'unsupported', 'error'] as const;

export type TailscaleStatus = (typeof tailscaleStatuses)[number];

export type TailscaleTone = 'neutral' | 'success' | 'warning' | 'danger' | 'info';

/** 未上报或取值未知时返回空串：不臆造节点运行状态。 */
export function tailscaleObservedStatus(server: ServerDto): TailscaleStatus | '' {
  const raw = server.traits?.['tailscale.status'] ?? '';
  return (tailscaleStatuses as readonly string[]).includes(raw) ? (raw as TailscaleStatus) : '';
}

export function tailscaleStatusKey(server: ServerDto) {
  const status = tailscaleObservedStatus(server);
  return status ? `serversPage.tailscale.status.${status}` : 'serversPage.tailscale.noStatus';
}

export function tailscaleTone(server: ServerDto): TailscaleTone {
  const status = tailscaleObservedStatus(server);
  if (status === 'running') return 'success';
  if (status === 'degraded' || status === 'pending' || status === 'installing') return 'warning';
  if (status === 'error') return 'danger';
  if (status === 'unsupported') return 'info';
  return 'neutral';
}

export function tailscaleTrait(server: ServerDto, key: 'ipv4' | 'ipv6' | 'hostname' | 'lastError' | 'updatedAt') {
  return server.traits?.[`tailscale.${key}`] ?? '';
}

/** 未启用 Tailscale 的节点不参与 prefer 分支：开关禁用，提交时固定为 false。 */
export function tailscalePreferences(input: Pick<ServerSaveInput, 'tailscaleEnabled' | 'tailscalePreferAgent' | 'tailscalePreferInterconnect'>) {
  if (!input.tailscaleEnabled) return { tailscalePreferAgent: false, tailscalePreferInterconnect: false };
  return { tailscalePreferAgent: input.tailscalePreferAgent, tailscalePreferInterconnect: input.tailscalePreferInterconnect };
}

/** 代理模式下拉与详情展示的文案 key；未知值按直连处理。 */
export function proxyModeLabelKey(mode: unknown) {
  const normalized = normalizeServerProxyMode(mode);
  if (normalized === 'ssh_tunnel') return 'serversPage.proxy.modeSshTunnel' as const;
  if (normalized === 'socks5') return 'serversPage.proxy.modeSocks5' as const;
  return 'serversPage.proxy.modeDirect' as const;
}

export function serverProxyMode(server: Pick<ServerDto, 'proxyMode'>): ServerProxyMode {
  return normalizeServerProxyMode(server.proxyMode);
}

/** 非直连服务器在列表中的紧凑标识；直连返回空串。 */
export function proxyShortLabelKey(server: Pick<ServerDto, 'proxyMode'>) {
  const mode = serverProxyMode(server);
  if (mode === 'ssh_tunnel') return 'serversPage.proxy.short.ssh_tunnel' as const;
  if (mode === 'socks5') return 'serversPage.proxy.short.socks5' as const;
  return '';
}

/** 隧道名称回退：派生名优先，缺失时回退到隧道服务器 id，便于定位悬空引用。 */
export function proxyTunnelDisplayName(server: Pick<ServerDto, 'tunnelServerName' | 'tunnelServerId'>) {
  return (server.tunnelServerName || server.tunnelServerId || '').trim();
}

/** 把派生错误码映射到翻译 key；未知码回退到通用错误，避免展示原始英文码。 */
export function proxyErrorKey(server: Pick<ServerDto, 'proxyError'>) {
  const code = server.proxyError;
  if (code === 'tunnel_missing' || code === 'cycle' || code === 'chain_too_deep' || code === 'socks5_address_missing' || code === 'socks5_address_invalid') {
    return `serversPage.proxy.error.${code}` as const;
  }
  return 'serversPage.proxy.error.unknown' as const;
}

/** 列表紧凑提示的 tooltip：无效时直接给出错误原因。 */
export function proxyListHintKey(server: Pick<ServerDto, 'proxyInvalid' | 'proxyError'>) {
  return server.proxyInvalid ? proxyErrorKey(server) : 'serversPage.proxy.listHint';
}

/**
 * 合并父页面分页列表与全量服务器列表作为隧道候选：按 id 去重（当前页数据优先，
 * 保持与页面状态一致），并始终排除当前编辑服务器自身。
 */
export function mergeServerCandidates(pageServers: ServerDto[], allServers: ServerDto[], selfId?: string): ServerDto[] {
  const byId = new Map<string, ServerDto>();
  for (const item of pageServers) byId.set(item.id, item);
  for (const item of allServers) if (!byId.has(item.id)) byId.set(item.id, item);
  return [...byId.values()].filter((item) => !selfId || item.id !== selfId);
}

export interface ServerProxyValidationErrors {
  tunnelServerId?: string;
  socks5Address?: string;
  socks5Username?: string;
}

/** 表单代理草稿的字段形态；proxyMode 保持宽松，由 normalize 兜底未知值。 */
export interface ServerProxyDraftInput {
  proxyMode?: unknown;
  tunnelServerId: string;
  socks5Address: string;
  socks5Username: string;
  socks5Password: string;
}

/**
 * 代理草稿的本地校验：只覆盖可本地判定的规则（必选、自引用、host:port、密码需用户名）；
 * 隧道存在性、成环与层级由保存接口兜底。
 */
export function validateProxyInput(input: ServerProxyDraftInput, options: { selfId?: string } = {}): ServerProxyValidationErrors {
  const errors: ServerProxyValidationErrors = {};
  const mode = normalizeServerProxyMode(input.proxyMode);
  if (mode === 'ssh_tunnel') {
    const tunnelId = input.tunnelServerId.trim();
    if (!tunnelId) errors.tunnelServerId = 'serversPage.proxy.validationTunnelRequired';
    else if (options.selfId && tunnelId === options.selfId) errors.tunnelServerId = 'serversPage.proxy.validationTunnelSelf';
  } else if (mode === 'socks5') {
    if (!input.socks5Address.trim()) errors.socks5Address = 'serversPage.proxy.validationSocks5AddressRequired';
    else if (!isSocks5Address(input.socks5Address)) errors.socks5Address = 'serversPage.proxy.validationSocks5AddressInvalid';
    if (input.socks5Password.trim() && !input.socks5Username.trim()) errors.socks5Username = 'serversPage.proxy.validationSocks5UsernameRequired';
  }
  return errors;
}

/** 保存 payload 的代理字段：非当前模式一律清空，避免把残留草稿带到其他模式。 */
export function proxySavePayload(input: ServerProxyDraftInput) {
  const mode = normalizeServerProxyMode(input.proxyMode);
  return {
    proxyMode: mode,
    tunnelServerId: mode === 'ssh_tunnel' ? input.tunnelServerId.trim() : '',
    socks5Address: mode === 'socks5' ? input.socks5Address.trim() : '',
    socks5Username: mode === 'socks5' ? input.socks5Username.trim() : '',
    socks5Password: mode === 'socks5' ? input.socks5Password : '',
  };
}

export function credentialReferences(credentialId: string, servers: ServerDto[]) {
  return servers.filter((server) => server.credentialId === credentialId).map((server) => ({ id: server.id, name: server.name, host: server.host }));
}

export function credentialLabel(credentialId: string, credentials: CredentialDto[]) {
  const credential = credentials.find((item) => item.id === credentialId);
  return credential ? `${credential.name} / ${credential.username}` : '';
}

export function isIPv4(value: string) {
  const parts = value.trim().split('.');
  if (parts.length !== 4) return false;
  return parts.every((part) => /^\d{1,3}$/.test(part) && Number(part) <= 255);
}

export function isIPv6(value: string) {
  const trimmed = value.trim();
  if (!trimmed) return false;
  const candidate = trimmed.startsWith('[') && trimmed.endsWith(']') ? trimmed.slice(1, -1) : trimmed;
  if (!candidate.includes(':')) return false;
  const groups = candidate.split('::');
  if (groups.length > 2) return false;
  const [head, tail] = groups;
  const headGroups = head ? head.split(':') : [];
  const tailGroups = tail ? tail.split(':') : [];
  const all = [...headGroups, ...tailGroups];
  if (all.some((group) => !/^[0-9a-fA-F]{1,4}$/.test(group))) return false;
  const total = headGroups.length + tailGroups.length;
  return groups.length === 2 ? total < 8 : total === 8;
}

export function connectionHost(input: Pick<ServerSaveInput, 'ipv4' | 'ipv6'>) {
  return input.ipv4.trim() || input.ipv6.trim();
}

export type PairIssueKind = 'missing_separator' | 'empty_key' | 'duplicate_key';

export interface PairIssue {
  line: number;
  kind: PairIssueKind;
  key?: string;
}

export interface PairsParseResult {
  pairs: Record<string, string>;
  issues: PairIssue[];
}

/** 逐行解析 key=value；缺分隔符和空变量名是阻断错误，重复 key 按 last-wins 保留但记录警告。 */
export function parsePairs(raw: string): PairsParseResult {
  const pairs: Record<string, string> = {};
  const issues: PairIssue[] = [];
  raw.split('\n').forEach((line, index) => {
    const trimmed = line.trim();
    if (!trimmed) return;
    const lineNumber = index + 1;
    const separator = trimmed.indexOf('=');
    if (separator < 0) {
      issues.push({ line: lineNumber, kind: 'missing_separator' });
      return;
    }
    const key = trimmed.slice(0, separator).trim();
    if (!key) {
      issues.push({ line: lineNumber, kind: 'empty_key' });
      return;
    }
    if (key in pairs) issues.push({ line: lineNumber, kind: 'duplicate_key', key });
    pairs[key] = trimmed.slice(separator + 1).trim();
  });
  return { pairs, issues };
}

export function hasBlockingPairIssues(issues: PairIssue[]) {
  return issues.some((issue) => issue.kind !== 'duplicate_key');
}

export function stringifyPairs(value?: Record<string, string>) {
  return Object.entries(value ?? {}).map(([key, val]) => `${key}=${val}`).join('\n');
}

/** 探测只依赖连接字段与代理草稿；名称、Docker Host、变量、备注的错误不得连坐探测按钮。 */
export function validateProbeInput(input: ServerProbeInput) {
  const errors: Partial<Record<'ipv4' | 'ipv6' | 'port' | 'credentialId' | 'tunnelServerId' | 'socks5Address' | 'socks5Username', string>> = {};
  if (!input.ipv4.trim() && !input.ipv6.trim()) errors.ipv4 = 'serversPage.validationAddressRequired';
  else if (input.ipv4.trim() && !isIPv4(input.ipv4)) errors.ipv4 = 'serversPage.validationIpv4';
  if (input.ipv6.trim() && !isIPv6(input.ipv6)) errors.ipv6 = 'serversPage.validationIpv6';
  if (!input.credentialId.trim()) errors.credentialId = 'serversPage.validationCredential';
  if (!Number.isFinite(input.port) || input.port < 1 || input.port > 65535) errors.port = 'serversPage.validationPort';
  return { ...errors, ...validateProxyInput({
    proxyMode: input.proxyMode ?? 'direct',
    tunnelServerId: input.tunnelServerId ?? '',
    socks5Address: input.socks5Address ?? '',
    socks5Username: input.socks5Username ?? '',
    socks5Password: input.socks5Password ?? '',
  }, { selfId: input.serverId }) };
}

/** 探测实际使用的连接字段签名；代理草稿变化同样视为旧探测结果过期。 */
export function connectionSignature(input: ServerProbeInput) {
  return JSON.stringify([
    input.ipv4.trim(),
    input.ipv6.trim(),
    input.port,
    input.sshUsername.trim(),
    input.credentialId,
    normalizeServerProxyMode(input.proxyMode),
    (input.tunnelServerId ?? '').trim(),
    (input.socks5Address ?? '').trim(),
    (input.socks5Username ?? '').trim(),
    input.socks5Password ?? '',
  ]);
}

export function validateServerInput(input: ServerSaveInput, options: { selfId?: string } = {}) {
  const errors: Partial<Record<keyof ServerSaveInput, string>> = {};
  if (!input.name.trim()) errors.name = 'serversPage.validationName';
  if (!input.ipv4.trim() && !input.ipv6.trim()) errors.ipv4 = 'serversPage.validationAddressRequired';
  if (input.ipv4.trim() && !isIPv4(input.ipv4)) errors.ipv4 = 'serversPage.validationIpv4';
  if (input.ipv6.trim() && !isIPv6(input.ipv6)) errors.ipv6 = 'serversPage.validationIpv6';
  if (!input.credentialId.trim()) errors.credentialId = 'serversPage.validationCredential';
  if (!Number.isFinite(input.port) || input.port < 1 || input.port > 65535) errors.port = 'serversPage.validationPort';
  if (!input.dockerHost.trim()) errors.dockerHost = 'serversPage.validationDocker';
  Object.assign(errors, validateProxyInput(input, options));
  return errors;
}
