// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { defineComponent, type Ref } from 'vue';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { useCompactViewport } from './useCompactViewport';

function stubMatchMedia(matches: boolean) {
  const listeners: Array<(event: { matches: boolean }) => void> = [];
  const removed: Array<(event: { matches: boolean }) => void> = [];
  const mql = {
    matches,
    media: '(max-width: 1279.98px)',
    onchange: null,
    addEventListener: (_type: string, listener: (event: { matches: boolean }) => void) => { listeners.push(listener); },
    removeEventListener: (_type: string, listener: (event: { matches: boolean }) => void) => {
      removed.push(listener);
      const index = listeners.indexOf(listener);
      if (index >= 0) listeners.splice(index, 1);
    },
    addListener: () => undefined,
    removeListener: () => undefined,
    dispatchEvent: () => false,
  };
  vi.stubGlobal('matchMedia', () => mql);
  return {
    emit(next: boolean) {
      mql.matches = next;
      listeners.slice().forEach((listener) => listener({ matches: next }));
    },
    removed,
  };
}

function mountProbe(): { compact: Ref<boolean>; unmount: () => void } {
  let compact!: Ref<boolean>;
  const Host = defineComponent({
    setup() {
      compact = useCompactViewport();
      return () => null;
    },
  });
  const wrapper = mount(Host);
  return { compact, unmount: () => wrapper.unmount() };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('useCompactViewport', () => {
  it('reads the compact breakpoint and follows media changes', () => {
    const mql = stubMatchMedia(true);
    const probe = mountProbe();

    expect(probe.compact.value).toBe(true);

    mql.emit(false);
    expect(probe.compact.value).toBe(false);

    probe.unmount();
    expect(mql.removed).toHaveLength(1);
  });

  it('falls back to the wide layout when matchMedia is unavailable', () => {
    vi.stubGlobal('matchMedia', undefined);
    const probe = mountProbe();

    expect(probe.compact.value).toBe(false);
    probe.unmount();
  });
});
