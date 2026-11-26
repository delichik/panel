<script setup lang="ts">
import { computed } from 'vue';
import Skeleton from '@/components/ui/Skeleton.vue';

/**
 * 通用占比环：WinUI 3 中性 chrome，4px token 描边环 + 中心数值。
 *
 * 由磁盘环形图（DiskUsageChart）抽取，专供“当前值占比”类数据（磁盘使用率、
 * 节点在线率、集群容量等），不承载历史曲线。`role="img"` + `label` 必须包含
 * 当前百分比；`percent=null` 表示没有有效样本，环为空且中心显示调用方传入的
 * `—`，不得伪造 0%。
 */
export type RatioTone = 'accent' | 'success' | 'warning' | 'danger' | 'info';

const props = withDefaults(defineProps<{
  /** 0–100；`null` 表示没有有效样本（渲染空环）。 */
  percent: number | null;
  /** 中心文本，已格式化/本地化（如 `68.4%` 或 `—`）。 */
  valueText: string;
  /** 含百分比的本地化可访问名称。 */
  label: string;
  tone?: RatioTone;
  /** 环直径：56（紧凑）/ 80（默认）/ 96（摘要主视觉）。 */
  size?: 'sm' | 'md' | 'lg';
  /** 根节点 data-testid，便于页面/测试定位具体环。 */
  testId?: string;
  /** 加载中：外环保持中性轨道，中心数值位置显示骨架。 */
  loading?: boolean;
}>(), { tone: 'accent', size: 'md', testId: 'ratio-ring', loading: false });

const clamped = computed(() => {
  if (props.loading || props.percent === null || !Number.isFinite(props.percent)) return null;
  return Math.min(100, Math.max(0, props.percent));
});
const hasData = computed(() => !props.loading && clamped.value !== null);
const dashArray = computed(() => (hasData.value ? `${clamped.value} 100` : '0 100'));

const toneClass = computed(() => ({
  accent: 'text-primary',
  success: 'text-success',
  warning: 'text-warning',
  danger: 'text-danger',
  info: 'text-info',
}[props.tone]));
const valueClass = computed(() => (hasData.value ? toneClass.value : 'text-muted-foreground'));
const sizeClass = computed(() => ({ sm: 'size-14', md: 'size-20', lg: 'size-24' }[props.size]));
const valueTextClass = computed(() => ({
  sm: 'text-caption',
  md: 'text-body-strong',
  lg: 'text-subtitle',
}[props.size]));
const skeletonSizeClass = computed(() => ({
  sm: 'h-3 w-7',
  md: 'h-4 w-10',
  lg: 'h-5 w-12',
}[props.size]));
</script>

<template>
  <div
    :data-testid="testId"
    :data-empty="hasData ? undefined : 'true'"
    :data-tone="tone"
    :aria-busy="loading ? 'true' : undefined"
    role="img"
    :aria-label="label"
    class="relative inline-grid shrink-0 place-items-center"
    :class="sizeClass"
  >
    <svg viewBox="0 0 36 36" class="size-full -rotate-90 text-foreground" aria-hidden="true" focusable="false">
      <circle cx="18" cy="18" r="15.9155" fill="none" stroke-width="4" pathLength="100" class="stroke-divider" />
      <circle
        cx="18"
        cy="18"
        r="15.9155"
        fill="none"
        stroke-width="4"
        pathLength="100"
        class="stroke-current"
        :class="valueClass"
        :stroke-dasharray="dashArray"
      />
    </svg>
    <span v-if="loading" class="absolute inset-0 grid place-items-center">
      <Skeleton :class="skeletonSizeClass" />
    </span>
    <span v-else class="absolute inset-0 grid place-items-center tabular-nums" :class="[valueTextClass, valueClass]">{{ valueText }}</span>
  </div>
</template>
