// @vitest-environment jsdom
import { mount, flushPromises } from '@vue/test-utils';
import { afterEach, describe, expect, it, vi } from 'vitest';
import FacilityDiagnostics from './FacilityDiagnostics.vue';
import { reverseProxyFacilityApi } from '@/api/facilityApps';
import { toastKey } from '@/components/ui/toast';
import type { ReverseProxyConfig, ProxyDiagnostics } from '@/types/facilityApps';

const config = {
  id: 'reverse_proxy', version: 1, deployments: [
    { serverId: 'a', serverName: 'Tokyo', observedState: 'running', operation: { id: 'job-a', operationId: 'intent-a', status: 'failed_retryable', errorCode: 'container_not_running', error: 'startup failed', errorDetail: 'containerLogs:\ncertificate missing', attempt: 2, nextRunAt: '2026-09-15T07:00:00Z' } },
    { serverId: 'b', serverName: 'Phoenix', observedState: 'running', operation: { id: 'job-b', status: 'succeeded' } },
  ],
} as ReverseProxyConfig;
function render() { return mount(FacilityDiagnostics, { props: { config }, global: { provide: { [toastKey as symbol]: { push: vi.fn() } }, stubs: { ActivityLink: true, StatusBadge: { props: ['status'], template: '<span>{{ status }}</span>' } } } }); }
afterEach(() => vi.restoreAllMocks());

describe('facility diagnostics', () => {
  it('shows each node failure and reads logs only on demand', async () => {
    const request = vi.spyOn(reverseProxyFacilityApi, 'getDiagnostics').mockResolvedValue({ serverId: 'a', status: 'sampled', checkedAt: '2026-09-15T06:00:00Z', truncated: false, issues: [{ code: 'upstream_tls_untrusted', domain: 'app.example.test', upstream: '10.0.0.2:443', count: 3 }] });
    const wrapper = render();
    expect(request).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain('Tokyo');
    expect(wrapper.text()).toContain('Phoenix');
    expect(wrapper.text()).toContain('failed_retryable');
    expect(wrapper.text()).toContain('certificate missing');
    await wrapper.find('button').trigger('click');
    await flushPromises();
    expect(request).toHaveBeenCalledTimes(1);
    expect(wrapper.text()).toContain('app.example.test');
    expect(wrapper.text()).toContain('Upstream certificate verification failed');
    expect(wrapper.text()).toContain('Keep verification enabled');
    wrapper.unmount();
  });

  it('discards checks from an old configuration and aborts on leave', async () => {
    let resolve!: (result: ProxyDiagnostics) => void;
    const request = vi.spyOn(reverseProxyFacilityApi, 'getDiagnostics').mockImplementation(() => new Promise((done) => { resolve = done; }));
    const wrapper = render();
    await wrapper.find('button').trigger('click');
    const signal = request.mock.calls[0][1]?.signal;
    await wrapper.setProps({ config: { ...config, version: 2 } });
    expect(signal?.aborted).toBe(true);
    resolve({ serverId: 'a', status: 'sampled', checkedAt: '', truncated: false, issues: [{ code: 'upstream_tls_untrusted', domain: 'stale.example.test', count: 1 }] });
    await flushPromises();
    expect(wrapper.text()).not.toContain('stale.example.test');
    await wrapper.find('button').trigger('click');
    wrapper.unmount();
    expect(request.mock.calls[1][1]?.signal?.aborted).toBe(true);
  });
});
