<script setup lang="ts">
import type { DependencyImpactPreviewDto } from '@/types/api';

defineProps<{
  modelValue: boolean;
  preview: DependencyImpactPreviewDto | null;
  loading?: boolean;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
  confirm: [];
}>();
</script>

<template>
  <v-dialog :model-value="modelValue" max-width="720" @update:model-value="emit('update:modelValue', $event)">
    <v-card>
      <v-card-title class="d-flex align-center justify-space-between py-3">
        <span class="font-weight-bold">Dependency Impact</span>
        <v-btn icon="mdi-close" variant="text" size="small" @click="emit('update:modelValue', false)" />
      </v-card-title>
      <v-divider />
      <v-card-text class="pa-4">
        <v-alert v-if="preview?.validationErrors?.length" type="error" variant="tonal" class="mb-4">
          {{ preview.validationErrors[0]?.message }}
        </v-alert>

        <div class="impact-grid">
          <div>
            <div class="text-caption text-medium-emphasis">Target</div>
            <div class="text-subtitle-2 font-weight-bold">{{ preview?.targetServiceName || '-' }}</div>
          </div>
          <div>
            <div class="text-caption text-medium-emphasis">Operation</div>
            <div class="text-subtitle-2 font-weight-bold">{{ preview?.operation || '-' }}</div>
          </div>
        </div>

        <v-list density="compact" class="mt-4">
          <v-list-subheader>Affected Services</v-list-subheader>
          <v-list-item
            v-for="service in preview?.affectedServices || []"
            :key="service.id"
            :title="service.name"
            :subtitle="service.enabled ? 'currently enabled' : 'currently disabled'"
          >
            <template #prepend>
              <v-icon color="primary">mdi-source-branch</v-icon>
            </template>
          </v-list-item>
          <v-list-item v-if="!(preview?.affectedServices?.length)" title="No additional services" />
        </v-list>

        <v-list density="compact" class="mt-2">
          <v-list-subheader>Expected Tasks</v-list-subheader>
          <v-list-item
            v-for="task in preview?.expectedTasks || preview?.tasks || []"
            :key="task.id"
            :title="task.summary || task.type"
            :subtitle="`${task.triggerType || 'user'} - ${task.resourceType || 'container_service'}`"
          />
          <v-list-item v-if="!((preview?.expectedTasks || preview?.tasks || []).length)" title="No task preview returned" />
        </v-list>
      </v-card-text>
      <v-divider />
      <v-card-actions class="pa-3">
        <v-spacer />
        <v-btn variant="outlined" class="text-none" @click="emit('update:modelValue', false)">Cancel</v-btn>
        <v-btn color="primary" variant="flat" class="text-none" :loading="loading" @click="emit('confirm')">Confirm</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<style scoped>
.impact-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}
</style>
