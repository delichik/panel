import { afterEach, describe, expect, it, vi } from 'vitest';
import type { RuntimeSettings } from '@/types/settings';
import { settingsApi } from './settings';

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
  imageRegistry: { endpoint: '', scheme: 'https' },
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
    await settingsApi.updateAutoUpdate({ appsEnabled: true, packagesEnabled: true, packagesMode: 'security', scheduleTime: '04:30' });
    await settingsApi.updateImageRegistry({ endpoint: 'registry.example.com:5000', scheme: 'http' });
    await settingsApi.updateImageRegistry({ endpoint: '' });

    const calls = fetchMock.mock.calls as unknown as FetchCall[];
    expect(calls.map((call) => call[0])).toEqual([
      '/api/v1/settings/runtime/general',
      '/api/v1/settings/runtime/security',
      '/api/v1/settings/runtime/certificates',
      '/api/v1/settings/runtime/agent',
      '/api/v1/settings/runtime/branding',
      '/api/v1/settings/runtime/auto-update',
      '/api/v1/settings/runtime/image-registry',
      '/api/v1/settings/runtime/image-registry',
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
    expect(bodyOf(calls[5]!)).toEqual({ appsEnabled: true, packagesEnabled: true, packagesMode: 'security', scheduleTime: '04:30' });
    expect(bodyOf(calls[6]!)).toEqual({ endpoint: 'registry.example.com:5000', scheme: 'http' });
    // 指针语义：显式空串表示官方仓库，省略字段表示保留；两者都必须原样送达。
    expect(bodyOf(calls[7]!)).toEqual({ endpoint: '' });
    // 已删除的整体 runtime PUT 不再被调用。
    expect(calls.some((call) => call[0] === '/api/v1/settings/runtime' && call[1].method === 'PUT')).toBe(false);
  });

  it('starts a manual automatic-update run without touching a settings partition', async () => {
    const fetchMock = vi.fn(async () => envelope({ taskId: 'task-auto-update-run-1' }, 202));
    vi.stubGlobal('fetch', fetchMock);

    const result = await settingsApi.runAutoUpdate();

    const calls = fetchMock.mock.calls as unknown as FetchCall[];
    expect(calls).toHaveLength(1);
    expect(calls[0]![0]).toBe('/api/v1/settings/runtime/auto-update/run');
    expect(calls[0]![1].method).toBe('POST');
    expect(bodyOf(calls[0]!)).toEqual({});
    expect(result.taskId).toBe('task-auto-update-run-1');
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
});
