<script setup lang="ts">
import FacilitySectionHeader from './FacilitySectionHeader.vue';

/**
 * 设施页统一分区：WinUI 设置卡分区（标题 + 说明 + 可选操作 + 内容）。
 * 所有设施详情/配置页共用，避免各页复制标题、间距与表面 class。
 */
withDefaults(defineProps<{
  title: string;
  description?: string;
}>(), {
  description: '',
});
</script>

<template>
  <section class="motion-reveal grid gap-3 surface-card p-4">
    <FacilitySectionHeader :title="title" :description="description">
      <template v-if="$slots.actions" #actions><slot name="actions" /></template>
    </FacilitySectionHeader>
    <slot />
  </section>
</template>
