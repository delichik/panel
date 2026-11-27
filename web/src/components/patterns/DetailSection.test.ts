// @vitest-environment jsdom
import { enableAutoUnmount, mount } from '@vue/test-utils';
import { afterEach, describe, expect, it } from 'vitest';
import DetailSection from './DetailSection.vue';

enableAutoUnmount(afterEach);

describe('DetailSection', () => {
  it('renders the card-external heading, description and a single divided card surface', () => {
    const wrapper = mount(DetailSection, {
      props: { title: 'Runtime', description: 'Observed state' },
      slots: { default: '<p class="row">Row content</p>' },
    });

    const section = wrapper.get('[data-detail-section]');
    const heading = section.get('h2');
    expect(heading.text()).toBe('Runtime');
    expect(heading.classes()).toContain('text-body-strong');
    expect(heading.classes()).not.toContain('font-semibold');
    expect(section.text()).toContain('Observed state');

    // 只允许一层 surface-card；行由 divide-y 分隔，不再套第二层卡片。
    expect(wrapper.findAll('.surface-card')).toHaveLength(1);
    expect(wrapper.get('.surface-card').classes()).toContain('surface-card');
    expect(wrapper.get('.divide-y').classes()).toContain('divide-divider');
    expect(wrapper.get('.row').text()).toBe('Row content');
  });

  it('renders the actions slot only when provided', () => {
    const withoutActions = mount(DetailSection, { props: { title: 'Runtime' } });
    expect(withoutActions.find('button').exists()).toBe(false);

    const withActions = mount(DetailSection, {
      props: { title: 'Runtime' },
      slots: { actions: '<button type="button">Refresh</button>' },
    });
    expect(withActions.get('button').text()).toBe('Refresh');
    expect(withActions.get('[data-detail-section]').text()).toContain('Runtime');
  });

  it('supports custom description and footer slots without adding extra surfaces', () => {
    const wrapper = mount(DetailSection, {
      props: { title: 'Storage' },
      slots: {
        description: '<span class="custom-description">custom</span>',
        footer: '<p class="custom-footer">footer</p>',
      },
    });
    expect(wrapper.get('.custom-description').text()).toBe('custom');
    expect(wrapper.get('.custom-footer').text()).toBe('footer');
    expect(wrapper.findAll('.surface-card')).toHaveLength(1);
  });
});
