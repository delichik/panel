import { describe, expect, it } from 'vitest';
import {
  canMaintainPackages,
  canUseDockerResources,
  containerActionDisabled,
  filterPackages,
  imageLabel,
  packageBlockReason,
  resourceTabFromPath,
  selectedPackageNames,
} from './model';
import type { ServerDto } from '@/types/servers';
import type { ContainerDto, ImageDto } from '@/types/resources';

const server: ServerDto = {
  id: 'srv-1',
  name: 'edge',
  host: '10.0.0.1',
  port: 22,
  credentialId: 'cred-1',
  reachable: true,
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

describe('resources model', () => {
  it('maps URL paths to resource tabs', () => {
    expect(resourceTabFromPath('/resources/images')).toBe('images');
    expect(resourceTabFromPath('/resources')).toBe('packages');
  });

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

  it('labels images without relying on mock-only summaries', () => {
    const image: ImageDto = { id: 'sha256:abcdef123456', repoTags: [], repoDigests: [], created: 0, size: 0, containers: 0, reference: '', checkable: false, updateAvailable: false, inUse: false, applicationIds: [], upgradeable: false };
    expect(imageLabel(image)).toBe('sha256:abcde');
  });

  it('labels untagged images when Docker reports null repoTags', () => {
    const image: ImageDto = { id: 'sha256:abcdef123456', repoTags: null, repoDigests: [], created: 0, size: 0, containers: 0, reference: '', checkable: false, updateAvailable: false, inUse: false, applicationIds: [], upgradeable: false };
    expect(imageLabel(image)).toBe('sha256:abcde');
  });
});
