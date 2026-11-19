/**
 * One process-event notification delivered by the in-memory notification hub.
 *
 * The hub does not read the database: these rows mirror the event stream's
 * "started" + "terminal" milestones (HTTP requests are never published).
 * JSON contract mirrors `internal/platform/notifications.Notification`.
 */
export interface EventNotification {
  /** Monotonic hub sequence; clients de-duplicate by it. */
  seq: number;
  eventId: string;
  module: string;
  title: string;
  summary?: string;
  state?: string;
  level: string;
  origin?: string;
  hadError: boolean;
  /** RFC3339Nano UTC timestamp. */
  updatedAt: string;
}

/** Long-poll page: `cursor` is the hub sequence to pass as the next `after`. */
export interface EventNotificationPage {
  items: EventNotification[];
  cursor: number;
}
