<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { Delete, Edit, Plus, Refresh, Search } from '@element-plus/icons-vue';
import { ElMessage, ElMessageBox } from 'element-plus';
import { composeApi } from '@/api/compose';
import { serversApi } from '@/api/servers';
import TaskLogPanel from '@/components/tasks/TaskLogPanel.vue';
import type {
  ComposeRenderPreviewDto,
  ComposeTemplateVariableDto,
  ComposeValidationResultDto,
  ComposeVisualModelDto,
  ComposeVisualServiceDto,
  ServerDto,
  ServiceTemplateDto,
  TemplateFileDto,
  TemplateFileKind,
} from '@/types/api';

type EditMode = 'visual' | 'yaml';

interface TemplateForm {
  id: string;
  name: string;
  description: string;
  composeYaml: string;
  visual: ComposeVisualModelDto;
  variables: ComposeTemplateVariableDto[];
}

interface FileForm {
  id: string;
  kind: TemplateFileKind;
  path: string;
  content: string;
  base64Content: string;
  mode: string;
}

const templates = ref<ServiceTemplateDto[]>([]);
const servers = ref<ServerDto[]>([]);
const files = ref<TemplateFileDto[]>([]);
const linkedServices = ref([]);
const loading = ref(false);
const detailLoading = ref(false);
const saving = ref(false);
const validating = ref(false);
const activeTemplate = ref<ServiceTemplateDto | null>(null);
const drawerOpen = ref(false);
const fileDrawerOpen = ref(false);
const mode = ref<EditMode>('visual');
const validation = ref<ComposeValidationResultDto | null>(null);
const preview = ref<ComposeRenderPreviewDto | null>(null);
const taskId = ref('');
const taskTitle = ref('Compose Task');
const error = ref('');
const previewServerId = ref('');

const form = reactive<TemplateForm>(emptyTemplateForm());
const fileForm = reactive<FileForm>(emptyFileForm());

const isEditing = computed(() => Boolean(form.id));
const canUseTemplateActions = computed(() => Boolean(form.id) && !saving.value && !validating.value);
const sortedTemplates = computed(() => [...templates.value].sort((a, b) => b.updatedAt.localeCompare(a.updatedAt)));

function emptyTemplateForm(): TemplateForm {
  const visual: ComposeVisualModelDto = {
    services: [
      {
        name: 'web',
        image: 'nginx:latest',
        labels: { 'panel.managed': 'true' },
        ports: ['8080:80'],
        environment: {},
        volumes: [],
      },
    ],
  };
  return {
    id: '',
    name: '',
    description: '',
    composeYaml: visualToYaml(visual),
    visual,
    variables: [],
  };
}

function emptyFileForm(): FileForm {
  return { id: '', kind: 'template', path: '', content: '', base64Content: '', mode: '0644' };
}

function resetForm(next = emptyTemplateForm()) {
  Object.assign(form, next);
  validation.value = null;
  preview.value = null;
}

function resetFileForm(next = emptyFileForm()) {
  Object.assign(fileForm, next);
}

function visualFromTemplate(template: ServiceTemplateDto): ComposeVisualModelDto {
  const visual = template.visual as ComposeVisualModelDto | null | undefined;
  if (visual?.services?.length) return visual;
  return yamlToVisual(template.composeYaml);
}

function templateToForm(template: ServiceTemplateDto): TemplateForm {
  const visual = visualFromTemplate(template);
  return {
    id: template.id,
    name: template.name,
    description: template.description ?? '',
    composeYaml: template.composeYaml || visualToYaml(visual),
    visual,
    variables: template.variables ?? [],
  };
}

function visualToYaml(visual: ComposeVisualModelDto) {
  const service = visual.services[0] ?? { name: 'service', image: 'nginx:latest' };
  const serviceName = service.name || 'service';
  const lines = ['services:', `  ${serviceName}:`];
  lines.push(`    image: ${service.image || 'nginx:latest'}`);
  lines.push(`    container_name: ${serviceName}`);
  if (service.command) lines.push(`    command: ${service.command}`);
  const labelEntries = Object.entries(service.labels ?? {}).filter(([key]) => key);
  if (labelEntries.length) {
    lines.push('    labels:');
    labelEntries.forEach(([key, value]) => lines.push(`      ${key}: "${value}"`));
  }
  if (service.ports?.length) {
    lines.push('    ports:');
    service.ports.filter(Boolean).forEach((port) => lines.push(`      - "${port}"`));
  }
  const envEntries = Object.entries(service.environment ?? {}).filter(([key]) => key);
  if (envEntries.length) {
    lines.push('    environment:');
    envEntries.forEach(([key, value]) => lines.push(`      ${key}: "${value}"`));
  }
  if (service.volumes?.length) {
    lines.push('    volumes:');
    service.volumes.filter(Boolean).forEach((volume) => lines.push(`      - "${volume}"`));
  }
  return `${lines.join('\n')}\n`;
}

