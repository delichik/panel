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
