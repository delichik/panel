// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils';
import { defineComponent, nextTick, ref } from 'vue';
import { afterEach, describe, expect, it } from 'vitest';
import Dialog from './Dialog.vue';
import Dropdown from './Dropdown.vue';
import DropdownItem from './DropdownItem.vue';
import LoadingOverlay from './LoadingOverlay.vue';
import Select from './Select.vue';
import Tabs from './Tabs.vue';

afterEach(() => {
  document.body.innerHTML = '';
});

describe('Dialog', () => {
  it('traps focus, closes with Escape, and restores the trigger focus', async () => {
    const Host = defineComponent({
      components: { Dialog },
      setup() {
        const open = ref(false);
        return { open };
      },
      template: `
        <button id="trigger" @click="open = true">Open</button>
        <Dialog v-model:open="open" title="Example" description="Details" close-label="Close">
          <button id="body-action">Action</button>
          <template #footer><button id="footer-action">Save</button></template>
        </Dialog>
      `,
    });
    const wrapper = mount(Host, { attachTo: document.body });
    const trigger = wrapper.get('#trigger').element as HTMLButtonElement;
    trigger.focus();
    await wrapper.get('#trigger').trigger('click');
    await nextTick();

    const renderedDialog = document.querySelector<HTMLElement>('[role="dialog"]');
    expect(renderedDialog?.getAttribute('aria-labelledby')).toBeTruthy();
    expect(renderedDialog?.getAttribute('aria-describedby')).toBeTruthy();
    expect(document.activeElement?.getAttribute('aria-label')).toBe('Close');

    const overlay = document.querySelector<HTMLElement>('[role="presentation"]')!;
    overlay.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await nextTick();
    expect(document.querySelector('[role="dialog"]')).not.toBeNull();

    const footer = document.querySelector<HTMLButtonElement>('#footer-action')!;
    footer.focus();
    footer.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true }));
    await nextTick();
    expect(document.activeElement?.getAttribute('aria-label')).toBe('Close');

    renderedDialog?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
    await flushPromises();
    expect(document.querySelector('[role="dialog"]')).toBeNull();
    expect(document.activeElement).toBe(trigger);
  });
});

describe('LoadingOverlay', () => {
  it('renders a spinner with an optional label', () => {
    const wrapper = mount(LoadingOverlay, { props: { label: 'Loading' } });
    expect(wrapper.find('[role="status"]').exists()).toBe(true);
    expect(wrapper.text()).toContain('Loading');
  });
});

describe('Select', () => {
  it('teleports and positions the listbox outside clipping containers', async () => {
    const wrapper = mount(Select, {
      attachTo: document.body,
      props: {
        modelValue: 'one',
        options: [{ label: 'One', value: 'one' }, { label: 'Two', value: 'two' }],
      },
    });
    const button = wrapper.get('[role="combobox"]').element as HTMLButtonElement;
    button.getBoundingClientRect = () => ({
      x: 20, y: 30, left: 20, top: 30, right: 220, bottom: 66, width: 200, height: 36,
      toJSON: () => ({}),
    });
    await wrapper.get('[role="combobox"]').trigger('click');
    await nextTick();

    const listbox = document.querySelector<HTMLElement>('[role="listbox"]')!;
    // The listbox is teleported to <body>; the Transition stub wraps it in tests.
    expect(document.body.contains(listbox)).toBe(true);
    expect(listbox.style.position).toBe('fixed');
    expect(listbox.style.width).toBe('200px');
    expect(listbox.style.left).toBe('20px');
  });
});

