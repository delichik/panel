import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import type { AssetFileAdapter, AssetFileItem } from './assetFileManager';

const source = readFileSync(resolve(__dirname, 'AssetFileManager.vue'), 'utf8');

describe('AssetFileManager', () => {
  it('keeps one interaction surface for text, binary, archive and delete actions', () => {
    expect(source.match(/<Button size="sm"[^>]*@click="openUpload"/g)?.length).toBe(1);
    expect(source.match(/<FileUploadButton/g)?.length).toBe(2);
    expect(source).toContain('uploadTypeText');
    expect(source).toContain('uploadTypeBinary');
    expect(source).toContain('uploadTypeArchive');
    expect(source).toContain('uploadMode === \'text\'');
    expect(source).toContain('FileUploadButton');
    expect(source).toContain('DownloadButton');
    expect(source).toContain('async function saveText');
    expect(source).toContain('async function confirmDelete');
    expect(source).toContain('textConflict');
  });

  it('exposes a backend-neutral adapter contract', () => {
    const item: AssetFileItem = { key: 'asset-1', name: 'site', kind: 'archive', size: 0 };
    const adapter: AssetFileAdapter = {
      upload: async () => undefined,
      replace: async () => undefined,
      download: async () => undefined,
      delete: async () => undefined,
      loadText: async () => ({ content: '' }),
      saveText: async () => undefined,
    };
    expect(item.kind).toBe('archive');
    expect(typeof adapter.saveText).toBe('function');
  });
});
