export interface PackageUpdateList {
  serverId: string;
  lastRefreshedAt?: string | null;
  updates: PackageUpdate[];
  refreshing: boolean;
}

export interface PackageUpdate {
  name: string;
  installedVersion: string;
  candidateVersion: string;
  source: string;
}

export interface RefreshResult {
  serverId: string;
  refreshing: boolean;
  taskId?: string;
}

export interface OperationResult {
  refreshTaskId?: string;
}

export interface ContainerDto {
  id: string;
  names: string[];
  image: string;
  imageId: string;
  command: string;
  created: number;
  state: string;
  status: string;
  ports: DockerPort[];
  labels: Record<string, string>;
  mounts: DockerMount[];
  managed: boolean;
  applicationId?: string;
  instanceId?: string;
}

export interface DockerPort {
  ip?: string;
  privatePort: number;
  publicPort?: number;
  type: string;
}

export interface DockerMount {
  type: string;
  name?: string;
  source: string;
  destination: string;
  driver?: string;
  mode?: string;
  rw: boolean;
}

export interface ContainerLogs {
  containerId: string;
  logs: string;
}

export interface ImageList {
  serverId?: string;
  items: ImageDto[];
  observedAt?: string | null;
  lastRefreshedAt?: string | null;
  stale?: boolean;
  refreshing: boolean;
  refreshTaskId?: string;
  lastRefreshError?: string;
}

export interface SnapshotList<T> { items: T[]; observedAt?: string | null; stale: boolean; refreshing: boolean; refreshTaskId?: string; lastRefreshError?: string }

export interface ImageDto {
  id: string;
  parentId?: string;
  repoTags: string[] | null;
  repoDigests: string[];
  created: number;
  size: number;
  containers: number;
  reference: string;
  localDigest?: string;
  latestDigest?: string;
  checkable: boolean;
  updateAvailable: boolean;
  checkedAt?: string | null;
  lastError?: string;
  inUse: boolean;
  applicationIds: string[];
  upgradeable: boolean;
}

export interface NetworkDto {
  id: string;
  name: string;
  driver: string;
  scope: string;
  created?: string;
  internal: boolean;
  labels: Record<string, string>;
}

export interface VolumeDto {
  name: string;
  driver: string;
  mountpoint: string;
  createdAt?: string;
  labels: Record<string, string>;
  usageData?: { size: number; refCount: number };
  inUse: boolean;
  containerCount: number;
}
