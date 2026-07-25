<script setup lang="ts">
import Button from '@/components/ui/Button.vue';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import EmptyState from '@/components/ui/EmptyState.vue';

defineProps<{ title: string; description: string; family: string }>();
</script>

<template>
  <ConsolePage :title="title" :description="description">
    <EmptyState :title="family" :description="$attrs['data-note'] as string || 'This route has its own entry and is ready for the page-family implementation stage.'">
      <template #actions>
        <Button size="sm">{{ family }}</Button>
      </template>
    </EmptyState>
  </ConsolePage>
</template>
