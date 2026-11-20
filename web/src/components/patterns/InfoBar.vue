<script setup lang="ts">
import { computed } from 'vue';
import { AlertTriangle, CheckCircle2, Info, XCircle } from '@/icons';
import IconButton from '@/components/ui/IconButton.vue';

export type InfoBarTone = 'info' | 'success' | 'warning' | 'danger';

/**
 * WinUI 3 InfoBar：4px 圆角、语义 bg/border、无阴影、4px 网格内边距（p-3）。
 * info/success 为 `role=status`（polite），warning/danger 为 `role=alert`（assertive）。
 * 关闭按钮只在 `closable` 时渲染，需同时提供 `closeLabel` 作为可访问名称（页面传 i18n）。
 * 语义色只表达状态，不得用强调色替代。
 */
const props = withDefaults(defineProps<{
  tone?: InfoBarTone;
  title?: string;
  closable?: boolean;
  /** 关闭按钮的可访问名称；closable 时必填。 */
  closeLabel?: string;
}>(), {
  tone: 'info',
  title: '',
  closable: false,
  closeLabel: '',
});

const emit = defineEmits<{ close: [] }>();

const toneIcon = computed(() => ({
  info: Info,
  success: CheckCircle2,
  warning: AlertTriangle,
  danger: XCircle,
}[props.tone]));

const toneClass = computed(() => ({
  info: 'border-info-border bg-info-bg text-info',
  success: 'border-success-border bg-success-bg text-success',
  warning: 'border-warning-border bg-warning-bg text-warning',
  danger: 'border-danger-border bg-danger-bg text-danger',
}[props.tone]));

const role = computed(() => (props.tone === 'danger' || props.tone === 'warning' ? 'alert' : 'status'));
</script>

<template>
  <div
    data-info-bar
    :data-tone="tone"
    :role="role"
    class="flex min-w-0 items-start gap-2 rounded-control border p-3"
    :class="toneClass"
  >
    <component :is="toneIcon" class="mt-0.5 size-4 shrink-0" aria-hidden="true" />
    <div class="grid min-w-0 flex-1 gap-0.5">
      <p v-if="title || $slots.title" class="m-0 text-body-strong">
        <slot name="title">{{ title }}</slot>
      </p>
      <div v-if="$slots.default" class="min-w-0 text-body">
        <slot />
      </div>
    </div>
    <IconButton
      v-if="closable"
      size="sm"
      variant="ghost"
      class="shrink-0 text-current"
      :label="closeLabel"
      @click="emit('close')"
    >
      <XCircle />
    </IconButton>
  </div>
</template>
