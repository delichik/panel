// @vitest-environment jsdom
import { enableAutoUnmount, mount } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import type { ServerMetricsSeries } from '@/api/servers';
import { useI18n } from '@/i18n';
import type { ServerDto } from '@/types/servers';
import OverviewSummary from './OverviewSummary.vue';

enableAutoUnmount(afterEach);

const GIB = 1024 ** 3;

function server(overrides: Partial<ServerDto> = {}): ServerDto {
  return {
    id: 'srv-1',
    name: 'edge-1',
    host: '10.0.0.1',
    port: 22,
    credentialId: 'cred-1',
    os: { id: 'debian', versionId: '13', prettyName: 'Debian GNU/Linux 13', supported: true },
    architecture: { os: 'linux', arch: 'amd64', rawMachine: 'x86_64' },
    privilege: { privileged: true },
    traits: {
      'agent.status': 'compatible',
      'agent.enabled': 'true',
      'agent.version': '0.2.0',
      'agent.report.status': 'connected',
      'agent.report.stability': 'excellent',
      'agent.report.disconnect_count_1h': '0',
    },
    reachable: true,
    proxyMode: 'direct',
    tunnelServerId: '',
    tunnelServerName: '',
    socks5Address: '',
    socks5Username: '',
    proxyInvalid: false,
    proxyError: '',
    lastCheckedAt: '2026-11-18T11:00:00.000Z',
    updatedAt: '2026-11-18T10:00:00.000Z',
    ...overrides,
  } as ServerDto;
}

function metrics(overrides: Partial<ServerMetricsSeries> = {}): ServerMetricsSeries {
  return {
    range: '1h',
    cpu: [],
    memory: [],
    diskCurrent: { time: '2026-11-18T11:00:00.000Z', usedBytes: 34 * GIB, totalBytes: 50 * GIB },
    diskIo: [],
    network: [],
    load: [],
    ...overrides,
  };
}

beforeEach(() => {
  useI18n().setLocale('en');
});

describe('OverviewSummary', () => {
  it('identifies the distribution and architecture with icons plus text', () => {
    // UI-SRV-028: icon is supplementary; the OS name and readable architecture text stay present.
    const wrapper = mount(OverviewSummary, { props: { server: server(), metrics: metrics() } });

    expect(wrapper.get('[data-testid="os-mark"]').attributes('data-os')).toBe('debian');
    expect(wrapper.get('[data-testid="os-mark"]').attributes('title')).toBe('Debian');
    expect(wrapper.text()).toContain('Debian GNU/Linux 13');
    expect(wrapper.get('[data-testid="arch-mark"]').attributes('title')).toBe('amd64 (x86_64)');
    expect(wrapper.get('[data-testid="arch-mark"]').text()).toContain('amd64');
    expect(wrapper.text()).toContain('Privileged');
    expect(wrapper.text()).toContain('Ready');
  });

  it('renders the status summary as labelled tiles instead of a text run', () => {
    const wrapper = mount(OverviewSummary, { props: { server: server(), metrics: metrics() } });

    for (const label of ['Report stream', 'Link stability', 'Connection proxy', 'Agent']) {
      expect(wrapper.text()).toContain(label);
    }
    expect(wrapper.text()).toContain('Connected');
    expect(wrapper.text()).toContain('Excellent');
    expect(wrapper.text()).toContain('Direct');
    expect(wrapper.text()).toContain('0.2.0');
  });

  it('shows disk current value as a ring chart with used/free/total and sample time', () => {
    // UI-SRV-029: current disk value only; no history series or extra request.
    const wrapper = mount(OverviewSummary, { props: { server: server(), metrics: metrics() } });

    const chart = wrapper.get('[data-testid="disk-usage-chart"]');
    expect(chart.attributes('aria-label')).toBe('Disk usage: 68.0%');
    expect(chart.text()).toBe('68.0%');
    expect(chart.attributes('data-empty')).toBeUndefined();

    expect(wrapper.text()).toContain('Disk');
    expect(wrapper.text()).toContain('Used');
    expect(wrapper.text()).toContain('Free');
    expect(wrapper.text()).toContain('Total');
    expect(wrapper.text()).toContain('34.0 GB');
    expect(wrapper.text()).toContain('16.0 GB');
    expect(wrapper.text()).toContain('50.0 GB');
    expect(wrapper.text()).toContain('Disk sampled at');
  });

  it('keeps an explicit empty state when no disk sample exists', () => {
    const wrapper = mount(OverviewSummary, { props: { server: server(), metrics: metrics({ diskCurrent: null }) } });

    const chart = wrapper.get('[data-testid="disk-usage-chart"]');
    expect(chart.attributes('data-empty')).toBe('true');
    expect(chart.attributes('aria-label')).toBe('Disk usage: —');
    expect(wrapper.text()).toContain('Waiting for the next disk sample');
    expect(wrapper.text()).not.toContain('0.0%');
  });
});
