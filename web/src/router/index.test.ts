import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const routerSource = readFileSync(resolve(__dirname, 'index.ts'), 'utf8');

describe('router key asset routes', () => {
  it('exposes the new key assets page and redirects the legacy self-signed path', () => {
    expect(routerSource).toContain("import KeyAssetsPage from '@/views/certificates/key-assets/index.vue'");
    expect(routerSource).toContain("path: 'certificates/key-assets'");
    expect(routerSource).toContain("name: 'certificates-key-assets'");
    expect(routerSource).toContain("component: KeyAssetsPage");
    expect(routerSource).toContain("path: 'certificates/self-signed', redirect: '/certificates/key-assets'");
  });
});
