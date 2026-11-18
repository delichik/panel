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

