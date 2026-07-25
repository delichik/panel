import type { IssueCertificateInput, SelfSignedCaInput, SelfSignedLeafInput } from '@/types/certificates';
import type { DnsDomainDto, DnsRecordDto, DnsRecordInput } from '@/types/dns';
import type {
  CreateCaAssetInput,
  CreateTlsAssetInput,
  GenerateSshAssetInput,
  ImportKeyAssetInput,
  KeyAssetDto,
} from '@/types/keyAssets';

const now = '2026-07-21T02:00:00.000Z';

export const mockDnsDomains: DnsDomainDto[] = [
  { id: 'domain-example', name: 'example.com', provider: 'cloudflare', createdAt: '2026-07-18T08:00:00.000Z', updatedAt: now },
  { id: 'domain-empty', name: 'empty-zone.test', provider: 'cloudflare', createdAt: '2026-07-19T08:00:00.000Z', updatedAt: now },
  { id: 'domain-error', name: 'token-expired.test', provider: 'cloudflare', createdAt: '2026-07-20T08:00:00.000Z', updatedAt: now },
  { id: 'domain-shop', name: 'shop.example.test', provider: 'cloudflare', createdAt: '2026-07-16T08:00:00.000Z', updatedAt: now },
  { id: 'domain-api', name: 'api.example.test', provider: 'cloudflare', createdAt: '2026-07-16T09:00:00.000Z', updatedAt: now },
  { id: 'domain-static', name: 'static.example.test', provider: 'cloudflare', createdAt: '2026-07-15T08:00:00.000Z', updatedAt: now },
  { id: 'domain-downloads', name: 'downloads.example.test', provider: 'cloudflare', createdAt: '2026-07-15T09:00:00.000Z', updatedAt: now },
  { id: 'domain-long', name: 'very-long-customer-facing-domain-name-for-layout-validation.example.test', provider: 'cloudflare', createdAt: '2026-07-14T08:00:00.000Z', updatedAt: now },
];

const recordsByDomain = new Map<string, DnsRecordDto[]>([
  ['domain-example', [
    { id: 'rec-root', type: 'A', name: '@', value: '203.0.113.10', ttl: 300, proxied: true, modifiedAt: now },
    { id: 'rec-api', type: 'CNAME', name: 'api', value: 'gateway.example.com', ttl: 300, proxied: true, modifiedAt: now },
    { id: 'rec-txt', type: 'TXT', name: '_acme-challenge', value: 'ready-for-dns-01', ttl: 120, proxied: false, modifiedAt: now },
  ]],
  ['domain-empty', []],
  ['domain-shop', [
    { id: 'rec-shop-root', type: 'CNAME', name: '@', value: 'gateway.example.test', ttl: 300, proxied: true, modifiedAt: now },
    { id: 'rec-shop-www', type: 'CNAME', name: 'www', value: 'shop.example.test', ttl: 300, proxied: true, modifiedAt: now },
    { id: 'rec-shop-acme', type: 'TXT', name: '_acme-challenge', value: 'shop-dns-ready', ttl: 120, proxied: false, modifiedAt: now },
  ]],
  ['domain-api', [
    { id: 'rec-api-root', type: 'A', name: '@', value: '203.0.113.21', ttl: 120, proxied: true, modifiedAt: now },
    { id: 'rec-api-events', type: 'CNAME', name: 'events', value: 'api.example.test', ttl: 120, proxied: true, modifiedAt: now },
    { id: 'rec-api-mx', type: 'MX', name: '@', value: '10 mail.example.test', ttl: 3600, proxied: false, modifiedAt: now },
  ]],
  ['domain-static', [
    { id: 'rec-static-root', type: 'A', name: '@', value: '203.0.113.34', ttl: 300, proxied: false, modifiedAt: now },
    { id: 'rec-static-cdn', type: 'CNAME', name: 'cdn', value: 'static.example.test', ttl: 300, proxied: true, modifiedAt: now },
  ]],
  ['domain-downloads', [
    { id: 'rec-downloads-root', type: 'A', name: '@', value: '203.0.113.35', ttl: 300, proxied: false, modifiedAt: now },
  ]],
  ['domain-long', [
    { id: 'rec-long-root', type: 'CNAME', name: '@', value: 'gateway.example.test', ttl: 600, proxied: true, comment: 'Long domain row used to validate wrapping and table density.', modifiedAt: now },
  ]],
]);

