<script setup lang="ts">
import { computed, defineAsyncComponent, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { DatabaseBackup, KeyRound, Play, RefreshCcw, Trash2, UploadCloud } from '@/icons';
import { eventsApi } from '@/api/events';
import { keyAssetsApi } from '@/api/keyAssets';
import { settingsApi } from '@/api/settings';
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
import TimeOfDayInput from '@/components/ui/TimeOfDayInput.vue';
import Table from '@/components/ui/Table.vue';
import Tabs from '@/components/ui/Tabs.vue';
import Textarea from '@/components/ui/Textarea.vue';
import LoadingOverlay from '@/components/ui/LoadingOverlay.vue';
import { useErrorDialog } from '@/components/ui/errorDialog';
import { useSuccessToast } from '@/components/ui/toast';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import AutoSaveStatus from '@/components/patterns/AutoSaveStatus.vue';
import SettingsCard from '@/components/patterns/SettingsCard.vue';
import SettingsSection from '@/components/patterns/SettingsSection.vue';
import { useDebouncedAutoSave, type AutoSaveStatusLabels } from '@/composables/useDebouncedAutoSave';
import { useI18n } from '@/i18n';
import { useSessionStore } from '@/stores/session';
import type { KeyAssetDto, SystemCertificateDto } from '@/types/keyAssets';
import type {
  ImageRegistryScheme,
  RestorePreflightResponse,
  RuntimeAgentUpdate,
  RuntimeAutoUpdateUpdate,
  RuntimeBrandingUpdate,
  RuntimeCertificatesUpdate,
  RuntimeGeneralUpdate,
  RuntimeImageRegistryUpdate,
  RuntimeSecurityUpdate,
  RuntimeSettings,
  ServerVariableDefinition,
} from '@/types/settings';
import { createLatestRequestGuard } from '@/views/_shared/requestState';
import { formatDateTime } from '@/utils/datetime';
import { normalizeImageRegistryEndpoint, normalizeImageRegistryScheme } from '@/utils/imageRegistry';
import AppearanceSectionSkeleton from './AppearanceSectionSkeleton.vue';

const { t, locale, setLocale } = useI18n();
const route = useRoute();
const router = useRouter();

/** 10 个设置分区；顺序即顶部 Tabs 顺序（默认落点 general 必须在第一位）。 */
const sectionKeys = ['general', 'appearance', 'security', 'certificates', 'agent', 'auto-update', 'system-certificates', 'system', 'backups', 'image-registry'] as const;
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
const notifyError = useErrorDialog();
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
const confirmKind = ref<'export' | 'restore' | 'system' | 'system-certificate' | 'jwt-secret' | 'clear-events-logs' | 'auto-update-run'>('export');
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
const loginTitleMaxLength = 80;
const loginSubtitleMaxLength = 240;

const form = reactive({
  containerReportIntervalSeconds: '30',
  tokenExpiration: '1d',
  language: 'zh-CN',
  logLevel: 'info',
  remoteCommandTimeoutSeconds: '45',
  reconcileTraceEnabled: false,
  jwtSecret: '',
  loginTitle: '',
  loginSubtitle: '',
  certificateEmail: '',
  dnsPropagationDelaySeconds: '30',
  panelDomain: 'localhost',
  panelTlsCertificateId: '',
  agentDownloadBaseUrl: '',
  agentDownloadVerifyTls: false,
  agentTransferTimeoutSeconds: '120',
  autoUpdateAppsEnabled: false,
  autoUpdatePackagesEnabled: false,
  autoUpdatePackagesMode: 'security',
  autoUpdateScheduleTime: '04:00',
  imageRegistryEndpoint: '',
  imageRegistryScheme: 'https' as ImageRegistryScheme,
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
  { key: 'auto-update', label: t('settingsPage.section.autoUpdate') },
  { key: 'system-certificates', label: t('settingsPage.section.systemCertificates') },
  { key: 'system', label: t('settingsPage.section.system') },
  { key: 'backups', label: t('settingsPage.section.backups') },
  { key: 'image-registry', label: t('settingsPage.section.imageRegistry') },
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

/* ------------------------------------------------------------------------ */
/* 本地校验：非法输入不发请求，只在字段旁展示错误并让 AutoSaveStatus 进入 invalid。 */
/* ------------------------------------------------------------------------ */

function integerField(raw: string, min: number): number | null {
  const text = raw.trim();
  if (!text) return null;
  const value = Number(text);
  return Number.isInteger(value) && value >= min ? value : null;
}

function integerInRange(raw: string, min: number, max: number): number | null {
  const value = integerField(raw, min);
  return value !== null && value <= max ? value : null;
}

function isIPv4(value: string): boolean {
  const parts = value.split('.');
  return parts.length === 4 && parts.every((part) => /^\d{1,3}$/.test(part) && Number(part) <= 255);
}

function isDottedHostname(value: string): boolean {
  const labels = value.split('.');
  if (labels.length < 2) return false;
  return labels.every((label) => label.length <= 63 && /^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/.test(label));
}

/**
 * Panel 域名：trim + 小写；非空、不含路径/查询/凭据字符与空白、不含 `..`，
 * 且必须是 IP、localhost 或带点主机名（<=253）。
 */
function normalizePanelDomain(raw: string): string | null {
  const value = raw.trim().toLowerCase();
  if (!value || value.length > 253) return null;
  if (/[/\\?#@\s:]/.test(value)) return null;
  if (value.includes('..')) return null;
  if (value === 'localhost' || isIPv4(value)) return value;
  return isDottedHostname(value) ? value : null;
}

/** ACME 联系邮箱：允许留空，否则要求基本邮箱形态。 */
function normalizeCertificateEmail(raw: string): string | null {
  const value = raw.trim();
  if (!value) return '';
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value) ? value : null;
}

/**
 * Agent 下载基址：允许留空；否则必须是裸 http(s) origin（无 path/query/fragment/userinfo），
 * 且不含空白、引号、反斜杠与 shell 元字符。返回规范化 origin 作为提交值。
 */
function normalizeAgentDownloadBaseUrl(raw: string): string | null {
  const value = raw.trim();
  if (!value) return '';
  if (/[\s"'`\\$&|;<>(){}*!~]/.test(value)) return null;
  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch {
    return null;
  }
  if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') return null;
  if (parsed.username || parsed.password) return null;
  if (parsed.search || parsed.hash || parsed.pathname !== '/') return null;
  if (!parsed.hostname) return null;
  return parsed.origin;
}

/** 每日执行时间：24 小时制 `HH:MM`（Panel 本地时区；与后端同一正则）。 */
const scheduleTimePattern = /^([01]\d|2[0-3]):[0-5]\d$/;

function isValidScheduleTime(raw: string): boolean {
  return scheduleTimePattern.test(raw.trim());
}

interface ParsedServerVariables {
  definitions: ServerVariableDefinition[];
  invalid: boolean;
}

/** `[*]KEY=Name` 每行一条；key 匹配 `[A-Za-z_][A-Za-z0-9_]*` 且唯一，显示名非空。 */
function parseServerVariables(raw: string): ParsedServerVariables {
  const definitions: ServerVariableDefinition[] = [];
  const seen = new Set<string>();
  let invalid = false;
  raw.split(/\r?\n/).forEach((rawLine) => {
    const line = rawLine.trim();
    if (!line) return;
    const required = line.startsWith('*');
    const body = required ? line.slice(1) : line;
    const separator = body.indexOf('=');
    if (separator <= 0) {
      invalid = true;
      return;
    }
    const key = body.slice(0, separator).trim();
    const name = body.slice(separator + 1).trim();
    if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(key) || !name || seen.has(key)) {
      invalid = true;
      return;
    }
    seen.add(key);
    definitions.push({ key, name, required });
  });
  return { definitions, invalid };
}

function formatServerVariables(definitions: ServerVariableDefinition[]): string {
  return definitions.map((item) => `${item.required ? '*' : ''}${item.key}=${item.name}`).join('\n');
}

const tokenOptions = computed(() => ['10m', '1h', '1d', '5d', '30d', 'never'].map((value) => ({ label: t(`settingsPage.token.${value}`), value })));
const languageOptions = computed(() => [{ label: t('settingsPage.language.en'), value: 'en' }, { label: t('settingsPage.language.zh'), value: 'zh-CN' }]);
const logOptions = computed(() => ['debug', 'info', 'warn', 'error'].map((value) => ({ label: value, value })));
const autoUpdatePackagesModeOptions = computed(() => [
  { label: t('settingsPage.autoUpdate.packagesMode.security'), value: 'security' },
  { label: t('settingsPage.autoUpdate.packagesMode.all'), value: 'all' },
]);
const imageRegistrySchemeOptions = computed(() => [
  { label: t('settingsPage.imageRegistry.scheme.https'), value: 'https' },
  { label: t('settingsPage.imageRegistry.scheme.http'), value: 'http' },
]);
/** 最近一次自动更新只读投影（来自 `autoUpdate.lastRun*`，不进入自动保存载荷）。 */
const autoUpdateLastRun = computed(() => runtime.value?.autoUpdate ?? null);
const autoUpdateRunPending = computed(() => pending.value === 'auto-update-run');
/** 手动运行与后端活跃任务共用同一互斥；运行中禁用按钮。 */
const autoUpdateRunning = computed(() => autoUpdateRunPending.value || autoUpdateLastRun.value?.lastRunStatus === 'running');
/**
 * 两个开关都关时后端返回 422 auto_update_disabled，按钮提前禁用。
 * 以已持久化快照为准：手动运行由后端读取当前设置，避免把尚未防抖保存的
 * 本地开关误当成运行参数。
 */
const autoUpdateCanRun = computed(() => Boolean(runtime.value?.autoUpdate?.appsEnabled || runtime.value?.autoUpdate?.packagesEnabled));
const autoUpdateRunHint = computed(() => {
  if (!autoUpdateCanRun.value) return t('settingsPage.autoUpdate.runNowDisabled');
  if (autoUpdateRunning.value) return t('settingsPage.autoUpdate.runNowRunning');
  return t('settingsPage.autoUpdate.runNowHint');
});

function autoUpdateRunStatusLabel(status?: string): string {
  if (!status) return t('settingsPage.autoUpdate.lastRunNever');
  const key = `settingsPage.autoUpdate.runStatus.${status}`;
  const translated = t(key);
  return translated === key ? status : translated;
}

function autoUpdateRunStatusTone(status?: string): 'neutral' | 'success' | 'warning' | 'danger' | 'info' {
  if (status === 'completed' || status === 'succeeded') return 'success';
  if (status === 'failed') return 'danger';
  if (status === 'running' || status === 'skipped' || status === 'disabled') return 'warning';
  return 'neutral';
}
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
  if (integerField(form.containerReportIntervalSeconds, 1) === null) errors.containerReportIntervalSeconds = 'settingsPage.validationPositiveNumber';
  if (integerField(form.remoteCommandTimeoutSeconds, 1) === null) errors.remoteCommandTimeoutSeconds = 'settingsPage.validationPositiveNumber';
  if (integerField(form.dnsPropagationDelaySeconds, 0) === null) errors.dnsPropagationDelaySeconds = 'settingsPage.validationNonNegativeNumber';
  if (integerInRange(form.agentTransferTimeoutSeconds, agentTransferTimeoutMin, agentTransferTimeoutMax) === null) errors.agentTransferTimeoutSeconds = 'settingsPage.validationAgentTransferTimeout';
  if (normalizePanelDomain(form.panelDomain) === null) errors.panelDomain = 'settingsPage.validationPanelDomain';
  if (normalizeCertificateEmail(form.certificateEmail) === null) errors.certificateEmail = 'settingsPage.validationCertificateEmail';
  if (normalizeAgentDownloadBaseUrl(form.agentDownloadBaseUrl) === null) errors.agentDownloadBaseUrl = 'settingsPage.validationAgentDownloadBaseUrl';
  if ([...form.loginTitle.trim()].length > loginTitleMaxLength) errors.loginTitle = 'settingsPage.validationMaxLength';
  if ([...form.loginSubtitle.trim()].length > loginSubtitleMaxLength) errors.loginSubtitle = 'settingsPage.validationMaxLength';
  if (!isValidScheduleTime(form.autoUpdateScheduleTime)) errors.autoUpdateScheduleTime = 'settingsPage.validationAutoUpdateScheduleTime';
  if (normalizeImageRegistryEndpoint(form.imageRegistryEndpoint) === null) errors.imageRegistryEndpoint = 'settingsPage.validationImageRegistryEndpoint';
  return errors;
});
const parsedServerVariables = computed(() => parseServerVariables(form.variablesText));

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

/* ------------------------------------------------------------------------ */
/* 分区自动保存：每分区一个串行控制器，500ms 防抖，只提交本分区子资源。       */
/* ------------------------------------------------------------------------ */

/** 程序性回填期间抑制自动保存，避免 hydrate 触发回写形成循环。 */
let hydrating = false;

function withProgrammatic(apply: () => void) {
  hydrating = true;
  try {
    apply();
  } finally {
    hydrating = false;
  }
}

const autoSaveLabels = computed<AutoSaveStatusLabels>(() => ({
  saving: t('settingsPage.autoSave.saving'),
  saved: t('settingsPage.autoSave.saved'),
  failed: t('settingsPage.autoSave.failed'),
  invalid: t('settingsPage.autoSave.invalid'),
  retry: t('common.retry'),
}));

function collectGeneralPayload(): RuntimeGeneralUpdate | null {
  const interval = integerField(form.containerReportIntervalSeconds, 1);
  if (interval === null) return null;
  return {
    containerReportIntervalSeconds: interval,
    language: form.language,
    logLevel: form.logLevel,
    reconcileTraceEnabled: form.reconcileTraceEnabled,
  };
}

function collectSecurityPayload(): RuntimeSecurityUpdate | null {
  const timeout = integerField(form.remoteCommandTimeoutSeconds, 1);
  if (timeout === null) return null;
  return { tokenExpiration: form.tokenExpiration, remoteCommandTimeoutSeconds: timeout };
}

function collectCertificatesPayload(): RuntimeCertificatesUpdate | null {
  const domain = normalizePanelDomain(form.panelDomain);
  const email = normalizeCertificateEmail(form.certificateEmail);
  const delay = integerField(form.dnsPropagationDelaySeconds, 0);
  if (domain === null || email === null || delay === null) return null;
  return {
    panel: {
      domain,
      tlsCertificateId: form.panelTlsCertificateId === builtInPanelTLSAssetID ? '' : form.panelTlsCertificateId,
    },
    certificates: { email, dnsPropagationDelaySeconds: delay },
  };
}

function collectAgentPayload(): RuntimeAgentUpdate | null {
  const baseUrl = normalizeAgentDownloadBaseUrl(form.agentDownloadBaseUrl);
  const timeout = integerInRange(form.agentTransferTimeoutSeconds, agentTransferTimeoutMin, agentTransferTimeoutMax);
  if (baseUrl === null || timeout === null) return null;
  return {
    downloadBaseUrl: baseUrl,
    downloadVerifyTls: form.agentDownloadVerifyTls,
    transferTimeoutSeconds: timeout,
  };
}

function collectBrandingPayload(): RuntimeBrandingUpdate | null {
  const loginTitle = form.loginTitle.trim();
  const loginSubtitle = form.loginSubtitle.trim();
  if ([...loginTitle].length > loginTitleMaxLength || [...loginSubtitle].length > loginSubtitleMaxLength) return null;
  return { loginTitle, loginSubtitle };
}

function collectAutoUpdatePayload(): RuntimeAutoUpdateUpdate | null {
  const scheduleTime = form.autoUpdateScheduleTime.trim();
  if (!isValidScheduleTime(scheduleTime)) return null;
  const packagesMode = form.autoUpdatePackagesMode === 'all' ? 'all' : 'security';
  return {
    appsEnabled: form.autoUpdateAppsEnabled,
    packagesEnabled: form.autoUpdatePackagesEnabled,
    packagesMode,
    scheduleTime,
  };
}

/**
 * 镜像仓库分区：只提交本分区两个字段；endpoint 空串是合法值（官方仓库），
 * 不能被指针省略语义吃掉，所以始终显式提交。
 */
function collectImageRegistryPayload(): RuntimeImageRegistryUpdate | null {
  const endpoint = normalizeImageRegistryEndpoint(form.imageRegistryEndpoint);
  if (endpoint === null) return null;
  return { endpoint, scheme: normalizeImageRegistryScheme(form.imageRegistryScheme) };
}

function collectVariablesPayload(): ServerVariableDefinition[] | null {
  const parsed = parsedServerVariables.value;
  return parsed.invalid ? null : parsed.definitions;
}

function refillGeneral(settings: RuntimeSettings) {
  form.containerReportIntervalSeconds = String(settings.containerReportIntervalSeconds);
  form.language = settings.language === 'zh-CN' ? 'zh-CN' : 'en';
  form.logLevel = settings.logLevel;
  form.reconcileTraceEnabled = settings.reconcileTraceEnabled;
  setLocale(settings.language === 'zh-CN' ? 'zh-CN' : 'en');
}

function refillSecurity(settings: RuntimeSettings) {
  form.tokenExpiration = settings.tokenExpiration;
  form.remoteCommandTimeoutSeconds = String(settings.remoteCommandTimeoutSeconds);
}

function refillCertificates(settings: RuntimeSettings) {
  form.panelDomain = settings.panel.domain;
  form.panelTlsCertificateId = settings.panel.tlsCertificateId || builtInPanelTLSAssetID;
  form.certificateEmail = settings.certificates.email;
  form.dnsPropagationDelaySeconds = String(settings.certificates.dnsPropagationDelaySeconds);
}

function refillAgent(settings: RuntimeSettings) {
  form.agentDownloadBaseUrl = settings.agent.downloadBaseUrl;
  form.agentDownloadVerifyTls = settings.agent.downloadVerifyTls;
  form.agentTransferTimeoutSeconds = String(settings.agent.transferTimeoutSeconds);
}

function refillBranding(settings: RuntimeSettings) {
  form.loginTitle = settings.branding.loginTitle;
  form.loginSubtitle = settings.branding.loginSubtitle;
}

function refillAutoUpdate(settings: RuntimeSettings) {
  const autoUpdate = settings.autoUpdate;
  form.autoUpdateAppsEnabled = autoUpdate?.appsEnabled ?? false;
  form.autoUpdatePackagesEnabled = autoUpdate?.packagesEnabled ?? false;
  form.autoUpdatePackagesMode = autoUpdate?.packagesMode === 'all' ? 'all' : 'security';
  form.autoUpdateScheduleTime = autoUpdate?.scheduleTime || '04:00';
}

function refillImageRegistry(settings: RuntimeSettings) {
  const imageRegistry = settings.imageRegistry;
  form.imageRegistryEndpoint = imageRegistry?.endpoint ?? '';
  form.imageRegistryScheme = normalizeImageRegistryScheme(imageRegistry?.scheme);
}

function refillVariables(definitions: ServerVariableDefinition[]) {
  form.variablesText = formatServerVariables(definitions);
}

/** 语言保存失败：把界面语言与表单回滚到服务端已持久化的值。 */
function rollbackLanguage() {
  const persisted = runtime.value?.language === 'zh-CN' ? 'zh-CN' : 'en';
  if (form.language !== persisted) withProgrammatic(() => { form.language = persisted; });
  if (locale.value !== persisted) setLocale(persisted);
}

function partitionAutoSave<P>(
  save: (payload: P) => Promise<RuntimeSettings>,
  refill: (settings: RuntimeSettings) => void,
  onFailed?: (errorValue: unknown) => void,
) {
  return useDebouncedAutoSave<P, RuntimeSettings>({
    save,
    onSaved(result, _payload, meta) {
      runtime.value = result;
      // 请求期间用户又改了本分区时，保留更新后的输入；等下一次防抖保存完成后再回填。
      if (!meta.queued) withProgrammatic(() => refill(result));
    },
    onFailed(errorValue) {
      // 自动保存失败只在 AutoSaveStatus 中进入 error + 重试，不 toast、不弹窗。
      onFailed?.(errorValue);
    },
  });
}

const generalAutoSave = partitionAutoSave<RuntimeGeneralUpdate>(
  (payload) => settingsApi.updateGeneral(payload),
  refillGeneral,
  rollbackLanguage,
);
const securityAutoSave = partitionAutoSave<RuntimeSecurityUpdate>(
  (payload) => settingsApi.updateSecurity(payload),
  refillSecurity,
);
const certificatesAutoSave = partitionAutoSave<RuntimeCertificatesUpdate>(
  (payload) => settingsApi.updateCertificates(payload),
  refillCertificates,
);
const agentAutoSave = partitionAutoSave<RuntimeAgentUpdate>(
  (payload) => settingsApi.updateAgent(payload),
  refillAgent,
);
const brandingAutoSave = partitionAutoSave<RuntimeBrandingUpdate>(
  (payload) => settingsApi.updateBranding(payload),
  refillBranding,
);
const autoUpdateAutoSave = partitionAutoSave<RuntimeAutoUpdateUpdate>(
  (payload) => settingsApi.updateAutoUpdate(payload),
  refillAutoUpdate,
);
const imageRegistryAutoSave = partitionAutoSave<RuntimeImageRegistryUpdate>(
  (payload) => settingsApi.updateImageRegistry(payload),
  refillImageRegistry,
);
const variablesAutoSave = useDebouncedAutoSave<ServerVariableDefinition[], ServerVariableDefinition[]>({
  save: (definitions) => settingsApi.updateServerVariables(definitions),
  onSaved(result, _payload, meta) {
    serverVariables.value = result;
    if (!meta.queued) withProgrammatic(() => refillVariables(result));
  },
  // 自动保存失败只在 AutoSaveStatus 中进入 error + 重试，不 toast、不弹窗。
});

const generalStatus = generalAutoSave.status;
const securityStatus = securityAutoSave.status;
const certificatesStatus = certificatesAutoSave.status;
const agentStatus = agentAutoSave.status;
const brandingStatus = brandingAutoSave.status;
const autoUpdateStatus = autoUpdateAutoSave.status;
const imageRegistryStatus = imageRegistryAutoSave.status;
const variablesStatus = variablesAutoSave.status;

const autoSaveControllers = [
  generalAutoSave,
  securityAutoSave,
  certificatesAutoSave,
  agentAutoSave,
  brandingAutoSave,
  autoUpdateAutoSave,
  imageRegistryAutoSave,
  variablesAutoSave,
];

function cancelAllAutoSaves() {
  autoSaveControllers.forEach((controller) => controller.cancel());
}

function disposeAllAutoSaves() {
  autoSaveControllers.forEach((controller) => controller.dispose());
}

function scheduleAutoSave<P>(controller: ReturnType<typeof useDebouncedAutoSave<P, RuntimeSettings>>, collect: () => P | null) {
  if (hydrating) return;
  controller.schedule(collect);
}

// flush: 'sync' 让程序性回填的 hydrating 标记能可靠抑制 watcher 触发的回写。
const syncWatch = { flush: 'sync' as const };

watch(() => form.containerReportIntervalSeconds, () => scheduleAutoSave(generalAutoSave, collectGeneralPayload), syncWatch);
watch(() => form.logLevel, () => scheduleAutoSave(generalAutoSave, collectGeneralPayload), syncWatch);
watch(() => form.reconcileTraceEnabled, () => scheduleAutoSave(generalAutoSave, collectGeneralPayload), syncWatch);
// 语言下拉：先立即切换界面语言，再进入 general 防抖保存；失败由 rollbackLanguage 回滚。
watch(() => form.language, (value) => {
  if (hydrating) return;
  const next = value === 'zh-CN' ? 'zh-CN' : 'en';
  if (next !== locale.value) setLocale(next);
  scheduleAutoSave(generalAutoSave, collectGeneralPayload);
}, syncWatch);
watch(() => form.tokenExpiration, () => scheduleAutoSave(securityAutoSave, collectSecurityPayload), syncWatch);
watch(() => form.remoteCommandTimeoutSeconds, () => scheduleAutoSave(securityAutoSave, collectSecurityPayload), syncWatch);
watch(() => form.panelDomain, () => scheduleAutoSave(certificatesAutoSave, collectCertificatesPayload), syncWatch);
watch(() => form.panelTlsCertificateId, () => scheduleAutoSave(certificatesAutoSave, collectCertificatesPayload), syncWatch);
watch(() => form.certificateEmail, () => scheduleAutoSave(certificatesAutoSave, collectCertificatesPayload), syncWatch);
watch(() => form.dnsPropagationDelaySeconds, () => scheduleAutoSave(certificatesAutoSave, collectCertificatesPayload), syncWatch);
watch(() => form.agentDownloadBaseUrl, () => scheduleAutoSave(agentAutoSave, collectAgentPayload), syncWatch);
watch(() => form.agentDownloadVerifyTls, () => scheduleAutoSave(agentAutoSave, collectAgentPayload), syncWatch);
watch(() => form.agentTransferTimeoutSeconds, () => scheduleAutoSave(agentAutoSave, collectAgentPayload), syncWatch);
watch(() => form.loginTitle, () => scheduleAutoSave(brandingAutoSave, collectBrandingPayload), syncWatch);
watch(() => form.loginSubtitle, () => scheduleAutoSave(brandingAutoSave, collectBrandingPayload), syncWatch);
watch(() => form.autoUpdateAppsEnabled, () => scheduleAutoSave(autoUpdateAutoSave, collectAutoUpdatePayload), syncWatch);
watch(() => form.autoUpdatePackagesEnabled, () => scheduleAutoSave(autoUpdateAutoSave, collectAutoUpdatePayload), syncWatch);
watch(() => form.autoUpdatePackagesMode, () => scheduleAutoSave(autoUpdateAutoSave, collectAutoUpdatePayload), syncWatch);
watch(() => form.autoUpdateScheduleTime, () => scheduleAutoSave(autoUpdateAutoSave, collectAutoUpdatePayload), syncWatch);
watch(() => form.imageRegistryEndpoint, () => scheduleAutoSave(imageRegistryAutoSave, collectImageRegistryPayload), syncWatch);
watch(() => form.imageRegistryScheme, () => scheduleAutoSave(imageRegistryAutoSave, collectImageRegistryPayload), syncWatch);
watch(() => form.variablesText, () => scheduleAutoSave(variablesAutoSave, collectVariablesPayload), syncWatch);

/**
 * 界面语言被其它入口（顶栏语言切换）改变时同步表单与 runtime，
 * 该语言已经由顶栏持久化，不能再次触发自动保存。
 */
watch(locale, (value) => {
  if (form.language === value) return;
  withProgrammatic(() => {
    form.language = value;
    if (runtime.value) runtime.value = { ...runtime.value, language: value };
  });
}, syncWatch);

/* ------------------------------------------------------------------------ */
/* 数据加载与动作                                                            */
/* ------------------------------------------------------------------------ */

/** 当前分区的刷新：取消待发自动保存后整体重载；外观激活时并行刷新共享 store。 */
async function refreshCurrentSection() {
  if (refreshing.value) return;
  refreshing.value = true;
  cancelAllAutoSaves();
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
      // 加载失败：EmptyState 就地展示并提供重试，不进弹窗/toast。
      error.value = firstError;
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('settingsPage.loadFailed');
  } finally {
    if (listRequests.isCurrent(requestId)) loading.value = false;
  }
}

function hydrate(settings: RuntimeSettings, variables: ServerVariableDefinition[]) {
  withProgrammatic(() => {
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
      autoUpdateAppsEnabled: settings.autoUpdate?.appsEnabled ?? false,
      autoUpdatePackagesEnabled: settings.autoUpdate?.packagesEnabled ?? false,
      autoUpdatePackagesMode: settings.autoUpdate?.packagesMode === 'all' ? 'all' : 'security',
      autoUpdateScheduleTime: settings.autoUpdate?.scheduleTime || '04:00',
      imageRegistryEndpoint: settings.imageRegistry?.endpoint ?? '',
      imageRegistryScheme: normalizeImageRegistryScheme(settings.imageRegistry?.scheme),
      variablesText: formatServerVariables(variables),
    });
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

async function confirmClearEventsAndLogs() {
  await run('clear-events-logs', async () => {
    await eventsApi.clear();
    confirmOpen.value = false;
    notifySuccess(t('settingsPage.clearEventsLogsSucceeded'));
  });
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
  if (confirmKind.value === 'jwt-secret') return confirmResetJwtSecret();
  if (confirmKind.value === 'clear-events-logs') return confirmClearEventsAndLogs();
  if (confirmKind.value === 'auto-update-run') return confirmAutoUpdateRun();
  return startBackup();
}

/**
 * 手动立即运行一次：不进入自动保存，不修改四个可写字段；成功后只刷新只读
 * lastRun 投影，避免覆盖用户尚未完成的表单输入。
 */
async function confirmAutoUpdateRun() {
  await run('auto-update-run', async () => {
    const result = await settingsApi.runAutoUpdate();
    notifySuccess(result.taskId
      ? t('settingsPage.autoUpdate.runAccepted', { taskId: result.taskId })
      : t('settingsPage.autoUpdate.runAcceptedWithoutId'), result);
    confirmOpen.value = false;
    try {
      runtime.value = await settingsApi.runtime();
    } catch {
      // 运行已受理；刷新只读投影失败不改变本次操作结果。
    }
  });
}

function handleConfirmOpenChange(open: boolean) {
  if (!open && pending.value) return;
  confirmOpen.value = open;
  if (!open) selectedSystemCertificate.value = null;
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
  || confirmKind.value === 'clear-events-logs'
));
const confirmImpact = computed(() => {
  if (confirmKind.value === 'clear-events-logs') return t('settingsPage.confirm.clear-events-logs.impact');
  if (confirmKind.value === 'auto-update-run') return t('settingsPage.confirm.auto-update-run.impact');
  return t('settingsPage.confirmDanger');
});
const confirmLabel = computed(() => {
  if (confirmKind.value === 'clear-events-logs') return t('settingsPage.clearEventsLogs');
  if (confirmKind.value === 'auto-update-run') return t('settingsPage.autoUpdate.runNow');
  return t('common.apply');
});

onMounted(load);

onBeforeUnmount(() => {
  // 卸载时把待保存修改 fire-and-forget 发出，并清理所有计时器。
  disposeAllAutoSaves();
});
</script>

<template>
  <ConsolePage width="content" :body-scroll="false" :title="activeSectionLabel" :description="t('routes.settings.description')">
    <template #actions>
      <Badge v-if="exportPending || restorePending" tone="warning">{{ restarting ? t('settingsPage.restarting') : t('settingsPage.pending') }}</Badge>
      <Button size="sm" :loading="loading || refreshing" @click="refreshCurrentSection"><RefreshCcw />{{ t('common.refresh') }}</Button>
    </template>

    <!--
      WinUI 3 Settings 页：TabView header 固定在内容区顶部，只有分区内容滚动；
      10 个分区共用 SettingsSection/SettingsCard。表单控件 500ms 自动保存，
      分区 actions 只承载 AutoSaveStatus 与保留的动作按钮。
    -->
    <Tabs
      class="h-full min-h-0 w-full [&_[role=tab]]:shrink-0 [&_[role=tab]]:whitespace-nowrap"
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
            <template #actions>
              <AutoSaveStatus :status="generalStatus" :labels="autoSaveLabels" @retry="generalAutoSave.flush()" />
            </template>

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
          </SettingsSection>

          <SettingsSection v-else-if="activeSection === 'security'">
            <template #actions>
              <AutoSaveStatus :status="securityStatus" :labels="autoSaveLabels" @retry="securityAutoSave.flush()" />
            </template>

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
          </SettingsSection>

          <SettingsSection v-else-if="activeSection === 'certificates'" :description="t('settingsPage.panelHttpsHint')">
            <template #actions>
              <AutoSaveStatus :status="certificatesStatus" :labels="autoSaveLabels" @retry="certificatesAutoSave.flush()" />
            </template>

            <SettingsCard :title="t('settingsPage.panelDomain')" :invalid="Boolean(fieldErrors.panelDomain)">
              <template #control>
                <Input
                  v-model="form.panelDomain"
                  :aria-label="t('settingsPage.panelDomain')"
                  :aria-describedby="fieldErrors.panelDomain ? 'settings-panel-domain-error' : undefined"
                  class="w-full sm:w-64"
                  :invalid="Boolean(fieldErrors.panelDomain)"
                />
              </template>
              <template v-if="fieldErrors.panelDomain" #hint>
                <span id="settings-panel-domain-error" class="text-danger">{{ t(fieldErrors.panelDomain) }}</span>
              </template>
            </SettingsCard>

            <SettingsCard :title="t('settingsPage.panelTlsCertificate')">
              <template #control>
                <Select v-model="form.panelTlsCertificateId" :aria-label="t('settingsPage.panelTlsCertificate')" class="w-full sm:w-64" :options="panelTlsOptions" />
              </template>
            </SettingsCard>

            <SettingsCard :title="t('settingsPage.certificateEmail')" :invalid="Boolean(fieldErrors.certificateEmail)">
              <template #control>
                <Input
                  v-model="form.certificateEmail"
                  :aria-label="t('settingsPage.certificateEmail')"
                  :aria-describedby="fieldErrors.certificateEmail ? 'settings-certificate-email-error' : undefined"
                  class="w-full sm:w-72"
                  :invalid="Boolean(fieldErrors.certificateEmail)"
                />
              </template>
              <template v-if="fieldErrors.certificateEmail" #hint>
                <span id="settings-certificate-email-error" class="text-danger">{{ t(fieldErrors.certificateEmail) }}</span>
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
          </SettingsSection>

          <SettingsSection v-else-if="activeSection === 'agent'" :description="t('settingsPage.agentDownloadHint')">
            <template #actions>
              <AutoSaveStatus :status="agentStatus" :labels="autoSaveLabels" @retry="agentAutoSave.flush()" />
            </template>

            <SettingsCard
              :title="t('settingsPage.agentDownloadBaseUrl')"
              :description="t('settingsPage.agentDownloadBaseUrlHint')"
              :invalid="Boolean(fieldErrors.agentDownloadBaseUrl)"
            >
              <template #control>
                <Input
                  v-model="form.agentDownloadBaseUrl"
                  :aria-label="t('settingsPage.agentDownloadBaseUrl')"
                  :aria-describedby="fieldErrors.agentDownloadBaseUrl ? 'settings-agent-base-url-error' : undefined"
                  class="w-full sm:w-72"
                  :invalid="Boolean(fieldErrors.agentDownloadBaseUrl)"
                  :placeholder="t('settingsPage.agentDownloadBaseUrlPlaceholder')"
                />
              </template>
              <template v-if="fieldErrors.agentDownloadBaseUrl" #hint>
                <span id="settings-agent-base-url-error" class="text-danger">{{ t(fieldErrors.agentDownloadBaseUrl) }}</span>
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
          </SettingsSection>

          <SettingsSection v-else-if="activeSection === 'auto-update'" :description="t('settingsPage.autoUpdateHint')">
            <template #actions>
              <AutoSaveStatus :status="autoUpdateStatus" :labels="autoSaveLabels" @retry="autoUpdateAutoSave.flush()" />
              <Button
                size="sm"
                variant="secondary"
                :loading="autoUpdateRunPending"
                :disabled="autoUpdateRunning || !autoUpdateCanRun"
                :title="autoUpdateRunHint"
                @click="openConfirm('auto-update-run')"
              ><Play />{{ t('settingsPage.autoUpdate.runNow') }}</Button>
            </template>

            <SettingsCard :title="t('settingsPage.autoUpdate.appsEnabled')" :description="t('settingsPage.autoUpdate.appsEnabledHint')">
              <template #control>
                <Switch v-model="form.autoUpdateAppsEnabled" :label="t('settingsPage.autoUpdate.appsEnabled')" />
              </template>
            </SettingsCard>

            <SettingsCard :title="t('settingsPage.autoUpdate.packagesEnabled')" :description="t('settingsPage.autoUpdate.packagesEnabledHint')">
              <template #control>
                <Switch v-model="form.autoUpdatePackagesEnabled" :label="t('settingsPage.autoUpdate.packagesEnabled')" />
              </template>
            </SettingsCard>

            <SettingsCard
              :title="t('settingsPage.autoUpdate.packagesMode')"
              :description="form.autoUpdatePackagesMode === 'security' ? t('settingsPage.autoUpdate.packagesModeSecurityHint') : t('settingsPage.autoUpdate.packagesModeAllHint')"
              :disabled="!form.autoUpdatePackagesEnabled"
            >
              <template #control>
                <Select
                  v-model="form.autoUpdatePackagesMode"
                  :aria-label="t('settingsPage.autoUpdate.packagesMode')"
                  class="w-full sm:w-64"
                  :options="autoUpdatePackagesModeOptions"
                  :disabled="!form.autoUpdatePackagesEnabled"
                />
              </template>
            </SettingsCard>

            <SettingsCard
              :title="t('settingsPage.autoUpdate.scheduleTime')"
              :description="t('settingsPage.autoUpdate.scheduleTimeHint')"
              :invalid="Boolean(fieldErrors.autoUpdateScheduleTime)"
            >
              <template #control>
                <div class="grid gap-1">
                  <TimeOfDayInput
                    v-model="form.autoUpdateScheduleTime"
                    :label="t('settingsPage.autoUpdate.scheduleTime')"
                    :hour-label="t('settingsPage.autoUpdate.hourLabel')"
                    :minute-label="t('settingsPage.autoUpdate.minuteLabel')"
                    :invalid="Boolean(fieldErrors.autoUpdateScheduleTime)"
                  />
                  <span class="text-caption text-muted-foreground">{{ t('settingsPage.autoUpdate.localTimeHint') }}</span>
                </div>
              </template>
              <template v-if="fieldErrors.autoUpdateScheduleTime" #hint>
                <span class="text-danger">{{ t(fieldErrors.autoUpdateScheduleTime) }}</span>
              </template>
            </SettingsCard>

            <SettingsCard :title="t('settingsPage.autoUpdate.lastRun')" :description="t('settingsPage.autoUpdate.lastRunHint')">
              <div class="grid gap-3 text-sm sm:grid-cols-2">
                <div>
                  <span class="block text-muted-foreground">{{ t('settingsPage.autoUpdate.lastRunAt') }}</span>
                  <strong class="block text-foreground">{{ formatDateTime(autoUpdateLastRun?.lastRunAt, t('settingsPage.autoUpdate.lastRunNever')) }}</strong>
                </div>
                <div>
                  <span class="block text-muted-foreground">{{ t('settingsPage.autoUpdate.lastRunStatus') }}</span>
                  <Badge :tone="autoUpdateRunStatusTone(autoUpdateLastRun?.lastRunStatus)">{{ autoUpdateRunStatusLabel(autoUpdateLastRun?.lastRunStatus) }}</Badge>
                </div>
                <div class="sm:col-span-2">
                  <span class="block text-muted-foreground">{{ t('settingsPage.autoUpdate.lastRunTask') }}</span>
                  <span class="panel-mono block break-all text-caption text-secondary-foreground">{{ autoUpdateLastRun?.lastRunTaskId || t('common.notAvailable') }}</span>
                </div>
              </div>
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
            <template #actions>
              <AutoSaveStatus :status="brandingStatus" :labels="autoSaveLabels" @retry="brandingAutoSave.flush()" />
            </template>

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

            <SettingsCard :title="t('settingsPage.loginTitle')" :invalid="Boolean(fieldErrors.loginTitle)">
              <template #control>
                <Input
                  id="settings-login-title"
                  v-model="form.loginTitle"
                  :aria-label="t('settingsPage.loginTitle')"
                  :aria-describedby="fieldErrors.loginTitle ? 'settings-login-title-error' : undefined"
                  class="w-full sm:w-72"
                  :invalid="Boolean(fieldErrors.loginTitle)"
                />
              </template>
              <template v-if="fieldErrors.loginTitle" #hint>
                <span id="settings-login-title-error" class="text-danger">{{ t(fieldErrors.loginTitle, { max: loginTitleMaxLength }) }}</span>
              </template>
            </SettingsCard>

            <SettingsCard :title="t('settingsPage.loginSubtitle')" :invalid="Boolean(fieldErrors.loginSubtitle)">
              <template #control>
                <Input
                  id="settings-login-subtitle"
                  v-model="form.loginSubtitle"
                  :aria-label="t('settingsPage.loginSubtitle')"
                  :aria-describedby="fieldErrors.loginSubtitle ? 'settings-login-subtitle-error' : undefined"
                  class="w-full sm:w-72"
                  :invalid="Boolean(fieldErrors.loginSubtitle)"
                />
              </template>
              <template v-if="fieldErrors.loginSubtitle" #hint>
                <span id="settings-login-subtitle-error" class="text-danger">{{ t(fieldErrors.loginSubtitle, { max: loginSubtitleMaxLength }) }}</span>
              </template>
            </SettingsCard>

            <SettingsCard :title="t('settingsPage.serverVariables')" stacked>
              <template #control>
                <Textarea v-model="form.variablesText" :aria-label="t('settingsPage.serverVariables')" class="min-h-44 w-full font-mono" :placeholder="t('settingsPage.serverVariablesHint')" />
              </template>
              <template #hint>
                <div class="grid gap-1">
                  <AutoSaveStatus :status="variablesStatus" :labels="autoSaveLabels" @retry="variablesAutoSave.flush()" />
                  <span v-if="variablesStatus === 'invalid'" class="text-danger">{{ t('settingsPage.serverVariablesInvalid') }}</span>
                </div>
              </template>
            </SettingsCard>
          </SettingsSection>

          <SettingsSection v-else-if="activeSection === 'image-registry'" :description="t('settingsPage.imageRegistry.hint')">
            <template #actions>
              <AutoSaveStatus :status="imageRegistryStatus" :labels="autoSaveLabels" @retry="imageRegistryAutoSave.flush()" />
            </template>

            <SettingsCard
              :title="t('settingsPage.imageRegistry.endpoint')"
              :description="t('settingsPage.imageRegistry.endpointHint')"
              :invalid="Boolean(fieldErrors.imageRegistryEndpoint)"
            >
              <template #control>
                <Input
                  id="settings-image-registry-endpoint"
                  v-model="form.imageRegistryEndpoint"
                  :aria-label="t('settingsPage.imageRegistry.endpoint')"
                  :aria-describedby="fieldErrors.imageRegistryEndpoint ? 'settings-image-registry-endpoint-error' : undefined"
                  class="w-full sm:w-72"
                  :invalid="Boolean(fieldErrors.imageRegistryEndpoint)"
                  :placeholder="t('settingsPage.imageRegistry.endpointPlaceholder')"
                />
              </template>
              <template v-if="fieldErrors.imageRegistryEndpoint" #hint>
                <span id="settings-image-registry-endpoint-error" class="text-danger">{{ t(fieldErrors.imageRegistryEndpoint) }}</span>
              </template>
            </SettingsCard>

            <SettingsCard :title="t('settingsPage.imageRegistry.scheme')">
              <template #control>
                <Select
                  v-model="form.imageRegistryScheme"
                  :aria-label="t('settingsPage.imageRegistry.scheme')"
                  class="w-full sm:w-64"
                  :options="imageRegistrySchemeOptions"
                />
              </template>
            </SettingsCard>
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
                <Button class="max-sm:w-full" variant="primary" :disabled="Boolean(form.exportEncrypt && !form.exportPassword.trim())" :loading="pending === 'export'" @click="openConfirm('export')">
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
                <Button class="max-sm:w-full" variant="danger" :disabled="!restoreFile || !preflight || !form.restoreConfirm" :loading="pending === 'restore'" @click="openConfirm('restore')">
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
      :impact="confirmImpact"
      :tone="confirmIsDanger ? 'danger' : 'warning'"
      :loading="Boolean(pending)"
      :require-checkbox="confirmIsDanger"
      :checkbox-label="confirmKind === 'clear-events-logs' ? t('settingsPage.confirm.clear-events-logs.checkbox') : t('serversPage.confirmCheckbox')"
      :confirm-label="confirmLabel"
      :cancel-label="t('common.cancel')"
      @update:open="handleConfirmOpenChange"
      @confirm="confirmAction"
    />
  </ConsolePage>
</template>