function yamlToVisual(yaml: string): ComposeVisualModelDto {
  const nameMatch = yaml.match(/\n\s{2}([A-Za-z0-9_-]+):\s*\n/);
  const imageMatch = yaml.match(/\n\s{4}image:\s*["']?([^"'\n]+)["']?/);
  return {
    version: yaml.match(/version:\s*["']?([^"'\n]+)["']?/)?.[1] || '3.9',
    services: [
      {
        name: nameMatch?.[1] || 'web',
        image: imageMatch?.[1] || 'nginx:latest',
        labels: {},
        ports: [...yaml.matchAll(/-\s*["']?([0-9.:]+:[0-9/]+)["']?/g)].map((match) => match[1]),
        environment: {},
        volumes: [],
      },
    ],
  };
}

function updateListField(service: ComposeVisualServiceDto, key: 'ports' | 'volumes', value: string) {
  service[key] = value
    .split('\n')
    .map((item) => item.trim())
    .filter(Boolean);
  syncYamlFromVisual();
}

function updateLabels(service: ComposeVisualServiceDto, value: string) {
  service.labels = Object.fromEntries(
    value
      .split('\n')
      .map((line) => line.split('='))
      .filter(([key]) => key?.trim())
      .map(([key, ...rest]) => [key.trim(), rest.join('=').trim()]),
  );
  syncYamlFromVisual();
}

function updateEnv(service: ComposeVisualServiceDto, value: string) {
  service.environment = Object.fromEntries(
    value
      .split('\n')
      .map((line) => line.split('='))
      .filter(([key]) => key?.trim())
      .map(([key, ...rest]) => [key.trim(), rest.join('=').trim()]),
  );
  syncYamlFromVisual();
}

function envText(service: ComposeVisualServiceDto) {
  return Object.entries(service.environment ?? {})
    .map(([key, value]) => `${key}=${value}`)
    .join('\n');
}

function labelsText(service: ComposeVisualServiceDto) {
  return Object.entries(service.labels ?? {})
    .map(([key, value]) => `${key}=${value}`)
    .join('\n');
}

function syncYamlFromVisual() {
  form.composeYaml = visualToYaml(form.visual);
}

function syncVisualFromYaml() {
  form.visual = yamlToVisual(form.composeYaml);
  ElMessage.success('Visual fields updated from YAML');
}

function addVariable() {
  form.variables.push({ name: '', type: 'string', required: false, defaultValue: '' });
}

function removeVariable(index: number) {
  form.variables.splice(index, 1);
}

async function loadTemplates() {
  loading.value = true;
  try {
    const [templateRows, serverRows] = await Promise.all([composeApi.listTemplates(), serversApi.listServers()]);
    templates.value = templateRows;
    servers.value = serverRows;
    if (!previewServerId.value && serverRows.length) previewServerId.value = serverRows[0].id;
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
  linkedServices.value = [];
  if (!template) {
    activeTemplate.value = null;
    resetForm();
    return;
  }
  detailLoading.value = true;
  try {
    const [detail, fileRows, serviceRows] = await Promise.all([
      composeApi.getTemplate(template.id),
      composeApi.listTemplateFiles(template.id),
      composeApi.listTemplateServices(template.id),
    ]);
    activeTemplate.value = detail;
    resetForm(templateToForm(detail));
    files.value = fileRows;
    linkedServices.value = serviceRows as never[];
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : 'Unable to load template detail');
  } finally {
    detailLoading.value = false;
  }
}

async function saveTemplate() {
  saving.value = true;
  try {
    const input = {
      name: form.name,
      description: form.description,
      composeYaml: form.composeYaml,
      visual: form.visual,
      variables: form.variables.filter((item) => item.name.trim()),
    };
    const saved = form.id ? await composeApi.updateTemplate(form.id, input) : await composeApi.createTemplate(input);
    activeTemplate.value = saved;
    resetForm(templateToForm(saved));
    await loadTemplates();
    ElMessage.success('Service template saved');
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : 'Unable to save service template');
  } finally {
    saving.value = false;
  }
}

async function deleteTemplate(template: ServiceTemplateDto) {
  await ElMessageBox.confirm(`Delete service template ${template.name}?`, 'Confirm delete', { type: 'warning' });
  await composeApi.deleteTemplate(template.id);
  await loadTemplates();
  ElMessage.success('Service template deleted');
}

async function validateTemplate() {
  if (!form.id) return;
  validating.value = true;
  try {
    validation.value = await composeApi.validateTemplate(form.id, { serverId: previewServerId.value });
    ElMessage.success(validation.value.valid ? 'Template validation passed' : 'Template validation returned issues');
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : 'Unable to validate template');
  } finally {
    validating.value = false;
  }
}

async function renderPreview() {
  if (!form.id) return;
  validating.value = true;
  try {
    preview.value = await composeApi.renderTemplatePreview(form.id, { serverId: previewServerId.value });
    ElMessage.success('Render preview refreshed');
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : 'Unable to render preview');
  } finally {
    validating.value = false;
  }
}

