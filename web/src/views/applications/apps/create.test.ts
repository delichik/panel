import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const page = readFileSync(resolve(__dirname, 'create.vue'), 'utf8');

describe('Application editor page', () => {
  it('serves both create and edit routes with the embedded editor', () => {
    expect(page).toContain('const applicationId = computed');
    expect(page).toContain('applicationsApi.get(id)');
    expect(page).toContain(':application="application"');
    expect(page).toContain('embedded');
    expect(page).toContain("application?.id || 'create'");
    expect(page).toContain('returnToApplications(application?.id)');
  });
});
