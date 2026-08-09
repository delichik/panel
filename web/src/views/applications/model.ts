import YAML from 'yaml';
import type { ApplicationDto, ApplicationRuntime, ApplicationSaveInput, ApplicationSummaryDto, Diagnostic, HttpRouteOptions, ReverseProxyPath, ReverseProxyRule } from '@/types/applications';
import type { FacilityRouteDomain, FacilityRoutePath, ReverseProxyConfig, ReverseProxySaveInput, StaticRuleType, StaticSourceType } from '@/types/facilityApps';

export type AppMode = 'apps' | 'create' | 'edit' | 'facilityCatalog' | 'facilityDetail' | 'facilityConfig';
export type SaveStage = 'idle' | 'validate' | 'preview' | 'commit';

export interface KeyValueRow {
  id: string;
  key: string;
  value: string;
}

export interface PortRow {
  id: string;
  label: string;
  to: string;
  staticPort: string;
}

export interface CommandRow {
  id: string;
  value: string;
}

export interface MountRow {
  id: string;
  type: string;
  source: string;
  target: string;
  readOnly: boolean;
  mode: string;
}

export type ProxyRuleDraft = ReverseProxyRule;

export interface ApplicationDraftUi {
  name: string;
  enabled: boolean;
  image: string;
  commandRows: CommandRow[];
  networkMode: 'bridge' | 'host';
  cpu: string;
  memoryMb: string;
  privileged: boolean;
  specYaml: string;
  yamlDirty: boolean;
  env: KeyValueRow[];
  ports: PortRow[];
  mounts: MountRow[];
  deploymentMode: 'all' | 'selected';
  deploymentServers: string[];
  reverseProxy: ReverseProxyRule[];
}

export interface FacilityDraftUi {
  deploymentServers: string[];
  panelEnabled: boolean;
  panelServerId: string;
  panelHostServerId: string;
  panelDomain: string;
  domains: FacilityRouteDomain[];
}

export interface FieldErrors {
  [field: string]: string;
}

export interface SectionState {
  id: string;
  complete: boolean;
  dirty?: boolean;
  error?: boolean;
}

export interface PreviewDiff {
  added: number;
  changed: number;
  removed: number;
  warnings: number;
}

let localIdSeed = 0;

export function routeMode(path: string, params: Record<string, unknown>): AppMode {
  if (path.endsWith('/create')) return 'create';
  if (params.applicationId) return 'edit';
  if (path.endsWith('/config') && params.facilityKind) return 'facilityConfig';
  if (params.facilityKind) return 'facilityDetail';
  if (path.endsWith('/facility-apps')) return 'facilityCatalog';
  return 'apps';
}

export function applicationStatus(app: ApplicationDto | ApplicationSummaryDto, runtime?: ApplicationRuntime | null) {
  const status = runtime?.status || app.runtimeStatus;
  if (!app.enabled) return 'disabled';
  if (app.reconcileStopped) return 'attention';
  if (status === 'deploying' || status === 'pending') return 'deploying';
  if (status === 'failed' || app.lastError) return 'failed';
  if (status === 'partially_deployed') return 'partial';
  if (status === 'deployed' || status === 'running') return 'deployed';
  return app.enabled ? 'enabled' : 'unknown';
}

export function statusTone(status: string) {
  if (['deployed', 'enabled', 'running'].includes(status)) return 'success';
  if (['deploying', 'partial', 'warning', 'pending', 'attention'].includes(status)) return 'warning';
  if (['failed', 'disabled', 'error'].includes(status)) return 'danger';
  return 'neutral';
}

