<script setup lang="ts">
import { computed } from 'vue';
import Badge from '@/components/ui/Badge.vue';
import InfoBar from '@/components/patterns/InfoBar.vue';
import PropertyList, { type PropertyListItem } from '@/components/patterns/PropertyList.vue';
import { useI18n } from '@/i18n';
import type { ServerDto } from '@/types/servers';
import { formatDateTime } from '@/utils/datetime';
import { tailscaleStatusKey, tailscaleTone, tailscaleTrait } from '../model';

const props = defineProps<{ server: ServerDto }>();
const { t } = useI18n();

/** 只读偏好：值用语义徽标表达，与页内其它只读属性一致。 */
const preferenceItems = computed<PropertyListItem[]>(() => [
  { key: 'enabled', label: t('serversPage.tailscale.enabled'), value: preferenceLabel('enabled') },
  { key: 'preferAgent', label: t('serversPage.tailscale.preferAgent'), value: preferenceLabel('preferAgent') },
  { key: 'preferInterconnect', label: t('serversPage.tailscale.preferInterconnect'), value: preferenceLabel('preferInterconnect') },
]);

const hostItems = computed<PropertyListItem[]>(() => [
  { key: 'hostname', label: t('serversPage.tailscale.hostname'), value: tailscaleTrait(props.server, 'hostname') || t('common.notAvailable') },
  { key: 'updatedAt', label: t('serversPage.tailscale.updatedAt'), value: formatDateTime(tailscaleTrait(props.server, 'updatedAt'), t('common.never')) },
  { key: 'ipv4', label: t('serversPage.tailscale.ipv4'), value: tailscaleTrait(props.server, 'ipv4') || t('common.notAvailable'), mono: true },
  { key: 'ipv6', label: t('serversPage.tailscale.ipv6'), value: tailscaleTrait(props.server, 'ipv6') || t('common.notAvailable'), mono: true },
]);

const lastError = computed(() => tailscaleTrait(props.server, 'lastError'));

function preferenceEnabled(key: string | undefined) {
  if (key === 'enabled') return true;
  if (key === 'preferAgent') return props.server.tailscalePreferAgent;
  if (key === 'preferInterconnect') return props.server.tailscalePreferInterconnect;
  return false;
}

function preferenceLabel(key: string | undefined) {
  return t(preferenceEnabled(key) ? 'serversPage.preferenceEnabled' : 'serversPage.preferenceDisabled');
}

function preferenceTone(key: string | undefined): 'success' | 'neutral' {
  return preferenceEnabled(key) ? 'success' : 'neutral';
}
</script>

<template>
  <div class="h-full min-h-0 overflow-y-auto pr-1">
    <section class="grid content-start gap-4">
      <div class="flex min-w-0 flex-wrap items-center justify-between gap-2">
        <Badge v-if="server.tailscaleEnabled" :tone="tailscaleTone(server)">{{ t(tailscaleStatusKey(server)) }}</Badge>
        <Badge v-else tone="neutral">{{ t('serversPage.tailscale.notEnabled') }}</Badge>
      </div>

      <InfoBar v-if="!server.tailscaleEnabled" tone="info">
        {{ t('serversPage.tailscale.disabledHint') }}
      </InfoBar>

      <template v-else>
        <p class="m-0 text-body text-muted-foreground">{{ t('serversPage.tailscale.applyHint') }}</p>
        <div class="surface-card overflow-hidden">
          <PropertyList :items="preferenceItems" :columns="1">
            <template #value="{ item }">
              <Badge :tone="preferenceTone(item.key)">{{ preferenceLabel(item.key) }}</Badge>
            </template>
          </PropertyList>
          <PropertyList class="border-t border-divider" :items="hostItems" :columns="2" />
        </div>
        <InfoBar v-if="lastError" tone="danger" :title="t('serversPage.tailscale.lastError')">
          <span class="break-words">{{ lastError }}</span>
        </InfoBar>
      </template>
    </section>
  </div>
</template>
