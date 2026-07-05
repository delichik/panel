<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, reactive, ref, watch } from 'vue';
import { useI18n } from '@/i18n';
import { applicationsApi } from '@/api/applications';
import { serversApi } from '@/api/servers';
import type { ApplicationDto, ApplicationFileDto, ApplicationFileKind, ApplicationPanelFileDto, ApplicationReverseProxyRuleDto, ApplicationSaveDto, ApplicationTemplateVariableDto, ServerDto } from '@/types/api';
import AppActionButton from '@/components/AppActionButton.vue';
import AppActionGroup from '@/components/AppActionGroup.vue';
import AppPagination from '@/components/AppPagination.vue';
import { usePagination } from '@/composables/usePagination';
import { parseSpecYaml, toSpecYaml } from './appSpecYaml';

const props = defineProps<{ application: ApplicationDto | null; open: boolean; embedded?: boolean }>();
const emit = defineEmits<{ close: []; saved: [ApplicationDto, string?] }>();
const { t, translateApplicationFileKind } = useI18n();

interface PortForm { label: string; to: number; static: number | null }
interface StringListItem { value: string }
interface EnvForm { key: string; value: string }
type MountType = 'volume' | 'host' | 'file' | 'panel_file' | 'persistent';
interface MountForm { type: MountType; source: string; target: string; readOnly: boolean; uid: number | null | ''; gid: number | null | ''; mode: string; expanded: boolean }
interface VariableForm { key: string; value: string }
interface EditorFile {
  id: string;
  path: string;
  kind: ApplicationFileKind;
  contentType: string;
  size: number;
  sha256: string;
  contentBase64?: string;
}
interface PendingArchive {
  id: string;
  basePath: string;
  kind: ApplicationFileKind;
  file: File;
  replacedFiles: EditorFile[];
}

const form = reactive<ApplicationSaveDto>({ name: '', enabled: false, specYaml: defaultSpec(), variables: {}, deploymentMode: 'all', deploymentServers: [], reverseProxy: [] });
const specForm = reactive({
  name: 'web',
  image: '',
  networkMode: 'bridge' as 'bridge' | 'host',
  command: [] as StringListItem[],
  cpu: null as number | null,
  memoryMb: null as number | null,
  privileged: false,
  capAdd: [] as StringListItem[],
  ports: [] as PortForm[],
  env: [] as EnvForm[],
  mounts: [] as MountForm[],
});
const activeEditorTab = ref<'visual' | 'yaml'>('visual');
const variableRows = ref<VariableForm[]>([]);
const loading = ref('');
const saveStep = ref('');
const error = ref('');
const embeddedSpecMode = ref<'visual' | 'yaml'>('visual');
const embeddedYamlEditing = ref(false);
const servers = ref<ServerDto[]>([]);
const files = ref<EditorFile[]>([]);
const pendingArchives = ref<PendingArchive[]>([]);
const loadingFileId = ref('');
const templateVariables = ref<ApplicationTemplateVariableDto[]>([]);
const panelFiles = ref<ApplicationPanelFileDto[]>([]);
const templateTextarea = ref();
const yamlTextarea = ref<HTMLTextAreaElement | { $el?: HTMLElement } | null>(null);
const yamlGutter = ref<HTMLElement | null>(null);
const editorBody = ref<HTMLElement | { $el?: HTMLElement } | null>(null);
const activeSectionId = ref('application-editor-general');
const proxyRuleDialog = reactive({ open: false, index: -1 });
const mountDialog = reactive({ open: false, index: -1 });
let filesRequestId = 0;
let sectionObserver: IntersectionObserver | null = null;
let sectionScrollRoot: HTMLElement | null = null;
let sectionScrollHandler: (() => void) | null = null;
const fileForm = reactive({
  mode: 'single' as 'single' | 'archive',
  intent: 'create' as 'create' | 'edit-template' | 'replace-binary' | 'replace-archive',
  path: 'config/app.conf',
  kind: 'template' as ApplicationFileKind,
  template: '',
  file: null as File | File[] | null,
});
const fileDialog = ref(false);

const title = computed(() => (props.application ? t('applicationEditor.editTitle', { name: props.application.name }) : t('applicationEditor.createTitle')));
const saving = computed(() => Boolean(loading.value));
const editorVisible = computed(() => props.embedded || props.open);
const editorShell = computed(() => (props.embedded ? 'div' : 'v-dialog'));
const editorShellProps = computed(() => (props.embedded
  ? { class: 'embedded-editor-shell' }
  : { modelValue: props.open, width: 'min(1120px, calc(100vw - 32px))', persistent: saving.value }));
const editorCardClass = computed(() => ['app-dialog-card', 'editor-card', { 'editor-card--embedded': props.embedded }]);
const closeIcon = computed(() => (props.embedded ? 'mdi-arrow-left' : 'mdi-close'));
const specEditorMode = computed<'visual' | 'yaml'>({
  get: () => (props.embedded ? embeddedSpecMode.value : activeEditorTab.value),
  set: (mode) => {
    if (props.embedded) setEmbeddedSpecMode(mode);
    else activeEditorTab.value = mode;
  },
});
const yamlLineNumbers = computed(() => Array.from({ length: Math.max(1, form.specYaml.split('\n').length) }, (_, index) => index + 1));
const serverOptions = computed(() => servers.value.map((server) => ({
  title: `${server.name} (${server.host})`,
  value: server.id,
})));
const proxyTargetTypeOptions = computed(() => [
  { title: t('applicationEditor.targetLocal'), value: 'local' },
  { title: t('applicationEditor.targetContainer'), value: 'container' },
]);
const mountTypeOptions = computed(() => [
  { title: t('applicationEditor.dockerVolume'), value: 'volume' },
  { title: t('applicationEditor.hostPath'), value: 'host' },
  { title: t('applicationEditor.appFile'), value: 'file' },
  { title: t('applicationEditor.panelFile'), value: 'panel_file' },
  { title: t('applicationEditor.persistent'), value: 'persistent' },
]);
const currentProxyRule = computed(() => form.reverseProxy?.[proxyRuleDialog.index] ?? null);
const currentMount = computed(() => specForm.mounts[mountDialog.index] ?? null);
const editorSections = computed(() => {
  const sections = [
    { id: 'application-editor-general', title: t('applicationEditor.general'), icon: 'mdi-application-edit-outline' },
  ];
  if (props.embedded && embeddedSpecMode.value === 'yaml') {
    sections.push({ id: 'application-editor-yaml', title: t('applicationEditor.yaml'), icon: 'mdi-code-braces' });
  } else {
    sections.push(
      { id: 'application-editor-runtime', title: t('applicationEditor.runtime'), icon: 'mdi-cube-outline' },
      { id: 'application-editor-network', title: t('applicationEditor.network'), icon: 'mdi-lan' },
      { id: 'application-editor-mounts', title: t('applicationEditor.mounts'), icon: 'mdi-folder-sync-outline' },
    );
  }
  sections.push(
    { id: 'application-editor-deployment', title: t('applicationEditor.deploymentTargets'), icon: 'mdi-server-network' },
    { id: 'application-editor-proxy', title: t('applicationEditor.reverseProxy'), icon: 'mdi-web' },
    { id: 'application-editor-files', title: t('applicationEditor.files'), icon: 'mdi-file-tree-outline' },
  );
  return sections;
});
const fileKindLocked = computed(() => fileForm.intent !== 'create');
const existingFileAtPath = computed(() => files.value.find((file) => file.path === fileForm.path.trim()));
const hasFilesUnderBasePath = computed(() => files.value.some((file) => isArchivePathMatch(file.path, fileForm.path.trim())));
const fileSubmitLabel = computed(() => {
  if (fileForm.mode === 'archive') {
    return hasFilesUnderBasePath.value ? t('applicationEditor.replaceFolderArchive') : t('applicationEditor.addFolderArchive');
  }
  if (existingFileAtPath.value) return t('applicationEditor.replaceFile');
  return t('common.addFile');
});
const {
  page: filePage,
  pageSize: filePageSize,
  total: fileTotal,
  pageItems: pagedFiles,
} = usePagination(files);

watch(editorVisible, (open) => {
  if (!open) return;
  const app = props.application;
  form.name = app?.name ?? '';
  form.enabled = app?.enabled ?? false;
  form.specYaml = app?.specYaml ?? defaultSpec();
  form.variables = { ...(app?.variables ?? {}) };
  form.deploymentMode = app?.deploymentMode ?? 'all';
  form.deploymentServers = [...(app?.deploymentServers ?? [])];
  form.reverseProxy = cloneReverseProxy(app?.reverseProxy ?? []);
  loadSpecForm(form.specYaml, app?.name);
  const syncedName = app?.name ?? (specForm.name || 'web');
  form.name = syncedName;
  specForm.name = syncedName;
  variableRows.value = Object.entries(form.variables).map(([key, value]) => ({ key, value }));
  closeProxyRuleDialog();
  closeMountDialog();
  error.value = '';
  activeEditorTab.value = 'visual';
  embeddedSpecMode.value = 'visual';
  embeddedYamlEditing.value = false;
  fileDialog.value = false;
  files.value = [];
  pendingArchives.value = [];
  filesRequestId += 1;
  void loadServers();
  void loadTemplateCatalog();
  if (app) void loadFiles(app.id);
  void syncSectionObserver();
}, { immediate: true });

watch(() => props.embedded, () => {
  void syncSectionObserver();
});

watch(embeddedSpecMode, () => {
  void syncSectionObserver();
});

watch(() => form.name, (name) => {
  if (specForm.name !== name) specForm.name = name;
});

watch(() => fileForm.kind, (kind) => {
  if (kind === 'template') {
    fileForm.mode = 'single';
    fileForm.file = null;
  }
});

watch(activeEditorTab, (tab, previous) => {
  if (tab === 'yaml' && previous === 'visual') applyVisualSpec();
  if (tab === 'visual' && previous === 'yaml') {
    loadSpecForm(form.specYaml, form.name);
    if (!props.application && specForm.name) form.name = specForm.name;
  }
});

