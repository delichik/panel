import { ApiClient } from './client';
import { createFacilityAppsApi } from './facilityApps';

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('facilityAppsApi', () => {
  it('uses the reverse proxy save-session contract', async () => {
    const fetcher = vi.fn().mockImplementation(() => jsonResponse({ data: {}, error: null }));
    const api = createFacilityAppsApi(new ApiClient({ baseUrl: '/api/v1', fetcher }));
    const save = {
      deploymentServers: ['srv-edge'],
      image: 'nginx:1.27-alpine',
      panelEntry: { enabled: false, serverId: '', domain: '' },
      staticSites: [],
      domainPolicies: [],
    };

    await api.beginSaveSession('2026-07-14T10:00:00Z');
    await api.uploadSaveSessionAsset('session/1', { assetId: 'asset/1', name: 'site', kind: 'uploaded_file', file: new File(['hello'], 'index.html') });
    await api.deleteSaveSessionAsset('session/1', 'asset/old');
    await api.commitSaveSession('session/1', save);
    await api.discardSaveSession('session/1');

    expect(fetcher).toHaveBeenNthCalledWith(1, '/api/v1/facility-apps/reverse-proxy/save-sessions', expect.objectContaining({ method: 'POST', body: JSON.stringify({ baseUpdatedAt: '2026-07-14T10:00:00Z' }) }));
    expect(fetcher).toHaveBeenNthCalledWith(2, '/api/v1/facility-apps/reverse-proxy/save-sessions/session%2F1/assets', expect.objectContaining({ method: 'POST', body: expect.any(FormData) }));
    expect(fetcher).toHaveBeenNthCalledWith(3, '/api/v1/facility-apps/reverse-proxy/save-sessions/session%2F1/assets/delete', expect.objectContaining({ method: 'POST', body: JSON.stringify({ assetId: 'asset/old' }) }));
    expect(fetcher).toHaveBeenNthCalledWith(4, '/api/v1/facility-apps/reverse-proxy/save-sessions/session%2F1/commit', expect.objectContaining({ method: 'POST', body: JSON.stringify({ save }) }));
    expect(fetcher).toHaveBeenNthCalledWith(5, '/api/v1/facility-apps/reverse-proxy/save-sessions/session%2F1', expect.objectContaining({ method: 'DELETE' }));
  });
});
