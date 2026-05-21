<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { composeApi } from '@/api/compose';
import { serversApi } from '@/api/servers';
import { serviceBodyYamlToVisual, visualToServiceBodyYaml } from '@/features/compose/serviceBodyYaml';
import type {
  ComposeRenderPreviewDto,
  ComposeTemplateVariableDto,
  ComposeVisualModelDto,
  ComposeVisualServiceDto,
  ServerDto,
  ServiceTemplateDto,
  TemplateFileDto,
  TemplateFileKind,
} from '@/types/api';

type EditMode = 'visual' | 'yaml';

interface KeyValueRow {
  id: string;
  key: string;
  value: string;
}

interface PortRow {
  id: string;
  host: string;
  container: string;
  protocol: 'tcp' | 'udp';
}

interface VolumeRow {
  id: string;
  source: string;
  target: string;
  mode: '' | 'ro' | 'rw';
}

interface TemplateForm {
  id: string;
  name: string;
  description: string;
  composeYaml: string;
  visual: ComposeVisualModelDto;
  variables: ComposeTemplateVariableDto[];
  dependencies: string[];
}

interface FileForm {
  id: string;
  kind: TemplateFileKind;
  path: string;
  content: string;
  base64Content: string;
  pickedName: string;
}

const templates = ref<ServiceTemplateDto[]>([]);
const servers = ref<ServerDto[]>([]);
const files = ref<TemplateFileDto[]>([]);
const loading = ref(false);
const detailLoading = ref(false);
const saving = ref(false);
const rendering = ref(false);
const drawerOpen = ref(false);
const fileDialogOpen = ref(false);
const mode = ref<EditMode>('visual');
const previewServerId = ref('');
const preview = ref<ComposeRenderPreviewDto | null>(null);
const error = ref('');
const editorError = ref('');
const previewError = ref('');

// Snackbar notification state
const snackbar = ref(false);
const snackbarText = ref('');
const snackbarColor = ref('success');

function showMessage(text: string, color = 'success') {
  snackbarText.value = text;
  snackbarColor.value = color;
  snackbar.value = true;
}

// Confirmation Dialog State
const confirmDialog = ref(false);
const confirmTitle = ref('Confirm action');
const confirmMessage = ref('');
const confirmAction = ref<(() => Promise<void>) | null>(null);

function confirm(title: string, message: string, action: () => Promise<void>) {
  confirmTitle.value = title;
  confirmMessage.value = message;
  confirmAction.value = action;
  confirmDialog.value = true;
}

async function executeConfirm() {
  if (confirmAction.value) {
    try {
      await confirmAction.value();
    } catch (err) {
      showMessage(err instanceof Error ? err.message : 'Action failed', 'error');
    }
  }
  confirmDialog.value = false;
}

const form = reactive<TemplateForm>(emptyTemplateForm());
const fileForm = reactive<FileForm>(emptyFileForm());
const ports = ref<PortRow[]>([]);
const volumes = ref<VolumeRow[]>([]);
const environment = ref<KeyValueRow[]>([]);
const labels = ref<KeyValueRow[]>([]);
const envFiles = ref<KeyValueRow[]>([]);
const networks = ref<KeyValueRow[]>([]);
const extraHosts = ref<KeyValueRow[]>([]);
const dns = ref<KeyValueRow[]>([]);
const dnsSearch = ref<KeyValueRow[]>([]);

const sortedTemplates = computed(() => [...safeArray(templates.value)].sort((a, b) => b.updatedAt.localeCompare(a.updatedAt)));
const isEditing = computed(() => Boolean(form.id));
const templateService = computed(() => {
  if (!form.visual) form.visual = { services: [defaultVisualService()] };
  if (!Array.isArray(form.visual.services) || !form.visual.services.length) form.visual.services = [defaultVisualService()];
  return form.visual.services[0];
});
const dependencyOptions = computed(() => safeArray(templates.value).filter((template) => template.id !== form.id));
const selectedDependencies = computed(() =>
  safeArray(form.dependencies)
    .map((id) => safeArray(templates.value).find((template) => template.id === id))
    .filter((item): item is ServiceTemplateDto => Boolean(item)),
);
const previewFiles = computed(() => safeArray(preview.value?.files));
const dependencyRisk = computed(() => {
  if (!form.id) return '';
  const graph = new Map(safeArray(templates.value).map((template) => [template.id, safeArray(template.dependencies)]));
  const visit = (id: string, seen = new Set<string>()): boolean => {
    if (id === form.id) return true;
    if (seen.has(id)) return false;
    seen.add(id);
    return (graph.get(id) ?? []).some((next) => visit(next, seen));
  };
  return safeArray(form.dependencies).some((id) => visit(id)) ? 'Dependency selection would create a cycle.' : '';
});
const builtIns = computed(() => [
  '{{ .server.name }}',
  '{{ .server.host }}',
  '{{ .server.ip }}',
  '{{ .server.user }}',
  '{{ .servers }}',
  '{{ .files }}',
  '{{ .template.name }}',
  '{{ .system_service_id }}',
  '{{ .system_remote_path }}',
  '{{ .system_template_name }}',
]);

function rowId() {
  return Math.random().toString(36).slice(2);
}

function safeArray<T>(value: T[] | null | undefined): T[] {
  return Array.isArray(value) ? value : [];
}

function normalizeTemplate(template: ServiceTemplateDto | null | undefined): ServiceTemplateDto | null {
  if (!template) return null;
  return {
    ...template,
    dependencies: safeArray(template.dependencies),
    variables: safeArray(template.variables),
    visual: normalizeVisual(template.visual as ComposeVisualModelDto | null | undefined),
  };
}

function defaultVisualService(): ComposeVisualServiceDto {
  return {
    name: 'app',
    image: 'nginx:latest',
    restart: 'unless-stopped',
    environment: {},
    labels: {},
    ports: [],
    volumes: [],
  };
}

function emptyTemplateForm(): TemplateForm {
  const visual = { services: [defaultVisualService()] };
  return {
    id: '',
    name: '',
    description: '',
    composeYaml: visualToYaml(visual),
    visual,
    variables: [],
    dependencies: [],
  };
}

