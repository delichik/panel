import { afterEach, describe, expect, it, vi } from 'vitest';
import type { RuntimeSettings, RuntimeTailscaleContainerState } from '@/types/settings';
import { runtimeLanguageUpdate, runtimeTailscaleUpdate, settingsApi, tailscaleUpdate } from './settings';

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

const current: RuntimeSettings = {
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
  reconcileTraceEnabled: true,
  branding: { loginTitle: 'Seamark', loginSubtitle: 'Panel' },
  certificates: { email: 'admin@example.com', dnsPropagationDelaySeconds: 30 },
  panel: { domain: 'panel.example.com', tlsCertificateId: 'tls-1' },
  agent: { downloadBaseUrl: 'https://panel.example.com', downloadVerifyTls: true, transferTimeoutSeconds: 180 },
  tailscale: { authKeyConfigured: true, tags: ['tag:server'], container },
  jwtSecretConfigured: true,
};

const scalars = {
  metricsRetentionDays: 14,
  metricsCollectionIntervalSeconds: 60,
  containerReportIntervalSeconds: 30,
  cleanupSchedule: 'daily',
  tokenExpiration: '1d',
  logLevel: 'info',
  remoteCommandTimeoutSeconds: 45,
};

describe('runtimeLanguageUpdate', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('preserves required scalar settings and changes only the language', () => {
    expect(runtimeLanguageUpdate(current, 'zh-CN')).toEqual({ ...scalars, language: 'zh-CN' });
  });

  it('omits the tailscale group so a language switch cannot drop the stored key or tags', () => {
    expect(runtimeLanguageUpdate(current, 'zh-CN')).not.toHaveProperty('tailscale');
  });

  it('posts the container tailscale apply endpoint and returns the reported state', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ data: container }), { status: 202, headers: { 'Content-Type': 'application/json' } }));
    vi.stubGlobal('fetch', fetchMock);

    await expect(settingsApi.applyTailscale()).resolves.toEqual(container);
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/settings/tailscale/apply', expect.objectContaining({ method: 'POST' }));
  });
});

describe('tailscaleUpdate', () => {
  it('sends a trimmed auth key and keeps the stored tags untouched when none are given', () => {
    expect(tailscaleUpdate({ authKey: '  tskey-auth-abc  ' })).toEqual({ authKey: 'tskey-auth-abc' });
  });

  it('keeps the stored key when the field is blank and the user did not confirm a clear', () => {
    expect(tailscaleUpdate({ authKey: '', clearAuthKey: false })).toEqual({});
    expect(tailscaleUpdate({ authKey: '   ', tags: [] })).toEqual({ tags: [] });
  });

  it('clears the key only when the caller confirms it', () => {
    expect(tailscaleUpdate({ authKey: '', clearAuthKey: true })).toEqual({ clearAuthKey: true });
    // authKey 优先：确认标记不会覆盖本次提交的新密钥。
    expect(tailscaleUpdate({ authKey: 'tskey-auth-new', clearAuthKey: true })).toEqual({ authKey: 'tskey-auth-new' });
  });

  it('replaces the tag list as a copy so callers cannot mutate the request body afterwards', () => {
    const tags = ['tag:server'];
    const update = tailscaleUpdate({ tags });
    tags.push('tag:panel');
    expect(update.tags).toEqual(['tag:server']);
  });
});

describe('runtimeTailscaleUpdate', () => {
  it('keeps the required scalars and changes only the tailscale group', () => {
    expect(runtimeTailscaleUpdate(current, { authKey: 'tskey-auth-new', tags: ['tag:server', 'tag:panel'] })).toEqual({
      ...scalars,
      language: 'en',
      tailscale: { authKey: 'tskey-auth-new', tags: ['tag:server', 'tag:panel'] },
    });
  });

  it('does not send the response-only container state back to the server', () => {
    const update = runtimeTailscaleUpdate(current, { clearAuthKey: true });
    expect(update.tailscale).toEqual({ clearAuthKey: true });
    expect(update.tailscale).not.toHaveProperty('container');
    expect(update.tailscale).not.toHaveProperty('authKeyConfigured');
  });
});
