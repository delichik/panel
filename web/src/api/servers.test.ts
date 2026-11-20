import { afterEach, describe, expect, it, vi } from 'vitest';
import { serversApi } from './servers';

describe('serversApi', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('requests server metrics with a backend-supported default range', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({
      data: {
        range: '1h',
        cpu: [],
        memory: [],
        diskIo: [],
        diskCurrent: null,
        network: [],
        load: [],
      },
    }), { status: 200, headers: { 'Content-Type': 'application/json' } }));
    vi.stubGlobal('fetch', fetchMock);

    await serversApi.metrics('srv-main');

    expect(fetchMock).toHaveBeenCalledWith('/api/v1/servers/srv-main/metrics?range=1h', expect.any(Object));
  });

  it('requests bucketed metrics with the range bucket used by the overview charts', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({
      data: { range: '1d', cpu: [], memory: [], diskIo: [], diskCurrent: null, network: [], load: [] },
    }), { status: 200, headers: { 'Content-Type': 'application/json' } }));
    vi.stubGlobal('fetch', fetchMock);

    await serversApi.metricsBucketed('srv-main', '1d', 300);

    expect(fetchMock).toHaveBeenCalledWith('/api/v1/servers/srv-main/metrics?range=1d&bucket=300', expect.any(Object));
  });

  it('requests the per-node network probe results', async () => {
    const payload = {
      serverId: 'srv-main',
      enabled: true,
      nodes: [{ serverId: 'srv-peer', name: 'peer', address: '10.0.0.2' }],
      results: [],
      updatedAt: null,
    };
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ data: payload }), { status: 200, headers: { 'Content-Type': 'application/json' } }));
    vi.stubGlobal('fetch', fetchMock);

    await expect(serversApi.networkProbes('srv-main')).resolves.toEqual(payload);
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/servers/srv-main/network-probes', expect.any(Object));
  });

  it('normalizes null probe arrays from older servers', async () => {
    const payload = { serverId: 'srv-main', enabled: true, nodes: null, results: null, updatedAt: null };
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ data: payload }), { status: 200, headers: { 'Content-Type': 'application/json' } }));
    vi.stubGlobal('fetch', fetchMock);

    await expect(serversApi.networkProbes('srv-main')).resolves.toEqual({
      serverId: 'srv-main',
      enabled: true,
      nodes: [],
      results: [],
      updatedAt: null,
    });
  });
});
