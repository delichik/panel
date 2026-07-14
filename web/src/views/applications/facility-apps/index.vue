<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { facilityAppsApi } from '@/api/facilityApps';
import { serversApi } from '@/api/servers';
import AppActionButton from '@/components/AppActionButton.vue';
import AppActionGroup from '@/components/AppActionGroup.vue';
import AppDetailPanel from '@/components/AppDetailPanel.vue';
import AppMasterDetailWorkspace from '@/components/AppMasterDetailWorkspace.vue';
import AppSelectorPanel from '@/components/AppSelectorPanel.vue';
import AppSelectorSummaryItem from '@/components/AppSelectorSummaryItem.vue';
import PageLoadingState from '@/components/PageLoadingState.vue';
import type { FacilityReverseProxyConfigDto, FacilityRouteSummaryDto, ServerDto } from '@/types/api';
import { useI18n } from '@/i18n';

const router = useRouter();
const { t, formatDateTime, translateLifecycleStage, translateRuntimeDesiredState, translateRuntimeStatus } = useI18n();
const loading = ref(true);
const reconciling = ref(false);
const error = ref('');
const message = ref('');
const snackbar = ref(false);
const config = ref<FacilityReverseProxyConfigDto | null>(null);
const servers = ref<ServerDto[]>([]);

const enabledServerNames = computed(() => (config.value?.deploymentServers ?? [])
  .map((id) => servers.value.find((server) => server.id === id)?.name ?? id)
  .join(', '));
const lifecycleTargets = computed(() => config.value?.operation?.targets ?? []);
const routeGroups = computed(() => {
  const groups = new Map<string, FacilityRouteSummaryDto[]>();
  for (const route of config.value?.routeSummaries ?? []) {
    const items = groups.get(route.domain) ?? [];
    const existing = items.find((item) => item.path === route.path && item.source === route.source && item.applicationId === route.applicationId);
    if (existing) {
      existing.serverIds = [...new Set([...existing.serverIds, ...route.serverIds])].sort();
    } else {
      items.push({ ...route, serverIds: [...route.serverIds] });
    }
    groups.set(route.domain, items);
  }
  return [...groups.entries()].map(([domain, routes]) => ({ domain, routes })).sort((a, b) => a.domain.localeCompare(b.domain));
});

async function load() {
  loading.value = true;
  try {
    const [nextConfig, nextServers] = await Promise.all([facilityAppsApi.reverseProxy(), serversApi.listServers()]);
    config.value = nextConfig;
    servers.value = nextServers;
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('facilityAppsPage.loadFailed');
  } finally {
    loading.value = false;
  }
}

async function reconcile() {
  reconciling.value = true;
  try {
    const result = await facilityAppsApi.reconcileReverseProxy();
    config.value = result.config;
    message.value = t('facilityAppsPage.syncRequested');
    snackbar.value = true;
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('facilityAppsPage.syncFailed');
  } finally {
    reconciling.value = false;
  }
}

function openConfiguration() {
  void router.push('/applications/facility-apps/reverse-proxy/config');
}

function openApplication(applicationId?: string) {
  if (!applicationId) return;
  void router.push({ path: '/applications/apps', query: { application: applicationId } });
}

function runtimeStatusColor(status?: string | null) {
  if (status === 'running' || status === 'deployed') return 'success';
  if (status === 'failed' || status === 'partially_deployed') return 'error';
  if (status === 'pending' || status === 'preparing' || status === 'deploying' || status === 'missing') return 'warning';
  return 'secondary';
}

function routeSourceLabel(route: FacilityRouteSummaryDto) {
  if (route.source === 'application') return route.applicationName || t('facilityAppsPage.applicationRoute');
  if (route.source === 'system_panel') return t('facilityAppsPage.panelEntry');
  return t('facilityAppsPage.facilityRoute');
}

onMounted(load);
</script>

