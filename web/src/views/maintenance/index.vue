<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import { LogOut, Play, RefreshCcw, RotateCcw } from '@/icons';
import { maintenanceApi } from '@/api/maintenance';
import { ApiError } from '@/api/client';
import { saveBlobDownload } from '@/api/download';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue';
import DownloadButton from '@/components/ui/DownloadButton.vue';
import Input from '@/components/ui/Input.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import FormField from '@/components/patterns/FormField.vue';
import InfoBar from '@/components/patterns/InfoBar.vue';
import ProgressBar from '@/components/patterns/ProgressBar.vue';
import SettingsCard from '@/components/patterns/SettingsCard.vue';
import SettingsSection from '@/components/patterns/SettingsSection.vue';
import { useI18n } from '@/i18n';
import type { MaintenanceStatus } from '@/types/maintenance';
import { formatDateTime } from '@/utils/datetime';

const { t } = useI18n();
const notifyError = useErrorToast();
const notifySuccess = useSuccessToast();

const status = ref<MaintenanceStatus | null>(null);
const authenticated = ref(Boolean(maintenanceApi.storedToken('export') || maintenanceApi.storedToken('restore')));
const loading = ref(false);
const pending = ref('');
const clearConfirmOpen = ref(false);
const loggingOut = ref(false);
const error = ref('');
const mode = ref<'export' | 'restore'>('export');
const form = reactive({ username: '', password: '', archivePassword: '' });
let timer: number | undefined;
let statusRequestId = 0;

const phaseTone = computed(() => {
  if (status.value?.phase === 'completed') return 'success';
  if (status.value?.phase === 'failed') return 'danger';
  if (status.value?.phase === 'password_required') return 'warning';
  return 'info';
});

async function login() {
  pending.value = 'login';
  error.value = '';
  statusRequestId += 1;
  try {
    await maintenanceApi.login(mode.value, form.username, form.password);
    authenticated.value = true;
    await load();
    schedulePoll();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('maintenancePage.loginFailed');
    notifyError(err instanceof Error ? err.message : t('maintenancePage.loginFailed'), err);
  } finally {
    pending.value = '';
  }
}

async function load() {
  if (!authenticated.value) return;
  const requestId = ++statusRequestId;
  loading.value = true;
  error.value = '';
  try {
    const next = mode.value === 'export' ? await maintenanceApi.exportStatus() : await maintenanceApi.restoreStatus();
    if (requestId !== statusRequestId) return;
    status.value = next;
  } catch (err) {
    if (requestId !== statusRequestId) return;
    if (err instanceof ApiError && err.status === 401) {
      await handleMaintenanceUnauthorized();
      return;
    }
    const message = err instanceof Error ? err.message : t('maintenancePage.loadFailed');
    notifyError(message, err);
    if (mode.value === 'export') {
      mode.value = 'restore';
      try {
        const next = await maintenanceApi.restoreStatus();
        if (requestId !== statusRequestId) return;
        status.value = next;
      } catch {
        error.value = message;
      }
    } else {
      error.value = message;
    }
  } finally {
    if (requestId === statusRequestId) loading.value = false;
  }
}

async function command(name: 'start' | 'password' | 'retry' | 'clear' | 'exit') {
  if (!status.value) return;
  statusRequestId += 1;
  pending.value = name;
  error.value = '';
  try {
    if (name === 'start') status.value = await maintenanceApi.startExport(status.value);
    if (name === 'password') status.value = mode.value === 'export' ? await maintenanceApi.submitExportPassword(status.value, form.archivePassword) : await maintenanceApi.submitRestorePassword(status.value, form.archivePassword);
    if (name === 'retry') status.value = await maintenanceApi.retryRestore(status.value);
    if (name === 'clear') status.value = await maintenanceApi.clearRestorePending(status.value);
    if (name === 'exit') status.value = await maintenanceApi.exitExport();
    notifySuccess(t('maintenancePage.commandAccepted'));
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) {
      await handleMaintenanceUnauthorized();
      return;
    }
    error.value = err instanceof Error ? err.message : t('common.operationFailed');
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    pending.value = '';
    schedulePoll();
  }
}

function openClearConfirm() {
  clearConfirmOpen.value = true;
}

async function confirmClearPending() {
  clearConfirmOpen.value = false;
  await command('clear');
}

async function handleMaintenanceUnauthorized() {
  stopPolling();
  statusRequestId += 1;
  try {
    await maintenanceApi.logout(mode.value);
  } catch {
    // token cleanup already happened inside logout
  }
  authenticated.value = false;
  status.value = null;
  error.value = '';
}

