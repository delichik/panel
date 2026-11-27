import type { UfwAllowInput, UfwState } from '@/types/security';

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
