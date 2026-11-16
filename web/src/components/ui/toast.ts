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

export function useToast() {
  const toast = inject(toastKey);
  if (!toast) throw new Error('ToastProvider is missing.');
  return toast;
}

export function useErrorToast() {
  const toast = useToast();
  const { t } = useI18n();
  return (title: string, reference?: unknown) => {
    const to = activityPath(reference);
    toast.push({ title, tone: 'danger', action: to ? { label: t('eventsPage.viewLogs'), to } : undefined });
  };
}

export function useSuccessToast() {
  const toast = useToast();
  const { t } = useI18n();
  return (title: string, reference?: unknown) => {
    const to = activityPath(reference);
    toast.push({ title, tone: 'success', action: to ? { label: t('eventsPage.viewLogs'), to } : undefined });
  };
}
