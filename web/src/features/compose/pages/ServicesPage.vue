<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { Delete, Edit, Plus, Refresh, Search, SwitchButton, Upload } from '@element-plus/icons-vue';
import { ElMessage, ElMessageBox } from 'element-plus';
import { composeApi } from '@/api/compose';
import { serversApi } from '@/api/servers';
import TaskLogPanel from '@/components/tasks/TaskLogPanel.vue';
import type {
  ComposeRenderPreviewDto,
  ComposeServiceDto,
  ServerDto,
  ServiceTemplateDto,
  TaskDto,
} from '@/types/api';

interface ServiceForm {
  id: string;
  name: string;
  templateId: string;
  serverId: string;
  remotePath: string;
}

const services = ref<ComposeServiceDto[]>([]);
const templates = ref<ServiceTemplateDto[]>([]);
const servers = ref<ServerDto[]>([]);
const loading = ref(false);
const saving = ref(false);
const rendering = ref(false);
const drawerOpen = ref(false);
const taskId = ref('');
const taskTitle = ref('Compose Service Task');
const actionLoading = ref('');
const error = ref('');
const valuesJson = ref('{}');
const serverVariablesJson = ref('{}');
const renderPreview = ref<ComposeRenderPreviewDto | null>(null);

const form = reactive<ServiceForm>(emptyForm());

const sortedServices = computed(() => [...services.value].sort((a, b) => b.updatedAt.localeCompare(a.updatedAt)));
const selectedTemplate = computed(() => templates.value.find((template) => template.id === form.templateId));
const selectedServer = computed(() => servers.value.find((server) => server.id === form.serverId));
const isEditing = computed(() => Boolean(form.id));

function emptyForm(): ServiceForm {
  return {
    id: '',
    name: '',
    templateId: '',
    serverId: '',
    remotePath: '/opt/panel/services/',
  };
}

function resetForm(next = emptyForm(), values: Record<string, unknown> = {}) {
  Object.assign(form, next);
  valuesJson.value = JSON.stringify(values, null, 2);
  renderPreview.value = null;
}

function defaultValues(template?: ServiceTemplateDto) {
  return Object.fromEntries((template?.variables ?? []).map((item) => [item.name, item.defaultValue ?? '']));
}

function parseValues() {
  try {
    return JSON.parse(valuesJson.value || '{}') as Record<string, unknown>;
  } catch {
    throw new Error('Values must be valid JSON');
  }
}

function parseServerVariables() {
  try {
    return JSON.parse(serverVariablesJson.value || '{}') as Record<string, unknown>;
  } catch {
    throw new Error('Server variables must be valid JSON');
  }
}

function serviceTemplateName(service: ComposeServiceDto) {
  return service.templateName || templates.value.find((template) => template.id === service.templateId)?.name || service.templateId;
}

function serviceServerName(service: ComposeServiceDto) {
  return service.serverName || servers.value.find((server) => server.id === service.serverId)?.name || service.serverId;
}

function syncStatus(service: ComposeServiceDto) {
  if (service.drift) return 'drifted';
  return service.syncStatus || 'unknown';
}

function syncStatusType(service: ComposeServiceDto) {
  const status = syncStatus(service);
  if (status === 'synced') return 'success';
  if (status === 'drifted' || status === 'pending') return 'warning';
  return 'info';
}

function runtimeStatusType(service: ComposeServiceDto) {
  const status = service.runtimeStatus || service.status;
  if (status === 'running' || status === 'deployed') return 'success';
  if (status === 'failed') return 'danger';
  if (status === 'stopped') return 'warning';
  return 'info';
}

async function load() {
  loading.value = true;
  try {
    const [serviceRows, templateRows, serverRows] = await Promise.all([
      composeApi.listServices(),
      composeApi.listTemplates(),
      serversApi.listServers(),
    ]);
    services.value = serviceRows;
    templates.value = templateRows;
    servers.value = serverRows;
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to load services';
  } finally {
    loading.value = false;
  }
}

async function loadServerVariables(serverId = form.serverId) {
  if (!serverId) {
    serverVariablesJson.value = '{}';
    return;
  }
  try {
    const variables = await composeApi.getServerVariables(serverId);
    serverVariablesJson.value = JSON.stringify(variables ?? {}, null, 2);
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : 'Unable to load server variables');
  }
}

function openService(service?: ComposeServiceDto) {
  if (service) {
    resetForm(
      {
        id: service.id,
        name: service.name,
        templateId: service.templateId,
        serverId: service.serverId,
        remotePath: service.remotePath,
      },
      service.values ?? {},
    );
  } else {
    const firstTemplate = templates.value[0];
    const firstServer = servers.value[0];
    resetForm({
      id: '',
      name: '',
      templateId: firstTemplate?.id ?? '',
      serverId: firstServer?.id ?? '',
      remotePath: '/opt/panel/services/',
    }, defaultValues(firstTemplate));
  }
  drawerOpen.value = true;
  void loadServerVariables(form.serverId);
}

