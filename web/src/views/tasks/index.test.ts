import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

const page = readFileSync(resolve(__dirname, 'index.vue'), 'utf8');

describe('TaskCenterPage', () => {
  it('uses task definition capabilities returned by the API', () => {
    expect(page).toContain('task.allowRunNow');
    expect(page).toContain('task.allowRetry');
    expect(page).not.toContain('runnableTaskTypes');
  });
});
