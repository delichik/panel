export type CertificateScope = 'single' | 'wildcard' | 'prefixes';
export type CertificateStatus = 'pending' | 'issuing' | 'issued' | 'failed';

export interface DomainCertificateDto {
  id: string;
  name: string;
  domainId: string;
  domain: string;
  prefix: string;
  scope: CertificateScope | string;
  domains: string[];
  variableName: string;
  certificatePath: string;
  privateKeyPath: string;
  issuer: string;
  status: CertificateStatus | string;
  lastError?: string;
  autoRenew: boolean;
  nextRenewAt?: string;
  notBefore?: string;
  notAfter?: string;
  createdAt: string;
  updatedAt: string;
}

export interface IssueCertificateInput {
  name: string;
  domainId: string;
  prefixes?: string[];
  prefix?: string;
  scope?: CertificateScope;
  variableName: string;
}

export interface IssueCertificateResult {
  certificate: DomainCertificateDto;
  taskId?: string;
}

export interface RenewCertificateResult {
  renewed: boolean;
}

export interface SelfSignedCertificateDto {
  id: string;
  parentCaId?: string;
  kind: 'ca' | 'leaf' | string;
  name: string;
  commonName: string;
  dnsNames: string[];
  ipAddresses: string[];
  fingerprint: string;
  notBefore: string;
  notAfter: string;
  createdAt: string;
  updatedAt: string;
}

export interface SelfSignedCaInput {
  name: string;
  commonName: string;
  years: number;
}

export interface SelfSignedLeafInput {
  name: string;
  caId: string;
  commonName: string;
  dnsNames: string[];
  ipAddresses: string[];
  days: number;
}
