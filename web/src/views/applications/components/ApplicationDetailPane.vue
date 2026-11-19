<script setup lang="ts">
import { ClipboardList, RefreshCcw } from '@/icons';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import StatusBadge from '@/components/ui/StatusBadge.vue';
import DownloadButton from '@/components/ui/DownloadButton.vue';
import FileUploadButton from '@/components/ui/FileUploadButton.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import LoadingOverlay from '@/components/ui/LoadingOverlay.vue';
import Tabs from '@/components/ui/Tabs.vue';
import Select from '@/components/ui/Select.vue';
import DetailSection from '@/components/patterns/DetailSection.vue';
import FormField from '@/components/patterns/FormField.vue';
import InfoBar from '@/components/patterns/InfoBar.vue';
import PropertyList from '@/components/patterns/PropertyList.vue';
import { useI18n } from '@/i18n';
import type { ApplicationDto, ApplicationFile, ApplicationRuntime, DeploymentPlanningError } from '@/types/applications';
import { applicationNodeCounts, statusTone, type AppDetailTab } from '../model';
import ApplicationInstanceList from './ApplicationInstanceList.vue';

/**
 * 应用详情主体（Runtime / 路由 / 文件 / 持久化数据）。
 *
 * 拆自 index.vue 的详情页签区域：数据加载、URL `tab` 同步、轮询、下载等行为仍由页面持有，
 * 组件只接收已加载的数据与回调并通过 emit 交回操作意图（重试/打开任务/下载/恢复）。
 * 详情页头仍在页面侧（PageHeader + 编辑入口），本组件负责页头徽标收敛后的状态条与页签内容。
 */
const props = withDefaults(defineProps<{
  application: ApplicationDto;
  appStatus: string;
  planningError?: DeploymentPlanningError;
  nodeCounts: ReturnType<typeof applicationNodeCounts>;
  runtime?: ApplicationRuntime | null;
  runtimeLoading?: boolean;
  runtimeError?: string;
  files: ApplicationFile[];
  fileActionPending?: string;
  persistentServerId?: string;
  persistentServerOptions: Array<{ label: string; value: string }>;
  pending?: string;
  tabs: Array<{ label: string; value: AppDetailTab }>;
  activeTab: AppDetailTab;
  /** 首次加载详情时显示遮罩（页面传入细节：detailLoading && !已缓存）。 */
  showLoading?: boolean;
  /** 服务器名称解析（路由页签的目标节点展示）。 */
  serverName: (id: string) => string;
}>(), {
  planningError: undefined,
  runtime: null,
  runtimeLoading: false,
  runtimeError: '',
  fileActionPending: '',
  persistentServerId: '',
  pending: '',
  showLoading: false,
});

const emit = defineEmits<{
  'update:activeTab': [value: AppDetailTab];
  'update:persistentServerId': [value: string];
  retryRuntime: [];
  openOperation: [operationId?: string, executionId?: string];
  downloadFile: [file: ApplicationFile];
  downloadPersistentData: [];
  restorePersistentData: [fileOrFiles: File | File[]];
}>();

const { t } = useI18n();

/** Tabs 只发 string；按声明的页签收敛为 AppDetailTab，避免把未知值写入 URL。 */
function selectTab(value: string) {
  const next = props.tabs.find((tab) => tab.value === value)?.value;
  if (next) emit('update:activeTab', next);
}
</script>

