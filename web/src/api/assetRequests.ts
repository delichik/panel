import { ApiError, type ApiEnvelope, authHeaders } from './client';

export function idempotencyKey() {
  return `${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

async function parseJsonEnvelope<T>(response: Response): Promise<T> {
  const envelope = await response.json().catch((error: unknown) => {
    throw new ApiError('Unable to parse JSON response.', response.status, 'invalid_json_response', error);
  }) as ApiEnvelope<T>;
  if (!response.ok || envelope.error) {
    const payload = envelope.error ?? {};
    throw new ApiError(payload.message ?? `Request failed with status ${response.status}.`, response.status, payload.code ?? 'api_error', payload.details);
  }
  if (!('data' in envelope)) throw new ApiError('API response is missing the data envelope.', response.status, 'missing_data_envelope');
  return envelope.data as T;
}

export async function multipartJson<T>(path: string, form: FormData, method = 'POST', key = idempotencyKey()): Promise<T> {
  const response = await fetch(`/api/v1${path}`, {
    method,
    headers: authHeaders({ Accept: 'application/json', 'Idempotency-Key': key }),
    body: form,
  });
  return parseJsonEnvelope<T>(response);
}

export async function deleteJson<T>(path: string, body: unknown, key = idempotencyKey()): Promise<T> {
  const response = await fetch(`/api/v1${path}`, {
    method: 'DELETE',
    headers: authHeaders({ Accept: 'application/json', 'Content-Type': 'application/json', 'Idempotency-Key': key }),
    body: JSON.stringify(body),
  });
  if (response.status === 204) return undefined as T;
  return parseJsonEnvelope<T>(response);
}