export const mockDomainCertificates = [
  {
    id: 'cert-wildcard-example',
    name: 'Wildcard example.com',
    domainId: 'domain-example',
    domain: 'example.com',
    prefix: '',
    scope: 'wildcard',
    domains: ['example.com', '*.example.com'],
    variableName: 'EXAMPLE_COM_TLS',
    certificatePath: '/data/certificates/cert-wildcard-example/fullchain.pem',
    privateKeyPath: '/data/certificates/cert-wildcard-example/privkey.pem',
    issuer: 'letsencrypt',
    status: 'issued',
    autoRenew: true,
    nextRenewAt: '2026-08-18T00:00:00.000Z',
    notBefore: '2026-06-20T00:00:00.000Z',
    notAfter: '2026-09-18T00:00:00.000Z',
    createdAt: '2026-06-20T00:00:00.000Z',
    updatedAt: now,
  },
  {
    id: 'cert-expired-api',
    name: 'api.expired.test',
    domainId: 'domain-example',
    domain: 'example.com',
    prefix: 'api',
    scope: 'single',
    domains: ['api.example.com'],
    variableName: 'API_TLS',
    certificatePath: '',
    privateKeyPath: '',
    issuer: 'letsencrypt',
    status: 'failed',
    lastError: 'ACME authorization expired while waiting for DNS propagation.',
    autoRenew: true,
    nextRenewAt: '2026-07-20T00:00:00.000Z',
    notBefore: '2026-04-20T00:00:00.000Z',
    notAfter: '2026-07-19T00:00:00.000Z',
    createdAt: '2026-04-20T00:00:00.000Z',
    updatedAt: now,
  },
  {
    id: 'cert-shop',
    name: 'shop.example.test',
    domainId: 'domain-shop',
    domain: 'shop.example.test',
    prefix: '',
    scope: 'single',
    domains: ['shop.example.test'],
    variableName: 'SHOP_TLS',
    certificatePath: '/data/certificates/cert-shop/fullchain.pem',
    privateKeyPath: '/data/certificates/cert-shop/privkey.pem',
    issuer: 'letsencrypt',
    status: 'issued',
    autoRenew: true,
    nextRenewAt: '2026-08-12T00:00:00.000Z',
    notBefore: '2026-06-14T00:00:00.000Z',
    notAfter: '2026-09-12T00:00:00.000Z',
    createdAt: '2026-06-14T00:00:00.000Z',
    updatedAt: now,
  },
  {
    id: 'cert-api-issuing',
    name: 'api.example.test issuing',
    domainId: 'domain-api',
    domain: 'api.example.test',
    prefix: '',
    scope: 'single',
    domains: ['api.example.test'],
    variableName: 'API_TLS',
    certificatePath: '',
    privateKeyPath: '',
    issuer: 'letsencrypt',
    status: 'issuing',
    autoRenew: true,
    nextRenewAt: '',
    notBefore: '',
    notAfter: '',
    createdAt: now,
    updatedAt: now,
  },
  {
    id: 'cert-downloads-pending',
    name: 'downloads.example.test pending',
    domainId: 'domain-downloads',
    domain: 'downloads.example.test',
    prefix: '',
    scope: 'single',
    domains: ['downloads.example.test'],
    variableName: 'DOWNLOADS_TLS',
    certificatePath: '',
    privateKeyPath: '',
    issuer: 'letsencrypt',
    status: 'pending',
    autoRenew: false,
    nextRenewAt: '',
    notBefore: '',
    notAfter: '',
    createdAt: now,
    updatedAt: now,
  },
  {
    id: 'cert-long-name',
    name: 'very-long-customer-facing-domain-name-for-layout-validation',
    domainId: 'domain-long',
    domain: 'very-long-customer-facing-domain-name-for-layout-validation.example.test',
    prefix: '',
    scope: 'single',
    domains: ['very-long-customer-facing-domain-name-for-layout-validation.example.test'],
    variableName: 'LONG_CUSTOMER_TLS',
    certificatePath: '/data/certificates/cert-long/fullchain.pem',
    privateKeyPath: '/data/certificates/cert-long/privkey.pem',
    issuer: 'letsencrypt',
    status: 'issued',
    autoRenew: true,
    nextRenewAt: '2026-07-28T00:00:00.000Z',
    notBefore: '2026-05-01T00:00:00.000Z',
    notAfter: '2026-07-30T00:00:00.000Z',
    createdAt: '2026-05-01T00:00:00.000Z',
    updatedAt: now,
  },
];