<template>
  <div class="page-shell">
    <v-alert v-if="error" type="error" variant="tonal">{{ error }}</v-alert>
    <PageLoadingState v-if="loading" />
    <AppMasterDetailWorkspace v-else>
      <template #aside>
        <AppSelectorPanel
          class="facility-list"
          :title="t('facilityAppsPage.title')"
          :empty="false"
          empty-icon="mdi-application-cog-outline"
          :empty-text="t('facilityAppsPage.noRouteSummaries')"
        >
          <AppSelectorSummaryItem
            selected
            :title="t('facilityAppsPage.reverseProxy')"
            :subtitle="t('facilityAppsPage.reverseProxyHint')"
            :status="config?.deploymentServers?.length ? 'success' : 'grey'"
          />
        </AppSelectorPanel>
      </template>

      <AppDetailPanel>
        <template #header>
          <div class="min-width-0">
            <div class="text-h6 font-weight-bold text-truncate">{{ t('facilityAppsPage.reverseProxy') }}</div>
            <div class="text-body-2 text-medium-emphasis text-truncate">
              {{ enabledServerNames || t('facilityAppsPage.noEnabledServers') }}
            </div>
            <div class="facility-statuses">
              <v-chip :color="config?.deploymentServers?.length ? 'success' : 'grey'" size="small" variant="tonal" label>
                {{ config?.deploymentServers?.length ? t('common.enabled') : t('common.disabled') }}
              </v-chip>
              <v-chip size="small" variant="tonal" label>{{ t('facilityAppsPage.gatewayNodes') }} {{ config?.deploymentServers?.length ?? 0 }}</v-chip>
            </div>
          </div>
          <AppActionGroup context="detail" class="app-detail-actions">
            <AppActionButton icon="mdi-refresh" :label="t('facilityAppsPage.syncNow')" :loading="reconciling" @click="reconcile" />
            <AppActionButton kind="primary" icon="mdi-cog-outline" :label="t('facilityAppsPage.configureReverseProxy')" @click="openConfiguration" />
          </AppActionGroup>
        </template>

        <template #body>
          <v-alert v-if="config?.lastError" type="warning" variant="tonal" density="compact">{{ config.lastError }}</v-alert>
          <div class="facility-summary">
            <div><span>{{ t('facilityAppsPage.enabledServers') }}</span><strong>{{ config?.deploymentServers?.length ?? 0 }}</strong></div>
            <div><span>{{ t('facilityAppsPage.proxyRoutes') }}</span><strong>{{ config?.routes ?? 0 }}</strong></div>
            <div><span>{{ t('common.updatedAt') }}</span><strong>{{ config?.updatedAt ? formatDateTime(config.updatedAt) : t('common.never') }}</strong></div>
          </div>

          <section class="detail-section">
            <div class="section-title">{{ t('facilityAppsPage.routeOverview') }}</div>
            <div v-if="!routeGroups.length" class="empty-inline">{{ t('facilityAppsPage.noRouteSummaries') }}</div>
            <div v-for="group in routeGroups" :key="group.domain" class="route-domain-summary">
              <div class="route-domain-summary__title">{{ group.domain }}</div>
              <div v-for="route in group.routes" :key="`${route.source}:${route.applicationId || ''}:${route.path}`" class="route-summary-row">
                <span class="mono">{{ route.path }}</span>
                <v-chip size="small" variant="tonal" label>{{ routeSourceLabel(route) }}</v-chip>
                <span class="text-caption text-medium-emphasis">{{ route.serverIds.length }} {{ t('facilityAppsPage.gatewayNodes') }}</span>
                <AppActionButton
                  v-if="route.source === 'application' && route.applicationId"
                  kind="plain"
                  icon="mdi-open-in-new"
                  :label="t('facilityAppsPage.openApplication')"
                  @click="openApplication(route.applicationId)"
                />
              </div>
            </div>
          </section>

          <section class="detail-section">
            <div class="section-title">{{ t('facilityAppsPage.deploymentRecords') }}</div>
            <div v-if="!config?.operation" class="empty-inline">{{ t('facilityAppsPage.noDeploymentRecords') }}</div>
            <div v-else class="deployment-target-list">
              <div v-for="target in lifecycleTargets" :key="target.id" class="deployment-target-row">
                <div class="deployment-target-row__top">
                  <div class="min-width-0">
                    <div class="font-weight-bold text-truncate">{{ target.serverName || target.serverId }}</div>
                    <div v-if="target.serverName" class="text-caption text-medium-emphasis mono text-truncate">{{ target.serverId }}</div>
                  </div>
                  <v-chip :color="runtimeStatusColor(target.status)" size="small" variant="tonal" label>{{ translateRuntimeStatus(target.status) }}</v-chip>
                </div>
                <div class="deployment-target-row__meta">
                  <span>{{ translateRuntimeDesiredState(target.desiredState) }}</span>
                  <span>{{ target.stage ? translateLifecycleStage(target.stage) : t('common.notAvailable') }}</span>
                  <span>{{ formatDateTime(target.finishedAt || target.updatedAt) }}</span>
                </div>
                <div v-if="target.error" class="text-caption text-error">{{ target.error }}</div>
              </div>
            </div>
          </section>
        </template>
      </AppDetailPanel>
    </AppMasterDetailWorkspace>
    <v-snackbar v-model="snackbar">{{ message }}</v-snackbar>
  </div>
</template>

<style scoped>
.facility-list { min-width: 0; min-height: 0; overflow: hidden; }
.facility-statuses { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 8px; }
.facility-summary { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px; }
.facility-summary > div { display: grid; gap: 3px; padding: 11px 12px; border-radius: var(--lp-radius-sm); background: color-mix(in srgb, var(--lp-surface-container), transparent 35%); }
.facility-summary span { color: rgb(var(--v-theme-on-surface-variant)); font-size: .78rem; }
.facility-summary strong { font-size: 1rem; }
.detail-section { display: grid; gap: 10px; }
.section-title { font-weight: 700; }
.route-domain-summary { display: grid; gap: 6px; padding: 10px 0; border-top: 1px solid color-mix(in srgb, var(--lp-border), transparent 35%); }
.route-domain-summary__title { font-weight: 700; }
.route-summary-row { display: grid; grid-template-columns: minmax(90px, .7fr) auto minmax(110px, 1fr) auto; align-items: center; gap: 10px; min-width: 0; padding: 8px 10px; border-radius: var(--lp-radius-sm); background: color-mix(in srgb, var(--lp-surface-container), transparent 48%); }
.deployment-target-list { display: grid; gap: 8px; }
.deployment-target-row { display: grid; gap: 6px; padding: 10px 12px; border-radius: var(--lp-radius-sm); background: color-mix(in srgb, var(--lp-surface-container), transparent 45%); }
.deployment-target-row__top { display: flex; align-items: flex-start; justify-content: space-between; gap: 10px; }
.deployment-target-row__meta { display: flex; flex-wrap: wrap; gap: 8px 14px; color: rgb(var(--v-theme-on-surface-variant)); font-size: .78rem; }
.empty-inline { padding: 16px; color: rgb(var(--v-theme-on-surface-variant)); text-align: center; }
.mono { font-family: var(--lp-font-mono); }

@media (max-width: 760px) {
  .facility-summary { grid-template-columns: 1fr; }
  .route-summary-row { grid-template-columns: 1fr auto; }
  .route-summary-row > :nth-child(3) { grid-column: 1 / -1; }
}
</style>
