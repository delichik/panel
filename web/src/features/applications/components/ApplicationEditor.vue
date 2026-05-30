<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue';
import { applicationsApi } from '@/api/applications';
import type { ApplicationDto, ApplicationPlanDto, ApplicationSaveDto, ApplicationValidationDto } from '@/types/api';

const props = defineProps<{ application: ApplicationDto | null; open: boolean }>();
const emit = defineEmits<{ close: []; saved: [ApplicationDto] }>();

interface PortForm { label: string; to: number; static: number | null }
interface EnvForm { key: string; value: string }
interface ServiceForm { name: string; port: string; tags: string }
interface CheckForm { name: string; type: 'tcp' | 'http' | 'script'; port: string; path: string; command: string; intervalSeconds: number; timeoutSeconds: number }
interface ConstraintForm { attribute: string; operator: string; value: string }
interface VolumeForm { source: string; target: string; readOnly: boolean }

const form = reactive<ApplicationSaveDto>({ name: '', enabled: false, specYaml: defaultSpec(), variables: {} });
const specForm = reactive({
  name: 'web',
  image: 'nginx:1.27',
  count: 1,
  command: '',
  args: '',
  cpu: 100,
  memoryMb: 128,
  ports: [{ label: 'http', to: 80, static: null }] as PortForm[],
  env: [] as EnvForm[],
  services: [] as ServiceForm[],
  checks: [] as CheckForm[],
  constraints: [] as ConstraintForm[],
  volumes: [] as VolumeForm[],
});
const activeTab = ref('container');
const variablesText = ref('{}');
const validation = ref<ApplicationValidationDto | null>(null);
const plan = ref<ApplicationPlanDto | null>(null);
const loading = ref('');
const error = ref('');

const title = computed(() => (props.application ? `Edit ${props.application.name}` : 'Create application'));
const portOptions = computed(() => specForm.ports.filter((port) => port.label.trim()).map((port) => port.label.trim()));

watch(() => props.open, (open) => {
  if (!open) return;
  const app = props.application;
  form.name = app?.name ?? '';
  form.enabled = app?.enabled ?? false;
  form.specYaml = app?.specYaml ?? defaultSpec();
  form.variables = { ...(app?.variables ?? {}) };
  loadDefaultSpecForm(app?.name);
  variablesText.value = JSON.stringify(form.variables, null, 2);
  validation.value = null;
  plan.value = null;
  error.value = '';
  activeTab.value = 'container';
}, { immediate: true });

watch(() => specForm.name, (name) => {
  if (!props.application) form.name = name;
});

function defaultSpec() {
  return 'name: web\nimage: nginx:1.27\ncount: 1\nports:\n  - label: http\n    to: 80\nresources:\n  cpu: 100\n  memoryMb: 128\n';
}

function loadDefaultSpecForm(appName?: string) {
  specForm.name = appName || 'web';
  specForm.image = 'nginx:1.27';
  specForm.count = 1;
  specForm.command = '';
  specForm.args = '';
  specForm.cpu = 100;
  specForm.memoryMb = 128;
  specForm.ports = [{ label: 'http', to: 80, static: null }];
  specForm.env = [];
  specForm.services = [];
  specForm.checks = [];
  specForm.constraints = [];
  specForm.volumes = [];
}

function addPort() {
  specForm.ports.push({ label: `port-${specForm.ports.length + 1}`, to: 80, static: null });
}

function addEnv() {
  specForm.env.push({ key: '', value: '' });
}

function addService() {
  specForm.services.push({ name: specForm.name || 'service', port: portOptions.value[0] || '', tags: '' });
}

function addCheck() {
  specForm.checks.push({ name: 'health', type: 'http', port: portOptions.value[0] || '', path: '/', command: '', intervalSeconds: 10, timeoutSeconds: 2 });
}

function addConstraint() {
  specForm.constraints.push({ attribute: '${node.class}', operator: '=', value: '' });
}

function addVolume() {
  specForm.volumes.push({ source: `${specForm.name || 'app'}-data`, target: '/data', readOnly: false });
}

