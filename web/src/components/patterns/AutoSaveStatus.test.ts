// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import AutoSaveStatus from './AutoSaveStatus.vue';
import type { AutoSaveState } from '@/composables/useDebouncedAutoSave';

const labels = {
  saving: 'Saving…',
  saved: 'All changes saved',
  failed: 'Save failed',
  invalid: 'Fix the highlighted fields to save',
  retry: 'Retry',
};

function render(status: AutoSaveState) {
  return mount(AutoSaveStatus, { props: { status, labels } });
}

describe('AutoSaveStatus', () => {
  it('renders nothing while idle', () => {
    const view = render('idle');
    expect(view.find('[data-autosave-status]').exists()).toBe(false);
    expect(view.text()).toBe('');
  });

  it('shows a progress ring and the saving copy for pending and saving', () => {
    for (const status of ['pending', 'saving'] as const) {
      const view = render(status);
      const indicator = view.find('[data-autosave-status]');
      expect(indicator.attributes('data-autosave-state')).toBe(status);
      expect(indicator.text()).toContain(labels.saving);
      expect(indicator.find('svg.winui-progress-ring').exists()).toBe(true);
      expect(indicator.attributes('role')).toBe('status');
    }
  });

  it('shows the success tone once every change is saved', () => {
    const view = render('saved');
    const indicator = view.find('[data-autosave-status]');
    expect(indicator.text()).toContain(labels.saved);
    expect(indicator.text()).not.toContain(labels.retry);
    expect(indicator.find('svg').classes()).toContain('text-success');
  });

  it('shows the danger tone for invalid input without offering a retry', () => {
    const view = render('invalid');
    const indicator = view.find('[data-autosave-status]');
    expect(indicator.text()).toContain(labels.invalid);
    expect(indicator.find('button').exists()).toBe(false);
    expect(indicator.find('svg').classes()).toContain('text-danger');
  });

  it('shows the failure copy plus a retry button and emits retry', async () => {
    const view = render('error');
    const indicator = view.find('[data-autosave-status]');
    expect(indicator.text()).toContain(labels.failed);
    expect(indicator.attributes('role')).toBe('alert');

    const retry = indicator.find('[data-autosave-retry]');
    expect(retry.text()).toBe(labels.retry);
    await retry.trigger('click');
    expect(view.emitted('retry')).toHaveLength(1);
  });
});
