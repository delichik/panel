<script setup lang="ts">
/**
 * WinUI 3 列表命令栏：搜索区 + 筛选区 + 批量/主操作区，控件统一 32px（h-control，
 * 由 ui primitives 自身保证）；间距 gap-2，窄屏纵向折叠、宽屏单行右对齐主操作。
 *
 * 本组件只负责布局与可访问分组，不实现、也不模拟任何按钮选中态：
 * 筛选切换由调用方使用 Tabs（role=tab）或 ToggleButton 语义（aria-pressed）实现，
 * 命令栏不提供私有 pressed/active 样式，避免出现两套选中态。
 */
withDefaults(defineProps<{
  /** 命令栏的无障碍名称（如“服务器筛选”）；提供时渲染 role=group。 */
  label?: string;
}>(), {
  label: '',
});
</script>

<template>
  <div
    data-list-command-bar
    :role="label ? 'group' : undefined"
    :aria-label="label || undefined"
    class="flex min-h-control min-w-0 flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center"
  >
    <div v-if="$slots.search" class="min-w-0 sm:w-72 sm:shrink-0 max-sm:w-full">
      <slot name="search" />
    </div>
    <div v-if="$slots.filters" class="flex min-w-0 flex-wrap items-center gap-2 max-sm:grid max-sm:grid-cols-1 max-sm:items-stretch">
      <slot name="filters" />
    </div>
    <div v-if="$slots.actions" class="flex min-w-0 flex-wrap items-center gap-2 sm:ml-auto max-sm:grid max-sm:w-full max-sm:grid-cols-1 max-sm:items-stretch">
      <slot name="actions" />
    </div>
    <div v-if="$slots.default" class="min-w-0 sm:basis-full">
      <slot />
    </div>
  </div>
</template>
