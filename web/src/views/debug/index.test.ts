import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

const page = readFileSync(resolve(__dirname, 'index.vue'), 'utf8');

describe('DebugPage', () => {
  it('loads, refreshes, pauses, and preserves the latest snapshot', () => {
    expect(page).toContain('diagnosticsApi.snapshot');
    expect(page).toContain('refreshIntervalMs = 5000');
    expect(page).toContain('function togglePaused');
    expect(page).toContain('if (refreshing.value) return');
    expect(page).not.toContain('snapshot.value = null');
  });

  it('uses the existing full-height page and card patterns', () => {
    expect(page).toContain('page-shell debug-page');
    expect(page).toContain('page-summary-grid');
    expect(page).toContain('variant="outlined"');
    expect(page).toContain('debug-scroll');
  });

  it('shows task worker and registry diagnostics', () => {
    expect(page).toContain('snapshot.tasks.workerRunning');
    expect(page).toContain('snapshot.value.tasks.registeredTypes');
    expect(page).toContain("t('debugPage.taskRuntime')");
  });
});
