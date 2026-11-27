<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue';
import { zIndex } from './states';

const props = withDefaults(defineProps<{ text: string; triggerClass?: string }>(), { triggerClass: '' });

/** WinUI Tooltip：悬停约 500ms 后出现，键盘 focus 立即出现；83ms 淡入淡出。 */
const HOVER_DELAY = 500;
const EDGE_PADDING = 8;
const GAP = 4;

const trigger = ref<HTMLElement | null>(null);
const panel = ref<HTMLElement | null>(null);
const visible = ref(false);
const position = ref<Record<string, string>>({});

let hoverTimer: number | undefined;
let frame = 0;

function clearHoverTimer() {
  if (hoverTimer === undefined) return;
  window.clearTimeout(hoverTimer);
  hoverTimer = undefined;
}

function show() {
  if (visible.value) return;
  visible.value = true;
  void nextTick(updatePosition);
}

function hide() {
  clearHoverTimer();
  visible.value = false;
}

function onPointerEnter() {
  clearHoverTimer();
  hoverTimer = window.setTimeout(() => {
    hoverTimer = undefined;
    show();
  }, HOVER_DELAY);
}

/**
 * Tooltip 挂到 body 并用 fixed 定位：表格/Tabs 的 overflow 容器不能裁切它。
 * 默认在触发器下方，空间不足且上方更宽裕时翻转到上方；水平方向夹在视口内。
 */
function updatePosition() {
  if (!visible.value || !trigger.value) return;
  const rect = trigger.value.getBoundingClientRect();
  const tipRect = panel.value?.getBoundingClientRect();
  const width = tipRect?.width ?? Math.min(320, window.innerWidth - EDGE_PADDING * 2);
  const height = tipRect?.height ?? 32;
  const spaceBelow = window.innerHeight - rect.bottom - GAP - EDGE_PADDING;
  const spaceAbove = rect.top - GAP - EDGE_PADDING;
  const placeAbove = spaceBelow < height && spaceAbove > spaceBelow;
  const left = Math.min(
    Math.max(EDGE_PADDING, rect.left + rect.width / 2 - width / 2),
    Math.max(EDGE_PADDING, window.innerWidth - width - EDGE_PADDING),
  );
  const top = placeAbove ? rect.top - height - GAP : rect.bottom + GAP;
  position.value = {
    position: 'fixed',
    left: `${Math.round(left)}px`,
    top: `${Math.round(top)}px`,
  };
}

function onViewportChange() {
  if (!visible.value) return;
  window.cancelAnimationFrame(frame);
  frame = window.requestAnimationFrame(() => updatePosition());
}

function onFocusIn() {
  clearHoverTimer();
  show();
}

function onFocusOut(event: FocusEvent) {
  const next = event.relatedTarget as Node | null;
  if (next && trigger.value?.contains(next)) return;
  hide();
}

onMounted(() => {
  window.addEventListener('resize', onViewportChange);
  window.addEventListener('scroll', onViewportChange, true);
});

onBeforeUnmount(() => {
  clearHoverTimer();
  window.cancelAnimationFrame(frame);
  window.removeEventListener('resize', onViewportChange);
  window.removeEventListener('scroll', onViewportChange, true);
});
</script>

<template>
  <span
    ref="trigger"
    class="group relative inline-flex"
    :class="triggerClass"
    @mouseenter="onPointerEnter"
    @mouseleave="hide"
    @focusin="onFocusIn"
    @focusout="onFocusOut"
  >
    <slot />
    <Teleport to="body">
      <Transition name="tooltip">
        <span
          v-if="visible"
          ref="panel"
          role="tooltip"
          class="fixed w-max max-w-80 rounded-control border border-stroke bg-flyout px-3 py-2 text-caption font-medium text-foreground shadow-flyout backdrop-blur-flyout"
          :class="zIndex.tooltip"
          :style="position"
        >
          {{ text }}
        </span>
      </Transition>
    </Teleport>
  </span>
</template>

<style scoped>
.tooltip-enter-active,
.tooltip-leave-active {
  transition: opacity var(--panel-motion-duration-fade) linear;
}

.tooltip-enter-from,
.tooltip-leave-to {
  opacity: 0;
}
</style>
