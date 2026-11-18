<script setup lang="ts">
import { onMounted } from 'vue';
import ToastProvider from '@/components/ui/ToastProvider.vue';
import { bootstrapAppearance } from '@/design/appearance';

// Load the server-persisted appearance as soon as the app mounts: authenticated
// users get the full settings, the login page gets the public subset.
onMounted(() => {
  void bootstrapAppearance();
});
</script>

<template>
  <ToastProvider>
    <RouterView />
  </ToastProvider>
</template>
