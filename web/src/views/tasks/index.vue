<script setup lang="ts">
import IntentRecordsPage from '@/views/_shared/IntentRecordsPage.vue';
</script>

<template>
  <IntentRecordsPage
    origin="system"
    title-key="routes.tasks.title"
    description-key="routes.tasks.description"
    empty-key="systemTasksPage.empty"
    empty-hint-key="systemTasksPage.emptyHint"
  />
</template>
