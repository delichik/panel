import {
  AppWindow,
  CircleGauge,
  FileText,
  FolderKanban,
  Globe,
  History,
  KeyRound,
  Server,
  Settings,
  ShieldCheck,
  Users,
} from '@/icons';
import type { Component } from 'vue';

export interface NavItem {
  key: string;
  titleKey: string;
  to: string;
  icon: Component;
  permission?: string;
}

export interface NavGroup {
  key: string;
  titleKey: string;
  items: NavItem[];
}

export const navGroups: NavGroup[] = [
  { key: 'home', titleKey: 'layout.nav.overview', items: [{ key: 'overview', titleKey: 'layout.nav.overview', to: '/overview', icon: CircleGauge, permission: 'overview.view' }] },
  {
    key: 'assets',
    titleKey: 'layout.nav.assets',
    items: [
      { key: 'servers', titleKey: 'routes.servers.title', to: '/servers', icon: Server, permission: 'servers.view' },
      { key: 'credentials', titleKey: 'routes.credentials.title', to: '/credentials', icon: KeyRound, permission: 'servers.credentials.manage' },
      { key: 'certificates', titleKey: 'routes.certificates.title', to: '/certificates/domains', icon: ShieldCheck, permission: 'certificates.view' },
      { key: 'dns', titleKey: 'routes.dns.title', to: '/dns/domains', icon: Globe, permission: 'dns.view' },
    ],
  },
  {
    key: 'delivery',
    titleKey: 'layout.nav.applications',
    items: [
      { key: 'applications', titleKey: 'routes.applications.title', to: '/applications/apps', icon: AppWindow, permission: 'applications.view' },
      { key: 'facility-apps', titleKey: 'routes.facilityApps.title', to: '/applications/facility-apps', icon: FolderKanban, permission: 'facilities.view' },
    ],
  },
  {
    key: 'system',
    titleKey: 'layout.nav.settings',
    items: [
      { key: 'events', titleKey: 'routes.events.title', to: '/events', icon: History, permission: 'events.view' },
      { key: 'logs', titleKey: 'routes.logs.title', to: '/logs', icon: FileText, permission: 'events.view' },
      { key: 'access', titleKey: 'routes.access.title', to: '/access/users', icon: Users, permission: 'access.users.manage' },
      { key: 'settings', titleKey: 'routes.settings.title', to: '/settings/general', icon: Settings, permission: 'settings.view' },
    ],
  },
];

/** Filter navigation to permissions the current session actually holds. */
export function visibleNavGroups(can: (permission: string) => boolean): NavGroup[] {
  return navGroups
    .map((group) => ({ ...group, items: group.items.filter((item) => !item.permission || can(item.permission)) }))
    .filter((group) => group.items.length > 0);
}

export function activeNavKey(path: string) {
  const normalizedPath = path === '/' ? '/overview' : path;
  const items = navGroups.flatMap((group) => group.items);
  const directMatch = [...items].sort((left, right) => right.to.length - left.to.length).find((item) => {
    return normalizedPath === item.to || normalizedPath.startsWith(`${item.to}/`);
  });
  if (directMatch) return directMatch.key;
  return items.find((item) => {
    const familyPath = `/${item.key}`;
    return normalizedPath === familyPath || normalizedPath.startsWith(`${familyPath}/`);
  })?.key;
}
