<script setup lang="ts">
import { onBeforeUnmount, reactive, watch } from 'vue';
import Button from '@/components/ui/Button.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import Expander from '@/components/ui/Expander.vue';
import StatusBadge from '@/components/ui/StatusBadge.vue';
import InfoBar from '@/components/patterns/InfoBar.vue';
import ActivityLink from '@/components/activity/ActivityLink.vue';
import { useErrorToast } from '@/components/ui/toast';
import { reverseProxyFacilityApi } from '@/api/facilityApps';
import type { ProxyDiagnostics, ProxyPlaneState } from '@/types/facilityApps';
import { useI18n } from '@/i18n';
import { formatDateTime } from '@/utils/datetime';
import { uncertaintyNeedsAttention } from './model';

/**
 * Proxy-plane diagnostics: per-node container observation + deployment result.
 * Route content lives in independent config records and is deliberately not
 * part of this component.
 */
const props = defineProps<{ state: ProxyPlaneState }>();
const { t } = useI18n();
const notifyError = useErrorToast();
const results = reactive<Record<string, ProxyDiagnostics>>({});
const pending = reactive<Record<string, boolean>>({});
const requests = new Map<string, AbortController>();
const knownCodes = new Set(['upstream_name_resolution_failed', 'upstream_tls_untrusted', 'upstream_connect_failed', 'upstream_timeout', 'proxy_request_error']);
function issueKey(code: string, suffix: string) { return `facilityDiagnostics.${knownCodes.has(code) ? code : 'proxy_request_error'}.${suffix}`; }
function label(prefix: string, value: string) { const key = `${prefix}.${value}`; const translated = t(key); return translated === key ? value : translated; }
function operationNeedsAttention(op: { errorClass?: string; stage?: string; status?: string }) {
  return uncertaintyNeedsAttention(op.errorClass, op.stage);
}
function cancelRequests() { for (const controller of requests.values()) controller.abort(); requests.clear(); }
watch(() => [props.state.updatedAt, ...(props.state.deploymentServers ?? [])], () => { cancelRequests(); for (const id of Object.keys(results)) delete results[id]; for (const id of Object.keys(pending)) delete pending[id]; });
onBeforeUnmount(cancelRequests);
async function check(serverId: string) {
  if (pending[serverId]) return;
  const controller = new AbortController();
  requests.set(serverId, controller);
  pending[serverId] = true;
  delete results[serverId];
  try {
    const result = await reverseProxyFacilityApi.getDiagnostics(serverId, { signal: controller.signal });
    if (!controller.signal.aborted && requests.get(serverId) === controller) results[serverId] = result;
  } catch (error) {
    if (!controller.signal.aborted) notifyError(t('facilityDiagnostics.loadFailed'), error);
  } finally {
    if (requests.get(serverId) === controller) { pending[serverId] = false; requests.delete(serverId); }
  }
}
</script>

