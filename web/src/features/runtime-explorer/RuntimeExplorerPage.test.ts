import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const runtimeExplorerPage = readFileSync(resolve(__dirname, 'pages/RuntimeExplorerPage.vue'), 'utf8');

describe('RuntimeExplorerPage managed container deletion', () => {
  it('requires explicit confirmation before deleting a managed container', () => {
    expect(runtimeExplorerPage).toContain('confirmManagedDelete');
    expect(runtimeExplorerPage).toContain('This container is managed by Container Service');
    expect(runtimeExplorerPage).toContain('will automatically redeploy');
    expect(runtimeExplorerPage).toContain('<v-dialog v-model="managedDeleteDialog"');
    expect(runtimeExplorerPage).toContain('@click="confirmManagedDelete"');
  });
});