async function logout() {
  loggingOut.value = true;
  stopPolling();
  statusRequestId += 1;
  try {
    await maintenanceApi.logout(mode.value);
    authenticated.value = false;
    status.value = null;
  } finally {
    loggingOut.value = false;
  }
}

async function downloadArchive() {
  if (!status.value) return;
  pending.value = 'download';
  error.value = '';
  try {
    saveBlobDownload(await maintenanceApi.downloadExport(status.value));
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.operationFailed');
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    pending.value = '';
  }
}

function schedulePoll(delay = status.value?.pollAfterMs || 2000) {
  if (timer !== undefined || !authenticated.value) return;
  if (status.value?.phase === 'completed' || status.value?.phase === 'failed') return;
  timer = window.setTimeout(async () => {
    timer = undefined;
    if (!authenticated.value || status.value?.phase === 'completed' || status.value?.phase === 'failed') return;
    if (document.visibilityState !== 'visible') {
      schedulePoll();
      return;
    }
    await load();
    if (authenticated.value && status.value?.phase !== 'completed' && status.value?.phase !== 'failed') {
      schedulePoll(status.value?.pollAfterMs);
    }
  }, delay);
}

function stopPolling() {
  if (timer !== undefined) {
    window.clearTimeout(timer);
    timer = undefined;
  }
}

function handleVisibilityChange() {
  if (document.visibilityState === 'visible') schedulePoll();
}

onMounted(async () => {
  if (authenticated.value) await load();
  schedulePoll();
  document.addEventListener('visibilitychange', handleVisibilityChange);
});
onBeforeUnmount(() => {
  stopPolling();
  document.removeEventListener('visibilitychange', handleVisibilityChange);
});
</script>