<template>
  <section class="grid min-h-0 gap-3">
    <h3 class="m-0 text-body-strong font-semibold text-foreground">{{ t('facilityDiagnostics.title') }}</h3>
    <p class="m-0 text-caption text-muted-foreground">{{ t('facilityDiagnostics.scope') }}</p>
    <InfoBar v-if="state.reconcileStopped" tone="danger">{{ t('facilityDiagnostics.stopped') }}</InfoBar>
    <InfoBar v-if="state.lastError" tone="danger" :title="t('facilityDiagnostics.configError')">
      <p class="m-0">{{ t('facilityDiagnostics.configErrorHint') }}</p>
      <pre class="m-0 max-h-40 overflow-auto whitespace-pre-wrap break-words text-caption">{{ state.lastError }}</pre>
    </InfoBar>
    <EmptyState v-if="!state.deployments?.length" variant="inset" :title="t('facilityDiagnostics.noNodes')" :description="t('facilityDiagnostics.noNodesHint')" />
    <article v-for="node in state.deployments ?? []" :key="node.serverId" class="grid min-w-0 gap-2 border-t border-divider pt-3 first:border-t-0 first:pt-0">
      <strong class="text-body-strong font-semibold text-foreground">{{ node.serverName || node.serverId }}</strong>
      <div class="flex flex-wrap items-center gap-2">
        <span class="text-caption text-muted-foreground">{{ t('facilityDiagnostics.containerState') }}</span>
        <StatusBadge :status="node.observedState" :label="label('applicationsPage.status', node.observedState)" />
      </div>
      <span v-if="node.observedAt" class="text-caption text-muted-foreground">{{ formatDateTime(node.observedAt) }}</span>
      <template v-if="node.operation">
        <div class="flex flex-wrap items-center gap-2">
          <span class="text-caption text-muted-foreground">{{ t('applicationsPage.currentDeployment') }}</span>
          <StatusBadge :status="operationNeedsAttention(node.operation) ? 'needs_attention' : node.operation.status" domain="operation" :label="label('applicationsPage.operationStatus', operationNeedsAttention(node.operation) ? 'result_unknown' : node.operation.status)" />
        </div>
        <span v-if="node.operation.stage" class="text-caption text-muted-foreground">{{ t('applicationsPage.operationStage') }}: {{ label('applicationOperationsPage.stage', node.operation.stage) }}</span>
        <span class="text-caption text-muted-foreground">{{ t('applicationsPage.operationAttempt') }}: {{ node.operation.attempt ?? 0 }}</span>
        <span v-if="node.operation.nextRunAt" class="text-caption text-muted-foreground">{{ t('applicationsPage.operationNextRetry') }}: {{ formatDateTime(node.operation.nextRunAt) }}</span>
        <InfoBar
          v-if="operationNeedsAttention(node.operation) || (node.operation.errorCode && node.operation.errorCode !== 'execution_monitoring') || (node.operation.error && node.operation.errorClass !== 'uncertainty')"
          tone="danger"
          :title="t(operationNeedsAttention(node.operation) ? 'applicationsPage.status.result_unknown' : 'facilityDiagnostics.deploymentFailed')"
        >
          <p class="m-0">{{ t(operationNeedsAttention(node.operation) ? 'applicationsPage.unknownResultHint' : node.operation.errorCode === 'container_not_running' ? 'facilityDiagnostics.startFailedHint' : 'facilityDiagnostics.deploymentFailedHint') }}</p>
          <p class="m-0 whitespace-pre-wrap break-words">{{ node.operation.error }}</p>
          <p v-if="node.operation.errorCode === 'container_not_running' && !node.operation.errorDetail?.includes('containerLogs:')" class="m-0 text-warning">{{ t('facilityDiagnostics.missingRunLogsHint') }}</p>
          <Expander v-if="node.operation.errorDetail" :title="t('facilityDiagnostics.technicalDetails')">
            <pre class="m-0 max-h-56 overflow-auto whitespace-pre-wrap break-words text-caption">{{ node.operation.errorDetail }}</pre>
          </Expander>
        </InfoBar>
        <ActivityLink v-if="node.operation.operationId" :operation-id="node.operation.operationId" />
      </template>
      <p v-else class="m-0 text-caption text-muted-foreground">{{ t('facilityDiagnostics.noDeployment') }}</p>
      <Button size="sm" :loading="pending[node.serverId]" :disabled="pending[node.serverId]" @click="check(node.serverId)">{{ t('facilityDiagnostics.check') }}</Button>
      <div v-if="results[node.serverId]" class="grid gap-2">
        <span class="text-caption text-muted-foreground">{{ t('facilityDiagnostics.checkedAt') }}: {{ formatDateTime(results[node.serverId].checkedAt) }}</span>
        <p v-if="results[node.serverId].status === 'unavailable'" class="m-0 text-caption font-semibold text-warning">{{ t('facilityDiagnostics.unavailable') }}</p>
        <p v-else-if="!results[node.serverId].issues.length" class="m-0 text-caption text-muted-foreground">{{ t('facilityDiagnostics.noErrors') }}</p>
        <InfoBar v-for="(issue, index) in results[node.serverId].issues" :key="index" tone="warning" :title="t(issueKey(issue.code, 'title'))">
          <span v-if="issue.domain" class="block">{{ t('facilityDiagnostics.domain') }}: {{ issue.domain }}</span>
          <span v-if="issue.upstream" class="block">{{ t('facilityDiagnostics.upstream') }}: {{ issue.upstream }}</span>
          <span class="block">{{ t('facilityDiagnostics.occurrences', { count: issue.count }) }}</span>
          <span v-if="issue.lastSeen" class="block">{{ t('facilityDiagnostics.lastSeen') }}: {{ issue.lastSeen }}</span>
          <p class="m-0">{{ t(issueKey(issue.code, 'hint')) }}</p>
          <Expander v-if="issue.evidence" :title="t('facilityDiagnostics.evidence')">
            <pre class="m-0 max-h-40 overflow-auto whitespace-pre-wrap break-words text-caption">{{ issue.evidence }}</pre>
          </Expander>
        </InfoBar>
        <p v-if="results[node.serverId].truncated" class="m-0 text-caption text-muted-foreground">{{ t('facilityDiagnostics.truncated') }}</p>
      </div>
    </article>
  </section>
</template>
