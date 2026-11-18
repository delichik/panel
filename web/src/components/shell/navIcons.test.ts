// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import { navGroups } from '@/components/shell/navModel';

describe('navigation icons', () => {
  it('every nav item renders visible Fluent SVG content (no degenerate/missing glyph)', () => {
    for (const item of navGroups.flatMap((group) => group.items)) {
      const wrapper = mount(item.icon, { attrs: { class: 'size-4' } });
      const svg = wrapper.find('svg');
      expect(svg.exists(), `${item.key} must render an svg`).toBe(true);
      // Guard against mapping an icon name to a partial/degenerate SVG (the old
      // logs icon mapped to a wifi dot and looked blank at 16px).
      expect(wrapper.element.innerHTML.length, `${item.key} glyph looks empty`).toBeGreaterThan(60);
      expect(svg.attributes('viewBox')).toBe('0 0 24 24');
      wrapper.unmount();
    }
  });
});
