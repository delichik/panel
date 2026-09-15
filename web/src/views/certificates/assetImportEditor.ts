import type { KeyAssetType } from '@/types/keyAssets';

export type AssetImportMaterialTab = 'privateKey' | 'certificate';

export function initialAssetImportMaterialTab(): AssetImportMaterialTab {
  return 'privateKey';
}

export function assetImportHasCertificate(type: KeyAssetType) {
  return type !== 'ssh_key_pair';
}
