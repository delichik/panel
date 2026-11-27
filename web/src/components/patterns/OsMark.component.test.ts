// @vitest-environment jsdom
import { enableAutoUnmount, mount } from '@vue/test-utils';
import { afterEach, describe, expect, it } from 'vitest';
import OsMark from './OsMark.vue';

enableAutoUnmount(afterEach);

describe('OsMark', () => {
  it('renders the vendored distribution glyph on its brand color', () => {
    const wrapper = mount(OsMark, { props: { os: { id: 'debian', prettyName: 'Debian GNU/Linux 13' }, size: 'lg' } });

    const mark = wrapper.get('[data-testid="os-mark"]');
    expect(mark.attributes('data-os')).toBe('debian');
    expect(mark.attributes('title')).toBe('Debian');
    // Decorative: the adjacent text names the OS.
    expect(mark.attributes('aria-hidden')).toBe('true');
    expect(mark.classes()).toContain('size-11');
    expect(mark.attributes('style')).toContain('background-color');

    const glyph = mark.get('svg');
    expect(glyph.find('path').attributes('d')?.length).toBeGreaterThan(20);
    expect(glyph.attributes('style')).toContain('--panel-mark-on-brand');
  });

  it('falls back to the neutral server glyph for an unknown distribution', () => {
    const wrapper = mount(OsMark, { props: { os: { id: 'unknown' } } });

    const mark = wrapper.get('[data-testid="os-mark"]');
    expect(mark.attributes('data-os')).toBe('unknown');
    expect(mark.attributes('title')).toBeUndefined();
    expect(mark.classes()).toContain('bg-muted');
    expect(mark.attributes('style')).toBeUndefined();
    // No brand path: the fallback is a Fluent icon.
    expect(mark.findAll('path')).toHaveLength(1);
  });

  it('uses a dark glyph on light brand colors for contrast', () => {
    const wrapper = mount(OsMark, { props: { os: { id: 'opensuse' } } });
    const glyph = wrapper.get('[data-testid="os-mark"] svg');
    expect(glyph.attributes('style')).toContain('--panel-mark-on-light');
  });
});
