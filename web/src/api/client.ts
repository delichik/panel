import { useI18n } from '@/i18n';

export interface ApiEnvelope<T> {
  data?: T;
  error?: ApiErrorPayload;
}

export interface ApiErrorPayload {
  code?: string;
  message?: string;
  details?: unknown;
}

export interface ApiRequestOptions {
  method?: string;
  body?: unknown;
  signal?: AbortSignal;
  headers?: HeadersInit;
  skipAuth?: boolean;
  /** Override the default request timeout (ms). 0 disables the timeout. */
  timeoutMs?: number;
  /** Set to false to suppress the global on-401 handler for this request. */
  triggerUnauthorized?: boolean;
}

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code = 'api_error',
    readonly details?: unknown,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

export interface DownloadResult {
  blob: Blob;
  filename: string;
}

const defaultBaseUrl = '/api/v1';
const DEFAULT_TIMEOUT_MS = 30_000;
const DEFAULT_DOWNLOAD_TIMEOUT_MS = 120_000;

const { t } = useI18n();

let authTokenProvider: (() => string) | null = null;

export function setAuthTokenProvider(provider: (() => string) | null) {
  authTokenProvider = provider;
}

export function authHeaders(headers?: HeadersInit, skipAuth = false): Headers {
  const next = new Headers(headers);
  if (!skipAuth && !next.has('Authorization')) {
    const token = authTokenProvider?.();
    if (token) next.set('Authorization', `Bearer ${token}`);
  }
  return next;
}

let unauthorizedHandler: (() => void) | null = null;

/** Registers the global handler invoked whenever a request receives HTTP 401. */
export function setUnauthorizedHandler(handler: (() => void) | null) {
  unauthorizedHandler = handler;
}

function notifyUnauthorized(options: ApiRequestOptions) {
  if (options.skipAuth || options.triggerUnauthorized === false) return;
  unauthorizedHandler?.();
}

interface TimeoutHandle {
  signal: AbortSignal;
  timedOut: boolean;
  cancel: () => void;
}

function createTimeout(timeoutMs: number, externalSignal?: AbortSignal): TimeoutHandle {
  const controller = new AbortController();
  let timedOut = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const onExternalAbort = () => controller.abort();
  if (externalSignal) {
    if (externalSignal.aborted) controller.abort();
    else externalSignal.addEventListener('abort', onExternalAbort, { once: true });
  } else if (timeoutMs > 0) {
    timer = setTimeout(() => {
      timedOut = true;
      controller.abort();
    }, timeoutMs);
  }
  return {
    signal: controller.signal,
    get timedOut() { return timedOut; },
    cancel() {
      if (timer !== undefined) clearTimeout(timer);
      externalSignal?.removeEventListener('abort', onExternalAbort);
    },
  };
}

function networkError(error: unknown, timedOut: boolean): ApiError {
  if (error instanceof DOMException && error.name === 'AbortError') {
    return new ApiError(
      timedOut ? t('api.requestTimeout') : t('api.requestAborted'),
      0,
      timedOut ? 'request_timeout' : 'request_aborted',
    );
  }
  return new ApiError(error instanceof Error ? error.message : t('api.networkFailed'), 0, 'network_error', error);
}

function defaultMessage(status: number, code: string) {
  if (code === 'mock_route_not_found') return t('api.mockRouteNotImplemented');
  if (status === 401) return t('api.authenticationRequired');
  return t('api.requestFailedStatus', { status });
}

/**
 * Shared JSON-envelope fetch base used by ApiClient and the raw-fetch helpers
 * (multipart, maintenance, key-asset import, blob downloads). It applies a
 * timeout, propagates AbortSignal, handles 204, normalizes HTML / non-JSON /
 * invalid-JSON / network errors into ApiError, and fires the global
 * on-401 handler unless the request opts out.
 */
