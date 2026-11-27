// @vitest-environment jsdom
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
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

  it('hides columns below the requested breakpoint on both header and data cells', () => {
    const responsiveColumns = [
      { key: 'a', label: 'A', hideBelow: 'sm' as const },
      { key: 'b', label: 'B', hideBelow: 'md' as const },
      { key: 'c', label: 'C', hideBelow: 'lg' as const },
      { key: 'd', label: 'D', hideBelow: 'wide' as const },
      { key: 'e', label: 'E', width: 'w-24', nowrap: true },
    ];
    const wrapper = mount(Table, {
      props: {
        columns: responsiveColumns,
        rows: [{ a: '1', b: '2', c: '3', d: '4', e: '5' }],
      },
    });

    const headers = wrapper.findAll('th');
    expect(headers[0]!.classes()).toContain('max-sm:hidden');
    expect(headers[1]!.classes()).toContain('max-md:hidden');
    expect(headers[2]!.classes()).toContain('max-lg:hidden');
    expect(headers[3]!.classes()).toContain('max-wide:hidden');
    expect(headers[4]!.classes()).not.toContain('max-sm:hidden');
    expect(headers[4]!.classes()).not.toContain('max-wide:hidden');
    // 列宽/nowrap 契约不变。
    expect(headers[4]!.classes()).toContain('w-24');
    expect(headers[4]!.classes()).toContain('whitespace-nowrap');

    const cells = wrapper.get('tbody tr').findAll('td');
    expect(cells[0]!.classes()).toContain('max-sm:hidden');
    expect(cells[1]!.classes()).toContain('max-md:hidden');
    expect(cells[2]!.classes()).toContain('max-lg:hidden');
    expect(cells[3]!.classes()).toContain('max-wide:hidden');
    expect(cells[4]!.classes()).not.toContain('max-wide:hidden');
    expect(cells[4]!.classes()).toContain('w-24');
    expect(cells[4]!.classes()).toContain('whitespace-nowrap');
  });

  it('applies hideBelow to loading skeleton cells too', () => {
    const responsiveColumns = [
      { key: 'a', label: 'A', hideBelow: 'sm' as const },
      { key: 'd', label: 'D', hideBelow: 'wide' as const },
    ];
    const wrapper = mount(Table, {
      props: { columns: responsiveColumns, rows: [], loading: true, loadingRows: 1 },
    });

    const headers = wrapper.findAll('th');
    expect(headers[0]!.classes()).toContain('max-sm:hidden');
    expect(headers[1]!.classes()).toContain('max-wide:hidden');

    const skeletonCells = wrapper.get('tbody tr').findAll('td');
    expect(skeletonCells[0]!.classes()).toContain('max-sm:hidden');
    expect(skeletonCells[1]!.classes()).toContain('max-wide:hidden');
    expect(skeletonCells[0]!.find('.motion-skeleton').exists()).toBe(true);
    expect(skeletonCells[0]!.find('.motion-skeleton').classes()).toContain('max-w-full');
  });

  it('keeps every hideBelow class as a static literal so Tailwind can generate it', () => {
    // Tailwind 只扫描源码中的完整类名字面量；动态 `max-${bp}:hidden` 不会被生成，
    // 因此这里用源码断言锁住四档静态类名（wide 依赖 main.css --breakpoint-wide:1239px）。
    const source = readFileSync(resolve(__dirname, 'Table.vue'), 'utf8');
    for (const className of ['max-sm:hidden', 'max-md:hidden', 'max-lg:hidden', 'max-wide:hidden']) {
      expect(source).toContain(className);
    }
  });

  it('drops its own frame when embedded in a card that already owns one', () => {
    const framed = mount(Table, { props: { columns, rows: [{ name: 'alpha' }] } });
    expect(framed.classes()).toContain('surface-card');

    const unframed = mount(Table, { props: { columns, rows: [{ name: 'alpha' }], framed: false } });
    expect(unframed.classes()).toContain('overflow-auto');
    expect(unframed.classes()).not.toContain('surface-card');
  });
});