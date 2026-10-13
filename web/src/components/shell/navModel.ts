import {
  AppWindow,
  CircleGauge,
  FolderKanban,
  Globe,
  KeyRound,
  RadioTower,
  Server,
  Settings,
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
    key: 'assets',
    titleKey: 'layout.nav.assets',
    items: [
      { key: 'servers', titleKey: 'routes.servers.title', to: '/servers', icon: Server },
      { key: 'credentials', titleKey: 'routes.credentials.title', to: '/credentials', icon: KeyRound },
      { key: 'certificates', titleKey: 'routes.certificates.title', to: '/certificates/domains', icon: ShieldCheck },
      { key: 'dns', titleKey: 'routes.dns.title', to: '/dns/domains', icon: Globe },
    ],
  },
  {
    key: 'delivery',
    titleKey: 'layout.nav.applications',
    items: [
      { key: 'applications', titleKey: 'routes.applications.title', to: '/applications/apps', icon: AppWindow },
      { key: 'facility-apps', titleKey: 'routes.facilityApps.title', to: '/applications/facility-apps', icon: FolderKanban },
    ],
  },
  {
    key: 'system',
    titleKey: 'layout.nav.settings',
    items: [
      { key: 'activity', titleKey: 'routes.activity.title', to: '/activity', icon: RadioTower },
      { key: 'settings', titleKey: 'routes.settings.title', to: '/settings/general', icon: Settings },
    ],
  },
];

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
