<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { ListFilter, Play, RefreshCcw, RotateCcw } from '@lucide/vue';
import { tasksApi } from '@/api/tasks';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import PaginationBar from '@/components/ui/PaginationBar.vue';
import SearchInput from '@/components/ui/SearchInput.vue';
import Select from '@/components/ui/Select.vue';
import Skeleton from '@/components/ui/Skeleton.vue';
import StatusBadge from '@/components/ui/StatusBadge.vue';
import Tabs from '@/components/ui/Tabs.vue';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import { useI18n } from '@/i18n';
import type { TaskDto, TaskLog, TaskOperationGroup, TaskStep } from '@/types/tasks';
import { groupTasksByOperation } from './model';

const { t } = useI18n();
const route = useRoute();
const router = useRouter();

const tasks = ref<TaskDto[]>([]);
const selectedOperationId = ref(String(route.query.operation ?? ''));
const selectedTaskId = ref(String(route.query.task ?? ''));
const statusFilter = ref(String(route.query.status ?? 'all'));
const search = ref(String(route.query.search ?? ''));
const page = ref(Math.max(1, Number(route.query.page ?? 1) || 1));
const pageSize = 12;
const totalTasks = ref(0);
const tab = ref('steps');
const steps = ref<TaskStep[]>([]);
const logs = ref<TaskLog[]>([]);
const logCursor = ref(0);
const loading = ref(false);
const detailLoading = ref(false);
const polling = ref(true);
const error = ref('');
const actionError = ref('');
const feedback = ref('');
const pending = ref('');
let timer: number | undefined;

const groups = computed(() => {
  const term = search.value.trim().toLowerCase();
  return groupTasksByOperation(tasks.value).filter((group) => {
    const statusOk = statusFilter.value === 'all' || group.status === statusFilter.value || group.tasks.some((task) => task.status === statusFilter.value);
    const searchOk = !term || [group.operationId, group.type, group.title, ...group.tasks.map((task) => `${task.id} ${task.summary} ${task.error}`)].some((value) => value.toLowerCase().includes(term));
    return statusOk && searchOk;
  });
});
const selectedGroup = computed<TaskOperationGroup | null>(() => groups.value.find((item) => item.operationId === selectedOperationId.value) ?? groups.value[0] ?? null);
const selectedTask = computed(() => selectedGroup.value?.tasks.find((task) => task.id === selectedTaskId.value) ?? selectedGroup.value?.tasks[0] ?? null);
const statusOptions = computed(() => [
  { label: t('tasksPage.filter.all'), value: 'all' },
  { label: t('tasksPage.status.running'), value: 'running' },
  { label: t('tasksPage.status.queued'), value: 'queued' },
  { label: t('tasksPage.status.failed'), value: 'failed' },
  { label: t('tasksPage.status.completed'), value: 'completed' },
]);

watch([statusFilter, search, page, selectedOperationId, selectedTaskId], () => {
  void router.replace({ query: { ...route.query, status: statusFilter.value === 'all' ? undefined : statusFilter.value, search: search.value || undefined, page: page.value > 1 ? page.value : undefined, operation: selectedOperationId.value || undefined, task: selectedTaskId.value || undefined } });
});

watch(statusFilter, async () => {
  page.value = 1;
  selectedOperationId.value = '';
  selectedTaskId.value = '';
  await load();
});

watch(page, async () => {
  selectedOperationId.value = '';
  selectedTaskId.value = '';
  await load();
});

watch(selectedGroup, (group) => {
  if (!group) return;
  selectedOperationId.value = group.operationId;
  if (!group.tasks.some((task) => task.id === selectedTaskId.value)) selectedTaskId.value = group.tasks[0]?.id ?? '';
});

watch(selectedTask, async (task) => {
  if (task) await loadDetail(task.id, true);
});

