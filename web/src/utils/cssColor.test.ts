// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { resolveCssColor } from './cssColor';

describe('resolveCssColor', () => {
  it('resolves hex/rgb tokens through computed style for canvas consumers', () => {
    expect(resolveCssColor('#ff0000')).toContain('rgb');
    expect(resolveCssColor('rgb(1, 2, 3)')).toContain('rgb');
  });

  it('falls back to the raw token for invalid values and empty input', () => {
    expect(resolveCssColor('')).toBe('');
    expect(resolveCssColor('definitely-not-a-color')).toBe('definitely-not-a-color');
  });

  it('returns the raw token when no document is available (SSR/tests)', () => {
    expect(resolveCssColor('color-mix(in srgb, #0078d4 45%, white)', null)).toBe('color-mix(in srgb, #0078d4 45%, white)');
  });
});
