<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue';
import { containerServicesApi } from '@/api/containerServices';
import { serversApi } from '@/api/servers';
import type {
  ContainerServiceDto,
  ContainerServiceFileDto,
  ContainerServiceInputDto,
  ContainerServiceValidationResultDto,
  RenderPreviewDto,
  SchedulePreviewDto,
  ServerDto,
} from '@/types/api';
import { serviceBodyYamlToVisual, visualToServiceBodyYaml, type ServiceVisualModel } from './serviceBodyYaml';

type KeyValueRow = { key: string; value: string };

const props = defineProps<{
  service: ContainerServiceDto | null;
  open: boolean;
}>();

const emit = defineEmits<{
  close: [];
  saved: [service: ContainerServiceDto];
}>();

const form = reactive({
  name: '',
  enabled: false,
  composeServiceYaml: 'image: nginx:latest\nrestart: unless-stopped\n',
  selectorRows: [] as KeyValueRow[],
});
const visual = reactive<ServiceVisualModel>({ image: '', restart: '', ports: [], volumes: [], environment: {}, labels: {}, dependsOn: [] });
const commandMode = ref<'list' | 'string'>('list');
const commandText = ref('');
const commandItems = ref<string[]>([]);
const portRows = ref<string[]>([]);
const volumeRows = ref<string[]>([]);
const envRows = ref<KeyValueRow[]>([]);
const labelRows = ref<KeyValueRow[]>([]);
const files = ref<ContainerServiceFileDto[]>([]);
const servers = ref<ServerDto[]>([]);
const validation = ref<ContainerServiceValidationResultDto | null>(null);
const renderPreview = ref<RenderPreviewDto | null>(null);
const schedulePreview = ref<SchedulePreviewDto | null>(null);
const loading = ref(false);
const previewLoading = ref('');
const filesLoading = ref(false);
const error = ref('');
const editorMode = ref<'visual' | 'yaml'>('visual');
const fileDialog = ref(false);
const editingFile = ref<ContainerServiceFileDto | null>(null);
const fileForm = reactive({ path: '', content: '' });
let previewTimer: ReturnType<typeof setTimeout> | null = null;

const restartOptions = ['', 'no', 'always', 'on-failure', 'unless-stopped'];
const networkModeOptions = ['', 'bridge', 'host', 'none', 'service:', 'container:'];

const isCreate = computed(() => !props.service?.id);
const dependencyOptions = computed(() => {
  const names = new Set<string>();
  props.service?.dependencyNames?.forEach((name) => names.add(name));
  props.service?.dependentNames?.forEach((name) => names.add(name));
  visual.dependsOn?.forEach((name) => names.add(name));
  if (props.service?.name) names.delete(props.service.name);
  return Array.from(names).sort((a, b) => a.localeCompare(b));
});
const traitKeyOptions = computed(() => {
  const keys = new Set<string>();
  for (const server of servers.value) {
    Object.keys(server.traits ?? {}).forEach((key) => keys.add(key));
  }
  return Array.from(keys).sort((a, b) => a.localeCompare(b));
});
const canManageFiles = computed(() => Boolean(props.service?.id));

function rowsFromMap(values?: Record<string, string>) {
  return Object.entries(values ?? {}).map(([key, value]) => ({ key, value }));
}

function rowsToMap(rows: KeyValueRow[]) {
  const out: Record<string, string> = {};
  for (const row of rows) {
    const key = row.key.trim();
    if (key) out[key] = row.value.trim();
  }
  return out;
}

function traitValueOptions(key: string) {
  const values = new Set<string>();
  for (const server of servers.value) {
    const value = server.traits?.[key];
    if (value) values.add(value);
  }
  return Array.from(values).sort((a, b) => a.localeCompare(b));
}

function syncStructuredRowsFromVisual() {
  portRows.value = [...(visual.ports ?? [])];
  volumeRows.value = [...(visual.volumes ?? [])];
  envRows.value = rowsFromMap(visual.environment);
  labelRows.value = rowsFromMap(visual.labels);
  if (Array.isArray(visual.command)) {
    commandMode.value = 'list';
    commandItems.value = [...visual.command];
    commandText.value = '';
  } else {
    commandMode.value = visual.command ? 'string' : 'list';
    commandText.value = visual.command ?? '';
    commandItems.value = [];
  }
}

