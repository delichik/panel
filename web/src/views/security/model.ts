import YAML from 'yaml';
import type { ServerDto } from '@/types/servers';
import type { Fail2BanJail, Fail2BanState, UfwAllowInput, UfwState } from '@/types/security';

export type Tone = 'neutral' | 'success' | 'warning' | 'danger' | 'info';

export function serverOptionState(server: ServerDto) {
  const agentReady = server.traits?.['agent.status'] === 'compatible' && Boolean(server.traits?.['agent.url']);
  const privileged = Boolean(server.privilege?.privileged || server.sudo?.passwordless || server.privilege?.mode === 'root' || server.privilege?.mode === 'passwordless_sudo');
  return {
    agentReady,
    privileged,
    reachable: server.reachable,
    canManageSecurity: server.reachable && (agentReady || privileged),
  };
}

export function ufwTone(state: UfwState | null): Tone {
  if (!state) return 'neutral';
  if (!state.supported) return 'danger';
  if (!state.installed) return 'warning';
  if (!state.active) return 'warning';
  return 'success';
}

export function fail2BanTone(state: Fail2BanState | null): Tone {
  if (!state) return 'neutral';
  if (!state.installed) return 'warning';
  if (state.managed && state.active) return 'success';
  if (state.panelConfigPresent && !state.managed) return 'warning';
  return 'info';
}

export function validateUfwRule(input: UfwAllowInput) {
  const errors: Partial<Record<keyof UfwAllowInput, string>> = {};
  if (!Number.isInteger(input.port) || input.port < 1 || input.port > 65535) errors.port = 'securityPage.validationPort';
  if (!['tcp', 'udp'].includes(input.protocol)) errors.protocol = 'securityPage.validationProtocol';
  if (!input.from.trim()) errors.from = 'securityPage.validationFrom';
  return errors;
}


const SIMPLE_JAIL_KEYS = new Set(['name', 'enabled', 'preset', 'filter', 'logpath', 'backend', 'port', 'protocol', 'action', 'maxretry', 'findtime', 'bantime', 'ignoreip']);

/**
 * Detects Fail2Ban YAML content that the simple visual draft cannot represent
 * (unknown top-level keys, per-jail advanced keys, or an options map).
 * Switching back to visual mode would silently drop this content.
 */
export function hasAdvancedJailConfig(raw: string): boolean {
  const trimmed = (raw ?? '').trim();
  if (!trimmed) return false;
  let parsed: unknown;
  try {
    parsed = YAML.parse(trimmed);
  } catch {
    return false;
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return false;
  const record = parsed as Record<string, unknown>;
  if (Object.keys(record).some((key) => key !== 'jails')) return true;
  const jails = record.jails;
  if (!Array.isArray(jails)) return false;
  return jails.some((item) => {
    if (!item || typeof item !== 'object' || Array.isArray(item)) return false;
    return Object.keys(item as Record<string, unknown>).some((key) => !SIMPLE_JAIL_KEYS.has(key));
  });
}

export function fail2BanPreset(name: 'ssh' | 'nginx-auth' | 'recidive'): Fail2BanJail {
  if (name === 'nginx-auth') {
    return { name: 'nginx-auth', enabled: true, preset: 'nginx', filter: 'nginx-http-auth', port: 'http,https', logpath: '/var/log/nginx/error.log', backend: 'auto', maxretry: 6, findtime: '10m', bantime: '1h' };
  }
  if (name === 'recidive') {
    return { name: 'recidive', enabled: true, preset: 'recidive', filter: 'recidive', port: 'all', logpath: '/var/log/fail2ban.log', backend: 'auto', maxretry: 5, findtime: '1d', bantime: '1w' };
  }
  return { name: 'sshd', enabled: true, preset: 'ssh', filter: 'sshd', port: 'ssh', logpath: '/var/log/auth.log', backend: 'systemd', maxretry: 5, findtime: '10m', bantime: '1h', ignoreip: ['127.0.0.1/8'] };
}

export function jailsToYaml(jails: Fail2BanJail[]) {
  const lines = ['jails:'];
  jails.forEach((jail) => {
    lines.push(`  - name: ${jail.name}`);
    lines.push(`    enabled: ${jail.enabled ? 'true' : 'false'}`);
    field(lines, 'preset', jail.preset);
    field(lines, 'filter', jail.filter);
    field(lines, 'port', jail.port);
    field(lines, 'logpath', jail.logpath);
    field(lines, 'backend', jail.backend);
    if (typeof jail.maxretry === 'number') lines.push(`    maxretry: ${jail.maxretry}`);
    field(lines, 'findtime', jail.findtime);
    field(lines, 'bantime', jail.bantime);
    if (jail.ignoreip?.length) {
      lines.push('    ignoreip:');
      jail.ignoreip.forEach((item) => lines.push(`      - ${item}`));
    }
  });
  return `${lines.join('\n')}\n`;
}

function field(lines: string[], key: string, value?: string) {
  if (value && value.trim()) lines.push(`    ${key}: ${value}`);
}

export function parseSimpleJailsFromYaml(raw: string): Fail2BanJail[] {
  const blocks = raw.split(/\n\s*-\s+name:\s*/).slice(1);
  return blocks.map((block) => {
    const [nameLine, ...rest] = block.split('\n');
    const jail: Fail2BanJail = { name: nameLine.trim(), enabled: !/enabled:\s*false/.test(block) };
    rest.forEach((line) => {
      const match = line.match(/^\s*([a-zA-Z0-9_]+):\s*(.+)$/);
      if (!match) return;
      const key = match[1] as keyof Fail2BanJail;
      const value = match[2].trim();
      if (key === 'maxretry') jail.maxretry = Number(value);
      else if (key !== 'enabled' && key !== 'name') (jail as unknown as Record<string, unknown>)[key] = value;
    });
    return jail;
  });
}
