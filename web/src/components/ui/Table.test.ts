// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import Table from './Table.vue';

const columns = [
  { key: 'name', label: 'Name' },
  { key: 'actions', label: 'Actions', align: 'right' as const, nowrap: true },
];

describe('Table', () => {
  it('renders per-column cell slots and header slots', () => {
    const wrapper = mount(Table, {
      props: { columns, rows: [{ name: 'alpha' }], rowKey: 'name' },
      slots: {
        name: ({ value }: { value: unknown }) => `row:${String(value)}`,
        actions: '<button class="row-action">open</button>',
        'head-actions': '<span class="head-action">select all</span>',
      },
    });

    expect(wrapper.text()).toContain('row:alpha');
    expect(wrapper.get('.row-action').text()).toBe('open');
    expect(wrapper.get('.head-action').text()).toBe('select all');
    // Header slot replaces the plain label without dropping the column cell.
    expect(wrapper.findAll('th')).toHaveLength(2);
  });

  it('marks the row matching activeKey with aria-current and a muted surface', () => {
    const wrapper = mount(Table, {
      props: {
        columns,
        rows: [{ name: 'alpha' }, { name: 'beta' }],
        rowKey: 'name',
        activeKey: 'beta',
      },
    });

    const rows = wrapper.findAll('tbody tr');
    expect(rows[0]!.attributes('aria-current')).toBeUndefined();
    expect(rows[1]!.attributes('aria-current')).toBe('true');
    expect(rows[1]!.classes()).toContain('bg-muted');
  });

  it('renders the empty slot only when there are rows to fill it', () => {
    const wrapper = mount(Table, {
      props: { columns, rows: [] as Array<{ name: string }> },
      slots: { empty: '<div class="empty-state">nothing here</div>' },
    });

    expect(wrapper.get('.empty-state').text()).toBe('nothing here');

    const other = mount(Table, {
      props: { columns, rows: [{ name: 'alpha' }] },
      slots: { empty: '<div class="empty-state">nothing here</div>' },
    });
    expect(other.find('.empty-state').exists()).toBe(false);
  });

  it('keeps the loading skeleton and exposes the localized loading label', () => {
    const wrapper = mount(Table, {
      props: { columns, rows: [], loading: true, loadingRows: 2, loadingLabel: 'Loading table' },
    });

    expect(wrapper.get('table').attributes('aria-busy')).toBe('true');
    expect(wrapper.get('table').attributes('aria-label')).toBe('Loading table');
    expect(wrapper.findAll('tbody tr')).toHaveLength(2);
  });
});
