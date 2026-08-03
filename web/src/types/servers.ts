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
  host: string;
  ipv4?: string;
  ipv6?: string;
  port: number;
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
  reachable: boolean;
  loadAverage?: string;
  lastCheckedAt?: string | null;
  lastError?: string;
  initialTaskId?: string;
  createdAt?: string;
  updatedAt?: string;
}

export interface ServerSaveInput {
  name: string;
  ipv4: string;
  ipv6: string;
  port: number;
  sshUsername: string;
  credentialId: string;
  dockerHost: string;
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

export interface OperationAccepted {
  taskId: string;
}

export interface ServerReference {
  id: string;
  name: string;
  host: string;
}
