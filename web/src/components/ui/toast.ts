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

export const toastKey: InjectionKey<ToastApi> = Symbol('toast');

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
    toast.push({ title, tone: 'danger', action: to ? { label: t('activity.viewProcess'), to } : undefined });
  };
}

export function useSuccessToast() {
  const toast = useToast();
  const { t } = useI18n();
  return (title: string, reference?: unknown) => {
    const to = activityPath(reference);
    toast.push({ title, tone: 'success', action: to ? { label: t('activity.viewProcess'), to } : undefined });
  };
}
