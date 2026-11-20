<script setup lang="ts">
import { computed } from 'vue';

export type ProgressTone = 'accent' | 'success' | 'warning' | 'danger' | 'info';

/**
 * WinUI 3 确定性进度条：4px 轨道（`h-1`）+ 4px 圆角，轨道 `bg-muted`，填充
 * 使用语义色（默认 accent）。填充宽度用 token 时长/曲线做过渡；
 * `prefers-reduced-motion` 由 main.css 全局降级。
 *
 * 可访问性：`role=progressbar` + `aria-valuemin/max/now`，`label` 为必填名称，
 * `valueLabel` 可提供更可读的 `aria-valuetext`（缺省为取整百分比）。
 * 不安放可见 label/value 文本：文案属于页面，避免组件内造 i18n 文案。
 */
const props = withDefaults(defineProps<{
  /** 0–100；越界会被夹取。 */
  value: number;
  /** 必填的可访问名称（页面传 i18n，如“部署进度”）。 */
  label: string;
  tone?: ProgressTone;
  /** 自定义 aria-valuetext（如“已完成 3/7”）；缺省为“42%”。 */
  valueLabel?: string;
}>(), {
  tone: 'accent',
  valueLabel: '',
});

const clamped = computed(() => (Number.isFinite(props.value) ? Math.min(100, Math.max(0, props.value)) : 0));
const rounded = computed(() => Math.round(clamped.value));
const valueText = computed(() => props.valueLabel || `${rounded.value}%`);
const toneClass = computed(() => ({
  accent: 'bg-primary',
  success: 'bg-success',
  warning: 'bg-warning',
  danger: 'bg-danger',
  info: 'bg-info',
}[props.tone]));
</script>

<template>
  <div
    data-progress-bar
    :data-tone="tone"
    role="progressbar"
    :aria-label="label"
    :aria-valuemin="0"
    :aria-valuemax="100"
    :aria-valuenow="rounded"
    :aria-valuetext="valueText"
    class="h-1 w-full overflow-hidden rounded-control bg-muted"
  >
    <div
      class="h-full rounded-control transition-width duration-normal ease-move"
      :class="toneClass"
      :style="{ width: `${clamped}%` }"
    />
  </div>
</template>
