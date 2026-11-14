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

/** 连接目标服务器时使用的代理模式；旧数据缺省等价于 `direct`。 */
export type ServerProxyMode = 'direct' | 'ssh_tunnel' | 'socks5';

/** 读取时派生的稳定代理错误码；空串表示代理配置有效。 */
export type ServerProxyErrorCode =
  | 'tunnel_missing'
  | 'cycle'
  | 'chain_too_deep'
  | 'socks5_address_missing'
  | 'socks5_address_invalid';

export type ServerProxyError = ServerProxyErrorCode | '';

/** 保存请求中代理字段的必填形态；表单恒发送全部字段，后端按模式取舍。 */
export interface ServerProxyConfig {
  proxyMode: ServerProxyMode;
  tunnelServerId: string;
  socks5Address: string;
  socks5Username: string;
  /** 只写字段：更新时留空表示保留已存密码，读取时永不回显。 */
  socks5Password: string;
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
  /** 用户意图：该节点是否加入 tailnet。 */
  tailscaleEnabled: boolean;
  /** 该节点的 agent 连接是否优先使用 tailscale 地址。 */
  tailscalePreferAgent: boolean;
  /** 该节点参与的节点互联是否优先使用 tailscale 地址。 */
  tailscalePreferInterconnect: boolean;
  /** 连接目标服务器使用的代理模式；恒有值。 */
  proxyMode: ServerProxyMode;
  /** ssh_tunnel 时选中的隧道服务器 id；允许指向已删除服务器以表达悬空。 */
  tunnelServerId: string;
  /** 读取时派生：隧道服务器名称，找不到时为空。 */
  tunnelServerName: string;
  /** socks5 模式的代理地址（host:port）。 */
  socks5Address: string;
  /** socks5 模式的代理用户名。 */
  socks5Username: string;
  /** 仅单条详情返回：是否已存 SOCKS5 密码；密码本身永不返回。 */
  socks5PasswordSet?: boolean;
  /** 读取时派生：代理链路是否无效。 */
  proxyInvalid: boolean;
  /** 读取时派生：稳定代理错误码。 */
  proxyError: ServerProxyError;
  reachable: boolean;
  loadAverage?: string;
  lastCheckedAt?: string | null;
  lastError?: string;
  hostKeyMismatch?: boolean;
  initialTaskId?: string;
  createdAt?: string;
  updatedAt?: string;
}

export interface ServerSaveInput extends ServerProxyConfig {
  name: string;
  ipv4: string;
  ipv6: string;
  port: number;
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

/** 探测请求：连接字段 + 代理草稿字段；serverId 仅供密码复用与成环检测。 */
export interface ServerProbeInput {
  ipv4: string;
  ipv6: string;
  port: number;
  sshUsername: string;
  credentialId: string;
  proxyMode?: ServerProxyMode;
  tunnelServerId?: string;
  socks5Address?: string;
  socks5Username?: string;
  socks5Password?: string;
  /** 可选：编辑已有服务器时传入，用于复用已存密码与把待保存服务器纳入成环检测。 */
  serverId?: string;
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
