import { createPinia, setActivePinia } from 'pinia';
import { useAuthStore } from './auth';
import { authApi } from '@/api/auth';

vi.mock('@/api/auth', () => ({
  authApi: {
    login: vi.fn(),
    logout: vi.fn(),
    session: vi.fn(),
    updateAccount: vi.fn(),
    updateJwtSecret: vi.fn(),
  },
}));

describe('auth store', () => {
  const storage = new Map<string, string>();

  beforeEach(() => {
    setActivePinia(createPinia());
    storage.clear();
    vi.resetAllMocks();
    vi.stubGlobal('localStorage', {
      getItem: vi.fn((key: string) => storage.get(key) ?? null),
      setItem: vi.fn((key: string, value: string) => storage.set(key, value)),
      removeItem: vi.fn((key: string) => storage.delete(key)),
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('restores an authenticated session', async () => {
    storage.set('authToken', 'jwt-token');
    vi.mocked(authApi.session).mockResolvedValue({ authenticated: true, username: 'admin', passwordChangeRequired: false });
    const store = useAuthStore();

    await expect(store.restoreSession()).resolves.toBe(true);
    expect(store.authenticated).toBe(true);
    expect(store.username).toBe('admin');
    expect(store.checked).toBe(true);
  });

  it('sets authenticated state after login', async () => {
    vi.mocked(authApi.login).mockResolvedValue({ authenticated: true, token: 'jwt-token', username: 'admin', passwordChangeRequired: false });
    const store = useAuthStore();

    await store.login('admin', 'secret');
    expect(store.authenticated).toBe(true);
    expect(store.username).toBe('admin');
    expect(store.token).toBe('jwt-token');
    expect(storage.get('authToken')).toBe('jwt-token');
  });
});
