// @vitest-environment jsdom
import { mount, flushPromises } from '@vue/test-utils';
import { defineComponent } from 'vue';
import { createRouter, createMemoryHistory } from 'vue-router';
import { afterEach, describe, expect, it, vi } from 'vitest';
import ToastProvider from './ToastProvider.vue';
import { useSuccessToast, useToast } from './toast';
import { useI18n } from '@/i18n';

afterEach(() => { document.body.innerHTML = ''; vi.useRealTimers(); });

async function mountToastHost() {
  useI18n().setLocale('en');
  let api!: ReturnType<typeof useToast>;
  const Sender = defineComponent({
    setup() {
      const success = useSuccessToast();
      // useToast is the infrastructure entry point (event notifications / shared
      // providers only); business code must use useSuccessToast or useErrorDialog.
      const toast = useToast();
      api = toast;
      return {
        send() {
          success('Accepted', { operationId: 'op-one' });
          toast.push({ title: 'Background sync', tone: 'info' });
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
    ],
  });
  await router.push('/');
  const wrapper = mount(Host, { attachTo: document.body, global: { plugins: [router] } });
  return { wrapper, router, toast: api };
}

function visibleBubbles() {
  return [...document.querySelectorAll<HTMLElement>('section[role="status"], section[role="alert"]')];
}

describe('toast process actions', () => {
  it('keeps the success action correlated to its own operation and navigates to the selected process', async () => {
    const { wrapper, router } = await mountToastHost();
    await wrapper.get('#send').trigger('click');
    const links = [...document.querySelectorAll<HTMLAnchorElement>('a')];
    expect(links.map((link) => link.getAttribute('href'))).toEqual(['/events?operationId=op-one']);
    links[0]!.click();
    await flushPromises();
    expect(router.currentRoute.value.query.operationId).toBe('op-one');
    wrapper.unmount();
  });

  it('uses the WinUI InfoBar visual (4px, semantic fill, no flyout shadow) in compact form', async () => {
    const { wrapper } = await mountToastHost();
    await wrapper.get('#send').trigger('click');
    const bubbles = visibleBubbles();
    expect(bubbles.length).toBe(2);
    expect(bubbles.map((bubble) => bubble.dataset.tone)).toEqual(['success', 'info']);
    for (const bubble of bubbles) {
      expect(bubble.className).toContain('rounded-control');
      expect(bubble.className).toContain('text-caption');
      expect(bubble.className).not.toContain('rounded-overlay');
      expect(bubble.className).not.toContain('shadow-flyout');
      expect(bubble.className).not.toContain('bg-popover');
      // 语义 InfoBar 底色由 tone 决定（不再用不透明 popover + 语义边框的混合体）。
      expect(bubble.className).toMatch(/bg-(success|warning|danger|info)-bg/);
      expect(bubble.dataset.tone).toBeTruthy();
    }
    wrapper.unmount();
  });

  it('replaces an existing toast in place when pushed with the same key', async () => {
    const { wrapper, toast } = await mountToastHost();
    toast.push({ key: 'evt-1', title: 'Deploy app', description: 'Running', tone: 'info' });
    await flushPromises();
    expect(visibleBubbles()).toHaveLength(1);
    expect(visibleBubbles()[0]?.dataset.tone).toBe('info');
    expect(visibleBubbles()[0]?.textContent).toContain('Running');

    toast.push({ key: 'evt-1', title: 'Deploy app', description: 'Succeeded', tone: 'success' });
    await flushPromises();
    expect(visibleBubbles()).toHaveLength(1);
    expect(visibleBubbles()[0]?.dataset.tone).toBe('success');
    expect(visibleBubbles()[0]?.textContent).toContain('Succeeded');
    wrapper.unmount();
  });

  it('caps the visible stack at three toasts by dropping the oldest', async () => {
    const { wrapper, toast } = await mountToastHost();
    toast.push({ title: 'First' });
    toast.push({ title: 'Second' });
    toast.push({ title: 'Third' });
    toast.push({ title: 'Fourth' });
    await flushPromises();

    const titles = visibleBubbles().map((bubble) => bubble.querySelector('strong')?.textContent);
    expect(titles).toEqual(['Second', 'Third', 'Fourth']);
    wrapper.unmount();
  });

  it('pauses auto-dismiss on hover and resumes with the remaining duration', async () => {
    vi.useFakeTimers();
    const { wrapper, toast } = await mountToastHost();
    toast.push({ title: 'Timed toast', tone: 'info' });
    await flushPromises();

    vi.advanceTimersByTime(2000);
    const bubble = visibleBubbles()[0]!;
    bubble.dispatchEvent(new MouseEvent('mouseenter'));
    vi.advanceTimersByTime(5000);
    await flushPromises();
    expect(visibleBubbles()).toHaveLength(1);

    bubble.dispatchEvent(new MouseEvent('mouseleave'));
    vi.advanceTimersByTime(1400);
    await flushPromises();
    expect(visibleBubbles()).toHaveLength(1);

    vi.advanceTimersByTime(200);
    await flushPromises();
    expect(visibleBubbles()).toHaveLength(0);
    wrapper.unmount();
  });
});
