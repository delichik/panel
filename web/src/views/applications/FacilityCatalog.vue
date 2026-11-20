<script setup lang="ts">
import { computed, type Component } from 'vue';
import { Rocket, Wrench } from '@/icons';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import StatusBadge from '@/components/ui/StatusBadge.vue';
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
  categoryKey: string;
  status: string;
};

const props = defineProps<{
  facilities: FacilityCatalogItem[];
  loading: boolean;
  pending: string;
  facility: ReverseProxyConfig | null;
  storageShareConfig: StorageShareConfig | null;
  networkProbeConfig: NetworkProbeConfig | null;
  metricsConfig: MetricsFacilityConfig | null;
  /** 目录统计用的服务器总数：探测目标恒为“所有其他服务器”。 */
  serverTotal: number;
}>();

const emit = defineEmits<{
  open: [kind: string];
  reconcile: [kind: string];
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
        class="motion-reveal grid h-full min-w-0 content-start gap-3 surface-card p-4"
      >
        <div class="flex min-w-0 items-start gap-3">
          <div class="grid size-11 shrink-0 place-items-center rounded-control bg-muted text-foreground">
            <component :is="item.icon" class="size-5" aria-hidden="true" />
          </div>
          <div class="min-w-0 flex-1">
            <div class="flex flex-wrap items-center gap-2">
              <h3 class="m-0 text-sm font-semibold leading-5 text-foreground">{{ t(item.titleKey) }}</h3>
              <Badge tone="info">{{ t(item.categoryKey) }}</Badge>
              <StatusBadge :status="item.status" :tone="item.status === 'degraded' ? 'danger' : 'success'" :label="t(`applicationsPage.facilityStatus.${item.status}`)" />
            </div>
            <p class="m-0 mt-1 text-sm leading-6 text-muted-foreground">{{ t(item.descriptionKey) }}</p>
          </div>
        </div>

        <div class="grid grid-cols-1 gap-3 border-t border-divider pt-3 sm:grid-cols-3">
          <template v-if="item.kind === 'storage-share'">
            <FacilityStat :loading="loading" :value="storageShareConfig?.partitions.length" :label="t('applicationsPage.storageSharePartitions')" />
            <FacilityStat :loading="loading" :value="storageShareConfig?.servers.length" :label="t('applicationsPage.storageShareServers')" />
            <FacilityStat :loading="loading" :value="storageShareConfig?.servers.filter((server) => server.root).length" :label="t('applicationsPage.storageShareRoot')" />
          </template>
          <template v-else-if="item.kind === 'network-probe'">
            <FacilityStat :loading="loading" :value="probeNodeCount" :label="t('applicationsPage.networkProbeNodes')" />
            <FacilityStat :loading="loading" :value="probeTargetCount" :label="t('applicationsPage.networkProbeTargets')" />
            <FacilityStat :loading="loading" :value="probeModeLabel" :label="t('applicationsPage.networkProbeMode')" />
          </template>
          <template v-else-if="item.kind === 'metrics'">
            <FacilityStat :loading="loading" :value="metricsNodeCount" :label="t('applicationsPage.metricsNodes')" />
            <FacilityStat :loading="loading" :value="metricsModeLabel" :label="t('applicationsPage.metricsMode')" />
            <FacilityStat :loading="loading" :value="metricsConfig?.retentionDays" :label="t('applicationsPage.metricsRetention')" />
          </template>
          <template v-else>
            <FacilityStat :loading="loading" :value="facility?.routes" :label="t('applicationsPage.gatewayRoutes')" />
            <FacilityStat :loading="loading" :value="facility?.deploymentServers.length" :label="t('applicationsPage.gatewayNodes')" />
            <FacilityStat :loading="loading" :value="facility?.staticAssets.length" :label="t('applicationsPage.staticAssets')" />
          </template>
        </div>

        <div class="flex flex-wrap items-center gap-2 border-t border-divider pt-3">
          <Button size="sm" variant="primary" @click="emit('open', item.kind)"><Wrench />{{ t('applicationsPage.manageFacility') }}</Button>
          <!-- 探测/指标设施结果由节点自动上报，没有可手动 reconcile 的网关/导出。 -->
          <Button
            v-if="item.kind !== 'network-probe' && item.kind !== 'metrics'"
            size="sm"
            variant="secondary"
            :disabled="item.kind === 'storage-share' && !storageShareConfig?.enabled"
            :loading="pending === `facility-reconcile-${item.kind}`"
            @click="emit('reconcile', item.kind)"
          >
            <Rocket />{{ item.kind === 'storage-share' ? t('applicationsPage.storageShareReconcile') : t('applicationsPage.reconcileGateway') }}
          </Button>
        </div>
      </article>
    </div>
    <EmptyState v-else variant="page" :title="t('applicationsPage.emptyFacilityCatalog')" :description="t('applicationsPage.emptyFacilityCatalogHint')" />
  </div>
</template>