function handleTemplateChange(templateId: string) {
  const template = templates.value.find((item) => item.id === templateId);
  valuesJson.value = JSON.stringify(defaultValues(template), null, 2);
}

function handleServerChange(serverId: string) {
  void loadServerVariables(serverId);
}

async function saveServerVariables() {
  if (!form.serverId) return;
  try {
    const saved = await composeApi.updateServerVariables(form.serverId, parseServerVariables());
    serverVariablesJson.value = JSON.stringify(saved ?? {}, null, 2);
    ElMessage.success('Server variables saved');
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : 'Unable to save server variables');
  }
}

async function saveService() {
  saving.value = true;
  try {
    const input = {
      name: form.name,
      templateId: form.templateId,
      serverId: form.serverId,
      remotePath: form.remotePath,
      values: parseValues(),
    };
    const saved = form.id ? await composeApi.updateService(form.id, input) : await composeApi.createService(input);
    resetForm(
      {
        id: saved.id,
        name: saved.name,
        templateId: saved.templateId,
        serverId: saved.serverId,
        remotePath: saved.remotePath,
      },
      saved.values,
    );
    await load();
    ElMessage.success('Service saved');
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : 'Unable to save service');
  } finally {
    saving.value = false;
  }
}

async function deleteService(service: ComposeServiceDto) {
  await ElMessageBox.confirm(`Delete service metadata ${service.name}? Remote containers are not removed by this action.`, 'Confirm delete', {
    type: 'warning',
  });
  await composeApi.deleteService(service.id);
  await load();
  ElMessage.success('Service metadata deleted');
}

async function previewService() {
  if (!form.id) return;
  rendering.value = true;
  try {
    renderPreview.value = await composeApi.renderService(form.id);
    ElMessage.success('Service render preview refreshed');
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : 'Unable to render service');
  } finally {
    rendering.value = false;
  }
}

async function runAction(service: ComposeServiceDto, action: 'deploy' | 'sync' | 'restart' | 'stop' | 'remove') {
  if (action === 'remove') {
    await ElMessageBox.confirm(`Remove remote Compose service ${service.name}?`, 'Confirm remove', { type: 'warning' });
  }
  actionLoading.value = `${action}:${service.id}`;
  try {
    const result =
      action === 'deploy'
        ? await composeApi.deployService(service.id)
        : action === 'sync'
          ? await composeApi.syncService(service.id)
          : action === 'restart'
            ? await composeApi.restartService(service.id)
            : action === 'stop'
              ? await composeApi.stopService(service.id)
              : await composeApi.removeService(service.id);
    taskId.value = result.taskId;
    taskTitle.value = `${action[0].toUpperCase()}${action.slice(1)} ${service.name}`;
    ElMessage.success(`${action} started`);
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : `Unable to ${action} service`);
  } finally {
    actionLoading.value = '';
  }
}

async function handleTaskFinished(_task: TaskDto) {
  await load();
}

onMounted(load);
</script>

