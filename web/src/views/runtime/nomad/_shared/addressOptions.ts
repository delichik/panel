export type NomadAddressOptionSource = "current" | "ssh" | "interface";

export interface NomadAddressTarget {
  host?: string | null;
  traits?: Record<string, string> | null;
}

export interface NomadAddressOption {
  source: NomadAddressOptionSource;
  value: string;
  name?: string;
}

const advertiseTrait = "nomad.advertise_address";
const legacyAdvertiseTrait = "nomad.server_advertise_address";
const networkInterfacesTrait = "sys.network_interfaces";

export function buildNomadAddressOptions(target?: NomadAddressTarget | null) {
  const options: NomadAddressOption[] = [];
  const seen = new Set<string>();

  const add = (source: NomadAddressOptionSource, raw?: string | null, name?: string) => {
    const value = normalizeIp(raw);
    if (!value || seen.has(value)) return;
    seen.add(value);
    options.push({ source, value, name });
  };

  const traits = target?.traits ?? {};
  add("current", traits[advertiseTrait] || traits[legacyAdvertiseTrait]);
  add("ssh", target?.host);

  for (const raw of (traits[networkInterfacesTrait] || "").split(/\s*,\s*/)) {
    const [name, family, cidr] = raw.split("|");
    const address = (cidr || "").split("/")[0];
    if (!name || !["inet", "inet6"].includes(family)) continue;
    add("interface", address, name);
  }

  return options;
}

function normalizeIp(raw?: string | null) {
  let value = (raw || "").trim();
  if (value.startsWith("[") && value.endsWith("]")) {
    value = value.slice(1, -1);
  }
  if (isUsableIPv4(value) || isUsableIPv6(value)) return value;
  return "";
}

function isUsableIPv4(value: string) {
  const parts = value.split(".");
  if (parts.length !== 4) return false;
  const octets = parts.map((part) => Number(part));
  if (octets.some((octet, index) => !Number.isInteger(octet) || octet < 0 || octet > 255 || parts[index] === "")) {
    return false;
  }
  if (octets[0] === 127) return false;
  return value !== "0.0.0.0";
}

function isUsableIPv6(value: string) {
  const lower = value.toLowerCase();
  if (!lower.includes(":")) return false;
  if (lower === "::" || lower === "::1") return false;
  return /^[0-9a-f:]+$/.test(lower) && /[0-9a-f]/.test(lower);
}
