import { afterEach, describe, expect, it, vi } from 'vitest';
import { networkProbeFacilityApi } from './networkProbe';

function jsonResponse(data: unknown, status = 200) {
  return new Response(JSON.stringify({ data }), { status, headers: { 'Content-Type': 'application/json' } });
}

const settings = { intervalSeconds: 60, pingCount: 3, pingTimeoutSeconds: 2 };

describe('networkProbeFacilityApi', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('reads the facility configuration including probe parameters', async () => {
    const config = { mode: 'selected', serverIds: ['srv-1'], ...settings, version: 3, updatedAt: '2026-11-16T05:00:00.000Z' };
    const fetchMock = vi.fn(async () => jsonResponse(config));
    vi.stubGlobal('fetch', fetchMock);

    await expect(networkProbeFacilityApi.config()).resolves.toEqual(config);
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/facility-apps/network-probe', expect.objectContaining({ method: 'GET' }));
  });

  it('saves mode, serverIds and every probe parameter', async () => {
    const saved = { mode: 'all', serverIds: [], intervalSeconds: 120, pingCount: 5, pingTimeoutSeconds: 4, version: 4, updatedAt: '2026-11-16T05:05:00.000Z' };
    const fetchMock = vi.fn(async () => jsonResponse(saved));
    vi.stubGlobal('fetch', fetchMock);

    const input = { mode: 'all' as const, serverIds: [], intervalSeconds: 120, pingCount: 5, pingTimeoutSeconds: 4 };
    await expect(networkProbeFacilityApi.saveConfig(input)).resolves.toEqual(saved);
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/facility-apps/network-probe', expect.objectContaining({
      method: 'PUT',
      body: JSON.stringify(input),
    }));
  });

  it('reads the results matrix', async () => {
    const matrix = { mode: 'all', nodes: [], results: [], updatedAt: null };
    const fetchMock = vi.fn(async () => jsonResponse(matrix));
    vi.stubGlobal('fetch', fetchMock);

    await expect(networkProbeFacilityApi.results()).resolves.toEqual(matrix);
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/facility-apps/network-probe/results', expect.objectContaining({ method: 'GET' }));
  });

  it('normalizes null matrix arrays', async () => {
    const fetchMock = vi.fn(async () => jsonResponse({ mode: 'all', nodes: null, results: null, updatedAt: null }));
    vi.stubGlobal('fetch', fetchMock);

    await expect(networkProbeFacilityApi.results()).resolves.toEqual({ mode: 'all', nodes: [], results: [], updatedAt: null });
  });

  it('reads the aggregated all-servers target view with 24h history', async () => {
    const node = {
      serverId: 'srv-1',
      name: 'edge',
      address: '10.0.0.1',
      sourceCount: 2,
      latencyMs: 42,
      packetLossPercent: 1.5,
      worstPacketLossPercent: 3,
      sampledAt: '2026-11-17T05:00:00.000Z',
      stale: false,
      history: [{ time: '2026-11-17T05:00:00.000Z', latencyMs: 42, packetLossPercent: 1.5 }],
    };
    const fetchMock = vi.fn(async () => jsonResponse({ nodes: [node], updatedAt: '2026-11-17T05:00:00.000Z' }));
    vi.stubGlobal('fetch', fetchMock);

    await expect(networkProbeFacilityApi.targets()).resolves.toEqual({ nodes: [node], updatedAt: '2026-11-17T05:00:00.000Z' });
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/facility-apps/network-probe/results?view=targets&history=1', expect.objectContaining({ method: 'GET' }));
  });

  it('can request the target view without history and normalizes null nodes', async () => {
    const fetchMock = vi.fn(async () => jsonResponse({ nodes: null, updatedAt: null }));
    vi.stubGlobal('fetch', fetchMock);

    await expect(networkProbeFacilityApi.targets(undefined, false)).resolves.toEqual({ nodes: [], updatedAt: null });
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/facility-apps/network-probe/results?view=targets', expect.objectContaining({ method: 'GET' }));
  });

  it('surfaces stable contract error codes', async () => {
    const codes = ['network_probe_servers_required', 'network_probe_interval_invalid', 'network_probe_ping_count_invalid', 'network_probe_ping_timeout_invalid'];
    for (const code of codes) {
      const fetchMock = vi.fn(async () => new Response(JSON.stringify({ error: { code, message: code } }), { status: 422, headers: { 'Content-Type': 'application/json' } }));
      vi.stubGlobal('fetch', fetchMock);
      await expect(networkProbeFacilityApi.saveConfig({ mode: 'selected', serverIds: [] })).rejects.toMatchObject({ code, status: 422 });
    }
  });
});