export function draftFromApplication(app?: ApplicationDto | null): ApplicationDraftUi {
  const parsed = parseSpec(app?.specYaml || '');
  const command = arrayValue(parsed.command).map((item) => String(item)).filter((item) => item.trim() !== '');
  const networkMode: 'bridge' | 'host' = stringValue(parsed.networkMode) === 'host' ? 'host' : 'bridge';
  return {
    name: app?.name || stringValue(parsed.name) || '',
    enabled: app?.enabled ?? true,
    image: stringValue(parsed.image),
    commandRows: command.map((value) => ({ id: makeId('cmd'), value })),
    networkMode,
    cpu: stringValue(objectValue(parsed.resources)?.cpu),
    memoryMb: stringValue(objectValue(parsed.resources)?.memoryMb),
    privileged: Boolean(parsed.privileged),
    specYaml: app?.specYaml || '',
    yamlDirty: false,
    env: pairsFromRecord(objectToStringRecord(objectValue(parsed.env))),
    ports: arrayValue(parsed.ports).map((item, index) => {
      const port = objectValue(item);
      return { id: makeId('port'), label: stringValue(port?.label) || `port-${index + 1}`, to: stringValue(port?.to) || '80', staticPort: stringValue(port?.static) };
    }),
    mounts: arrayValue(parsed.mounts).map((item) => {
      const mount = objectValue(item);
      return { id: makeId('mount'), type: stringValue(mount?.type) || 'volume', source: stringValue(mount?.source), target: stringValue(mount?.target) || '/data', readOnly: Boolean(mount?.readOnly), mode: stringValue(mount?.mode) };
    }),
    deploymentMode: app?.deploymentMode === 'selected' ? 'selected' : 'all',
    deploymentServers: [...(app?.deploymentServers ?? [])],
    reverseProxy: cloneProxyRules(app?.reverseProxy ?? []).map((rule) => (networkMode === 'host' && rule.targetType === 'container' ? { ...rule, targetType: 'local' } : rule)),
  };
}

export function applyYamlToDraft(draft: ApplicationDraftUi): { ok: boolean; error?: string } {
  try {
    const parsed = parseSpec(draft.specYaml);
    const next = draftFromApplication({ ...emptyApp(), name: draft.name, enabled: draft.enabled, specYaml: draft.specYaml, deploymentMode: draft.deploymentMode, deploymentServers: draft.deploymentServers, reverseProxy: draft.reverseProxy });
    draft.image = next.image;
    draft.commandRows = next.commandRows;
    draft.networkMode = next.networkMode;
    draft.cpu = next.cpu;
    draft.memoryMb = next.memoryMb;
    draft.privileged = next.privileged;
    draft.env = next.env;
    draft.ports = next.ports;
    draft.mounts = next.mounts;
    draft.name = draft.name || stringValue(parsed.name);
    draft.yamlDirty = false;
    return { ok: true };
  } catch (error) {
    return { ok: false, error: error instanceof Error ? error.message : 'Invalid YAML' };
  }
}

export function syncDraftToYaml(draft: ApplicationDraftUi) {
  draft.specYaml = specYamlFromDraft(draft);
  draft.yamlDirty = false;
}

export function specYamlFromDraft(draft: ApplicationDraftUi) {
  const doc: Record<string, unknown> = {
    name: draft.name,
    image: draft.image,
    networkMode: draft.networkMode,
  };
  const command = draft.commandRows.map((row) => row.value.trim()).filter(Boolean);
  if (command.length) doc.command = command;
  const env = recordFromPairs(draft.env);
  if (Object.keys(env).length) doc.env = env;
  const ports = draft.ports.map((port) => compact({ label: port.label.trim(), to: numberOrString(port.to), static: numberOrUndefined(port.staticPort) })).filter((port) => port.to);
  if (ports.length) doc.ports = ports;
  const mounts = draft.mounts.map((mount) => compact({ type: mount.type, source: mount.source.trim(), target: mount.target.trim(), readOnly: mount.readOnly || undefined, mode: mount.mode.trim() || undefined })).filter((mount) => mount.type && mount.target);
  if (mounts.length) doc.mounts = mounts;
  const resources = compact({ cpu: numberOrUndefined(draft.cpu), memoryMb: numberOrUndefined(draft.memoryMb) });
  if (Object.keys(resources).length) doc.resources = resources;
  if (draft.privileged) doc.privileged = true;
  return `${YAML.stringify(doc).trim()}\n`;
}