function emptyFileForm(): FileForm {
  return { id: '', kind: 'template', path: '', content: '', base64Content: '', pickedName: '' };
}

function resetForm(next = emptyTemplateForm()) {
  next.visual = normalizeVisual(next.visual);
  next.variables = safeArray(next.variables);
  next.dependencies = safeArray(next.dependencies);
  Object.assign(form, next);
  hydrateRowsFromVisual();
  preview.value = null;
  editorError.value = '';
  previewError.value = '';
}

function templateToForm(template: ServiceTemplateDto): TemplateForm {
  const visual = visualFromTemplate(template);
  return {
    id: template.id,
    name: template.name,
    description: template.description ?? '',
    composeYaml: template.composeYaml || visualToYaml(visual),
    visual,
    variables: safeArray(template.variables),
    dependencies: safeArray(template.dependencies),
  };
}

function visualFromTemplate(template: ServiceTemplateDto): ComposeVisualModelDto {
  const visual = template.visual as ComposeVisualModelDto | undefined | null;
  if (Array.isArray(visual?.services) && visual.services.length) return { services: [visual.services[0]] };
  return yamlToVisual(template.composeYaml);
}

function normalizeVisual(visual: ComposeVisualModelDto | null | undefined): ComposeVisualModelDto {
  if (!visual || !Array.isArray(visual.services) || !visual.services.length) {
    return { services: [defaultVisualService()] };
  }
  return { ...visual, services: [visual.services[0] ?? defaultVisualService()] };
}

function hydrateRowsFromVisual() {
  const service = templateService.value;
  ports.value = safeArray(service.ports).map((item) => {
    const [left, right = ''] = item.split(':');
    const [container, protocol = 'tcp'] = right.split('/');
    return { id: rowId(), host: left, container, protocol: protocol === 'udp' ? 'udp' : 'tcp' };
  });
  volumes.value = safeArray(service.volumes).map((item) => {
    const [source = '', target = '', mode = ''] = item.split(':');
    return { id: rowId(), source, target, mode: mode === 'ro' || mode === 'rw' ? mode : '' };
  });
  environment.value = mapToRows(service.environment);
  labels.value = mapToRows(service.labels);
  envFiles.value = valuesToRows(service.envFile);
  networks.value = valuesToRows(service.networks);
  extraHosts.value = valuesToRows(service.extraHosts);
  dns.value = valuesToRows(service.dns);
  dnsSearch.value = valuesToRows(service.dnsSearch);
}

function mapToRows(value?: Record<string, string>) {
  return Object.entries(value ?? {}).map(([key, rowValue]) => ({ id: rowId(), key, value: String(rowValue) }));
}

function valuesToRows(values?: string[]) {
  return safeArray(values).map((value) => ({ id: rowId(), key: value, value: '' }));
}

function rowsToMap(rows: KeyValueRow[]) {
  return Object.fromEntries(safeArray(rows).filter((row) => row.key.trim()).map((row) => [row.key.trim(), row.value]));
}

function rowsToValues(rows: KeyValueRow[]) {
  return safeArray(rows).map((row) => row.key.trim()).filter(Boolean);
}

function syncYamlFromVisual() {
  const service = templateService.value;
  service.name = form.name || service.name || 'app';
  service.ports = ports.value.filter((row) => row.host && row.container).map((row) => `${row.host}:${row.container}${row.protocol === 'udp' ? '/udp' : ''}`);
  service.volumes = volumes.value
    .filter((row) => row.source && row.target)
    .map((row) => `${row.source}:${row.target}${row.mode ? `:${row.mode}` : ''}`);
  service.environment = rowsToMap(environment.value);
  service.labels = rowsToMap(labels.value);
  service.envFile = rowsToValues(envFiles.value);
  service.networks = rowsToValues(networks.value);
  service.extraHosts = rowsToValues(extraHosts.value);
  service.dns = rowsToValues(dns.value);
  service.dnsSearch = rowsToValues(dnsSearch.value);
  form.composeYaml = visualToYaml(form.visual);
  editorError.value = '';
}

function visualToYaml(visual: ComposeVisualModelDto) {
  const service = visual.services[0] ?? defaultVisualService();
  return visualToServiceBodyYaml(service);
}

function yamlToVisual(yaml: string): ComposeVisualModelDto {
  return { services: [{ ...defaultVisualService(), ...serviceBodyYamlToVisual(yaml), name: form.name || 'app' }] };
}

function syncVisualFromYaml(notify = true) {
  form.visual = yamlToVisual(form.composeYaml);
  hydrateRowsFromVisual();
  if (notify) showMessage('Visual fields refreshed from YAML');
}

function addPort() {
  ports.value.push({ id: rowId(), host: '', container: '', protocol: 'tcp' });
}

function addVolume() {
  volumes.value.push({ id: rowId(), source: '', target: '', mode: '' });
}

function addKeyValue(rows: KeyValueRow[], key = '', value = '') {
  rows.push({ id: rowId(), key, value });
}

function removeRow<T>(rows: T[], index: number) {
  rows.splice(index, 1);
  syncYamlFromVisual();
}

function addVariable() {
  form.variables.push({ name: '', type: 'string', required: false, defaultValue: '' });
}

function removeVariable(index: number) {
  form.variables.splice(index, 1);
}

async function copyBuiltIn(value: string) {
  if (navigator.clipboard) {
    await navigator.clipboard.writeText(value);
    showMessage('Variable copied');
  }
}

function insertVariable(row: KeyValueRow, value: string) {
  row.value = `${row.value || ''}${value}`;
  syncYamlFromVisual();
}

function dependencyCount(template: ServiceTemplateDto | null | undefined) {
  return safeArray(template?.dependencies).length;
}

async function loadTemplates() {
  loading.value = true;
  try {
    const [templateRows, serverRows] = await Promise.all([composeApi.listTemplates(), serversApi.listServers()]);
    templates.value = safeArray(templateRows).map(normalizeTemplate).filter((template): template is ServiceTemplateDto => Boolean(template));
    servers.value = safeArray(serverRows);
    if (!previewServerId.value && servers.value.length) previewServerId.value = servers.value[0].id;
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to load service templates';
  } finally {
    loading.value = false;
  }
}

