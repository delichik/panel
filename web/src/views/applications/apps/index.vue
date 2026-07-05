<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useI18n } from '@/i18n';
import { applicationsApi } from '@/api/applications';
import type { ApplicationDto } from '@/types/api';
import AppActionButton from '@/components/AppActionButton.vue';
import AppActionGroup from '@/components/AppActionGroup.vue';
import AppMasterDetailWorkspace from '@/components/AppMasterDetailWorkspace.vue';
import AppSelectorPanel from '@/components/AppSelectorPanel.vue';
import AppSelectorSummaryItem from '@/components/AppSelectorSummaryItem.vue';
import { usePagination } from '@/composables/usePagination';
import ApplicationDetail from './ApplicationDetail.vue';

type SelectorStatus = 'info' | 'success' | 'warning' | 'error' | 'grey';

const route = useRoute();
const router = useRouter();
const applications = ref<ApplicationDto[]>([]);
const selectedId = ref('');
const loading = ref(false);
const actionLoading = ref('');
const error = ref('');
const message = ref('');
const lastTaskId = ref('');
const deleteDialog = ref(false);
const deletingApplication = ref<ApplicationDto | null>(null);
const { formatDateTime, t, translateRuntimeStatus } = useI18n();

const selectedApplication = computed(() => applications.value.find((app) => app.id === selectedId.value) ?? null);
const {
  page,
  pageSize,
  total,
  pageItems: pagedApplications,
} = usePagination(applications);

function actionLabel(action: 'deploy' | 'stop' | 'restart' | 'delete') {
  if (action === 'deploy') return t('applicationsPage.sync');
  if (action === 'stop') return t('applicationsPage.disable');
  if (action === 'restart') return t('common.restart');
  return t('common.delete');
}

function statusColor(status?: string): SelectorStatus {
  if (status === 'running') return 'success';
  if (status === 'pending') return 'warning';
  if (status === 'failed' || status === 'unknown') return 'error';
  if (status === 'stopped') return 'grey';
  return 'info';
}

function runtimeStatus(app: ApplicationDto) {
  return app.runtimeStatus || (app.enabled ? 'pending' : 'stopped');
}

function selectorStatusColor(app: ApplicationDto) {
  if (runtimeStatus(app) === 'running' && app.imageUpdateAvailable) return 'warning';
  return statusColor(runtimeStatus(app));
}

function selectorStatusLabel(app: ApplicationDto) {
  if (runtimeStatus(app) === 'running' && app.imageUpdateAvailable) return t('applicationsPage.runningWithUpdate');
  return translateRuntimeStatus(runtimeStatus(app));
}

async function load() {
  loading.value = true;
  try {
    applications.value = await applicationsApi.list();
    if (route.query.application && typeof route.query.application === 'string') selectedId.value = route.query.application;
    if (!selectedId.value && applications.value.length) selectedId.value = applications.value[0].id;
    if (selectedId.value && !applications.value.some((app) => app.id === selectedId.value)) selectedId.value = applications.value[0]?.id ?? '';
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('applicationsPage.loadFailed');
  } finally {
    loading.value = false;
  }
}

function createApplication() {
  void router.push('/applications/apps/create');
}

function editApplication(app: ApplicationDto) {
  void router.push(`/applications/apps/${encodeURIComponent(app.id)}/edit`);
}

function taskRoute(taskId = lastTaskId.value) {
  return taskId ? { path: '/tasks', query: { task: taskId } } : '/tasks';
}

function showOperation(fallback: string, taskId?: string) {
  message.value = fallback;
  lastTaskId.value = taskId || '';
}

function replaceApplication(app: ApplicationDto) {
  const index = applications.value.findIndex((item) => item.id === app.id);
  if (index >= 0) applications.value[index] = app;
}

function askDelete(app: ApplicationDto) {
  deletingApplication.value = app;
  deleteDialog.value = true;
}

async function runAction(action: 'deploy' | 'stop' | 'restart' | 'delete', app: ApplicationDto) {
  actionLoading.value = `${action}:${app.id}`;
  try {
    if (action === 'delete') {
      await applicationsApi.delete(app.id);
      lastTaskId.value = '';
      message.value = t('applicationsPage.deleteRequested', { name: app.name });
      deleteDialog.value = false;
      deletingApplication.value = null;
    } else {
      const result = action === 'deploy'
        ? await applicationsApi.deploy(app.id)
        : action === 'stop'
          ? await applicationsApi.stop(app.id)
          : await applicationsApi.restart(app.id);
      showOperation(t('applicationsPage.actionAccepted', { action: actionLabel(action) }), result?.taskId);
    }
    await load();
  } catch (err) {
    lastTaskId.value = '';
    error.value = err instanceof Error ? err.message : t('applicationsPage.actionFailed', { action: actionLabel(action) });
  } finally {
    actionLoading.value = '';
  }
}

