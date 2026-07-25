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

export interface CredentialWithReferences extends CredentialDto {
  references: ServerReference[];
}
