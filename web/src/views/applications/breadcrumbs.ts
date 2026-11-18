import type { RouteLocationRaw } from 'vue-router';

/** 面包屑项：末项不带 `to`，由共享 BreadcrumbBar 渲染 `aria-current="page"`。 */
export interface ApplicationBreadcrumbItem {
  label: string;
  to?: RouteLocationRaw;
}

type Translate = (key: string) => string;

const APPLICATIONS_ROOT: RouteLocationRaw = '/applications/apps';
const FACILITIES_ROOT: RouteLocationRaw = '/applications/facility-apps';

/**
 * 应用详情：Applications → 当前应用名（深度链接加载中回退 Loading）。
 * 列表页本身不加面包屑，只有覆盖式详情/编辑器使用。
 */
export function applicationDetailTrail(t: Translate, name: string): ApplicationBreadcrumbItem[] {
  return [
    { label: t('routes.applications.title'), to: APPLICATIONS_ROOT },
    { label: name || t('common.loading') },
  ];
}

/** 应用编辑器：Applications → 当前应用名 → 编辑；新建时 Applications → 创建应用。 */
export function applicationEditorTrail(t: Translate, options: { create: boolean; name: string }): ApplicationBreadcrumbItem[] {
  if (options.create) {
    return [
      { label: t('routes.applications.title'), to: APPLICATIONS_ROOT },
      { label: t('applicationsPage.createApplication') },
    ];
  }
  if (!options.name) {
    return [
      { label: t('routes.applications.title'), to: APPLICATIONS_ROOT },
      { label: t('applicationsPage.applicationEditor') },
    ];
  }
  return [
    { label: t('routes.applications.title'), to: APPLICATIONS_ROOT },
    { label: options.name },
    { label: t('common.edit') },
  ];
}

/** 设施详情：Facilities → 设施标题；配置页追加“配置”级。 */
export function facilityTrail(t: Translate, facilityTitle: string | null, configuring = false): ApplicationBreadcrumbItem[] {
  const trail: ApplicationBreadcrumbItem[] = [
    { label: t('routes.facilityApps.title'), to: FACILITIES_ROOT },
    { label: facilityTitle || t('applicationsPage.facilityUnavailable') },
  ];
  if (configuring) trail.push({ label: t('common.configuration') });
  return trail;
}
