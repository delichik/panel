import { fetchBlob, filenameFromDisposition, type DownloadResult } from './client';

export type { DownloadResult } from './client';
export { filenameFromDisposition } from './client';

export interface DownloadOptions extends RequestInit {
  /** Set to false to suppress the global on-401 handler for this download. */
  triggerUnauthorized?: boolean;
}

export async function fetchDownload(path: string, options: DownloadOptions = {}, fallbackFilename = 'download.bin'): Promise<DownloadResult> {
  return fetchBlob(path, {
    method: options.method,
    headers: options.headers,
    signal: options.signal ?? undefined,
    triggerUnauthorized: options.triggerUnauthorized,
    fallbackFilename,
  });
}

export function saveBlobDownload({ blob, filename }: DownloadResult) {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = filename;
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}