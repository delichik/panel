import { createRouter, createWebHistory } from 'vue-router';
import type { RouteLocationGeneric } from 'vue-router';
import { setUnauthorizedHandler } from '@/api/client';
import { useSessionStore } from '@/stores/session';

// Route components are lazy-loaded so each page family ships as its own async
// chunk; the initial bundle only carries the shell (AppShell is the single
// eager layout component) plus shared vendor chunks.
const fail2BanRoute = import.meta.env.DEV
  ? { path: 'resources/fail2ban', component: () => import('@/views/security/index.vue'), meta: { titleKey: 'routes.fail2ban.title' } }
  : { path: 'resources/fail2ban', redirect: (to: RouteLocationGeneric) => ({ path: '/resources/firewall', query: to.query }) };

const redirectToFirewall = (to: RouteLocationGeneric) => ({ path: '/resources/firewall', query: to.query });
const redirectToFail2Ban = (to: RouteLocationGeneric) => ({ path: import.meta.env.DEV ? '/resources/fail2ban' : '/resources/firewall', query: to.query });

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', component: () => import('@/views/auth/LoginPage.vue'), meta: { titleKey: 'routes.login.title', public: true } },
    { path: '/maintenance/backup', component: () => import('@/views/maintenance/index.vue'), meta: { titleKey: 'routes.maintenance.title', public: true } },
    {
      path: '/',
      component: () => import('@/components/shell/AppShell.vue'),
      children: [
        { path: '', redirect: '/overview' },
        { path: 'overview', component: () => import('@/views/overview/index.vue'), meta: { titleKey: 'routes.overview.title' } },
        { path: 'servers', component: () => import('@/views/servers/index.vue'), meta: { titleKey: 'routes.servers.title' } },
        { path: 'credentials', component: () => import('@/views/credentials/index.vue'), meta: { titleKey: 'routes.credentials.title' } },
        { path: 'security', redirect: redirectToFirewall },
        { path: 'security/firewall', redirect: redirectToFirewall },
        { path: 'security/fail2ban', redirect: redirectToFail2Ban },
        { path: 'resources', redirect: '/resources/packages' },
        { path: 'resources/packages', component: () => import('@/views/resources/index.vue'), meta: { titleKey: 'routes.packages.title' } },
        { path: 'resources/containers', component: () => import('@/views/resources/index.vue'), meta: { titleKey: 'routes.containers.title' } },
        { path: 'resources/images', component: () => import('@/views/resources/index.vue'), meta: { titleKey: 'routes.images.title' } },
        { path: 'resources/networks', component: () => import('@/views/resources/index.vue'), meta: { titleKey: 'routes.networks.title' } },
        { path: 'resources/volumes', component: () => import('@/views/resources/index.vue'), meta: { titleKey: 'routes.volumes.title' } },
        { path: 'resources/firewall', component: () => import('@/views/security/index.vue'), meta: { titleKey: 'routes.firewall.title' } },
        fail2BanRoute,
        { path: 'applications', redirect: '/applications/apps' },
        { path: 'applications/apps', component: () => import('@/views/applications/index.vue'), meta: { titleKey: 'routes.applications.title' } },
        { path: 'applications/apps/create', component: () => import('@/views/applications/index.vue'), meta: { titleKey: 'routes.applications.title' } },
        { path: 'applications/apps/:applicationId/edit', component: () => import('@/views/applications/index.vue'), meta: { titleKey: 'routes.applications.title' } },
        { path: 'applications/facility-apps', component: () => import('@/views/applications/index.vue'), meta: { titleKey: 'routes.facilityApps.title' } },
        { path: 'applications/facility-apps/:facilityKind/config', component: () => import('@/views/applications/index.vue'), meta: { titleKey: 'routes.facilityApps.title' } },
        { path: 'applications/facility-apps/:facilityKind', component: () => import('@/views/applications/index.vue'), meta: { titleKey: 'routes.facilityApps.title' } },
        { path: 'dns/domains', component: () => import('@/views/dns/index.vue'), meta: { titleKey: 'routes.dns.title' } },
        { path: 'certificates/domains', component: () => import('@/views/certificates/index.vue'), meta: { titleKey: 'routes.certificates.title' } },
        { path: 'certificates/self-signed', component: () => import('@/views/certificates/index.vue'), meta: { titleKey: 'routes.certificates.title' } },
        { path: 'certificates/keys', component: () => import('@/views/certificates/index.vue'), meta: { titleKey: 'routes.certificates.title' } },
        { path: 'application-operations', component: () => import('@/views/application-operations/index.vue'), meta: { titleKey: 'routes.applicationOperations.title' } },
        { path: 'system-events', component: () => import('@/views/system-events/index.vue'), meta: { titleKey: 'routes.systemEvents.title' } },
        { path: 'tasks', component: () => import('@/views/tasks/index.vue'), meta: { titleKey: 'routes.tasks.title' } },
        { path: 'settings', redirect: '/settings/general' },
        { path: 'settings/general', component: () => import('@/views/settings/index.vue'), meta: { titleKey: 'routes.settings.title' } },
        { path: 'settings/security', component: () => import('@/views/settings/index.vue'), meta: { titleKey: 'routes.settings.title' } },
        { path: 'settings/certificates', component: () => import('@/views/settings/index.vue'), meta: { titleKey: 'routes.settings.title' } },
        { path: 'settings/system-certificates', component: () => import('@/views/settings/index.vue'), meta: { titleKey: 'routes.settings.title' } },
        { path: 'settings/system', component: () => import('@/views/settings/index.vue'), meta: { titleKey: 'routes.settings.title' } },
        { path: 'settings/backups', component: () => import('@/views/settings/index.vue'), meta: { titleKey: 'routes.settings.title' } },
        { path: 'debug', component: () => import('@/views/debug/index.vue'), meta: { titleKey: 'routes.debug.title' } },
        // Catch-all inside the shell so unknown paths keep the app navigation.
        { path: ':pathMatch(.*)*', component: () => import('@/components/templates/NotFoundPage.vue'), meta: { titleKey: 'routes.notFound.title' } },
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
