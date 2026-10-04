<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Languages, LoaderCircle, Menu, Moon, Palette, PanelLeftClose, PanelLeftOpen, Sun, UserCircle, X } from '@lucide/vue';
import { useOverlayBehavior } from '@/composables/useOverlayBehavior';
import Badge from '@/components/ui/Badge.vue';
import Dropdown from '@/components/ui/Dropdown.vue';
import DropdownItem from '@/components/ui/DropdownItem.vue';
import IconButton from '@/components/ui/IconButton.vue';
import LoadingOverlay from '@/components/ui/LoadingOverlay.vue';
import { useErrorToast } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import { useThemeMode, type ThemeMode, type ThemeScheme } from '@/design/theme';
import { settingsApi } from '@/api/settings';
import { useSessionStore } from '@/stores/session';
import { routeNavigation } from '@/router/navigationState';
import { activeNavKey } from './navModel';
import NavList from './NavList.vue';

const route = useRoute();
const router = useRouter();
const session = useSessionStore();
const { t, locale, setLocale } = useI18n();
const notifyError = useErrorToast();
const { mode, resolved, setMode, scheme, setScheme } = useThemeMode();
const collapsed = ref(localStorage.getItem('panel.nav.collapsed') === 'true');
const drawerId = useId();
const drawerOpen = ref(false);
const drawer = ref<HTMLElement | null>(null);
const { onKeydown: onDrawerKeydown } = useOverlayBehavior({
  open: () => drawerOpen.value,
  containerRef: drawer,
  onClose: () => {
    drawerOpen.value = false;
  },
  lockScroll: true,
});
const lgQuery = typeof window.matchMedia === 'function' ? window.matchMedia('(min-width: 1024px)') : null;
function closeDrawerOnDesktop(event: MediaQueryListEvent) {
  if (event.matches) drawerOpen.value = false;
}
lgQuery?.addEventListener('change', closeDrawerOnDesktop);
onBeforeUnmount(() => lgQuery?.removeEventListener('change', closeDrawerOnDesktop));
const signingOut = ref(false);
const languageSaving = ref(false);
let languageRequestId = 0;
const activeKey = computed(() => activeNavKey(route.path));
const pendingNavKey = computed(() => routeNavigation.pending.value ? activeNavKey(routeNavigation.targetPath.value.split('?')[0] || '') : undefined);
const displayedActiveKey = computed(() => pendingNavKey.value || activeKey.value);
const title = computed(() => t(String(route.meta.titleKey || 'app.name')));
const navigationLabel = computed(() => {
  if (!routeNavigation.pending.value) return title.value;
  const targetTitle = routeNavigation.titleKey.value ? t(routeNavigation.titleKey.value) : title.value;
  return t('layout.navigation.loading', { title: targetTitle });
});

watch(collapsed, (value) => localStorage.setItem('panel.nav.collapsed', String(value)));
watch(() => route.fullPath, () => {
  drawerOpen.value = false;
});
watch(() => routeNavigation.pending.value, (pending) => {
  if (pending) drawerOpen.value = false;
});

function supportedLocale(value: string) {
  return value === 'zh-CN' ? 'zh-CN' : 'en';
}

onMounted(async () => {
  const requestId = languageRequestId;
  try {
    const settings = await settingsApi.runtime();
    if (requestId === languageRequestId) setLocale(supportedLocale(settings.language));
  } catch {
    // Keep the locally stored locale when runtime settings are temporarily
    // unavailable; the next explicit switch retries persistence.
  }
});

async function toggleLanguage() {
  if (languageSaving.value) return;
  const requestId = ++languageRequestId;
  const previous = locale.value;
  const next = previous === 'zh-CN' ? 'en' : 'zh-CN';
  languageSaving.value = true;
  setLocale(next);
  try {
    const settings = await settingsApi.updateLanguage(next);
    if (requestId === languageRequestId) setLocale(supportedLocale(settings.language));
  } catch (error) {
    if (requestId === languageRequestId) {
      setLocale(previous);
      notifyError(t('layout.language.saveFailed'), error);
    }
  } finally {
    if (requestId === languageRequestId) languageSaving.value = false;
  }
}

const themeItems: Array<{ key: ThemeMode; labelKey: string }> = [
  { key: 'system', labelKey: 'layout.theme.system' },
  { key: 'light', labelKey: 'layout.theme.light' },
  { key: 'dark', labelKey: 'layout.theme.dark' },
];
const schemeItems: Array<{ key: ThemeScheme; labelKey: string }> = [
  { key: 'lighthouse', labelKey: 'layout.scheme.lighthouse' },
  { key: 'ocean', labelKey: 'layout.scheme.ocean' },
];

async function signOut() {
  signingOut.value = true;
  try {
    await session.logout();
    await router.push('/login');
  } finally {
    signingOut.value = false;
  }
}
</script>

