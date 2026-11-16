import { afterEach, describe, expect, it, vi } from 'vitest';
import { networkProbeFacilityApi } from './networkProbe';

function jsonResponse(data: unknown, status = 200) {
  return new Response(JSON.stringify({ data }), { status, headers: { 'Content-Type': 'application/json' } });
}

describe('networkProbeFacilityApi', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('reads the facility configuration', async () => {
    const config = { mode: 'selected', serverIds: ['srv-1'], version: 3, updatedAt: '2026-11-16T05:00:00.000Z' };
    const fetchMock = vi.fn(async () => jsonResponse(config));
    vi.stubGlobal('fetch', fetchMock);

    await expect(networkProbeFacilityApi.config()).resolves.toEqual(config);
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/facility-apps/network-probe', expect.objectContaining({ method: 'GET' }));
  });

  it('saves the configuration with mode and serverIds', async () => {
    const saved = { mode: 'all', serverIds: [], version: 4, updatedAt: '2026-11-16T05:05:00.000Z' };
    const fetchMock = vi.fn(async () => jsonResponse(saved));
    vi.stubGlobal('fetch', fetchMock);

    await expect(networkProbeFacilityApi.saveConfig({ mode: 'all', serverIds: [] })).resolves.toEqual(saved);
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/facility-apps/network-probe', expect.objectContaining({
      method: 'PUT',
      body: JSON.stringify({ mode: 'all', serverIds: [] }),
    }));
  });

  it('reads the results matrix', async () => {
    const matrix = { mode: 'all', nodes: [], results: [], updatedAt: null };
    const fetchMock = vi.fn(async () => jsonResponse(matrix));
    vi.stubGlobal('fetch', fetchMock);

    await expect(networkProbeFacilityApi.results()).resolves.toEqual(matrix);
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/facility-apps/network-probe/results', expect.objectContaining({ method: 'GET' }));
  });

  it('surfaces stable contract error codes', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({
      error: { code: 'network_probe_servers_required', message: 'Select at least one probe node.' },
    }), { status: 422, headers: { 'Content-Type': 'application/json' } }));
    vi.stubGlobal('fetch', fetchMock);

    await expect(networkProbeFacilityApi.saveConfig({ mode: 'selected', serverIds: [] })).rejects.toMatchObject({
      code: 'network_probe_servers_required',
      status: 422,
    });
  });
});
