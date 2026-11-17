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

  it('keeps disabled tiles visible with their disabled reason', () => {
    const wrapper = mount(ServerMultiPicker, {
      props: { modelValue: [], servers, label: 'nodes', variant: 'compact', disabledIds: ['srv-1'], disabledReasons: { 'srv-1': 'Agent required' } },
    });
    const tiles = wrapper.findAll('button');
    expect(tiles[0]!.attributes('disabled')).toBeDefined();
    expect(tiles[0]!.text()).toContain('Agent required');
  });
});
