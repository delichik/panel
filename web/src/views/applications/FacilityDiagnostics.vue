<script setup lang="ts">
import { onBeforeUnmount, reactive, watch } from 'vue';
import Button from '@/components/ui/Button.vue';
import StatusBadge from '@/components/ui/StatusBadge.vue';
import ActivityLink from '@/components/activity/ActivityLink.vue';
import { useErrorToast } from '@/components/ui/toast';
import { reverseProxyFacilityApi } from '@/api/facilityApps';
import type { ProxyDiagnostics, ReverseProxyConfig } from '@/types/facilityApps';
import { useI18n } from '@/i18n';
import { formatDateTime } from '@/utils/datetime';

const props = defineProps<{ config: ReverseProxyConfig }>();
const { t } = useI18n();
const notifyError = useErrorToast();
const results = reactive<Record<string, ProxyDiagnostics>>({});
const pending = reactive<Record<string, boolean>>({});
const requests = new Map<string, AbortController>();
const knownCodes = new Set(['upstream_name_resolution_failed', 'upstream_tls_untrusted', 'upstream_connect_failed', 'upstream_timeout', 'proxy_request_error']);
function issueKey(code: string, suffix: string) { return `facilityDiagnostics.${knownCodes.has(code) ? code : 'proxy_request_error'}.${suffix}`; }
function label(prefix: string, value: string) { const key = `${prefix}.${value}`; const translated = t(key); return translated === key ? value : translated; }
function cancelRequests() { for (const controller of requests.values()) controller.abort(); requests.clear(); }
watch(() => props.config.version, () => { cancelRequests(); for (const id of Object.keys(results)) delete results[id]; for (const id of Object.keys(pending)) delete pending[id]; });
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
  <section class="grid min-h-0 gap-3 text-sm">
    <h3 class="m-0">{{ t('facilityDiagnostics.title') }}</h3>
    <p class="m-0 text-muted-foreground">{{ t('facilityDiagnostics.scope') }}</p>
    <p v-if="config.reconcileStopped" role="alert" class="m-0 text-danger">{{ t('facilityDiagnostics.stopped') }}</p>
    <div v-if="config.lastError" role="alert" class="grid gap-2 rounded-xl border border-danger-border bg-danger-bg p-3 text-danger">
      <strong>{{ t('facilityDiagnostics.configError') }}</strong>
      <p class="m-0">{{ t('facilityDiagnostics.configErrorHint') }}</p>
      <pre class="m-0 max-h-40 overflow-auto whitespace-pre-wrap break-words text-xs">{{ config.lastError }}</pre>
    </div>
    <article v-for="node in config.deployments ?? []" :key="node.serverId" class="grid min-w-0 gap-2 rounded-xl border border-border p-3">
      <strong>{{ node.serverName || node.serverId }}</strong>
      <div class="flex flex-wrap items-center gap-2">
        <span>{{ t('facilityDiagnostics.containerState') }}</span>
        <StatusBadge :status="node.observedState" :label="label('applicationsPage.status', node.observedState)" />
      </div>
      <span v-if="node.observedAt" class="text-xs text-muted-foreground">{{ formatDateTime(node.observedAt) }}</span>
      <template v-if="node.operation">
        <div class="flex flex-wrap items-center gap-2">
          <span>{{ t('applicationsPage.currentDeployment') }}</span>
          <StatusBadge :status="node.operation.errorClass === 'uncertainty' ? 'needs_attention' : node.operation.status" domain="operation" :label="label('applicationsPage.operationStatus', node.operation.errorClass === 'uncertainty' ? 'result_unknown' : node.operation.status)" />
        </div>
        <span v-if="node.operation.stage">{{ t('applicationsPage.operationStage') }}: {{ label('applicationOperationsPage.stage', node.operation.stage) }}</span>
        <span>{{ t('applicationsPage.operationAttempt') }}: {{ node.operation.attempt ?? 0 }}</span>
        <span v-if="node.operation.nextRunAt">{{ t('applicationsPage.operationNextRetry') }}: {{ formatDateTime(node.operation.nextRunAt) }}</span>
        <div v-if="node.operation.errorCode || node.operation.error" role="alert" class="grid gap-2 text-danger">
          <strong>{{ t('facilityDiagnostics.deploymentFailed') }}</strong>
          <p class="m-0">{{ t(node.operation.errorClass === 'uncertainty' ? 'applicationsPage.unknownResultHint' : node.operation.errorCode === 'container_not_running' ? 'facilityDiagnostics.startFailedHint' : 'facilityDiagnostics.deploymentFailedHint') }}</p>
          <p class="m-0 whitespace-pre-wrap break-words">{{ node.operation.error }}</p>
          <details v-if="node.operation.errorDetail"><summary class="cursor-pointer">{{ t('facilityDiagnostics.technicalDetails') }}</summary><pre class="mt-2 max-h-56 overflow-auto whitespace-pre-wrap break-words text-xs">{{ node.operation.errorDetail }}</pre></details>
        </div>
        <ActivityLink v-if="node.operation.operationId" :operation-id="node.operation.operationId" />
      </template>
      <p v-else class="m-0 text-muted-foreground">{{ t('facilityDiagnostics.noDeployment') }}</p>
      <Button size="sm" :loading="pending[node.serverId]" :disabled="pending[node.serverId]" @click="check(node.serverId)">{{ t('facilityDiagnostics.check') }}</Button>
      <div v-if="results[node.serverId]" class="grid gap-2">
        <span class="text-xs text-muted-foreground">{{ t('facilityDiagnostics.checkedAt') }}: {{ formatDateTime(results[node.serverId].checkedAt) }}</span>
        <p v-if="results[node.serverId].status === 'unavailable'" class="m-0 text-warning">{{ t('facilityDiagnostics.unavailable') }}</p>
        <p v-else-if="!results[node.serverId].issues.length" class="m-0 text-muted-foreground">{{ t('facilityDiagnostics.noErrors') }}</p>
        <div v-for="(issue, index) in results[node.serverId].issues" :key="index" class="grid gap-1 rounded-xl border border-warning-border bg-warning-bg p-3">
          <strong>{{ t(issueKey(issue.code, 'title')) }}</strong>
          <span v-if="issue.domain">{{ t('facilityDiagnostics.domain') }}: {{ issue.domain }}</span>
          <span v-if="issue.upstream">{{ t('facilityDiagnostics.upstream') }}: {{ issue.upstream }}</span>
          <span>{{ t('facilityDiagnostics.occurrences', { count: issue.count }) }}</span>
          <span v-if="issue.lastSeen">{{ t('facilityDiagnostics.lastSeen') }}: {{ issue.lastSeen }}</span>
          <p class="m-0">{{ t(issueKey(issue.code, 'hint')) }}</p>
          <details v-if="issue.evidence"><summary class="cursor-pointer">{{ t('facilityDiagnostics.evidence') }}</summary><pre class="mt-2 max-h-40 overflow-auto whitespace-pre-wrap break-words text-xs">{{ issue.evidence }}</pre></details>
        </div>
        <p v-if="results[node.serverId].truncated" class="m-0 text-muted-foreground">{{ t('facilityDiagnostics.truncated') }}</p>
      </div>
    </article>
  </section>
</template>
