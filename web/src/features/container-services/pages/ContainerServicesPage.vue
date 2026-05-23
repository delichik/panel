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
    <div class="d-flex justify-space-between align-center mb-5">
      <div>
        <h1 class="text-h4 font-weight-bold">Container Services</h1>
        <p class="text-subtitle-1 text-medium-emphasis">Create, validate, schedule, reconcile, and observe managed Compose service bodies.</p>
      </div>
      <div class="d-flex" style="gap: 10px;">
        <v-btn prepend-icon="mdi-refresh" variant="outlined" :loading="loading" class="text-none" @click="load">Refresh</v-btn>
        <v-btn prepend-icon="mdi-plus" color="primary" variant="flat" class="text-none font-weight-bold" @click="createService">Create</v-btn>
      </div>
    </div>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>
    <v-alert v-if="taskMessage" type="info" variant="tonal" class="mb-4" closable @click:close="taskMessage = ''">{{ taskMessage }}</v-alert>

    <div class="summary-strip mb-4">
      <v-card variant="outlined" class="pa-4"><div class="text-caption text-medium-emphasis">Services</div><div class="text-h5 font-weight-bold font-tabular">{{ serviceCount }}</div></v-card>
      <v-card variant="outlined" class="pa-4"><div class="text-caption text-medium-emphasis">Enabled</div><div class="text-h5 font-weight-bold font-tabular">{{ enabledCount }}</div></v-card>
      <v-card variant="outlined" class="pa-4"><div class="text-caption text-medium-emphasis">Needs attention</div><div class="text-h5 font-weight-bold font-tabular">{{ unhealthyCount }}</div></v-card>
    </div>

    <div class="services-workspace">
      <v-card variant="outlined" :loading="loading" class="service-list">
        <v-table class="text-left">
          <thead>
            <tr>
              <th>Name</th>
              <th>Enabled</th>
              <th>Runtime</th>
              <th>Node</th>
              <th>Generation</th>
              <th>Last task</th>
              <th class="text-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="services.length === 0">
              <td colspan="7" class="text-center py-8 text-medium-emphasis">No Container Services</td>
            </tr>
            <tr
              v-for="service in services"
              :key="service.id"
              class="cursor-pointer"
              :class="{ selected: selectedId === service.id }"
              @click="selectedId = service.id"
            >
              <td>
                <div class="font-weight-bold">{{ service.name }}</div>
                <div v-if="service.lastError" class="text-caption text-error text-truncate" style="max-width: 220px;">{{ service.lastError }}</div>
              </td>
              <td><v-chip :color="service.enabled ? 'success' : 'grey'" size="small" label>{{ service.enabled ? 'enabled' : 'disabled' }}</v-chip></td>
              <td><v-chip :color="statusColor(service)" size="small" label>{{ service.runtimeStatus || 'unknown' }}</v-chip></td>
              <td>{{ service.nodeName || service.nodeId || '-' }}</td>
              <td>
                <v-chip :color="isGenerationMismatched(service) ? 'warning' : 'info'" size="small" label>{{ generationLabel(service) }}</v-chip>
              </td>
              <td class="text-caption">{{ service.lastTask?.summary || service.lastTaskId || '-' }}</td>
              <td class="text-right">
                <div class="d-flex justify-end" style="gap: 6px;">
                  <v-btn size="small" icon="mdi-pencil" variant="text" @click.stop="editService(service)" />
                  <v-btn size="small" icon="mdi-sync" variant="text" :loading="actionLoading === `reconcile:${service.id}`" @click.stop="quickAction('reconcile', service)" />
                  <v-btn size="small" icon="mdi-restart" variant="text" :disabled="!service.enabled" :loading="actionLoading === `restart:${service.id}`" @click.stop="quickAction('restart', service)" />
                  <v-btn
                    v-if="service.enabled"
                    size="small"
                    icon="mdi-toggle-switch-off-outline"
                    variant="text"
                    :loading="actionLoading === `disable:${service.id}`"
                    @click.stop="previewDependencyAction('disable', service)"
                  />
                  <v-btn
                    v-else
                    size="small"
                    icon="mdi-toggle-switch-outline"
                    variant="text"
                    :loading="actionLoading === `enable:${service.id}`"
                    @click.stop="previewDependencyAction('enable', service)"
                  />
                  <v-btn size="small" icon="mdi-delete" color="error" variant="text" :disabled="service.enabled" :loading="actionLoading === `delete:${service.id}`" @click.stop="quickAction('delete', service)" />
                </div>
              </td>
            </tr>
          </tbody>
        </v-table>
      </v-card>

      <div class="detail-column">
        <ServiceDetail v-if="selectedService" :service="selectedService" />
        <v-card v-else variant="outlined" class="pa-8 text-center text-medium-emphasis">Select a service to inspect runtime, labels, logs, and tasks.</v-card>
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
  grid-template-columns: repeat(3, minmax(0, 180px));
  gap: 12px;
}

.services-workspace {
  display: grid;
  grid-template-columns: minmax(720px, 1.15fr) minmax(420px, 0.85fr);
  gap: 18px;
  align-items: start;
}

.service-list,
.detail-column {
  min-width: 0;
}

.cursor-pointer {
  cursor: pointer;
}

tr.selected {
  background: rgba(var(--v-theme-primary), 0.06);
}
</style>
