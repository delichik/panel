<script setup lang="ts">
import { ClipboardList } from '@/icons';
import Button from '@/components/ui/Button.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import Expander from '@/components/ui/Expander.vue';
import InfoBar from '@/components/patterns/InfoBar.vue';
import StatusBadge from '@/components/ui/StatusBadge.vue';
import { useI18n } from '@/i18n';
import { formatDateTime } from '@/utils/datetime';
import type { ApplicationRuntimeInstance } from '@/types/applications';
import { instanceOperationStatus, isDeployProgressStage, statusTone, uncertaintyNeedsAttention } from '../model';

/**
 * 应用 Runtime 实例列表：每台部署节点的容器状态 + 最近部署操作。
 * 纯展示组件：数据由页面加载并传入，操作入口（打开任务中心）通过 emit 交回页面；
 * 实例行直接渲染为父级 `DetailSection` 的 `divide-y` 子节点，保持原有分隔线视觉。
 */
withDefaults(defineProps<{
  instances: ApplicationRuntimeInstance[];
  /** Runtime 正在加载：无实例时展示骨架行而不是空态。 */
  loading?: boolean;
}>(), {
  loading: false,
});

const emit = defineEmits<{ openOperation: [operationId?: string, executionId?: string] }>();

const { t } = useI18n();

function instanceStatusLabel(status: string) {
  if (!status) return t('common.notAvailable');
  const key = `applicationsPage.instanceStatus.${status}`;
  const label = t(key);
  return label === key ? status : label;
}

function operationStatusLabel(status: string) {
  if (!status) return t('common.notAvailable');
  const keys = [`applicationsPage.operationStatus.${status}`, `activity.state.${status}`, `tasksPage.status.${status}`];
  for (const key of keys) {
    const label = t(key);
    if (label !== key) return label;
  }
  return status;
}

function operationStageLabel(stage: string) {
  if (!stage) return t('common.notAvailable');
  const key = `applicationOperationsPage.stage.${stage}`;
  const label = t(key);
  return label === key ? stage : label;
}
</script>

<template>
  <template v-if="instances.length">
    <div
      v-for="instance in instances"
      :key="instance.instanceId || instance.id"
      class="grid gap-2 p-4 text-body"
    >
    <div class="flex flex-wrap items-center justify-between gap-2">
      <strong class="min-w-0 break-all text-body-strong font-semibold text-foreground">{{ instance.serverName || instance.serverId || instance.instanceId }}</strong>
      <StatusBadge :status="instance.status || instance.state || 'unknown'" :tone="statusTone(instance.status || instance.state || 'unknown')" :label="instanceStatusLabel(instance.status || instance.state || '')" />
    </div>
    <span class="text-caption text-muted-foreground">{{ instance.containerName || instance.containerId || t('common.notAvailable') }}</span>
    <div v-if="instance.operation" class="grid gap-2 rounded-control bg-muted p-3">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <StatusBadge
          :status="instanceOperationStatus(instance)"
          domain="operation"
          :tone="instanceOperationStatus(instance) === 'result_unknown' ? 'warning' : undefined"
          :label="operationStatusLabel(instanceOperationStatus(instance))"
        />
        <Button v-if="instance.operation.operationId" size="sm" variant="ghost" @click="emit('openOperation', instance.operation.operationId, instance.operation.executionId)"><ClipboardList />{{ t('applicationsPage.viewOperation') }}</Button>
      </div>
      <dl class="m-0 grid gap-2 sm:grid-cols-3">
        <div class="min-w-0">
          <dt class="text-caption text-muted-foreground">{{ t('applicationsPage.operationStage') }}</dt>
          <dd class="m-0 break-words text-caption font-semibold text-foreground">{{ uncertaintyNeedsAttention(instance.operation.errorClass, instance.operation.stage) ? t('applicationsPage.status.result_unknown') : (isDeployProgressStage(instance.operation.stage) || instance.operation.stage ? operationStageLabel(instance.operation.stage || '') : t('common.notAvailable')) }}</dd>
        </div>
        <div class="min-w-0">
          <dt class="text-caption text-muted-foreground">{{ t('applicationsPage.operationAttempt') }}</dt>
          <dd class="m-0 text-caption font-semibold text-foreground">{{ instance.operation.attempt ?? t('common.notAvailable') }}</dd>
        </div>
        <div class="min-w-0">
          <dt class="text-caption text-muted-foreground">{{ t('applicationsPage.operationNextRetry') }}</dt>
          <dd class="m-0 text-caption font-semibold text-foreground">{{ instance.operation.nextRunAt ? formatDateTime(instance.operation.nextRunAt) : t('common.notAvailable') }}</dd>
        </div>
      </dl>
      <p v-if="instance.operation.monitorReasonCode" class="m-0 text-caption text-muted-foreground">{{ t('applicationsPage.monitorReason') }}: {{ instance.operation.monitorReasonCode }}<span v-if="instance.operation.evidenceDeadlineAt"> · {{ t('applicationsPage.evidenceDeadline') }} {{ formatDateTime(instance.operation.evidenceDeadlineAt) }}</span></p>
      <p v-if="instanceOperationStatus(instance) === 'result_unknown'" class="m-0 text-caption font-semibold text-warning">{{ t('applicationsPage.unknownResultHint') }}</p>
      <InfoBar
        v-if="instanceOperationStatus(instance) !== 'result_unknown' && instance.operation.errorCode !== 'execution_monitoring' && (instance.operation.error || instance.operation.errorCode || instance.operation.errorDetail)"
        tone="danger"
        :title="instance.operation.errorCode ? `${t('applicationsPage.operationError')} · ${instance.operation.errorCode}` : t('applicationsPage.operationError')"
      >
        <p v-if="instance.operation.error" class="m-0 whitespace-pre-wrap break-words">{{ instance.operation.error }}</p>
        <p v-if="instance.operation.errorCode === 'container_not_running' && !instance.operation.errorDetail?.includes('containerLogs:')" class="m-0 text-warning">{{ t('facilityDiagnostics.missingRunLogsHint') }}</p>
        <Expander v-if="instance.operation.errorDetail && instance.operation.errorDetail !== instance.operation.error" :title="t('facilityDiagnostics.technicalDetails')">
          <pre class="m-0 max-h-56 overflow-auto whitespace-pre-wrap break-words text-caption">{{ instance.operation.errorDetail }}</pre>
        </Expander>
      </InfoBar>
    </div>
    <span v-if="instance.lastError && !instance.operation?.error" class="text-caption font-semibold text-danger">{{ instance.lastError }}</span>
    </div>
  </template>

  <div v-else-if="loading" class="grid gap-3 p-4" aria-hidden="true">
    <div class="motion-skeleton h-4 w-40 rounded bg-muted animate-pulse" />
    <div class="motion-skeleton h-4 w-56 max-w-full rounded bg-muted animate-pulse" />
    <div class="motion-skeleton h-4 w-48 max-w-full rounded bg-muted animate-pulse" />
    <div class="motion-skeleton h-4 w-52 max-w-full rounded bg-muted animate-pulse" />
  </div>

  <EmptyState v-else variant="inset" :title="t('applicationsPage.noRuntime')" :description="t('applicationsPage.noRuntimeHint')" />
</template>
