import type { TaskDto, TaskLog, TaskStep } from '@/types/tasks';

const now = new Date('2026-08-01T08:00:00.000Z');

export const mockTasks: TaskDto[] = [
  task('task-deploy-1', 'op-deploy-storefront', 'application_deploy', 'running', 'app-storefront', 'Deploy storefront to edge nodes', 64, true, false),
  task('task-deploy-2', 'op-deploy-storefront', 'application_deploy_target', 'completed', 'srv-edge-sgp', 'Target edge-sgp-01 completed', 100, false, false),
  task('task-deploy-3', 'op-deploy-storefront', 'application_deploy_target', 'failed_retryable', 'srv-api-hkg', 'Target api-hkg-01 failed while pulling image', 38, true, true, 'registry timeout after 30s'),
  task('task-backup-1', 'op-backup-nightly', 'backup_export', 'scheduled', 'system', 'Nightly backup export is waiting for its window', 0, false, true),
  task('task-cert-1', 'op-cert-renew', 'certificate_renew', 'failed', 'cert-staging-failed', 'Certificate renewal failed DNS validation', 72, true, false, 'TXT record did not propagate before timeout'),
  task('task-cert-hooks', 'op-cert-hooks', 'certificate_renew', 'running', 'cert-hooks-renewing', 'Renew hooks.example.test certificate', 55, false, false),
  task('task-package-fleet-1', 'op-package-fleet-upgrade', 'package_upgrade', 'running', 'srv-worker-nrt', 'Upgrade worker fleet security packages', 46, false, false),
  task('task-package-fleet-2', 'op-package-fleet-upgrade', 'package_upgrade_target', 'queued', 'srv-legacy-lon', 'Waiting for maintenance lock on legacy-lon-ubuntu-20', 0, false, false),
  task('task-agent-rollout-1', 'op-agent-rollout', 'agent_deploy', 'failed_retryable', 'srv-media-syd', 'Agent rollout blocked by unsupported cgroup layout', 28, true, false, 'unsupported cgroup layout detected'),
  task('task-agent-rollout-2', 'op-agent-rollout', 'agent_deploy', 'completed', 'srv-api-hkg', 'Agent updated on api-hkg-01', 100, false, false),
  task('task-media-12', 'op-media-deploy', 'application_deploy', 'running', 'app-media', 'Deploy media-transcoder to GPU and media hosts', 41, false, false),
  task('task-canary-3', 'op-canary-failed', 'application_deploy', 'failed', 'app-canary-broken', 'Canary deploy failed readiness checks', 90, true, false, 'Canary probe failed: /ready returned 503'),
  task('task-billing-image', 'op-billing-image', 'application_image_update', 'completed', 'app-billing', 'billing-portal image update finished', 100, false, false),
  task('task-webhook-sync', 'op-webhook-sync', 'application_sync', 'running', 'app-webhook', 'Sync webhook-ingress across edge nodes', 62, false, false),
  task('task-fail2ban-lax', 'op-fail2ban-lax', 'server_fail2ban_apply', 'queued', 'srv-edge-lax', 'Apply fail2ban config on edge-lax-01', 0, false, false),
  ...Array.from({ length: 96 }, (_, index) => {
    const statuses = ['queued', 'running', 'completed', 'failed_retryable', 'scheduled', 'failed'];
    const status = statuses[index % statuses.length];
    const operationId = `op-sample-${String(Math.floor(index / 4) + 1).padStart(2, '0')}`;
    const serverIds = ['srv-edge-sgp', 'srv-edge-sgp-02', 'srv-core-fra', 'srv-api-hkg', 'srv-worker-nrt', 'srv-db-fra', 'srv-cache-sfo', 'srv-legacy-lon', 'srv-gpu-nrt', 'srv-backup-fra', 'srv-edge-lax'];
    const serverId = serverIds[index % serverIds.length];
    const type = index % 4 === 0 ? 'application_deploy_target' : index % 4 === 1 ? 'package_upgrade' : index % 4 === 2 ? 'container_image_check' : 'certificate_renew';
    const summary = index % 11 === 0
      ? `Sample paged task ${index + 1} with intentionally long operation text to validate wrapping inside the task center list and detail panels`
      : `Sample paged task ${index + 1}`;
    return task(`task-sample-${String(index + 1).padStart(2, '0')}`, operationId, type, status, index % 5 === 0 ? 'system' : serverId, summary, status === 'completed' ? 100 : (index * 13) % 91, status === 'failed_retryable', status === 'scheduled', status.includes('failed') ? 'sample failure detail for pagination and retry coverage' : '');
  }),
];

