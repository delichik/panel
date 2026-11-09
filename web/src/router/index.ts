import { createRouter, createWebHistory } from 'vue-router';
import { resolveActivityRedirect } from '@/api/activityRedirect';
import { setUnauthorizedHandler } from '@/api/client';
import { useSessionStore } from '@/stores/session';
import { lazyPage } from './lazyPage';
import { pageLoaders } from './pageLoaders';

// Page modules stay code-split, but defineAsyncComponent (via lazyPage) makes
// the route component itself sync so navigation resolves immediately. Chunk
// download no longer blocks title/nav/content swap; a page skeleton covers the
// rare first visit before prefetch finishes.
export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', component: lazyPage(pageLoaders.login), meta: { titleKey: 'routes.login.title', public: true } },
    { path: '/maintenance/backup', component: lazyPage(pageLoaders.maintenance), meta: { titleKey: 'routes.maintenance.title', public: true } },
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
        { path: 'applications/facility-apps', component: lazyPage(pageLoaders.applications), meta: { titleKey: 'routes.facilityApps.title' } },
        { path: 'applications/facility-apps/:facilityKind/config', component: lazyPage(pageLoaders.applications), meta: { titleKey: 'routes.facilityApps.title' } },
        { path: 'applications/facility-apps/:facilityKind', component: lazyPage(pageLoaders.applications), meta: { titleKey: 'routes.facilityApps.title' } },
        { path: 'dns/domains', component: lazyPage(pageLoaders.dns), meta: { titleKey: 'routes.dns.title' } },
        { path: 'certificates/domains', component: lazyPage(pageLoaders.certificates), meta: { titleKey: 'routes.certificates.title' } },
        { path: 'certificates/self-signed', component: lazyPage(pageLoaders.certificates), meta: { titleKey: 'routes.certificates.title' } },
        { path: 'certificates/keys', component: lazyPage(pageLoaders.certificates), meta: { titleKey: 'routes.certificates.title' } },
        { path: 'events', component: lazyPage(pageLoaders.events), meta: { titleKey: 'routes.events.title' } },
        { path: 'operations', redirect: (to) => ({ path: '/events', query: to.query }) },
        { path: 'tasks', redirect: (to) => ({ path: '/events', query: to.query }) },
        { path: 'logs', component: lazyPage(pageLoaders.logs), meta: { titleKey: 'routes.logs.title' } },
        { path: 'activity', redirect: (to) => resolveActivityRedirect(to.query) },
        { path: 'settings', redirect: '/settings/general' },
        { path: 'settings/general', component: lazyPage(pageLoaders.settings), meta: { titleKey: 'routes.settings.title' } },
        { path: 'settings/security', component: lazyPage(pageLoaders.settings), meta: { titleKey: 'routes.settings.title' } },
        { path: 'settings/certificates', component: lazyPage(pageLoaders.settings), meta: { titleKey: 'routes.settings.title' } },
        { path: 'settings/agent', component: lazyPage(pageLoaders.settings), meta: { titleKey: 'routes.settings.title' } },
        { path: 'settings/tailscale', component: lazyPage(pageLoaders.settings), meta: { titleKey: 'routes.settings.title' } },
        { path: 'settings/system-certificates', component: lazyPage(pageLoaders.settings), meta: { titleKey: 'routes.settings.title' } },
        { path: 'settings/system', component: lazyPage(pageLoaders.settings), meta: { titleKey: 'routes.settings.title' } },
        { path: 'settings/backups', component: lazyPage(pageLoaders.settings), meta: { titleKey: 'routes.settings.title' } },
        { path: 'debug', component: lazyPage(pageLoaders.debug), meta: { titleKey: 'routes.debug.title' } },
        // Catch-all inside the shell so unknown paths keep the app navigation.
        { path: ':pathMatch(.*)*', component: lazyPage(pageLoaders.notFound), meta: { titleKey: 'routes.notFound.title' } },
      ],
    },
  ],
});

function redirectToLogin() {
  const session = useSessionStore();
  session.onUnauthorized();
  const current = router.currentRoute.value;
  if (current.path !== '/login') {
    const redirect = current.fullPath;
    void router.push({ path: '/login', query: { redirect } }).catch(() => {
      // The navigation may race with bootstrap; the router guard performs the
      // same redirect when the session is not authenticated.
    });
  }
}

setUnauthorizedHandler(redirectToLogin);

// Lazy route chunks can fail to load at runtime (network hiccup, stale hashed
// bundles after a redeploy). Without a handler the navigation rejects and the
// page area stays blank. Retry once with a full reload; the guard flag is
// cleared on every successful navigation so a later genuine failure gets its
// own retry instead of looping forever on a permanently missing chunk.
router.onError((error) => {
  const message = String(error?.message ?? error);
  if (/Failed to fetch dynamically imported module|Importing a module script failed|ChunkLoadError/.test(message)) {
    if (!globalThis.sessionStorage?.getItem('router.chunkRetry')) {
      globalThis.sessionStorage?.setItem('router.chunkRetry', '1');
      window.location.reload();
    }
  }
});

router.afterEach(() => {
  globalThis.sessionStorage?.removeItem('router.chunkRetry');
});

router.beforeEach(async (to) => {
  const session = useSessionStore();
  if (!session.ready) await session.restore();
  if (to.meta.public) {
    if (to.path === '/login' && session.authenticated && !session.passwordChangeRequired) return '/overview';
    return true;
  }
  if (!session.authenticated) {
    // A stored token that could not be verified during restore (for example a
    // transient network error) is kept and allowed through optimistically; a
    // real 401 later clears the session and redirects to login.
    if (session.token) return true;
    return { path: '/login', query: { redirect: to.fullPath } };
  }
  if (session.passwordChangeRequired) return { path: '/login', query: { redirect: to.fullPath } };
  return true;
});
