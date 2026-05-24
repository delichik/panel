export interface ServiceVisualModel {
  image: string;
  restart?: string;
  ports?: string[];
  volumes?: string[];
  environment?: Record<string, string>;
  labels?: Record<string, string>;
  dependsOn?: string[];
  networkMode?: string;
  command?: string | string[];
}

export function visualToServiceBodyYaml(service: ServiceVisualModel) {
  const lines: string[] = [];
  if (service.image) lines.push(`image: ${quoteYaml(service.image)}`);
  if (service.restart) lines.push(`restart: ${quoteYaml(service.restart)}`);
  if (service.networkMode) lines.push(`network_mode: ${quoteYaml(service.networkMode)}`);
  if (Array.isArray(service.command)) appendList(lines, 'command', service.command);
  else if (service.command) lines.push(`command: ${quoteYaml(service.command)}`);
  appendList(lines, 'ports', service.ports);
  appendList(lines, 'volumes', service.volumes);
  appendList(lines, 'depends_on', service.dependsOn);
  appendMap(lines, 'environment', service.environment);
  appendMap(lines, 'labels', service.labels);
  return `${lines.join('\n')}\n`;
}

export function serviceBodyYamlToVisual(yaml: string): ServiceVisualModel {
  return {
    image: scalarValue(yaml, 'image') || '',
    restart: scalarValue(yaml, 'restart') || undefined,
    networkMode: scalarValue(yaml, 'network_mode') || undefined,
    command: listValues(yaml, 'command').length ? listValues(yaml, 'command') : scalarValue(yaml, 'command') || undefined,
    ports: listValues(yaml, 'ports'),
    volumes: listValues(yaml, 'volumes'),
    dependsOn: dependsOnValues(yaml),
    environment: mapValues(yaml, 'environment'),
    labels: mapValues(yaml, 'labels'),
  };
}

function scalarValue(yaml: string, key: string) {
  const match = yaml.match(new RegExp(`^${key}:\\s*["']?([^"'\\n]+)["']?`, 'm'));
  return match?.[1]?.trim() ?? '';
}

function listValues(yaml: string, key: string) {
  const block = blockLines(yaml, key);
  return block
    .map((line) => line.match(/^\s*-\s*["']?([^"'\n]+)["']?/)?.[1]?.trim() ?? '')
    .filter(Boolean);
}

function dependsOnValues(yaml: string) {
  const shortSyntax = listValues(yaml, 'depends_on');
  if (shortSyntax.length) return shortSyntax;
  const block = blockLines(yaml, 'depends_on');
  return block
    .map((line) => line.match(/^\s{2}([a-z0-9-]+):/)?.[1] ?? '')
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

function quoteYaml(value: string, forceQuote = false) {
  if (!value) return '""';
  if (forceQuote || /[#{}[\],&*?|<>=!%@`]/.test(value) || value.includes('{{') || value.includes(' ')) {
    return JSON.stringify(value);
  }
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
  entries.forEach(([name, value]) => lines.push(`  ${name}: ${quoteYaml(String(value), key === 'labels')}`));
}
