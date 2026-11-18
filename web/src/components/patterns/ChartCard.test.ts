// @vitest-environment jsdom
import { enableAutoUnmount, mount } from '@vue/test-utils';
import { afterEach, describe, expect, it } from 'vitest';
import ChartCard from './ChartCard.vue';

enableAutoUnmount(afterEach);

describe('ChartCard', () => {
  it('renders a single card surface with a Body Strong title and current value', () => {
    const wrapper = mount(ChartCard, {
      props: { title: 'Latency', value: '12 ms', valueLabel: 'Current latency 12 ms' },
      slots: { default: '<div class="chart-body">chart</div>' },
    });

    const card = wrapper.get('[data-chart-card]');
    expect(card.classes()).toContain('surface-card');
    expect(card.classes()).toContain('p-3');

    const title = card.get('h3');
    expect(title.text()).toBe('Latency');
    expect(title.classes()).toContain('text-body-strong');
    // text-body-strong 自带 600，不再手写 font-semibold。
    expect(title.classes()).not.toContain('font-semibold');

    const value = card.get('[data-chart-value]');
    expect(value.text()).toBe('12 ms');
    expect(value.attributes('aria-label')).toBe('Current latency 12 ms');
    expect(value.classes()).toContain('tabular-nums');
  });

  it('forbids card-in-card: chart content stays inside the single card surface', () => {
    const wrapper = mount(ChartCard, {
      props: { title: 'Packet loss' },
      slots: { default: '<div class="chart-body"><span>chart</span></div>' },
    });

    expect(wrapper.findAll('.surface-card')).toHaveLength(1);
    const chart = wrapper.get('.chart-body');
    expect(chart.element.closest('.surface-card')).toBe(wrapper.get('[data-chart-card]').element);
    // 插槽外层只是布局容器，不再叠加 surface-card / surface-control。
    expect(chart.element.parentElement?.className).not.toContain('surface-card');
    expect(chart.element.parentElement?.className).not.toContain('surface-control');
  });

  it('renders the actions slot in the header and keeps 0 as a valid value', async () => {
    const wrapper = mount(ChartCard, {
      props: { title: 'Probes', value: 0 },
      slots: { actions: '<button id="range" type="button">24h</button>' },
    });
    expect(wrapper.get('#range').text()).toBe('24h');

    const value = wrapper.get('[data-chart-value]');
    expect(value.text()).toBe('0');

    await wrapper.setProps({ value: null });
    expect(wrapper.find('[data-chart-value]').exists()).toBe(false);
  });

  it('shows a dashed placeholder when empty instead of the chart slot', () => {
    const wrapper = mount(ChartCard, {
      props: { title: 'Latency', empty: true, emptyText: 'No history yet' },
      slots: { default: '<div class="chart-body">chart</div>' },
    });

    expect(wrapper.find('.chart-body').exists()).toBe(false);
    const placeholder = wrapper.get('[data-chart-card-empty]');
    expect(placeholder.text()).toBe('No history yet');
    expect(placeholder.classes()).toContain('border-dashed');
    expect(placeholder.classes()).toContain('text-muted-foreground');
    expect(wrapper.findAll('.surface-card')).toHaveLength(1);
  });

  it('supports a custom empty slot and a footer slot', () => {
    const wrapper = mount(ChartCard, {
      props: { title: 'Latency', empty: true },
      slots: {
        empty: '<span class="custom-empty">custom empty</span>',
        footer: '<p class="custom-footer">footer</p>',
      },
    });
    expect(wrapper.get('.custom-empty').text()).toBe('custom empty');
    expect(wrapper.get('.custom-footer').text()).toBe('footer');
  });
});
