import type { ServerArchitecture, ServerOsRelease } from '@/types/servers';
import { osMarks, type OsMarkDefinition } from './osMarkData';

/**
 * Server platform identification marks.
 *
 * The detailed server DTO already carries the detected distribution (`os.id` /
 * `os.prettyName`) and machine architecture (`architecture.arch` /
 * `architecture.rawMachine`); no extra request or backend field is needed.
 * These helpers only map those stable values to a local vendored mark. Unknown
 * distributions stay unknown (`null` → neutral fallback) rather than guessing a
 * brand from free-form text, and marks never imply support or health.
 */

/** `os.id` aliases and upstream derivatives → vendored mark key. */
const osAliases: Record<string, string> = {
  ubuntu: 'ubuntu',
  pop: 'ubuntu',
  debian: 'debian',
  arch: 'archlinux',
  archlinux: 'archlinux',
  endeavouros: 'archlinux',
  manjaro: 'manjaro',
  manjaroarm: 'manjaro',
  fedora: 'fedora',
  centos: 'centos',
  rhel: 'redhat',
  redhat: 'redhat',
  almalinux: 'almalinux',
  rocky: 'rockylinux',
  rockylinux: 'rockylinux',
  alpine: 'alpinelinux',
  alpinelinux: 'alpinelinux',
  opensuse: 'opensuse',
  'opensuse-leap': 'opensuse',
  'opensuse-tumbleweed': 'opensuse',
  sles: 'opensuse',
  suse: 'opensuse',
  mint: 'linuxmint',
  linuxmint: 'linuxmint',
  kali: 'kalilinux',
  kalilinux: 'kalilinux',
  raspbian: 'raspberrypi',
  'raspberry-pi-os': 'raspberrypi',
  raspberrypi: 'raspberrypi',
  gentoo: 'gentoo',
  nixos: 'nixos',
};

/** `prettyName` fallbacks, used only when `os.id` is missing or unknown. */
const prettyNameHints: Array<[RegExp, string]> = [
  [/ubuntu/i, 'ubuntu'],
  [/debian/i, 'debian'],
  [/arch\s*linux|\barch\b/i, 'archlinux'],
  [/fedora/i, 'fedora'],
  [/centos/i, 'centos'],
  [/red\s*hat|\brhel\b/i, 'redhat'],
  [/almalinux/i, 'almalinux'],
  [/rocky\s*linux/i, 'rockylinux'],
  [/alpine/i, 'alpinelinux'],
  [/opensuse|suse/i, 'opensuse'],
  [/linux\s*mint/i, 'linuxmint'],
  [/kali/i, 'kalilinux'],
  [/manjaro/i, 'manjaro'],
  [/raspbian|raspberry\s*pi/i, 'raspberrypi'],
  [/gentoo/i, 'gentoo'],
  [/nixos/i, 'nixos'],
];

function normalizeOsId(value: string) {
  return value.trim().toLowerCase().replace(/[\s_]+/g, '-');
}

/** Resolve a distro mark for a server; `null` means "unknown distribution". */
export function resolveOsMark(os?: Pick<ServerOsRelease, 'id' | 'prettyName'> | null): OsMarkDefinition | null {
  if (!os) return null;
  const id = normalizeOsId(os.id ?? '');
  if (id) {
    const key = osAliases[id];
    if (key) return osMarks[key] ?? null;
  }
  const prettyName = os.prettyName?.trim() ?? '';
  for (const [pattern, key] of prettyNameHints) {
    if (pattern.test(prettyName)) return osMarks[key] ?? null;
  }
  // A recognised Linux release with an unrecognised vendor still gets the generic
  // Tux mark, but a completely unknown OS keeps the neutral placeholder.
  if (/linux|gnu/i.test(prettyName)) return osMarks.linux ?? null;
  return null;
}

export type ArchitectureFamily = 'x86' | 'arm64' | 'arm' | 'riscv' | 's390x' | 'ppc' | 'generic';

export interface ArchitectureMarkInfo {
  family: ArchitectureFamily;
  /** Compact chip label: normalized arch first, then the raw `uname -m` value. */
  label: string;
  /** Full readable label (`amd64 (x86_64)`) for tooltips/accessible names. */
  title: string;
}

const architectureFamilies: Array<[RegExp, ArchitectureFamily]> = [
  [/^(x86[_-]?64|amd64|x64|i[3-6]86|i86pc|386|x86)$/i, 'x86'],
  [/^(aarch64|arm64|armv8.*)$/i, 'arm64'],
  [/^(arm|armv[4-7].*|armhf|armel)$/i, 'arm'],
  [/^riscv(32|64)?$/i, 'riscv'],
  [/^s390x?$/i, 's390x'],
  [/^(ppc|ppc64|ppc64le|powerpc.*)$/i, 'ppc'],
];

function architectureFamily(value: string): ArchitectureFamily {
  const normalized = value.trim().toLowerCase();
  if (!normalized) return 'generic';
  for (const [pattern, family] of architectureFamilies) {
    if (pattern.test(normalized)) return family;
  }
  return 'generic';
}

/** Full readable label: `arch` preferred, `rawMachine` only when it adds information. */
function architectureTitle(arch: string, rawMachine: string) {
  if (arch && rawMachine && arch !== rawMachine) return `${arch} (${rawMachine})`;
  return arch || rawMachine;
}

/** Resolve the architecture chip; `null` when the server has no architecture yet. */
export function resolveArchitectureMark(architecture?: ServerArchitecture | null): ArchitectureMarkInfo | null {
  const arch = architecture?.arch?.trim() ?? '';
  const rawMachine = architecture?.rawMachine?.trim() ?? '';
  const primary = arch || rawMachine;
  if (!primary) return null;
  return {
    family: architectureFamily(primary),
    label: primary,
    title: architectureTitle(arch, rawMachine),
  };
}
