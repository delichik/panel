import { describe, expect, it } from 'vitest';
import type { RuntimeSettings, RuntimeTailscaleContainerState } from '@/types/settings';
import { formatTailscaleTags, parseTailscaleTags, tailscaleContainerTone, tailscaleFormState } from './tailscale';

const container: RuntimeTailscaleContainerState = {
  available: true,
  running: true,
  loggedIn: true,
  hostname: 'seamark-panel',
  ipv4: '100.101.102.103',
  ipv6: 'fd7a:115c:a1e0::1',
  version: '1.78.1',
  backendState: 'Running',
  lastError: '',
  updatedAt: '2026-08-01T07:55:00.000Z',
};

function settings(tailscale: Partial<RuntimeSettings['tailscale']> = {}): RuntimeSettings {
  return {
    listenAddress: '127.0.0.1:8080',
    appDatabase: 'app.db',
    metricsDatabase: 'metrics.db',
    dataRoot: 'data',
    metricsRetentionDays: 14,
    metricsCollectionIntervalSeconds: 60,
    containerReportIntervalSeconds: 30,
    cleanupSchedule: 'daily',
    tokenExpiration: '1d',
    language: 'en',
    logLevel: 'info',
    remoteCommandTimeoutSeconds: 45,
    reconcileTraceEnabled: false,
    branding: { loginTitle: 'Seamark', loginSubtitle: '' },
    certificates: { email: '', dnsPropagationDelaySeconds: 30 },
    panel: { domain: 'localhost', tlsCertificateId: '' },
    agent: { downloadBaseUrl: '', downloadVerifyTls: false, transferTimeoutSeconds: 120 },
    tailscale: { authKeyConfigured: true, tags: ['tag:server'], container, ...tailscale },
    jwtSecretConfigured: true,
  };
}

describe('tailscale tag parsing', () => {
  it('accepts comma and whitespace separated tags and removes duplicates', () => {
    expect(parseTailscaleTags('tag:server, tag:panel  tag:server\ntag:edge-1')).toEqual({
      tags: ['tag:server', 'tag:panel', 'tag:edge-1'],
      invalid: [],
    });
    expect(parseTailscaleTags('')).toEqual({ tags: [], invalid: [] });
  });

  it('reports every entry that does not match the tag pattern instead of silently dropping it', () => {
    const result = parseTailscaleTags('tag:server, Tag:Panel, server, tag:-edge, tag:edge-, tag:a-');
    expect(result.tags).toEqual(['tag:server']);
    expect(result.invalid).toEqual(['Tag:Panel', 'server', 'tag:-edge', 'tag:edge-', 'tag:a-']);
  });

  it('round-trips the tag list through the form representation', () => {
    expect(formatTailscaleTags(['tag:server', 'tag:panel'])).toBe('tag:server, tag:panel');
    expect(parseTailscaleTags(formatTailscaleTags(['tag:server', 'tag:panel'])).tags).toEqual(['tag:server', 'tag:panel']);
  });
});

describe('tailscale form state', () => {
  it('never hydrates the auth key from the server response', () => {
    // 响应里只有 authKeyConfigured；即使中间层意外带回密钥字段，也不进入表单。
    const withLeakedKey = settings({ authKey: 'tskey-auth-leaked' } as unknown as Partial<RuntimeSettings['tailscale']>);
    const state = tailscaleFormState(withLeakedKey);
    expect(state.tailscaleAuthKey).toBe('');
    expect(JSON.stringify(state)).not.toContain('tskey-auth-leaked');
    expect(state.tailscaleTags).toBe('tag:server');
  });

  it('hydrates the configured flag and tags from the response', () => {
    expect(settings({ authKeyConfigured: false, tags: [] }).tailscale.authKeyConfigured).toBe(false);
    expect(tailscaleFormState(settings({ tags: ['tag:panel'] })).tailscaleTags).toBe('tag:panel');
  });
});

describe('container tailscale tone', () => {
  it('maps availability, errors and login state to semantic tones', () => {
    expect(tailscaleContainerTone(container)).toBe('success');
    expect(tailscaleContainerTone({ ...container, loggedIn: false })).toBe('warning');
    expect(tailscaleContainerTone({ ...container, lastError: 'backend unreachable' })).toBe('danger');
    expect(tailscaleContainerTone({ ...container, running: false, loggedIn: false })).toBe('neutral');
    expect(tailscaleContainerTone({ ...container, available: false })).toBe('neutral');
    expect(tailscaleContainerTone(null)).toBe('neutral');
  });
});
