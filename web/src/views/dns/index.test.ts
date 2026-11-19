// @vitest-environment jsdom
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createPinia } from 'pinia';
import { createMemoryHistory, createRouter, type Router } from 'vue-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import DnsView from './index.vue';
import { dnsApi } from '@/api/dns';
import { errorDialogKey } from '@/components/ui/errorDialog';
import { toastKey } from '@/components/ui/toast';
import Table from '@/components/ui/Table.vue';
import { useI18n } from '@/i18n';
import type { DnsDomainDto } from '@/types/dns';

const domain: DnsDomainDto = {
  id: 'dom-1',
  name: 'example.com',
  provider: 'cloudflare',
  createdAt: '2026-01-01T00:00:00.000Z',
  updatedAt: '2026-01-01T00:00:00.000Z',
};

let wrapper: VueWrapper | undefined;
let router: Router;
const errorDialogPush = vi.fn();

function createStorage(): Storage {
  let store = new Map<string, string>();
  return {
    get length() {
      return store.size;
    },
    clear() {
      store = new Map();
    },
    getItem(key: string) {
      return store.has(key) ? store.get(key)! : null;
    },
    key(index: number) {
      return Array.from(store.keys())[index] ?? null;
    },
    removeItem(key: string) {
      store.delete(key);
    },
    setItem(key: string, value: string) {
      store.set(key, String(value));
    },
  };
}
vi.stubGlobal('localStorage', createStorage());

async function render(path: string) {
  router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/dns/domains', component: DnsView }] });
  await router.push(path);
  await router.isReady();
  wrapper = mount(DnsView, {
    global: {
      plugins: [createPinia(), router],
      provide: {
        [toastKey as symbol]: { push: vi.fn(), remove: vi.fn() },
        [errorDialogKey as symbol]: { push: errorDialogPush, dismiss: vi.fn() },
      },
    },
  });
  await flushPromises();
  return wrapper;
}

beforeEach(() => {
  useI18n().setLocale('en');
  errorDialogPush.mockClear();
  vi.spyOn(dnsApi, 'listDomainPage').mockResolvedValue({ items: [domain], total: 1, page: 1, pageSize: 50 });
  vi.spyOn(dnsApi, 'listRecords').mockResolvedValue({ items: [], stale: false, refreshing: false });
});

afterEach(() => {
  wrapper?.unmount();
  wrapper = undefined;
  document.body.innerHTML = '';
  vi.restoreAllMocks();
});

describe('dns detail migration', () => {
  it('renders the list toolbar through ListCommandBar', async () => {
    const view = await render('/dns/domains');
    const commandBar = view.find('[data-list-command-bar]');
    expect(commandBar.exists()).toBe(true);
    expect(commandBar.text()).toContain(useI18n().t('common.refresh'));
    expect(commandBar.text()).toContain(useI18n().t('common.add'));

    // 列表列精简：名称(+provider 副行) / 更新时间 / 操作；provider 不再单独成列。
    const headers = view.findAll('table thead th').map((item) => item.text());
    expect(headers).toHaveLength(3);
    expect(headers).toEqual([
      useI18n().t('common.name'),
      useI18n().t('dnsPage.updatedAt'),
      useI18n().t('common.actions'),
    ]);
    // 列表页没有面包屑。
    expect(view.find('header nav').exists()).toBe(false);

    // 次级时间列 <wide 隐藏；行内 Sync 在 <wide 收进 More（More 中保留）。
    const listColumns = (view.findComponent(Table).vm.$props as unknown as { columns: Array<{ key: string; hideBelow?: string }> }).columns;
    expect(listColumns.find((column) => column.key === 'updatedAt')?.hideBelow).toBe('wide');
    const updatedHeader = view.findAll('table thead th').find((item) => item.text() === useI18n().t('dnsPage.updatedAt'));
    expect(updatedHeader?.classes()).toContain('max-wide:hidden');
    expect(listColumns.length).toBe(3);
    expect(view.find('[data-list-command-bar] .flex-nowrap').exists()).toBe(true);
    const syncButton = view.findAll('button').find((item) => item.text() === useI18n().t('dnsPage.syncRecords'));
    expect(syncButton?.classes()).toContain('max-wide:hidden');

    const moreButton = view.findAll('button').find((item) => item.attributes('aria-label') === useI18n().t('common.moreActions'));
    expect(moreButton).toBeTruthy();
    await moreButton!.trigger('click');
    await flushPromises();
    expect(document.body.textContent).toContain(useI18n().t('dnsPage.syncRecords'));
    expect(document.body.textContent).toContain(useI18n().t('common.edit'));
  });

  it('keeps one records hint and renders the in-table empty state as inset', async () => {
    const { t } = useI18n();
    const view = await render('/dns/domains?domain=dom-1');
    await flushPromises();

    // PageHeader description 已删除，recordsHint 只在正文标题下出现一次。
    const headerDescriptions = view.findAll('header p').map((item) => item.text());
    expect(headerDescriptions).not.toContain(t('dnsPage.recordsHint'));
    expect(view.text().split(t('dnsPage.recordsHint')).length - 1).toBe(1);

    const empty = view.findAll('*').find((item) => item.text() === t('dnsPage.noRecords'));
    expect(empty).toBeTruthy();
    // inset 空态有内边距但不再自带卡片描边。
    const emptyRoot = empty!.element.closest('.min-h-32');
    expect(emptyRoot).not.toBeNull();
    expect(emptyRoot!.classList.contains('rounded-overlay')).toBe(false);

    expect(view.find('[data-settings-card]').exists()).toBe(false);

    // 记录表单 TTL 使用 NumberBox（spinbutton + min/max）。
    const addRecord = view.findAll('button').find((item) => item.text() === t('dnsPage.addRecord'));
    expect(addRecord).toBeTruthy();
    await addRecord!.trigger('click');
    await flushPromises();
    const ttl = document.querySelector('[role="dialog"] [role="spinbutton"]');
    expect(ttl).not.toBeNull();
    expect(ttl!.getAttribute('aria-valuemin')).toBe('1');
    expect(ttl!.getAttribute('aria-valuemax')).toBe('86400');
    expect(document.querySelector('[role="dialog"] .flex-col-reverse')).not.toBeNull();

    // 详情面包屑：DNS 列表链接 + 当前域名 aria-current。
    const breadcrumb = view.find('header nav');
    expect(breadcrumb.exists()).toBe(true);
    const listLink = breadcrumb.find('a');
    expect(listLink.text()).toBe(t('routes.dns.title'));
    expect(listLink.attributes('href')).toBe('/dns/domains');
    expect(breadcrumb.find('[aria-current="page"]').text()).toBe('example.com');
  });

  it('keeps a domain load failure inline with a retry and never raises a dialog', async () => {
    vi.spyOn(dnsApi, 'listDomainPage').mockRejectedValue(new Error('dns down'));
    const view = await render('/dns/domains');

    expect(view.text()).toContain(useI18n().t('common.loadFailed'));
    expect(view.text()).toContain('dns down');
    expect(errorDialogPush).not.toHaveBeenCalled();
  });

  it('opens the blocking error dialog when an explicit DNS refresh fails', async () => {
    vi.spyOn(dnsApi, 'refreshRecords').mockRejectedValue(new Error('sync down'));
    const view = await render('/dns/domains');

    const syncButton = view.findAll('button').find((item) => item.text() === useI18n().t('dnsPage.syncRecords'))!;
    await syncButton.trigger('click');
    await flushPromises();

    expect(errorDialogPush).toHaveBeenCalledWith(expect.objectContaining({ title: 'sync down' }));
  });
});
