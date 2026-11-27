// @vitest-environment jsdom
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { defineComponent, nextTick } from 'vue';
import { createMemoryHistory, createRouter, type Router } from 'vue-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useI18n } from '@/i18n';

// App.vue 挂载时会加载服务端外观；本用例只关心根组件的 provide 树。
vi.mock('@/design/appearance', () => ({ bootstrapAppearance: vi.fn() }));

import App from './App.vue';
import { useErrorDialog } from '@/components/ui/errorDialog';

const wrappers: VueWrapper[] = [];

/**
 * 真实布局里的每个页面都通过 `useErrorDialog()` 报阻塞错误（AppShell 自己在
 * setup 里就调用）。这里用同样的方式在“路由视图”内调用，验证 App.vue 的
 * ErrorDialogHost 确实位于路由视图之上，而不是它的兄弟节点。
 */
const RoutedView = defineComponent({
  setup() {
    const notify = useErrorDialog();
    return { notify };
  },
  template: '<button id="fail" @click="notify(\'Save failed\', { operationId: \'op-1\' })">fail</button>',
});

function makeRouter(): Router {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: RoutedView },
      // 错误弹窗的「查看日志」深链要能解析，避免测试里出现 "No match" 警告。
      { path: '/events', component: { template: '<div />' } },
    ],
  });
}

beforeEach(() => {
  useI18n().setLocale('en');
});

afterEach(() => {
  while (wrappers.length) wrappers.pop()!.unmount();
  document.body.innerHTML = '';
  document.body.style.overflow = '';
  useI18n().setLocale('zh-CN');
});

describe('app root', () => {
  it('provides the global error dialog to routed views', async () => {
    const router = makeRouter();
    await router.push('/');
    await router.isReady();

    const wrapper = mount(App, { attachTo: document.body, global: { plugins: [router] } });
    wrappers.push(wrapper);
    await flushPromises();

    await wrapper.get('#fail').trigger('click');
    await nextTick();
    await flushPromises();

    const dialog = document.querySelector<HTMLElement>('[role="dialog"]');
    expect(dialog).not.toBeNull();
    expect(dialog?.textContent).toContain('Save failed');
  });
});
