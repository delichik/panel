// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import { Wrench } from '@/icons';
import Badge from '@/components/ui/Badge.vue';
import StatusBadge from '@/components/ui/StatusBadge.vue';
import { useI18n } from '@/i18n';
import FacilityCatalog, { type FacilityCatalogItem } from './FacilityCatalog.vue';

const facilities: FacilityCatalogItem[] = [
  {
    kind: 'reverse-proxy',
    icon: Wrench,
    titleKey: 'applicationsPage.entranceProxyFacility',
    descriptionKey: 'applicationsPage.entranceProxyFacilityDescription',
  },
  {
    kind: 'storage-share',
    icon: Wrench,
    titleKey: 'applicationsPage.storageShareFacility',
    descriptionKey: 'applicationsPage.storageShareFacilityDescription',
  },
  {
    kind: 'network-probe',
    icon: Wrench,
    titleKey: 'applicationsPage.networkProbeFacility',
    descriptionKey: 'applicationsPage.networkProbeFacilityDescription',
  },
  {
    kind: 'metrics',
    icon: Wrench,
    titleKey: 'applicationsPage.metricsFacility',
    descriptionKey: 'applicationsPage.metricsFacilityDescription',
  },
];

function mountCatalog() {
  useI18n().setLocale('en');
  return mount(FacilityCatalog, {
    props: {
      facilities,
      loading: false,
      facility: null,
      storageShareConfig: null,
      networkProbeConfig: { mode: 'all', serverIds: [], intervalSeconds: 60, pingCount: 3, pingTimeoutSeconds: 2, thresholds: {}, version: 0, updatedAt: '' },
      metricsConfig: { mode: 'selected', serverIds: ['s1'], retentionDays: 14, collectionIntervalSeconds: 60, cleanupSchedule: 'daily', thresholds: {}, version: 1, updatedAt: '' },
      serverTotal: 3,
    },
  });
}

describe('FacilityCatalog', () => {
  it('renders clean WinUI facility cards with stats, manage actions, and no category/status/reconcile chrome', () => {
    const { t } = useI18n();
    const wrapper = mountCatalog();

    const cards = wrapper.findAll('article');
    expect(cards).toHaveLength(4);
    // Facilities render as WinUI cards, two per row on desktop.
    expect(wrapper.find('.lg\\:grid-cols-2').exists()).toBe(true);
    expect(cards[0]!.classes()).toContain('surface-card');
    expect(cards[0]!.text()).toContain(t('applicationsPage.entranceProxyFacility'));
    expect(cards[0]!.text()).toContain(t('applicationsPage.entranceProxyFacilityDescription'));
    expect(cards[2]!.text()).toContain(t('applicationsPage.networkProbeFacility'));
    expect(cards[2]!.text()).toContain(t('applicationsPage.networkProbeNodes'));
    expect(cards[3]!.text()).toContain(t('applicationsPage.metricsFacility'));
    expect(cards[3]!.text()).toContain(t('applicationsPage.metricsRetention'));

    // Category ("xx设施"), availability ("可用"), and catalog-level reconcile buttons are removed.
    expect(wrapper.findComponent(Badge).exists()).toBe(false);
    expect(wrapper.findComponent(StatusBadge).exists()).toBe(false);
    expect(wrapper.findAll('[data-testid="facility-reconcile"]')).toHaveLength(0);
    expect(cards.map((card) => card.find('header').text())).toEqual(
      facilities.map((item) => `${t(item.titleKey)}${t(item.descriptionKey)}`),
    );

    const fullText = wrapper.text();
    for (const removedLabel of [
      t('applicationsPage.facilityCategoryTraffic'),
      t('applicationsPage.facilityCategoryNetwork'),
      t('applicationsPage.facilityCategoryObservability'),
      t('applicationsPage.facilityStatus.available'),
      t('applicationsPage.facilityStatus.degraded'),
      t('applicationsPage.facilityStatus.unavailable'),
      t('applicationsPage.reconcileGateway'),
      t('applicationsPage.storageShareReconcile'),
    ]) {
      expect(fullText).not.toContain(removedLabel);
    }

    expect(wrapper.findAll('[data-testid="facility-open"]')).toHaveLength(4);
  });

  it('opens the selected facility when clicking manage', async () => {
    const wrapper = mountCatalog();
    const cards = wrapper.findAll('article');

    await cards[0]!.find('[data-testid="facility-open"]').trigger('click');
    await cards[1]!.find('[data-testid="facility-open"]').trigger('click');
    expect(wrapper.emitted('open')).toEqual([['reverse-proxy'], ['storage-share']]);
  });
});
