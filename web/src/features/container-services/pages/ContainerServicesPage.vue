<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { containerServicesApi } from '@/api/containerServices';
import type { ContainerServiceDto, ContainerServiceRuntimeOperationDto, DependencyImpactPreviewDto } from '@/types/api';
import ServiceDetail from '../components/ServiceDetail.vue';
import ServiceEditor from '../components/ServiceEditor.vue';
import DependencyImpactDialog from '../components/DependencyImpactDialog.vue';

const route = useRoute();
const router = useRouter();
const services = ref<ContainerServiceDto[]>([]);
const selectedId = ref('');
const editorOpen = ref(false);
const editingService = ref<ContainerServiceDto | null>(null);
const dependencyDialog = ref(false);
const dependencyPreview = ref<DependencyImpactPreviewDto | null>(null);
const pendingDependencyAction = ref<'enable' | 'disable' | ''>('');
const taskMessage = ref('');
const loading = ref(false);
const actionLoading = ref('');
const error = ref('');

const selectedService = computed(() => services.value.find((service) => service.id === selectedId.value) ?? null);
const serviceCount = computed(() => services.value.length);
const enabledCount = computed(() => services.value.filter((service) => service.enabled).length);
const unhealthyCount = computed(() => services.value.filter((service) => ['missing', 'unhealthy', 'exited', 'stale'].includes(service.runtimeStatus || '')).length);

function statusColor(service: ContainerServiceDto) {
  const status = service.runtimeStatus || 'unknown';
  if (status === 'healthy' || status === 'running') return 'success';
  if (status === 'starting' || status === 'stale') return 'warning';
  if (status === 'missing' || status === 'unhealthy' || status === 'exited') return 'error';
  return 'grey';
}

function generationLabel(service: ContainerServiceDto) {
  const runtimeGeneration = service.runtimeGeneration ?? null;
  if (runtimeGeneration == null) return `db ${service.generation}`;
  return runtimeGeneration === service.generation ? `gen ${service.generation}` : `db ${service.generation} / runtime ${runtimeGeneration}`;
}

function isGenerationMismatched(service: ContainerServiceDto) {
  return service.runtimeGeneration != null && service.runtimeGeneration !== service.generation;
}

async function load() {
  loading.value = true;
  try {
    services.value = await containerServicesApi.list();
    if (route.query.service && typeof route.query.service === 'string') {
      selectedId.value = route.query.service;
    }
    if (!selectedId.value && services.value.length) selectedId.value = services.value[0].id;
    if (selectedId.value && !services.value.some((service) => service.id === selectedId.value)) {
      selectedId.value = services.value[0]?.id ?? '';
    }
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to load Container Services';
  } finally {
    loading.value = false;
  }
}

function createService() {
  editingService.value = null;
  editorOpen.value = true;
}

function editService(service: ContainerServiceDto) {
  editingService.value = service;
  editorOpen.value = true;
}

async function handleSaved(service: ContainerServiceDto) {
  editorOpen.value = false;
  taskMessage.value = service.lastTask
    ? `Save accepted. Reconcile task ${service.lastTask.id} is ${service.lastTask.status}.`
    : service.enabled
      ? 'Save accepted. Enabled services reconcile automatically when the backend reports the task.'
      : 'Save accepted.';
  await load();
  selectedId.value = service.id;
}

function showTaskResult(result: ContainerServiceRuntimeOperationDto, fallback: string) {
  const task = result.tasks?.[0];
  taskMessage.value = task ? `${fallback}: ${task.id}` : result.taskId ? `${fallback}: ${result.taskId}` : fallback;
}

async function quickAction(action: 'reconcile' | 'restart' | 'delete', service: ContainerServiceDto) {
  actionLoading.value = `${action}:${service.id}`;
  try {
    if (action === 'delete') {
      await containerServicesApi.delete(service.id);
      taskMessage.value = `Deleted ${service.name}`;
    } else {
      const result = action === 'reconcile' ? await containerServicesApi.reconcile(service.id) : await containerServicesApi.restart(service.id);
      showTaskResult(result, `${action} queued`);
    }
    await load();
  } catch (err) {
    error.value = err instanceof Error ? err.message : `Unable to ${action} service`;
  } finally {
    actionLoading.value = '';
  }
}

