<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { composeApi } from '@/api/compose';
import { serversApi } from '@/api/servers';
import TaskLogPanel from '@/components/tasks/TaskLogPanel.vue';
import { serviceDeleteMode } from '@/features/compose/serviceDeleteMode';
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
const renderPreview = ref<ComposeRenderPreviewDto | null>(null);

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

const form = reactive<ServiceForm>(emptyForm());

const sortedServices = computed(() => [...safeArray(services.value)].sort((a, b) => b.updatedAt.localeCompare(a.updatedAt)));
const selectedTemplate = computed(() => safeArray(templates.value).find((template) => template.id === form.templateId));
const selectedServer = computed(() => safeArray(servers.value).find((server) => server.id === form.serverId));
const isEditing = computed(() => Boolean(form.id));
const dependencyTemplates = computed(() =>
  safeArray(selectedTemplate.value?.dependencies)
    .map((id) => safeArray(templates.value).find((template) => template.id === id))
    .filter((template): template is ServiceTemplateDto => Boolean(template)),
);

function safeArray<T>(value: T[] | null | undefined): T[] {
  return Array.isArray(value) ? value : [];
}

function emptyForm(): ServiceForm {
  return {
    id: '',
    name: '',
    templateId: '',
    serverId: '',
    remotePath: '/opt/panel/services/',
  };
}

function resetForm(next = emptyForm()) {
  Object.assign(form, next);
  renderPreview.value = null;
}

function serviceTemplateName(service: ComposeServiceDto) {
  return service.templateName || safeArray(templates.value).find((template) => template.id === service.templateId)?.name || '';
}

function serviceServerName(service: ComposeServiceDto) {
  return service.serverName || safeArray(servers.value).find((server) => server.id === service.serverId)?.name || service.serverId;
}

function syncStatus(service: ComposeServiceDto) {
  if (service.managementState) return service.managementState;
  if (service.drift) return 'drifted';
  return service.syncStatus || 'unknown';
}

function syncStatusType(service: ComposeServiceDto) {
  const status = syncStatus(service);
  if (status === 'synced') return 'success';
  if (status === 'managed') return 'success';
  if (status === 'drifted' || status === 'pending' || status === 'missing_remote') return 'warning';
  if (status === 'orphaned') return 'error';
  return 'info';
}

function runtimeStatusType(service: ComposeServiceDto) {
  const status = service.runtimeStatus || service.status;
  if (status === 'running' || status === 'deployed') return 'success';
  if (status === 'failed') return 'error';
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
    services.value = safeArray(serviceRows);
    templates.value = safeArray(templateRows).map((template) => ({ ...template, dependencies: safeArray(template.dependencies), variables: safeArray(template.variables) }));
    servers.value = safeArray(serverRows);
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to load services';
  } finally {
    loading.value = false;
  }
}

function openService(service?: ComposeServiceDto) {
  if (service) {
    resetForm({
      id: service.id,
      name: service.name,
      templateId: service.templateId,
      serverId: service.serverId,
      remotePath: service.remotePath,
    });
  } else {
    const firstTemplate = safeArray(templates.value)[0];
    const firstServer = safeArray(servers.value)[0];
    resetForm({
      id: '',
      name: '',
      templateId: firstTemplate?.id ?? '',
      serverId: firstServer?.id ?? '',
      remotePath: defaultRemotePath(firstTemplate?.name),
    });
  }
  drawerOpen.value = true;
}

function handleTemplateChange(templateId: string) {
  const template = safeArray(templates.value).find((item) => item.id === templateId);
  if (!isEditing.value) {
    form.name = template?.name ?? form.name;
    form.remotePath = defaultRemotePath(template?.name);
  }
}

function defaultRemotePath(name?: string) {
  const slug = (name || 'service').toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || 'service';
  return `/opt/panel/services/${slug}`;
}

