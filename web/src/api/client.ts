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
}

export class ApiClient {
  private readonly baseUrl: string;
  private readonly fetcher: typeof fetch;

  constructor(options: ApiClientOptions = {}) {
    this.baseUrl = options.baseUrl ?? '/api/v1';
    this.fetcher = options.fetcher ?? fetch.bind(globalThis);
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
    const response = await this.fetcher(`${this.baseUrl}${path}`, {
      credentials: 'include',
      ...init,
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
