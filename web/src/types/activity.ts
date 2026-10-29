export interface ActivityReceipt {
  operationId?: string;
  acceptedEventId?: string;
  acceptedSeq?: number;
  executionId?: string;
  taskId?: string;
}
export interface ActivityResource {
  resourceType: string;
  resourceId: string;
  nameSnapshot?: string;
  role?: string;
  revisionId?: string;
  generation?: number;
  snapshot?: Record<string, unknown>;
}
export interface ActivityActor { kind?: string; id?: string; name?: string }
export interface ActivityEvent {
  eventId: string;
  seq: number;
  eventVersion: number;
  eventType: string;
  kind: string;
  level: string;
  domain: string;
  action?: string;
  operationId?: string;
  runId?: string;
  executionId?: string;
  stepId?: string;
  causationEventId?: string;
  occurredAt: string;
  recordedAt: string;
  trigger?: string;
  actor?: ActivityActor;
  initiator?: ActivityActor;
  sourceId?: string;
  sourceEpoch?: string;
  sourceStreamId?: string;
  sourceSeq?: number;
  resources: ActivityResource[];
  text?: string;
  messageCode?: string;
  messageArgs?: Record<string, string | number>;
  stream?: string;
  data?: Record<string, unknown>;
}
export interface ActivityOperation {
  operationId: string;
  domain: string;
  action: string;
  title: string;
  phase: string;
  result?: string;
  attention: boolean;
  uncertainty?: boolean;
  hadError?: boolean;
  createdAt: string;
  updatedAt: string;
  startedAt?: string;
  finishedAt?: string;
  firstSeq: number;
  lastSeq: number;
  eventCount: number;
  attemptCount: number;
  evidenceComplete: boolean;
  trigger?: string;
  actor?: ActivityActor;
  resources: ActivityResource[];
  failureSummary?: string;
}
export interface ActivityPage<T> {
  items: T[];
  nextCursor: string;
  hasMore: boolean;
  snapshotSeq: number;
  headSeq: number;
  projectedThroughSeq: number;
  indexState: string;
}
export interface ActivityExecution { executionId: string; runId?: string; phase: string; result?: string; resources: ActivityResource[]; startedAt?: string; finishedAt?: string }
export interface ActivityStep { stepId: string; parentStepId?: string; executionId: string; name: string; phase: string; result?: string; startedAt?: string; finishedAt?: string }
export interface ActivityOperationDetail {
  operation: ActivityOperation;
  events: ActivityEvent[];
  executions: ActivityExecution[];
  steps: ActivityStep[];
  relatedOperations: string[];
  availableCommands: Array<{ kind: string; executionId: string; label: string }>;
  snapshotSeq: number;
  headSeq: number;
  hasMore: boolean;
  nextCursor?: string;
}
export type ActivityContext = ActivityPage<ActivityEvent>;
export interface ActivityCapacity { state: 'ok' | 'warning' | 'blocked' | 'unknown'; availableBytes: number; totalBytes: number }
export interface ActivitySummary { capacity?: ActivityCapacity; total: number; byLevel: Record<string, number>; byDomain: Record<string, number>; snapshotSeq: number; headSeq: number; projectedThroughSeq: number }
export type ActivityQuery = Record<string, string | number | boolean | undefined>;
