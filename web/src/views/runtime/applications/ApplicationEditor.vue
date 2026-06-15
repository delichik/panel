<script setup lang="ts">
import { computed, nextTick, reactive, ref, watch } from 'vue';
import { useI18n } from '@/i18n';
import { applicationsApi } from '@/api/applications';
import { serversApi } from '@/api/servers';
import type { ApplicationDto, ApplicationFileDto, ApplicationFileKind, ApplicationPanelFileDto, ApplicationReverseProxyRuleDto, ApplicationSaveDto, ApplicationTemplateVariableDto, ServerDto } from '@/types/api';
import AppPagination from '@/components/AppPagination.vue';
import { usePagination } from '@/composables/usePagination';

const props = defineProps<{ application: ApplicationDto | null; open: boolean }>();
const emit = defineEmits<{ close: []; saved: [ApplicationDto, string?] }>();
const { t, translateApplicationFileKind, translateApplicationRestartPolicy } = useI18n();

interface PortForm { label: string; to: number; static: number | null }
interface StringListItem { value: string }
interface EnvForm { key: string; value: string }
type MountType = 'volume' | 'host' | 'file' | 'panel_file' | 'persistent';
interface MountForm { type: MountType; source: string; target: string; readOnly: boolean }
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

const form = reactive<ApplicationSaveDto>({ name: '', enabled: false, specYaml: defaultSpec(), variables: {}, persistentPath: '', deploymentMode: 'all', deploymentServers: [], reverseProxy: [] });
const specForm = reactive({
  name: 'web',
  image: '',
  networkMode: 'bridge' as 'bridge' | 'host',
  command: [] as StringListItem[],
  args: [] as StringListItem[],
  cpu: null as number | null,
  memoryMb: null as number | null,
  restartPolicy: 'unless-stopped' as 'no' | 'on-failure' | 'always' | 'unless-stopped',
  privileged: false,
  ports: [] as PortForm[],
  env: [] as EnvForm[],
  mounts: [] as MountForm[],
});
const activeEditorTab = ref<'visual' | 'yaml'>('visual');
const variableRows = ref<VariableForm[]>([]);
const loading = ref('');
const error = ref('');
const servers = ref<ServerDto[]>([]);
const files = ref<EditorFile[]>([]);
const templateVariables = ref<ApplicationTemplateVariableDto[]>([]);
const panelFiles = ref<ApplicationPanelFileDto[]>([]);
const templateTextarea = ref();
const yamlTextarea = ref();
const fileForm = reactive({
  path: 'config/app.conf',
  kind: 'template' as ApplicationFileKind,
  template: '',
  file: null as File | File[] | null,
});

const title = computed(() => (props.application ? t('applicationEditor.editTitle', { name: props.application.name }) : t('applicationEditor.createTitle')));
const serverOptions = computed(() => servers.value.map((server) => ({
  title: `${server.name} (${server.host})`,
  value: server.id,
})));
const {
  page: filePage,
  pageSize: filePageSize,
  total: fileTotal,
  pageItems: pagedFiles,
} = usePagination(files);

watch(() => props.open, (open) => {
  if (!open) return;
  const app = props.application;
  form.name = app?.name ?? '';
  form.enabled = app?.enabled ?? false;
  form.specYaml = app?.specYaml ?? defaultSpec();
  form.variables = { ...(app?.variables ?? {}) };
  form.persistentPath = app?.persistentPath ?? '';
  form.deploymentMode = app?.deploymentMode ?? 'all';
  form.deploymentServers = [...(app?.deploymentServers ?? [])];
  form.reverseProxy = cloneReverseProxy(app?.reverseProxy ?? []);
  loadSpecForm(form.specYaml, app?.name);
  const syncedName = app?.name ?? (specForm.name || 'web');
  form.name = syncedName;
  specForm.name = syncedName;
  variableRows.value = Object.entries(form.variables).map(([key, value]) => ({ key, value }));
  error.value = '';
  activeEditorTab.value = 'visual';
  files.value = [];
  void loadServers();
  void loadTemplateCatalog();
  if (app) void loadFiles(app.id);
}, { immediate: true });

watch(() => form.name, (name) => {
  if (specForm.name !== name) specForm.name = name;
});

watch(activeEditorTab, (tab, previous) => {
  if (tab === 'yaml' && previous === 'visual') applyVisualSpec();
});

function defaultSpec() {
  return 'name: web\nnetworkMode: bridge\nrestart:\n  policy: unless-stopped\n';
}

