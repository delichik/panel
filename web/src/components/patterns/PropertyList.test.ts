// @vitest-environment jsdom
import { h } from 'vue';
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils';
import { afterEach, describe, expect, it } from 'vitest';
import PropertyList from './PropertyList.vue';

enableAutoUnmount(afterEach);

const items = [
  { key: 'state', label: 'State', value: 'running' },
  { key: 'id', label: 'Container ID', value: 'abc123', mono: true },
];

describe('PropertyList', () => {
  it('renders Caption labels and Body values as a description list', () => {
    const wrapper = mount(PropertyList, { props: { items } });

    const list = wrapper.get('[data-property-list]');
    expect(list.element.tagName).toBe('DL');
    expect(list.attributes('data-columns')).toBe('2');

    const rows = wrapper.findAll('[data-property-row]');
    expect(rows).toHaveLength(2);
    expect(rows[0]!.get('dt').text()).toBe('State');
    expect(rows[0]!.get('dt').classes()).toContain('text-caption');
    expect(rows[0]!.get('dt').classes()).toContain('text-muted-foreground');
    expect(rows[0]!.get('dd').text()).toBe('running');
    expect(rows[0]!.get('dd').classes()).toContain('text-body');
    expect(rows[1]!.get('dd span').classes()).toContain('panel-mono');
  });

  it('supports 1 / 2 / 3 column layouts', () => {
    const single = mount(PropertyList, { props: { items, columns: 1 } });
    expect(single.get('[data-property-list]').attributes('data-columns')).toBe('1');
    // 单列用 divide-y 行分隔；多列改用网格避免分隔线错位。
    expect(single.get('[data-property-list]').classes()).toContain('divide-y');
    expect(single.get('[data-property-list]').classes()).toContain('divide-divider');

    const double = mount(PropertyList, { props: { items, columns: 2 } });
    expect(double.get('[data-property-list]').classes()).toContain('sm:grid-cols-2');
    // 手机宽度（<sm）强制单列，避免两列把标签/值挤到溢出。
    expect(double.get('[data-property-list]').classes()).toContain('max-sm:grid-cols-1');
    expect(double.get('[data-property-list]').classes()).not.toContain('divide-y');

    const triple = mount(PropertyList, { props: { items, columns: 3 } });
    expect(triple.get('[data-property-list]').classes()).toContain('xl:grid-cols-3');
    expect(triple.get('[data-property-list]').classes()).toContain('max-sm:grid-cols-1');
  });

  it('lets labels and values wrap on narrow screens', () => {
    const wrapper = mount(PropertyList, { props: { items } });
    const row = wrapper.get('[data-property-row]');
    expect(row.get('dt').classes()).toContain('break-words');
    expect(row.get('dd').classes()).toContain('break-words');
  });

  it('renders links for href items and opts external links out of the app window', () => {
    const wrapper = mount(PropertyList, {
      props: {
        items: [
          { label: 'Console', value: 'Open', href: '/servers/srv-1' },
          { label: 'Docs', value: 'docs.example', href: 'https://example.com', external: true },
        ],
      },
    });

    const internal = wrapper.findAll('a')[0]!;
    expect(internal.attributes('href')).toBe('/servers/srv-1');
    expect(internal.classes()).toContain('text-accent-text');
    expect(internal.attributes('target')).toBeUndefined();

    const external = wrapper.findAll('a')[1]!;
    expect(external.attributes('target')).toBe('_blank');
    expect(external.attributes('rel')).toBe('noreferrer');
  });

  it('renders a copy affordance that emits the copied value and switches its label', async () => {
    const wrapper = mount(PropertyList, {
      props: {
        items: [{ key: 'fp', label: 'Fingerprint', value: 'SHA256:abc', copyable: true }],
        copyLabel: 'Copy',
        copiedLabel: 'Copied',
      },
    });

    const button = wrapper.get('button');
    expect(button.text()).toBe('Copy');
    expect(button.attributes('title')).toBe('Copy');

    await button.trigger('click');
    await flushPromises();

    expect(wrapper.emitted('copy')?.at(-1)).toEqual(['SHA256:abc']);
    expect(button.text()).toBe('Copied');
    expect(button.find('svg').exists()).toBe(true);
  });

  it('hides the copy button when no copy label is provided', () => {
    const wrapper = mount(PropertyList, {
      props: { items: [{ label: 'Fingerprint', value: 'SHA256:abc', copyable: true }] },
    });
    expect(wrapper.find('button').exists()).toBe(false);
  });

  it('uses the empty text for empty lists and missing values', () => {
    const empty = mount(PropertyList, { props: { items: [], emptyText: 'No properties' } });
    expect(empty.get('[data-property-list-empty]').text()).toBe('No properties');
    expect(empty.find('[data-property-list]').exists()).toBe(false);

    const missing = mount(PropertyList, { props: { items: [{ label: 'Target', value: null }] } });
    expect(missing.get('[data-property-row] dd').text()).toBe('—');
  });

  it('lets callers override value rendering through the value slot', () => {
    const wrapper = mount(PropertyList, {
      props: { items: [{ label: 'State', value: 'running', key: 'state' }] },
      slots: {
        value: (slotProps: Record<string, unknown>) => h('strong', { class: 'custom-value' }, String(slotProps.text)),
      },
    });
    expect(wrapper.get('.custom-value').text()).toBe('running');
  });

  it('keeps fixed labels and skeletons only values when loading', () => {
    const wrapper = mount(PropertyList, {
      props: {
        items: [
          { key: 'fp', label: 'Fingerprint', value: 'SHA256:abc', copyable: true, skeletonWidth: 'w-52' },
          { key: 'state', label: 'State', value: 'running' },
        ],
        copyLabel: 'Copy',
        loading: true,
      },
    });

    const list = wrapper.get('[data-property-list]');
    expect(list.attributes('aria-busy')).toBe('true');
    const rows = wrapper.findAll('[data-property-row]');
    expect(rows[0]!.get('dt').text()).toBe('Fingerprint');
    expect(rows[0]!.find('dd .motion-skeleton').exists()).toBe(true);
    expect(rows[0]!.find('dd .motion-skeleton').classes()).toContain('w-52');
    expect(rows[0]!.text()).not.toContain('SHA256:abc');
    expect(rows[0]!.find('button').exists()).toBe(false);
    expect(rows[1]!.get('dt').text()).toBe('State');
    expect(rows[1]!.find('dd .motion-skeleton').exists()).toBe(true);
  });
});
