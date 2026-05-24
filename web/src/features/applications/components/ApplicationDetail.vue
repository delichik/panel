<script setup lang="ts">
import type { ApplicationDto } from '@/types/api';
import ApplicationRuntimePanel from './ApplicationRuntimePanel.vue';
import ApplicationLogsPanel from './ApplicationLogsPanel.vue';

defineProps<{ application: ApplicationDto }>();
</script>

<template>
  <div class="detail-stack">
    <v-card variant="outlined" class="detail-card">
      <div class="d-flex align-start justify-space-between ga-3">
        <div class="min-width-0">
          <div class="text-subtitle-1 font-weight-bold text-truncate">{{ application.name }}</div>
          <div class="text-caption text-medium-emphasis text-truncate">{{ application.jobId }} · {{ application.namespace }}</div>
        </div>
        <v-chip :color="application.enabled ? 'success' : 'grey'" size="small" variant="tonal" label>{{ application.enabled ? 'enabled' : 'disabled' }}</v-chip>
      </div>
      <v-divider class="my-3" />
      <div class="meta-grid">
        <div><div class="text-caption text-medium-emphasis">Generation</div><div class="font-weight-bold font-tabular">{{ application.generation }}</div></div>
        <div><div class="text-caption text-medium-emphasis">Spec hash</div><div class="mono text-truncate">{{ application.specHash || '-' }}</div></div>
        <div><div class="text-caption text-medium-emphasis">Last eval</div><div class="mono text-truncate">{{ application.lastEvalId || '-' }}</div></div>
      </div>
      <v-alert v-if="application.lastError" type="error" variant="tonal" class="mt-3">{{ application.lastError }}</v-alert>
    </v-card>
    <ApplicationRuntimePanel :application="application" />
    <ApplicationLogsPanel :application="application" />
  </div>
</template>

<style scoped>
.detail-stack { display: grid; gap: 14px; }
.detail-card { padding: 16px; }
.min-width-0 { min-width: 0; }
.meta-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 0.8rem; }
</style>
