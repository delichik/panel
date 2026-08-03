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
  return trimmed.includes(':') && /^[0-9a-fA-F:.]+$/.test(trimmed);
}

export function connectionHost(input: Pick<ServerSaveInput, 'ipv4' | 'ipv6'>) {
  return input.ipv4.trim() || input.ipv6.trim();
}

export function validateServerInput(input: ServerSaveInput) {
  const errors: Partial<Record<keyof ServerSaveInput, string>> = {};
  if (!input.name.trim()) errors.name = 'serversPage.validationName';
  if (!input.ipv4.trim() && !input.ipv6.trim()) errors.ipv4 = 'serversPage.validationAddressRequired';
  if (input.ipv4.trim() && !isIPv4(input.ipv4)) errors.ipv4 = 'serversPage.validationIpv4';
  if (input.ipv6.trim() && !isIPv6(input.ipv6)) errors.ipv6 = 'serversPage.validationIpv6';
  if (!input.credentialId.trim()) errors.credentialId = 'serversPage.validationCredential';
  if (!Number.isFinite(input.port) || input.port < 1 || input.port > 65535) errors.port = 'serversPage.validationPort';
  if (!input.dockerHost.trim()) errors.dockerHost = 'serversPage.validationDocker';
  return errors;
}
