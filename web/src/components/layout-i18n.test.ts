// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { defineComponent, nextTick } from 'vue';
import { afterEach, describe, expect, it } from 'vitest';
import { useI18n } from '@/i18n';
import PageHeader from './shell/PageHeader.vue';
import Dialog from './ui/Dialog.vue';
import Table from './ui/Table.vue';
import ToastProvider from './ui/ToastProvider.vue';
import { useToast } from './ui/toast';

afterEach(() => {
  document.body.innerHTML = '';
  useI18n().setLocale('zh-CN');
});

describe('responsive shared layout', () => {
  it('uses the table root as the constrained scroll container', () => {
    const wrapper = mount(Table, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        rows: [{ name: 'Panel' }],
      },
    });

    expect(wrapper.classes()).toContain('overflow-auto');
    expect(wrapper.classes()).toContain('rounded-xl');
    expect(wrapper.get('thead').classes()).toContain('sticky');
    expect(wrapper.element.querySelector(':scope > div')).toBeNull();
  });

  it('stacks the page header and wraps actions below the large breakpoint', () => {
    const wrapper = mount(PageHeader, {
      props: { title: 'Diagnostics' },
      slots: { actions: '<button>Refresh</button><button>Pause</button>' },
    });

    expect(wrapper.classes()).toContain('max-lg:flex-col');
    const actions = wrapper.get('header > div:last-child');
    expect(actions.classes()).toContain('flex-wrap');
    expect(actions.classes()).toContain('max-lg:w-full');
  });
});

describe('localized accessible names', () => {
  it('localizes the default dialog close action', async () => {
    useI18n().setLocale('zh-CN');
    mount(Dialog, {
      attachTo: document.body,
      props: { open: true, title: '确认' },
    });
    await nextTick();

    expect(document.querySelector('[role="dialog"] button')?.getAttribute('aria-label')).toBe('关闭');
  });

  it('localizes the toast dismiss action', async () => {
    useI18n().setLocale('zh-CN');
    const Producer = defineComponent({
      setup() {
        useToast().push({ title: '已保存' });
      },
      template: '<span />',
    });
    mount(ToastProvider, {
      attachTo: document.body,
      slots: { default: Producer },
    });
    await nextTick();

    expect(document.querySelector('[role="status"] button')?.getAttribute('aria-label')).toBe('关闭');
  });

  it('provides both locale variants for the main navigation name', () => {
    const { setLocale, t } = useI18n();
    setLocale('en');
    expect(t('layout.main')).toBe('Main navigation');
    setLocale('zh-CN');
    expect(t('layout.main')).toBe('主导航');
  });
});