function openFile(file?: TemplateFileDto) {
  resetFileForm(
    file
      ? {
          id: file.id,
          kind: file.kind,
          path: file.path,
          content: file.content ?? '',
          base64Content: file.base64Content ?? '',
          mode: file.mode ?? '0644',
        }
      : emptyFileForm(),
  );
  fileDrawerOpen.value = true;
}

async function saveFile() {
  if (!form.id) return;
  const input = {
    path: fileForm.path,
    content: fileForm.kind === 'template' ? fileForm.content : undefined,
    base64Content: fileForm.kind === 'binary' ? fileForm.base64Content : undefined,
    mode: fileForm.mode,
  };
  const saved = fileForm.id
    ? await composeApi.updateTemplateFile(form.id, fileForm.id, input)
    : fileForm.kind === 'binary'
      ? await composeApi.createTemplateBinaryFile(form.id, input)
      : await composeApi.createTemplateTextFile(form.id, input);
  const index = files.value.findIndex((item) => item.id === saved.id);
  if (index >= 0) files.value[index] = saved;
  else files.value.push(saved);
  fileDrawerOpen.value = false;
  ElMessage.success('Template file saved');
}

async function deleteFile(file: TemplateFileDto) {
  if (!form.id) return;
  await ElMessageBox.confirm(`Delete attached file ${file.path}?`, 'Confirm delete', { type: 'warning' });
  await composeApi.deleteTemplateFile(form.id, file.id);
  files.value = files.value.filter((item) => item.id !== file.id);
  ElMessage.success('Template file deleted');
}

function handleTaskFinished() {
  void loadTemplates();
}

onMounted(loadTemplates);
</script>

