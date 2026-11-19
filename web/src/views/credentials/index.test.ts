// @vitest-environment jsdom
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createPinia } from 'pinia';
import { createMemoryHistory, createRouter, type Router } from 'vue-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import CredentialsView from './index.vue';
import { credentialsApi } from '@/api/credentials';
import { serversApi } from '@/api/servers';
import { errorDialogKey } from '@/components/ui/errorDialog';
import { toastKey } from '@/components/ui/toast';
import Table from '@/components/ui/Table.vue';
import { useI18n } from '@/i18n';
import type { CredentialDetailDto, CredentialDto } from '@/types/credentials';
import type { ServerDto } from '@/types/servers';

const credential: CredentialDto = {
  id: 'cred-1',
  name: 'deploy-key',
  type: 'password',
  username: 'root',
  updatedAt: '2026-01-01T00:00:00.000Z',
};

const detail: CredentialDetailDto = {
  ...credential,
  type: 'private_key',
  keySummary: { algorithm: 'ED25519', bits: 256, fingerprint: 'SHA256:abc', comment: 'deploy-key' },
};

function server(id: string, name: string, host: string): ServerDto {
  return { id, name, host, credentialId: credential.id } as unknown as ServerDto;
}

let wrapper: VueWrapper | undefined;
let router: Router;
const errorDialogPush = vi.fn();

// jsdom in this vitest environment has no localStorage; stub it like the other page tests.
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