function defaultSpec() {
  return 'name: web\nnetworkMode: bridge\n';
}

function loadDefaultSpecForm(appName?: string) {
  specForm.name = appName || 'web';
  specForm.image = '';
  specForm.networkMode = 'bridge';
  specForm.command = [];
  specForm.cpu = null;
  specForm.memoryMb = null;
  specForm.privileged = false;
  specForm.capAdd = [];
  specForm.ports = [];
  specForm.env = [];
  specForm.mounts = [];
}

function loadSpecForm(raw: string, appName?: string) {
  loadDefaultSpecForm(appName);
  const parsed = parseSpecYaml(raw);
  if (!parsed) return;
  specForm.name = stringValue(parsed.name) || appName || 'web';
  specForm.image = stringValue(parsed.image);
  const networkMode = stringValue(parsed.networkMode);
  specForm.networkMode = networkMode === 'host' ? 'host' : 'bridge';
  specForm.command = arrayItems(parsed.command, false);
  specForm.cpu = numericLimit(objectValue(parsed.resources)?.cpu);
  specForm.memoryMb = numericLimit(objectValue(parsed.resources)?.memoryMb);
  specForm.privileged = Boolean(parsed.privileged);
  specForm.capAdd = arrayItems(parsed.capAdd, false).map((item) => ({ value: item.value.toUpperCase() }));
  specForm.ports = arrayValue(parsed.ports).map((item, index) => {
    const port = objectValue(item);
    return {
      label: stringValue(port?.label) || `port-${index + 1}`,
      to: numberValue(port?.to) || 80,
      static: numberValue(port?.static),
    };
  });
  const env = objectValue(parsed.env);
  specForm.env = env ? Object.entries(env).map(([key, value]) => ({ key, value: stringValue(value) })) : [];
  specForm.mounts = arrayValue(parsed.mounts).map((item) => {
    const mount = objectValue(item);
    const type = stringValue(mount?.type) as MountType;
    return {
      type: ['volume', 'host', 'file', 'panel_file', 'persistent'].includes(type) ? type : 'volume',
      source: stringValue(mount?.source),
      target: stringValue(mount?.target) || '/data',
      readOnly: Boolean(mount?.readOnly),
      uid: nonNegativeNumberValue(mount?.uid),
      gid: nonNegativeNumberValue(mount?.gid),
      mode: stringValue(mount?.mode),
      expanded: false,
    };
  });
}

function addPort() {
  specForm.ports.push({ label: `port-${specForm.ports.length + 1}`, to: 80, static: null });
}

function addEnv() {
  specForm.env.push({ key: '', value: '' });
}

function addStringItem(items: StringListItem[]) {
  items.push({ value: '' });
}

function addVariable() {
  variableRows.value.push({ key: '', value: '' });
}

function addMount(type: MountType = 'volume') {
  specForm.mounts.push({ type, source: defaultMountSource(type), target: '/data', readOnly: mountDefaultsReadOnly(type), uid: null, gid: null, mode: '', expanded: false });
  openMountDialog(specForm.mounts.length - 1);
}

function defaultMountSource(type: MountType) {
  if (type === 'host') return '/srv/app';
  if (type === 'file') return files.value[0]?.path ?? 'config/app.conf';
  if (type === 'panel_file') return panelFiles.value[0]?.source ?? '';
  if (type === 'persistent') return '';
  return `${specForm.name || 'app'}-data`;
}

function updateMountType(mount: MountForm) {
  mount.source = defaultMountSource(mount.type);
  mount.readOnly = mountDefaultsReadOnly(mount.type);
  if (!mountSupportsOwnership(mount.type)) {
    mount.uid = null;
    mount.gid = null;
  }
  if (!mountSupportsMode(mount.type)) {
    mount.mode = '';
  }
}

function mountDefaultsReadOnly(type: MountType) {
  return type === 'file' || type === 'panel_file';
}

function mountSupportsOwnership(type: MountType) {
  return type === 'file' || type === 'panel_file' || type === 'persistent';
}

function mountSupportsMode(type: MountType) {
  return type === 'file' || type === 'persistent';
}

function mountSupportsExecutable(type: MountType) {
  return type === 'file';
}

function fileMountExecutable(mount: MountForm) {
  const mode = mount.mode.trim();
  if (!mode) return false;
  const parsed = Number.parseInt(mode, 8);
  return Number.isFinite(parsed) && (parsed & 0o111) !== 0;
}

function setFileMountExecutable(mount: MountForm, value: boolean | null) {
  mount.mode = value ? '0755' : '';
}

function removeMount(index: number) {
  specForm.mounts.splice(index, 1);
  if (mountDialog.index === index) closeMountDialog();
  else if (mountDialog.index > index) mountDialog.index -= 1;
}

function openMountDialog(index: number) {
  mountDialog.index = index;
  mountDialog.open = true;
}

function closeMountDialog() {
  mountDialog.open = false;
  mountDialog.index = -1;
}

function mountTypeLabel(mount: MountForm) {
  return mountTypeOptions.value.find((item) => item.value === mount.type)?.title ?? t('common.unknown');
}

function mountSourceLabel(mount: MountForm) {
  if (mount.type === 'persistent') return mount.source || t('applicationEditor.persistent');
  return mount.source || t('common.noData');
}

function mountTargetLabel(mount: MountForm) {
  return mount.target || '/data';
}

function addProxyRule() {
  form.reverseProxy = [...(form.reverseProxy ?? []), { domain: '', targetType: 'local', targetPort: 80, paths: [{ path: '/', webSocket: false }] }];
  openProxyRuleDialog(form.reverseProxy.length - 1);
}

function addProxyPath(rule: ApplicationReverseProxyRuleDto) {
  rule.paths.push({ path: '/', webSocket: false });
}

function removeProxyRule(index: number) {
  form.reverseProxy = [...(form.reverseProxy ?? [])].filter((_, itemIndex) => itemIndex !== index);
  if (proxyRuleDialog.index === index) closeProxyRuleDialog();
  else if (proxyRuleDialog.index > index) proxyRuleDialog.index -= 1;
}

function openProxyRuleDialog(index: number) {
  proxyRuleDialog.index = index;
  proxyRuleDialog.open = true;
}

function closeProxyRuleDialog() {
  proxyRuleDialog.open = false;
  proxyRuleDialog.index = -1;
}

function removeAt<T>(items: T[], index: number) {
  items.splice(index, 1);
}

function cloneReverseProxy(rules: ApplicationReverseProxyRuleDto[]) {
  return rules.map((rule) => ({
    domain: rule.domain,
    targetType: rule.targetType === 'container' ? 'container' : 'local',
    targetPort: rule.targetPort,
    paths: rule.paths?.map((path) => ({ path: path.path, webSocket: path.webSocket })) ?? [{ path: '/', webSocket: false }],
  }));
}

function proxyTargetName(rule: ApplicationReverseProxyRuleDto) {
  return rule.targetType === 'container'
    ? (specForm.name || t('applicationEditor.appTargetFallback'))
    : t('applicationEditor.nodeTarget');
}

async function loadFiles(applicationId: string) {
  const requestId = ++filesRequestId;
  try {
    const result = (await applicationsApi.files(applicationId)).map(toEditorFile);
    if (requestId !== filesRequestId || props.application?.id !== applicationId || !editorVisible.value) return;
    files.value = result;
  } catch (err) {
    if (requestId !== filesRequestId || props.application?.id !== applicationId || !editorVisible.value) return;
    error.value = err instanceof Error ? err.message : t('applicationEditor.loadFilesFailed');
  }
}

async function loadServers() {
  try {
    servers.value = await serversApi.listServers();
  } catch {
    servers.value = [];
  }
}

async function loadTemplateCatalog() {
  try {
    const catalog = await applicationsApi.templateCatalog();
    templateVariables.value = catalog.variables ?? [];
    panelFiles.value = catalog.panelFiles ?? [];
  } catch {
    templateVariables.value = [];
    panelFiles.value = [];
  }
}

function variableItems(target: 'spec' | 'template') {
  const builtins = templateVariables.value.map((item) => ({
    title: item.key,
    value: target === 'spec' ? item.specExpression : item.templateExpression,
  }));
  const custom = variableRows.value
    .filter((item) => item.key.trim())
    .map((item) => ({ title: `vars.${item.key.trim()}`, value: `{{ .vars.${item.key.trim()} }}` }));
  return [...builtins, ...custom];
}

async function insertVariable(target: 'spec' | 'template', expression: string) {
  const component = target === 'spec' ? yamlTextarea.value : templateTextarea.value;
  const textarea = resolveTextarea(component);
  const current = target === 'spec' ? form.specYaml : fileForm.template;
  const start = textarea?.selectionStart ?? current.length;
  const end = textarea?.selectionEnd ?? start;
  const next = current.slice(0, start) + expression + current.slice(end);
  if (target === 'spec') form.specYaml = next;
  else fileForm.template = next;
  await nextTick();
  const nextTextarea = resolveTextarea(component);
  nextTextarea?.focus();
  nextTextarea?.setSelectionRange(start + expression.length, start + expression.length);
}

function resolveTextarea(component: HTMLTextAreaElement | { $el?: HTMLElement } | null | undefined) {
  if (!component) return undefined;
  if (component instanceof HTMLTextAreaElement) return component;
  return component.$el?.querySelector?.('textarea') as HTMLTextAreaElement | undefined;
}

function toEditorFile(file: ApplicationFileDto): EditorFile {
  return {
    id: file.id,
    path: file.path,
    kind: file.kind,
    contentType: file.contentType,
    size: file.size,
    sha256: file.sha256,
  };
}

function selectedFile() {
  return Array.isArray(fileForm.file) ? fileForm.file[0] : fileForm.file;
}

function resetFileForm(kind: ApplicationFileKind = 'template') {
  fileForm.mode = 'single';
  fileForm.intent = 'create';
  fileForm.path = kind === 'template' ? 'config/app.conf' : 'public/app.bin';
  fileForm.kind = kind;
  fileForm.template = '';
  fileForm.file = null;
}

