<script setup lang="ts">
import { computed } from 'vue';
import type { ContainerServiceDto, ContainerServiceRuntimeDto } from '@/types/api';

const props = defineProps<{
  service: ContainerServiceDto | null;
  runtime?: ContainerServiceRuntimeDto | null;
}>();

const status = computed(() => props.runtime?.status || props.service?.runtimeStatus || 'unknown');
const runtimeGeneration = computed(() => props.runtime?.observedGeneration ?? props.service?.runtimeGeneration ?? null);
const runtimeRevision = computed(() => props.runtime?.observedSpecRevision || props.service?.runtimeSpecRevision || '');
const generationMismatch = computed(() => {
  if (!props.service || runtimeGeneration.value == null) return false;
  return Number(runtimeGeneration.value) !== Number(props.service.generation);
});
const revisionMismatch = computed(() => {
  if (!props.service || !runtimeRevision.value || !props.service.specRevision) return false;
  return runtimeRevision.value !== props.service.specRevision;
});

function statusColor(value: string) {
  if (value === 'healthy' || value === 'running') return 'success';
  if (value === 'starting' || value === 'stale') return 'warning';
  if (value === 'missing' || value === 'unhealthy' || value === 'exited') return 'error';
  return 'grey';
}
</script>

<template>
  <v-card variant="outlined" class="runtime-status-panel">
    <v-card-item class="py-3">
      <div class="d-flex justify-space-between align-center">
        <div>
          <v-card-title class="text-subtitle-1 font-weight-bold pa-0">Runtime Status</v-card-title>
          <v-card-subtitle class="pa-0">{{ service?.nodeName || runtime?.nodeName || 'No placement' }}</v-card-subtitle>
        </div>
        <v-chip :color="statusColor(status)" size="small" label>{{ status }}</v-chip>
      </div>
    </v-card-item>
    <v-divider />
    <v-card-text class="pa-4">
      <div class="status-grid">
        <div>
          <div class="text-caption text-medium-emphasis">DB generation</div>
          <div class="font-weight-bold font-tabular">{{ service?.generation ?? '-' }}</div>
        </div>
        <div>
          <div class="text-caption text-medium-emphasis">Runtime generation</div>
          <div class="font-weight-bold font-tabular">{{ runtimeGeneration ?? '-' }}</div>
        </div>
        <div>
          <div class="text-caption text-medium-emphasis">Spec revision</div>
          <div class="text-truncate">{{ service?.specRevision || service?.specHash || '-' }}</div>
        </div>
        <div>
          <div class="text-caption text-medium-emphasis">Observed revision</div>
          <div class="text-truncate">{{ runtimeRevision || '-' }}</div>
        </div>
      </div>

      <v-alert v-if="generationMismatch || revisionMismatch" type="warning" variant="tonal" density="compact" class="mt-4">
        Runtime labels do not match the current service spec. Reconcile will deploy the current generation.
      </v-alert>
      <v-alert v-if="service?.lastError || runtime?.error" type="error" variant="tonal" density="compact" class="mt-3">
        {{ service?.lastError || runtime?.error }}
      </v-alert>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.runtime-status-panel {
  min-width: 0;
}

.status-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}
</style>