export function saveInputFromDraft(draft: ApplicationDraftUi): ApplicationSaveInput {
  const specYaml = draft.yamlDirty ? draft.specYaml : specYamlFromDraft(draft);
  return {
    name: draft.name.trim(),
    enabled: draft.enabled,
    specYaml,
    deploymentMode: draft.deploymentMode,
    deploymentServers: draft.deploymentMode === 'selected' ? [...draft.deploymentServers] : [],
    reverseProxy: cloneProxyRules(draft.reverseProxy).map((rule) => (draft.networkMode === 'host' && rule.targetType === 'container' ? { ...rule, targetType: 'local' } : rule)),
  };
}

export function validateApplicationDraft(draft: ApplicationDraftUi): FieldErrors {
  const errors: FieldErrors = {};
  if (!draft.name.trim()) errors.name = 'applicationsPage.validationName';
  if (draft.yamlDirty && !draft.specYaml.trim()) errors.specYaml = 'applicationsPage.validationSpec';
  if (!draft.yamlDirty && !draft.image.trim()) errors.image = 'applicationsPage.validationImage';
  if (draft.deploymentMode === 'selected' && !draft.deploymentServers.length) errors.deploymentServers = 'applicationsPage.validationDeploymentServers';
  if (draft.env.some((row) => !row.key.trim())) errors.env = 'applicationsPage.validationEnv';
  if (draft.ports.some((row) => !row.to.trim())) errors.ports = 'applicationsPage.validationPorts';
  if (draft.mounts.some((row) => !row.target.trim())) errors.mounts = 'applicationsPage.validationMounts';
  if (draft.reverseProxy.some((rule) => !rule.domain.trim() || !rule.targetType || !rule.targetPort || !rule.paths.length || rule.paths.some((path) => !path.path.trim()))) errors.reverseProxy = 'applicationsPage.validationReverseProxy';
  if (draft.yamlDirty) {
    try {
      parseSpec(draft.specYaml);
      errors.specYaml = 'applicationsPage.validationSourceStaged';
    } catch {
      errors.specYaml = 'applicationsPage.validationYaml';
    }
  }
  return errors;
}

export function applicationSections(draft: ApplicationDraftUi, errors: FieldErrors): SectionState[] {
  return [
    { id: 'identity', complete: Boolean(draft.name.trim() && draft.enabled !== undefined), error: Boolean(errors.name) },
    { id: 'runtime', complete: draft.yamlDirty ? Boolean(draft.specYaml.trim()) : Boolean(draft.image.trim()), error: Boolean(errors.image || errors.specYaml) },
    { id: 'networking', complete: true, error: Boolean(errors.ports || errors.reverseProxy) },
    { id: 'storage', complete: true, error: Boolean(errors.env || errors.mounts) },
    { id: 'deployment', complete: draft.deploymentMode === 'all' || draft.deploymentServers.length > 0, error: Boolean(errors.deploymentServers) },
    { id: 'files', complete: true },
    { id: 'source', complete: Boolean(draft.specYaml.trim()), dirty: draft.yamlDirty, error: Boolean(errors.specYaml) },
  ];
}

export function facilityDraftFromConfig(config?: ReverseProxyConfig | null): FacilityDraftUi {
  return {
    deploymentServers: [...(config?.deploymentServers ?? [])],
    panelEnabled: Boolean(config?.panelEntry?.enabled),
    panelServerId: config?.panelHostServerId ?? config?.panelEntry?.serverId ?? '',
    panelDomain: config?.panelEntry?.domain ?? '',
    panelHostServerId: config?.panelHostServerId ?? '',
    domains: cloneFacilityDomains(config?.domains ?? []),
  };
}

export function facilitySaveInputFromDraft(draft: FacilityDraftUi): ReverseProxySaveInput {
  return {
    deploymentServers: [...draft.deploymentServers],
    panelEntry: { enabled: draft.panelEnabled, serverId: draft.panelEnabled ? draft.panelServerId.trim() : '', domain: draft.panelEnabled ? draft.panelDomain.trim().toLowerCase() : '' },
    domains: cloneFacilityDomains(draft.domains).map((domain) => ({ ...domain, domain: domain.domain.trim().toLowerCase() })),
  };
}

