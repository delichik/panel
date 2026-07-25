import { createRouter, createWebHistory } from 'vue-router';
import AppShell from '@/components/shell/AppShell.vue';
import OverviewPage from '@/views/overview/index.vue';
import ServersPage from '@/views/servers/index.vue';
import CredentialsPage from '@/views/credentials/index.vue';
import SecurityPage from '@/views/security/index.vue';
import ResourcesPage from '@/views/resources/index.vue';
import ApplicationsPage from '@/views/applications/index.vue';
import DnsPage from '@/views/dns/index.vue';
import CertificatesPage from '@/views/certificates/index.vue';
import TasksPage from '@/views/tasks/index.vue';
import SettingsPage from '@/views/settings/index.vue';
import MaintenancePage from '@/views/maintenance/index.vue';
import DebugPage from '@/views/debug/index.vue';
import LoginPage from '@/views/auth/LoginPage.vue';
import { useSessionStore } from '@/stores/session';

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
        { path: 'security', redirect: '/security/firewall' },
        { path: 'security/firewall', component: SecurityPage, meta: { titleKey: 'routes.security.title' } },
        { path: 'security/fail2ban', component: SecurityPage, meta: { titleKey: 'routes.security.title' } },
        { path: 'resources', redirect: '/resources/packages' },
        { path: 'resources/packages', component: ResourcesPage, meta: { titleKey: 'routes.resources.title' } },
        { path: 'resources/containers', component: ResourcesPage, meta: { titleKey: 'routes.resources.title' } },
        { path: 'resources/images', component: ResourcesPage, meta: { titleKey: 'routes.resources.title' } },
        { path: 'resources/networks', component: ResourcesPage, meta: { titleKey: 'routes.resources.title' } },
        { path: 'resources/volumes', component: ResourcesPage, meta: { titleKey: 'routes.resources.title' } },
        { path: 'applications', redirect: '/applications/apps' },
        { path: 'applications/apps', component: ApplicationsPage, meta: { titleKey: 'routes.applications.title' } },
        { path: 'applications/apps/create', component: ApplicationsPage, meta: { titleKey: 'routes.applications.title' } },
        { path: 'applications/apps/:applicationId/edit', component: ApplicationsPage, meta: { titleKey: 'routes.applications.title' } },
        { path: 'applications/facility-apps', component: ApplicationsPage, meta: { titleKey: 'routes.applications.title' } },
        { path: 'applications/facility-apps/:facilityKind/config', component: ApplicationsPage, meta: { titleKey: 'routes.applications.title' } },
        { path: 'applications/facility-apps/:facilityKind', component: ApplicationsPage, meta: { titleKey: 'routes.applications.title' } },
        { path: 'dns/domains', component: DnsPage, meta: { titleKey: 'routes.dns.title' } },
        { path: 'certificates/domains', component: CertificatesPage, meta: { titleKey: 'routes.certificates.title' } },
        { path: 'certificates/self-signed', component: CertificatesPage, meta: { titleKey: 'routes.certificates.title' } },
        { path: 'certificates/keys', component: CertificatesPage, meta: { titleKey: 'routes.certificates.title' } },
        { path: 'tasks', component: TasksPage, meta: { titleKey: 'routes.tasks.title' } },
        { path: 'settings', redirect: '/settings/general' },
        { path: 'settings/general', component: SettingsPage, meta: { titleKey: 'routes.settings.title' } },
        { path: 'settings/security', component: SettingsPage, meta: { titleKey: 'routes.settings.title' } },
        { path: 'settings/certificates', component: SettingsPage, meta: { titleKey: 'routes.settings.title' } },
        { path: 'settings/system-certificates', component: SettingsPage, meta: { titleKey: 'routes.settings.title' } },
        { path: 'settings/system', component: SettingsPage, meta: { titleKey: 'routes.settings.title' } },
        { path: 'settings/backups', component: SettingsPage, meta: { titleKey: 'routes.settings.title' } },
        { path: 'debug', component: DebugPage, meta: { titleKey: 'routes.debug.title' } },
      ],
    },
    { path: '/:pathMatch(.*)*', redirect: '/overview' },
  ],
});

router.beforeEach(async (to) => {
  const session = useSessionStore();
  if (!session.ready) await session.restore();
  if (to.meta.public) {
    if (to.path === '/login' && session.authenticated && !session.passwordChangeRequired) return '/overview';
    return true;
  }
  if (!session.authenticated) return { path: '/login', query: { redirect: to.fullPath } };
  if (session.passwordChangeRequired) return { path: '/login', query: { redirect: to.fullPath } };
  return true;
});