function syncStructuredRowsToVisual() {
  visual.ports = portRows.value.filter(Boolean);
  visual.volumes = volumeRows.value.filter(Boolean);
  visual.environment = rowsToMap(envRows.value);
  visual.labels = rowsToMap(labelRows.value);
  visual.command = commandMode.value === 'list' ? commandItems.value.filter(Boolean) : commandText.value.trim();
}

function syncFromService() {
  const service = props.service;
  editorMode.value = 'visual';
  form.name = service?.name ?? '';
  form.enabled = service?.enabled ?? false;
  form.composeServiceYaml = service?.composeServiceYaml ?? 'image: nginx:latest\nrestart: unless-stopped\n';
  form.selectorRows = rowsFromMap(service?.selector);
  Object.assign(visual, serviceBodyYamlToVisual(form.composeServiceYaml));
  syncStructuredRowsFromVisual();
  validation.value = null;
  renderPreview.value = null;
  schedulePreview.value = null;
  error.value = '';
  if (service?.id) void loadFiles(service.id);
  else files.value = [];
  scheduleAutoPreview();
}

function currentComposeServiceYaml() {
  if (editorMode.value === 'visual') {
    syncStructuredRowsToVisual();
    return visualToServiceBodyYaml(visual);
  }
  return form.composeServiceYaml;
}

function input(): ContainerServiceInputDto {
  return {
    name: isCreate.value ? form.name.trim() : props.service?.name,
    enabled: form.enabled,
    composeServiceYaml: currentComposeServiceYaml(),
    variables: {},
    selector: rowsToMap(form.selectorRows),
  };
}

async function loadServers() {
  try {
    servers.value = await serversApi.listServers();
  } catch {
    servers.value = [];
  }
}

async function loadFiles(serviceId: string) {
  filesLoading.value = true;
  try {
    files.value = await containerServicesApi.listFiles(serviceId);
  } catch {
    files.value = [];
  } finally {
    filesLoading.value = false;
  }
}

function syncVisualFromYaml() {
  Object.assign(visual, serviceBodyYamlToVisual(form.composeServiceYaml));
  syncStructuredRowsFromVisual();
}

async function runValidate() {
  previewLoading.value = 'validate';
  try {
    validation.value = await containerServicesApi.validate(props.service?.id || 'draft', input());
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to validate service';
  } finally {
    previewLoading.value = '';
  }
}

async function runRenderPreview() {
  previewLoading.value = 'render';
  try {
    renderPreview.value = await containerServicesApi.renderPreview(props.service?.id || 'draft', input());
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to render preview';
  } finally {
    previewLoading.value = '';
  }
}

async function runSchedulePreview() {
  previewLoading.value = 'schedule';
  try {
    schedulePreview.value = await containerServicesApi.schedulePreview(props.service?.id || 'draft', input());
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to preview schedule';
  } finally {
    previewLoading.value = '';
  }
}

function scheduleAutoPreview() {
  if (!props.open) return;
  if (previewTimer) clearTimeout(previewTimer);
  previewTimer = setTimeout(async () => {
    await runValidate();
    await runSchedulePreview();
  }, 350);
}

async function save() {
  loading.value = true;
  try {
    const saved = props.service?.id
      ? await containerServicesApi.update(props.service.id, input())
      : await containerServicesApi.create(input());
    emit('saved', saved);
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to save service';
  } finally {
    loading.value = false;
  }
}

function addSelectorRow() {
  form.selectorRows.push({ key: traitKeyOptions.value[0] ?? '', value: '' });
}

function addPort() {
  portRows.value.push('');
}

function removePort(index: number) {
  portRows.value.splice(index, 1);
}

function addVolume() {
  volumeRows.value.push('');
}

function removeVolume(index: number) {
  volumeRows.value.splice(index, 1);
}

function addCommandItem() {
  commandItems.value.push('');
}

function removeCommandItem(index: number) {
  commandItems.value.splice(index, 1);
}