export function validateFacilityDraft(draft: FacilityDraftUi): FieldErrors {
  const errors: FieldErrors = {};
  if (!draft.deploymentServers.length) errors.deploymentServers = 'applicationsPage.validationGatewayServers';
  if (draft.panelEnabled && !draft.panelServerId.trim()) errors.panelServerId = 'applicationsPage.validationPanelServer';
  if (draft.panelEnabled && !draft.panelDomain.trim()) errors.panelDomain = 'applicationsPage.validationPanelDomain';
  if (draft.domains.some((domain) => !domain.domain.trim())) errors.domains = 'applicationsPage.validationDomains';
  if (draft.domains.some((domain) => !domain.originServerIds.length || domain.originServerIds.some((id) => !draft.deploymentServers.includes(id)))) errors.originServers = 'applicationsPage.validationOriginServers';
  if (draft.domains.some((domain) => !domain.paths.length)) errors.paths = 'applicationsPage.validationPaths';
  return errors;
}

export function validateFacilityPathFields(path: FacilityRoutePath): FieldErrors {
  const errors: FieldErrors = {};
  const pathValue = (path.path ?? '').trim();
  if (pathValue === '' || !pathValue.startsWith('/') || /[\s;{}#"\\']/.test(pathValue)) {
    errors.path = 'applicationsPage.validationPath';
  }
  if (path.ruleType === 'redirect') {
    const target = (path.redirectUrl ?? '').trim();
    if (!target || /[\s\x00;{}#"\\']/.test(target)) {
      errors.redirectUrl = 'applicationsPage.validationRedirectUrl';
    }
  } else if (path.ruleType === 'proxy_pass') {
    const target = (path.proxyUrl ?? '').trim();
    const lower = target.toLowerCase();
    if (!target || !(lower.startsWith('http://') || lower.startsWith('https://')) || /[\s\x00;{}#"\\']/.test(target)) {
      errors.proxyUrl = 'applicationsPage.validationProxyUrl';
    }
  } else if (path.ruleType === 'static') {
    const source = (path.sourceType ?? '').trim();
    if ((source === 'uploaded_file' || source === 'uploaded_bundle') && !(path.assetName ?? '').trim()) {
      errors.asset = 'applicationsPage.validationStaticAsset';
    }
  }
  return errors;
}
export function validateFacilityDomainFields(domain: FacilityRouteDomain, existing: FacilityRouteDomain[], selfIndex: number): FieldErrors {
  const errors: FieldErrors = {};
  const value = (domain.domain ?? '').trim().toLowerCase();
  if (!value || /[\s;{}]/.test(value)) {
    errors.domain = 'applicationsPage.validationDomain';
  } else if (existing.some((item, index) => index !== selfIndex && (item.domain ?? '').trim().toLowerCase() === value)) {
    errors.domain = 'applicationsPage.validationDomainDuplicate';
  }
  if (!domain.originServerIds.length) {
    errors.originServers = 'applicationsPage.validationDomainOriginServers';
  }
  return errors;
}
export function facilitySections(draft: FacilityDraftUi, errors: FieldErrors): SectionState[] {
  return [
    { id: 'gateways', complete: draft.deploymentServers.length > 0, error: Boolean(errors.deploymentServers) },
    { id: 'domains', complete: draft.domains.length > 0, error: Boolean(errors.domains || errors.originServers || errors.paths) },
    { id: 'panel', complete: !draft.panelEnabled || Boolean(draft.panelServerId && draft.panelDomain), error: Boolean(errors.panelServerId || errors.panelDomain) },
    { id: 'assets', complete: true },
  ];
}

export function diffApplications(base?: ApplicationDto | null, draft?: ApplicationDraftUi | null): PreviewDiff {
  if (!draft) return { added: 0, changed: 0, removed: 0, warnings: 0 };
  if (!base?.id) return { added: 1 + draft.reverseProxy.length + draft.mounts.length + draft.ports.length, changed: 0, removed: 0, warnings: 0 };
  const input = saveInputFromDraft(draft);
  // Normalize the saved application through the same draft pipeline so that
  // YAML round-trip formatting and defaulted route options do not surface as
  // a fake "changed" entry on a freshly opened editor.
  const baseComparable = saveInputFromDraft(draftFromApplication(base));
  return diffObjects(baseComparable, input);
}

export function diffFacility(base?: ReverseProxyConfig | null, draft?: FacilityDraftUi | null): PreviewDiff {
  if (!draft) return { added: 0, changed: 0, removed: 0, warnings: 0 };
  const input = facilitySaveInputFromDraft(draft);
  if (!base) return { added: input.deploymentServers.length + input.domains.length + Number(input.panelEntry.enabled), changed: 0, removed: 0, warnings: 0 };
  return {
    added: Math.max(0, input.domains.length - base.domains.length) + Math.max(0, input.deploymentServers.length - base.deploymentServers.length),
    removed: Math.max(0, base.domains.length - input.domains.length) + Math.max(0, base.deploymentServers.length - input.deploymentServers.length),
    changed: countChanged([JSON.stringify(input.panelEntry) !== JSON.stringify(base.panelEntry), JSON.stringify(input.domains) !== JSON.stringify(base.domains), JSON.stringify(input.deploymentServers) !== JSON.stringify(base.deploymentServers)]),
    warnings: input.domains.some((domain) => domain.domain.includes('conflict')) ? 1 : 0,
  };
}

export function hasBlockingDiagnostic(items: Diagnostic[]) {
  return items.some((item) => item.severity === 'error');
}

export function routeSummary(app: ApplicationDto) {
  return app.reverseProxy.flatMap((rule) => rule.paths.map((path) => `${rule.domain}${path.path || '/'}`));
}

export function runtimeSummary(runtime?: ApplicationRuntime | null) {
  if (!runtime) return { total: 0, failed: 0, running: 0 };
  return {
    total: runtime.instances.length,
    failed: runtime.instances.filter((item) => item.status === 'failed' || item.state === 'failed').length,
    running: runtime.instances.filter((item) => ['running', 'ready', 'deployed'].includes(String(item.status || item.state))).length,
  };
}

export function makeKeyValueRow(key = '', value = ''): KeyValueRow {
  return { id: makeId('kv'), key, value };
}

export function makePortRow(): PortRow {
  return { id: makeId('port'), label: '', to: '', staticPort: '' };
}

export function makeMountRow(type = 'persistent'): MountRow {
  return { id: makeId('mount'), type, source: '', target: '', readOnly: type === 'file' || type === 'panel_file', mode: '' };
}

export function makeProxyRule(): ReverseProxyRule {
  return { domain: '', targetType: undefined, targetPort: 0, originServerIds: [], anyAccess: { enabled: false, strategy: '', primaryOriginServerId: '', relayServerIds: [] }, paths: [] };
}

export function makeProxyPath(): ReverseProxyPath {
  return { path: '', webSocket: false, options: defaultRouteOptions() };
}

export function makeFacilityDomain(): FacilityRouteDomain {
  return { domain: '', originServerIds: [], anyAccess: { enabled: false, strategy: 'round_robin', relayServerIds: [] }, paths: [makeFacilityPath()] };
}

export function makeFacilityPath(type: StaticRuleType = 'static'): FacilityRoutePath {
  return { path: '', ruleType: type, sourceType: 'uploaded_file', rootPath: '', assetName: '', redirectUrl: '', redirectCode: 0, proxyUrl: '', proxySourceMode: '', options: defaultRouteOptions() };
}

export function cloneProxyRules(rules: ReverseProxyRule[]) {
  return rules.map((rule) => ({
    domain: rule.domain,
    targetType: rule.targetType || 'local',
    targetPort: Number(rule.targetPort || 0),
    originServerIds: [...(rule.originServerIds ?? [])],
    anyAccess: { enabled: Boolean(rule.anyAccess?.enabled), strategy: rule.anyAccess?.strategy || '', primaryOriginServerId: rule.anyAccess?.primaryOriginServerId || '', relayServerIds: [...(rule.anyAccess?.relayServerIds ?? [])] },
    paths: (rule.paths ?? []).map((path) => ({ path: path.path || '/', webSocket: Boolean(path.webSocket), options: { ...defaultRouteOptions(), ...(path.options ?? {}) } })),
  }));
}

export function normalizeFacilityPath(path: FacilityRoutePath): FacilityRoutePath {
  const ruleType: StaticRuleType = path.ruleType === 'proxy_pass' ? 'static' : (path.ruleType as StaticRuleType) || 'static';
  const sourceType: StaticSourceType = path.sourceType === 'host_path' ? 'uploaded_file' : (path.sourceType as StaticSourceType) || 'uploaded_file';
  const normalized: FacilityRoutePath = {
    ...makeFacilityPath(ruleType),
    ...path,
    ruleType,
    sourceType,
    options: { ...defaultRouteOptions(), ...(path.options ?? {}) },
  };
  normalized.rootPath = '';
  if (normalized.ruleType !== 'proxy_pass') {
    normalized.proxyUrl = '';
    normalized.proxySourceMode = '';
  }
  if (normalized.ruleType !== 'redirect') {
    normalized.redirectUrl = '';
    normalized.redirectCode = 0;
  }
  if (normalized.ruleType !== 'static') {
    normalized.assetName = '';
  }
  return normalized;
}

export function cloneFacilityDomains(domains: FacilityRouteDomain[]) {
  return domains.map((domain) => ({
    domain: domain.domain,
    originServerIds: [...(domain.originServerIds ?? [])],
    anyAccess: { enabled: Boolean(domain.anyAccess?.enabled), strategy: domain.anyAccess?.strategy || 'round_robin', primaryOriginServerId: domain.anyAccess?.primaryOriginServerId || '', relayServerIds: [...(domain.anyAccess?.relayServerIds ?? [])] },
    paths: (domain.paths ?? []).map((path) => normalizeFacilityPath(path)),
  }));
}

export function cloneFacilityPath(path: FacilityRoutePath) {
  return normalizeFacilityPath(path);
}

export function defaultRouteOptions(): HttpRouteOptions {
  return { gzipMode: 'inherit', clientMaxBodySizeMb: 0, connectTimeoutSeconds: 0, readTimeoutSeconds: 0, sendTimeoutSeconds: 0, bufferingMode: 'inherit', webSocketMode: 'off', requestHeaders: [], responseHeaders: [] };
}

function parseSpec(raw: string) {
  const parsed = YAML.parse(raw || '{}');
  return objectValue(parsed) ?? {};
}

function pairsFromRecord(value?: Record<string, unknown>) {
  return Object.entries(value ?? {}).map(([key, val]) => makeKeyValueRow(key, String(val ?? '')));
}

function recordFromPairs(rows: KeyValueRow[]) {
  const out: Record<string, string> = {};
  rows.forEach((row) => {
    const key = row.key.trim();
    if (key) out[key] = row.value;
  });
  return out;
}

function objectToStringRecord(value?: Record<string, unknown>) {
  if (!value) return {};
  return Object.fromEntries(Object.entries(value).map(([key, val]) => [key, String(val ?? '')]));
}

function arrayValue(value: unknown) {
  return Array.isArray(value) ? value : [];
}

function objectValue(value: unknown) {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : undefined;
}

function stringValue(value: unknown) {
  if (value === undefined || value === null) return '';
  return String(value);
}

function numberOrString(raw: string) {
  const value = Number(raw);
  return Number.isFinite(value) && raw.trim() !== '' ? value : raw.trim();
}

function numberOrUndefined(raw: string) {
  const value = Number(raw);
  return Number.isFinite(value) && raw.trim() !== '' ? value : undefined;
}

function compact<T extends Record<string, unknown>>(input: T) {
  return Object.fromEntries(Object.entries(input).filter(([, value]) => value !== undefined && value !== '')) as Partial<T>;
}

function countChanged(values: boolean[]) {
  return values.filter(Boolean).length;
}

function diffObjects(base: unknown, next: unknown): PreviewDiff {
  const baseValue = JSON.stringify(base);
  const nextValue = JSON.stringify(next);
  return { added: 0, changed: baseValue === nextValue ? 0 : 1, removed: 0, warnings: 0 };
}

function makeId(prefix: string) {
  localIdSeed += 1;
  return `${prefix}-${localIdSeed}`;
}

function emptyApp(): ApplicationDto {
  return { id: '', version: 0, kind: 'application', name: '', enabled: true, specYaml: '', deploymentMode: 'all', deploymentServers: [], reverseProxy: [], generation: 0, specHash: '', imageUpdateAvailable: false, jobId: '', namespace: '', createdAt: '', updatedAt: '' };
}
