import type { ApiEnvelope } from '@/types/api';
import {
  createAsset,
  createDomain,
  createSelfCa,
  createSelfLeaf,
  deleteAsset,
  deleteCertificate,
  deleteDomain,
  deleteRecord,
  deleteSelfSigned,
  issueCertificate,
  listRecords,
  mockDnsDomains,
  mockDomainCertificates,
  mockKeyAssets,
  mockSelfSigned,
  mutateAsset,
  reissueCertificate,
  renewCertificate,
  renewSelfSigned,
  saveRecord,
  updateDomain,
} from './certificates';
import { getOverviewCardData, getOverviewCards, overviewFromServers, setOverviewCards } from './overview';
import {
  accepted,
  createCredential,
  createServer,
  deleteCredential,
  deleteServer,
  mockAgentCertificate,
  mockCredentials,
  mockServerMetrics,
  mockServers,
  probeServer,
  testServer,
  updateCredential,
  updateServer,
} from './servers';
import type { CredentialInput } from '@/types/credentials';
import type { OverviewCardConfigurationSet } from '@/types/overview';
import type { ServerSaveInput } from '@/types/servers';
import {
  appDiagnostics,
  appLogs,
  appOperation,
  appRuntime,
  appSession,
  beginAppSession,
  beginFacilitySession,
  commitAppSession,
  commitFacilitySession,
  deleteApp,
  deleteAppFile,
  deleteFacilityAsset,
  facilityDiagnostics,
  facilitySession,
  getAppFile,
  mockApplicationSummaries,
  mockApplications,
  mockFacility,
  mockFacilitySummaries,
  patchAppSession,
  patchFacilitySession,
  putAppFile,
  putFacilityAsset,
  restorePersistentData,
  uploadAppArchive,
} from './applications';
import type { ApplicationEditSession } from '@/types/applications';
import type { FacilityEditSession } from '@/types/facilityApps';
import type { SystemCertificateDto } from '@/types/keyAssets';
import type { UfwAllowInput } from '@/types/security';
import {
  mockAddUfwRule,
  mockDeleteUfwRule,
  mockEnableFail2Ban,
  mockEnableUfw,
  mockFail2BanState,
  mockReleaseFail2Ban,
  mockSaveFail2Ban,
  mockUfwState,
} from './security';
import {
  mockContainerAction,
  mockContainerLogs,
  mockContainers,
  mockDeleteContainer,
  mockDeleteImage,
  mockDeleteVolume,
  mockImages,
  mockNetworks,
  mockPackages,
  mockPruneImages,
  mockPruneVolumes,
  mockPullImage,
  mockRefreshPackages,
  mockUpgradePackages,
  mockVolumes,
} from './resources';
import { mockTasks, mockTaskLogs, mockTaskSteps, retryTask, runTaskNow } from './tasks';
import { applicationOperationDetail, mockApplicationOperations, mockSystemEvents, systemEventDetail } from './runtimeEvents';
import { confirmRestore, mockRuntimeSettings, mockServerVariables, restorePreflight, saveRuntime, saveServerVariables, startExport } from './settings';
import { advanceExport, exportStatus, resetExport, restoreStatus } from './maintenance';
import { debugSnapshot } from './debug';

const nativeFetch = window.fetch.bind(window);
const mockAuthToken = 'panel_mock_admin_token';
const mockAuthSession = {
  authenticated: true,
  token: mockAuthToken,
  username: 'admin',
  passwordChangeRequired: false,
};
let mockPassword = 'admin';
let mockUsername = 'admin';
let mockPasswordChangeRequired = true;

function authValidationEnabled() {
  return import.meta.env.VITE_PANEL_TEST_AUTH === 'true';
}

function authSession() {
  return { ...mockAuthSession, username: mockUsername, passwordChangeRequired: authValidationEnabled() ? mockPasswordChangeRequired : false };
}

function systemCertificates(): SystemCertificateDto[] {
  const base: SystemCertificateDto[] = [
    {
      id: 'agent-ca',
      type: 'ca_certificate',
      name: 'Panel Agent CA',
      commonName: 'Panel Agent Root CA',
      fingerprint: 'SHA256:3A:8F:42:19:CA:77:ED:0B:20:66:92:31:91:8B:AC',
      notBefore: '2026-01-01T00:00:00.000Z',
      notAfter: '2031-01-01T00:00:00.000Z',
      status: 'valid',
      builtIn: true,
      canReset: true,
    },
    {
      id: 'agent-panel-client',
      type: 'tls_certificate',
      name: 'Panel Agent client',
      commonName: 'panel-agent-client',
      fingerprint: 'SHA256:9C:12:DE:88:4A:11:07:C4:E2:58:D0:34:91:AB:71',
      notBefore: '2026-01-01T00:00:00.000Z',
      notAfter: '2027-01-01T00:00:00.000Z',
      status: 'valid',
      builtIn: true,
      canReset: true,
    },
  ];
  return base.concat(mockServers.slice(0, 10).map((server, index) => ({
    id: `agent-server:${server.id}`,
    type: 'tls_certificate',
    name: `Agent server certificate - ${server.name}`,
    commonName: `panel-agent-${server.id}`,
    fingerprint: `SHA256:${String(index + 11).padStart(2, '0')}:AF:33:90:51:64:2D:E8:77:${String(index + 31).padStart(2, '0')}:AA:19:42:70:CC`,
    notBefore: '2026-03-01T00:00:00.000Z',
    notAfter: index === 1 ? '2026-08-01T00:00:00.000Z' : '2027-03-01T00:00:00.000Z',
    serverId: server.id,
    serverName: server.name,
    status: index === 1 ? 'expiring' : 'valid',
    builtIn: true,
    canReset: true,
  })));
}

