<script setup lang="ts">
import { X } from '@lucide/vue';
import { ref, useId } from 'vue';
import { useI18n } from '@/i18n';
import { useOverlayBehavior } from '@/composables/useOverlayBehavior';
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

function close() {
  emit('update:open', false);
}

const { onKeydown } = useOverlayBehavior({
  open: () => props.open,
  containerRef: dialog,
  onClose: close,
});
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
