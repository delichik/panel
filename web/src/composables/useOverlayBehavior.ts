import { nextTick, onBeforeUnmount, watch, type Ref } from 'vue';

export interface OverlayBehaviorOptions {
  open: () => boolean;
  containerRef: Ref<HTMLElement | null>;
  onClose: () => void;
  /** Lock body scroll while the overlay is open (restored on close/unmount). */
  lockScroll?: boolean;
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
 * Shared modal-overlay keyboard behavior used by Dialog and the mobile nav drawer:
 * moves focus into the overlay, traps Tab, closes on Escape, restores focus to the
 * trigger, and optionally locks background scrolling.
 */
export function useOverlayBehavior({ open, containerRef, onClose, lockScroll = false }: OverlayBehaviorOptions) {
  let restoreFocusTo: HTMLElement | null = null;
  let scrollLocked = false;

  function focusableElements() {
    return containerRef.value
      ? Array.from(containerRef.value.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR))
      : [];
  }

  function focusOverlay() {
    (focusableElements()[0] ?? containerRef.value)?.focus();
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

  watch(open, async (isOpen) => {
    if (isOpen) {
      restoreFocusTo = document.activeElement instanceof HTMLElement ? document.activeElement : null;
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
    if (scrollLocked) {
      unlockBodyScroll();
      scrollLocked = false;
    }
    await nextTick();
    if (restoreFocusTo?.isConnected) restoreFocusTo.focus();
    restoreFocusTo = null;
  }, { immediate: true });

  onBeforeUnmount(() => {
    if (restoreFocusTo?.isConnected) restoreFocusTo.focus();
    restoreFocusTo = null;
    if (scrollLocked) {
      unlockBodyScroll();
      scrollLocked = false;
    }
  });

  return { onKeydown };
}