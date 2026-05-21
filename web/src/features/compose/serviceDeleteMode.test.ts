import type { ComposeServiceDto } from '@/types/api';
import { serviceDeleteMode } from './serviceDeleteMode';

function service(overrides: Partial<ComposeServiceDto>): ComposeServiceDto {
  return {
    id: 'svc_1',
    name: 'web',
    serverId: 'srv_1',
    templateId: 'tmpl_1',
    templateVersion: 1,
    remotePath: '/opt/panel/services/web',
    values: {},
    labels: {},
    status: 'ready',
    drift: false,
    createdAt: '',
    updatedAt: '',
    ...overrides,
  };
}

describe('serviceDeleteMode', () => {
  it('deletes pending local records directly', () => {
    expect(serviceDeleteMode(service({ managementState: 'pending', status: 'draft' }))).toBe('metadata');
  });

  it('removes deployed services through lifecycle reconciliation', () => {
    expect(serviceDeleteMode(service({ managementState: 'managed', status: 'ready' }))).toBe('lifecycle');
  });
});
