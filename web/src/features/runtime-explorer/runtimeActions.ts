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
  const managedStopReason = 'Managed resources are operated from Container Service. Runtime Explorer only allows restart or confirmed delete.';
  const managedDeleteReason = 'Managed container: deleting it removes the current runtime container, and the enabled service will automatically redeploy it.';
  const serviceLink = container.managed && container.serviceId ? `/container-services?service=${encodeURIComponent(container.serviceId)}` : '';
  return {
    restart: { disabled: false, reason: '' },
    stop: { disabled: Boolean(container.managed), reason: container.managed ? managedStopReason : '' },
    delete: { disabled: false, reason: container.managed ? managedDeleteReason : '' },
    serviceLink,
  };
}
