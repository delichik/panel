<script setup lang="ts">
import { ClipboardList, Server } from '@/icons';
import Button from '@/components/ui/Button.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import Expander from '@/components/ui/Expander.vue';
import InfoBar from '@/components/patterns/InfoBar.vue';
import Skeleton from '@/components/ui/Skeleton.vue';
import StatusBadge from '@/components/ui/StatusBadge.vue';
import { useI18n } from '@/i18n';
import { formatDateTime } from '@/utils/datetime';
import type { ApplicationRuntimeInstance } from '@/types/applications';
import { instanceOperationStatus, isDeployProgressStage, statusTone, uncertaintyNeedsAttention } from '../model';

/**
 * 应用 Runtime 节点实例卡片：每台部署节点一张 surface-card（wide 2 列，窄屏单列）。
 * 卡片层级：头部（Server 图标 + 节点名 + 容器名/短 id + 单状态徽标，左侧 2px 语义色条）
 * → 主体（operation 状态/阶段/尝试/下次重试 2×2，无 operation 时显示当前状态/最近更新）
 * → 底部（查看操作 + 更新时间）；错误用 InfoBar/Expander 保留，不再嵌套第二层灰块。
 * 父级 `DetailSection` 以 `:surface="false"` 使用，避免 card-in-card。
 */
