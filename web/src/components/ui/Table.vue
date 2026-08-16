<script setup lang="ts" generic="T extends Record<string, unknown>">
withDefaults(defineProps<{
  columns: Array<{ key: keyof T & string; label: string; align?: 'left' | 'right'; width?: string; nowrap?: boolean }>;
  rows: T[];
  rowKey?: keyof T & string;
  loading?: boolean;
  loadingRows?: number;
  loadingLabel?: string;
  fixed?: boolean;
}>(), {
  loading: false,
  loadingRows: 6,
  loadingLabel: '',
  fixed: false,
});

const skeletonWidths = ['w-24', 'w-32', 'w-40', 'w-28', 'w-48'];

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
  <div class="min-h-0 overflow-auto rounded-xl border border-border">
    <table class="w-full border-collapse text-sm" :class="fixed ? 'table-fixed' : undefined" :aria-busy="loading ? 'true' : undefined" :aria-label="loading && loadingLabel ? loadingLabel : undefined">
      <thead class="sticky top-0 z-10 bg-muted text-xs font-semibold uppercase text-muted-foreground">
          <tr>
            <th v-for="column in columns" :key="column.key" class="border-b border-border px-3 py-2" :class="[column.align === 'right' ? 'text-right' : 'text-left', column.width, column.nowrap ? 'whitespace-nowrap' : undefined]">
              {{ column.label }}
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
          <tr v-for="(row, index) in rows" v-else :key="rowKey ? String(row[rowKey]) : index" class="motion-table-row hover:bg-accent/60" :style="staggerStyle(index)">
            <td v-for="column in columns" :key="column.key" class="px-3 py-2 text-foreground/80" :class="[column.align === 'right' ? 'text-right' : 'text-left', column.width, column.nowrap ? 'whitespace-nowrap' : undefined]">
              <slot :name="column.key" :row="row" :value="row[column.key]">
                {{ row[column.key] }}
              </slot>
            </td>
          </tr>
      </tbody>
    </table>
  </div>
</template>
