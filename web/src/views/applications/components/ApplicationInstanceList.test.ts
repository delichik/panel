// @vitest-environment jsdom
import { enableAutoUnmount, mount } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { useI18n } from '@/i18n';
import DetailSection from '@/components/patterns/DetailSection.vue';
import StatusBadge from '@/components/ui/StatusBadge.vue';
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
    updatedAt: '2026-09-15T05:00:00Z',
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
    updatedAt: '2026-09-15T04:00:00Z',
    ...overrides,
  };
}

describe('ApplicationInstanceList', () => {
  it('renders one/two card skeletons instead of the empty state while runtime loads', () => {
    const wrapper = mount(ApplicationInstanceList, { props: { instances: [], loading: true } });
    const skeletons = wrapper.findAll('[data-instance-skeleton]');
    expect(skeletons).toHaveLength(2);
    expect(skeletons[0]!.classes()).toContain('surface-card');
    expect(skeletons[0]!.text()).toContain(useI18n().t('common.status'));
    expect(skeletons[0]!.text()).toContain(useI18n().t('applicationsPage.updatedAtColumn'));
    expect(wrapper.text()).not.toContain(useI18n().t('applicationsPage.noRuntime'));
    expect(wrapper.find('[role="status"]').exists()).toBe(false);
  });

  it('shows the inset empty state when the runtime snapshot has no instances', () => {
    const wrapper = mount(ApplicationInstanceList, { props: { instances: [], loading: false } });
    const { t } = useI18n();
    expect(wrapper.text()).toContain(t('applicationsPage.noRuntime'));
    expect(wrapper.text()).toContain(t('applicationsPage.noRuntimeHint'));
    expect(wrapper.findAll('[data-instance-card]')).toHaveLength(0);
  });

  it('renders a node status card with header, 2x2 operation body, error area and footer', async () => {
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
    // 卡片网格：wide 两列，窄屏单列；卡片自身是 surface-card。
    expect(wrapper.get('[data-instance-list]').classes()).toContain('wide:grid-cols-2');
    const cards = wrapper.findAll('article[data-instance-card]');
    expect(cards).toHaveLength(1);
    expect(cards[0]!.classes()).toContain('surface-card');

    // 左侧 2px 语义色条（running → success）。
    const accent = cards[0]!.get(':scope > span');
    expect(accent.classes()).toContain('absolute');
    expect(accent.classes()).toContain('bg-success');

    // 头部：节点名 + 容器名/短 id + 单状态徽标。
    const header = cards[0]!.get('header');
    expect(header.text()).toContain('edge');
    expect(header.text()).toContain('panel-app');
    expect(header.findAllComponents(StatusBadge)).toHaveLength(1);
    expect(header.text()).toContain(t('applicationsPage.instanceStatus.running'));

    // 主体：2×2 operation 状态/阶段/尝试/下次重试。
    const body = cards[0]!.get('dl');
    expect(body.text()).toContain(t('applicationsPage.currentDeployment'));
    expect(body.text()).toContain(t('applicationsPage.operationStage'));
    expect(body.text()).toContain(t('applicationsPage.operationAttempt'));
    expect(body.text()).toContain(t('applicationsPage.operationNextRetry'));
    expect(body.text()).toContain('2');

    // 错误区保留 InfoBar/Expander 语义。
    expect(cards[0]!.text()).toContain('startup failed');
    expect(cards[0]!.find('[role="alert"]').exists()).toBe(true);
    expect(cards[0]!.text()).toContain(t('facilityDiagnostics.technicalDetails'));

    // 底部：查看操作 + 更新时间，点击 emit openOperation。
    expect(cards[0]!.get('footer').text()).toContain(t('applicationsPage.updatedAtColumn'));
    const operationButton = wrapper.findAll('button').find((button) => button.text().includes(t('applicationsPage.viewOperation')))!;
    await operationButton.trigger('click');
    expect(wrapper.emitted('openOperation')).toEqual([['intent-1', 'exec-1']]);
  });

  it('renders current status / last update instead of empty placeholders when there is no operation', () => {
    const wrapper = mount(ApplicationInstanceList, { props: { instances: [instance({ status: 'stopped' })] } });
    const { t } = useI18n();
    const card = wrapper.get('article[data-instance-card]');
    const body = card.get('dl');
    expect(body.text()).toContain(t('common.status'));
    expect(body.text()).toContain(t('applicationsPage.updatedAtColumn'));
    expect(body.text()).toContain(t('applicationsPage.instanceStatus.stopped'));
    // 无 operationId 时不渲染查看操作按钮。
    expect(card.findAll('button')).toHaveLength(0);
  });

  it('keeps the instance lastError visible when there is no operation error', () => {
    const wrapper = mount(ApplicationInstanceList, { props: { instances: [instance({ lastError: 'container exited with code 1' })] } });
    expect(wrapper.text()).toContain('container exited with code 1');
  });
});

describe('DetailSection surface prop', () => {
  it('renders its own surface-card by default and can opt out for external card lists', () => {
    const framed = mount(DetailSection, {
      props: { title: 'Nodes' },
      slots: { default: '<div class="section-row">row</div>' },
    });
    expect(framed.find('.surface-card').exists()).toBe(true);
    expect(framed.find('.divide-y').exists()).toBe(true);

    const unframed = mount(DetailSection, {
      props: { title: 'Nodes', surface: false },
      slots: { default: '<div class="section-row">row</div>' },
    });
    expect(unframed.find('.surface-card').exists()).toBe(false);
    expect(unframed.find('.divide-y').exists()).toBe(false);
    expect(unframed.find('[data-detail-section-body]').exists()).toBe(true);
    expect(unframed.text()).toContain('row');
  });
});
