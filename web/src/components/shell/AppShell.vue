<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Languages, Menu, PanelLeftClose, PanelLeftOpen, UserCircle, X } from '@/icons';
import { useOverlayBehavior } from '@/composables/useOverlayBehavior';
import ProgressRing from '@/components/ui/ProgressRing.vue';
import Dropdown from '@/components/ui/Dropdown.vue';
import DropdownItem from '@/components/ui/DropdownItem.vue';
import IconButton from '@/components/ui/IconButton.vue';
import LoadingOverlay from '@/components/ui/LoadingOverlay.vue';
import { useErrorDialog } from '@/components/ui/errorDialog';
import { useI18n } from '@/i18n';
import { useAppearance } from '@/design/appearance';
import { settingsApi } from '@/api/settings';
import { useSessionStore } from '@/stores/session';
import { formatCountdown, remainingSeconds } from '@/utils/datetime';
import { focusWithoutScroll } from '@/utils/focus';
import { prefetchShellPages } from '@/router/pageLoaders';
import { activeNavKey } from './navModel';
import AppearanceFlyout from './AppearanceFlyout.vue';
import EventNotificationBridge from './EventNotificationBridge.vue';
import NavList from './NavList.vue';

const route = useRoute();
const router = useRouter();
const session = useSessionStore();
const { t, locale, setLocale } = useI18n();
const notifyError = useErrorDialog();
const { load: loadAppearance } = useAppearance();
const collapsed = ref(localStorage.getItem('panel.nav.collapsed') === 'true');
const drawerId = useId();
const drawerOpen = ref(false);
const drawer = ref<HTMLElement | null>(null);
const mainContent = ref<HTMLElement | null>(null);
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
const signingOut = ref(false);
const languageSaving = ref(false);
let languageRequestId = 0;
const activeKey = computed(() => activeNavKey(route.path));
const nowMs = ref(Date.now());
let credentialClock: number | undefined;
const isTempCredential = computed(() => session.authKind === 'credential');
const tempCredentialActingUser = computed(() => {
  const display = session.displayName.trim();
  const username = session.username.trim();
  if (display && username && display !== username) return `${display} (${username})`;
  return display || username || '—';
});
const tempCredentialRemainingSeconds = computed(() => remainingSeconds(session.expiresAt, nowMs.value));
const tempCredentialRemainingLabel = computed(() => (
  tempCredentialRemainingSeconds.value > 0
    ? t('layout.tempCredential.remaining', { time: formatCountdown(tempCredentialRemainingSeconds.value) })
    : t('account.credentials.statusValue.expired')
));

function syncCredentialClock() {
  window.clearInterval(credentialClock);
  credentialClock = undefined;
  if (!isTempCredential.value) return;
  nowMs.value = Date.now();
  credentialClock = window.setInterval(() => {
    nowMs.value = Date.now();
  }, 1000);
}
watch(isTempCredential, syncCredentialClock, { immediate: true });

/**
 * WinUI NavigationView pane widths. The token layer (Lead) owns the numbers;
 * the CSS var fallbacks keep the shell usable when a token is missing and let
 * main.css replace the value in one place.
 */
const shellNavStyle = computed<Record<string, string>>(() => ({
  '--shell-nav': collapsed.value
    ? 'var(--panel-nav-width-compact, 48px)'
    : 'var(--panel-nav-width, 320px)',
}));

/**
 * Page identity for the WinUI Page-refresh transition. Routes that belong to
 * one page surface (for example the 9 settings sections) share a `meta.pageKey`
 * so switching inside the page never remounts it or replays the route motion.
 */
const pageKey = computed(() => (route.meta.pageKey as string | undefined) ?? route.path);
let routeFocusTimer: number | undefined;

function focusPageContent() {
  // WinUI moves focus into the new page on navigation: the page heading when
  // the page provides one (PageHeader h1 has tabindex="-1"), else the content
  // region itself. preventScroll keeps the shell from jumping.
  const heading = mainContent.value?.querySelector<HTMLElement>('h1');
  focusWithoutScroll(heading ?? mainContent.value);
}