function openFileDialog(kind: ApplicationFileKind = 'template') {
  resetFileForm(kind);
  fileDialog.value = true;
}

function closeFileDialog() {
  fileDialog.value = false;
}

function setEmbeddedSpecMode(mode: 'visual' | 'yaml') {
  if (embeddedSpecMode.value === mode) return;
  if (mode === 'yaml') {
    if (!embeddedYamlEditing.value) applyVisualSpec();
    embeddedYamlEditing.value = true;
  } else {
    loadSpecForm(form.specYaml, form.name);
    if (!props.application && specForm.name) form.name = specForm.name;
  }
  embeddedSpecMode.value = mode;
}

function onEmbeddedSpecModeUpdate(value: unknown) {
  if (value === 'visual' || value === 'yaml') setEmbeddedSpecMode(value);
}

function prepareEmbeddedYamlEdit() {
  if (!props.embedded) return;
  if (embeddedYamlEditing.value) return;
  applyVisualSpec();
  embeddedYamlEditing.value = true;
}

function markYamlEdited() {
  if (!props.embedded) return;
  embeddedYamlEditing.value = true;
}

function syncYamlEditorScroll(event: Event) {
  if (!yamlGutter.value) return;
  yamlGutter.value.scrollTop = (event.target as HTMLTextAreaElement).scrollTop;
}

function editorBodyElement() {
  const target = editorBody.value;
  if (!target) return null;
  if (target instanceof HTMLElement) return target;
  return target.$el ?? null;
}

function stopSectionObserver() {
  sectionObserver?.disconnect();
  sectionObserver = null;
  if (sectionScrollRoot && sectionScrollHandler) {
    sectionScrollRoot.removeEventListener('scroll', sectionScrollHandler);
  }
  sectionScrollRoot = null;
  sectionScrollHandler = null;
}

async function syncSectionObserver() {
  stopSectionObserver();
  if (!props.embedded || !editorVisible.value) return;
  await nextTick();
  const root = editorBodyElement();
  if (!root) return;
  const sections = editorSections.value
    .map((section) => root.querySelector<HTMLElement>(`#${section.id}`))
    .filter((section): section is HTMLElement => Boolean(section));
  if (!sections.length) return;
  updateActiveSection(root, sections);
  sectionScrollRoot = root;
  sectionScrollHandler = () => updateActiveSection(root, sections);
  root.addEventListener('scroll', sectionScrollHandler, { passive: true });
  sectionObserver = new IntersectionObserver((entries) => {
    if (entries.some((entry) => entry.isIntersecting)) updateActiveSection(root, sections);
  }, {
    root,
    rootMargin: '-6% 0px -72% 0px',
    threshold: [0, 0.2, 0.5],
  });
  sections.forEach((section) => sectionObserver?.observe(section));
}

function updateActiveSection(root: HTMLElement, sections: HTMLElement[]) {
  const rootTop = root.getBoundingClientRect().top;
  const anchor = rootTop + 120;
  let active = sections[0];
  for (const section of sections) {
    const rect = section.getBoundingClientRect();
    if (rect.top <= anchor) active = section;
    if (rect.top > anchor) break;
  }
  activeSectionId.value = active.id;
}

onBeforeUnmount(stopSectionObserver);

function encodeText(value: string) {
  return bytesToBase64(new TextEncoder().encode(value));
}

async function encodeFile(file: File) {
  return bytesToBase64(new Uint8Array(await file.arrayBuffer()));
}

function bytesToBase64(bytes: Uint8Array) {
  let binary = '';
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary);
}

function base64ToText(value?: string) {
  if (!value) return '';
  const binary = atob(value);
  const bytes = Uint8Array.from(binary, (char) => char.charCodeAt(0));
  return new TextDecoder().decode(bytes);
}

function isArchivePathMatch(path: string, basePath: string) {
  const base = basePath.replace(/^\/+|\/+$/g, '');
  if (!base) return false;
  return path === base || path.startsWith(`${base}/`);
}

async function addFile() {
  const path = fileForm.path.trim();
  if (!path) return;
  const picked = selectedFile();
  if (fileForm.mode === 'archive') {
    if (!picked) return;
    const replacedFiles = files.value.filter((file) => isArchivePathMatch(file.path, path));
    files.value = files.value.filter((file) => !isArchivePathMatch(file.path, path));
    pendingArchives.value.push({
      id: `archive-${Date.now()}`,
      basePath: path,
      kind: 'archive',
      file: picked,
      replacedFiles,
    });
    fileForm.file = null;
    fileDialog.value = false;
    return;
  }
  const contentBase64 = fileForm.kind === 'template'
    ? encodeText(fileForm.template)
    : picked
      ? await encodeFile(picked)
      : '';
  const size = fileForm.kind === 'template' ? new TextEncoder().encode(fileForm.template).length : picked?.size ?? 0;
  const next: EditorFile = {
    id: `local-${Date.now()}`,
    path,
    kind: fileForm.kind,
    contentType: '',
    size,
    sha256: 'pending',
    contentBase64,
  };
  const index = files.value.findIndex((file) => file.path === path);
  if (index >= 0) files.value.splice(index, 1, next);
  else files.value.push(next);
  fileForm.template = '';
  fileForm.file = null;
  fileDialog.value = false;
}

function removeArchive(archive: PendingArchive) {
  const index = pendingArchives.value.findIndex((item) => item.id === archive.id);
  if (index >= 0) pendingArchives.value.splice(index, 1);
  for (const file of archive.replacedFiles) {
    if (!files.value.some((item) => item.path === file.path)) files.value.push(file);
  }
  files.value.sort((a, b) => a.path.localeCompare(b.path));
}

function removeFile(file: EditorFile) {
  const index = files.value.findIndex((item) => item.id === file.id && item.path === file.path);
  if (index >= 0) files.value.splice(index, 1);
}

async function editTemplateFile(file: EditorFile) {
  fileForm.kind = 'template';
  fileForm.mode = 'single';
  fileForm.intent = 'edit-template';
  fileForm.path = file.path;
  loadingFileId.value = file.id;
  try {
    const loaded = file.contentBase64 !== undefined || !props.application?.id
      ? file
      : await applicationsApi.getFile(props.application.id, file.id);
    fileForm.template = base64ToText(loaded.contentBase64);
    fileForm.file = null;
    fileDialog.value = true;
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('applicationEditor.loadFileFailed');
  } finally {
    loadingFileId.value = '';
  }
}

function replaceBinaryFile(file: EditorFile) {
  fileForm.kind = 'binary';
  fileForm.mode = 'single';
  fileForm.intent = 'replace-binary';
  fileForm.path = file.path;
  fileForm.file = null;
  fileDialog.value = true;
}

function replaceArchiveFile(file: EditorFile) {
  fileForm.kind = 'archive';
  fileForm.mode = 'archive';
  fileForm.intent = 'replace-archive';
  fileForm.path = file.path;
  fileForm.file = null;
  fileDialog.value = true;
}

