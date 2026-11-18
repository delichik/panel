<script setup lang="ts">
import { computed, type Component } from 'vue';

/**
 * 概览摘要区的胶囊 KPI（label + value + 可选语义图标）。
 *
 * 用胶囊而不是矩形数字格，是为了在 WinUI 3 8px 表面圆角限制下用形状语言
 * 降低“方块感”；底色走 `bg-muted` 井道，不新增卡片层，也不承载交互。
 */
export type StatChipTone = 'neutral' | 'success' | 'warning' | 'danger' | 'info';

const props = withDefaults(defineProps<{
  label: string;
  value: string | number;
  tone?: StatChipTone;
  icon?: Component;
}>(), { tone: 'neutral', icon: undefined });

const toneClass = computed(() => ({
  neutral: 'text-foreground',
  success: 'text-success',
  warning: 'text-warning',
  danger: 'text-danger',
  info: 'text-info',
}[props.tone]));
</script>

<template>
  <span class="inline-flex min-h-7 min-w-0 items-center gap-1.5 rounded-pill bg-muted px-3 py-1">
    <component :is="icon" v-if="icon" class="size-3.5 shrink-0 text-secondary-foreground" aria-hidden="true" />
    <span class="truncate text-caption text-muted-foreground">{{ label }}</span>
    <strong class="shrink-0 text-body-strong tabular-nums" :class="toneClass">{{ value }}</strong>
  </span>
</template>
