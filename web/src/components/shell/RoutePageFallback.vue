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
 */
const { t } = useI18n();
const route = useRoute();
/** Settings/form pages use the 1000px column; data pages use 1200px. */
const fallbackWidth = computed(() => (route?.meta?.pageKey === 'settings' ? 'max-w-content-narrow' : 'max-w-content'));
</script>

<template>
  <div
    class="mx-auto grid h-full min-h-0 w-full min-w-0 gap-4 p-4 max-lg:p-3"
    :class="fallbackWidth"
    aria-busy="true"
    :aria-label="t('common.loading')"
  >
    <div class="grid gap-2">
      <Skeleton class="h-7 w-48 max-w-full" />
      <Skeleton class="h-4 w-72 max-w-full" />
    </div>
    <div class="grid gap-3">
      <Skeleton class="h-10 w-full" />
      <Skeleton class="h-40 w-full" />
      <Skeleton class="h-40 w-full" />
    </div>
  </div>
</template>