function sizeLabel(size: number) {
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / 1024 / 1024).toFixed(1)} MB`;
}

function applyVisualSpec() {
  form.specYaml = buildSpecYaml();
  error.value = '';
}

function buildSpecYaml() {
  const currentSpec = parseSpecYaml(form.specYaml);
  const spec: Record<string, unknown> = {
    name: specForm.name,
    image: specForm.image,
    networkMode: specForm.networkMode,
  };
  const command = stringListValues(specForm.command);
  const env = Object.fromEntries(specForm.env.filter((item) => item.key.trim()).map((item) => [item.key.trim(), item.value]));
  const ports = specForm.networkMode === 'bridge' ? specForm.ports
    .filter((port) => port.label.trim() && port.to)
    .map((port) => ({ label: port.label.trim(), to: Number(port.to), ...(port.static ? { static: Number(port.static) } : {}) })) : [];
  const mounts = specForm.mounts
    .filter((mount) => mount.target.trim() && (mount.type === 'persistent' || mount.source.trim()))
    .map(mountYamlValue);
  if (command.length) spec.command = command;
  if (Object.keys(env).length) spec.env = env;
  if (ports.length) spec.ports = ports;
  const resources: Record<string, number> = {};
  const cpu = numericLimit(specForm.cpu);
  const memoryMb = numericLimit(specForm.memoryMb);
  if (cpu !== null) resources.cpu = cpu;
  if (memoryMb !== null) resources.memoryMb = memoryMb;
  if (Object.keys(resources).length) spec.resources = resources;
  if (specForm.privileged) spec.privileged = true;
  const capAdd = capabilityValues(specForm.capAdd);
  if (capAdd.length) spec.capAdd = capAdd;
  if (mounts.length) spec.mounts = mounts;
  if (!mounts.length && arrayValue(currentSpec?.mounts).length) spec.mounts = currentSpec?.mounts;
  if (arrayValue(currentSpec?.volumes).length) spec.volumes = currentSpec?.volumes;
  return toSpecYaml(spec);
}

function numericLimit(value: unknown) {
  if (value === null || value === undefined || value === '') return null;
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : null;
}

function nonNegativeNumberValue(value: unknown) {
  if (value === null || value === undefined || value === '') return null;
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : null;
}

function mountYamlValue(mount: MountForm) {
  const out: Record<string, unknown> = {
    type: mount.type,
    source: mount.source.trim(),
    target: mount.target.trim(),
    readOnly: mount.readOnly,
  };
  if (mountSupportsOwnership(mount.type)) {
    const uid = mountPermissionNumber(mount.uid);
    const gid = mountPermissionNumber(mount.gid);
    if (uid !== null) out.uid = uid;
    if (gid !== null) out.gid = gid;
  }
  if (mountSupportsMode(mount.type)) {
    if (mount.mode.trim()) out.mode = mount.mode.trim();
  }
  return out;
}

function mountPermissionNumber(value: number | null | '') {
  if (value === null || value === '') return null;
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : null;
}

function objectValue(value: unknown): Record<string, unknown> | null {
  return isPlainObject(value) ? value : null;
}

function arrayValue(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

function stringValue(value: unknown) {
  return value === null || value === undefined ? '' : String(value);
}

function numberValue(value: unknown) {
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : null;
}

function arrayItems(value: unknown, keepBlank: boolean) {
  const items = Array.isArray(value) ? value.map((item) => ({ value: stringValue(item) })) : [];
  return items.length || !keepBlank ? items : [{ value: '' }];
}

function stringListValues(items: StringListItem[]) {
  return items.map((item) => item.value.trim()).filter(Boolean);
}

function capabilityValues(items: StringListItem[]) {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const item of stringListValues(items)) {
    const value = item.toUpperCase();
    if (seen.has(value)) continue;
    seen.add(value);
    out.push(value);
  }
  return out;
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value);
}

function readInput(): ApplicationSaveDto {
  const shouldUseVisualSpec = props.embedded ? embeddedSpecMode.value === 'visual' : activeEditorTab.value !== 'yaml';
  if (shouldUseVisualSpec) applyVisualSpec();
  return {
    name: form.name,
    enabled: form.enabled,
    specYaml: form.specYaml,
    variables: Object.fromEntries(variableRows.value
      .filter((item) => item.key.trim())
      .map((item) => [item.key.trim(), item.value])),
    deploymentMode: form.deploymentMode || 'all',
    deploymentServers: form.deploymentMode === 'selected' ? [...(form.deploymentServers ?? [])] : [],
    reverseProxy: (form.reverseProxy ?? [])
      .filter((rule) => rule.domain.trim())
      .map((rule) => ({
        domain: rule.domain.trim(),
        targetType: rule.targetType === 'container' ? 'container' : 'local',
        targetPort: Number(rule.targetPort || 0),
        paths: (rule.paths ?? [])
          .filter((path) => path.path.trim())
          .map((path) => ({ path: path.path.trim(), webSocket: Boolean(path.webSocket) })),
      })),
  };
}

function requestClose(open: boolean) {
  if (open || saving.value) return;
  emit('close');
}

async function save() {
  loading.value = 'save';
  error.value = '';
  saveStep.value = t('applicationEditor.saveStepPreparing');
  try {
    const input = readInput();
    saveStep.value = t('applicationEditor.saveStepStartingSession');
    const session = await applicationsApi.beginSaveSession({
      applicationId: props.application?.id,
      save: input,
    });
    const finalPaths = new Set(files.value.map((file) => file.path));
    const filesToDelete = session.files.filter((staged) => !finalPaths.has(staged.path));
    for (let index = 0; index < filesToDelete.length; index += 1) {
      const staged = filesToDelete[index];
      saveStep.value = t('applicationEditor.saveStepDeletingFile', { current: index + 1, total: filesToDelete.length });
      await applicationsApi.deleteSaveSessionFile(session.id, { path: staged.path });
    }
    const filesToUpload = files.value.filter((file) => file.contentBase64 !== undefined);
    for (let index = 0; index < filesToUpload.length; index += 1) {
      const file = filesToUpload[index];
      saveStep.value = t('applicationEditor.saveStepUploadingFile', { current: index + 1, total: filesToUpload.length });
      await applicationsApi.uploadSaveSessionFile(session.id, {
        path: file.path,
        kind: file.kind,
        contentType: '',
        contentBase64: file.contentBase64 || '',
      });
    }
    for (let index = 0; index < pendingArchives.value.length; index += 1) {
      const archive = pendingArchives.value[index];
      saveStep.value = t('applicationEditor.saveStepUploadingArchive', { current: index + 1, total: pendingArchives.value.length });
      await applicationsApi.uploadSaveSessionArchive(session.id, {
        basePath: archive.basePath,
        kind: archive.kind,
        file: archive.file,
      });
    }
    saveStep.value = input.enabled
      ? t('applicationEditor.saveStepCommitApplying')
      : t('applicationEditor.saveStepCommitting');
    const app = await applicationsApi.commitSaveSession(session.id);
    emit('saved', app);
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('applicationEditor.saveFailed');
  } finally {
    loading.value = '';
    saveStep.value = '';
  }
}
</script>

<template>
  <component :is="editorShell" v-bind="editorShellProps" @update:model-value="requestClose">
    <v-card :class="editorCardClass">
      <v-card-title v-if="!embedded" class="app-dialog-title">
        <span class="app-dialog-title-text">{{ title }}</span>
        <AppActionButton kind="tool" :icon="closeIcon" :label="t('common.cancel')" :disabled="saving" @click="requestClose(false)" />
      </v-card-title>
      <v-card-text ref="editorBody" class="app-dialog-body">
        <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>
        <div class="editor-main" :class="{ 'editor-main--embedded': embedded }">
          <nav v-if="embedded" class="editor-section-nav" :aria-label="t('applicationEditor.sections')">
            <a
              v-for="section in editorSections"
              :key="section.id"
              class="editor-section-link"
              :class="{ 'editor-section-link--active': activeSectionId === section.id }"
              :href="`#${section.id}`"
              :aria-current="activeSectionId === section.id ? 'true' : undefined"
            >
              <v-icon :icon="section.icon" size="18" />
              <span>{{ section.title }}</span>
            </a>
          </nav>
          <div class="editor-form-flow">
          <v-tabs v-if="!embedded" v-model="activeEditorTab" density="comfortable" class="editor-tabs mb-4">
            <v-tab value="visual">{{ t('applicationEditor.general') }}</v-tab>
            <v-tab value="yaml">{{ t('applicationEditor.yaml') }}</v-tab>
          </v-tabs>

          <section id="application-editor-general" class="editor-section">
            <div class="section-title">{{ t('applicationEditor.general') }}</div>
            <div class="field-grid">
              <v-text-field v-model="form.name" :label="t('applicationEditor.applicationName')" density="compact" variant="outlined" :readonly="Boolean(application)" hide-details />
              <v-switch v-model="form.enabled" :label="t('common.enabled')" color="primary" density="compact" hide-details class="switch-field" />
            </div>
            <div v-if="embedded" class="spec-mode-panel">
              <div class="spec-mode-copy">
                <div class="spec-mode-title">{{ t('applicationEditor.specMode') }}</div>
                <div class="spec-mode-hint">{{ t('applicationEditor.specModeHint') }}</div>
              </div>
              <v-btn-toggle :model-value="embeddedSpecMode" density="compact" mandatory divided class="spec-mode-toggle" @update:model-value="onEmbeddedSpecModeUpdate">
                <v-btn value="visual" prepend-icon="mdi-view-dashboard-outline">{{ t('applicationEditor.visualMode') }}</v-btn>
                <v-btn value="yaml" prepend-icon="mdi-code-braces">{{ t('applicationEditor.yamlMode') }}</v-btn>
              </v-btn-toggle>
            </div>
          </section>

          <v-divider class="section-divider" />

          <v-window v-model="specEditorMode" class="editor-window">
            <v-window-item value="visual">
          <section id="application-editor-runtime" class="editor-section">
            <div class="section-title">{{ t('applicationEditor.runtime') }}</div>
            <div class="editor-subsection">
            <div class="section-title">{{ t('applicationEditor.image') }}</div>
            <div class="field-grid">
              <v-text-field v-model="specForm.image" :label="t('applicationEditor.image')" density="compact" variant="outlined" />
            </div>
            </div>
            <div class="editor-subsection">
            <div class="section-title">{{ t('applicationEditor.command') }}</div>
            <div v-for="(item, index) in specForm.command" :key="`command-${index}`" class="repeat-row command-row">
              <v-text-field
                v-model="item.value"
                :label="t('applicationEditor.commandItem', { index: index + 1 })"
                :hint="index === 0 ? t('applicationEditor.commandHint') : ''"
                density="compact"
                variant="outlined"
                hide-details="auto"
              />
              <AppActionButton kind="danger" icon="mdi-delete" :label="t('common.delete')" :disabled="specForm.command.length === 1" @click="removeAt(specForm.command, index)" />
            </div>
            <AppActionGroup context="section" align="start" class="repeat-actions">
              <AppActionButton icon="mdi-plus" :label="t('applicationEditor.addCommandItem')" @click="addStringItem(specForm.command)" />
            </AppActionGroup>
            </div>
            <div class="editor-subsection">
            <div class="section-title">{{ t('applicationEditor.resources') }}</div>
            <div class="field-grid">
              <v-text-field v-model.number="specForm.cpu" type="number" min="0" :label="t('applicationEditor.cpuMhz')" density="compact" variant="outlined" />
              <v-text-field v-model.number="specForm.memoryMb" type="number" min="0" :label="t('applicationEditor.memoryMb')" density="compact" variant="outlined" />
              <v-switch v-model="specForm.privileged" :label="t('applicationEditor.privilegedContainer')" color="primary" density="compact" hide-details class="switch-field" />
            </div>
            <div class="section-title mt-2">{{ t('applicationEditor.capabilities') }}</div>
            <div v-for="(item, index) in specForm.capAdd" :key="`cap-${index}`" class="repeat-row cap-row">
              <v-text-field
                v-model="item.value"
                :label="t('applicationEditor.capabilityItem', { index: index + 1 })"
                :hint="index === 0 ? t('applicationEditor.capabilityHint') : ''"
                density="compact"
                variant="outlined"
                hide-details="auto"
              />
              <AppActionButton kind="danger" icon="mdi-delete" :label="t('common.delete')" @click="removeAt(specForm.capAdd, index)" />
            </div>
            <AppActionGroup context="section" align="start" class="repeat-actions">
              <AppActionButton icon="mdi-plus" :label="t('applicationEditor.addCapability')" @click="addStringItem(specForm.capAdd)" />
            </AppActionGroup>
            </div>
            <div class="editor-subsection">
            <div class="section-title">{{ t('applicationEditor.environment') }}</div>
            <div v-for="(item, index) in specForm.env" :key="index" class="repeat-row env-row">
              <v-text-field v-model="item.key" :label="t('common.key')" density="compact" variant="outlined" hide-details />
              <v-text-field v-model="item.value" :label="t('common.value')" density="compact" variant="outlined" hide-details />
              <AppActionButton kind="danger" icon="mdi-delete" :label="t('common.delete')" @click="removeAt(specForm.env, index)" />
            </div>
            <AppActionGroup context="section" align="start" class="repeat-actions">
              <AppActionButton icon="mdi-plus" :label="t('common.addVariable')" @click="addEnv" />
            </AppActionGroup>
            </div>
          </section>

          <v-divider class="section-divider" />

          <section id="application-editor-network" class="editor-section">
            <div class="section-title">{{ t('applicationEditor.network') }}</div>
            <div class="field-grid">
              <v-select
                v-model="specForm.networkMode"
                :items="[
                  { title: t('applicationEditor.bridge'), value: 'bridge' },
                  { title: t('applicationEditor.host'), value: 'host' },
                ]"
                item-title="title"
                item-value="value"
                :label="t('applicationEditor.networkMode')"
                density="compact"
                variant="outlined"
              />
            </div>

            <template v-if="specForm.networkMode === 'bridge'">
              <div class="section-title mt-2">{{ t('applicationEditor.ports') }}</div>
              <AppActionGroup context="section" align="start" class="network-actions">
                <AppActionButton icon="mdi-plus" :label="t('common.addPort')" @click="addPort" />
              </AppActionGroup>
              <div v-for="(port, index) in specForm.ports" :key="index" class="repeat-row ports-row">
                <v-text-field v-model="port.label" :label="t('applicationEditor.label')" density="compact" variant="outlined" hide-details />
                <span class="network-target-name">{{ specForm.name || t('applicationEditor.appTargetFallback') }}:</span>
                <v-text-field v-model.number="port.to" type="number" :label="t('applicationEditor.containerPort')" density="compact" variant="outlined" hide-details />
                <v-icon icon="mdi-arrow-right" size="20" class="network-arrow" />
                <span class="network-target-name">{{ t('applicationEditor.nodeTarget') }}:</span>
                <v-text-field v-model.number="port.static" type="number" :label="t('applicationEditor.hostPort')" density="compact" variant="outlined" hide-details />
                <AppActionButton kind="danger" icon="mdi-delete" :label="t('common.delete')" @click="removeAt(specForm.ports, index)" />
              </div>
            </template>

          </section>

          <v-divider class="section-divider" />

          <section id="application-editor-mounts" class="editor-section">
            <div class="section-title">{{ t('applicationEditor.mounts') }}</div>
            <div v-if="specForm.mounts.length" class="mount-list">
              <div v-for="(mount, index) in specForm.mounts" :key="index" class="mount-list-row">
                <button type="button" class="mount-list-row__main" @click="openMountDialog(index)">
                  <span class="mount-type text-truncate">{{ mountTypeLabel(mount) }}</span>
                  <span class="mount-source text-truncate">{{ mountSourceLabel(mount) }}</span>
                  <v-icon icon="mdi-arrow-right" size="16" class="mount-arrow" />
                  <span class="mount-target text-truncate">{{ mountTargetLabel(mount) }}</span>
                  <v-chip v-if="mount.readOnly" size="small" variant="tonal" label>{{ t('applicationEditor.readOnly') }}</v-chip>
                </button>
                <AppActionGroup context="table" class="mount-list-row__actions">
                  <AppActionButton icon="mdi-pencil" :label="t('common.edit')" @click="openMountDialog(index)" />
                  <AppActionButton kind="danger" icon="mdi-delete" :label="t('common.delete')" @click="removeMount(index)" />
                </AppActionGroup>
              </div>
            </div>
            <AppActionGroup context="section" align="start" class="mount-actions">
              <AppActionButton icon="mdi-plus" :label="t('applicationEditor.dockerVolume')" @click="addMount('volume')" />
              <AppActionButton icon="mdi-folder" :label="t('applicationEditor.hostPath')" @click="addMount('host')" />
              <AppActionButton icon="mdi-file" :label="t('applicationEditor.appFile')" @click="addMount('file')" />
              <AppActionButton icon="mdi-shield-key" :label="t('applicationEditor.panelFile')" @click="addMount('panel_file')" />
              <AppActionButton icon="mdi-database" :label="t('applicationEditor.persistent')" @click="addMount('persistent')" />
            </AppActionGroup>
          </section>

            </v-window-item>

            <v-window-item value="yaml" class="editor-yaml-pane">
          <section :id="embedded ? 'application-editor-yaml' : undefined" class="editor-section editor-yaml-section">
            <div class="section-title">{{ t('applicationEditor.yaml') }}</div>
            <div class="yaml-code-editor">
              <div class="yaml-code-toolbar">
                <span>{{ t('applicationEditor.yamlSpec') }}</span>
                <span>{{ t('applicationEditor.lineCount', { count: yamlLineNumbers.length }) }}</span>
              </div>
              <div class="yaml-code-body">
                <pre ref="yamlGutter" class="yaml-code-gutter" aria-hidden="true"><span v-for="line in yamlLineNumbers" :key="line">{{ line }}</span></pre>
                <textarea
                  ref="yamlTextarea"
                  v-model="form.specYaml"
                  class="yaml-code-textarea"
                  spellcheck="false"
                  autocomplete="off"
                  autocapitalize="off"
                  @focus="prepareEmbeddedYamlEdit"
                  @input="markYamlEdited"
                  @scroll="syncYamlEditorScroll"
                />
              </div>
            </div>
            <v-menu>
              <template #activator="{ props: menuProps }">
                <AppActionButton v-bind="menuProps" icon="mdi-code-braces" :label="t('applicationEditor.insertVariable')" class="yaml-insert-action" @click="prepareEmbeddedYamlEdit" />
              </template>
              <v-list density="compact">
                <v-list-item v-for="item in variableItems('spec')" :key="item.title" :title="item.title" @click="insertVariable('spec', item.value)" />
              </v-list>
            </v-menu>
          </section>
            </v-window-item>
          </v-window>

          <v-divider class="section-divider" />

          <section id="application-editor-deployment" class="editor-section">
            <div class="section-title">{{ t('applicationEditor.deploymentTargets') }}</div>
            <div class="placement-row">
              <v-select
                v-model="form.deploymentMode"
                :items="[
                  { title: t('applicationEditor.allServers'), value: 'all' },
                  { title: t('applicationEditor.selectedServers'), value: 'selected' },
                ]"
                :label="t('applicationEditor.mode')"
                density="compact"
                variant="outlined"
                hide-details
              />
              <v-select
                v-if="form.deploymentMode === 'selected'"
                v-model="form.deploymentServers"
                :items="serverOptions"
                :label="t('applicationEditor.servers')"
                density="compact"
                variant="outlined"
                multiple
                chips
                closable-chips
                hide-details
              />
            </div>
          </section>

          <v-divider class="section-divider" />

          <section id="application-editor-proxy" class="editor-section">
            <div class="section-title">{{ t('applicationEditor.reverseProxy') }}</div>
            <AppActionGroup context="section" align="start" class="proxy-actions">
              <AppActionButton icon="mdi-plus" :label="t('common.addProxyRule')" @click="addProxyRule" />
            </AppActionGroup>
            <div v-if="form.reverseProxy?.length" class="proxy-rule-list">
              <div
                v-for="(rule, ruleIndex) in form.reverseProxy"
                :key="`proxy-row-${ruleIndex}`"
                class="proxy-rule-list-row"
              >
                <button type="button" class="proxy-rule-list-row__main" @click="openProxyRuleDialog(ruleIndex)">
                  <span class="proxy-rule-domain text-truncate">{{ rule.domain || t('applicationEditor.domain') }}</span>
                  <span class="proxy-rule-target text-truncate">{{ proxyTargetName(rule) }}:{{ rule.targetPort || 80 }}</span>
                  <v-chip size="small" variant="tonal" label>{{ rule.paths.length }} {{ t('common.path') }}</v-chip>
                </button>
                <AppActionGroup context="table" class="proxy-rule-list-row__actions">
                  <AppActionButton icon="mdi-pencil-outline" :label="t('common.edit')" @click="openProxyRuleDialog(ruleIndex)" />
                  <AppActionButton kind="danger" icon="mdi-delete-outline" :label="t('common.delete')" @click="removeProxyRule(ruleIndex)" />
                </AppActionGroup>
              </div>
            </div>
          </section>

          <v-divider class="section-divider" />

          <section id="application-editor-files" class="editor-section">
            <div class="section-title">{{ t('applicationEditor.files') }}</div>
            <div class="editor-subsection">
            <div class="section-title">{{ t('applicationEditor.variables') }}</div>
            <div v-for="(item, index) in variableRows" :key="index" class="repeat-row variable-row">
              <v-text-field v-model="item.key" :label="t('common.key')" density="compact" variant="outlined" hide-details />
              <v-text-field v-model="item.value" :label="t('common.value')" density="compact" variant="outlined" hide-details />
              <AppActionButton kind="danger" icon="mdi-delete" :label="t('common.delete')" @click="removeAt(variableRows, index)" />
            </div>
            <AppActionGroup context="section" align="start" class="repeat-actions mb-4">
              <AppActionButton icon="mdi-plus" :label="t('common.addVariable')" @click="addVariable" />
            </AppActionGroup>
            </div>

            <div class="editor-subsection">
            <div class="files-heading">
              <div class="section-title">{{ t('applicationEditor.applicationFiles') }}</div>
              <AppActionGroup context="section">
                <AppActionButton icon="mdi-file-code-outline" :label="t('applicationEditor.addTemplateFile')" @click="openFileDialog('template')" />
                <AppActionButton icon="mdi-file-upload-outline" :label="t('applicationEditor.addBinaryFile')" @click="openFileDialog('binary')" />
              </AppActionGroup>
            </div>
            <div v-if="pendingArchives.length" class="pending-archives">
              <div v-for="archive in pendingArchives" :key="archive.id" class="pending-archive">
                <div class="min-width-0">
                  <strong class="text-truncate">{{ archive.basePath }}</strong>
                  <div class="text-caption text-medium-emphasis text-truncate">
                    {{ archive.file.name }} / {{ translateApplicationFileKind(archive.kind) }}
                    <span v-if="archive.replacedFiles.length"> · {{ t('applicationEditor.replacesFileCount', { count: archive.replacedFiles.length }) }}</span>
                  </div>
                </div>
                <AppActionButton kind="danger" icon="mdi-delete" :label="t('common.delete')" @click="removeArchive(archive)" />
              </div>
            </div>
            <v-table density="compact" class="mt-3">
              <thead><tr><th>{{ t('common.path') }}</th><th>{{ t('applicationEditor.kind') }}</th><th>{{ t('common.size') }}</th><th>{{ t('common.sha256') }}</th><th class="text-right">{{ t('common.actions') }}</th></tr></thead>
              <tbody>
                <tr v-for="file in pagedFiles" :key="`${file.id}:${file.path}`">
                  <td class="mono text-truncate">{{ file.path }}</td>
                  <td><v-chip size="small" variant="tonal" label>{{ translateApplicationFileKind(file.kind) }}</v-chip></td>
                  <td>{{ sizeLabel(file.size) }}</td>
                  <td class="mono text-truncate hash-cell">{{ file.sha256 }}</td>
                  <td class="text-right file-row-actions">
                    <AppActionGroup context="table">
                      <AppActionButton v-if="file.kind === 'template'" icon="mdi-pencil" :label="t('common.edit')" :loading="loadingFileId === file.id" @click="editTemplateFile(file)" />
                      <AppActionButton v-else-if="file.kind === 'archive'" icon="mdi-folder-zip-outline" :label="t('applicationEditor.replaceFolderArchive')" @click="replaceArchiveFile(file)" />
                      <AppActionButton v-else icon="mdi-upload" :label="t('applicationEditor.replaceBinaryFile')" @click="replaceBinaryFile(file)" />
                      <AppActionButton kind="danger" icon="mdi-delete" :label="t('common.delete')" @click="removeFile(file)" />
                    </AppActionGroup>
                  </td>
                </tr>
                <tr v-if="files.length === 0"><td colspan="5" class="text-center text-medium-emphasis py-4">{{ t('applicationEditor.noFiles') }}</td></tr>
              </tbody>
            </v-table>
            <AppPagination v-model:page="filePage" v-model:page-size="filePageSize" :total="fileTotal" />
            </div>
          </section>
          </div>
        </div>
      </v-card-text>
      <v-card-actions class="app-dialog-actions">
        <AppActionGroup context="dialog">
          <AppActionButton kind="plain" :label="t('common.cancel')" :disabled="saving" @click="requestClose(false)" />
          <AppActionButton kind="primary" :label="form.enabled ? t('common.saveAndDeploy') : t('common.save')" :loading="saving" :disabled="saving" @click="save()" />
        </AppActionGroup>
      </v-card-actions>
      <v-dialog v-model="fileDialog" width="720" :persistent="saving">
        <v-card class="app-dialog-card">
          <v-card-title class="app-dialog-title">
            <span class="app-dialog-title-text">{{ t('applicationEditor.applicationFiles') }}</span>
            <AppActionButton kind="tool" icon="mdi-close" :label="t('common.cancel')" @click="closeFileDialog" />
          </v-card-title>
          <v-card-text class="app-dialog-body">
            <div class="file-form">
              <v-select
                v-if="!fileKindLocked"
                v-model="fileForm.kind"
                :items="[
                  { title: t('applicationEditor.template'), value: 'template' },
                  { title: t('applicationEditor.binary'), value: 'binary' },
                ]"
                item-title="title"
                item-value="value"
                :label="t('applicationEditor.kind')"
                density="compact"
                variant="outlined"
                hide-details
              />
              <v-text-field
                v-else
                :model-value="translateApplicationFileKind(fileForm.kind)"
                :label="t('applicationEditor.kind')"
                density="compact"
                variant="outlined"
                readonly
                hide-details
              />
              <v-text-field v-model="fileForm.path" :label="t('applicationEditor.workspacePath')" density="compact" variant="outlined" hide-details />
              <v-slide-y-transition>
                <div v-if="fileForm.kind === 'binary'" class="file-mode-group span-all">
                  <v-btn-toggle v-model="fileForm.mode" density="compact" mandatory divided class="file-mode-toggle">
                    <v-btn value="single" prepend-icon="mdi-file-outline">{{ t('applicationEditor.singleFile') }}</v-btn>
                    <v-btn value="archive" prepend-icon="mdi-folder-zip-outline">{{ t('applicationEditor.folderArchive') }}</v-btn>
                  </v-btn-toggle>
                </div>
              </v-slide-y-transition>
              <v-textarea
                v-if="fileForm.kind === 'template'"
                ref="templateTextarea"
                v-model="fileForm.template"
                :label="t('applicationEditor.template')"
                rows="9"
                variant="outlined"
                spellcheck="false"
                class="mono-input span-all"
              />
              <v-menu v-if="fileForm.kind === 'template'">
                <template #activator="{ props: menuProps }">
                  <AppActionButton v-bind="menuProps" icon="mdi-code-braces" :label="t('applicationEditor.insertVariable')" class="file-secondary-action" />
                </template>
                <v-list density="compact">
                  <v-list-item v-for="item in variableItems('template')" :key="item.title" :title="item.title" @click="insertVariable('template', item.value)" />
                </v-list>
              </v-menu>
              <v-file-input
                v-else
                v-model="fileForm.file"
                :label="fileForm.mode === 'archive' ? t('applicationEditor.folderArchiveFile') : t('applicationEditor.binaryFile')"
                density="compact"
                variant="outlined"
                hide-details
                class="span-all"
              />
            </div>
          </v-card-text>
          <v-card-actions class="app-dialog-actions">
            <AppActionGroup context="dialog">
              <AppActionButton kind="plain" :label="t('common.cancel')" @click="closeFileDialog" />
              <AppActionButton kind="primary" :label="fileSubmitLabel" :disabled="!fileForm.path || (fileForm.mode === 'archive' && !selectedFile()) || (fileForm.mode === 'single' && fileForm.kind === 'binary' && !selectedFile())" @click="addFile" />
            </AppActionGroup>
          </v-card-actions>
        </v-card>
      </v-dialog>
      <v-dialog v-model="proxyRuleDialog.open" width="min(760px, calc(100vw - 32px))" :persistent="saving">
        <v-card v-if="currentProxyRule" class="app-dialog-card">
          <v-card-title class="app-dialog-title">
            <span class="app-dialog-title-text">{{ t('applicationEditor.reverseProxy') }}</span>
            <AppActionButton kind="tool" icon="mdi-close" :label="t('common.cancel')" @click="closeProxyRuleDialog" />
          </v-card-title>
          <v-card-text class="app-dialog-body">
            <div class="proxy-rule-dialog-form">
              <v-text-field v-model="currentProxyRule.domain" :label="t('applicationEditor.domain')" density="comfortable" variant="outlined" hide-details="auto" />
              <v-select
                v-model="currentProxyRule.targetType"
                :items="proxyTargetTypeOptions"
                :label="t('applicationEditor.targetDestination')"
                density="comfortable"
                variant="outlined"
                hide-details="auto"
              />
              <v-text-field v-model.number="currentProxyRule.targetPort" type="number" min="1" max="65535" :label="t('applicationEditor.target')" density="comfortable" variant="outlined" hide-details="auto" />
            </div>
            <div class="proxy-dialog-paths">
              <div class="proxy-dialog-paths__heading">
                <div class="section-title">{{ t('applicationEditor.path') }}</div>
                <AppActionButton icon="mdi-plus" :label="t('common.addPath')" @click="addProxyPath(currentProxyRule)" />
              </div>
              <div v-for="(path, pathIndex) in currentProxyRule.paths" :key="pathIndex" class="repeat-row proxy-path-row">
                <v-text-field v-model="path.path" :label="t('applicationEditor.path')" density="compact" variant="outlined" hide-details />
                <v-checkbox v-model="path.webSocket" :label="t('applicationEditor.websocket')" density="compact" hide-details />
                <AppActionButton kind="danger" icon="mdi-delete" :label="t('common.delete')" @click="removeAt(currentProxyRule.paths, pathIndex)" />
              </div>
            </div>
          </v-card-text>
          <v-card-actions class="app-dialog-actions">
            <AppActionGroup context="dialog">
              <AppActionButton kind="plain" :label="t('common.cancel')" @click="closeProxyRuleDialog" />
              <AppActionButton kind="primary" :label="t('common.save')" @click="closeProxyRuleDialog" />
            </AppActionGroup>
          </v-card-actions>
        </v-card>
      </v-dialog>
      <v-dialog v-model="mountDialog.open" width="min(760px, calc(100vw - 32px))" :persistent="saving">
        <v-card v-if="currentMount" class="app-dialog-card">
          <v-card-title class="app-dialog-title">
            <span class="app-dialog-title-text">{{ t('applicationEditor.mounts') }}</span>
            <AppActionButton kind="tool" icon="mdi-close" :label="t('common.cancel')" @click="closeMountDialog" />
          </v-card-title>
          <v-card-text class="app-dialog-body">
            <div class="mount-dialog-form">
              <v-select
                v-model="currentMount.type"
                :items="mountTypeOptions"
                :label="t('applicationEditor.sourceType')"
                density="comfortable"
                variant="outlined"
                hide-details="auto"
                @update:model-value="updateMountType(currentMount)"
              />
              <v-combobox
                v-if="currentMount.type === 'file'"
                v-model="currentMount.source"
                :items="files.map(file => file.path)"
                :label="t('applicationEditor.workspaceFile')"
                density="comfortable"
                variant="outlined"
                hide-details="auto"
              />
              <v-select
                v-else-if="currentMount.type === 'panel_file'"
                v-model="currentMount.source"
                :items="panelFiles"
                item-value="source"
                :item-title="item => `${item.name} / ${item.kind}`"
                :label="t('applicationEditor.panelFile')"
                density="comfortable"
                variant="outlined"
                hide-details="auto"
              />
              <v-text-field
                v-else-if="currentMount.type === 'persistent'"
                v-model="currentMount.source"
                :label="t('applicationEditor.persistentSubpath')"
                density="comfortable"
                variant="outlined"
                hide-details="auto"
              />
              <v-text-field
                v-else
                v-model="currentMount.source"
                :label="currentMount.type === 'host' ? t('applicationEditor.hostAbsolutePath') : t('applicationEditor.volumeName')"
                density="comfortable"
                variant="outlined"
                hide-details="auto"
              />
              <v-text-field v-model="currentMount.target" :label="t('applicationEditor.containerPath')" density="comfortable" variant="outlined" hide-details="auto" />
              <v-checkbox v-model="currentMount.readOnly" :label="t('applicationEditor.readOnlyMount')" density="comfortable" hide-details class="span-all" />
              <v-text-field v-if="mountSupportsOwnership(currentMount.type)" v-model.number="currentMount.uid" type="number" min="0" :label="t('applicationEditor.mountUid')" density="comfortable" variant="outlined" hide-details="auto" />
              <v-text-field v-if="mountSupportsOwnership(currentMount.type)" v-model.number="currentMount.gid" type="number" min="0" :label="t('applicationEditor.mountGid')" density="comfortable" variant="outlined" hide-details="auto" />
              <v-checkbox
                v-if="mountSupportsExecutable(currentMount.type)"
                :model-value="fileMountExecutable(currentMount)"
                :label="t('applicationEditor.executableFile')"
                density="comfortable"
                hide-details
                @update:model-value="setFileMountExecutable(currentMount, $event)"
              />
              <v-text-field v-else-if="currentMount.type === 'persistent'" v-model="currentMount.mode" :label="t('applicationEditor.mountMode')" placeholder="0755" density="comfortable" variant="outlined" hide-details="auto" />
            </div>
          </v-card-text>
          <v-card-actions class="app-dialog-actions">
            <AppActionGroup context="dialog">
              <AppActionButton kind="plain" :label="t('common.cancel')" @click="closeMountDialog" />
              <AppActionButton kind="primary" :label="t('common.save')" @click="closeMountDialog" />
            </AppActionGroup>
          </v-card-actions>
        </v-card>
      </v-dialog>
      <v-overlay :model-value="saving" contained persistent scrim="surface" class="editor-save-overlay">
        <div class="editor-save-progress" role="status" aria-live="polite">
          <v-progress-circular indeterminate color="primary" :size="42" :width="4" />
          <div class="editor-save-title">{{ t('applicationEditor.saveInProgress') }}</div>
          <div class="editor-save-step">{{ saveStep }}</div>
        </div>
      </v-overlay>
    </v-card>
  </component>
