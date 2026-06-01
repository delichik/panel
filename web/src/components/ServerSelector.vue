<script setup lang="ts">
import type { ServerDto } from '@/types/api';
import { useI18n } from '@/i18n';

defineProps<{
  modelValue: string;
  servers: ServerDto[];
  loading?: boolean;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: string];
}>();
const { t } = useI18n();

// Extract 1-min load average from raw loadAverage string (e.g. "0.15 0.08 0.02")
function getOneMinLoad(loadAverage: string | null | undefined): string {
  if (!loadAverage) return '-';
  const parts = loadAverage.trim().split(/\s+/);
  return parts[0] || '-';
}
</script>

<template>
  <v-card :loading="loading" class="server-selector d-flex flex-column h-100 overflow-hidden" variant="outlined">
    <v-card-item class="bg-surface-variant py-3 flex-shrink-0">
      <div class="d-flex justify-space-between align-center">
        <v-card-title class="text-subtitle-1 font-weight-bold my-0 py-0">{{ t('shared.serverSelector.title') }}</v-card-title>
        <v-chip size="small" color="primary">{{ servers.length }}</v-chip>
      </div>
    </v-card-item>

    <v-card-text class="flex-grow-1 overflow-y-auto pa-3">
      <div v-if="servers.length" class="server-cards d-flex flex-column" style="gap: 10px;">
        <div
          v-for="server in servers"
          :key="server.id"
          class="server-item"
          :class="{ 'selected': server.id === modelValue }"
          @click="emit('update:modelValue', server.id)"
        >
          <div class="d-flex justify-space-between align-center mb-1">
            <div class="d-flex align-center overflow-hidden">
              <span class="status-pulse" :class="server.reachable ? 'online' : 'offline'"></span>
              <span class="text-subtitle-2 font-weight-bold text-high-emphasis text-truncate">{{ server.name }}</span>
            </div>

            <div class="text-caption font-weight-bold text-medium-emphasis flex-shrink-0 ml-2">
              {{ t('shared.serverSelector.load') }}: {{ getOneMinLoad(server.loadAverage) }}
            </div>
          </div>
        </div>
      </div>
      <div v-else class="text-center py-6 text-medium-emphasis h-100 d-flex flex-column align-center justify-center">
        <v-icon size="40" color="medium-emphasis" class="mb-2">mdi-server-off</v-icon>
        <div class="text-caption">{{ t('shared.serverSelector.empty') }}</div>
      </div>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.server-selector {
  min-width: 280px;
}

/* Sleek Server List Items */
.server-item {
  position: relative;
  border: 1px solid var(--lp-border);
  border-radius: 8px;
  transition: background-color 0.2s ease, border-color 0.2s ease, box-shadow 0.2s ease, transform 0.2s ease;
  padding: 12px 14px;
  cursor: pointer;
  background: color-mix(in srgb, var(--lp-surface), transparent 18%);
}

.server-item:hover {
  transform: translateY(-1px);
  border-color: rgba(var(--v-theme-primary), 0.4);
  box-shadow: 0 4px 12px rgba(var(--v-theme-primary), 0.08);
}

.server-item.selected {
  border-color: rgb(var(--v-theme-primary));
  background-color: rgba(var(--v-theme-primary), 0.04);
  box-shadow: 0 4px 16px rgba(var(--v-theme-primary), 0.12);
}

.server-item.selected::before {
  content: '';
  position: absolute;
  left: 0;
  top: 12px;
  bottom: 12px;
  width: 4px;
  background-color: rgb(var(--v-theme-primary));
  border-top-right-radius: 4px;
  border-bottom-right-radius: 4px;
}

.server-item .status-pulse {
  width: 8px;
  height: 8px;
  margin-right: 8px;
  display: inline-block;
  border-radius: 50%;
}

.server-item .status-pulse.online {
  background-color: rgb(var(--v-theme-success));
  box-shadow: 0 0 0 0 rgba(16, 185, 129, 0.4);
  animation: pulse-green 2s infinite;
}

.server-item .status-pulse.offline {
  background-color: rgb(var(--v-theme-error));
}

@keyframes pulse-green {
  0% {
    transform: scale(0.95);
    box-shadow: 0 0 0 0 rgba(16, 185, 129, 0.7);
  }
  70% {
    transform: scale(1);
    box-shadow: 0 0 0 6px rgba(16, 185, 129, 0);
  }
  100% {
    transform: scale(0.95);
    box-shadow: 0 0 0 0 rgba(16, 185, 129, 0);
  }
}
</style>
