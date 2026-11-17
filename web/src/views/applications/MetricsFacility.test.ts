// @vitest-environment jsdom
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { toastKey } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { ServerDto } from '@/types/servers';
import MetricsFacility from './MetricsFacility.vue';

enableAutoUnmount(afterEach);

const config = vi.fn();
const saveConfig = vi.fn();

vi.mock('@/api/metricsFacility', () => ({
  metricsFacilityApi: {
    config: (...args: unknown[]) => config(...args),
    saveConfig: (...args: unknown[]) => saveConfig(...args),
  },
}));

function server(id: string, name: string, host: string): ServerDto {
  return {
    id,
    name,
    host,
    ipv4: host,
    ipv6: '',
    port: 22,
    credentialId: 'cred-1',
    reachable: true,
    tailscaleEnabled: false,
    tailscalePreferAgent: false,
    tailscalePreferInterconnect: false,
    proxyMode: 'direct',
    tunnelServerId: '',
    tunnelServerName: '',
    socks5Address: '',
    socks5Username: '',
    proxyInvalid: false,
    proxyError: '',
  } as ServerDto;
}

const servers = [server('srv-1', 'edge', '10.0.0.1'), server('srv-2', 'core', '10.0.0.2')];

const defaultConfig = {
  mode: 'all' as const,
  serverIds: [] as string[],
  retentionDays: 7,
  collectionIntervalSeconds: 60,
  cleanupSchedule: 'daily' as const,
  version: 0,
  updatedAt: '',
};

const toastPush = vi.fn();

async function mountFacility() {
  const wrapper = mount(MetricsFacility, {
    props: { mode: 'facilityConfig', servers },
    global: { provide: { [toastKey as symbol]: { push: toastPush, remove: vi.fn() } } },
    attachTo: document.body,
  });
  await flushPromises();
  await flushPromises();
  return wrapper;
}

async function selectOption(comboboxIndex: number, label: string) {
  const comboboxes = document.querySelectorAll<HTMLButtonElement>('[role="combobox"]');
  comboboxes[comboboxIndex]!.click();
  await flushPromises();
  const option = Array.from(document.querySelectorAll<HTMLButtonElement>('[role="option"]')).find((item) => item.textContent?.trim() === label);
  expect(option, `option "${label}"`).toBeTruthy();
  option!.click();
  await flushPromises();
}

function saveButton(wrapper: VueWrapper) {
  return wrapper.findAll('button').find((button) => button.text() === 'Save configuration')!;
}

beforeEach(() => {
  vi.clearAllMocks();
  toastPush.mockReset();
  useI18n().setLocale('en');
  config.mockResolvedValue({ ...defaultConfig });
  saveConfig.mockImplementation(async (input: typeof defaultConfig) => ({
    ...input,
    version: 1,
    updatedAt: new Date().toISOString(),
  }));
});

afterEach(() => {
  document.body.innerHTML = '';
});

describe('metrics facility', () => {
  it('loads and renders the current collection configuration', async () => {
    const wrapper = await mountFacility();

    expect(config).toHaveBeenCalledWith(expect.objectContaining({ signal: expect.anything() }));
    expect((wrapper.find('[data-testid="metrics-retention"]').element as HTMLInputElement).value).toBe('7');
    expect((wrapper.find('[data-testid="metrics-interval"]').element as HTMLInputElement).value).toBe('60');
    expect(wrapper.text()).toContain('Collection configuration');
    expect(wrapper.text()).toContain('All servers');
    expect(wrapper.text()).toContain('Daily');
    expect(saveButton(wrapper).attributes('disabled')).toBeDefined();
  });

  it('requires at least one node when the scope is selected', async () => {
    const wrapper = await mountFacility();
    await selectOption(0, 'Selected servers');

    expect(wrapper.text()).toContain('Metrics nodes');
    await saveButton(wrapper).trigger('click');
    await flushPromises();

    expect(wrapper.text()).toContain('Select at least one metrics node.');
    expect(saveConfig).not.toHaveBeenCalled();
  });

  it('blocks saving an out-of-range retention value with an inline error', async () => {
    const wrapper = await mountFacility();
    const retention = wrapper.find<HTMLInputElement>('[data-testid="metrics-retention"]');
    retention.element.value = '0';
    await retention.trigger('input');
    await flushPromises();

    expect(wrapper.text()).toContain('Enter a whole number of days of at least 1.');
    await saveButton(wrapper).trigger('click');
    await flushPromises();
    expect(saveConfig).not.toHaveBeenCalled();
  });

  it('saves the edited interval and reloads the facility configuration', async () => {
    const wrapper = await mountFacility();
    const interval = wrapper.find<HTMLInputElement>('[data-testid="metrics-interval"]');
    interval.element.value = '120';
    await interval.trigger('input');
    await flushPromises();

    await saveButton(wrapper).trigger('click');
    await flushPromises();

    expect(saveConfig).toHaveBeenCalledWith({
      mode: 'all',
      serverIds: [],
      retentionDays: 7,
      collectionIntervalSeconds: 120,
      cleanupSchedule: 'daily',
    });
    expect(config).toHaveBeenCalledTimes(2);
    expect(toastPush).toHaveBeenCalledWith(expect.objectContaining({ tone: 'success' }));
  });

  it('shows a retry path when the initial load fails', async () => {
    config.mockRejectedValueOnce(new Error('network down'));
    const wrapper = await mountFacility();

    expect(wrapper.text()).toContain('network down');
    config.mockResolvedValueOnce({ ...defaultConfig });
    const retry = wrapper.findAll('button').find((button) => button.text() === 'Retry')!;
    await retry.trigger('click');
    await flushPromises();

    expect(wrapper.find('[data-testid="metrics-config-form"]').exists()).toBe(true);
    expect((wrapper.find('[data-testid="metrics-retention"]').element as HTMLInputElement).value).toBe('7');
  });
});
