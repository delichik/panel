// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { defineComponent } from 'vue';
import { afterEach, describe, expect, it } from 'vitest';
import Badge from './Badge.vue';
import Button from './Button.vue';
import Checkbox from './Checkbox.vue';
import Dialog from './Dialog.vue';
import Switch from './Switch.vue';

afterEach(() => {
  document.body.innerHTML = '';
});

describe('WinUI 3 Button', () => {
  it('uses Body 14 / regular weight and the 32px control height by default', () => {
    const wrapper = mount(Button, { slots: { default: 'Save' } });
    const classes = wrapper.classes();
    expect(classes).toContain('text-body');
    expect(classes).toContain('font-normal');
    expect(classes).not.toContain('font-semibold');
    expect(classes).toContain('h-control');
    expect(classes).toContain('rounded-control');
    expect(classes).toContain('focus-visible:ring-2');
    expect(classes).toContain('focus-visible:ring-ring');
    expect(classes).not.toContain('focus-visible:ring-offset-2');
  });

  it('uses Caption 12 / 28px for the small size with 4px-grid horizontal padding', () => {
    const wrapper = mount(Button, { props: { size: 'sm' }, slots: { default: 'Small' } });
    expect(wrapper.classes()).toContain('text-caption');
    expect(wrapper.classes()).toContain('h-control-sm');
    expect(wrapper.classes()).toContain('px-2');
    expect(wrapper.classes()).not.toContain('px-2.5');
  });

  it('switches to the disabled colour palette instead of a blanket opacity', () => {
    const wrapper = mount(Button, { props: { variant: 'primary', disabled: true }, slots: { default: 'Primary' } });
    const classes = wrapper.classes();
    expect(classes).toContain('bg-muted');
    expect(classes).toContain('text-muted-foreground');
    expect(classes).not.toContain('bg-primary');
    expect(classes).not.toContain('opacity-45');
  });

  it('keeps the variant palette while loading (only blocks interaction)', () => {
    const wrapper = mount(Button, { props: { variant: 'primary', loading: true }, slots: { default: 'Saving' } });
    const classes = wrapper.classes();
    expect(wrapper.attributes('aria-busy')).toBe('true');
    expect(classes).toContain('bg-primary');
    expect(classes).not.toContain('bg-muted');
  });
});

describe('WinUI 3 Checkbox', () => {
  it('uses a 20px box inside a 32px hit area and exposes the native checkbox', () => {
    const wrapper = mount(Checkbox, { props: { modelValue: true, label: 'Enable' } });
    const input = wrapper.get('input[type="checkbox"]');
    expect((input.element as HTMLInputElement).checked).toBe(true);
    expect(wrapper.classes()).toContain('min-h-control');
    const box = wrapper.get('span[aria-hidden="true"]');
    expect(box.classes()).toContain('size-5');
    expect(box.classes()).toContain('rounded-control');
    expect(box.classes()).toContain('peer-focus-visible:ring-2');
    expect(wrapper.find('svg').exists()).toBe(true);
  });

  it('emits updates and uses the disabled palette', async () => {
    const wrapper = mount(Checkbox, { props: { modelValue: false, label: 'Enable' } });
    await wrapper.get('input').setValue(true);
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([true]);

    const disabled = mount(Checkbox, { props: { modelValue: false, label: 'Enable', disabled: true } });
    expect(disabled.classes()).toContain('text-muted-foreground');
    expect(disabled.get('span[aria-hidden="true"]').classes()).toContain('bg-muted');
  });
});

describe('WinUI 3 ToggleSwitch', () => {
  it('keeps the 40x20 track inside a 32px hit area and toggles aria-checked', async () => {
    const wrapper = mount(Switch, { props: { modelValue: true, label: 'Mica' } });
    const control = wrapper.get('[role="switch"]');
    expect(control.attributes('aria-checked')).toBe('true');
    expect(control.classes()).toContain('min-h-control');
    const track = control.get('span');
    expect(track.classes()).toContain('h-5');
    expect(track.classes()).toContain('w-10');
    expect(track.classes()).toContain('bg-primary');

    await control.trigger('click');
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([false]);
  });

  it('uses the disabled palette for the track and thumb', () => {
    const wrapper = mount(Switch, { props: { modelValue: false, label: 'Mica', disabled: true } });
    const control = wrapper.get('[role="switch"]');
    expect(control.attributes('disabled')).toBeDefined();
    expect(control.get('span').classes()).toContain('bg-muted');
    expect(control.get('span span').classes()).toContain('bg-muted-foreground');
  });
});

describe('WinUI 3 Badge', () => {
  it('renders as a 24px InfoBadge pill', () => {
    const wrapper = mount(Badge, { props: { tone: 'success' }, slots: { default: 'OK' } });
    expect(wrapper.classes()).toContain('rounded-pill');
    expect(wrapper.classes()).toContain('h-6');
    expect(wrapper.classes()).toContain('text-caption');
  });
});

describe('WinUI 3 ContentDialog', () => {
  it('uses the content-adaptive 320/548 width model, 24px content padding and spacing-only footer', () => {
    const Host = defineComponent({
      components: { Dialog },
      template: `<Dialog :open="true" title="Example"><p>Body</p><template #footer><button>OK</button></template></Dialog>`,
    });
    const wrapper = mount(Host, { attachTo: document.body });
    const dialog = document.querySelector<HTMLElement>('[role="dialog"]')!;
    expect(dialog).not.toBeNull();
    expect(dialog.className).toContain('min-w-dialog');
    expect(dialog.className).toContain('w-auto');
    expect(dialog.className).toContain('max-w-[var(--panel-dialog-max-width)]');
    expect(dialog.querySelector('h2')!.className).toContain('text-subtitle');
    expect(dialog.querySelector('h2')!.className).toContain('break-words');
    expect(dialog.querySelector('h2')!.className).not.toContain('tracking-tight');
    const footer = dialog.querySelector('footer')!;
    expect(footer.className).not.toContain('border-t');
    // 手机宽度：footer 两列 full width，主操作（DOM 最后）保持在最后。
    expect(footer.className).toContain('max-sm:grid');
    expect(footer.className).toContain('max-sm:grid-cols-2');
    expect(footer.className).toContain('max-sm:items-stretch');
    const body = dialog.querySelector<HTMLElement>('header + div')!;
    expect(body.className).toContain('p-6');
    wrapper.unmount();
  });

  it('uses the large width token for the legacy large size', () => {
    const Host = defineComponent({
      components: { Dialog },
      template: `<Dialog :open="true" title="Large" size="large"><p>Body</p></Dialog>`,
    });
    const wrapper = mount(Host, { attachTo: document.body });
    const dialog = document.querySelector<HTMLElement>('[role="dialog"]')!;
    expect(dialog.className).toContain('max-w-[var(--panel-dialog-max-width-large)]');
    expect(dialog.className).toContain('h-[min(820px,calc(100dvh-32px))]');
    wrapper.unmount();
  });

  it('supports width="wide" with the pending --panel-dialog-max-width-wide token fallback', () => {
    const Host = defineComponent({
      components: { Dialog },
      template: `<Dialog :open="true" title="Wide" width="wide"><p>Body</p></Dialog>`,
    });
    const wrapper = mount(Host, { attachTo: document.body });
    const dialog = document.querySelector<HTMLElement>('[role="dialog"]')!;
    expect(dialog.className).toContain('max-w-[var(--panel-dialog-max-width-wide,720px)]');
    expect(dialog.className).not.toContain('max-w-[var(--panel-dialog-max-width)]');
    wrapper.unmount();
  });
});
