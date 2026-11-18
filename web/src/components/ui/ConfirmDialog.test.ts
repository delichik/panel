// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { afterEach, describe, expect, it } from 'vitest';
import ConfirmDialog from './ConfirmDialog.vue';

afterEach(() => {
  document.body.innerHTML = '';
});

function mountConfirm(overrides: Record<string, unknown> = {}) {
  return mount(ConfirmDialog, {
    attachTo: document.body,
    props: {
      open: true,
      title: 'Delete server',
      impact: 'This cannot be undone.',
      confirmLabel: 'Delete',
      cancelLabel: 'Cancel',
      checkboxLabel: 'I understand',
      ...overrides,
    },
  });
}

describe('ConfirmDialog impact', () => {
  it('renders the impact as a WinUI InfoBar instead of a hand-made 8px alert', () => {
    const wrapper = mountConfirm({ tone: 'danger' });
    const bar = document.querySelector<HTMLElement>('[data-info-bar]');
    expect(bar).not.toBeNull();
    expect(bar!.dataset.tone).toBe('danger');
    expect(bar!.textContent).toContain('This cannot be undone.');
    expect(bar!.getAttribute('role')).toBe('alert');
    // InfoBar 的 4px 控件圆角，不再使用 8px 浮层圆角。
    expect(bar!.className).toContain('rounded-control');
    expect(bar!.className).not.toContain('rounded-overlay');
    wrapper.unmount();
  });

  it('maps warning and neutral tones to InfoBar tones', () => {
    const warning = mountConfirm({ tone: 'warning' });
    expect(document.querySelector<HTMLElement>('[data-info-bar]')!.dataset.tone).toBe('warning');
    warning.unmount();
    document.body.innerHTML = '';

    const neutral = mountConfirm({ tone: 'neutral' });
    expect(document.querySelector<HTMLElement>('[data-info-bar]')!.dataset.tone).toBe('info');
    neutral.unmount();
  });

  it('still renders the checkbox confirmation row with semantic Body text', () => {
    const wrapper = mountConfirm({ requireCheckbox: true });
    const checkbox = document.querySelector<HTMLInputElement>('input[type="checkbox"]');
    expect(checkbox).not.toBeNull();
    const row = checkbox!.closest('div');
    expect(row?.className).toContain('text-body');
    expect(row?.className).not.toContain('text-sm');
    wrapper.unmount();
  });
});
