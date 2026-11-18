import { afterEach, describe, expect, it, vi } from 'vitest';
import type { RuntimeSettings, RuntimeTailscaleContainerState } from '@/types/settings';
import { settingsApi, tailscaleUpdate } from './settings';

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
  containerReportIntervalSeconds: 30,
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

function envelope(data: unknown, status = 200): Response {
  return new Response(JSON.stringify({ data }), { status, headers: { 'Content-Type': 'application/json' } });
}

type FetchCall = [string, RequestInit];

function bodyOf(call: FetchCall): unknown {
  return JSON.parse(String(call[1].body));
}

describe('settings partition endpoints', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('submits each partition to its own sub-resource instead of the removed whole-runtime PUT', async () => {
    const fetchMock = vi.fn(async () => envelope(current));
    vi.stubGlobal('fetch', fetchMock);

    await settingsApi.updateGeneral({ containerReportIntervalSeconds: 45, language: 'zh-CN', logLevel: 'warn', reconcileTraceEnabled: true });
    await settingsApi.updateSecurity({ tokenExpiration: '30d', remoteCommandTimeoutSeconds: 90 });
    await settingsApi.updateCertificates({
      panel: { domain: 'panel.example.com', tlsCertificateId: '' },
      certificates: { email: '', dnsPropagationDelaySeconds: 10 },
    });
    await settingsApi.updateAgent({ downloadBaseUrl: '', downloadVerifyTls: false, transferTimeoutSeconds: 120 });
    await settingsApi.updateBranding({ loginTitle: 'Ops', loginSubtitle: 'Control plane' });
    await settingsApi.updateTailscale({ tags: ['tag:panel'], authKey: 'tskey-auth-new' });

    const calls = fetchMock.mock.calls as unknown as FetchCall[];
    expect(calls.map((call) => call[0])).toEqual([
      '/api/v1/settings/runtime/general',
      '/api/v1/settings/runtime/security',
      '/api/v1/settings/runtime/certificates',
      '/api/v1/settings/runtime/agent',
      '/api/v1/settings/runtime/branding',
      '/api/v1/settings/tailscale',
    ]);
    calls.forEach((call) => expect(call[1].method).toBe('PUT'));
    expect(bodyOf(calls[0]!)).toEqual({ containerReportIntervalSeconds: 45, language: 'zh-CN', logLevel: 'warn', reconcileTraceEnabled: true });
    expect(bodyOf(calls[1]!)).toEqual({ tokenExpiration: '30d', remoteCommandTimeoutSeconds: 90 });
    expect(bodyOf(calls[2]!)).toEqual({
      panel: { domain: 'panel.example.com', tlsCertificateId: '' },
      certificates: { email: '', dnsPropagationDelaySeconds: 10 },
    });
    expect(bodyOf(calls[3]!)).toEqual({ downloadBaseUrl: '', downloadVerifyTls: false, transferTimeoutSeconds: 120 });
    expect(bodyOf(calls[4]!)).toEqual({ loginTitle: 'Ops', loginSubtitle: 'Control plane' });
    expect(bodyOf(calls[5]!)).toEqual({ tags: ['tag:panel'], authKey: 'tskey-auth-new' });
    // 已删除的整体 runtime PUT 不再被调用。
    expect(calls.some((call) => call[0] === '/api/v1/settings/runtime' && call[1].method === 'PUT')).toBe(false);
  });

  it('keeps the whole-runtime GET for loading and for the top-bar language switch', async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo, init?: RequestInit) => (
      init?.method === 'PUT' ? envelope({ ...current, language: 'zh-CN' }) : envelope(current)
    ));
    vi.stubGlobal('fetch', fetchMock);

    await settingsApi.runtime();
    const updated = await settingsApi.updateLanguage('zh-CN');

    const calls = fetchMock.mock.calls as unknown as FetchCall[];
    expect(calls[0]![0]).toBe('/api/v1/settings/runtime');
    expect(calls[0]![1].method).toBe('GET');
    // updateLanguage 自己也会先 GET 一次当前 runtime，再提交 general 分区。
    expect(calls[1]![0]).toBe('/api/v1/settings/runtime');
    expect(calls[1]![1].method).toBe('GET');
    expect(calls[2]![0]).toBe('/api/v1/settings/runtime/general');
    expect(calls[2]![1].method).toBe('PUT');
    // updateLanguage = GET runtime + 只提交 general 分区负载。
    expect(bodyOf(calls[2]!)).toEqual({
      containerReportIntervalSeconds: 30,
      language: 'zh-CN',
      logLevel: 'info',
      reconcileTraceEnabled: true,
    });
    expect(updated.language).toBe('zh-CN');
  });

  it('posts the container tailscale apply endpoint and returns the reported state', async () => {
    const fetchMock = vi.fn(async () => envelope(container));
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