<template>
  <div class="relative grid h-full min-h-0 min-w-0 grid-rows-page gap-3">
    <LoadingOverlay v-if="showLoading" />
    <!--
      详情页头徽标收敛：只保留综合状态；规划失败 / 需关注各一枚。
      节点计数等明细移入 Runtime 汇总，避免 7 枚徽标堆叠。
    -->
    <div class="flex flex-wrap items-center gap-2">
      <StatusBadge :status="appStatus" :tone="statusTone(appStatus)" :label="t(`applicationsPage.status.${appStatus}`)" />
      <Badge v-if="planningError" tone="danger">{{ t('applicationsPage.status.planning_failed') }}</Badge>
      <Badge v-else-if="application.reconcileStopped" tone="warning">{{ t('applicationsPage.status.attention') }}</Badge>
    </div>

    <Tabs
      :model-value="activeTab"
      :tabs="tabs"
      class="h-full min-h-0"
      @update:model-value="selectTab"
    >
      <section v-if="activeTab === 'runtime'" class="grid h-full min-h-0 content-start gap-4 overflow-y-auto pr-1">
        <InfoBar v-if="runtimeError" tone="danger" :title="t('common.loadFailed')">
          <p class="m-0 whitespace-pre-wrap break-words">{{ runtimeError }}</p>
          <div class="mt-2 flex flex-wrap gap-2">
            <Button size="sm" :loading="runtimeLoading" @click="emit('retryRuntime')"><RefreshCcw />{{ t('common.retry') }}</Button>
          </div>
        </InfoBar>
        <template v-else>
          <InfoBar v-if="application.imageUpdateAvailable" tone="warning" :title="t('applicationsPage.imageUpdate')">
            <p class="m-0 break-all">{{ application.imageReference || t('common.notAvailable') }}</p>
          </InfoBar>
          <InfoBar v-if="planningError" tone="danger" :title="t('applicationsPage.planningFailed')">
            <p class="m-0 whitespace-pre-wrap break-words">{{ planningError.message }}</p>
            <p v-if="planningError.fileName" class="m-0 break-words">{{ t('applicationsPage.planningFile') }}: {{ planningError.fileName }}</p>
            <p v-if="planningError.field" class="m-0 break-words">{{ t('applicationsPage.planningField') }}: {{ planningError.field }}</p>
            <p class="m-0">{{ t(planningError.retryable ? 'applicationsPage.planningRetryHint' : 'applicationsPage.planningFixHint') }}</p>
            <div class="mt-2">
              <Button size="sm" variant="ghost" @click="emit('openOperation', planningError.operationId)"><ClipboardList />{{ t('applicationsPage.viewOperation') }}</Button>
            </div>
          </InfoBar>
          <InfoBar v-if="application.reconcileStopped" tone="warning" :title="t('applicationsPage.status.attention')">
            {{ t('applicationsPage.reconcileStoppedHint') }}
          </InfoBar>
          <InfoBar v-else-if="!planningError && application.lastError" tone="danger" :title="t('applicationsPage.operationError')">
            <p class="m-0 whitespace-pre-wrap break-words">{{ application.lastError }}</p>
          </InfoBar>

          <!--
            ≥wide：主内容（实例列表）+ 摘要侧栏（短属性 PropertyList 2 列）。
            <wide 单列堆叠，避免 1200 内容列被压窄后仍强行三列。
          -->
          <div class="grid min-w-0 content-start gap-4 wide:grid-cols-[minmax(0,1fr)_320px] wide:items-start">
            <DetailSection :title="t('applicationsPage.nodeInstances')" class="min-w-0" :surface="false">
              <ApplicationInstanceList
                :instances="runtime?.instances ?? []"
                :loading="runtimeLoading"
                @open-operation="(operationId, executionId) => emit('openOperation', operationId, executionId)"
              />
            </DetailSection>

            <DetailSection :title="t('applicationsPage.runtime')" class="min-w-0 wide:sticky wide:top-0">
              <PropertyList
                :columns="2"
                :items="[
                  { label: t('applicationsPage.nodeBucketHealthyLabel'), value: nodeCounts.healthy },
                  { label: t('applicationsPage.nodeBucketAbnormalLabel'), value: nodeCounts.abnormal },
                  { label: t('applicationsPage.nodeBucketDeployingLabel'), value: nodeCounts.deploying },
                  { label: t('applicationsPage.imageUpdate'), value: application.imageUpdateAvailable ? t('applicationsPage.imageUpdate') : t('applicationsPage.imageCurrent') },
                  { label: t('applicationsPage.allowAutoUpdate'), value: application.autoUpdateEnabled !== false ? t('applicationsPage.autoUpdateAllowed') : t('applicationsPage.autoUpdateBlocked') },
                ]"
              />
            </DetailSection>
          </div>
        </template>
      </section>

      <section v-else-if="activeTab === 'routes'" class="grid h-full min-h-0 content-start gap-4 overflow-y-auto pr-1">
        <DetailSection :title="t('applicationsPage.routes')">
          <div v-for="rule in application.reverseProxy" :key="rule.domain" class="grid gap-2 p-4">
            <div class="flex flex-wrap items-center justify-between gap-2">
              <strong class="min-w-0 break-all text-body-strong font-semibold text-foreground">{{ rule.domain }}</strong>
              <div class="flex flex-wrap items-center gap-1">
                <Badge :tone="rule.httpsEnabled === false ? 'neutral' : 'success'">{{ t('applicationsPage.transport.httpsShort') }}</Badge>
                <Badge :tone="rule.httpsEnabled !== false && rule.http2Enabled !== false ? 'info' : 'neutral'">{{ t('applicationsPage.transport.http2Short') }}</Badge>
                <Badge :tone="rule.httpsEnabled !== false && rule.http3Enabled === true ? 'info' : 'neutral'">{{ t('applicationsPage.transport.http3Short') }}</Badge>
                <Badge tone="info">{{ rule.targetPort }}</Badge>
              </div>
            </div>
            <p class="m-0 text-caption text-muted-foreground">{{ t('applicationsPage.originServers', { count: rule.originServerIds.length }) }} · {{ rule.originServerIds.map((id) => serverName(id)).join(', ') || t('common.notAvailable') }}</p>
            <div class="flex flex-wrap gap-2"><Badge v-for="path in rule.paths" :key="path.path" tone="neutral">{{ path.path }}</Badge></div>
          </div>
          <EmptyState v-if="!application.reverseProxy.length" variant="inset" :title="t('applicationsPage.noRoutes')" :description="t('applicationsPage.noRoutesHint')" />
        </DetailSection>
      </section>

      <section v-else-if="activeTab === 'files'" class="grid h-full min-h-0 content-start gap-4 overflow-y-auto pr-1">
        <DetailSection :title="t('applicationsPage.files')">
          <div v-for="file in files" :key="file.name" class="flex flex-wrap items-center justify-between gap-3 p-4">
            <div class="min-w-0">
              <strong class="block break-all text-body-strong font-semibold text-foreground">{{ file.name }}</strong>
              <span class="text-caption text-muted-foreground">{{ file.size }} {{ t('applicationsPage.bytes') }}</span>
            </div>
            <DownloadButton size="sm" :loading="fileActionPending === `committed:${file.name}`" :label="t('common.download')" @click="emit('downloadFile', file)" />
          </div>
          <EmptyState v-if="!files.length" variant="inset" :title="t('applicationsPage.noFiles')" :description="t('applicationsPage.noCommittedFilesHint')" />
        </DetailSection>
      </section>

      <section v-else class="grid h-full min-h-0 content-start gap-4 overflow-y-auto pr-1">
        <DetailSection
          :title="t('applicationsPage.persistentData')"
          :description="application.hasPersistentData ? t('applicationsPage.persistentDataHint') : t('applicationsPage.persistentDataUnavailable')"
        >
          <div class="grid gap-3 p-4">
            <FormField v-if="persistentServerOptions.length" :label="t('applicationsPage.persistentDataNode')">
              <template #default="{ id }">
                <Select :id="id" :model-value="persistentServerId" :options="persistentServerOptions" :placeholder="t('applicationsPage.selectPersistentDataNode')" @update:model-value="emit('update:persistentServerId', $event)" />
              </template>
            </FormField>
            <div class="flex flex-wrap gap-2">
              <DownloadButton size="sm" :disabled="!persistentServerId" :loading="pending === 'persistent-download'" :label="t('applicationsPage.downloadPersistentData')" @click="emit('downloadPersistentData')" />
              <FileUploadButton size="sm" accept=".zip,application/zip" :disabled="!application.persistentPath" :loading="pending === 'persistent-restore'" :label="t('applicationsPage.restorePersistentData')" @change="emit('restorePersistentData', $event)" />
            </div>
          </div>
        </DetailSection>
      </section>
    </Tabs>
  </div>
</template>
