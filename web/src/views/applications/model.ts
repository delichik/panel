import YAML from 'yaml';
import type { ApplicationDto, ApplicationFile, ApplicationRuntime, ApplicationSaveInput, ApplicationSummaryDto, Diagnostic, HttpRouteOptions, PanelFileDefinition, ReverseProxyPath, ReverseProxyRule } from '@/types/applications';
import type { ConfigRecord, FacilityRouteDomain, FacilityRoutePath, ReverseProxyConfig, ReverseProxySaveInput, StaticRuleType, StaticSourceType } from '@/types/facilityApps';

export type AppMode = 'apps' | 'create' | 'edit' | 'facilityCatalog' | 'facilityDetail' | 'facilityConfig';
export type SaveStage = 'idle' | 'validate' | 'preview' | 'commit';

/** 应用详情页签（URL `tab` 查询参数）。 */
export type AppDetailTab = 'runtime' | 'routes' | 'files' | 'data';
/** 反向代理设施详情页签（URL `tab` 查询参数）。 */
export type FacilityDetailTab = 'proxy' | 'configs' | 'dns' | 'settings';

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
  protocol: 'tcp' | 'udp';
  openFirewall: boolean;
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

export interface ApplicationFileMountOption {
  label: string;
  value: string;
  disabled?: boolean;
}

export function applicationFileMountOptions(
  files: Array<Pick<ApplicationFile, 'name'>>,
  currentSource = '',
  missingLabel: (name: string) => string = (name) => name,
): ApplicationFileMountOption[] {
  const options: ApplicationFileMountOption[] = files.map((file) => ({ label: file.name, value: file.name }));
  const selected = currentSource.trim();
  if (selected && !options.some((option) => option.value === selected)) {
    options.unshift({ label: missingLabel(selected), value: selected, disabled: true });
  }
  return options;
}

export function panelFileMountOptions(
  files: Array<Pick<PanelFileDefinition, 'name' | 'kind' | 'resourceType' | 'source'>>,
  currentSource = '',
  optionLabel: (file: Pick<PanelFileDefinition, 'name' | 'kind' | 'resourceType' | 'source'>) => string = (file) => file.name,
  missingLabel: (source: string) => string = (source) => source,
): ApplicationFileMountOption[] {
  const options: ApplicationFileMountOption[] = files.map((file) => ({ label: optionLabel(file), value: file.source }));
  const selected = currentSource.trim();
  if (selected && !options.some((option) => option.value === selected)) {
    options.unshift({ label: missingLabel(selected), value: selected, disabled: true });
  }
  return options;
}

export type ProxyRuleDraft = ReverseProxyRule;

export interface ApplicationDraftUi {
  name: string;
  enabled: boolean;
  /** Object-level auto-update opt-out (default true). */
  autoUpdateEnabled: boolean;
  image: string;
  commandRows: CommandRow[];
  cpu: string;
  memoryMb: string;
  privileged: boolean;
  env: KeyValueRow[];
  ports: PortRow[];
  mounts: MountRow[];
  deploymentMode: 'all' | 'selected';
  deploymentServers: string[];
  reverseProxy: ReverseProxyRule[];
  /** Structured editing only covers a subset of spec keys; anything else
   *  (capAdd, sysctls, healthcheck, resource limits, ...) is preserved here
   *  and merged back when the YAML is regenerated. */
  uncoveredSpec: Record<string, unknown>;
}

export interface FacilityDraftUi {
  deploymentServers: string[];
  domains: FacilityRouteDomain[];
}