watch(selectedId, (id) => {
  const query = { ...route.query };
  if (id) query.application = id;
  void router.replace({ query });
});

onMounted(load);
</script>

<template>
  <div class="page-shell">
    <v-alert v-if="error" type="error" variant="tonal">{{ error }}</v-alert>
    <v-alert v-if="message" type="info" variant="tonal" closable @click:close="message = ''">
      <div class="task-alert">
        <span>{{ message }}</span>
        <AppActionButton v-if="lastTaskId" kind="plain" icon="mdi-clipboard-list-outline" :label="t('taskCenter.task')" :to="taskRoute()" />
      </div>
    </v-alert>

    <AppMasterDetailWorkspace>
      <template #aside>
      <AppSelectorPanel
        class="application-list"
        :title="t('applicationsPage.applications')"
        :loading="loading"
        :empty="applications.length === 0"
        empty-icon="mdi-application-brackets-outline"
        :empty-text="t('applicationsPage.noApplications')"
        :page="page"
        :page-size="pageSize"
        :total="total"
        @update:page="page = $event"
        @update:page-size="pageSize = $event"
      >
        <template #actions>
          <AppActionButton kind="tool" icon="mdi-plus" :label="t('applicationsPage.create')" @click="createApplication" />
        </template>
        <AppSelectorSummaryItem
          v-for="app in pagedApplications"
          :key="app.id"
          :selected="selectedId === app.id"
          :title="app.name"
          :subtitle="formatDateTime(app.updatedAt)"
          :status="statusColor(runtimeStatus(app))"
          @select="selectedId = app.id"
        >
          <v-chip :color="selectorStatusColor(app)" size="x-small" variant="tonal" label>{{ selectorStatusLabel(app) }}</v-chip>
        </AppSelectorSummaryItem>
      </AppSelectorPanel>
      </template>

      <div class="detail-column">
        <ApplicationDetail v-if="selectedApplication" :application="selectedApplication" @changed="replaceApplication">
          <template #actions>
            <AppActionButton icon="mdi-pencil" :label="t('common.edit')" @click="editApplication(selectedApplication)" />
            <AppActionButton icon="mdi-sync" :label="t('applicationsPage.sync')" :loading="actionLoading === `deploy:${selectedApplication.id}`" @click="runAction('deploy', selectedApplication)" />
            <AppActionButton icon="mdi-stop-circle-outline" :label="t('applicationsPage.disable')" :disabled="!selectedApplication.enabled" :loading="actionLoading === `stop:${selectedApplication.id}`" @click="runAction('stop', selectedApplication)" />
            <AppActionButton icon="mdi-restart" :label="t('common.restart')" :disabled="!selectedApplication.enabled" :loading="actionLoading === `restart:${selectedApplication.id}`" @click="runAction('restart', selectedApplication)" />
          </template>
          <template #more-actions>
            <v-list-item
              prepend-icon="mdi-delete"
              :title="t('common.delete')"
              class="text-error"
              @click="askDelete(selectedApplication)"
            />
          </template>
        </ApplicationDetail>
        <v-card v-else variant="outlined" class="empty-detail">
          <v-icon size="28" color="medium-emphasis">mdi-application-brackets-outline</v-icon>
          <div class="text-body-2 text-medium-emphasis">{{ t('applicationsPage.emptyHint') }}</div>
        </v-card>
      </div>
    </AppMasterDetailWorkspace>

    <v-dialog v-model="deleteDialog" width="460">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('applicationsPage.deleteApplication') }}</span>
          <AppActionButton kind="tool" icon="mdi-close" :label="t('common.close')" @click="deleteDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          {{ t('applicationsPage.deleteApplicationConfirm', { name: deletingApplication?.name || t('common.unknown') }) }}
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <AppActionGroup context="dialog">
            <AppActionButton kind="plain" :label="t('common.cancel')" @click="deleteDialog = false" />
            <AppActionButton
              kind="danger-primary"
              :label="t('common.delete')"
              :loading="deletingApplication ? actionLoading === `delete:${deletingApplication.id}` : false"
              @click="deletingApplication && runAction('delete', deletingApplication)"
            />
          </AppActionGroup>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>

<style scoped>
.application-list, .detail-column { min-width: 0; min-height: 0; }
.detail-column { display: flex; min-height: 0; overflow: hidden; }
.detail-column > * { flex: 1 1 auto; min-height: 0; }
.min-width-0 { min-width: 0; }
.task-alert { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
@media (min-width: 761px) { .detail-column { overflow: hidden; } }
@media (max-width: 760px) {
  .detail-column { display: block; overflow: visible; }
}
</style>