async function load(silent = false) {
  if (!silent) loading.value = true;
  error.value = '';
  try {
    const result = await tasksApi.list({ operationPage: true, status: statusFilter.value === 'all' ? undefined : statusFilter.value, page: page.value, pageSize });
    tasks.value = result.items;
    totalTasks.value = result.total;
    if (page.value !== result.page) page.value = result.page;
    if (!selectedOperationId.value && groups.value.length) selectedOperationId.value = groups.value[0].operationId;
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('tasksPage.loadFailed');
  } finally {
    loading.value = false;
  }
}

async function loadDetail(taskId: string, resetLogs = false) {
  detailLoading.value = true;
  actionError.value = '';
  try {
    const [nextSteps, nextLogs] = await Promise.all([tasksApi.steps(taskId), tasksApi.logs(taskId, resetLogs ? 0 : logCursor.value)]);
    steps.value = nextSteps;
    logs.value = resetLogs ? nextLogs.logs : [...logs.value, ...nextLogs.logs];
    logCursor.value = nextLogs.nextCursor;
  } catch (err) {
    actionError.value = err instanceof Error ? err.message : t('tasksPage.detailLoadFailed');
  } finally {
    detailLoading.value = false;
  }
}

async function runTaskAction(kind: 'retry' | 'run-now', task: TaskDto) {
  pending.value = `${kind}:${task.id}`;
  actionError.value = '';
  feedback.value = '';
  try {
    const result = kind === 'retry' ? await tasksApi.retry(task.id) : await tasksApi.runNow(task.id);
    feedback.value = t(kind === 'retry' ? 'tasksPage.retryAccepted' : 'tasksPage.runNowAccepted', { taskId: result.id });
    await load(true);
  } catch (err) {
    actionError.value = err instanceof Error ? err.message : t('common.operationFailed');
  } finally {
    pending.value = '';
  }
}

function startPolling() {
  window.clearInterval(timer);
  timer = window.setInterval(() => {
    if (polling.value) void load(true);
  }, 5000);
}

onMounted(async () => {
  await load();
  startPolling();
});
onBeforeUnmount(() => window.clearInterval(timer));
</script>

