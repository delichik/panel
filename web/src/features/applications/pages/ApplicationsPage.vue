<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { t, useI18n } from '@/i18n';
import { applicationsApi } from '@/api/applications';
import type { ApplicationDto, ApplicationOperationDto } from '@/types/api';
import ApplicationDetail from '../components/ApplicationDetail.vue';
import ApplicationEditor from '../components/ApplicationEditor.vue';

const route = useRoute();
const router = useRouter();
useI18n();
const applications = ref<ApplicationDto[]>([]);
const selectedId = ref('');
const editorOpen = ref(false);
const editingApplication = ref<ApplicationDto | null>(null);
const loading = ref(false);
const actionLoading = ref('');
const error = ref('');
const message = ref('');

const selectedApplication = computed(() => applications.value.find((app) => app.id === selectedId.value) ?? null);
const totalCount = computed(() => applications.value.length);
const enabledCount = computed(() => applications.value.filter((app) => app.enabled).length);
const attentionCount = computed(() => applications.value.filter((app) => ['failed', 'pending', 'unknown'].includes(app.runtimeStatus || '') || app.lastError).length);

function actionLabel(action: 'deploy' | 'stop' | 'restart' | 'delete') {
  if (action === 'deploy') return t('common.deploy');
  if (action === 'stop') return t('common.stop');
  if (action === 'restart') return t('common.restart');
  return t('common.delete');
}

function statusColor(status?: string) {
  if (status === 'running') return 'success';
  if (status === 'pending') return 'warning';
  if (status === 'failed' || status === 'unknown') return 'error';
  if (status === 'stopped') return 'grey';
  return 'info';
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
  editingApplication.value = null;
  editorOpen.value = true;
}

function editApplication(app: ApplicationDto) {
  editingApplication.value = app;
  editorOpen.value = true;
}

async function handleSaved(app: ApplicationDto) {
  editorOpen.value = false;
  message.value = app.enabled ? t('applicationsPage.savedAndDeploymentRequested') : t('applicationsPage.saved');
  await load();
  selectedId.value = app.id;
}

function showOperation(result: ApplicationOperationDto, fallback: string) {
  message.value = result.taskId ? `${fallback}: ${result.taskId}` : result.evalId ? `${fallback}: ${result.evalId}` : fallback;
}

function replaceApplication(app: ApplicationDto) {
  const index = applications.value.findIndex((item) => item.id === app.id);
  if (index >= 0) applications.value[index] = app;
}

