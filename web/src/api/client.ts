import { getLocaleHeader } from '@/i18n';

export interface ApiEnvelope<T> {
  data: T | null;
  error: ApiErrorBody | null;
}

export interface ApiErrorBody {
  code: string;
  message: string;
  details?: Record<string, unknown>;
}

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly details?: Record<string, unknown>;

  constructor(status: number, body: ApiErrorBody) {
    super(body.message);
    this.name = 'ApiError';
    this.status = status;
    this.code = body.code;
    this.details = body.details;
  }
}

export interface ApiClientOptions {
  baseUrl?: string;
  fetcher?: typeof fetch;
  getToken?: () => string;
}

export class ApiClient {
  private readonly baseUrl: string;
  private readonly fetcher: typeof fetch;
  private readonly getToken: () => string;

  constructor(options: ApiClientOptions = {}) {
    this.baseUrl = options.baseUrl ?? '/api/v1';
    this.fetcher = options.fetcher ?? fetch.bind(globalThis);
    this.getToken = options.getToken ?? readAuthToken;
  }

  get<T>(path: string, init?: RequestInit) {
    return this.request<T>(path, { ...init, method: 'GET' });
  }

  post<T>(path: string, body?: unknown, init?: RequestInit) {
    return this.request<T>(path, this.withJson(init, 'POST', body));
  }

  put<T>(path: string, body?: unknown, init?: RequestInit) {
    return this.request<T>(path, this.withJson(init, 'PUT', body));
  }

  delete<T = void>(path: string, init?: RequestInit) {
    return this.request<T>(path, { ...init, method: 'DELETE' });
  }

  async download(path: string, init: RequestInit = {}) {
    const headers = new Headers(init.headers);
    const token = this.getToken();
    headers.set('Accept-Language', getLocaleHeader());
    if (token) {
      headers.set('Authorization', `Bearer ${token}`);
    }
    const response = await this.fetcher(`${this.baseUrl}${path}`, {
      ...init,
      method: init.method ?? 'GET',
      headers,
    });
    if (!response.ok) {
      const envelope = (await response.json().catch(() => ({ error: null }))) as ApiEnvelope<unknown>;
      throw new ApiError(
        response.status,
        envelope.error ?? {
          code: 'http_error',
          message: `Request failed with status ${response.status}`,
        },
      );
    }
    return {
      blob: await response.blob(),
      filename: filenameFromDisposition(response.headers.get('Content-Disposition')),
    };
  }

  private withJson(init: RequestInit = {}, method: string, body?: unknown): RequestInit {
    const headers = new Headers(init.headers);
    if (body !== undefined) {
      headers.set('Content-Type', 'application/json');
    }
    return {
      ...init,
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    };
  }

  private async request<T>(path: string, init: RequestInit): Promise<T> {
    const headers = new Headers(init.headers);
    const token = this.getToken();
    headers.set('Accept-Language', getLocaleHeader());
    if (token) {
      headers.set('Authorization', `Bearer ${token}`);
    }
    const response = await this.fetcher(`${this.baseUrl}${path}`, {
      ...init,
      headers,
    });

    if (response.status === 204) {
      return undefined as T;
    }

    const envelope = (await response.json()) as ApiEnvelope<T>;
    if (!response.ok || envelope.error) {
      throw new ApiError(
        response.status,
        envelope.error ?? {
          code: 'http_error',
          message: `Request failed with status ${response.status}`,
        },
      );
    }
    return envelope.data as T;
  }
}

export const apiClient = new ApiClient();

export function readAuthToken() {
  const storage = globalThis.localStorage;
  if (!storage || typeof storage.getItem !== 'function') {
    return '';
  }
  return storage.getItem('authToken') ?? '';
}

function filenameFromDisposition(disposition: string | null) {
  const match = disposition?.match(/filename="([^"]+)"/i) ?? disposition?.match(/filename=([^;]+)/i);
  return match?.[1]?.trim() || 'download.bin';
}
