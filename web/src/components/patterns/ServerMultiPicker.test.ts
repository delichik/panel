// @vitest-environment jsdom
import { enableAutoUnmount, mount } from '@vue/test-utils';
import { afterEach, describe, expect, it } from 'vitest';
import ServerMultiPicker from './ServerMultiPicker.vue';

enableAutoUnmount(afterEach);

const servers = [
  { id: 'srv-1', name: 'edge', description: '10.0.0.1', status: 'reachable' },
  { id: 'srv-2', name: 'core', description: '10.0.0.2', status: 'unreachable' },
  { id: 'srv-3', name: 'worker', description: '10.0.0.3', capabilities: ['agent'] },
];

describe('ServerMultiPicker', () => {
  it('defaults to the full-width list layout used by existing callers', () => {
    const wrapper = mount(ServerMultiPicker, { props: { modelValue: [], servers, label: 'nodes' } });
    const grid = wrapper.find('section');
    expect(grid.attributes('aria-label')).toBe('nodes');
    expect(grid.classes()).not.toContain('sm:grid-cols-2');
    expect(grid.classes()).not.toContain('xl:grid-cols-3');
  });

  it('renders the compact grid tiles with aria-pressed and keyboard-capable buttons', async () => {
    const wrapper = mount(ServerMultiPicker, {
      props: { modelValue: ['srv-2'], servers, label: 'nodes', variant: 'compact' },
      attachTo: document.body,
    });

    const grid = wrapper.find('section');
    expect(grid.classes()).toContain('sm:grid-cols-2');
    expect(grid.classes()).toContain('xl:grid-cols-3');

    const tiles = wrapper.findAll('button');
    expect(tiles).toHaveLength(3);
    expect(tiles[0]!.attributes('type')).toBe('button');
    expect(tiles[0]!.attributes('aria-pressed')).toBe('false');
    expect(tiles[1]!.attributes('aria-pressed')).toBe('true');

    await tiles[0]!.trigger('click');
    expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toEqual(['srv-2', 'srv-1']);
  });

  it('marks selection with the WinUI accent fill only (no self-made accent border/text)', () => {
    const wrapper = mount(ServerMultiPicker, {
      props: { modelValue: ['srv-2'], servers, label: 'nodes', variant: 'compact' },
    });

    const selectedTile = wrapper.findAll('button')[1]!;
    expect(selectedTile.classes()).toContain('aria-pressed:bg-accent-selected');
    expect(selectedTile.classes()).not.toContain('aria-pressed:border-accent-text');
    // 选中行的 checkbox 使用 accent 填充 + white check。
    const box = selectedTile.get('span[aria-hidden="true"]');
    expect(box.classes()).toContain('bg-primary');
    expect(box.classes()).toContain('border-primary');

    const unselectedBox = wrapper.findAll('button')[0]!.get('span[aria-hidden="true"]');
    expect(unselectedBox.classes()).not.toContain('bg-primary');
  });

  it('applies the same selectable-row treatment to the list variant', () => {
    const wrapper = mount(ServerMultiPicker, {
      props: { modelValue: ['srv-3'], servers, label: 'nodes' },
    });
    const selectedRow = wrapper.findAll('button')[2]!;
    expect(selectedRow.classes()).toContain('aria-pressed:bg-accent-selected');
    expect(selectedRow.classes()).not.toContain('aria-pressed:border-accent-text');
    expect(selectedRow.find('span[aria-hidden="true"]').classes()).toContain('bg-primary');
  });

  it('keeps disabled tiles visible with their disabled reason', () => {
    const wrapper = mount(ServerMultiPicker, {
      props: { modelValue: [], servers, label: 'nodes', variant: 'compact', disabledIds: ['srv-1'], disabledReasons: { 'srv-1': 'Agent required' } },
    });
    const tiles = wrapper.findAll('button');
    expect(tiles[0]!.attributes('disabled')).toBeDefined();
    expect(tiles[0]!.text()).toContain('Agent required');
  });
});