export const mockSelfSigned = [
  { id: 'self-ca-local', kind: 'ca', name: 'Local development CA', commonName: 'Panel Local CA', dnsNames: [], ipAddresses: [], fingerprint: 'SHA256:CA11', notBefore: '2026-01-01T00:00:00.000Z', notAfter: '2031-01-01T00:00:00.000Z', createdAt: '2026-01-01T00:00:00.000Z', updatedAt: now },
  { id: 'self-leaf-gateway', parentCaId: 'self-ca-local', kind: 'leaf', name: 'Gateway local leaf', commonName: 'gateway.internal', dnsNames: ['gateway.internal'], ipAddresses: ['10.0.0.10'], fingerprint: 'SHA256:1EAF', notBefore: '2026-07-01T00:00:00.000Z', notAfter: '2026-10-01T00:00:00.000Z', createdAt: '2026-07-01T00:00:00.000Z', updatedAt: now },
  { id: 'self-leaf-agent', parentCaId: 'self-ca-local', kind: 'leaf', name: 'Agent mutual TLS leaf', commonName: 'agent.internal', dnsNames: ['agent.internal', 'agent.service.consul'], ipAddresses: ['10.8.0.12', '10.22.0.41'], fingerprint: 'SHA256:A6E7', notBefore: '2026-07-10T00:00:00.000Z', notAfter: '2026-08-10T00:00:00.000Z', createdAt: '2026-07-10T00:00:00.000Z', updatedAt: now },
  { id: 'self-ca-customer', kind: 'ca', name: 'Customer staging CA', commonName: 'Customer Staging Root', dnsNames: [], ipAddresses: [], fingerprint: 'SHA256:C057', notBefore: '2026-03-01T00:00:00.000Z', notAfter: '2029-03-01T00:00:00.000Z', createdAt: '2026-03-01T00:00:00.000Z', updatedAt: now },
];

export const mockKeyAssets: KeyAssetDto[] = [
  { id: 'asset-ca-platform', type: 'ca_certificate', name: 'Platform CA', algorithm: 'ed25519', commonName: 'Panel Platform CA', dnsNames: [], ipAddresses: [], fingerprint: 'SHA256:CAFE', notBefore: '2026-01-01T00:00:00.000Z', notAfter: '2031-01-01T00:00:00.000Z', hasCertificate: true, hasPrivateKey: true, hasPublicKey: false, downloadKinds: ['certificate', 'private_key'], childCount: 1, referenceCount: 1, references: [{ resourceType: 'application', resourceId: 'app-gateway', resourceName: 'Entrance gateway', relation: 'ca' }], canReissue: false, canRegenerate: false, canDelete: false, createdAt: '2026-01-01T00:00:00.000Z', updatedAt: now },
  { id: 'asset-tls-gateway', type: 'tls_certificate', name: 'Gateway TLS', parentAssetId: 'asset-ca-platform', algorithm: 'rsa', keySize: 2048, commonName: 'gateway.internal', dnsNames: ['gateway.internal'], ipAddresses: ['10.0.0.10'], fingerprint: 'SHA256:7715', notBefore: '2026-07-01T00:00:00.000Z', notAfter: '2026-10-01T00:00:00.000Z', hasCertificate: true, hasPrivateKey: true, hasPublicKey: false, downloadKinds: ['certificate', 'private_key'], childCount: 0, referenceCount: 0, references: [], canReissue: true, canRegenerate: false, canDelete: true, createdAt: '2026-07-01T00:00:00.000Z', updatedAt: now },
  { id: 'asset-ssh-deploy', type: 'ssh_key_pair', name: 'Deploy SSH key', algorithm: 'ed25519', commonName: '', dnsNames: [], ipAddresses: [], fingerprint: 'SHA256:55H', hasCertificate: false, hasPrivateKey: true, hasPublicKey: true, downloadKinds: ['private_key', 'public_key'], childCount: 0, referenceCount: 0, references: [], canReissue: false, canRegenerate: true, canDelete: true, createdAt: '2026-07-10T00:00:00.000Z', updatedAt: now },
  { id: 'asset-ca-customer', type: 'ca_certificate', name: 'Customer staging CA', algorithm: 'rsa', keySize: 4096, commonName: 'Customer Staging Root', dnsNames: [], ipAddresses: [], fingerprint: 'SHA256:C057CA', notBefore: '2026-03-01T00:00:00.000Z', notAfter: '2029-03-01T00:00:00.000Z', hasCertificate: true, hasPrivateKey: true, hasPublicKey: false, downloadKinds: ['certificate', 'private_key'], childCount: 2, referenceCount: 0, references: [], canReissue: false, canRegenerate: false, canDelete: true, createdAt: '2026-03-01T00:00:00.000Z', updatedAt: now },
  { id: 'asset-tls-downloads', type: 'tls_certificate', name: 'Downloads TLS near expiry', parentAssetId: 'asset-ca-customer', algorithm: 'rsa', keySize: 2048, commonName: 'downloads.example.test', dnsNames: ['downloads.example.test'], ipAddresses: [], fingerprint: 'SHA256:D04D', notBefore: '2026-05-01T00:00:00.000Z', notAfter: '2026-07-29T00:00:00.000Z', hasCertificate: true, hasPrivateKey: true, hasPublicKey: false, downloadKinds: ['certificate', 'private_key'], childCount: 0, referenceCount: 1, references: [{ resourceType: 'facility', resourceId: 'reverse_proxy', resourceName: 'Entrance gateway', relation: 'static route tls' }], canReissue: true, canRegenerate: false, canDelete: false, createdAt: '2026-05-01T00:00:00.000Z', updatedAt: now },
  { id: 'asset-ssh-ci-runner', type: 'ssh_key_pair', name: 'CI runner SSH key with long rotation label', algorithm: 'rsa', keySize: 4096, commonName: '', dnsNames: [], ipAddresses: [], fingerprint: 'SHA256:C1RUN', hasCertificate: false, hasPrivateKey: true, hasPublicKey: true, downloadKinds: ['private_key', 'public_key'], childCount: 0, referenceCount: 2, references: [{ resourceType: 'server', resourceId: 'srv-api-hkg', resourceName: 'api-hkg-01', relation: 'ssh credential' }, { resourceType: 'server', resourceId: 'srv-worker-nrt', resourceName: 'worker-nrt-queue-a', relation: 'ssh credential' }], canReissue: false, canRegenerate: true, canDelete: false, createdAt: '2026-06-10T00:00:00.000Z', updatedAt: now },
];

