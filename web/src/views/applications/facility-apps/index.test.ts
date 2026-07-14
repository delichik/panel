import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

const detailSource = readFileSync(resolve(__dirname, 'index.vue'), 'utf8');
const configSource = readFileSync(resolve(__dirname, 'config.vue'), 'utf8');

describe('facility apps page', () => {
  it('keeps the facility detail read-only and links to the dedicated configuration page', () => {
    expect(detailSource).toContain('<AppDetailPanel>');
    expect(detailSource).toContain("router.push('/applications/facility-apps/reverse-proxy/config')");
    expect(detailSource).not.toContain('<v-text-field');
    expect(detailSource).not.toContain('<v-select');
    expect(detailSource).not.toContain('<v-switch');
    expect(detailSource).not.toContain('saveReverseProxy(');
  });

  it('keeps static domain group keys independent from the editable domain value', () => {
    expect(configSource).toContain('const byGroupId = new Map<string, number>();');
    expect(configSource).toContain('const key = site.localGroupId;');
    expect(configSource).not.toContain('const key = domain || site.localGroupId;');
  });

  it('uses an explicit baseline and validates domain gateway nodes before opening a save session', () => {
    expect(configSource).toContain("baselinePayload.value = JSON.stringify(savePayload())");
    expect(configSource).toContain('const validationError = validateSavePayload(payload);');
    expect(configSource.indexOf('const validationError = validateSavePayload(payload);')).toBeLessThan(configSource.indexOf('facilityAppsApi.beginSaveSession'));
    expect(configSource).toContain(':items="gatewayServerOptions"');
    expect(configSource).toContain("if (!policy.entryServerIds.length) return t('facilityAppsPage.selectDomainGatewayNodes')");
  });

  it('keeps path dialog edits isolated until save', () => {
    expect(configSource).toContain('routeDialog.draft = structuredClone(form.staticSites[index]);');
    expect(configSource).toContain('routeDialog.draft = structuredClone(route);');
    expect(configSource).toContain('const draft = structuredClone(routeDialog.draft);');
    expect(configSource).toContain('routeDialog.draft = null;');
    expect(configSource).toContain('<RoutePathAdvancedFields');
  });

  it('commits configuration and staged assets through one save session', () => {
    expect(configSource).toContain('facilityAppsApi.beginSaveSession');
    expect(configSource).toContain('facilityAppsApi.deleteSaveSessionAsset');
    expect(configSource).toContain('facilityAppsApi.uploadSaveSessionAsset');
    expect(configSource).toContain('facilityAppsApi.commitSaveSession');
    expect(configSource).toContain('facilityAppsApi.discardSaveSession');
    expect(configSource).toContain('v-overlay :model-value="saving" contained persistent');
  });
});
