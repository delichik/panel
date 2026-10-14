import { describe, expect, it } from 'vitest';
import {
  canMaintainPackages,
  canUseDockerResources,
  containerActionDisabled,
  filterPackages,
  imageLabel,
  imageUpgradeApplicationIds,
  packageBlockReason,
  selectedPackageNames,
} from './resourceModel';
import type { ServerDto } from '@/types/servers';
import type { ContainerDto, ImageDto } from '@/types/resources';

const server: ServerDto = {
  id: 'srv-1',
  name: 'edge',
  host: '10.0.0.1',
  port: 22,
  credentialId: 'cred-1',
  reachable: true,
  tailscaleEnabled: false,
  tailscalePreferAgent: false,
  tailscalePreferInterconnect: false,
  os: { supported: true },
  sudo: { passwordless: true },
  privilege: { privileged: true, mode: 'passwordless_sudo' },
  traits: { 'agent.status': 'compatible', 'agent.url': 'https://10.0.0.1:9786' },
};

const container: ContainerDto = {
  id: 'ctr-1',
  names: ['/web'],
  image: 'nginx',
  imageId: 'img-1',
  command: '',
  created: 0,
  state: 'running',
  status: 'Up',
  ports: [],
  labels: {},
  mounts: [],
  managed: false,
};

function image(overrides: Partial<ImageDto>): ImageDto {
  return { id: 'sha256:abcdef123456', repoTags: [], repoDigests: [], created: 0, size: 0, containers: 0, reference: '', checkable: false, updateAvailable: false, inUse: false, applicationIds: [], upgradeable: false, ...overrides };
}

describe('server resource tabs model', () => {
  it('separates package privilege from Docker Agent readiness', () => {
    expect(canMaintainPackages(server)).toBe(true);
    expect(canUseDockerResources(server)).toBe(true);
    expect(packageBlockReason({ ...server, privilege: { privileged: false, mode: 'none' }, sudo: { passwordless: false } })).toBe('resourcesPage.blockPrivilege');
  });

  it('filters package updates and extracts selected names', () => {
    const updates = [{ name: 'openssl', installedVersion: '1', candidateVersion: '2', source: 'security' }];
    expect(filterPackages(updates, 'ssl')).toHaveLength(1);
    expect(selectedPackageNames({ openssl: true, nginx: false })).toEqual(['openssl']);
  });

  it('allows direct mutations for managed application containers', () => {
    expect(containerActionDisabled(container, 'stop')).toBe('');
    expect(containerActionDisabled({ ...container, managed: true }, 'stop')).toBe('');
    expect(containerActionDisabled({ ...container, managed: true }, 'delete')).toBe('');
    expect(containerActionDisabled({ ...container, state: 'exited' }, 'restart')).toBe('resourcesPage.containerNotRunning');
  });

  it('tolerates null package fields when filtering', () => {
    const updates = [{ name: 'openssl', installedVersion: null as unknown as string, candidateVersion: '2', source: null as unknown as string }];
    expect(filterPackages(updates, 'openssl')).toHaveLength(1);
    expect(filterPackages(updates, 'zzz')).toHaveLength(0);
  });

  it('labels images without relying on mock-only summaries', () => {
    expect(imageLabel(image({ id: 'sha256:abcdef123456' }))).toBe('sha256:abcde');
  });

  it('labels untagged images when Docker reports null repoTags', () => {
    expect(imageLabel(image({ repoTags: null }))).toBe('sha256:abcde');
  });

  it('deduplicates upgradeable application ids for the image upgrade action', () => {
    const images = [
      image({ updateAvailable: true, upgradeable: true, applicationIds: ['app-1', 'app-2'] }),
      image({ updateAvailable: true, upgradeable: true, applicationIds: ['app-2', 'app-3'] }),
      image({ updateAvailable: true, upgradeable: false, applicationIds: ['app-9'] }),
      image({ updateAvailable: false, upgradeable: true, applicationIds: ['app-9'] }),
    ];
    expect(imageUpgradeApplicationIds(images)).toEqual(['app-1', 'app-2', 'app-3']);
  });
});
