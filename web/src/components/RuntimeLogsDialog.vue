<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { useI18n } from '@/i18n';
import PageLoadingState from '@/components/PageLoadingState.vue';

const props = withDefaults(defineProps<{
  open: boolean;
  title: string;
  subtitle?: string;
  targetKey?: string;
  loader: (tail: number) => Promise<string>;
}>(), {
  subtitle: '',
  targetKey: '',
});

const emit = defineEmits<{ 'update:open': [boolean] }>();
const { t } = useI18n();
const tail = ref(200);
const logs = ref('');
const loading = ref(false);
const error = ref('');
const canLoad = computed(() => Boolean(props.targetKey || props.title));
let logsRequestId = 0;

function normalizedTail() {
  const value = Number(tail.value);
  if (!Number.isFinite(value) || value <= 0) return 200;
  return Math.min(10000, Math.max(1, Math.trunc(value)));
}

function close() {
  emit('update:open', false);
}

async function loadLogs() {
  if (!canLoad.value) return;
  const requestedTargetKey = props.targetKey;
  const requestId = ++logsRequestId;
  const nextTail = normalizedTail();
  tail.value = nextTail;
  logs.value = '';
  error.value = '';
  loading.value = true;
  try {
    const result = await props.loader(nextTail);
    if (requestId !== logsRequestId || props.targetKey !== requestedTargetKey) return;
    logs.value = result;
    error.value = '';
  } catch (err) {
    if (requestId !== logsRequestId || props.targetKey !== requestedTargetKey) return;
    error.value = err instanceof Error ? err.message : t('applicationLogs.loadFailed');
  } finally {
    if (requestId === logsRequestId && props.targetKey === requestedTargetKey) {
      loading.value = false;
    }
  }
}

watch(() => [props.open, props.targetKey], ([open]) => {
  if (!open) return;
  logs.value = '';
  error.value = '';
  void loadLogs();
});
</script>

<template>
  <v-dialog :model-value="open" width="980" @update:model-value="emit('update:open', $event)">
    <v-card class="app-dialog-card runtime-logs-dialog">
      <v-card-title class="app-dialog-title">
        <div class="min-width-0">
          <span class="app-dialog-title-text">{{ title }}</span>
          <div v-if="subtitle" class="text-caption text-medium-emphasis text-truncate">{{ subtitle }}</div>
        </div>
        <v-btn icon="mdi-close" variant="text" :aria-label="t('common.close')" @click="close" />
      </v-card-title>
      <v-divider />
      <v-card-text class="app-dialog-body runtime-logs-body">
        <v-alert v-if="error" type="error" variant="tonal" density="compact">{{ error }}</v-alert>
        <div class="logs-controls">
          <v-text-field
            v-model.number="tail"
            :label="t('applicationLogs.tail')"
            type="number"
            min="1"
            max="10000"
            density="compact"
            variant="outlined"
            hide-details
          />
          <v-btn color="primary" variant="flat" class="text-none" prepend-icon="mdi-refresh" :disabled="!canLoad" :loading="loading" @click="loadLogs">
            {{ t('common.refresh') }}
          </v-btn>
        </div>
        <div class="log-output-wrap">
          <pre class="log-output">{{ logs || t('applicationLogs.emptyContent') }}</pre>
          <PageLoadingState v-if="loading && !logs" compact min-height="260px" class="log-loading-state" />
        </div>
      </v-card-text>
      <v-divider />
      <v-card-actions class="app-dialog-actions">
        <v-btn variant="text" class="text-none" @click="close">{{ t('common.close') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<style scoped>
.runtime-logs-dialog {
  max-height: calc(100vh - 48px);
}

.runtime-logs-body {
  display: grid;
  grid-template-rows: auto auto minmax(260px, 1fr);
  gap: 12px;
  min-height: 0;
}

.logs-controls {
  display: grid;
  grid-template-columns: 140px auto;
  gap: 8px;
  align-items: center;
  justify-content: start;
}

.logs-controls .v-btn {
  min-height: 40px;
}

.log-output {
  min-height: 260px;
  max-height: min(58vh, 560px);
  overflow: auto;
  margin: 0;
  padding: 12px;
  border: 1px solid var(--lp-border);
  border-radius: var(--lp-radius-md);
  background: var(--lp-log-background);
  color: var(--lp-log-text);
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.log-output-wrap {
  position: relative;
  min-height: 260px;
}

.log-loading-state {
  position: absolute;
  inset: 0;
}

.min-width-0 {
  min-width: 0;
}

@media (max-width: 760px) {
  .logs-controls {
    grid-template-columns: 1fr;
    justify-content: stretch;
  }
}
</style>
