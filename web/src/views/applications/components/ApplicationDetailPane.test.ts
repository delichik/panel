// @vitest-environment jsdom
import { enableAutoUnmount, mount } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { useI18n } from '@/i18n';
import DetailSection from '@/components/patterns/DetailSection.vue';
import type { ApplicationDto, ApplicationFile, ApplicationRuntime, ApplicationRuntimeInstance, DeploymentPlanningError } from '@/types/applications';
import ApplicationDetailPane from './ApplicationDetailPane.vue';

enableAutoUnmount(afterEach);

beforeEach(() => {
  useI18n().setLocale('en');
});

const tabs: Array<{ label: string; value: 'runtime' | 'routes' | 'files' | 'data' }> = [
  { label: 'Runtime', value: 'runtime' },
  { label: 'Routes', value: 'routes' },
  { label: 'Files', value: 'files' },
  { label: 'Persistent data', value: 'data' },
];

function application(overrides: Partial<ApplicationDto> = {}): ApplicationDto {
  return {
    id: 'app-1',
    version: 3,
    kind: 'container',
    name: 'API',
    enabled: true,
    specYaml: '',
    deploymentMode: 'all',
    deploymentServers: ['srv-1'],
    reverseProxy: [],
    generation: 1,
    specHash: '',
    imageReference: 'registry.example/api:1',
    imageUpdateAvailable: false,
    jobId: '',
    namespace: 'default',
    createdAt: '',
    updatedAt: '',
    ...overrides,
  };
}

function node(): ApplicationRuntimeInstance {
  return { instanceId: 'instance-1', serverId: 'srv-1', serverName: 'edge', containerName: 'panel-app', status: 'running' };
}

function runtime(overrides: Partial<ApplicationRuntime> = {}): ApplicationRuntime {
  return { applicationId: 'app-1', runtimeId: 'runtime-1', status: 'running', instances: [node()], observedAt: '', ...overrides };
}

function file(overrides: Partial<ApplicationFile> = {}): ApplicationFile {
  return { name: 'config/app.yaml', kind: 'text', contentType: 'text/yaml', size: 128, sha256: 'abc', createdAt: '', updatedAt: '', ...overrides };
}

function render(overrides: Record<string, unknown> = {}) {
  return mount(ApplicationDetailPane, {
    props: {
      application: application(),
      appStatus: 'deployed',
      nodeCounts: { healthy: 2, abnormal: 0, deploying: 1 },
      runtime: runtime(),
      files: [],
      persistentServerOptions: [],
      tabs,
      activeTab: 'runtime' as const,
      serverName: (id: string) => (id === 'srv-1' ? 'edge' : id),
      ...overrides,
    },
  });
}

describe('ApplicationDetailPane', () => {
  it('renders the consolidated status row and the runtime summary counts', () => {
    const wrapper = render();
    const { t } = useI18n();

    expect(wrapper.text()).toContain(t('applicationsPage.status.deployed'));
    expect(wrapper.text()).toContain(t('applicationsPage.nodeBucketHealthyLabel'));
    expect(wrapper.text()).toContain('2');
    expect(wrapper.text()).toContain(t('applicationsPage.instanceStatus.running'));
    expect(wrapper.text()).toContain('edge · panel-app'.split(' · ')[0]!);
  });

  it('turns off the DetailSection surface for node instance cards (no card-in-card)', () => {
    const wrapper = render();
    const { t } = useI18n();
    const sections = wrapper.findAllComponents(DetailSection);
    expect(sections.length).toBeGreaterThanOrEqual(2);

    const nodeInstances = sections.find((section) => section.props('title') === t('applicationsPage.nodeInstances'));
    expect(nodeInstances).toBeTruthy();
    expect(nodeInstances!.props('surface')).toBe(false);
    // 父级分区不再渲染自身 surface-card/divide-y 包裹；卡片网格直接落在 body 里。
    expect(nodeInstances!.find('[data-detail-section-body]').exists()).toBe(true);
    expect(nodeInstances!.find('.surface-card.overflow-hidden > .divide-y').exists()).toBe(false);
    expect(nodeInstances!.find('[data-instance-card]').exists()).toBe(true);

    const runtimeSummary = sections.find((section) => section.props('title') === t('applicationsPage.runtime'));
    expect(runtimeSummary?.props('surface')).not.toBe(false);
  });

  it('shows the planning-failed badge and planning detail without a second attention badge', () => {
    const planningError: DeploymentPlanningError = {
      code: 'invalid_spec',
      message: 'reverse proxy domain is invalid',
      field: 'reverseProxy[0].domain',
      retryable: false,
      operationId: 'intent-9',
      occurredAt: '',
      configVersion: 4,
    };
    const wrapper = render({ planningError });
    const { t } = useI18n();

    expect(wrapper.text()).toContain(t('applicationsPage.status.planning_failed'));
    expect(wrapper.text()).toContain('reverse proxy domain is invalid');
    expect(wrapper.text()).not.toContain(t('applicationsPage.status.attention'));
  });

  it('shows the loading overlay from the page-provided showLoading flag', () => {
    const idle = render();
    expect(idle.find('.loading-overlay').exists()).toBe(false);

    const loading = render({ showLoading: true });
    expect(loading.find('.loading-overlay').exists()).toBe(true);
  });

  it('emits tab changes and keeps the URL owner in the page', async () => {
    const wrapper = render();
    const routesTab = wrapper.findAll('[role="tab"]').find((tab) => tab.text() === 'Routes')!;
    await routesTab.trigger('click');
    expect(wrapper.emitted('update:activeTab')).toEqual([['routes']]);
  });

  it('renders routes with resolved server names and files with a download emit', async () => {
    const wrapper = render({
      activeTab: 'routes',
      application: application({
        reverseProxy: [{ domain: 'app.example.test', targetPort: 8080, originServerIds: ['srv-1'], paths: [{ path: '/', options: undefined }] } as ApplicationDto['reverseProxy'][number]],
      }),
    });
    expect(wrapper.text()).toContain('app.example.test');
    expect(wrapper.text()).toContain('edge');

    const filesWrapper = render({ activeTab: 'files', files: [file()] });
    expect(filesWrapper.text()).toContain('config/app.yaml');
    const download = filesWrapper.findAll('button').find((button) => button.text().includes(useI18n().t('common.download')))!;
    await download.trigger('click');
    expect(filesWrapper.emitted('downloadFile')?.[0]?.[0]).toMatchObject({ name: 'config/app.yaml' });
  });

  it('wires the persistent data tab to page-owned select/download/restore handlers', async () => {
    const wrapper = render({
      activeTab: 'data',
      application: application({ hasPersistentData: true, persistentPath: '/data/app', persistentServers: ['srv-1'] }),
      persistentServerId: 'srv-1',
      persistentServerOptions: [{ label: 'edge', value: 'srv-1' }],
    });

    expect(wrapper.text()).toContain(useI18n().t('applicationsPage.persistentDataHint'));

    const buttons = wrapper.findAll('button');
    const download = buttons.find((button) => button.text().includes(useI18n().t('applicationsPage.downloadPersistentData')))!;
    await download.trigger('click');
    expect(wrapper.emitted('downloadPersistentData')).toHaveLength(1);
  });
});
