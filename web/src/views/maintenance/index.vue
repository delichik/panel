<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import { Download, LogOut, Play, RefreshCcw, RotateCcw, ShieldCheck } from '@lucide/vue';
import { maintenanceApi } from '@/api/maintenance';
import { saveBlobDownload } from '@/api/download';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import Input from '@/components/ui/Input.vue';
import { useErrorToast } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { MaintenanceStatus } from '@/types/maintenance';

const { t } = useI18n();
const notifyError = useErrorToast();

const status = ref<MaintenanceStatus | null>(null);
const authenticated = ref(Boolean(maintenanceApi.storedToken('export') || maintenanceApi.storedToken('restore')));
const loading = ref(false);
const pending = ref('');
const loggingOut = ref(false);
const error = ref('');
const feedback = ref('');
const mode = ref<'export' | 'restore'>('export');
const form = reactive({ username: '', password: '', archivePassword: '' });
let timer: number | undefined;

const phaseTone = computed(() => {
  if (status.value?.phase === 'completed') return 'success';
  if (status.value?.phase === 'failed') return 'danger';
  if (status.value?.phase === 'password_required') return 'warning';
  return 'info';
});

async function login() {
  pending.value = 'login';
  error.value = '';
  try {
    await maintenanceApi.login(mode.value, form.username, form.password);
    authenticated.value = true;
    await load();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('maintenancePage.loginFailed');
    notifyError(err instanceof Error ? err.message : t('maintenancePage.loginFailed'));
  } finally {
    pending.value = '';
  }
}

async function load() {
  if (!authenticated.value) return;
  loading.value = true;
  error.value = '';
  try {
    status.value = mode.value === 'export' ? await maintenanceApi.exportStatus() : await maintenanceApi.restoreStatus();
  } catch (err) {
    const message = err instanceof Error ? err.message : t('maintenancePage.loadFailed');
    notifyError(message);
    if (mode.value === 'export') {
      mode.value = 'restore';
      try {
        status.value = await maintenanceApi.restoreStatus();
      } catch {
        error.value = message;
      }
    } else {
      error.value = message;
    }
  } finally {
    loading.value = false;
  }
}

async function command(name: 'start' | 'password' | 'retry' | 'clear' | 'exit') {
  if (!status.value) return;
  pending.value = name;
  error.value = '';
  feedback.value = '';
  try {
    if (name === 'start') status.value = await maintenanceApi.startExport(status.value);
    if (name === 'password') status.value = mode.value === 'export' ? await maintenanceApi.submitExportPassword(status.value, form.archivePassword) : await maintenanceApi.submitRestorePassword(status.value, form.archivePassword);
    if (name === 'retry') status.value = await maintenanceApi.retryRestore(status.value);
    if (name === 'clear') status.value = await maintenanceApi.clearRestorePending(status.value);
    if (name === 'exit') status.value = await maintenanceApi.exitExport();
    feedback.value = t('maintenancePage.commandAccepted');
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.operationFailed');
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'));
  } finally {
    pending.value = '';
  }
}

async function logout() {
  loggingOut.value = true;
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
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'));
  } finally {
    pending.value = '';
  }
}

function startPolling() {
  timer = window.setInterval(() => {
    if (authenticated.value && status.value?.phase !== 'completed' && status.value?.phase !== 'failed') void load();
  }, status.value?.pollAfterMs || 2000);
}

onMounted(async () => {
  if (authenticated.value) await load();
  startPolling();
});
onBeforeUnmount(() => window.clearInterval(timer));
</script>

