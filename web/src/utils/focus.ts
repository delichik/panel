/**
 * Move DOM focus without asking the browser to scroll the nearest scroll
 * container. Teleported popups (dropdown menus, select listboxes, flyouts,
 * dialogs) must call this instead of `.focus()`: while the popup is being
 * positioned, a normal focus can scroll the document and leave the shell
 * shifted up with blank space below (body overflow:hidden prevents scrolling it
 * back, so only a refresh recovers).
 */
export function focusWithoutScroll(target: HTMLElement | null | undefined): void {
  target?.focus({ preventScroll: true });
}
