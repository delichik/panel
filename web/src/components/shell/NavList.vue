<script setup lang="ts">
import { LoaderCircle } from '@lucide/vue';
import { RouterLink } from 'vue-router';
import { useI18n } from '@/i18n';
import { navGroups } from './navModel';

/**
 * Shared primary navigation list used by the desktop sidebar and the mobile
 * drawer. Keeps the two surfaces from drifting apart (labels, pending state,
 * active rail, collapsed behavior) and owns the nav motion.
 */
withDefaults(defineProps<{
  /** Nav item key that should read as the current location. */
  activeKey?: string;
  /** Nav item key whose route is still resolving. */
  pendingKey?: string;
  /** Icon-only mode: labels fade with the sidebar width instead of being removed. */
  collapsed?: boolean;
  label: string;
}>(), {
  collapsed: false,
});

const emit = defineEmits<{ navigate: [] }>();
const { t } = useI18n();
</script>

<template>
  <nav class="min-h-0 flex-1 overflow-auto px-3 py-3" :aria-label="label">
    <section v-for="(group, groupIndex) in navGroups" :key="group.key" class="mb-5 last:mb-0">
      <div v-if="collapsed && groupIndex > 0" class="mx-2 my-3 h-px bg-border" aria-hidden="true" />
      <div class="mb-2 h-4 overflow-hidden px-2">
        <div class="shell-label whitespace-nowrap text-[11px] font-semibold uppercase leading-4 text-muted-foreground" :class="collapsed ? 'max-w-0 opacity-0' : 'max-w-[200px]'">
          {{ t(group.titleKey) }}
        </div>
      </div>
      <RouterLink
        v-for="item in group.items"
        :key="item.key"
        :to="item.to"
        :title="collapsed ? t(item.titleKey) : undefined"
        :aria-busy="pendingKey === item.key ? 'true' : undefined"
        :aria-current="activeKey === item.key ? 'page' : undefined"
        class="group relative mb-1 flex h-9 max-lg:h-11 items-center overflow-hidden rounded-xl px-3 text-sm font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
        :class="[activeKey === item.key ? 'bg-brand-bg text-brand' : '', collapsed ? 'justify-center px-0' : 'gap-3']"
        @click="emit('navigate')"
      >
        <span
          v-if="activeKey === item.key"
          class="absolute inset-y-1.5 left-0 w-0.5 rounded-full bg-brand"
          aria-hidden="true"
        />
        <LoaderCircle v-if="pendingKey === item.key" class="size-4 shrink-0 animate-spin motion-reduce:animate-none" aria-hidden="true" />
        <component :is="item.icon" v-else class="size-4 shrink-0" aria-hidden="true" />
        <span
          class="shell-label min-w-0 truncate"
          :class="collapsed ? 'max-w-0 opacity-0' : 'max-w-[200px] flex-1'"
        >{{ t(item.titleKey) }}</span>
      </RouterLink>
    </section>
  </nav>
</template>