<template>
  <div>
    <div class="panel-header panel">
      <div>
        <p class="page-subtitle">Deploy template-backed Compose services, edit values and paths, inspect drift, and run lifecycle tasks.</p>
      </div>
      <div class="toolbar">
        <el-button :icon="Refresh" :loading="loading" @click="load">Reload</el-button>
        <el-button type="primary" :icon="Plus" :disabled="templates.length === 0 || servers.length === 0" @click="openService()">
          New Service
        </el-button>
      </div>
    </div>

    <el-alert v-if="error" class="page-alert" type="error" :title="error" show-icon />
    <el-alert
      v-if="!loading && (templates.length === 0 || servers.length === 0)"
      class="page-alert"
      type="warning"
      title="Create at least one service template and one server before creating services."
      show-icon
    />

    <section class="panel services-panel" v-loading="loading">
      <el-table :data="sortedServices" empty-text="No deployed services">
        <el-table-column prop="name" label="Name" min-width="170" />
        <el-table-column label="Template" min-width="180">
          <template #default="{ row }">{{ serviceTemplateName(row) }}</template>
        </el-table-column>
        <el-table-column label="Server" min-width="150">
          <template #default="{ row }">{{ serviceServerName(row) }}</template>
        </el-table-column>
        <el-table-column prop="remotePath" label="Remote Path" min-width="230" show-overflow-tooltip />
        <el-table-column label="Runtime" width="130">
          <template #default="{ row }">
            <el-tag :type="runtimeStatusType(row)">{{ row.runtimeStatus || row.status || 'unknown' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="Sync" width="120">
          <template #default="{ row }">
            <el-tag :type="syncStatusType(row)">{{ syncStatus(row) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="Template Version" width="160">
          <template #default="{ row }">{{ row.lastAppliedTemplateVersion ?? '-' }} / {{ row.templateVersion ?? '-' }}</template>
        </el-table-column>
        <el-table-column label="Actions" width="430" fixed="right">
          <template #default="{ row }">
            <el-button size="small" :icon="Search" @click="openService(row)">Open</el-button>
            <el-button
              size="small"
              type="primary"
              :icon="Upload"
              :loading="actionLoading === `deploy:${row.id}`"
              @click="runAction(row, 'deploy')"
            >
              Deploy
            </el-button>
            <el-button
              size="small"
              :icon="Refresh"
              :loading="actionLoading === `sync:${row.id}`"
              @click="runAction(row, 'sync')"
            >
              Sync
            </el-button>
            <el-dropdown trigger="click">
              <el-button size="small" :icon="SwitchButton">More</el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item @click="runAction(row, 'restart')">Restart</el-dropdown-item>
                  <el-dropdown-item @click="runAction(row, 'stop')">Stop</el-dropdown-item>
                  <el-dropdown-item divided @click="runAction(row, 'remove')">Remove remote</el-dropdown-item>
                  <el-dropdown-item divided @click="deleteService(row)">Delete metadata</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
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

    <el-drawer v-model="drawerOpen" title="Compose Service" size="68%">
      <div class="service-detail">
        <section class="panel">
          <div class="panel-header">
            <strong>{{ isEditing ? form.name || 'Service' : 'New Service' }}</strong>
            <div class="toolbar">
              <el-button :disabled="!isEditing" :loading="rendering" :icon="Search" @click="previewService">Render</el-button>
              <el-button type="primary" :loading="saving" :icon="Edit" @click="saveService">Save</el-button>
            </div>
          </div>
          <div class="panel-body service-form">
            <el-form label-position="top">
              <el-form-item label="Name">
                <el-input v-model="form.name" placeholder="customer-site" />
              </el-form-item>
              <el-form-item label="Template">
                <el-select v-model="form.templateId" filterable placeholder="Select template" @change="handleTemplateChange">
                  <el-option v-for="template in templates" :key="template.id" :label="template.name" :value="template.id" />
                </el-select>
              </el-form-item>
              <el-form-item label="Server">
                <el-select v-model="form.serverId" filterable placeholder="Select server" @change="handleServerChange">
                  <el-option v-for="server in servers" :key="server.id" :label="server.name" :value="server.id" />
                </el-select>
              </el-form-item>
              <el-form-item label="Remote path">
                <el-input v-model="form.remotePath" placeholder="/opt/panel/services/customer-site" />
              </el-form-item>
            </el-form>

            <div class="side-summary">
              <div>
                <div class="detail-label">Template</div>
                <strong>{{ selectedTemplate?.name || '-' }}</strong>
                <div class="muted">version {{ selectedTemplate?.version ?? '-' }}</div>
              </div>
              <div>
                <div class="detail-label">Server</div>
                <strong>{{ selectedServer?.name || '-' }}</strong>
                <div class="muted">{{ selectedServer?.host || '-' }}</div>
              </div>
              <div>
                <div class="detail-label">Variables</div>
                <el-tag>{{ selectedTemplate?.variables?.length ?? 0 }}</el-tag>
              </div>
            </div>
          </div>
        </section>

        <section class="panel">
          <div class="panel-header"><strong>Values</strong></div>
          <div class="panel-body">
            <el-input v-model="valuesJson" type="textarea" :rows="14" spellcheck="false" />
          </div>
        </section>

        <section class="panel">
          <div class="panel-header">
            <strong>Server Variables</strong>
            <el-button :disabled="!form.serverId" @click="saveServerVariables">Save Variables</el-button>
          </div>
          <div class="panel-body">
            <el-input v-model="serverVariablesJson" type="textarea" :rows="10" spellcheck="false" />
          </div>
        </section>

        <section class="panel">
          <div class="panel-header"><strong>Rendered Config</strong></div>
          <div class="panel-body">
            <el-empty v-if="!renderPreview" description="Save the service, then render preview" />
            <pre v-else class="code-preview">{{ renderPreview.renderedYaml }}</pre>
          </div>
        </section>
      </div>
    </el-drawer>
  </div>
</template>

<style scoped>
.page-alert,
.services-panel,
.task-section,
.service-detail {
  margin-top: 20px;
}

.services-panel {
  padding: 0 20px 20px;
}

.service-detail {
  display: grid;
  gap: 18px;
}

.service-form {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 260px;
  gap: 18px;
}

.side-summary {
  display: grid;
  gap: 16px;
  align-content: start;
  border: 1px solid #dfe4ea;
  border-radius: 8px;
  padding: 14px;
}

.detail-label {
  margin-bottom: 6px;
  color: #667085;
  font-size: 12px;
  text-transform: uppercase;
}

.code-preview {
  min-height: 260px;
  max-height: 480px;
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

@media (max-width: 960px) {
  .service-form {
    grid-template-columns: 1fr;
  }
}
</style>
