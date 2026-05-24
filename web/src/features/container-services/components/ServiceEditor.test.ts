import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const serviceEditor = readFileSync(resolve(__dirname, 'ServiceEditor.vue'), 'utf8');

describe('ServiceEditor edit mode behavior', () => {
  it('defaults to the visual editor and offers a YAML mode toggle', () => {
    expect(serviceEditor).toContain("const editorMode = ref<'visual' | 'yaml'>('visual')");
    expect(serviceEditor).toContain('<v-btn-toggle v-model="editorMode"');
    expect(serviceEditor).toContain('value="visual"');
    expect(serviceEditor).toContain('value="yaml"');
  });

  it('does not require a manual write-back action before save or previews', () => {
    expect(serviceEditor).not.toContain('Write helper fields to YAML');
    expect(serviceEditor).not.toContain('@click="applyVisualToYaml"');
    expect(serviceEditor).not.toContain('class="preview-actions"');
    expect(serviceEditor).not.toContain('@click="runValidate"');
    expect(serviceEditor).not.toContain('@click="runSchedulePreview"');
    expect(serviceEditor).toContain('function scheduleAutoPreview()');
    expect(serviceEditor).toContain('function currentComposeServiceYaml()');
    expect(serviceEditor).toContain('composeServiceYaml: currentComposeServiceYaml()');
  });

  it('uses choice controls for bounded visual fields', () => {
    expect(serviceEditor).toContain('const restartOptions =');
    expect(serviceEditor).toContain('const networkModeOptions =');
    expect(serviceEditor).toContain('const dependencyOptions = computed');
    expect(serviceEditor).toContain('<v-select v-model="visual.restart"');
    expect(serviceEditor).toContain('<v-select v-model="visual.networkMode"');
    expect(serviceEditor).toContain('<v-select v-model="visual.dependsOn"');
    expect(serviceEditor).not.toContain('<v-text-field v-model="visual.restart"');
    expect(serviceEditor).not.toContain('<v-text-field v-model="visual.networkMode"');
    expect(serviceEditor).not.toContain('<v-combobox v-model="visual.dependsOn"');
  });

  it('removes variables and makes taints selectable from server traits', () => {
    expect(serviceEditor).not.toContain('variablesRaw');
    expect(serviceEditor).not.toContain('<span>Variables</span>');
    expect(serviceEditor).toContain('serversApi.listServers');
    expect(serviceEditor).toContain('const traitKeyOptions = computed');
    expect(serviceEditor).toContain('function traitValueOptions');
    expect(serviceEditor).toContain('<span>Taints</span>');
    expect(serviceEditor).toContain('<v-select v-model="row.key" :items="traitKeyOptions"');
    expect(serviceEditor).toContain('<v-select v-model="row.value" :items="traitValueOptions(row.key)"');
  });

  it('uses list editors for command ports and volumes and exposes file upload', () => {
    expect(serviceEditor).toContain('const commandItems = ref<string[]>([])');
    expect(serviceEditor).toContain('function addPort()');
    expect(serviceEditor).toContain('function addVolume()');
    expect(serviceEditor).toContain('function uploadBinary');
    expect(serviceEditor).toContain('base64Content: await readBase64(file)');
    expect(serviceEditor).toContain('<v-file-input');
    expect(serviceEditor).not.toContain('<v-combobox v-model="visual.ports"');
    expect(serviceEditor).not.toContain('<v-combobox v-model="visual.volumes"');
  });
});