<template>
  <div class="shell-grid relative grid min-h-dvh w-full overflow-visible bg-background lg:h-dvh lg:min-h-0 lg:overflow-hidden lg:grid-cols-[var(--shell-nav)_minmax(0,1fr)]" :inert="drawerOpen || undefined" :style="{ '--shell-nav': collapsed ? '76px' : '260px' }">
    <a href="#main-content" class="sr-only focus:not-sr-only focus:absolute focus:left-4 focus:top-4 focus:z-[70] focus:rounded-xl focus:border focus:border-border focus:bg-popover focus:px-3 focus:py-2 focus:text-sm focus:text-popover-foreground focus:shadow-xl">{{ t('layout.skipToContent') }}</a>
    <LoadingOverlay v-if="signingOut" :label="t('layout.signingOut')" />
    <aside class="hidden min-h-0 flex-col border-r border-border bg-card lg:flex">
      <div class="flex min-h-16 items-center border-b border-border px-4" :class="collapsed ? 'justify-center' : 'gap-3'">
        <img src="/favicon.svg" class="size-9 shrink-0 rounded-xl" alt="" aria-hidden="true" />
        <div class="shell-label min-w-0 truncate" :class="collapsed ? 'max-w-0 opacity-0' : 'max-w-[160px] flex-1'">
          <strong class="block truncate text-sm font-semibold text-foreground">{{ t('app.name') }}</strong>
          <span class="block truncate text-xs text-muted-foreground">{{ t('app.subtitle') }}</span>
        </div>
      </div>
      <NavList id="app-navigation" :active-key="displayedActiveKey" :pending-key="pendingNavKey" :collapsed="collapsed" :label="t('layout.main')" />
    </aside>

    <main class="grid min-h-dvh min-w-0 grid-rows-[56px_auto] lg:min-h-0 lg:grid-rows-[56px_minmax(0,1fr)]">
      <header class="relative flex min-w-0 items-center justify-between gap-3 border-b border-border bg-background px-4 max-lg:sticky max-lg:top-0 max-lg:z-40" :aria-busy="routeNavigation.pending.value ? 'true' : undefined">
        <div class="flex min-w-0 items-center gap-2">
          <IconButton class="lg:hidden" :label="t('layout.nav.open')" :aria-expanded="drawerOpen" :aria-controls="drawerId" aria-haspopup="dialog" @click="drawerOpen = true">
            <Menu />
          </IconButton>
          <IconButton
            class="hidden lg:inline-grid"
            :label="t('layout.nav.collapse')"
            :aria-expanded="collapsed ? 'false' : 'true'"
            aria-controls="app-navigation"
            @click="collapsed = !collapsed"
          >
            <Transition name="fade" mode="out-in">
              <PanelLeftOpen v-if="collapsed" />
              <PanelLeftClose v-else />
            </Transition>
          </IconButton>
          <h1 class="truncate text-base font-semibold text-foreground">{{ navigationLabel }}</h1>
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
          <Dropdown>
            <template #trigger>
              <IconButton :label="t('layout.scheme')">
                <Palette />
              </IconButton>
            </template>
            <DropdownItem v-for="item in schemeItems" :key="item.key" @click="setScheme(item.key)">
              <span class="w-3">{{ scheme === item.key ? '*' : '' }}</span>
              {{ t(item.labelKey) }}
            </DropdownItem>
          </Dropdown>
          <IconButton :label="t(languageSaving ? 'layout.language.saving' : 'layout.language')" :disabled="languageSaving" :aria-busy="languageSaving ? 'true' : undefined" @click="toggleLanguage">
            <LoaderCircle v-if="languageSaving" class="animate-spin motion-reduce:animate-none" aria-hidden="true" />
            <Languages v-else />
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
        <div v-if="routeNavigation.pending.value" class="absolute inset-x-0 bottom-0 h-0.5 overflow-hidden bg-accent" role="progressbar" :aria-label="navigationLabel">
          <div class="motion-progress h-full w-1/3 rounded-full bg-brand" />
        </div>
      </header>
      <section id="main-content" tabindex="-1" class="min-h-0 min-w-0 overflow-visible lg:overflow-hidden focus:outline-none max-lg:scroll-mt-14">
        <RouterView v-slot="{ Component }">
          <Transition name="route" mode="out-in">
            <!-- 路由组件可能含多个根节点（如 ListPage + Dialog），包一层单一根元素才能做过渡动画；
                 按 route.path 作 key：每次路径切换都重挂载页面并播放淡出淡入（含网络/卷等同组件路由）；
                 仅查询参数变化（分页、筛选）不重挂载，保持 URL 状态行为 -->
            <div :key="route.path" class="h-full min-h-0">
              <component :is="Component" />
            </div>
          </Transition>
        </RouterView>
      </section>
    </main>

    <Teleport to="body">
      <Transition name="drawer">
        <div v-if="drawerOpen" class="fixed inset-0 z-50 bg-overlay lg:hidden" @click.self="drawerOpen = false">
          <aside
            :id="drawerId"
            ref="drawer"
            role="dialog"
            aria-modal="true"
            :aria-label="t('layout.main')"
            tabindex="-1"
            class="drawer-panel flex h-full w-[292px] max-w-[86vw] flex-col border-r border-border bg-card"
            @keydown="onDrawerKeydown"
          >
            <div class="flex min-h-16 items-center justify-between gap-3 border-b border-border px-4">
              <div class="flex min-w-0 items-center gap-3">
                <img src="/favicon.svg" class="size-9 rounded-xl" alt="" aria-hidden="true" />
                <div class="min-w-0">
                  <strong class="block truncate text-sm font-semibold text-foreground">{{ t('app.name') }}</strong>
                  <span class="block truncate text-xs text-muted-foreground">{{ t('app.subtitle') }}</span>
                </div>
              </div>
              <IconButton class="max-lg:size-11" :label="t('common.close')" @click="drawerOpen = false">
                <X />
              </IconButton>
            </div>
            <NavList :active-key="displayedActiveKey" :pending-key="pendingNavKey" :label="t('layout.main')" @navigate="drawerOpen = false" />
          </aside>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>
