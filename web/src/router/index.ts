import { createRouter, createWebHistory } from 'vue-router';
import type { RouteLocationGeneric } from 'vue-router';
import AppShell from '@/components/shell/AppShell.vue';
import OverviewPage from '@/views/overview/index.vue';
import ServersPage from '@/views/servers/index.vue';
import CredentialsPage from '@/views/credentials/index.vue';
import SecurityPage from '@/views/security/index.vue';
import ResourcesPage from '@/views/resources/index.vue';
import ApplicationsPage from '@/views/applications/index.vue';
import DnsPage from '@/views/dns/index.vue';
import CertificatesPage from '@/views/certificates/index.vue';
import ApplicationOperationsPage from '@/views/application-operations/index.vue';
import SystemEventsPage from '@/views/system-events/index.vue';
import TasksPage from '@/views/tasks/index.vue';
import SettingsPage from '@/views/settings/index.vue';
import MaintenancePage from '@/views/maintenance/index.vue';
import DebugPage from '@/views/debug/index.vue';
import LoginPage from '@/views/auth/LoginPage.vue';
import NotFoundPage from '@/components/templates/NotFoundPage.vue';
import { setUnauthorizedHandler } from '@/api/client';
import { useSessionStore } from '@/stores/session';

const fail2BanRoute = import.meta.env.DEV
  ? { path: 'resources/fail2ban', component: SecurityPage, meta: { titleKey: 'routes.fail2ban.title' } }
  : { path: 'resources/fail2ban', redirect: (to: RouteLocationGeneric) => ({ path: '/resources/firewall', query: to.query }) };

const redirectToFirewall = (to: RouteLocationGeneric) => ({ path: '/resources/firewall', query: to.query });
const redirectToFail2Ban = (to: RouteLocationGeneric) => ({ path: import.meta.env.DEV ? '/resources/fail2ban' : '/resources/firewall', query: to.query });

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', component: LoginPage, meta: { titleKey: 'routes.login.title', public: true } },
    { path: '/maintenance/backup', component: MaintenancePage, meta: { titleKey: 'routes.maintenance.title', public: true } },
    {
      path: '/',
      component: AppShell,
      children: [
        { path: '', redirect: '/overview' },
        { path: 'overview', component: OverviewPage, meta: { titleKey: 'routes.overview.title' } },
        { path: 'servers', component: ServersPage, meta: { titleKey: 'routes.servers.title' } },
        { path: 'credentials', component: CredentialsPage, meta: { titleKey: 'routes.credentials.title' } },
        { path: 'security', redirect: redirectToFirewall },
        { path: 'security/firewall', redirect: redirectToFirewall },
        { path: 'security/fail2ban', redirect: redirectToFail2Ban },
        { path: 'resources', redirect: '/resources/packages' },
        { path: 'resources/packages', component: ResourcesPage, meta: { titleKey: 'routes.packages.title' } },
        { path: 'resources/containers', component: ResourcesPage, meta: { titleKey: 'routes.containers.title' } },
        { path: 'resources/images', component: ResourcesPage, meta: { titleKey: 'routes.images.title' } },
        { path: 'resources/networks', component: ResourcesPage, meta: { titleKey: 'routes.networks.title' } },
        { path: 'resources/volumes', component: ResourcesPage, meta: { titleKey: 'routes.volumes.title' } },
        { path: 'resources/firewall', component: SecurityPage, meta: { titleKey: 'routes.firewall.title' } },
        fail2BanRoute,
        { path: 'applications', redirect: '/applications/apps' },
        { path: 'applications/apps', component: ApplicationsPage, meta: { titleKey: 'routes.applications.title' } },
        { path: 'applications/apps/create', component: ApplicationsPage, meta: { titleKey: 'routes.applications.title' } },
        { path: 'applications/apps/:applicationId/edit', component: ApplicationsPage, meta: { titleKey: 'routes.applications.title' } },
        { path: 'applications/facility-apps', component: ApplicationsPage, meta: { titleKey: 'routes.facilityApps.title' } },
        { path: 'applications/facility-apps/:facilityKind/config', component: ApplicationsPage, meta: { titleKey: 'routes.facilityApps.title' } },
        { path: 'applications/facility-apps/:facilityKind', component: ApplicationsPage, meta: { titleKey: 'routes.facilityApps.title' } },
        { path: 'dns/domains', component: DnsPage, meta: { titleKey: 'routes.dns.title' } },
        { path: 'certificates/domains', component: CertificatesPage, meta: { titleKey: 'routes.certificates.title' } },
        { path: 'certificates/self-signed', component: CertificatesPage, meta: { titleKey: 'routes.certificates.title' } },
        { path: 'certificates/keys', component: CertificatesPage, meta: { titleKey: 'routes.certificates.title' } },
        { path: 'application-operations', component: ApplicationOperationsPage, meta: { titleKey: 'routes.applicationOperations.title' } },
        { path: 'system-events', component: SystemEventsPage, meta: { titleKey: 'routes.systemEvents.title' } },
        { path: 'tasks', component: TasksPage, meta: { titleKey: 'routes.tasks.title' } },
        { path: 'settings', redirect: '/settings/general' },
        { path: 'settings/general', component: SettingsPage, meta: { titleKey: 'routes.settings.title' } },
        { path: 'settings/security', component: SettingsPage, meta: { titleKey: 'routes.settings.title' } },
        { path: 'settings/certificates', component: SettingsPage, meta: { titleKey: 'routes.settings.title' } },
        { path: 'settings/system-certificates', component: SettingsPage, meta: { titleKey: 'routes.settings.title' } },
        { path: 'settings/system', component: SettingsPage, meta: { titleKey: 'routes.settings.title' } },
        { path: 'settings/backups', component: SettingsPage, meta: { titleKey: 'routes.settings.title' } },
        { path: 'debug', component: DebugPage, meta: { titleKey: 'routes.debug.title' } },
        // Catch-all inside the shell so unknown paths keep the app navigation.
        { path: ':pathMatch(.*)*', component: NotFoundPage, meta: { titleKey: 'routes.notFound.title' } },
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