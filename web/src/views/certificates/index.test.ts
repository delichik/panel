// @vitest-environment jsdom
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createPinia } from 'pinia';
import { createMemoryHistory, createRouter, type Router } from 'vue-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import CertificatesView from './index.vue';
import { certificatesApi } from '@/api/certificates';
import { dnsApi } from '@/api/dns';
import { keyAssetsApi } from '@/api/keyAssets';
import { errorDialogKey } from '@/components/ui/errorDialog';
import { toastKey } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { DomainCertificateDto } from '@/types/certificates';

const certificate: DomainCertificateDto = {
  id: 'cert-1',
  name: 'example-cert',
  domainId: 'domain-1',
  domain: 'example.com',
  prefix: 'example.com',
  scope: 'single',
  domains: ['example.com', 'www.example.com'],
  issuer: 'Let us Encrypt R3',
  status: 'issued',
  autoRenew: true,
  notBefore: '2026-01-01T00:00:00.000Z',
  notAfter: '2026-04-01T00:00:00.000Z',
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
  router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/certificates/domains', component: CertificatesView },
      { path: '/certificates/self-signed', component: CertificatesView },
      { path: '/certificates/keys', component: CertificatesView },
    ],
  });
  await router.push(path);
  await router.isReady();
  wrapper = mount(CertificatesView, {
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
  vi.spyOn(dnsApi, 'listDomains').mockResolvedValue([]);
  vi.spyOn(certificatesApi, 'list').mockResolvedValue({ items: [certificate], total: 1, page: 1, pageSize: 50 });
  vi.spyOn(certificatesApi, 'listSelfSignedPage').mockResolvedValue({ items: [], total: 0, page: 1, pageSize: 50 });
  vi.spyOn(keyAssetsApi, 'listPage').mockResolvedValue({ items: [], total: 0, page: 1, pageSize: 50 });
});

afterEach(() => {
  wrapper?.unmount();
  wrapper = undefined;
  document.body.innerHTML = '';
  vi.restoreAllMocks();
});

