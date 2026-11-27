import { beforeEach, describe, expect, it, vi } from 'vitest';
import { createPinia, setActivePinia } from 'pinia';
import { ApiError } from '@/api/client';

const storage = new Map<string, string>();
const localStorageStub = {
  getItem: (key: string) => storage.get(key) ?? null,
  setItem: (key: string, value: string) => { storage.set(key, String(value)); },
  removeItem: (key: string) => { storage.delete(key); },
  clear: () => { storage.clear(); },
};
vi.stubGlobal('localStorage', localStorageStub);

const sessionMock = vi.fn();

vi.mock('@/api/auth', () => ({
  authApi: {
    session: (...args: unknown[]) => sessionMock(...args),
    login: vi.fn(),
    logout: vi.fn(),
    updateAccount: vi.fn(),
    updateJwtSecret: vi.fn(),
  },
  setupApi: {
    status: vi.fn().mockResolvedValue({ required: false }),
  },
}));

import { useSessionStore } from './session';

describe('useSessionStore.restore', () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    storage.clear();
    sessionMock.mockReset();
  });

  it('keeps stored token and leaves ready=false on transient network/5xx errors so later navigations retry', async () => {
    localStorageStub.setItem('panel.auth.token', 'stored-token');
    sessionMock.mockRejectedValueOnce(new ApiError('Bad Gateway', 502, 'bad_gateway'));

    const store = useSessionStore();
    await store.restore();

    expect(store.token).toBe('stored-token');
    expect(store.authenticated).toBe(false);
    expect(store.ready).toBe(false);

    sessionMock.mockResolvedValueOnce({
      authenticated: true,
      token: 'stored-token',
      mustChangePassword: false,
      user: { id: 'u1', username: 'admin', displayName: 'Admin' },
      roles: [],
      permissions: ['overview.view'],
    });

    await store.restore();
    expect(store.authenticated).toBe(true);
    expect(store.ready).toBe(true);
    expect(store.can('overview.view')).toBe(true);
  });

  it('clears stored token and marks ready=true on explicit 401 unauthorized', async () => {
    localStorageStub.setItem('panel.auth.token', 'expired-token');
    sessionMock.mockRejectedValueOnce(new ApiError('Unauthorized', 401, 'unauthorized'));

    const store = useSessionStore();
    await store.restore();

    expect(store.token).toBe('');
    expect(store.authenticated).toBe(false);
    expect(store.ready).toBe(true);
  });
});
