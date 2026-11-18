<script setup lang="ts">
import { computed, defineAsyncComponent, nextTick, onMounted, reactive, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { DatabaseBackup, KeyRound, Network, RefreshCcw, Save, Shield, Trash2, UploadCloud } from '@/icons';
import { eventsApi } from '@/api/events';
import { keyAssetsApi } from '@/api/keyAssets';
import { runtimeTailscaleUpdate, settingsApi, tailscaleUpdate } from '@/api/settings';
import { systemApi, type VersionInfo } from '@/api/system';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import Checkbox from '@/components/ui/Checkbox.vue';
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import FileUploadButton from '@/components/ui/FileUploadButton.vue';
import Input from '@/components/ui/Input.vue';
import NumberBox from '@/components/ui/NumberBox.vue';
import Select from '@/components/ui/Select.vue';
import Switch from '@/components/ui/Switch.vue';
import Table from '@/components/ui/Table.vue';
import Tabs from '@/components/ui/Tabs.vue';
import Textarea from '@/components/ui/Textarea.vue';
import LoadingOverlay from '@/components/ui/LoadingOverlay.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import SettingsCard from '@/components/patterns/SettingsCard.vue';
import SettingsSection from '@/components/patterns/SettingsSection.vue';
import { useI18n } from '@/i18n';
import { useSessionStore } from '@/stores/session';
import type { KeyAssetDto, SystemCertificateDto } from '@/types/keyAssets';
import type { RestorePreflightResponse, RuntimeSettings, RuntimeUpdate, ServerVariableDefinition } from '@/types/settings';
import { createLatestRequestGuard } from '@/views/_shared/requestState';
import { formatDateTime } from '@/utils/datetime';
import AppearanceSectionSkeleton from './AppearanceSectionSkeleton.vue';
import { parseTailscaleTags, tailscaleFormState } from './tailscale';

const { t, locale, setLocale } = useI18n();
const route = useRoute();
const router = useRouter();

/** 9 个设置分区；顺序即顶部 Tabs 顺序（默认落点 general 必须在第一位）。 */
const sectionKeys = ['general', 'appearance', 'security', 'certificates', 'agent', 'tailscale', 'system-certificates', 'system', 'backups'] as const;
type SectionKey = (typeof sectionKeys)[number];

function isSectionKey(value: string): value is SectionKey {
  return (sectionKeys as readonly string[]).includes(value);
}

/** 单一 `settings/:section?` 路由：分区只由 route.params.section 决定。 */
function routeSectionValue(): string {
  const raw = route.params.section;
  return Array.isArray(raw) ? (raw[0] ?? '') : (raw ?? '');
}

const activeSection = ref<SectionKey>(isSectionKey(routeSectionValue()) ? routeSectionValue() as SectionKey : 'general');
/** 分区内容滚动容器：切分区回到顶部，Tab header 不随内容滚走。 */
const settingsScroll = ref<HTMLElement | null>(null);
const appearanceSectionRef = ref<{ reload: () => Promise<void> } | null>(null);
const refreshing = ref(false);
// 外观分区按需加载：它消费共享 appearance store（带首屏缓存/DOM 应用），
// 其它设置分区不需要把外观模块拉进同一个同步依赖图。loadingComponent 保证
// 首次切到外观 tab 时不会空白一帧。
const AppearanceSection = defineAsyncComponent({
  loader: () => import('@/components/appearance/AppearanceSection.vue'),
  loadingComponent: AppearanceSectionSkeleton,
  delay: 0,
  suspensible: false,
});
const session = useSessionStore();
const notifyError = useErrorToast();
const notifySuccess = useSuccessToast();

const runtime = ref<RuntimeSettings | null>(null);
const version = ref<VersionInfo | null>(null);
const serverVariables = ref<ServerVariableDefinition[]>([]);
const systemCertificates = ref<SystemCertificateDto[]>([]);
const systemCertificateColumns = computed(() => [
  { key: 'name', label: t('common.name') },
  { key: 'type', label: t('common.type'), nowrap: true },
  { key: 'status', label: t('common.status'), nowrap: true },
  { key: 'fingerprint', label: t('settingsPage.systemCertificateFingerprint') },
  { key: 'expiresAt', label: t('settingsPage.systemCertificateNotAfter'), nowrap: true },
  { key: 'actions', label: t('common.actions'), align: 'right' as const, nowrap: true },
]);
const tlsCertificates = ref<KeyAssetDto[]>([]);
const loading = ref(false);
const listRequests = createLatestRequestGuard();
const pending = ref('');
const error = ref('');
const actionError = ref('');
const confirmOpen = ref(false);
const confirmKind = ref<'export' | 'restore' | 'system' | 'system-certificate' | 'jwt-secret' | 'tailscale-auth-key' | 'clear-events-logs'>('export');
const selectedSystemCertificate = ref<SystemCertificateDto | null>(null);
const preflight = ref<RestorePreflightResponse | null>(null);
const restoreFile = ref<File | null>(null);
const exportPending = ref(false);
const restorePending = ref(false);
const restarting = ref(false);
const builtInPanelTLSAssetID = 'panel-tls';
// Mirrors MinAgentTransferTimeoutSeconds / MaxAgentTransferTimeoutSeconds on the
// backend so an out-of-range value is caught before the request.
const agentTransferTimeoutMin = 60;
const agentTransferTimeoutMax = 3600;

const form = reactive({
  containerReportIntervalSeconds: '30',
  tokenExpiration: '1d',
  language: 'zh-CN',
  logLevel: 'info',
  remoteCommandTimeoutSeconds: '45',
  reconcileTraceEnabled: false,
  jwtSecret: '',
  tailscaleAuthKey: '',
  tailscaleTags: '',
  loginTitle: '',
  loginSubtitle: '',
  certificateEmail: '',
  dnsPropagationDelaySeconds: '30',
  panelDomain: 'localhost',
  panelTlsCertificateId: '',
  agentDownloadBaseUrl: '',
  agentDownloadVerifyTls: false,
  agentTransferTimeoutSeconds: '120',
  variablesText: '',
  exportEncrypt: true,
  exportPassword: '',
  restorePassword: '',
  restoreConfirm: false,
});

const sections = computed<Array<{ key: SectionKey; label: string }>>(() => [
  { key: 'general', label: t('settingsPage.section.runtime') },
  { key: 'appearance', label: t('settingsPage.section.appearance') },
  { key: 'security', label: t('settingsPage.section.security') },
  { key: 'certificates', label: t('settingsPage.section.certificates') },
  { key: 'agent', label: t('settingsPage.section.agent') },
  { key: 'tailscale', label: t('settingsPage.section.tailscale') },
  { key: 'system-certificates', label: t('settingsPage.section.systemCertificates') },
  { key: 'system', label: t('settingsPage.section.system') },
  { key: 'backups', label: t('settingsPage.section.backups') },
]);
/**
 * 设置分区用共享 WinUI Tabs 作为顶部横向选择器（不再有第二条左侧导航）；
 * value 是分区 key，与 `settings/:section?` 路由参数一一对应，直链/刷新/前进后退由 URL 驱动。
 */
const sectionTabs = computed(() => sections.value.map((section) => ({ label: section.label, value: section.key })));
/** PageHeader 标题跟随当前分区（不再固定“设置”），避免“设置 → 运行时”双重标题。 */
const activeSectionLabel = computed(() => sections.value.find((section) => section.key === activeSection.value)?.label ?? t('routes.settings.title'));

function navigateSection(value: string) {
  if (!isSectionKey(value)) return;
  if (value === activeSection.value && routeSectionValue() === value) return;
  void router.push({ name: 'settings', params: { section: value } });
}

function resetSectionScroll() {
  void nextTick(() => {
    if (settingsScroll.value) settingsScroll.value.scrollTop = 0;
  });
}

/** 当前分区的刷新：始终重取设置数据；外观激活时并行刷新共享 appearance store。 */
async function refreshCurrentSection() {
  if (refreshing.value) return;
  refreshing.value = true;
  try {
    const tasks: Array<Promise<void>> = [load()];
    if (activeSection.value === 'appearance' && appearanceSectionRef.value) {
      tasks.push(appearanceSectionRef.value.reload());
    }
    await Promise.all(tasks);
  } finally {
    refreshing.value = false;
  }
}
const tokenOptions = computed(() => ['10m', '1h', '1d', '5d', '30d', 'never'].map((value) => ({ label: t(`settingsPage.token.${value}`), value })));const languageOptions = computed(() => [{ label: t('settingsPage.language.en'), value: 'en' }, { label: t('settingsPage.language.zh'), value: 'zh-CN' }]);
const logOptions = computed(() => ['debug', 'info', 'warn', 'error'].map((value) => ({ label: value, value })));
const panelTlsOptions = computed(() => tlsCertificates.value.map((certificate) => ({ label: certificate.name, value: certificate.id })));
const jwtSecretValid = computed(() => form.jwtSecret.trim().length >= 16);
/** NumberBox 使用 number|null；设置表单仍是字符串，保留原校验/保存负载逻辑。 */
function toNumber(value: string): number | null {
  const raw = value.trim();
  if (!raw) return null;
  const parsed = Number(raw);
  return Number.isFinite(parsed) ? parsed : null;
}
function fromNumber(value: number | null): string {
  return value === null ? '' : String(value);
}
const fieldErrors = computed<Record<string, string>>(() => {
  const errors: Record<string, string> = {};
  const positive: Array<keyof typeof form> = ['containerReportIntervalSeconds', 'remoteCommandTimeoutSeconds'];
  positive.forEach((key) => {
    const value = Number(form[key]);
    if (!Number.isFinite(value) || value < 1) errors[key] = 'settingsPage.validationPositiveNumber';
  });
  if (!Number.isFinite(Number(form.dnsPropagationDelaySeconds)) || Number(form.dnsPropagationDelaySeconds) < 0) {
    errors.dnsPropagationDelaySeconds = 'settingsPage.validationNonNegativeNumber';
  }
  const transferTimeout = Number(form.agentTransferTimeoutSeconds);
  if (!Number.isFinite(transferTimeout) || transferTimeout < agentTransferTimeoutMin || transferTimeout > agentTransferTimeoutMax) {
    errors.agentTransferTimeoutSeconds = 'settingsPage.validationAgentTransferTimeout';
  }
  return errors;
});
const runtimeSectionValid = computed(() => !fieldErrors.value.containerReportIntervalSeconds);
const tailscaleTagResult = computed(() => parseTailscaleTags(form.tailscaleTags));
const invalidTailscaleTags = computed(() => tailscaleTagResult.value.invalid);
const tailscaleContainer = computed(() => runtime.value?.tailscale.container ?? null);

// 单条 settings/:section? 路由：只监听 section 参数，切分区不重挂载页面。
watch(() => route.params.section, (raw) => {
  const value = Array.isArray(raw) ? (raw[0] ?? '') : (raw ?? '');
  if (value && !isSectionKey(value)) {
    void router.replace({ name: 'settings', params: { section: 'general' } });
    return;
  }
  const nextKey: SectionKey = isSectionKey(value) ? value : 'general';
  if (activeSection.value !== nextKey) {
    activeSection.value = nextKey;
    resetSectionScroll();
  }
}, { immediate: true });
watch(locale, (value) => {
  form.language = value;
  if (runtime.value) runtime.value = { ...runtime.value, language: value };
});

async function load() {
  const requestId = listRequests.begin();
  loading.value = true;
  error.value = '';
  try {
    const [settingsResult, variablesResult, versionResult, certsResult, tlsResult] = await Promise.allSettled([
      settingsApi.runtime(),
      settingsApi.serverVariables(),
      systemApi.version(),
      keyAssetsApi.systemCertificates(),
      keyAssetsApi.tlsCertificates(),
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
    if (tlsResult.status === 'fulfilled') tlsCertificates.value = tlsResult.value;
    else if (!firstError) firstError = tlsResult.reason instanceof Error ? tlsResult.reason.message : t('settingsPage.loadFailed');
    if (settingsResult.status === 'fulfilled' && variablesResult.status === 'fulfilled') {
      hydrate(settingsResult.value, variablesResult.value);
    }
    if (firstError) {
      error.value = firstError;
      notifyError(firstError);
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('settingsPage.loadFailed');
    notifyError(err instanceof Error ? err.message : t('settingsPage.loadFailed'), err);
  } finally {
    if (listRequests.isCurrent(requestId)) loading.value = false;
  }
}

function hydrate(settings: RuntimeSettings, variables: ServerVariableDefinition[]) {
  Object.assign(form, {
    containerReportIntervalSeconds: String(settings.containerReportIntervalSeconds),
    tokenExpiration: settings.tokenExpiration,
    language: settings.language,
    logLevel: settings.logLevel,
    remoteCommandTimeoutSeconds: String(settings.remoteCommandTimeoutSeconds),
    reconcileTraceEnabled: settings.reconcileTraceEnabled,
    jwtSecret: '',
    loginTitle: settings.branding.loginTitle,
    loginSubtitle: settings.branding.loginSubtitle,
    certificateEmail: settings.certificates.email,
    dnsPropagationDelaySeconds: String(settings.certificates.dnsPropagationDelaySeconds),
    panelDomain: settings.panel.domain,
    panelTlsCertificateId: settings.panel.tlsCertificateId || builtInPanelTLSAssetID,
    agentDownloadBaseUrl: settings.agent.downloadBaseUrl,
    agentDownloadVerifyTls: settings.agent.downloadVerifyTls,
    agentTransferTimeoutSeconds: String(settings.agent.transferTimeoutSeconds),
    // 认证密钥只写不读：表单永远以空值开始，服务端响应里没有密钥可回填。
    ...tailscaleFormState(settings),
    variablesText: variables.map((item) => `${item.required ? '*' : ''}${item.key}=${item.name}`).join('\n'),
  });
}

function resetJwtSecret() {
  if (!jwtSecretValid.value) {
    actionError.value = t('settingsPage.jwtSecretValidation');
    return;
  }
  openConfirm('jwt-secret');
}

async function confirmResetJwtSecret() {
  await run('jwt-secret', async () => {
    await session.updateJwtSecret(form.jwtSecret.trim());
    if (runtime.value) runtime.value.jwtSecretConfigured = true;
    form.jwtSecret = '';
    confirmOpen.value = false;
    notifySuccess(t('settingsPage.jwtSecretReset'));
  });
}

async function resetSystemCertificate() {
  const cert = selectedSystemCertificate.value;
  if (!cert) return;
  await run(`system-certificate-${cert.id}`, async () => {
    const result = await keyAssetsApi.resetSystemCertificate(cert.id);
    notifySuccess(t('settingsPage.systemCertificateResetAccepted', { taskId: result.taskId }), result);
    confirmOpen.value = false;
    selectedSystemCertificate.value = null;
  });
}

/** 清空认证密钥是危险操作：只经确认对话框触发，且不携带新的 authKey。 */
async function confirmClearTailscaleAuthKey() {
  const current = runtime.value;
  if (!current) return;
  await run('clear-tailscale-auth-key', async () => {
    runtime.value = await settingsApi.updateRuntime(runtimeTailscaleUpdate(current, { clearAuthKey: true }));
    hydrate(runtime.value, serverVariables.value);
    confirmOpen.value = false;
    notifySuccess(t('settingsPage.tailscale.authKeyCleared'), runtime.value);
  });
}

async function confirmClearEventsAndLogs() {
  await run('clear-events-logs', async () => {
    await eventsApi.clear();
    confirmOpen.value = false;
    notifySuccess(t('settingsPage.clearEventsLogsSucceeded'));
  });
}

/** 202 语义：只表示协调请求已受理，状态以本次返回和后续刷新为准。 */
async function applyTailscale() {
  const current = runtime.value;
  if (!current?.tailscale.container.available) return;
  await run('apply-tailscale', async () => {
    const container = await settingsApi.applyTailscale();
    runtime.value = { ...current, tailscale: { ...current.tailscale, container } };
    notifySuccess(t('settingsPage.tailscale.applyAccepted'), container);
  });
}

async function saveRuntimeSection(kind: 'runtime' | 'security' | 'certificates' | 'agent' | 'system' | 'tailscale') {
  if (!runtime.value) return;
  if (kind === 'runtime' && !runtimeSectionValid.value) {
    actionError.value = t('settingsPage.validationPositiveNumber');
    return;
  }
  if (kind === 'security' && fieldErrors.value.remoteCommandTimeoutSeconds) {
    actionError.value = t('settingsPage.validationPositiveNumber');
    return;
  }
  if (kind === 'certificates' && fieldErrors.value.dnsPropagationDelaySeconds) {
    actionError.value = t('settingsPage.validationNonNegativeNumber');
    return;
  }
  if (kind === 'agent' && fieldErrors.value.agentTransferTimeoutSeconds) {
    actionError.value = t('settingsPage.validationAgentTransferTimeout');
    return;
  }
  if (kind === 'tailscale' && invalidTailscaleTags.value.length) {
    actionError.value = t('settingsPage.tailscale.tagsInvalid', { tags: invalidTailscaleTags.value.join(', ') });
    return;
  }
  await run(`save-${kind}`, async () => {
    runtime.value = await settingsApi.updateRuntime(buildRuntimeUpdate(kind));
    hydrate(runtime.value, serverVariables.value);
    if (kind === 'runtime') setLocale(runtime.value.language === 'zh-CN' ? 'zh-CN' : 'en');
    notifySuccess(t(`settingsPage.saved.${kind}`), runtime.value);
  });
}

async function saveVariables() {
  await run('save-variables', async () => {
    serverVariables.value = await settingsApi.updateServerVariables(parseVariables(form.variablesText));
    notifySuccess(t('settingsPage.saved.system'), serverVariables.value);
  });
}

async function startBackup() {
  if (form.exportEncrypt && !form.exportPassword.trim()) {
    actionError.value = t('settingsPage.backupPasswordRequired');
    return;
  }
  await run('export', async () => {
    const result = await settingsApi.startBackupExport({ encrypt: form.exportEncrypt, password: form.exportPassword || undefined });
    exportPending.value = true;
    notifySuccess(t('settingsPage.backupStarted', { exportId: result.exportId }), result);
    confirmOpen.value = false;
  });
}

async function handleRestoreFile(file: File | File[]) {
  const selected = Array.isArray(file) ? file[0] : file;
  restoreFile.value = selected ?? null;
  preflight.value = null;
  if (!selected) return;
  await run('preflight', async () => {
    preflight.value = await settingsApi.preflightRestore(selected, form.restorePassword);
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
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    pending.value = '';
  }
}

function buildRuntimeUpdate(kind: 'runtime' | 'security' | 'certificates' | 'agent' | 'system' | 'tailscale'): RuntimeUpdate {
  const current = runtime.value!;
  const update: RuntimeUpdate = {
    containerReportIntervalSeconds: current.containerReportIntervalSeconds,
    tokenExpiration: current.tokenExpiration,
    language: current.language,
    logLevel: current.logLevel,
    remoteCommandTimeoutSeconds: current.remoteCommandTimeoutSeconds,
    branding: current.branding,
    certificates: current.certificates,
    panel: current.panel,
    agent: current.agent,
  };
  if (kind === 'runtime') {
    update.containerReportIntervalSeconds = numberField(form.containerReportIntervalSeconds, current.containerReportIntervalSeconds);
    update.language = form.language;
    update.logLevel = form.logLevel;
    update.reconcileTraceEnabled = form.reconcileTraceEnabled;
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
    update.panel = {
      domain: form.panelDomain.trim().toLowerCase(),
      tlsCertificateId: form.panelTlsCertificateId === builtInPanelTLSAssetID ? '' : form.panelTlsCertificateId,
    };
  }
  if (kind === 'agent') {
    update.agent = {
      downloadBaseUrl: form.agentDownloadBaseUrl.trim(),
      downloadVerifyTls: form.agentDownloadVerifyTls,
      transferTimeoutSeconds: numberField(form.agentTransferTimeoutSeconds, current.agent.transferTimeoutSeconds),
    };
  }
  if (kind === 'system') {
    update.branding = { loginTitle: form.loginTitle, loginSubtitle: form.loginSubtitle };
  }
  if (kind === 'tailscale') {
    // 密钥留空表示保留已存密钥；保存分区不会顺带清除它。
    update.tailscale = tailscaleUpdate({ authKey: form.tailscaleAuthKey, tags: tailscaleTagResult.value.tags });
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

const confirmIsDanger = computed(() => (
  confirmKind.value === 'restore'
  || confirmKind.value === 'system-certificate'
  || confirmKind.value === 'jwt-secret'
  || confirmKind.value === 'tailscale-auth-key'
  || confirmKind.value === 'clear-events-logs'
));

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
  if (confirmKind.value === 'jwt-secret') return confirmResetJwtSecret();
  if (confirmKind.value === 'tailscale-auth-key') return confirmClearTailscaleAuthKey();
  if (confirmKind.value === 'clear-events-logs') return confirmClearEventsAndLogs();
  return startBackup();
}

function handleConfirmOpenChange(open: boolean) {
  if (!open && pending.value) return;
  confirmOpen.value = open;
  if (!open) selectedSystemCertificate.value = null;
}

onMounted(load);
</script>

<template>
  <ConsolePage width="content" :body-scroll="false" :title="activeSectionLabel" :description="t('routes.settings.description')">
    <template #actions>
      <Badge v-if="exportPending || restorePending" tone="warning">{{ restarting ? t('settingsPage.restarting') : t('settingsPage.pending') }}</Badge>
      <Button size="sm" :loading="loading || refreshing" @click="refreshCurrentSection"><RefreshCcw />{{ t('common.refresh') }}</Button>
    </template>

    <!--
      WinUI 3 Settings 页：TabView header 固定在内容区顶部，只有分区内容滚动；
      9 个分区共用 SettingsSection/SettingsCard，保存动作在分区 footer，不再有整页保存。
    -->
    <Tabs
      class="h-full min-h-0 w-full"
      :model-value="activeSection"
      :tabs="sectionTabs"
      keep-panels
      @update:model-value="navigateSection"
    >
      <div ref="settingsScroll" data-settings-scroll class="h-full min-h-0 overflow-y-auto">
        <div class="grid gap-4 pb-6">
          <section v-if="actionError" class="grid gap-2">
            <div v-if="actionError" class="rounded-overlay border border-danger-border bg-danger-bg p-3 text-sm text-danger">{{ actionError }}</div>
          </section>

          <AppearanceSection v-if="activeSection === 'appearance'" ref="appearanceSectionRef" />

          <div v-else-if="loading && !runtime" class="relative min-h-48" aria-busy="true">
            <LoadingOverlay :label="t('common.loading')" />
          </div>

          <EmptyState v-else-if="error && !runtime" variant="inset" :title="t('common.loadFailed')" :description="error">
            <template #actions>
              <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button>
            </template>
          </EmptyState>

          <SettingsSection v-else-if="activeSection === 'general'">
            <SettingsCard
              :title="t('settingsPage.containerInterval')"
              :invalid="Boolean(fieldErrors.containerReportIntervalSeconds)"
            >
              <template #control>
                <NumberBox
                  id="settings-container-interval"
                  :aria-label="t('settingsPage.containerInterval')"
                  :aria-describedby="fieldErrors.containerReportIntervalSeconds ? 'settings-container-interval-error' : undefined"
                  class="w-full sm:w-44"
                  :min="1"
                  :step="1"
                  :invalid="Boolean(fieldErrors.containerReportIntervalSeconds)"
                  :model-value="toNumber(form.containerReportIntervalSeconds)"
                  @update:model-value="form.containerReportIntervalSeconds = fromNumber($event)"
                />
              </template>
              <template v-if="fieldErrors.containerReportIntervalSeconds" #hint>
                <span id="settings-container-interval-error" class="text-danger">{{ t(fieldErrors.containerReportIntervalSeconds) }}</span>
              </template>
            </SettingsCard>

            <SettingsCard :title="t('settingsPage.language')">
              <template #control>
                <Select v-model="form.language" :aria-label="t('settingsPage.language')" class="w-full sm:w-64" :options="languageOptions" />
              </template>
            </SettingsCard>

            <SettingsCard :title="t('settingsPage.logLevel')">
              <template #control>
                <Select v-model="form.logLevel" :aria-label="t('settingsPage.logLevel')" class="w-full sm:w-64" :options="logOptions" />
              </template>
            </SettingsCard>

            <SettingsCard :title="t('settingsPage.reconcileTrace')" :description="t('settingsPage.reconcileTraceHint')">
              <template #control>
                <Switch v-model="form.reconcileTraceEnabled" :label="t('settingsPage.reconcileTrace')" />
              </template>
            </SettingsCard>

            <SettingsCard>
              <div class="flex min-w-0 flex-wrap items-center justify-between gap-3">
                <div class="min-w-0">
                  <h3 class="m-0 text-sm font-medium leading-5 text-danger">{{ t('settingsPage.clearEventsLogs') }}</h3>
                  <p class="m-0 text-xs leading-4 text-muted-foreground">{{ t('settingsPage.clearEventsLogsHint') }}</p>
                </div>
                <Button variant="danger" :loading="pending === 'clear-events-logs'" @click="openConfirm('clear-events-logs')">
                  <Trash2 />{{ t('settingsPage.clearEventsLogs') }}
                </Button>
              </div>
            </SettingsCard>

            <template #footer>
              <Button variant="primary" :disabled="!runtimeSectionValid" :loading="pending === 'save-runtime'" @click="saveRuntimeSection('runtime')">
                <Save />{{ t('settingsPage.saveSection') }}
              </Button>
            </template>
          </SettingsSection>

          <SettingsSection v-else-if="activeSection === 'security'">
            <SettingsCard :title="t('settingsPage.tokenExpiration')">
              <template #control>
                <Select v-model="form.tokenExpiration" :aria-label="t('settingsPage.tokenExpiration')" class="w-full sm:w-64" :options="tokenOptions" />
              </template>
            </SettingsCard>

            <SettingsCard
              :title="t('settingsPage.remoteTimeout')"
              :invalid="Boolean(fieldErrors.remoteCommandTimeoutSeconds)"
            >
              <template #control>
                <NumberBox
                  id="settings-remote-timeout"
                  :aria-label="t('settingsPage.remoteTimeout')"
                  :aria-describedby="fieldErrors.remoteCommandTimeoutSeconds ? 'settings-remote-timeout-error' : undefined"
                  class="w-full sm:w-44"
                  :min="1"
                  :step="1"
                  :invalid="Boolean(fieldErrors.remoteCommandTimeoutSeconds)"
                  :model-value="toNumber(form.remoteCommandTimeoutSeconds)"
                  @update:model-value="form.remoteCommandTimeoutSeconds = fromNumber($event)"
                />
              </template>
              <template v-if="fieldErrors.remoteCommandTimeoutSeconds" #hint>
                <span id="settings-remote-timeout-error" class="text-danger">{{ t(fieldErrors.remoteCommandTimeoutSeconds) }}</span>
              </template>
            </SettingsCard>

            <SettingsCard :title="t('settingsPage.jwtSecretConfigured')">
              <template #control>
                <Badge :tone="runtime?.jwtSecretConfigured ? 'success' : 'warning'">{{ runtime?.jwtSecretConfigured ? t('state.healthy') : t('state.warning') }}</Badge>
              </template>
            </SettingsCard>

            <SettingsCard :title="t('settingsPage.jwtSecret')">
              <template #control>
                <Input v-model="form.jwtSecret" :aria-label="t('settingsPage.jwtSecret')" class="w-full sm:w-64" type="password" :placeholder="t('settingsPage.jwtSecretHint')" />
              </template>
            </SettingsCard>

            <SettingsCard>
              <template #control>
                <Button :disabled="!form.jwtSecret.trim()" :loading="pending === 'jwt-secret'" @click="resetJwtSecret">
                  <KeyRound />{{ t('settingsPage.resetJwtSecret') }}
                </Button>
              </template>
            </SettingsCard>

            <template #footer>
              <Button variant="primary" :disabled="Boolean(fieldErrors.remoteCommandTimeoutSeconds)" :loading="pending === 'save-security'" @click="saveRuntimeSection('security')">
                <Shield />{{ t('settingsPage.saveSection') }}
              </Button>
            </template>
          </SettingsSection>

          <SettingsSection v-else-if="activeSection === 'certificates'" :description="t('settingsPage.panelHttpsHint')">
            <SettingsCard :title="t('settingsPage.panelDomain')">
              <template #control>
                <Input v-model="form.panelDomain" :aria-label="t('settingsPage.panelDomain')" class="w-full sm:w-64" />
              </template>
            </SettingsCard>

            <SettingsCard :title="t('settingsPage.panelTlsCertificate')">
              <template #control>
                <Select v-model="form.panelTlsCertificateId" :aria-label="t('settingsPage.panelTlsCertificate')" class="w-full sm:w-64" :options="panelTlsOptions" />
              </template>
            </SettingsCard>

            <SettingsCard :title="t('settingsPage.certificateEmail')">
              <template #control>
                <Input v-model="form.certificateEmail" :aria-label="t('settingsPage.certificateEmail')" class="w-full sm:w-72" />
              </template>
            </SettingsCard>

            <SettingsCard
              :title="t('settingsPage.dnsDelay')"
              :invalid="Boolean(fieldErrors.dnsPropagationDelaySeconds)"
            >
              <template #control>
                <NumberBox
                  id="settings-dns-delay"
                  :aria-label="t('settingsPage.dnsDelay')"
                  :aria-describedby="fieldErrors.dnsPropagationDelaySeconds ? 'settings-dns-delay-error' : undefined"
                  class="w-full sm:w-44"
                  :min="0"
                  :step="1"
                  :invalid="Boolean(fieldErrors.dnsPropagationDelaySeconds)"
                  :model-value="toNumber(form.dnsPropagationDelaySeconds)"
                  @update:model-value="form.dnsPropagationDelaySeconds = fromNumber($event)"
                />
              </template>
              <template v-if="fieldErrors.dnsPropagationDelaySeconds" #hint>
                <span id="settings-dns-delay-error" class="text-danger">{{ t(fieldErrors.dnsPropagationDelaySeconds) }}</span>
              </template>
            </SettingsCard>

            <template #footer>
              <Button variant="primary" :disabled="Boolean(fieldErrors.dnsPropagationDelaySeconds)" :loading="pending === 'save-certificates'" @click="saveRuntimeSection('certificates')">
                <Save />{{ t('settingsPage.saveSection') }}
              </Button>
            </template>
          </SettingsSection>

          <SettingsSection v-else-if="activeSection === 'agent'" :description="t('settingsPage.agentDownloadHint')">
            <SettingsCard :title="t('settingsPage.agentDownloadBaseUrl')" :description="t('settingsPage.agentDownloadBaseUrlHint')">
              <template #control>
                <Input
                  v-model="form.agentDownloadBaseUrl"
                  :aria-label="t('settingsPage.agentDownloadBaseUrl')"
                  class="w-full sm:w-72"
                  :placeholder="t('settingsPage.agentDownloadBaseUrlPlaceholder')"
                />
              </template>
            </SettingsCard>

            <SettingsCard
              :title="t('settingsPage.agentTransferTimeout')"
              :description="t('settingsPage.agentTransferTimeoutHint')"
              :invalid="Boolean(fieldErrors.agentTransferTimeoutSeconds)"
            >
              <template #control>
                <NumberBox
                  id="settings-agent-transfer-timeout"
                  :aria-label="t('settingsPage.agentTransferTimeout')"
                  :aria-describedby="fieldErrors.agentTransferTimeoutSeconds ? 'settings-agent-transfer-timeout-error' : undefined"
                  class="w-full sm:w-44"
                  :min="agentTransferTimeoutMin"
                  :max="agentTransferTimeoutMax"
                  :step="1"
                  :invalid="Boolean(fieldErrors.agentTransferTimeoutSeconds)"
                  :model-value="toNumber(form.agentTransferTimeoutSeconds)"
                  @update:model-value="form.agentTransferTimeoutSeconds = fromNumber($event)"
                />
              </template>
              <template v-if="fieldErrors.agentTransferTimeoutSeconds" #hint>
                <span id="settings-agent-transfer-timeout-error" class="text-danger">{{ t(fieldErrors.agentTransferTimeoutSeconds) }}</span>
              </template>
            </SettingsCard>

            <SettingsCard :title="t('settingsPage.agentDownloadVerifyTls')" :description="t('settingsPage.agentDownloadVerifyTlsHint')">
              <template #control>
                <Switch v-model="form.agentDownloadVerifyTls" :label="t('settingsPage.agentDownloadVerifyTls')" />
              </template>
            </SettingsCard>

            <template #footer>
              <Button variant="primary" :disabled="Boolean(fieldErrors.agentTransferTimeoutSeconds)" :loading="pending === 'save-agent'" @click="saveRuntimeSection('agent')">
                <Save />{{ t('settingsPage.saveSection') }}
              </Button>
            </template>
          </SettingsSection>

          <SettingsSection v-else-if="activeSection === 'tailscale'" :description="t('settingsPage.tailscale.hint')">
            <SettingsCard v-if="!runtime?.tailscale.authKeyConfigured">
              <div class="rounded-control border border-warning-border bg-warning-bg px-3 py-2 text-sm text-warning">{{ t('settingsPage.tailscale.authKeyMissing') }}</div>
            </SettingsCard>

            <SettingsCard :title="t('settingsPage.tailscale.authKeyConfigured')">
              <template #control>
                <Badge :tone="runtime?.tailscale.authKeyConfigured ? 'success' : 'warning'">{{ runtime?.tailscale.authKeyConfigured ? t('state.healthy') : t('state.warning') }}</Badge>
              </template>
            </SettingsCard>

            <SettingsCard :title="t('settingsPage.tailscale.authKey')" :description="t('settingsPage.tailscale.authKeyHint')">
              <template #control>
                <Input v-model="form.tailscaleAuthKey" :aria-label="t('settingsPage.tailscale.authKey')" class="w-full sm:w-64" type="password" :placeholder="t('settingsPage.tailscale.authKeyPlaceholder')" />
              </template>
            </SettingsCard>

            <SettingsCard>
              <template #control>
                <Button variant="danger" :disabled="!runtime?.tailscale.authKeyConfigured" :loading="pending === 'clear-tailscale-auth-key'" @click="openConfirm('tailscale-auth-key')">
                  <KeyRound />{{ t('settingsPage.tailscale.clearAuthKey') }}
                </Button>
              </template>
            </SettingsCard>

            <SettingsCard :title="t('settingsPage.tailscale.tags')" :invalid="invalidTailscaleTags.length > 0">
              <template #control>
                <Input
                  v-model="form.tailscaleTags"
                  :aria-label="t('settingsPage.tailscale.tags')"
                  class="w-full sm:w-72"
                  :invalid="invalidTailscaleTags.length > 0"
                  :placeholder="t('settingsPage.tailscale.tagsPlaceholder')"
                />
              </template>
              <template #description>
                <span :class="invalidTailscaleTags.length ? 'text-xs leading-4 text-danger' : 'text-xs leading-4 text-muted-foreground'">
                  {{ invalidTailscaleTags.length ? t('settingsPage.tailscale.tagsInvalid', { tags: invalidTailscaleTags.join(', ') }) : t('settingsPage.tailscale.tagsHint') }}
                </span>
              </template>
            </SettingsCard>

            <template #footer>
              <Button variant="primary" :disabled="invalidTailscaleTags.length > 0" :loading="pending === 'save-tailscale'" @click="saveRuntimeSection('tailscale')">
                <Save />{{ t('settingsPage.saveSection') }}
              </Button>
            </template>

            <SettingsCard :title="t('settingsPage.tailscale.containerTitle')" :description="t('settingsPage.tailscale.containerHint')">
              <template #control>
                <Button size="sm" variant="secondary" :disabled="!tailscaleContainer?.available" :loading="pending === 'apply-tailscale'" @click="applyTailscale">
                  <Network />{{ t('settingsPage.tailscale.apply') }}
                </Button>
              </template>
            </SettingsCard>

            <SettingsCard v-if="tailscaleContainer?.available">
              <div class="grid gap-3">
                <div class="flex flex-wrap items-center gap-2">
                  <Badge tone="info">{{ t('settingsPage.tailscale.available') }}</Badge>
                  <Badge :tone="tailscaleContainer.running ? 'success' : 'neutral'">{{ tailscaleContainer.running ? t('settingsPage.tailscale.running') : t('settingsPage.tailscale.stopped') }}</Badge>
                  <Badge :tone="tailscaleContainer.loggedIn ? 'success' : 'warning'">{{ tailscaleContainer.loggedIn ? t('settingsPage.tailscale.loggedIn') : t('settingsPage.tailscale.loggedOut') }}</Badge>
                </div>
                <div class="grid gap-3 text-sm lg:grid-cols-2-safe">
                  <div class="min-w-0"><span class="text-muted-foreground">{{ t('settingsPage.tailscale.hostname') }}</span><strong class="block truncate text-foreground">{{ tailscaleContainer.hostname || t('common.notAvailable') }}</strong></div>
                  <div class="min-w-0"><span class="text-muted-foreground">{{ t('settingsPage.tailscale.version') }}</span><strong class="block truncate text-foreground">{{ tailscaleContainer.version || t('common.notAvailable') }}</strong></div>
                  <div class="min-w-0"><span class="text-muted-foreground">{{ t('settingsPage.tailscale.ipv4') }}</span><strong class="block truncate font-mono text-xs text-foreground">{{ tailscaleContainer.ipv4 || t('common.notAvailable') }}</strong></div>
                  <div class="min-w-0"><span class="text-muted-foreground">{{ t('settingsPage.tailscale.ipv6') }}</span><strong class="block truncate font-mono text-xs text-foreground">{{ tailscaleContainer.ipv6 || t('common.notAvailable') }}</strong></div>
                  <div class="min-w-0"><span class="text-muted-foreground">{{ t('settingsPage.tailscale.backendState') }}</span><strong class="block truncate text-foreground">{{ tailscaleContainer.backendState || t('common.notAvailable') }}</strong></div>
                  <div class="min-w-0"><span class="text-muted-foreground">{{ t('settingsPage.tailscale.updatedAt') }}</span><strong class="block text-foreground">{{ formatDateTime(tailscaleContainer.updatedAt, t('common.never')) }}</strong></div>
                </div>
                <div v-if="tailscaleContainer.lastError" class="grid gap-1 rounded-control border border-danger-border bg-danger-bg px-3 py-2 text-sm text-danger">
                  <strong>{{ t('settingsPage.tailscale.lastError') }}</strong>
                  <span class="break-words">{{ tailscaleContainer.lastError }}</span>
                </div>
                <p class="m-0 text-xs text-muted-foreground">{{ t('settingsPage.tailscale.applyHint') }}</p>
              </div>
            </SettingsCard>

            <SettingsCard v-else>
              <EmptyState variant="inset" :title="t('settingsPage.tailscale.unavailableTitle')" :description="t('settingsPage.tailscale.unavailableHint')" />
            </SettingsCard>
          </SettingsSection>

          <SettingsSection v-else-if="activeSection === 'system-certificates'" :description="t('settingsPage.systemCertificatesHint')">
            <EmptyState v-if="!systemCertificates.length" variant="inset" :title="t('settingsPage.systemCertificatesEmpty')" :description="t('settingsPage.systemCertificatesEmptyHint')" />
            <div v-else class="p-3">
              <Table
                :framed="false"
                row-height="auto"
                :columns="systemCertificateColumns"
                :rows="systemCertificates"
                row-key="id"
              >
                <template #name="{ row }">
                  <strong class="block max-w-72 truncate text-foreground">{{ row.name }}</strong>
                  <span class="block max-w-72 truncate text-xs text-muted-foreground">{{ row.serverName ? t('settingsPage.systemCertificateServer', { server: row.serverName }) : t('settingsPage.systemCertificateBuiltIn') }}</span>
                </template>
                <template #type="{ row }">
                  <Badge tone="info">{{ certificateType(row.type) }}</Badge>
                </template>
                <template #status="{ row }">
                  <Badge :tone="certificateTone(row.status)">{{ certificateStatus(row.status) }}</Badge>
                </template>
                <template #fingerprint="{ row }">
                  <span class="panel-mono block max-w-72 truncate text-xs text-muted-foreground">{{ row.fingerprint || t('common.notAvailable') }}</span>
                </template>
                <template #expiresAt="{ row }">
                  <span class="text-muted-foreground">{{ formatDateTime(row.notAfter, t('common.notAvailable')) }}</span>
                </template>
                <template #actions="{ row }">
                  <Button class="w-fit" :disabled="!row.canReset" :loading="pending === `system-certificate-${row.id}`" @click="openSystemCertificateConfirm(row)"><KeyRound />{{ t('settingsPage.systemCertificateReset') }}</Button>
                </template>
              </Table>
            </div>
          </SettingsSection>

          <SettingsSection v-else-if="activeSection === 'system'">
            <SettingsCard>
              <div class="grid gap-3 text-sm">
                <div class="grid grid-cols-2 gap-3 max-md:grid-cols-1">
                  <div><span class="text-muted-foreground">{{ t('settingsPage.panelVersion') }}</span><strong class="block text-foreground">{{ version?.version || t('common.notAvailable') }}</strong></div>
                  <div><span class="text-muted-foreground">{{ t('settingsPage.releaseChannel') }}</span><strong class="block text-foreground">{{ version?.channel || t('common.notAvailable') }}</strong></div>
                  <div><span class="text-muted-foreground">{{ t('settingsPage.latestVersion') }}</span><strong class="block text-foreground">{{ version?.latestVersion || t('common.notAvailable') }}</strong></div>
                  <div><span class="text-muted-foreground">{{ t('settingsPage.updateStatus') }}</span><Badge :tone="version?.checkError ? 'danger' : version?.updateAvailable ? 'warning' : 'success'">{{ version?.checkError ? t('settingsPage.versionCheckError') : version?.updateAvailable ? t('settingsPage.updateAvailable') : t('settingsPage.upToDate') }}</Badge></div>
                </div>
                <p v-if="version?.checkError" class="m-0 text-sm text-danger">{{ t('settingsPage.versionCheckErrorDetail', { error: version.checkError }) }}</p>
              </div>
            </SettingsCard>

            <SettingsCard :title="t('settingsPage.loginTitle')">
              <template #control>
                <Input v-model="form.loginTitle" :aria-label="t('settingsPage.loginTitle')" class="w-full sm:w-72" />
              </template>
            </SettingsCard>

            <SettingsCard :title="t('settingsPage.loginSubtitle')">
              <template #control>
                <Input v-model="form.loginSubtitle" :aria-label="t('settingsPage.loginSubtitle')" class="w-full sm:w-72" />
              </template>
            </SettingsCard>

            <SettingsCard :title="t('settingsPage.serverVariables')" stacked>
              <template #control>
                <Textarea v-model="form.variablesText" :aria-label="t('settingsPage.serverVariables')" class="min-h-44 w-full font-mono" :placeholder="t('settingsPage.serverVariablesHint')" />
              </template>
            </SettingsCard>

            <template #footer>
              <Button :loading="pending === 'save-variables'" @click="saveVariables"><Save />{{ t('settingsPage.saveVariables') }}</Button>
              <Button :loading="pending === 'save-system'" variant="primary" @click="saveRuntimeSection('system')"><Save />{{ t('settingsPage.saveBranding') }}</Button>
            </template>
          </SettingsSection>

          <template v-else>
            <SettingsSection :title="t('settingsPage.backupExport')">
              <SettingsCard :title="t('settingsPage.encryptExport')">
                <template #control>
                  <Switch v-model="form.exportEncrypt" :label="t('settingsPage.encryptExport')" />
                </template>
              </SettingsCard>

              <SettingsCard v-if="!form.exportEncrypt">
                <div class="rounded-control border border-warning-border bg-warning-bg px-3 py-2 text-sm text-warning">{{ t('settingsPage.unencryptedExportWarning') }}</div>
              </SettingsCard>

              <SettingsCard
                :title="t('settingsPage.backupPassword')"
                :invalid="Boolean(form.exportEncrypt && !form.exportPassword.trim())"
              >
                <template #control>
                  <Input
                    v-model="form.exportPassword"
                    :aria-label="t('settingsPage.backupPassword')"
                    class="w-full sm:w-64"
                    type="password"
                    :invalid="Boolean(form.exportEncrypt && !form.exportPassword.trim())"
                  />
                </template>
                <template v-if="form.exportEncrypt && !form.exportPassword.trim()" #hint>
                  <span class="text-danger">{{ t('settingsPage.backupPasswordRequired') }}</span>
                </template>
              </SettingsCard>

              <template #footer>
                <Button variant="primary" :disabled="Boolean(form.exportEncrypt && !form.exportPassword.trim())" :loading="pending === 'export'" @click="openConfirm('export')">
                  <DatabaseBackup />{{ t('settingsPage.startExport') }}
                </Button>
              </template>
            </SettingsSection>

            <SettingsSection :title="t('settingsPage.restore')">
              <SettingsCard :title="t('settingsPage.backupPassword')">
                <template #control>
                  <Input v-model="form.restorePassword" :aria-label="t('settingsPage.backupPassword')" class="w-full sm:w-64" type="password" />
                </template>
              </SettingsCard>

              <SettingsCard stacked>
                <template #control>
                  <div class="relative grid w-full gap-3">
                    <FileUploadButton
                      accept=".panel-backup,application/octet-stream"
                      :loading="pending === 'preflight'"
                      :label="restoreFile ? restoreFile.name : t('settingsPage.selectRestoreFile')"
                      @change="handleRestoreFile"
                    />
                    <div v-if="preflight" class="rounded-control bg-muted p-3 text-sm">
                      {{ t('settingsPage.restoreManifest', { version: preflight.manifest.panelVersion, count: preflight.manifest.files.length }) }}
                    </div>
                    <LoadingOverlay v-if="pending === 'preflight'" />
                  </div>
                </template>
              </SettingsCard>

              <SettingsCard>
                <Checkbox v-model="form.restoreConfirm" :label="t('settingsPage.confirmRestoreOverwrite')" class="rounded-control border border-warning-border bg-warning-bg p-3 text-warning" />
              </SettingsCard>

              <template #footer>
                <Button variant="danger" :disabled="!restoreFile || !preflight || !form.restoreConfirm" :loading="pending === 'restore'" @click="openConfirm('restore')">
                  <UploadCloud />{{ t('settingsPage.confirmRestore') }}
                </Button>
              </template>
            </SettingsSection>

            <div v-if="exportPending || restorePending" class="rounded-control border border-warning-border bg-warning-bg px-3 py-2 text-sm text-warning">
              {{ restarting ? t('settingsPage.restartingHint') : t('settingsPage.pendingHint') }}
            </div>
          </template>
        </div>
      </div>
    </Tabs>

    <ConfirmDialog
      :open="confirmOpen"
      :title="t(`settingsPage.confirm.${confirmKind}.title`)"
      :description="t(`settingsPage.confirm.${confirmKind}.description`)"
      :impact="confirmKind === 'clear-events-logs' ? t('settingsPage.confirm.clear-events-logs.impact') : t('settingsPage.confirmDanger')"
      :tone="confirmIsDanger ? 'danger' : 'warning'"
      :loading="Boolean(pending)"
      :require-checkbox="confirmIsDanger"
      :checkbox-label="confirmKind === 'clear-events-logs' ? t('settingsPage.confirm.clear-events-logs.checkbox') : t('serversPage.confirmCheckbox')"
      :confirm-label="confirmKind === 'clear-events-logs' ? t('settingsPage.clearEventsLogs') : t('common.apply')"
      :cancel-label="t('common.cancel')"
      @update:open="handleConfirmOpenChange"
      @confirm="confirmAction"
    />
  </ConsolePage>
</template>
