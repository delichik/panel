<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import TaskLogPanel from '@/components/tasks/TaskLogPanel.vue';
import { tasksApi } from '@/api/tasks';
import { serversApi } from '@/api/servers';
import type { ServerDto, TaskDto, TaskStatus } from '@/types/api';

const tasks = ref<TaskDto[]>([]);
const servers = ref<ServerDto[]>([]);
const selectedTaskId = ref('');
const statusFilter = ref<TaskStatus | 'all'>('all');
const serverFilter = ref('');
const typeFilter = ref('');
const loading = ref(false);
const error = ref('');
const page = ref(1);
const pageSize = ref(20);
const total = ref(0);
let timer: number | undefined;

const taskTypeOptions = computed(() => {
  const known = new Set(tasks.value.map((task) => task.type));
  return Array.from(known).sort();
});

const totalPages = computed(() => Math.ceil(total.value / pageSize.value));

async function loadTasks() {
  loading.value = true;
  try {
    const [taskPage, serverRows] = await Promise.all([
      tasksApi.list({
        status: statusFilter.value,
        serverId: serverFilter.value,
        type: typeFilter.value.trim(),
        page: page.value,
        pageSize: pageSize.value,
      }),
      serversApi.listServers(),
    ]);
    tasks.value = taskPage.items ?? [];
    total.value = taskPage.total ?? 0;
    servers.value = serverRows ?? [];
    if (!tasks.value.some((task) => task.id === selectedTaskId.value) && tasks.value.length > 0) {
      selectedTaskId.value = tasks.value[0]?.id ?? '';
    }
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to load tasks';
  } finally {
    loading.value = false;
  }
}

function reloadFirstPage() {
  page.value = 1;
  void loadTasks();
}

function handlePageChange(nextPage: number) {
  page.value = nextPage;
  void loadTasks();
}

function handlePageSizeChange(nextSize: number) {
  pageSize.value = nextSize;
  page.value = 1;
  void loadTasks();
}

function serverName(serverId?: string | null) {
  if (!serverId) return 'No server';
  return servers.value.find((server) => server.id === serverId)?.name || serverId;
}

function formatTaskType(value: string) {
  return value.replace(/_/g, ' ');
}

function canRunNow(task: TaskDto) {
  return ['queued', 'scheduled', 'failed_retryable'].includes(task.status);
}

function taskStatusColor(status: TaskStatus) {
  if (status === 'failed' || status === 'blocked') return 'error';
  if (status === 'completed') return 'success';
  if (status === 'running') return 'primary';
  if (status === 'failed_retryable' || status === 'scheduled') return 'warning';
  return 'info';
}

function formatNextRun(task: TaskDto) {
  if (!task.nextRunAt) return '-';
  return new Date(task.nextRunAt).toLocaleString();
}

async function runNow(task: TaskDto) {
  try {
    const updated = await tasksApi.runNow(task.id);
    const idx = tasks.value.findIndex((item) => item.id === updated.id);
    if (idx >= 0) tasks.value[idx] = updated;
    selectedTaskId.value = updated.id;
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to run task now';
  }
}

function selectedTaskServerName() {
  const selected = tasks.value.find((task) => task.id === selectedTaskId.value);
  return serverName(selected?.serverId);
}

function startPolling() {
  if (timer) window.clearInterval(timer);
  void loadTasks();
  timer = window.setInterval(loadTasks, 5000);
}

function clearFilters() {
  statusFilter.value = 'all';
  serverFilter.value = '';
  typeFilter.value = '';
  page.value = 1;
  void loadTasks();
}

onMounted(startPolling);
onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer);
});
</script>

