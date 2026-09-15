import { describe, expect, it } from 'vitest';
import { assetImportHasCertificate, initialAssetImportMaterialTab } from './assetImportEditor';

describe('asset import editor state', () => {
  it('opens on the private key material', () => {
    expect(initialAssetImportMaterialTab()).toBe('privateKey');
  });

  it('shows certificate material only for CA and TLS imports', () => {
    expect(assetImportHasCertificate('ssh_key_pair')).toBe(false);
    expect(assetImportHasCertificate('ca_certificate')).toBe(true);
    expect(assetImportHasCertificate('tls_certificate')).toBe(true);
  });
});
