<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import { PlayCircle, RefreshCcw, ServerCog } from '@lucide/vue';
import { activityApi } from '@/api/activity';
import { serversApi } from '@/api/servers';
import { tasksApi } from '@/api/tasks';
import ActivityLink from '@/components/activity/ActivityLink.vue';
import Button from '@/components/ui/Button.vue';
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue';
import LoadingOverlay from '@/components/ui/LoadingOverlay.vue';
import StatusBadge from '@/components/ui/StatusBadge.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { ServerDto } from '@/types/servers';
import type { TaskDto, TaskLog } from '@/types/tasks';
import { createLatestRequestGuard, isAbortError } from '@/views/_shared/requestState';
import { agentStatusKey, canRunPrivilegedOperation } from '../model';

const props = defineProps<{ server: ServerDto; active: boolean }>();
const { t } = useI18n();
const notifyError = useErrorToast();
const notifySuccess = useSuccessToast();

const agentTask = ref<TaskDto | null>(null);
const logs = ref<TaskLog[]>([]);
const loading = ref(false);
const error = ref('');
const pending = ref('');
const restartOpen = ref(false);

const agentTaskGuard = createLatestRequestGuard();
let inFlight = false;
let pollTimer: number | undefined;

const visibleLogs = computed(() => logs.value.slice(-20));

function isActiveTask(task: TaskDto | null) {
  return Boolean(task && ['queued', 'scheduled', 'running', 'failed_retryable'].includes(task.status));
}

async function load(reset = false, preferredTaskId = '') {
  if (!reset && inFlight) return;
  if (reset) agentTaskGuard.invalidate();
  const requestId = agentTaskGuard.begin();
  inFlight = true;
  loading.value = true;
  error.value = '';
  if (reset) {
    agentTask.value = null;
    logs.value = [];
  }
  try {
    let nextTask: TaskDto | null;
    if (preferredTaskId) {
      nextTask = await tasksApi.get(preferredTaskId);
    } else if (agentTask.value?.id) {
      nextTask = await tasksApi.get(agentTask.value.id);
    } else {
      const result = await tasksApi.list({ serverId: props.server.id, type: 'server_agent_deploy', page: 1, pageSize: 1 });
      nextTask = result.items[0] ?? null;
    }
    if (!agentTaskGuard.isCurrent(requestId)) return;
    if (!nextTask) {
      agentTask.value = null;
      logs.value = [];
      return;
    }
    if (agentTask.value?.id !== nextTask.id) logs.value = [];
    agentTask.value = nextTask;
    const nextLogs = await activityApi.events({ executionId: nextTask.id, kind: 'output', limit: 20 });
    if (!agentTaskGuard.isCurrent(requestId)) return;
    logs.value = [...nextLogs.items].sort((left, right) => left.seq - right.seq).map((event) => ({ cursor: event.seq, time: event.occurredAt, stream: event.stream || 'stdout', line: event.text || '' }));
  } catch (err) {
    if (isAbortError(err)) return;
    if (!agentTaskGuard.isCurrent(requestId)) return;
    error.value = err instanceof Error ? err.message : t('serversPage.agentTaskLoadFailed');
  } finally {
    if (agentTaskGuard.isCurrent(requestId)) {
      inFlight = false;
      loading.value = false;
    }
  }
}

function startPolling() {
  window.clearInterval(pollTimer);
  pollTimer = window.setInterval(() => {
    if (props.active && document.visibilityState === 'visible' && agentTask.value) void load();
  }, 2000);
}

async function deploy() {
  pending.value = 'agent';
  try {
    const accepted = await serversApi.deployAgent(props.server.id);
    notifySuccess(t('serversPage.agentTaskAccepted', { taskId: accepted.taskId }), accepted);
    await load(true, accepted.taskId);
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    pending.value = '';
  }
}

async function restart() {
  pending.value = 'restart';
  try {
    const accepted = await serversApi.restart(props.server.id);
    notifySuccess(t('serversPage.restartAccepted', { taskId: accepted.taskId }), accepted);
    restartOpen.value = false;
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    pending.value = '';
  }
}

function stageLabel(stage: string) {
  const key = ({
    preparing: 'serversPage.agentTaskStagePreparing',
    checking: 'serversPage.agentTaskStageChecking',
    downloading: 'serversPage.agentTaskStageDownloading',
    uploading: 'serversPage.agentTaskStageUploading',
    configuring: 'serversPage.agentTaskStageConfiguring',
    starting: 'serversPage.agentTaskStageStarting',
    restarting: 'serversPage.agentTaskStageRestarting',
    collecting: 'serversPage.agentTaskStageCollecting',
    completed: 'serversPage.agentTaskStageCompleted',
  } as Record<string, string>)[stage];
  return key ? t(key) : stage || t('common.notAvailable');
}