function loadDefaultSpecForm(appName?: string) {
  specForm.name = appName || 'web';
  specForm.image = '';
  specForm.networkMode = 'bridge';
  specForm.command = [{ value: '' }];
  specForm.args = [];
  specForm.cpu = null;
  specForm.memoryMb = null;
  specForm.restartPolicy = 'unless-stopped';
  specForm.privileged = false;
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
  specForm.command = arrayItems(parsed.command, true);
  specForm.args = arrayItems(parsed.args, false);
  specForm.cpu = numericLimit(objectValue(parsed.resources)?.cpu);
  specForm.memoryMb = numericLimit(objectValue(parsed.resources)?.memoryMb);
  const restartPolicy = stringValue(objectValue(parsed.restart)?.policy);
  if (restartPolicy === 'no' || restartPolicy === 'on-failure' || restartPolicy === 'always' || restartPolicy === 'unless-stopped') {
    specForm.restartPolicy = restartPolicy;
  }
  specForm.privileged = Boolean(parsed.privileged);
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
  specForm.mounts.push({ type, source: defaultMountSource(type), target: '/data', readOnly: false });
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
}

function addProxyRule() {
  form.reverseProxy = [...(form.reverseProxy ?? []), { domain: '', targetPort: 80, paths: [{ path: '/', webSocket: false }] }];
}

function addProxyPath(rule: ApplicationReverseProxyRuleDto) {
  rule.paths.push({ path: '/', webSocket: false });
}

function removeProxyRule(index: number) {
  form.reverseProxy = [...(form.reverseProxy ?? [])].filter((_, itemIndex) => itemIndex !== index);
}

function removeAt<T>(items: T[], index: number) {
  items.splice(index, 1);
}

function cloneReverseProxy(rules: ApplicationReverseProxyRuleDto[]) {
  return rules.map((rule) => ({
    domain: rule.domain,
    targetPort: rule.targetPort,
    paths: rule.paths?.map((path) => ({ path: path.path, webSocket: path.webSocket })) ?? [{ path: '/', webSocket: false }],
  }));
}

