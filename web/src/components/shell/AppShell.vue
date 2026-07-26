<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Languages, Menu, Moon, PanelLeftClose, PanelLeftOpen, Sun, UserCircle } from '@lucide/vue';
import Badge from '@/components/ui/Badge.vue';
import Dropdown from '@/components/ui/Dropdown.vue';
import DropdownItem from '@/components/ui/DropdownItem.vue';
import IconButton from '@/components/ui/IconButton.vue';
import { useI18n } from '@/i18n';
import { useThemeMode, type ThemeMode } from '@/design/theme';
import { useSessionStore } from '@/stores/session';
import { activeNavKey, navGroups } from './navModel';

const route = useRoute();
const router = useRouter();
const session = useSessionStore();
const { t, locale, setLocale } = useI18n();
const { mode, resolved, setMode } = useThemeMode();
const collapsed = ref(localStorage.getItem('panel.nav.collapsed') === 'true');
const drawerOpen = ref(false);
const activeKey = computed(() => activeNavKey(route.path));
const title = computed(() => t(String(route.meta.titleKey || 'app.name')));

watch(collapsed, (value) => localStorage.setItem('panel.nav.collapsed', String(value)));
watch(() => route.fullPath, () => {
  drawerOpen.value = false;
});

const themeItems: Array<{ key: ThemeMode; labelKey: string }> = [
  { key: 'system', labelKey: 'layout.theme.system' },
  { key: 'light', labelKey: 'layout.theme.light' },
  { key: 'dark', labelKey: 'layout.theme.dark' },
];

async function signOut() {
  await session.logout();
  await router.push('/login');
}
</script>

<template>
  <div class="grid h-dvh min-h-0 w-full overflow-hidden bg-background lg:grid-cols-[var(--shell-nav)_minmax(0,1fr)]" :style="{ '--shell-nav': collapsed ? '76px' : '260px' }">
    <aside class="hidden min-h-0 flex-col border-r border-border bg-card lg:flex">
      <div class="flex min-h-16 items-center gap-3 border-b border-border px-4">
        <div class="grid size-9 shrink-0 place-items-center rounded-xl bg-primary text-sm font-bold text-primary-foreground">P</div>
        <div v-if="!collapsed" class="min-w-0">
          <strong class="block truncate text-sm font-semibold text-foreground">{{ t('app.name') }}</strong>
          <span class="block truncate text-xs text-muted-foreground">{{ t('app.subtitle') }}</span>
        </div>
      </div>
      <nav class="min-h-0 flex-1 overflow-auto px-3 py-3" :aria-label="t('layout.main')">
        <section v-for="group in navGroups" :key="group.key" class="mb-5 last:mb-0">
          <div v-if="!collapsed" class="mb-2 px-2 text-[11px] font-semibold uppercase text-muted-foreground">{{ t(group.titleKey) }}</div>
          <div v-else-if="group.key !== navGroups[0]?.key" class="mx-2 my-3 h-px bg-border" />
          <RouterLink
            v-for="item in group.items"
            :key="item.key"
            :to="item.to"
            :title="collapsed ? t(item.titleKey) : undefined"
            class="mb-1 flex h-9 items-center rounded-xl px-3 text-sm font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
            :class="[activeKey === item.key ? 'bg-accent text-foreground' : '', collapsed ? 'justify-center px-0' : 'gap-3']"
          >
            <component :is="item.icon" class="size-4 shrink-0" aria-hidden="true" />
            <span v-if="!collapsed" class="truncate">{{ t(item.titleKey) }}</span>
          </RouterLink>
        </section>
      </nav>
    </aside>

    <main class="grid min-h-0 min-w-0 grid-rows-[56px_minmax(0,1fr)]">
      <header class="flex min-w-0 items-center justify-between gap-3 border-b border-border bg-background px-4">
        <div class="flex min-w-0 items-center gap-2">
          <IconButton class="lg:hidden" :label="t('layout.nav.open')" @click="drawerOpen = true">
            <Menu />
          </IconButton>
          <IconButton class="hidden lg:inline-grid" :label="t('layout.nav.collapse')" @click="collapsed = !collapsed">
            <PanelLeftOpen v-if="collapsed" />
            <PanelLeftClose v-else />
          </IconButton>
          <h1 class="truncate text-base font-semibold text-foreground">{{ title }}</h1>
          <Badge>{{ t('layout.alpha') }}</Badge>
        </div>
        <div class="flex items-center gap-1">
          <Dropdown>
            <template #trigger>
              <IconButton :label="t('layout.theme')">
                <Moon v-if="resolved === 'dark'" />
                <Sun v-else />
              </IconButton>
            </template>
            <DropdownItem v-for="item in themeItems" :key="item.key" @click="setMode(item.key)">
              <span class="w-3">{{ mode === item.key ? '*' : '' }}</span>
              {{ t(item.labelKey) }}
            </DropdownItem>
          </Dropdown>
          <IconButton :label="t('layout.language')" @click="setLocale(locale === 'zh-CN' ? 'en' : 'zh-CN')">
            <Languages />
          </IconButton>
          <Dropdown>
            <template #trigger>
              <IconButton :label="t('layout.account')">
                <UserCircle />
              </IconButton>
            </template>
            <div class="px-3 py-2 text-xs text-muted-foreground">{{ session.username }}</div>
            <DropdownItem @click="signOut">{{ t('layout.logout') }}</DropdownItem>
          </Dropdown>
        </div>
      </header>
      <section class="min-h-0 min-w-0 overflow-hidden">
        <RouterView />
      </section>
    </main>

    <Teleport to="body">
      <div v-if="drawerOpen" class="fixed inset-0 z-50 bg-overlay lg:hidden" @click.self="drawerOpen = false">
        <aside class="flex h-full w-[292px] max-w-[86vw] flex-col border-r border-border bg-card">
          <div class="flex min-h-16 items-center gap-3 border-b border-border px-4">
            <div class="grid size-9 place-items-center rounded-xl bg-primary text-sm font-bold text-primary-foreground">P</div>
            <div class="min-w-0">
              <strong class="block truncate text-sm font-semibold text-foreground">{{ t('app.name') }}</strong>
              <span class="block truncate text-xs text-muted-foreground">{{ t('app.subtitle') }}</span>
            </div>
          </div>
          <nav class="min-h-0 flex-1 overflow-auto px-3 py-3" :aria-label="t('layout.main')">
            <section v-for="group in navGroups" :key="group.key" class="mb-5 last:mb-0">
              <div class="mb-2 px-2 text-[11px] font-semibold uppercase text-muted-foreground">{{ t(group.titleKey) }}</div>
              <RouterLink
                v-for="item in group.items"
                :key="item.key"
                :to="item.to"
                class="mb-1 flex h-9 items-center gap-3 rounded-xl px-3 text-sm font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                :class="activeKey === item.key ? 'bg-accent text-foreground' : ''"
              >
                <component :is="item.icon" class="size-4 shrink-0" aria-hidden="true" />
                <span class="truncate">{{ t(item.titleKey) }}</span>
              </RouterLink>
            </section>
          </nav>
        </aside>
      </div>
    </Teleport>
  </div>
</template>