async function openTemplate(template?: ServiceTemplateDto) {
  drawerOpen.value = true;
  mode.value = 'visual';
  files.value = [];
  if (!template) {
    resetForm();
    return;
  }
  detailLoading.value = true;
  try {
    const [detail, fileRows] = await Promise.all([composeApi.getTemplate(template.id), composeApi.listTemplateFiles(template.id)]);
    const normalizedDetail = normalizeTemplate(detail);
    if (!normalizedDetail) throw new Error('Template detail is empty');
    resetForm(templateToForm(normalizedDetail));
    files.value = safeArray(fileRows);
  } catch (err) {
    showMessage(err instanceof Error ? err.message : 'Unable to load template', 'error');
  } finally {
    detailLoading.value = false;
  }
}

async function saveTemplate() {
  saving.value = true;
  if (mode.value === 'visual') {
    syncYamlFromVisual();
  } else {
    form.visual = yamlToVisual(form.composeYaml);
  }
  templateService.value.name = form.name || templateService.value.name || 'app';
  try {
    const input = {
      name: form.name,
      description: form.description,
      composeYaml: form.composeYaml,
      visual: form.visual,
      variables: safeArray(form.variables).filter((item) => item.name.trim()),
      dependencies: safeArray(form.dependencies),
    };
    const saved = form.id ? await composeApi.updateTemplate(form.id, input) : await composeApi.createTemplate(input);
    resetForm(templateToForm(saved));
    await loadTemplates();
    drawerOpen.value = false;
    showMessage('Service template saved successfully');
  } catch (err) {
    editorError.value = err instanceof Error ? err.message : 'Unable to save service template';
    showMessage(editorError.value, 'error');
  } finally {
    saving.value = false;
  }
}

async function deleteTemplate(template: ServiceTemplateDto) {
  confirm('Confirm delete', `Delete service template ${template.name}?`, async () => {
    await composeApi.deleteTemplate(template.id);
    await loadTemplates();
    showMessage('Service template deleted');
  });
}

async function renderPreview() {
  if (!form.id) return;
  rendering.value = true;
  try {
    preview.value = await composeApi.renderTemplatePreview(form.id, { serverId: previewServerId.value });
    previewError.value = '';
  } catch (err) {
    previewError.value = err instanceof Error ? err.message : 'Unable to render preview';
  } finally {
    rendering.value = false;
  }
}

function openFile(file?: TemplateFileDto) {
  Object.assign(
    fileForm,
    file
      ? { id: file.id, kind: file.kind, path: file.path, content: file.content ?? '', base64Content: file.base64Content ?? '', pickedName: '' }
      : emptyFileForm(),
  );
  fileDialogOpen.value = true;
}

function handleBinaryInput(event: Event) {
  const target = event.target as HTMLInputElement;
  const file = target.files?.[0];
  if (!file) return;
  fileForm.pickedName = file.name;
  const reader = new FileReader();
  reader.onload = () => {
    fileForm.base64Content = String(reader.result).split(',')[1] ?? '';
    if (!fileForm.path) fileForm.path = file.name;
  };
  reader.readAsDataURL(file);
}

async function saveFile() {
  if (!form.id) return;
  try {
    const input = {
      path: fileForm.path,
      content: fileForm.kind === 'template' ? fileForm.content : undefined,
      base64Content: fileForm.kind === 'binary' ? fileForm.base64Content : undefined,
    };
    const saved = fileForm.id
      ? await composeApi.updateTemplateFile(form.id, fileForm.id, input)
      : fileForm.kind === 'binary'
        ? await composeApi.createTemplateBinaryFile(form.id, input)
        : await composeApi.createTemplateTextFile(form.id, input);
    const index = files.value.findIndex((item) => item.id === saved.id);
    if (index >= 0) files.value[index] = saved;
    else files.value.push(saved);
    fileDialogOpen.value = false;
    showMessage('Template file saved');
  } catch (err) {
    showMessage(err instanceof Error ? err.message : 'Failed to save template file', 'error');
  }
}

async function deleteFile(file: TemplateFileDto) {
  if (!form.id) return;
  confirm('Confirm delete', `Delete attached file ${file.path}?`, async () => {
    await composeApi.deleteTemplateFile(form.id, file.id);
    files.value = files.value.filter((item) => item.id !== file.id);
    showMessage('Template file deleted');
  });
}

onMounted(loadTemplates);
</script>

