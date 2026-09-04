export type KeyAssetType = 'ca_certificate' | 'tls_certificate' | 'ssh_key_pair';
export type KeyAssetAlgorithm = 'ed25519' | 'rsa';

export interface KeyAssetReferenceDto {
  resourceType: string;
  resourceId: string;
  resourceName: string;
  relation: string;
}

export interface KeyAssetDto {
  id: string;
  type: KeyAssetType | string;
  name: string;
  parentAssetId?: string;
  algorithm?: string;
  keySize?: number;
  commonName?: string;
  dnsNames: string[];
  ipAddresses: string[];
  fingerprint: string;
  notBefore?: string;
  notAfter?: string;
  hasCertificate: boolean;
  hasPrivateKey: boolean;
  hasPublicKey: boolean;
  downloadKinds: string[];
  childCount: number;
  referenceCount: number;
  references: KeyAssetReferenceDto[];
  canReissue: boolean;
  canRegenerate: boolean;
  canDelete: boolean;
  createdAt: string;
  updatedAt: string;
  metadata?: Record<string, unknown>;
}

export interface KeyAssetMutationResult {
  asset?: KeyAssetDto;
  taskId?: string;
  operationId?: string;
}

export interface SystemCertificateDto {
  id: string;
  type: KeyAssetType | string;
  name: string;
  commonName?: string;
  fingerprint?: string;
  notBefore?: string;
  notAfter?: string;
  serverId?: string;
  serverName?: string;
  status?: string;
  builtIn: boolean;
  canReset: boolean;
}

export interface SystemCertificateResetResult {
  taskId: string;
}

export interface CreateCaAssetInput {
  name: string;
  commonName: string;
  algorithm: KeyAssetAlgorithm;
  keySize: number;
  years: number;
  validityDays: number;
}

export interface CreateTlsAssetInput {
  name: string;
  parentAssetId: string;
  caId: string;
  commonName: string;
  algorithm: KeyAssetAlgorithm;
  keySize: number;
  dnsNames: string[];
  ipAddresses: string[];
  days: number;
  validityDays: number;
}

export interface GenerateSshAssetInput {
  name: string;
  algorithm: KeyAssetAlgorithm;
  keySize: number;
  comment: string;
}

export interface ImportKeyAssetInput {
  type: KeyAssetType;
  name: string;
  parentAssetId?: string;
  commonName?: string;
  algorithm?: string;
  keySize?: number;
  certificatePem?: string;
  privateKeyPem?: string;
  publicKeyPem?: string;
  publicKey?: string;
}

export interface ExportKeyAssetsInput {
  assetIds: string[];
  password: string;
}

export interface ExportKeyAssetsResult {
  taskId: string;
}

export interface ImportPlanSummaryDto {
  totalAssets: number;
  caCount: number;
  tlsCount: number;
  sshCount: number;
  standaloneTlsCount: number;
  conflictCount: number;
}

export interface ImportPlanAssetDto {
  assetId: string;
  type: string;
  name: string;
  parentAssetId?: string;
  algorithm?: string;
  keySize?: number;
  commonName?: string;
  fingerprint?: string;
  standalone: boolean;
  conflictTypes: string[];
}

export interface ImportConflictDto {
  assetId: string;
  assetName: string;
  assetType: string;
  conflictType: string;
  existingAssetId?: string;
  existingAssetName?: string;
  affectedReferences?: KeyAssetReferenceDto[];
}

export interface ImportPreflightDto {
  planId: string;
  expiresAt: string;
  summary: ImportPlanSummaryDto;
  assets: ImportPlanAssetDto[];
  conflicts: ImportConflictDto[];
  requiresDangerConfirm: boolean;
}

export interface ImportExecuteInput {
  strategy: string;
  confirmOverwriteInUse: boolean;
  confirmDangerousOverwrite: boolean;
  resolutions: Array<{ assetId: string; action: string; targetAssetId?: string }>;
}

export interface ImportExecuteResult {
  taskId: string;
  operationId?: string;
}
