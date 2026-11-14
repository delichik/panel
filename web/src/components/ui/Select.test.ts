// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import { afterEach, describe, expect, it } from 'vitest';
import Select from './Select.vue';

afterEach(() => {
  document.body.innerHTML = '';
});

function mountSelectable(overrides: Record<string, unknown> = {}) {
  return mount(Select, {
    attachTo: document.body,
    props: {
      modelValue: 'one',
      options: [
        { label: 'One · 10.0.0.1', value: 'one' },
        { label: 'Two · 10.0.0.2', value: 'two' },
        { label: 'Three · 10.0.0.3', value: 'three' },
      ],
      searchable: true,
      searchPlaceholder: 'Search servers',
      searchEmptyText: 'No matching servers',
      ...overrides,
    },
  });
}

async function openSelect() {
  document.querySelector<HTMLElement>('[role="combobox"]')!.click();
  await nextTick();
}

function optionLabels() {
  return Array.from(document.querySelectorAll<HTMLElement>('[role="option"]')).map((element) => element.textContent?.trim() ?? '');
}

async function typeSearch(value: string) {
  const input = document.querySelector<HTMLInputElement>('[role="searchbox"]')!;
  expect(input).not.toBeNull();
  input.value = value;
  input.dispatchEvent(new Event('input'));
  await nextTick();
}

describe('Select searchable mode', () => {
  it('filters visible options and emits search without changing the selected label', async () => {
    const wrapper = mountSelectable();
    await openSelect();

    await typeSearch('two');

    expect(optionLabels()).toEqual(['Two · 10.0.0.2']);
    expect(wrapper.emitted('search')).toEqual([['two']]);
    // 过滤结果不含当前值，触发器仍展示已选标签。
    expect(wrapper.get('[role="combobox"]').text()).toContain('One · 10.0.0.1');
  });

  it('shows the empty text when no option matches the query', async () => {
    const wrapper = mountSelectable();
    await openSelect();

    await typeSearch('does-not-exist');

    expect(optionLabels()).toEqual([]);
    expect(document.body.textContent).toContain('No matching servers');
  });

  it('supports keyboard selection inside the search box', async () => {
    const wrapper = mountSelectable();
    await openSelect();
    await typeSearch('three');

    const input = document.querySelector<HTMLInputElement>('[role="searchbox"]')!;
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
    await nextTick();
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    await nextTick();

    expect(wrapper.emitted('update:modelValue')).toEqual([['three']]);
    expect(document.querySelector('[role="listbox"]')).toBeNull();
  });

  it('keeps filtering after remote options are merged in', async () => {
    const wrapper = mountSelectable();
    await openSelect();
    await typeSearch('far');

    expect(optionLabels()).toEqual([]);
    await wrapper.setProps({
      options: [
        { label: 'One · 10.0.0.1', value: 'one' },
        { label: 'Far away · 10.9.9.9', value: 'far' },
      ],
    });
    await nextTick();

    expect(optionLabels()).toEqual(['Far away · 10.9.9.9']);
  });
});
