<script setup lang="ts">
import { onMounted } from 'vue';
import ToastProvider from '@/components/ui/ToastProvider.vue';
import ErrorDialogHost from '@/components/ui/ErrorDialogHost.vue';
import { bootstrapAppearance } from '@/design/appearance';

// Load the server-persisted appearance as soon as the app mounts: authenticated
// users get the full settings, the login page gets the public subset.
onMounted(() => {
  void bootstrapAppearance();
});
</script>

<template>
  <ToastProvider>
    <!-- provide 只对后代生效：宿主必须包裹 RouterView，否则路由视图内的
         useErrorDialog() 拿不到注入会直接抛错。 -->
    <ErrorDialogHost>
      <RouterView />
    </ErrorDialogHost>
  </ToastProvider>
</template>
