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

  it('marks the row matching activeKey with aria-current and a selected fill', () => {
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
    // WinUI ListView 选中行只改填充，不使用通高强调条。
    expect(rows[1]!.classes()).toContain('bg-accent-selected');
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

  it('keeps rows a fixed single-line height (no wrap-induced height changes)', () => {
    const wrapper = mount(Table, {
      props: {
        columns: [
          { key: 'name', label: 'Name' },
          { key: 'description', label: 'Description' },
        ],
        rows: [{ name: 'alpha', description: 'a very long value that must not wrap the row taller' }],
      },
    });

    const row = wrapper.get('tbody tr');
    expect(row.classes()).toContain('h-10');
    for (const cell of row.findAll('td')) {
      expect(cell.classes()).toContain('whitespace-nowrap');
      expect(cell.classes()).toContain('align-middle');
    }
  });

  it('supports the shared rowHeight="auto" multi-line row contract', () => {
    const wrapper = mount(Table, {
      props: {
        columns: [
          { key: 'message', label: 'Message' },
          { key: 'time', label: 'Time', nowrap: true },
        ],
        rows: [{ message: 'line one\nline two\nline three', time: '12:00' }],
        rowHeight: 'auto',
      },
    });

    const row = wrapper.get('tbody tr');
    expect(row.classes()).toContain('min-h-10');
    expect(row.classes()).not.toContain('h-10');

    const cells = row.findAll('td');
    // 非 nowrap 列允许换行、顶部对齐。
    expect(cells[0]!.classes()).toContain('whitespace-normal');
    expect(cells[0]!.classes()).toContain('align-top');
    expect(cells[0]!.classes()).not.toContain('whitespace-nowrap');
    // auto 模式仍尊重列级 nowrap 元数据（时间列不换行）。
    expect(cells[1]!.classes()).toContain('whitespace-nowrap');
    expect(cells[1]!.classes()).toContain('align-top');
    expect(cells[1]!.classes()).not.toContain('whitespace-normal');
    expect(cells[1]!.classes()).not.toContain('align-middle');
  });

  it('uses the WinUI list surface and Body 14 semibold sticky header', () => {
    const wrapper = mount(Table, { props: { columns, rows: [{ name: 'alpha' }] } });
    expect(wrapper.get('thead').classes()).toContain('sticky');
    expect(wrapper.get('thead').classes()).toContain('text-body-strong');
    expect(wrapper.get('thead').classes()).toContain('bg-card-solid');
    // text-body-strong 自带 600，不再手写 font-semibold。
    expect(wrapper.get('th').classes()).not.toContain('font-semibold');
  });

  it('drops its own frame when embedded in a card that already owns one', () => {
    const framed = mount(Table, { props: { columns, rows: [{ name: 'alpha' }] } });
    expect(framed.classes()).toContain('surface-card');

    const unframed = mount(Table, { props: { columns, rows: [{ name: 'alpha' }], framed: false } });
    expect(unframed.classes()).toContain('overflow-auto');
    expect(unframed.classes()).not.toContain('surface-card');
  });
});