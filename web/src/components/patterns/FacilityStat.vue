<script setup lang="ts">
/**
 * 设施统计小格：值 + 标签，用于设施目录与存储共享摘要。
 * `loading` 时显示骨架，避免目录/摘要加载期间出现 0 或空串闪烁。
 */
withDefaults(defineProps<{
  value?: string | number | null;
  label: string;
  loading?: boolean;
}>(), {
  value: null,
  loading: false,
});
</script>

<template>
  <div class="grid min-w-0 gap-0.5">
    <span v-if="loading" class="motion-skeleton h-5 w-10 rounded bg-muted animate-pulse" aria-hidden="true" />
    <strong v-else class="block truncate text-subtitle font-semibold text-foreground">{{ value ?? '—' }}</strong>
    <span class="truncate text-caption text-muted-foreground">{{ label }}</span>
  </div>
</template>
