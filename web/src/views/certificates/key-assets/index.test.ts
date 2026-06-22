import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const page = readFileSync(resolve(__dirname, 'index.vue'), 'utf8');
const domainCertificatesPage = readFileSync(resolve(__dirname, '../domains/index.vue'), 'utf8');

describe('KeyAssetsPage', () => {
  it('uses the shared selector-detail workspace and keeps private keys out of inline display', () => {
    expect(page).toContain('class="asset-workspace"');
    expect(page).toContain('<AppSelectorPanel');
    expect(page).toContain('<AppSelectorItem');
    expect(page).toContain('class="asset-detail-card"');
    expect(page).toContain('icon="mdi-dots-vertical"');
    expect(page).toContain(":title=\"t('keyAssetsPage.importAsset')\"");
    expect(page).toContain(":title=\"t('keyAssetsPage.importArchive')\"");
    expect(page).toContain(":title=\"t('keyAssetsPage.exportSelected')\"");
    expect(page).toContain('<template #leading>');
    expect(page).toContain(':indeterminate="someSelectableSelected"');
    expect(page).toContain('@update:model-value="toggleSelectAllAssets"');
    expect(page).toContain('class="asset-selector-checkbox"');
    expect(page).toContain('margin-left: -6px');
    expect(page).toContain("t('keyAssetsPage.tabs.ssh')");
    expect(page).not.toContain("t('keyAssetsPage.privateKeysHiddenHint')");
    expect(page).toContain("keyAssetsApi.preflightImportArchive");
    expect(page).toContain("keyAssetsApi.exportSelected");
    expect(page).toContain("keyAssetsApi.reissue");
    expect(page).toContain("keyAssetsApi.regenerate");
    expect(page).not.toContain("keyAssetsApi.listSystemCertificates");
    expect(page).not.toContain("keyAssetsApi.resetSystemCertificate");
    expect(page).not.toContain('systemCertificates');
    expect(page).not.toContain('system-certificates-card');
    expect(page).not.toContain('class="table-card"');
    expect(page).not.toContain('privateKeyCiphertext');
  });

  it('uses selector-detail mode for domain certificates too', () => {
    expect(domainCertificatesPage).toContain('class="certificate-workspace"');
    expect(domainCertificatesPage).toContain('<AppSelectorPanel');
    expect(domainCertificatesPage).toContain('<AppSelectorItem');
    expect(domainCertificatesPage).toContain('class="certificate-detail"');
  });

  it('supports SSH algorithm selection and archive conflict handling', () => {
    expect(page).toContain("sshForm.algorithm === 'rsa'");
    expect(page).toContain("[2048, 3072, 4096]");
    expect(page).toContain("archiveStrategy.value === 'overwrite_existing'");
    expect(page).toContain("archiveDangerAcknowledged");
    expect(page).toContain("t('keyAssetsPage.targetAsset')");
  });
});