<template>
  <!--
    独立无壳页几何：不加载 AppShell，内容列与数据页一致 1200px（max-w-content）并水平
    居中；短分组（状态 / 动作 / 清单）在 wide(≥1239px) 起两列排布，表单/登录仍收窄到
    1000px（max-w-content-narrow）。登录页的聚焦卡居中属于检查表登记的例外，不在本页套用。
  -->
  <main class="grid min-h-dvh p-6 text-foreground max-sm:p-4">
    <div class="mx-auto grid w-full max-w-content content-start gap-4">
      <header class="flex flex-wrap items-start justify-between gap-4">
        <div class="grid min-w-0 gap-2">
          <div><Badge tone="warning">{{ t('layout.alpha') }}</Badge></div>
          <h1 class="m-0 text-title font-semibold tracking-tight">{{ t('maintenancePage.title') }}</h1>
          <p class="m-0 max-w-2xl text-sm text-muted-foreground">{{ t('maintenancePage.description') }}</p>
        </div>
        <div v-if="authenticated" class="flex flex-wrap gap-2">
          <Button :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
          <Button :loading="loggingOut" @click="logout"><LogOut />{{ t('layout.logout') }}</Button>
        </div>
      </header>

      <InfoBar v-if="error" tone="danger">{{ error }}</InfoBar>

      <div v-if="!authenticated" class="mx-auto w-full max-w-content-narrow">
        <SettingsSection :title="t('maintenancePage.signIn')">
          <SettingsCard stacked>
            <FormField :label="t('auth.username')">
              <template #default="{ id, describedBy, invalid }">
                <Input :id="id" v-model="form.username" autocomplete="username" :aria-describedby="describedBy" :invalid="invalid" />
              </template>
            </FormField>
          </SettingsCard>
          <SettingsCard stacked>
            <FormField :label="t('auth.password')">
              <template #default="{ id, describedBy, invalid }">
                <Input :id="id" v-model="form.password" type="password" autocomplete="current-password" :aria-describedby="describedBy" :invalid="invalid" />
              </template>
            </FormField>
          </SettingsCard>
          <div class="flex justify-end p-4">
            <Button variant="primary" :loading="pending === 'login'" @click="login">{{ t('auth.signIn') }}</Button>
          </div>
        </SettingsSection>
      </div>

      <template v-else>
        <div class="grid gap-4 wide:grid-cols-2">
          <div class="grid content-start gap-4">
            <SettingsSection
              :title="mode === 'export' ? t('maintenancePage.exportMode') : t('maintenancePage.restoreMode')"
              :description="t('maintenancePage.modeHint')"
            >
              <SettingsCard stacked>
                <div class="grid gap-2">
                  <div class="flex flex-wrap items-center gap-2">
                    <Badge :tone="phaseTone">{{ status?.phase || t('state.unknown') }}</Badge>
                    <span class="text-caption text-muted-foreground">{{ status?.mode || t('state.unknown') }}</span>
                  </div>
                  <ProgressBar
                    :value="status?.progress || 0"
                    :label="mode === 'export' ? t('maintenancePage.exportMode') : t('maintenancePage.restoreMode')"
                  />
                </div>
              </SettingsCard>
              <SettingsCard :title="t('maintenancePage.revision')">
                <template #control><span class="text-body text-foreground">{{ status?.revision ?? '-' }}</span></template>
              </SettingsCard>
              <SettingsCard :title="t('maintenancePage.startedAt')">
                <template #control><span class="text-body text-foreground">{{ formatDateTime(status?.startedAt) || t('common.never') }}</span></template>
              </SettingsCard>
              <SettingsCard :title="t('maintenancePage.restart')">
                <template #control><span class="text-body text-foreground">{{ status?.restartSupported ? t('state.healthy') : t('common.notAvailable') }}</span></template>
              </SettingsCard>
            </SettingsSection>

            <InfoBar v-if="status?.error || status?.errorDetail" tone="danger">{{ status.errorDetail?.message || status.error }}</InfoBar>
          </div>

          <div class="grid content-start gap-4">
            <SettingsSection :title="t('maintenancePage.actions')">
              <div class="grid gap-2 p-4">
                <Button v-if="status?.capabilities.canStart" variant="primary" :loading="pending === 'start'" @click="command('start')"><Play />{{ t('maintenancePage.startExport') }}</Button>
                <template v-if="status?.capabilities.canSubmitPassword">
                  <FormField :label="t('maintenancePage.archivePassword')">
                    <template #default="{ id, describedBy, invalid }">
                      <Input :id="id" v-model="form.archivePassword" type="password" :aria-describedby="describedBy" :invalid="invalid" />
                    </template>
                  </FormField>
                  <Button variant="primary" :disabled="!form.archivePassword" :loading="pending === 'password'" @click="command('password')">{{ t('maintenancePage.submitPassword') }}</Button>
                </template>
                <Button v-if="status?.capabilities.canRetry" :loading="pending === 'retry'" @click="command('retry')"><RotateCcw />{{ t('common.retry') }}</Button>
                <Button v-if="status?.capabilities.canClearPending && status.phase !== 'applying'" variant="danger" :loading="pending === 'clear'" @click="openClearConfirm">{{ t('maintenancePage.clearPending') }}</Button>
                <DownloadButton
                  v-if="status?.capabilities.canDownload"
                  :loading="pending === 'download'"
                  :label="t('maintenancePage.download')"
                  @click="downloadArchive"
                />
                <Button v-if="status?.capabilities.canExit" :loading="pending === 'exit'" @click="command('exit')">{{ t('maintenancePage.exit') }}</Button>
              </div>
            </SettingsSection>

            <SettingsSection :title="t('maintenancePage.manifest')">
              <SettingsCard :title="t('maintenancePage.panelVersion')">
                <template #control><span class="text-body text-foreground">{{ status?.manifest?.panelVersion || '-' }}</span></template>
              </SettingsCard>
              <SettingsCard :title="t('maintenancePage.encrypted')">
                <template #control><span class="text-body text-foreground">{{ status?.manifest?.encrypted ? t('state.healthy') : t('common.notAvailable') }}</span></template>
              </SettingsCard>
              <SettingsCard :title="t('maintenancePage.includes')" stacked>
                <template #control><span class="break-words text-body text-foreground">{{ status?.manifest?.includes?.join(', ') || '-' }}</span></template>
              </SettingsCard>
            </SettingsSection>
          </div>
        </div>
      </template>

      <ConfirmDialog
        :open="clearConfirmOpen"
        :title="t('maintenancePage.clearPending')"
        :impact="t('maintenancePage.clearPendingImpact')"
        tone="danger"
        :loading="pending === 'clear'"
        :confirm-label="t('common.delete')"
        :cancel-label="t('common.cancel')"
        :checkbox-label="t('common.confirm')"
        @update:open="(value) => { if (!value) clearConfirmOpen = false }"
        @confirm="confirmClearPending"
        @cancel="clearConfirmOpen = false"
      />
    </div>
  </main>
</template>
