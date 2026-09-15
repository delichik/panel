import type { DebugDatabaseSnapshots, DebugPprofStatus, DebugRuntimeSnapshot, DebugTaskSnapshot } from '@/types/debug';
import type { ClearRuntimeDataStatus } from '@/api/debug';

let failNextSnapshot = false;

export function setDebugFailure(value: boolean) {
  failNextSnapshot = value;
}

function failIfRequested() {
  if (failNextSnapshot) {
    failNextSnapshot = false;
    throw new Error('Diagnostics collection timed out while reading database table sizes.');
  }
}

export function debugRuntime(): DebugRuntimeSnapshot {
  return {
    collectedAt: new Date().toISOString(),
    process: { startedAt: '2026-07-21T06:00:00.000Z', uptimeSeconds: 7200, pid: 4242, goVersion: 'go1.24', os: 'windows', architecture: 'amd64', cpuCount: 12, goroutineCount: 48, cgoCallCount: 0 },
    memory: { allocBytes: 32000000, sysBytes: 128000000, heapAllocBytes: 28000000, heapObjects: 18200, gcCycles: 8, nextGcBytes: 64000000 },
  };
}

export function debugTasks(): DebugTaskSnapshot {
  return {
    collectedAt: new Date().toISOString(),
    tasks: { runningExecutions: 3, registeredTypes: 18, queued: 2, failedRetryable: 1 },
  };
}

export function debugDatabases(): DebugDatabaseSnapshots {
  failIfRequested();
  return {
    collectedAt: new Date().toISOString(),
    databases: [
      { name: 'app', healthy: true, fileSizeBytes: 870000, pageSizeBytes: 4096, pageCount: 240, freePageCount: 8, usedBytes: 950272, freeBytes: 32768, connections: { openConnections: 2, inUse: 1, idle: 1 }, tables: [{ name: 'servers', rowCount: 12, dataSizeBytes: 42000, indexSizeBytes: 12000, totalSizeBytes: 54000, databasePercent: 5.4 }] },
      { name: 'log', healthy: false, errorCode: 'database_tables_unavailable', fileSizeBytes: 1940000, pageSizeBytes: 4096, pageCount: 510, freePageCount: 4, usedBytes: 2072576, freeBytes: 16384, connections: { openConnections: 1, inUse: 0, idle: 1 }, tables: [] },
      { name: 'metrics', healthy: true, fileSizeBytes: 8100000, pageSizeBytes: 4096, pageCount: 2100, freePageCount: 40, usedBytes: 8437760, freeBytes: 163840, connections: { openConnections: 1, inUse: 0, idle: 1 }, tables: [{ name: 'metrics_snapshots', rowCount: 28000, dataSizeBytes: 6200000, indexSizeBytes: 900000, totalSizeBytes: 7100000, databasePercent: 84.1 }] },
    ],
  };
}

let pprofEnabled = false;

export function debugPprofStatus(): DebugPprofStatus {
  return { enabled: pprofEnabled, address: '127.0.0.1:6060' };
}

export function setDebugPprof(enabled: boolean): DebugPprofStatus {
  pprofEnabled = enabled;
  return debugPprofStatus();
}

let cleanupStatus: ClearRuntimeDataStatus = { cleared: false, running: false, status: 'idle' };
let cleanupStarted = 0;
let cleanupSequence = 0;

export function startDebugClearRuntimeData() {
  if (debugClearRuntimeDataStatus().running) return { ...cleanupStatus };
  cleanupStarted = Date.now();
  cleanupStatus = { cleared: false, running: true, status: 'running', runId: `mock-cleanup-${++cleanupSequence}`, stage: 'stopping_workers', startedAt: new Date(cleanupStarted).toISOString() };
  return { ...cleanupStatus };
}

export function debugClearRuntimeDataStatus(): ClearRuntimeDataStatus {
  if (cleanupStatus.running) {
    const stages = ['stopping_workers', 'clearing_coordination', 'clearing_logs', 'clearing_metrics', 'compacting', 'resuming_workers'];
    const step = Math.floor((Date.now() - cleanupStarted) / 1000);
    if (step < stages.length) cleanupStatus.stage = stages[step];
    else cleanupStatus = { ...cleanupStatus, cleared: true, running: false, status: 'succeeded', stage: 'completed', finishedAt: new Date().toISOString() };
  }
  return { ...cleanupStatus };
}
