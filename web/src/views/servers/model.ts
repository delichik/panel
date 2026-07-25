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

export function validateServerInput(input: ServerSaveInput) {
  const errors: Partial<Record<keyof ServerSaveInput, string>> = {};
  if (!input.name.trim()) errors.name = 'serversPage.validationName';
  if (!input.host.trim()) errors.host = 'serversPage.validationHost';
  if (!input.credentialId.trim()) errors.credentialId = 'serversPage.validationCredential';
  if (!Number.isFinite(input.port) || input.port < 1 || input.port > 65535) errors.port = 'serversPage.validationPort';
  if (!input.dockerHost.trim()) errors.dockerHost = 'serversPage.validationDocker';
  return errors;
}
