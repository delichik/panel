import type { CodeEditorLanguage } from '@/components/ui/codeEditorLanguage';

export type AssetFileKind = 'text' | 'binary' | 'archive';

export interface AssetFileItem {
  key: string;
  name: string;
  filename?: string;
  kind: AssetFileKind;
  size: number;
  sha256?: string;
  editable?: boolean;
}

export interface UploadAssetInput {
  file: File;
  kind: Exclude<AssetFileKind, 'text'>;
}

export interface SaveTextAssetInput {
  key: string;
  item?: AssetFileItem;
  name: string;
  filename?: string;
  content: string;
  language: CodeEditorLanguage;
}

export interface LoadedTextAsset {
  content: string;
  name?: string;
  filename?: string;
  language?: CodeEditorLanguage;
}

export interface AssetFileAdapter {
  upload(input: UploadAssetInput): Promise<unknown>;
  replace(item: AssetFileItem, input: UploadAssetInput): Promise<unknown>;
  download(item: AssetFileItem): Promise<unknown>;
  delete(item: AssetFileItem): Promise<unknown>;
  loadText(item: AssetFileItem): Promise<LoadedTextAsset>;
  saveText(input: SaveTextAssetInput): Promise<unknown>;
  reload?(): Promise<unknown>;
}

export interface AssetFileManagerLabels {
  title: string;
  hint: string;
  uploadAsset: string;
  uploadAssetTitle: string;
  uploadType: string;
  uploadTypeText: string;
  uploadTypeBinary: string;
  uploadTypeArchive: string;
  uploadFile: string;
  uploadArchive: string;
  operationFailed: string;
  edit: string;
  replace: string;
  download: string;
  delete: string;
  bytes: string;
  noAssets: string;
  noAssetsHint: string;
  textTitle: string;
  newTextTitle: string;
  name: string;
  nameHint?: string;
  filename: string;
  filenameHint?: string;
  language: string;
  content: string;
  loading: string;
  loadFailed: string;
  cancel: string;
  save: string;
  close: string;
  reload: string;
  deleteTitle: string;
  deleteDescription: string;
  confirmDelete: string;
  archiveAccept?: string;
}
