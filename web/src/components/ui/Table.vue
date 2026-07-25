<script setup lang="ts" generic="T extends Record<string, unknown>">
defineProps<{
  columns: Array<{ key: keyof T & string; label: string; align?: 'left' | 'right' }>;
  rows: T[];
  rowKey?: keyof T & string;
}>();
</script>

<template>
  <div class="overflow-hidden rounded-xl border border-border">
    <div class="max-h-full overflow-auto">
      <table class="w-full border-collapse text-sm">
        <thead class="sticky top-0 z-10 bg-muted text-xs font-semibold uppercase text-muted-foreground">
          <tr>
            <th v-for="column in columns" :key="column.key" class="border-b border-border px-3 py-2" :class="column.align === 'right' ? 'text-right' : 'text-left'">
              {{ column.label }}
            </th>
          </tr>
        </thead>
        <tbody class="divide-y divide-border bg-background">
          <tr v-for="(row, index) in rows" :key="rowKey ? String(row[rowKey]) : index" class="motion-table-row hover:bg-accent/60">
            <td v-for="column in columns" :key="column.key" class="px-3 py-2 text-foreground/80" :class="column.align === 'right' ? 'text-right' : 'text-left'">
              <slot :name="column.key" :row="row" :value="row[column.key]">
                {{ row[column.key] }}
              </slot>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
