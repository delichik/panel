import { runtimeContainerActions } from './runtimeActions';
import type { RuntimeExplorerContainerDto } from '@/types/api';

function container(overrides: Partial<RuntimeExplorerContainerDto>): RuntimeExplorerContainerDto {
  return {
    id: 'abc123',
    name: 'web',
    image: 'nginx',
    state: 'running',
    status: 'Up',
    ports: [],
    labels: {},
    managed: false,
    serviceId: null,
    serviceName: null,
    observedAt: '2026-05-23T00:00:00Z',
    ...overrides,
  };
}

describe('runtimeContainerActions', () => {
  it('allows managed restart and delete but blocks managed stop', () => {
    const actions = runtimeContainerActions(container({ managed: true, serviceId: 'svc-1', serviceName: 'web' }));

    expect(actions.restart.disabled).toBe(false);
    expect(actions.stop.disabled).toBe(true);
    expect(actions.delete.disabled).toBe(false);
    expect(actions.stop.reason).toContain('Container Service');
    expect(actions.delete.reason).toContain('automatically redeploy');
    expect(actions.serviceLink).toBe('/container-services?service=svc-1');
  });

  it('allows limited unmanaged stop and delete actions', () => {
    const actions = runtimeContainerActions(container({ managed: false }));

    expect(actions.restart.disabled).toBe(false);
    expect(actions.stop.disabled).toBe(false);
    expect(actions.delete.disabled).toBe(false);
    expect(actions.serviceLink).toBe('');
  });
});
