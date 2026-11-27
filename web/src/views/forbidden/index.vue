<script setup lang="ts">
import { useRouter } from 'vue-router';
import Button from '@/components/ui/Button.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import { useI18n } from '@/i18n';

const { t } = useI18n();
const router = useRouter();
</script>

<template>
  <ConsolePage :title="t('routes.forbidden.title')" width="narrow">
    <EmptyState :title="t('forbidden.title')" :description="t('forbidden.description')">
      <Button variant="primary" @click="router.push('/overview')">{{ t('forbidden.back') }}</Button>
    </EmptyState>
  </ConsolePage>
</template>
