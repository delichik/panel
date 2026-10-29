// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import PaginationBar from './PaginationBar.vue';

describe('cursor pagination', () => {
  it('uses availability rather than fabricated totals and does not emit page changes', async () => {
    const wrapper = mount(PaginationBar, { props: { mode: 'cursor', hasPrevious: false, hasNext: true, summaryLabel: 'Snapshot 300', previousLabel: 'Previous', nextLabel: 'Next' } });
    expect(wrapper.text()).not.toContain('1 / 1');
    expect(wrapper.text()).toContain('Snapshot 300');
    expect(wrapper.findAll('button')[0]!.attributes('disabled')).toBeDefined();
    await wrapper.findAll('button')[1]!.trigger('click');
    expect(wrapper.emitted('next')).toHaveLength(1);
    expect(wrapper.emitted('update:page')).toBeUndefined();
    wrapper.unmount();
  });

  it('hides total summary and page count when totals are unknown', () => {
    const wrapper = mount(PaginationBar, { props: { mode: 'cursor', hasPrevious: false, hasNext: true, previousLabel: 'Previous', nextLabel: 'Next' } });
    expect(wrapper.text()).not.toContain('of');
    expect(wrapper.text()).not.toContain('/');
    expect(wrapper.text()).toContain('Previous');
    expect(wrapper.text()).toContain('Next');
    wrapper.unmount();
  });

  it('does not invent 0 totals in page mode when total is omitted', () => {
    const wrapper = mount(PaginationBar, { props: { mode: 'page', page: 1, pageSize: 20, previousLabel: 'Previous', nextLabel: 'Next' } });
    expect(wrapper.text()).not.toContain('0-0 of 0');
    expect(wrapper.text()).not.toContain('1 / 1');
    wrapper.unmount();
  });
});
