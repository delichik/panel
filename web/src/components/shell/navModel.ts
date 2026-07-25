import {
  AppWindow,
  CircleGauge,
  Database,
  Globe,
  KeyRound,
  ListTodo,
  Server,
  Settings,
  Shield,
  ShieldCheck,
} from '@lucide/vue';
import type { Component } from 'vue';

export interface NavItem {
  key: string;
  titleKey: string;
  to: string;
  icon: Component;
}

export interface NavGroup {
  key: string;
  titleKey: string;
  items: NavItem[];
}

export const navGroups: NavGroup[] = [
  { key: 'home', titleKey: 'layout.nav.overview', items: [{ key: 'overview', titleKey: 'layout.nav.overview', to: '/overview', icon: CircleGauge }] },
  {
    key: 'infrastructure',
    titleKey: 'layout.nav.servers',
    items: [
      { key: 'servers', titleKey: 'routes.servers.title', to: '/servers', icon: Server },
      { key: 'credentials', titleKey: 'routes.credentials.title', to: '/credentials', icon: KeyRound },
      { key: 'security', titleKey: 'routes.security.title', to: '/security/firewall', icon: Shield },
    ],
  },
  {
    key: 'resources',
    titleKey: 'layout.nav.resources',
    items: [{ key: 'resources', titleKey: 'routes.resources.title', to: '/resources/packages', icon: Database }],
  },
  {
    key: 'delivery',
    titleKey: 'layout.nav.applications',
    items: [
      { key: 'applications', titleKey: 'routes.applications.title', to: '/applications/apps', icon: AppWindow },
      { key: 'dns', titleKey: 'routes.dns.title', to: '/dns/domains', icon: Globe },
      { key: 'certificates', titleKey: 'routes.certificates.title', to: '/certificates/domains', icon: ShieldCheck },
    ],
  },
  {
    key: 'system',
    titleKey: 'layout.nav.settings',
    items: [
      { key: 'tasks', titleKey: 'routes.tasks.title', to: '/tasks', icon: ListTodo },
      { key: 'settings', titleKey: 'routes.settings.title', to: '/settings/general', icon: Settings },
      { key: 'debug', titleKey: 'routes.debug.title', to: '/debug', icon: Database },
    ],
  },
];

export function activeNavKey(path: string) {
  const normalizedPath = path === '/' ? '/overview' : path;
  return navGroups.flatMap((group) => group.items).find((item) => {
    const familyPath = `/${item.key}`;
    return normalizedPath === item.to || normalizedPath.startsWith(`${item.to}/`) || normalizedPath === familyPath || normalizedPath.startsWith(`${familyPath}/`);
  })?.key;
}
