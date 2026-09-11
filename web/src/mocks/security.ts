import type { Fail2BanState, UfwAllowInput, UfwState } from '@/types/security';

const defaultYaml = `jails:
  - name: sshd
    enabled: true
    preset: ssh
    filter: sshd
    port: ssh
    logpath: /var/log/auth.log
    backend: systemd
    maxretry: 5
    findtime: 10m
    bantime: 1h
    ignoreip:
      - 127.0.0.1/8
`;

const ufwStates: Record<string, UfwState> = {
  'srv-edge-sgp': {
    serverId: 'srv-edge-sgp',
    supported: true,
    installed: true,
    active: true,
    status: 'active',
    defaultPolicy: 'deny incoming, allow outgoing',
    rules: [
      { number: 1, to: '22/tcp', action: 'ALLOW IN', from: '10.8.0.0/16' },
      { number: 2, to: '80/tcp', action: 'ALLOW IN', from: 'Anywhere' },
      { number: 3, to: '443/tcp', action: 'ALLOW IN', from: 'Anywhere' },
    ],
  },
  'srv-core-fra': { serverId: 'srv-core-fra', supported: true, installed: false, active: false, status: 'inactive', defaultPolicy: '', rules: [] },
  'srv-lab-dead': { serverId: 'srv-lab-dead', supported: false, installed: false, active: false, status: 'unreachable', defaultPolicy: '', rules: [] },
};

const fail2banStates: Record<string, Fail2BanState> = {
  'srv-edge-sgp': {
    serverId: 'srv-edge-sgp',
    installed: true,
    active: true,
    managed: true,
    panelConfigPresent: true,
    jails: ['sshd', 'nginx-auth'],
    raw: 'Status\n|- Number of jail: 2\n`- Jail list: sshd, nginx-auth',
    configYaml: defaultYaml,
    config: { jails: [{ name: 'sshd', enabled: true, preset: 'ssh', filter: 'sshd', port: 'ssh', logpath: '/var/log/auth.log', backend: 'systemd', maxretry: 5, findtime: '10m', bantime: '1h', ignoreip: ['127.0.0.1/8'] }] },
    updatedAt: '2026-07-21T02:00:00.000Z',
  },
  'srv-core-fra': {
    serverId: 'srv-core-fra',
    installed: true,
    active: true,
    managed: false,
    panelConfigPresent: false,
    jails: ['sshd'],
    raw: 'Existing fail2ban service is active. Confirm takeover before Seamark writes panel.local.',
    configYaml: defaultYaml,
    config: { jails: [{ name: 'sshd', enabled: true, preset: 'ssh', filter: 'sshd', port: 'ssh', logpath: '/var/log/auth.log', backend: 'systemd', maxretry: 5 }] },
  },
  'srv-lab-dead': {
    serverId: 'srv-lab-dead',
    installed: false,
    active: false,
    managed: false,
    panelConfigPresent: false,
    jails: [],
    raw: '',
    configYaml: defaultYaml,
    config: { jails: [] },
  },
};

export function mockUfwState(serverId: string) {
  if (serverId.includes('dead') || serverId.includes('timeout')) throw new Error('Server is unreachable.');
  if (!ufwStates[serverId]) ufwStates[serverId] = defaultUfwState(serverId);
  return ufwStates[serverId];
}

export function mockAddUfwRule(serverId: string, input: UfwAllowInput) {
  const state = ufwStates[serverId];
  if (!state) return null;
  if (!state.installed) throw new Error('UFW is not installed on this server.');
  state.rules = [...state.rules, { number: Math.max(0, ...state.rules.map((rule) => rule.number)) + 1, to: `${input.port}/${input.protocol}`, action: 'ALLOW IN', from: input.from }];
  return state;
}

export function mockDeleteUfwRule(serverId: string, number: number) {
  const state = ufwStates[serverId];
  if (!state) return null;
  state.rules = state.rules.filter((rule) => rule.number !== number);
  return state;
}

export function mockEnableUfw(serverId: string) {
  const state = ufwStates[serverId];
  if (!state) return false;
  state.installed = true;
  state.active = true;
  state.status = 'active';
  if (!state.rules.length) state.rules = [{ number: 1, to: '22/tcp', action: 'ALLOW IN', from: 'Anywhere' }];
  return true;
}

export function mockFail2BanState(serverId: string) {
  if (serverId.includes('dead') || serverId.includes('timeout')) throw new Error('Server connectivity has not been confirmed.');
  if (!fail2banStates[serverId]) fail2banStates[serverId] = defaultFail2BanState(serverId);
  return fail2banStates[serverId];
}

export function mockSaveFail2Ban(serverId: string, configYaml: string) {
  const state = fail2banStates[serverId];
  if (!state) return null;
  if (configYaml.includes('broken:')) throw new Error('fail2ban YAML is invalid: unknown field broken');
  state.configYaml = configYaml;
  state.updatedAt = new Date().toISOString();
  return state;
}

export function mockEnableFail2Ban(serverId: string, confirmTakeover: boolean) {
  const state = fail2banStates[serverId];
  if (!state) return 'missing';
  if (state.installed && !state.managed && !confirmTakeover) return 'takeover';
  state.installed = true;
  state.active = true;
  state.managed = true;
  state.panelConfigPresent = true;
  return 'accepted';
}

export function mockReleaseFail2Ban(serverId: string) {
  const state = fail2banStates[serverId];
  if (!state) return false;
  state.managed = false;
  state.panelConfigPresent = false;
  return true;
}

function defaultUfwState(serverId: string): UfwState {
  const active = !serverId.includes('cache') && !serverId.includes('legacy') && !serverId.includes('staging');
  return {
    serverId,
    supported: true,
    installed: active || serverId.includes('db') || serverId.includes('api'),
    active,
    status: active ? 'active' : 'inactive',
    defaultPolicy: active ? 'deny incoming, allow outgoing' : '',
    rules: active ? [
      { number: 1, to: '22/tcp', action: 'ALLOW IN', from: '10.0.0.0/8' },
      { number: 2, to: '443/tcp', action: 'ALLOW IN', from: 'Anywhere' },
      ...(serverId.includes('db') ? [{ number: 3, to: '5432/tcp', action: 'ALLOW IN', from: '10.12.0.0/16' }] : []),
    ] : [],
  };
}

function defaultFail2BanState(serverId: string): Fail2BanState {
  const managed = !serverId.includes('legacy') && !serverId.includes('staging');
  const active = !serverId.includes('media');
  return {
    serverId,
    installed: active,
    active,
    managed: active && managed,
    panelConfigPresent: active && managed,
    jails: active ? ['sshd', ...(serverId.includes('api') ? ['nginx-auth'] : []), ...(serverId.includes('db') ? ['postgres-auth'] : [])] : [],
    raw: active ? `Status\n|- Number of jail: ${serverId.includes('api') || serverId.includes('db') ? 2 : 1}\n\`- Jail list: sshd` : 'fail2ban service is not active on this host',
    configYaml: defaultYaml,
    config: { jails: [{ name: 'sshd', enabled: true, preset: 'ssh', filter: 'sshd', port: 'ssh', logpath: '/var/log/auth.log', backend: 'systemd', maxretry: 5, findtime: '10m', bantime: '1h', ignoreip: ['127.0.0.1/8'] }] },
    updatedAt: '2026-07-21T02:00:00.000Z',
  };
}
