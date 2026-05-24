<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue';
import { applicationsApi } from '@/api/applications';
import type { ApplicationDto, ApplicationPlanDto, ApplicationSaveDto, ApplicationValidationDto } from '@/types/api';

const props = defineProps<{ application: ApplicationDto | null; open: boolean }>();
const emit = defineEmits<{ close: []; saved: [ApplicationDto] }>();

const form = reactive<ApplicationSaveDto>({ name: '', enabled: false, specYaml: defaultSpec(), variables: {} });
const variablesText = ref('{}');
const validation = ref<ApplicationValidationDto | null>(null);
const plan = ref<ApplicationPlanDto | null>(null);
const loading = ref('');
const error = ref('');

const title = computed(() => (props.application ? `Edit ${props.application.name}` : 'Create application'));

watch(() => props.open, (open) => {
  if (!open) return;
  const app = props.application;
  form.name = app?.name ?? '';
  form.enabled = app?.enabled ?? false;
  form.specYaml = app?.specYaml ?? defaultSpec();
  form.variables = { ...(app?.variables ?? {}) };
  variablesText.value = JSON.stringify(form.variables, null, 2);
  validation.value = null;
  plan.value = null;
  error.value = '';
}, { immediate: true });

function defaultSpec() {
  return 'name: web\nimage: nginx:1.27\ncount: 1\nports:\n  - label: http\n    to: 80\nresources:\n  cpu: 100\n  memoryMb: 128\n';
}

function readInput(): ApplicationSaveDto {
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
  <v-dialog :model-value="open" width="960" @update:model-value="emit('close')">
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
            <v-text-field v-model="form.name" label="Name" density="compact" variant="outlined" :readonly="Boolean(application)" />
            <v-switch v-model="form.enabled" label="Enabled" color="primary" density="compact" hide-details />
            <v-textarea v-model="form.specYaml" label="YAML spec" variant="outlined" rows="16" spellcheck="false" class="mono-input" />
          </div>
          <div class="editor-side">
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
.panel { padding: 12px; margin-top: 12px; }
.mono-input :deep(textarea), .plan-preview { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 0.82rem; }
.plan-preview { max-height: 180px; overflow: auto; white-space: pre-wrap; margin: 0; }
@media (max-width: 900px) { .editor-grid { grid-template-columns: 1fr; } }
</style>
