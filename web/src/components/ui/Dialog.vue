<script setup lang="ts">
import { X } from '@/icons';
import { computed, ref, useId } from 'vue';
import { useI18n } from '@/i18n';
import { useOverlayBehavior } from '@/composables/useOverlayBehavior';
import { zIndex } from './states';
import IconButton from './IconButton.vue';

const props = withDefaults(defineProps<{
  open: boolean;
  title: string;
  description?: string;
  closeLabel?: string;
  /** 旧的尺寸档位：large = 1024 宽 + 固定高（保留给既有调用方）。 */
  size?: 'default' | 'large';
  /** 新的宽度能力：default=548；wide 使用 --panel-dialog-max-width-wide（main.css 待补，组件内 fallback 720）。 */
  width?: 'default' | 'wide';
  /** While true the dialog cannot be dismissed by the close button or Escape (used during saves). */
  closeDisabled?: boolean;
}>(), {
  closeDisabled: false,
  width: 'default',
});
const emit = defineEmits<{ 'update:open': [value: boolean] }>();
const { t } = useI18n();

const dialog = ref<HTMLElement | null>(null);
const titleId = useId();
const descriptionId = useId();

const widthClass = computed(() => {
  if (props.width === 'wide') return 'max-w-[var(--panel-dialog-max-width-wide,720px)]';
  if (props.size === 'large') return 'max-w-[var(--panel-dialog-max-width-large)]';
  return 'max-w-[var(--panel-dialog-max-width)]';
});

const heightClass = computed(() => (
  props.size === 'large' ? 'h-[min(820px,calc(100dvh-32px))]' : 'max-h-[min(720px,calc(100dvh-32px))]'
));

function close() {
  if (props.closeDisabled) return;
  emit('update:open', false);
}

const { onKeydown } = useOverlayBehavior({
  open: () => props.open,
  containerRef: dialog,
  onClose: close,
  // A modal dialog must not scroll the page behind it; the lock is
  // reference-counted so nested dialogs restore it only on the last close.
  lockScroll: true,
});
</script>

<template>
  <Teleport to="body">
    <Transition name="dialog">
      <div v-if="open" class="fixed inset-0 grid place-items-center bg-overlay p-4" :class="zIndex.dialog" role="presentation">
        <!-- WinUI ContentDialog 宽度模型：内容自适应，min 320 / default max 548 / wide 走
             --panel-dialog-max-width-wide（main.css 待补 token，组件 fallback 720）/ legacy large 1024；
             8px 圆角、浮层表面、短阴影、内容 24px padding；命令区右对齐，仅用间距分隔（无 footer 分割线）。 -->
        <section ref="dialog" class="dialog-panel grid w-auto min-w-dialog grid-rows-page-footer overflow-hidden surface-flyout text-popover-foreground backdrop-blur-flyout" :class="[widthClass, heightClass]" role="dialog" aria-modal="true" :aria-labelledby="titleId" :aria-describedby="description ? descriptionId : undefined" tabindex="-1" @keydown="onKeydown">
          <header class="flex min-h-0 items-start justify-between gap-4 px-6 pt-6 max-sm:gap-3">
            <div class="min-w-0">
              <h2 :id="titleId" class="m-0 break-words text-subtitle font-semibold text-foreground">{{ title }}</h2>
              <p v-if="description" :id="descriptionId" class="m-0 mt-1 break-words text-body text-secondary-foreground">{{ description }}</p>
            </div>
            <IconButton :label="closeLabel || t('common.close')" size="sm" :disabled="closeDisabled" @click="close">
              <X />
            </IconButton>
          </header>
          <div class="min-h-0 overflow-auto p-6">
            <slot />
          </div>
          <footer v-if="$slots.footer" class="flex justify-end gap-2 px-6 pb-6 max-sm:grid max-sm:grid-cols-2 max-sm:items-stretch">
            <slot name="footer" />
          </footer>
        </section>
      </div>
    </Transition>
  </Teleport>
</template>
