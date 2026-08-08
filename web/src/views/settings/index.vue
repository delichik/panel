<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { AlertTriangle, DatabaseBackup, KeyRound, RefreshCcw, Save, Shield, UploadCloud } from '@lucide/vue';
import { keyAssetsApi } from '@/api/keyAssets';
import { settingsApi } from '@/api/settings';
import { systemApi, type VersionInfo } from '@/api/system';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import Dialog from '@/components/ui/Dialog.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import Input from '@/components/ui/Input.vue';
import Select from '@/components/ui/Select.vue';
import Switch from '@/components/ui/Switch.vue';
import Textarea from '@/components/ui/Textarea.vue';
import LoadingOverlay from '@/components/ui/LoadingOverlay.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import SettingsPage from '@/components/templates/SettingsPage.vue';
import { useI18n } from '@/i18n';
import { useSessionStore } from '@/stores/session';
import type { SystemCertificateDto } from '@/types/keyAssets';
import type { RestorePreflightResponse, RuntimeSettings, RuntimeUpdate, ServerVariableDefinition } from '@/types/settings';
import { createLatestRequestGuard } from '@/views/_shared/requestState';
import { formatDateTime } from '@/utils/datetime';

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const session = useSessionStore();
const notifyError = useErrorToast();
const notifySuccess = useSuccessToast();

const runtime = ref<RuntimeSettings | null>(null);
const version = ref<VersionInfo | null>(null);
const serverVariables = ref<ServerVariableDefinition[]>([]);
const systemCertificates = ref<SystemCertificateDto[]>([]);
const activeSection = ref(sectionFromPath(route.path));
const loading = ref(false);
const listRequests = createLatestRequestGuard();
const pending = ref('');
const error = ref('');
const actionError = ref('');
const confirmOpen = ref(false);
const confirmKind = ref<'export' | 'restore' | 'system' | 'system-certificate'>('export');
const selectedSystemCertificate = ref<SystemCertificateDto | null>(null);
const preflight = ref<RestorePreflightResponse | null>(null);
const restoreFile = ref<File | null>(null);
const exportPending = ref(false);
const restorePending = ref(false);
const restarting = ref(false);

const form = reactive({
  metricsRetentionDays: '14',
  metricsCollectionIntervalSeconds: '60',
  containerReportIntervalSeconds: '30',
  cleanupSchedule: 'daily',
  runtimeEventRetentionDays: '30',
  runtimeEventDetailRetentionDays: '7',
  runtimeEventCleanupSchedule: 'daily',
  tokenExpiration: '1d',
  language: 'zh-CN',
  logLevel: 'info',
  remoteCommandTimeoutSeconds: '45',
  jwtSecret: '',
  loginTitle: '',
  loginSubtitle: '',
  certificateEmail: '',
  dnsPropagationDelaySeconds: '30',
  variablesText: '',
  exportEncrypt: true,
  exportPassword: '',
  restorePassword: '',
  restoreConfirm: false,
});

const sections = computed(() => [
  { key: 'general', label: t('settingsPage.section.runtime'), to: '/settings/general' },
  { key: 'security', label: t('settingsPage.section.security'), to: '/settings/security' },
  { key: 'certificates', label: t('settingsPage.section.certificates'), to: '/settings/certificates' },
  { key: 'system-certificates', label: t('settingsPage.section.systemCertificates'), to: '/settings/system-certificates' },
  { key: 'system', label: t('settingsPage.section.system'), to: '/settings/system' },
  { key: 'backups', label: t('settingsPage.section.backups'), to: '/settings/backups' },
]);
const cleanupOptions = computed(() => ['hourly', 'daily', 'weekly'].map((value) => ({ label: t(`settingsPage.cleanup.${value}`), value })));
const tokenOptions = computed(() => ['10m', '1h', '1d', '5d', '30d', 'never'].map((value) => ({ label: t(`settingsPage.token.${value}`), value })));
const languageOptions = computed(() => [{ label: t('settingsPage.language.en'), value: 'en' }, { label: t('settingsPage.language.zh'), value: 'zh-CN' }]);
const logOptions = computed(() => ['debug', 'info', 'warn', 'error'].map((value) => ({ label: value, value })));
const jwtSecretValid = computed(() => form.jwtSecret.trim().length >= 16);
const runtimeEventRetentionValid = computed(() => {
  const retention = Number(form.runtimeEventRetentionDays);
  const detailRetention = Number(form.runtimeEventDetailRetentionDays);
  return Number.isFinite(retention) && Number.isFinite(detailRetention) && retention >= detailRetention;
});

