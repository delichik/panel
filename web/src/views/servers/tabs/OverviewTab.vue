<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { KeyRound } from '@/icons';
import { serversApi, type ServerMetricsSeries } from '@/api/servers';
import { tasksApi } from '@/api/tasks';
import ActivityLink from '@/components/activity/ActivityLink.vue';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue';
import StatusBadge from '@/components/ui/StatusBadge.vue';
import { useErrorDialog } from '@/components/ui/errorDialog';
import { useSuccessToast } from '@/components/ui/toast';
import DetailSection from '@/components/patterns/DetailSection.vue';
import PropertyList, { type PropertyListItem } from '@/components/patterns/PropertyList.vue';
import { useI18n } from '@/i18n';
import type { CredentialDto } from '@/types/credentials';
import type { ServerDto } from '@/types/servers';
import type { TaskDto } from '@/types/tasks';
import { formatDateTime } from '@/utils/datetime';
import { createLatestRequestGuard, isAbortError } from '@/views/_shared/requestState';
import MetricsPanel from './MetricsPanel.vue';
import OverviewSummary from './OverviewSummary.vue';
import ProbeOverviewPanel from './ProbeOverviewPanel.vue';
import {
  credentialLabel,
  proxyErrorKey,
  proxyModeLabelKey,
  proxyTunnelDisplayName,
  serverProxyMode,
} from '../model';

const props = defineProps<{ server: ServerDto; credentials: CredentialDto[]; active: boolean }>();
const emit = defineEmits<{ refresh: [] }>();
const { t } = useI18n();
const notifyError = useErrorDialog();
const notifySuccess = useSuccessToast();

const initialTask = ref<TaskDto | null>(null);
const initialTaskLoading = ref(false);
const initialTaskError = ref('');
const metricsSnapshot = ref<ServerMetricsSeries | null>(null);
const metricsLoading = ref(true);
const trustHostKeyOpen = ref(false);
const trusting = ref(false);

const initialTaskGuard = createLatestRequestGuard();
let initialTaskInFlight = false;
let pollTimer: number | undefined;

const credentialName = computed(() => credentialLabel(props.server.credentialId, props.credentials));
const hostKeyMismatch = computed(() => Boolean(props.server.hostKeyMismatch));
const proxyMode = computed(() => serverProxyMode(props.server));
const proxyModeLabel = computed(() => t(proxyModeLabelKey(props.server.proxyMode)));
const proxyDetail = computed(() => {
  if (proxyMode.value === 'ssh_tunnel') return proxyTunnelDisplayName(props.server) || t('serversPage.proxy.tunnelUnknown');
  if (proxyMode.value === 'socks5') return props.server.socks5Address || t('common.notAvailable');
  return '';
});
const proxyInvalid = computed(() => Boolean(props.server.proxyInvalid));
const connectionItems = computed<PropertyListItem[]>(() => [
  { key: 'host', label: t('serversPage.host'), value: props.server.host },
  { key: 'port', label: t('serversPage.port'), value: props.server.port },
  { key: 'credential', label: t('serversPage.credential'), value: credentialName.value || t('common.notAvailable') },
  { key: 'dockerHost', label: t('serversPage.dockerHost'), value: props.server.dockerHost || t('common.notAvailable') },
  { key: 'ipv4', label: t('serversPage.ipv4'), value: props.server.ipv4 || t('common.notAvailable') },
  { key: 'ipv6', label: t('serversPage.ipv6'), value: props.server.ipv6 || t('common.notAvailable') },
  {
    key: 'proxy',
    label: t('serversPage.proxy.label'),
    value: `${proxyModeLabel.value}${proxyDetail.value ? ` · ${proxyDetail.value}` : ''}`,
  },
]);

/** 概览摘要只保留状态/发行版/磁盘；CPU、内存、磁盘 IO、网络与延迟的当前值/历史都在下方图表区展示。 */
function isActiveTask(task: TaskDto | null) {
  return Boolean(task && ['queued', 'scheduled', 'running', 'failed_retryable'].includes(task.status));
}

async function loadInitialTask() {
  if (initialTaskInFlight) return;
  const serverId = props.server.id;
  const requestId = initialTaskGuard.begin();
  initialTaskInFlight = true;
  initialTaskLoading.value = true;
  initialTaskError.value = '';
  try {
    let taskId = props.server.initialTaskId || '';
    if (!taskId) {
      const result = await tasksApi.list({ serverId, type: 'server_info_collect', page: 1, pageSize: 1 });
      if (!initialTaskGuard.isCurrent(requestId)) return;
      taskId = result.items[0]?.id ?? '';
    }
    if (!taskId) {
      initialTask.value = null;
      return;
    }
    const nextTask = await tasksApi.get(taskId);
    if (!initialTaskGuard.isCurrent(requestId)) return;
    initialTask.value = nextTask;
  } catch (err) {
    if (isAbortError(err)) return;
    if (!initialTaskGuard.isCurrent(requestId)) return;
    initialTaskError.value = err instanceof Error ? err.message : t('serversPage.initialTaskLoadFailed');
  } finally {
    if (initialTaskGuard.isCurrent(requestId)) {
      initialTaskInFlight = false;
      initialTaskLoading.value = false;
    }
  }
}

function startPolling() {
  window.clearInterval(pollTimer);
  pollTimer = window.setInterval(() => {
    if (props.active && document.visibilityState === 'visible' && isActiveTask(initialTask.value)) void loadInitialTask();
  }, 2000);
}