<template>
  <div>
    <div class="d-flex justify-space-between align-center mb-6">
      <div>
        <h1 class="text-h4 font-weight-bold">Service Templates</h1>
        <p class="text-subtitle-1 text-medium-emphasis">Create reusable Compose declarations, define template variables, and link dependency chains.</p>
      </div>
      <div class="d-flex" style="gap: 12px;">
        <v-btn
          prepend-icon="mdi-refresh"
          :loading="loading"
          variant="outlined"
          @click="loadTemplates"
          class="text-none font-weight-bold"
        >
          Reload
        </v-btn>
        <v-btn
          color="primary"
          prepend-icon="mdi-plus"
          @click="openTemplate()"
          class="text-none font-weight-bold"
        >
          New Template
        </v-btn>
      </div>
    </div>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>

    <v-card :loading="loading" variant="outlined" class="mb-6">
      <v-table class="text-left" style="background: transparent;">
        <thead>
          <tr>
            <th class="font-weight-bold">Name</th>
            <th class="font-weight-bold">Description</th>
            <th class="font-weight-bold">Dependencies</th>
            <th class="font-weight-bold">Version</th>
            <th class="font-weight-bold">Updated</th>
            <th class="font-weight-bold text-right" style="width: 210px;">Actions</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="sortedTemplates.length === 0">
            <td colspan="6" class="text-center py-6 text-grey-darken-1">No service templates</td>
          </tr>
          <tr v-for="row in sortedTemplates" :key="row.id">
            <td class="font-weight-bold">{{ row.name }}</td>
            <td class="text-truncate text-grey-darken-1" style="max-width: 300px;" :title="row.description">
              {{ row.description || '-' }}
            </td>
            <td>
              <v-chip size="small" label color="grey-lighten-1">{{ dependencyCount(row) }}</v-chip>
            </td>
            <td>{{ row.version }}</td>
            <td class="text-caption text-grey-darken-1">
              {{ row.updatedAt ? new Date(row.updatedAt).toLocaleString() : '-' }}
            </td>
            <td class="text-right">
              <div class="d-flex justify-end" style="gap: 6px;">
                <v-btn size="small" variant="outlined" prepend-icon="mdi-magnify" @click="openTemplate(row)">Open</v-btn>
                <v-btn size="small" color="error" variant="outlined" prepend-icon="mdi-delete" @click="deleteTemplate(row)">Delete</v-btn>
              </div>
            </td>
          </tr>
        </tbody>
      </v-table>
    </v-card>

    <!-- Slide-out Drawer for Template Editor -->
    <v-navigation-drawer v-model="drawerOpen" location="right" temporary width="1100" style="z-index: 1005;">
      <div v-if="detailLoading" class="fill-height d-flex justify-center align-center">
        <v-progress-circular indeterminate color="primary" />
      </div>
      <div v-else class="pa-4 fill-height d-flex flex-column">
        <div class="d-flex justify-space-between align-center mb-4">
          <div class="text-h6 font-weight-bold">{{ isEditing ? form.name || 'Template' : 'New Template' }}</div>
          <div class="d-flex align-center" style="gap: 8px;">
            <v-btn-toggle v-model="mode" mandatory color="primary" density="compact" class="mr-2">
              <v-btn value="visual" class="text-none">Visual</v-btn>
              <v-btn value="yaml" class="text-none">YAML</v-btn>
            </v-btn-toggle>
            <v-btn color="primary" :loading="saving" prepend-icon="mdi-content-save" variant="flat" size="small" class="text-none font-weight-bold" @click="saveTemplate">Save</v-btn>
            <v-btn icon="mdi-close" variant="text" size="small" @click="drawerOpen = false" />
          </div>
        </div>
        <v-divider />

        <div class="template-editor-shell flex-grow-1 overflow-hidden mt-4">
          <!-- Main Editor Panel (Left) -->
          <main class="editor-main">
            <!-- Metadata Section -->
            <v-card variant="outlined" class="pa-4 mb-4">
              <div class="text-subtitle-2 font-weight-bold mb-3">Template Metadata</div>
              <v-alert v-if="editorError" type="error" variant="tonal" class="mb-3">{{ editorError }}</v-alert>
              <div class="basic-grid">
                <v-text-field v-model="form.name" label="Service Name" placeholder="postgres" variant="outlined" density="comfortable" @change="syncYamlFromVisual" />
                <v-text-field v-model="form.description" label="Description" placeholder="Shared database container" variant="outlined" density="comfortable" />
              </div>
            </v-card>

            <!-- Compose Configuration Card -->
            <v-card variant="outlined" class="mb-4">
              <v-card-item class="bg-surface-variant py-2">
                <div class="d-flex justify-space-between align-center">
                  <v-card-title class="text-subtitle-1 font-weight-bold my-0 py-0">Compose Configuration</v-card-title>
                  <v-btn-toggle v-model="mode" mandatory color="primary" density="compact">
                    <v-btn value="visual" class="text-none">Visual</v-btn>
                    <v-btn value="yaml" class="text-none">YAML</v-btn>
                  </v-btn-toggle>
                </div>
              </v-card-item>
              <v-divider />

              <v-card-text class="pa-4">
                <template v-if="mode === 'visual'">
                  <div class="visual-sections">
                    <!-- General Settings -->
                    <section class="config-section">
                      <div class="text-subtitle-2 font-weight-bold mb-3">Basic</div>
                      <div class="compose-grid pt-2">
                        <v-text-field v-model="templateService.image" label="Docker Image" placeholder="nginx:latest" variant="outlined" density="comfortable" @change="syncYamlFromVisual" />
                        <v-text-field v-model="templateService.build" label="Build Context Path" placeholder="./app" variant="outlined" density="comfortable" @change="syncYamlFromVisual" />
                        <v-select v-model="templateService.restart" :items="['no', 'always', 'on-failure', 'unless-stopped']" label="Restart Policy" variant="outlined" density="comfortable" clearable @update:model-value="syncYamlFromVisual" />
                        <v-text-field v-model="templateService.command" label="Command" variant="outlined" density="comfortable" @change="syncYamlFromVisual" />
                        <v-text-field v-model="templateService.entrypoint" label="Entrypoint" variant="outlined" density="comfortable" @change="syncYamlFromVisual" />
                        <v-text-field v-model="templateService.workingDir" label="Working Dir" placeholder="/app" variant="outlined" density="comfortable" @change="syncYamlFromVisual" />
                        <v-text-field v-model="templateService.user" label="User Override" placeholder="{{ .server.user }}" variant="outlined" density="comfortable" @change="syncYamlFromVisual" />
                        <v-text-field v-model="templateService.hostname" label="Hostname" variant="outlined" density="comfortable" @change="syncYamlFromVisual" />
                        <v-select v-model="templateService.pullPolicy" :items="['always', 'missing', 'never', 'build']" label="Pull Policy" variant="outlined" density="comfortable" clearable @update:model-value="syncYamlFromVisual" />
                        <v-text-field v-model="templateService.stopGracePeriod" label="Stop Grace Period" placeholder="30s" variant="outlined" density="comfortable" @change="syncYamlFromVisual" />

                        <div class="d-flex align-center" style="gap: 20px;">
                          <v-switch v-model="templateService.privileged" label="Privileged" color="primary" density="comfortable" hide-details @change="syncYamlFromVisual" />
                          <v-switch v-model="templateService.init" label="Init Process" color="primary" density="comfortable" hide-details @change="syncYamlFromVisual" />
                        </div>
                      </div>
                    </section>

                    <!-- Ports & Volumes Settings -->
                    <section class="config-section">
                      <div class="pt-2">
                        <div class="d-flex justify-space-between align-center mb-3">
                          <span class="text-subtitle-2 font-weight-bold">Ports</span>
                          <v-btn size="small" prepend-icon="mdi-plus" color="primary" variant="outlined" @click="addPort">Add Port</v-btn>
                        </div>
                        <div class="row-editor mb-6">
                          <div v-for="(row, index) in ports" :key="row.id" class="port-row">
                            <v-text-field v-model="row.host" placeholder="Host (e.g. 80)" density="compact" hide-details variant="outlined" @change="syncYamlFromVisual" />
                            <v-text-field v-model="row.container" placeholder="Container (e.g. 80)" density="compact" hide-details variant="outlined" @change="syncYamlFromVisual" />
                            <v-select v-model="row.protocol" :items="['tcp', 'udp']" density="compact" hide-details variant="outlined" @update:model-value="syncYamlFromVisual" />
                            <v-btn icon="mdi-delete" color="error" variant="text" size="small" @click="removeRow(ports, index)" />
                          </div>
                          <div v-if="ports.length === 0" class="text-caption text-grey text-center py-2">No ports defined</div>
                        </div>

                        <v-divider class="my-4" />

                        <div class="d-flex justify-space-between align-center mb-3">
                          <span class="text-subtitle-2 font-weight-bold">Volumes</span>
                          <v-btn size="small" prepend-icon="mdi-plus" color="primary" variant="outlined" @click="addVolume">Add Volume</v-btn>
                        </div>
                        <div class="row-editor">
                          <div v-for="(row, index) in volumes" :key="row.id" class="volume-row">
                            <v-text-field v-model="row.source" placeholder="Source: {{ .data_dir }}/db" density="compact" hide-details variant="outlined" @change="syncYamlFromVisual" />
                            <v-text-field v-model="row.target" placeholder="Target: /var/lib/postgresql/data" density="compact" hide-details variant="outlined" @change="syncYamlFromVisual" />
                            <v-select v-model="row.mode" :items="['', 'ro', 'rw']" placeholder="Mode" density="compact" hide-details variant="outlined" @update:model-value="syncYamlFromVisual" />
                            <v-btn icon="mdi-delete" color="error" variant="text" size="small" @click="removeRow(volumes, index)" />
                          </div>
                          <div v-if="volumes.length === 0" class="text-caption text-grey text-center py-2">No volumes defined</div>
                        </div>
                      </div>
                    </section>

                    <!-- Environment & Labels Settings -->
                    <section class="config-section">
                      <div class="pt-2">
                        <div class="mb-6">
                          <div class="d-flex justify-space-between align-center mb-3">
                            <span class="text-subtitle-2 font-weight-bold">Environment Variables</span>
                            <v-btn size="small" prepend-icon="mdi-plus" color="primary" variant="outlined" @click="addKeyValue(environment)">Add ENV</v-btn>
                          </div>
                          <div v-for="(row, index) in environment" :key="row.id" class="kv-row">
                            <v-text-field v-model="row.key" placeholder="KEY" density="compact" hide-details variant="outlined" @change="syncYamlFromVisual" />
                            <v-text-field v-model="row.value" placeholder="value or {{ .var }}" density="compact" hide-details variant="outlined" @change="syncYamlFromVisual" />

                            <v-menu>
                              <template v-slot:activator="{ props }">
                                <v-btn icon="mdi-code-braces" variant="outlined" density="comfortable" size="small" v-bind="props" />
                              </template>
                              <v-list density="compact" max-height="300">
                                <v-list-item v-for="item in builtIns" :key="item" :title="item" @click="insertVariable(row, item)" />
                              </v-list>
                            </v-menu>

                            <v-btn icon="mdi-delete" color="error" variant="text" size="small" @click="removeRow(environment, index)" />
                          </div>
                          <div v-if="environment.length === 0" class="text-caption text-grey text-center py-2">No environment variables defined</div>
                        </div>

                        <v-divider class="my-4" />

                        <div class="mb-6">
                          <div class="d-flex justify-space-between align-center mb-3">
                            <span class="text-subtitle-2 font-weight-bold">Docker Labels</span>
                            <v-btn size="small" prepend-icon="mdi-plus" color="primary" variant="outlined" @click="addKeyValue(labels)">Add Label</v-btn>
                          </div>
                          <div v-for="(row, index) in labels" :key="row.id" class="kv-row">
                            <v-text-field v-model="row.key" placeholder="label.key" density="compact" hide-details variant="outlined" @change="syncYamlFromVisual" />
                            <v-text-field v-model="row.value" placeholder="value" density="compact" hide-details variant="outlined" @change="syncYamlFromVisual" />
                            <v-btn icon="mdi-delete" color="error" variant="text" size="small" @click="removeRow(labels, index)" />
                          </div>
                          <div v-if="labels.length === 0" class="text-caption text-grey text-center py-2">No labels defined</div>
                        </div>

                        <v-divider class="my-4" />

                        <div>
                          <div class="d-flex justify-space-between align-center mb-3">
                            <span class="text-subtitle-2 font-weight-bold">Env files</span>
                            <v-btn size="small" prepend-icon="mdi-plus" color="primary" variant="outlined" @click="addKeyValue(envFiles)">Add File</v-btn>
                          </div>
                          <div v-for="(row, index) in envFiles" :key="row.id" class="single-row">
                            <v-text-field v-model="row.key" placeholder="./.env" density="compact" hide-details variant="outlined" @change="syncYamlFromVisual" />
                            <v-btn icon="mdi-delete" color="error" variant="text" size="small" @click="removeRow(envFiles, index)" />
                          </div>
                          <div v-if="envFiles.length === 0" class="text-caption text-grey text-center py-2">No env files defined</div>
                        </div>
                      </div>
                    </section>

                    <!-- Network & DNS Settings -->
                    <section class="config-section">
                      <div class="pt-2">
                        <div class="text-subtitle-2 font-weight-bold mb-3">Network & DNS</div>
                        <div class="mb-4">
                          <v-combobox v-model="templateService.networkMode" :items="['bridge', 'host', 'none']" label="Network Mode" variant="outlined" density="comfortable" clearable @update:model-value="syncYamlFromVisual" />
                        </div>

                        <v-divider class="my-4" />

                        <div class="mb-6">
                          <div class="d-flex justify-space-between align-center mb-3">
                            <span class="text-subtitle-2 font-weight-bold">Networks</span>
                            <v-btn size="small" prepend-icon="mdi-plus" color="primary" variant="outlined" @click="addKeyValue(networks)">Add Network</v-btn>
                          </div>
                          <div v-for="(row, index) in networks" :key="row.id" class="single-row">
                            <v-text-field v-model="row.key" placeholder="frontend" density="compact" hide-details variant="outlined" @change="syncYamlFromVisual" />
                            <v-btn icon="mdi-delete" color="error" variant="text" size="small" @click="removeRow(networks, index)" />
                          </div>
                          <div v-if="networks.length === 0" class="text-caption text-grey text-center py-2">No networks defined</div>
                        </div>

                        <v-divider class="my-4" />

                        <div class="mb-6">
                          <div class="d-flex justify-space-between align-center mb-3">
                            <span class="text-subtitle-2 font-weight-bold">Extra hosts</span>
                            <v-btn size="small" prepend-icon="mdi-plus" color="primary" variant="outlined" @click="addKeyValue(extraHosts)">Add Host</v-btn>
                          </div>
                          <div v-for="(row, index) in extraHosts" :key="row.id" class="single-row">
                            <v-text-field v-model="row.key" placeholder="host.docker.internal:host-gateway" density="compact" hide-details variant="outlined" @change="syncYamlFromVisual" />
                            <v-btn icon="mdi-delete" color="error" variant="text" size="small" @click="removeRow(extraHosts, index)" />
                          </div>
                          <div v-if="extraHosts.length === 0" class="text-caption text-grey text-center py-2">No extra hosts defined</div>
                        </div>

                        <v-divider class="my-4" />

                        <div class="kv-groups">
                          <div>
                            <div class="d-flex justify-space-between align-center mb-3">
                              <span class="text-subtitle-2 font-weight-bold">DNS Servers</span>
                              <v-btn size="small" prepend-icon="mdi-plus" color="primary" variant="outlined" @click="addKeyValue(dns)">Add DNS</v-btn>
                            </div>
                            <div v-for="(row, index) in dns" :key="row.id" class="single-row">
                              <v-text-field v-model="row.key" placeholder="1.1.1.1" density="compact" hide-details variant="outlined" @change="syncYamlFromVisual" />
                              <v-btn icon="mdi-delete" color="error" variant="text" size="small" @click="removeRow(dns, index)" />
                            </div>
                            <div v-if="dns.length === 0" class="text-caption text-grey text-center py-2">No DNS servers defined</div>
                          </div>

                          <div>
                            <div class="d-flex justify-space-between align-center mb-3">
                              <span class="text-subtitle-2 font-weight-bold">DNS Search Domains</span>
                              <v-btn size="small" prepend-icon="mdi-plus" color="primary" variant="outlined" @click="addKeyValue(dnsSearch)">Add Search</v-btn>
                            </div>
                            <div v-for="(row, index) in dnsSearch" :key="row.id" class="single-row">
                              <v-text-field v-model="row.key" placeholder="svc.local" density="compact" hide-details variant="outlined" @change="syncYamlFromVisual" />
                              <v-btn icon="mdi-delete" color="error" variant="text" size="small" @click="removeRow(dnsSearch, index)" />
                            </div>
                            <div v-if="dnsSearch.length === 0" class="text-caption text-grey text-center py-2">No DNS search domains defined</div>
                          </div>
                        </div>
                      </div>
                    </section>

                    <!-- Healthcheck Settings -->
                    <section class="config-section">
                      <div class="text-subtitle-2 font-weight-bold mb-3">Healthcheck</div>
                      <div class="compose-grid pt-2">
                        <v-text-field v-model="templateService.healthcheckTest" label="Test Command" placeholder="CMD curl -f http://localhost/ || exit 1" variant="outlined" density="comfortable" @change="syncYamlFromVisual" />
                        <v-text-field v-model="templateService.healthcheckInterval" label="Interval" placeholder="30s" variant="outlined" density="comfortable" @change="syncYamlFromVisual" />
                        <v-text-field v-model="templateService.healthcheckTimeout" label="Timeout" placeholder="10s" variant="outlined" density="comfortable" @change="syncYamlFromVisual" />
                        <v-text-field v-model.number="templateService.healthcheckRetries" type="number" label="Retries" min="0" variant="outlined" density="comfortable" @change="syncYamlFromVisual" />
                      </div>
                    </section>
                  </div>
                </template>
                <template v-else>
                  <div class="d-flex justify-end mb-3">
                    <v-btn prepend-icon="mdi-pencil" size="small" variant="outlined" @click="syncVisualFromYaml">Update Visual Fields</v-btn>
                  </div>
                  <v-textarea
                    v-model="form.composeYaml"
                    variant="outlined"
                    rows="22"
                    spellcheck="false"
                    class="font-mono"
                    @update:model-value="syncVisualFromYaml(false)"
                  />
                </template>
              </v-card-text>
            </v-card>

            <!-- Template Variables Section -->
            <v-card variant="outlined" class="pa-4 mb-4">
              <div class="d-flex justify-space-between align-center mb-3">
                <span class="text-subtitle-2 font-weight-bold">Template Variables</span>
                <v-btn size="small" prepend-icon="mdi-plus" variant="outlined" @click="addVariable">Add Variable</v-btn>
              </div>
              <v-table density="compact" style="background: transparent;">
                <thead>
                  <tr>
                    <th class="font-weight-bold text-left">Name</th>
                    <th class="font-weight-bold text-left" style="width: 150px;">Type</th>
                    <th class="font-weight-bold text-left">Default Value</th>
                    <th class="font-weight-bold text-center" style="width: 100px;">Required</th>
                    <th class="text-right" style="width: 60px;"></th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-if="form.variables.length === 0">
                    <td colspan="5" class="text-center py-4 text-grey text-caption">No variables defined</td>
                  </tr>
                  <tr v-for="(row, index) in form.variables" :key="index">
                    <td class="py-1">
                      <v-text-field v-model="row.name" density="compact" hide-details variant="outlined" />
                    </td>
                    <td class="py-1">
                      <v-select v-model="row.type" :items="['string', 'number', 'boolean', 'secret']" density="compact" hide-details variant="outlined" />
                    </td>
                    <td class="py-1">
                      <v-text-field v-model="row.defaultValue" density="compact" hide-details variant="outlined" />
                    </td>
                    <td class="py-1 text-center">
                      <v-checkbox-btn v-model="row.required" color="primary" class="d-inline-flex" />
                    </td>
                    <td class="py-1 text-right">
                      <v-btn icon="mdi-delete" color="error" variant="text" size="small" @click="removeVariable(index)" />
                    </td>
                  </tr>
                </tbody>
              </v-table>
            </v-card>

            <!-- Dependencies Section -->
            <v-card variant="outlined" class="pa-4 mb-4">
              <div class="text-subtitle-2 font-weight-bold mb-3">Dependencies</div>
              <v-select
                v-model="form.dependencies"
                :items="dependencyOptions"
                item-title="name"
                item-value="id"
                label="Templates to deploy first"
                multiple
                chips
                closable-chips
                variant="outlined"
                density="comfortable"
                class="mb-3"
              />
              <div class="dependency-list mb-2">
                <v-chip v-for="template in selectedDependencies" :key="template.id" color="warning" size="small" label>{{ template.name }}</v-chip>
                <span v-if="selectedDependencies.length === 0" class="text-caption text-grey">No dependencies selected</span>
              </div>
              <v-alert v-slot:prepend v-if="dependencyRisk" type="error" variant="tonal" class="mt-2" density="compact">
                {{ dependencyRisk }}
              </v-alert>
            </v-card>

            <!-- Template Files Section -->
            <v-card variant="outlined" class="pa-4">
              <div class="d-flex justify-space-between align-center mb-3">
                <span class="text-subtitle-2 font-weight-bold">Template Files</span>
                <v-btn size="small" prepend-icon="mdi-plus" variant="outlined" :disabled="!isEditing" @click="openFile()">Add File</v-btn>
              </div>
              <v-table density="compact" style="background: transparent;">
                <thead>
                  <tr>
                    <th class="font-weight-bold text-left">Path</th>
                    <th class="font-weight-bold text-left" style="width: 120px;">Kind</th>
                    <th class="font-weight-bold text-left" style="width: 120px;">Size</th>
                    <th class="font-weight-bold text-right" style="width: 160px;">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-if="files.length === 0">
                    <td colspan="4" class="text-center py-4 text-grey text-caption">
                      {{ isEditing ? 'No template files attached' : 'Save the template to attach files' }}
                    </td>
                  </tr>
                  <tr v-for="row in files" :key="row.id">
                    <td class="font-weight-bold">{{ row.path }}</td>
                    <td>
                      <v-chip size="x-small" label>{{ row.kind }}</v-chip>
                    </td>
                    <td>{{ row.sizeBytes ?? '-' }} bytes</td>
                    <td class="text-right">
                      <div class="d-flex justify-end" style="gap: 4px;">
                        <v-btn size="small" variant="outlined" @click="openFile(row)">Edit</v-btn>
                        <v-btn size="small" color="error" variant="outlined" @click="deleteFile(row)">Delete</v-btn>
                      </div>
                    </td>
                  </tr>
                </tbody>
              </v-table>
            </v-card>
          </main>

          <!-- Preview & Variable Sidebar (Right) -->
          <aside class="preview-side">
            <!-- Preview Section -->
            <v-card variant="outlined" class="pa-4 mb-4">
              <div class="d-flex justify-space-between align-center mb-3">
                <span class="text-subtitle-2 font-weight-bold">Preview Renderer</span>
                <v-btn :disabled="!isEditing" :loading="rendering" prepend-icon="mdi-sync" size="small" variant="outlined" @click="renderPreview">Render</v-btn>
              </div>
              <v-select
                v-model="previewServerId"
                :items="servers"
                item-title="name"
                item-value="id"
                label="Target Server (for variables)"
                placeholder="Preview server"
                variant="outlined"
                density="comfortable"
                class="mb-3"
                clearable
              />
              <v-alert v-if="previewError" type="error" variant="tonal" class="mb-3" density="compact">{{ previewError }}</v-alert>
              <pre class="code-preview mb-4">{{ preview?.renderedYaml || form.composeYaml }}</pre>

              <div class="text-subtitle-2 font-weight-bold mb-2">Rendered Files</div>
              <v-table density="compact" class="border rounded" style="background: transparent;">
                <thead>
                  <tr>
                    <th class="font-weight-bold text-left text-caption">Path</th>
                    <th class="font-weight-bold text-left text-caption" style="width: 80px;">Kind</th>
                    <th class="font-weight-bold text-left text-caption" style="width: 80px;">Size</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-if="previewFiles.length === 0">
                    <td colspan="3" class="text-center py-3 text-grey text-caption">No files rendered</td>
                  </tr>
                  <tr v-for="pf in previewFiles" :key="pf.path">
                    <td class="text-caption font-weight-bold text-truncate" style="max-width: 140px;" :title="pf.path">{{ pf.path }}</td>
                    <td class="text-caption">{{ pf.kind }}</td>
                    <td class="text-caption">{{ pf.sizeBytes ?? '-' }} bytes</td>
                  </tr>
                </tbody>
              </v-table>
            </v-card>

            <!-- Built-in Variables Checklist -->
            <v-card variant="outlined" class="pa-4">
              <div class="text-subtitle-2 font-weight-bold mb-2">Built-in Variables</div>
              <div class="text-caption text-grey mb-3">Click to copy variable placeholder</div>
              <div class="variable-list">
                <v-btn
                  v-for="item in builtIns"
                  :key="item"
                  variant="outlined"
                  size="small"
                  block
                  class="justify-space-between text-none font-mono py-2"
                  style="text-transform: none; text-align: left; height: auto;"
                  @click="copyBuiltIn(item)"
                >
                  <span class="text-truncate">{{ item }}</span>
                  <v-icon size="x-small" class="ml-2">mdi-content-copy</v-icon>
                </v-btn>
              </div>
            </v-card>
          </aside>
        </div>
      </div>
    </v-navigation-drawer>

    <!-- File Dialog -->
    <v-dialog v-model="fileDialogOpen" max-width="620px" scrollable>
      <v-card>
        <v-card-title class="bg-surface-variant py-3 font-weight-bold">
          Template File
        </v-card-title>
        <v-divider />
        <v-card-text class="pa-4">
          <v-form @submit.prevent="saveFile">
            <div class="text-subtitle-2 mb-2">File Kind</div>
            <v-btn-toggle v-model="fileForm.kind" mandatory color="primary" density="compact" :disabled="Boolean(fileForm.id)" class="mb-4">
              <v-btn value="template" class="text-none">Template</v-btn>
              <v-btn value="binary" class="text-none">Binary</v-btn>
            </v-btn-toggle>

            <v-text-field v-model="fileForm.path" label="Destination Path" placeholder="config/{{ .server.name }}/app.env" variant="outlined" density="comfortable" class="mb-3" />

            <v-textarea
              v-if="fileForm.kind === 'template'"
              v-model="fileForm.content"
              label="Content"
              placeholder="Paste template file content here"
              variant="outlined"
              density="comfortable"
              rows="14"
              class="font-mono"
              spellcheck="false"
            />
            <div v-else class="border rounded pa-4 d-flex flex-column align-center justify-center bg-grey-lighten-5">
              <v-icon size="40" color="grey" class="mb-2">mdi-upload</v-icon>
              <div class="text-body-2 font-weight-bold mb-2">
                {{ fileForm.pickedName || 'Select a binary file to upload' }}
              </div>
              <v-file-input
                label="Choose file"
                variant="outlined"
                density="compact"
                hide-details
                prepend-icon="mdi-file-upload"
                @change="handleBinaryInput"
                style="width: 240px;"
              />
            </div>
          </v-form>
        </v-card-text>
        <v-divider />
        <v-card-actions class="pa-3 bg-surface-variant">
          <v-spacer />
          <v-btn variant="outlined" class="text-none font-weight-bold" @click="fileDialogOpen = false">Cancel</v-btn>
          <v-btn color="primary" variant="flat" class="text-none font-weight-bold" @click="saveFile">Save File</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- Confirmation Dialog -->
    <v-dialog v-model="confirmDialog" max-width="450px">
      <v-card>
        <v-card-title class="bg-surface-variant py-3 font-weight-bold">
          {{ confirmTitle }}
        </v-card-title>
        <v-divider />
        <v-card-text class="pa-4 text-body-1">
          {{ confirmMessage }}
        </v-card-text>
        <v-divider />
        <v-card-actions class="pa-3 bg-surface-variant">
          <v-spacer />
          <v-btn variant="outlined" class="text-none font-weight-bold" @click="confirmDialog = false">Cancel</v-btn>
          <v-btn color="error" variant="flat" class="text-none font-weight-bold" @click="executeConfirm">Confirm</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- Global Snackbar -->
    <v-snackbar v-model="snackbar" :color="snackbarColor" timeout="3000">
      {{ snackbarText }}
      <template v-slot:actions>
        <v-btn color="white" variant="text" @click="snackbar = false">Close</v-btn>
      </template>
    </v-snackbar>
  </div>
