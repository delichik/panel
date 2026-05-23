<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue';
import { containerServicesApi } from '@/api/containerServices';
import type {
  ContainerServiceDto,
  ContainerServiceFileDto,
  ContainerServiceInputDto,
  ContainerServiceValidationResultDto,
  RenderPreviewDto,
  SchedulePreviewDto,
} from '@/types/api';
import { serviceBodyYamlToVisual, visualToServiceBodyYaml, type ServiceVisualModel } from './serviceBodyYaml';

const props = defineProps<{
  service: ContainerServiceDto | null;
  open: boolean;
}>();

const emit = defineEmits<{
  close: [];
  saved: [service: ContainerServiceDto];
}>();

const form = reactive({
  name: '',
  enabled: false,
  composeServiceYaml: 'image: nginx:latest\nrestart: unless-stopped\n',
  variablesRaw: [] as string[],
  selectorRaw: [] as string[],
});
const visual = reactive<ServiceVisualModel>({ image: '', restart: '', ports: [], volumes: [], environment: {}, labels: {}, dependsOn: [] });
const files = ref<ContainerServiceFileDto[]>([]);
const validation = ref<ContainerServiceValidationResultDto | null>(null);
const renderPreview = ref<RenderPreviewDto | null>(null);
const schedulePreview = ref<SchedulePreviewDto | null>(null);
const loading = ref(false);
const previewLoading = ref('');
const error = ref('');

const isCreate = computed(() => !props.service?.id);

function mapToRaw(values?: Record<string, string>) {
  return Object.entries(values ?? {}).map(([key, value]) => `${key}=${value}`);
}

function rawToMap(values: string[]) {
  const out: Record<string, string> = {};
  for (const raw of values) {
    const parts = raw.split('=');
    if (parts.length < 2) continue;
    const key = parts[0].trim();
    if (key) out[key] = parts.slice(1).join('=').trim();
  }
  return out;
}

function syncFromService() {
  const service = props.service;
  form.name = service?.name ?? '';
  form.enabled = service?.enabled ?? false;
  form.composeServiceYaml = service?.composeServiceYaml ?? 'image: nginx:latest\nrestart: unless-stopped\n';
  form.variablesRaw = mapToRaw(service?.variables);
  form.selectorRaw = mapToRaw(service?.selector);
  Object.assign(visual, serviceBodyYamlToVisual(form.composeServiceYaml));
  validation.value = null;
  renderPreview.value = null;
  schedulePreview.value = null;
  error.value = '';
  if (service?.id) void loadFiles(service.id);
  else files.value = [];
}

function input(): ContainerServiceInputDto {
  return {
    name: isCreate.value ? form.name.trim() : props.service?.name,
    enabled: form.enabled,
    composeServiceYaml: form.composeServiceYaml,
    variables: rawToMap(form.variablesRaw),
    selector: rawToMap(form.selectorRaw),
  };
}

async function loadFiles(serviceId: string) {
  try {
    files.value = await containerServicesApi.listFiles(serviceId);
  } catch {
    files.value = [];
  }
}

function syncVisualFromYaml() {
  Object.assign(visual, serviceBodyYamlToVisual(form.composeServiceYaml));
}

function applyVisualToYaml() {
  form.composeServiceYaml = visualToServiceBodyYaml(visual);
}

async function runValidate() {
  previewLoading.value = 'validate';
  try {
    validation.value = await containerServicesApi.validate(props.service?.id || 'draft', input());
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to validate service';
  } finally {
    previewLoading.value = '';
  }
}

async function runRenderPreview() {
  previewLoading.value = 'render';
  try {
    renderPreview.value = await containerServicesApi.renderPreview(props.service?.id || 'draft', input());
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to render preview';
  } finally {
    previewLoading.value = '';
  }
}

async function runSchedulePreview() {
  previewLoading.value = 'schedule';
  try {
    schedulePreview.value = await containerServicesApi.schedulePreview(props.service?.id || 'draft', input());
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to preview schedule';
  } finally {
    previewLoading.value = '';
  }
}

async function save() {
  loading.value = true;
  try {
    const saved = props.service?.id
      ? await containerServicesApi.update(props.service.id, input())
      : await containerServicesApi.create(input());
    emit('saved', saved);
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to save service';
  } finally {
    loading.value = false;
  }
}

watch(() => props.open, (open) => {
  if (open) syncFromService();
});
watch(() => props.service?.id, syncFromService);
</script>

