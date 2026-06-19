import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';

describe('debug diagnostics page', () => {
  const page = readFileSync(new URL('./index.vue', import.meta.url), 'utf8');

  it('separates runtime, task, and database diagnostics with tabs', () => {
    expect(page).toContain('<v-tab value="runtime">');
    expect(page).toContain('<v-tab value="tasks">');
    expect(page).toContain('<v-tab value="databases">');
    expect(page).toContain('v-model="activeDatabase"');
  });

  it('shows task definitions and current per-table allocation details', () => {
    expect(page).toContain('snapshot.tasks.definitions');
    expect(page).toContain('table.dataSizeBytes');
    expect(page).toContain('table.indexSizeBytes');
    expect(page).toContain('table.totalSizeBytes');
    expect(page).toContain('table.databasePercent');
  });
});
