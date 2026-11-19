import { apiClient } from './client';
import type { EventNotificationPage } from '@/types/notifications';

/** Server-side long-poll window; the request timeout adds a safety margin on top. */
export const NOTIFICATION_WAIT_SECONDS = 20;

/** Extra seconds granted on top of the long-poll window before the client aborts. */
const NOTIFICATION_TIMEOUT_MARGIN_MS = 10 * 1000;

export const notificationsApi = {
  /**
   * Long-polls `GET /api/v1/notifications`.
   *
   * `after = 0` (or a cursor newer than the hub) immediately returns the current
   * baseline cursor with no replayed history; otherwise the request parks until
   * a new notification is published or the wait window expires.
   */
  wait: (after: number, waitSeconds = NOTIFICATION_WAIT_SECONDS, signal?: AbortSignal) =>
    apiClient.get<EventNotificationPage>(
      `/notifications?after=${encodeURIComponent(String(after))}&wait=${encodeURIComponent(String(waitSeconds))}`,
      {
        signal,
        timeoutMs: (waitSeconds * 1000) + NOTIFICATION_TIMEOUT_MARGIN_MS,
      },
    ),
};