describe('certificates detail migration', () => {
  it('renders domain, status/renewal and issuer as DetailSection + PropertyList rows', async () => {
    const view = await render('/certificates/domains?selected=cert-1');
    await flushPromises();

    const section = view.find('[data-detail-section]');
    expect(section.exists()).toBe(true);
    const rows = view.findAll('[data-property-row]');
    expect(rows).toHaveLength(4);
    expect(view.text()).toContain(useI18n().t('certificatesPage.issuer'));
    expect(view.text()).toContain('Let us Encrypt R3');
    expect(view.text()).toContain('www.example.com');

    // PageHeader description 只保留域名身份，issuer 不再出现在页头。
    const headerDescriptions = view.findAll('header p');
    const headerDescription = headerDescriptions[headerDescriptions.length - 1]!;
    expect(headerDescription.exists()).toBe(true);
    expect(headerDescription.text()).toBe('example.com');
    expect(headerDescription.text()).not.toContain('Let us Encrypt R3');

    // 旧 surface-panel / border-b 分节已移除，且没有卡内第二层卡。
    expect(view.find('.surface-panel').exists()).toBe(false);
    expect(view.find('.surface-card .surface-card').exists()).toBe(false);

    // 列表列精简：名称 / 状态 / 到期 / 操作（coverage/issuer 进名称副行与详情）。
    const headers = view.findAll('table thead th').map((item) => item.text());
    expect(headers).toHaveLength(4);
    expect(headers).toEqual([
      useI18n().t('common.name'),
      useI18n().t('common.status'),
      useI18n().t('certificatesPage.expiresAt'),
      useI18n().t('common.actions'),
    ]);

    // 二级列在 <wide 隐藏；行内续签在 <wide 只保留详情+更多。
    const listColumns = (view.findComponent({ name: 'Table' }).vm.$props as unknown as { columns: Array<{ key: string; hideBelow?: string }> }).columns;
    expect(listColumns.find((column) => column.key === 'expiresAt')?.hideBelow).toBe('wide');
    expect(listColumns.some((column) => column.key === 'actions')).toBe(true);
    const expiresHeader = view.findAll('table thead th').find((item) => item.text() === useI18n().t('certificatesPage.expiresAt'));
    expect(expiresHeader?.classes()).toContain('max-wide:hidden');
    const statusHeader = view.findAll('table thead th').find((item) => item.text() === useI18n().t('common.status'));
    expect(statusHeader?.classes()).toContain('max-sm:hidden');
    const actionsHeader = view.findAll('table thead th').find((item) => item.text() === useI18n().t('common.actions'));
    expect(actionsHeader?.classes()).toContain('text-right');
    // 命令栏操作行在移动端不换行（横向可滚）。
    expect(view.find('[data-list-command-bar] .flex-nowrap').exists()).toBe(true);
    const renewButton = view.findAll('button').find((item) => item.text() === useI18n().t('certificatesPage.renew'));
    expect(renewButton?.classes()).toContain('max-wide:hidden');

    // 详情面包屑：列表标题链接 + 当前对象 aria-current；列表页不渲染面包屑。
    const breadcrumb = view.find('header nav');
    expect(breadcrumb.exists()).toBe(true);
    const listLink = breadcrumb.find('a');
    expect(listLink.text()).toBe(useI18n().t('routes.certificates.title'));
    expect(listLink.attributes('href')).toBe('/certificates/domains');
    expect(breadcrumb.find('[aria-current="page"]').text()).toBe('example-cert');
  });

  it('uses ListCommandBar and ListPage state slots for toolbar and first-load state', async () => {
    let resolveList: ((value: { items: typeof certificate[]; total: number; page: number; pageSize: number }) => void) | undefined;
    vi.mocked(certificatesApi.list).mockReturnValueOnce(new Promise((resolve) => { resolveList = resolve; }));

    const view = await render('/certificates/domains');
    const commandBar = view.find('[data-list-command-bar]');
    expect(commandBar.exists()).toBe(true);
    expect(commandBar.text()).toContain(useI18n().t('common.refresh'));
    expect(commandBar.text()).toContain(useI18n().t('certificatesPage.issue'));
    // 首屏 loading 走 ListPage #loading 槽内的真实 Table 骨架（保留固定表头，仅行数据占位）。
    expect(view.find('[aria-busy="true"]').exists()).toBe(true);
    expect(view.findAll('table thead th').map((cell) => cell.text())).toEqual([
      useI18n().t('common.name'),
      useI18n().t('common.status'),
      useI18n().t('certificatesPage.expiresAt'),
      useI18n().t('common.actions'),
    ]);

    resolveList?.({ items: [certificate], total: 1, page: 1, pageSize: 50 });
    await flushPromises();
    expect(view.find('[aria-busy="true"]').exists()).toBe(false);
    expect(view.find('[data-list-command-bar]').exists()).toBe(true);
  });

  it('uses NumberBox for numeric form fields', async () => {
    const { t } = useI18n();
    const view = await render('/certificates/self-signed');

    const generateCa = view.findAll('button').find((item) => item.text() === t('certificatesPage.generateCa'));
    expect(generateCa).toBeTruthy();
    await generateCa!.trigger('click');
    await flushPromises();

    const spinbutton = document.querySelector('[role="dialog"] [role="spinbutton"]');
    expect(spinbutton).not.toBeNull();
    expect(spinbutton!.getAttribute('aria-valuemin')).toBe('1');
    // Dialog footer 在移动端纵向堆叠（full width 容器）。
    expect(document.querySelector('[role="dialog"] .flex-col-reverse')).not.toBeNull();
  });

  it('lazy-loads each certificate tab and reuses the cache when switching back', async () => {
    const { t } = useI18n();
    const view = await render('/certificates/domains');

    // 首屏只加载 domains：DNS 域名 + 域名证书；self/keys 列表不请求。
    expect(dnsApi.listDomains).toHaveBeenCalledTimes(1);
    expect(certificatesApi.list).toHaveBeenCalledTimes(1);
    expect(certificatesApi.listSelfSignedPage).toHaveBeenCalledTimes(0);
    expect(keyAssetsApi.listPage).toHaveBeenCalledTimes(0);

    const selfTab = view.findAll('[role="tab"]').find((item) => item.text() === t('routes.selfSigned.title'))!;
    await selfTab.trigger('click');
    await flushPromises();
    expect(certificatesApi.listSelfSignedPage).toHaveBeenCalledTimes(1);
    expect(certificatesApi.list).toHaveBeenCalledTimes(1);

    // 切回 domains：命中缓存，不再重取列表。
    const domainsTab = view.findAll('[role="tab"]').find((item) => item.text() === t('routes.certificates.title'))!;
    await domainsTab.trigger('click');
    await flushPromises();
    expect(certificatesApi.list).toHaveBeenCalledTimes(1);
    expect(dnsApi.listDomains).toHaveBeenCalledTimes(1);
    expect(certificatesApi.listSelfSignedPage).toHaveBeenCalledTimes(1);
  });

  it('keeps a certificate list load failure inline and never raises a dialog', async () => {
    vi.spyOn(certificatesApi, 'list').mockRejectedValue(new Error('certs down'));
    const view = await render('/certificates/domains');

    expect(view.text()).toContain(useI18n().t('common.loadFailed'));
    expect(view.text()).toContain('certs down');
    expect(errorDialogPush).not.toHaveBeenCalled();
  });

  it('opens the blocking error dialog when an explicit certificate renewal fails', async () => {
    vi.spyOn(certificatesApi, 'renew').mockRejectedValue(new Error('renew down'));
    const view = await render('/certificates/domains');

    const renewButton = view.findAll('button').find((item) => item.text() === useI18n().t('certificatesPage.renew'))!;
    await renewButton.trigger('click');
    await flushPromises();

    expect(errorDialogPush).toHaveBeenCalledWith(expect.objectContaining({ title: 'renew down' }));
  });
});
