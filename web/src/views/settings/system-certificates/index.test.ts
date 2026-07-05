import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const page = readFileSync(resolve(__dirname, 'index.vue'), 'utf8');

describe('SettingsSystemCertificatesPage', () => {
  it('uses selector-detail mode for read-only system certificates and reset', () => {
    expect(page).toContain('<AppSelectorPanel');
    expect(page).toContain('<AppSelectorSummaryItem');
    expect(page).toContain('<AppMasterDetailWorkspace');
    expect(page).toContain('keyAssetsApi.listSystemCertificates');
    expect(page).toContain('keyAssetsApi.resetSystemCertificate');
    expect(page).not.toContain('v-checkbox-btn');
    expect(page).not.toContain('downloadAssetFile');
  });
});
