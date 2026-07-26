<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue';

defineProps<{ align?: 'left' | 'right' }>();

const open = ref(false);
const root = ref<HTMLElement | null>(null);
const trigger = ref<HTMLElement | null>(null);
const menu = ref<HTMLElement | null>(null);
const menuId = useId();

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
  if (focus === 'first') menuItems()[0]?.focus();
  if (focus === 'last') menuItems().at(-1)?.focus();
  if (focus === 'trigger') triggerControl()?.focus();
}

function onDocumentClick(event: MouseEvent) {
  if (root.value && !root.value.contains(event.target as Node)) void setOpen(false);
}

function onTriggerKeydown(event: KeyboardEvent) {
  if (!['ArrowDown', 'ArrowUp', 'Enter', ' '].includes(event.key)) return;
  event.preventDefault();
  void setOpen(true, event.key === 'ArrowUp' ? 'last' : 'first');
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
});
onBeforeUnmount(() => document.removeEventListener('click', onDocumentClick));
</script>

<template>
  <div ref="root" class="relative inline-block text-left">
    <div ref="trigger" @click.stop="setOpen(!open, open ? 'trigger' : 'first')" @keydown="onTriggerKeydown">
      <slot name="trigger" :open="open" />
    </div>
    <div
      v-if="open"
      :id="menuId"
      ref="menu"
      class="motion-popover absolute z-40 mt-2 min-w-52 overflow-hidden rounded-2xl border border-border bg-popover p-1 text-popover-foreground shadow-xl"
      :class="align === 'left' ? 'left-0' : 'right-0'"
      role="menu"
      @click="setOpen(false, 'trigger')"
      @keydown="onMenuKeydown"
    >
      <slot />
    </div>
  </div>
</template>
