import type { ApplicationOperationDetailDto, ApplicationOperationDto, ApplicationOperationStageDto, ApplicationOperationTargetDto } from '@/types/applicationOperations';
import type { SystemEventDto } from '@/types/systemEvents';

const now = new Date('2026-08-01T08:00:00.000Z');

export const mockApplicationOperations: ApplicationOperationDto[] = [
  operation('op-apply-storefront', 'app-storefront', 'storefront', 'apply', 'user', 'partial_failed', 2, 1, 1, 'edge-02 health check failed: /ready returned 503'),
  operation('op-recover-api', 'app-api', 'public-api', 'recover', 'system', 'failed', 2, 0, 1, 'worker-01 image pull timeout: registry 30s no response'),
  operation('op-stop-preview', 'app-disabled', 'disabled-preview', 'stop', 'user', 'succeeded', 2, 2, 0, ''),
  operation('op-billing-image', 'app-billing', 'billing-portal', 'image_update', 'user', 'succeeded', 2, 2, 0, ''),
  operation('op-media-deploy', 'app-media', 'media-transcoder', 'apply', 'user', 'running', 1, 0, 0, ''),
  operation('op-canary-failed', 'app-canary-broken', 'checkout-canary', 'apply', 'user', 'failed', 1, 0, 1, 'Canary probe failed: /ready returned 503'),
  operation('op-facility-deploy', 'facility-reverse-proxy', '__panel_facility_reverse_proxy__', 'apply', 'user', 'succeeded', 2, 2, 0, ''),
  ...Array.from({ length: 48 }, (_, index) => {
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
      failed ? 'Target reported a runtime error' : '',
    );
  }),
];

export const mockSystemEvents: SystemEventDto[] = [
  event('evt-deploy-fail', 'application.operation.failed', 'application', 'error', 'applications', 'Deploy storefront failed: container startup timed out (120s)'),
  event('evt-agent-down', 'agent.disconnected', 'system', 'warning', 'agent', 'Agent report stream disconnected: edge-lax-01: connection timed out'),
  event('evt-deploy-done', 'application.operation.completed', 'application', 'info', 'applications', 'Deploy api-gateway completed (2/2 targets succeeded)'),
  event('evt-cert-fail', 'task.failed', 'task', 'warning', 'tasks', 'ACME certificate renewal failed: connection timed out, will retry in 5 minutes'),
  event('evt-agent-up', 'agent.connected', 'system', 'info', 'agent', 'Agent report stream connected: edge-lax-01'),
  event('evt-task-done', 'task.completed', 'task', 'info', 'tasks', 'DNS record sync completed (3 records)'),
  ...Array.from({ length: 56 }, (_, index) => {
    const categories = ['application', 'task', 'system'];
    const severities = ['info', 'warning', 'error'];
    const types = ['task.completed', 'task.failed', 'application.operation.completed', 'application.operation.failed', 'agent.connected', 'agent.disconnected'];
    return event(
      `evt-sample-${String(index + 1).padStart(2, '0')}`,
      types[index % types.length],
      categories[index % categories.length],
      severities[index % severities.length],
      index % 3 === 0 ? 'applications' : index % 3 === 1 ? 'tasks' : 'agent',
      `Sample system log ${index + 1}`,
    );
  }),
];

export function applicationOperationDetail(operationId: string): ApplicationOperationDetailDto | null {
  const found = mockApplicationOperations.find((item) => item.operationId === operationId);
  if (!found) return null;
  const startedAt = found.startedAt;
  const endedAt = found.finishedAt || found.latestAt;
  const targets: ApplicationOperationTargetDto[] = [];
  for (let index = 0; index < found.targetTotal; index++) {
    const failed = index < found.targetFailed;
    const succeeded = !failed && index < found.targetSucceeded;
    const status = failed ? 'failed' : succeeded ? 'succeeded' : found.status === 'queued' ? 'queued' : 'running';
    const serverId = `srv-edge-${index + 1}`;
    const serverName = `edge-${index + 1}`;
    const stages: ApplicationOperationStageDto[] = stageSamples(found, index, status, failed, startedAt, endedAt);
    targets.push({
      id: `${operationId}-target-${index + 1}`,
      operationId,
      applicationId: found.applicationId,
      serverId,
      serverName,
      action: found.action,
      state: failed ? 'failed' : succeeded ? 'succeeded' : status === 'queued' ? 'ready' : 'applying',
      status,
      stage: failed ? 'verify' : status === 'queued' ? 'waiting' : 'verify',
      claimedTaskId: `task-deploy-${index + 1}`,
      desiredState: 'running',
      desiredGeneration: 3,
      desiredSpecHash: '8f2a1c9d',
      observedState: found.action === 'stop' ? 'running' : index === 0 ? 'exited' : 'stopped',
      observedExitCode: index === 0 ? '137' : '',
      observedError: index === 0 ? 'OOMKilled' : '',
      observedGeneration: 2,
      observedSpecHash: 'c91b7e42',
      observedImage: 'ghcr.io/example/storefront:1.8.0',
      observedAt: startedAt,
      errorCode: failed ? 'verify_failed' : '',
      errorMessage: failed ? found.failureSummary || 'Target reported a runtime error.' : '',
      errorDetail: failed ? 'Readiness probe timed out after 30s.' : '',
      createdAt: startedAt,
      startedAt,
      finishedAt: status === 'succeeded' || failed ? endedAt : undefined,
      updatedAt: status === 'succeeded' || failed ? endedAt : found.latestAt,
      stages,
    });
  }
  // 一台“一致”服务器样本
  targets.push({
    id: `${operationId}-consistent-edge-9`,
    operationId,
    applicationId: found.applicationId,
    serverId: 'srv-edge-9',
    serverName: 'edge-9',
    state: 'consistent',
    status: 'consistent',
    desiredState: 'running',
    desiredGeneration: 3,
    createdAt: startedAt,
    updatedAt: endedAt,
    stages: [],
  });
  return { operation: found, targets };
}

