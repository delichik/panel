import type { ApplicationOperationDetailDto, ApplicationOperationDto } from '@/types/applicationOperations';
import type { SystemEventDetailDto, SystemEventDto } from '@/types/systemEvents';

const now = new Date('2026-07-25T08:00:00.000Z');

export const mockApplicationOperations: ApplicationOperationDto[] = [
  operation('op-apply-storefront', 'app-storefront', 'Storefront', 'apply', 'user', 'running', 3, 1, 0, true, ''),
  operation('op-recover-api', 'app-api', 'Public API', 'recover', 'system', 'partial_failed', 4, 2, 1, true, 'edge-2 failed health verification'),
  operation('op-stop-preview', 'app-preview', 'Preview app', 'stop', 'user', 'succeeded', 2, 2, 0, false, ''),
  ...Array.from({ length: 34 }, (_, index) => {
    const statuses = ['queued', 'running', 'succeeded', 'failed', 'partial_failed', 'cancelled'];
    const actions = ['apply', 'sync', 'stop', 'purge'];
    const sources = ['user', 'system', 'scheduler'];
    const total = (index % 4) + 1;
    const failed = statuses[index % statuses.length] === 'failed' ? 1 : statuses[index % statuses.length] === 'partial_failed' ? 1 : 0;
    return operation(
      `op-sample-${String(index + 1).padStart(2, '0')}`,
      `app-sample-${(index % 8) + 1}`,
      `Sample app ${(index % 8) + 1}`,
      actions[index % actions.length],
      sources[index % sources.length],
      statuses[index % statuses.length],
      total,
      Math.max(0, total - failed - (statuses[index % statuses.length] === 'queued' ? total : 0)),
      failed,
      index % 7 !== 0,
      failed ? 'Target reported a runtime error' : '',
    );
  }),
];

export const mockSystemEvents: SystemEventDto[] = [
  event('evt-task-failed', 'task.failed', 'task', 'error', 'task', 'task-agent-rollout-1', 'tasks', 'Agent rollout failed and can be retried.', true),
  event('evt-log-attached', 'log.attached', 'log', 'info', 'application', 'app-storefront', 'applications', 'Runtime log reference attached to application operation.', true),
  event('evt-warning-pruned', 'event.detail.pruned', 'runtime', 'warning', 'operation', 'op-stop-preview', 'runtime-events', 'Event detail was cleaned after retention elapsed.', false),
  ...Array.from({ length: 42 }, (_, index) => {
    const categories = ['application', 'task', 'alert', 'log', 'runtime', 'system'];
    const severities = ['info', 'warning', 'error'];
    return event(
      `evt-sample-${String(index + 1).padStart(2, '0')}`,
      index % 5 === 0 ? 'task.failed' : index % 5 === 1 ? 'task.completed' : index % 5 === 2 ? 'log.attached' : index % 5 === 3 ? 'alert.raised' : 'task.started',
      categories[index % categories.length],
      severities[index % severities.length],
      index % 2 === 0 ? 'server' : 'application',
      index % 2 === 0 ? `srv-${index + 1}` : `app-sample-${(index % 8) + 1}`,
      categories[index % categories.length],
      `Sample diagnostic event ${index + 1}`,
      index % 6 !== 0,
    );
  }),
];

export function applicationOperationDetail(operationId: string): ApplicationOperationDetailDto | null {
  const operation = mockApplicationOperations.find((item) => item.operationId === operationId);
  if (!operation || !operation.detailAvailable) return null;
  return {
    operation,
    targets: Array.from({ length: operation.targetTotal }, (_, index) => ({
      id: `${operation.operationId}-target-${index + 1}`,
      serverId: `srv-edge-${index + 1}`,
      serverName: `edge-${index + 1}`,
      action: operation.action,
      status: index < operation.targetFailed ? 'failed' : index < operation.targetSucceeded ? 'succeeded' : operation.status === 'queued' ? 'queued' : 'running',
      stage: index < operation.targetFailed ? 'verify' : 'apply',
      error: index < operation.targetFailed ? operation.failureSummary || 'Target failed.' : '',
      logRef: `log:${operation.operationId}:${index + 1}`,
      updatedAt: operation.latestEventAt,
    })),
    events: [
      {
        id: `${operation.operationId}-created`,
        eventType: 'application.operation.created',
        severity: 'info',
        summary: `${operation.applicationNameSnapshot} operation was created.`,
        occurredAt: operation.startedAt,
        detailAvailable: true,
      },
      {
        id: `${operation.operationId}-latest`,
        eventType: operation.status === 'failed' ? 'application.operation.failed' : 'application.operation.target.started',
        severity: operation.targetFailed ? 'error' : 'info',
        summary: operation.failureSummary || `${operation.applicationNameSnapshot} operation updated.`,
        occurredAt: operation.latestEventAt,
        detailAvailable: true,
      },
    ],
  };
}

export function systemEventDetail(eventId: string): SystemEventDetailDto | null {
  const found = mockSystemEvents.find((item) => item.id === eventId);
  if (!found || !found.detailAvailable) return null;
  return {
    event: found,
    payload: { category: found.category, subjectType: found.subjectType, subjectId: found.subjectId },
    logRefs: found.category === 'log' ? [{ label: 'runtime log', source: found.sourceModule || found.source }] : [],
    taskRefs: found.category === 'task' && found.subjectType === 'task' && found.subjectId ? [found.subjectId] : [],
    targetRefs: found.subjectType === 'operation' && found.subjectId ? [found.subjectId] : [],
  };
}

function operation(
  operationId: string,
  applicationId: string,
  applicationNameSnapshot: string,
  action: string,
  source: string,
  status: string,
  targetTotal: number,
  targetSucceeded: number,
  targetFailed: number,
  detailAvailable: boolean,
  failureSummary: string,
): ApplicationOperationDto {
  const seed = numericSeed(operationId);
  const latest = new Date(now.getTime() - seed * 60000).toISOString();
  return {
    operationId,
    applicationId,
    applicationNameSnapshot,
    action,
    source,
    triggeredBy: source === 'user' ? 'admin' : source,
    triggerReason: source === 'system' ? 'runtime drift detected' : '',
    status,
    startedAt: new Date(now.getTime() - seed * 90000).toISOString(),
    finishedAt: ['succeeded', 'failed', 'partial_failed', 'cancelled'].includes(status) ? latest : undefined,
    targetTotal,
    targetSucceeded,
    targetFailed,
    latestEventAt: latest,
    detailAvailable,
    detailPrunedAt: detailAvailable ? undefined : new Date(now.getTime() - seed * 30000).toISOString(),
    failureSummary,
  };
}

function event(
  id: string,
  eventType: string,
  category: string,
  severity: string,
  subjectType: string,
  subjectId: string,
  sourceModule: string,
  summary: string,
  detailAvailable: boolean,
): SystemEventDto {
  const seed = numericSeed(id);
  return {
    id,
    eventType,
    category,
    severity,
    subjectType,
    subjectId,
    source: sourceModule,
    sourceModule,
    summary,
    occurredAt: new Date(now.getTime() - seed * 45000).toISOString(),
    detailAvailable,
    detailPrunedAt: detailAvailable ? undefined : new Date(now.getTime() - seed * 30000).toISOString(),
  };
}

function numericSeed(id: string) {
  return id.split('').reduce((sum, char) => sum + char.charCodeAt(0), 0) % 180;
}