async function previewDependencyAction(action: 'enable' | 'disable', service: ContainerServiceDto) {
  actionLoading.value = `${action}:${service.id}`;
  try {
    dependencyPreview.value = action === 'enable'
      ? await containerServicesApi.enablePreview(service.id)
      : await containerServicesApi.disablePreview(service.id);
    pendingDependencyAction.value = action;
    dependencyDialog.value = true;
  } catch (err) {
    error.value = err instanceof Error ? err.message : `Unable to preview ${action}`;
  } finally {
    actionLoading.value = '';
  }
}

async function confirmDependencyAction() {
  if (!selectedService.value || !pendingDependencyAction.value) return;
  actionLoading.value = `${pendingDependencyAction.value}:${selectedService.value.id}`;
  try {
    const result = pendingDependencyAction.value === 'enable'
      ? await containerServicesApi.enable(selectedService.value.id)
      : await containerServicesApi.disable(selectedService.value.id);
    showTaskResult(result, `${pendingDependencyAction.value} operation queued`);
    dependencyDialog.value = false;
    await load();
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to confirm dependency action';
  } finally {
    actionLoading.value = '';
  }
}

watch(selectedId, (id) => {
  const query = { ...route.query };
  if (id) query.service = id;
  void router.replace({ query });
});

onMounted(load);
</script>

