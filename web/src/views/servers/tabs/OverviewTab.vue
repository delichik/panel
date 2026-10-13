<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { KeyRound } from '@lucide/vue';
import { serversApi } from '@/api/servers';
import { tasksApi } from '@/api/tasks';
import ActivityLink from '@/components/activity/ActivityLink.vue';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue';
import StatusBadge from '@/components/ui/StatusBadge.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { CredentialDto } from '@/types/credentials';
import type { ServerDto } from '@/types/servers';
import type { TaskDto } from '@/types/tasks';
import { formatDateTime } from '@/utils/datetime';
import { createLatestRequestGuard, isAbortError } from '@/views/_shared/requestState';
import { canRunPrivilegedOperation, credentialLabel, serverReachabilityTone } from '../model';

const props = defineProps<{ server: ServerDto; credentials: CredentialDto[]; active: boolean }>();
const emit = defineEmits<{ refresh: [] }>();
const { t } = useI18n();
const notifyError = useErrorToast();
const notifySuccess = useSuccessToast();

const initialTask = ref<TaskDto | null>(null);
const initialTaskLoading = ref(false);
const initialTaskError = ref('');
const trustHostKeyOpen = ref(false);
const trusting = ref(false);

const initialTaskGuard = createLatestRequestGuard();
let initialTaskInFlight = false;
let pollTimer: number | undefined;

const credentialName = computed(() => credentialLabel(props.server.credentialId, props.credentials));
const hostKeyMismatch = computed(() => Boolean(props.server.hostKeyMismatch));

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
  <div class="h-full min-h-0 overflow-y-auto pr-1">
    <div v-if="hostKeyMismatch || server.lastError" class="mb-4 grid gap-2">
      <div v-if="hostKeyMismatch" class="rounded-xl border border-danger-border bg-danger-bg p-3 text-sm text-danger">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div class="grid min-w-0 gap-1">
            <strong>{{ t('serversPage.hostKeyMismatchTitle') }}</strong>
            <p class="m-0">{{ t('serversPage.hostKeyMismatchDescription') }}</p>
          </div>
          <Button size="sm" variant="danger" @click="trustHostKeyOpen = true"><KeyRound />{{ t('serversPage.trustHostKey') }}</Button>
        </div>
      </div>
      <div v-if="server.lastError" class="rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning">{{ server.lastError }}</div>
    </div>

    <div class="grid gap-4 2xl:grid-cols-[minmax(0,1fr)_320px]">
      <div class="grid content-start gap-4">
        <section class="rounded-2xl border border-border bg-muted p-4">
          <h3 class="m-0 text-sm font-semibold text-foreground">{{ t('serversPage.connection') }}</h3>
          <dl class="mt-3 grid grid-cols-2 gap-3 text-sm max-md:grid-cols-1">
            <div><dt>{{ t('serversPage.host') }}</dt><dd>{{ server.host }}</dd></div>
            <div><dt>{{ t('serversPage.port') }}</dt><dd>{{ server.port }}</dd></div>
            <div><dt>{{ t('serversPage.credential') }}</dt><dd>{{ credentialName || t('common.notAvailable') }}</dd></div>
            <div><dt>{{ t('serversPage.dockerHost') }}</dt><dd>{{ server.dockerHost || t('common.notAvailable') }}</dd></div>
            <div><dt>{{ t('serversPage.ipv4') }}</dt><dd>{{ server.ipv4 || t('common.notAvailable') }}</dd></div>
            <div><dt>{{ t('serversPage.ipv6') }}</dt><dd>{{ server.ipv6 || t('common.notAvailable') }}</dd></div>
          </dl>
        </section>

        <section class="rounded-2xl border border-border bg-muted p-4">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <h3 class="m-0 text-sm font-semibold text-foreground">{{ t('serversPage.recentOperations') }}</h3>
            <ActivityLink resource-type="server" :resource-id="server.id" />
          </div>
          <div class="mt-3 grid gap-2 text-sm text-muted-foreground">
            <span>{{ t('serversPage.lastChecked') }}: {{ formatDateTime(server.lastCheckedAt) || t('common.never') }}</span>
            <span>{{ t('serversPage.updatedAt') }}: {{ formatDateTime(server.updatedAt) || t('common.never') }}</span>
            <div v-if="initialTask || server.initialTaskId" class="grid gap-1">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <span>{{ t('serversPage.initialTask') }}</span>
                <span class="flex items-center gap-2">
                  <StatusBadge v-if="initialTask" :status="initialTask.status" domain="task" :label="t(`tasksPage.status.${initialTask.status}`)" />
                  <ActivityLink v-if="server.initialTaskId" :execution-id="server.initialTaskId" />
                </span>
              </div>
              <span class="panel-mono text-xs">{{ server.initialTaskId }}</span>
              <p v-if="initialTaskLoading && !initialTask" class="m-0 text-xs">{{ t('serversPage.agentTaskLoading') }}</p>
              <p v-if="initialTaskError" class="m-0 break-words text-danger">{{ initialTaskError }}</p>
              <p v-else-if="initialTask?.error" class="m-0 break-words text-danger">{{ initialTask.error }}</p>
            </div>
          </div>
        </section>
      </div>

      <aside class="grid content-start gap-3">
        <section class="rounded-2xl border border-border bg-muted p-4">
          <h3 class="m-0 text-sm font-semibold text-foreground">{{ t('serversPage.privilegeAndSecurity') }}</h3>
          <div class="mt-3 flex flex-wrap gap-2">
            <Badge :tone="serverReachabilityTone(server)">{{ server.reachable ? t('serversPage.reachable') : t('serversPage.unreachable') }}</Badge>
            <Badge :tone="canRunPrivilegedOperation(server) ? 'success' : 'warning'">{{ canRunPrivilegedOperation(server) ? t('serversPage.privileged') : t('serversPage.noPrivilege') }}</Badge>
          </div>
          <p class="m-0 mt-3 text-xs text-muted-foreground">{{ server.os?.prettyName || t('common.notAvailable') }} · {{ server.architecture || t('common.notAvailable') }}</p>
        </section>
      </aside>
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

<style scoped>
dt {
  margin: 0 0 4px;
  color: var(--panel-text-muted);
  font-size: 12px;
}

dd {
  margin: 0;
  overflow-wrap: anywhere;
  color: var(--panel-text);
  font-weight: 600;
}
</style>
