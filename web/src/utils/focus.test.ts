// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { focusWithoutScroll } from './focus';

afterEach(() => {
  vi.restoreAllMocks();
});

describe('focusWithoutScroll', () => {
  it('moves focus with preventScroll so teleported popups never scroll the shell', () => {
    const focusSpy = vi.spyOn(HTMLElement.prototype, 'focus').mockImplementation(() => undefined);
    const target = document.createElement('button');

    focusWithoutScroll(target);

    expect(focusSpy).toHaveBeenCalledWith({ preventScroll: true });
    expect(focusSpy).toHaveBeenCalledTimes(1);
  });

  it('is a no-op for missing targets', () => {
    const focusSpy = vi.spyOn(HTMLElement.prototype, 'focus').mockImplementation(() => undefined);
    focusWithoutScroll(null);
    focusWithoutScroll(undefined);
    expect(focusSpy).not.toHaveBeenCalled();
  });
});
