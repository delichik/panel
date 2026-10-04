import type { ServerReference } from './servers';

export type CredentialType = 'password' | 'private_key';

export interface CredentialDto {
  id: string;
  name: string;
  type: CredentialType;
  username: string;
  createdAt?: string;
  updatedAt?: string;
}

export interface CredentialInput {
  name: string;
  type: CredentialType;
  username: string;
  password?: string;
  privateKey?: string;
  passphrase?: string;
}

export interface KeySummary {
  algorithm?: string;
  bits?: number;
  fingerprint?: string;
  comment?: string;
}

export interface CredentialDetailDto extends CredentialDto {
  keySummary?: KeySummary;
}
