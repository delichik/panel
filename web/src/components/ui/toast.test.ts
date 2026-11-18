// @vitest-environment jsdom
import { mount, flushPromises } from '@vue/test-utils';
import { defineComponent } from 'vue';
import { createRouter, createMemoryHistory } from 'vue-router';
import { afterEach, describe, expect, it, vi } from 'vitest';
import ToastProvider from './ToastProvider.vue';
import { useErrorToast, useSuccessToast } from './toast';
import { ApiError } from '@/api/client';
import { useI18n } from '@/i18n';

afterEach(() => { document.body.innerHTML = ''; vi.useRealTimers(); });

async function mountToastHost() {
  useI18n().setLocale('en');
  const Sender = defineComponent({
    setup() {
      const success = useSuccessToast();
      const error = useErrorToast();
      return {
        send() {
          success('Accepted', { operationId: 'op-one' });
          error('Remote failure', new ApiError('failed', 409, 'failed', { taskId: 'task-two' }));
        },
      };
    },
    template: '<button id="send" @click="send">Send</button>',
  });
  const Host = defineComponent({
    components: { ToastProvider, Sender },
    template: '<ToastProvider><Sender /></ToastProvider>',
  });
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: Sender },
      { path: '/events', component: { template: '<div />' } },
      { path: '/logs', component: { template: '<div />' } },
    ],
  });
  await router.push('/');
  const wrapper = mount(Host, { attachTo: document.body, global: { plugins: [router] } });
  return { wrapper, router };
}

describe('toast process actions', () => {
  it('keeps success and failure correlated to their own operation and navigates to the selected process', async () => {
    vi.useFakeTimers();
    const { wrapper, router } = await mountToastHost();
    await wrapper.get('#send').trigger('click');
    const links = [...document.querySelectorAll<HTMLAnchorElement>('a')];
    expect(links.map((link) => link.getAttribute('href'))).toEqual([
      '/events?operationId=op-one',
      '/events?executionId=task-two',
    ]);
    await vi.advanceTimersByTimeAsync(4500);
    expect(document.querySelectorAll('a')).toHaveLength(2);
    links[1]!.click();
    await flushPromises();
    expect(router.currentRoute.value.query.executionId).toBe('task-two');
    wrapper.unmount();
  });

  it('uses the WinUI InfoBar visual (4px, semantic fill, no flyout shadow)', async () => {
    const { wrapper } = await mountToastHost();
    await wrapper.get('#send').trigger('click');
    const bubbles = [...document.querySelectorAll<HTMLElement>('section[role="status"], section[role="alert"]')];
    expect(bubbles.length).toBeGreaterThan(0);
    for (const bubble of bubbles) {
      expect(bubble.className).toContain('rounded-control');
      expect(bubble.className).not.toContain('rounded-overlay');
      expect(bubble.className).not.toContain('shadow-flyout');
      expect(bubble.className).not.toContain('bg-popover');
      // 语义 InfoBar 底色由 tone 决定（不再用不透明 popover + 语义边框的混合体）。
      expect(bubble.className).toMatch(/bg-(success|warning|danger|info)-bg/);
      expect(bubble.dataset.tone).toBeTruthy();
    }
    wrapper.unmount();
  });
});
