<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue';
import { applicationsApi } from '@/api/applications';
import type { ApplicationRuntimeInstance } from '@/types/applications';
import Dialog from '@/components/ui/Dialog.vue';
import Select from '@/components/ui/Select.vue';
import Button from '@/components/ui/Button.vue';
import LoadingOverlay from '@/components/ui/LoadingOverlay.vue';
import { useErrorToast } from '@/components/ui/toast';
import { useI18n } from '@/i18n';

const props = defineProps<{ open: boolean; applicationId: string; applicationName: string }>();
const emit = defineEmits<{ 'update:open': [value: boolean] }>();
const { t } = useI18n();
const notifyError = useErrorToast();
const instances = ref<ApplicationRuntimeInstance[]>([]);
const selected = ref('');
const text = ref('');
const error = ref('');
const instancesLoading = ref(false);
const logsLoading = ref(false);
let instancesRequest: AbortController | undefined;
let logRequest: AbortController | undefined;

const options = computed(() => instances.value.flatMap(instance => {
  const id = instance.instanceId || instance.id;
  if (!id) return [];
  const status = instance.status || instance.state || 'unknown';
  const disabled = ['missing', 'purged', 'deleted'].includes(status) || (!instance.containerId && (!instance.containerName || status === 'pending'));
  const stateKey = `applicationsPage.instanceStatus.${status}`;
  const state = t(stateKey) === stateKey ? status : t(stateKey);
  return [{ value: id, label: `${instance.serverName || instance.serverId || t('common.notAvailable')} · ${instance.containerName || instance.containerId || t('applicationLogs.noContainer')} · ${state}`, disabled }];
}));
const selectable = computed(() => options.value.filter(option => !option.disabled));

function cancelLogs() { logRequest?.abort(); logRequest = undefined; logsLoading.value = false; }
function cancelAll() { instancesRequest?.abort(); instancesRequest = undefined; instancesLoading.value = false; cancelLogs(); }

async function readLogs() {
  cancelLogs();
  text.value = '';
  error.value = '';
  const instanceId = selected.value;
  const applicationId = props.applicationId;
  if (!props.open || !applicationId || !selectable.value.some(option => option.value === instanceId)) return;
  const controller = new AbortController();
  logRequest = controller;
  logsLoading.value = true;
  try {
    const result = await applicationsApi.logs(applicationId, { instanceId, tail: 240 }, { signal: controller.signal });
    if (controller.signal.aborted || logRequest !== controller || !props.open || props.applicationId !== applicationId || selected.value !== instanceId) return;
    if (result.instanceId && result.instanceId !== instanceId) throw new Error(t('applicationLogs.instanceMismatch'));
    text.value = result.logs;
  } catch (cause) {
    if (controller.signal.aborted || logRequest !== controller) return;
    error.value = cause instanceof Error ? cause.message : t('applicationsPage.logsFailed');
    notifyError(error.value, cause);
  } finally { if (logRequest === controller) { logRequest = undefined; logsLoading.value = false; } }
}

async function loadInstances() {
  cancelAll();
  selected.value = '';
  instances.value = [];
  text.value = '';
  error.value = '';
  if (!props.open || !props.applicationId) return;
  const applicationId = props.applicationId;
  const controller = new AbortController();
  instancesRequest = controller;
  instancesLoading.value = true;
  try {
    const runtime = await applicationsApi.runtime(applicationId, { signal: controller.signal });
    if (controller.signal.aborted || instancesRequest !== controller || !props.open || props.applicationId !== applicationId) return;
    instances.value = runtime.instances;
    if (selectable.value.length === 1) selected.value = selectable.value[0].value;
  } catch (cause) {
    if (controller.signal.aborted || instancesRequest !== controller) return;
    error.value = cause instanceof Error ? cause.message : t('applicationLogs.instancesFailed');
    notifyError(error.value, cause);
  } finally { if (instancesRequest === controller) { instancesRequest = undefined; instancesLoading.value = false; } }
}

watch(() => [props.open, props.applicationId] as const, () => { void loadInstances(); }, { immediate: true });
watch(selected, () => { void readLogs(); });
onBeforeUnmount(cancelAll);
</script>

<template>
  <Dialog :open="open" :title="t('applicationsPage.logs')" :description="applicationName" :close-label="t('common.close')" size="large" @update:open="emit('update:open', $event)">
    <div class="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-3">
      <div class="flex flex-wrap items-end gap-3">
        <label class="grid min-w-0 flex-1 gap-2 text-sm">
          <span>{{ t('applicationLogs.instance') }}</span>
          <Select v-model="selected" :options="options" :placeholder="t('applicationLogs.chooseInstance')" :disabled="instancesLoading || !selectable.length" />
        </label>
        <Button size="sm" :disabled="!selected || instancesLoading || logsLoading" @click="readLogs">{{ t('common.refresh') }}</Button>
        <Button size="sm" variant="ghost" :disabled="instancesLoading || logsLoading" @click="loadInstances">{{ t('applicationLogs.reloadInstances') }}</Button>
      </div>
      <div class="relative min-h-48 min-w-0 overflow-auto rounded-xl border border-border bg-muted p-3">
        <LoadingOverlay v-if="instancesLoading || logsLoading" :label="t(instancesLoading ? 'applicationLogs.instancesLoading' : 'applicationsPage.logsLoading')" />
        <div v-else-if="error" role="alert" class="grid gap-3 text-sm text-danger">
          <p class="m-0 whitespace-pre-wrap break-words">{{ error }}</p>
          <Button size="sm" @click="selected ? readLogs() : loadInstances()">{{ t('common.retry') }}</Button>
        </div>
        <p v-else-if="!selectable.length" class="m-0 text-sm text-muted-foreground">{{ t('applicationLogs.noInstances') }}</p>
        <p v-else-if="!selected" class="m-0 text-sm text-muted-foreground">{{ t('applicationLogs.chooseHint') }}</p>
        <pre v-else-if="text" class="m-0 whitespace-pre-wrap break-words text-xs text-foreground">{{ text }}</pre>
        <p v-else class="m-0 text-sm text-muted-foreground">{{ t('applicationLogs.empty') }}</p>
      </div>
    </div>
  </Dialog>
</template>