function removeAt<T>(items: T[], index: number) {
  items.splice(index, 1);
}

function applyVisualSpec() {
  form.specYaml = buildSpecYaml();
  error.value = '';
}

function buildSpecYaml() {
  const spec: Record<string, unknown> = {
    name: specForm.name,
    image: specForm.image,
    count: Number(specForm.count || 1),
  };
  const command = splitWords(specForm.command);
  const args = splitWords(specForm.args);
  const env = Object.fromEntries(specForm.env.filter((item) => item.key.trim()).map((item) => [item.key.trim(), item.value]));
  const ports = specForm.ports
    .filter((port) => port.label.trim() && port.to)
    .map((port) => ({ label: port.label.trim(), to: Number(port.to), ...(port.static ? { static: Number(port.static) } : {}) }));
  const services = specForm.services
    .filter((service) => service.name.trim())
    .map((service) => ({ name: service.name.trim(), port: service.port, tags: splitCSV(service.tags) }));
  const checks = specForm.checks
    .filter((check) => check.name.trim())
    .map((check) => ({
      name: check.name.trim(),
      type: check.type,
      ...(check.type !== 'script' ? { port: check.port } : {}),
      ...(check.type === 'http' ? { path: check.path || '/' } : {}),
      ...(check.type === 'script' ? { command: check.command } : {}),
      intervalSeconds: Number(check.intervalSeconds || 10),
      timeoutSeconds: Number(check.timeoutSeconds || 2),
    }));
  const constraints = specForm.constraints
    .filter((constraint) => constraint.attribute.trim() && constraint.operator.trim())
    .map((constraint) => ({ attribute: constraint.attribute.trim(), operator: constraint.operator.trim(), value: constraint.value }));
  const volumes = specForm.volumes
    .filter((volume) => volume.source.trim() && volume.target.trim())
    .map((volume) => ({ source: volume.source.trim(), target: volume.target.trim(), readOnly: volume.readOnly }));
  if (command.length) spec.command = command;
  if (args.length) spec.args = args;
  if (Object.keys(env).length) spec.env = env;
  if (ports.length) spec.ports = ports;
  spec.resources = { cpu: Number(specForm.cpu || 100), memoryMb: Number(specForm.memoryMb || 128) };
  if (services.length) spec.services = services;
  if (checks.length) spec.checks = checks;
  if (constraints.length) spec.constraints = constraints;
  if (volumes.length) spec.volumes = volumes;
  return toYaml(spec);
}

function splitWords(value: string) {
  return value.split(/\s+/).map((part) => part.trim()).filter(Boolean);
}

function splitCSV(value: string) {
  return value.split(',').map((part) => part.trim()).filter(Boolean);
}

function toYaml(value: unknown, indent = 0): string {
  const pad = ' '.repeat(indent);
  if (Array.isArray(value)) {
    if (value.length === 0) return '[]';
    return value.map((item) => {
      if (isPlainObject(item)) {
        const lines = Object.entries(item).filter(([, child]) => shouldEmit(child));
        if (lines.length === 0) return `${pad}- {}`;
        const [first, ...rest] = lines;
        return `${pad}- ${first[0]}: ${yamlScalar(first[1])}\n${rest.map(([key, child]) => `${pad}  ${key}: ${yamlScalar(child)}`).join('\n')}`;
      }
      return `${pad}- ${yamlScalar(item)}`;
    }).join('\n');
  }
  if (isPlainObject(value)) {
    return Object.entries(value)
      .filter(([, child]) => shouldEmit(child))
      .map(([key, child]) => {
        if (Array.isArray(child) || isPlainObject(child)) return `${pad}${key}:\n${toYaml(child, indent + 2)}`;
        return `${pad}${key}: ${yamlScalar(child)}`;
      })
      .join('\n') + '\n';
  }
  return yamlScalar(value);
}