</template>

<style scoped>
.font-mono {
  font-family: monospace !important;
}

.template-editor-shell {
  display: grid;
  grid-template-columns: minmax(680px, 1fr) 380px;
  gap: 24px;
  height: 100%;
  min-height: 0;
}

.editor-main,
.preview-side {
  display: flex;
  flex-direction: column;
  gap: 20px;
  min-height: 0;
  overflow-y: auto;
  padding-right: 8px;
}

.editor-main > *,
.preview-side > * {
  flex-shrink: 0;
}

.preview-side {
  position: sticky;
  top: 0;
}

.basic-grid,
.compose-grid,
.kv-groups {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 16px;
  align-items: start;
}

.visual-sections {
  display: grid;
  gap: 18px;
}

.config-section {
  border-bottom: 1px solid rgba(var(--v-border-color), 0.12);
  padding-bottom: 18px;
}

.config-section:last-child {
  border-bottom: 0;
  padding-bottom: 0;
}

.field-cell {
  display: grid;
  gap: 6px;
  color: #606266;
  font-size: 14px;
}

.port-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) 100px 48px;
  gap: 8px;
  align-items: center;
  margin-bottom: 8px;
}

.volume-row {
  display: grid;
  grid-template-columns: minmax(0, 1.3fr) minmax(0, 1fr) 90px 48px;
  gap: 8px;
  align-items: center;
  margin-bottom: 8px;
}

.kv-row {
  display: grid;
  grid-template-columns: minmax(0, 0.9fr) minmax(0, 1.1fr) 40px 40px;
  gap: 8px;
  align-items: center;
  margin-bottom: 8px;
}

.single-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 48px;
  gap: 8px;
  align-items: center;
  margin-bottom: 8px;
}

.dependency-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.variable-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.code-preview {
  min-height: 360px;
  max-height: calc(100vh - 360px);
  overflow: auto;
  margin: 0;
  border: 1px solid rgba(var(--v-border-color), 0.12);
  border-radius: 8px;
  background: #1e1e1e;
  color: #d4d4d4;
  padding: 12px;
  font-family: monospace;
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
}

@media (max-width: 1280px) {
  .template-editor-shell {
    grid-template-columns: 1fr;
    height: auto;
    overflow-y: visible;
  }
  .editor-main, .preview-side {
    overflow-y: visible;
  }
}
</style>
