import { createRouter, createWebHistory } from 'vue-router';
import { useAuthStore } from '@/stores/auth';
import { useSettingsStore } from '@/stores/settings';
import AppLayout from '@/layouts/AppLayout.vue';
import ChangePasswordPage from '@/views/auth/change-password/index.vue';
import LoginPage from '@/views/auth/login/index.vue';
import OverviewPage from '@/views/overview/index.vue';
import ServerNodePage from '@/views/servers/node/index.vue';
import ServerCredentialsPage from '@/views/servers/credentials/index.vue';
import PackageUpdatesPage from '@/views/servers/packages/index.vue';
import FirewallPage from '@/views/servers/firewall/index.vue';
import ApplicationsPage from '@/views/runtime/applications/index.vue';
import FacilityAppsPage from '@/views/containerization/facility-apps/index.vue';
import ContainersPage from '@/views/containerization/containers/index.vue';
import ImagesPage from '@/views/containerization/images/index.vue';
import NetworksPage from '@/views/containerization/networks/index.vue';
import VolumesPage from '@/views/containerization/volumes/index.vue';
import CertificatesPage from '@/views/certificates/domains/index.vue';
import SelfSignedCertificatesPage from '@/views/certificates/self-signed/index.vue';
import KeysPage from '@/views/certificates/keys/index.vue';
import DomainsPage from '@/views/dns/domains/index.vue';
import TaskCenterPage from '@/views/tasks/index.vue';
import SettingsGeneralPage from '@/views/settings/general/index.vue';
import SettingsSecurityPage from '@/views/settings/security/index.vue';
import SettingsCertificatesPage from '@/views/settings/certificates/index.vue';
import SettingsSystemPage from '@/views/settings/system/index.vue';
import SettingsSystemCertificatesPage from '@/views/settings/system-certificates/index.vue';
import DebugPage from '@/views/debug/index.vue';

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
        { path: 'servers', name: 'servers', component: ServerNodePage, meta: { titleKey: 'routes.servers.title' } },
        { path: 'credentials', name: 'credentials', component: ServerCredentialsPage, meta: { titleKey: 'routes.credentials.title' } },
        { path: 'servers/firewall', name: 'server-firewall', component: FirewallPage, meta: { titleKey: 'routes.firewall.title' } },
        { path: 'servers/packages', name: 'system-packages', component: PackageUpdatesPage, meta: { titleKey: 'routes.systemPackages.title' } },
        { path: 'containerization/applications', name: 'applications', component: ApplicationsPage, meta: { titleKey: 'routes.applications.title' } },
        { path: 'containerization/facility-apps', name: 'facility-apps', component: FacilityAppsPage, meta: { titleKey: 'routes.facilityApps.title' } },
        { path: 'containerization/containers', name: 'containers', component: ContainersPage, meta: { titleKey: 'routes.containers.title' } },
        { path: 'containerization/images', name: 'images', component: ImagesPage, meta: { titleKey: 'routes.images.title' } },
        { path: 'containerization/networks', name: 'networks', component: NetworksPage, meta: { titleKey: 'routes.networks.title' } },
        { path: 'containerization/volumes', name: 'volumes', component: VolumesPage, meta: { titleKey: 'routes.volumes.title' } },
        { path: 'dns/domains', name: 'dns-domains', component: DomainsPage, meta: { titleKey: 'routes.domains.title' } },
        { path: 'certificates', redirect: '/certificates/domains' },
        { path: 'certificates/domains', name: 'certificates-domains', component: CertificatesPage, meta: { titleKey: 'routes.certificates.title' } },
        { path: 'certificates/self-signed', name: 'certificates-self-signed', component: SelfSignedCertificatesPage, meta: { titleKey: 'routes.selfSignedCertificates.title' } },
        { path: 'certificates/keys', name: 'certificates-keys', component: KeysPage, meta: { titleKey: 'routes.keys.title' } },
        { path: 'tasks', name: 'tasks', component: TaskCenterPage, meta: { titleKey: 'routes.tasks.title' } },
        { path: 'settings', redirect: '/settings/general' },
        { path: 'settings/general', name: 'settings-general', component: SettingsGeneralPage, meta: { titleKey: 'routes.settingsGeneral.title', settingsCategory: 'general' } },
        { path: 'settings/security', name: 'settings-security', component: SettingsSecurityPage, meta: { titleKey: 'routes.settingsSecurity.title', settingsCategory: 'security' } },
        { path: 'settings/certificates', name: 'settings-certificates', component: SettingsCertificatesPage, meta: { titleKey: 'routes.settingsCertificates.title', settingsCategory: 'certificates' } },
        { path: 'settings/system-certificates', name: 'settings-system-certificates', component: SettingsSystemCertificatesPage, meta: { titleKey: 'routes.settingsSystemCertificates.title' } },
        { path: 'settings/system', name: 'settings-system', component: SettingsSystemPage, meta: { titleKey: 'routes.settingsSystem.title', settingsCategory: 'system' } },
        { path: 'debug', name: 'debug', component: DebugPage, meta: { titleKey: 'routes.debug.title' } },
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
