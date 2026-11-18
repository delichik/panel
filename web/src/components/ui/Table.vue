<script setup lang="ts" generic="T">
type TableColumn = {
  key: string;
  label: string;
  align?: 'left' | 'right';
  width?: string;
  nowrap?: boolean;
  /**
   * 响应式列隐藏：在断点以下隐藏该列（th/td/skeleton 同步）。
   * `sm|md|lg` 走 Tailwind 内置断点；`wide` 依赖 main.css `--breakpoint-wide: 1239px`，
   * 生成 `max-wide:hidden`。
   */
  hideBelow?: 'sm' | 'md' | 'lg' | 'wide';
};

const props = withDefaults(defineProps<{
  columns: TableColumn[];
  rows: T[];
  rowKey?: string;
  /**
   * Identity of the row that is currently open in a covered detail view.
   * Matching rows get a selected fill and `aria-current="true"`.
   */
  activeKey?: string | number | null;
  loading?: boolean;
  loadingRows?: number;
  loadingLabel?: string;
  fixed?: boolean;
  /**
   * 行高模式（与 pages-misc 共享契约，勿改名）：
   * - default：固定 40px 行高 + 单行 `whitespace-nowrap` + 垂直居中，用于常规列表；
   * - auto：不固定行高（`tr` 仅保底 `min-h-10`），单元格 `whitespace-normal` + `align-top`，
   *   用于 events/logs 等多行内容行。
   */
  rowHeight?: 'default' | 'auto';
  /**
   * Render the table as its own framed card (border + radius). Set false when
   * the table is embedded in a card that already owns the frame, so the region
   * keeps a single card layer.
   */
  framed?: boolean;
}>(), {
  loading: false,
  loadingRows: 6,
  loadingLabel: '',
  fixed: false,
  activeKey: null,
  framed: true,
  rowHeight: 'default',
});

// Cell slots are addressed by column key (`#<key>`), header slots by `#head-<key>`;
// an open string index keeps page-defined slot names type-checkable.
defineSlots<Record<string, (props: { row: T; value: unknown }) => unknown>>();

const skeletonWidths = ['w-24', 'w-32', 'w-40', 'w-28', 'w-48'];

function cellValue(row: T, key: string): unknown {
  return (row as Record<string, unknown>)[key];
}

function rowId(row: T, index: number): string | number {
  if (props.rowKey) return String((row as Record<string, unknown>)[props.rowKey]);
  return index;
}

function isActive(row: T): boolean {
  if (!props.rowKey || props.activeKey == null) return false;
  return String((row as Record<string, unknown>)[props.rowKey]) === String(props.activeKey);
}

function staggerStyle(index: number): Record<string, string> {
  return { '--panel-stagger': `${Math.min(index, 6) * 20}ms` };
}

/**
 * 列级响应式隐藏 class；宽/nowrap/align 契约不受影响。
 * 使用静态映射而不是模板字符串拼接：Tailwind 只扫描源码中的完整类名字面量，
 * 动态 `` `max-${bp}:hidden` `` 不会被生成 CSS。
 */
const HIDE_BELOW_CLASS: Record<NonNullable<TableColumn['hideBelow']>, string> = {
  sm: 'max-sm:hidden',
  md: 'max-md:hidden',
  lg: 'max-lg:hidden',
  wide: 'max-wide:hidden',
};

function hideClass(column: TableColumn): string | undefined {
  return column.hideBelow ? HIDE_BELOW_CLASS[column.hideBelow] : undefined;
}

function skeletonClass(rowIndex: number, columnIndex: number, align?: 'left' | 'right') {
  return [
    'motion-skeleton h-4 rounded-control bg-hover animate-pulse',
    skeletonWidths[(rowIndex + columnIndex) % skeletonWidths.length],
    align === 'right' ? 'ml-auto' : '',
  ];
}
</script>

<template>
  <div class="min-h-0 overflow-auto" :class="framed ? 'surface-card' : undefined">
    <!-- WinUI ListView：8px 圆角列表表面、40px 行高、单元格单行不换行；
         hover/选中仅填充变化；表头 Body 14 semibold + 次级色，sticky 且与列表表面同底色。 -->
    <table class="w-full border-collapse text-body" :class="fixed ? 'table-fixed' : undefined" :aria-busy="loading ? 'true' : undefined" :aria-label="loadingLabel || undefined">
      <thead class="sticky top-0 z-10 bg-card-solid text-body-strong text-secondary-foreground">
          <tr>
            <th v-for="column in columns" :key="column.key" scope="col" class="whitespace-nowrap border-b border-divider px-3 py-2.5" :class="[column.align === 'right' ? 'text-right' : 'text-left', column.width, hideClass(column)]">
              <slot v-if="$slots[`head-${column.key}`]" :name="`head-${column.key}`" :row="undefined as unknown as T" :value="undefined" />
              <template v-else>{{ column.label }}</template>
            </th>
          </tr>
      </thead>
      <tbody class="divide-y divide-divider">
          <!--
            说明（4px 网格例外）：数据/骨架单元格使用 `px-3 py-1.5`（横向 12、纵向 6）。
            行高由 `h-10`（40px）固定，6px 纵向 padding 用来在 20px 行内文本外给
            28px 行内操作按钮/徽标留出垂直余量；这是 Table 行内的显式例外，
            不在其它组件复用（其它控件间距一律 8/12/16）。
          -->
          <template v-if="loading && !rows.length">
            <tr v-for="rowIndex in loadingRows" :key="`loading-${rowIndex}`" class="h-10">
              <td v-for="(column, columnIndex) in columns" :key="column.key" class="px-3 py-1.5" :class="hideClass(column)">
                <div :class="skeletonClass(rowIndex, columnIndex, column.align)" />
              </td>
            </tr>
          </template>
          <template v-else-if="rows.length">
            <tr v-for="(row, index) in rows" :key="rowId(row, index)" class="motion-table-row transition-colors duration-fast hover:bg-hover" :class="[rowHeight === 'auto' ? 'min-h-10' : 'h-10', isActive(row) ? 'bg-accent-selected hover:bg-accent-selected' : undefined]" :aria-current="isActive(row) ? 'true' : undefined" :style="staggerStyle(index)">
              <td v-for="column in columns" :key="column.key" class="px-3 py-1.5 text-foreground" :class="[hideClass(column), rowHeight === 'auto' ? (column.nowrap ? 'whitespace-nowrap align-top' : 'whitespace-normal align-top') : 'whitespace-nowrap align-middle', column.align === 'right' ? 'text-right' : 'text-left', column.width]">
                <slot :name="column.key" :row="row" :value="cellValue(row, column.key)">
                  {{ cellValue(row, column.key) }}
                </slot>
              </td>
            </tr>
          </template>
          <tr v-else class="text-muted-foreground">
            <!-- 空态跨全部列（含被 hideBelow 隐藏的列）；<td> 仍是表格合法结构，隐藏列不参与布局。 -->
            <td :colspan="columns.length" class="px-3 py-2">
              <slot name="empty" :row="undefined as unknown as T" :value="undefined" />
            </td>
          </tr>
      </tbody>
    </table>
  </div>
</template>
