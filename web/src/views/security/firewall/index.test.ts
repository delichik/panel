import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const page = readFileSync(resolve(__dirname, 'index.vue'), 'utf8');

describe('FirewallPage UFW enable flow', () => {
  it('offers one confirmed enable action for missing or inactive UFW', () => {
    expect(page).toContain('v-if="state && !state.active"');
    expect(page).toContain("t('firewallPage.ufwMissingEnableHint')");
    expect(page).toContain("t('firewallPage.ufwInactiveHint')");
    expect(page).toContain('serversApi.enableUFW(serverId.value)');
    expect(page).toContain('v-model="enableDialog"');
    expect(page).not.toContain('serversApi.installUFW(serverId.value)');
  });
});
