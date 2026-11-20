<script setup lang="ts">
import { onBeforeUnmount, provide, ref } from 'vue';
import { RouterLink } from 'vue-router';
import { AlertTriangle, CheckCircle2, Info, X, XCircle } from '@/icons';
import { useI18n } from '@/i18n';
import IconButton from './IconButton.vue';
import { zIndex } from './states';
import { toastKey, type ToastPayload, type ToastRecord, type ToastTone } from './toast';

const { t } = useI18n();
/** Visible stack cap: older toasts are dismissed so the newest stays reachable. */
const MAX_VISIBLE = 4;
const TOAST_DURATION = 4200;
const TOAST_ACTION_DURATION = 15000;

let nextId = 1;
const toasts = ref<ToastRecord[]>([]);
interface ToastTimer { handle: number; remaining: number; startedAt: number }
const timers = new Map<number, ToastTimer>();

function clearTimer(id: number) {
  const timer = timers.get(id);
  if (!timer) return;
  window.clearTimeout(timer.handle);
  timers.delete(id);
}

function schedule(id: number, remaining: number) {
  clearTimer(id);
  timers.set(id, {
    handle: window.setTimeout(() => remove(id), remaining),
    remaining,
    startedAt: Date.now(),
  });
}

function pause(id: number) {
  const timer = timers.get(id);
  if (!timer) return;
  window.clearTimeout(timer.handle);
  timers.set(id, { handle: 0, remaining: Math.max(400, timer.remaining - (Date.now() - timer.startedAt)), startedAt: Date.now() });
}

function resume(id: number) {
  const timer = timers.get(id);
  if (!timer || timer.handle) return;
  schedule(id, timer.remaining);
}

function push(payload: ToastPayload) {
  const id = nextId++;
  const record: ToastRecord = {
    id,
    title: payload.title,
    description: payload.description ?? '',
    tone: payload.tone ?? 'info',
    action: payload.action,
  };
  const next = [...toasts.value, record];
  while (next.length > MAX_VISIBLE) {
    const dropped = next.shift();
    if (dropped) clearTimer(dropped.id);
  }
  toasts.value = next;
  schedule(id, payload.action ? TOAST_ACTION_DURATION : TOAST_DURATION);
}

function remove(id: number) {
  clearTimer(id);
  toasts.value = toasts.value.filter((toast) => toast.id !== id);
}

provide(toastKey, { push, remove });

onBeforeUnmount(() => {
  timers.forEach((timer) => window.clearTimeout(timer.handle));
  timers.clear();
});

const toneIcons = {
  success: CheckCircle2,
  info: Info,
  warning: AlertTriangle,
  danger: XCircle,
} as const satisfies Record<ToastTone, unknown>;

const toneClasses: Record<ToastTone, string> = {
  success: 'text-success',
  info: 'text-info',
  warning: 'text-warning',
  danger: 'text-danger',
};

/** WinUI InfoBar 视觉：4px 圆角 + 语义 bg/border + 无阴影。 */
const bubbleClasses: Record<ToastTone, string> = {
  success: 'border-success-border bg-success-bg text-success',
  info: 'border-info-border bg-info-bg text-info',
  warning: 'border-warning-border bg-warning-bg text-warning',
  danger: 'border-danger-border bg-danger-bg text-danger',
};

const stackClasses = 'fixed right-4 top-4 grid w-[min(360px,calc(100vw-32px))] gap-2 lg:top-17';
</script>

<template>
  <slot />
  <Teleport to="body">
    <TransitionGroup name="toast-stack" tag="div" :class="[stackClasses, zIndex.toast]">
      <section
        v-for="toast in toasts"
        :key="toast.id"
        class="flex min-w-0 items-start gap-2 rounded-control border p-3 text-body"
        :class="bubbleClasses[toast.tone]"
        :data-tone="toast.tone"
        :role="toast.tone === 'danger' ? 'alert' : 'status'"
        @mouseenter="pause(toast.id)"
        @mouseleave="resume(toast.id)"
        @focusin="pause(toast.id)"
        @focusout="resume(toast.id)"
      >
        <component :is="toneIcons[toast.tone]" class="mt-0.5 size-4 shrink-0" :class="toneClasses[toast.tone]" aria-hidden="true" />
        <div class="min-w-0 flex-1">
          <strong class="block text-body-strong">{{ toast.title }}</strong>
          <RouterLink v-if="toast.action" :to="toast.action.to" class="mt-2 inline-flex items-center rounded-control text-body font-medium underline underline-offset-4 text-current focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" @click="remove(toast.id)">{{ toast.action.label }}</RouterLink>
          <p v-if="toast.description" class="m-0 mt-1">{{ toast.description }}</p>
        </div>
        <IconButton size="sm" class="shrink-0 text-current" :label="t('common.close')" @click="remove(toast.id)">
          <X />
        </IconButton>
      </section>
    </TransitionGroup>
  </Teleport>
</template>
