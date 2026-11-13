import type { ServerProxyErrorCode, ServerProxyMode } from '@/types/servers';

/** 代理链路最多允许的隧道跳数（契约 3.2 / 4）。 */
export const sshTunnelMaxDepth = 8;

export function normalizeServerProxyMode(value: unknown): ServerProxyMode {
  return value === 'ssh_tunnel' || value === 'socks5' ? value : 'direct';
}

function isValidPort(raw: string) {
  if (!/^\d{1,5}$/.test(raw)) return false;
  const port = Number(raw);
  return port >= 1 && port <= 65535;
}

/**
 * SOCKS5 地址必须是 `host:port`：host 可为域名、IPv4 或方括号包裹的 IPv6，
 * 端口为 1-65535。空串与多冒号的裸 IPv6 均不合法（与后端 SplitHostPort 语义一致）。
 */
export function isSocks5Address(raw: string): boolean {
  const value = raw.trim();
  if (!value) return false;
  const bracketed = /^\[([0-9a-fA-F:.]+)\]:(\d{1,5})$/.exec(value);
  if (bracketed) return bracketed[1].includes(':') && isValidPort(bracketed[2] ?? '');
  const separator = value.lastIndexOf(':');
  if (separator <= 0 || separator === value.length - 1) return false;
  const host = value.slice(0, separator);
  const port = value.slice(separator + 1);
  if (host.includes(':') || /[\s/@]/.test(host)) return false;
  if (!/^[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?$/.test(host)) return false;
  return isValidPort(port);
}

/** 只含解析代理链所需的字段，便于 mock 与展示复用。 */
export interface ServerProxyNode {
  id: string;
  name?: string;
  proxyMode?: ServerProxyMode;
  tunnelServerId?: string;
  socks5Address?: string;
}

export interface DerivedServerProxy {
  tunnelServerName: string;
  proxyInvalid: boolean;
  proxyError: ServerProxyErrorCode | '';
}

function socks5AddressError(raw: string | undefined): ServerProxyErrorCode | '' {
  const address = (raw ?? '').trim();
  if (!address) return 'socks5_address_missing';
  return isSocks5Address(address) ? '' : 'socks5_address_invalid';
}

function resolveTunnelChain(target: ServerProxyNode, byId: Map<string, ServerProxyNode>): ServerProxyErrorCode | '' {
  const visited = new Set<string>([target.id]);
  let current: ServerProxyNode | undefined = target;
  let hops = 0;
  while (current) {
    const mode = normalizeServerProxyMode(current.proxyMode);
    if (mode === 'direct') return '';
    if (mode === 'socks5') return socks5AddressError(current.socks5Address);
    hops += 1;
    if (hops > sshTunnelMaxDepth) return 'chain_too_deep';
    const next = byId.get((current.tunnelServerId ?? '').trim());
    if (!next) return 'tunnel_missing';
    if (visited.has(next.id)) return 'cycle';
    visited.add(next.id);
    current = next;
  }
  return '';
}

/** 读取时派生：解析隧道名称，并按契约返回稳定错误码（缺失/成环/超层/SOCKS5 地址问题）。 */
export function deriveServerProxy(target: ServerProxyNode, nodes: ServerProxyNode[]): DerivedServerProxy {
  const byId = new Map(nodes.filter((node) => node.id).map((node) => [node.id, node]));
  const mode = normalizeServerProxyMode(target.proxyMode);
  if (mode === 'direct') return { tunnelServerName: '', proxyInvalid: false, proxyError: '' };
  if (mode === 'socks5') {
    const proxyError = socks5AddressError(target.socks5Address);
    return { tunnelServerName: '', proxyInvalid: Boolean(proxyError), proxyError };
  }
  const immediate = byId.get((target.tunnelServerId ?? '').trim());
  const proxyError = resolveTunnelChain(target, byId);
  return { tunnelServerName: immediate?.name ?? '', proxyInvalid: Boolean(proxyError), proxyError };
}

/**
 * 保存前的整链校验（mock 与真实后端语义一致）：
 * 返回契约稳定错误码；`candidate` 会替换/追加到 `nodes` 后再解析，以便检测成环与层级。
 */
export function proxyChainSaveError(candidate: ServerProxyNode, nodes: ServerProxyNode[]): ServerProxyErrorCode | '' {
  const mode = normalizeServerProxyMode(candidate.proxyMode);
  if (mode === 'direct') return '';
  if (mode === 'ssh_tunnel') {
    const tunnelId = (candidate.tunnelServerId ?? '').trim();
    if (!tunnelId) return 'tunnel_missing';
    if (tunnelId === candidate.id) return 'cycle';
    const nextNodes = nodes.some((node) => node.id === candidate.id)
      ? nodes.map((node) => (node.id === candidate.id ? { ...node, ...candidate } : node))
      : [...nodes, candidate];
    return deriveServerProxy(candidate, nextNodes).proxyError;
  }
  return socks5AddressError(candidate.socks5Address);
}