withDefaults(defineProps<{
  instances: ApplicationRuntimeInstance[];
  /** Runtime 正在加载：无实例时展示卡片骨架而不是空态。 */
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

function instanceState(instance: ApplicationRuntimeInstance) {
  return instance.status || instance.state || 'unknown';
}

/** 头部容器名/短 id：有名字优先显示名字，容器 id 只保留前 12 位，避免撑破卡片。 */
function containerLabel(instance: ApplicationRuntimeInstance) {
  const name = instance.containerName || '';
  const id = instance.containerId ? instance.containerId.slice(0, 12) : '';
  if (name && id) return `${name} · ${id}`;
  return name || id || t('common.notAvailable');
}

/** 左侧 2px 语义色条（复用状态色 token，不新增色板）。 */
function statusAccentClass(status: string) {
  const tone = statusTone(status);
  if (tone === 'success') return 'bg-success';
  if (tone === 'warning') return 'bg-warning';
  if (tone === 'danger') return 'bg-danger';
  return 'bg-divider';
}

function instanceUpdatedAt(instance: ApplicationRuntimeInstance) {
  return instance.operation?.updatedAt ?? instance.updatedAt;
}
</script>

<template>
  <div data-instance-list class="grid min-w-0 gap-3 wide:grid-cols-2">
    <template v-if="instances.length">
      <article
        v-for="instance in instances"
        :key="instance.instanceId || instance.id"
        data-instance-card
        class="relative grid min-w-0 content-start gap-3 overflow-hidden surface-card p-4"
      >
        <span class="absolute inset-y-0 left-0 w-0.5" :class="statusAccentClass(instanceState(instance))" aria-hidden="true" />

        <!-- 头部：Server 图标 + 节点名 + 容器名/短 id，右侧单状态徽标。 -->
        <header class="flex min-w-0 items-start justify-between gap-3 pl-1">
          <div class="flex min-w-0 items-start gap-2">
            <Server class="mt-0.5 size-4 shrink-0 text-secondary-foreground" aria-hidden="true" />
            <div class="grid min-w-0 gap-0.5">
              <strong class="block truncate text-body-strong font-semibold text-foreground">{{ instance.serverName || instance.serverId || instance.instanceId }}</strong>
              <span class="panel-mono block truncate text-caption text-muted-foreground" :title="containerLabel(instance)">{{ containerLabel(instance) }}</span>
            </div>
          </div>
          <StatusBadge :status="instanceState(instance)" :tone="statusTone(instanceState(instance))" :label="instanceStatusLabel(instance.status || instance.state || '')" />
        </header>

        <!-- 主体：operation 状态/阶段/尝试/下次重试 2×2；无 operation 时显示当前状态/最近更新。 -->
        <dl v-if="instance.operation" class="m-0 grid min-w-0 grid-cols-2 gap-3 pl-1">
          <div class="grid min-w-0 content-start gap-0.5">
            <dt class="text-caption text-muted-foreground">{{ t('applicationsPage.currentDeployment') }}</dt>
            <dd class="m-0 min-w-0">
              <StatusBadge
                :status="instanceOperationStatus(instance)"
                domain="operation"
                :tone="instanceOperationStatus(instance) === 'result_unknown' ? 'warning' : undefined"
                :label="operationStatusLabel(instanceOperationStatus(instance))"
              />
            </dd>
          </div>
          <div class="grid min-w-0 content-start gap-0.5">
            <dt class="text-caption text-muted-foreground">{{ t('applicationsPage.operationStage') }}</dt>
            <dd class="m-0 break-words text-caption font-semibold text-foreground">{{ uncertaintyNeedsAttention(instance.operation.errorClass, instance.operation.stage) ? t('applicationsPage.status.result_unknown') : (isDeployProgressStage(instance.operation.stage) || instance.operation.stage ? operationStageLabel(instance.operation.stage || '') : t('common.notAvailable')) }}</dd>
          </div>
          <div class="grid min-w-0 content-start gap-0.5">
            <dt class="text-caption text-muted-foreground">{{ t('applicationsPage.operationAttempt') }}</dt>
            <dd class="m-0 text-caption font-semibold text-foreground">{{ instance.operation.attempt ?? t('common.notAvailable') }}</dd>
          </div>
          <div class="grid min-w-0 content-start gap-0.5">
            <dt class="text-caption text-muted-foreground">{{ t('applicationsPage.operationNextRetry') }}</dt>
            <dd class="m-0 break-words text-caption font-semibold text-foreground">{{ instance.operation.nextRunAt ? formatDateTime(instance.operation.nextRunAt) : t('common.notAvailable') }}</dd>
          </div>
        </dl>
        <dl v-else class="m-0 grid min-w-0 grid-cols-2 gap-3 pl-1">
          <div class="grid min-w-0 content-start gap-0.5">
            <dt class="text-caption text-muted-foreground">{{ t('common.status') }}</dt>
            <dd class="m-0 text-caption font-semibold text-foreground">{{ instanceStatusLabel(instance.status || instance.state || '') }}</dd>
          </div>
          <div class="grid min-w-0 content-start gap-0.5">
            <dt class="text-caption text-muted-foreground">{{ t('applicationsPage.updatedAtColumn') }}</dt>
            <dd class="m-0 break-words text-caption font-semibold text-foreground">{{ formatDateTime(instance.updatedAt, t('common.never')) }}</dd>
          </div>
        </dl>

        <p v-if="instance.operation?.monitorReasonCode" class="m-0 pl-1 text-caption text-muted-foreground">{{ t('applicationsPage.monitorReason') }}: {{ instance.operation.monitorReasonCode }}<span v-if="instance.operation.evidenceDeadlineAt"> · {{ t('applicationsPage.evidenceDeadline') }} {{ formatDateTime(instance.operation.evidenceDeadlineAt) }}</span></p>
        <p v-if="instance.operation && instanceOperationStatus(instance) === 'result_unknown'" class="m-0 pl-1 text-caption font-semibold text-warning">{{ t('applicationsPage.unknownResultHint') }}</p>
        <p v-if="instance.lastError && !instance.operation?.error" class="m-0 pl-1 text-caption font-semibold text-danger">{{ instance.lastError }}</p>
        <InfoBar
          v-if="instance.operation && instanceOperationStatus(instance) !== 'result_unknown' && instance.operation.errorCode !== 'execution_monitoring' && (instance.operation.error || instance.operation.errorCode || instance.operation.errorDetail)"
          tone="danger"
          :title="instance.operation.errorCode ? `${t('applicationsPage.operationError')} · ${instance.operation.errorCode}` : t('applicationsPage.operationError')"
        >
          <p v-if="instance.operation.error" class="m-0 whitespace-pre-wrap break-words">{{ instance.operation.error }}</p>
          <p v-if="instance.operation.errorCode === 'container_not_running' && !instance.operation.errorDetail?.includes('containerLogs:')" class="m-0 text-warning">{{ t('facilityDiagnostics.missingRunLogsHint') }}</p>
          <Expander v-if="instance.operation.errorDetail && instance.operation.errorDetail !== instance.operation.error" :title="t('facilityDiagnostics.technicalDetails')">
            <pre class="m-0 max-h-56 overflow-auto whitespace-pre-wrap break-words text-caption">{{ instance.operation.errorDetail }}</pre>
          </Expander>
        </InfoBar>

        <!-- 底部：查看操作（仅 operationId）+ 更新时间。 -->
        <footer class="flex flex-wrap items-center justify-between gap-2 pl-1">
          <Button v-if="instance.operation?.operationId" size="sm" variant="ghost" class="max-sm:w-full" @click="emit('openOperation', instance.operation.operationId, instance.operation.executionId)"><ClipboardList />{{ t('applicationsPage.viewOperation') }}</Button>
          <span class="text-caption text-muted-foreground">{{ t('applicationsPage.updatedAtColumn') }}: {{ formatDateTime(instanceUpdatedAt(instance), t('common.never')) }}</span>
        </footer>
      </article>
    </template>

    <template v-else-if="loading">
      <div
        v-for="item in 2"
        :key="`instance-skeleton-${item}`"
        data-instance-skeleton
        aria-busy="true"
        class="relative grid min-w-0 content-start gap-3 overflow-hidden surface-card p-4"
      >
        <span class="absolute inset-y-0 left-0 w-0.5 bg-divider" aria-hidden="true" />
        <header class="flex min-w-0 items-start justify-between gap-3 pl-1">
          <div class="flex min-w-0 items-start gap-2">
            <Server class="mt-0.5 size-4 shrink-0 text-secondary-foreground" aria-hidden="true" />
            <div class="grid min-w-0 gap-1">
              <Skeleton class="h-4 w-28" />
              <Skeleton class="h-3 w-36 max-w-full" />
            </div>
          </div>
          <Skeleton class="h-5 w-14 shrink-0 rounded-pill" />
        </header>
        <dl class="m-0 grid min-w-0 grid-cols-2 gap-3 pl-1">
          <div class="grid min-w-0 content-start gap-1">
            <dt class="text-caption text-muted-foreground">{{ t('common.status') }}</dt>
            <dd class="m-0"><Skeleton class="h-4 w-16" /></dd>
          </div>
          <div class="grid min-w-0 content-start gap-1">
            <dt class="text-caption text-muted-foreground">{{ t('applicationsPage.updatedAtColumn') }}</dt>
            <dd class="m-0"><Skeleton class="h-4 w-24 max-w-full" /></dd>
          </div>
        </dl>
        <footer class="flex flex-wrap items-center justify-between gap-2 pl-1">
          <span class="inline-flex items-center gap-1.5 text-caption text-muted-foreground">
            <span>{{ t('applicationsPage.updatedAtColumn') }}:</span>
            <Skeleton class="inline-block h-3.5 w-28" />
          </span>
        </footer>
      </div>
    </template>

    <EmptyState v-else variant="inset" class="wide:col-span-2" :title="t('applicationsPage.noRuntime')" :description="t('applicationsPage.noRuntimeHint')" />
  </div>
</template>
