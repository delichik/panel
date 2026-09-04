<script setup lang="ts">
import { provide, ref } from 'vue';
import { useI18n } from '@/i18n';
import { toastKey, type ToastPayload, type ToastRecord } from './toast';

const { t } = useI18n();
let nextId = 1;
const toasts = ref<ToastRecord[]>([]);

function push(payload: ToastPayload) {
  const id = nextId++;
  const record: ToastRecord = {
    id,
    title: payload.title,
    description: payload.description ?? '',
    tone: payload.tone ?? 'info',
  };
  toasts.value = [...toasts.value, record];
  window.setTimeout(() => remove(id), 4200);
}

function remove(id: number) {
  toasts.value = toasts.value.filter((toast) => toast.id !== id);
}

provide(toastKey, { push, remove });
</script>

<template>
  <slot />
  <Teleport to="body">
    <TransitionGroup name="toast-stack" tag="div" class="fixed right-4 top-4 z-[60] grid w-[min(360px,calc(100vw-32px))] gap-2">
      <section
        v-for="toast in toasts"
        :key="toast.id"
        class="rounded-2xl border bg-popover p-4 text-sm text-popover-foreground shadow-xl"
        :class="{
          'border-success-border': toast.tone === 'success',
          'border-warning-border': toast.tone === 'warning',
          'border-danger-border': toast.tone === 'danger',
          'border-info-border': toast.tone === 'info',
        }"
        :role="toast.tone === 'danger' ? 'alert' : 'status'"
      >
        <div class="flex items-start justify-between gap-3">
          <div class="min-w-0">
            <strong class="block text-foreground">{{ toast.title }}</strong>
            <p v-if="toast.description" class="m-0 mt-1 text-muted-foreground">{{ toast.description }}</p>
          </div>
          <button type="button" class="motion-icon-control text-muted-foreground hover:text-foreground" :aria-label="t('common.close')" @click="remove(toast.id)">x</button>
        </div>
      </section>
    </TransitionGroup>
  </Teleport>
</template>
