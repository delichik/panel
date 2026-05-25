import { createRouter, createWebHistory } from 'vue-router';
import { useAuthStore } from '@/stores/auth';
import AppLayout from '@/layouts/AppLayout.vue';
import LoginPage from '@/features/auth/pages/LoginPage.vue';
import OverviewPage from '@/features/overview/pages/OverviewPage.vue';
import ServersPage from '@/features/servers/pages/ServersPage.vue';
import PackageUpdatesPage from '@/features/packages/pages/PackageUpdatesPage.vue';
import ApplicationsPage from '@/features/applications/pages/ApplicationsPage.vue';
import NomadNodesPage from '@/features/nomad/pages/NomadNodesPage.vue';
import NomadSetupPage from '@/features/nomad/pages/NomadSetupPage.vue';
import NomadJobsPage from '@/features/nomad/pages/NomadJobsPage.vue';
import DeploymentsPage from '@/features/deployments/pages/DeploymentsPage.vue';
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
        { path: 'overview', name: 'overview', component: OverviewPage, meta: { title: 'Overview' } },
        { path: 'servers', name: 'servers', component: ServersPage, meta: { title: 'Servers' } },
        { path: 'credentials', name: 'credentials', component: ServersPage, meta: { title: 'Credentials' } },
        { path: 'packages', name: 'packages', component: PackageUpdatesPage, meta: { title: 'Package Updates' } },
        { path: 'applications', name: 'applications', component: ApplicationsPage, meta: { title: 'Applications' } },
        { path: 'nomad/setup', name: 'nomad-setup', component: NomadSetupPage, meta: { title: 'Nomad Setup' } },
        { path: 'nomad/nodes', name: 'nomad-nodes', component: NomadNodesPage, meta: { title: 'Nomad Nodes' } },
        { path: 'nomad/jobs', name: 'nomad-jobs', component: NomadJobsPage, meta: { title: 'Nomad Jobs' } },
        { path: 'deployments', name: 'deployments', component: DeploymentsPage, meta: { title: 'Deployments' } },
        { path: 'tasks', name: 'tasks', component: TaskCenterPage, meta: { title: 'Task Center' } },
        { path: 'settings', name: 'settings', component: SettingsPage, meta: { title: 'Settings' } },
      ],
    },
  ],
});

router.beforeEach(async (to) => {
  const auth = useAuthStore();
  if (!auth.checked) {
    await auth.restoreSession();
  }
  if (to.meta.public && auth.authenticated) {
    return '/overview';
  }
  if (!to.meta.public && !auth.authenticated) {
    return { path: '/login', query: { redirect: to.fullPath } };
  }
  return true;
});
