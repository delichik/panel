<script setup lang="ts">
/**
 * 设施页分区标题行：标题 + 说明 + 右侧操作。
 * 由 FacilitySection 与需要自定义内部 grid 的设施分区（如探测结果）共用。
 */
withDefaults(defineProps<{
  title: string;
  description?: string;
}>(), {
  description: '',
});
</script>

<template>
  <div class="flex min-w-0 flex-wrap items-start justify-between gap-2">
    <div class="min-w-0">
      <h2 class="m-0 text-body-strong text-foreground">{{ title }}</h2>
      <p v-if="description" class="m-0 mt-1 text-body text-muted-foreground">{{ description }}</p>
    </div>
    <div v-if="$slots.actions" class="flex flex-wrap items-center gap-2">
      <slot name="actions" />
    </div>
  </div>
</template>
