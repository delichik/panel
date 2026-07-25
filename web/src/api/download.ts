import { ApiError, type ApiEnvelope, authHeaders } from './client';

export interface DownloadResult {
  blob: Blob;
  filename: string;
}

export function filenameFromDisposition(disposition: string | null, fallback: string) {
  if (!disposition) return fallback;
  const utf8 = disposition.match(/filename\*=UTF-8''([^;]+)/i);
  if (utf8?.[1]) return decodeURIComponent(utf8[1]);
  const plain = disposition.match(/filename="?([^";]+)"?/i);
  return plain?.[1] ?? fallback;
}

export async function fetchDownload(path: string, options: RequestInit = {}, fallbackFilename = 'download.bin'): Promise<DownloadResult> {
  const response = await fetch(path, {
    ...options,
    headers: authHeaders({
      Accept: 'application/octet-stream, application/zip, */*',
      ...options.headers,
    }),
  });

  if (!response.ok) {
    const contentType = response.headers.get('content-type') ?? '';
    if (contentType.includes('application/json')) {
      const envelope = await response.json().catch((error: unknown) => {
        throw new ApiError('Unable to parse JSON response.', response.status, 'invalid_json_response', error);
      }) as ApiEnvelope<unknown>;
      throw new ApiError(envelope.error?.message ?? `Request failed with status ${response.status}.`, response.status, envelope.error?.code ?? 'api_error', envelope.error?.details);
    }
    throw new ApiError(`Download failed with status ${response.status}.`, response.status, 'download_failed');
  }

  return {
    blob: await response.blob(),
    filename: filenameFromDisposition(response.headers.get('content-disposition'), fallbackFilename),
  };
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
