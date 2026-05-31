import { createRouter, createWebHistory } from 'vue-router';
import { useAuthStore } from '@/stores/auth';
import { useSettingsStore } from '@/stores/settings';
import AppLayout from '@/layouts/AppLayout.vue';
import LoginPage from '@/features/auth/pages/LoginPage.vue';
import OverviewPage from '@/features/overview/pages/OverviewPage.vue';
import ServersPage from '@/features/servers/pages/ServersPage.vue';
import PackageUpdatesPage from '@/features/packages/pages/PackageUpdatesPage.vue';
import ApplicationsPage from '@/features/applications/pages/ApplicationsPage.vue';
import CertificatesPage from '@/features/certificates/pages/CertificatesPage.vue';
import DomainsPage from '@/features/dns/pages/DomainsPage.vue';
import NomadNodesPage from '@/features/nomad/pages/NomadNodesPage.vue';
import NomadSetupPage from '@/features/nomad/pages/NomadSetupPage.vue';
import TaskCenterPage from '@/features/tasks/pages/TaskCenterPage.vue';
import SettingsPage from '@/features/settings/pages/SettingsPage.vue';

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: LoginPage, meta: { public: true } },
    {
      path: '/',
      component: AppLayout,
      meta: { requiresAuth: true },
      children: [
        { path: '', redirect: '/overview' },
        { path: 'overview', name: 'overview', component: OverviewPage, meta: { eyebrowKey: 'routes.overview.eyebrow', titleKey: 'routes.overview.title', subtitleKey: 'routes.overview.subtitle' } },
        { path: 'servers', name: 'servers', component: ServersPage, meta: { eyebrowKey: 'routes.servers.eyebrow', titleKey: 'routes.servers.title', subtitleKey: 'routes.servers.subtitle' } },
        { path: 'credentials', name: 'credentials', component: ServersPage, meta: { eyebrowKey: 'routes.credentials.eyebrow', titleKey: 'routes.credentials.title', subtitleKey: 'routes.credentials.subtitle' } },
        { path: 'servers/packages', name: 'system-packages', component: PackageUpdatesPage, meta: { eyebrowKey: 'routes.systemPackages.eyebrow', titleKey: 'routes.systemPackages.title', subtitleKey: 'routes.systemPackages.subtitle' } },
        { path: 'packages', redirect: '/servers/packages' },
        { path: 'applications', name: 'applications', component: ApplicationsPage, meta: { eyebrowKey: 'routes.applications.eyebrow', titleKey: 'routes.applications.title', subtitleKey: 'routes.applications.subtitle' } },
        { path: 'dns/domains', name: 'dns-domains', component: DomainsPage, meta: { eyebrowKey: 'routes.domains.eyebrow', titleKey: 'routes.domains.title', subtitleKey: 'routes.domains.subtitle' } },
        { path: 'dns/certificates', name: 'dns-certificates', component: CertificatesPage, meta: { eyebrowKey: 'routes.certificates.eyebrow', titleKey: 'routes.certificates.title', subtitleKey: 'routes.certificates.subtitle' } },
        { path: 'certificates', redirect: '/dns/certificates' },
        { path: 'nomad/setup', name: 'nomad-setup', component: NomadSetupPage, meta: { eyebrowKey: 'routes.nomadSetup.eyebrow', titleKey: 'routes.nomadSetup.title', subtitleKey: 'routes.nomadSetup.subtitle' } },
        { path: 'nomad/nodes', name: 'nomad-nodes', component: NomadNodesPage, meta: { eyebrowKey: 'routes.nomadNodes.eyebrow', titleKey: 'routes.nomadNodes.title', subtitleKey: 'routes.nomadNodes.subtitle' } },
        { path: 'nomad/jobs', redirect: '/tasks' },
        { path: 'deployments', redirect: '/tasks' },
        { path: 'tasks', name: 'tasks', component: TaskCenterPage, meta: { eyebrowKey: 'routes.tasks.eyebrow', titleKey: 'routes.tasks.title', subtitleKey: 'routes.tasks.subtitle' } },
        { path: 'settings', name: 'settings', component: SettingsPage, meta: { eyebrowKey: 'routes.settings.eyebrow', titleKey: 'routes.settings.title', subtitleKey: 'routes.settings.subtitle' } },
      ],
    },
  ],
});

router.beforeEach(async (to) => {
  const auth = useAuthStore();
  const settings = useSettingsStore();
  if (!auth.checked) {
    await auth.restoreSession();
  }
  if (auth.authenticated) {
    try {
      await settings.loadRuntime();
    } catch {
      // The settings page surfaces runtime load errors explicitly.
    }
  }
  if (to.meta.public && auth.authenticated) {
    return '/overview';
  }
  if (!to.meta.public && !auth.authenticated) {
    return { path: '/login', query: { redirect: to.fullPath } };
  }
  return true;
});