<template>
  <div>
    <div class="panel-header panel">
      <div>
        <p class="page-subtitle">Reusable Compose definitions with visual/YAML editing, attached files, validation, and render preview.</p>
      </div>
      <div class="toolbar">
        <el-button :icon="Refresh" :loading="loading" @click="loadTemplates">Reload</el-button>
        <el-button type="primary" :icon="Plus" @click="openTemplate()">New Template</el-button>
      </div>
    </div>

    <el-alert v-if="error" class="page-alert" type="error" :title="error" show-icon />

    <section class="panel table-panel" v-loading="loading">
      <el-table :data="sortedTemplates" empty-text="No service templates">
        <el-table-column prop="name" label="Name" min-width="180" />
        <el-table-column prop="description" label="Description" min-width="220" show-overflow-tooltip />
        <el-table-column prop="version" label="Version" width="100" />
        <el-table-column label="Files" width="100">
          <template #default="{ row }">{{ row.fileCount ?? '-' }}</template>
        </el-table-column>
        <el-table-column label="Services" width="110">
          <template #default="{ row }">{{ row.linkedServiceCount ?? '-' }}</template>
        </el-table-column>
        <el-table-column prop="updatedAt" label="Updated" min-width="170" />
        <el-table-column label="Actions" width="210" fixed="right">
          <template #default="{ row }">
            <el-button size="small" :icon="Search" @click="openTemplate(row)">Open</el-button>
            <el-button size="small" type="danger" :icon="Delete" @click="deleteTemplate(row)">Delete</el-button>
          </template>
        </el-table-column>
      </el-table>
    </section>

    <section v-if="taskId" class="panel task-section">
      <div class="panel-header"><strong>{{ taskTitle }}</strong></div>
      <div class="panel-body">
        <TaskLogPanel :task-id="taskId" compact @finished="handleTaskFinished" />
      </div>
    </section>

    <el-drawer v-model="drawerOpen" title="Service Template" size="78%">
      <div v-loading="detailLoading" class="template-detail">
        <section class="panel">
          <div class="panel-header">
            <strong>{{ isEditing ? form.name || 'Template' : 'New Template' }}</strong>
            <div class="toolbar">
              <el-button :disabled="!isEditing" :loading="validating" @click="validateTemplate">Validate</el-button>
              <el-button :disabled="!isEditing" :loading="validating" @click="renderPreview">Render Preview</el-button>
              <el-button type="primary" :loading="saving" @click="saveTemplate">Save</el-button>
            </div>
          </div>
          <div class="panel-body form-grid">
            <el-form label-position="top">
              <el-form-item label="Name">
                <el-input v-model="form.name" placeholder="nginx-site" />
              </el-form-item>
              <el-form-item label="Description">
                <el-input v-model="form.description" type="textarea" :rows="2" />
              </el-form-item>
            </el-form>
            <el-form label-position="top">
              <el-form-item label="Preview server">
                <el-select v-model="previewServerId" filterable clearable placeholder="Use system variables only">
                  <el-option v-for="server in servers" :key="server.id" :label="server.name" :value="server.id">
                    <span>{{ server.name }}</span>
                    <span class="option-meta">{{ server.host }}</span>
                  </el-option>
                </el-select>
              </el-form-item>
              <el-alert
                type="info"
                title="Render values come from built-in system variables and the selected server variables. The template only declares required variable names."
                show-icon
              />
            </el-form>
          </div>
        </section>

        <section class="panel">
          <div class="panel-header">
            <strong>Compose Definition</strong>
            <el-segmented v-model="mode" :options="['visual', 'yaml']" />
          </div>
          <div class="panel-body">
            <div v-if="mode === 'visual'" class="visual-editor">
              <div v-for="(service, index) in form.visual.services.slice(0, 1)" :key="index" class="service-editor">
                <div class="service-editor-header">
                  <strong>{{ service.name || 'Service' }}</strong>
                  <span class="muted">container_name uses the service name</span>
                </div>
                <div class="service-fields">
                  <el-input v-model="service.name" placeholder="web" @change="syncYamlFromVisual">
                    <template #prepend>Name</template>
                  </el-input>
                  <el-input v-model="service.image" placeholder="nginx:latest" @change="syncYamlFromVisual">
                    <template #prepend>Image</template>
                  </el-input>
                  <el-input v-model="service.command" placeholder="optional command" @change="syncYamlFromVisual">
                    <template #prepend>Command</template>
                  </el-input>
                  <el-input
                    :model-value="labelsText(service)"
                    type="textarea"
                    :rows="3"
                    placeholder="com.example.role=web"
                    @update:model-value="updateLabels(service, String($event))"
                  />
                  <el-input
                    :model-value="service.ports?.join('\n')"
                    type="textarea"
                    :rows="3"
                    placeholder="8080:80"
                    @update:model-value="updateListField(service, 'ports', String($event))"
                  />
                  <el-input
                    :model-value="envText(service)"
                    type="textarea"
                    :rows="3"
                    placeholder="KEY=value"
                    @update:model-value="updateEnv(service, String($event))"
                  />
                  <el-input
                    :model-value="service.volumes?.join('\n')"
                    type="textarea"
                    :rows="3"
                    placeholder="./data:/data"
                    @update:model-value="updateListField(service, 'volumes', String($event))"
                  />
                </div>
              </div>
            </div>
            <div v-else class="yaml-editor">
              <div class="editor-toolbar">
                <el-button :icon="Edit" @click="syncVisualFromYaml">Update Visual Fields</el-button>
              </div>
              <el-input v-model="form.composeYaml" type="textarea" :rows="22" spellcheck="false" />
            </div>
          </div>
        </section>

        <section class="panel">
          <div class="panel-header">
            <strong>Variables</strong>
            <el-button :icon="Plus" @click="addVariable">Add Variable</el-button>
          </div>
          <div class="panel-body">
            <el-table :data="form.variables" empty-text="No template variables">
              <el-table-column label="Name" min-width="160">
                <template #default="{ row }"><el-input v-model="row.name" /></template>
              </el-table-column>
              <el-table-column label="Type" width="150">
                <template #default="{ row }">
                  <el-select v-model="row.type">
                    <el-option label="string" value="string" />
                    <el-option label="number" value="number" />
                    <el-option label="boolean" value="boolean" />
                    <el-option label="secret" value="secret" />
                  </el-select>
                </template>
              </el-table-column>
              <el-table-column label="Default" min-width="160">
                <template #default="{ row }"><el-input v-model="row.defaultValue" /></template>
              </el-table-column>
              <el-table-column label="Required" width="110">
                <template #default="{ row }"><el-switch v-model="row.required" /></template>
              </el-table-column>
              <el-table-column width="90">
                <template #default="{ $index }">
                  <el-button size="small" type="danger" :icon="Delete" @click="removeVariable($index)" />
                </template>
              </el-table-column>
            </el-table>
          </div>
        </section>

        <section class="panel">
          <div class="panel-header">
            <strong>Attached Files</strong>
            <el-button :icon="Plus" :disabled="!canUseTemplateActions" @click="openFile()">Add File</el-button>
          </div>
          <div class="panel-body">
            <el-table :data="files" empty-text="Save the template, then attach files">
              <el-table-column prop="path" label="Path" min-width="220" />
              <el-table-column prop="kind" label="Kind" width="120" />
              <el-table-column label="Size" width="120">
                <template #default="{ row }">{{ row.sizeBytes ?? '-' }}</template>
              </el-table-column>
              <el-table-column label="Actions" width="180" fixed="right">
                <template #default="{ row }">
                  <el-button size="small" @click="openFile(row)">Edit</el-button>
                  <el-button size="small" type="danger" :icon="Delete" @click="deleteFile(row)">Delete</el-button>
                </template>
              </el-table-column>
            </el-table>
          </div>
        </section>

        <section class="preview-grid">
          <div class="panel">
            <div class="panel-header"><strong>Validation</strong></div>
            <div class="panel-body">
              <el-empty v-if="!validation" description="No validation result" />
              <div v-else>
                <el-tag :type="validation.valid ? 'success' : 'danger'">{{ validation.valid ? 'valid' : 'invalid' }}</el-tag>
                <el-alert
                  v-for="(issue, index) in validation.issues ?? []"
                  :key="index"
                  class="issue-alert"
                  :type="issue.severity === 'warning' ? 'warning' : 'error'"
                  :title="issue.message"
                  show-icon
                />
              </div>
            </div>
          </div>
          <div class="panel">
            <div class="panel-header"><strong>Render Preview</strong></div>
            <div class="panel-body">
              <el-empty v-if="!preview" description="No render preview" />
              <pre v-else class="code-preview">{{ preview.renderedYaml }}</pre>
            </div>
          </div>
        </section>
      </div>
    </el-drawer>

    <el-drawer v-model="fileDrawerOpen" title="Template File" size="520px">
      <el-form label-position="top">
        <el-form-item label="Kind">
          <el-segmented v-model="fileForm.kind" :options="['template', 'binary']" :disabled="Boolean(fileForm.id)" />
        </el-form-item>
        <el-form-item label="Path">
          <el-input v-model="fileForm.path" placeholder="config/app.env" />
        </el-form-item>
        <el-form-item label="Mode">
          <el-input v-model="fileForm.mode" placeholder="0644" />
        </el-form-item>
        <el-form-item v-if="fileForm.kind === 'template'" label="Text template content">
          <el-input v-model="fileForm.content" type="textarea" :rows="12" spellcheck="false" />
        </el-form-item>
        <el-form-item v-else label="Base64 binary content">
          <el-input v-model="fileForm.base64Content" type="textarea" :rows="12" spellcheck="false" />
        </el-form-item>
        <div class="drawer-actions">
          <el-button @click="fileDrawerOpen = false">Cancel</el-button>
          <el-button type="primary" @click="saveFile">Save File</el-button>
        </div>
      </el-form>
    </el-drawer>
  </div>
