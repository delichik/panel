// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { createPinia, setActivePinia } from 'pinia';
import { describe, expect, it } from 'vitest';
import { nextTick } from 'vue';
import { vPermission } from './permission';
import { useSessionStore } from '@/stores/session';

describe('v-permission', () => {
  it('hides elements without the permission and keeps granted ones', async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const session = useSessionStore();
    session.permissions = ['servers.view'];

    const wrapper = mount({
      template: `
        <div>
          <button id="manage" v-permission="'servers.manage'">manage</button>
          <button id="view" v-permission="'servers.view'">view</button>
        </div>
      `,
    }, {
      global: { plugins: [pinia], directives: { permission: vPermission } },
    });

    expect(wrapper.get('#manage').attributes('hidden')).toBeDefined();
    expect(wrapper.get('#view').attributes('hidden')).toBeUndefined();

    session.permissions = ['servers.view', 'servers.manage'];
    wrapper.vm.$forceUpdate();
    await nextTick();
    expect(wrapper.get('#manage').attributes('hidden')).toBeUndefined();
  });
});
