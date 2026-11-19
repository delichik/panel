<script setup lang="ts">
import { onBeforeUnmount, onMounted } from 'vue';
import { eventsPath } from '@/api/activityReference';
import { notificationsApi, NOTIFICATION_WAIT_SECONDS } from '@/api/notifications';
import { useToast, type ToastTone } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { EventNotification, EventNotificationPage } from '@/types/notifications';

/**
 * 右上角事件通知桥：只把后端过程事件（部署/任务执行/证书/备份…）的“开始/成功/失败”
 * 以 toast 呈现。HTTP 请求永远不是事件；报错绝不在这里弹窗（由页面就地处理或
 * `useErrorDialog` 负责）。
 *
 * 长轮询契约：
 * - 首连 `wait(0)` 只取基线游标，不回放历史；
 * - 之后 `wait(cursor)` 循环，服务端无事件时挂起最多 20s，不存在忙轮询；
 * - 页面隐藏时暂停发起下一轮（在途请求可完成），恢复可见后继续；
 * - 装配于 AppShell，登录页（无 AppShell）不会轮询；
 * - 网络/服务端失败静默退避 5s 重试，不 toast、不弹窗；卸载/登出 abort。
 */
const POLL_WAIT_SECONDS = NOTIFICATION_WAIT_SECONDS;
const RETRY_DELAY_MS = 5000;
/**
 * 空闲轮询下限：mock / 异常代理可能立即返回空页；若此时仍 scheduleNext(0)，
 * 就会变成高频忙轮询（违背“不能无限轮询”）。空批且响应太快时补足到 1s 再发下一轮。
 */
const MIN_IDLE_INTERVAL_MS = 1000;

const { t } = useI18n();
const toast = useToast();

/** 事件状态 → 通知色调；未知/空状态按 info 处理。 */
const STATE_TONES: Record<string, ToastTone> = {
  queued: 'info',
  running: 'info',
  succeeded: 'success',
  success: 'success',
  failed: 'danger',
  failure: 'danger',
  cancelled: 'warning',
  superseded: 'warning',
  partial: 'warning',
  partial_success: 'warning',
};

let stopped = false;
let controller: AbortController | null = null;
let timer: number | undefined;
let inFlight = false;
let baselineLoaded = false;
let cursor = 0;
const seenSeqs = new Set<number>();

function pageVisible() {
  return typeof document === 'undefined' || document.visibilityState === 'visible';
}

function isAbortError(error: unknown) {
  if (!error || typeof error !== 'object') return false;
  if (typeof DOMException !== 'undefined' && error instanceof DOMException && error.name === 'AbortError') return true;
  const candidate = error as { name?: string; code?: string };
  return candidate.name === 'AbortError' || candidate.code === 'request_aborted';
}

function localizedState(state?: string) {
  if (!state) return '';
  const key = `activity.state.${state}`;
  const label = t(key);
  // 后端可能上报后端独有的状态值；没有词条时按内部稳定值展示，避免露出 key。
  return label === key ? state : label;
}

function deliver(notification: EventNotification) {
  if (!notification || typeof notification.seq !== 'number') return;
  if (seenSeqs.has(notification.seq)) return;
  seenSeqs.add(notification.seq);
  if (seenSeqs.size > 1024) {
    const oldest = seenSeqs.values().next().value;
    if (oldest !== undefined) seenSeqs.delete(oldest);
  }

  const title = notification.title?.trim() || notification.eventId;
  if (!title) return;
  const stateLabel = localizedState(notification.state);
  const summary = notification.summary && notification.summary !== title ? notification.summary : '';
  const description = [stateLabel, summary].filter(Boolean).join(' · ');

  toast.push({
    title,
    description: description || undefined,
    tone: notification.state ? (STATE_TONES[notification.state] ?? 'info') : 'info',
    action: notification.eventId
      ? { label: t('eventsPage.viewLogs'), to: eventsPath({ eventId: notification.eventId }) }
      : undefined,
  });
}

function scheduleNext(delay: number) {
  if (stopped) return;
  if (timer !== undefined) window.clearTimeout(timer);
  timer = window.setTimeout(() => {
    timer = undefined;
    void poll();
  }, delay);
}

async function poll() {
  if (stopped || inFlight || !pageVisible()) return;
  const signal = controller?.signal;
  if (!signal) return;
  inFlight = true;
  const startedAt = Date.now();
  try {
    const page = await notificationsApi.wait(baselineLoaded ? cursor : 0, POLL_WAIT_SECONDS, signal);
    const elapsed = Date.now() - startedAt;
    if (stopped) return;
    if (typeof page.cursor === 'number') cursor = page.cursor;
    if (!baselineLoaded) {
      // 首连只取基线：即使服务端异常回放了历史也不推送。
      baselineLoaded = true;
      scheduleNext(nextRoundDelay(page, elapsed));
      return;
    }
    for (const notification of page.items ?? []) deliver(notification);
    scheduleNext(nextRoundDelay(page, elapsed));
  } catch (error) {
    if (stopped || isAbortError(error)) return;
    scheduleNext(RETRY_DELAY_MS);
  } finally {
    inFlight = false;
  }
}

/**
 * 有增量时立即续 poll；空批时若响应过快（短于 1s）补足空闲间隔，
 * 正常长轮询（挂起约 20s）耗时已超过间隔，因此不受影响。
 */
function nextRoundDelay(page: EventNotificationPage, elapsedMs: number) {
  if ((page.items?.length ?? 0) > 0) return 0;
  return elapsedMs < MIN_IDLE_INTERVAL_MS ? MIN_IDLE_INTERVAL_MS - elapsedMs : 0;
}

function onVisibilityChange() {
  if (stopped || !pageVisible()) return;
  if (!inFlight && timer === undefined) scheduleNext(0);
}

onMounted(() => {
  controller = new AbortController();
  document.addEventListener('visibilitychange', onVisibilityChange);
  void poll();
});

onBeforeUnmount(() => {
  stopped = true;
  if (timer !== undefined) window.clearTimeout(timer);
  timer = undefined;
  document.removeEventListener('visibilitychange', onVisibilityChange);
  controller?.abort();
  controller = null;
});
</script>

<template>
  <span hidden aria-hidden="true" />
</template>
