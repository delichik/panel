import type { DomainCertificateDto, SelfSignedCertificateDto } from '@/types/certificates';
import type { KeyAssetDto } from '@/types/keyAssets';

export function certificateTone(certificate: DomainCertificateDto) {
  if (certificate.status === 'failed') return 'danger';
  if (certificate.status === 'pending' || certificate.status === 'issuing') return 'info';
  if (isExpired(certificate.notAfter)) return 'danger';
  if (daysUntil(certificate.notAfter) <= 14) return 'warning';
  return 'success';
}

export function certificateState(certificate: DomainCertificateDto) {
  if (certificate.status === 'failed') return 'certificatesPage.statusFailed';
  if (certificate.status === 'pending') return 'certificatesPage.statusPending';
  if (certificate.status === 'issuing') return 'certificatesPage.statusIssuing';
  if (isExpired(certificate.notAfter)) return 'certificatesPage.statusExpired';
  if (daysUntil(certificate.notAfter) <= 14) return 'certificatesPage.statusExpiring';
  return 'certificatesPage.statusIssued';
}

export function selfSignedTone(certificate: SelfSignedCertificateDto) {
  if (isExpired(certificate.notAfter)) return 'danger';
  if (daysUntil(certificate.notAfter) <= 30) return 'warning';
  return certificate.kind === 'ca' ? 'info' : 'success';
}

export function assetTone(asset: KeyAssetDto) {
  if (!asset.canDelete && asset.referenceCount > 0) return 'warning';
  if (asset.type === 'ssh_key_pair') return 'info';
  if (daysUntil(asset.notAfter) <= 30) return 'warning';
  return 'success';
}

export function daysUntil(value?: string) {
  if (!value) return Number.POSITIVE_INFINITY;
  return Math.ceil((new Date(value).getTime() - Date.now()) / 86400000);
}

function isExpired(value?: string) {
  return daysUntil(value) < 0;
}
