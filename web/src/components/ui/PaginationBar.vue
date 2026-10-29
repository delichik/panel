<script setup lang="ts">
import { ChevronLeft, ChevronRight } from '@lucide/vue';
import { computed, useSlots } from 'vue';
import { useI18n } from '@/i18n';
import Button from './Button.vue';

const props = withDefaults(defineProps<{
  page?: number;
  pageSize?: number;
  /** Omit or pass null when the total is unknown (cursor lists). Never invent 0. */
  total?: number | null;
  mode?: 'page' | 'cursor';
  hasPrevious?: boolean;
  hasNext?: boolean;
  disabled?: boolean;
  loading?: boolean;
  summaryLabel?: string;
  previousLabel: string;
  nextLabel: string;
  navLabel?: string;
}>(), {
  page: 1,
  pageSize: 20,
  total: undefined,
  mode: 'page',
  disabled: false,
  loading: false,
});

const emit = defineEmits<{
  'update:page': [value: number];
  previous: [value: number];
  next: [value: number];
}>();

const { t } = useI18n();
const slots = useSlots();

const totalKnown = computed(() => props.mode !== 'cursor' && props.total != null && Number.isFinite(props.total) && props.total >= 0);
const pageCount = computed(() => {
  if (!totalKnown.value) return 1;
  return Math.max(1, Math.ceil((props.total as number) / Math.max(1, props.pageSize)));
});
const currentPage = computed(() => Math.min(Math.max(1, props.page), pageCount.value));
const start = computed(() => {
  if (!totalKnown.value || (props.total as number) === 0) return 0;
  return (currentPage.value - 1) * props.pageSize + 1;
});
const end = computed(() => {
  if (!totalKnown.value) return 0;
  return Math.min(props.total as number, currentPage.value * props.pageSize);
});
const canPrevious = computed(() => (props.mode === 'cursor' ? props.hasPrevious : currentPage.value > 1) && !props.disabled && !props.loading);
const canNext = computed(() => (props.mode === 'cursor' ? props.hasNext : currentPage.value < pageCount.value) && !props.disabled && !props.loading);
const resolvedNavLabel = computed(() => props.navLabel || t('pagination.navLabel'));
const hasCustomSummarySlot = computed(() => Boolean(slots.summary));
const showDefaultSummary = computed(() => !hasCustomSummarySlot.value && (Boolean(props.summaryLabel) || totalKnown.value));
const showSummaryRegion = computed(() => hasCustomSummarySlot.value || showDefaultSummary.value);
const resolvedSummary = computed(() => {
  if (props.summaryLabel) return props.summaryLabel;
  if (!totalKnown.value) return '';
  return t('pagination.summary', {
    start: start.value,
    end: end.value,
    total: props.total as number,
  });
});

function goPrevious() {
  if (!canPrevious.value) return;
  const nextPage = currentPage.value - 1;
  if (props.mode !== 'cursor') emit('update:page', nextPage);
  emit('previous', nextPage);
}

function goNext() {
  if (!canNext.value) return;
  const nextPage = currentPage.value + 1;
  if (props.mode !== 'cursor') emit('update:page', nextPage);
  emit('next', nextPage);
}
</script>

<template>
  <nav class="flex min-h-12 flex-wrap items-center justify-between gap-3 border-t border-border bg-card/95 px-1 py-3" :aria-label="resolvedNavLabel">
    <p v-if="showSummaryRegion" class="m-0 text-sm text-muted-foreground">
      <slot name="summary" :start="start" :end="end" :total="total" :page="currentPage" :page-count="pageCount">
        {{ resolvedSummary }}
      </slot>
    </p>
    <span v-else class="min-w-0 flex-1" aria-hidden="true" />
    <div class="flex items-center gap-2">
      <Button size="sm" variant="secondary" :disabled="!canPrevious" :loading="loading && (mode === 'cursor' || currentPage > 1)" @click="goPrevious">
        <ChevronLeft />
        {{ previousLabel }}
      </Button>
      <span v-if="mode !== 'cursor' && totalKnown" class="min-w-16 text-center text-sm font-medium text-foreground">
        {{ currentPage }} / {{ pageCount }}
      </span>
      <Button size="sm" variant="secondary" :disabled="!canNext" :loading="loading && (mode === 'cursor' || currentPage < pageCount)" @click="goNext">
        {{ nextLabel }}
        <ChevronRight />
      </Button>
    </div>
  </nav>
</template>
