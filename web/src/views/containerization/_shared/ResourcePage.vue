<script setup lang="ts">
import ServerSelector from '@/components/ServerSelector.vue';
import PageLoadingState from '@/components/PageLoadingState.vue';
import type { ServerDto } from '@/types/api';

defineProps<{
  servers: ServerDto[];
  serverId: string;
  loadingServers: boolean;
  loading: boolean;
  error: string;
}>();

defineEmits<{ 'update:serverId': [value: string] }>();
</script>

<template>
  <div class="page-shell">
    <v-alert v-if="error" type="error" variant="tonal">{{ error }}</v-alert>
    <div class="resource-grid">
      <ServerSelector
        :model-value="serverId"
        :servers="servers"
        :loading="loadingServers"
        @update:model-value="$emit('update:serverId', $event)"
      />
      <v-card variant="outlined" class="resource-panel">
        <PageLoadingState v-if="loading" min-height="280px" />
        <slot v-else />
      </v-card>
    </div>
  </div>
</template>

<style scoped>
.resource-grid {
  display: grid;
  grid-template-columns: clamp(300px, 26vw, 340px) minmax(0, 1fr);
  flex: 1 1 auto;
  gap: 18px;
  min-height: 0;
}

.resource-panel {
  min-height: 0;
  overflow: auto;
}

@media (max-width: 1080px) {
  .resource-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 760px) {
  .resource-grid {
    flex: none;
    min-height: auto;
  }

  .resource-panel {
    overflow-x: auto;
    overflow-y: visible;
  }
}
</style>
