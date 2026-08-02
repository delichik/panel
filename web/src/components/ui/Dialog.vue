<script setup lang="ts">
import { X } from '@lucide/vue';
import { nextTick, onBeforeUnmount, ref, useId, watch } from 'vue';
import { useI18n } from '@/i18n';
import IconButton from './IconButton.vue';

const props = defineProps<{
  open: boolean;
  title: string;
  description?: string;
  closeLabel?: string;
  size?: 'default' | 'large';
}>();
const emit = defineEmits<{ 'update:open': [value: boolean] }>();
const { t } = useI18n();

const dialog = ref<HTMLElement | null>(null);
const titleId = useId();
const descriptionId = useId();
let restoreFocusTo: HTMLElement | null = null;

const focusableSelector = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',');

function close() {
  emit('update:open', false);
}

function focusableElements() {
  return dialog.value ? Array.from(dialog.value.querySelectorAll<HTMLElement>(focusableSelector)) : [];
}

function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    event.preventDefault();
    close();
    return;
  }
  if (event.key !== 'Tab') return;

  const elements = focusableElements();
  if (!elements.length) {
    event.preventDefault();
    dialog.value?.focus();
    return;
  }
  const first = elements[0];
  const last = elements[elements.length - 1];
  if (event.shiftKey && (document.activeElement === first || document.activeElement === dialog.value)) {
    event.preventDefault();
    last?.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first?.focus();
  }
}

watch(() => props.open, async (open) => {
  if (open) {
    restoreFocusTo = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    await nextTick();
    (focusableElements()[0] ?? dialog.value)?.focus();
    return;
  }
  await nextTick();
  restoreFocusTo?.focus();
  restoreFocusTo = null;
}, { immediate: true });

onBeforeUnmount(() => restoreFocusTo?.focus());
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="motion-overlay fixed inset-0 z-50 grid place-items-center bg-overlay p-4" role="presentation">
      <section ref="dialog" class="motion-popover grid w-full grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden rounded-2xl border border-border bg-popover text-popover-foreground shadow-2xl" :class="size === 'large' ? 'h-[min(820px,calc(100dvh-32px))] max-w-5xl' : 'max-h-[min(720px,calc(100dvh-32px))] max-w-lg'" role="dialog" aria-modal="true" :aria-labelledby="titleId" :aria-describedby="description ? descriptionId : undefined" tabindex="-1" @keydown="onKeydown">
        <header class="flex min-h-0 items-start justify-between gap-4 border-b border-border px-5 py-4">
          <div class="min-w-0">
            <h2 :id="titleId" class="m-0 text-base font-semibold text-foreground">{{ title }}</h2>
            <p v-if="description" :id="descriptionId" class="m-0 mt-1 text-sm leading-6 text-muted-foreground">{{ description }}</p>
          </div>
          <IconButton :label="closeLabel || t('common.close')" size="sm" @click="close">
            <X />
          </IconButton>
        </header>
        <div class="min-h-0 overflow-auto px-5 py-4">
          <slot />
        </div>
        <footer v-if="$slots.footer" class="flex justify-end gap-2 border-t border-border px-5 py-4">
          <slot name="footer" />
        </footer>
      </section>
    </div>
  </Teleport>
</template>
