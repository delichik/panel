import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const page = readFileSync(resolve(__dirname, 'pages/ApplicationsPage.vue'), 'utf8');

describe('ApplicationsPage', () => {
  it('presents the operational Applications workspace', () => {
    expect(page).toContain("t('applicationsPage.applications')");
    expect(page).toContain('applicationsApi.list');
    expect(page).toContain('ApplicationEditor');
    expect(page).toContain('ApplicationDetail');
    expect(page).toContain('mdi-plus');
    expect(page).toContain("t('applicationsPage.deleteApplication')");
  });
});
