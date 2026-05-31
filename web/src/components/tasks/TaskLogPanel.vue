<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue';
import { tasksApi } from '@/api/tasks';
import { formatDateTime, t, translateTaskStatus } from '@/i18n';
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
      return 'error';
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
const serverLabel = computed(() => props.serverName || task.value?.serverId || t('shared.taskLogPanel.noServer'));

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
    error.value = err instanceof Error ? err.message : t('shared.taskLogPanel.loadFailed');
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
          <strong>{{ task?.summary || t('shared.taskLogPanel.defaultTitle') }}</strong>
          <v-chip v-if="task" :color="statusType" size="small" label class="ml-2">{{ translateTaskStatus(task.status) }}</v-chip>
        </div>
        <div class="task-meta">
          <span>{{ t('shared.taskLogPanel.server') }}: {{ serverLabel }}</span>
          <span>{{ t('shared.taskLogPanel.type') }}: {{ formatTaskType(task?.type) }}</span>
          <span>{{ t('shared.taskLogPanel.stage') }}: {{ task?.stage || t('shared.taskLogPanel.pending') }}</span>
          <span>{{ t('shared.taskLogPanel.started') }}: {{ formatDateTime(task?.startedAt) }}</span>
          <span v-if="task?.finishedAt">{{ t('shared.taskLogPanel.finished') }}: {{ formatDateTime(task.finishedAt) }}</span>
        </div>
      </div>
    </div>

    <v-progress-linear
      v-if="task"
      :model-value="progressValue"
      :indeterminate="task.percentage === null && isActive"
      :color="task.status === 'failed' ? 'error' : task.status === 'completed' ? 'success' : 'primary'"
      height="18"
      rounded
      class="my-2"
    >
      <template v-if="task.percentage !== null" v-slot:default="{ value }">
        <strong class="text-caption text-white font-weight-bold">{{ Math.ceil(value) }}%</strong>
      </template>
    </v-progress-linear>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-2" density="compact">{{ error }}</v-alert>
    <v-alert v-if="task?.error" type="error" variant="tonal" class="mb-2" density="compact">{{ task.error }}</v-alert>

    <div class="log-box-container position-relative">
      <div class="log-box">
        <div v-if="logs.length === 0 && !loading" class="muted empty-log">{{ t('shared.taskLogPanel.noLogs') }}</div>
        <div v-for="entry in logs" :key="entry.cursor" class="log-line" :class="entry.stream">
          <span class="log-time">{{ formatDateTime(entry.time) }}</span>
          <span class="log-stream">{{ entry.stream }}</span>
          <span>{{ entry.line }}</span>
        </div>
      </div>
      <div v-if="loading && logs.length === 0" class="log-loading position-absolute d-flex justify-center align-center fill-height width-100">
        <v-progress-circular indeterminate color="primary" />
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
}

.task-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 14px;
  margin-top: 8px;
  color: rgba(var(--v-theme-on-surface), 0.6);
  font-size: 12px;
}

.log-box {
  min-height: 180px;
  max-height: 360px;
  overflow-y: auto;
  border: 1px solid var(--lp-border);
  border-radius: 8px;
  background: var(--lp-log-background);
  color: var(--lp-log-text);
  padding: 12px;
  font-family: "Cascadia Code", "SFMono-Regular", Consolas, monospace;
  font-size: 12px;
}

.log-loading {
  inset: 0;
  width: 100%;
  border-radius: 8px;
  background: color-mix(in srgb, var(--lp-log-background), transparent 18%);
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
  color: var(--lp-log-error);
}

.log-stream,
.log-time,
.empty-log {
  color: var(--lp-log-muted);
}
</style>