export interface FieldErrors {
  [field: string]: string;
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

/** Agent ensure steps projected onto Job.lastStage while deploy is in flight. */
const DEPLOY_PROGRESS_STAGES = new Set([
  'write_files',
  'ensure_image',
  'inspect_container',
  'create_container',
  'start_container',
  'verify_running',
  'replace_stop',
  'replace_remove',
  'cleanup_legacy_workspace',
  'stop',
  'purge',
  'apply',
]);

export function isDeployProgressStage(stage?: string | null) {
  return !!stage && DEPLOY_PROGRESS_STAGES.has(stage.trim());
}

/** Monitor-mode fence without projected steps still needs attention; with steps it is normal converging. */
export function uncertaintyNeedsAttention(errorClass?: string | null, lastStage?: string | null) {
  return errorClass === 'uncertainty' && !isDeployProgressStage(lastStage);
}

export function applicationStatus(app: ApplicationDto | ApplicationSummaryDto, _runtime?: ApplicationRuntime | null) {
  // Application primary badge is enable/disable only. Node health and deploy
  // stages live in bucket counts and per-instance operation projections.
  return app.enabled ? 'enabled' : 'disabled';
}

export function applicationNodeCounts(app: ApplicationDto | ApplicationSummaryDto, runtime?: ApplicationRuntime | null) {
  return {
    healthy: runtime?.healthyInstanceCount ?? (app as ApplicationSummaryDto).healthyInstanceCount ?? 0,
    abnormal: runtime?.abnormalInstanceCount ?? (app as ApplicationSummaryDto).abnormalInstanceCount ?? 0,
    deploying: runtime?.deployingInstanceCount ?? (app as ApplicationSummaryDto).deployingInstanceCount ?? 0,
  };
}

export function instanceOperationStatus(instance?: { operation?: ApplicationRuntime['operation'] | null; status?: string; state?: string } | null) {
  const operation = instance?.operation;
  if (uncertaintyNeedsAttention(operation?.errorClass, operation?.stage)) return 'result_unknown';
  if (operation?.errorClass === 'uncertainty' && isDeployProgressStage(operation.stage)) {
    return operation.status || 'running';
  }
  return operation?.status || '';
}

export function applicationPlanningError(app: ApplicationDto | ApplicationSummaryDto, runtime?: ApplicationRuntime | null) {
  return runtime ? runtime.planningError : app.planningError;
}

export function applicationOperationStatus(runtime?: ApplicationRuntime | null) {
  if (uncertaintyNeedsAttention(runtime?.operation?.errorClass, runtime?.operation?.stage)) return 'result_unknown';
  if (runtime?.operation?.errorClass === 'uncertainty' && isDeployProgressStage(runtime.operation?.stage)) {
    return runtime.operation?.status || 'running';
  }
  return runtime?.operation?.status || '';
}

export function statusTone(status: string) {
  if (status === 'result_unknown') return 'warning';
  if (status === 'planning_failed') return 'danger';
  if (['deployed', 'enabled', 'running'].includes(status)) return 'success';
  if (['deploying', 'failed_retryable', 'partial', 'warning', 'pending', 'attention'].includes(status)) return 'warning';
  if (['failed', 'disabled', 'error'].includes(status)) return 'danger';
  return 'neutral';
}

const activeJobStatuses = new Set(['deploying', 'pending', 'queued', 'running', 'waiting', 'failed_retryable']);

export function shouldPollApplicationRuntime(runtime?: ApplicationRuntime | null) {
  if (!runtime) return false;
  if (runtime.planningError) return true;
  return (runtime.instances ?? []).some((instance) => activeJobStatuses.has(instance.operation?.status || ''));
}

export function applicationRuntimePollDelay(runtime: ApplicationRuntime, now = Date.now()) {
  const baseDelay = 3000;
  let soonest = Number.POSITIVE_INFINITY;
  for (const instance of runtime.instances ?? []) {
    if (instance.operation?.status !== 'failed_retryable' || !instance.operation.nextRunAt) continue;
    const nextRunAt = Date.parse(instance.operation.nextRunAt);
    if (Number.isFinite(nextRunAt) && nextRunAt < soonest) soonest = nextRunAt;
  }
  if (!Number.isFinite(soonest)) return baseDelay;
  return Math.min(30000, Math.max(baseDelay, soonest - now + 250));
}

export function draftFromApplication(app?: ApplicationDto | null): ApplicationDraftUi {
  const parsed = parseSpec(app?.specYaml || '');
  const command = arrayValue(parsed.command).map((item) => String(item)).filter((item) => item.trim() !== '');
  return {
    name: app?.name || stringValue(parsed.name) || '',
    enabled: app?.enabled ?? true,
    autoUpdateEnabled: app?.autoUpdateEnabled !== false,
    image: stringValue(parsed.image),
    commandRows: command.map((value) => ({ id: makeId('cmd'), value })),
    cpu: stringValue(objectValue(parsed.resources)?.cpu),
    memoryMb: stringValue(objectValue(parsed.resources)?.memoryMb),
    privileged: Boolean(parsed.privileged),
    env: pairsFromRecord(objectToStringRecord(objectValue(parsed.env))),
    ports: arrayValue(parsed.ports).map((item, index) => {
      const port = objectValue(item);
      return {
        id: makeId('port'),
        label: stringValue(port?.label) || `port-${index + 1}`,
        to: stringValue(port?.to) || '80',
        staticPort: stringValue(port?.static),
        protocol: stringValue(port?.protocol).toLowerCase() === 'udp' ? 'udp' : 'tcp',
        openFirewall: port?.openFirewall === true,
      };
    }),
    mounts: arrayValue(parsed.mounts).map((item) => {
      const mount = objectValue(item);
      return { id: makeId('mount'), type: stringValue(mount?.type) || 'volume', source: stringValue(mount?.source), target: stringValue(mount?.target) || '/data', readOnly: Boolean(mount?.readOnly), mode: stringValue(mount?.mode) };
    }),
    deploymentMode: app?.deploymentMode === 'selected' ? 'selected' : 'all',
    deploymentServers: [...(app?.deploymentServers ?? [])],
    reverseProxy: cloneProxyRules(app?.reverseProxy ?? []),
    uncoveredSpec: uncoveredSpecFrom(parsed),
  };
}

export function specYamlFromDraft(draft: ApplicationDraftUi) {
  const uncovered = draft.uncoveredSpec ?? {};
  const doc: Record<string, unknown> = { ...uncovered };
  doc.name = draft.name;
  doc.image = draft.image;
  const command = draft.commandRows.map((row) => row.value.trim()).filter(Boolean);
  if (command.length) doc.command = command; else delete doc.command;
  const env = recordFromPairs(draft.env);
  if (Object.keys(env).length) doc.env = env; else delete doc.env;
  const ports = draft.ports.map((port) => compact({
    label: port.label.trim(),
    to: numberOrString(port.to),
    static: numberOrUndefined(port.staticPort),
    protocol: port.protocol === 'udp' ? 'udp' : undefined,
    openFirewall: port.staticPort.trim() && port.openFirewall ? true : undefined,
  })).filter((port) => port.to);
  if (ports.length) doc.ports = ports; else delete doc.ports;
  const mounts = draft.mounts.map((mount) => compact({ type: mount.type, source: mount.source.trim(), target: mount.target.trim(), readOnly: mount.readOnly || undefined, mode: mount.mode.trim() || undefined })).filter((mount) => mount.type && mount.target);
  if (mounts.length) doc.mounts = mounts; else delete doc.mounts;
  const resources = { ...(objectValue(uncovered.resources) ?? {}), ...compact({ cpu: numberOrUndefined(draft.cpu), memoryMb: numberOrUndefined(draft.memoryMb) }) };
  if (Object.keys(resources).length) doc.resources = resources; else delete doc.resources;
  if (draft.privileged) doc.privileged = true; else delete doc.privileged;
  return `${YAML.stringify(doc).trim()}\n`;
}

export function saveInputFromDraft(draft: ApplicationDraftUi): ApplicationSaveInput {
  const specYaml = specYamlFromDraft(draft);
  return {
    name: draft.name.trim(),
    enabled: draft.enabled,
    autoUpdateEnabled: draft.autoUpdateEnabled !== false,
    specYaml,
    deploymentMode: draft.deploymentMode,
    deploymentServers: draft.deploymentMode === 'selected' ? [...draft.deploymentServers] : [],
    // Origin servers and the primary origin are resolved by the backend from
    // the global gateway nodes and the deployment targets. The client only
    // sends the user-arranged priority order for primary_backup.
    reverseProxy: cloneProxyRules(draft.reverseProxy).map((rule) => ({
      ...withDomainTransportOverrides(rule),
      originServerIds: [] as string[],
      anyAccess: { ...rule.anyAccess, primaryOriginServerId: '' },
      customServerDirectives: normalizedCustomDirectives(rule.customServerDirectives, 'server') ?? [...(rule.customServerDirectives ?? [])],
    })),
  };
}

export function validateApplicationDraft(draft: ApplicationDraftUi): FieldErrors {
  const errors: FieldErrors = {};
  if (!draft.name.trim()) errors.name = 'applicationsPage.validationName';
  if (!draft.image.trim()) errors.image = 'applicationsPage.validationImage';
  if (draft.cpu.trim() && !Number.isFinite(Number(draft.cpu))) errors.cpu = 'applicationsPage.validationNumber';
  if (draft.memoryMb.trim() && !Number.isFinite(Number(draft.memoryMb))) errors.memoryMb = 'applicationsPage.validationNumber';
  if (draft.deploymentMode === 'selected' && !draft.deploymentServers.length) errors.deploymentServers = 'applicationsPage.validationDeploymentServers';
  if (draft.env.some((row) => !row.key.trim())) errors.env = 'applicationsPage.validationEnv';
  if (draft.ports.some((row) => !row.to.trim())) errors.ports = 'applicationsPage.validationPorts';
  if (draft.ports.some((row) => row.openFirewall && !row.staticPort.trim())) errors.ports = 'applicationsPage.validationFirewallStaticPort';
  if (draft.mounts.some((row) => !row.target.trim())) errors.mounts = 'applicationsPage.validationMounts';
  if (draft.reverseProxy.some((rule) => !rule.domain.trim() || !rule.targetPort || !rule.paths.length || rule.paths.some((path) => !path.path.trim()))) errors.reverseProxy = 'applicationsPage.validationReverseProxyRule';
  if (draft.reverseProxy.some((rule) => !isValidDomainHttpPortMode(rule.httpPortMode))) errors.httpPortMode = 'applicationsPage.validationHttpPortMode';
  if (draft.reverseProxy.some((rule) => !isValidDomainClientMaxBodySize(rule.clientMaxBodySize))) errors.clientMaxBodySize = 'applicationsPage.transport.clientMaxBodySizeInvalid';
  const directiveError = firstCustomDirectiveError(draft.reverseProxy.flatMap((rule) => rule.customServerDirectives ?? []), 'server');
  if (directiveError) errors.customDirectives = directiveError;
  return errors;
}

export function validateFileName(name: string): string | null {
  const value = name.trim();
  if (!value || /[\\/]/.test(value) || value.includes('..') || /[\x00-\x1f\x7f]/.test(value)) {
    return 'applicationsPage.validationFileName';
  }
  return null;
}

export function facilityDraftFromConfig(config?: ReverseProxyConfig | null): FacilityDraftUi {
  return {
    deploymentServers: [...(config?.deploymentServers ?? [])],
    domains: cloneFacilityDomains(config?.domains ?? []),
  };
}

export function facilitySaveInputFromDraft(draft: FacilityDraftUi): ReverseProxySaveInput {
  return {
    deploymentServers: [...draft.deploymentServers],
    domains: cloneFacilityDomains(draft.domains).map((domain) => ({
      ...withDomainTransportOverrides(domain),
      domain: domain.domain.trim().toLowerCase(),
      customServerDirectives: normalizedCustomDirectives(domain.customServerDirectives, 'server') ?? [...(domain.customServerDirectives ?? [])],
    })),
  };
}

/** 保存用单域名 payload：收敛传输开关、HSTS/请求体覆盖并归一化自定义 server 指令。 */
export function facilityDomainSaveInput(domain: FacilityRouteDomain): FacilityRouteDomain {
  const cloned = cloneFacilityDomain(domain);
  const directives = normalizedCustomDirectives(cloned.customServerDirectives, 'server');
  return {
    ...withDomainTransportOverrides(cloned),
    httpPortMode: domainHttpPortMode(cloned),
    clientMaxBodySize: domainClientMaxBodySize(cloned),
    customServerDirectives: directives ?? [...(cloned.customServerDirectives ?? [])],
  };
}

export function validateFacilityDraft(draft: FacilityDraftUi): FieldErrors {
  const errors: FieldErrors = {};
  if (!draft.deploymentServers.length) errors.deploymentServers = 'applicationsPage.validationGatewayServers';
  if (draft.domains.some((domain) => !domain.domain.trim())) errors.domains = 'applicationsPage.validationDomains';
  if (draft.domains.some((domain) => !domain.originServerIds.length || domain.originServerIds.some((id) => !draft.deploymentServers.includes(id)))) errors.originServers = 'applicationsPage.validationOriginServers';
  if (draft.domains.some((domain) => !domain.paths.length)) errors.paths = 'applicationsPage.validationPaths';
  if (draft.domains.some((domain) => !isValidDomainHttpPortMode(domain.httpPortMode))) errors.httpPortMode = 'applicationsPage.validationHttpPortMode';
  if (draft.domains.some((domain) => !isValidDomainClientMaxBodySize(domain.clientMaxBodySize))) errors.clientMaxBodySize = 'applicationsPage.transport.clientMaxBodySizeInvalid';
  const directiveError = firstCustomDirectiveError(draft.domains.flatMap((domain) => domain.customServerDirectives ?? []), 'server');
  if (directiveError) errors.customDirectives = directiveError;
  return errors;
}

function firstCustomDirectiveError(directives: string[], scope: CustomDirectiveScope): string | null {
  return validateCustomDirectives(directives, scope).errors[0]?.key ?? null;
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
  if (!base) return { added: input.deploymentServers.length + input.domains.length, changed: 0, removed: 0, warnings: 0 };
  const baseInput = facilitySaveInputFromDraft(facilityDraftFromConfig(base));
  const baseDomains = new Map(baseInput.domains.map((domain) => [domain.domain, domain]));
  const inputDomains = new Map(input.domains.map((domain) => [domain.domain, domain]));
  const baseServers = new Set(baseInput.deploymentServers);
  const inputServers = new Set(input.deploymentServers);
  return {
    added: [...inputDomains.keys()].filter((domain) => !baseDomains.has(domain)).length
      + [...inputServers].filter((server) => !baseServers.has(server)).length,
    removed: [...baseDomains.keys()].filter((domain) => !inputDomains.has(domain)).length
      + [...baseServers].filter((server) => !inputServers.has(server)).length,
    changed: [...inputDomains].filter(([domain, next]) => {
      const previous = baseDomains.get(domain);
      return previous !== undefined && JSON.stringify(next) !== JSON.stringify(previous);
    }).length,
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
  const counts = applicationNodeCounts({ id: '', name: '', enabled: true, jobId: '', namespace: '', imageUpdateAvailable: false, updatedAt: '' }, runtime);
  return {
    total: runtime?.instances?.length ?? 0,
    healthy: counts.healthy,
    abnormal: counts.abnormal,
    deploying: counts.deploying,
    // Legacy aliases kept for transitional call sites.
    failed: counts.abnormal,
    running: counts.healthy,
  };
}

export function makeKeyValueRow(key = '', value = ''): KeyValueRow {
  return { id: makeId('kv'), key, value };
}

export function makePortRow(): PortRow {
  return { id: makeId('port'), label: '', to: '', staticPort: '', protocol: 'tcp', openFirewall: false };
}

export function makeMountRow(type = 'persistent'): MountRow {
  return { id: makeId('mount'), type, source: '', target: '', readOnly: type === 'file' || type === 'panel_file', mode: '' };
}

/* ------------------------------------------------------------------------- */
/* Per-domain transport switches (HTTPS / HTTP2 / HTTP3) and custom nginx     */
/* directives shared by facility domain records and application proxy rules.   */
/* ------------------------------------------------------------------------- */

export interface DomainTransportSwitches {
  httpsEnabled: boolean;
  http2Enabled: boolean;
  http3Enabled: boolean;
}

/** Raw switch values with backend defaults (HTTPS on, H2 on, H3 off). */
export function domainTransportSwitches(domain: { httpsEnabled?: boolean; http2Enabled?: boolean; http3Enabled?: boolean }): DomainTransportSwitches {
  return {
    httpsEnabled: domain.httpsEnabled !== false,
    http2Enabled: domain.http2Enabled !== false,
    http3Enabled: domain.http3Enabled === true,
  };
}

/**
 * Effective switches for rendering/saving: H2/H3 only apply when HTTPS is on
 * (the backend renders them ignored otherwise). This keeps the saved payload
 * self-consistent when the user turns HTTPS off while H2/H3 were enabled.
 */
export function effectiveDomainTransport(domain: { httpsEnabled?: boolean; http2Enabled?: boolean; http3Enabled?: boolean }): DomainTransportSwitches {
  const switches = domainTransportSwitches(domain);
  return {
    httpsEnabled: switches.httpsEnabled,
    http2Enabled: switches.httpsEnabled && switches.http2Enabled,
    http3Enabled: switches.httpsEnabled && switches.http3Enabled,
  };
}

/* ------------------------------------------------------------------------- */
/* Per-domain port 80 policy (inherit / serve / redirect) shared by facility   */
/* domain records and application reverse-proxy rules.                        */
/* ------------------------------------------------------------------------- */

export type DomainHttpPortMode = '' | 'serve' | 'redirect';

/** Normalized port 80 override: '' (inherit facility settings), 'serve', or 'redirect'. */
export function domainHttpPortMode(domain: { httpPortMode?: string } | null | undefined): DomainHttpPortMode {
  return domain?.httpPortMode === 'serve' || domain?.httpPortMode === 'redirect' ? domain.httpPortMode : '';
}

export function isValidDomainHttpPortMode(value: unknown): value is DomainHttpPortMode {
  return value === undefined || value === '' || value === 'serve' || value === 'redirect';
}

/* ------------------------------------------------------------------------- */
/* Per-domain HSTS and request-body limit overrides shared by facility domain  */
/* records and application reverse-proxy rules.                               */
/* ------------------------------------------------------------------------- */

/** Tri-state HSTS override; the backend persists it as ''/on/off. */
export type DomainHstsMode = '' | 'on' | 'off';

/** Normalized HSTS override: '' (inherit facility setting), 'on', or 'off'. */
export function domainHstsMode(domain: { hstsEnabled?: boolean } | null | undefined): DomainHstsMode {
  if (domain?.hstsEnabled === true) return 'on';
  if (domain?.hstsEnabled === false) return 'off';
  return '';
}

/** Payload value for the tri-state selector; undefined means inherit and omits the field. */
export function hstsEnabledFromMode(mode: DomainHstsMode): boolean | undefined {
  return mode === 'on' ? true : mode === 'off' ? false : undefined;
}

export function isValidDomainHstsMode(value: unknown): value is DomainHstsMode {
  return value === undefined || value === '' || value === 'on' || value === 'off';
}

/** Normalized per-domain request body limit; '' means inherit the facility-wide value. */
export function domainClientMaxBodySize(domain: { clientMaxBodySize?: string } | null | undefined): string {
  return (domain?.clientMaxBodySize ?? '').trim();
}

/** Empty inherits the facility-wide limit; otherwise nginx size syntax (1k..1024m). */
export function isValidDomainClientMaxBodySize(value: unknown): boolean {
  const raw = typeof value === 'string' ? value.trim() : '';
  return raw === '' || isValidNginxClientMaxBodySize(raw);
}

interface DomainTransportOverridesInput {
  httpsEnabled?: boolean;
  http2Enabled?: boolean;
  http3Enabled?: boolean;
  httpPortMode?: string;
  hstsEnabled?: boolean;
  clientMaxBodySize?: string;
}

/**
 * Applies the effective transport switches and the per-domain HSTS/body-limit
 * overrides to a save payload. Inherited HSTS is omitted entirely so the
 * backend keeps following the facility setting; an empty body limit is sent as
 * '' (inherit) per the existing backend convention.
 */
function withDomainTransportOverrides<T extends DomainTransportOverridesInput>(domain: T): T {
  const next: T & DomainTransportOverridesInput = {
    ...domain,
    ...effectiveDomainTransport(domain),
    httpPortMode: domainHttpPortMode(domain),
    clientMaxBodySize: domainClientMaxBodySize(domain),
  };
  const hsts = hstsEnabledFromMode(domainHstsMode(domain));
  if (hsts === undefined) delete next.hstsEnabled;
  else next.hstsEnabled = hsts;
  return next;
}

/* ------------------------------------------------------------------------- */
/* Entrance-proxy image rolling-update node interval (settings partition).    */
/* ------------------------------------------------------------------------- */

export const IMAGE_UPDATE_INTERVAL_MAX_SECONDS = 600;

export function isValidImageUpdateIntervalSeconds(value: unknown): value is number {
  return typeof value === 'number' && Number.isInteger(value) && value >= 0 && value <= IMAGE_UPDATE_INTERVAL_MAX_SECONDS;
}

/** Clamps an arbitrary GET value to the documented 0..600 integer contract; invalid input falls back to 0. */
export function normalizeImageUpdateIntervalSeconds(value: unknown): number {
  return isValidImageUpdateIntervalSeconds(value) ? value : 0;
}

export type CustomDirectiveScope = 'http' | 'server';

export interface CustomDirectiveError {
  /** 1-based line index in the edited text area. */
  line: number;
  key: string;
}

/** 面板保留的安全/身份指令：自定义指令不允许覆盖。 */
const CUSTOM_DIRECTIVE_DENYLIST = new Set([
  'listen', 'server_name', 'ssl_certificate', 'ssl_certificate_key', 'ssl_certificate_path',
  'include', 'load_module', 'root', 'alias', 'proxy_pass', 'upstream', 'map', 'if',
  'location', 'limit_except', 'resolver',
]);

const CUSTOM_DIRECTIVE_MAX_LINES = 64;
const CUSTOM_DIRECTIVE_MAX_LINE_BYTES = 2048;
const CUSTOM_DIRECTIVE_MAX_TOTAL_BYTES = 16384;

export function customDirectiveName(line: string): string {
  return /^[a-z][a-z0-9_]*/.exec(line.trim())?.[0] ?? '';
}

/**
 * 设施入口参数 `client_max_body_size` 的本地校验（nginx size 语法）：
 * `^\d+[kKmMgG]?$`，且换算后必须落在 1k..1024m 之间。
 */
const NGINX_SIZE_PATTERN = /^\d+[kKmMgG]?$/;
const NGINX_SIZE_MIN_BYTES = 1024;
const NGINX_SIZE_MAX_BYTES = 1024 * 1024 * 1024;

export function parseNginxSizeBytes(raw: string): number | null {
  const value = (raw ?? '').trim();
  if (!NGINX_SIZE_PATTERN.test(value)) return null;
  const match = /^(\d+)([kKmMgG]?)$/.exec(value);
  if (!match) return null;
  const amount = Number(match[1]);
  const unit = (match[2] ?? '').toLowerCase();
  const multiplier = unit === 'k' ? 1024 : unit === 'm' ? 1024 ** 2 : unit === 'g' ? 1024 ** 3 : 1;
  const bytes = amount * multiplier;
  return Number.isFinite(bytes) ? bytes : null;
}

export function isValidNginxClientMaxBodySize(raw: string): boolean {
  const bytes = parseNginxSizeBytes(raw);
  return bytes !== null && bytes >= NGINX_SIZE_MIN_BYTES && bytes <= NGINX_SIZE_MAX_BYTES;
}

/** Splits the textarea into trimmed, non-empty lines (keeps user order). */
export function parseCustomDirectivesText(text: string): string[] {
  return (text ?? '').split(/\r?\n/).map((line) => line.trim()).filter(Boolean);
}

export function formatCustomDirectives(directives: string[] | undefined): string {
  return (directives ?? []).join('\n');
}

/** 单行校验：结构/禁项/长度。返回 i18n key，合法返回 null。 */
export function validateCustomDirectiveLine(line: string, scope: CustomDirectiveScope): string | null {
  const raw = (line ?? '').trim();
  if (!raw) return null;
  // 只允许单行指令：禁止块、注释、多指令与换行。
  if (/[{}\r\n\0#]/.test(raw)) return 'applicationsPage.customDirectives.invalidSyntax';
  const body = raw.endsWith(';') ? raw.slice(0, -1).trim() : raw;
  if (!body || body.includes(';')) return 'applicationsPage.customDirectives.invalidSyntax';
  const name = customDirectiveName(body);
  if (!name) return 'applicationsPage.customDirectives.invalidSyntax';
  if (CUSTOM_DIRECTIVE_DENYLIST.has(name)) {
    return scope === 'http'
      ? 'applicationsPage.customDirectives.forbiddenHttp'
      : 'applicationsPage.customDirectives.forbiddenServer';
  }
  if (new TextEncoder().encode(raw).length > CUSTOM_DIRECTIVE_MAX_LINE_BYTES) return 'applicationsPage.customDirectives.lineTooLong';
  return null;
}

export interface CustomDirectivesValidation {
  errors: CustomDirectiveError[];
  normalized: string[];
}

/**
 * 归一化：trim、丢弃空行、缺失 `;` 自动补齐、同名指令保留最后一条（面板按“同名覆盖”
 * 只输出一条），并校验行数/单行/总长度上限。
 */
export function validateCustomDirectives(directives: string[], scope: CustomDirectiveScope): CustomDirectivesValidation {
  const errors: CustomDirectiveError[] = [];
  const normalizedByLine: Array<{ name: string; value: string }> = [];
  let totalBytes = 0;
  (directives ?? []).forEach((raw, index) => {
    const line = (raw ?? '').trim();
    if (!line) return;
    const key = validateCustomDirectiveLine(line, scope);
    if (key) {
      errors.push({ line: index + 1, key });
      return;
    }
    const value = line.endsWith(';') ? line : `${line};`;
    totalBytes += new TextEncoder().encode(value).length;
    normalizedByLine.push({ name: customDirectiveName(value), value });
  });
  if (directives.length > CUSTOM_DIRECTIVE_MAX_LINES) errors.push({ line: 1, key: 'applicationsPage.customDirectives.tooManyLines' });
  if (totalBytes > CUSTOM_DIRECTIVE_MAX_TOTAL_BYTES) errors.push({ line: 1, key: 'applicationsPage.customDirectives.totalTooLong' });
  // 同名覆盖：保留最后一条，按首次出现的位置输出。
  const lastByName = new Map<string, string>();
  const order: string[] = [];
  for (const item of normalizedByLine) {
    if (!lastByName.has(item.name)) order.push(item.name);
    lastByName.set(item.name, item.value);
  }
  return { errors, normalized: order.map((name) => lastByName.get(name)!).filter(Boolean) };
}

/** 保存用归一化：非法输入返回 null（由调用方展示行级错误并阻止保存）。 */
export function normalizedCustomDirectives(directives: string[] | undefined, scope: CustomDirectiveScope): string[] | null {
  const result = validateCustomDirectives(directives ?? [], scope);
  return result.errors.length ? null : result.normalized;
}

export function makeProxyRule(): ReverseProxyRule {
  return {
    domain: '',
    targetPort: 0,
    originServerIds: [],
    anyAccess: { enabled: false, strategy: '', originPriority: [], relayServerIds: [] },
    paths: [],
    httpsEnabled: true,
    http2Enabled: true,
    http3Enabled: false,
    httpPortMode: '',
    clientMaxBodySize: '',
    customServerDirectives: [],
  };
}

export function makeProxyPath(): ReverseProxyPath {
  return { path: '', options: defaultRouteOptions() };
}

export function cloneProxyPath(path: ReverseProxyPath): ReverseProxyPath {
  return {
    path: path.path || '/',
    options: {
      ...defaultRouteOptions(),
      ...(path.options ?? {}),
      requestHeaders: (path.options?.requestHeaders ?? []).map((header) => ({ ...header })),
      responseHeaders: (path.options?.responseHeaders ?? []).map((header) => ({ ...header })),
    },
  };
}

export function makeFacilityDomain(): FacilityRouteDomain {
  return {
    domain: '',
    originServerIds: [],
    anyAccess: { enabled: false, strategy: 'round_robin', relayServerIds: [] },
    paths: [makeFacilityPath()],
    httpsEnabled: true,
    http2Enabled: true,
    http3Enabled: false,
    httpPortMode: '',
    clientMaxBodySize: '',
    customServerDirectives: [],
  };
}

export function makeFacilityPath(type: StaticRuleType = 'static'): FacilityRoutePath {
  // sourceType 只对静态规则有意义；redirect/proxy_pass 不带默认值，避免
  // 后端资产引用校验把非静态路由误判为引用了已删除资产。
  return { path: '', ruleType: type, sourceType: type === 'static' ? 'uploaded_file' : '', assetName: '', redirectUrl: '', redirectCode: 0, proxyUrl: '', proxySourceMode: '', options: defaultRouteOptions() };
}

export function cloneProxyRules(rules: ReverseProxyRule[]) {
  return rules.map((rule): ReverseProxyRule => {
    const switches = domainTransportSwitches(rule);
    const cloned: ReverseProxyRule = {
      domain: rule.domain,
      targetPort: Number(rule.targetPort || 0),
      originServerIds: [...(rule.originServerIds ?? [])],
      anyAccess: { enabled: Boolean(rule.anyAccess?.enabled), strategy: rule.anyAccess?.strategy || '', originPriority: [...(rule.anyAccess?.originPriority ?? [])], relayServerIds: [...(rule.anyAccess?.relayServerIds ?? [])] },
      paths: (rule.paths ?? []).map((path) => cloneProxyPath(path)),
      ...switches,
      httpPortMode: domainHttpPortMode(rule),
      clientMaxBodySize: domainClientMaxBodySize(rule),
      customServerDirectives: [...(rule.customServerDirectives ?? [])],
    };
    const hsts = hstsEnabledFromMode(domainHstsMode(rule));
    if (hsts !== undefined) cloned.hstsEnabled = hsts;
    return cloned;
  });
}

export function normalizeFacilityPath(path: FacilityRoutePath): FacilityRoutePath {
  // proxy_pass 是设施 Path 的一等规则类型（后端完整支持并渲染），必须原样保留；
  // 遗留 host_path 静态来源（该功能已整体移除）在加载时降级为 uploaded_file，
  // 用户需重新选择静态文件。
  const ruleType: StaticRuleType = (path.ruleType as StaticRuleType) || 'static';
  const sourceType: StaticSourceType = path.sourceType === 'host_path' ? 'uploaded_file' : (path.sourceType as StaticSourceType) || 'uploaded_file';
  const normalized: FacilityRoutePath = {
    ...makeFacilityPath(ruleType),
    ...path,
    ruleType,
    sourceType,
    options: { ...defaultRouteOptions(), ...(path.options ?? {}) },
  };
  if (normalized.ruleType !== 'proxy_pass') {
    normalized.proxyUrl = '';
    normalized.proxySourceMode = '';
  }
  if (normalized.ruleType !== 'redirect') {
    normalized.redirectUrl = '';
    normalized.redirectCode = 0;
  }
  if (normalized.ruleType !== 'static') {
    normalized.sourceType = '';
    normalized.assetName = '';
  }
  return normalized;
}

export function cloneFacilityDomains(domains: FacilityRouteDomain[]) {
  return domains.map((domain) => cloneFacilityDomain(domain));
}

/** Deep-clones one domain (including the owning record version) for a single-record editor. */
export function cloneFacilityDomain(domain: FacilityRouteDomain): FacilityRouteDomain {
  const switches = domainTransportSwitches(domain);
  const cloned: FacilityRouteDomain = {
    domain: domain.domain ?? '',
    originServerIds: [...(domain.originServerIds ?? [])],
    anyAccess: {
      enabled: Boolean(domain.anyAccess?.enabled),
      strategy: domain.anyAccess?.strategy || 'round_robin',
      primaryOriginServerId: domain.anyAccess?.primaryOriginServerId || '',
      relayServerIds: [...(domain.anyAccess?.relayServerIds ?? [])],
    },
    paths: (domain.paths ?? []).map((path) => normalizeFacilityPath(path)),
    ...switches,
    httpPortMode: domainHttpPortMode(domain),
    clientMaxBodySize: domainClientMaxBodySize(domain),
    customServerDirectives: [...(domain.customServerDirectives ?? [])],
  };
  const hsts = hstsEnabledFromMode(domainHstsMode(domain));
  if (hsts !== undefined) cloned.hstsEnabled = hsts;
  if (typeof domain.version === 'number') cloned.version = domain.version;
  return cloned;
}

/** Seeds the single-record editor draft from one committed config record. */
export function facilityRecordDraftFromRecord(record: ConfigRecord): FacilityRouteDomain {
  const draft = cloneFacilityDomain(record.domain);
  if (!draft.domain) draft.domain = record.recordId;
  draft.version = record.domain?.version ?? record.version;
  return draft;
}

/** Blank draft for a brand-new facility domain record: empty domain + one default static path. */
export function makeNewFacilityDomainRecord(): FacilityRouteDomain {
  return makeFacilityDomain();
}

/**
 * Validates one config-record draft against the current proxy-plane gateways.
 * Keys: domain / originServers / primaryOrigin / paths / asset / redirectUrl / proxyUrl.
 */
export function validateFacilityRecordDraft(draft: FacilityRouteDomain, gateways: string[]): FieldErrors {
  const errors: FieldErrors = {};
  const value = (draft.domain ?? '').trim().toLowerCase();
  if (!value || /[\s;]/.test(value)) {
    errors.domain = 'applicationsPage.validationDomain';
  }
  const origins = draft.originServerIds ?? [];
  const gatewaySet = new Set(gateways ?? []);
  if (!origins.length || origins.some((serverId) => !gatewaySet.has(serverId))) {
    errors.originServers = 'applicationsPage.validationOriginServers';
  }
  if (draft.anyAccess?.enabled) {
    const primary = (draft.anyAccess.primaryOriginServerId ?? '').trim();
    if (primary && !origins.includes(primary)) {
      errors.primaryOrigin = 'applicationsPage.validationPrimaryOrigin';
    }
    const relays = draft.anyAccess.relayServerIds ?? [];
    if (relays.some((serverId) => !gatewaySet.has(serverId) || origins.includes(serverId))) {
      errors.originServers = 'applicationsPage.validationOriginServers';
    }
  }
  const paths = draft.paths ?? [];
  if (!paths.length) {
    errors.paths = 'applicationsPage.validationPaths';
  }
  for (const path of paths) {
    Object.assign(errors, validateFacilityPathFields(path));
  }
  if (!isValidDomainHttpPortMode(draft.httpPortMode)) {
    errors.httpPortMode = 'applicationsPage.validationHttpPortMode';
  }
  if (!isValidDomainClientMaxBodySize(draft.clientMaxBodySize)) {
    errors.clientMaxBodySize = 'applicationsPage.transport.clientMaxBodySizeInvalid';
  }
  const directiveError = firstCustomDirectiveError(draft.customServerDirectives ?? [], 'server');
  if (directiveError) errors.customDirectives = directiveError;
  return errors;
}

/** Path-granularity summary for the single-record editor (matched by path.path). */
export function diffFacilityRecord(base?: FacilityRouteDomain | null, draft?: FacilityRouteDomain | null): PreviewDiff {
  if (!draft) return { added: 0, changed: 0, removed: 0, warnings: 0 };
  const nextPaths = new Map<string, string>();
  for (const path of draft.paths ?? []) {
    const normalized = normalizeFacilityPath(path);
    nextPaths.set(normalized.path, JSON.stringify(normalized));
  }
  if (!base) return { added: nextPaths.size, changed: 0, removed: 0, warnings: 0 };
  const basePaths = new Map<string, string>();
  for (const path of base.paths ?? []) {
    const normalized = normalizeFacilityPath(path);
    basePaths.set(normalized.path, JSON.stringify(normalized));
  }
  let added = 0;
  let changed = 0;
  let removed = 0;
  for (const [pathValue, next] of nextPaths) {
    const previous = basePaths.get(pathValue);
    if (previous === undefined) added += 1;
    else if (previous !== next) changed += 1;
  }
  for (const pathValue of basePaths.keys()) {
    if (!nextPaths.has(pathValue)) removed += 1;
  }
  // 传输开关 / 自定义 server 指令变化即使路径不变也必须计入“已修改”。
  const transportChanged = JSON.stringify(domainTransportSignature(base)) !== JSON.stringify(domainTransportSignature(draft));
  if (transportChanged && added === 0 && changed === 0 && removed === 0) changed = 1;
  return { added, changed, removed, warnings: 0 };
}

function domainTransportSignature(domain: FacilityRouteDomain) {
  return {
    ...effectiveDomainTransport(domain),
    httpPortMode: domainHttpPortMode(domain),
    hstsMode: domainHstsMode(domain),
    clientMaxBodySize: domainClientMaxBodySize(domain),
    directives: normalizedCustomDirectives(domain.customServerDirectives, 'server') ?? [...(domain.customServerDirectives ?? [])],
  };
}

export function cloneFacilityPath(path: FacilityRoutePath) {
  return normalizeFacilityPath(path);
}

export function defaultRouteOptions(): HttpRouteOptions {
  return { gzipMode: 'inherit', clientMaxBodySizeMb: 0, connectTimeoutSeconds: 0, readTimeoutSeconds: 0, sendTimeoutSeconds: 0, bufferingMode: 'inherit', webSocketMode: 'off', requestHeaders: [], responseHeaders: [] };
}

function uncoveredSpecFrom(parsed: Record<string, unknown>): Record<string, unknown> {
  const uncovered: Record<string, unknown> = { ...parsed };
  delete uncovered.name;
  delete uncovered.image;
  delete uncovered.command;
  delete uncovered.env;
  delete uncovered.ports;
  delete uncovered.mounts;
  delete uncovered.privileged;
  delete uncovered.networkMode;
  const resources = objectValue(parsed.resources);
  if (resources) {
    const rest = { ...resources };
    delete rest.cpu;
    delete rest.memoryMb;
    if (Object.keys(rest).length) uncovered.resources = rest;
    else delete uncovered.resources;
  }
  return uncovered;
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
