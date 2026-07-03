import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const page = readFileSync(resolve(__dirname, 'index.vue'), 'utf8');

describe('ApplicationsPage', () => {
  it('presents the operational Applications workspace', () => {
    expect(page).toContain("t('applicationsPage.applications')");
    expect(page).toContain('applicationsApi.list');
    expect(page).not.toContain('ApplicationEditor');
    expect(page).toContain('ApplicationDetail');
    expect(page).toContain('<AppSelectorPanel');
    expect(page).toContain('<AppSelectorItem');
    expect(page).toContain('grid-template-columns: clamp(300px, 26vw, 340px) minmax(0, 1fr)');
    expect(page).toContain('<template #actions>');
    expect(page).not.toContain('class="summary-strip"');
    expect(page).not.toContain('enabledCount');
    expect(page).not.toContain('attentionCount');
    expect(page).toContain('mdi-plus');
    expect(page).toContain("router.push('/applications/apps/create')");
    expect(page).toContain("router.push(`/applications/apps/${encodeURIComponent(app.id)}/edit`)");
    expect(page).toContain("t('applicationsPage.deleteApplication')");
    expect(page).toContain("runtimeStatus(app) === 'running' && app.imageUpdateAvailable");
    expect(page).toContain("t('applicationsPage.runningWithUpdate')");
    expect(page).toContain(':color="selectorStatusColor(app)"');
  });
});