function addEnvRow() {
  envRows.value.push({ key: '', value: '' });
}

function addLabelRow() {
  labelRows.value.push({ key: '', value: '' });
}

function removeRow(rows: KeyValueRow[], index: number) {
  rows.splice(index, 1);
}

function newTemplateFile() {
  editingFile.value = null;
  fileForm.path = '';
  fileForm.content = '';
  fileDialog.value = true;
}

function editTemplateFile(file: ContainerServiceFileDto) {
  editingFile.value = file;
  fileForm.path = file.path;
  fileForm.content = file.content ?? '';
  fileDialog.value = true;
}

async function saveTemplateFile() {
  if (!props.service?.id) return;
  filesLoading.value = true;
  try {
    const payload = { path: fileForm.path.trim(), kind: 'template' as const, content: fileForm.content, contentType: 'text/plain; charset=utf-8' };
    if (editingFile.value?.id) await containerServicesApi.updateFile(props.service.id, editingFile.value.id, payload);
    else await containerServicesApi.createFile(props.service.id, payload);
    await loadFiles(props.service.id);
    fileDialog.value = false;
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to save file';
  } finally {
    filesLoading.value = false;
  }
}

async function deleteFile(file: ContainerServiceFileDto) {
  if (!props.service?.id) return;
  filesLoading.value = true;
  try {
    await containerServicesApi.deleteFile(props.service.id, file.id);
    await loadFiles(props.service.id);
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to delete file';
  } finally {
    filesLoading.value = false;
  }
}

async function uploadBinary(filesInput: File | File[] | null) {
  if (!props.service?.id || !filesInput) return;
  const file = Array.isArray(filesInput) ? filesInput[0] : filesInput;
  if (!file) return;
  filesLoading.value = true;
  try {
    await containerServicesApi.createFile(props.service.id, {
      path: file.name,
      kind: 'binary',
      base64Content: await readBase64(file),
      contentType: file.type || 'application/octet-stream',
    });
    await loadFiles(props.service.id);
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to upload file';
  } finally {
    filesLoading.value = false;
  }
}

function readBase64(file: File) {
  return new Promise<string>((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result ?? '').split(',')[1] ?? '');
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(file);
  });
}

onMounted(() => {
  void loadServers();
});

watch(() => props.open, (open) => {
  if (open) {
    syncFromService();
    void loadServers();
  }
});
watch(() => props.service?.id, syncFromService);
watch(editorMode, (mode, previousMode) => {
  if (mode === previousMode) return;
  if (mode === 'yaml') {
    form.composeServiceYaml = visualToServiceBodyYaml(visual);
  } else {
    syncVisualFromYaml();
  }
});
watch(
  [() => form.name, () => form.enabled, () => form.composeServiceYaml, () => editorMode.value, () => visual, () => form.selectorRows, commandMode, commandText, commandItems, portRows, volumeRows, envRows, labelRows],
  scheduleAutoPreview,
  { deep: true },
);
</script>

