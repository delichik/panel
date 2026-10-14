import type { ServerDto } from '@/types/servers';
import type { ContainerDto, ImageDto, PackageUpdate, VolumeDto } from '@/types/resources';

export type Tone = 'neutral' | 'success' | 'warning' | 'danger' | 'info';

export function canMaintainPackages(server: ServerDto | null) {
  if (!server) return false;
  const supported = server.os?.supported !== false;
  const privileged = Boolean(server.privilege?.privileged || server.sudo?.passwordless || server.privilege?.mode === 'root' || server.privilege?.mode === 'passwordless_sudo');
  return server.reachable && supported && privileged;
}

export function packageBlockReason(server: ServerDto | null) {
  if (!server) return 'resourcesPage.selectServerHint';
  if (!server.reachable) return 'resourcesPage.blockUnreachable';
  if (server.os?.supported === false) return 'resourcesPage.blockUnsupported';
  if (!canMaintainPackages(server)) return 'resourcesPage.blockPrivilege';
  return '';
}

export function canUseDockerResources(server: ServerDto | null) {
  return Boolean(server?.traits?.['agent.status'] === 'compatible' && server.traits?.['agent.url']);
}

export function dockerBlockReason(server: ServerDto | null) {
  if (!server) return 'resourcesPage.selectServerHint';
  if (!server.reachable) return 'resourcesPage.blockUnreachable';
  if (!canUseDockerResources(server)) return 'resourcesPage.blockAgent';
  return '';
}

export function filterPackages(items: PackageUpdate[], term: string) {
  const needle = term.trim().toLowerCase();
  if (!needle) return items;
  return items.filter((item) => [item.name, item.installedVersion, item.candidateVersion, item.source].some((value) => String(value ?? '').toLowerCase().includes(needle)));
}

export function containerTone(container: ContainerDto): Tone {
  if (container.managed) return 'info';
  if (container.state === 'running') return 'success';
  if (container.state === 'exited') return 'warning';
  return 'neutral';
}

export function containerActionDisabled(container: ContainerDto, action: 'start' | 'stop' | 'restart' | 'delete') {
  if (action === 'start' && container.state === 'running') return 'resourcesPage.containerAlreadyRunning';
  if ((action === 'stop' || action === 'restart') && container.state !== 'running') return 'resourcesPage.containerNotRunning';
  return '';
}

export function imageTone(image: ImageDto): Tone {
  if (image.lastError) return 'warning';
  if (image.updateAvailable) return 'info';
  if (image.inUse) return 'success';
  return 'neutral';
}

export function imageLabel(image: ImageDto) {
  return image.reference || image.repoTags?.[0] || image.id.slice(0, 12);
}

/** 可升级镜像关联的应用 id 去重集合；“升级有更新应用”的 payload 来源。 */
export function imageUpgradeApplicationIds(images: ImageDto[]) {
  return Array.from(new Set(images.filter((item) => item.updateAvailable && item.upgradeable).flatMap((item) => item.applicationIds)));
}

export function volumeTone(volume: VolumeDto): Tone {
  if (volume.inUse) return 'success';
  if (volume.usageData && volume.usageData.size > 1024 * 1024 * 1024) return 'warning';
  return 'neutral';
}

export function selectedPackageNames(selection: Record<string, boolean>) {
  return Object.entries(selection).filter(([, selected]) => selected).map(([name]) => name);
}
