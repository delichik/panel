import { createRouter, createWebHistory } from 'vue-router';
import { useAuthStore } from '@/stores/auth';
import { useSettingsStore } from '@/stores/settings';
import AppLayout from '@/layouts/AppLayout.vue';
import ChangePasswordPage from '@/features/auth/pages/ChangePasswordPage.vue';
import LoginPage from '@/features/auth/pages/LoginPage.vue';
import OverviewPage from '@/features/overview/pages/OverviewPage.vue';
import ServersPage from '@/features/servers/pages/ServersPage.vue';
import PackageUpdatesPage from '@/features/packages/pages/PackageUpdatesPage.vue';
import FirewallPage from '@/features/firewall/pages/FirewallPage.vue';
import ApplicationsPage from '@/features/applications/pages/ApplicationsPage.vue';
import CertificatesPage from '@/features/certificates/pages/CertificatesPage.vue';
import BuiltinCertificatesPage from '@/features/certificates/pages/BuiltinCertificatesPage.vue';
import KeyAssetsPage from '@/features/certificates/pages/KeyAssetsPage.vue';
import DomainsPage from '@/features/dns/pages/DomainsPage.vue';
import NomadNodesPage from '@/features/nomad/pages/NomadNodesPage.vue';
import NomadSetupPage from '@/features/nomad/pages/NomadSetupPage.vue';
import TaskCenterPage from '@/features/tasks/pages/TaskCenterPage.vue';
import SettingsPage from '@/features/settings/pages/SettingsPage.vue';

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: LoginPage, meta: { public: true } },
    { path: '/change-password', name: 'change-password', component: ChangePasswordPage, meta: { requiresAuth: true, allowPasswordChange: true } },
    {
      path: '/',
      component: AppLayout,
      meta: { requiresAuth: true },
      children: [
        { path: '', redirect: '/overview' },
        { path: 'overview', name: 'overview', component: OverviewPage, meta: { titleKey: 'routes.overview.title' } },
        { path: 'servers', name: 'servers', component: ServersPage, meta: { titleKey: 'routes.servers.title' } },
        { path: 'credentials', name: 'credentials', component: ServersPage, meta: { titleKey: 'routes.credentials.title' } },
        { path: 'servers/firewall', name: 'server-firewall', component: FirewallPage, meta: { titleKey: 'routes.firewall.title' } },
        { path: 'servers/packages', name: 'system-packages', component: PackageUpdatesPage, meta: { titleKey: 'routes.systemPackages.title' } },
        { path: 'packages', redirect: '/servers/packages' },
        { path: 'applications', name: 'applications', component: ApplicationsPage, meta: { titleKey: 'routes.applications.title' } },
        { path: 'dns/domains', name: 'dns-domains', component: DomainsPage, meta: { titleKey: 'routes.domains.title' } },
        { path: 'dns/certificates', redirect: '/certificates/domains' },
        { path: 'certificates', redirect: '/certificates/domains' },
        { path: 'certificates/builtin', name: 'certificates-builtin', component: BuiltinCertificatesPage, meta: { titleKey: 'routes.builtinCertificates.title' } },
        { path: 'certificates/domains', name: 'certificates-domains', component: CertificatesPage, meta: { titleKey: 'routes.certificates.title' } },
        { path: 'certificates/key-assets', name: 'certificates-key-assets', component: KeyAssetsPage, meta: { titleKey: 'routes.keyAssets.title' } },
        { path: 'certificates/self-signed', redirect: '/certificates/key-assets' },
        { path: 'nomad/setup', name: 'nomad-setup', component: NomadSetupPage, meta: { titleKey: 'routes.nomadSetup.title' } },
        { path: 'nomad/nodes', name: 'nomad-nodes', component: NomadNodesPage, meta: { titleKey: 'routes.nomadNodes.title' } },
        { path: 'tasks', name: 'tasks', component: TaskCenterPage, meta: { titleKey: 'routes.tasks.title' } },
        { path: 'settings', redirect: '/settings/general' },
        { path: 'settings/general', name: 'settings-general', component: SettingsPage, meta: { titleKey: 'routes.settingsGeneral.title', settingsCategory: 'general' } },
        { path: 'settings/security', name: 'settings-security', component: SettingsPage, meta: { titleKey: 'routes.settingsSecurity.title', settingsCategory: 'security' } },
        { path: 'settings/nomad', name: 'settings-nomad', component: SettingsPage, meta: { titleKey: 'routes.settingsNomad.title', settingsCategory: 'nomad' } },
        { path: 'settings/certificates', name: 'settings-certificates', component: SettingsPage, meta: { titleKey: 'routes.settingsCertificates.title', settingsCategory: 'certificates' } },
        { path: 'settings/system', name: 'settings-system', component: SettingsPage, meta: { titleKey: 'routes.settingsSystem.title', settingsCategory: 'system' } },
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
  if (auth.authenticated && !auth.passwordChangeRequired) {
    try {
      await settings.loadRuntime();
    } catch {
      // The settings page surfaces runtime load errors explicitly.
    }
  }
  if (to.meta.public && auth.authenticated) {
    return auth.passwordChangeRequired ? '/change-password' : '/overview';
  }
  if (!to.meta.public && !auth.authenticated) {
    return { path: '/login', query: { redirect: to.fullPath } };
  }
  if (auth.authenticated && auth.passwordChangeRequired && !to.meta.allowPasswordChange) {
    return { path: '/change-password', query: { redirect: to.fullPath } };
  }
  if (to.name === 'change-password' && auth.authenticated && !auth.passwordChangeRequired) {
    return '/overview';
  }
  return true;
});
