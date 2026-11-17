import { afterEach, describe, expect, it, vi } from 'vitest';
import { metricsFacilityApi } from './metricsFacility';

function jsonResponse(data: unknown, status = 200) {
  return new Response(JSON.stringify({ data }), { status, headers: { 'Content-Type': 'application/json' } });
}

describe('metricsFacilityApi', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('reads the facility configuration', async () => {
    const config = {
      mode: 'selected',
      serverIds: ['srv-1'],
      retentionDays: 14,
      collectionIntervalSeconds: 120,
      cleanupSchedule: 'hourly',
      version: 2,
      updatedAt: '2026-11-17T00:00:00.000Z',
    };
    const fetchMock = vi.fn(async () => jsonResponse(config));
    vi.stubGlobal('fetch', fetchMock);

    await expect(metricsFacilityApi.config()).resolves.toEqual(config);
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/facility-apps/metrics', expect.objectContaining({ method: 'GET' }));
  });

  it('saves mode, serverIds, retention, interval and cleanup schedule', async () => {
    const input = {
      mode: 'selected' as const,
      serverIds: ['srv-1', 'srv-2'],
      retentionDays: 30,
      collectionIntervalSeconds: 30,
      cleanupSchedule: 'weekly' as const,
    };
    const saved = { ...input, version: 3, updatedAt: '2026-11-17T00:05:00.000Z' };
    const fetchMock = vi.fn(async () => jsonResponse(saved));
    vi.stubGlobal('fetch', fetchMock);

    await expect(metricsFacilityApi.saveConfig(input)).resolves.toEqual(saved);
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/facility-apps/metrics', expect.objectContaining({
      method: 'PUT',
      body: JSON.stringify(input),
    }));
  });

  it('surfaces stable contract error codes', async () => {
    const codes = [
      'metrics_facility_mode_invalid',
      'metrics_facility_servers_required',
      'metrics_facility_retention_invalid',
      'metrics_facility_interval_invalid',
      'metrics_facility_cleanup_schedule_invalid',
    ];
    for (const code of codes) {
      const fetchMock = vi.fn(async () => new Response(JSON.stringify({ error: { code, message: code } }), { status: 422, headers: { 'Content-Type': 'application/json' } }));
      vi.stubGlobal('fetch', fetchMock);
      await expect(metricsFacilityApi.saveConfig({
        mode: 'all',
        serverIds: [],
        retentionDays: 7,
        collectionIntervalSeconds: 60,
        cleanupSchedule: 'daily',
      })).rejects.toMatchObject({ code, status: 422 });
    }
  });
});
