// @vitest-environment jsdom
import { enableAutoUnmount, mount } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { useI18n } from '@/i18n';
import type { ApplicationRuntimeInstance, LifecycleOperation } from '@/types/applications';
import ApplicationInstanceList from './ApplicationInstanceList.vue';

enableAutoUnmount(afterEach);

beforeEach(() => {
  useI18n().setLocale('en');
});

function operation(overrides: Partial<LifecycleOperation> = {}): LifecycleOperation {
  return {
    id: 'op-db',
    operationId: 'intent-1',
    executionId: 'exec-1',
    applicationId: 'app-1',
    type: 'deploy',
    status: 'failed_retryable',
    generation: 1,
    createdAt: '',
    updatedAt: '',
    ...overrides,
  };
}

function instance(overrides: Partial<ApplicationRuntimeInstance> = {}): ApplicationRuntimeInstance {
  return {
    instanceId: 'instance-1',
    serverId: 'srv-1',
    serverName: 'edge',
    containerName: 'panel-app',
    containerId: 'container-1',
    status: 'running',
    ...overrides,
  };
}

describe('ApplicationInstanceList', () => {
  it('renders skeleton rows instead of the empty state while runtime loads', () => {
    const wrapper = mount(ApplicationInstanceList, { props: { instances: [], loading: true } });
    expect(wrapper.findAll('.motion-skeleton').length).toBeGreaterThan(0);
    expect(wrapper.text()).not.toContain(useI18n().t('applicationsPage.noRuntime'));
    expect(wrapper.find('[role="status"]').exists()).toBe(false);
  });

  it('shows the inset empty state when the runtime snapshot has no instances', () => {
    const wrapper = mount(ApplicationInstanceList, { props: { instances: [], loading: false } });
    const { t } = useI18n();
    expect(wrapper.text()).toContain(t('applicationsPage.noRuntime'));
    expect(wrapper.text()).toContain(t('applicationsPage.noRuntimeHint'));
  });

  it('renders the node status and emits openOperation for the latest deployment', async () => {
    const wrapper = mount(ApplicationInstanceList, {
      props: {
        instances: [
          instance({
            operation: operation({ attempt: 2, errorCode: 'container_not_running', error: 'startup failed', errorDetail: 'containerLogs:\ncertificate missing' }),
          }),
        ],
      },
    });

    const { t } = useI18n();
    expect(wrapper.text()).toContain('edge');
    expect(wrapper.text()).toContain('panel-app');
    expect(wrapper.text()).toContain(t('applicationsPage.instanceStatus.running'));
    expect(wrapper.text()).toContain(t('applicationsPage.operationStage'));
    expect(wrapper.text()).toContain('startup failed');

    const operationButton = wrapper.findAll('button').find((button) => button.text().includes(t('applicationsPage.viewOperation')))!;
    await operationButton.trigger('click');
    expect(wrapper.emitted('openOperation')).toEqual([['intent-1', 'exec-1']]);
  });

  it('keeps the instance lastError visible when there is no operation error', () => {
    const wrapper = mount(ApplicationInstanceList, { props: { instances: [instance({ lastError: 'container exited with code 1' })] } });
    expect(wrapper.text()).toContain('container exited with code 1');
  });
});