async function saveService() {
  saving.value = true;
  try {
    const input = {
      name: form.name,
      templateId: form.templateId,
      serverId: form.serverId,
      remotePath: form.remotePath,
      values: {},
    };
    const creating = !form.id;
    const saved = form.id ? await composeApi.updateService(form.id, input) : await composeApi.createService(input);
    resetForm(
      {
        id: saved.id,
        name: saved.name,
        templateId: saved.templateId,
        serverId: saved.serverId,
        remotePath: saved.remotePath,
      },
    );
    if (creating && saved.lastTaskId) {
      taskId.value = saved.lastTaskId;
      taskTitle.value = `Deploy ${saved.name}`;
    }
    await load();
    drawerOpen.value = false;
    showMessage(creating && saved.lastTaskId ? 'Service created and deployment started' : 'Service saved successfully');
  } catch (err) {
    showMessage(err instanceof Error ? err.message : 'Unable to save service', 'error');
  } finally {
    saving.value = false;
  }
}

async function deleteService(service: ComposeServiceDto) {
  const mode = serviceDeleteMode(service);
  const message =
    mode === 'metadata'
      ? `Delete service ${service.name}?`
      : `Delete service ${service.name}? This will remove the remote Compose project and then release this service slot.`;
  confirm('Confirm delete', message, async () => {
    if (mode === 'metadata') {
      await composeApi.deleteService(service.id);
      await load();
      showMessage('Service deleted');
      return;
    }
    actionLoading.value = `remove:${service.id}`;
    try {
      const result = await composeApi.removeService(service.id);
      taskId.value = result.taskId;
      taskTitle.value = `Delete ${service.name}`;
      showMessage('Delete started');
    } finally {
      actionLoading.value = '';
    }
  });
}

async function previewService() {
  if (!form.id) return;
  rendering.value = true;
  try {
    renderPreview.value = await composeApi.renderService(form.id);
    showMessage('Service render preview refreshed');
  } catch (err) {
    showMessage(err instanceof Error ? err.message : 'Unable to render service', 'error');
  } finally {
    rendering.value = false;
  }
}

async function runAction(service: ComposeServiceDto, action: 'deploy' | 'sync' | 'restart' | 'stop' | 'remove') {
  const proceed = async () => {
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
      showMessage(`${action[0].toUpperCase()}${action.slice(1)} started`);
    } catch (err) {
      showMessage(err instanceof Error ? err.message : `Unable to ${action} service`, 'error');
    } finally {
      actionLoading.value = '';
    }
  };

  if (action === 'remove') {
    confirm('Confirm remove', `Remove remote Compose service ${service.name}?`, proceed);
  } else {
    await proceed();
  }
}

async function handleTaskFinished(_task: TaskDto) {
  await load();
}

onMounted(load);
</script>

