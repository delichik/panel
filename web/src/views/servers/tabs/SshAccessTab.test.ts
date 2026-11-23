// @vitest-environment jsdom
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useI18n } from '@/i18n';
import type { ServerDto } from '@/types/servers';
import SshAccessTab from './SshAccessTab.vue';

enableAutoUnmount(afterEach);

const sessions = vi.fn();
const attempts = vi.fn();

vi.mock('@/api/servers', () => ({
  serversApi: {
    sshAccessSessions: (...args: unknown[]) => sessions(...args),
    sshAccessAttempts: (...args: unknown[]) => attempts(...args),
  },
}));

const server = { id: 'srv-1', name: 'edge', host: '10.0.0.1' } as ServerDto;

function session(overrides: Record<string, unknown> = {}) {
  return {
    id: 'sess-1',
    user: 'root',
    tty: 'pts/0',
    sourceHost: 'bastion.example.com',
    sourceAddress: '203.0.113.9',
    kind: 'ssh',
    loginAt: '2026-05-01T10:00:00Z',
    logoutAt: '2026-05-01T11:00:00Z',
    logoutOrigin: 'wtmp',
    durationSeconds: 3600,
    ...overrides,
  };
}

async function mountTab(active = true) {
  const wrapper = mount(SshAccessTab, { props: { server, active } });
  await flushPromises();
  await flushPromises();
  return wrapper;
}

beforeEach(() => {
  useI18n().setLocale('en');
  vi.clearAllMocks();
  sessions.mockResolvedValue({
    items: [session(), session({ id: 'sess-2', user: 'deploy', logoutAt: undefined, logoutOrigin: undefined, durationSeconds: undefined })],
    total: 2,
    page: 1,
    pageSize: 20,
    coverage: { state: 'live', lastLiveAt: new Date().toISOString() },
  });
  attempts.mockResolvedValue({ items: [], total: 0, page: 1, pageSize: 20, coverage: { state: 'unknown' } });
});

describe('SshAccessTab', () => {
  it('renders session rows with login/logout times and coverage', async () => {
    const view = await mountTab();
    expect(sessions).toHaveBeenCalledWith('srv-1', expect.objectContaining({ page: 1, pageSize: 20 }), expect.anything());
    expect(view.text()).toContain('root');
    expect(view.text()).toContain('bastion.example.com');
    expect(view.text()).toContain('1h 0m');
    expect(view.text()).toContain('Online');
    expect(view.text()).toContain('Agent reporting');
  });

  it('shows the unsupported-agent hint when coverage is unknown', async () => {
    sessions.mockResolvedValueOnce({ items: [], total: 0, page: 1, pageSize: 20, coverage: { state: 'unknown' } });
    const view = await mountTab();
    expect(view.text()).toContain('Agent has not reported SSH sessions yet');
  });
});