<template>
  <v-navigation-drawer :model-value="open" location="right" temporary width="720" style="z-index: 1005;" @update:model-value="!$event && emit('close')">
    <div class="pa-4 fill-height d-flex flex-column">
      <div class="d-flex justify-space-between align-center mb-4">
        <div>
          <div class="text-h6 font-weight-bold">{{ isCreate ? 'Create Container Service' : service?.name }}</div>
          <div class="text-caption text-medium-emphasis">YAML is the source of truth for the Compose service body.</div>
        </div>
        <div class="d-flex align-center" style="gap: 8px;">
          <v-btn color="primary" variant="flat" size="small" class="text-none font-weight-bold" :loading="loading" @click="save">Save</v-btn>
          <v-btn icon="mdi-close" variant="text" size="small" @click="emit('close')" />
        </div>
      </div>

      <v-alert v-if="error" type="error" variant="tonal" density="compact" class="mb-3">{{ error }}</v-alert>

      <div class="flex-grow-1 overflow-auto editor-body">
        <v-text-field
          v-if="isCreate"
          v-model="form.name"
          label="Name"
          hint="Immutable runtime identity, lowercase letters, digits, and dashes."
          persistent-hint
          variant="outlined"
          density="comfortable"
        />
        <div v-else class="readonly-name">
          <span class="text-caption text-medium-emphasis">Immutable name</span>
          <strong>{{ service?.name }}</strong>
        </div>

        <v-switch v-model="form.enabled" color="primary" label="Enabled" hide-details class="mt-2" />

        <div class="section-title">Compose Service Body</div>
        <v-textarea
          v-model="form.composeServiceYaml"
          rows="14"
          spellcheck="false"
          class="font-mono"
          variant="outlined"
          density="compact"
          @blur="syncVisualFromYaml"
        />

        <div class="section-title">Visual Helper</div>
        <div class="visual-grid">
          <v-text-field v-model="visual.image" label="Image" variant="outlined" density="compact" />
          <v-text-field v-model="visual.restart" label="Restart" variant="outlined" density="compact" />
          <v-text-field v-model="visual.networkMode" label="Network mode" variant="outlined" density="compact" />
          <v-text-field v-model="visual.command" label="Command" variant="outlined" density="compact" />
        </div>
        <v-combobox v-model="visual.ports" label="Ports" multiple chips closable-chips variant="outlined" density="compact" class="mt-2" />
        <v-combobox v-model="visual.volumes" label="Volumes" multiple chips closable-chips variant="outlined" density="compact" />
        <v-combobox v-model="visual.dependsOn" label="Depends on" multiple chips closable-chips variant="outlined" density="compact" />
        <v-btn size="small" variant="outlined" prepend-icon="mdi-arrow-up-bold-box" class="text-none mb-4" @click="applyVisualToYaml">Write helper fields to YAML</v-btn>

        <div class="section-title">Variables</div>
        <v-combobox v-model="form.variablesRaw" label="KEY=value" multiple chips closable-chips variant="outlined" density="compact" />

        <div class="section-title">Selector</div>
        <v-combobox v-model="form.selectorRaw" label="trait=value" multiple chips closable-chips variant="outlined" density="compact" />

        <div class="section-title">Files</div>
        <v-table density="compact" class="mb-4">
          <tbody>
            <tr v-if="files.length === 0">
              <td class="text-medium-emphasis">No service files</td>
            </tr>
            <tr v-for="file in files" :key="file.id">
              <td>{{ file.path }}</td>
              <td>{{ file.kind }}</td>
              <td class="font-tabular">{{ file.size ?? '-' }}</td>
            </tr>
          </tbody>
        </v-table>

        <div class="preview-actions">
          <v-btn size="small" variant="outlined" prepend-icon="mdi-check-circle-outline" :loading="previewLoading === 'validate'" @click="runValidate">Validate</v-btn>
          <v-btn size="small" variant="outlined" prepend-icon="mdi-file-eye-outline" :loading="previewLoading === 'render'" @click="runRenderPreview">Render</v-btn>
          <v-btn size="small" variant="outlined" prepend-icon="mdi-map-marker-path" :loading="previewLoading === 'schedule'" @click="runSchedulePreview">Schedule</v-btn>
        </div>

        <v-alert v-if="validation" :type="validation.valid ? 'success' : 'error'" variant="tonal" density="compact" class="mt-3">
          {{ validation.valid ? 'Validation passed' : 'Validation failed' }}
          <div v-for="issue in validation.issues || []" :key="`${issue.path}-${issue.message}`">{{ issue.severity || 'issue' }}: {{ issue.message }}</div>
        </v-alert>

        <v-card v-if="renderPreview" variant="outlined" class="mt-3">
          <v-card-title class="text-subtitle-2">Render Preview</v-card-title>
          <v-card-text>
            <pre>{{ renderPreview.composeYaml || renderPreview.renderedYaml || renderPreview.overrideYaml || 'No rendered content returned' }}</pre>
          </v-card-text>
        </v-card>

        <v-card v-if="schedulePreview" variant="outlined" class="mt-3">
          <v-card-title class="text-subtitle-2">Schedule Preview</v-card-title>
          <v-card-text>
            <div class="font-weight-bold">{{ schedulePreview.selectedNodeName || schedulePreview.selectedNodeId || 'No eligible node selected' }}</div>
            <div v-for="candidate in schedulePreview.candidates || []" :key="candidate.nodeId" class="text-caption">
              {{ candidate.nodeName || candidate.nodeId }} - {{ candidate.eligible ? 'eligible' : 'blocked' }}
            </div>
          </v-card-text>
        </v-card>
      </div>
    </div>
  </v-navigation-drawer>
</template>

<style scoped>
.editor-body {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.readonly-name {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 10px 12px;
  border-radius: 8px;
  background: rgba(var(--v-theme-surface-variant), 0.6);
}

.section-title {
  margin-top: 12px;
  font-weight: 700;
}

.visual-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.preview-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.font-mono,
pre {
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
}

pre {
  white-space: pre-wrap;
  margin: 0;
}
</style>
