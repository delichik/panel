import { describe, expect, it } from 'vitest';
import { resolveArchitectureMark, resolveOsMark } from './osMark';

describe('resolveOsMark', () => {
  it('maps stable os-release IDs to vendored distribution marks', () => {
    const debian = resolveOsMark({ id: 'debian', prettyName: 'Debian GNU/Linux 13' });
    expect(debian?.key).toBe('debian');
    expect(debian?.color).toBe('#A81D33');

    expect(resolveOsMark({ id: 'ubuntu', prettyName: 'Ubuntu 24.04.1 LTS' })?.key).toBe('ubuntu');
    // Arch Linux uses the archlinux slug; upstream derivatives reuse it.
    expect(resolveOsMark({ id: 'arch', prettyName: 'Arch Linux' })?.key).toBe('archlinux');
    expect(resolveOsMark({ id: 'manjaro', prettyName: 'Manjaro Linux' })?.key).toBe('manjaro');
    expect(resolveOsMark({ id: 'rocky', prettyName: 'Rocky Linux 9.4' })?.key).toBe('rockylinux');
    expect(resolveOsMark({ id: 'rhel', prettyName: 'Red Hat Enterprise Linux 9.4' })?.key).toBe('redhat');
  });

  it('falls back to prettyName hints when the ID is missing or unknown', () => {
    expect(resolveOsMark({ prettyName: 'Fedora Linux 40 (Server Edition)' })?.key).toBe('fedora');
    expect(resolveOsMark({ prettyName: 'Linux Mint 22' })?.key).toBe('linuxmint');
    expect(resolveOsMark({ id: 'some-vendor', prettyName: 'Oracle Linux Server 9.4' })?.key).toBe('linux');
  });

  it('does not invent a distribution for unknown or missing OS data', () => {
    expect(resolveOsMark(undefined)).toBeNull();
    expect(resolveOsMark(null)).toBeNull();
    expect(resolveOsMark({})).toBeNull();
    expect(resolveOsMark({ id: 'windows', prettyName: 'Windows Server 2022' })).toBeNull();
  });
});

describe('resolveArchitectureMark', () => {
  it('normalizes arch families and keeps a readable label', () => {
    expect(resolveArchitectureMark({ arch: 'amd64', rawMachine: 'x86_64' })).toMatchObject({
      family: 'x86',
      label: 'amd64',
      title: 'amd64 (x86_64)',
    });
    expect(resolveArchitectureMark({ arch: 'arm64', rawMachine: 'aarch64' })).toMatchObject({
      family: 'arm64',
      label: 'arm64',
      title: 'arm64 (aarch64)',
    });
    expect(resolveArchitectureMark({ rawMachine: 'armv7l' })).toMatchObject({ family: 'arm', label: 'armv7l' });
    expect(resolveArchitectureMark({ rawMachine: 'riscv64' })).toMatchObject({ family: 'riscv', label: 'riscv64' });
    expect(resolveArchitectureMark({ arch: 's390x' })?.family).toBe('s390x');
    expect(resolveArchitectureMark({ arch: 'ppc64le' })?.family).toBe('ppc');
  });

  it('returns null until the server reports an architecture', () => {
    expect(resolveArchitectureMark(undefined)).toBeNull();
    expect(resolveArchitectureMark(null)).toBeNull();
    expect(resolveArchitectureMark({})).toBeNull();
    // Unknown machine types still get the generic chip rather than a wrong family.
    expect(resolveArchitectureMark({ rawMachine: 'loongarch64' })?.family).toBe('generic');
  });
});
