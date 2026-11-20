/**
 * Shared dynamic-import factories for route pages. Keeping them in one place
 * lets the router, shell prefetch, and nav hover warm the same module promise.
 */
export const pageLoaders = {
  login: () => import('@/views/auth/LoginPage.vue'),
  maintenance: () => import('@/views/maintenance/index.vue'),
  appShell: () => import('@/components/shell/AppShell.vue'),
  overview: () => import('@/views/overview/index.vue'),
  servers: () => import('@/views/servers/index.vue'),
  credentials: () => import('@/views/credentials/index.vue'),
  applications: () => import('@/views/applications/index.vue'),
  dns: () => import('@/views/dns/index.vue'),
  certificates: () => import('@/views/certificates/index.vue'),
  events: () => import('@/views/events/index.vue'),
  logs: () => import('@/views/logs/index.vue'),
  settings: () => import('@/views/settings/index.vue'),
  debug: () => import('@/views/debug/index.vue'),
  notFound: () => import('@/components/templates/NotFoundPage.vue'),
} as const;

export type PageLoaderKey = keyof typeof pageLoaders;

/** Prefetch every authenticated console page after the shell is up. */
export function prefetchShellPages(): void {
  const keys: PageLoaderKey[] = [
    'overview',
    'servers',
    'credentials',
    'applications',
    'dns',
    'certificates',
    'events',
    'logs',
    'settings',
    'debug',
    'notFound',
  ];
  for (const key of keys) {
    void pageLoaders[key]();
  }
}

/** Map a nav/target path to its page loader for hover/focus warm-up. */
export function pageLoaderForPath(path: string): (() => Promise<unknown>) | undefined {
  const normalized = path.split('?')[0] || '';
  if (normalized.startsWith('/overview')) return pageLoaders.overview;
  if (normalized.startsWith('/servers')) return pageLoaders.servers;
  if (normalized.startsWith('/credentials')) return pageLoaders.credentials;
  if (normalized.startsWith('/applications')) return pageLoaders.applications;
  if (normalized.startsWith('/dns')) return pageLoaders.dns;
  if (normalized.startsWith('/certificates')) return pageLoaders.certificates;
  if (normalized.startsWith('/events') || normalized.startsWith('/operations') || normalized.startsWith('/tasks') || normalized.startsWith('/activity')) {
    return pageLoaders.events;
  }
  if (normalized.startsWith('/logs')) return pageLoaders.logs;
  if (normalized.startsWith('/settings')) return pageLoaders.settings;
  if (normalized.startsWith('/debug')) return pageLoaders.debug;
  if (normalized.startsWith('/login')) return pageLoaders.login;
  if (normalized.startsWith('/maintenance')) return pageLoaders.maintenance;
  return undefined;
}