export function createDomain(input: { name: string; provider: string }) {
  if (mockDnsDomains.some((item) => item.name === input.name)) throw new Error('domain_conflict');
  const domain = { id: `domain-${Date.now()}`, name: input.name, provider: input.provider, createdAt: now, updatedAt: now };
  mockDnsDomains.unshift(domain);
  recordsByDomain.set(domain.id, []);
  return domain;
}

export function updateDomain(id: string, input: { name: string; provider: string }) {
  const domain = mockDnsDomains.find((item) => item.id === id);
  if (!domain) return null;
  domain.name = input.name;
  domain.provider = input.provider;
  domain.updatedAt = now;
  return domain;
}

export function deleteDomain(id: string) {
  const index = mockDnsDomains.findIndex((item) => item.id === id);
  if (index < 0) return false;
  if (mockDomainCertificates.some((item) => item.domainId === id)) throw new Error('domain_in_use');
  mockDnsDomains.splice(index, 1);
  recordsByDomain.delete(id);
  return true;
}

export function listRecords(domainId: string) {
  if (domainId === 'domain-error') throw new Error('Cloudflare token rejected while reading the zone.');
  return recordsByDomain.get(domainId) ?? null;
}

export function saveRecord(domainId: string, input: DnsRecordInput, recordId?: string) {
  const list = recordsByDomain.get(domainId);
  if (!list) return null;
  if (recordId) {
    const record = list.find((item) => item.id === recordId);
    if (!record) return null;
    Object.assign(record, input, { modifiedAt: now });
    return record;
  }
  const record = { id: `rec-${Date.now()}`, ...input, modifiedAt: now };
  list.unshift(record);
  return record;
}

export function deleteRecord(domainId: string, recordId: string) {
  const list = recordsByDomain.get(domainId);
  if (!list) return false;
  const index = list.findIndex((item) => item.id === recordId);
  if (index < 0) return false;
  list.splice(index, 1);
  return true;
}

export function issueCertificate(input: IssueCertificateInput) {
  const domain = mockDnsDomains.find((item) => item.id === input.domainId);
  if (!domain) return null;
  const prefixes = input.prefixes?.length ? input.prefixes : [input.prefix ?? '@'];
  const normalizedPrefixes = prefixes.map((item) => {
    const prefix = item.trim().toLowerCase().replace(/\.$/, '');
    return prefix || '@';
  }).filter((item, index, items) => items.indexOf(item) === index);
  const coveredDomains = normalizedPrefixes.map((prefix) => prefix === '@' ? domain.name : `${prefix}.${domain.name}`);
  const domains = input.scope === 'wildcard' && !input.prefixes?.length ? [domain.name, `*.${domain.name}`] : coveredDomains;
  const cert = {
    id: `cert-${Date.now()}`,
    name: input.name,
    domainId: input.domainId,
    domain: domain.name,
    prefix: normalizedPrefixes.join(','),
    scope: input.prefixes?.length ? 'prefixes' : (input.scope ?? 'single'),
    domains,
    variableName: input.variableName,
    certificatePath: '',
    privateKeyPath: '',
    issuer: 'letsencrypt',
    status: 'issuing',
    autoRenew: true,
    nextRenewAt: '',
    notBefore: '',
    notAfter: '',
    createdAt: now,
    updatedAt: now,
  };
  mockDomainCertificates.unshift(cert);
  return { certificate: cert, taskId: `task-cert-${Date.now()}` };
}

