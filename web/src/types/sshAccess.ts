export interface SshAccessSession {
  id: string;
  user: string;
  tty: string;
  sourceHost: string;
  sourceAddress: string;
  kind: string;
  loginAt: string;
  logoutAt?: string;
  logoutOrigin?: string;
  durationSeconds?: number;
  lastSeenAt?: string;
}

export interface SshAccessAttempt {
  id: string;
  user: string;
  sourceHost: string;
  sourceAddress: string;
  pid: number;
  attemptAt: string;
}

export type SshAccessCoverageState = 'live' | 'stale' | 'unknown';

export interface SshAccessCoverage {
  state: SshAccessCoverageState;
  lastLiveAt?: string;
}

export interface SshAccessSessionPage {
  items: SshAccessSession[];
  total: number;
  page: number;
  pageSize: number;
  coverage: SshAccessCoverage;
}

export interface SshAccessAttemptPage {
  items: SshAccessAttempt[];
  total: number;
  page: number;
  pageSize: number;
  coverage: SshAccessCoverage;
}

export interface SshAccessQuery {
  from?: string;
  to?: string;
  user?: string;
  source?: string;
  q?: string;
  page?: number;
  pageSize?: number;
}