function json<T>(data: T, status = 200) {
  return new Response(JSON.stringify({ data } satisfies ApiEnvelope<T>), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function error(code: string, message: string, status = 400, details?: unknown) {
  return new Response(JSON.stringify({ error: { code, message, details } }), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function blobResponse(content: BlobPart, filename: string, contentType = 'application/octet-stream') {
  return new Response(content, {
    status: 200,
    headers: {
      'Content-Type': contentType,
      'Content-Disposition': `attachment; filename="${filename.replace(/["\\\r\n]/g, '_')}"`,
    },
  });
}

export function installMockApi() {
  mockPassword = 'admin';
  mockUsername = 'admin';
  mockPasswordChangeRequired = authValidationEnabled();
  window.fetch = async (input, init) => {
    const target = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
    const url = new URL(target, window.location.origin);
    if (!url.pathname.startsWith('/api/v1/')) return nativeFetch(input, init);

    if (url.pathname === '/api/v1/overview') return json(overviewFromServers(mockServers));
    if (url.pathname === '/api/v1/overview/cards' && method(init) === 'GET') return json(getOverviewCards());
    if (url.pathname === '/api/v1/overview/cards' && method(init) === 'PUT') return json(setOverviewCards(await body<OverviewCardConfigurationSet>(init)));
    const cardMatch = url.pathname.match(/^\/api\/v1\/overview\/cards\/([^/]+)\/data$/);
    if (cardMatch) {
      try {
        const data = getOverviewCardData(decodeURIComponent(cardMatch[1]), mockServers);
        return data ? json(data) : error('overview_card_not_found', 'Overview card was not found.', 404);
      } catch (err) {
        return error('overview_card_data_failed', err instanceof Error ? err.message : 'Unable to load card data.', 503);
      }
    }

    if (url.pathname === '/api/v1/servers' && method(init) === 'GET') return json(mockServers);
    if (url.pathname === '/api/v1/servers' && method(init) === 'POST') return json(createServer(await body<ServerSaveInput>(init)), 201);
    if (url.pathname === '/api/v1/servers/probe' && method(init) === 'POST') return json(probeServer(await body<ServerSaveInput>(init)));
    const metricsMatch = url.pathname.match(/^\/api\/v1\/servers\/([^/]+)\/metrics$/);
    if (metricsMatch && method(init) === 'GET') {
      const series = mockServerMetrics(decodeURIComponent(metricsMatch[1]));
      return series ? json(series) : error('server_not_found', 'Server was not found.', 404);
    }
    const agentCertMatch = url.pathname.match(/^\/api\/v1\/servers\/([^/]+)\/agent\/certificate$/);
    if (agentCertMatch && method(init) === 'POST') {
      const bundle = mockAgentCertificate(decodeURIComponent(agentCertMatch[1]));
      return bundle ? json(bundle) : error('server_not_found', 'Server was not found.', 404);
    }
    const serverMatch = url.pathname.match(/^\/api\/v1\/servers\/([^/]+)$/);
    if (serverMatch && method(init) === 'PUT') {
      const saved = updateServer(decodeURIComponent(serverMatch[1]), await body<ServerSaveInput>(init));
      return saved ? json(saved) : error('server_not_found', 'Server was not found.', 404);
    }
    if (serverMatch && method(init) === 'DELETE') {
      return deleteServer(decodeURIComponent(serverMatch[1])) ? json(null, 200) : error('server_not_found', 'Server was not found.', 404);
    }
    const serverOperationMatch = url.pathname.match(/^\/api\/v1\/servers\/([^/]+)\/(test|restart|agent\/deploy|ufw\/install)$/);
    if (serverOperationMatch && method(init) === 'POST') {
      const id = decodeURIComponent(serverOperationMatch[1]);
      const op = serverOperationMatch[2];
      if (op === 'test') {
        try {
          const tested = testServer(id);
          return tested ? json(tested) : error('server_not_found', 'Server was not found.', 404);
        } catch (err) {
          return error('server_unreachable', err instanceof Error ? err.message : 'Server is unreachable.', 502);
        }
      }
      return mockServers.some((item) => item.id === id) ? json(accepted(op.replace('/', '-')), 202) : error('server_not_found', 'Server was not found.', 404);
    }

    const ufwMatch = url.pathname.match(/^\/api\/v1\/servers\/([^/]+)\/ufw$/);
    if (ufwMatch && method(init) === 'GET') {
      try {
        const state = mockUfwState(decodeURIComponent(ufwMatch[1]));
        return state ? json(state) : error('server_not_found', 'Server was not found.', 404);
      } catch (err) {
        return error('server_unreachable', err instanceof Error ? err.message : 'Server is unreachable.', 502);
      }
    }
    const ufwEnableMatch = url.pathname.match(/^\/api\/v1\/servers\/([^/]+)\/ufw\/enable$/);
    if (ufwEnableMatch && method(init) === 'POST') {
      const ok = mockEnableUfw(decodeURIComponent(ufwEnableMatch[1]));
      return ok ? json(accepted('server-ufw-enable'), 202) : error('server_not_found', 'Server was not found.', 404);
    }
    const ufwRulesMatch = url.pathname.match(/^\/api\/v1\/servers\/([^/]+)\/ufw\/rules$/);
    if (ufwRulesMatch && method(init) === 'POST') {
      try {
        const state = mockAddUfwRule(decodeURIComponent(ufwRulesMatch[1]), await body<UfwAllowInput>(init));
        return state ? json(state) : error('server_not_found', 'Server was not found.', 404);
      } catch (err) {
        return error('ufw_not_installed', err instanceof Error ? err.message : 'UFW is not installed.', 409);
      }
    }
    const ufwDeleteMatch = url.pathname.match(/^\/api\/v1\/servers\/([^/]+)\/ufw\/rules\/(\d+)$/);
    if (ufwDeleteMatch && method(init) === 'DELETE') {
      const state = mockDeleteUfwRule(decodeURIComponent(ufwDeleteMatch[1]), Number(ufwDeleteMatch[2]));
      return state ? json(state) : error('server_not_found', 'Server was not found.', 404);
    }

    const fail2banMatch = url.pathname.match(/^\/api\/v1\/servers\/([^/]+)\/fail2ban$/);
    if (fail2banMatch && method(init) === 'GET') {
      try {
        const state = mockFail2BanState(decodeURIComponent(fail2banMatch[1]));
        return state ? json(state) : error('server_not_found', 'Server was not found.', 404);
      } catch (err) {
        return error('server_not_reachable', err instanceof Error ? err.message : 'Server is unreachable.', 422);
      }
    }
    if (fail2banMatch && method(init) === 'PUT') {
      try {
        const input = await body<{ configYaml: string }>(init);
        const state = mockSaveFail2Ban(decodeURIComponent(fail2banMatch[1]), input.configYaml);
        return state ? json(state) : error('server_not_found', 'Server was not found.', 404);
      } catch (err) {
        return error('fail2ban_config_invalid', err instanceof Error ? err.message : 'Fail2Ban YAML is invalid.', 422);
      }
    }
    const fail2banOperationMatch = url.pathname.match(/^\/api\/v1\/servers\/([^/]+)\/fail2ban\/(enable|release|install)$/);
    if (fail2banOperationMatch && method(init) === 'POST') {
      const serverId = decodeURIComponent(fail2banOperationMatch[1]);
      const op = fail2banOperationMatch[2];
      if (op === 'release') return mockReleaseFail2Ban(serverId) ? json(accepted('server-fail2ban-release'), 202) : error('server_not_found', 'Server was not found.', 404);
      if (op === 'install') return mockEnableFail2Ban(serverId, true) === 'accepted' ? json(accepted('server-fail2ban-install'), 202) : error('server_not_found', 'Server was not found.', 404);
      const input = await body<{ confirmTakeover?: boolean }>(init);
      const result = mockEnableFail2Ban(serverId, Boolean(input.confirmTakeover));
      if (result === 'takeover') return error('fail2ban_takeover_confirmation_required', 'Taking over an installed fail2ban service requires confirmation.', 422);
      return result === 'accepted' ? json(accepted('server-fail2ban-apply'), 202) : error('server_not_found', 'Server was not found.', 404);
    }

    if (url.pathname === '/api/v1/credentials' && method(init) === 'GET') return json(mockCredentials);
    if (url.pathname === '/api/v1/credentials' && method(init) === 'POST') {
      const input = await body<CredentialInput>(init);
      return json(createCredential({ id: `cred-${Date.now()}`, name: input.name, type: input.type, username: input.username, createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() }), 201);
    }
    const credentialMatch = url.pathname.match(/^\/api\/v1\/credentials\/([^/]+)$/);
    if (credentialMatch && method(init) === 'PUT') {
      const input = await body<CredentialInput>(init);
      const saved = updateCredential(decodeURIComponent(credentialMatch[1]), { name: input.name, type: input.type, username: input.username, createdAt: '', updatedAt: new Date().toISOString() });
      return saved ? json(saved) : error('credential_not_found', 'Credential was not found.', 404);
    }
    if (credentialMatch && method(init) === 'DELETE') {
      const result = deleteCredential(decodeURIComponent(credentialMatch[1]));
      if (result === 'in_use') return error('credential_in_use', 'Credential is still used by one or more servers.', 409);
      return result === 'deleted' ? json(null) : error('credential_not_found', 'Credential was not found.', 404);
    }

    if (url.pathname === '/api/v1/dns/domains' && method(init) === 'GET') return json(mockDnsDomains);
    if (url.pathname === '/api/v1/dns/domains' && method(init) === 'POST') {
      try {
        return json(createDomain(await body<{ name: string; provider: string }>(init)), 201);
      } catch {
        return error('dns_domain_conflict', 'Domain already exists.', 409);
      }
    }
    const domainMatch = url.pathname.match(/^\/api\/v1\/dns\/domains\/([^/]+)$/);
    if (domainMatch && method(init) === 'PUT') {
      const saved = updateDomain(decodeURIComponent(domainMatch[1]), await body<{ name: string; provider: string }>(init));
      return saved ? json(saved) : error('dns_domain_not_found', 'DNS domain was not found.', 404);
    }
    if (domainMatch && method(init) === 'DELETE') {
      try {
        return deleteDomain(decodeURIComponent(domainMatch[1])) ? json(null) : error('dns_domain_not_found', 'DNS domain was not found.', 404);
      } catch {
        return error('dns_domain_in_use', 'Domain has issued certificates and cannot be deleted.', 409);
      }
    }
    const recordsMatch = url.pathname.match(/^\/api\/v1\/dns\/domains\/([^/]+)\/records$/);
    if (recordsMatch && method(init) === 'GET') {
      try {
        const records = listRecords(decodeURIComponent(recordsMatch[1]));
        return records ? json(records) : error('dns_domain_not_found', 'DNS domain was not found.', 404);
      } catch (err) {
        return error('dns_provider_error', err instanceof Error ? err.message : 'DNS provider rejected the request.', 502);
      }
    }
    if (recordsMatch && method(init) === 'POST') {
      const saved = saveRecord(decodeURIComponent(recordsMatch[1]), await body(init));
      return saved ? json(saved, 201) : error('dns_domain_not_found', 'DNS domain was not found.', 404);
    }
    const recordMatch = url.pathname.match(/^\/api\/v1\/dns\/domains\/([^/]+)\/records\/([^/]+)$/);
    if (recordMatch && method(init) === 'PUT') {
      const saved = saveRecord(decodeURIComponent(recordMatch[1]), await body(init), decodeURIComponent(recordMatch[2]));
      return saved ? json(saved) : error('dns_record_not_found', 'DNS record was not found.', 404);
    }
    if (recordMatch && method(init) === 'DELETE') {
      return deleteRecord(decodeURIComponent(recordMatch[1]), decodeURIComponent(recordMatch[2])) ? json(null) : error('dns_record_not_found', 'DNS record was not found.', 404);
    }

    if (url.pathname === '/api/v1/certificates' && method(init) === 'GET') return json(mockDomainCertificates);
    if (url.pathname === '/api/v1/certificates' && method(init) === 'POST') {
      const result = issueCertificate(await body(init));
      return result ? json(result, 201) : error('dns_domain_not_found', 'DNS domain was not found.', 404);
    }
    const certOperationMatch = url.pathname.match(/^\/api\/v1\/certificates\/([^/]+)(?:\/renew)?$/);
    if (certOperationMatch && method(init) === 'PUT') {
      const result = reissueCertificate(decodeURIComponent(certOperationMatch[1]), await body(init));
      return result ? json(result, 202) : error('certificate_not_found', 'Certificate was not found.', 404);
    }
    if (certOperationMatch && method(init) === 'POST' && url.pathname.endsWith('/renew')) {
      return renewCertificate(decodeURIComponent(certOperationMatch[1])) ? json({ renewed: true }, 202) : error('certificate_not_found', 'Certificate was not found.', 404);
    }
    if (certOperationMatch && method(init) === 'DELETE') {
      return deleteCertificate(decodeURIComponent(certOperationMatch[1])) ? json(null) : error('certificate_not_found', 'Certificate was not found.', 404);
    }

    if (url.pathname === '/api/v1/self-signed-certificates' && method(init) === 'GET') return json(mockSelfSigned);
    if (url.pathname === '/api/v1/self-signed-cas' && method(init) === 'POST') return json(createSelfCa(await body(init)), 201);
    if (url.pathname === '/api/v1/self-signed-certificates' && method(init) === 'POST') return json(createSelfLeaf(await body(init)), 201);
    const selfMatch = url.pathname.match(/^\/api\/v1\/self-signed-certificates\/([^/]+)(?:\/renew)?$/);
    if (selfMatch && method(init) === 'POST' && url.pathname.endsWith('/renew')) {
      const renewed = renewSelfSigned(decodeURIComponent(selfMatch[1]));
      return renewed ? json(renewed) : error('self_signed_certificate_not_found', 'Self-signed certificate was not found.', 404);
    }
    if (selfMatch && method(init) === 'DELETE') {
      return deleteSelfSigned(decodeURIComponent(selfMatch[1])) ? json(null) : error('self_signed_certificate_not_found', 'Self-signed certificate was not found.', 404);
    }

    if (url.pathname === '/api/v1/key-assets' && method(init) === 'GET') return json(mockKeyAssets);
    if (url.pathname === '/api/v1/key-assets/ca' && method(init) === 'POST') return json(createAsset(await body(init), 'ca_certificate'), 201);
    if (url.pathname === '/api/v1/key-assets/tls' && method(init) === 'POST') return json(createAsset(await body(init), 'tls_certificate'), 201);
    if (url.pathname === '/api/v1/key-assets/ssh/generate' && method(init) === 'POST') return json(createAsset(await body(init), 'ssh_key_pair'), 201);
    if (url.pathname === '/api/v1/key-assets/import' && method(init) === 'POST') return json(createAsset(await body(init)), 201);
    if (url.pathname === '/api/v1/key-assets/exports' && method(init) === 'POST') return json({ taskId: `task-export-${Date.now()}` }, 202);
    if (url.pathname === '/api/v1/key-assets/system' && method(init) === 'GET') return json(systemCertificates());
    const systemCertificateResetMatch = url.pathname.match(/^\/api\/v1\/key-assets\/system\/(.+)\/reset$/);
    if (systemCertificateResetMatch && method(init) === 'POST') {
      const certificateId = decodeURIComponent(systemCertificateResetMatch[1]);
      return systemCertificates().some((item) => item.id === certificateId)
        ? json({ taskId: `task-agent-cert-reset-${Date.now()}` }, 202)
        : error('system_certificate_not_found', 'System certificate was not found.', 404);
    }
    const assetFileMatch = url.pathname.match(/^\/api\/v1\/key-assets\/([^/]+)\/files\/([^/]+)$/);
    if (assetFileMatch && method(init) === 'GET') {
      const assetId = decodeURIComponent(assetFileMatch[1]);
      const kind = decodeURIComponent(assetFileMatch[2]);
      const asset = mockKeyAssets.find((item) => item.id === assetId);
      return asset ? blobResponse(`mock key asset file\nasset=${assetId}\nkind=${kind}\n`, `${assetId}-${kind}.pem`) : error('key_asset_not_found', 'Key asset was not found.', 404);
    }
    const assetExportDownloadMatch = url.pathname.match(/^\/api\/v1\/key-assets\/exports\/([^/]+)\/download$/);
    if (assetExportDownloadMatch && method(init) === 'GET') {
      const taskId = decodeURIComponent(assetExportDownloadMatch[1]);
      return blobResponse(`mock key asset export\n${taskId}\n`, `${taskId}.panel-key-assets`);
    }
    if (url.pathname === '/api/v1/key-assets/imports/preflight' && method(init) === 'POST') {
      return json({
        planId: 'plan-conflict-demo',
        expiresAt: new Date(Date.now() + 600000).toISOString(),
        summary: { totalAssets: 3, caCount: 1, tlsCount: 1, sshCount: 1, standaloneTlsCount: 0, conflictCount: 2 },
        assets: [
          { assetId: 'asset-ca-platform', type: 'ca_certificate', name: 'Platform CA', standalone: false, conflictTypes: ['id_conflict', 'overwrite_in_use'] },
          { assetId: 'asset-new-tls', type: 'tls_certificate', name: 'Imported TLS', standalone: false, conflictTypes: [] },
        ],
        conflicts: [
          { assetId: 'asset-ca-platform', assetName: 'Platform CA', assetType: 'ca_certificate', conflictType: 'id_conflict', existingAssetId: 'asset-ca-platform', existingAssetName: 'Platform CA' },
          { assetId: 'asset-ca-platform', assetName: 'Platform CA', assetType: 'ca_certificate', conflictType: 'overwrite_in_use', existingAssetId: 'asset-ca-platform', existingAssetName: 'Platform CA', affectedReferences: [{ resourceType: 'application', resourceId: 'app-gateway', resourceName: 'Entrance gateway', relation: 'ca' }] },
        ],
        requiresDangerConfirm: true,
      });
    }
    const importExecuteMatch = url.pathname.match(/^\/api\/v1\/key-assets\/imports\/([^/]+)\/execute$/);
    if (importExecuteMatch && method(init) === 'POST') {
      const input = await body<{ confirmDangerousOverwrite?: boolean }>(init);
      if (!input.confirmDangerousOverwrite) return error('key_asset_import_confirmation_required', 'Overwrite confirmation is required for in-use key assets.', 409);
      return json({ taskId: `task-import-${Date.now()}`, operationId: 'key_asset_import' }, 202);
    }
    const assetMatch = url.pathname.match(/^\/api\/v1\/key-assets\/([^/]+)$/);
    if (assetMatch && method(init) === 'GET') {
      const asset = mockKeyAssets.find((item) => item.id === decodeURIComponent(assetMatch[1]));
      return asset ? json(asset) : error('key_asset_not_found', 'Key asset was not found.', 404);
    }
    if (assetMatch && method(init) === 'DELETE') {
      const result = deleteAsset(decodeURIComponent(assetMatch[1]));
      if (result === 'conflict') return error('key_asset_in_use', 'Key asset is in use and cannot be deleted.', 409);
      return result === 'deleted' ? json(null) : error('key_asset_not_found', 'Key asset was not found.', 404);
    }
    const assetOperationMatch = url.pathname.match(/^\/api\/v1\/key-assets\/([^/]+)\/(reissue|regenerate)$/);
    if (assetOperationMatch && method(init) === 'POST') {
      const result = mutateAsset(decodeURIComponent(assetOperationMatch[1]), assetOperationMatch[2] as 'reissue' | 'regenerate');
      return result ? json(result, 202) : error('key_asset_not_found', 'Key asset was not found.', 404);
    }

    if (url.pathname === '/api/v1/applications' && method(init) === 'GET') return json(mockApplicationSummaries());
    const appMatch = url.pathname.match(/^\/api\/v1\/applications\/([^/]+)$/);
    if (appMatch && method(init) === 'GET') {
      const found = mockApplications.find((item) => item.id === decodeURIComponent(appMatch[1]));
      return found ? json(found) : error('application_not_found', 'Application was not found.', 404);
    }
    if (appMatch && method(init) === 'DELETE') {
      return deleteApp(decodeURIComponent(appMatch[1])) ? json(null) : error('application_not_found', 'Application was not found.', 404);
    }
    const appRuntimeMatch = url.pathname.match(/^\/api\/v1\/applications\/([^/]+)\/runtime$/);
    if (appRuntimeMatch) {
      const runtime = appRuntime(decodeURIComponent(appRuntimeMatch[1]));
      return runtime ? json(runtime) : error('application_runtime_unavailable', 'Application runtime is unavailable.', 503);
    }
    const appLogsMatch = url.pathname.match(/^\/api\/v1\/applications\/([^/]+)\/logs$/);
    if (appLogsMatch) {
      try {
        return json(appLogs(decodeURIComponent(appLogsMatch[1])));
      } catch (err) {
        return error('application_logs_unavailable', err instanceof Error ? err.message : 'Logs are unavailable.', 503);
      }
    }
    const appPersistentMatch = url.pathname.match(/^\/api\/v1\/applications\/([^/]+)\/persistent-data$/);
    if (appPersistentMatch && method(init) === 'GET') {
      const appId = decodeURIComponent(appPersistentMatch[1]);
      const app = mockApplications.find((item) => item.id === appId);
      if (!app) return error('application_not_found', 'Application was not found.', 404);
      if (!app.persistentPath) return error('application_persistent_data_unavailable', 'Application has no persistent data mount.', 422);
      return blobResponse(`mock persistent archive\napplication=${appId}\npath=${app.persistentPath}\n`, `${appId}-persistent.zip`, 'application/zip');
    }
    if (appPersistentMatch && method(init) === 'POST') {
      const appId = decodeURIComponent(appPersistentMatch[1]);
      const form = await formBody(init);
      if (!(form.get('file') instanceof File)) return error('application_persistent_archive_required', 'Persistent data restore requires a zip archive.', 422);
      const result = restorePersistentData(appId);
      return result ? json(result, 202) : error('application_not_found', 'Application was not found.', 404);
    }
    const appOperationMatch = url.pathname.match(/^\/api\/v1\/applications\/([^/]+)\/(deploy|stop|restart|image\/check|image\/update)$/);
    if (appOperationMatch && method(init) === 'POST') {
      const appId = decodeURIComponent(appOperationMatch[1]);
      if (appOperationMatch[2] === 'image/check') {
        const app = mockApplications.find((item) => item.id === appId);
        return app ? json(app) : error('application_not_found', 'Application was not found.', 404);
      }
      const result = appOperation(appId, appOperationMatch[2]);
      return result ? json(result, 202) : error('application_not_found', 'Application was not found.', 404);
    }

    if (url.pathname === '/api/v1/application-edit-sessions/recoverable') return json([]);
    if (url.pathname === '/api/v1/application-edit-sessions' && method(init) === 'POST') {
      const input = await body<{ applicationId?: string }>(init);
      return json(beginAppSession(input.applicationId), 201);
    }
    const appSessionMatch = url.pathname.match(/^\/api\/v1\/application-edit-sessions\/([^/]+)$/);
    if (appSessionMatch && method(init) === 'GET') {
      const session = appSession(decodeURIComponent(appSessionMatch[1]));
      return session ? json(session) : error('application_edit_session_not_found', 'Application edit session was not found.', 404);
    }
    if (appSessionMatch && method(init) === 'DELETE') return json(null);
    const appSessionDraftMatch = url.pathname.match(/^\/api\/v1\/application-edit-sessions\/([^/]+)\/draft$/);
    if (appSessionDraftMatch && method(init) === 'PATCH') {
      const input = await body<{ draft: ApplicationEditSession['draft'] }>(init);
      const session = patchAppSession(decodeURIComponent(appSessionDraftMatch[1]), input.draft);
      return session ? json(session) : error('application_edit_session_not_found', 'Application edit session was not found.', 404);
    }
    const appSessionFileMatch = url.pathname.match(/^\/api\/v1\/application-edit-sessions\/([^/]+)\/files\/([^/]+)$/);
    if (appSessionFileMatch && method(init) === 'GET') {
      const file = getAppFile(decodeURIComponent(appSessionFileMatch[1]), decodeURIComponent(appSessionFileMatch[2]));
      return file ? json(file) : error('application_edit_session_file_not_found', 'Application edit session file was not found.', 404);
    }
    if (appSessionFileMatch && method(init) === 'PUT') {
      const session = putAppFile(decodeURIComponent(appSessionFileMatch[1]), decodeURIComponent(appSessionFileMatch[2]), await body<{ path: string; kind: string; contentType: string; contentBase64: string }>(init));
      return session ? json(session) : error('application_edit_session_not_found', 'Application edit session was not found.', 404);
    }
    if (appSessionFileMatch && method(init) === 'DELETE') {
      const session = deleteAppFile(decodeURIComponent(appSessionFileMatch[1]), decodeURIComponent(appSessionFileMatch[2]));
      return session ? json(session) : error('application_edit_session_not_found', 'Application edit session was not found.', 404);
    }
    const appSessionArchiveMatch = url.pathname.match(/^\/api\/v1\/application-edit-sessions\/([^/]+)\/archives$/);
    if (appSessionArchiveMatch && method(init) === 'POST') {
      const form = await formBody(init);
      const file = form.get('file');
      const fileKey = textField(form, 'fileKey') || `archive-${Date.now()}`;
      const session = uploadAppArchive(decodeURIComponent(appSessionArchiveMatch[1]), {
        fileKey,
        basePath: textField(form, 'basePath') || '/',
        filename: file instanceof File ? file.name : `${fileKey}.zip`,
        size: file instanceof File ? file.size : 0,
        contentType: file instanceof File ? file.type || 'application/zip' : 'application/zip',
      });
      return session ? json(session) : error('application_edit_session_not_found', 'Application edit session was not found.', 404);
    }
    const appSessionValidateMatch = url.pathname.match(/^\/api\/v1\/application-edit-sessions\/([^/]+)\/(validate|preview)$/);
    if (appSessionValidateMatch && method(init) === 'POST') {
      const session = appSession(decodeURIComponent(appSessionValidateMatch[1]));
      if (!session) return error('application_edit_session_not_found', 'Application edit session was not found.', 404);
      const diagnostics = appDiagnostics(session);
      if (appSessionValidateMatch[2] === 'validate') return json({ valid: !diagnostics.some((item) => item.severity === 'error'), revision: session.revision, diagnostics });
      return json({ revision: session.revision, diagnostics, token: { value: `apreview-${Date.now()}`, action: 'application.commit', subjectVersion: session.baseResourceVersion.value }, expiresAt: '2026-07-21T08:05:00.000Z' });
    }
    const appSessionCommitMatch = url.pathname.match(/^\/api\/v1\/application-edit-sessions\/([^/]+)\/commit$/);
    if (appSessionCommitMatch && method(init) === 'POST') {
      try {
        const result = commitAppSession(decodeURIComponent(appSessionCommitMatch[1]));
        return result ? json(result) : error('application_edit_session_not_found', 'Application edit session was not found.', 404);
      } catch (err) {
        return error('resource_version_conflict', err instanceof Error ? err.message : 'Application changed while editing.', 409);
      }
    }

    if (url.pathname === '/api/v1/facility-apps' && method(init) === 'GET') return json(mockFacilitySummaries());
    if (url.pathname === '/api/v1/facility-apps/reverse-proxy' && method(init) === 'GET') return json(mockFacility);
    if (url.pathname === '/api/v1/facility-apps/reverse-proxy/reconcile' && method(init) === 'POST') return json({ config: mockFacility }, 202);
    if (url.pathname === '/api/v1/facility-apps/reverse-proxy/edit-sessions/recoverable') return json([]);
    if (url.pathname === '/api/v1/facility-apps/reverse-proxy/edit-sessions' && method(init) === 'POST') return json(beginFacilitySession(), 201);
    const facilitySessionMatch = url.pathname.match(/^\/api\/v1\/facility-apps\/reverse-proxy\/edit-sessions\/([^/]+)$/);
    if (facilitySessionMatch && method(init) === 'GET') {
      const session = facilitySession(decodeURIComponent(facilitySessionMatch[1]));
      return session ? json(session) : error('facility_edit_session_not_found', 'Facility edit session was not found.', 404);
    }
    if (facilitySessionMatch && method(init) === 'DELETE') return json(null);
    const facilityDraftMatch = url.pathname.match(/^\/api\/v1\/facility-apps\/reverse-proxy\/edit-sessions\/([^/]+)\/draft$/);
    if (facilityDraftMatch && method(init) === 'PATCH') {
      const input = await body<{ draft: FacilityEditSession['draft'] }>(init);
      const session = patchFacilitySession(decodeURIComponent(facilityDraftMatch[1]), input.draft);
      return session ? json(session) : error('facility_edit_session_not_found', 'Facility edit session was not found.', 404);
    }
    const facilityAssetMatch = url.pathname.match(/^\/api\/v1\/facility-apps\/reverse-proxy\/edit-sessions\/([^/]+)\/assets\/([^/]+)$/);
    if (facilityAssetMatch && method(init) === 'PUT') {
      const form = await formBody(init);
      const file = form.get('file');
      const assetKey = decodeURIComponent(facilityAssetMatch[2]);
      const session = putFacilityAsset(decodeURIComponent(facilityAssetMatch[1]), assetKey, {
        name: textField(form, 'name') || (file instanceof File ? file.name.replace(/\.[^.]+$/, '') : assetKey),
        kind: textField(form, 'kind') || (file instanceof File && file.name.endsWith('.zip') ? 'uploaded_bundle' : 'uploaded_file'),
        filename: file instanceof File ? file.name : `${assetKey}.bin`,
        size: file instanceof File ? file.size : 0,
      });
      return session ? json(session) : error('facility_edit_session_not_found', 'Facility edit session was not found.', 404);
    }
    if (facilityAssetMatch && method(init) === 'DELETE') {
      const session = deleteFacilityAsset(decodeURIComponent(facilityAssetMatch[1]), decodeURIComponent(facilityAssetMatch[2]));
      return session ? json(session) : error('facility_edit_session_not_found', 'Facility edit session was not found.', 404);
    }
    const facilityValidateMatch = url.pathname.match(/^\/api\/v1\/facility-apps\/reverse-proxy\/edit-sessions\/([^/]+)\/(validate|preview)$/);
    if (facilityValidateMatch && method(init) === 'POST') {
      const session = facilitySession(decodeURIComponent(facilityValidateMatch[1]));
      if (!session) return error('facility_edit_session_not_found', 'Facility edit session was not found.', 404);
      const diagnostics = facilityDiagnostics(session);
      if (facilityValidateMatch[2] === 'validate') return json({ valid: !diagnostics.some((item) => item.severity === 'error'), revision: session.revision, diagnostics });
      return json({ revision: session.revision, diagnostics, token: { value: `fpreview-${Date.now()}`, action: 'facility.reverse_proxy.commit', subjectVersion: session.baseResourceVersion.value }, expiresAt: '2026-07-21T08:05:00.000Z' });
    }
    const facilityCommitMatch = url.pathname.match(/^\/api\/v1\/facility-apps\/reverse-proxy\/edit-sessions\/([^/]+)\/commit$/);
    if (facilityCommitMatch && method(init) === 'POST') {
      try {
        const result = commitFacilitySession(decodeURIComponent(facilityCommitMatch[1]));
        return result ? json(result) : error('facility_edit_session_not_found', 'Facility edit session was not found.', 404);
      } catch (err) {
        return error('facility_commit_failed', err instanceof Error ? err.message : 'Gateway commit failed after preview.', 409);
      }
    }

    if (url.pathname === '/api/v1/tasks' && method(init) === 'GET') {
      const status = url.searchParams.get('status') || '';
      const type = url.searchParams.get('type') || '';
      const operationId = url.searchParams.get('operationId') || '';
      const page = Math.max(1, Number(url.searchParams.get('page') || 1));
      const pageSize = Math.max(1, Math.min(100, Number(url.searchParams.get('pageSize') || url.searchParams.get('limit') || 50)));
      let items = mockTasks;
      if (status) items = items.filter((item) => item.status === status);
      if (type) items = items.filter((item) => item.type === type);
      if (operationId) items = items.filter((item) => item.operationId === operationId);
      const total = items.length;
      const start = (page - 1) * pageSize;
      return json({ items: items.slice(start, start + pageSize), total, page, pageSize });
    }
    const taskMatch = url.pathname.match(/^\/api\/v1\/tasks\/([^/]+)$/);
    if (taskMatch && method(init) === 'GET') {
      const found = mockTasks.find((item) => item.id === decodeURIComponent(taskMatch[1]));
      return found ? json(found) : error('task_not_found', 'Task was not found.', 404);
    }
    const taskStepsMatch = url.pathname.match(/^\/api\/v1\/tasks\/([^/]+)\/steps$/);
    if (taskStepsMatch && method(init) === 'GET') return json(mockTaskSteps[decodeURIComponent(taskStepsMatch[1])] ?? []);
    const taskLogsMatch = url.pathname.match(/^\/api\/v1\/tasks\/([^/]+)\/logs$/);
    if (taskLogsMatch && method(init) === 'GET') {
      const after = Number(url.searchParams.get('after') || 0);
      const logs = (mockTaskLogs[decodeURIComponent(taskLogsMatch[1])] ?? []).filter((item) => item.cursor > after);
      return json({ nextCursor: logs.at(-1)?.cursor ?? after, logs });
    }
    const taskOperationMatch = url.pathname.match(/^\/api\/v1\/tasks\/([^/]+)\/(retry|run-now)$/);
    if (taskOperationMatch && method(init) === 'POST') {
      const result = taskOperationMatch[2] === 'retry' ? retryTask(decodeURIComponent(taskOperationMatch[1])) : runTaskNow(decodeURIComponent(taskOperationMatch[1]));
      return result ? json(result, 202) : error('task_operation_unavailable', 'Task operation is unavailable for this task.', 422);
    }

    if (url.pathname === '/api/v1/application-operations' && method(init) === 'GET') {
      const applicationId = url.searchParams.get('applicationId') || '';
      const status = url.searchParams.get('status') || '';
      const source = url.searchParams.get('source') || '';
      const page = Math.max(1, Number(url.searchParams.get('page') || 1));
      const pageSize = Math.max(1, Math.min(100, Number(url.searchParams.get('pageSize') || 20)));
      let items = mockApplicationOperations;
      if (applicationId) items = items.filter((item) => item.applicationId.includes(applicationId));
      if (status) items = items.filter((item) => item.status === status);
      if (source) items = items.filter((item) => item.source === source);
      const total = items.length;
      const start = (page - 1) * pageSize;
      return json({ items: items.slice(start, start + pageSize), total, page, pageSize });
    }
    const applicationOperationMatch = url.pathname.match(/^\/api\/v1\/application-operations\/([^/]+)$/);
    if (applicationOperationMatch && method(init) === 'GET') {
      const found = applicationOperationDetail(decodeURIComponent(applicationOperationMatch[1]));
      return found ? json(found) : error('application_operation_detail_unavailable', 'Application operation detail is unavailable.', 404);
    }

    if (url.pathname === '/api/v1/system-events' && method(init) === 'GET') {
      const subjectId = url.searchParams.get('subjectId') || '';
      const eventType = url.searchParams.get('eventType') || '';
      const severity = url.searchParams.get('severity') || '';
      const category = url.searchParams.get('category') || '';
      const from = url.searchParams.get('from') || '';
      const to = url.searchParams.get('to') || '';
      const page = Math.max(1, Number(url.searchParams.get('page') || 1));
      const pageSize = Math.max(1, Math.min(100, Number(url.searchParams.get('pageSize') || 20)));
      let items = mockSystemEvents;
      if (subjectId) items = items.filter((item) => (item.subjectId || '').includes(subjectId));
      if (eventType) items = items.filter((item) => item.eventType.includes(eventType));
      if (severity) items = items.filter((item) => item.severity === severity);
      if (category) items = items.filter((item) => item.category === category);
      if (from) items = items.filter((item) => item.occurredAt >= from);
      if (to) items = items.filter((item) => item.occurredAt <= to);
      const total = items.length;
      const start = (page - 1) * pageSize;
      return json({ items: items.slice(start, start + pageSize), total, page, pageSize });
    }
    const systemEventMatch = url.pathname.match(/^\/api\/v1\/system-events\/([^/]+)$/);
    if (systemEventMatch && method(init) === 'GET') {
      const found = systemEventDetail(decodeURIComponent(systemEventMatch[1]));
      return found ? json(found) : error('system_event_detail_unavailable', 'System event detail is unavailable.', 404);
    }

    if (url.pathname === '/api/v1/settings/runtime' && method(init) === 'GET') {
      return json(mockRuntimeSettings);
    }
    if (url.pathname === '/api/v1/settings/public-branding' && method(init) === 'GET') return json(mockRuntimeSettings.branding);
    if (url.pathname === '/api/v1/system/version' && method(init) === 'GET') return json({ version: '0.2.0-dev', channel: 'dev', commit: 'mock', repository: 'mock/panel', latestVersion: '', updateAvailable: false });
    if (url.pathname === '/api/v1/settings/runtime' && method(init) === 'PUT') {
      try {
        return json(saveRuntime(await body(init)));
      } catch (err) {
        return error('settings_conflict', err instanceof Error ? err.message : 'Settings conflict.', 409);
      }
    }
    if (url.pathname === '/api/v1/settings/server-variables' && method(init) === 'GET') return json(mockServerVariables);
    if (url.pathname === '/api/v1/settings/server-variables' && method(init) === 'PUT') {
      try {
        const input = await body<{ definitions: typeof mockServerVariables }>(init);
        return json(saveServerVariables(input.definitions ?? []));
      } catch (err) {
        return error('server_variables_invalid', err instanceof Error ? err.message : 'Server variables are invalid.', 422);
      }
    }
    if (url.pathname === '/api/v1/backups/export' && method(init) === 'POST') return json(startExport(), 202);
    if (url.pathname === '/api/v1/backups/restore/preflight' && method(init) === 'POST') return json(restorePreflight());
    if (url.pathname === '/api/v1/backups/restore/confirm' && method(init) === 'POST') return json(confirmRestore());

    if (url.pathname === '/api/v1/auth/login' && method(init) === 'POST') {
      const input = await body<{ username?: string; password?: string }>(init);
      if (!authValidationEnabled()) {
        mockUsername = input.username?.trim() || mockUsername;
        resetExport();
        return json(authSession());
      }
      if (input.username !== mockUsername || input.password !== mockPassword) return error('unauthorized', 'Invalid username or password.', 401);
      resetExport();
      return json(authSession());
    }
    if (url.pathname === '/api/v1/auth/session' && method(init) === 'GET') {
      if (!authValidationEnabled()) return json(authSession());
      return authorization(init) === `Bearer ${mockAuthToken}` ? json(authSession()) : json({ authenticated: false });
    }
    if (url.pathname === '/api/v1/auth/logout' && method(init) === 'POST') return json({ authenticated: false });
    if (url.pathname === '/api/v1/auth/account' && method(init) === 'POST') {
      const input = await body<{ currentPassword?: string; username?: string; newPassword?: string }>(init);
      if (!authValidationEnabled()) {
        mockUsername = input.username?.trim() || mockUsername;
        mockPasswordChangeRequired = false;
        return json(authSession());
      }
      if (input.currentPassword !== mockPassword) return error('unauthorized', 'Authentication failed.', 401);
      if (!input.username?.trim()) return error('admin_username_required', 'Username is required.', 422);
      const newPassword = input.newPassword || '';
      if (newPassword.length < 8) return error('admin_password_too_short', 'Password must be at least 8 characters.', 422);
      if (newPassword === input.currentPassword) return error('admin_password_unchanged', 'New password must be different from the current password.', 422);
      mockPassword = newPassword;
      mockUsername = input.username.trim();
      mockPasswordChangeRequired = false;
      return json(authSession());
    }
    if (url.pathname === '/api/v1/auth/jwt-secret' && method(init) === 'POST') {
      const input = await body<{ jwtSecret?: string }>(init);
      if (!authValidationEnabled()) return json(authSession());
      if ((input.jwtSecret || '').trim().length < 16) return error('invalid_jwt_secret', 'JWT secret must be at least 16 characters.', 422);
      return json(authSession());
    }
    if (url.pathname === '/api/v1/backups/export/current' && method(init) === 'GET') return json(exportStatus());
    if (url.pathname === '/api/v1/backups/export/start' && method(init) === 'POST') return json(advanceExport(), 202);
    if (url.pathname === '/api/v1/backups/export/password' && method(init) === 'POST') return json(advanceExport(), 202);
    if (url.pathname === '/api/v1/backups/export/exit' && method(init) === 'POST') return json(exportStatus());
    const exportDownloadMatch = url.pathname.match(/^\/api\/v1\/backups\/export\/([^/]+)\/download$/);
    if (exportDownloadMatch && method(init) === 'GET') {
      if (!authorization(init)) return error('unauthorized', 'Maintenance token is required.', 401);
      return blobResponse('mock backup archive', `${decodeURIComponent(exportDownloadMatch[1])}.zip`);
    }
    if (url.pathname === '/api/v1/restore/status' && method(init) === 'GET') return json(restoreStatus());
    if (url.pathname === '/api/v1/restore/password' && method(init) === 'POST') return json(restoreStatus(), 202);
    if (url.pathname === '/api/v1/restore/retry' && method(init) === 'POST') return json({ ...restoreStatus(), phase: 'password_required', progress: 10 }, 202);
    if (url.pathname === '/api/v1/restore/clear-pending' && method(init) === 'POST') return json({ ...restoreStatus(), phase: 'completed', progress: 100 });

    if (url.pathname === '/api/v1/debug/snapshot') {
      try {
        return json(debugSnapshot());
      } catch (err) {
        return error('debug_snapshot_failed', err instanceof Error ? err.message : 'Unable to collect diagnostics.', 503);
      }
    }

    const packagesMatch = url.pathname.match(/^\/api\/v1\/servers\/([^/]+)\/packages\/(updates|refresh|upgrade-selected|upgrade-all)$/);
    if (packagesMatch) {
      const serverId = decodeURIComponent(packagesMatch[1]);
      const action = packagesMatch[2];
      try {
        if (action === 'updates' && method(init) === 'GET') {
          const state = mockPackages(serverId);
          return state ? json(state) : error('server_not_found', 'Server was not found.', 404);
        }
        if (action === 'refresh' && method(init) === 'POST') {
          const state = mockRefreshPackages(serverId);
          return state ? json(state, 202) : error('server_not_found', 'Server was not found.', 404);
        }
        if (action === 'upgrade-selected' && method(init) === 'POST') {
          const input = await body<{ packages?: string[] }>(init);
          const acceptedTask = mockUpgradePackages(serverId, input.packages ?? []);
          return acceptedTask ? json(acceptedTask, 202) : error('server_not_found', 'Server was not found.', 404);
        }
        if (action === 'upgrade-all' && method(init) === 'POST') {
          const acceptedTask = mockUpgradePackages(serverId);
          return acceptedTask ? json(acceptedTask, 202) : error('server_not_found', 'Server was not found.', 404);
        }
      } catch (err) {
        return error('server_unreachable', err instanceof Error ? err.message : 'Server is unreachable.', 502);
      }
    }

    const containerListMatch = url.pathname.match(/^\/api\/v1\/servers\/([^/]+)\/containers$/);
    if (containerListMatch && method(init) === 'GET') {
      try {
        const items = mockContainers(decodeURIComponent(containerListMatch[1]));
        return items ? json(items) : error('server_not_found', 'Server was not found.', 404);
      } catch (err) {
        return error('agent_required', err instanceof Error ? err.message : 'Agent is required.', 422);
      }
    }
    const containerLogsMatch = url.pathname.match(/^\/api\/v1\/servers\/([^/]+)\/containers\/([^/]+)\/logs$/);
    if (containerLogsMatch && method(init) === 'GET') {
      const logs = mockContainerLogs(decodeURIComponent(containerLogsMatch[1]), decodeURIComponent(containerLogsMatch[2]));
      return logs ? json(logs) : error('container_not_found', 'Container was not found.', 404);
    }
    const containerActionMatch = url.pathname.match(/^\/api\/v1\/servers\/([^/]+)\/containers\/([^/]+)\/(start|stop|restart)$/);
    if (containerActionMatch && method(init) === 'POST') {
      const result = mockContainerAction(decodeURIComponent(containerActionMatch[1]), decodeURIComponent(containerActionMatch[2]), containerActionMatch[3]);
      if (result === 'managed') return error('container_managed_by_application', 'Managed application containers must be changed from the application lifecycle.', 409);
      return result === 'ok' ? json({}) : error('container_not_found', 'Container was not found.', 404);
    }
    const containerDeleteMatch = url.pathname.match(/^\/api\/v1\/servers\/([^/]+)\/containers\/([^/]+)$/);
    if (containerDeleteMatch && method(init) === 'DELETE') {
      const result = mockDeleteContainer(decodeURIComponent(containerDeleteMatch[1]), decodeURIComponent(containerDeleteMatch[2]));
      if (result === 'managed') return error('container_managed_by_application', 'Managed application containers must be changed from the application lifecycle.', 409);
      return result === 'ok' ? json({}) : error('container_not_found', 'Container was not found.', 404);
    }

    const imageMatch = url.pathname.match(/^\/api\/v1\/servers\/([^/]+)\/images$/);
    if (imageMatch && method(init) === 'GET') {
      try {
        const images = mockImages(decodeURIComponent(imageMatch[1]));
        return images ? json(images) : error('server_not_found', 'Server was not found.', 404);
      } catch (err) {
        return error('agent_incompatible', err instanceof Error ? err.message : 'Agent is not compatible.', 422);
      }
    }
    const imagePullMatch = url.pathname.match(/^\/api\/v1\/servers\/([^/]+)\/images\/pull$/);
    if (imagePullMatch && method(init) === 'POST') {
      const input = await body<{ reference: string }>(init);
      const result = mockPullImage(decodeURIComponent(imagePullMatch[1]), input.reference);
      return result ? json(result) : error('server_not_found', 'Server was not found.', 404);
    }
    const imageRefreshMatch = url.pathname.match(/^\/api\/v1\/servers\/([^/]+)\/images\/refresh$/);
    if (imageRefreshMatch && method(init) === 'POST') return json(accepted('image-refresh'), 202);
    const imagePruneMatch = url.pathname.match(/^\/api\/v1\/servers\/([^/]+)\/images\/delete-unused$/);
    if (imagePruneMatch && method(init) === 'POST') {
      const result = mockPruneImages(decodeURIComponent(imagePruneMatch[1]));
      return result ? json(result) : error('server_not_found', 'Server was not found.', 404);
    }
    const imageDeleteMatch = url.pathname.match(/^\/api\/v1\/servers\/([^/]+)\/images\/([^/]+)$/);
    if (imageDeleteMatch && method(init) === 'DELETE') {
      const result = mockDeleteImage(decodeURIComponent(imageDeleteMatch[1]), decodeURIComponent(imageDeleteMatch[2]));
      if (result === 'in_use') return error('image_in_use', 'Image is in use.', 409);
      return result === 'ok' ? json({}) : error('image_not_found', 'Image was not found.', 404);
    }
    if (url.pathname === '/api/v1/images/upgrade-selected' && method(init) === 'POST') return json(accepted('application-image-upgrade-selected'), 202);
    if (url.pathname === '/api/v1/images/upgrade-all' && method(init) === 'POST') return json(accepted('application-image-upgrade-all'), 202);

    const networkMatch = url.pathname.match(/^\/api\/v1\/servers\/([^/]+)\/networks$/);
    if (networkMatch && method(init) === 'GET') {
      const items = mockNetworks(decodeURIComponent(networkMatch[1]));
      return items ? json(items) : error('server_not_found', 'Server was not found.', 404);
    }
    const volumeMatch = url.pathname.match(/^\/api\/v1\/servers\/([^/]+)\/volumes$/);
    if (volumeMatch && method(init) === 'GET') {
      const items = mockVolumes(decodeURIComponent(volumeMatch[1]));
      return items ? json(items) : error('server_not_found', 'Server was not found.', 404);
    }
    const volumePruneMatch = url.pathname.match(/^\/api\/v1\/servers\/([^/]+)\/volumes\/delete-unused$/);
    if (volumePruneMatch && method(init) === 'POST') {
      const result = mockPruneVolumes(decodeURIComponent(volumePruneMatch[1]));
      return result ? json(result) : error('server_not_found', 'Server was not found.', 404);
    }
    const volumeDeleteMatch = url.pathname.match(/^\/api\/v1\/servers\/([^/]+)\/volumes\/([^/]+)$/);
    if (volumeDeleteMatch && method(init) === 'DELETE') {
      const result = mockDeleteVolume(decodeURIComponent(volumeDeleteMatch[1]), decodeURIComponent(volumeDeleteMatch[2]));
      if (result === 'in_use') return error('volume_in_use', 'Volume is in use.', 409);
      return result === 'ok' ? json({}) : error('volume_not_found', 'Volume was not found.', 404);
    }

    return error('mock_route_not_found', `Mock route not found: ${url.pathname}`, 404, { path: url.pathname });
  };
}

function method(init?: RequestInit) {
  return (init?.method ?? 'GET').toUpperCase();
}

async function body<T>(init?: RequestInit): Promise<T> {
  if (!init?.body || typeof init.body !== 'string') return {} as T;
  return JSON.parse(init.body) as T;
}

async function formBody(init?: RequestInit): Promise<FormData> {
  if (!init?.body) return new FormData();
  if (init.body instanceof FormData) return init.body;
  if (typeof init.body === 'string') {
    const form = new FormData();
    const parsed = JSON.parse(init.body) as Record<string, unknown>;
    Object.entries(parsed).forEach(([key, value]) => {
      if (value !== undefined && value !== null) form.set(key, String(value));
    });
    return form;
  }
  return new FormData();
}

function textField(form: FormData, key: string) {
  const value = form.get(key);
  return typeof value === 'string' ? value : '';
}

function authorization(init?: RequestInit) {
  const headers = new Headers(init?.headers);
  return headers.get('Authorization') || headers.get('authorization') || '';
}
