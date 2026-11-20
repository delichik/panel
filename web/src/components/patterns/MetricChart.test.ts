// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { MetricSeriesData } from '@/utils/metrics';

const captured = vi.hoisted(() => ({ option: { current: undefined as unknown } }));

vi.mock('vue-echarts', async () => {
  const { defineComponent: define, h } = await import('vue');
  return {
    default: define({
      name: 'VChartStub',
      props: { option: { type: Object, required: true }, autoresize: { type: Boolean, default: false } },
      setup(props) {
        return () => {
          captured.option.current = props.option;
          return h('div', { 'data-testid': 'vchart-stub' });
        };
      },
    }),
  };
});

import MetricChart from './MetricChart.vue';

type MediaHandlers = Set<(event: MediaQueryListEvent) => void>;

function stubMatchMedia(initial: Record<string, boolean> = {}) {
  const current = { ...initial };
  const listeners = new Map<string, MediaHandlers>();
  const makeQuery = (query: string) => ({
    media: query,
    get matches() { return Boolean(current[query]); },
    onchange: null,
    addEventListener: (_type: 'change', handler: (event: MediaQueryListEvent) => void) => {
      if (!listeners.has(query)) listeners.set(query, new Set());
      listeners.get(query)!.add(handler);
    },
    removeEventListener: (_type: 'change', handler: (event: MediaQueryListEvent) => void) => {
      listeners.get(query)?.delete(handler);
    },
    addListener: (handler: (event: MediaQueryListEvent) => void) => {
      if (!listeners.has(query)) listeners.set(query, new Set());
      listeners.get(query)!.add(handler);
    },
    removeListener: (handler: (event: MediaQueryListEvent) => void) => {
      listeners.get(query)?.delete(handler);
    },
    dispatchEvent: () => true,
  });
  vi.stubGlobal('matchMedia', vi.fn((query: string) => makeQuery(query)));
  return {
    set(query: string, matches: boolean) {
      current[query] = matches;
      listeners.get(query)?.forEach((handler) => handler({ matches } as MediaQueryListEvent));
    },
  };
}

function series(id: string, name: string, value: number): MetricSeriesData {
  return { id, name, points: [{ time: '2026-08-01T00:00:00.000Z', value }] };
}

function latestOption(): Record<string, any> {
  return captured.option.current as Record<string, any>;
}

beforeEach(() => {
  document.documentElement.removeAttribute('data-theme');
  document.documentElement.dataset.theme = 'light';
  captured.option.current = undefined;
});

afterEach(() => {
  vi.unstubAllGlobals();
  document.documentElement.removeAttribute('data-theme');
});

describe('MetricChart series differentiation', () => {
  it('uses dashed lines and different symbols for multiple series (not color-only)', async () => {
    const wrapper = mount(MetricChart, {
      props: {
        series: [series('rx', 'RX', 10), series('tx', 'TX', 20)],
        valueKind: 'bytes',
        label: 'Traffic',
      },
    });
    await nextTick();
    const option = latestOption();
    expect(option.series).toHaveLength(2);
    expect(option.series[0].lineStyle.type).toBe('solid');
    expect(option.series[1].lineStyle.type).toBe('dashed');
    expect(option.series[0].symbol).not.toBe(option.series[1].symbol);
    expect(option.series[0].showSymbol).toBe(true);
    expect(option.series[1].showSymbol).toBe(true);
    expect(option.series[0].emphasis.focus).toBe('none');
    expect(wrapper.get('[data-testid="metric-chart"]').attributes('role')).toBe('img');
    wrapper.unmount();
  });

  it('keeps a single series solid/without symbols and still has no focus emphasis', async () => {
    const wrapper = mount(MetricChart, {
      props: { series: [series('rx', 'RX', 10)], valueKind: 'bytes', label: 'Traffic' },
    });
    await nextTick();
    const option = latestOption();
    expect(option.series[0].lineStyle.type).toBe('solid');
    expect(option.series[0].showSymbol).toBe(false);
    expect(option.series[0].emphasis.focus).toBe('none');
    wrapper.unmount();
  });

  it('colors the average line and band portions above the warning threshold red', async () => {
    document.documentElement.style.setProperty('--panel-danger', '#cc0000');
    const time = '2026-08-01T00:00:00.000Z';
    const banded: MetricSeriesData = {
      id: 'cpu',
      name: 'CPU',
      points: [{ time, value: 20 }],
      band: { min: [{ time, value: 10 }], max: [{ time, value: 30 }] },
    };
    const wrapper = mount(MetricChart, {
      props: {
        series: [banded],
        valueKind: 'percent',
        label: 'CPU usage',
        threshold: { value: 15, label: 'Warning 15%' },
      },
    });
    await nextTick();
    const option = latestOption();

    // 阈值把阴影带拆成正常段和超阈值段，并把平均线拆成正常/超阈值两条。
    expect(option.series.map((item: { id: string }) => item.id)).toEqual([
      'cpu-band-min', 'cpu-band-normal', 'cpu-band-over', 'cpu', 'cpu-over',
    ]);
    expect(option.series[2].areaStyle.color).toBeTruthy();
    expect(option.series[4].markLine.data[0].yAxis).toBe(15);
    expect(option.series[4].lineStyle.color).toBeTruthy();
    expect(option.visualMap).toBeUndefined();
    wrapper.unmount();
    document.documentElement.style.removeProperty('--panel-danger');
  });

  it('renders a min–max band behind the average line and reports all three values in the tooltip', async () => {
    const time = '2026-08-01T00:00:00.000Z';
    const banded: MetricSeriesData = {
      id: 'cpu',
      name: 'CPU',
      points: [{ time, value: 20 }],
      band: { min: [{ time, value: 10 }], max: [{ time, value: 30 }] },
    };
    const wrapper = mount(MetricChart, {
      props: {
        series: [banded],
        valueKind: 'percent',
        label: 'CPU usage',
        bandLabels: { average: 'Avg', minimum: 'Min', maximum: 'Max' },
      },
    });
    await nextTick();
    const option = latestOption();

    // 两条透明堆叠线组成阴影带，第三条是平均值线。
    expect(option.series).toHaveLength(3);
    expect(option.series[0].stack).toBe(option.series[1].stack);
    expect(option.series[1].areaStyle.opacity).toBeGreaterThan(0);
    expect(option.series[1].data).toEqual([[new Date(time).getTime(), 20]]);
    expect(option.series[2].id).toBe('cpu');

    const html = option.tooltip.formatter([{
      seriesId: 'cpu',
      value: [new Date(time).getTime(), 20],
      marker: '<span class="marker"></span>',
    }]);
    expect(html).toContain('Avg 20.0%');
    expect(html).toContain('Min 10.0% – Max 30.0%');
    wrapper.unmount();
  });
});

