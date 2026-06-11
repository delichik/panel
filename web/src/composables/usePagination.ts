import { computed, ref, toValue, watch, type MaybeRefOrGetter } from 'vue';

export function usePagination<T>(items: MaybeRefOrGetter<readonly T[]>, initialPageSize = 20) {
  const page = ref(1);
  const pageSize = ref(initialPageSize);
  const total = computed(() => toValue(items).length);
  const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize.value)));
  const pageItems = computed(() => {
    const start = (page.value - 1) * pageSize.value;
    return toValue(items).slice(start, start + pageSize.value);
  });

  watch([total, pageSize], () => {
    if (page.value > totalPages.value) page.value = totalPages.value;
    if (page.value < 1) page.value = 1;
  });

  return {
    page,
    pageSize,
    total,
    totalPages,
    pageItems,
  };
}
