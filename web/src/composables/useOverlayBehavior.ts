import { nextTick, onBeforeUnmount, watch, type Ref } from 'vue';

export interface OverlayBehaviorOptions {
  open: () => boolean;
  containerRef: Ref<HTMLElement | null>;
  onClose: () => void;
  /** Lock body scroll while the overlay is open (restored on close/unmount). */
  lockScroll?: boolean;
  /**
   * Optional explicit initial focus target: a CSS selector searched inside the
   * overlay, or a resolver returning the element. Defaults to the first
   * focusable element (falling back to the overlay container itself).
   */
  initialFocus?: string | (() => HTMLElement | null);
}

const FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',');

/**
 * Body scroll locks are reference-counted so nested overlays (for example a
 * dialog opened above another dialog) restore the page scroll only when the
 * last overlay closes, instead of each overlay clobbering the previous one.
 */
let bodyScrollLocks = 0;
let savedBodyOverflow = '';

function lockBodyScroll() {
  if (bodyScrollLocks === 0) savedBodyOverflow = document.body.style.overflow;
  bodyScrollLocks += 1;
  document.body.style.overflow = 'hidden';
}

function unlockBodyScroll() {
  bodyScrollLocks = Math.max(0, bodyScrollLocks - 1);
  if (bodyScrollLocks === 0) document.body.style.overflow = savedBodyOverflow;
}

/**
 * Open overlays in stacking order. Only the topmost overlay answers the
 * document-level Escape fallback, so a dialog opened above the mobile drawer
 * (or above another dialog) is never closed by the layer underneath it.
 */
interface OverlayEntry { token: symbol; container: () => HTMLElement | null }
const overlayStack: OverlayEntry[] = [];

function isVisible(element: HTMLElement) {
  if (element.hasAttribute('hidden')) return false;
  if (element.getAttribute('aria-hidden') === 'true') return false;
  if (element.closest('[inert]')) return false;
  const style = typeof window !== 'undefined' && typeof window.getComputedStyle === 'function'
    ? window.getComputedStyle(element)
    : null;
  if (style && (style.display === 'none' || style.visibility === 'hidden')) return false;
  return true;
}

/**
 * Shared modal-overlay keyboard behavior used by Dialog and the mobile nav drawer:
 * moves focus into the overlay, traps Tab, closes on Escape, restores focus to the
 * trigger, and optionally locks background scrolling.
 *
 * Escape is also handled at document level while the overlay is topmost, so it
 * keeps working if focus escapes the panel (for example after the focused
 * control was disabled mid-save).
 */
export function useOverlayBehavior({ open, containerRef, onClose, lockScroll = false, initialFocus }: OverlayBehaviorOptions) {
  const token = Symbol('panel-overlay');
  let restoreFocusTo: HTMLElement | null = null;
  let scrollLocked = false;
  let registered = false;

  function isTopmost() {
    return overlayStack[overlayStack.length - 1]?.token === token;
  }

  function focusableElements() {
    if (!containerRef.value) return [];
    return Array.from(containerRef.value.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)).filter(isVisible);
  }

  function resolveInitialFocus(): HTMLElement | null {
    if (typeof initialFocus === 'function') return initialFocus();
    if (typeof initialFocus === 'string') return containerRef.value?.querySelector<HTMLElement>(initialFocus) ?? null;
    return focusableElements()[0] ?? containerRef.value;
  }

  function focusOverlay() {
    (resolveInitialFocus() ?? containerRef.value)?.focus();
  }

  function onKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      event.preventDefault();
      onClose();
      return;
    }
    if (event.key !== 'Tab') return;

    const elements = focusableElements();
    if (!elements.length) {
      event.preventDefault();
      containerRef.value?.focus();
      return;
    }
    const first = elements[0];
    const last = elements[elements.length - 1];
    if (event.shiftKey && (document.activeElement === first || document.activeElement === containerRef.value)) {
      event.preventDefault();
      last?.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first?.focus();
    }
  }

  /**
   * Document-level fallback for Escape: the overlay panel handles keys itself
   * while focus is inside any overlay, so this only covers the case where focus
   * escaped every overlay (for example the focused control was disabled
   * mid-save). Only the topmost overlay answers.
   */
  function onDocumentKeydown(event: KeyboardEvent) {
    if (event.key !== 'Escape' || !open() || !isTopmost()) return;
    const target = event.target as Node | null;
    if (target && overlayStack.some((entry) => entry.container()?.contains(target))) return;
    event.preventDefault();
    onClose();
  }

  function register() {
    if (registered) return;
    registered = true;
    overlayStack.push({ token, container: () => containerRef.value });
    document.addEventListener('keydown', onDocumentKeydown);
  }

  function unregister() {
    if (!registered) return;
    registered = false;
    const index = overlayStack.findIndex((entry) => entry.token === token);
    if (index >= 0) overlayStack.splice(index, 1);
    document.removeEventListener('keydown', onDocumentKeydown);
  }

  watch(open, async (isOpen) => {
    if (isOpen) {
      restoreFocusTo = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      register();
      if (lockScroll) {
        lockBodyScroll();
        scrollLocked = true;
      }
      // The overlay DOM may be rendered one tick after the watcher fires,
      // especially when the overlay starts already open (immediate branch).
      await nextTick();
      if (!containerRef.value) await nextTick();
      focusOverlay();
      return;
    }
    unregister();
    if (scrollLocked) {
      unlockBodyScroll();
      scrollLocked = false;
    }
    await nextTick();
    if (restoreFocusTo?.isConnected) restoreFocusTo.focus();
    restoreFocusTo = null;
  }, { immediate: true });

  onBeforeUnmount(() => {
    unregister();
    if (restoreFocusTo?.isConnected) restoreFocusTo.focus();
    restoreFocusTo = null;
    if (scrollLocked) {
      unlockBodyScroll();
      scrollLocked = false;
    }
  });

  return { onKeydown };
}
