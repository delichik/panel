<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue';

const props = defineProps<{ align?: 'left' | 'right' }>();

const open = ref(false);
const root = ref<HTMLElement | null>(null);
const trigger = ref<HTMLElement | null>(null);
const menu = ref<HTMLElement | null>(null);
const menuId = useId();
const menuStyle = ref<Record<string, string>>({});
const MIN_WIDTH = 160;
let suppressSyntheticClick = false;
let syntheticClickTimer: number | undefined;

function triggerControl() {
  return trigger.value?.querySelector<HTMLElement>('button, [href], [tabindex]:not([tabindex="-1"])');
}

function syncTriggerAria() {
  const control = triggerControl();
  control?.setAttribute('aria-haspopup', 'menu');
  control?.setAttribute('aria-controls', menuId);
  control?.setAttribute('aria-expanded', String(open.value));
}

function menuItems() {
  return menu.value ? Array.from(menu.value.querySelectorAll<HTMLElement>('[role="menuitem"]:not([disabled])')) : [];
}

async function setOpen(value: boolean, focus: 'first' | 'last' | 'trigger' | 'none' = 'none') {
  open.value = value;
  await nextTick();
  if (value) updateMenuPosition();
  if (focus === 'first') menuItems()[0]?.focus();
  if (focus === 'last') menuItems().at(-1)?.focus();
  if (focus === 'trigger') triggerControl()?.focus();
}

function onDocumentClick(event: MouseEvent) {
  const target = event.target as Node;
  if (root.value && !root.value.contains(target) && !menu.value?.contains(target)) void setOpen(false);
}

function updateMenuPosition() {
  if (!open.value) return;
  const control = triggerControl();
  if (!control) return;
  const rect = control.getBoundingClientRect();
  const gap = 8;
  const padding = 16;
  const maxWidth = window.innerWidth - padding * 2;
  const width = Math.min(maxWidth, Math.max(MIN_WIDTH, menu.value?.offsetWidth ?? MIN_WIDTH));
  const left = props.align === 'left'
    ? Math.min(rect.left, window.innerWidth - width - padding)
    : Math.max(padding, rect.right - width);
  const availableBelow = window.innerHeight - rect.bottom - gap - padding;
  const availableAbove = rect.top - gap - padding;
  const placeAbove = availableBelow < 160 && availableAbove > availableBelow;
  menuStyle.value = {
    position: 'fixed',
    left: `${Math.max(padding, left)}px`,
    right: 'auto',
    top: placeAbove ? 'auto' : `${rect.bottom + gap}px`,
    bottom: placeAbove ? `${window.innerHeight - rect.top + gap}px` : 'auto',
    minWidth: `${width}px`,
    maxWidth: `${maxWidth}px`,
    maxHeight: `${Math.max(96, Math.min(320, placeAbove ? availableAbove : availableBelow))}px`,
  };
}

function onTriggerKeydown(event: KeyboardEvent) {
  if (!['ArrowDown', 'ArrowUp', 'Enter', ' '].includes(event.key)) return;
	event.preventDefault();
	if (event.key === 'Enter' || event.key === ' ') {
		suppressSyntheticClick = true;
		window.clearTimeout(syntheticClickTimer);
		syntheticClickTimer = window.setTimeout(() => { suppressSyntheticClick = false; }, 0);
	}
	void setOpen(true, event.key === 'ArrowUp' ? 'last' : 'first');
}

function onTriggerClick(event: MouseEvent) {
	if (suppressSyntheticClick && event.detail === 0) {
		suppressSyntheticClick = false;
		window.clearTimeout(syntheticClickTimer);
		return;
	}
	void setOpen(!open.value, open.value ? 'trigger' : 'first');
}

function onMenuKeydown(event: KeyboardEvent) {
  const items = menuItems();
  const index = items.indexOf(document.activeElement as HTMLElement);
  let nextIndex: number | undefined;
  if (event.key === 'ArrowDown') nextIndex = (index + 1 + items.length) % items.length;
  if (event.key === 'ArrowUp') nextIndex = (index - 1 + items.length) % items.length;
  if (event.key === 'Home') nextIndex = 0;
  if (event.key === 'End') nextIndex = items.length - 1;
  if (event.key === 'Escape') {
    event.preventDefault();
    void setOpen(false, 'trigger');
    return;
  }
  if (nextIndex !== undefined && items.length) {
    event.preventDefault();
    items[nextIndex]?.focus();
  }
}

watch(open, () => nextTick(syncTriggerAria));
onMounted(() => {
  syncTriggerAria();
  document.addEventListener('click', onDocumentClick);
  window.addEventListener('resize', updateMenuPosition);
  window.addEventListener('scroll', updateMenuPosition, true);
});
onBeforeUnmount(() => {
	window.clearTimeout(syntheticClickTimer);
  document.removeEventListener('click', onDocumentClick);
  window.removeEventListener('resize', updateMenuPosition);
  window.removeEventListener('scroll', updateMenuPosition, true);
});
</script>

<template>
  <div ref="root" class="relative inline-block text-left">
    <div ref="trigger" @click.stop="onTriggerClick" @keydown="onTriggerKeydown">
      <slot name="trigger" :open="open" />
    </div>
    <Teleport to="body">
      <div
        v-if="open"
        :id="menuId"
        ref="menu"
        class="motion-popover z-50 w-max overflow-y-auto overflow-x-hidden rounded-2xl border border-border bg-popover p-1 text-popover-foreground shadow-xl"
        :style="menuStyle"
        role="menu"
        @click="setOpen(false, 'trigger')"
        @keydown="onMenuKeydown"
      >
        <slot />
      </div>
    </Teleport>
  </div>
</template>
