import { fetchJson } from './client';

export function idempotencyKey() {
  return `${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

export async function multipartJson<T>(path: string, form: FormData, method = 'POST', key = idempotencyKey()): Promise<T> {
  return fetchJson<T>(`/api/v1${path}`, {
    method,
    body: form,
    headers: { 'Idempotency-Key': key },
  });
}

export async function deleteJson<T>(path: string, body: unknown, key = idempotencyKey()): Promise<T> {
  return fetchJson<T>(`/api/v1${path}`, {
    method: 'DELETE',
    body,
    headers: { 'Idempotency-Key': key },
  });
}