export async function fetchJson<T>(path: string, options: ApiRequestOptions = {}): Promise<T> {
  const timeout = createTimeout(options.timeoutMs ?? DEFAULT_TIMEOUT_MS, options.signal);
  const isFormData = typeof FormData !== 'undefined' && options.body instanceof FormData;
  let body: BodyInit | undefined;
  if (options.body !== undefined) {
    body = isFormData ? options.body as FormData : JSON.stringify(options.body);
  }
  try {
    const response = await fetch(path, {
      method: options.method ?? 'GET',
      headers: authHeaders(
        {
          Accept: 'application/json',
          ...(body === undefined || isFormData ? {} : { 'Content-Type': 'application/json' }),
          ...options.headers,
        },
        options.skipAuth,
      ),
      body,
      signal: timeout.signal,
    }).catch((error: unknown) => {
      throw networkError(error, timeout.timedOut);
    });

    if (response.status === 204) return undefined as T;
    if (response.status === 401) notifyUnauthorized(options);

    const contentType = response.headers.get('content-type') ?? '';
    if (!contentType.includes('application/json')) {
      const text = await response.text().catch(() => '');
      const looksHtml = /^\s*</.test(text) || contentType.includes('text/html');
      throw new ApiError(
        looksHtml ? t('api.htmlResponse') : t('api.nonJsonResponse'),
        response.status,
        response.status === 401 ? 'unauthorized' : looksHtml ? 'html_response' : 'non_json_response',
        { contentType },
      );
    }

    const envelope = (await response.json().catch((error: unknown) => {
      throw new ApiError(t('api.invalidJson'), response.status, 'invalid_json_response', error);
    })) as ApiEnvelope<T>;

    if (!response.ok || envelope.error) {
      const payload = envelope.error ?? {};
      const code = payload.code ?? (response.status === 401 ? 'unauthorized' : 'api_error');
      throw new ApiError(payload.message ?? defaultMessage(response.status, code), response.status, code, payload.details);
    }

    if (!('data' in envelope)) {
      throw new ApiError(t('api.missingDataEnvelope'), response.status, 'missing_data_envelope');
    }

    return envelope.data as T;
  } finally {
    timeout.cancel();
  }
}

/**
 * Shared blob-download base with the same timeout / abort / error normalization
 * contract as fetchJson. The default timeout is longer so large archives can
 * finish downloading.
 */
export async function fetchBlob(path: string, options: ApiRequestOptions & { fallbackFilename?: string } = {}): Promise<DownloadResult> {
  const timeout = createTimeout(options.timeoutMs ?? DEFAULT_DOWNLOAD_TIMEOUT_MS, options.signal);
  try {
    const response = await fetch(path, {
      method: options.method ?? 'GET',
      headers: authHeaders({ Accept: 'application/octet-stream, application/zip, */*', ...options.headers }, options.skipAuth),
      signal: timeout.signal,
    }).catch((error: unknown) => {
      throw networkError(error, timeout.timedOut);
    });

    if (response.status === 401) notifyUnauthorized(options);

    if (!response.ok) {
      const contentType = response.headers.get('content-type') ?? '';
      if (contentType.includes('application/json')) {
        const envelope = (await response.json().catch((error: unknown) => {
          throw new ApiError(t('api.invalidJson'), response.status, 'invalid_json_response', error);
        })) as ApiEnvelope<unknown>;
        const code = envelope.error?.code ?? (response.status === 401 ? 'unauthorized' : 'api_error');
        throw new ApiError(envelope.error?.message ?? defaultMessage(response.status, code), response.status, code, envelope.error?.details);
      }
      throw new ApiError(
        t('api.downloadFailedStatus', { status: response.status }),
        response.status,
        response.status === 401 ? 'unauthorized' : 'download_failed',
      );
    }

    return {
      blob: await response.blob(),
      filename: filenameFromDisposition(response.headers.get('content-disposition'), options.fallbackFilename ?? 'download.bin'),
    };
  } finally {
    timeout.cancel();
  }
}

export function filenameFromDisposition(disposition: string | null, fallback: string) {
  if (!disposition) return fallback;
  const utf8 = disposition.match(/filename\*=UTF-8''([^;]+)/i);
  if (utf8?.[1]) return decodeURIComponent(utf8[1]);
  const plain = disposition.match(/filename="?([^";]+)"?/i);
  return plain?.[1] ?? fallback;
}

export class ApiClient {
  constructor(private readonly baseUrl = defaultBaseUrl) {}

  get<T>(path: string, options?: ApiRequestOptions) {
    return this.request<T>('GET', path, undefined, options);
  }

  post<T>(path: string, body?: unknown, options?: ApiRequestOptions) {
    return this.request<T>('POST', path, body, options);
  }

  put<T>(path: string, body?: unknown, options?: ApiRequestOptions) {
    return this.request<T>('PUT', path, body, options);
  }

  patch<T>(path: string, body?: unknown, options?: ApiRequestOptions) {
    return this.request<T>('PATCH', path, body, options);
  }

  delete<T>(path: string, options?: ApiRequestOptions) {
    return this.request<T>('DELETE', path, undefined, options);
  }

  private request<T>(method: string, path: string, body?: unknown, options?: ApiRequestOptions): Promise<T> {
    return fetchJson<T>(`${this.baseUrl}${path}`, { ...options, method, body });
  }
}

export const apiClient = new ApiClient();
export const apiGet = <T>(path: string, options?: ApiRequestOptions) => apiClient.get<T>(path, options);
export const apiPost = <T>(path: string, body?: unknown, options?: ApiRequestOptions) => apiClient.post<T>(path, body, options);
export const apiPut = <T>(path: string, body?: unknown, options?: ApiRequestOptions) => apiClient.put<T>(path, body, options);
