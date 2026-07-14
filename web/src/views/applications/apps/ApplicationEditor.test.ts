import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const editor = readFileSync(resolve(__dirname, 'ApplicationEditor.vue'), 'utf8');

describe('ApplicationEditor', () => {
  it('commits the save session without calling the deploy endpoint', () => {
    expect(editor).toContain('const app = await applicationsApi.commitSaveSession(session.id)');
    expect(editor).toContain("emit('saved', app)");
    expect(editor).not.toContain('applicationsApi.deploy(app.id)');
  });

  it('blocks the editor while showing save progress stages', () => {
    expect(editor).toContain('persistent: saving.value');
    expect(editor).toContain('v-overlay :model-value="saving" contained persistent');
    expect(editor).toContain("t('applicationEditor.saveStepStartingSession')");
    expect(editor).toContain("t('applicationEditor.saveStepDeletingFile'");
    expect(editor).toContain("t('applicationEditor.saveStepUploadingFile'");
    expect(editor).toContain("t('applicationEditor.saveStepUploadingArchive'");
    expect(editor).toContain("t('applicationEditor.saveStepCommitApplying')");
    expect(editor).toContain(':disabled="saving" @click="requestClose(false)"');
  });

  it('can render the same editor inside a full page shell', () => {
    expect(editor).toContain('embedded?: boolean');
    expect(editor).toContain("props.embedded ? 'div' : 'v-dialog'");
    expect(editor).toContain('editor-card--embedded');
    expect(editor).toContain('editorVisible');
    expect(editor).toContain('editor-main--embedded');
    expect(editor).toContain('editor-section-nav');
    expect(editor).toContain('editorSections');
    expect(editor).toContain('v-if="!embedded" class="app-dialog-title"');
    expect(editor).toContain('v-tabs v-if="!embedded"');
    expect(editor).not.toContain('createEditorTabs');
  });

  it('uses YAML as an embedded AppSpec replacement mode with a code editor surface', () => {
    expect(editor).toContain("id: 'application-editor-yaml'");
    expect(editor).toContain('const embeddedYamlEditing = ref(false)');
    expect(editor).toContain('function prepareEmbeddedYamlEdit()');
    expect(editor).toContain('const specEditorMode = computed');
    expect(editor).toContain('const yamlLineNumbers = computed');
    expect(editor).toContain('v-btn-toggle :model-value="embeddedSpecMode"');
    expect(editor).toContain('v-window v-model="specEditorMode"');
    expect(editor).toContain("embeddedSpecMode.value === 'visual'");
    expect(editor).toContain(":id=\"embedded ? 'application-editor-yaml' : undefined\"");
    expect(editor).toContain('class="yaml-code-editor"');
    expect(editor).toContain('class="yaml-code-gutter"');
    expect(editor).toContain('class="yaml-code-textarea"');
    expect(editor).toContain('@focus="prepareEmbeddedYamlEdit"');
    expect(editor).toContain('@scroll="syncYamlEditorScroll"');
    expect(editor).not.toContain('v-dialog v-model="yamlDialog"');
    expect(editor).not.toContain('yaml-inline-input');
  });

  it('keeps application file edits in a focused dialog', () => {
    expect(editor).toContain('const fileDialog = ref(false)');
    expect(editor).toContain('v-dialog v-model="fileDialog" width="720"');
    expect(editor).toContain("openFileDialog('template')");
    expect(editor).toContain("openFileDialog('binary')");
    expect(editor).toContain('fileDialog.value = true');
    expect(editor).toContain('fileDialog.value = false');
    expect(editor).toContain('fileKindLocked');
    expect(editor).toContain("fileForm.intent = 'edit-template'");
    expect(editor).toContain("fileForm.intent = 'replace-binary'");
  });

  it('keeps the editor header and footer without heavy divider lines', () => {
    expect(editor).not.toContain('<v-divider />\r\n      <v-card-text class="app-dialog-body">');
    expect(editor).not.toContain('</v-card-text>\r\n      <v-divider />');
  });

  it('exposes persistent mount permissions in the visual editor', () => {
    expect(editor).toContain('uid: nonNegativeNumberValue(mount?.uid)');
    expect(editor).toContain('gid: nonNegativeNumberValue(mount?.gid)');
    expect(editor).toContain("mount.type === 'persistent' || mount.source.trim()");
    expect(editor).toContain('const uid = mountPermissionNumber(mount.uid)');
    expect(editor).toContain('const gid = mountPermissionNumber(mount.gid)');
    expect(editor).toContain('if (uid !== null) out.uid = uid');
    expect(editor).toContain('if (gid !== null) out.gid = gid');
    expect(editor).toContain('if (mountSupportsMode(mount.type))');
    expect(editor).toContain('if (mount.mode.trim()) out.mode = mount.mode.trim()');
    expect(editor).toContain("t('applicationEditor.mountUid')");
    expect(editor).toContain("t('applicationEditor.mountGid')");
    expect(editor).toContain("t('applicationEditor.mountMode')");
  });

  it('supports editing templates and replacing binary uploads', () => {
    expect(editor).toContain('applicationsApi.getFile(props.application.id, file.id)');
    expect(editor).toContain("fileForm.kind = 'template'");
    expect(editor).toContain("fileForm.kind = 'binary'");
    expect(editor).toContain("fileForm.mode = 'single'");
    expect(editor).toContain("file.contentBase64 !== undefined");
    expect(editor).toContain('@click="editTemplateFile(file)"');
    expect(editor).toContain("t('applicationEditor.replaceBinaryFile')");
  });

  it('treats folder archives as archive replacements for matching workspace prefixes', () => {
    expect(editor).toContain("kind: 'archive'");
    expect(editor).toContain('const replacedFiles = files.value.filter((file) => isArchivePathMatch(file.path, path));');
    expect(editor).toContain('files.value = files.value.filter((file) => !isArchivePathMatch(file.path, path));');
    expect(editor).toContain('archive.replacedFiles');
    expect(editor).toContain("t('applicationEditor.replaceFolderArchive')");
  });

  it('keeps reverse proxy dialog edits isolated until the dialog is saved', () => {
    expect(editor).toContain('draft: null as ApplicationReverseProxyRuleDto | null');
    expect(editor).toContain('proxyRuleDialog.draft = structuredClone(form.reverseProxy?.[index] ?? null);');
    expect(editor).toContain('const next = structuredClone(proxyRuleDialog.draft);');
    expect(editor).toContain('form.reverseProxy = [...(form.reverseProxy ?? []), next];');
    expect(editor).toContain('proxyRuleDialog.draft = null;');
    expect(editor).toContain('@click="saveProxyRuleDialog"');
    expect(editor).toContain('@click="closeProxyRuleDialog"');
  });

  it('clones and saves every structured path option', () => {
    expect(editor).toContain("gzipMode: path.options?.gzipMode || 'inherit'");
    expect(editor).toContain('clientMaxBodySizeMb: path.options?.clientMaxBodySizeMb || 0');
    expect(editor).toContain('connectTimeoutSeconds: path.options?.connectTimeoutSeconds || 0');
    expect(editor).toContain('readTimeoutSeconds: path.options?.readTimeoutSeconds || 0');
    expect(editor).toContain('sendTimeoutSeconds: path.options?.sendTimeoutSeconds || 0');
    expect(editor).toContain("bufferingMode: path.options?.bufferingMode || 'inherit'");
    expect(editor).toContain("webSocketMode: path.options?.webSocketMode || (path.webSocket ? 'on' : 'off')");
    expect(editor).toContain('requestHeaders: (path.options?.requestHeaders ?? [])');
    expect(editor).toContain('responseHeaders: (path.options?.responseHeaders ?? [])');
    expect(editor).toContain('<RoutePathAdvancedFields v-model="path.options" proxy gzip />');
  });
});
