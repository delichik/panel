import { createRouter, createWebHistory } from 'vue-router';
import { setUnauthorizedHandler } from '@/api/client';
import { resolveActivityRedirect } from '@/api/activityRedirect';
import { useSessionStore } from '@/stores/session';
import { lazyPage } from './lazyPage';
import { pageLoaders } from './pageLoaders';

const CHUNK_RETRY_KEY = 'router.chunkRetry';

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      component: lazyPage(pageLoaders.login),
      meta: { titleKey: 'routes.login.title', public: true },
    },
    {
      path: '/maintenance/backup',
      component: lazyPage(pageLoaders.maintenance),
      meta: { titleKey: 'routes.maintenance.title', public: true },
    },
    {
      path: '/',
      component: lazyPage(pageLoaders.appShell),
      children: [
        { path: '', redirect: '/overview' },
        { path: 'overview', component: lazyPage(pageLoaders.overview), meta: { titleKey: 'routes.overview.title' } },
        { path: 'servers', component: lazyPage(pageLoaders.servers), meta: { titleKey: 'routes.servers.title' } },
        { path: 'credentials', component: lazyPage(pageLoaders.credentials), meta: { titleKey: 'routes.credentials.title' } },
        { path: 'applications', redirect: '/applications/apps' },
        { path: 'applications/apps', component: lazyPage(pageLoaders.applications), meta: { titleKey: 'routes.applications.title' } },
        { path: 'applications/apps/create', component: lazyPage(pageLoaders.applications), meta: { titleKey: 'routes.applications.title' } },
        { path: 'applications/apps/:applicationId/edit', component: lazyPage(pageLoaders.applications), meta: { titleKey: 'routes.applications.title' } },
        { path: 'applications/apps/:applicationId', component: lazyPage(pageLoaders.applications), meta: { titleKey: 'routes.applications.title' } },
        { path: 'applications/facility-apps', component: lazyPage(pageLoaders.applications), meta: { titleKey: 'routes.facilityApps.title' } },
        { path: 'applications/facility-apps/:facilityKind/config', component: lazyPage(pageLoaders.applications), meta: { titleKey: 'routes.facilityApps.title' } },
        { path: 'applications/facility-apps/:facilityKind', component: lazyPage(pageLoaders.applications), meta: { titleKey: 'routes.facilityApps.title' } },
        { path: 'dns/domains', component: lazyPage(pageLoaders.dns), meta: { titleKey: 'routes.dns.title' } },
        { path: 'certificates/domains', component: lazyPage(pageLoaders.certificates), meta: { titleKey: 'routes.certificates.title', pageKey: 'certificates' } },
        { path: 'certificates/self-signed', component: lazyPage(pageLoaders.certificates), meta: { titleKey: 'routes.certificates.title', pageKey: 'certificates' } },
        { path: 'certificates/keys', component: lazyPage(pageLoaders.certificates), meta: { titleKey: 'routes.certificates.title', pageKey: 'certificates' } },
        { path: 'events', component: lazyPage(pageLoaders.events), meta: { titleKey: 'routes.events.title' } },
        { path: 'operations', redirect: (to) => ({ path: '/events', query: to.query }) },
        { path: 'tasks', redirect: (to) => ({ path: '/events', query: to.query }) },
        { path: 'logs', component: lazyPage(pageLoaders.logs), meta: { titleKey: 'routes.logs.title' } },
        { path: 'activity', redirect: (to) => resolveActivityRedirect(to.query) },
        // Settings is one page: section switches must not remount it or replay the
        // route transition. `pageKey` keeps the shell transition key stable.
        { path: 'settings', redirect: '/settings/general' },
        {
          path: 'settings/:section',
          name: 'settings',
          component: lazyPage(pageLoaders.settings),
          meta: { titleKey: 'routes.settings.title', pageKey: 'settings' },
        },
        { path: 'debug', component: lazyPage(pageLoaders.debug), meta: { titleKey: 'routes.debug.title' } },
        // Catch-all inside the shell so unknown paths keep the app navigation.
        { path: ':pathMatch(.*)*', component: lazyPage(pageLoaders.notFound), meta: { titleKey: 'routes.notFound.title' } },
      ],
    },
  ],
  // WinUI Frame navigation starts a new page at the top; only same-pageKey
  // parameter changes (settings sections, certificate tabs) keep scroll.
  scrollBehavior(to, from, savedPosition) {
    const toKey = to.meta.pageKey ?? to.path;
    const fromKey = from.meta.pageKey ?? from.path;
    if (toKey === fromKey) return savedPosition ?? false;
    return { top: 0 };
  },
});

/**
 * Global 401 handler: drop the session and send the user to the login page with
 * a redirect back to the current location. Kept in the router module so API
 * calls do not need to know about navigation.
 */
setUnauthorizedHandler(() => {
  useSessionStore().onUnauthorized();
  const current = router.currentRoute.value;
  if (current.path !== '/login') {
    const redirect = current.fullPath;
    void router.push({ path: '/login', query: { redirect } }).catch(() => {});
  }
});

/**
 * A stale deployment can leave a hashed lazy chunk missing. Reload once so the
 * new build's asset manifest is used, but do not loop if the reload fails too.
 */
router.onError((error) => {
  const message = String((error as Error | null)?.message ?? error);
  if (
    /Failed to fetch dynamically imported module|Importing a module script failed|ChunkLoadError/.test(message)
    && !globalThis.sessionStorage?.getItem(CHUNK_RETRY_KEY)
  ) {
    globalThis.sessionStorage?.setItem(CHUNK_RETRY_KEY, '1');
    window.location.reload();
  }
});
router.afterEach(() => {
  globalThis.sessionStorage?.removeItem(CHUNK_RETRY_KEY);
});

router.beforeEach(async (to) => {
  const session = useSessionStore();
  if (!session.ready) await session.restore();
  if (to.meta.public) {
    if (to.path === '/login' && session.authenticated && !session.passwordChangeRequired) return '/overview';
    return true;
  }
  if (session.authenticated) {
    if (session.passwordChangeRequired) return { path: '/login', query: { redirect: to.fullPath } };
    return true;
  }
  if (session.token) return true;
  return { path: '/login', query: { redirect: to.fullPath } };
});
