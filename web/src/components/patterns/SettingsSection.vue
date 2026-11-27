<script setup lang="ts">
/**
 * WinUI 3 设置分区：分区标题 + 说明 + 单一 surface-card 卡片容器。
 * 卡片内的行使用 `divide-y divide-divider` 分隔，行组件为 SettingsCard；
 * 禁止在卡片内再嵌套第二层带描边的卡片。
 */
withDefaults(defineProps<{
  /** 可选：无标题的分区只渲染卡片行（页面级 PageHeader 已是标题）。 */
  title?: string;
  description?: string;
}>(), {
  title: '',
  description: '',
});
</script>

<template>
  <section data-settings-section class="grid min-w-0 gap-3">
    <div v-if="title || description || $slots.description || $slots.actions" class="flex min-w-0 flex-wrap items-end justify-between gap-2 px-1">
      <div class="grid min-w-0 gap-1">
        <h2 v-if="title" class="m-0 text-body-strong text-foreground">{{ title }}</h2>
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

    <div v-if="$slots.footer" class="flex flex-wrap items-center justify-end gap-2">
      <slot name="footer" />
    </div>
  </section>
</template>
