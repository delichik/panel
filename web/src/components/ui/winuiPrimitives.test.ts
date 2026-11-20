// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { afterEach, describe, expect, it } from 'vitest';
import BreadcrumbBar from './BreadcrumbBar.vue';
import Expander from './Expander.vue';
import NumberBox from './NumberBox.vue';
import RadioButton from './RadioButton.vue';
import ToggleButton from './ToggleButton.vue';

afterEach(() => {
  document.body.innerHTML = '';
});

describe('NumberBox', () => {
  it('exposes spinbutton semantics with value bounds and id wiring', () => {
    const wrapper = mount(NumberBox, {
      attachTo: document.body,
      props: { modelValue: 5, min: 0, max: 10, step: 2, id: 'port', ariaDescribedby: 'port-hint' },
    });
    const input = wrapper.get('input');
    expect(input.attributes('role')).toBe('spinbutton');
    expect(input.attributes('aria-valuemin')).toBe('0');
    expect(input.attributes('aria-valuemax')).toBe('10');
    expect(input.attributes('aria-valuenow')).toBe('5');
    expect(input.attributes('id')).toBe('port');
    expect(input.attributes('aria-describedby')).toBe('port-hint');
    wrapper.unmount();
  });

  it('clamps typed values and steps with keyboard and buttons', async () => {
    const wrapper = mount(NumberBox, {
      attachTo: document.body,
      props: { modelValue: 5, min: 0, max: 10, step: 2 },
    });
    const input = wrapper.get('input');

    await input.setValue('99');
    await input.trigger('change');
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([10]);

    await input.setValue('abc');
    await input.trigger('change');
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([10]);

    await input.setValue('5');
    await input.trigger('keydown', { key: 'ArrowUp' });
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([7]);
    await input.trigger('keydown', { key: 'ArrowDown' });
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([3]);
    await input.trigger('keydown', { key: 'Home' });
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([0]);
    await input.trigger('keydown', { key: 'End' });
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([10]);

    const buttons = wrapper.findAll('button');
    expect(buttons).toHaveLength(2);
    await buttons[0]!.trigger('click');
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([3]);
    await buttons[1]!.trigger('click');
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([7]);
    wrapper.unmount();
  });

  it('marks invalid state and disables interaction', async () => {
    const invalid = mount(NumberBox, {
      attachTo: document.body,
      props: { modelValue: null, invalid: true },
    });
    expect(invalid.get('input').attributes('aria-invalid')).toBe('true');
    expect(invalid.classes()).toContain('border-danger-border');
    invalid.unmount();

    const disabled = mount(NumberBox, {
      attachTo: document.body,
      props: { modelValue: 1, disabled: true },
    });
    expect(disabled.get('input').attributes('disabled')).toBeDefined();
    expect(disabled.findAll('button').every((button) => button.attributes('disabled') !== undefined)).toBe(true);
    await disabled.get('input').trigger('keydown', { key: 'ArrowUp' });
    expect(disabled.emitted('update:modelValue')).toBeUndefined();
    disabled.unmount();
  });
});

describe('RadioButton', () => {
  it('exposes role=radio/aria-checked and roving tabindex', async () => {
    const unchecked = mount(RadioButton, {
      attachTo: document.body,
      props: { modelValue: 'a', value: 'b', label: 'Option B', description: 'Second option' },
    });
    const button = unchecked.get('[role="radio"]');
    expect(button.attributes('aria-checked')).toBe('false');
    expect(button.attributes('tabindex')).toBe('-1');
    const descriptionId = button.attributes('aria-describedby');
    expect(descriptionId).toBeTruthy();
    expect(unchecked.get(`#${descriptionId}`).text()).toBe('Second option');
    await button.trigger('click');
    expect(unchecked.emitted('update:modelValue')?.[0]).toEqual(['b']);
    unchecked.unmount();

    const checked = mount(RadioButton, {
      attachTo: document.body,
      props: { modelValue: 'b', value: 'b', label: 'Option B' },
    });
    expect(checked.get('[role="radio"]').attributes('aria-checked')).toBe('true');
    expect(checked.get('[role="radio"]').attributes('tabindex')).toBe('0');
    expect(checked.classes()).toContain('focus-visible:ring-2');
    checked.unmount();
  });

  it('does not emit when disabled', async () => {
    const wrapper = mount(RadioButton, {
      attachTo: document.body,
      props: { modelValue: 'a', value: 'b', label: 'Option B', disabled: true },
    });
    const button = wrapper.get('[role="radio"]');
    expect(button.attributes('disabled')).toBeDefined();
    await button.trigger('click');
    expect(wrapper.emitted('update:modelValue')).toBeUndefined();
    wrapper.unmount();
  });
});

