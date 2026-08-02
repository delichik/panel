import { inject, type InjectionKey } from 'vue';

export type ToastTone = 'success' | 'info' | 'warning' | 'danger';

export interface ToastPayload {
  title: string;
  description?: string;
  tone?: ToastTone;
}

export interface ToastRecord extends Required<ToastPayload> {
  id: number;
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
  return (title: string) => toast.push({ title, tone: 'danger' });
}
