import { inject, type InjectionKey } from 'vue';

/** One blocking-error dialog payload; only the front of the queue is rendered at a time. */
export interface ErrorDialogPayload {
  title: string;
  description?: string;
  /** Server-provided correlation ids (eventId / operationId / taskId …) used for the log deep link. */
  reference?: unknown;
}

export interface ErrorDialogApi {
  push(payload: ErrorDialogPayload): void;
  /** Dismiss the current dialog and reveal the next queued one. */
  dismiss(): void;
}

/**
 * Same global-symbol registry trick as `toastKey`: duplicated module instances
 * (stale chunks / HMR) must still resolve the provider installed by App.vue.
 */
export const errorDialogKey: InjectionKey<ErrorDialogApi> = Symbol.for('seamark.errorDialog') as InjectionKey<ErrorDialogApi>;

/**
 * 全局阻塞错误弹窗入口。
 *
 * 只有“用户当前阻塞操作失败”（保存/提交/删除/确认/登录等显式动作）才允许调用；
 * 页面首次加载失败、自动刷新/自动保存失败必须就地展示错误态/陈旧标记 + 重试，
 * 不得弹窗，也不得再送回右上角 toast。
 */
export function useErrorDialog() {
  const dialog = inject(errorDialogKey);
  if (!dialog) throw new Error('ErrorDialogHost is missing.');
  return (title: string, reference?: unknown) => {
    dialog.push({ title, reference });
  };
}