async function loadFiles(applicationId: string) {
  try {
    files.value = (await applicationsApi.files(applicationId)).map(toEditorFile);
  } catch (err) {
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
  const textarea = component?.$el?.querySelector?.('textarea') as HTMLTextAreaElement | undefined;
  const current = target === 'spec' ? form.specYaml : fileForm.template;
  const start = textarea?.selectionStart ?? current.length;
  const end = textarea?.selectionEnd ?? start;
  const next = current.slice(0, start) + expression + current.slice(end);
  if (target === 'spec') form.specYaml = next;
  else fileForm.template = next;
  await nextTick();
  const nextTextarea = component?.$el?.querySelector?.('textarea') as HTMLTextAreaElement | undefined;
  nextTextarea?.focus();
  nextTextarea?.setSelectionRange(start + expression.length, start + expression.length);
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

async function addFile() {
  const path = fileForm.path.trim();
  if (!path) return;
  const picked = selectedFile();
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
}

function removeFile(file: EditorFile) {
  const index = files.value.findIndex((item) => item.id === file.id && item.path === file.path);
  if (index >= 0) files.value.splice(index, 1);
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
  const spec: Record<string, unknown> = {
    name: specForm.name,
    image: specForm.image,
    networkMode: specForm.networkMode,
  };
  const command = stringListValues(specForm.command);
  const args = stringListValues(specForm.args);
  const env = Object.fromEntries(specForm.env.filter((item) => item.key.trim()).map((item) => [item.key.trim(), item.value]));
  const ports = specForm.networkMode === 'bridge' ? specForm.ports
    .filter((port) => port.label.trim() && port.to)
    .map((port) => ({ label: port.label.trim(), to: Number(port.to), ...(port.static ? { static: Number(port.static) } : {}) })) : [];
  const mounts = specForm.mounts
    .filter((mount) => mount.target.trim() && (mount.type === 'persistent' || mount.source.trim()))
    .map((mount) => ({ type: mount.type, source: mount.source.trim(), target: mount.target.trim(), readOnly: mount.readOnly }));
  if (command.length) spec.command = command;
  if (args.length) spec.args = args;
  if (Object.keys(env).length) spec.env = env;
  if (ports.length) spec.ports = ports;
  const resources: Record<string, number> = {};
  const cpu = numericLimit(specForm.cpu);
  const memoryMb = numericLimit(specForm.memoryMb);
  if (cpu !== null) resources.cpu = cpu;
  if (memoryMb !== null) resources.memoryMb = memoryMb;
  if (Object.keys(resources).length) spec.resources = resources;
  spec.restart = { policy: specForm.restartPolicy };
  if (specForm.privileged) spec.privileged = true;
  if (mounts.length) spec.mounts = mounts;
  return toYaml(spec);
}

function numericLimit(value: unknown) {
  if (value === null || value === undefined || value === '') return null;
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : null;
}

function parseSpecYaml(raw: string) {
  const lines = raw.split(/\r?\n/)
    .map((text) => ({ indent: text.match(/^\s*/)?.[0].length ?? 0, text: text.trim() }))
    .filter((line) => line.text && !line.text.startsWith('#'));
  return objectValue(parseYamlObject(lines, 0, 0).value);
}

function parseYamlObject(lines: Array<{ indent: number; text: string }>, start: number, indent: number): { value: Record<string, unknown>; next: number } {
  const out: Record<string, unknown> = {};
  let index = start;
  while (index < lines.length) {
    const line = lines[index];
    if (line.indent < indent || line.text.startsWith('- ')) break;
    if (line.indent > indent) {
      index++;
      continue;
    }
    const match = line.text.match(/^([^:]+):(.*)$/);
    if (!match) {
      index++;
      continue;
    }
    const key = match[1].trim();
    const rest = match[2].trim();
    if (rest) {
      out[key] = parseYamlScalar(rest);
      index++;
      continue;
    }
    const child = parseYamlBlock(lines, index + 1, indent + 2);
    out[key] = child.value;
    index = child.next;
  }
  return { value: out, next: index };
}

function parseYamlArray(lines: Array<{ indent: number; text: string }>, start: number, indent: number): { value: unknown[]; next: number } {
  const out: unknown[] = [];
  let index = start;
  while (index < lines.length) {
    const line = lines[index];
    if (line.indent < indent || !line.text.startsWith('- ')) break;
    const rest = line.text.slice(2).trim();
    if (!rest) {
      const child = parseYamlBlock(lines, index + 1, indent + 2);
      out.push(child.value);
      index = child.next;
      continue;
    }
    const firstPair = rest.match(/^([^:]+):(.*)$/);
    if (firstPair) {
      const item: Record<string, unknown> = { [firstPair[1].trim()]: parseYamlScalar(firstPair[2].trim()) };
      index++;
      while (index < lines.length && lines[index].indent >= indent + 2 && !lines[index].text.startsWith('- ')) {
        const pair = lines[index].text.match(/^([^:]+):(.*)$/);
        if (pair) item[pair[1].trim()] = pair[2].trim() ? parseYamlScalar(pair[2].trim()) : parseYamlBlock(lines, index + 1, lines[index].indent + 2).value;
        index++;
      }
      out.push(item);
      continue;
    }
    out.push(parseYamlScalar(rest));
    index++;
  }
  return { value: out, next: index };
}

function parseYamlBlock(lines: Array<{ indent: number; text: string }>, start: number, indent: number): { value: unknown; next: number } {
  if (start >= lines.length || lines[start].indent < indent) return { value: {}, next: start };
  if (lines[start].text.startsWith('- ')) return parseYamlArray(lines, start, lines[start].indent);
  return parseYamlObject(lines, start, lines[start].indent);
}

function parseYamlScalar(value: string): unknown {
  if (value === '') return '';
  if (value === 'true') return true;
  if (value === 'false') return false;
  if (/^-?\d+(\.\d+)?$/.test(value)) return Number(value);
  if (value.startsWith('[') && value.endsWith(']')) {
    const inner = value.slice(1, -1).trim();
    return inner ? inner.split(',').map((item): unknown => parseYamlScalar(item.trim())) : [];
  }
  if ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'"))) {
    try {
      return value.startsWith('"') ? JSON.parse(value) : value.slice(1, -1);
    } catch {
      return value.slice(1, -1);
    }
  }
  return value;
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

function toYaml(value: unknown, indent = 0): string {
  const pad = ' '.repeat(indent);
  if (Array.isArray(value)) {
    if (value.length === 0) return '[]';
    return value.map((item) => {
      if (isPlainObject(item)) {
        const lines = Object.entries(item).filter(([, child]) => shouldEmit(child));
        if (lines.length === 0) return `${pad}- {}`;
        const [first, ...rest] = lines;
        return `${pad}- ${first[0]}: ${yamlScalar(first[1])}\n${rest.map(([key, child]) => `${pad}  ${key}: ${yamlScalar(child)}`).join('\n')}`;
      }
      return `${pad}- ${yamlScalar(item)}`;
    }).join('\n');
  }
  if (isPlainObject(value)) {
    return Object.entries(value)
      .filter(([, child]) => shouldEmit(child))
      .map(([key, child]) => {
        if (Array.isArray(child) || isPlainObject(child)) return `${pad}${key}:\n${toYaml(child, indent + 2)}`;
        return `${pad}${key}: ${yamlScalar(child)}`;
      })
      .join('\n') + '\n';
  }
  return yamlScalar(value);
}

function yamlScalar(value: unknown): string {
  if (Array.isArray(value)) return value.length ? `[${value.map(yamlScalar).join(', ')}]` : '[]';
  if (isPlainObject(value)) return `\n${toYaml(value, 2)}`;
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  const text = String(value ?? '');
  if (text === '' || /[:#\[\]{},&*?|-]|^\s|\s$|\n/.test(text)) return JSON.stringify(text);
  return text;
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value);
}

function shouldEmit(value: unknown) {
  if (Array.isArray(value)) return value.length > 0;
  if (isPlainObject(value)) return Object.keys(value).length > 0;
  return value !== undefined && value !== null && value !== '';
}

function readInput(): ApplicationSaveDto {
  if (activeEditorTab.value !== 'yaml') applyVisualSpec();
  return {
    name: form.name,
    enabled: form.enabled,
    specYaml: form.specYaml,
    variables: Object.fromEntries(variableRows.value
      .filter((item) => item.key.trim())
      .map((item) => [item.key.trim(), item.value])),
    persistentPath: '',
    deploymentMode: form.deploymentMode || 'all',
    deploymentServers: form.deploymentMode === 'selected' ? [...(form.deploymentServers ?? [])] : [],
    reverseProxy: (form.reverseProxy ?? [])
      .filter((rule) => rule.domain.trim())
      .map((rule) => ({
        domain: rule.domain.trim(),
        targetPort: Number(rule.targetPort || 0),
        paths: (rule.paths ?? [])
          .filter((path) => path.path.trim())
          .map((path) => ({ path: path.path.trim(), webSocket: Boolean(path.webSocket) })),
      })),
  };
}

async function save(deploy = false) {
  loading.value = deploy ? 'deploy' : 'save';
  try {
    const input = readInput();
    const session = await applicationsApi.beginSaveSession({
      applicationId: props.application?.id,
      save: input,
    });
    const finalPaths = new Set(files.value.map((file) => file.path));
    for (const staged of session.files) {
      if (!finalPaths.has(staged.path)) {
        await applicationsApi.deleteSaveSessionFile(session.id, { path: staged.path });
      }
    }
    for (const file of files.value) {
      if (!file.contentBase64) continue;
      await applicationsApi.uploadSaveSessionFile(session.id, {
        path: file.path,
        kind: file.kind,
        contentType: '',
        contentBase64: file.contentBase64,
      });
    }
    const app = await applicationsApi.commitSaveSession(session.id);
    if (deploy) {
      const result = await applicationsApi.deploy(app.id);
      emit('saved', result.application ?? app, result.taskId);
    } else {
      emit('saved', app);
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('applicationEditor.saveFailed');
  } finally {
    loading.value = '';
  }
}
</script>

<template>
  <v-dialog :model-value="open" width="920" @update:model-value="emit('close')">
    <v-card class="app-dialog-card editor-card">
      <v-card-title class="app-dialog-title">
        <span class="app-dialog-title-text">{{ title }}</span>
        <v-btn icon="mdi-close" variant="text" @click="emit('close')" />
      </v-card-title>
      <v-divider />
      <v-card-text class="app-dialog-body">
        <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>
        <div class="editor-main">
          <v-tabs v-model="activeEditorTab" density="comfortable" class="editor-tabs mb-4">
            <v-tab value="visual">{{ t('applicationEditor.general') }}</v-tab>
            <v-tab value="yaml">{{ t('applicationEditor.yaml') }}</v-tab>
          </v-tabs>

          <v-window v-model="activeEditorTab" class="editor-window">
            <v-window-item value="visual">
          <section class="editor-section">
            <div class="section-title">{{ t('applicationEditor.general') }}</div>
            <div class="field-grid">
              <v-text-field v-model="form.name" :label="t('applicationEditor.applicationName')" density="compact" variant="outlined" :readonly="Boolean(application)" hide-details />
              <v-text-field v-model="specForm.image" :label="t('applicationEditor.image')" density="compact" variant="outlined" />
              <v-switch v-model="form.enabled" :label="t('common.enabled')" color="primary" density="compact" hide-details class="switch-field" />
            </div>
          </section>

          <v-divider class="section-divider" />

          <section class="editor-section">
            <div class="command-grid">
              <div>
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
                  <v-btn icon="mdi-delete" variant="text" color="error" :disabled="specForm.command.length === 1" @click="removeAt(specForm.command, index)" />
                </div>
                <v-btn size="small" variant="outlined" prepend-icon="mdi-plus" class="text-none" @click="addStringItem(specForm.command)">{{ t('applicationEditor.addCommandItem') }}</v-btn>
              </div>

              <div>
                <div class="section-title">{{ t('applicationEditor.arguments') }}</div>
                <div v-for="(item, index) in specForm.args" :key="`args-${index}`" class="repeat-row command-row">
                  <v-text-field
                    v-model="item.value"
                    :label="t('applicationEditor.argumentItem', { index: index + 1 })"
                    :hint="index === 0 ? t('applicationEditor.argumentsHint') : ''"
                    density="compact"
                    variant="outlined"
                    hide-details="auto"
                  />
                  <v-btn icon="mdi-delete" variant="text" color="error" :disabled="specForm.args.length === 0" @click="removeAt(specForm.args, index)" />
                </div>
                <v-btn size="small" variant="outlined" prepend-icon="mdi-plus" class="text-none" @click="addStringItem(specForm.args)">{{ t('applicationEditor.addArgumentItem') }}</v-btn>
              </div>
            </div>
          </section>

          <v-divider class="section-divider" />

          <section class="editor-section">
            <div class="section-title">{{ t('applicationEditor.runtime') }}</div>
            <div class="field-grid">
              <v-select
                v-model="specForm.restartPolicy"
                :items="[
                  { title: translateApplicationRestartPolicy('unless-stopped'), value: 'unless-stopped' },
                  { title: translateApplicationRestartPolicy('always'), value: 'always' },
                  { title: translateApplicationRestartPolicy('on-failure'), value: 'on-failure' },
                  { title: translateApplicationRestartPolicy('no'), value: 'no' },
                ]"
                :label="t('applicationEditor.policy')"
                density="compact"
                variant="outlined"
              />
              <v-text-field v-model.number="specForm.cpu" type="number" min="0" :label="t('applicationEditor.cpuMhz')" density="compact" variant="outlined" />
              <v-text-field v-model.number="specForm.memoryMb" type="number" min="0" :label="t('applicationEditor.memoryMb')" density="compact" variant="outlined" />
              <v-switch v-model="specForm.privileged" :label="t('applicationEditor.privilegedContainer')" color="primary" density="compact" hide-details class="switch-field" />
            </div>
          </section>

          <v-divider class="section-divider" />

          <section class="editor-section">
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

          <section class="editor-section">
            <div class="section-title">{{ t('applicationEditor.environment') }}</div>
            <div v-for="(item, index) in specForm.env" :key="index" class="repeat-row env-row">
              <v-text-field v-model="item.key" :label="t('common.key')" density="compact" variant="outlined" hide-details />
              <v-text-field v-model="item.value" :label="t('common.value')" density="compact" variant="outlined" hide-details />
              <v-btn icon="mdi-delete" variant="text" color="error" @click="removeAt(specForm.env, index)" />
            </div>
            <v-btn size="small" variant="outlined" prepend-icon="mdi-plus" class="text-none" @click="addEnv">{{ t('common.addVariable') }}</v-btn>
          </section>

          <v-divider class="section-divider" />

          <section class="editor-section">
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
              <div class="network-actions">
                <v-btn size="small" variant="outlined" prepend-icon="mdi-plus" class="text-none" @click="addPort">{{ t('common.addPort') }}</v-btn>
              </div>
              <div v-for="(port, index) in specForm.ports" :key="index" class="repeat-row ports-row">
                <v-text-field v-model="port.label" :label="t('applicationEditor.label')" density="compact" variant="outlined" hide-details />
                <span class="network-target-name">{{ specForm.name || t('applicationEditor.appTargetFallback') }}:</span>
                <v-text-field v-model.number="port.to" type="number" :label="t('applicationEditor.containerPort')" density="compact" variant="outlined" hide-details />
                <v-icon icon="mdi-arrow-right" size="20" class="network-arrow" />
                <span class="network-target-name">{{ t('applicationEditor.nodeTarget') }}:</span>
                <v-text-field v-model.number="port.static" type="number" :label="t('applicationEditor.hostPort')" density="compact" variant="outlined" hide-details />
                <v-btn icon="mdi-delete" variant="text" color="error" @click="removeAt(specForm.ports, index)" />
              </div>
            </template>

            <div class="section-title">{{ t('applicationEditor.reverseProxy') }}</div>
            <div class="proxy-actions">
              <v-btn size="small" variant="outlined" prepend-icon="mdi-plus" class="text-none" @click="addProxyRule">{{ t('common.addProxyRule') }}</v-btn>
            </div>
            <div v-for="(rule, ruleIndex) in form.reverseProxy" :key="ruleIndex" class="proxy-rule">
              <div class="proxy-rule-header">
                <v-text-field v-model="rule.domain" :label="t('applicationEditor.domain')" density="compact" variant="outlined" hide-details />
                <v-icon icon="mdi-arrow-right" size="20" class="proxy-arrow" />
                <span class="proxy-target-name">{{ specForm.name || t('applicationEditor.appTargetFallback') }}:</span>
                <v-text-field v-model.number="rule.targetPort" type="number" min="1" max="65535" :label="t('applicationEditor.target')" density="compact" variant="outlined" hide-details />
                <v-btn icon="mdi-delete" variant="text" color="error" @click="removeProxyRule(ruleIndex)" />
              </div>
              <div class="proxy-actions">
                <v-btn size="small" variant="outlined" prepend-icon="mdi-plus" class="text-none" @click="addProxyPath(rule)">{{ t('common.addPath') }}</v-btn>
              </div>
              <div v-for="(path, pathIndex) in rule.paths" :key="pathIndex" class="repeat-row proxy-path-row">
                <v-text-field v-model="path.path" :label="t('applicationEditor.path')" density="compact" variant="outlined" hide-details />
                <v-checkbox v-model="path.webSocket" :label="t('applicationEditor.websocket')" density="compact" hide-details />
                <v-btn icon="mdi-delete" variant="text" color="error" @click="removeAt(rule.paths, pathIndex)" />
              </div>
            </div>
          </section>

          <v-divider class="section-divider" />

          <section class="editor-section">
            <div class="section-title">{{ t('applicationEditor.variables') }}</div>
            <div v-for="(item, index) in variableRows" :key="index" class="repeat-row variable-row">
              <v-text-field v-model="item.key" :label="t('common.key')" density="compact" variant="outlined" hide-details />
              <v-text-field v-model="item.value" :label="t('common.value')" density="compact" variant="outlined" hide-details />
              <v-btn icon="mdi-delete" variant="text" color="error" @click="removeAt(variableRows, index)" />
            </div>
            <v-btn size="small" variant="outlined" prepend-icon="mdi-plus" class="text-none mb-4" @click="addVariable">{{ t('common.addVariable') }}</v-btn>

            <div class="section-title">{{ t('applicationEditor.applicationFiles') }}</div>
            <div class="file-form">
              <v-text-field v-model="fileForm.path" :label="t('applicationEditor.workspacePath')" density="compact" variant="outlined" hide-details />
              <v-select
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
              <v-textarea
                v-if="fileForm.kind === 'template'"
                ref="templateTextarea"
                v-model="fileForm.template"
                :label="t('applicationEditor.template')"
                rows="7"
                variant="outlined"
                spellcheck="false"
                class="mono-input span-all"
              />
              <v-menu v-if="fileForm.kind === 'template'">
                <template #activator="{ props: menuProps }">
                  <v-btn v-bind="menuProps" variant="outlined" prepend-icon="mdi-code-braces" class="text-none span-all">{{ t('applicationEditor.insertVariable') }}</v-btn>
                </template>
                <v-list density="compact">
                  <v-list-item v-for="item in variableItems('template')" :key="item.title" :title="item.title" @click="insertVariable('template', item.value)" />
                </v-list>
              </v-menu>
              <v-file-input
                v-else
                v-model="fileForm.file"
                :label="t('applicationEditor.binaryFile')"
                density="compact"
                variant="outlined"
                hide-details
                class="span-all"
              />
              <v-btn color="primary" variant="flat" class="text-none" :disabled="!fileForm.path || (fileForm.kind === 'binary' && !selectedFile())" @click="addFile">{{ t('common.addFile') }}</v-btn>
            </div>
            <v-table density="compact" class="mt-3">
              <thead><tr><th>{{ t('common.path') }}</th><th>{{ t('applicationEditor.kind') }}</th><th>{{ t('common.size') }}</th><th>{{ t('common.sha256') }}</th><th class="text-right">{{ t('common.actions') }}</th></tr></thead>
              <tbody>
                <tr v-for="file in pagedFiles" :key="`${file.id}:${file.path}`">
                  <td class="mono text-truncate">{{ file.path }}</td>
                  <td><v-chip size="small" variant="tonal" label>{{ translateApplicationFileKind(file.kind) }}</v-chip></td>
                  <td>{{ sizeLabel(file.size) }}</td>
                  <td class="mono text-truncate hash-cell">{{ file.sha256 }}</td>
                  <td class="text-right"><v-btn size="small" icon="mdi-delete" variant="text" color="error" @click="removeFile(file)" /></td>
                </tr>
                <tr v-if="files.length === 0"><td colspan="5" class="text-center text-medium-emphasis py-4">{{ t('applicationEditor.noFiles') }}</td></tr>
              </tbody>
            </v-table>
            <AppPagination v-model:page="filePage" v-model:page-size="filePageSize" :total="fileTotal" />
          </section>

          <v-divider class="section-divider" />

          <section class="editor-section">
            <div class="section-title">{{ t('applicationEditor.mounts') }}</div>
            <div v-for="(mount, index) in specForm.mounts" :key="index" class="repeat-row mount-row">
              <v-select v-model="mount.type" :items="[
                { title: t('applicationEditor.dockerVolume'), value: 'volume' },
                { title: t('applicationEditor.hostPath'), value: 'host' },
                { title: t('applicationEditor.appFile'), value: 'file' },
                { title: t('applicationEditor.panelFile'), value: 'panel_file' },
                { title: t('applicationEditor.persistent'), value: 'persistent' },
              ]" :label="t('applicationEditor.sourceType')" density="compact" variant="outlined" hide-details @update:model-value="updateMountType(mount)" />
              <v-combobox
                v-if="mount.type === 'file'"
                v-model="mount.source"
                :items="files.map(file => file.path)"
                :label="t('applicationEditor.workspaceFile')"
                density="compact"
                variant="outlined"
                hide-details
              />
              <v-select
                v-else-if="mount.type === 'panel_file'"
                v-model="mount.source"
                :items="panelFiles"
                item-value="source"
                :item-title="item => `${item.name} / ${item.kind}`"
                :label="t('applicationEditor.panelFile')"
                density="compact"
                variant="outlined"
                hide-details
              />
              <v-text-field
                v-else-if="mount.type === 'persistent'"
                v-model="mount.source"
                :label="t('applicationEditor.persistentSubpath')"
                density="compact"
                variant="outlined"
                hide-details
              />
              <v-text-field v-else v-model="mount.source" :label="mount.type === 'host' ? t('applicationEditor.hostAbsolutePath') : t('applicationEditor.volumeName')" density="compact" variant="outlined" hide-details />
              <v-text-field v-model="mount.target" :label="t('applicationEditor.containerPath')" density="compact" variant="outlined" hide-details />
              <v-checkbox v-model="mount.readOnly" :label="t('applicationEditor.readOnly')" density="compact" hide-details />
              <v-btn icon="mdi-delete" variant="text" color="error" @click="removeAt(specForm.mounts, index)" />
            </div>
            <div class="mount-actions">
              <v-btn size="small" variant="outlined" prepend-icon="mdi-plus" class="text-none" @click="addMount('volume')">{{ t('applicationEditor.dockerVolume') }}</v-btn>
              <v-btn size="small" variant="outlined" prepend-icon="mdi-folder" class="text-none" @click="addMount('host')">{{ t('applicationEditor.hostPath') }}</v-btn>
              <v-btn size="small" variant="outlined" prepend-icon="mdi-file" class="text-none" @click="addMount('file')">{{ t('applicationEditor.appFile') }}</v-btn>
              <v-btn size="small" variant="outlined" prepend-icon="mdi-shield-key" class="text-none" @click="addMount('panel_file')">{{ t('applicationEditor.panelFile') }}</v-btn>
              <v-btn size="small" variant="outlined" prepend-icon="mdi-database" class="text-none" @click="addMount('persistent')">{{ t('applicationEditor.persistent') }}</v-btn>
            </div>
          </section>

            </v-window-item>

            <v-window-item value="yaml" class="editor-yaml-pane">
          <section class="editor-section">
            <div class="section-title">{{ t('applicationEditor.yaml') }}</div>
            <v-textarea
              ref="yamlTextarea"
              v-model="form.specYaml"
              :label="t('applicationEditor.yamlSpec')"
              variant="outlined"
              rows="18"
              spellcheck="false"
              class="mono-input"
            />
            <v-menu>
              <template #activator="{ props: menuProps }">
                <v-btn v-bind="menuProps" variant="outlined" prepend-icon="mdi-code-braces" class="text-none mt-2">{{ t('applicationEditor.insertVariable') }}</v-btn>
              </template>
              <v-list density="compact">
                <v-list-item v-for="item in variableItems('spec')" :key="item.title" :title="item.title" @click="insertVariable('spec', item.value)" />
              </v-list>
            </v-menu>
          </section>
            </v-window-item>
          </v-window>
        </div>
      </v-card-text>
      <v-divider />
      <v-card-actions class="app-dialog-actions">
        <v-btn variant="text" class="text-none" @click="emit('close')">{{ t('common.cancel') }}</v-btn>
        <v-btn color="primary" variant="flat" class="text-none" :loading="Boolean(loading)" @click="save(form.enabled)">
          {{ form.enabled ? t('common.saveAndDeploy') : t('common.save') }}
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<style scoped>
.editor-card { max-height: calc(100dvh - 24px); }
.editor-main { min-width: 0; }
.editor-section { min-width: 0; }
.section-divider { margin: 18px 0; }
.field-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; align-items: start; }
.command-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; align-items: start; }
.switch-field { min-height: 40px; }
.span-2 { grid-column: span 2; }
.span-all { grid-column: 1 / -1; }
.section-title { margin: 14px 0 8px; }
.editor-section > .section-title:first-child { margin-top: 0; }
.repeat-row { display: grid; gap: 8px; align-items: center; margin-bottom: 8px; }
.placement-row { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; align-items: start; }
.ports-row { grid-template-columns: minmax(120px, 1fr) auto minmax(96px, 0.6fr) auto auto minmax(96px, 0.6fr) 40px; }
.command-row { grid-template-columns: minmax(0, 1fr) 40px; }
.env-row { grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) 40px; }
.mount-row { grid-template-columns: 150px minmax(180px, 1.2fr) minmax(150px, 0.8fr) auto 40px; }
.mount-actions { display: flex; gap: 8px; flex-wrap: wrap; margin-bottom: 12px; }
.variable-row { grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) 40px; }
.proxy-rule { border: 1px solid var(--lp-border); border-radius: 8px; padding: 12px; margin-bottom: 12px; background: color-mix(in srgb, var(--lp-surface-container), transparent 28%); }
.network-actions,
.proxy-actions { display: flex; justify-content: flex-start; margin-bottom: 10px; }
.network-arrow,
.network-target-name { color: var(--lp-text-muted); white-space: nowrap; }
.network-arrow { justify-self: center; }
.network-target-name { font-size: 0.82rem; }
.proxy-rule-header { display: grid; grid-template-columns: minmax(220px, 1fr) auto auto 140px 40px; gap: 8px; align-items: center; margin-bottom: 10px; }
.proxy-arrow,
.proxy-target-name { color: var(--lp-text-muted); white-space: nowrap; }
.proxy-arrow { justify-self: center; }
.proxy-target-name { font-size: 0.82rem; }
.proxy-path-row { grid-template-columns: minmax(0, 1fr) 130px 40px; }
.file-form { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; align-items: start; }
.repeat-row > .v-btn,
.proxy-rule-header > .v-btn {
  justify-self: end;
}
.mono, .mono-input :deep(textarea) { font-size: 0.82rem; }
.hash-cell { max-width: 180px; }
@media (max-width: 900px) {
  .field-grid,
  .command-grid,
  .mount-row,
  .ports-row,
  .command-row,
  .env-row,
  .variable-row,
  .proxy-rule-header,
  .proxy-path-row,
  .file-form,
  .placement-row {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 760px) {
  .mount-actions .v-btn,
  .network-actions .v-btn,
  .proxy-actions .v-btn {
    flex: 1 1 100%;
  }
}

</style>
