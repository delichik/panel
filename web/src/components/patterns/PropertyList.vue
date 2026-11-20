<script setup lang="ts">
import { computed, ref } from 'vue';
import { Check } from '@/icons';
import Button from '@/components/ui/Button.vue';

/**
 * WinUI 3 只读属性列表：Caption label（12，muted）+ Body value（14，foreground）。
 * 与 DetailSection 组合使用：PropertyList 作为分区行内容；单列时自带
 * `divide-y divide-divider` 行分隔线，2/3 列时改为网格（避免多列下分隔线错位）。
 *
 * 值支持三种呈现：纯文本（可 `mono` 等宽用于路径/指纹）、链接（`href`，`external`
 * 时新窗口打开）、复制按钮（`copyable` + `copyLabel`/`copiedLabel`，由页面传入 i18n
 * 文案）。`#value` 具名插槽可整体接管值渲染。
 */
export type PropertyListItem = {
  /** 稳定 key（缺省用索引）；复制态按此 key 记忆。 */
  key?: string;
  label: string;
  value?: string | number | null;
  /** 值作为链接渲染。 */
  href?: string;
  /** 外部链接：新窗口打开并加 rel。 */
  external?: boolean;
  /** 等宽字体（路径、指纹、容器 ID 等）。 */
  mono?: boolean;
  /** 值截断时的 title 提示。 */
  title?: string;
  /** 渲染复制按钮（需同时提供 copyLabel）。 */
  copyable?: boolean;
}

const props = withDefaults(defineProps<{
  items: PropertyListItem[];
  /** 列数：1（带分隔线）/ 2 / 3（响应式折叠为 1 列）。 */
  columns?: 1 | 2 | 3;
  /** 无 value 时的占位（默认断线符）。 */
  emptyText?: string;
  /** 复制按钮文案；缺省时不渲染复制按钮。 */
  copyLabel?: string;
  /** 复制成功后的按钮文案。 */
  copiedLabel?: string;
}>(), {
  columns: 2,
  emptyText: '—',
  copyLabel: '',
  copiedLabel: '',
});

const emit = defineEmits<{ copy: [value: string] }>();

const columnsClass = computed(() => {
  if (props.columns === 1) return 'grid-cols-1 divide-y divide-divider';
  if (props.columns === 3) return 'grid-cols-1 gap-x-4 sm:grid-cols-2 xl:grid-cols-3 max-sm:grid-cols-1';
  return 'grid-cols-1 gap-x-4 sm:grid-cols-2 max-sm:grid-cols-1';
});

const copiedKey = ref<string | null>(null);

function rowKey(item: PropertyListItem, index: number) {
  return item.key ?? String(index);
}

function displayValue(item: PropertyListItem) {
  if (item.value === null || item.value === undefined || item.value === '') return props.emptyText;
  return String(item.value);
}

async function copyValue(item: PropertyListItem, index: number) {
  const text = item.value === null || item.value === undefined ? '' : String(item.value);
  try {
    await navigator.clipboard?.writeText(text);
    copiedKey.value = rowKey(item, index);
    emit('copy', text);
  } catch {
    // 剪贴板不可用：保持原文案，由页面决定失败反馈（就地/弹窗），不在组件内造文案。
  }
}
</script>

<template>
  <dl v-if="items.length" data-property-list :data-columns="columns" class="m-0 grid min-w-0" :class="columnsClass">
    <div
      v-for="(item, index) in items"
      :key="rowKey(item, index)"
      data-property-row
      class="grid min-w-0 gap-1 px-4 py-3"
    >
      <dt class="m-0 min-w-0 break-words text-caption text-muted-foreground">{{ item.label }}</dt>
      <dd class="m-0 flex min-w-0 flex-wrap items-center gap-2 break-words text-body text-foreground">
        <slot name="value" :item="item" :index="index" :text="displayValue(item)">
          <a
            v-if="item.href"
            :href="item.href"
            :target="item.external ? '_blank' : undefined"
            :rel="item.external ? 'noreferrer' : undefined"
            class="min-w-0 break-all text-accent-text hover:underline"
            :title="item.title"
          >{{ displayValue(item) }}</a>
          <span
            v-else
            class="min-w-0 break-all"
            :class="item.mono ? 'panel-mono' : undefined"
            :title="item.title ?? (typeof item.value === 'string' ? item.value : undefined)"
          >{{ displayValue(item) }}</span>
        </slot>
        <Button
          v-if="item.copyable && copyLabel"
          size="sm"
          variant="ghost"
          class="shrink-0"
          :title="copyLabel"
          @click="copyValue(item, index)"
        >
          <Check v-if="copiedKey === rowKey(item, index)" />
          {{ copiedKey === rowKey(item, index) && copiedLabel ? copiedLabel : copyLabel }}
        </Button>
      </dd>
    </div>
  </dl>
  <p v-else data-property-list-empty class="m-0 px-4 py-3 text-caption text-muted-foreground">{{ emptyText }}</p>
</template>
