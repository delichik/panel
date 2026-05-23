import type { RuntimeExplorerContainerDto } from '@/types/api';

interface RuntimeActionState {
  disabled: boolean;
  reason: string;
}

export interface RuntimeContainerActions {
  restart: RuntimeActionState;
  stop: RuntimeActionState;
  delete: RuntimeActionState;
  serviceLink: string;
}

export function runtimeContainerActions(container: RuntimeExplorerContainerDto): RuntimeContainerActions {
  const managedReason = 'Managed resources are operated from Container Service. Runtime Explorer only allows restart.';
  const serviceLink = container.managed && container.serviceId ? `/container-services?service=${encodeURIComponent(container.serviceId)}` : '';
  return {
    restart: { disabled: false, reason: '' },
    stop: { disabled: Boolean(container.managed), reason: container.managed ? managedReason : '' },
    delete: { disabled: Boolean(container.managed), reason: container.managed ? managedReason : '' },
    serviceLink,
  };
}
