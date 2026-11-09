<script setup lang="ts">
import { RouterLink } from 'vue-router';
import { useI18n } from '@/i18n';
import { prefetchPage } from '@/router/lazyPage';
import { pageLoaderForPath } from '@/router/pageLoaders';
import { navGroups } from './navModel';

/**
 * Shared primary navigation list used by the desktop sidebar and the mobile
 * drawer. Keeps the two surfaces from drifting apart (labels, active rail,
 * collapsed behavior) and owns the nav motion.
 */
withDefaults(defineProps<{
  /** Nav item key that should read as the current location. */
  activeKey?: string;
  /** Icon-only mode: labels fade with the sidebar width instead of being removed. */
  collapsed?: boolean;
  label: string;
}>(), {
  collapsed: false,
});

const emit = defineEmits<{ navigate: [] }>();
const { t } = useI18n();

function warmTarget(path: string) {
  const loader = pageLoaderForPath(path);
  if (loader) prefetchPage(loader);
}
</script>

<template>
  <nav class="min-h-0 flex-1 overflow-auto px-2 py-2.5" :aria-label="label">
    <section v-for="(group, groupIndex) in navGroups" :key="group.key" class="mb-3.5 last:mb-0">
      <div v-if="collapsed && groupIndex > 0" class="mx-2 my-2.5 h-px bg-border" aria-hidden="true" />
      <div class="mb-1 h-4 overflow-hidden px-2.5">
        <div class="shell-label whitespace-nowrap text-[11px] font-medium leading-4 text-muted-foreground" :class="collapsed ? 'max-w-0 opacity-0' : 'max-w-[200px]'">
          {{ t(group.titleKey) }}
        </div>
      </div>
      <RouterLink
        v-for="item in group.items"
        :key="item.key"
        :to="item.to"
        :title="collapsed ? t(item.titleKey) : undefined"
        :aria-current="activeKey === item.key ? 'page' : undefined"
        class="group relative mb-0.5 flex h-9 max-lg:h-11 items-center overflow-hidden rounded-lg px-2.5 text-sm font-medium transition-[background-color,color,box-shadow]"
        :class="[
          activeKey === item.key
            ? 'bg-primary text-primary-foreground shadow-sm'
            : 'text-secondary-foreground hover:bg-accent hover:text-foreground',
          collapsed ? 'justify-center px-0' : 'gap-2.5',
        ]"
        @pointerenter="warmTarget(item.to)"
        @focusin="warmTarget(item.to)"
        @click="emit('navigate')"
      >
        <component :is="item.icon" class="size-4 shrink-0" aria-hidden="true" />
        <span
          class="shell-label min-w-0 truncate"
          :class="collapsed ? 'max-w-0 opacity-0' : 'max-w-[200px] flex-1'"
        >{{ t(item.titleKey) }}</span>
      </RouterLink>
    </section>
  </nav>
</template>
