<script setup lang="ts">
import ToastProvider from '@/components/ui/ToastProvider.vue';
import { useThemeMode } from '@/design/theme';

useThemeMode();
</script>

<template>
  <ToastProvider>
    <RouterView />
  </ToastProvider>
</template>
