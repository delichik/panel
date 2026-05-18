<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue';
import { tasksApi } from '@/api/tasks';
import type { TaskDto, TaskLogDto } from '@/types/api';

const props = defineProps<{
  taskId: string;
  compact?: boolean;
  serverName?: string;
}>();
const emit = defineEmits<{
  updated: [task: TaskDto];
  finished: [task: TaskDto];
}>();

const task = ref<TaskDto | null>(null);
const logs = ref<TaskLogDto[]>([]);
const cursor = ref(0);
const loading = ref(false);
const error = ref('');
const finishedEmitted = ref(false);
let timer: number | undefined;

const statusType = computed(() => {
  switch (task.value?.status) {
    case 'completed':
      return 'success';
    case 'failed':
      return 'danger';
    case 'cancelled':
      return 'info';
    case 'queued':
      return 'warning';
    default:
      return 'primary';
  }
});

const isActive = computed(() => task.value?.status === 'queued' || task.value?.status === 'running');
const progressValue = computed(() => task.value?.percentage ?? (isActive.value ? 100 : 0));
const serverLabel = computed(() => props.serverName || task.value?.serverId || 'No server');

function formatTime(value?: string | null) {
  return value ? new Date(value).toLocaleString() : '-';
}

function formatTaskType(value?: string) {
  return value ? value.replace(/_/g, ' ') : '-';
}

function shouldPoll(current: TaskDto | null) {
  return !current || current.status === 'queued' || current.status === 'running';
}

async function load() {
  if (!props.taskId) return;
  loading.value = true;
  try {
    const [nextTask, nextLogs] = await Promise.all([
      tasksApi.get(props.taskId),
      tasksApi.logs(props.taskId, cursor.value),
    ]);
    task.value = nextTask;
    emit('updated', nextTask);
    logs.value.push(...nextLogs.logs);
    cursor.value = nextLogs.nextCursor;
    error.value = '';
    if (!shouldPoll(nextTask)) {
      if (!finishedEmitted.value) {
        finishedEmitted.value = true;
        emit('finished', nextTask);
      }
      if (timer) {
        window.clearInterval(timer);
        timer = undefined;
      }
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to load task';
  } finally {
    loading.value = false;
  }
}

function startPolling() {
  if (timer) window.clearInterval(timer);
  logs.value = [];
  cursor.value = 0;
  finishedEmitted.value = false;
  void load();
  timer = window.setInterval(load, 2500);
}

watch(() => props.taskId, startPolling, { immediate: true });

onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer);
});
</script>

<template>
  <div class="task-panel" :class="{ compact }">
    <div class="task-summary">
      <div>
        <div class="task-title-row">
          <strong>{{ task?.summary || 'Task' }}</strong>
          <el-tag v-if="task" :type="statusType" size="small">{{ task.status }}</el-tag>
        </div>
        <div class="task-meta">
          <span>Server: {{ serverLabel }}</span>
          <span>Type: {{ formatTaskType(task?.type) }}</span>
          <span>Stage: {{ task?.stage || 'pending' }}</span>
          <span>Started: {{ formatTime(task?.startedAt) }}</span>
          <span v-if="task?.finishedAt">Finished: {{ formatTime(task.finishedAt) }}</span>
        </div>
      </div>
    </div>

    <el-progress
      v-if="task"
      :percentage="progressValue"
      :indeterminate="task.percentage === null && isActive"
      :duration="1.2"
      :status="task.status === 'failed' ? 'exception' : task.status === 'completed' ? 'success' : undefined"
      :show-text="task.percentage !== null"
    />

    <el-alert v-if="error" type="error" :title="error" show-icon />
    <el-alert v-if="task?.error" type="error" :title="task.error" show-icon />

    <div class="log-box" v-loading="loading && logs.length === 0">
      <div v-if="logs.length === 0" class="muted empty-log">No logs yet.</div>
      <div v-for="entry in logs" :key="entry.cursor" class="log-line" :class="entry.stream">
        <span class="log-time">{{ new Date(entry.time).toLocaleTimeString() }}</span>
        <span class="log-stream">{{ entry.stream }}</span>
        <span>{{ entry.line }}</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.task-panel {
  display: grid;
  gap: 12px;
}

.task-summary {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: center;
}

.task-title-row {
  display: flex;
  align-items: center;
  gap: 10px;
}

.task-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 14px;
  margin-top: 8px;
  color: #667085;
  font-size: 12px;
}

.log-box {
  min-height: 180px;
  max-height: 360px;
  overflow: auto;
  border: 1px solid #dfe4ea;
  border-radius: 8px;
  background: #101828;
  color: #e5e7eb;
  padding: 12px;
  font-family: "Cascadia Code", "SFMono-Regular", Consolas, monospace;
  font-size: 12px;
}

.compact .log-box {
  max-height: 240px;
}

.log-line {
  display: grid;
  grid-template-columns: 86px 64px 1fr;
  gap: 10px;
  line-height: 1.6;
  white-space: pre-wrap;
}

.log-line.stderr {
  color: #fecaca;
}

.log-stream,
.log-time,
.empty-log {
  color: #98a2b3;
}
</style>