describe('MetricChart high contrast', () => {
  it('degrades to readable system colors and stronger strokes under forced-colors', async () => {
    stubMatchMedia({ '(forced-colors: active)': true });
    const wrapper = mount(MetricChart, {
      props: {
        series: [series('rx', 'RX', 10), series('tx', 'TX', 20)],
        valueKind: 'bytes',
        label: 'Traffic',
      },
    });
    await nextTick();
    const option = latestOption();

    // 2 鏉＄嚎鍚岃壊锛堢郴缁熷墠鏅級锛岄潬 dashed + symbol 鍖哄垎銆?    expect(option.color[0]).toBe('#000000');
    expect(option.color[1]).toBe('#000000');
    expect(option.series[0].lineStyle.color).toBe('#000000');
    expect(option.series[1].lineStyle.color).toBe('#000000');
    expect(option.series[0].lineStyle.width).toBe(3);
    expect(option.series[1].lineStyle.type).toBe('dashed');
    expect(option.series[0].symbol).not.toBe(option.series[1].symbol);
    expect(option.series[0].showSymbol).toBe(true);
    expect(option.series[0].areaStyle).toBeUndefined();

    // 杞?/ tooltip 涔熷繀椤绘槸鍙绯荤粺鑹层€?    expect(option.yAxis.axisLabel.color).toBe('#000000');
    expect(option.xAxis.axisLine.lineStyle.color).toBe('#000000');
    expect(option.tooltip.backgroundColor).toBe('#ffffff');
    expect(option.tooltip.textStyle.color).toBe('#000000');
    expect(option.tooltip.borderWidth).toBe(2);
    expect(option.series[0].emphasis.focus).toBe('none');
    wrapper.unmount();
  });

  it('reacts to prefers-contrast change events at runtime', async () => {
    const media = stubMatchMedia({ '(forced-colors: active)': false, '(prefers-contrast: more)': false });
    const wrapper = mount(MetricChart, {
      props: { series: [series('rx', 'RX', 10)], valueKind: 'bytes', label: 'Traffic' },
    });
    await nextTick();
    expect(latestOption().series[0].lineStyle.width).toBe(2);

    media.set('(prefers-contrast: more)', true);
    await vi.waitFor(() => {
      expect(latestOption().series[0].lineStyle.width).toBe(3);
      expect(latestOption().series[0].lineStyle.color).toBe('#000000');
    });
    wrapper.unmount();
  });

  it('falls back to the normal token palette when no contrast query matches', async () => {
    document.documentElement.style.setProperty('--accent-fill-default', '#123456');
    stubMatchMedia({ '(forced-colors: active)': false, '(prefers-contrast: more)': false });
    const wrapper = mount(MetricChart, {
      props: { series: [series('rx', 'RX', 10)], valueKind: 'bytes', label: 'Traffic' },
    });
    await nextTick();
    // resolveCssColor 会把 hex 具体化为 canvas 可用的 rgb()。
    expect(latestOption().color[0]).toBe('rgb(18, 52, 86)');
    wrapper.unmount();
    document.documentElement.style.removeProperty('--accent-fill-default');
  });
});