async function trustHostKey() {
  trusting.value = true;
  try {
    const trusted = await serversApi.trustHostKey(props.server.id);
    notifySuccess(t('serversPage.trustHostKeySucceeded', { name: trusted.name }), trusted);
    trustHostKeyOpen.value = false;
    emit('refresh');
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    trusting.value = false;
  }
}

watch(() => props.server.id, () => {
  initialTask.value = null;
  initialTaskError.value = '';
  void loadInitialTask();
});

onMounted(() => {
  void loadInitialTask();
  startPolling();
});
onBeforeUnmount(() => {
  initialTaskGuard.invalidate();
  window.clearInterval(pollTimer);
});
</script>

<template>
  <div class="h-full min-h-0 overflow-y-auto pr-1 max-lg:h-auto max-lg:overflow-visible max-lg:pr-0">
    <div class="grid content-start gap-4">
      <div v-if="hostKeyMismatch || server.lastError" class="grid gap-2">
        <div v-if="hostKeyMismatch" class="rounded-control border border-danger-border bg-danger-bg p-3 text-body text-danger">
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div class="grid min-w-0 gap-1">
              <strong>{{ t('serversPage.hostKeyMismatchTitle') }}</strong>
              <p class="m-0">{{ t('serversPage.hostKeyMismatchDescription') }}</p>
            </div>
            <Button size="sm" variant="danger" @click="trustHostKeyOpen = true"><KeyRound />{{ t('serversPage.trustHostKey') }}</Button>
          </div>
        </div>
        <div v-if="server.lastError" class="rounded-control border border-warning-border bg-warning-bg p-3 text-body text-warning">{{ server.lastError }}</div>
      </div>

      <OverviewSummary :server="server" :metrics="metricsSnapshot" :metrics-loading="metricsLoading" />

      <!-- 摘要卡之后：连接信息与最近操作并排（宽屏）。指标/探测的当前值与历史统一由下方图表面板承担，摘要卡不再重复。 -->
      <div class="grid content-start gap-4 wide:grid-cols-2">
        <DetailSection :title="t('serversPage.connection')">
          <PropertyList :items="connectionItems" :columns="2">
            <template #value="{ item, text }">
              <template v-if="item.key === 'proxy'">
                <span :class="proxyInvalid ? 'text-warning' : undefined">
                  {{ proxyModeLabel }}<template v-if="proxyDetail"> · <span class="panel-mono break-all">{{ proxyDetail }}</span></template>
                </span>
                <Badge v-if="proxyInvalid" tone="warning">{{ t(proxyErrorKey(server)) }}</Badge>
              </template>
              <span v-else class="min-w-0 break-all" :class="item.mono ? 'panel-mono' : undefined">{{ text }}</span>
            </template>
          </PropertyList>
        </DetailSection>

        <DetailSection :title="t('serversPage.recentOperations')">
          <template #actions>
            <ActivityLink resource-type="server" :resource-id="server.id" />
          </template>
          <div class="grid gap-2 p-4 text-body text-muted-foreground">
            <span>{{ t('serversPage.updatedAt') }}: {{ formatDateTime(server.updatedAt) || t('common.never') }}</span>
            <div v-if="initialTask || server.initialTaskId" class="grid gap-1">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <span>{{ t('serversPage.initialTask') }}</span>
                <span class="flex items-center gap-2">
                  <StatusBadge v-if="initialTask" :status="initialTask.status" domain="task" :label="t(`tasksPage.status.${initialTask.status}`)" />
                  <ActivityLink v-if="server.initialTaskId" :execution-id="server.initialTaskId" />
                </span>
              </div>
              <span class="panel-mono text-caption">{{ server.initialTaskId }}</span>
              <p v-if="initialTaskLoading && !initialTask" class="m-0 text-caption">{{ t('serversPage.agentTaskLoading') }}</p>
              <p v-if="initialTaskError" class="m-0 break-words text-danger">{{ initialTaskError }}</p>
              <p v-else-if="initialTask?.error" class="m-0 break-words text-danger">{{ initialTask.error }}</p>
            </div>
            <span v-else class="text-caption">{{ t('serversPage.agentTaskNoHistory') }}</span>
          </div>
        </DetailSection>
      </div>

      <!-- 指标/探测的当前值与历史由下方图表面板展示（含当前值、时间轴、阈值与空态），摘要卡不再重复。 -->
      <MetricsPanel
        :server="server"
        :active="active"
        @metrics-loaded="metricsSnapshot = $event"
        @loading-change="metricsLoading = $event"
      />

      <ProbeOverviewPanel :server="server" :active="active" />
    </div>

    <ConfirmDialog
      :open="trustHostKeyOpen"
      :title="t('serversPage.confirmTrustHostKeyTitle')"
      :description="t('serversPage.confirmTrustHostKeyDescription', { name: server.name })"
      :impact="t('serversPage.confirmTrustHostKeyImpact')"
      tone="danger"
      :loading="trusting"
      :confirm-label="t('common.confirm')"
      :cancel-label="t('common.cancel')"
      require-checkbox
      :checkbox-label="t('serversPage.trustHostKeyCheckbox')"
      @update:open="(value) => { trustHostKeyOpen = value; }"
      @confirm="trustHostKey"
      @cancel="trustHostKeyOpen = false"
    />
  </div>
</template>
