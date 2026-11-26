<script setup lang="ts">
import RatioRing from './RatioRing.vue';

/**
 * 磁盘使用率环（当前值）：`RatioRing` 的磁盘专用薄包装，保持既有 `data-testid`
 * 与 `tone/size` 语义，供服务器详情概览使用。
 *
 * 旧实现已抽到 `RatioRing.vue`（通用占比环）；这里只固定 testid、文档语义与
 * 尺寸档位，不再重复绘制逻辑。
 */
export type DiskUsageTone = 'accent' | 'success' | 'warning' | 'danger';

withDefaults(defineProps<{
  /** 0–100；`null` 表示尚无有效磁盘样本（渲染空环）。 */
  percent: number | null;
  /** 中心文本，已格式化/本地化（如 `68.4%` 或 `—`）。 */
  valueText: string;
  /** 含百分比的本地化可访问名称。 */
  label: string;
  tone?: DiskUsageTone;
  /** 环直径：80（紧凑）或 96（摘要主视觉）。 */
  size?: 'md' | 'lg';
  /** 加载中：仅中心数值位置渲染骨架。 */
  loading?: boolean;
}>(), { tone: 'accent', size: 'md', loading: false });
</script>

<template>
  <RatioRing
    :percent="percent"
    :value-text="valueText"
    :label="label"
    :tone="tone"
    :size="size"
    :loading="loading"
    test-id="disk-usage-chart"
  />
</template>
