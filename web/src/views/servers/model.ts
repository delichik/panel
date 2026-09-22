import type { ServerDto, ServerSaveInput } from '@/types/servers';
import type { CredentialDto } from '@/types/credentials';

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

export function canRunPrivilegedOperation(server: ServerDto | null) {
  return Boolean(server?.reachable && (server.privilege?.privileged || server.sudo?.passwordless));
}

export function canInstallUfw(server: ServerDto | null) {
  if (!server || !canRunPrivilegedOperation(server)) return false;
  return server.traits?.['sys.ufw_supported'] === 'true' && server.traits?.['sys.ufw_installed'] !== 'true';
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

export type ServerProbeInput = Pick<ServerSaveInput, 'ipv4' | 'ipv6' | 'port' | 'sshUsername' | 'credentialId'>;

/** 探测只依赖连接字段；名称、Docker Host、变量、备注的错误不得连坐探测按钮。 */
export function validateProbeInput(input: ServerProbeInput) {
  const errors: Partial<Record<'ipv4' | 'ipv6' | 'port' | 'credentialId', string>> = {};
  if (!input.ipv4.trim() && !input.ipv6.trim()) errors.ipv4 = 'serversPage.validationAddressRequired';
  else if (input.ipv4.trim() && !isIPv4(input.ipv4)) errors.ipv4 = 'serversPage.validationIpv4';
  if (input.ipv6.trim() && !isIPv6(input.ipv6)) errors.ipv6 = 'serversPage.validationIpv6';
  if (!input.credentialId.trim()) errors.credentialId = 'serversPage.validationCredential';
  if (!Number.isFinite(input.port) || input.port < 1 || input.port > 65535) errors.port = 'serversPage.validationPort';
  return errors;
}

/** 探测实际使用的连接字段签名；变化即视为旧探测结果过期。 */
export function connectionSignature(input: ServerProbeInput) {
  return JSON.stringify([input.ipv4.trim(), input.ipv6.trim(), input.port, input.sshUsername.trim(), input.credentialId]);
}

export function validateServerInput(input: ServerSaveInput) {
  const errors: Partial<Record<keyof ServerSaveInput, string>> = {};
  if (!input.name.trim()) errors.name = 'serversPage.validationName';
  if (input.kind !== 'normal' && input.kind !== 'nat') errors.kind = 'serversPage.validationKind';
  if (!input.ipv4.trim() && !input.ipv6.trim()) errors.ipv4 = 'serversPage.validationAddressRequired';
  if (input.ipv4.trim() && !isIPv4(input.ipv4)) errors.ipv4 = 'serversPage.validationIpv4';
  if (input.ipv6.trim() && !isIPv6(input.ipv6)) errors.ipv6 = 'serversPage.validationIpv6';
  if (!input.credentialId.trim()) errors.credentialId = 'serversPage.validationCredential';
  if (!Number.isFinite(input.port) || input.port < 1 || input.port > 65535) errors.port = 'serversPage.validationPort';
  if (!input.dockerHost.trim()) errors.dockerHost = 'serversPage.validationDocker';
  return errors;
}
