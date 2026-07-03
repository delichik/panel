import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

function read(relativePath: string) {
  return readFileSync(resolve(__dirname, relativePath), 'utf8');
}

describe('selector detail workspaces', () => {
  it('uses the shared selector width across desktop layouts', () => {
    const pages = [
      read('servers/_shared/ServersPageContent.vue'),
      read('resources/packages/index.vue'),
      read('security/firewall/index.vue'),
      read('security/fail2ban/index.vue'),
      read('dns/domains/index.vue'),
      read('tasks/index.vue'),
      read('applications/apps/index.vue'),
    ];

    for (const page of pages) {
      expect(page).toContain('clamp(300px, 26vw, 340px) minmax(0, 1fr)');
      expect(page).toContain('@media (max-width: 1080px)');
    }
  });

  it('clears stale async details and ignores late responses', () => {
    const packagesPage = read('resources/packages/index.vue');
    const firewallPage = read('security/firewall/index.vue');
    const fail2banPage = read('security/fail2ban/index.vue');
    const domainsPage = read('dns/domains/index.vue');
    const runtimePanel = read('applications/apps/ApplicationRuntimePanel.vue');
    const logsDialog = read('../components/RuntimeLogsDialog.vue');
    const tasksPage = read('tasks/index.vue');
    const applicationEditor = read('applications/apps/ApplicationEditor.vue');

    expect(packagesPage).toContain('updates.value = null;');
    expect(packagesPage).toContain('requestId !== updatesRequestId || serverId.value !== requestedServerId');
    expect(packagesPage).toContain('loadingUpdates && !updates');

    expect(firewallPage).toContain('state.value = null;');
    expect(firewallPage).toContain('requestId !== stateRequestId || serverId.value !== requestedServerId');
    expect(firewallPage).toContain('loadingState && !state');

    expect(fail2banPage).toContain('fail2banState.value = null;');
    expect(fail2banPage).toContain('requestId !== stateRequestId || serverId.value !== requestedServerId');
    expect(fail2banPage).toContain('loadingState && !fail2banState');

    expect(domainsPage).toContain('records.value = [];');
    expect(domainsPage).toContain('requestId !== recordsRequestId || selectedDomainId.value !== domain.id');
    expect(domainsPage).toContain('recordsLoading && records.length === 0 && selectedDomain');
    expect(domainsPage).toContain('class="domain-detail-actions"');
    expect(domainsPage).toContain('@click="resetForm(selectedDomain)"');
    expect(domainsPage).toContain('@click="askDeleteDomain(selectedDomain)"');
    expect(domainsPage).not.toContain('icon="mdi-dots-vertical"');

    expect(runtimePanel).toContain('runtime.value = null;');
    expect(runtimePanel).toContain('requestId !== runtimeRequestId || props.application.id !== requestedApplicationId');
    expect(runtimePanel).toContain('loading && !runtime');

    expect(logsDialog).toContain('logs.value = \'\';');
    expect(logsDialog).toContain('requestId !== logsRequestId || props.targetKey !== requestedTargetKey');
    expect(logsDialog).toContain('loading && !logs');

    expect(tasksPage).toContain('steps.value = [];');
    expect(tasksPage).toContain('requestId !== stepsRequestId || selectedTaskId.value !== taskId');
    expect(tasksPage).toContain('stepsLoading && steps.length === 0');

    expect(applicationEditor).toContain('filesRequestId += 1;');
    expect(applicationEditor).toContain('requestId !== filesRequestId || props.application?.id !== applicationId || !editorVisible.value');
  });

  it('uses the shared selector components across left-side selectors', () => {
    const selector = read('../components/ServerSelector.vue');
    const serverItem = read('../components/ServerSelectorItem.vue');
    const serversPage = read('servers/_shared/ServersPageContent.vue');
    const domainsPage = read('dns/domains/index.vue');
    const tasksPage = read('tasks/index.vue');

    for (const page of [domainsPage, tasksPage]) {
      expect(page).toContain('<AppSelectorPanel');
      expect(page).toContain('<AppSelectorItem');
    }

    for (const page of [selector, serversPage]) {
      expect(page).toContain('<AppSelectorPanel');
      expect(page).toContain('<ServerSelectorItem');
    }

    expect(serverItem).toContain('<AppSelectorItem');
    expect(serverItem).toContain('{{ server.host }}:{{ server.port }}');
    expect(serverItem).toContain('agentStatusForServer(server).label');
  });

  it('routes every server-based selector through the same presentation', () => {
    const firewallPage = read('security/firewall/index.vue');
    const packagesPage = read('resources/packages/index.vue');
    const fail2banPage = read('security/fail2ban/index.vue');
    const resourcePage = read('resources/_shared/ResourcePage.vue');
    const resourceConsumers = [
      read('resources/containers/index.vue'),
      read('resources/images/index.vue'),
      read('resources/networks/index.vue'),
      read('resources/volumes/index.vue'),
    ];

    expect(firewallPage).toContain('<ServerSelector');
    expect(packagesPage).toContain('<ServerSelector');
    expect(fail2banPage).toContain('<ServerSelector');
    expect(resourcePage).toContain('<ServerSelector');
    for (const page of resourceConsumers) expect(page).toContain('<ResourcePage');
  });

  it('clears server resource content and ignores late server responses', () => {
    const resourceConsumers = [
      read('resources/containers/index.vue'),
      read('resources/images/index.vue'),
      read('resources/networks/index.vue'),
      read('resources/volumes/index.vue'),
    ];

    for (const page of resourceConsumers) {
      expect(page).toContain('const requestedServerId = serverId.value;');
      expect(page).toContain('serverId.value !== requestedServerId');
      expect(page).toContain('loading.value = false;');
    }

    expect(resourceConsumers[0]).toContain('items.value = [];');
    expect(resourceConsumers[1]).toContain('data.value = null;');
    expect(resourceConsumers[2]).toContain('items.value = [];');
    expect(resourceConsumers[3]).toContain('items.value = [];');
  });

  it('keeps selector loading content within the narrow panel', () => {
    const panel = read('../components/AppSelectorPanel.vue');
    const loading = read('../components/PageLoadingState.vue');

    expect(panel).toContain('<PageLoadingState v-if="loading && empty" compact');
    expect(loading).toContain('width: min(320px, 100%)');
    expect(loading).toContain(':size="compact ? 32 : 42"');
    expect(loading).not.toContain('72vw');
  });

  it('does not show a count chip in the shared selector header', () => {
    const panel = read('../components/AppSelectorPanel.vue');

    expect(panel).not.toContain('count: number');
    expect(panel).not.toContain('{{ count }}');
  });

  it('supports a vertically centered leading control in the selector header', () => {
    const panel = read('../components/AppSelectorPanel.vue');

    expect(panel).toContain('<slot name="leading" />');
    expect(panel).toContain('class="app-selector-panel__header-leading"');
    expect(panel).toContain('align-self: stretch');
  });
});
