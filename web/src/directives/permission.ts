import type { Directive, DirectiveBinding } from 'vue';
import { useSessionStore } from '@/stores/session';

type PermissionValue = string | string[] | {
  permission: string | string[];
  effect?: 'hide' | 'disable';
};

function allowed(
  value: PermissionValue,
  modifiers?: Partial<Record<string, boolean>>,
): { keys: string[]; effect: 'hide' | 'disable' } {
  const defaultEffect: 'hide' | 'disable' = modifiers?.disable ? 'disable' : 'hide';
  if (typeof value === 'string') return { keys: [value], effect: defaultEffect };
  if (Array.isArray(value)) return { keys: value, effect: defaultEffect };
  return {
    keys: Array.isArray(value.permission) ? value.permission : [value.permission],
    effect: value.effect ?? defaultEffect,
  };
}

function apply(el: HTMLElement, binding: DirectiveBinding<PermissionValue>) {
  if (!binding.value) return;
  const { keys, effect } = allowed(binding.value, binding.modifiers);
  const session = useSessionStore();
  const granted = keys.some((key) => session.can(key));
  if (effect === 'disable') {
    const control = el as HTMLElement & { disabled?: boolean };
    control.disabled = !granted;
    el.setAttribute('aria-disabled', granted ? 'false' : 'true');
    el.classList.toggle('pointer-events-none', !granted);
    el.classList.toggle('opacity-60', !granted);
    return;
  }
  el.hidden = !granted;
  el.setAttribute('aria-hidden', granted ? 'false' : 'true');
}

/**
 * v-permission="'servers.manage'" hides the element unless the session holds
 * the permission. `v-permission.disable` keeps it visible but disabled.
 */
export const vPermission: Directive<HTMLElement, PermissionValue> = {
  mounted(el, binding) {
    apply(el, binding);
  },
  updated(el, binding) {
    apply(el, binding);
  },
};