<template>
  <ConsolePage :title="t('routes.tasks.title')" :description="t('routes.tasks.description')">
    <template #actions>
      <Button size="sm" :loading="loading" @click="load()"><RefreshCcw />{{ t('common.refresh') }}</Button>
      <Button size="sm" :variant="polling ? 'secondary' : 'primary'" @click="polling = !polling">{{ polling ? t('tasksPage.pausePolling') : t('tasksPage.resumePolling') }}</Button>
    </template>

    <div class="grid h-full min-h-[640px] min-w-0 grid-cols-[minmax(0,360px)_minmax(0,1fr)] gap-4 overflow-x-hidden max-xl:grid-cols-1">
      <aside class="grid min-h-0 min-w-0 grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden rounded-2xl border border-border bg-card">
        <div class="grid min-w-0 gap-3 border-b border-border p-4">
          <SearchInput v-model="search" clearable :placeholder="t('tasksPage.searchPlaceholder')" :label="t('common.search')" :clear-label="t('common.clearSearch')" />
          <label class="grid gap-1 text-xs text-muted-foreground">{{ t('tasksPage.statusFilter') }}<Select v-model="statusFilter" :options="statusOptions" /></label>
        </div>
        <div class="min-h-0 min-w-0 overflow-y-auto overflow-x-hidden p-2">
          <div v-if="loading && !groups.length" class="grid gap-2">
            <Skeleton v-for="item in 6" :key="item" class="h-20" />
          </div>
          <EmptyState v-else-if="!groups.length" :title="t('tasksPage.empty')" :description="t('tasksPage.emptyHint')" />
          <button v-for="group in groups" v-else :key="group.operationId" type="button" class="motion-list-item mb-2 grid w-full min-w-0 gap-2 overflow-hidden rounded-xl border p-3 text-left hover:bg-accent" :class="selectedOperationId === group.operationId ? 'border-border-strong bg-background' : 'border-transparent'" :aria-current="selectedOperationId === group.operationId ? 'true' : undefined" @click="selectedOperationId = group.operationId">
            <div class="grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-start gap-2 max-[420px]:grid-cols-1">
              <strong class="min-w-0 truncate text-sm text-foreground">{{ group.title }}</strong>
              <StatusBadge class="max-w-full shrink-0 justify-self-start whitespace-nowrap" :status="group.status" domain="task" :label="t(`tasksPage.status.${group.status}`)" />
            </div>
            <span class="min-w-0 truncate text-xs text-muted-foreground">{{ group.operationId }}</span>
            <div class="flex min-w-0 flex-wrap gap-1.5 overflow-hidden"><Badge class="max-w-full shrink-0 whitespace-nowrap" tone="info">{{ group.tasks.length }} {{ t('tasksPage.tasks') }}</Badge><Badge v-if="group.failed" class="max-w-full shrink-0 whitespace-nowrap" tone="danger">{{ group.failed }} {{ t('tasksPage.failed') }}</Badge><Badge v-if="group.running" class="max-w-full shrink-0 whitespace-nowrap" tone="info">{{ group.running }} {{ t('tasksPage.running') }}</Badge></div>
          </button>
        </div>
        <PaginationBar v-model:page="page" class="min-w-0 overflow-hidden px-3" :page-size="pageSize" :total="totalTasks" :loading="loading" :previous-label="t('common.previous')" :next-label="t('common.next')">
          <template #summary="{ page: currentPage, pageCount }">
            {{ t('tasksPage.paginationSummary', { page: currentPage, pages: pageCount, total: totalTasks }) }}
          </template>
        </PaginationBar>
      </aside>

      <main class="grid min-h-0 min-w-0 overflow-hidden">
        <section v-if="error" class="rounded-2xl border border-danger-border bg-danger-bg p-4 text-sm text-danger">{{ error }}</section>
        <EmptyState v-else-if="!selectedGroup" :title="t('tasksPage.selectOperation')" :description="t('tasksPage.selectOperationHint')" />
        <article v-else class="grid min-h-0 min-w-0 grid-rows-[auto_auto_minmax(0,1fr)] overflow-hidden rounded-2xl border border-border bg-card">
          <header class="grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-start gap-4 border-b border-border p-5 max-lg:grid-cols-1">
            <div class="min-w-0 overflow-hidden">
              <div class="grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-start gap-2 max-[420px]:grid-cols-1"><h2 class="m-0 min-w-0 truncate text-xl font-semibold">{{ selectedGroup.title }}</h2><StatusBadge class="max-w-full shrink-0 justify-self-start whitespace-nowrap" :status="selectedGroup.status" domain="task" :label="t(`tasksPage.status.${selectedGroup.status}`)" /></div>
              <p class="m-0 mt-1 min-w-0 truncate text-sm text-muted-foreground">{{ selectedGroup.type }} / {{ selectedGroup.operationId }}</p>
            </div>
            <div class="flex min-w-0 flex-wrap justify-end gap-2 max-lg:justify-start">
              <Button v-if="selectedTask?.allowRunNow" size="sm" :loading="pending === `run-now:${selectedTask.id}`" @click="runTaskAction('run-now', selectedTask)"><Play />{{ t('common.runNow') }}</Button>
              <Button v-if="selectedTask?.allowRetry" size="sm" variant="primary" :loading="pending === `retry:${selectedTask.id}`" @click="runTaskAction('retry', selectedTask)"><RotateCcw />{{ t('common.retry') }}</Button>
            </div>
          </header>
          <div v-if="feedback || actionError" class="grid min-w-0 gap-2 overflow-hidden border-b border-border p-4">
            <div v-if="feedback" class="min-w-0 break-words rounded-xl border border-success-border bg-success-bg p-3 text-sm text-success">{{ feedback }}</div>
            <div v-if="actionError" class="min-w-0 break-words rounded-xl border border-danger-border bg-danger-bg p-3 text-sm text-danger">{{ actionError }}</div>
          </div>
          <div class="grid min-h-0 min-w-0 grid-cols-[minmax(0,280px)_minmax(0,1fr)] gap-4 overflow-hidden p-4 max-lg:grid-cols-1">
            <section class="min-h-0 min-w-0 overflow-y-auto overflow-x-hidden rounded-2xl border border-border bg-background p-3">
              <h3 class="m-0 mb-3 flex min-w-0 items-center gap-2 text-sm font-semibold"><ListFilter class="size-4 shrink-0" /><span class="min-w-0 truncate">{{ t('tasksPage.executionItems') }}</span></h3>
              <button v-for="task in selectedGroup.tasks" :key="task.id" type="button" class="motion-list-item mb-2 grid w-full min-w-0 gap-1 overflow-hidden rounded-xl border p-3 text-left text-sm hover:bg-accent" :class="selectedTaskId === task.id ? 'border-border-strong bg-card' : 'border-border'" :aria-current="selectedTaskId === task.id ? 'true' : undefined" @click="selectedTaskId = task.id">
                <div class="grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-start gap-2 max-[420px]:grid-cols-1"><strong class="min-w-0 truncate">{{ task.id }}</strong><StatusBadge class="max-w-full shrink-0 justify-self-start whitespace-nowrap" :status="task.status" domain="task" /></div>
                <span class="min-w-0 truncate text-xs text-muted-foreground">{{ task.summary }}</span>
              </button>
            </section>
            <section class="min-h-0 min-w-0 overflow-hidden rounded-2xl border border-border bg-background p-4">
              <Tabs v-model="tab" class="h-full min-h-0" :tabs="[{ label: t('tasksPage.steps'), value: 'steps' }, { label: t('tasksPage.logs'), value: 'logs' }, { label: t('tasksPage.error'), value: 'error' }]">
                <div v-if="tab === 'steps'" class="min-h-0 min-w-0 overflow-y-auto overflow-x-hidden">
                  <div v-for="step in steps" :key="step.id" class="mb-2 grid min-w-0 gap-2 overflow-hidden rounded-xl border border-border p-3 text-sm">
                    <div class="grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-start gap-2 max-[420px]:grid-cols-1"><strong class="min-w-0 truncate">{{ step.step }}</strong><StatusBadge class="max-w-full shrink-0 justify-self-start whitespace-nowrap" :status="step.status" domain="task" /></div>
                    <div class="h-2 overflow-hidden rounded-full bg-muted"><div class="h-full bg-primary" :style="{ width: `${step.percentage}%` }" /></div>
                    <p v-if="step.error" class="m-0 min-w-0 break-words text-danger">{{ step.error }}</p>
                  </div>
                  <EmptyState v-if="!steps.length" :title="t('tasksPage.noSteps')" :description="detailLoading ? t('tasksPage.loadingDetail') : t('tasksPage.noStepsHint')" />
                </div>
                <pre v-else-if="tab === 'logs'" class="h-full min-h-[420px] min-w-0 overflow-y-auto overflow-x-hidden whitespace-pre-wrap break-words rounded-xl border border-border bg-card p-3 text-xs [overflow-wrap:anywhere]">{{ logs.map((line) => `[${line.stream}] ${line.line}`).join('\n') || t('tasksPage.noLogs') }}</pre>
                <div v-else class="min-w-0 break-words rounded-xl border p-4 text-sm [overflow-wrap:anywhere]" :class="selectedTask?.error ? 'border-danger-border bg-danger-bg text-danger' : 'border-border text-muted-foreground'">{{ selectedTask?.error || t('tasksPage.noError') }}</div>
              </Tabs>
            </section>
          </div>
        </article>
      </main>
    </div>
  </ConsolePage>
</template>
