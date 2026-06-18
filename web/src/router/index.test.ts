import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const routerSource = readFileSync(resolve(__dirname, 'index.ts'), 'utf8');

describe('router key asset routes', () => {
  it('exposes the new key assets page and redirects the legacy self-signed path', () => {
    expect(routerSource).toContain("import SelfSignedCertificatesPage from '@/views/certificates/self-signed/index.vue'");
    expect(routerSource).toContain("import KeysPage from '@/views/certificates/keys/index.vue'");
    expect(routerSource).toContain("path: 'certificates/self-signed'");
    expect(routerSource).toContain("name: 'certificates-self-signed'");
    expect(routerSource).toContain("path: 'certificates/keys'");
    expect(routerSource).toContain("name: 'certificates-keys'");
    expect(routerSource).toContain("path: 'certificates/key-assets', redirect: '/certificates/self-signed'");
  });
});

describe('hidden debug route', () => {
  it('registers the authenticated page without adding it to navigation', () => {
    expect(routerSource).toContain("import DebugPage from '@/views/debug/index.vue'");
    expect(routerSource).toContain("path: 'debug', name: 'debug'");
    const layoutSource = readFileSync(resolve(__dirname, '../layouts/AppLayout.vue'), 'utf8');
    expect(layoutSource).not.toContain("to: '/debug'");
  });
});