<template>
  <div>
    <div class="d-flex justify-space-between align-center mb-6">
      <div>
        <h1 class="text-h4 font-weight-bold">Task Center</h1>
        <p class="text-subtitle-1 text-medium-emphasis">Inspect running and recent operations with real-time log streaming.</p>
      </div>
    </div>

    <!-- Filters Header Card -->
    <v-card variant="outlined" class="pa-4 mb-6">
      <div class="d-flex flex-wrap align-center" style="gap: 12px;">
        <v-select
          v-model="serverFilter"
          :items="servers"
          item-title="name"
          item-value="id"
          label="Server"
          placeholder="All servers"
          variant="outlined"
          density="compact"
          hide-details
          clearable
          style="max-width: 220px;"
          @update:model-value="reloadFirstPage"
        />

        <v-select
          v-model="typeFilter"
          :items="taskTypeOptions"
          label="Type"
          placeholder="All types"
          variant="outlined"
          density="compact"
          hide-details
          clearable
          filterable
          style="max-width: 200px;"
          @update:model-value="reloadFirstPage"
        />

        <v-select
          v-model="statusFilter"
          :items="[
            { title: 'All', value: 'all' },
            { title: 'Queued', value: 'queued' },
            { title: 'Scheduled', value: 'scheduled' },
            { title: 'Running', value: 'running' },
            { title: 'Completed', value: 'completed' },
            { title: 'Failed', value: 'failed' },
            { title: 'Retrying', value: 'failed_retryable' },
            { title: 'Blocked', value: 'blocked' },
            { title: 'Cancelled', value: 'cancelled' }
          ]"
          item-title="title"
          item-value="value"
          label="Status"
          variant="outlined"
          density="compact"
          hide-details
          style="max-width: 160px;"
          @update:model-value="reloadFirstPage"
        />

        <v-btn variant="outlined" class="text-none font-weight-bold" @click="clearFilters">
          Clear
        </v-btn>
        <v-btn prepend-icon="mdi-refresh" :loading="loading" color="primary" class="text-none font-weight-bold" @click="loadTasks">
          Refresh
        </v-btn>
      </div>
    </v-card>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>

    <div class="task-grid">
      <!-- Tasks List Table -->
      <v-card :loading="loading" variant="outlined" class="d-flex flex-column">
        <v-table class="text-left flex-grow-1" style="background: transparent;">
          <thead>
            <tr>
              <th class="font-weight-bold">Type</th>
              <th class="font-weight-bold">Server</th>
              <th class="font-weight-bold">Status</th>
              <th class="font-weight-bold">Stage</th>
              <th class="font-weight-bold">Retry</th>
              <th class="font-weight-bold">Next Run</th>
              <th class="font-weight-bold">Summary</th>
              <th class="font-weight-bold text-right">Action</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="tasks.length === 0">
              <td colspan="8" class="text-center py-6 text-grey-darken-1">No tasks</td>
            </tr>
            <tr
              v-for="row in tasks"
              :key="row.id"
              @click="selectedTaskId = row.id"
              :class="{ 'selected-row': selectedTaskId === row.id }"
              class="cursor-pointer"
            >
              <td class="font-weight-bold text-truncate" style="max-width: 150px;">{{ formatTaskType(row.type) }}</td>
              <td>{{ serverName(row.serverId) }}</td>
              <td>
                <v-chip :color="taskStatusColor(row.status)" size="x-small" label>
                  {{ row.status }}
                </v-chip>
              </td>
              <td>{{ row.stage || '-' }}</td>
              <td class="font-tabular">{{ row.retryCount || 0 }} / {{ row.maxRetries || 0 }}</td>
              <td class="text-caption font-tabular">{{ formatNextRun(row) }}</td>
              <td class="text-caption text-truncate" style="max-width: 200px;" :title="row.summary">{{ row.summary || '-' }}</td>
              <td class="text-right">
                <v-btn
                  v-if="canRunNow(row)"
                  size="x-small"
                  variant="outlined"
                  prepend-icon="mdi-play"
                  @click.stop="runNow(row)"
                >
                  Run now
                </v-btn>
              </td>
            </tr>
          </tbody>
        </v-table>

        <v-divider />

        <!-- Pagination -->
        <div class="pagination-row d-flex align-center justify-end pa-3" style="gap: 16px;">
          <div class="d-flex align-center" style="gap: 8px;">
            <span class="text-caption text-grey-darken-1">Rows per page:</span>
            <v-select
              v-model="pageSize"
              :items="[10, 20, 50, 100]"
              density="compact"
              hide-details
              variant="outlined"
              style="width: 90px;"
              @update:model-value="handlePageSizeChange"
            />
          </div>
          <v-pagination
            v-model="page"
            :length="totalPages"
            density="compact"
            total-visible="5"
            @update:model-value="handlePageChange"
          />
        </div>
      </v-card>

      <!-- Task Details & Logs -->
      <v-card variant="outlined" class="pa-4">
        <v-card-title class="px-0 pt-0 text-subtitle-1 font-weight-bold mb-3">Task Detail & Logs</v-card-title>
        <v-divider class="mb-4" />
        <v-card-text class="pa-0">
          <TaskLogPanel
            v-if="selectedTaskId"
            :key="selectedTaskId"
            :task-id="selectedTaskId"
            :server-name="selectedTaskServerName()"
            @finished="loadTasks"
          />
          <div v-else class="text-center py-10 text-grey-darken-1 text-caption">
            <v-icon size="40" class="mb-2" color="grey-lighten-1">mdi-clipboard-text-outline</v-icon>
            <div>Select a task from the list to view logs</div>
          </div>
        </v-card-text>
      </v-card>
    </div>
  </div>
</template>

<style scoped>
.cursor-pointer {
  cursor: pointer;
}

.selected-row {
  background-color: rgba(var(--v-theme-primary), 0.08) !important;
}

.task-grid {
  display: grid;
  grid-template-columns: minmax(420px, 1.2fr) minmax(420px, 1fr);
  gap: 24px;
}

.pagination-row {
  background-color: rgba(var(--v-border-color), 0.04);
}
</style>