function stageSamples(found: ApplicationOperationDto, index: number, status: string, failed: boolean, startedAt: string | undefined, endedAt: string): ApplicationOperationStageDto[] {
  const base = new Date(startedAt ?? now.toISOString()).getTime();
  const steps: Array<{ stage: string; seconds: number; detail: string }> = [
    { stage: 'write_files', seconds: 36, detail: index === 0 ? '已写入 5 个文件：nginx.conf、app.env、docker-compose.yml、.env.production、health-check.sh' : '已写入 5 个文件：nginx.conf、app.env、docker-compose.yml、.env.production、health-check.sh' },
    { stage: 'pull_image', seconds: 15, detail: '镜像 ghcr.io/example/storefront:1.9.0 已存在，跳过下载（digest abc123...）' },
    { stage: 'create_container', seconds: 44, detail: `容器 storefront-edge${index + 1}（ID 8be2...d11）已创建，端口映射 3000:8080，内存 512MB` },
    { stage: 'start_container', seconds: 12, detail: '容器已启动，状态 running' },
    { stage: 'verify', seconds: 51, detail: failed ? 'GET /ready 返回 503 Service Unavailable，30 秒超时，错误码 verify_failed' : 'GET /ready 返回 200 OK，耗时 90ms' },
  ];
  let cursor = base;
  return steps.map((step, stepIndex) => {
    const start = cursor;
    cursor += step.seconds * 1000;
    const isLast = stepIndex === steps.length - 1;
    const stepStatus = failed && isLast ? 'failed' : 'succeeded';
    return {
      id: `${found.operationId}-stage-${index + 1}-${stepIndex + 1}`,
      stage: step.stage,
      status: stepStatus,
      detail: step.detail,
      startedAt: new Date(start).toISOString(),
      finishedAt: new Date(start + step.seconds * 1000).toISOString(),
    };
  });
}


function operation(
  operationId: string,
  applicationId: string,
  applicationName: string,
  action: string,
  source: string,
  status: string,
  targetTotal: number,
  targetSucceeded: number,
  targetFailed: number,
  failureSummary: string,
): ApplicationOperationDto {
  const seed = numericSeed(operationId);
  const latest = new Date(now.getTime() - seed * 60000).toISOString();
  return {
    operationId,
    applicationId,
    applicationName,
    action,
    source,
    triggeredBy: source === 'user' ? 'admin' : source,
    status,
    startedAt: new Date(now.getTime() - seed * 90000).toISOString(),
    finishedAt: ['succeeded', 'failed', 'partial_failed', 'cancelled'].includes(status) ? latest : undefined,
    targetTotal,
    targetSucceeded,
    targetFailed,
    targetServers: Array.from({ length: targetTotal }, (_, index) => `edge-${index + 1}`).concat('edge-9'),
    latestAt: latest,
    failureSummary,
    createdAt: new Date(now.getTime() - seed * 95000).toISOString(),
    updatedAt: latest,
  };
}

function event(
  id: string,
  eventType: string,
  category: string,
  severity: string,
  sourceModule: string,
  summary: string,
): SystemEventDto {
  const seed = numericSeed(id);
  return {
    id,
    eventType,
    category,
    severity,
    source: sourceModule,
    sourceModule,
    summary,
    occurredAt: new Date(now.getTime() - seed * 45000).toISOString(),
  };
}

function numericSeed(id: string) {
  return id.split('').reduce((sum, char) => sum + char.charCodeAt(0), 0) % 180;
}