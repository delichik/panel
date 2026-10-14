// @vitest-environment jsdom
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import ApplicationLogsDialog from './ApplicationLogsDialog.vue';
import { applicationsApi } from '@/api/applications';
import { toastKey } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { ApplicationRuntime, ApplicationRuntimeInstance, LogResult } from '@/types/applications';

const nodeA: ApplicationRuntimeInstance = { instanceId: 'instance-a', serverId: 'server-a', serverName: 'Tokyo', containerName: 'panel-app', containerId: 'container-a', status: 'running' };
const nodeB: ApplicationRuntimeInstance = { instanceId: 'instance-b', serverId: 'server-b', serverName: 'Phoenix', containerName: 'panel-app', containerId: 'container-b', status: 'stopped' };
const runtime = (instances = [nodeA, nodeB]): ApplicationRuntime => ({ applicationId: 'app', runtimeId: '', status: 'running', instances, observedAt: '' });
let wrapper: VueWrapper | undefined;
const toast = vi.fn();
async function render(open = true) {
  wrapper = mount(ApplicationLogsDialog, { props: { open, applicationId: 'app', applicationName: 'My app' }, global: {
    provide: { [toastKey as symbol]: { push: toast } },
    stubs: {
      Dialog: { props: ['open'], template: '<div v-if="open"><slot /></div>' },
      Select: { props: ['modelValue', 'options', 'disabled'], emits: ['update:modelValue'], template: '<select :value="modelValue" :disabled="disabled" @change="$emit(\'update:modelValue\', $event.target.value)"><option value=""></option><option v-for="option in options" :key="option.value" :value="option.value" :disabled="option.disabled">{{ option.label }}</option></select>' },
    },
  } });
  await flushPromises();
  return wrapper;
}
beforeEach(() => {
  useI18n().setLocale('en');
  toast.mockClear();
  vi.spyOn(applicationsApi, 'runtime').mockResolvedValue(runtime());
  vi.spyOn(applicationsApi, 'logs').mockImplementation(async (_app, { instanceId }) => ({ instanceId, logs: `logs for ${instanceId}` }));
});
afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.restoreAllMocks(); });

describe('application container logs', () => {
  it('requires a choice for multiple instances and identifies servers with the same container name', async () => {
    await render(false);
    expect(applicationsApi.runtime).not.toHaveBeenCalled();
    await wrapper!.setProps({ open: true });
    await flushPromises();
    expect(wrapper!.text()).toContain('Tokyo · panel-app');
    expect(wrapper!.text()).toContain('Phoenix · panel-app');
    expect(applicationsApi.logs).not.toHaveBeenCalled();
    await wrapper!.find('select').setValue('instance-b');
    await flushPromises();
    expect(applicationsApi.logs).toHaveBeenCalledWith('app', { instanceId: 'instance-b', tail: 240 }, expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(wrapper!.text()).toContain('logs for instance-b');
  });

  it('automatically selects a single stopped container and keeps missing containers unavailable', async () => {
    vi.mocked(applicationsApi.runtime).mockResolvedValue(runtime([nodeB, { ...nodeA, status: 'missing' }]));
    await render();
    expect(applicationsApi.logs).toHaveBeenCalledTimes(1);
    expect(applicationsApi.logs).toHaveBeenCalledWith('app', expect.objectContaining({ instanceId: 'instance-b' }), expect.anything());
    expect(wrapper!.find('option[value="instance-a"]').attributes('disabled')).toBeDefined();
  });

  it('aborts the previous selection and ignores late logs, then aborts on close', async () => {
    let finish!: (result: LogResult) => void;
    vi.mocked(applicationsApi.logs).mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
    await render();
    await wrapper!.find('select').setValue('instance-a');
    const signal = vi.mocked(applicationsApi.logs).mock.calls[0][2]?.signal;
    await wrapper!.find('select').setValue('instance-b');
    await flushPromises();
    expect(signal?.aborted).toBe(true);
    finish({ instanceId: 'instance-a', logs: 'old container logs' });
    await flushPromises();
    expect(wrapper!.text()).toContain('logs for instance-b');
    expect(wrapper!.text()).not.toContain('old container logs');
    vi.mocked(applicationsApi.logs).mockImplementationOnce(() => new Promise(() => {}));
    await wrapper!.find('select').setValue('instance-a');
    const pending = vi.mocked(applicationsApi.logs).mock.calls.at(-1)?.[2]?.signal;
    await wrapper!.setProps({ open: false });
    expect(pending?.aborted).toBe(true);
  });

  it('shows an empty state without requesting logs when no containers exist', async () => {
    vi.mocked(applicationsApi.runtime).mockResolvedValue(runtime([]));
    await render();
    expect(wrapper!.text()).toContain('No containers with available logs');
    expect(applicationsApi.logs).not.toHaveBeenCalled();
  });

  it('keeps failures distinct from empty logs and retries the selected instance', async () => {
    vi.mocked(applicationsApi.runtime).mockResolvedValue(runtime([nodeA]));
    vi.mocked(applicationsApi.logs).mockRejectedValueOnce(new Error('Agent unavailable')).mockResolvedValueOnce({ instanceId: 'instance-a', logs: '' });
    await render();
    expect(wrapper!.find('[role="alert"]').text()).toContain('Agent unavailable');
    expect(toast).toHaveBeenCalledTimes(1);
    await wrapper!.findAll('button').find(button => button.text() === 'Retry')!.trigger('click');
    await flushPromises();
    expect(wrapper!.text()).toContain('This container has no log output');
    expect(applicationsApi.logs).toHaveBeenCalledTimes(2);
  });

  it('ignores the old instance list after changing applications', async () => {
    let finish!: (result: ApplicationRuntime) => void;
    vi.mocked(applicationsApi.runtime).mockImplementationOnce(() => new Promise(resolve => { finish = resolve; })).mockResolvedValueOnce({ ...runtime([nodeB]), applicationId: 'another-app' });
    await render();
    const signal = vi.mocked(applicationsApi.runtime).mock.calls[0][1]?.signal;
    await wrapper!.setProps({ applicationId: 'another-app' });
    await flushPromises();
    expect(signal?.aborted).toBe(true);
    finish(runtime([nodeA]));
    await flushPromises();
    expect(applicationsApi.logs).toHaveBeenCalledTimes(1);
    expect(applicationsApi.logs).toHaveBeenCalledWith('another-app', { instanceId: 'instance-b', tail: 240 }, expect.anything());
    expect(wrapper!.text()).not.toContain('Tokyo');
  });

  it('shows instance loading failures and allows retry', async () => {
    vi.mocked(applicationsApi.runtime).mockRejectedValueOnce(new Error('Unable to list instances')).mockResolvedValueOnce(runtime([nodeA]));
    await render();
    expect(wrapper!.find('[role="alert"]').text()).toContain('Unable to list instances');
    expect(applicationsApi.logs).not.toHaveBeenCalled();
    await wrapper!.findAll('button').find(button => button.text() === 'Retry')!.trigger('click');
    await flushPromises();
    expect(wrapper!.text()).toContain('logs for instance-a');
  });
});
