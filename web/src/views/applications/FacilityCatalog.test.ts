// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import { Wrench } from '@/icons';
import { useI18n } from '@/i18n';
import FacilityCatalog, { type FacilityCatalogItem } from './FacilityCatalog.vue';

const facilities: FacilityCatalogItem[] = [
  {
    kind: 'reverse-proxy',
    icon: Wrench,
    titleKey: 'applicationsPage.entranceProxyFacility',
    descriptionKey: 'applicationsPage.entranceProxyFacilityDescription',
    categoryKey: 'applicationsPage.facilityCategoryTraffic',
    status: 'available',
  },
  {
    kind: 'network-probe',
    icon: Wrench,
    titleKey: 'applicationsPage.networkProbeFacility',
    descriptionKey: 'applicationsPage.networkProbeFacilityDescription',
    categoryKey: 'applicationsPage.facilityCategoryNetwork',
    status: 'degraded',
  },
];

function mountCatalog() {
  useI18n().setLocale('en');
  return mount(FacilityCatalog, {
    props: {
      facilities,
      loading: false,
      pending: '',
      facility: null,
      storageShareConfig: null,
      networkProbeConfig: { mode: 'all', serverIds: [], intervalSeconds: 60, pingCount: 3, pingTimeoutSeconds: 2, version: 0, updatedAt: '' },
      metricsConfig: null,
      serverTotal: 3,
    },
  });
}

describe('FacilityCatalog', () => {
  it('renders one settings-style row per facility with stats and a manage action', () => {
    const { t } = useI18n();
    const wrapper = mountCatalog();

    const rows = wrapper.findAll('article');
    expect(rows).toHaveLength(2);
    // Facilities render as WinUI cards, two per row on desktop.
    expect(wrapper.find('.lg\\:grid-cols-2').exists()).toBe(true);
    expect(rows[0]!.classes()).toContain('surface-card');
    expect(rows[0]!.text()).toContain(t('applicationsPage.entranceProxyFacility'));
    expect(rows[0]!.text()).toContain(t('applicationsPage.facilityCategoryTraffic'));
    expect(rows[1]!.text()).toContain(t('applicationsPage.networkProbeFacility'));
    expect(rows[1]!.text()).toContain(t('applicationsPage.facilityStatus.degraded'));
    expect(rows[1]!.text()).toContain(t('applicationsPage.networkProbeNodes'));
    expect(wrapper.findAll('button').some((button) => button.text().includes(t('applicationsPage.manageFacility')))).toBe(true);
  });

  it('opens the selected facility and only offers reconcile for reconcilable facilities', async () => {
    const { t } = useI18n();
    const wrapper = mountCatalog();
    const rows = wrapper.findAll('article');

    await rows[0]!.find('button').trigger('click');
    expect(wrapper.emitted('open')?.[0]).toEqual(['reverse-proxy']);

    // network probe results are reported by agents; no manual reconcile button.
    expect(rows[1]!.text()).not.toContain(t('applicationsPage.reconcileGateway'));
    expect(rows[0]!.text()).toContain(t('applicationsPage.reconcileGateway'));
  });
});