describe('ToggleButton', () => {
  it('exposes aria-pressed and toggles with the selected fill', async () => {
    const wrapper = mount(ToggleButton, { props: { modelValue: false, label: 'Pin' }, slots: { default: 'Pin' } });
    const button = wrapper.get('button');
    expect(button.attributes('aria-pressed')).toBe('false');
    expect(button.classes()).not.toContain('bg-accent-selected');
    expect(button.classes()).toContain('h-control');

    await button.trigger('click');
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([true]);

    await wrapper.setProps({ modelValue: true });
    expect(wrapper.get('button').attributes('aria-pressed')).toBe('true');
    expect(wrapper.get('button').classes()).toContain('bg-accent-selected');
    expect(wrapper.get('button').classes()).toContain('text-accent-text');
    wrapper.unmount();
  });

  it('does not toggle when disabled', async () => {
    const wrapper = mount(ToggleButton, { props: { modelValue: false, label: 'Pin', disabled: true } });
    await wrapper.get('button').trigger('click');
    expect(wrapper.emitted('update:modelValue')).toBeUndefined();
    expect(wrapper.get('button').attributes('disabled')).toBeDefined();
    wrapper.unmount();
  });
});

describe('Expander', () => {
  it('toggles aria-expanded/aria-controls and the content visibility', async () => {
    const wrapper = mount(Expander, {
      props: { title: 'Advanced', description: 'More options' },
      slots: { default: '<p class="expander-body">Body</p>' },
    });
    const header = wrapper.get('button');
    const panelId = header.attributes('aria-controls');
    expect(header.attributes('aria-expanded')).toBe('false');
    expect(panelId).toBeTruthy();
    // v-show 隐藏时写入 display:none（jsdom 下不用 isVisible，避免布局尺寸误判）。
    expect(wrapper.get(`#${panelId}`).attributes('style') ?? '').toContain('display: none');

    await header.trigger('click');
    expect(header.attributes('aria-expanded')).toBe('true');
    expect(wrapper.emitted('update:open')?.[0]).toEqual([true]);
    expect(wrapper.get(`#${panelId}`).attributes('style') ?? '').not.toContain('display: none');
    expect(wrapper.get(`#${panelId}`).attributes('role')).toBe('region');
    expect(wrapper.text()).toContain('Advanced');
    wrapper.unmount();
  });

  it('honours the open prop and ignores clicks when disabled', async () => {
    const open = mount(Expander, { props: { title: 'Advanced', open: true }, slots: { default: 'Body' } });
    expect(open.get('button').attributes('aria-expanded')).toBe('true');
    open.unmount();

    const disabled = mount(Expander, { props: { title: 'Advanced', disabled: true }, slots: { default: 'Body' } });
    await disabled.get('button').trigger('click');
    expect(disabled.emitted('update:open')).toBeUndefined();
    expect(disabled.get('button').attributes('aria-expanded')).toBe('false');
    disabled.unmount();
  });
});

describe('BreadcrumbBar', () => {
  it('renders linked ancestors, separators and the current page', () => {
    const wrapper = mount(BreadcrumbBar, {
      props: {
        label: 'Breadcrumb',
        items: [
          { label: 'Home', to: '/' },
          { label: 'Servers', to: '/servers' },
          { label: 'web-1' },
        ],
      },
      global: {
        stubs: {
          RouterLink: { props: ['to'], template: '<a class="crumb-link" :href="to"><slot /></a>' },
        },
      },
    });
    const nav = wrapper.get('nav');
    expect(nav.attributes('aria-label')).toBe('Breadcrumb');
    expect(wrapper.findAll('.crumb-link')).toHaveLength(2);

    const current = wrapper.get('[aria-current="page"]');
    expect(current.text()).toBe('web-1');
    // 两条分隔符位于 3 个条目之间，且对辅助技术隐藏。
    const separators = wrapper.findAll('svg[aria-hidden="true"]');
    expect(separators).toHaveLength(2);
    wrapper.unmount();
  });

  it('renders plain labels for non-linked ancestors and a single current item', () => {
    const wrapper = mount(BreadcrumbBar, {
      props: { label: 'Breadcrumb', items: [{ label: 'Only' }] },
      global: { stubs: { RouterLink: true } },
    });
    expect(wrapper.findAll('svg')).toHaveLength(0);
    expect(wrapper.get('[aria-current="page"]').text()).toBe('Only');
    wrapper.unmount();
  });
});