async function render(path = '/credentials') {
  router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/credentials', component: CredentialsView }] });
  await router.push(path);
  await router.isReady();
  wrapper = mount(CredentialsView, {
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
  vi.spyOn(credentialsApi, 'listPage').mockResolvedValue({ items: [credential], total: 1, page: 1, pageSize: 50 });
  vi.spyOn(credentialsApi, 'get').mockResolvedValue(detail);
  vi.spyOn(serversApi, 'list').mockResolvedValue([
    server('srv-1', 'edge-1', '10.0.0.1'),
    server('srv-2', 'edge-2', '10.0.0.2'),
  ]);
});

afterEach(() => {
  wrapper?.unmount();
  wrapper = undefined;
  document.body.innerHTML = '';
  vi.restoreAllMocks();
});

describe('credentials detail migration', () => {
  it('renders the list toolbar through ListCommandBar with search and primary action', async () => {
    const view = await render();
    const commandBar = view.find('[data-list-command-bar]');
    expect(commandBar.exists()).toBe(true);
    expect(commandBar.text()).toContain(useI18n().t('common.refresh'));
    expect(commandBar.text()).toContain(useI18n().t('common.add'));

    // 列表列精简为 5 列：名称(+类型副行) / 摘要 / 引用数 / 更新时间 / 操作。
    const headers = view.findAll('table thead th').map((item) => item.text());
    expect(headers).toHaveLength(5);
    expect(headers).toEqual([
      useI18n().t('common.name'),
      useI18n().t('credentialsPage.username'),
      useI18n().t('credentialsPage.references'),
      useI18n().t('credentialsPage.updatedAtColumn'),
      useI18n().t('common.actions'),
    ]);
    // 列表页没有面包屑。
    expect(view.find('header nav').exists()).toBe(false);

    // 次级列按优先级隐藏；行内 Test 在 <wide 收进 More（More 中保留）。
    const listColumns = (view.findComponent(Table).vm.$props as unknown as { columns: Array<{ key: string; hideBelow?: string }> }).columns;
    expect(listColumns.find((column) => column.key === 'summary')?.hideBelow).toBe('wide');
    expect(listColumns.find((column) => column.key === 'references')?.hideBelow).toBe('lg');
    expect(listColumns.find((column) => column.key === 'updatedAt')?.hideBelow).toBe('wide');
    const summaryHeader = view.findAll('table thead th').find((item) => item.text() === useI18n().t('credentialsPage.username'));
    const referencesHeader = view.findAll('table thead th').find((item) => item.text() === useI18n().t('credentialsPage.references'));
    const updatedHeader = view.findAll('table thead th').find((item) => item.text() === useI18n().t('credentialsPage.updatedAtColumn'));
    expect(summaryHeader?.classes()).toContain('max-wide:hidden');
    expect(referencesHeader?.classes()).toContain('max-lg:hidden');
    expect(updatedHeader?.classes()).toContain('max-wide:hidden');
    // <sm 时只保留名称+操作（摘要/引用/时间都已隐藏）。
    expect(view.find('[data-list-command-bar] .flex-nowrap').exists()).toBe(true);
    const testButton = view.findAll('button').find((item) => item.text() === useI18n().t('credentialsPage.test'));
    expect(testButton?.classes()).toContain('max-wide:hidden');

    const moreButton = view.findAll('button').find((item) => item.attributes('aria-label') === useI18n().t('common.moreActions'));
    expect(moreButton).toBeTruthy();
    await moreButton!.trigger('click');
    await flushPromises();
    expect(document.body.textContent).toContain(useI18n().t('credentialsPage.test'));
    expect(document.body.textContent).toContain(useI18n().t('common.edit'));
  });

  it('renders key summary and server references as DetailSection + PropertyList rows', async () => {
    const view = await render('/credentials?credential=cred-1');
    await flushPromises();

    expect(view.findAll('[data-detail-section]')).toHaveLength(2);
    expect(view.find('[data-property-list]').exists()).toBe(true);
    // 3 个密钥摘要属性 + 2 个服务器引用行；不再一属性一描边小卡。
    expect(view.findAll('[data-property-row]')).toHaveLength(5);
    expect(view.find('.surface-card .surface-card').exists()).toBe(false);

    // 详情面包屑：凭据列表链接 + 当前凭据 aria-current。
    const breadcrumb = view.find('header nav');
    expect(breadcrumb.exists()).toBe(true);
    const listLink = breadcrumb.find('a');
    expect(listLink.text()).toBe(useI18n().t('routes.credentials.title'));
    expect(listLink.attributes('href')).toBe('/credentials');
    expect(breadcrumb.find('[aria-current="page"]').text()).toBe('deploy-key');
    // 详情属性列表在移动端单列（共享 PropertyList 的 grid-cols-1 基础类）。
    expect(view.findAll('[data-property-list]').every((item) => item.classes().includes('grid-cols-1'))).toBe(true);
  });

  it('offers semantic radio targets for credential testing and supports arrow keys', async () => {
    const view = await render();

    const testButton = view.findAll('button').find((item) => item.text() === useI18n().t('credentialsPage.test'));
    expect(testButton).toBeTruthy();
    await testButton!.trigger('click');
    await flushPromises();

    const dialog = document.querySelector('[role="dialog"]');
    expect(dialog).not.toBeNull();
    const group = dialog!.querySelector('[role="radiogroup"]');
    expect(group).not.toBeNull();
    const radios = Array.from(group!.querySelectorAll<HTMLButtonElement>('[role="radio"]'));
    expect(radios).toHaveLength(2);
    expect(document.querySelector('[role="dialog"] .flex-col-reverse')).not.toBeNull();
    expect(radios[0]!.getAttribute('aria-checked')).toBe('true');
    expect(radios[0]!.closest('.bg-accent-selected')).not.toBeNull();
    expect(radios[0]!.classList.contains('border-accent-text')).toBe(false);
    expect(radios[0]!.closest('.border-accent-text')).toBeNull();

    radios[0]!.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
    await flushPromises();

    expect(radios[1]!.getAttribute('aria-checked')).toBe('true');
    expect(radios[1]!.closest('.bg-accent-selected')).not.toBeNull();
    expect(radios[0]!.getAttribute('aria-checked')).toBe('false');
  });

  it('keeps a credentials list load failure inline and never raises a dialog', async () => {
    vi.mocked(credentialsApi.listPage).mockRejectedValue(new Error('credentials down'));
    const view = await render();

    expect(view.text()).toContain(useI18n().t('common.loadFailed'));
    expect(view.text()).toContain('credentials down');
    expect(errorDialogPush).not.toHaveBeenCalled();
  });

  it('opens the blocking error dialog when the explicit connection test fails', async () => {
    vi.spyOn(serversApi, 'test').mockRejectedValue(new Error('ssh down'));
    const view = await render();

    const testButton = view.findAll('button').find((item) => item.text() === useI18n().t('credentialsPage.test'))!;
    await testButton.trigger('click');
    await flushPromises();
    const confirm = Array.from(document.querySelectorAll<HTMLButtonElement>('[role="dialog"] button'))
      .find((item) => item.textContent?.trim() === useI18n().t('credentialsPage.test'))!;
    confirm.click();
    await flushPromises();

    expect(errorDialogPush).toHaveBeenCalledWith(expect.objectContaining({ title: 'ssh down' }));
  });
});