<template>
  <v-navigation-drawer :model-value="open" location="right" temporary width="820" style="z-index: 1005;" @update:model-value="!$event && emit('close')">
    <div class="editor-shell fill-height d-flex flex-column">
      <div class="editor-header">
        <div class="min-width-0">
          <div class="text-h6 font-weight-bold">{{ isCreate ? 'Create Container Service' : service?.name }}</div>
          <div class="text-caption text-medium-emphasis">Compose service body only. Validation and scheduling refresh automatically while you edit.</div>
        </div>
        <div class="editor-actions">
          <v-btn color="primary" variant="flat" size="small" class="text-none font-weight-bold" :loading="loading" @click="save">Save</v-btn>
          <v-btn icon="mdi-close" variant="text" size="small" @click="emit('close')" />
        </div>
      </div>

      <v-alert v-if="error" type="error" variant="tonal" density="compact" class="mb-3">{{ error }}</v-alert>

      <div class="flex-grow-1 overflow-auto editor-body">
        <v-text-field
          v-if="isCreate"
          v-model="form.name"
          label="Name"
          hint="Immutable runtime identity, lowercase letters, digits, and dashes."
          persistent-hint
          variant="outlined"
          density="comfortable"
        />
        <div v-else class="readonly-name">
          <span class="text-caption text-medium-emphasis">Immutable name</span>
          <strong>{{ service?.name }}</strong>
        </div>

        <v-switch v-model="form.enabled" color="primary" label="Enabled" hide-details class="mt-2" />

        <div class="mode-switch-row">
          <div class="section-title">
            <v-icon size="16">mdi-pencil-ruler</v-icon>
            <span>Service Body Editor</span>
          </div>
          <v-btn-toggle v-model="editorMode" mandatory density="compact" color="primary" variant="outlined">
            <v-btn value="visual" class="text-none" prepend-icon="mdi-tune-variant">Visual</v-btn>
            <v-btn value="yaml" class="text-none" prepend-icon="mdi-code-braces">YAML</v-btn>
          </v-btn-toggle>
        </div>

        <div v-if="editorMode === 'visual'" class="editor-section">
          <div class="section-title">
            <v-icon size="16">mdi-tune-variant</v-icon>
            <span>Visual Editor</span>
          </div>
          <div class="visual-grid">
            <v-text-field v-model="visual.image" label="Image" variant="outlined" density="compact" />
            <v-select v-model="visual.restart" :items="restartOptions" label="Restart" variant="outlined" density="compact" clearable />
            <v-select v-model="visual.networkMode" :items="networkModeOptions" label="Network mode" variant="outlined" density="compact" clearable />
            <v-select v-model="visual.dependsOn" :items="dependencyOptions" label="Depends on" multiple chips closable-chips variant="outlined" density="compact" />
          </div>

          <div class="list-block">
            <div class="list-title">
              <span>Command</span>
              <v-btn-toggle v-model="commandMode" mandatory density="compact" variant="outlined">
                <v-btn value="list" size="small" class="text-none">List</v-btn>
                <v-btn value="string" size="small" class="text-none">String</v-btn>
              </v-btn-toggle>
            </div>
            <template v-if="commandMode === 'list'">
              <div v-for="(_, index) in commandItems" :key="`command-${index}`" class="row-line">
                <v-text-field v-model="commandItems[index]" label="Argument" variant="outlined" density="compact" hide-details />
                <v-btn icon="mdi-delete-outline" variant="text" size="small" @click="removeCommandItem(index)" />
              </div>
              <v-btn size="small" variant="outlined" prepend-icon="mdi-plus" class="align-self-start text-none" @click="addCommandItem">Add argument</v-btn>
            </template>
            <v-text-field v-else v-model="commandText" label="Command" variant="outlined" density="compact" hide-details />
          </div>

          <div class="list-block">
            <div class="list-title">
              <span>Ports</span>
              <v-btn icon="mdi-plus" variant="text" size="small" @click="addPort" />
            </div>
            <div v-for="(_, index) in portRows" :key="`port-${index}`" class="row-line">
              <v-text-field v-model="portRows[index]" label="HOST:CONTAINER[/PROTO]" variant="outlined" density="compact" hide-details />
              <v-btn icon="mdi-delete-outline" variant="text" size="small" @click="removePort(index)" />
            </div>
          </div>

          <div class="list-block">
            <div class="list-title">
              <span>Volumes</span>
              <v-btn icon="mdi-plus" variant="text" size="small" @click="addVolume" />
            </div>
            <div v-for="(_, index) in volumeRows" :key="`volume-${index}`" class="row-line">
              <v-text-field v-model="volumeRows[index]" label="SOURCE:TARGET[:MODE]" variant="outlined" density="compact" hide-details />
              <v-btn icon="mdi-delete-outline" variant="text" size="small" @click="removeVolume(index)" />
            </div>
          </div>

          <div class="split-grid">
            <div class="list-block">
              <div class="list-title">
                <span>Environment</span>
                <v-btn icon="mdi-plus" variant="text" size="small" @click="addEnvRow" />
              </div>
              <div v-for="(row, index) in envRows" :key="`env-${index}`" class="row-line kv-row">
                <v-text-field v-model="row.key" label="Key" variant="outlined" density="compact" hide-details />
                <v-text-field v-model="row.value" label="Value" variant="outlined" density="compact" hide-details />
                <v-btn icon="mdi-delete-outline" variant="text" size="small" @click="removeRow(envRows, index)" />
              </div>
            </div>
            <div class="list-block">
              <div class="list-title">
                <span>Docker Labels</span>
                <v-btn icon="mdi-plus" variant="text" size="small" @click="addLabelRow" />
              </div>
              <div v-for="(row, index) in labelRows" :key="`label-${index}`" class="row-line kv-row">
                <v-text-field v-model="row.key" label="Key" variant="outlined" density="compact" hide-details />
                <v-text-field v-model="row.value" label="Value" variant="outlined" density="compact" hide-details />
                <v-btn icon="mdi-delete-outline" variant="text" size="small" @click="removeRow(labelRows, index)" />
              </div>
            </div>
          </div>
        </div>

        <div v-else class="editor-section yaml-section">
          <div class="section-title">
            <v-icon size="16">mdi-code-braces</v-icon>
            <span>YAML Editor</span>
          </div>
          <v-textarea
            v-model="form.composeServiceYaml"
            rows="16"
            spellcheck="false"
            class="font-mono"
            variant="outlined"
            density="compact"
            hide-details
          />
        </div>

        <div class="editor-section">
          <div class="section-title">
            <v-icon size="16">mdi-crosshairs-gps</v-icon>
            <span>Taints</span>
          </div>
          <v-alert v-if="traitKeyOptions.length === 0" type="info" variant="tonal" density="compact">
            No server traits are available yet.
          </v-alert>
          <div v-for="(row, index) in form.selectorRows" :key="`selector-${index}`" class="row-line kv-row">
            <v-select v-model="row.key" :items="traitKeyOptions" label="Trait" variant="outlined" density="compact" hide-details />
            <v-select v-model="row.value" :items="traitValueOptions(row.key)" label="Value" variant="outlined" density="compact" hide-details />
            <v-btn icon="mdi-delete-outline" variant="text" size="small" @click="removeRow(form.selectorRows, index)" />
          </div>
          <v-btn size="small" variant="outlined" prepend-icon="mdi-plus" class="align-self-start text-none" :disabled="traitKeyOptions.length === 0" @click="addSelectorRow">Add taint selector</v-btn>
        </div>

        <div class="editor-section">
          <div class="section-title file-title">
            <span class="d-inline-flex align-center ga-2">
              <v-icon size="16">mdi-file-tree-outline</v-icon>
              <span>Files</span>
            </span>
            <span class="file-actions">
              <v-btn size="small" variant="outlined" prepend-icon="mdi-file-plus-outline" class="text-none" :disabled="!canManageFiles" @click="newTemplateFile">Template</v-btn>
              <v-file-input
                class="binary-upload"
                density="compact"
                variant="outlined"
                hide-details
                prepend-icon=""
                prepend-inner-icon="mdi-upload-outline"
                label="Binary"
                :disabled="!canManageFiles"
                @update:model-value="uploadBinary"
              />
            </span>
          </div>
          <v-alert v-if="!canManageFiles" type="info" variant="tonal" density="compact">Save the service before adding files.</v-alert>
          <v-table density="compact" class="files-table">
            <tbody>
              <tr v-if="files.length === 0">
                <td class="text-medium-emphasis">No service files</td>
              </tr>
              <tr v-for="file in files" :key="file.id">
                <td>{{ file.path }}</td>
                <td>{{ file.kind }}</td>
                <td class="font-tabular">{{ file.size ?? '-' }}</td>
                <td class="text-right">
                  <v-btn v-if="file.kind === 'template'" icon="mdi-pencil-outline" variant="text" size="small" @click="editTemplateFile(file)" />
                  <v-btn icon="mdi-delete-outline" variant="text" size="small" :loading="filesLoading" @click="deleteFile(file)" />
                </td>
              </tr>
            </tbody>
          </v-table>
        </div>

        <div class="status-grid">
          <v-alert v-if="validation" :type="validation.valid ? 'success' : 'error'" variant="tonal" density="compact">
            {{ validation.valid ? 'Validation passed' : 'Validation failed' }}
            <div v-for="issue in validation.issues || []" :key="`${issue.path}-${issue.message}`">{{ issue.severity || 'issue' }}: {{ issue.message }}</div>
          </v-alert>

          <v-card v-if="schedulePreview" variant="outlined">
            <v-card-title class="text-subtitle-2">Schedule</v-card-title>
            <v-card-text>
              <div class="font-weight-bold">{{ schedulePreview.selectedNodeName || schedulePreview.selectedNodeId || 'No eligible node selected' }}</div>
              <div v-for="candidate in schedulePreview.candidates || []" :key="candidate.nodeId" class="text-caption">
                {{ candidate.nodeName || candidate.nodeId }} - {{ candidate.eligible ? 'eligible' : 'blocked' }}
              </div>
            </v-card-text>
          </v-card>
        </div>

        <v-expansion-panels variant="accordion">
          <v-expansion-panel @group:selected="runRenderPreview">
            <v-expansion-panel-title>Generated artifacts</v-expansion-panel-title>
            <v-expansion-panel-text>
              <v-progress-linear v-if="previewLoading === 'render'" indeterminate class="mb-2" />
              <pre>{{ renderPreview?.composeYaml || renderPreview?.renderedYaml || renderPreview?.overrideYaml || 'Open to render generated Compose artifacts.' }}</pre>
            </v-expansion-panel-text>
          </v-expansion-panel>
        </v-expansion-panels>
      </div>
    </div>

    <v-dialog v-model="fileDialog" max-width="720">
      <v-card>
        <v-card-title class="text-subtitle-1">{{ editingFile ? 'Edit Template File' : 'New Template File' }}</v-card-title>
        <v-card-text class="d-flex flex-column ga-3">
          <v-text-field v-model="fileForm.path" label="Path" variant="outlined" density="compact" />
          <v-textarea v-model="fileForm.content" label="Content" rows="14" class="font-mono" variant="outlined" density="compact" />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" class="text-none" @click="fileDialog = false">Cancel</v-btn>
          <v-btn color="primary" variant="flat" class="text-none" :loading="filesLoading" @click="saveTemplateFile">Save</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </v-navigation-drawer>
