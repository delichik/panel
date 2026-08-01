import { describe, expect, it } from 'vitest';
import {
  applyYamlToDraft,
  cloneFacilityDomains,
  diffFacility,
  draftFromApplication,
  facilityDraftFromConfig,
  facilitySaveInputFromDraft,
  facilitySaveInputsEqual,
  makeFacilityDomain,
  makeKeyValueRow,
  saveInputFromDraft,
  specYamlFromDraft,
  syncDraftToYaml,
  validateApplicationDraft,
  validateFacilityDraft,
} from './model';
import type { ApplicationDto } from '@/types/applications';
import type { ReverseProxyConfig } from '@/types/facilityApps';

const app = {
  id: 'app-1',
  version: 1,
  kind: 'application',
  name: 'api',
  enabled: true,
  specYaml: 'name: api\nimage: nginx\nports:\n  - label: http\n    to: 8080\n',
  variables: { NODE_ENV: 'production' },
  deploymentMode: 'selected',
  deploymentServers: ['srv-1'],
  reverseProxy: [{ domain: 'api.example.test', targetPort: 8080, originServerIds: ['srv-1'], anyAccess: { enabled: false }, paths: [{ path: '/' }] }],
  generation: 1,
  specHash: 'hash',
  imageUpdateAvailable: false,
  jobId: 'job',
  namespace: 'default',
  createdAt: '',
  updatedAt: '',
} satisfies ApplicationDto;

describe('application editor model', () => {
  it('builds save input from structured editor draft without storing display text', () => {
    const draft = draftFromApplication(app);
    draft.env.push(makeKeyValueRow('PORT', '8080'));

    const input = saveInputFromDraft(draft);

    expect(input.name).toBe('api');
    expect(input.variables.NODE_ENV).toBe('production');
    expect(input.deploymentServers).toEqual(['srv-1']);
    expect(input.reverseProxy[0].domain).toBe('api.example.test');
    expect(input.specYaml).toContain('PORT: "8080"');
  });

  it('syncs YAML from form and applies YAML back to structured sections', () => {
    const draft = draftFromApplication(app);
    draft.image = 'ghcr.io/example/api:2';
    syncDraftToYaml(draft);

    expect(draft.specYaml).toContain('ghcr.io/example/api:2');

    draft.specYaml = 'name: api\nimage: redis:7\nnetworkMode: host\nports:\n  - label: tcp\n    to: 6379\n';
    draft.yamlDirty = true;

    expect(applyYamlToDraft(draft).ok).toBe(true);
    expect(draft.image).toBe('redis:7');
    expect(draft.networkMode).toBe('host');
    expect(draft.ports[0].to).toBe('6379');
  });

  it('keeps dialog drafts independent until saved', () => {
    const domain = makeFacilityDomain();
    domain.domain = 'static.example.test';
    const draftCopy = cloneFacilityDomains([domain])[0];
    draftCopy.domain = 'changed.example.test';

    expect(domain.domain).toBe('static.example.test');
    expect(draftCopy.domain).toBe('changed.example.test');
  });

  it('validates application sections before preview and commit', () => {
    const draft = draftFromApplication();
    draft.name = '';
    draft.image = '';
    draft.deploymentMode = 'selected';
    draft.deploymentServers = [];

    const errors = validateApplicationDraft(draft);

    expect(errors.name).toBe('applicationsPage.validationName');
    expect(errors.image).toBe('applicationsPage.validationImage');
    expect(errors.deploymentServers).toBe('applicationsPage.validationDeploymentServers');
  });

  it('serializes facility config and reports preview diff', () => {
    const base = {
      id: 'reverse_proxy',
      version: 1,
      deploymentServers: ['srv-1'],
      panelEntry: { enabled: false },
      domains: [],
      staticAssets: [],
      routeSummaries: [],
      applicationRoutes: [],
      updatedAt: '',
      routes: 0,
      enabledServers: ['srv-1'],
    } satisfies ReverseProxyConfig;
    const draft = facilityDraftFromConfig(base);
    draft.panelEnabled = true;
    draft.panelServerId = 'srv-1';
    draft.panelDomain = 'panel.example.test';
    draft.domains.push({ domain: 'static.example.test', originServerIds: ['srv-1'], anyAccess: { enabled: false }, paths: [{ path: '/', ruleType: 'static', sourceType: 'host_path', rootPath: '/srv/www' }] });

    expect(validateFacilityDraft(draft)).toEqual({});
    expect(facilitySaveInputFromDraft(draft).panelEntry.domain).toBe('panel.example.test');
    expect(diffFacility(base, draft)).toMatchObject({ added: 1, changed: 2 });
  });

  it('keeps generated YAML parseable for changed runtime fields', () => {
    const draft = draftFromApplication(app);
    draft.mounts.push({ id: 'm1', type: 'persistent', source: '', target: '/data', readOnly: false, mode: '0755' });

    expect(specYamlFromDraft(draft)).toContain('mounts:');
  });
});


describe('facilitySaveInputsEqual', () => {
  it('compares deployment servers, panel entry, and domains', () => {
    const base = {
      id: 'reverse_proxy',
      version: 1,
      deploymentServers: ['srv-1'],
      panelEntry: { enabled: true, serverId: 'srv-1', domain: 'panel.example.test' },
      domains: [{ domain: 'static.example.test', originServerIds: ['srv-1'], anyAccess: { enabled: false }, paths: [{ path: '/', ruleType: 'static', sourceType: 'host_path', rootPath: '/srv/www' }] }],
      staticAssets: [],
      routeSummaries: [],
      applicationRoutes: [],
      updatedAt: '',
      routes: 0,
      enabledServers: ['srv-1'],
    } satisfies ReverseProxyConfig;
    const draft = facilitySaveInputFromDraft(facilityDraftFromConfig(base));
    expect(facilitySaveInputsEqual(draft, facilitySaveInputFromDraft(facilityDraftFromConfig(base)))).toBe(true);
    expect(facilitySaveInputsEqual(draft, { ...draft, deploymentServers: ['srv-2'] })).toBe(false);
    expect(facilitySaveInputsEqual(draft, { ...draft, domains: [] })).toBe(false);
    expect(facilitySaveInputsEqual(draft, { ...draft, panelEntry: { enabled: false } })).toBe(false);
  });
});