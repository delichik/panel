<script setup lang="ts">
import { ChevronLeft, ChevronRight } from '@lucide/vue';
import { computed } from 'vue';
import { useI18n } from '@/i18n';
import Button from './Button.vue';

const props = withDefaults(defineProps<{
  page: number;
  pageSize: number;
  total: number;
  disabled?: boolean;
  loading?: boolean;
  summaryLabel?: string;
  previousLabel: string;
  nextLabel: string;
  navLabel?: string;
}>(), {
  disabled: false,
  loading: false,
});

const emit = defineEmits<{
  'update:page': [value: number];
  previous: [value: number];
  next: [value: number];
}>();

const { t } = useI18n();

const pageCount = computed(() => Math.max(1, Math.ceil(props.total / Math.max(1, props.pageSize))));
const currentPage = computed(() => Math.min(Math.max(1, props.page), pageCount.value));
const start = computed(() => props.total === 0 ? 0 : (currentPage.value - 1) * props.pageSize + 1);
const end = computed(() => Math.min(props.total, currentPage.value * props.pageSize));
const canPrevious = computed(() => currentPage.value > 1 && !props.disabled && !props.loading);
const canNext = computed(() => currentPage.value < pageCount.value && !props.disabled && !props.loading);
const resolvedNavLabel = computed(() => props.navLabel || t('pagination.navLabel'));
const resolvedSummary = computed(() => props.summaryLabel || t('pagination.summary', { start: start.value, end: end.value, total: props.total }));

function goPrevious() {
  if (!canPrevious.value) return;
  const nextPage = currentPage.value - 1;
  emit('update:page', nextPage);
  emit('previous', nextPage);
}

function goNext() {
  if (!canNext.value) return;
  const nextPage = currentPage.value + 1;
  emit('update:page', nextPage);
  emit('next', nextPage);
}
</script>

<template>
  <nav class="flex min-h-12 flex-wrap items-center justify-between gap-3 border-t border-border bg-background/95 px-1 py-3" :aria-label="resolvedNavLabel">
    <p class="m-0 text-sm text-muted-foreground">
      <slot name="summary" :start="start" :end="end" :total="total" :page="currentPage" :page-count="pageCount">
        {{ resolvedSummary }}
      </slot>
    </p>
    <div class="flex items-center gap-2">
      <Button size="sm" variant="secondary" :disabled="!canPrevious" :loading="loading && currentPage > 1" @click="goPrevious">
        <ChevronLeft />
        {{ previousLabel }}
      </Button>
      <span class="min-w-16 text-center text-sm font-medium text-foreground">
        {{ currentPage }} / {{ pageCount }}
      </span>
      <Button size="sm" variant="secondary" :disabled="!canNext" :loading="loading && currentPage < pageCount" @click="goNext">
        {{ nextLabel }}
        <ChevronRight />
      </Button>
    </div>
  </nav>
</template>