async function runAction(action: 'deploy' | 'stop' | 'restart' | 'delete', app: ApplicationDto) {
  actionLoading.value = `${action}:${app.id}`;
  try {
    if (action === 'delete') {
      await applicationsApi.delete(app.id);
      message.value = t('applicationsPage.deleted', { name: app.name });
    } else {
      const result = action === 'deploy'
        ? await applicationsApi.deploy(app.id)
        : action === 'stop'
          ? await applicationsApi.stop(app.id)
          : await applicationsApi.restart(app.id);
      showOperation(result, t('applicationsPage.actionAccepted', { action: actionLabel(action) }));
    }
    await load();
  } catch (err) {
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
  <div>
    <div class="page-actions mb-4">
      <v-btn prepend-icon="mdi-plus" color="primary" variant="flat" class="text-none font-weight-bold action-btn" @click="createApplication">{{ t('applicationsPage.create') }}</v-btn>
    </div>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>
    <v-alert v-if="message" type="info" variant="tonal" class="mb-4" closable @click:close="message = ''">{{ message }}</v-alert>

    <div class="summary-strip mb-4">
      <v-card variant="outlined" class="summary-card">
        <div class="summary-icon surface-primary"><v-icon size="18">mdi-apps</v-icon></div>
        <div><div class="text-caption text-medium-emphasis">{{ t('applicationsPage.applications') }}</div><div class="text-h5 font-weight-bold font-tabular">{{ totalCount }}</div></div>
      </v-card>
      <v-card variant="outlined" class="summary-card">
        <div class="summary-icon surface-success"><v-icon size="18">mdi-toggle-switch-outline</v-icon></div>
        <div><div class="text-caption text-medium-emphasis">{{ t('common.enabled') }}</div><div class="text-h5 font-weight-bold font-tabular">{{ enabledCount }}</div></div>
      </v-card>
      <v-card variant="outlined" class="summary-card">
        <div class="summary-icon surface-warning"><v-icon size="18">mdi-alert-circle-outline</v-icon></div>
        <div><div class="text-caption text-medium-emphasis">{{ t('applicationsPage.needsAttention') }}</div><div class="text-h5 font-weight-bold font-tabular">{{ attentionCount }}</div></div>
      </v-card>
    </div>

    <div class="applications-workspace">
      <v-card variant="outlined" :loading="loading" class="application-list">
        <div class="list-header">
          <div>
            <div class="text-subtitle-1 font-weight-bold">{{ t('applicationsPage.desiredState') }}</div>
            <div class="text-caption text-medium-emphasis">{{ t('applicationsPage.desiredStateHint') }}</div>
          </div>
          <v-chip size="small" variant="tonal" color="primary" label class="font-tabular">{{ t('common.total', { count: totalCount }) }}</v-chip>
        </div>
        <v-table class="text-left application-table">
          <thead>
            <tr>
              <th>{{ t('serversPage.name') }}</th><th>{{ t('common.enabled') }}</th><th>{{ t('applicationsPage.runtime') }}</th><th>{{ t('applicationsPage.jobId') }}</th><th>{{ t('applicationsPage.namespace') }}</th><th>{{ t('applicationsPage.generation') }}</th><th>{{ t('applicationsPage.lastEval') }}</th><th class="text-right">{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="applications.length === 0"><td colspan="8" class="text-center py-8 text-medium-emphasis">{{ t('applicationsPage.noApplications') }}</td></tr>
            <tr v-for="app in applications" :key="app.id" class="application-row cursor-pointer" :class="{ selected: selectedId === app.id }" @click="selectedId = app.id">
              <td>
                <div class="name-line">
                  <span class="status-dot" :class="statusColor(app.runtimeStatus)" />
                  <div class="min-width-0">
                    <div class="font-weight-bold text-truncate">{{ app.name }}</div>
                    <div class="text-caption text-medium-emphasis text-truncate">{{ app.id }}</div>
                  </div>
                </div>
              </td>
              <td><v-chip :color="app.enabled ? 'success' : 'grey'" size="small" variant="tonal" label>{{ app.enabled ? t('common.enabled') : t('common.disabled') }}</v-chip></td>
              <td><v-chip :color="statusColor(app.runtimeStatus)" size="small" variant="tonal" label>{{ app.runtimeStatus || (app.enabled ? 'pending' : 'stopped') }}</v-chip></td>
              <td class="text-truncate mono-cell">{{ app.jobId }}</td>
              <td>{{ app.namespace }}</td>
              <td><v-chip size="small" variant="tonal" color="info" label class="font-tabular">{{ t('applicationsPage.generation') }} {{ app.generation }}</v-chip></td>
              <td class="text-truncate mono-cell">{{ app.lastEvalId || t('common.notAvailable') }}</td>
              <td class="text-right">
                <div class="row-actions">
                  <v-btn size="small" icon="mdi-pencil" variant="text" @click.stop="editApplication(app)" />
                  <v-btn size="small" icon="mdi-upload" variant="text" :loading="actionLoading === `deploy:${app.id}`" @click.stop="runAction('deploy', app)" />
                  <v-btn size="small" icon="mdi-stop-circle-outline" variant="text" :disabled="!app.enabled" :loading="actionLoading === `stop:${app.id}`" @click.stop="runAction('stop', app)" />
                  <v-btn size="small" icon="mdi-restart" variant="text" :disabled="!app.enabled" :loading="actionLoading === `restart:${app.id}`" @click.stop="runAction('restart', app)" />
                  <v-btn size="small" icon="mdi-delete" color="error" variant="text" :disabled="app.enabled" :loading="actionLoading === `delete:${app.id}`" @click.stop="runAction('delete', app)" />
                </div>
              </td>
            </tr>
          </tbody>
        </v-table>
      </v-card>

      <div class="detail-column">
        <ApplicationDetail v-if="selectedApplication" :application="selectedApplication" @changed="replaceApplication" />
        <v-card v-else variant="outlined" class="empty-detail">
          <v-icon size="28" color="medium-emphasis">mdi-application-brackets-outline</v-icon>
          <div class="text-body-2 text-medium-emphasis">{{ t('applicationsPage.emptyHint') }}</div>
        </v-card>
      </div>
    </div>

    <ApplicationEditor :application="editingApplication" :open="editorOpen" @close="editorOpen = false" @saved="handleSaved" />
  </div>
</template>

<style scoped>
.page-actions { display: flex; gap: 10px; flex-wrap: wrap; justify-content: flex-end; }
.action-btn { min-height: 40px; }
.summary-strip { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; max-width: 680px; }
.summary-card { display: flex; align-items: center; gap: 12px; padding: 14px; }
.summary-icon { display: grid; place-items: center; width: 36px; height: 36px; border-radius: 8px; }
.surface-primary { color: rgb(var(--v-theme-primary)); background: rgba(var(--v-theme-primary), 0.1); }
.surface-success { color: rgb(var(--v-theme-success)); background: rgba(var(--v-theme-success), 0.1); }
.surface-warning { color: rgb(var(--v-theme-warning)); background: rgba(var(--v-theme-warning), 0.12); }
.applications-workspace { display: grid; grid-template-columns: minmax(760px, 1.15fr) minmax(420px, 0.85fr); gap: 18px; align-items: start; }
.application-list, .detail-column { min-width: 0; }
.application-list { overflow: hidden; }
.list-header { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 16px 18px 10px; }
.application-table { background: transparent; }
.application-table :deep(th) { height: 40px; color: rgba(var(--v-theme-on-surface), 0.58); font-size: 0.74rem; font-weight: 700; letter-spacing: 0; text-transform: uppercase; white-space: nowrap; }
.application-table :deep(td) { height: 58px; border-bottom-color: rgba(var(--v-border-color), 0.06); vertical-align: middle; }
.cursor-pointer { cursor: pointer; }
.application-row { transition-property: background-color; transition-duration: 160ms; transition-timing-function: ease; }
.application-row:hover { background: rgba(var(--v-theme-on-surface), 0.025); }
.application-row.selected { background: rgba(var(--v-theme-primary), 0.06); }
.name-line { display: flex; align-items: center; gap: 10px; min-width: 0; }
.min-width-0 { min-width: 0; }
.mono-cell { max-width: 190px; font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 0.8rem; }
.row-actions { display: flex; justify-content: flex-end; gap: 2px; }
.row-actions :deep(.v-btn) { min-width: 40px; min-height: 40px; }
.status-dot { flex: 0 0 auto; width: 9px; height: 9px; border-radius: 999px; background: rgb(var(--v-theme-info)); box-shadow: 0 0 0 4px rgba(var(--v-theme-info), 0.12); }
.status-dot.success { background: rgb(var(--v-theme-success)); box-shadow: 0 0 0 4px rgba(var(--v-theme-success), 0.12); }
.status-dot.warning { background: rgb(var(--v-theme-warning)); box-shadow: 0 0 0 4px rgba(var(--v-theme-warning), 0.14); }
.status-dot.error { background: rgb(var(--v-theme-error)); box-shadow: 0 0 0 4px rgba(var(--v-theme-error), 0.12); }
.status-dot.grey { background: rgb(var(--v-theme-on-surface-variant)); box-shadow: 0 0 0 4px rgba(var(--v-theme-on-surface), 0.04); }
.empty-detail { display: grid; place-items: center; gap: 10px; min-height: 220px; padding: 32px; text-align: center; }
@media (max-width: 1360px) { .applications-workspace { grid-template-columns: 1fr; } }
@media (max-width: 760px) { .page-actions, .page-actions .v-btn { width: 100%; } .summary-strip { grid-template-columns: 1fr; max-width: none; } }
</style>