watch(() => route.path, (path) => {
  activeSection.value = sectionFromPath(path);
});

async function load() {
  const requestId = listRequests.begin();
  loading.value = true;
  error.value = '';
  try {
    const [settingsResult, variablesResult, versionResult, certsResult] = await Promise.allSettled([
      settingsApi.runtime(),
      settingsApi.serverVariables(),
      systemApi.version(),
      keyAssetsApi.systemCertificates(),
    ]);
    if (!listRequests.isCurrent(requestId)) return;
    let firstError = '';
    if (settingsResult.status === 'fulfilled') runtime.value = settingsResult.value;
    else firstError = settingsResult.reason instanceof Error ? settingsResult.reason.message : t('settingsPage.loadFailed');
    if (variablesResult.status === 'fulfilled') serverVariables.value = variablesResult.value;
    else if (!firstError) firstError = variablesResult.reason instanceof Error ? variablesResult.reason.message : t('settingsPage.loadFailed');
    if (versionResult.status === 'fulfilled') version.value = versionResult.value;
    else if (!firstError) firstError = versionResult.reason instanceof Error ? versionResult.reason.message : t('settingsPage.loadFailed');
    if (certsResult.status === 'fulfilled') systemCertificates.value = certsResult.value;
    else if (!firstError) firstError = certsResult.reason instanceof Error ? certsResult.reason.message : t('settingsPage.loadFailed');
    if (settingsResult.status === 'fulfilled' && variablesResult.status === 'fulfilled') {
      hydrate(settingsResult.value, variablesResult.value);
    }
    if (firstError) {
      error.value = firstError;
      notifyError(firstError);
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('settingsPage.loadFailed');
    notifyError(err instanceof Error ? err.message : t('settingsPage.loadFailed'));
  } finally {
    if (listRequests.isCurrent(requestId)) loading.value = false;
  }
}

function hydrate(settings: RuntimeSettings, variables: ServerVariableDefinition[]) {
  Object.assign(form, {
    metricsRetentionDays: String(settings.metricsRetentionDays),
    metricsCollectionIntervalSeconds: String(settings.metricsCollectionIntervalSeconds),
    containerReportIntervalSeconds: String(settings.containerReportIntervalSeconds),
    cleanupSchedule: settings.cleanupSchedule,
    runtimeEventRetentionDays: String(settings.runtimeEventRetentionDays),
    runtimeEventDetailRetentionDays: String(settings.runtimeEventDetailRetentionDays),
    runtimeEventCleanupSchedule: settings.runtimeEventCleanupSchedule,
    tokenExpiration: settings.tokenExpiration,
    language: settings.language,
    logLevel: settings.logLevel,
    remoteCommandTimeoutSeconds: String(settings.remoteCommandTimeoutSeconds),
    jwtSecret: '',
    loginTitle: settings.branding.loginTitle,
    loginSubtitle: settings.branding.loginSubtitle,
    certificateEmail: settings.certificates.email,
    dnsPropagationDelaySeconds: String(settings.certificates.dnsPropagationDelaySeconds),
    variablesText: variables.map((item) => `${item.required ? '*' : ''}${item.key}=${item.name}`).join('\n'),
  });
}

async function resetJwtSecret() {
  if (!jwtSecretValid.value) {
    actionError.value = t('settingsPage.jwtSecretValidation');
    return;
  }
  await run('jwt-secret', async () => {
    await session.updateJwtSecret(form.jwtSecret.trim());
    if (runtime.value) runtime.value.jwtSecretConfigured = true;
    form.jwtSecret = '';
    notifySuccess(t('settingsPage.jwtSecretReset'));
  });
}

async function resetSystemCertificate() {
  const cert = selectedSystemCertificate.value;
  if (!cert) return;
  await run(`system-certificate-${cert.id}`, async () => {
    const result = await keyAssetsApi.resetSystemCertificate(cert.id);
    notifySuccess(t('settingsPage.systemCertificateResetAccepted', { taskId: result.taskId }));
    confirmOpen.value = false;
    selectedSystemCertificate.value = null;
  });
}

async function saveRuntimeSection(kind: 'runtime' | 'security' | 'certificates' | 'system') {
  if (!runtime.value) return;
  if (kind === 'runtime' && !runtimeEventRetentionValid.value) {
    actionError.value = t('settingsPage.runtimeEventRetentionValidation');
    return;
  }
  await run(`save-${kind}`, async () => {
    runtime.value = await settingsApi.updateRuntime(buildRuntimeUpdate(kind));
    hydrate(runtime.value, serverVariables.value);
    notifySuccess(t(`settingsPage.saved.${kind}`));
  });
}

async function saveVariables() {
  await run('save-variables', async () => {
    serverVariables.value = await settingsApi.updateServerVariables(parseVariables(form.variablesText));
    notifySuccess(t('settingsPage.saved.system'));
  });
}

async function startBackup() {
  await run('export', async () => {
    const result = await settingsApi.startBackupExport({ encrypt: form.exportEncrypt, password: form.exportPassword || undefined });
    exportPending.value = true;
    notifySuccess(t('settingsPage.backupStarted', { exportId: result.exportId }));
    confirmOpen.value = false;
  });
}

async function handleRestoreFile(event: Event) {
  const input = event.target as HTMLInputElement;
  restoreFile.value = input.files?.[0] ?? null;
  preflight.value = null;
  if (!restoreFile.value) return;
  await run('preflight', async () => {
    preflight.value = await settingsApi.preflightRestore(restoreFile.value!, form.restorePassword);
  });
}

async function confirmRestore() {
  if (!restoreFile.value) return;
  await run('restore', async () => {
    await settingsApi.confirmRestore(restoreFile.value!, form.restorePassword, form.restoreConfirm);
    restorePending.value = true;
    restarting.value = true;
    notifySuccess(t('settingsPage.restorePending'));
    confirmOpen.value = false;
  });
}

async function run(name: string, action: () => Promise<void>) {
  pending.value = name;
  actionError.value = '';
  try {
    await action();
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'));
  } finally {
    pending.value = '';
  }
}

function buildRuntimeUpdate(kind: 'runtime' | 'security' | 'certificates' | 'system'): RuntimeUpdate {
  const current = runtime.value!;
  const update: RuntimeUpdate = {
    metricsRetentionDays: current.metricsRetentionDays,
    metricsCollectionIntervalSeconds: current.metricsCollectionIntervalSeconds,
    containerReportIntervalSeconds: current.containerReportIntervalSeconds,
    cleanupSchedule: current.cleanupSchedule,
    runtimeEventRetentionDays: current.runtimeEventRetentionDays,
    runtimeEventDetailRetentionDays: current.runtimeEventDetailRetentionDays,
    runtimeEventCleanupSchedule: current.runtimeEventCleanupSchedule,
    tokenExpiration: current.tokenExpiration,
    language: current.language,
    logLevel: current.logLevel,
    remoteCommandTimeoutSeconds: current.remoteCommandTimeoutSeconds,
    branding: current.branding,
    certificates: current.certificates,
  };
  if (kind === 'runtime') {
    update.metricsRetentionDays = numberField(form.metricsRetentionDays, current.metricsRetentionDays);
    update.metricsCollectionIntervalSeconds = numberField(form.metricsCollectionIntervalSeconds, current.metricsCollectionIntervalSeconds);
    update.containerReportIntervalSeconds = numberField(form.containerReportIntervalSeconds, current.containerReportIntervalSeconds);
    update.cleanupSchedule = form.cleanupSchedule;
    update.runtimeEventRetentionDays = numberField(form.runtimeEventRetentionDays, current.runtimeEventRetentionDays);
    update.runtimeEventDetailRetentionDays = numberField(form.runtimeEventDetailRetentionDays, current.runtimeEventDetailRetentionDays);
    update.runtimeEventCleanupSchedule = form.runtimeEventCleanupSchedule;
    update.language = form.language;
    update.logLevel = form.logLevel;
  }
  if (kind === 'security') {
    update.tokenExpiration = form.tokenExpiration;
    update.remoteCommandTimeoutSeconds = numberField(form.remoteCommandTimeoutSeconds, current.remoteCommandTimeoutSeconds);
  }
  if (kind === 'certificates') {
    update.certificates = {
      email: form.certificateEmail,
      dnsPropagationDelaySeconds: numberField(form.dnsPropagationDelaySeconds, current.certificates.dnsPropagationDelaySeconds),
    };
  }
  if (kind === 'system') {
    update.branding = { loginTitle: form.loginTitle, loginSubtitle: form.loginSubtitle };
  }
  return update;
}

function parseVariables(raw: string): ServerVariableDefinition[] {
  return raw.split(/\r?\n/).map((line) => line.trim()).filter(Boolean).map((line) => {
    const required = line.startsWith('*');
    const clean = required ? line.slice(1) : line;
    const [key, ...nameParts] = clean.split('=');
    return { key: key.trim(), name: (nameParts.join('=') || key).trim(), required };
  });
}

function numberField(value: string, fallback: number) {
  const next = Number(value);
  return Number.isFinite(next) ? next : fallback;
}

function certificateTone(status?: string): 'neutral' | 'success' | 'warning' | 'danger' | 'info' {
  if (status === 'valid') return 'success';
  if (status === 'expiring') return 'warning';
  if (status === 'expired') return 'danger';
  return 'neutral';
}

function certificateStatus(status?: string) {
  return status ? t(`settingsPage.certificateStatus.${status}`) : t('state.unknown');
}

function certificateType(type: string) {
  return type ? t(`settingsPage.certificateType.${type}`) : t('common.notAvailable');
}

function sectionFromPath(path: string) {
  return path.split('/').filter(Boolean).at(-1) || 'general';
}

function openConfirm(kind: typeof confirmKind.value) {
  confirmKind.value = kind;
  confirmOpen.value = true;
}

function openSystemCertificateConfirm(cert: SystemCertificateDto) {
  selectedSystemCertificate.value = cert;
  openConfirm('system-certificate');
}

function confirmAction() {
  if (confirmKind.value === 'restore') return confirmRestore();
  if (confirmKind.value === 'system-certificate') return resetSystemCertificate();
  return startBackup();
}

onMounted(load);
</script>

<template>
  <ConsolePage :title="t('routes.settings.title')" :description="t('routes.settings.description')">
    <template #actions>
      <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
    </template>

    <div class="relative grid h-full min-h-[640px] grid-cols-[260px_minmax(0,1fr)] gap-4 max-lg:grid-cols-1">
      <aside class="min-h-0 overflow-auto rounded-2xl border border-border bg-card p-2">
        <button v-for="section in sections" :key="section.key" type="button" class="mb-1 flex w-full items-center justify-between rounded-xl px-3 py-2 text-left text-sm hover:bg-accent" :class="activeSection === section.key ? 'bg-background text-foreground' : 'text-muted-foreground'" @click="router.push(section.to)">
          {{ section.label }}
          <Badge v-if="section.key === 'backups' && (exportPending || restorePending)" tone="warning">{{ restarting ? t('settingsPage.restarting') : t('settingsPage.pending') }}</Badge>
        </button>
      </aside>
      <LoadingOverlay v-if="loading && !runtime" />

      <SettingsPage>
        <section v-if="actionError" class="grid gap-2">
          <div v-if="actionError" class="rounded-xl border border-danger-border bg-danger-bg p-3 text-sm text-danger">{{ actionError }}</div>
        </section>

        <section v-if="activeSection === 'general'" class="grid gap-4 rounded-2xl border border-border bg-card p-5">
          <h2>{{ t('settingsPage.section.runtime') }}</h2>
          <div class="grid grid-cols-2 gap-3 max-md:grid-cols-1">
            <label class="grid gap-1 text-sm">{{ t('settingsPage.metricsRetention') }}<Input v-model="form.metricsRetentionDays" type="number" /></label>
            <label class="grid gap-1 text-sm">{{ t('settingsPage.metricsInterval') }}<Input v-model="form.metricsCollectionIntervalSeconds" type="number" /></label>
            <label class="grid gap-1 text-sm">{{ t('settingsPage.containerInterval') }}<Input v-model="form.containerReportIntervalSeconds" type="number" /></label>
            <label class="grid gap-1 text-sm">{{ t('settingsPage.cleanupSchedule') }}<Select v-model="form.cleanupSchedule" :options="cleanupOptions" /></label>
            <label class="grid gap-1 text-sm">{{ t('settingsPage.runtimeEventRetention') }}<Input v-model="form.runtimeEventRetentionDays" type="number" min="1" /></label>
            <label class="grid gap-1 text-sm">{{ t('settingsPage.runtimeEventDetailRetention') }}<Input v-model="form.runtimeEventDetailRetentionDays" type="number" min="1" /></label>
            <label class="grid gap-1 text-sm">{{ t('settingsPage.runtimeEventCleanupSchedule') }}<Select v-model="form.runtimeEventCleanupSchedule" :options="cleanupOptions" /></label>
            <label class="grid gap-1 text-sm">{{ t('settingsPage.language') }}<Select v-model="form.language" :options="languageOptions" /></label>
            <label class="grid gap-1 text-sm">{{ t('settingsPage.logLevel') }}<Select v-model="form.logLevel" :options="logOptions" /></label>
          </div>
          <p v-if="!runtimeEventRetentionValid" class="m-0 rounded-xl border border-danger-border bg-danger-bg p-3 text-sm text-danger">{{ t('settingsPage.runtimeEventRetentionValidation') }}</p>
          <Button class="w-fit" variant="primary" :disabled="!runtimeEventRetentionValid" :loading="pending === 'save-runtime'" @click="saveRuntimeSection('runtime')"><Save />{{ t('settingsPage.saveSection') }}</Button>
        </section>

        <section v-else-if="activeSection === 'security'" class="grid gap-4 rounded-2xl border border-border bg-card p-5">
          <h2>{{ t('settingsPage.section.security') }}</h2>
          <div class="grid grid-cols-2 gap-3 max-md:grid-cols-1">
            <label class="grid gap-1 text-sm">{{ t('settingsPage.tokenExpiration') }}<Select v-model="form.tokenExpiration" :options="tokenOptions" /></label>
            <label class="grid gap-1 text-sm">{{ t('settingsPage.remoteTimeout') }}<Input v-model="form.remoteCommandTimeoutSeconds" type="number" /></label>
          </div>
          <div class="grid gap-3 rounded-xl border border-border bg-background p-4">
            <div class="flex flex-wrap items-center justify-between gap-3 text-sm">
              <span class="text-muted-foreground">{{ t('settingsPage.jwtSecretConfigured') }}</span>
              <Badge :tone="runtime?.jwtSecretConfigured ? 'success' : 'warning'">{{ runtime?.jwtSecretConfigured ? t('state.healthy') : t('state.warning') }}</Badge>
            </div>
            <label class="grid gap-1 text-sm">{{ t('settingsPage.jwtSecret') }}<Input v-model="form.jwtSecret" type="password" :placeholder="t('settingsPage.jwtSecretHint')" /></label>
            <Button class="w-fit" :disabled="!form.jwtSecret.trim()" :loading="pending === 'jwt-secret'" @click="resetJwtSecret"><KeyRound />{{ t('settingsPage.resetJwtSecret') }}</Button>
          </div>
          <Button class="w-fit" variant="primary" :loading="pending === 'save-security'" @click="saveRuntimeSection('security')"><Shield />{{ t('settingsPage.saveSection') }}</Button>
        </section>

        <section v-else-if="activeSection === 'certificates'" class="grid gap-4 rounded-2xl border border-border bg-card p-5">
          <h2>{{ t('settingsPage.section.certificates') }}</h2>
          <label class="grid gap-1 text-sm">{{ t('settingsPage.certificateEmail') }}<Input v-model="form.certificateEmail" /></label>
          <label class="grid gap-1 text-sm">{{ t('settingsPage.dnsDelay') }}<Input v-model="form.dnsPropagationDelaySeconds" type="number" /></label>
          <Button class="w-fit" variant="primary" :loading="pending === 'save-certificates'" @click="saveRuntimeSection('certificates')"><Save />{{ t('settingsPage.saveSection') }}</Button>
        </section>

        <section v-else-if="activeSection === 'system-certificates'" class="grid gap-4 rounded-2xl border border-border bg-card p-5">
          <h2>{{ t('settingsPage.section.systemCertificates') }}</h2>
          <p class="m-0 text-sm text-muted-foreground">{{ t('settingsPage.systemCertificatesHint') }}</p>
          <EmptyState v-if="!systemCertificates.length" :title="t('settingsPage.systemCertificatesEmpty')" :description="t('settingsPage.systemCertificatesEmptyHint')" />
          <div v-else class="grid gap-3 motion-stagger">
            <article v-for="cert in systemCertificates" :key="cert.id" class="motion-reveal grid gap-3 rounded-xl border border-border bg-background p-4">
              <div class="flex min-w-0 flex-wrap items-start justify-between gap-3">
                <div class="min-w-0">
                  <h3 class="truncate">{{ cert.name }}</h3>
                  <p class="m-0 truncate text-xs text-muted-foreground">{{ cert.commonName || cert.id }}</p>
                </div>
                <div class="flex flex-wrap items-center gap-2">
                  <Badge tone="info">{{ certificateType(cert.type) }}</Badge>
                  <Badge :tone="certificateTone(cert.status)">{{ certificateStatus(cert.status) }}</Badge>
                </div>
              </div>
              <div class="grid gap-3 text-sm lg:grid-cols-[minmax(0,1.25fr)_minmax(0,1fr)_minmax(0,1fr)]">
                <div class="min-w-0">
                  <span class="text-muted-foreground">{{ t('settingsPage.systemCertificateFingerprint') }}</span>
                  <strong class="block truncate font-mono text-xs text-foreground">{{ cert.fingerprint || t('common.notAvailable') }}</strong>
                </div>
                <div>
                  <span class="text-muted-foreground">{{ t('settingsPage.systemCertificateNotBefore') }}</span>
                  <strong class="block text-foreground">{{ formatDateTime(cert.notBefore, t('common.notAvailable')) }}</strong>
                </div>
                <div>
                  <span class="text-muted-foreground">{{ t('settingsPage.systemCertificateNotAfter') }}</span>
                  <strong class="block text-foreground">{{ formatDateTime(cert.notAfter, t('common.notAvailable')) }}</strong>
                </div>
              </div>
              <div class="flex flex-wrap items-center justify-between gap-3">
                <span class="text-xs text-muted-foreground">{{ cert.serverName ? t('settingsPage.systemCertificateServer', { server: cert.serverName }) : t('settingsPage.systemCertificateBuiltIn') }}</span>
                <Button class="w-fit" :disabled="!cert.canReset" :loading="pending === `system-certificate-${cert.id}`" @click="openSystemCertificateConfirm(cert)"><KeyRound />{{ t('settingsPage.systemCertificateReset') }}</Button>
              </div>
            </article>
          </div>
        </section>

        <section v-else-if="activeSection === 'system'" class="grid gap-4 rounded-2xl border border-border bg-card p-5">
          <h2>{{ t('settingsPage.section.system') }}</h2>
          <div class="grid gap-3 rounded-xl border border-border bg-background p-4 text-sm">
            <div class="grid grid-cols-2 gap-3 max-md:grid-cols-1">
              <div><span class="text-muted-foreground">{{ t('settingsPage.panelVersion') }}</span><strong class="block text-foreground">{{ version?.version || t('common.notAvailable') }}</strong></div>
              <div><span class="text-muted-foreground">{{ t('settingsPage.releaseChannel') }}</span><strong class="block text-foreground">{{ version?.channel || t('common.notAvailable') }}</strong></div>
              <div><span class="text-muted-foreground">{{ t('settingsPage.latestVersion') }}</span><strong class="block text-foreground">{{ version?.latestVersion || t('common.notAvailable') }}</strong></div>
              <div><span class="text-muted-foreground">{{ t('settingsPage.updateStatus') }}</span><Badge :tone="version?.updateAvailable ? 'warning' : 'success'">{{ version?.updateAvailable ? t('settingsPage.updateAvailable') : t('settingsPage.upToDate') }}</Badge></div>
            </div>
          </div>
          <div class="grid grid-cols-2 gap-3 max-md:grid-cols-1">
            <label class="grid gap-1 text-sm">{{ t('settingsPage.loginTitle') }}<Input v-model="form.loginTitle" /></label>
            <label class="grid gap-1 text-sm">{{ t('settingsPage.loginSubtitle') }}<Input v-model="form.loginSubtitle" /></label>
          </div>
          <label class="grid gap-1 text-sm">{{ t('settingsPage.serverVariables') }}<Textarea v-model="form.variablesText" class="min-h-[180px] font-mono" :placeholder="t('settingsPage.serverVariablesHint')" /></label>
          <div class="flex flex-wrap gap-2"><Button :loading="pending === 'save-system'" variant="primary" @click="saveRuntimeSection('system')"><Save />{{ t('settingsPage.saveBranding') }}</Button><Button :loading="pending === 'save-variables'" @click="saveVariables"><Save />{{ t('settingsPage.saveVariables') }}</Button></div>
        </section>

        <section v-else class="grid gap-4 rounded-2xl border border-border bg-card p-5">
          <h2>{{ t('settingsPage.section.backups') }}</h2>
          <div class="grid gap-4 xl:grid-cols-2">
            <section class="grid gap-3 rounded-xl border border-border p-4">
              <h3>{{ t('settingsPage.backupExport') }}</h3>
              <label class="flex items-center justify-between gap-3 text-sm">{{ t('settingsPage.encryptExport') }}<Switch v-model="form.exportEncrypt" :label="t('settingsPage.encryptExport')" /></label>
              <label class="grid gap-1 text-sm">{{ t('settingsPage.backupPassword') }}<Input v-model="form.exportPassword" type="password" /></label>
              <Button variant="primary" :loading="pending === 'export'" @click="openConfirm('export')"><DatabaseBackup />{{ t('settingsPage.startExport') }}</Button>
            </section>
            <section class="grid gap-3 rounded-xl border border-border p-4">
              <h3>{{ t('settingsPage.restore') }}</h3>
              <label class="grid gap-1 text-sm">{{ t('settingsPage.backupPassword') }}<Input v-model="form.restorePassword" type="password" /></label>
              <div class="relative grid gap-3">
                <input type="file" accept=".panel-backup,application/octet-stream" class="text-sm" @change="handleRestoreFile" />
                <div v-if="preflight" class="rounded-xl border border-border p-3 text-sm">{{ t('settingsPage.restoreManifest', { version: preflight.manifest.panelVersion, count: preflight.manifest.files.length }) }}</div>
                <LoadingOverlay v-if="pending === 'preflight'" />
              </div>
              <label class="flex items-start gap-2 rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning"><input v-model="form.restoreConfirm" class="mt-1" type="checkbox" />{{ t('settingsPage.confirmRestoreOverwrite') }}</label>
              <Button variant="danger" :disabled="!restoreFile || !preflight || !form.restoreConfirm" :loading="pending === 'restore'" @click="openConfirm('restore')"><UploadCloud />{{ t('settingsPage.confirmRestore') }}</Button>
            </section>
          </div>
          <div v-if="exportPending || restorePending" class="rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning">{{ restarting ? t('settingsPage.restartingHint') : t('settingsPage.pendingHint') }}</div>
        </section>
      </SettingsPage>
    </div>

    <Dialog v-model:open="confirmOpen" :title="t(`settingsPage.confirm.${confirmKind}.title`)" :description="t(`settingsPage.confirm.${confirmKind}.description`)" :close-label="t('common.close')">
      <div class="flex gap-3 rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning"><AlertTriangle class="mt-0.5 size-4 shrink-0" />{{ t('settingsPage.confirmDanger') }}</div>
      <template #footer>
        <Button @click="confirmOpen = false">{{ t('common.cancel') }}</Button>
        <Button :variant="confirmKind === 'restore' || confirmKind === 'system-certificate' ? 'danger' : 'primary'" :loading="Boolean(pending)" @click="confirmAction">{{ t('common.apply') }}</Button>
      </template>
    </Dialog>
  </ConsolePage>
</template>

<style scoped>
h2,
h3 {
  margin: 0;
  color: var(--panel-text);
  font-weight: 650;
}
</style>
