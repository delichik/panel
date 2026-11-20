// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { createMemoryHistory, createRouter } from 'vue-router';
import { describe, expect, it } from 'vitest';
import ConsolePage from './ConsolePage.vue';
import EditorPage from './EditorPage.vue';
import WorkspacePage from './WorkspacePage.vue';
import PageHeader from '@/components/shell/PageHeader.vue';
import RoutePageFallback from '@/components/shell/RoutePageFallback.vue';

function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: { template: '<div />' } }],
  });
}

/**
 * WinUI page geometry: ConsolePage is the single page-padding owner. PageHeader
 * and the templates composed inside a page must not add another padding layer,
 * and the lazy-page skeleton must reserve the same content column width as the
 * page it replaces.
 */
describe('page padding ownership', () => {
  it('ConsolePage owns the page padding (desktop 16 / narrow 12)', () => {
    const wrapper = mount(ConsolePage, {
      props: { title: 'Servers' },
      slots: { default: '<div data-body>body</div>' },
    });
    expect(wrapper.classes()).toContain('p-4');
    expect(wrapper.classes()).toContain('max-lg:p-3');
    const body = wrapper.element.children[1] as HTMLElement;
    expect(body.className).not.toContain('p-4');
  });

  it('PageHeader is a padding-free structural block', () => {
    const wrapper = mount(PageHeader, {
      props: { title: 'Diagnostics' },
      slots: { actions: '<button type="button">Refresh</button>' },
    });
    const header = wrapper.get('header');
    for (const padding of ['p-4', 'px-4', 'pt-4', 'pb-3', 'p-6']) {
      expect(header.classes(), `PageHeader must not own ${padding}`).not.toContain(padding);
    }
    // Layout and action wrapping stay in the shell contract.
    expect(header.classes()).toContain('max-lg:flex-col');
    expect(header.classes()).toContain('max-wide:flex-col');
    const actions = wrapper.get('header > div:last-child');
    expect(actions.classes()).toContain('flex-wrap');
    expect(actions.classes()).toContain('max-wide:w-full');
  });

  it('places the back entry above the title so the h1 keeps the body left edge', () => {
    const wrapper = mount(PageHeader, {
      props: { title: 'Server detail', description: 'web-01', backLabel: 'Back to list' },
    });
    const back = wrapper.get('header button[aria-label="Back to list"]');
    const title = wrapper.get('header h1');
    // The back control sits in its own row above the title, not in front of it.
    const backRow = back.element.parentElement as HTMLElement;
    expect(backRow.contains(title.element)).toBe(false);
    expect(backRow.className).toContain('mb-2');
    expect(title.element.previousElementSibling).toBe(backRow);
    // Description/title are in the same text column.
    expect(title.element.parentElement?.contains(wrapper.get('header p').element)).toBe(true);
  });

  it('renders the breadcrumb row without page padding and above the h1', () => {
    const wrapper = mount(PageHeader, {
      props: {
        title: 'Servers',
        breadcrumbs: [{ label: 'Assets' }, { label: 'Servers' }],
      },
    });
    const header = wrapper.get('header');
    for (const padding of ['p-4', 'px-4', 'pt-4', 'pb-3']) {
      expect(header.classes(), `PageHeader must not own ${padding}`).not.toContain(padding);
    }
    const nav = wrapper.get('nav[aria-label="Breadcrumb"]');
    const row = nav.element.parentElement as HTMLElement;
    expect(row.className).toContain('mb-2');
    expect(wrapper.get('header h1').element.previousElementSibling).toBe(row);
    for (const padding of ['p-4', 'px-4', 'pt-4', 'pb-3']) {
      expect(row.className, `breadcrumb row must not own ${padding}`).not.toContain(padding);
    }
  });

  it('EditorPage footer is a full-bleed command bar with vertical spacing only', () => {
    const wrapper = mount(EditorPage, {
      slots: { default: '<div data-body>editor</div>', footer: '<div data-footer>footer</div>' },
    });
    const [body, footer] = Array.from(wrapper.element.children) as HTMLElement[];
    expect(body!.className).not.toContain('p-6');
    expect(body!.className).not.toContain('p-4');
    expect(body!.className).toContain('overflow-auto');
    expect(body!.className).toContain('max-lg:overflow-visible');
    expect(footer!.className).toContain('border-t');
    expect(footer!.className).toContain('py-3');
    for (const padding of ['p-4', 'px-4', 'pb-4', 'pt-4']) {
      expect(footer!.className, `footer must not own page ${padding}`).not.toContain(padding);
    }
  });

  it('WorkspacePage releases viewport clipping below the desktop breakpoint', () => {
    const wrapper = mount(WorkspacePage, {
      slots: { toolbar: '<div data-toolbar>window</div>', default: '<div data-body>terminal</div>' },
    });
    const [toolbar, body] = Array.from(wrapper.element.children) as HTMLElement[];
    expect(toolbar!.className).not.toContain('p-4');
    expect(toolbar!.className).toContain('border-b');
    expect(body!.className).not.toContain('p-4');
    expect(body!.className).toContain('overflow-auto');
    expect(body!.className).toContain('max-lg:overflow-visible');
    expect(wrapper.classes()).toContain('max-lg:h-auto');
    expect(wrapper.classes()).toContain('max-lg:grid-rows-none');
  });

  it('RoutePageFallback reserves the same content column and page padding', () => {
    const wrapper = mount(RoutePageFallback, { global: { plugins: [makeRouter()] } });

    expect(wrapper.classes()).toContain('max-w-content');
    expect(wrapper.classes()).toContain('p-4');
    expect(wrapper.classes()).toContain('max-lg:p-3');
    expect(wrapper.classes()).toContain('mx-auto');
    expect(wrapper.attributes('aria-busy')).toBe('true');
  });
});
