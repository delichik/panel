<script setup lang="ts">
import Skeleton from '@/components/ui/Skeleton.vue';

/**
 * WinUI 3 图表卡：单一 `surface-card`（8px + 1px card-stroke）+ Body Strong（14/600）
 * 标题，右上角展示当前值与操作按钮；图表本体由默认插槽提供。
 *
 * 禁止 card-in-card：卡片内部不要再包 `surface-card`/`surface-control`/带描边卡片，
 * 图表占位只用虚线描边容器；一个视图区域内只允许一层卡片。
 */
withDefaults(defineProps<{
  title: string;
  /** 右上当前值（已格式化；0 是有效值）。 */
  value?: string | number | null;
  /** 当前值的可访问说明（如“当前延迟 12ms”）。 */
  valueLabel?: string;
  /** 无数据：渲染占位而不是图表。 */
  empty?: boolean;
  /** 占位文案；也可用 #empty 插槽自定义。 */
  emptyText?: string;
  /** 加载中：保留标题与卡片框架，右上角当前值位置渲染骨架。 */
  loading?: boolean;
}>(), {
  value: null,
  valueLabel: '',
  empty: false,
  emptyText: '',
  loading: false,
});
</script>

<template>
  <section data-chart-card :aria-busy="loading ? 'true' : undefined" class="grid min-w-0 gap-2 surface-card p-3">
    <div class="flex min-w-0 flex-wrap items-baseline justify-between gap-2">
      <h3 class="m-0 min-w-0 text-body-strong text-foreground">{{ title }}</h3>
      <div class="flex min-w-0 flex-wrap items-center gap-2">
        <Skeleton v-if="loading" class="h-5 w-16" />
        <span
          v-else-if="value !== null && value !== undefined"
          data-chart-value
          class="shrink-0 text-body-strong tabular-nums text-foreground"
          :aria-label="valueLabel || undefined"
        >{{ value }}</span>
        <slot name="actions" />
      </div>
    </div>

    <div class="relative min-h-0 min-w-0">
      <slot v-if="!empty" />
      <div
        v-else
        data-chart-card-empty
        class="grid h-full min-h-0 place-items-center rounded-control border border-dashed border-stroke px-3 text-center text-caption text-muted-foreground"
      >
        <slot name="empty">{{ emptyText }}</slot>
      </div>
    </div>

    <slot name="footer" />
  </section>
</template>
