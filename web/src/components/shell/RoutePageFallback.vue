<script setup lang="ts">
import { computed } from 'vue';
import { useRoute } from 'vue-router';
import Skeleton from '@/components/ui/Skeleton.vue';
import { useI18n } from '@/i18n';

/**
 * Shown only while a page chunk is still downloading. Navigation itself already
 * completed, so the shell title/nav can update immediately; this keeps the
 * content area from freezing on the previous page. The skeleton mirrors the
 * ConsolePage page geometry (same content column width + page padding) so the
 * lazy chunk swap does not shrink the page from full width to the 1200px column.
 * When the target route declares `meta.titleKey`, render the real localized title
 * and description immediately rather than drawing grey blocks over static copy.
 */
const { t } = useI18n();
const route = useRoute();
/** Settings/form pages use the 1000px column; data pages use 1200px. */
const isSettings = computed(() => route?.meta?.pageKey === 'settings');
const fallbackWidth = computed(() => (isSettings.value ? 'max-w-content-narrow' : 'max-w-content'));
const titleKey = computed(() => (typeof route?.meta?.titleKey === 'string' ? route.meta.titleKey : ''));
const descriptionKey = computed(() => {
  if (!titleKey.value.endsWith('.title')) return '';
  const candidate = `${titleKey.value.slice(0, -'.title'.length)}.description`;
  const translated = t(candidate);
  return translated === candidate ? '' : translated;
});
</script>

<template>
  <div
    class="mx-auto grid h-full min-h-0 w-full min-w-0 content-start gap-3 p-4 max-lg:p-3 max-sm:gap-3"
    :class="fallbackWidth"
    aria-busy="true"
    :aria-label="t('common.loading')"
  >
    <header class="min-w-0">
      <template v-if="titleKey">
        <h1 tabindex="-1" class="m-0 text-balance break-words text-title font-semibold tracking-tight text-foreground focus:outline-none">
          {{ t(titleKey) }}
        </h1>
        <p v-if="descriptionKey" class="m-0 mt-0.5 max-w-3xl break-words text-body text-secondary-foreground">
          {{ descriptionKey }}
        </p>
      </template>
      <div v-else class="grid gap-1.5">
        <Skeleton class="h-7 w-48 max-w-full" />
        <Skeleton class="h-4 w-72 max-w-full max-sm:w-full" />
      </div>
    </header>

    <div v-if="isSettings" class="grid gap-3">
      <div class="flex gap-2 overflow-hidden border-b border-divider pb-2">
        <Skeleton v-for="item in 5" :key="item" class="h-6 w-20 shrink-0" />
      </div>
      <div class="surface-card divide-y divide-divider overflow-hidden">
        <div v-for="row in 4" :key="row" class="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
          <Skeleton class="h-4 w-36 max-w-full" />
          <Skeleton class="h-8 w-44 max-w-full" />
        </div>
      </div>
    </div>

    <div v-else class="grid gap-3">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <Skeleton class="h-8 w-64 max-w-full" />
        <div class="flex gap-2">
          <Skeleton class="h-8 w-20" />
          <Skeleton class="h-8 w-20" />
        </div>
      </div>
      <div class="surface-card divide-y divide-divider overflow-hidden">
        <div v-for="row in 6" :key="row" class="grid grid-cols-3 gap-4 px-3 py-2.5">
          <Skeleton class="h-4 w-32 max-w-full" />
          <Skeleton class="h-4 w-24 max-w-full" />
          <Skeleton class="ml-auto h-4 w-16 max-w-full" />
        </div>
      </div>
    </div>
  </div>
</template>