<template>
  <div>
    <div class="page-heading mb-5">
      <div class="page-heading-copy">
        <div class="eyebrow">Docker control plane</div>
        <h1 class="text-h4 font-weight-bold">Container Services</h1>
        <p class="text-subtitle-1 text-medium-emphasis">Create, validate, schedule, reconcile, and observe managed Compose service bodies.</p>
      </div>
      <div class="page-actions">
        <v-btn prepend-icon="mdi-refresh" variant="outlined" :loading="loading" class="text-none action-btn" @click="load">Refresh</v-btn>
        <v-btn prepend-icon="mdi-plus" color="primary" variant="flat" class="text-none font-weight-bold action-btn" @click="createService">Create</v-btn>
      </div>
    </div>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>
    <v-alert v-if="taskMessage" type="info" variant="tonal" class="mb-4" closable @click:close="taskMessage = ''">{{ taskMessage }}</v-alert>

    <div class="summary-strip mb-4">
      <v-card variant="outlined" class="summary-card">
        <div class="summary-icon surface-primary"><v-icon size="18">mdi-view-grid-outline</v-icon></div>
        <div>
          <div class="text-caption text-medium-emphasis">Services</div>
          <div class="text-h5 font-weight-bold font-tabular">{{ serviceCount }}</div>
        </div>
      </v-card>
      <v-card variant="outlined" class="summary-card">
        <div class="summary-icon surface-success"><v-icon size="18">mdi-toggle-switch-outline</v-icon></div>
        <div>
          <div class="text-caption text-medium-emphasis">Enabled</div>
          <div class="text-h5 font-weight-bold font-tabular">{{ enabledCount }}</div>
        </div>
      </v-card>
      <v-card variant="outlined" class="summary-card">
        <div class="summary-icon surface-warning"><v-icon size="18">mdi-alert-circle-outline</v-icon></div>
        <div>
          <div class="text-caption text-medium-emphasis">Needs attention</div>
          <div class="text-h5 font-weight-bold font-tabular">{{ unhealthyCount }}</div>
        </div>
      </v-card>
    </div>

    <div class="services-workspace">
      <v-card variant="outlined" :loading="loading" class="service-list">
        <div class="list-header">
          <div>
            <div class="text-subtitle-1 font-weight-bold">Managed services</div>
            <div class="text-caption text-medium-emphasis">Select a row to inspect runtime details and logs.</div>
          </div>
          <v-chip size="small" variant="tonal" color="primary" label class="font-tabular">{{ serviceCount }} total</v-chip>
        </div>
        <v-table class="text-left service-table">
          <thead>
            <tr>
              <th class="name-column">Name</th>
              <th>Enabled</th>
              <th>Runtime</th>
              <th>Node</th>
              <th>Generation</th>
              <th>Last task</th>
              <th class="text-right actions-column">Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="services.length === 0">
              <td colspan="7" class="text-center py-8 text-medium-emphasis">No Container Services</td>
            </tr>
            <tr
              v-for="service in services"
              :key="service.id"
              class="cursor-pointer service-row"
              :class="{ selected: selectedId === service.id }"
              @click="selectedId = service.id"
            >
              <td class="name-cell">
                <div class="service-name-line">
                  <span class="status-dot" :class="statusColor(service)" />
                  <div class="min-width-0">
                    <div class="font-weight-bold text-truncate">{{ service.name }}</div>
                    <div v-if="service.lastError" class="text-caption text-error text-truncate">{{ service.lastError }}</div>
                    <div v-else class="text-caption text-medium-emphasis text-truncate">{{ service.id }}</div>
                  </div>
                </div>
              </td>
              <td><v-chip :color="service.enabled ? 'success' : 'grey'" size="small" variant="tonal" label>{{ service.enabled ? 'enabled' : 'disabled' }}</v-chip></td>
              <td><v-chip :color="statusColor(service)" size="small" variant="tonal" label>{{ service.runtimeStatus || 'unknown' }}</v-chip></td>
              <td class="text-truncate node-cell">{{ service.nodeName || service.nodeId || '-' }}</td>
              <td>
                <v-chip :color="isGenerationMismatched(service) ? 'warning' : 'info'" size="small" variant="tonal" label class="font-tabular">{{ generationLabel(service) }}</v-chip>
              </td>
              <td class="text-caption text-truncate task-cell">{{ service.lastTask?.summary || service.lastTaskId || '-' }}</td>
              <td class="text-right">
                <div class="row-actions">
                  <v-tooltip text="Edit service">
                    <template #activator="{ props }">
                      <v-btn v-bind="props" size="small" icon="mdi-pencil" variant="text" @click.stop="editService(service)" />
                    </template>
                  </v-tooltip>
                  <v-tooltip text="Reconcile current generation">
                    <template #activator="{ props }">
                      <v-btn v-bind="props" size="small" icon="mdi-sync" variant="text" :loading="actionLoading === `reconcile:${service.id}`" @click.stop="quickAction('reconcile', service)" />
                    </template>
                  </v-tooltip>
                  <v-tooltip text="Restart runtime containers">
                    <template #activator="{ props }">
                      <v-btn v-bind="props" size="small" icon="mdi-restart" variant="text" :disabled="!service.enabled" :loading="actionLoading === `restart:${service.id}`" @click.stop="quickAction('restart', service)" />
                    </template>
                  </v-tooltip>
                  <v-tooltip :text="service.enabled ? 'Disable service' : 'Enable service'">
                    <template #activator="{ props }">
                      <v-btn
                        v-if="service.enabled"
                        v-bind="props"
                        size="small"
                        icon="mdi-toggle-switch-off-outline"
                        variant="text"
                        :loading="actionLoading === `disable:${service.id}`"
                        @click.stop="previewDependencyAction('disable', service)"
                      />
                      <v-btn
                        v-else
                        v-bind="props"
                        size="small"
                        icon="mdi-toggle-switch-outline"
                        variant="text"
                        :loading="actionLoading === `enable:${service.id}`"
                        @click.stop="previewDependencyAction('enable', service)"
                      />
                    </template>
                  </v-tooltip>
                  <v-tooltip text="Delete disabled service">
                    <template #activator="{ props }">
                      <v-btn v-bind="props" size="small" icon="mdi-delete" color="error" variant="text" :disabled="service.enabled" :loading="actionLoading === `delete:${service.id}`" @click.stop="quickAction('delete', service)" />
                    </template>
                  </v-tooltip>
                </div>
              </td>
            </tr>
          </tbody>
        </v-table>
      </v-card>

      <div class="detail-column">
        <ServiceDetail v-if="selectedService" :service="selectedService" />
        <v-card v-else variant="outlined" class="empty-detail">
          <v-icon size="28" color="medium-emphasis">mdi-cube-scan</v-icon>
          <div class="text-body-2 text-medium-emphasis">Select a service to inspect runtime, labels, logs, and tasks.</div>
        </v-card>
      </div>
    </div>

    <ServiceEditor :service="editingService" :open="editorOpen" @close="editorOpen = false" @saved="handleSaved" />
    <DependencyImpactDialog
      v-model="dependencyDialog"
      :preview="dependencyPreview"
      :loading="Boolean(actionLoading)"
      @confirm="confirmDependencyAction"
    />
  </div>
