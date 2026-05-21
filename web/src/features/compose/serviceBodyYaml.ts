import type { ComposeVisualServiceDto } from '@/types/api';

export function visualToServiceBodyYaml(service: ComposeVisualServiceDto) {
  const lines: string[] = [];
  if (service.image) lines.push(`image: ${quoteYaml(service.image)}`);
  if (service.build) lines.push(`build: ${quoteYaml(service.build)}`);
  if (service.restart) lines.push(`restart: ${quoteYaml(service.restart)}`);
  if (service.command) lines.push(`command: ${quoteYaml(service.command)}`);
  if (service.entrypoint) lines.push(`entrypoint: ${quoteYaml(service.entrypoint)}`);
  if (service.workingDir) lines.push(`working_dir: ${quoteYaml(service.workingDir)}`);
  if (service.user) lines.push(`user: ${quoteYaml(service.user)}`);
  if (service.hostname) lines.push(`hostname: ${quoteYaml(service.hostname)}`);
  if (service.networkMode) lines.push(`network_mode: ${quoteYaml(service.networkMode)}`);
  if (service.pullPolicy) lines.push(`pull_policy: ${quoteYaml(service.pullPolicy)}`);
  if (service.stopGracePeriod) lines.push(`stop_grace_period: ${quoteYaml(service.stopGracePeriod)}`);
  if (service.privileged) lines.push('privileged: true');
  if (service.init) lines.push('init: true');
  appendList(lines, 'ports', service.ports);
  appendList(lines, 'volumes', service.volumes);
  appendMap(lines, 'environment', service.environment);
  appendMap(lines, 'labels', service.labels);
  appendList(lines, 'env_file', service.envFile);
  appendList(lines, 'networks', service.networks);
  appendList(lines, 'extra_hosts', service.extraHosts);
  appendList(lines, 'dns', service.dns);
  appendList(lines, 'dns_search', service.dnsSearch);
  if (service.healthcheckTest) {
    lines.push('healthcheck:');
    lines.push(`  test: ${quoteYaml(service.healthcheckTest)}`);
    if (service.healthcheckInterval) lines.push(`  interval: ${quoteYaml(service.healthcheckInterval)}`);
    if (service.healthcheckTimeout) lines.push(`  timeout: ${quoteYaml(service.healthcheckTimeout)}`);
    if (service.healthcheckRetries) lines.push(`  retries: ${service.healthcheckRetries}`);
  }
  return `${lines.join('\n')}\n`;
}

export function serviceBodyYamlToVisual(yaml: string): ComposeVisualServiceDto {
  return {
    name: 'app',
    image: scalarValue(yaml, 'image') || 'nginx:latest',
    restart: scalarValue(yaml, 'restart') || undefined,
    build: scalarValue(yaml, 'build') || undefined,
    command: scalarValue(yaml, 'command') || undefined,
    entrypoint: scalarValue(yaml, 'entrypoint') || undefined,
    workingDir: scalarValue(yaml, 'working_dir') || undefined,
    user: scalarValue(yaml, 'user') || undefined,
    hostname: scalarValue(yaml, 'hostname') || undefined,
    networkMode: scalarValue(yaml, 'network_mode') || undefined,
    pullPolicy: scalarValue(yaml, 'pull_policy') || undefined,
    stopGracePeriod: scalarValue(yaml, 'stop_grace_period') || undefined,
    privileged: scalarValue(yaml, 'privileged') === 'true',
    init: scalarValue(yaml, 'init') === 'true',
    ports: listValues(yaml, 'ports'),
    volumes: listValues(yaml, 'volumes'),
    envFile: listValues(yaml, 'env_file'),
    networks: listValues(yaml, 'networks'),
    extraHosts: listValues(yaml, 'extra_hosts'),
    dns: listValues(yaml, 'dns'),
    dnsSearch: listValues(yaml, 'dns_search'),
    environment: mapValues(yaml, 'environment'),
    labels: mapValues(yaml, 'labels'),
    healthcheckTest: nestedScalarValue(yaml, 'healthcheck', 'test') || undefined,
    healthcheckInterval: nestedScalarValue(yaml, 'healthcheck', 'interval') || undefined,
    healthcheckTimeout: nestedScalarValue(yaml, 'healthcheck', 'timeout') || undefined,
    healthcheckRetries: numberValue(nestedScalarValue(yaml, 'healthcheck', 'retries')),
  };
}

function scalarValue(yaml: string, key: string) {
  const match = yaml.match(new RegExp(`^${key}:\\s*["']?([^"'\\n]+)["']?`, 'm'));
  return match?.[1]?.trim() ?? '';
}

function nestedScalarValue(yaml: string, parent: string, key: string) {
  const match = yaml.match(new RegExp(`^${parent}:\\s*\\n(?:  .+\\n)*?  ${key}:\\s*["']?([^"'\\n]+)["']?`, 'm'));
  return match?.[1]?.trim() ?? '';
}

function numberValue(value: string) {
  if (!value) return undefined;
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : undefined;
}

function listValues(yaml: string, key: string) {
  const block = blockLines(yaml, key);
  return block
    .map((line) => line.match(/^\s*-\s*["']?([^"'\n]+)["']?/)?.[1]?.trim() ?? '')
    .filter(Boolean);
}

function mapValues(yaml: string, key: string) {
  const block = blockLines(yaml, key);
  const out: Record<string, string> = {};
  for (const line of block) {
    const match = line.match(/^\s*([^:\s]+):\s*["']?([^"'\n]*)["']?/);
    if (match?.[1]) out[match[1]] = match[2] ?? '';
  }
  return out;
}

function blockLines(yaml: string, key: string) {
  const lines = yaml.split(/\r?\n/);
  const start = lines.findIndex((line) => line.trim() === `${key}:`);
  if (start < 0) return [];
  const out: string[] = [];
  for (const line of lines.slice(start + 1)) {
    if (!line.startsWith('  ')) break;
    out.push(line);
  }
  return out;
}

function quoteYaml(value: string, forceColonQuote = false) {
	if (!value) return '""';
	if ((forceColonQuote && value.includes(':')) || /[#{}[\],&*?|<>=!%@`]/.test(value) || value.includes('{{') || value.includes(' ')) return JSON.stringify(value);
	return value;
}

function appendList(lines: string[], key: string, values?: string[]) {
  const items = Array.isArray(values) ? values.filter(Boolean) : [];
	if (!items.length) return;
	lines.push(`${key}:`);
	items.forEach((item) => lines.push(`  - ${quoteYaml(item, true)}`));
}

function appendMap(lines: string[], key: string, values?: Record<string, string>) {
  const entries = Object.entries(values ?? {}).filter(([name]) => name);
  if (!entries.length) return;
  lines.push(`${key}:`);
  entries.forEach(([name, value]) => lines.push(`  ${name}: ${quoteYaml(String(value))}`));
}
