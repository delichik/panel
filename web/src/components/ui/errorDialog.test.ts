// @vitest-environment jsdom
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { defineComponent, nextTick } from 'vue';
import { createMemoryHistory, createRouter, type Router } from 'vue-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useI18n } from '@/i18n';
import ErrorDialogHost from './ErrorDialogHost.vue';
import { errorDialogKey, useErrorDialog, type ErrorDialogApi } from './errorDialog';

const wrappers: VueWrapper[] = [];

function makeRouter(): Router {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/logs', component: { template: '<div />' } },
    ],
  });
}

async function mountHost() {
  const router = makeRouter();
  await router.push('/');
  await router.isReady();
  const wrapper = mount(ErrorDialogHost, { attachTo: document.body, global: { plugins: [router] } });
  wrappers.push(wrapper);
  return { wrapper, router, api: wrapper.vm as unknown as ErrorDialogApi };
}

function dialog() {
  return document.querySelector<HTMLElement>('[role="dialog"]');
}

function dialogTitle() {
  return dialog()?.querySelector('h2')?.textContent?.trim();
}

async function closeCurrent() {
  const button = dialog()?.querySelector<HTMLButtonElement>('button[aria-label="Close"]');
  expect(button).not.toBeNull();
  button!.click();
  await nextTick();
  await flushPromises();
}

beforeEach(() => {
  useI18n().setLocale('en');
});

afterEach(() => {
  while (wrappers.length) wrappers.pop()!.unmount();
  document.body.innerHTML = '';
  document.body.style.overflow = '';
  useI18n().setLocale('zh-CN');
  vi.restoreAllMocks();
});

describe('global error dialog', () => {
  it('exposes title and reference through useErrorDialog', async () => {
    const push = vi.fn();
    const Sender = defineComponent({
      setup() {
        const notify = useErrorDialog();
        return { notify };
      },
      template: '<button id="send" @click="notify(\'Save failed\', { operationId: \'op-1\' })">Send</button>',
    });
    const wrapper = mount(Sender, {
      global: { provide: { [errorDialogKey as symbol]: { push, dismiss: vi.fn() } } },
    });
    wrappers.push(wrapper);

    await wrapper.get('#send').trigger('click');

    expect(push).toHaveBeenCalledWith({ title: 'Save failed', reference: { operationId: 'op-1' } });
  });

  it('fails fast when the host is not mounted', () => {
    const Sender = defineComponent({
      setup() {
        expect(() => useErrorDialog()).toThrow('ErrorDialogHost is missing.');
        return () => null;
      },
    });
    // Vue logs the missing injection before the guard throws; silence expected noise.
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    try {
      expect(() => mount(Sender)).not.toThrow();
    } finally {
      warn.mockRestore();
    }
  });

  it('renders one blocking error at a time and reveals the next after closing', async () => {
    const { api } = await mountHost();

    api.push({ title: 'Save failed' });
    await nextTick();
    await flushPromises();
    expect(dialogTitle()).toBe('Save failed');
    expect(document.querySelector('[data-testid="error-dialog"]')).not.toBeNull();

    api.push({ title: 'Delete failed' });
    await nextTick();
    await flushPromises();
    expect(dialogTitle()).toBe('Save failed');

    await closeCurrent();
    expect(dialogTitle()).toBe('Delete failed');

    await closeCurrent();
    expect(dialog()).toBeNull();
  });

  it('caps the pending queue at five dialogs', async () => {
    const { api } = await mountHost();
    for (let index = 1; index <= 6; index += 1) api.push({ title: `Error ${index}` });
    await nextTick();
    await flushPromises();

    expect(dialogTitle()).toBe('Error 1');
    await closeCurrent();
    for (let index = 2; index <= 5; index += 1) {
      expect(dialogTitle()).toBe(`Error ${index}`);
      await closeCurrent();
    }
    // The sixth payload exceeded the queue cap and was dropped.
    expect(dialog()).toBeNull();
  });

  it('links the current error to its correlated logs page', async () => {
    const { api } = await mountHost();

    api.push({ title: 'Deploy failed', reference: { eventId: 'evt-42' } });
    await nextTick();
    await flushPromises();

    const link = document.querySelector<HTMLAnchorElement>('[role="dialog"] a');
    expect(link?.textContent?.trim()).toBe('View logs');
    expect(link?.getAttribute('href')).toBe('/logs?eventId=evt-42&level=info%2Cwarning%2Cerror');

    link!.click();
    await flushPromises();
    expect(dialog()).toBeNull();
  });

  it('omits the logs link when the reference carries no correlation id', async () => {
    const { api } = await mountHost();

    api.push({ title: 'Save failed' });
    await nextTick();
    await flushPromises();

    expect(document.querySelector('[role="dialog"] a')).toBeNull();
    expect(dialog()?.textContent).toContain('Save failed');
    expect(dialog()?.querySelector('button[aria-label="Close"]')).not.toBeNull();
  });
});
