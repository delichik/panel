// @vitest-environment jsdom
import { enableAutoUnmount, mount } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { useI18n } from '@/i18n';
import type { Diagnostic } from '@/types/applications';
import ApplicationDiagnosticsList from './ApplicationDiagnosticsList.vue';

enableAutoUnmount(afterEach);

beforeEach(() => {
  useI18n().setLocale('en');
});

function diagnostic(overrides: Partial<Diagnostic> = {}): Diagnostic {
  return { code: 'asset_missing', severity: 'warning', message: 'Referenced static asset is missing.', ...overrides };
}

describe('ApplicationDiagnosticsList', () => {
  it('shows the default empty hint when there are no diagnostics', () => {
    const wrapper = mount(ApplicationDiagnosticsList, { props: { items: [] } });
    expect(wrapper.text()).toContain(useI18n().t('applicationsPage.noDiagnostics'));
    expect(wrapper.findAll('[role="alert"]')).toHaveLength(0);
  });

  it('accepts a custom empty hint', () => {
    const wrapper = mount(ApplicationDiagnosticsList, { props: { items: [], emptyText: 'Nothing to fix' } });
    expect(wrapper.text()).toContain('Nothing to fix');
  });

  it('renders one semantic InfoBar per diagnostic with its detail context', () => {
    const wrapper = mount(ApplicationDiagnosticsList, {
      props: {
        items: [
          diagnostic({ code: 'route_asset_missing', field: 'reverseProxy[0].paths[0]', details: { domain: 'app.example.test', path: '/assets', assetName: 'logo.svg', serverId: 'srv-1' } }),
          diagnostic({ code: 'image_pull_failed', severity: 'error', message: 'Image pull failed.' }),
        ],
      },
    });

    const { t } = useI18n();
    const bars = wrapper.findAll('[role]');
    expect(bars).toHaveLength(2);
    // warning 与 error 都是需要立刻注意的状态提示条。
    expect(bars[0]!.attributes('role')).toBe('alert');
    expect(bars[0]!.classes()).toContain('bg-warning-bg');
    expect(bars[1]!.classes()).toContain('bg-danger-bg');

    const text = wrapper.text();
    expect(text).toContain('reverseProxy[0].paths[0]');
    expect(text).toContain('app.example.test');
    expect(text).toContain('/assets');
    expect(text).toContain(t('applicationsPage.assetReferenceName'));
    expect(text).toContain('Image pull failed.');
  });
});
