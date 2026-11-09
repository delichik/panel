<script setup lang="ts">
import Skeleton from '@/components/ui/Skeleton.vue';
import { useI18n } from '@/i18n';

/**
 * Shown only while a page chunk is still downloading. Navigation itself already
 * completed, so the shell title/nav can update immediately; this keeps the
 * content area from freezing on the previous page.
 */
const { t } = useI18n();
</script>

<template>
  <div class="grid h-full min-h-0 gap-4 p-4 lg:p-6" aria-busy="true" :aria-label="t('common.loading')">
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
