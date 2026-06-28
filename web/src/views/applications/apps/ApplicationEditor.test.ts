import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const editor = readFileSync(resolve(__dirname, 'ApplicationEditor.vue'), 'utf8');

describe('ApplicationEditor', () => {
  it('exposes persistent mount permissions in the visual editor', () => {
    expect(editor).toContain('uid: nonNegativeNumberValue(mount?.uid)');
    expect(editor).toContain('gid: nonNegativeNumberValue(mount?.gid)');
    expect(editor).toContain("if (mount.type === 'persistent')");
    expect(editor).toContain('const uid = mountPermissionNumber(mount.uid)');
    expect(editor).toContain('const gid = mountPermissionNumber(mount.gid)');
    expect(editor).toContain('if (uid !== null) out.uid = uid');
    expect(editor).toContain('if (gid !== null) out.gid = gid');
    expect(editor).toContain("out.mode = mount.mode.trim()");
    expect(editor).toContain("t('applicationEditor.mountUid')");
    expect(editor).toContain("t('applicationEditor.mountGid')");
    expect(editor).toContain("t('applicationEditor.mountMode')");
  });
});
