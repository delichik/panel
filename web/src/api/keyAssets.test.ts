import { ApiClient } from './client';
import { createKeyAssetsApi, keyAssetsApi } from './keyAssets';

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('keyAssetsApi', () => {
  it('targets the key asset CRUD and action endpoints', async () => {
    const fetcher = vi.fn().mockImplementation(() => jsonResponse({ data: { taskId: 'task_1' }, error: null }));
    const api = createKeyAssetsApi(new ApiClient({ baseUrl: '/api/v1', fetcher }));

    await api.createCa({ name: 'Root CA', commonName: 'Root CA', validityDays: 3650 });
    await api.createTls({
      name: 'example.com',
      caId: 'ca_1',
      commonName: 'example.com',
      dnsNames: ['example.com'],
      ipAddresses: [],
      validityDays: 365,
    });
    await api.generateSsh({ name: 'ops', algorithm: 'rsa', keySize: 3072 });
    await api.importAsset({ type: 'ssh_key_pair', name: 'ops-import', privateKeyPem: 'PRIVATE KEY' });
    await api.reissue('tls_1');
    await api.regenerate('ssh_1');
    await api.delete('asset_1');

    expect(fetcher).toHaveBeenNthCalledWith(1, '/api/v1/key-assets/ca', expect.objectContaining({ method: 'POST' }));
    expect(fetcher).toHaveBeenNthCalledWith(2, '/api/v1/key-assets/tls', expect.objectContaining({ method: 'POST' }));
    expect(fetcher).toHaveBeenNthCalledWith(3, '/api/v1/key-assets/ssh/generate', expect.objectContaining({ method: 'POST' }));
    expect(fetcher).toHaveBeenNthCalledWith(4, '/api/v1/key-assets/import', expect.objectContaining({ method: 'POST' }));
    expect(fetcher).toHaveBeenNthCalledWith(5, '/api/v1/key-assets/tls_1/reissue', expect.objectContaining({ method: 'POST' }));
    expect(fetcher).toHaveBeenNthCalledWith(6, '/api/v1/key-assets/ssh_1/regenerate', expect.objectContaining({ method: 'POST' }));
    expect(fetcher).toHaveBeenNthCalledWith(7, '/api/v1/key-assets/asset_1', expect.objectContaining({ method: 'DELETE' }));
  });

  it('lists and resets system-built-in certificates', async () => {
    const fetcher = vi.fn().mockImplementation(() => jsonResponse({ data: [], error: null }));
    const api = createKeyAssetsApi(new ApiClient({ baseUrl: '/api/v1', fetcher }));

    await api.listSystemCertificates();
    await api.resetSystemCertificate('agent-server:srv_1');

    expect(fetcher).toHaveBeenNthCalledWith(1, '/api/v1/key-assets/system', expect.objectContaining({ method: 'GET' }));
    expect(fetcher).toHaveBeenNthCalledWith(2, '/api/v1/key-assets/system/agent-server%3Asrv_1/reset', expect.objectContaining({ method: 'POST' }));
  });

  it('uploads archive preflight as multipart form data and executes import plans', async () => {
    const fetcher = vi.fn()
      .mockResolvedValueOnce(jsonResponse({ data: { planId: 'plan_1', expiresAt: '2026-06-12T00:00:00Z', summary: {}, assets: [], conflicts: [], requiresDangerConfirm: false }, error: null }))
      .mockResolvedValueOnce(jsonResponse({ data: { taskId: 'task_2' }, error: null }));
    const api = createKeyAssetsApi(new ApiClient({ baseUrl: '/api/v1', fetcher }));
    const file = new File(['archive'], 'bundle.panel-key-assets');

    await api.preflightImportArchive(file, 'secret-123456');
    await api.executeImport('plan_1', {
      strategy: 'overwrite_existing',
      confirmDangerousOverwrite: true,
      resolutions: [{ assetId: 'asset_1', action: 'overwrite_existing', targetAssetId: 'existing_1' }],
    });

    const preflightInit = fetcher.mock.calls[0][1] as RequestInit;
    expect(fetcher).toHaveBeenNthCalledWith(
      1,
      '/api/v1/key-assets/imports/preflight',
      expect.objectContaining({ method: 'POST' }),
    );
    expect(preflightInit.body).toBeInstanceOf(FormData);
    expect(fetcher).toHaveBeenNthCalledWith(
      2,
      '/api/v1/key-assets/imports/plan_1/execute',
      expect.objectContaining({ method: 'POST' }),
    );
  });

  it('keeps the default exported client available', () => {
    expect(keyAssetsApi.list).toBeDefined();
  });
});
