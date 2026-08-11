import { describe, expect, it } from 'vitest';
import {
  applicationDraftWarnings,
  applicationStatus,
  applyYamlToDraft,
  cloneFacilityDomains,
  cloneProxyRules,
  diffApplications,
  diffFacility,
  draftFromApplication,
  facilityDraftFromConfig,
  facilitySaveInputFromDraft,
  makeFacilityDomain,
  makeFacilityPath,
  makeKeyValueRow,
  saveInputFromDraft,
  specYamlFromDraft,
  statusTone,
  syncDraftToYaml,
  validateApplicationDraft,
  validateFileName,
  validateFacilityDraft,
  validateFacilityDomainFields,
  validateFacilityPathFields,
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
  it('marks stopped reconciliation as needing attention', () => {
    expect(applicationStatus({ ...app, reconcileStopped: true })).toBe('attention');
    expect(statusTone('attention')).toBe('warning');
    expect(applicationStatus({ ...app, reconcileStopped: false })).toBe('enabled');
  });

  it('builds save input from structured editor draft without storing display text', () => {
    const draft = draftFromApplication(app);
    draft.env.push(makeKeyValueRow('PORT', '8080'));

    const input = saveInputFromDraft(draft);

    expect(input.name).toBe('api');
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

  it('reports no pending changes for a freshly opened editor', () => {
    const draft = draftFromApplication(app);
    expect(diffApplications(app, draft)).toEqual({ added: 0, changed: 0, removed: 0, warnings: 0 });
  });

  it('ignores YAML formatting differences when comparing the saved app', () => {
    const formatted = { ...app, specYaml: 'name: api\n\nimage: nginx\n\nports:\n  - label: http\n    to: 8080\n\n' };
    const draft = draftFromApplication(formatted);
    expect(diffApplications(formatted, draft)).toEqual({ added: 0, changed: 0, removed: 0, warnings: 0 });
  });

  it('reports a change when a route option is edited', () => {
    const draft = draftFromApplication(app);
    draft.reverseProxy[0].paths[0].options!.gzipMode = 'off';
    expect(diffApplications(app, draft).changed).toBe(1);
  });

  it('reports a change when a deployment server is removed', () => {
    const draft = draftFromApplication(app);
    draft.deploymentServers = [];
    expect(diffApplications(app, draft).changed).toBe(1);
  });

  it('preserves uncovered spec fields like capAdd through structured edits', () => {
    const withCap = {
      ...app,
      specYaml: 'name: api\nimage: nginx\ncapAdd:\n  - NET_ADMIN\n  - SYS_PTRACE\nresources:\n  memoryMb: 256\n  limits:\n    cpus: "1.5"\n',
    };
    const draft = draftFromApplication(withCap);
    draft.image = 'nginx:2';

    const yaml = specYamlFromDraft(draft);
    expect(yaml).toContain('NET_ADMIN');
    expect(yaml).toContain('SYS_PTRACE');
    expect(yaml).toContain('memoryMb: 256');
    expect(yaml).toContain('cpus:');
    expect(yaml).toContain('1.5');
    expect(yaml).toContain('image: nginx:2');

    const input = saveInputFromDraft(draft);
    expect(input.specYaml).toContain('NET_ADMIN');
    expect(input.specYaml).toContain('SYS_PTRACE');
  });

  it('reports no pending changes when uncovered spec fields are preserved', () => {
    const withCap = { ...app, specYaml: 'name: api\nimage: nginx\ncapAdd:\n  - NET_ADMIN\n' };
    const draft = draftFromApplication(withCap);
    expect(diffApplications(withCap, draft)).toEqual({ added: 0, changed: 0, removed: 0, warnings: 0 });
  });

  it('reports staged YAML as a warning instead of a blocking error', () => {
    const draft = draftFromApplication(app);
    draft.specYaml = 'name: api\nimage: redis:7\n';
    draft.yamlDirty = true;

    expect(validateApplicationDraft(draft)).toEqual({});
    expect(applicationDraftWarnings(draft)).toEqual({ specYaml: 'applicationsPage.validationSourceStaged' });
  });

  it('rejects non-numeric CPU and memory values', () => {
    const draft = draftFromApplication(app);
    draft.cpu = 'fast';
    draft.memoryMb = 'lots';
    const errors = validateApplicationDraft(draft);
    expect(errors.cpu).toBe('applicationsPage.validationNumber');
    expect(errors.memoryMb).toBe('applicationsPage.validationNumber');
  });

  it('validates application file names like the backend', () => {
    expect(validateFileName('config.yaml')).toBeNull();
    expect(validateFileName('../secret')).toBe('applicationsPage.validationFileName');
    expect(validateFileName('a/b')).toBe('applicationsPage.validationFileName');
    expect(validateFileName('a\\b')).toBe('applicationsPage.validationFileName');
    expect(validateFileName('bad\u0000name')).toBe('applicationsPage.validationFileName');
    expect(validateFileName('')).toBe('applicationsPage.validationFileName');
  });

  it('starts create drafts blank without sample defaults', () => {
    const draft = draftFromApplication();
    expect(draft.name).toBe('');
    expect(draft.image).toBe('');
    expect(draft.specYaml).toBe('');
    expect(draft.ports).toEqual([]);
    expect(draft.commandRows).toEqual([]);
    expect(draft.env).toEqual([]);
    expect(draft.mounts).toEqual([]);
    expect(draft.reverseProxy).toEqual([]);

    const yaml = specYamlFromDraft(draft);
    expect(yaml).not.toContain('nginx');
    expect(yaml).not.toContain('name: web');
  });

  it('keeps dialog drafts independent until saved', () => {
    const domain = makeFacilityDomain();
    domain.domain = 'static.example.test';
    const draftCopy = cloneFacilityDomains([domain])[0];
    draftCopy.domain = 'changed.example.test';

    expect(domain.domain).toBe('static.example.test');
    expect(draftCopy.domain).toBe('changed.example.test');
  });

  it('preserves AnyAccess relay servers when cloning rules and domains', () => {
    const rule = cloneProxyRules([{ domain: 'api.example.test', targetPort: 8080, originServerIds: ['srv-1'], anyAccess: { enabled: true, strategy: 'round_robin', relayServerIds: ['srv-2'] }, paths: [{ path: '/' }] }])[0];
    expect(rule.anyAccess.relayServerIds).toEqual(['srv-2']);
    const domain = cloneFacilityDomains([{ domain: 'example.test', originServerIds: ['srv-1'], anyAccess: { enabled: true, strategy: 'round_robin', relayServerIds: ['srv-2'] }, paths: [] }])[0];
    expect(domain.anyAccess.relayServerIds).toEqual(['srv-2']);
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

  it('defaults the panel server to the registered host node', () => {
    const base = {
      id: 'reverse_proxy',
      version: 1,
      deploymentServers: ['srv-host'],
      panelEntry: { enabled: true, serverId: 'srv-host', domain: 'panel.example.test' },
      panelHostServerId: 'srv-host',
      domains: [],
      staticAssets: [],
      routeSummaries: [],
      applicationRoutes: [],
      updatedAt: '',
      routes: 1,
      enabledServers: ['srv-host'],
    } satisfies ReverseProxyConfig;
    const draft = facilityDraftFromConfig(base);
    expect(draft.panelServerId).toBe('srv-host');
    expect(draft.panelHostServerId).toBe('srv-host');
    expect(draft.panelDomain).toBe('panel.example.test');
  });

  it('keeps generated YAML parseable for changed runtime fields', () => {
    const draft = draftFromApplication(app);
    draft.mounts.push({ id: 'm1', type: 'persistent', source: '', target: '/data', readOnly: false, mode: '0755' });

    expect(specYamlFromDraft(draft)).toContain('mounts:');
  });

  it('forces local proxy targets for host-network applications', () => {
    const hostApp: ApplicationDto = {
      ...app,
      specYaml: 'name: api\nimage: nginx\nnetworkMode: host\n',
      reverseProxy: [{ domain: 'api.example.test', targetType: 'container', targetPort: 8080, originServerIds: ['srv-1'], anyAccess: { enabled: false }, paths: [{ path: '/' }] }],
    };
    const draft = draftFromApplication(hostApp);

    expect(draft.networkMode).toBe('host');
    expect(draft.reverseProxy[0].targetType).toBe('local');

    draft.reverseProxy[0].targetType = 'container';
    expect(saveInputFromDraft(draft).reverseProxy[0].targetType).toBe('local');
  });

  it('keeps container targets for bridge-network applications', () => {
    const draft = draftFromApplication(app);
    expect(draft.networkMode).toBe('bridge');

    draft.reverseProxy[0].targetType = 'container';
    expect(saveInputFromDraft(draft).reverseProxy[0].targetType).toBe('container');
  });
});
describe('facility path dialog validation', () => {
  it('accepts a clean redirect target', () => {
    const errors = validateFacilityPathFields({ path: '/go', ruleType: 'redirect', sourceType: 'host_path', redirectUrl: 'https://example.com/target?q=1&r=2' });
    expect(errors).toEqual({});
  });

  it('rejects an empty redirect target', () => {
    const errors = validateFacilityPathFields({ path: '/go', ruleType: 'redirect', sourceType: 'host_path', redirectUrl: '' });
    expect(errors.redirectUrl).toBe('applicationsPage.validationRedirectUrl');
  });

  it('rejects a redirect target with spaces or special characters', () => {
    const spaced = validateFacilityPathFields({ path: '/go', ruleType: 'redirect', sourceType: 'host_path', redirectUrl: 'https://example.com/a b' });
    expect(spaced.redirectUrl).toBe('applicationsPage.validationRedirectUrl');
    const semi = validateFacilityPathFields({ path: '/go', ruleType: 'redirect', sourceType: 'host_path', redirectUrl: 'https://example.com/a;b' });
    expect(semi.redirectUrl).toBe('applicationsPage.validationRedirectUrl');
    const fragment = validateFacilityPathFields({ path: '/go', ruleType: 'redirect', sourceType: 'host_path', redirectUrl: 'https://example.com/a#b' });
    expect(fragment.redirectUrl).toBe('applicationsPage.validationRedirectUrl');
  });

  it('rejects a proxy target without http(s) scheme', () => {
    const errors = validateFacilityPathFields({ path: '/p', ruleType: 'proxy_pass', sourceType: 'host_path', proxyUrl: '127.0.0.1:9000' });
    expect(errors.proxyUrl).toBe('applicationsPage.validationProxyUrl');
  });

  it('accepts an http proxy target', () => {
    const errors = validateFacilityPathFields({ path: '/p', ruleType: 'proxy_pass', sourceType: 'host_path', proxyUrl: 'http://127.0.0.1:9000' });
    expect(errors).toEqual({});
  });

  it('rejects a path that does not start with a slash', () => {
    const errors = validateFacilityPathFields({ path: 'go', ruleType: 'static', sourceType: 'host_path', rootPath: '/srv/www' });
    expect(errors.path).toBe('applicationsPage.validationPath');
  });

  it('rejects an empty path', () => {
    const errors = validateFacilityPathFields({ path: '', ruleType: 'static', sourceType: 'host_path', rootPath: '/srv/www' });
    expect(errors.path).toBe('applicationsPage.validationPath');
  });

  it('defaults new gateway paths to uploaded static files', () => {
    const path = makeFacilityPath();
    expect(path.ruleType).toBe('static');
    expect(path.sourceType).toBe('uploaded_file');
  });

  it('normalizes legacy gateway proxy and host-directory paths', () => {
    const domain = cloneFacilityDomains([{
      domain: 'example.test',
      originServerIds: ['srv-1'],
      anyAccess: { enabled: false },
      paths: [
        { path: '/api', ruleType: 'proxy_pass', sourceType: 'uploaded_file', proxyUrl: 'http://127.0.0.1:8080', proxySourceMode: 'preserve_source' },
        { path: '/static', ruleType: 'static', sourceType: 'host_path', rootPath: '/srv/www' },
      ],
    }])[0];

    expect(domain.paths[0].ruleType).toBe('static');
    expect(domain.paths[0].proxyUrl).toBe('');
    expect(domain.paths[0].proxySourceMode).toBe('');
    expect(domain.paths[1].sourceType).toBe('uploaded_file');
    expect(domain.paths[1].rootPath).toBe('');
  });

  it('requires a static asset for uploaded file sources', () => {
    const missing = validateFacilityPathFields({ path: '/p', ruleType: 'static', sourceType: 'uploaded_file', assetName: '' });
    expect(missing.asset).toBe('applicationsPage.validationStaticAsset');

    const selected = validateFacilityPathFields({ path: '/p', ruleType: 'static', sourceType: 'uploaded_file', assetName: 'index.html' });
    expect(selected).toEqual({});
  });
});
describe('facility domain dialog validation', () => {
  const baseDomain = { domain: 'a.example.test', originServerIds: ['srv-1'], anyAccess: { enabled: false, strategy: 'round_robin' }, paths: [{ path: '/', ruleType: 'static', sourceType: 'host_path' }] };

  it('accepts a clean domain with origin servers', () => {
    const errors = validateFacilityDomainFields(baseDomain, [baseDomain], 0);
    expect(errors).toEqual({});
  });

  it('rejects an empty or invalid domain', () => {
    const errors = validateFacilityDomainFields({ ...baseDomain, domain: '' }, [], -1);
    expect(errors.domain).toBe('applicationsPage.validationDomain');
    const spaced = validateFacilityDomainFields({ ...baseDomain, domain: 'a b.test' }, [], -1);
    expect(spaced.domain).toBe('applicationsPage.validationDomain');
  });

  it('rejects a duplicate domain', () => {
    const existing = [
      { ...baseDomain, domain: 'b.example.test' },
      { ...baseDomain, domain: 'a.example.test' },
    ];
    const errors = validateFacilityDomainFields({ ...baseDomain, domain: 'B.EXAMPLE.TEST' }, existing, 1);
    expect(errors.domain).toBe('applicationsPage.validationDomainDuplicate');
  });

  it('rejects a domain without origin servers', () => {
    const errors = validateFacilityDomainFields({ ...baseDomain, originServerIds: [] }, [], -1);
    expect(errors.originServers).toBe('applicationsPage.validationDomainOriginServers');
  });
});
