import type { ActivityReceipt } from './activity';
export interface ServerOsRelease {
  id?: string;
  versionId?: string;
  prettyName?: string;
  supported?: boolean;
}

export interface ServerArchitecture {
  os?: string;
  arch?: string;
  rawMachine?: string;
}

export interface ServerSudoState {
  passwordless?: boolean;
  lastCheckedAt?: string | null;
}

export interface ServerPrivilegeState {
  mode?: string;
  privileged?: boolean;
  lastCheckedAt?: string | null;
}

export interface ServerDto {
  id: string;
  name: string;
  kind: string;
  host: string;
  ipv4?: string;
  ipv6?: string;
  port: number;
  agentPublicPort?: number;
  sshUsername?: string;
  credentialId: string;
  dockerHost?: string;
  traits?: Record<string, string>;
  variables?: Record<string, string>;
  notes?: string;
  os?: ServerOsRelease;
  architecture?: ServerArchitecture;
  sudo?: ServerSudoState;
  privilege?: ServerPrivilegeState;
  /** 用户意图：该节点是否加入 tailnet。 */
  tailscaleEnabled: boolean;
  /** 该节点的 agent 连接是否优先使用 tailscale 地址。 */
  tailscalePreferAgent: boolean;
  /** 该节点参与的节点互联是否优先使用 tailscale 地址。 */
  tailscalePreferInterconnect: boolean;
  reachable: boolean;
  loadAverage?: string;
  lastCheckedAt?: string | null;
  lastError?: string;
  hostKeyMismatch?: boolean;
  initialTaskId?: string;
  createdAt?: string;
  updatedAt?: string;
}

export interface ServerSaveInput {
  name: string;
  kind: string;
  ipv4: string;
  ipv6: string;
  port: number;
  agentPublicPort: number;
  sshUsername: string;
  credentialId: string;
  dockerHost: string;
  tailscaleEnabled: boolean;
  tailscalePreferAgent: boolean;
  tailscalePreferInterconnect: boolean;
  traits?: Record<string, string>;
  variables: Record<string, string>;
  notes: string;
}

export interface ServerProbeResult {
  reachable: boolean;
  passwordlessSudo: boolean;
  root: boolean;
  privileged: boolean;
  privilegeMode: string;
  os?: ServerOsRelease;
  architecture?: ServerArchitecture;
  traits?: Record<string, string>;
  variables?: Record<string, string>;
  error?: string;
  passwordlessSudoText?: string;
}

export interface OperationAccepted extends ActivityReceipt {
  taskId: string;
}

export interface ServerReference {
  id: string;
  name: string;
  host: string;
}

export interface NatPortMapping {
  id: string;
  serverId: string;
  appId: string;
  appName?: string;
  hostPort: number;
  publicPort: number;
  protocol: string;
  label: string;
  notes: string;
  createdAt?: string;
  updatedAt?: string;
}

export interface NatPortMappingSave {
  appId: string;
  hostPort: number;
  publicPort: number;
  protocol: string;
  label: string;
  notes: string;
}

export interface NatPortNeedOpen {
  kind: 'ssh' | 'agent' | 'app';
  port: number;
  label: string;
  target?: string;
}

export interface NatPortConfig {
  serverId: string;
  serverHost: string;
  kind: string;
  mappings: NatPortMapping[];
  needOpen: NatPortNeedOpen[];
}
