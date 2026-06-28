import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const detail = readFileSync(resolve(__dirname, 'ApplicationDetail.vue'), 'utf8');
const runtime = readFileSync(resolve(__dirname, 'ApplicationRuntimePanel.vue'), 'utf8');

describe('ApplicationDetail', () => {
  it('uses one unified detail card with embedded runtime content', () => {
    expect(detail).toContain('class="application-detail"');
    expect(detail).toContain('class="detail-header"');
    expect(detail).toContain('class="detail-body"');
    expect(detail).toContain('<ApplicationRuntimePanel embedded');
    expect(detail).toContain('icon="mdi-dots-vertical"');
    expect(detail).toContain('<slot name="more-actions" />');
    expect(detail).toContain('class="detail-statuses"');
    expect(detail).not.toContain('class="detail-stack"');
    expect(runtime).toContain("'runtime-card--embedded': embedded");
    expect(runtime).toContain('instance.serverName || instance.serverId');
  });
});
