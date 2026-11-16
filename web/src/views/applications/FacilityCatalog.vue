<script setup lang="ts">
import { computed, type Component } from 'vue';
import { Rocket, Wrench } from '@lucide/vue';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import StatusBadge from '@/components/ui/StatusBadge.vue';
import { useI18n } from '@/i18n';
import type { ReverseProxyConfig, StorageShareConfig } from '@/types/facilityApps';
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
  if (!config) return '—';
  return t(config.mode === 'all' ? 'applicationsPage.networkProbeModeAll' : 'applicationsPage.networkProbeModeSelected');
});
</script>

<template>
  <div class="grid gap-4">
    <div>
      <h2 class="m-0 text-lg font-semibold text-foreground">{{ t('applicationsPage.facilityCatalogTitle') }}</h2>
      <p class="m-0 mt-1 text-sm text-muted-foreground">{{ t('applicationsPage.facilityCatalogHint') }}</p>
    </div>
    <div class="motion-stagger overflow-hidden rounded-2xl border border-border bg-card">
      <article
        v-for="item in facilities"
        :key="item.kind"
        class="motion-list-item grid gap-4 border-t border-border p-4 first:border-t-0 lg:grid-cols-[auto_minmax(0,1fr)_auto]"
      >
        <div class="grid size-12 shrink-0 place-items-center rounded-lg bg-muted/40 text-foreground">
          <component :is="item.icon" class="size-5" aria-hidden="true" />
        </div>
        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-2">
            <h3 class="m-0 text-base font-semibold text-foreground">{{ t(item.titleKey) }}</h3>
            <Badge tone="info">{{ t(item.categoryKey) }}</Badge>
            <StatusBadge :status="item.status" :tone="item.status === 'degraded' ? 'danger' : 'success'" :label="t(`applicationsPage.facilityStatus.${item.status}`)" />
          </div>
          <p class="m-0 mt-1 text-sm leading-6 text-muted-foreground">{{ t(item.descriptionKey) }}</p>
          <div class="facility-card-stats mt-3">
            <template v-if="loading">
              <div v-for="stat in 3" :key="stat" class="facility-card-stat" aria-hidden="true">
                <div class="motion-skeleton h-5 w-10 rounded bg-muted animate-pulse" />
                <div class="motion-skeleton mt-1 h-3 w-16 rounded bg-muted animate-pulse" />
              </div>
            </template>
            <template v-else-if="item.kind === 'storage-share'">
              <div class="facility-card-stat"><strong>{{ storageShareConfig?.partitions.length ?? '—' }}</strong><span>{{ t('applicationsPage.storageSharePartitions') }}</span></div>
              <div class="facility-card-stat"><strong>{{ storageShareConfig?.servers.length ?? '—' }}</strong><span>{{ t('applicationsPage.storageShareServers') }}</span></div>
              <div class="facility-card-stat"><strong>{{ storageShareConfig?.servers.filter((server) => server.root).length ?? '—' }}</strong><span>{{ t('applicationsPage.storageShareRoot') }}</span></div>
            </template>
            <template v-else-if="item.kind === 'network-probe'">
              <div class="facility-card-stat"><strong>{{ probeNodeCount ?? '—' }}</strong><span>{{ t('applicationsPage.networkProbeNodes') }}</span></div>
              <div class="facility-card-stat"><strong>{{ probeTargetCount ?? '—' }}</strong><span>{{ t('applicationsPage.networkProbeTargets') }}</span></div>
              <div class="facility-card-stat"><strong>{{ probeModeLabel }}</strong><span>{{ t('applicationsPage.networkProbeMode') }}</span></div>
            </template>
            <template v-else>
              <div class="facility-card-stat"><strong>{{ facility?.routes ?? '—' }}</strong><span>{{ t('applicationsPage.gatewayRoutes') }}</span></div>
              <div class="facility-card-stat"><strong>{{ facility?.deploymentServers.length ?? '—' }}</strong><span>{{ t('applicationsPage.gatewayNodes') }}</span></div>
              <div class="facility-card-stat"><strong>{{ facility?.staticAssets.length ?? '—' }}</strong><span>{{ t('applicationsPage.staticAssets') }}</span></div>
            </template>
          </div>
        </div>
        <div class="flex flex-wrap items-start gap-2 lg:justify-end">
          <Button size="sm" variant="primary" @click="emit('open', item.kind)"><Wrench />{{ t('applicationsPage.manageFacility') }}</Button>
          <!-- 探测设施结果由节点自动上报，没有可手动 reconcile 的网关/导出。 -->
          <Button
            v-if="item.kind !== 'network-probe'"
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
      <EmptyState v-if="!facilities.length" variant="inset" :title="t('applicationsPage.emptyFacilityCatalog')" :description="t('applicationsPage.emptyFacilityCatalogHint')" />
    </div>
  </div>
</template>

<style scoped>
.facility-card-stats {
  display: flex;
  flex-wrap: wrap;
  gap: 0.75rem 1.25rem;
}

.facility-card-stat {
  display: grid;
  gap: 0.125rem;
  min-width: 0;
}

.facility-card-stat strong {
  display: block;
  color: var(--panel-text);
  font-size: 1.125rem;
  font-weight: 700;
  line-height: 1.25;
}

.facility-card-stat span {
  color: var(--panel-text-muted);
  font-size: 0.75rem;
  line-height: 1.4;
}
</style>