</template>

<style scoped>
.page-alert,
.table-panel,
.task-section,
.template-detail {
  margin-top: 20px;
}

.table-panel {
  padding: 0 20px 20px;
}

.template-detail {
  display: grid;
  gap: 18px;
}

.form-grid,
.preview-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 18px;
}

.visual-editor {
  display: grid;
  gap: 14px;
}

.editor-toolbar {
  margin-bottom: 12px;
}

.version-input {
  max-width: 220px;
}

.service-editor {
  border: 1px solid #dfe4ea;
  border-radius: 8px;
  padding: 14px;
}

.service-editor-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}

.service-fields {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}

.code-preview {
  min-height: 220px;
  max-height: 420px;
  overflow: auto;
  margin: 0;
  border: 1px solid #dfe4ea;
  border-radius: 8px;
  background: #101828;
  color: #e5e7eb;
  padding: 12px;
  font-family: "Cascadia Code", "SFMono-Regular", Consolas, monospace;
  font-size: 12px;
  white-space: pre-wrap;
}

.issue-alert {
  margin-top: 10px;
}

.option-meta {
  float: right;
  color: #98a2b3;
  font-size: 12px;
  margin-left: 16px;
}

.drawer-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}

@media (max-width: 960px) {
  .form-grid,
  .preview-grid,
  .service-fields {
    grid-template-columns: 1fr;
  }
}
</style>