function formatLog(line: string) {
  if (line === 'waiting for agent restart readiness') return t('serversPage.agentTaskLogWaitingReadiness');
  if (line === 'agent restart readiness confirmed (state=ready)') return t('serversPage.agentTaskLogReadinessConfirmed');
  const restartDelay = line.match(/^agent requested a restart delay \(state=holdon; (.+) elapsed\); the protocol did not provide a reason$/);
  if (restartDelay) return t('serversPage.agentTaskLogRestartDelay', { elapsed: restartDelay[1] });
  const maintenanceWait = line.match(/^panel agent is still running package maintenance; waiting to restart \((.+) elapsed\)$/);
  if (maintenanceWait) return t('serversPage.agentTaskLogRestartDelay', { elapsed: maintenanceWait[1] });
  const readinessTimeout = line.match(/^agent restart readiness wait timed out after (.+); deployment will continue$/);
  if (readinessTimeout) return t('serversPage.agentTaskLogReadinessTimeout', { timeout: readinessTimeout[1] });
  return line;
}

onMounted(() => {
  void load(true);
  startPolling();
});
onBeforeUnmount(() => {
  window.clearInterval(pollTimer);
  agentTaskGuard.invalidate();
});
</script>

<template>
  <div class="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-4">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <div class="flex flex-wrap items-center gap-2">
        <span class="text-sm text-muted-foreground">{{ t(agentStatusKey(server)) }}</span>
        <StatusBadge v-if="agentTask" :status="agentTask.status" domain="task" :label="t(`tasksPage.status.${agentTask.status}`)" />
      </div>
      <div class="flex flex-wrap gap-2">
        <Button size="sm" :loading="loading" @click="load(true)"><RefreshCcw />{{ t('common.refresh') }}</Button>
        <Button size="sm" :loading="pending === 'agent'" @click="deploy"><ServerCog />{{ t('serversPage.deployAgent') }}</Button>
        <Button size="sm" variant="danger" :disabled="!canRunPrivilegedOperation(server)" :loading="pending === 'restart'" @click="restartOpen = true"><PlayCircle />{{ t('serversPage.restart') }}</Button>
      </div>
    </div>

    <div class="relative min-h-0 overflow-y-auto">
      <LoadingOverlay v-if="loading && !agentTask" :label="t('serversPage.agentTaskLoading')" />
      <div v-if="agentTask" class="grid min-w-0 gap-3 rounded-xl border border-border bg-muted p-4 text-xs">
        <div class="flex min-w-0 flex-wrap items-center justify-between gap-2">
          <strong class="text-foreground">{{ t('serversPage.agentTaskCurrentStage') }}</strong>
          <span class="text-muted-foreground">{{ stageLabel(agentTask.stage) }}</span>
        </div>
        <p v-if="agentTask.error" class="m-0 break-words text-danger">{{ agentTask.error }}</p>
        <div class="grid min-w-0 gap-1">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <strong class="text-foreground">{{ t('serversPage.agentTaskRecentLogs') }}</strong>
            <ActivityLink :execution-id="agentTask.id" />
          </div>
          <pre aria-live="polite" class="m-0 max-h-64 min-w-0 overflow-y-auto overflow-x-hidden whitespace-pre-wrap break-words rounded-lg bg-muted p-2 text-[11px] text-muted-foreground [overflow-wrap:anywhere]">{{ visibleLogs.map((line) => `[${line.stream}] ${formatLog(line.line)}`).join('\n') || t('serversPage.agentTaskNoLogs') }}</pre>
        </div>
      </div>
      <div v-if="error" class="mt-3 grid gap-2 rounded-xl border border-danger-border bg-danger-bg p-3 text-xs text-danger">
        <span class="break-words">{{ error }}</span>
        <Button size="sm" variant="secondary" :loading="loading" @click="load(true)"><RefreshCcw />{{ t('common.retry') }}</Button>
      </div>
      <p v-else-if="!agentTask && !loading" class="m-0 text-sm text-muted-foreground">{{ t('serversPage.agentTaskNoHistory') }}</p>
    </div>

    <ConfirmDialog
      :open="restartOpen"
      :title="t('serversPage.confirmRestartTitle')"
      :description="t('serversPage.confirmRestartDescription', { name: server.name })"
      :impact="t('serversPage.confirmRestartImpact')"
      tone="danger"
      :loading="pending === 'restart'"
      :confirm-label="t('common.confirm')"
      :cancel-label="t('common.cancel')"
      :checkbox-label="t('serversPage.confirmCheckbox')"
      @update:open="(value) => { restartOpen = value; }"
      @confirm="restart"
      @cancel="restartOpen = false"
    />
  </div>
</template>
