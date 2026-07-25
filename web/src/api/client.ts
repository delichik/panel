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
  signal?: AbortSignal;
  headers?: HeadersInit;
  skipAuth?: boolean;
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

const defaultBaseUrl = '/api/v1';
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

  private async request<T>(method: string, path: string, body?: unknown, options?: ApiRequestOptions): Promise<T> {
    const response = await fetch(`${this.baseUrl}${path}`, {
      method,
      signal: options?.signal,
      headers: authHeaders({
          Accept: 'application/json',
          ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
          ...options?.headers,
        },
        options?.skipAuth,
      ),
      body: body === undefined ? undefined : JSON.stringify(body),
    }).catch((error: unknown) => {
      if (error instanceof DOMException && error.name === 'AbortError') {
        throw new ApiError('Request was aborted.', 0, 'request_aborted');
      }
      throw new ApiError(error instanceof Error ? error.message : 'Network request failed.', 0, 'network_error', error);
    });

    if (response.status === 204) {
      return undefined as T;
    }

    const contentType = response.headers.get('content-type') ?? '';
    if (!contentType.includes('application/json')) {
      const text = await response.text().catch(() => '');
      const looksHtml = /^\s*</.test(text) || contentType.includes('text/html');
      throw new ApiError(
        looksHtml ? 'Server returned an HTML page instead of JSON.' : 'Server returned a non-JSON response.',
        response.status,
        looksHtml ? 'html_response' : 'non_json_response',
        { contentType },
      );
    }

    const envelope = (await response.json().catch((error: unknown) => {
      throw new ApiError('Unable to parse JSON response.', response.status, 'invalid_json_response', error);
    })) as ApiEnvelope<T>;

    if (!response.ok || envelope.error) {
      const payload = envelope.error ?? {};
      const code = payload.code ?? (response.status === 401 ? 'unauthorized' : 'api_error');
      throw new ApiError(payload.message ?? defaultMessage(response.status, code), response.status, code, payload.details);
    }

    if (!('data' in envelope)) {
      throw new ApiError('API response is missing the data envelope.', response.status, 'missing_data_envelope');
    }

    return envelope.data as T;
  }
}

function defaultMessage(status: number, code: string) {
  if (code === 'mock_route_not_found') return 'Mock API route is not implemented.';
  if (status === 401) return 'Authentication is required.';
  return `Request failed with status ${status}.`;
}

export const apiClient = new ApiClient();
export const apiGet = <T>(path: string, options?: ApiRequestOptions) => apiClient.get<T>(path, options);
export const apiPost = <T>(path: string, body?: unknown, options?: ApiRequestOptions) => apiClient.post<T>(path, body, options);
export const apiPut = <T>(path: string, body?: unknown, options?: ApiRequestOptions) => apiClient.put<T>(path, body, options);
