<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import { Refresh } from '@element-plus/icons-vue';
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
    if (!tasks.value.some((task) => task.id === selectedTaskId.value)) {
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
    <div class="panel-header panel">
      <div>
        <p class="page-subtitle">Inspect running and recent operations with cursor-based log polling.</p>
      </div>
      <div class="toolbar">
        <el-select v-model="serverFilter" class="filter server-filter" placeholder="Server" clearable @change="reloadFirstPage">
          <el-option label="All servers" value="" />
          <el-option v-for="server in servers" :key="server.id" :label="server.name" :value="server.id">
            <span>{{ server.name }}</span>
            <span class="option-meta">{{ server.host }}</span>
          </el-option>
        </el-select>
        <el-select
          v-model="typeFilter"
          class="filter type-filter"
          placeholder="Type"
          clearable
          filterable
          @change="reloadFirstPage"
        >
          <el-option label="All types" value="" />
          <el-option
            v-for="type in taskTypeOptions"
            :key="type"
            :label="formatTaskType(type)"
            :value="type"
          />
        </el-select>
        <el-select v-model="statusFilter" class="filter" placeholder="Status" @change="reloadFirstPage">
          <el-option label="All" value="all" />
          <el-option label="Queued" value="queued" />
          <el-option label="Running" value="running" />
          <el-option label="Completed" value="completed" />
          <el-option label="Failed" value="failed" />
          <el-option label="Cancelled" value="cancelled" />
        </el-select>
        <el-button @click="clearFilters">Clear</el-button>
        <el-button :icon="Refresh" :loading="loading" @click="loadTasks">Refresh</el-button>
      </div>
    </div>

    <el-alert v-if="error" class="page-alert" type="error" :title="error" show-icon />

    <div class="task-grid">
      <section class="panel" v-loading="loading">
        <el-table
          :data="tasks"
          highlight-current-row
          empty-text="No tasks"
          @row-click="selectedTaskId = $event.id"
        >
          <el-table-column label="Type" min-width="180">
            <template #default="{ row }">{{ formatTaskType(row.type) }}</template>
          </el-table-column>
          <el-table-column label="Server" min-width="160">
            <template #default="{ row }">{{ serverName(row.serverId) }}</template>
          </el-table-column>
          <el-table-column prop="status" label="Status" width="120">
            <template #default="{ row }">
              <el-tag
                :type="row.status === 'failed' ? 'danger' : row.status === 'completed' ? 'success' : 'warning'"
              >
                {{ row.status }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="stage" label="Stage" width="130" />
          <el-table-column prop="summary" label="Summary" min-width="220" />
        </el-table>
        <div class="pagination-row">
          <el-pagination
            v-model:current-page="page"
            v-model:page-size="pageSize"
            :page-sizes="[10, 20, 50, 100]"
            :total="total"
            layout="total, sizes, prev, pager, next"
            background
            @current-change="handlePageChange"
            @size-change="handlePageSizeChange"
          />
        </div>
      </section>

      <section class="panel">
        <div class="panel-header"><strong>Task Detail</strong></div>
        <div class="panel-body">
          <TaskLogPanel
            v-if="selectedTaskId"
            :task-id="selectedTaskId"
            :server-name="selectedTaskServerName()"
            @finished="loadTasks"
          />
          <el-empty v-else description="Select a task" />
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.page-alert,
.task-grid {
  margin-top: 20px;
}

.filter {
  width: 160px;
}

.server-filter {
  width: 220px;
}

.type-filter {
  width: 200px;
}

.option-meta {
  float: right;
  color: #98a2b3;
  font-size: 12px;
  margin-left: 16px;
}

.task-grid {
  display: grid;
  grid-template-columns: minmax(420px, 1fr) minmax(420px, 1fr);
  gap: 20px;
}

.pagination-row {
  display: flex;
  justify-content: flex-end;
  padding: 14px 16px 16px;
}
</style>
