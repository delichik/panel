import { inject, type InjectionKey } from 'vue';
import { useI18n } from '@/i18n';
import { activityPath } from '@/api/activityReference';

export type ToastTone = 'success' | 'info' | 'warning' | 'danger';

export interface ToastPayload {
  title: string;
  description?: string;
  tone?: ToastTone;
  action?: { label: string; to: string };
}

export interface ToastRecord extends ToastPayload {
  id: number;
  description: string;
  tone: ToastTone;
}

export interface ToastApi {
  push(payload: ToastPayload): void;
  remove(id: number): void;
}

/**
 * 使用全局 Symbol 注册表：浏览器缓存了新旧 chunk 或 Vite HMR 造成模块被重复
 * 求值时，Symbol('toast') 会得到两个不同键，inject 便会误报 ToastProvider 缺失。
 * Symbol.for 保证同一页面内所有模块实例解析到同一个键。
 */
export const toastKey: InjectionKey<ToastApi> = Symbol.for('seamark.toast') as InjectionKey<ToastApi>;

/**
 * 右上角通知基础设施（仅供 ToastProvider / EventNotificationBridge 等基础设施使用）。
 *
 * 业务代码不得直接调用 `useToast().push` 承载报错：阻塞用户操作的失败走
 * `useErrorDialog()` 弹窗，事件通知由 EventNotificationBridge 统一推送，
 * 用户操作成功提示使用 `useSuccessToast()`。该约束由 toastPolicy.test.ts 守卫。
 */
export function useToast() {
  const toast = inject(toastKey);
  if (!toast) throw new Error('ToastProvider is missing.');
  return toast;
}

/**
 * 用户主动操作成功提示：右上角是唯一允许的业务提示通道。
 * `useErrorToast` 已删除（阻塞错误改走全局错误弹窗），此处不再提供任何报错入口。
 */
export function useSuccessToast() {
  const toast = useToast();
  const { t } = useI18n();
  return (title: string, reference?: unknown) => {
    const to = activityPath(reference);
    toast.push({ title, tone: 'success', action: to ? { label: t('eventsPage.viewLogs'), to } : undefined });
  };
}