describe('Dropdown', () => {
  it('keeps the menu compact instead of stretching it to the viewport', async () => {
    const wrapper = mount(Dropdown, {
      attachTo: document.body,
      slots: {
        trigger: '<button id="compact-trigger">Menu</button>',
        default: '<DropdownItem>First</DropdownItem>',
      },
      global: { components: { DropdownItem } },
    });
    await wrapper.get('#compact-trigger').trigger('click');
    await flushPromises();

    const menu = document.querySelector<HTMLElement>('[role="menu"]')!;
    expect(menu.classList.contains('w-max')).toBe(true);
    expect(menu.style.minWidth).toBe('160px');
    wrapper.unmount();
  });

	 it('does not close from the synthetic click after keyboard activation', async () => {
		const wrapper = mount(Dropdown, { attachTo: document.body, slots: { trigger: '<button id="keyboard-trigger">Menu</button>', default: '<DropdownItem>First</DropdownItem>' }, global: { components: { DropdownItem } } });
		const trigger = wrapper.get('#keyboard-trigger');
		await trigger.trigger('keydown', { key: 'Enter' });
		trigger.element.dispatchEvent(new MouseEvent('click', { bubbles: true, detail: 0 }));
		await nextTick();
		expect(trigger.attributes('aria-expanded')).toBe('true');
		expect(document.querySelector('[role="menu"]')).not.toBeNull();
		wrapper.unmount();
	 });

	 it.each(['Enter', ' '])('opens once from %s keyboard activation', async (key) => {
		const wrapper = mount(Dropdown, { attachTo: document.body, slots: { trigger: '<button id="key-trigger">Menu</button>', default: '<DropdownItem>First</DropdownItem>' }, global: { components: { DropdownItem } } });
		const trigger = wrapper.get('#key-trigger');
		await trigger.trigger('keydown', { key });
		trigger.element.dispatchEvent(new MouseEvent('click', { bubbles: true, detail: 0 }));
		await nextTick();
		expect(trigger.attributes('aria-expanded')).toBe('true');
		wrapper.unmount();
	 });

	 it('opens from an independent assistive-technology style click', async () => {
		const wrapper = mount(Dropdown, { attachTo: document.body, slots: { trigger: '<button id="at-trigger">Menu</button>', default: '<DropdownItem>First</DropdownItem>' }, global: { components: { DropdownItem } } });
		wrapper.get('#at-trigger').element.dispatchEvent(new MouseEvent('click', { bubbles: true, detail: 0 }));
		await nextTick();
		expect(document.querySelector('[role="menu"]')).not.toBeNull();
		wrapper.unmount();
	 });

	 it('toggles from a pointer click', async () => {
		const wrapper = mount(Dropdown, { attachTo: document.body, slots: { trigger: '<button id="pointer-trigger">Menu</button>', default: '<DropdownItem>First</DropdownItem>' }, global: { components: { DropdownItem } } });
		wrapper.get('#pointer-trigger').element.dispatchEvent(new MouseEvent('click', { bubbles: true, detail: 1 }));
		await nextTick();
		expect(document.querySelector('[role="menu"]')).not.toBeNull();
		wrapper.unmount();
	 });
  it('exposes menu state and supports menu keyboard navigation', async () => {
    const wrapper = mount(Dropdown, {
      attachTo: document.body,
      slots: {
        trigger: '<button id="menu-trigger">Menu</button>',
        default: '<DropdownItem>First</DropdownItem><DropdownItem disabled>Disabled</DropdownItem><DropdownItem>Last</DropdownItem>',
      },
      global: { components: { DropdownItem } },
    });
    const trigger = wrapper.get('#menu-trigger');
    await trigger.trigger('keydown', { key: 'ArrowDown' });
    await nextTick();

    expect(trigger.attributes('aria-expanded')).toBe('true');
    const items = Array.from(document.querySelectorAll<HTMLElement>('[role="menuitem"]'));
    expect(document.activeElement).toBe(items[0]);
    items[0]!.dispatchEvent(new KeyboardEvent('keydown', { key: 'End', bubbles: true }));
    await nextTick();
    expect(document.activeElement).toBe(items[2]);
    items[2]!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    await nextTick();
    expect(document.querySelector('[role="menu"]')).toBeNull();
    expect(document.activeElement).toBe(trigger.element);
  });
});

describe('Tabs', () => {
  it('links tabs to the panel and skips disabled tabs with arrow keys', async () => {
    const Host = defineComponent({
      components: { Tabs },
      setup() {
        const value = ref('one');
        const tabs = [
          { label: 'One', value: 'one' },
          { label: 'Two', value: 'two', disabled: true },
          { label: 'Three', value: 'three' },
        ];
        return { value, tabs };
      },
      template: '<Tabs v-model="value" :tabs="tabs">Panel content</Tabs>',
    });
    const wrapper = mount(Host, { attachTo: document.body });
    const tabs = wrapper.findAll('[role="tab"]');
    const panel = wrapper.get('[role="tabpanel"]');
    expect(tabs[0]?.attributes('aria-controls')).toBe(panel.attributes('id'));
    expect(panel.attributes('aria-labelledby')).toBe(tabs[0]?.attributes('id'));
    expect(tabs.map((tab) => tab.attributes('tabindex'))).toEqual(['0', '-1', '-1']);

    await tabs[0]!.trigger('keydown', { key: 'ArrowRight' });
    await nextTick();
    expect(tabs[2]?.attributes('aria-selected')).toBe('true');
    expect(document.activeElement).toBe(tabs[2]?.element);
  });
});
