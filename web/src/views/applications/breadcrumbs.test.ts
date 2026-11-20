import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { useI18n } from '@/i18n';
import { applicationDetailTrail, applicationEditorTrail, facilityTrail } from './breadcrumbs';

const { t } = useI18n();

/**
 * applications 面包屑契约（task-54）：
 * Applications/Facilities 根项带链接，末项不带 `to`（BreadcrumbBar 渲染 aria-current=page）；
 * 列表页不加面包屑；编辑器新建/编辑、设施标题按现有 i18n key 生成。
 */
describe('applications breadcrumbs', () => {
  it('builds the application detail trail with a linked root and a current leaf', () => {
    const trail = applicationDetailTrail(t, 'checkout-api');
    expect(trail).toHaveLength(2);
    expect(trail[0]).toEqual({ label: t('routes.applications.title'), to: '/applications/apps' });
    expect(trail[1]).toEqual({ label: 'checkout-api' });
    expect(trail[1]!.to).toBeUndefined();
  });

  it('falls back to the loading label when the detail name is not known yet', () => {
    expect(applicationDetailTrail(t, '')[1]!.label).toBe(t('common.loading'));
  });

  it('labels the editor leaf by create/edit state and keeps the ancestors linked', () => {
    const create = applicationEditorTrail(t, { create: true, name: '' });
    expect(create).toHaveLength(2);
    expect(create[0]).toEqual({ label: t('routes.applications.title'), to: '/applications/apps' });
    expect(create[1]).toEqual({ label: t('applicationsPage.createApplication') });

    const edit = applicationEditorTrail(t, { create: false, name: 'checkout-api' });
    expect(edit).toHaveLength(3);
    expect(edit[0]).toEqual({ label: t('routes.applications.title'), to: '/applications/apps' });
    expect(edit[1]).toEqual({ label: 'checkout-api' });
    expect(edit[2]).toEqual({ label: t('common.edit') });
    expect(edit[2]!.to).toBeUndefined();

    const editUnknown = applicationEditorTrail(t, { create: false, name: '' });
    expect(editUnknown).toHaveLength(2);
    expect(editUnknown[1]).toEqual({ label: t('applicationsPage.applicationEditor') });
  });

  it('builds the facility trail from the facility title, linked to the facilities root', () => {
    const trail = facilityTrail(t, t('applicationsPage.entranceProxyFacility'));
    expect(trail[0]).toEqual({ label: t('routes.facilityApps.title'), to: '/applications/facility-apps' });
    expect(trail[1]).toEqual({ label: t('applicationsPage.entranceProxyFacility') });
    expect(trail).toHaveLength(2);

    const config = facilityTrail(t, t('applicationsPage.entranceProxyFacility'), true);
    expect(config).toHaveLength(3);
    expect(config[2]).toEqual({ label: t('common.configuration') });
    expect(config[2]!.to).toBeUndefined();

    expect(facilityTrail(t, null)[1]!.label).toBe(t('applicationsPage.facilityUnavailable'));
  });
});

describe('applications page wiring', () => {
  const source = readFileSync(resolve(__dirname, 'index.vue'), 'utf8');

  it('passes breadcrumbs only to detail/editor/facility pages, never list pages', () => {
    expect(source.match(/:breadcrumbs="/g) ?? []).toHaveLength(3);
    expect(source).toContain(':breadcrumbs="applicationDetailBreadcrumbs"');
    expect(source).toContain(':breadcrumbs="applicationEditorBreadcrumbs"');
    expect(source).toContain(':breadcrumbs="facilityBreadcrumbs"');
    // 列表与设施目录仍保持无面包屑。
    expect(source).not.toMatch(/<ConsolePage v-if="mode === 'apps'"[^>]*:breadcrumbs/);
    expect(source).not.toMatch(/mode === 'facilityCatalog'[^>]*:breadcrumbs/);
    // back 行为保留。
    expect(source).toContain(':back-label="t(\'common.backToList\')"');
    expect(source).toContain(':back-label="t(\'common.back\')"');
  });
});
