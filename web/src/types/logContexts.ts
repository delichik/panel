export interface LogContextActor {
  kind?: string;
  id?: string;
  name?: string;
}

export interface LogContextResource {
  resourceType: string;
  resourceId: string;
  nameSnapshot?: string;
  role?: string;
}

/** Product DTO for one log_contexts stream index row. */
export interface LogContext {
  streamId: string;
  module: string;
  origin: string;
  operationId?: string;
  executionId?: string;
  requestId?: string;
  title?: string;
  summary?: string;
  result?: string;
  hadError: boolean;
  startedAt?: string;
  updatedAt?: string;
  finishedAt?: string;
  actor?: LogContextActor;
  resources?: LogContextResource[];
}

export interface LogContextPage {
  items: LogContext[];
  nextCursor?: string;
  hasMore: boolean;
}

/** One JSONL log line; backend returns a flexible map with stable core fields. */
export interface LogContextLine {
  ts?: string;
  level?: string;
  module?: string;
  streamId?: string;
  msg?: string;
  [key: string]: unknown;
}

export interface LogContextLinesPage {
  items: LogContextLine[];
  nextCursor?: string;
  hasMore: boolean;
}

export type LogContextQuery = Record<string, string | number | boolean | undefined>;
