import type { ComposeServiceDto } from '@/types/api';

export type ServiceDeleteMode = 'metadata' | 'lifecycle';

export function serviceDeleteMode(service: ComposeServiceDto): ServiceDeleteMode {
  const state = service.managementState || service.status;
  return state === 'pending' || state === 'draft' || state === 'removed' ? 'metadata' : 'lifecycle';
}
