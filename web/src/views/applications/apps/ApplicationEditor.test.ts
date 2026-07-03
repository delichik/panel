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
    expect(editor).toContain(':persistent="saving"');
    expect(editor).toContain('v-overlay :model-value="saving" contained persistent');
    expect(editor).toContain("t('applicationEditor.saveStepStartingSession')");
    expect(editor).toContain("t('applicationEditor.saveStepDeletingFile'");
    expect(editor).toContain("t('applicationEditor.saveStepUploadingFile'");
    expect(editor).toContain("t('applicationEditor.saveStepUploadingArchive'");
    expect(editor).toContain("t('applicationEditor.saveStepCommitApplying')");
    expect(editor).toContain(':disabled="saving" @click="requestClose(false)"');
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
    expect(editor).toContain("t('applicationEditor.editTemplateFile')");
    expect(editor).toContain("t('applicationEditor.replaceBinaryFile')");
  });

  it('treats folder archives as binary replacements for matching workspace prefixes', () => {
    expect(editor).toContain("kind: 'binary'");
    expect(editor).toContain('const replacedFiles = files.value.filter((file) => isArchivePathMatch(file.path, path));');
    expect(editor).toContain('files.value = files.value.filter((file) => !isArchivePathMatch(file.path, path));');
    expect(editor).toContain('archive.replacedFiles');
    expect(editor).toContain("t('applicationEditor.replaceFolderArchive')");
  });
});
