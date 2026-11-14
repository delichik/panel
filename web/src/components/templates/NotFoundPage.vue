<script setup lang="ts">
import { useRouter } from 'vue-router';
import { useI18n } from '@/i18n';
import Button from '@/components/ui/Button.vue';
import ConsolePage from './ConsolePage.vue';
import EmptyState from '@/components/ui/EmptyState.vue';

const { t } = useI18n();
const router = useRouter();
</script>

<template>
  <ConsolePage :title="t('routes.notFound.title')" :description="t('routes.notFound.description')">
    <EmptyState :title="t('routes.notFound.emptyTitle')" :description="t('routes.notFound.emptyDescription')">
      <template #actions>
        <Button variant="primary" @click="router.push('/overview')">{{ t('routes.notFound.backToOverview') }}</Button>
      </template>
    </EmptyState>
  </ConsolePage>
</template>