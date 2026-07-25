export interface UfwState {
  serverId: string;
  supported: boolean;
  installed: boolean;
  active: boolean;
  status: string;
  defaultPolicy?: string;
  rules: UfwRule[];
}

export interface UfwRule {
  number: number;
  to: string;
  action: string;
  from: string;
}

export interface UfwAllowInput {
  port: number;
  protocol: string;
  from: string;
}

export interface Fail2BanState {
  serverId: string;
  installed: boolean;
  active: boolean;
  managed: boolean;
  panelConfigPresent: boolean;
  jails: string[];
  raw: string;
  configYaml: string;
  config: Fail2BanConfig;
  updatedAt?: string | null;
}

export interface Fail2BanConfig {
  jails: Fail2BanJail[];
}

export interface Fail2BanJail {
  name: string;
  enabled: boolean;
  preset?: string;
  filter?: string;
  logpath?: string;
  backend?: string;
  port?: string;
  protocol?: string;
  action?: string;
  maxretry?: number;
  findtime?: string;
  bantime?: string;
  ignoreip?: string[];
  options?: Record<string, string>;
}

export interface Fail2BanEnableInput {
  configYaml: string;
  confirmTakeover: boolean;
}