</template>

<style scoped>
.editor-body {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 16px 18px 20px;
}

.editor-shell {
  background: rgb(var(--v-theme-surface));
}

.editor-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 14px;
  padding: 18px;
  border-bottom: 1px solid rgba(var(--v-border-color), 0.08);
}

.editor-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.min-width-0 {
  min-width: 0;
}

.readonly-name {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 10px 12px;
  border-radius: 8px;
  background: rgba(var(--v-theme-surface-variant), 0.6);
}

.section-title,
.list-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 7px;
  margin-top: 6px;
  margin-bottom: 2px;
  font-weight: 700;
  color: rgba(var(--v-theme-on-surface), 0.88);
}

.mode-switch-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.mode-switch-row :deep(.v-btn) {
  min-height: 40px;
}

.editor-section {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 12px;
  border-radius: 8px;
  background: rgba(var(--v-theme-surface-variant), 0.34);
}

.yaml-section {
  background: transparent;
  padding: 0;
}

.visual-grid,
.split-grid,
.status-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.list-block {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.row-line {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
  align-items: center;
}

.kv-row {
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto;
}

.file-title {
  align-items: flex-start;
}

.file-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.binary-upload {
  width: 180px;
}

.files-table {
  border-radius: 8px;
  background: transparent;
}

.font-mono,
pre {
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
}

pre {
  white-space: pre-wrap;
  margin: 0;
}

@media (max-width: 720px) {
  .editor-header,
  .mode-switch-row,
  .visual-grid,
  .split-grid,
  .status-grid,
  .row-line,
  .kv-row {
    grid-template-columns: 1fr;
  }

  .editor-header,
  .mode-switch-row {
    flex-direction: column;
  }

  .editor-actions,
  .editor-actions .v-btn:first-child,
  .binary-upload {
    width: 100%;
  }
}
</style>
