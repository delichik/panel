<script setup lang="ts">
import { ChevronRight } from '@/icons';
import { RouterLink, type RouteLocationRaw } from 'vue-router';

/**
 * WinUI 3 BreadcrumbBar：nav[aria-label] + 可点击祖先项（RouterLink）+ 最后一项
 * `aria-current="page"` 的当前项 + ChevronRight 分隔符。
 */
export type BreadcrumbBarItem = {
  label: string;
  to?: RouteLocationRaw;
};

withDefaults(defineProps<{
  items: BreadcrumbBarItem[];
  /** 必填的 nav 可访问名称（页面传 i18n）。 */
  label: string;
}>(), {});
</script>

<template>
  <nav class="flex min-w-0 flex-wrap items-center gap-1 text-body" :aria-label="label">
    <template v-for="(item, index) in items" :key="`${item.label}-${index}`">
      <ChevronRight v-if="index > 0" class="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
      <span
        v-if="index === items.length - 1"
        class="min-w-0 truncate px-1 text-foreground"
        aria-current="page"
      >{{ item.label }}</span>
      <RouterLink
        v-else-if="item.to"
        v-slot="{ href, navigate }"
        :to="item.to"
        custom
      >
        <a
          :href="href"
          class="min-w-0 truncate rounded-control px-1 text-accent-text hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          @click="navigate"
        >{{ item.label }}</a>
      </RouterLink>
      <span v-else class="min-w-0 truncate px-1 text-secondary-foreground">{{ item.label }}</span>
    </template>
  </nav>
</template>