</template>

<style scoped>
.summary-strip {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  max-width: 640px;
}

.page-heading {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 20px;
}

.page-heading-copy {
  min-width: 0;
}

.page-heading-copy h1 {
  text-wrap: balance;
}

.page-heading-copy p {
  margin-top: 4px;
  text-wrap: pretty;
}

.eyebrow {
  margin-bottom: 4px;
  color: rgb(var(--v-theme-primary));
  font-size: 0.72rem;
  font-weight: 700;
  letter-spacing: 0;
  text-transform: uppercase;
}

.page-actions {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.action-btn {
  min-height: 40px;
}

.summary-card {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px;
}

.summary-icon {
  display: grid;
  place-items: center;
  width: 36px;
  height: 36px;
  border-radius: 8px;
}

.surface-primary {
  color: rgb(var(--v-theme-primary));
  background: rgba(var(--v-theme-primary), 0.1);
}

.surface-success {
  color: rgb(var(--v-theme-success));
  background: rgba(var(--v-theme-success), 0.1);
}

.surface-warning {
  color: rgb(var(--v-theme-warning));
  background: rgba(var(--v-theme-warning), 0.12);
}

.services-workspace {
  display: grid;
  grid-template-columns: minmax(720px, 1.12fr) minmax(420px, 0.88fr);
  gap: 18px;
  align-items: start;
}

.service-list,
.detail-column {
  min-width: 0;
}

.service-list {
  overflow: hidden;
}

.list-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 16px 18px 10px;
}

.service-table {
  background: transparent;
}

.service-table :deep(th) {
  height: 40px;
  color: rgba(var(--v-theme-on-surface), 0.58);
  font-size: 0.74rem;
  font-weight: 700;
  letter-spacing: 0;
  text-transform: uppercase;
  white-space: nowrap;
}

.service-table :deep(td) {
  height: 58px;
  border-bottom-color: rgba(var(--v-border-color), 0.06);
  vertical-align: middle;
}

.name-column {
  min-width: 220px;
}

.actions-column {
  width: 220px;
}

.cursor-pointer {
  cursor: pointer;
}

.service-row {
  transition-property: background-color;
  transition-duration: 160ms;
  transition-timing-function: ease;
}

.service-row:hover {
  background: rgba(var(--v-theme-on-surface), 0.025);
}

.service-row.selected {
  background: rgba(var(--v-theme-primary), 0.06);
}

.name-cell {
  max-width: 260px;
}

.service-name-line {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.min-width-0 {
  min-width: 0;
}

.node-cell {
  max-width: 150px;
}

.task-cell {
  max-width: 180px;
}

.row-actions {
  display: flex;
  justify-content: flex-end;
  gap: 2px;
}

.row-actions :deep(.v-btn) {
  min-width: 40px;
  min-height: 40px;
}

.status-dot {
  flex: 0 0 auto;
  width: 9px;
  height: 9px;
  border-radius: 999px;
  background: rgb(var(--v-theme-on-surface-variant));
  box-shadow: 0 0 0 4px rgba(var(--v-theme-on-surface), 0.04);
}

.status-dot.success {
  background: rgb(var(--v-theme-success));
  box-shadow: 0 0 0 4px rgba(var(--v-theme-success), 0.12);
}

.status-dot.warning {
  background: rgb(var(--v-theme-warning));
  box-shadow: 0 0 0 4px rgba(var(--v-theme-warning), 0.14);
}

.status-dot.error {
  background: rgb(var(--v-theme-error));
  box-shadow: 0 0 0 4px rgba(var(--v-theme-error), 0.12);
}

.empty-detail {
  display: grid;
  place-items: center;
  gap: 10px;
  min-height: 220px;
  padding: 32px;
  text-align: center;
}

@media (max-width: 1360px) {
  .services-workspace {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 760px) {
  .page-heading {
    flex-direction: column;
  }

  .page-actions,
  .page-actions .v-btn {
    width: 100%;
  }

  .summary-strip {
    grid-template-columns: 1fr;
    max-width: none;
  }
}
</style>