export function renewCertificate(id: string) {
  const cert = mockDomainCertificates.find((item) => item.id === id);
  if (!cert) return false;
  cert.status = 'issuing';
  cert.lastError = '';
  cert.updatedAt = now;
  return true;
}

export function deleteCertificate(id: string) {
  const index = mockDomainCertificates.findIndex((item) => item.id === id);
  if (index < 0) return false;
  mockDomainCertificates.splice(index, 1);
  return true;
}

export function createSelfCa(input: SelfSignedCaInput) {
  const item = { id: `self-ca-${Date.now()}`, kind: 'ca', parentCaId: '', name: input.name, commonName: input.commonName, dnsNames: [], ipAddresses: [], fingerprint: `SHA256:${Date.now()}`, notBefore: now, notAfter: '2031-07-21T00:00:00.000Z', createdAt: now, updatedAt: now };
  mockSelfSigned.unshift(item);
  return item;
}

export function createSelfLeaf(input: SelfSignedLeafInput) {
  const item = { id: `self-leaf-${Date.now()}`, kind: 'leaf', parentCaId: input.caId, name: input.name, commonName: input.commonName, dnsNames: input.dnsNames, ipAddresses: input.ipAddresses, fingerprint: `SHA256:${Date.now()}`, notBefore: now, notAfter: '2026-10-21T00:00:00.000Z', createdAt: now, updatedAt: now };
  mockSelfSigned.unshift(item);
  return item;
}

export function renewSelfSigned(id: string) {
  const item = mockSelfSigned.find((cert) => cert.id === id);
  if (!item) return null;
  item.updatedAt = now;
  item.notAfter = '2027-07-21T00:00:00.000Z';
  return item;
}

export function deleteSelfSigned(id: string) {
  const index = mockSelfSigned.findIndex((item) => item.id === id);
  if (index < 0) return false;
  mockSelfSigned.splice(index, 1);
  return true;
}

export function createAsset(input: CreateCaAssetInput | CreateTlsAssetInput | GenerateSshAssetInput | ImportKeyAssetInput, type?: string) {
  const asset: KeyAssetDto = {
    id: `asset-${Date.now()}`,
    type: type || ('type' in input ? input.type : 'ssh_key_pair'),
    name: input.name,
    parentAssetId: 'parentAssetId' in input ? input.parentAssetId : undefined,
    algorithm: 'algorithm' in input ? input.algorithm : 'ed25519',
    keySize: 'keySize' in input ? input.keySize : 0,
    commonName: 'commonName' in input ? input.commonName : '',
    dnsNames: 'dnsNames' in input ? input.dnsNames : [],
    ipAddresses: 'ipAddresses' in input ? input.ipAddresses : [],
    fingerprint: `SHA256:${Date.now()}`,
    notBefore: now,
    notAfter: type === 'ssh_key_pair' ? undefined : '2027-07-21T00:00:00.000Z',
    hasCertificate: type !== 'ssh_key_pair',
    hasPrivateKey: true,
    hasPublicKey: type === 'ssh_key_pair',
    downloadKinds: type === 'ssh_key_pair' ? ['private_key', 'public_key'] : ['certificate', 'private_key'],
    childCount: 0,
    referenceCount: 0,
    references: [],
    canReissue: type === 'tls_certificate',
    canRegenerate: type === 'ssh_key_pair',
    canDelete: true,
    createdAt: now,
    updatedAt: now,
  };
  mockKeyAssets.unshift(asset);
  return { asset };
}

export function mutateAsset(id: string, operation: 'reissue' | 'regenerate') {
  const asset = mockKeyAssets.find((item) => item.id === id);
  if (!asset) return null;
  asset.updatedAt = now;
  return { asset, taskId: `task-${operation}-${Date.now()}` };
}

export function deleteAsset(id: string) {
  const index = mockKeyAssets.findIndex((item) => item.id === id);
  if (index < 0) return 'missing';
  if (!mockKeyAssets[index].canDelete) return 'conflict';
  mockKeyAssets.splice(index, 1);
  return 'deleted';
}
