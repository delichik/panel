import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const page = readFileSync(resolve(__dirname, 'index.vue'), 'utf8');

describe('KeyAssetsPage', () => {
  it('implements the tabbed key asset workspace and keeps private keys out of inline display', () => {
    expect(page).toContain("v-tabs v-model=\"activeTab\"");
    expect(page).toContain("t('keyAssetsPage.tabs.ca')");
    expect(page).toContain("t('keyAssetsPage.tabs.tls')");
    expect(page).toContain("t('keyAssetsPage.tabs.ssh')");
    expect(page).toContain("t('keyAssetsPage.privateKeysHiddenHint')");
    expect(page).toContain("keyAssetsApi.preflightImportArchive");
    expect(page).toContain("keyAssetsApi.exportSelected");
    expect(page).toContain("keyAssetsApi.reissue");
    expect(page).toContain("keyAssetsApi.regenerate");
    expect(page).toContain("keyAssetsApi.listSystemCertificates");
    expect(page).toContain("keyAssetsApi.resetSystemCertificate");
    expect(page).toContain("t('keyAssetsPage.systemBuiltIn')");
    expect(page).toContain("t('keyAssetsPage.userDomain')");
    expect(page).not.toContain('privateKeyCiphertext');
  });

  it('supports SSH algorithm selection and archive conflict handling', () => {
    expect(page).toContain("sshForm.algorithm === 'rsa'");
    expect(page).toContain("[2048, 3072, 4096]");
    expect(page).toContain("archiveStrategy.value === 'overwrite_existing'");
    expect(page).toContain("archiveDangerAcknowledged");
    expect(page).toContain("t('keyAssetsPage.targetAsset')");
  });
});
