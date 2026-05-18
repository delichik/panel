import { createPinia, setActivePinia } from 'pinia';
import { useAuthStore } from './auth';
import { authApi } from '@/api/auth';

vi.mock('@/api/auth', () => ({
  authApi: {
    login: vi.fn(),
    logout: vi.fn(),
    session: vi.fn(),
  },
}));

describe('auth store', () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.resetAllMocks();
  });

  it('restores an authenticated session', async () => {
    vi.mocked(authApi.session).mockResolvedValue({ authenticated: true, username: 'admin' });
    const store = useAuthStore();

    await expect(store.restoreSession()).resolves.toBe(true);
    expect(store.authenticated).toBe(true);
    expect(store.username).toBe('admin');
    expect(store.checked).toBe(true);
  });

  it('sets authenticated state after login', async () => {
    vi.mocked(authApi.login).mockResolvedValue({ authenticated: true });
    const store = useAuthStore();

    await store.login('admin', 'secret');
    expect(store.authenticated).toBe(true);
    expect(store.username).toBe('admin');
  });
});
