import { parse, stringify } from 'yaml';

export function parseSpecYaml(raw: string): Record<string, unknown> | null {
  try {
    return objectValue(parse(raw));
  } catch {
    return null;
  }
}

export function toSpecYaml(value: unknown): string {
  const text = stringify(compactYamlValue(value), {
    indent: 2,
    lineWidth: 0,
  }).trimEnd();
  return `${text}\n`;
}

function compactYamlValue(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value
      .map((item) => compactYamlValue(item))
      .filter((item) => shouldEmit(item));
  }
  if (!isPlainObject(value)) return value;
  return Object.fromEntries(
    Object.entries(value)
      .map(([key, child]) => [key, compactYamlValue(child)] as const)
      .filter(([, child]) => shouldEmit(child)),
  );
}

function shouldEmit(value: unknown) {
  if (Array.isArray(value)) return value.length > 0;
  if (isPlainObject(value)) return Object.keys(value).length > 0;
  return value !== undefined && value !== null && value !== '';
}

function objectValue(value: unknown): Record<string, unknown> | null {
  return isPlainObject(value) ? value : null;
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value);
}