<template>
  <div>
    <div class="d-flex justify-space-between align-center mb-6">
      <div>
        <h1 class="text-h4 font-weight-bold">Services</h1>
        <p class="text-subtitle-1 text-medium-emphasis">Deploy templates to servers, check sync state, and manage container lifecycles.</p>
      </div>
      <div class="d-flex" style="gap: 12px;">
        <v-btn
          prepend-icon="mdi-refresh"
          :loading="loading"
          variant="outlined"
          @click="load"
          class="text-none font-weight-bold"
        >
          Reload
        </v-btn>
        <v-btn
          color="primary"
          prepend-icon="mdi-plus"
          :disabled="templates.length === 0 || servers.length === 0"
          @click="openService()"
          class="text-none font-weight-bold"
        >
          New Service
        </v-btn>
      </div>
    </div>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>
    <v-alert
      v-if="!loading && (templates.length === 0 || servers.length === 0)"
      type="warning"
      variant="tonal"
      class="mb-4"
    >
      Create at least one service template and one server before creating services.
    </v-alert>

    <v-card :loading="loading" variant="outlined" class="mb-6">
      <v-table class="text-left" style="background: transparent;">
        <thead>
          <tr>
            <th class="font-weight-bold">Name</th>
            <th class="font-weight-bold">Template</th>
            <th class="font-weight-bold">Server</th>
            <th class="font-weight-bold">Remote Path</th>
            <th class="font-weight-bold">Runtime</th>
            <th class="font-weight-bold">Sync</th>
            <th class="font-weight-bold">Template Version</th>
            <th class="font-weight-bold text-right" style="width: 430px;">Actions</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="sortedServices.length === 0">
            <td colspan="8" class="text-center py-6 text-grey-darken-1">No deployed services</td>
          </tr>
          <tr v-for="row in sortedServices" :key="row.id">
            <td class="font-weight-bold">{{ row.name }}</td>
            <td>
              <v-chip v-if="serviceTemplateName(row)" color="success" size="small" label>{{ serviceTemplateName(row) }}</v-chip>
              <v-chip v-else color="grey" size="small" label>Unmanaged</v-chip>
            </td>
            <td>{{ serviceServerName(row) }}</td>
            <td class="text-caption text-grey-darken-1 text-truncate" style="max-width: 200px;" :title="row.remotePath">
              {{ row.remotePath }}
            </td>
            <td>
              <v-chip :color="runtimeStatusType(row)" size="small" label>
                {{ row.runtimeStatus || row.status || 'unknown' }}
              </v-chip>
            </td>
            <td>
              <v-chip :color="syncStatusType(row)" size="small" label>{{ syncStatus(row) }}</v-chip>
            </td>
            <td>
              {{ row.lastAppliedTemplateVersion ?? '-' }} / {{ row.templateVersion ?? '-' }}
            </td>
            <td class="text-right">
              <div class="d-flex justify-end" style="gap: 6px;">
                <v-btn size="small" variant="outlined" prepend-icon="mdi-magnify" @click="openService(row)">Open</v-btn>
                <!-- More actions dropdown -->
                <v-menu>
                  <template v-slot:activator="{ props }">
                    <v-btn size="small" variant="outlined" append-icon="mdi-chevron-down" v-bind="props">
                      More
                    </v-btn>
                  </template>
                  <v-list density="compact">
                    <v-list-item prepend-icon="mdi-restart" title="Restart" @click="runAction(row, 'restart')" />
                    <v-list-item prepend-icon="mdi-stop" title="Stop" @click="runAction(row, 'stop')" />
                    <v-divider class="my-1" />
                    <v-list-item prepend-icon="mdi-delete" title="Delete" class="text-error" @click="deleteService(row)" />
                  </v-list>
                </v-menu>
              </div>
            </td>
          </tr>
        </tbody>
      </v-table>
    </v-card>

    <!-- Active Task Status Panel -->
    <v-card v-if="taskId" class="mb-6 pa-4" variant="outlined">
      <v-card-title class="px-0 pt-0 text-subtitle-1 font-weight-bold">{{ taskTitle }}</v-card-title>
      <v-card-text class="px-0 pb-0">
        <TaskLogPanel :task-id="taskId" compact @finished="handleTaskFinished" />
      </v-card-text>
    </v-card>

    <!-- Slide-out Drawer for Service Details -->
    <v-navigation-drawer v-model="drawerOpen" location="right" temporary width="750" style="z-index: 1005;">
      <div class="pa-4 fill-height d-flex flex-column">
        <div class="d-flex justify-space-between align-center mb-4">
          <div class="text-h6 font-weight-bold">{{ isEditing ? form.name || 'Service' : 'New Service' }}</div>
          <div class="d-flex" style="gap: 8px;">
            <v-btn :disabled="!isEditing" :loading="rendering" prepend-icon="mdi-magnify" variant="outlined" size="small" class="text-none font-weight-bold" @click="previewService">Render</v-btn>
            <v-btn color="primary" :loading="saving" prepend-icon="mdi-content-save" variant="flat" size="small" class="text-none font-weight-bold" @click="saveService">Save</v-btn>
            <v-btn icon="mdi-close" variant="text" size="small" @click="drawerOpen = false" />
          </div>
        </div>
        <v-divider />

        <div class="overflow-y-auto flex-grow-1 py-4 d-flex flex-column" style="gap: 16px;">
          <!-- Basic Form Section -->
          <v-card variant="outlined" class="pa-4">
            <div class="service-form">
              <div>
                <v-form>
                  <v-text-field v-model="form.name" label="Service Name" placeholder="customer-site" variant="outlined" density="comfortable" class="mb-3" />

                  <v-select
                    v-model="form.templateId"
                    :items="templates"
                    item-title="name"
                    item-value="id"
                    label="Service Template"
                    placeholder="Select template"
                    variant="outlined"
                    density="comfortable"
                    class="mb-3"
                    @update:model-value="handleTemplateChange"
                  />

                  <v-select
                    v-model="form.serverId"
                    :items="servers"
                    item-title="name"
                    item-value="id"
                    label="Target Server"
                    placeholder="Select server"
                    variant="outlined"
                    density="comfortable"
                    class="mb-3"
                  />

                  <v-text-field v-model="form.remotePath" label="Remote Path on Server" placeholder="/opt/panel/services/customer-site" variant="outlined" density="comfortable" />
                </v-form>
              </div>

              <!-- Side Summary column -->
              <div class="side-summary mt-4 mt-md-0">
                <div>
                  <div class="detail-label">Template</div>
                  <strong class="text-body-2">{{ selectedTemplate?.name || '-' }}</strong>
                  <div class="text-caption text-grey-darken-1">version {{ selectedTemplate?.version ?? '-' }}</div>
                </div>
                <v-divider class="my-2" />
                <div>
                  <div class="detail-label">Server</div>
                  <strong class="text-body-2">{{ selectedServer?.name || '-' }}</strong>
                  <div class="text-caption text-grey-darken-1">{{ selectedServer?.host || '-' }}</div>
                </div>
                <v-divider class="my-2" />
                <div>
                  <div class="detail-label">Template variables</div>
                  <v-chip size="small" color="primary">{{ selectedTemplate?.variables?.length ?? 0 }}</v-chip>
                </div>
                <v-divider class="my-2" />
                <div>
                  <div class="detail-label">Dependencies</div>
                  <div class="dependency-list mt-1">
                    <v-chip v-for="template in dependencyTemplates" :key="template.id" color="warning" size="x-small" label>{{ template.name }}</v-chip>
                    <span v-if="dependencyTemplates.length === 0" class="text-caption text-grey-darken-1">None</span>
                  </div>
                </div>
              </div>
            </div>
          </v-card>

          <!-- Deployment Plan Section -->
          <v-card variant="outlined" class="pa-4">
            <div class="text-subtitle-2 font-weight-bold mb-3">Deployment Plan</div>
            <v-table density="compact" style="background: transparent;">
              <tbody>
                <tr>
                  <td class="font-weight-bold text-caption text-grey-darken-1 px-0 py-2 text-uppercase" style="width: 180px;">Server</td>
                  <td class="px-0 py-2">{{ selectedServer?.name || '-' }}</td>
                </tr>
                <tr>
                  <td class="font-weight-bold text-caption text-grey-darken-1 px-0 py-2 text-uppercase">Template</td>
                  <td class="px-0 py-2">{{ selectedTemplate?.name || '-' }}</td>
                </tr>
                <tr>
                  <td class="font-weight-bold text-caption text-grey-darken-1 px-0 py-2 text-uppercase">Auto-deploy dependencies</td>
                  <td class="px-0 py-2">
                    <div class="dependency-list">
                      <v-chip v-for="template in dependencyTemplates" :key="template.id" color="warning" size="x-small" label>{{ template.name }}</v-chip>
                      <span v-if="dependencyTemplates.length === 0" class="text-caption text-grey-darken-1">None</span>
                    </div>
                  </td>
                </tr>
              </tbody>
            </v-table>
          </v-card>

          <!-- Rendered Config Preview -->
          <v-card variant="outlined" class="pa-4">
            <div class="text-subtitle-2 font-weight-bold mb-3">Rendered Config</div>
            <div v-if="!renderPreview" class="text-center py-6 text-grey-darken-1 text-caption">
              Save the service, then click Render preview
            </div>
            <pre v-else class="code-preview">{{ renderPreview.renderedYaml }}</pre>
          </v-card>
        </div>
      </div>
    </v-navigation-drawer>

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
.service-form {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 260px;
  gap: 24px;
}

.side-summary {
  display: flex;
  flex-direction: column;
  border: 1px solid rgba(var(--v-border-color), 0.12);
  border-radius: 8px;
  padding: 16px;
  background-color: rgba(var(--v-border-color), 0.05);
}

.dependency-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
}

.detail-label {
  color: rgba(var(--v-theme-on-surface), 0.6);
  font-size: 11px;
  text-transform: uppercase;
  font-weight: 700;
  margin-bottom: 4px;
}

.code-preview {
  min-height: 260px;
  max-height: 480px;
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

@media (max-width: 960px) {
  .service-form {
    grid-template-columns: 1fr;
  }
}
</style>
