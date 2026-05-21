import { createRouter, createWebHistory } from 'vue-router';
import { useAuthStore } from '@/stores/auth';
import AppLayout from '@/layouts/AppLayout.vue';
import LoginPage from '@/features/auth/pages/LoginPage.vue';
import OverviewPage from '@/features/overview/pages/OverviewPage.vue';
import ServersPage from '@/features/servers/pages/ServersPage.vue';
import PackageUpdatesPage from '@/features/packages/pages/PackageUpdatesPage.vue';
import DockerRuntimePage from '@/features/docker/pages/DockerRuntimePage.vue';
import ServicesPage from '@/features/compose/pages/ServicesPage.vue';
import ServiceTemplatesPage from '@/features/compose/pages/ServiceTemplatesPage.vue';
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
        { path: 'packages', name: 'packages', component: PackageUpdatesPage, meta: { title: 'Package Updates' } },
        { path: 'docker', redirect: '/services' },
        { path: 'services', name: 'services', component: ServicesPage, meta: { title: 'Services' } },
        { path: 'runtime-resources', name: 'runtime-resources', component: DockerRuntimePage, meta: { title: 'Runtime Resources' } },
        { path: 'service-templates', name: 'service-templates', component: ServiceTemplatesPage, meta: { title: 'Service Templates' } },
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
