<script setup lang="ts" generic="T">
const props = withDefaults(defineProps<{
  columns: Array<{ key: string; label: string; align?: 'left' | 'right'; width?: string; nowrap?: boolean }>;
  rows: T[];
  rowKey?: string;
  /**
   * Identity of the row that is currently open in a covered detail view.
   * Matching rows get a muted surface and `aria-current="true"`.
   */
  activeKey?: string | number | null;
  loading?: boolean;
  loadingRows?: number;
  loadingLabel?: string;
  fixed?: boolean;
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

function skeletonClass(rowIndex: number, columnIndex: number, align?: 'left' | 'right') {
  return [
    'motion-skeleton h-4 rounded bg-muted animate-pulse',
    skeletonWidths[(rowIndex + columnIndex) % skeletonWidths.length],
    align === 'right' ? 'ml-auto' : '',
  ];
}
</script>

<template>
  <div class="min-h-0 overflow-auto" :class="framed ? 'rounded-xl border border-border bg-card' : undefined">
    <table class="w-full border-collapse text-sm" :class="fixed ? 'table-fixed' : undefined" :aria-busy="loading ? 'true' : undefined" :aria-label="loadingLabel || undefined">
      <thead class="sticky top-0 z-10 bg-muted text-xs font-semibold uppercase tracking-wide text-secondary-foreground">
          <tr>
            <th v-for="column in columns" :key="column.key" scope="col" class="border-b border-border px-3 py-2.5" :class="[column.align === 'right' ? 'text-right' : 'text-left', column.width, column.nowrap ? 'whitespace-nowrap' : undefined]">
              <slot v-if="$slots[`head-${column.key}`]" :name="`head-${column.key}`" :row="undefined as unknown as T" :value="undefined" />
              <template v-else>{{ column.label }}</template>
            </th>
          </tr>
      </thead>
      <tbody class="divide-y divide-border">
          <template v-if="loading && !rows.length">
            <tr v-for="rowIndex in loadingRows" :key="`loading-${rowIndex}`">
              <td v-for="(column, columnIndex) in columns" :key="column.key" class="px-3 py-3">
                <div :class="skeletonClass(rowIndex, columnIndex, column.align)" />
              </td>
            </tr>
          </template>
          <template v-else-if="rows.length">
            <tr v-for="(row, index) in rows" :key="rowId(row, index)" class="motion-table-row hover:bg-accent" :class="isActive(row) ? 'bg-muted' : undefined" :aria-current="isActive(row) ? 'true' : undefined" :style="staggerStyle(index)">
              <td v-for="column in columns" :key="column.key" class="px-3 py-2.5 text-foreground" :class="[column.align === 'right' ? 'text-right' : 'text-left', column.width, column.nowrap ? 'whitespace-nowrap' : undefined]">
                <slot :name="column.key" :row="row" :value="cellValue(row, column.key)">
                  {{ cellValue(row, column.key) }}
                </slot>
              </td>
            </tr>
          </template>
          <tr v-else class="text-muted-foreground">
            <td :colspan="columns.length" class="px-3 py-2">
              <slot name="empty" :row="undefined as unknown as T" :value="undefined" />
            </td>
          </tr>
      </tbody>
    </table>
  </div>
</template>
