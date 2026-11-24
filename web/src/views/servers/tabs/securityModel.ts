import type { UfwAllowInput, UfwState } from '@/types/security';

export type Tone = 'neutral' | 'success' | 'warning' | 'danger' | 'info';

export function ufwTone(state: UfwState | null): Tone {
  if (!state) return 'neutral';
  if (!state.supported) return 'danger';
  if (!state.installed) return 'warning';
  if (!state.active) return 'warning';
  return 'success';
}

export function validateUfwRule(input: UfwAllowInput) {
  const errors: Partial<Record<keyof UfwAllowInput, string>> = {};
  if (!Number.isInteger(input.port) || input.port < 1 || input.port > 65535) errors.port = 'securityPage.validationPort';
  if (!['tcp', 'udp'].includes(input.protocol)) errors.protocol = 'securityPage.validationProtocol';
  if (!input.from.trim()) errors.from = 'securityPage.validationFrom';
  return errors;
}