function yamlScalar(value: unknown): string {
  if (Array.isArray(value)) return value.length ? `[${value.map(yamlScalar).join(', ')}]` : '[]';
  if (isPlainObject(value)) return `\n${toYaml(value, 2)}`;
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  const text = String(value ?? '');
  if (text === '' || /[:#\[\]{},&*?|-]|^\s|\s$|\n/.test(text)) return JSON.stringify(text);
  return text;
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value);
}

function shouldEmit(value: unknown) {
  if (Array.isArray(value)) return value.length > 0;
  if (isPlainObject(value)) return Object.keys(value).length > 0;
  return value !== undefined && value !== null && value !== '';
}

function readInput(): ApplicationSaveDto {
  if (activeTab.value !== 'yaml') applyVisualSpec();
  return {
    name: form.name,
    enabled: form.enabled,
    specYaml: form.specYaml,
    variables: variablesText.value.trim() ? JSON.parse(variablesText.value) : {},
  };
}

async function validate() {
  if (!props.application) return;
  loading.value = 'validate';
  try {
    validation.value = await applicationsApi.validate(props.application.id);
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to validate application';
  } finally {
    loading.value = '';
  }
}

async function previewPlan() {
  if (!props.application) return;
  loading.value = 'plan';
  try {
    plan.value = await applicationsApi.plan(props.application.id);
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to plan application';
  } finally {
    loading.value = '';
  }
}

async function save(deploy = false) {
  loading.value = deploy ? 'deploy' : 'save';
  try {
    const input = readInput();
    const app = props.application
      ? await applicationsApi.update(props.application.id, input)
      : await applicationsApi.create(input);
    if (deploy) await applicationsApi.deploy(app.id);
    emit('saved', app);
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to save application';
  } finally {
    loading.value = '';
  }
}
</script>

<template>
  <v-dialog :model-value="open" width="1180" @update:model-value="emit('close')">
    <v-card class="editor-card">
      <v-card-title class="d-flex align-center justify-space-between">
        <span>{{ title }}</span>
        <v-btn icon="mdi-close" variant="text" @click="emit('close')" />
      </v-card-title>
      <v-divider />
      <v-card-text>
        <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>
        <div class="editor-grid">
          <div class="editor-main">
            <div class="header-row mb-3">
              <v-text-field v-model="form.name" label="Application name" density="compact" variant="outlined" :readonly="Boolean(application)" hide-details />
              <v-switch v-model="form.enabled" label="Enabled" color="primary" density="compact" hide-details />
            </div>
            <v-tabs v-model="activeTab" density="comfortable" class="mb-4">
              <v-tab value="container">Container</v-tab>
              <v-tab value="scheduling">Scheduling</v-tab>
              <v-tab value="yaml">YAML</v-tab>
            </v-tabs>
            <v-window v-model="activeTab">
              <v-window-item value="container">
                <div class="field-grid">
                  <v-text-field v-model="specForm.name" label="Spec name" density="compact" variant="outlined" />
                  <v-text-field v-model="specForm.image" label="Image" density="compact" variant="outlined" />
                  <v-text-field v-model.number="specForm.count" type="number" min="1" label="Replicas" density="compact" variant="outlined" />
                  <v-text-field v-model.number="specForm.cpu" type="number" min="0" label="CPU MHz" density="compact" variant="outlined" />
                  <v-text-field v-model.number="specForm.memoryMb" type="number" min="0" label="Memory MB" density="compact" variant="outlined" />
                  <v-text-field v-model="specForm.command" label="Command" density="compact" variant="outlined" />
                  <v-text-field v-model="specForm.args" label="Arguments" density="compact" variant="outlined" class="span-2" />
                </div>

                <div class="section-title mt-2">Ports</div>
                <div v-for="(port, index) in specForm.ports" :key="index" class="repeat-row ports-row">
                  <v-text-field v-model="port.label" label="Label" density="compact" variant="outlined" hide-details />
                  <v-text-field v-model.number="port.to" type="number" label="Container port" density="compact" variant="outlined" hide-details />
                  <v-text-field v-model.number="port.static" type="number" label="Static host port" density="compact" variant="outlined" hide-details />
                  <v-btn icon="mdi-delete" variant="text" color="error" @click="removeAt(specForm.ports, index)" />
                </div>
                <v-btn size="small" variant="outlined" prepend-icon="mdi-plus" class="text-none mb-4" @click="addPort">Add port</v-btn>

                <div class="section-title">Environment</div>
                <div v-for="(item, index) in specForm.env" :key="index" class="repeat-row env-row">
                  <v-text-field v-model="item.key" label="Key" density="compact" variant="outlined" hide-details />
                  <v-text-field v-model="item.value" label="Value" density="compact" variant="outlined" hide-details />
                  <v-btn icon="mdi-delete" variant="text" color="error" @click="removeAt(specForm.env, index)" />
                </div>
                <v-btn size="small" variant="outlined" prepend-icon="mdi-plus" class="text-none" @click="addEnv">Add variable</v-btn>
              </v-window-item>

              <v-window-item value="scheduling">
                <div class="section-title">Services</div>
                <div v-for="(service, index) in specForm.services" :key="index" class="repeat-row service-row">
                  <v-text-field v-model="service.name" label="Name" density="compact" variant="outlined" hide-details />
                  <v-select v-model="service.port" :items="portOptions" label="Port" density="compact" variant="outlined" hide-details />
                  <v-text-field v-model="service.tags" label="Tags" density="compact" variant="outlined" hide-details />
                  <v-btn icon="mdi-delete" variant="text" color="error" @click="removeAt(specForm.services, index)" />
                </div>
                <v-btn size="small" variant="outlined" prepend-icon="mdi-plus" class="text-none mb-4" @click="addService">Add service</v-btn>

                <div class="section-title">Health checks</div>
                <div v-for="(check, index) in specForm.checks" :key="index" class="check-row">
                  <v-text-field v-model="check.name" label="Name" density="compact" variant="outlined" hide-details />
                  <v-select v-model="check.type" :items="['http', 'tcp', 'script']" label="Type" density="compact" variant="outlined" hide-details />
                  <v-select v-if="check.type !== 'script'" v-model="check.port" :items="portOptions" label="Port" density="compact" variant="outlined" hide-details />
                  <v-text-field v-if="check.type === 'http'" v-model="check.path" label="Path" density="compact" variant="outlined" hide-details />
                  <v-text-field v-if="check.type === 'script'" v-model="check.command" label="Command" density="compact" variant="outlined" hide-details />
                  <v-text-field v-model.number="check.intervalSeconds" type="number" label="Interval" density="compact" variant="outlined" hide-details />
                  <v-text-field v-model.number="check.timeoutSeconds" type="number" label="Timeout" density="compact" variant="outlined" hide-details />
                  <v-btn icon="mdi-delete" variant="text" color="error" @click="removeAt(specForm.checks, index)" />
                </div>
                <v-btn size="small" variant="outlined" prepend-icon="mdi-plus" class="text-none mb-4" @click="addCheck">Add check</v-btn>

                <div class="section-title">Constraints</div>
                <div v-for="(constraint, index) in specForm.constraints" :key="index" class="repeat-row constraint-row">
                  <v-text-field v-model="constraint.attribute" label="Attribute" density="compact" variant="outlined" hide-details />
                  <v-select v-model="constraint.operator" :items="['=', '!=', 'regexp', 'set_contains']" label="Operator" density="compact" variant="outlined" hide-details />
                  <v-text-field v-model="constraint.value" label="Value" density="compact" variant="outlined" hide-details />
                  <v-btn icon="mdi-delete" variant="text" color="error" @click="removeAt(specForm.constraints, index)" />
                </div>
                <v-btn size="small" variant="outlined" prepend-icon="mdi-plus" class="text-none mb-4" @click="addConstraint">Add constraint</v-btn>

                <div class="section-title">Host volumes</div>
                <div v-for="(volume, index) in specForm.volumes" :key="index" class="repeat-row volume-row">
                  <v-text-field v-model="volume.source" label="Nomad host volume" density="compact" variant="outlined" hide-details />
                  <v-text-field v-model="volume.target" label="Container path" density="compact" variant="outlined" hide-details />
                  <v-checkbox v-model="volume.readOnly" label="Read only" density="compact" hide-details />
                  <v-btn icon="mdi-delete" variant="text" color="error" @click="removeAt(specForm.volumes, index)" />
                </div>
                <v-btn size="small" variant="outlined" prepend-icon="mdi-plus" class="text-none" @click="addVolume">Add volume</v-btn>
              </v-window-item>

              <v-window-item value="yaml">
                <v-textarea v-model="form.specYaml" label="YAML spec" variant="outlined" rows="22" spellcheck="false" class="mono-input" />
              </v-window-item>
            </v-window>
          </div>
          <div class="editor-side">
            <v-btn block variant="outlined" prepend-icon="mdi-file-sync-outline" class="text-none mb-3" @click="applyVisualSpec">Render YAML from form</v-btn>
            <v-textarea v-model="variablesText" label="Variables JSON" variant="outlined" rows="8" spellcheck="false" class="mono-input" />
            <v-card variant="tonal" class="panel">
              <div class="text-subtitle-2 font-weight-bold mb-2">Validation</div>
              <div v-if="!validation" class="text-body-2 text-medium-emphasis">Run validation after saving an application.</div>
              <v-list v-else density="compact">
                <v-list-item v-for="issue in validation.issues" :key="`${issue.field || issue.path}:${issue.message}`" :title="issue.message" :subtitle="issue.field || issue.path || 'nomad'" />
                <v-list-item v-if="validation.valid" title="Valid" subtitle="Panel and Nomad validation passed" />
              </v-list>
            </v-card>
            <v-card variant="tonal" class="panel">
              <div class="text-subtitle-2 font-weight-bold mb-2">Plan preview</div>
              <pre class="plan-preview">{{ plan ? JSON.stringify(plan.plan, null, 2) : 'No plan loaded' }}</pre>
            </v-card>
          </div>
        </div>
      </v-card-text>
      <v-divider />
      <v-card-actions class="justify-end">
        <v-btn variant="text" class="text-none" @click="emit('close')">Cancel</v-btn>
        <v-btn variant="outlined" class="text-none" :disabled="!application" :loading="loading === 'validate'" @click="validate">Validate</v-btn>
        <v-btn variant="outlined" class="text-none" :disabled="!application" :loading="loading === 'plan'" @click="previewPlan">Plan</v-btn>
        <v-btn color="primary" variant="flat" class="text-none" :loading="loading === 'save'" @click="save(false)">Save</v-btn>
        <v-btn color="primary" variant="flat" class="text-none" :loading="loading === 'deploy'" @click="save(true)">Save and deploy</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<style scoped>
.editor-card { border-radius: 8px; }
.editor-grid { display: grid; grid-template-columns: minmax(0, 1fr) 340px; gap: 16px; }
.editor-main, .editor-side { min-width: 0; }
.header-row { display: grid; grid-template-columns: minmax(0, 1fr) 160px; gap: 12px; align-items: center; }
.field-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; }
.span-2 { grid-column: span 2; }
.section-title { margin: 14px 0 8px; color: rgba(var(--v-theme-on-surface), 0.72); font-size: 0.76rem; font-weight: 700; letter-spacing: 0; text-transform: uppercase; }
.repeat-row, .check-row { display: grid; gap: 8px; align-items: center; margin-bottom: 8px; }
.ports-row { grid-template-columns: 1fr 150px 150px 40px; }
.env-row { grid-template-columns: 1fr 1.4fr 40px; }
.service-row, .constraint-row { grid-template-columns: 1fr 150px 1fr 40px; }
.volume-row { grid-template-columns: 1fr 1fr 130px 40px; }
.check-row { grid-template-columns: 1fr 120px 130px 1fr 110px 110px 40px; }
.panel { padding: 12px; margin-top: 12px; }
.mono-input :deep(textarea), .plan-preview { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 0.82rem; }
.plan-preview { max-height: 180px; overflow: auto; white-space: pre-wrap; margin: 0; }
@media (max-width: 1100px) {
  .editor-grid { grid-template-columns: 1fr; }
  .field-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .check-row, .service-row, .constraint-row, .volume-row, .ports-row, .env-row { grid-template-columns: 1fr; }
}
</style>
