<script setup lang="ts">
import { computed, type Component } from 'vue';
import { ChevronRight } from '@/icons';
import Button from '@/components/ui/Button.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import FacilityStat from '@/components/patterns/FacilityStat.vue';
import { useI18n } from '@/i18n';
import type { ReverseProxyConfig, StorageShareConfig } from '@/types/facilityApps';
import type { MetricsFacilityConfig } from '@/types/metricsFacility';
import type { NetworkProbeConfig } from '@/types/networkProbe';

export type FacilityCatalogItem = {
  kind: string;
  icon: Component;
  titleKey: string;
  descriptionKey: string;
};

const props = defineProps<{
  facilities: FacilityCatalogItem[];
  loading: boolean;
  facility: ReverseProxyConfig | null;
  storageShareConfig: StorageShareConfig | null;
  networkProbeConfig: NetworkProbeConfig | null;
  metricsConfig: MetricsFacilityConfig | null;
  /** 目录统计用的服务器总数：探测目标恒为“所有其他服务器”。 */
  serverTotal: number;
}>();

const emit = defineEmits<{
  open: [kind: string];
}>();

const { t } = useI18n();

const probeNodeCount = computed(() => {
  const config = props.networkProbeConfig;
  if (!config) return null;
  return config.mode === 'all' ? props.serverTotal : config.serverIds.length;
});

/** 每个探测节点都向其余所有节点发起探测，目标对数 = 节点数 x (服务器总数 - 1)。 */
const probeTargetCount = computed(() => {
  const nodes = probeNodeCount.value;
  if (nodes === null) return null;
  return nodes * Math.max(0, props.serverTotal - 1);
});

const probeModeLabel = computed(() => {
  const config = props.networkProbeConfig;
  if (!config) return null;
  return t(config.mode === 'all' ? 'applicationsPage.networkProbeModeAll' : 'applicationsPage.networkProbeModeSelected');
});

/** 采集范围与探测一致：all 覆盖全部服务器，selected 只统计选中节点。 */
const metricsNodeCount = computed(() => {
  const config = props.metricsConfig;
  if (!config) return null;
  return config.mode === 'all' ? props.serverTotal : config.serverIds.length;
});

const metricsModeLabel = computed(() => {
  const config = props.metricsConfig;
  if (!config) return null;
  return t(config.mode === 'all' ? 'applicationsPage.metricsModeAll' : 'applicationsPage.metricsModeSelected');
});
</script>

<template>
  <div class="grid gap-4">
    <div v-if="facilities.length" class="motion-stagger grid gap-4 lg:grid-cols-2">
      <article
        v-for="item in facilities"
        :key="item.kind"
        class="motion-reveal flex h-full min-w-0 flex-col gap-4 surface-card p-5"
      >
        <header class="flex min-w-0 items-start gap-4">
          <div class="grid size-10 shrink-0 place-items-center rounded-control border border-brand-border bg-accent-selected text-accent-text">
            <component :is="item.icon" class="size-5" aria-hidden="true" />
          </div>
          <div class="min-w-0 flex-1">
            <h3 class="m-0 truncate text-subtitle font-semibold text-foreground">{{ t(item.titleKey) }}</h3>
            <p class="m-0 mt-1 text-body text-secondary-foreground">{{ t(item.descriptionKey) }}</p>
          </div>
        </header>

        <div class="mt-auto grid min-w-0 gap-4">
          <div class="grid grid-cols-1 gap-3 rounded-control bg-muted px-4 py-3 sm:grid-cols-3 sm:gap-0 sm:divide-x sm:divide-divider">
            <template v-if="item.kind === 'storage-share'">
              <FacilityStat class="sm:px-3 sm:first:pl-0 sm:last:pr-0" :loading="loading" :value="storageShareConfig?.partitions.length" :label="t('applicationsPage.storageSharePartitions')" />
              <FacilityStat class="sm:px-3 sm:first:pl-0 sm:last:pr-0" :loading="loading" :value="storageShareConfig?.servers.length" :label="t('applicationsPage.storageShareServers')" />
              <FacilityStat class="sm:px-3 sm:first:pl-0 sm:last:pr-0" :loading="loading" :value="storageShareConfig?.servers.filter((server) => server.root).length" :label="t('applicationsPage.storageShareRoot')" />
            </template>
            <template v-else-if="item.kind === 'network-probe'">
              <FacilityStat class="sm:px-3 sm:first:pl-0 sm:last:pr-0" :loading="loading" :value="probeNodeCount" :label="t('applicationsPage.networkProbeNodes')" />
              <FacilityStat class="sm:px-3 sm:first:pl-0 sm:last:pr-0" :loading="loading" :value="probeTargetCount" :label="t('applicationsPage.networkProbeTargets')" />
              <FacilityStat class="sm:px-3 sm:first:pl-0 sm:last:pr-0" :loading="loading" :value="probeModeLabel" :label="t('applicationsPage.networkProbeMode')" />
            </template>
            <template v-else-if="item.kind === 'metrics'">
              <FacilityStat class="sm:px-3 sm:first:pl-0 sm:last:pr-0" :loading="loading" :value="metricsNodeCount" :label="t('applicationsPage.metricsNodes')" />
              <FacilityStat class="sm:px-3 sm:first:pl-0 sm:last:pr-0" :loading="loading" :value="metricsModeLabel" :label="t('applicationsPage.metricsMode')" />
              <FacilityStat class="sm:px-3 sm:first:pl-0 sm:last:pr-0" :loading="loading" :value="metricsConfig?.retentionDays" :label="t('applicationsPage.metricsRetention')" />
            </template>
            <template v-else>
              <FacilityStat class="sm:px-3 sm:first:pl-0 sm:last:pr-0" :loading="loading" :value="facility?.routes" :label="t('applicationsPage.gatewayRoutes')" />
              <FacilityStat class="sm:px-3 sm:first:pl-0 sm:last:pr-0" :loading="loading" :value="facility?.deploymentServers.length" :label="t('applicationsPage.gatewayNodes')" />
              <FacilityStat class="sm:px-3 sm:first:pl-0 sm:last:pr-0" :loading="loading" :value="facility?.staticAssets.length" :label="t('applicationsPage.staticAssets')" />
            </template>
          </div>

          <footer class="flex items-center justify-end max-sm:flex-col max-sm:items-stretch">
            <Button
              size="md"
              variant="secondary"
              class="max-sm:w-full"
              data-testid="facility-open"
              @click="emit('open', item.kind)"
            >
              {{ t('applicationsPage.manageFacility') }}<ChevronRight />
            </Button>
          </footer>
        </div>
      </article>
    </div>
    <EmptyState v-else variant="page" :title="t('applicationsPage.emptyFacilityCatalog')" :description="t('applicationsPage.emptyFacilityCatalogHint')" />
  </div>
</template>
