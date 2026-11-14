/** Product DTO for one indexed event / process row. */
export interface Event {
  id: string;
  module: string;
  level: string;
  origin: string;
  operationId?: string;
  executionId?: string;
  requestId?: string;
  title?: string;
  summary?: string;
  state?: string;
  hadError: boolean;
  startedAt?: string;
  updatedAt?: string;
  finishedAt?: string;
}

export interface EventPage {
  items: Event[];
  nextCursor?: string;
  hasMore: boolean;
}

/** One JSONL log line; backend returns a flexible map with stable core fields. */
export interface LogLine {
  ts?: string;
  level?: string;
  module?: string;
  eventId?: string;
  msg?: string;
  [key: string]: unknown;
}

export interface LogLinesPage {
  items: LogLine[];
  nextCursor?: string;
  hasMore: boolean;
}

export type EventQuery = Record<string, string | number | boolean | undefined>;
export type LogsQuery = Record<string, string | number | boolean | undefined>;
