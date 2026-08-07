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
 * Shared modal-overlay keyboard behavior used by Dialog and the mobile nav drawer:
 * moves focus into the overlay, traps Tab, closes on Escape, restores focus to the
 * trigger, and optionally locks background scrolling.
 */
export function useOverlayBehavior({ open, containerRef, onClose, lockScroll = false }: OverlayBehaviorOptions) {
  let restoreFocusTo: HTMLElement | null = null;
  let previousBodyOverflow = '';

  function focusableElements() {
    return containerRef.value
      ? Array.from(containerRef.value.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR))
      : [];
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
        previousBodyOverflow = document.body.style.overflow;
        document.body.style.overflow = 'hidden';
      }
      await nextTick();
      (focusableElements()[0] ?? containerRef.value)?.focus();
      return;
    }
    if (lockScroll) {
      document.body.style.overflow = previousBodyOverflow;
    }
    await nextTick();
    restoreFocusTo?.focus();
    restoreFocusTo = null;
  }, { immediate: true });

  onBeforeUnmount(() => {
    restoreFocusTo?.focus();
    if (lockScroll) {
      document.body.style.overflow = previousBodyOverflow;
    }
  });

  return { onKeydown };
}