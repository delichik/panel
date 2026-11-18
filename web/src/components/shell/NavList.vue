<script setup lang="ts">
import { RouterLink } from 'vue-router';
import { useI18n } from '@/i18n';
import { prefetchPage } from '@/router/lazyPage';
import { pageLoaderForPath } from '@/router/pageLoaders';
import { navGroups } from './navModel';

/**
 * Shared NavigationView list used by the desktop pane and the mobile drawer.
 * WinUI 3 item anatomy: a neutral selected fill (ControlFillColorSecondary
 * family), the 3x16px accent indicator, and TextFillColorPrimary text/icon;
 * accent never tints the label itself. Compact mode (48px pane) keeps 40x40
 * icon targets and the same indicator, with labels sr-only. Feedback is
 * fill-only (hover 167ms / press 83ms); no lift or scale.
 *
 * Mobile drawer: rows keep the 44px touch height, labels truncate inside the
 * item column and the list never scrolls horizontally (no sideways drift at
 * 360–430px).
 */
withDefaults(defineProps<{
  /** Nav item key that should read as the current location. */
  activeKey?: string;
  /** WinUI compact mode: 48px pane, 40x40 icon targets, labels sr-only. */
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
  <nav class="min-h-0 flex-1 overflow-y-auto overflow-x-hidden py-2" :class="collapsed ? 'px-1' : 'px-2'" :aria-label="label">
    <section v-for="(group, groupIndex) in navGroups" :key="group.key" :class="collapsed ? 'mb-1.5 last:mb-0' : 'mb-2 last:mb-0'">
      <div v-if="collapsed && groupIndex > 0" class="mx-2 my-1.5 h-px bg-divider" aria-hidden="true" />
      <span v-if="collapsed" class="sr-only">{{ t(group.titleKey) }}</span>
      <div v-else class="mb-0.5 h-7 overflow-hidden px-3">
        <div class="flex h-7 whitespace-nowrap text-xs font-semibold leading-4 text-muted-foreground">
          {{ t(group.titleKey) }}
        </div>
      </div>
      <RouterLink
        v-for="item in group.items"
        :key="item.key"
        :to="item.to"
        :title="collapsed ? t(item.titleKey) : undefined"
        :aria-current="activeKey === item.key ? 'page' : undefined"
        class="nav-item group relative flex min-w-0 items-center rounded-control text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset"
        :class="[
          activeKey === item.key
            ? 'bg-control text-foreground hover:bg-hover active:bg-pressed'
            : 'text-secondary-foreground hover:bg-hover hover:text-foreground active:bg-pressed',
          collapsed ? 'mx-auto mb-0.5 h-10 w-10 justify-center px-0' : 'mb-0.5 h-8 max-lg:h-11 gap-3 px-3',
        ]"
        @pointerenter="warmTarget(item.to)"
        @focusin="warmTarget(item.to)"
        @click="emit('navigate')"
      >
        <!-- WinUI 3 NavigationView selection indicator: 3x16 accent pill. -->
        <span
          v-if="activeKey === item.key"
          class="nav-indicator pointer-events-none absolute left-0 top-1/2 h-4 w-indicator -translate-y-1/2 rounded-full bg-primary"
          aria-hidden="true"
        />
        <component :is="item.icon" class="size-4 shrink-0" aria-hidden="true" />
        <span v-if="collapsed" class="sr-only">{{ t(item.titleKey) }}</span>
        <span v-else class="min-w-0 flex-1 overflow-hidden text-ellipsis whitespace-nowrap">{{ t(item.titleKey) }}</span>
      </RouterLink>
    </section>
  </nav>
</template>

<style scoped>
/*
 * NavigationView feedback is fill-only: hover 167ms, press 83ms, no translate
 * or scale. Timing tokens come from main.css so the pane matches the rest of
 * the WinUI motion system.
 */
.nav-item {
  transition:
    background-color var(--panel-motion-duration-fast) var(--panel-motion-ease-enter),
    color var(--panel-motion-duration-fast) var(--panel-motion-ease-enter);
}

.nav-item:active {
  transition-duration: var(--panel-motion-duration-fade);
  transition-timing-function: var(--panel-motion-ease-linear);
}

@media (prefers-reduced-motion: reduce) {
  .nav-item {
    transition-duration: 1ms;
  }
}
</style>
