import { describe, expect, it, vi } from 'vitest';
import {
  assetTone,
  certificateState,
  certificateTone,
  validateIssueCertificateForm,
  validateKeyAssetForm,
  validateSelfSignedForm,
} from './model';
import type { DomainCertificateDto } from '@/types/certificates';
import type { KeyAssetDto } from '@/types/keyAssets';

const baseCert: DomainCertificateDto = {
  id: 'cert-1',
  name: 'cert',
  domainId: 'domain-1',
  domain: 'example.com',
  prefix: '',
  scope: 'single',
  domains: ['example.com'],
  certificatePath: '',
  privateKeyPath: '',
  issuer: 'letsencrypt',
  status: 'issued',
  autoRenew: true,
  createdAt: '2026-07-01T00:00:00.000Z',
  updatedAt: '2026-07-01T00:00:00.000Z',
};

const baseAsset: KeyAssetDto = {
  id: 'asset-1',
  type: 'tls_certificate',
  name: 'asset',
  algorithm: 'ed25519',
  dnsNames: [],
  ipAddresses: [],
  fingerprint: 'SHA256:test',
  hasCertificate: true,
  hasPrivateKey: true,
  hasPublicKey: false,
  downloadKinds: [],
  childCount: 0,
  referenceCount: 0,
  references: [],
  canReissue: true,
  canRegenerate: false,
  canDelete: true,
  createdAt: '2026-07-01T00:00:00.000Z',
  updatedAt: '2026-07-01T00:00:00.000Z',
};

describe('certificate page model', () => {
  it('marks failed certificates as dangerous before date checks', () => {
    const cert = { ...baseCert, status: 'failed', notAfter: '2027-01-01T00:00:00.000Z' };
    expect(certificateTone(cert)).toBe('danger');
    expect(certificateState(cert)).toBe('certificatesPage.statusFailed');
  });

  it('separates expired and expiring certificates', () => {
    vi.setSystemTime(new Date('2026-07-21T00:00:00.000Z'));
    expect(certificateState({ ...baseCert, notAfter: '2026-07-20T00:00:00.000Z' })).toBe('certificatesPage.statusExpired');
    expect(certificateTone({ ...baseCert, notAfter: '2026-07-30T00:00:00.000Z' })).toBe('warning');
    vi.useRealTimers();
  });

  it('keeps referenced key assets in warning state', () => {
    expect(assetTone({ ...baseAsset, canDelete: false, referenceCount: 1 })).toBe('warning');
  });

  it('validates issue, self-signed, and key asset required fields locally', () => {
    expect(validateIssueCertificateForm({ name: '', domainId: '', coverage: [] })).toEqual({
      name: 'certificatesPage.validationNameRequired',
      domainId: 'certificatesPage.validationDomainRequired',
      coverage: 'certificatesPage.validationCoverageRequired',
    });
    expect(validateIssueCertificateForm({ name: 'web', domainId: 'd1', coverage: ['example.com'] })).toEqual({});
    expect(validateSelfSignedForm({ name: '', commonName: '', requireCa: true, caId: '' })).toEqual({
      name: 'certificatesPage.validationNameRequired',
      commonName: 'certificatesPage.validationCommonNameRequired',
      caId: 'certificatesPage.validationCaRequired',
    });
    expect(validateKeyAssetForm({ name: 'ssh', requireCommonName: false })).toEqual({});
    expect(validateKeyAssetForm({ name: '', commonName: '', requireCommonName: true, requireParent: true, parentAssetId: '' })).toEqual({
      name: 'certificatesPage.validationNameRequired',
      commonName: 'certificatesPage.validationCommonNameRequired',
      parentAssetId: 'certificatesPage.validationCaRequired',
    });
  });
});
