<script setup lang="ts">
/**
 * WinUI 3 只读详情分区：卡片外 Body Strong（14/600）标题 + 可选说明 + 单一
 * `surface-card` 容器（8px + card-stroke），卡片内行用 `divide-y divide-divider`
 * 分隔。行内容由调用方提供（推荐 PropertyList，或自定义行）。
 *
 * 表面层级：标题/说明在卡片外；卡片内只有一层表面，禁止再嵌第二层带描边卡片。
 */
withDefaults(defineProps<{
  title: string;
  description?: string;
}>(), {
  description: '',
});
</script>

<template>
  <section data-detail-section class="grid min-w-0 gap-3">
    <div class="flex min-w-0 flex-wrap items-end justify-between gap-2">
      <div class="grid min-w-0 gap-1">
        <h2 class="m-0 text-body-strong text-foreground">{{ title }}</h2>
        <p v-if="description" class="m-0 text-caption text-muted-foreground">{{ description }}</p>
        <slot name="description" />
      </div>
      <div v-if="$slots.actions" class="flex flex-wrap items-center gap-2">
        <slot name="actions" />
      </div>
    </div>

    <div class="surface-card overflow-hidden">
      <div class="divide-y divide-divider">
        <slot />
      </div>
    </div>

    <slot name="footer" />
  </section>
</template>