export const mockTaskSteps: Record<string, TaskStep[]> = {
  'task-deploy-1': [
    step('task-deploy-1', 'plan', 'completed', 100),
    step('task-deploy-1', 'pull', 'running', 64),
    step('task-deploy-1', 'health-check', 'queued', 0),
  ],
  'task-deploy-3': [
    step('task-deploy-3', 'plan', 'completed', 100),
    step('task-deploy-3', 'pull', 'failed_retryable', 38, 'registry timeout after 30s'),
  ],
  'task-cert-1': [
    step('task-cert-1', 'order', 'completed', 100),
    step('task-cert-1', 'dns-01', 'failed', 72, 'TXT record did not propagate before timeout'),
  ],
  'task-cert-hooks': [
    step('task-cert-hooks', 'order', 'completed', 100),
    step('task-cert-hooks', 'dns-01', 'running', 55),
    step('task-cert-hooks', 'finalize', 'queued', 0),
  ],
  'task-package-fleet-1': [
    step('task-package-fleet-1', 'refresh-indexes', 'completed', 100),
    step('task-package-fleet-1', 'download-packages', 'running', 46),
    step('task-package-fleet-1', 'restart-services', 'queued', 0),
  ],
  'task-agent-rollout-1': [
    step('task-agent-rollout-1', 'probe', 'completed', 100),
    step('task-agent-rollout-1', 'install-agent', 'failed_retryable', 28, 'unsupported cgroup layout detected'),
  ],
  'task-media-12': [
    step('task-media-12', 'plan', 'completed', 100),
    step('task-media-12', 'pull-gpu-image', 'running', 41),
    step('task-media-12', 'start-workers', 'queued', 0),
  ],
  'task-canary-3': [
    step('task-canary-3', 'plan', 'completed', 100),
    step('task-canary-3', 'deploy', 'completed', 100),
    step('task-canary-3', 'readiness', 'failed', 90, 'Canary probe failed: /ready returned 503'),
  ],
  'task-billing-image': [
    step('task-billing-image', 'check', 'completed', 100),
    step('task-billing-image', 'pull', 'completed', 100),
    step('task-billing-image', 'restart', 'completed', 100),
  ],
  'task-webhook-sync': [
    step('task-webhook-sync', 'diff', 'completed', 100),
    step('task-webhook-sync', 'apply-sgp', 'completed', 100),
    step('task-webhook-sync', 'apply-lax', 'running', 40),
  ],
};

export const mockTaskLogs: Record<string, TaskLog[]> = {
  'task-deploy-1': longLogs('task-deploy-1', 'Pulling ghcr.io/example/storefront:1.9.0'),
  'task-deploy-3': longLogs('task-deploy-3', 'retryable pull failure: registry timeout after 30s', 'stderr'),
  'task-cert-1': longLogs('task-cert-1', 'waiting for _acme-challenge.staging.internal.test TXT record', 'stderr'),
  'task-cert-hooks': longLogs('task-cert-hooks', 'renewing hooks.example.test via DNS-01'),
  'task-package-fleet-1': longLogs('task-package-fleet-1', 'apt upgrade running on worker fleet'),
  'task-agent-rollout-1': longLogs('task-agent-rollout-1', 'agent installer detected unsupported cgroup layout', 'stderr'),
  'task-media-12': longLogs('task-media-12', 'pulling transcoder image on gpu-nrt-render'),
  'task-canary-3': longLogs('task-canary-3', 'canary readiness failed on api-hkg-02-canary', 'stderr'),
  'task-billing-image': longLogs('task-billing-image', 'billing image digest updated on api-hkg-01 and edge-sgp-01'),
  'task-webhook-sync': longLogs('task-webhook-sync', 'syncing webhook-ingress config to edge-lax-01'),
};

export function task(id: string, operationId: string, type: string, status: string, resourceId: string, summary: string, percentage: number, allowRetry: boolean, allowRunNow: boolean, error = ''): TaskDto {
  return {
    id,
    operationId,
    type,
    status,
    stage: status === 'running' ? 'executing' : status,
    percentage,
    summary,
    error,
    serverId: resourceId.startsWith('srv-') ? resourceId : '',
    resourceType: resourceId === 'system' ? 'system' : type.split('_')[0],
    resourceId,
    triggeredBy: 'panel',
    retryCount: status === 'failed_retryable' ? 1 : 0,
    maxRetries: 3,
    nextRunAt: allowRunNow ? new Date(now.getTime() + 900000).toISOString() : undefined,
    createdAt: new Date(now.getTime() - numericSeed(id) * 45000).toISOString(),
    startedAt: status === 'running' ? new Date(now.getTime() - 240000).toISOString() : undefined,
    finishedAt: ['completed', 'failed'].includes(status) ? new Date(now.getTime() - 120000).toISOString() : undefined,
    allowRetry,
    allowRunNow,
  };
}

function numericSeed(id: string) {
  return id.split('').reduce((sum, char) => sum + char.charCodeAt(0), 0) % 240;
}

export function completedTask(prefix: string) {
  const taskId = `${prefix}-${Date.now()}`;
  mockTasks.unshift(task(taskId, `op-${prefix}`, 'server_resource_refresh', 'completed', 'system', `${prefix} finished`, 100, false, false));
  return taskId;
}

export function retryTask(taskId: string) {
  const source = mockTasks.find((item) => item.id === taskId);
  if (!source || !source.allowRetry) return null;
  const next = task(`task-retry-${Date.now()}`, source.operationId, source.type, 'queued', source.resourceId || '', `Retrying ${source.summary}`, 0, false, true);
  mockTasks.unshift(next);
  return next;
}

export function runTaskNow(taskId: string) {
  const source = mockTasks.find((item) => item.id === taskId);
  if (!source || !source.allowRunNow) return null;
  source.status = 'running';
  source.stage = 'starting';
  source.percentage = 5;
  source.startedAt = new Date().toISOString();
  source.allowRunNow = false;
  return source;
}

function step(taskId: string, name: string, status: string, percentage: number, error = ''): TaskStep {
  return { id: `${taskId}-${name}`, taskId, step: name, status, percentage, error, metadataJson: '{}', startedAt: now.toISOString() };
}

function longLogs(taskId: string, seed: string, stream = 'system'): TaskLog[] {
  return Array.from({ length: 42 }, (_, index) => ({
    cursor: index + 1,
    time: new Date(now.getTime() + index * 1000).toISOString(),
    stream,
    line: `${seed} :: line ${index + 1} :: task=${taskId}`,
  }));
}
