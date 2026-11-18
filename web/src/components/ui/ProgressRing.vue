<script setup lang="ts">
/**
 * WinUI 3 ProgressRing：确定性旋转的短弧 + 微弱轨道；无 label 时为装饰性 loading
 * （aria-hidden），传 label 时才作为 progressbar 暴露给辅助技术。
 */
withDefaults(defineProps<{ label?: string; size?: number | string }>(), { label: '', size: 24 });
</script>

<template>
  <svg
    viewBox="0 0 24 24"
    class="winui-progress-ring shrink-0 text-primary"
    :width="size"
    :height="size"
    fill="none"
    focusable="false"
    :role="label ? 'progressbar' : undefined"
    :aria-label="label || undefined"
    :aria-hidden="label ? undefined : 'true'"
  >
    <circle cx="12" cy="12" r="9" stroke="currentColor" stroke-width="2.5" class="winui-progress-ring__track" />
    <circle cx="12" cy="12" r="9" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" class="winui-progress-ring__arc" />
  </svg>
</template>
