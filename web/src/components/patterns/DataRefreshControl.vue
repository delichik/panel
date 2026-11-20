<script setup lang="ts">
import { computed } from 'vue';
import { useI18n } from '@/i18n';
import AutoRefreshControl from './AutoRefreshControl.vue';

const { t } = useI18n();
const options = computed(() => [
  { value: '10m' as const, label: t('autoRefresh.10m') },
  { value: '30m' as const, label: t('autoRefresh.30m') },
  { value: '1h' as const, label: t('autoRefresh.1h') },
]);
</script>

<template>
  <AutoRefreshControl :off-label="t('autoRefresh.off')" :options="options" :hint-label="t('autoRefresh.hint')" />
</template>
