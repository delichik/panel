<script setup lang="ts">
import IntentRecordsPage from '@/views/_shared/IntentRecordsPage.vue';
</script>

<template>
  <IntentRecordsPage
    origin="user"
    title-key="routes.operations.title"
    description-key="routes.operations.description"
    empty-key="operationsPage.empty"
    empty-hint-key="operationsPage.emptyHint"
  />
</template>