</template>

<style scoped>
.editor-card { display: flex; flex-direction: column; max-height: calc(100dvh - 24px); position: relative; overflow: hidden; }
.embedded-editor-shell { display: flex; flex: 1 1 auto; min-height: 0; min-width: 0; }
.editor-card--embedded {
  width: 100%;
  max-height: none;
  min-height: 0;
  border-color: color-mix(in srgb, var(--lp-border), transparent 28%) !important;
  background:
    radial-gradient(circle at 18% 0%, color-mix(in srgb, rgb(var(--v-theme-primary)), transparent 88%), transparent 26rem),
    linear-gradient(180deg, color-mix(in srgb, var(--lp-surface), transparent 2%), color-mix(in srgb, var(--lp-surface-container), transparent 14%)) !important;
  box-shadow: 0 18px 48px color-mix(in srgb, var(--lp-background), transparent 24%) !important;
}
.editor-card--embedded :deep(.app-dialog-body) {
  flex: 1 1 auto;
  min-height: 0;
  max-height: none;
  overflow: auto;
  scroll-behavior: smooth;
  padding: 18px !important;
}
.editor-card--embedded :deep(.app-dialog-actions) {
  flex: 0 0 auto;
  border-top: 1px solid color-mix(in srgb, var(--lp-border), transparent 18%);
  background: color-mix(in srgb, var(--lp-surface), transparent 4%);
  box-shadow: 0 -12px 28px color-mix(in srgb, var(--lp-background), transparent 38%);
}
.editor-main { min-width: 0; }
.editor-main--embedded {
  display: grid;
  grid-template-columns: 208px minmax(0, 1fr);
  gap: 18px;
  align-items: start;
  justify-content: stretch;
}
.editor-section-nav {
  position: sticky;
  top: 4px;
  display: grid;
  gap: 6px;
  align-self: start;
  max-height: calc(100dvh - 190px);
  overflow: auto;
  padding: 10px;
  border: 1px solid color-mix(in srgb, var(--lp-border), transparent 16%);
  border-radius: var(--lp-radius-md);
  background: color-mix(in srgb, var(--lp-surface), transparent 8%);
  box-shadow: var(--lp-shadow-sm);
  backdrop-filter: blur(10px);
}
.editor-section-link {
  display: grid;
  grid-template-columns: 24px minmax(0, 1fr);
  gap: 9px;
  align-items: center;
  min-height: 40px;
  padding: 8px 10px;
  border: 1px solid transparent;
  border-radius: var(--lp-radius-sm);
  color: var(--lp-text-muted);
  text-decoration: none;
  transition: background-color 180ms ease, border-color 180ms ease, color 180ms ease, transform 180ms ease;
}
.editor-section-link :deep(.v-icon) {
  opacity: 0.8;
}
.editor-section-link:hover,
.editor-section-link:focus-visible {
  background: color-mix(in srgb, rgb(var(--v-theme-primary)), transparent 90%);
  color: rgb(var(--v-theme-on-surface));
  outline: none;
}
.editor-section-link--active {
  border-color: color-mix(in srgb, rgb(var(--v-theme-primary)), transparent 58%);
  background:
    linear-gradient(90deg, color-mix(in srgb, rgb(var(--v-theme-primary)), transparent 82%), transparent 120%),
    color-mix(in srgb, var(--lp-surface-container), transparent 18%);
  color: rgb(var(--v-theme-primary));
  font-weight: 700;
  box-shadow: inset 3px 0 0 rgb(var(--v-theme-primary));
}
.editor-form-flow {
  min-width: 0;
  width: 100%;
}
.editor-section { min-width: 0; }
.editor-main--embedded .editor-form-flow {
  display: grid;
  gap: 22px;
  align-content: start;
}
.editor-main--embedded .editor-window :deep(.v-window-item--active) {
  display: grid;
  gap: 22px;
  align-content: start;
}
.editor-main--embedded .editor-section {
  scroll-margin-top: 18px;
  display: grid;
  gap: 14px;
  position: relative;
  overflow: visible;
  padding: 0 0 22px;
  border-bottom: 1px solid color-mix(in srgb, var(--lp-border), transparent 34%);
}
.editor-main--embedded .editor-section::before {
  content: none;
}
.editor-main--embedded .editor-section:hover {
  box-shadow: none;
}
.editor-main--embedded .editor-form-flow > .editor-section:last-child,
.editor-main--embedded .editor-window :deep(.v-window-item--active) > .editor-section:last-child {
  padding-bottom: 0;
  border-bottom: 0;
}
.editor-main--embedded .editor-section > .v-btn {
  justify-self: start;
}
.editor-main--embedded .editor-section > .v-btn:not(.v-btn--icon) {
  min-width: 0;
}
.editor-subsection {
  display: grid;
  gap: 10px;
  min-width: 0;
}
.editor-main--embedded .editor-subsection {
  padding: 0 0 18px;
  border-bottom: 1px solid color-mix(in srgb, var(--lp-border), transparent 42%);
}
.editor-main--embedded .editor-subsection:last-child {
  padding-bottom: 0;
  border-bottom: 0;
}
.editor-main--embedded .editor-subsection > .v-btn {
  justify-self: start;
  min-width: 0;
}
.editor-main--embedded .section-divider {
  display: none;
}
.editor-main--embedded .section-title {
  margin-top: 0;
  margin-bottom: 2px;
  color: var(--lp-text);
  font-size: 1rem;
  font-weight: 750;
  text-transform: none;
}
.editor-main--embedded .editor-section > .section-title:not(:first-child) {
  margin-top: 2px;
  color: var(--lp-text-muted);
  font-size: 0.84rem;
  font-weight: 700;
}
.editor-main--embedded .editor-subsection > .section-title {
  color: var(--lp-text-muted);
  font-size: 0.84rem;
  font-weight: 700;
}
.spec-mode-panel {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  min-width: 0;
  margin-top: 4px;
  padding: 12px;
  border: 1px solid color-mix(in srgb, var(--lp-border), transparent 20%);
  border-radius: var(--lp-radius-sm);
  background: color-mix(in srgb, var(--lp-surface-container), transparent 34%);
}
.spec-mode-copy {
  display: grid;
  gap: 2px;
  min-width: 0;
}
.spec-mode-title {
  color: var(--lp-text);
  font-size: 0.88rem;
  font-weight: 750;
}
.spec-mode-hint {
  color: var(--lp-text-muted);
  font-size: 0.78rem;
  line-height: 1.4;
}
.spec-mode-toggle {
  flex: 0 0 auto;
  max-width: 100%;
}
.spec-mode-toggle :deep(.v-btn) {
  min-width: 0;
}
.section-divider { margin: 18px 0; }
.field-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; align-items: start; }
.switch-field { min-height: 40px; }
.span-2 { grid-column: span 2; }
.span-all { grid-column: 1 / -1; }
.section-title { margin: 14px 0 8px; }
.editor-section > .section-title:first-child { margin-top: 0; }
.repeat-row { display: grid; gap: 8px; align-items: center; margin-bottom: 8px; min-width: 0; }
.editor-main--embedded .repeat-row,
.editor-main--embedded .mount-list-row,
.editor-main--embedded .pending-archive {
  border-color: color-mix(in srgb, var(--lp-border), transparent 20%);
}
.placement-row { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; align-items: start; }
.ports-row { grid-template-columns: minmax(120px, 1fr) auto minmax(96px, 0.6fr) auto auto minmax(96px, 0.6fr) auto; }
.command-row { grid-template-columns: minmax(0, 1fr) auto; }
.cap-row { grid-template-columns: minmax(0, 1fr) auto; }
.env-row { grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto; }
.mount-list {
  display: grid;
  gap: 8px;
}
.mount-list-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 10px;
  align-items: center;
  min-width: 0;
  padding: 8px 10px;
  border: 1px solid color-mix(in srgb, var(--lp-border), transparent 28%);
  border-radius: var(--lp-radius-sm);
  background: color-mix(in srgb, var(--lp-surface), transparent 38%);
}
.mount-list-row__main {
  display: grid;
  grid-template-columns: minmax(120px, 0.45fr) minmax(160px, 1fr) auto minmax(140px, 0.8fr) auto;
  gap: 10px;
  align-items: center;
  min-width: 0;
  border: 0;
  background: transparent;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
}
.mount-list-row__main:focus-visible {
  outline: 2px solid color-mix(in srgb, rgb(var(--v-theme-primary)), transparent 42%);
  outline-offset: 3px;
  border-radius: var(--lp-radius-sm);
}
.mount-type {
  min-width: 0;
  font-weight: 760;
}
.mount-source,
.mount-target,
.mount-arrow {
  min-width: 0;
  color: var(--lp-text-muted);
  font-size: 0.82rem;
}
.mount-arrow {
  justify-self: center;
}
.mount-actions { margin-bottom: 12px; }
.mount-dialog-form {
  display: grid;
  grid-template-columns: minmax(160px, 0.58fr) minmax(0, 1fr);
  gap: 10px;
  align-items: start;
}
.variable-row { grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto; }
.proxy-rule-list {
  display: grid;
  gap: 8px;
}
.proxy-rule-list-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 10px;
  align-items: center;
  min-width: 0;
  padding: 8px 10px;
  border: 1px solid color-mix(in srgb, var(--lp-border), transparent 28%);
  border-radius: var(--lp-radius-sm);
  background: color-mix(in srgb, var(--lp-surface), transparent 38%);
}
.proxy-rule-list-row__main {
  display: grid;
  grid-template-columns: minmax(150px, 1fr) minmax(120px, 0.45fr) auto;
  gap: 10px;
  align-items: center;
  min-width: 0;
  border: 0;
  background: transparent;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
}
.proxy-rule-list-row__main:focus-visible {
  outline: 2px solid color-mix(in srgb, rgb(var(--v-theme-primary)), transparent 42%);
  outline-offset: 3px;
  border-radius: var(--lp-radius-sm);
}
.proxy-rule-domain {
  min-width: 0;
  font-weight: 760;
}
.proxy-rule-target {
  min-width: 0;
  color: var(--lp-text-muted);
  font-size: 0.82rem;
}
.proxy-rule-dialog-form {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(180px, 0.7fr) minmax(140px, 0.45fr);
  gap: 10px;
  align-items: start;
}
.proxy-dialog-paths {
  display: grid;
  gap: 10px;
  margin-top: 16px;
}
.proxy-dialog-paths__heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}
.network-actions,
.proxy-actions { margin-bottom: 10px; }
.editor-main--embedded .network-actions,
.editor-main--embedded .proxy-actions,
.editor-main--embedded .mount-actions {
  margin-bottom: 0;
}
.network-arrow,
.network-target-name { color: var(--lp-text-muted); white-space: nowrap; }
.network-arrow { justify-self: center; }
.network-target-name { font-size: 0.82rem; }
.proxy-arrow,
.proxy-target-name { color: var(--lp-text-muted); white-space: nowrap; }
.proxy-arrow { justify-self: center; }
.proxy-target-name { font-size: 0.82rem; }
.proxy-path-row {
  grid-template-columns: minmax(0, 1fr) 130px auto;
  padding: 8px 0 0;
  border-top: 1px solid color-mix(in srgb, var(--lp-border), transparent 48%);
}
.file-form {
  display: grid;
  grid-template-columns: minmax(170px, 0.68fr) minmax(0, 1.32fr);
  gap: 10px;
  align-items: start;
  padding: 12px;
  border: 1px solid var(--lp-border);
  border-radius: var(--lp-radius-sm);
  background: color-mix(in srgb, var(--lp-surface-container), transparent 36%);
}
.file-mode-group {
  display: flex;
  justify-content: flex-start;
  min-width: 0;
}
.file-mode-toggle {
  max-width: 100%;
}
.file-mode-toggle :deep(.v-btn) {
  min-width: 0;
}
.file-secondary-action,
.file-primary-action {
  justify-self: start;
}
.file-row-actions {
  white-space: nowrap;
}
.editor-yaml-section {
  background: transparent !important;
}
.yaml-insert-action {
  justify-self: start;
}
.yaml-code-editor {
  min-width: 0;
  overflow: hidden;
  border: 1px solid color-mix(in srgb, var(--lp-border), transparent 10%);
  border-radius: var(--lp-radius-sm);
  background: color-mix(in srgb, var(--lp-log-background), transparent 4%);
  box-shadow: inset 0 1px 0 color-mix(in srgb, white, transparent 92%);
}
.yaml-code-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-height: 36px;
  padding: 8px 12px;
  border-bottom: 1px solid color-mix(in srgb, var(--lp-border), transparent 18%);
  color: color-mix(in srgb, var(--lp-log-text), transparent 16%);
  font-family: ui-monospace, SFMono-Regular, Consolas, "Liberation Mono", monospace;
  font-size: 0.76rem;
}
.yaml-code-body {
  display: grid;
  grid-template-columns: 52px minmax(0, 1fr);
  min-height: 430px;
  max-height: min(58dvh, 640px);
  overflow: hidden;
}
.yaml-code-gutter {
  display: block;
  min-height: 100%;
  margin: 0;
  padding: 14px 10px 14px 0;
  overflow: hidden;
  border-right: 1px solid color-mix(in srgb, var(--lp-border), transparent 26%);
  background: color-mix(in srgb, var(--lp-log-background), black 8%);
  color: color-mix(in srgb, var(--lp-log-text), transparent 54%);
  font-family: ui-monospace, SFMono-Regular, Consolas, "Liberation Mono", monospace;
  font-size: 0.82rem;
  line-height: 1.62;
  text-align: right;
  user-select: none;
}
.yaml-code-gutter span {
  display: block;
  height: 1.62em;
}
.yaml-code-textarea {
  width: 100%;
  min-width: 0;
  min-height: 430px;
  max-height: min(58dvh, 640px);
  padding: 14px 16px;
  border: 0;
  outline: none;
  resize: none;
  overflow: auto;
  background: transparent;
  color: var(--lp-log-text);
  caret-color: rgb(var(--v-theme-primary));
  font-family: ui-monospace, SFMono-Regular, Consolas, "Liberation Mono", monospace;
  font-size: 0.82rem;
  line-height: 1.62;
  tab-size: 2;
  white-space: pre;
}
.yaml-code-textarea:focus {
  box-shadow: inset 0 0 0 2px color-mix(in srgb, rgb(var(--v-theme-primary)), transparent 58%);
}
.files-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-top: 10px;
}
.files-heading .section-title {
  margin-top: 0;
}
.pending-archives { display: grid; gap: 8px; margin-top: 10px; }
.pending-archive { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 8px; align-items: center; padding: 8px 10px; border: 1px solid var(--lp-border); border-radius: var(--lp-radius-sm); background: color-mix(in srgb, var(--lp-surface), transparent 34%); }
.min-width-0 { min-width: 0; }
.editor-save-overlay :deep(.v-overlay__scrim) { opacity: 0.86; }
.editor-save-overlay :deep(.v-overlay__content) {
  width: 100%;
  height: 100%;
  display: grid;
  place-items: center;
}
.editor-save-progress {
  display: grid;
  justify-items: center;
  gap: 12px;
  max-width: min(360px, calc(100% - 48px));
  padding: 24px;
  border: 1px solid var(--lp-border);
  border-radius: 8px;
  background: rgb(var(--v-theme-surface));
  box-shadow: var(--lp-shadow-2);
  text-align: center;
}
.editor-save-title {
  font-weight: 700;
}
.editor-save-step {
  color: var(--lp-text-muted);
}
.repeat-row > .v-btn {
  justify-self: end;
}
.mono, .mono-input :deep(textarea) { font-size: 0.82rem; }
.hash-cell { max-width: 180px; }
@media (max-width: 1180px) {
  .editor-main--embedded {
    grid-template-columns: 1fr;
  }
  .editor-section-nav {
    position: static;
    grid-auto-flow: column;
    grid-auto-columns: max-content;
    max-height: none;
    overflow-x: auto;
    align-items: center;
  }
  .field-grid,
  .spec-mode-panel,
  .mount-list-row,
  .mount-list-row__main,
  .mount-dialog-form,
  .ports-row,
  .command-row,
  .cap-row,
  .env-row,
  .variable-row,
  .proxy-rule-list-row,
  .proxy-rule-list-row__main,
  .proxy-rule-dialog-form,
  .proxy-path-row,
  .file-form,
  .placement-row {
    grid-template-columns: 1fr;
  }
  .spec-mode-panel {
    align-items: stretch;
    flex-direction: column;
  }
}

@media (max-width: 760px) {
  .editor-card--embedded :deep(.app-dialog-body) {
    padding: 16px !important;
  }
  .editor-card--embedded :deep(.app-dialog-actions) {
    flex-wrap: wrap;
  }
  .files-heading {
    align-items: stretch;
    flex-direction: column;
  }
  .mount-actions .v-btn,
  .network-actions .v-btn,
  .proxy-actions .v-btn {
    flex: 1 1 100%;
  }
}

</style>
