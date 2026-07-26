export interface DebugSnapshot {
  collectedAt: string;
  process: {
    startedAt: string;
    uptimeSeconds: number;
    pid: number;
    goVersion: string;
    os: string;
    architecture: string;
    cpuCount: number;
    goroutineCount: number;
    cgoCallCount: number;
  };
  memory: Record<string, number | string | null>;
  tasks: DebugTaskRuntime;
  databases: DebugDatabase[];
}

export interface DebugTaskRuntime {
  workerRunning?: boolean;
  registeredTypes?: number;
  executableTypes?: number;
  periodicTypes?: number;
  runningExecutions?: number;
  definitions?: DebugTaskDefinition[];
  [key: string]: unknown;
}

export interface DebugTaskDefinition {
  type: string;
  hidden: boolean;
  executable: boolean;
  periodic: boolean;
  allowRunNow: boolean;
  allowRetry: boolean;
  defaultMaxRetries: number;
  concurrencyPolicy: string;
  staleQueuedAfterSeconds: number;
  periodicIntervalSeconds: number;
}

export interface DebugDatabase {
  name: string;
  healthy: boolean;
  errorCode?: string;
  tableSizeErrorCode?: string;
  fileSizeBytes: number;
  pageSizeBytes: number;
  pageCount: number;
  freePageCount: number;
  usedBytes: number;
  freeBytes: number;
  connections: Record<string, number>;
  tables: DebugTable[];
}

export interface DebugTable {
  name: string;
  rowCount: number;
  dataSizeBytes: number;
  indexSizeBytes: number;
  totalSizeBytes: number;
  databasePercent: number;
  errorCode?: string;
}