<template>
  <main class="grid min-h-dvh bg-background p-6 text-foreground max-sm:p-4">
    <section class="mx-auto grid w-full max-w-6xl gap-5 self-center">
      <header class="flex items-start justify-between gap-4 max-md:grid">
        <div>
          <Badge tone="warning">{{ t('layout.alpha') }}</Badge>
          <h1 class="mt-3 text-3xl font-semibold tracking-normal">{{ t('maintenancePage.title') }}</h1>
          <p class="m-0 mt-2 max-w-2xl text-sm text-muted-foreground">{{ t('maintenancePage.description') }}</p>
        </div>
        <div v-if="authenticated" class="flex flex-wrap gap-2">
          <Button :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
          <Button :loading="loggingOut" @click="logout"><LogOut />{{ t('layout.logout') }}</Button>
        </div>
      </header>

      <section v-if="!authenticated" class="grid gap-4 rounded-2xl border border-border bg-card p-5 md:max-w-md">
        <div class="flex items-center gap-3"><ShieldCheck class="size-5 text-muted-foreground" /><h2 class="m-0 text-lg font-semibold">{{ t('maintenancePage.signIn') }}</h2></div>
        <label class="grid gap-1 text-sm">{{ t('auth.username') }}<Input v-model="form.username" autocomplete="username" /></label>
        <label class="grid gap-1 text-sm">{{ t('auth.password') }}<Input v-model="form.password" type="password" autocomplete="current-password" /></label>
        <Button variant="primary" :loading="pending === 'login'" @click="login">{{ t('auth.signIn') }}</Button>
      </section>

      <section v-else class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_360px]">
        <article class="grid gap-4 rounded-2xl border border-border bg-card p-5">
          <div class="flex items-center justify-between gap-3">
            <div>
              <h2 class="m-0 text-xl font-semibold">{{ mode === 'export' ? t('maintenancePage.exportMode') : t('maintenancePage.restoreMode') }}</h2>
              <p class="m-0 mt-1 text-sm text-muted-foreground">{{ status?.mode || t('state.unknown') }}</p>
            </div>
            <Badge :tone="phaseTone">{{ status?.phase || t('state.unknown') }}</Badge>
          </div>
          <div class="h-3 overflow-hidden rounded-full bg-muted"><div class="h-full bg-primary transition-all" :style="{ width: `${status?.progress || 0}%` }" /></div>
          <div class="grid grid-cols-3 gap-3 max-md:grid-cols-1">
            <div class="rounded-xl border border-border bg-background p-3"><span>{{ t('maintenancePage.revision') }}</span><strong>{{ status?.revision ?? '-' }}</strong></div>
            <div class="rounded-xl border border-border bg-background p-3"><span>{{ t('maintenancePage.startedAt') }}</span><strong>{{ status?.startedAt || t('common.never') }}</strong></div>
            <div class="rounded-xl border border-border bg-background p-3"><span>{{ t('maintenancePage.restart') }}</span><strong>{{ status?.restartSupported ? t('state.healthy') : t('common.notAvailable') }}</strong></div>
          </div>
          <div v-if="status?.error || status?.errorDetail" class="rounded-xl border border-danger-border bg-danger-bg p-3 text-sm text-danger">{{ status.errorDetail?.message || status.error }}</div>
          <div v-if="feedback" class="rounded-xl border border-success-border bg-success-bg p-3 text-sm text-success">{{ feedback }}</div>
        </article>

        <aside class="grid content-start gap-4">
          <section class="grid gap-3 rounded-2xl border border-border bg-card p-5">
            <h3 class="m-0 text-sm font-semibold">{{ t('maintenancePage.actions') }}</h3>
            <Button v-if="status?.capabilities.canStart" variant="primary" :loading="pending === 'start'" @click="command('start')"><Play />{{ t('maintenancePage.startExport') }}</Button>
            <template v-if="status?.capabilities.canSubmitPassword">
              <label class="grid gap-1 text-sm">{{ t('maintenancePage.archivePassword') }}<Input v-model="form.archivePassword" type="password" /></label>
              <Button variant="primary" :disabled="!form.archivePassword" :loading="pending === 'password'" @click="command('password')">{{ t('maintenancePage.submitPassword') }}</Button>
            </template>
            <Button v-if="status?.capabilities.canRetry" :loading="pending === 'retry'" @click="command('retry')"><RotateCcw />{{ t('common.retry') }}</Button>
            <Button v-if="status?.capabilities.canClearPending" variant="danger" :loading="pending === 'clear'" @click="command('clear')">{{ t('maintenancePage.clearPending') }}</Button>
            <Button v-if="status?.capabilities.canDownload" :loading="pending === 'download'" @click="downloadArchive"><Download />{{ t('maintenancePage.download') }}</Button>
            <Button v-if="status?.capabilities.canExit" :loading="pending === 'exit'" @click="command('exit')">{{ t('maintenancePage.exit') }}</Button>
          </section>

          <section class="rounded-2xl border border-border bg-card p-5">
            <h3 class="m-0 text-sm font-semibold">{{ t('maintenancePage.manifest') }}</h3>
            <div class="mt-3 grid gap-2 text-sm">
              <div><span>{{ t('maintenancePage.panelVersion') }}</span><strong>{{ status?.manifest?.panelVersion || '-' }}</strong></div>
              <div><span>{{ t('maintenancePage.encrypted') }}</span><strong>{{ status?.manifest?.encrypted ? t('state.healthy') : t('common.notAvailable') }}</strong></div>
              <div><span>{{ t('maintenancePage.includes') }}</span><strong>{{ status?.manifest?.includes?.join(', ') || '-' }}</strong></div>
            </div>
          </section>
        </aside>
      </section>
    </section>
  </main>
</template>

<style scoped>
span {
  display: block;
  color: hsl(var(--muted-foreground));
  font-size: 12px;
}

strong {
  display: block;
  overflow-wrap: anywhere;
  color: hsl(var(--foreground));
  font-size: 13px;
}
</style>