watch(pageKey, () => {
  window.clearTimeout(routeFocusTimer);
  // Deferred a task so the drawer's focus restore (narrow screen) and the new
  // page's DOM commit finish first; the page region then owns focus.
  routeFocusTimer = window.setTimeout(focusPageContent, 0);
});

watch(collapsed, (value) => localStorage.setItem('panel.nav.collapsed', String(value)));
watch(() => route.fullPath, () => {
  drawerOpen.value = false;
});

function supportedLocale(value: string) {
  return value === 'zh-CN' ? 'zh-CN' : 'en';
}

onMounted(async () => {
  // Warm console page chunks in the background so later clicks swap static
  // page chrome immediately instead of waiting on the first import().
  prefetchShellPages();
  // The server is the appearance source of truth: App.vue already applied the
  // cached/public value for the first paint, this authenticated load overwrites
  // it once the session is available (deduplicated by the store).
  void loadAppearance();
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

async function signOut() {
  signingOut.value = true;
  try {
    await session.logout();
    await router.push('/login');
  } finally {
    signingOut.value = false;
  }
}

onBeforeUnmount(() => {
  lgQuery?.removeEventListener('change', closeDrawerOnDesktop);
  window.clearTimeout(routeFocusTimer);
  window.clearInterval(credentialClock);
});
</script>

<template>
  <div
    class="shell-grid relative grid min-h-dvh w-full overflow-visible bg-transparent lg:h-dvh lg:min-h-0 lg:overflow-hidden lg:grid-cols-shell lg:grid-rows-shell"
    :inert="drawerOpen || undefined"
    :style="shellNavStyle"
  >
    <!--
      WinUI 3 shell: a single full-width title bar row (app identity + global
      commands) above a NavigationView pane / content row. The pane shows the
      Mica base, the title bar and content sit on the translucent layer fill.
    -->
    <a href="#main-content" class="panel-shadow-overlay sr-only focus:not-sr-only focus:absolute focus:left-4 focus:top-4 focus:z-skip focus:rounded-overlay focus:border focus:border-border-strong focus:bg-popover focus:px-3 focus:py-2 focus:text-sm focus:text-popover-foreground focus:outline-none focus:ring-2 focus:ring-ring focus:ring-inset">{{ t('layout.skipToContent') }}</a>
    <LoadingOverlay v-if="signingOut" :label="t('layout.signingOut')" />
    <!-- Event notifications only exist inside the authenticated shell: the login page has no AppShell. -->
    <EventNotificationBridge />

    <!-- Title bar: full window width (both grid columns); no page-title duplication.
         Mobile (360–430px): the bar keeps only the app identity + global commands,
         with the identity truncating instead of pushing the command strip off-screen. -->
    <header class="relative z-30 flex h-shell-header min-w-0 items-center justify-between gap-3 border-b border-stroke bg-layer px-3 max-lg:sticky max-lg:top-0 max-lg:z-40 max-lg:bg-popover max-sm:gap-2 max-sm:px-2 lg:col-span-2">
      <div class="flex min-w-0 flex-1 items-center gap-2 max-sm:gap-1.5">
        <IconButton class="lg:hidden" :label="t('layout.nav.open')" :aria-expanded="drawerOpen" :aria-controls="drawerId" aria-haspopup="dialog" @click="drawerOpen = true">
          <Menu />
        </IconButton>
        <IconButton class="hidden lg:inline-grid" :label="t('layout.nav.collapse')" :aria-expanded="collapsed ? 'false' : 'true'" aria-controls="app-navigation" @click="collapsed = !collapsed">
          <Transition name="fade" mode="out-in">
            <PanelLeftOpen v-if="collapsed" />
            <PanelLeftClose v-else />
          </Transition>
        </IconButton>
        <strong class="min-w-0 truncate text-sm font-semibold text-foreground">{{ t('app.name') }}</strong>
      </div>
      <!-- WinUI command strip: borderless icon controls, fill-only hover feedback. -->
      <div class="flex shrink-0 items-center gap-0.5 max-sm:gap-0">
        <button
          v-if="isTempCredential"
          type="button"
          class="mr-1.5 inline-flex max-w-80 items-center gap-1.5 truncate rounded-control border border-warning-border bg-warning-bg px-2.5 py-1 text-xs font-medium text-warning transition-control duration-fast hover:bg-warning-bg/80 max-md:max-w-44"
          :title="t('layout.tempCredential.banner', { user: tempCredentialActingUser, remaining: tempCredentialRemainingLabel })"
          @click="router.push('/account')"
        >
          <span class="truncate">{{ t('layout.tempCredential.banner', { user: tempCredentialActingUser, remaining: tempCredentialRemainingLabel }) }}</span>
        </button>
        <AppearanceFlyout />
        <IconButton :label="t(languageSaving ? 'layout.language.saving' : 'layout.language')" :disabled="languageSaving" :aria-busy="languageSaving ? 'true' : undefined" @click="toggleLanguage">
          <ProgressRing v-if="languageSaving" :size="16" aria-hidden="true" />
          <Languages v-else />
        </IconButton>
        <Dropdown>
          <template #trigger>
            <IconButton :label="t('layout.account')">
              <UserCircle />
            </IconButton>
          </template>
          <div class="px-3 py-2 text-xs text-muted-foreground">
            <div>{{ session.displayName || session.username }}</div>
            <div v-if="isTempCredential" class="mt-0.5 text-warning">
              {{ t('layout.tempCredential.banner', { user: tempCredentialActingUser, remaining: tempCredentialRemainingLabel }) }}
            </div>
          </div>
          <DropdownItem @click="router.push('/account')">{{ t('routes.account.title') }}</DropdownItem>
          <DropdownItem @click="signOut">{{ t('layout.logout') }}</DropdownItem>
        </Dropdown>
      </div>
    </header>

    <!-- NavigationView pane: transparent over Mica, separated by a 1px stroke. -->
    <aside class="hidden min-h-0 flex-col border-r border-stroke lg:flex">
      <NavList id="app-navigation" :active-key="activeKey" :collapsed="collapsed" :label="t('layout.main')" />
    </aside>

    <!-- Content region is the main landmark; the title bar stays outside it. -->
    <main id="main-content" ref="mainContent" tabindex="-1" class="relative isolate grid min-h-[calc(100dvh-var(--panel-shell-header))] min-w-0 grid-rows-1-safe overflow-visible lg:overflow-hidden bg-layer focus:outline-none lg:min-h-0 max-lg:scroll-mt-14">
      <RouterView v-slot="{ Component }">
        <!-- WinUI Page refresh: the outgoing page becomes an absolute overlay
             and fades out while the target page fades in; both background and
             text leave together. Query changes keep the same key and never
             replay the transition. -->
        <Transition name="route" mode="out-in">
          <!-- 路由组件可能含多个根节点（如 ListPage + Dialog），包一层单一根元素才能做过渡动画；
               按页面标识作 key：切换设置分区等同页路由不重挂载，只有真正换页才重播进场。 -->
          <div :key="pageKey" class="h-full min-h-0">
            <component :is="Component" />
          </div>
        </Transition>
      </RouterView>
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
            class="drawer-panel flex h-full w-nav max-w-[86vw] flex-col border-r border-stroke bg-flyout backdrop-blur-flyout"
            @keydown="onDrawerKeydown"
          >
            <div class="flex h-shell-header shrink-0 items-center justify-end border-b border-stroke px-2">
              <IconButton class="max-lg:size-11" :label="t('common.close')" @click="drawerOpen = false">
                <X />
              </IconButton>
            </div>
            <NavList :active-key="activeKey" :label="t('layout.main')" @navigate="drawerOpen = false" />
          </aside>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>


