<script setup lang="ts">
import AppSelectorItem from '@/components/AppSelectorItem.vue';
import { useI18n } from '@/i18n';
import type { ServerDto } from '@/types/api';

defineProps<{
  server: ServerDto;
  selected?: boolean;
}>();

const emit = defineEmits<{
  select: [];
}>();

const { t } = useI18n();

function agentStatusForServer(server: ServerDto) {
  const traits = server.traits;
  if (traits?.['agent.enabled'] !== 'true' || !traits?.['agent.url']) {
    return { label: t('serversPage.agentUnavailable'), color: 'error' };
  }
  if (traits['agent.status'] === 'compatible') {
    return { label: t('serversPage.agentCompatible'), color: 'success' };
  }
  if (traits['agent.status'] === 'undeployable') {
    return { label: t('serversPage.agentUndeployable'), color: 'error' };
  }
  if (traits['agent.status'] === 'unavailable') {
    return { label: t('serversPage.agentUnavailable'), color: 'error' };
  }
  return { label: t('serversPage.agentIncompatible'), color: 'warning' };
}
</script>

<template>
  <AppSelectorItem :selected="selected" @select="emit('select')">
    <span class="server-selector-item__main min-width-0">
      <span class="server-selector-item__name text-truncate">{{ server.name }}</span>
      <span class="server-selector-item__meta text-truncate">{{ server.host }}:{{ server.port }}</span>
    </span>
    <v-chip :color="agentStatusForServer(server).color" size="x-small" variant="tonal" label>
      {{ agentStatusForServer(server).label }}
    </v-chip>
  </AppSelectorItem>
</template>

<style scoped>
.server-selector-item__main {
  display: block;
  min-width: 0;
}

.server-selector-item__name {
  display: block;
  min-width: 0;
  font-size: 0.92rem;
  font-weight: 800;
  letter-spacing: 0;
}

.server-selector-item__meta {
  display: block;
  margin-top: 3px;
  color: var(--lp-text-muted);
  font-size: 0.76rem;
  font-variant-numeric: tabular-nums;
}
</style>
