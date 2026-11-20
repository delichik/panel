<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue';
import { focusWithoutScroll } from '@/utils/focus';
import { disabledText, focusRingInset } from './states';

const props = defineProps<{
  modelValue: string;
  tabs: Array<{ label: string; value: string; disabled?: boolean }>;
  /**
   * Keep the panel content mounted when the active tab changes. Used by pages
   * that own per-tab visibility (v-show) and rely on the tab panel not being
   * destroyed, for example covered server details that lazily mount tabs once
   * and preserve their scroll/selection state across switches.
   */
  keepPanels?: boolean;
}>();
const emit = defineEmits<{ 'update:modelValue': [value: string] }>();
const id = useId();
const enabledTabs = computed(() => props.tabs.filter((tab) => !tab.disabled));

const tablist = ref<HTMLElement | null>(null);
const tabButtons = ref<HTMLElement[]>([]);
/** WinUI TabView selection underline: one shared indicator that slides between tabs. */
const indicator = ref({ left: 0, width: 0, visible: false });
let resizeObserver: ResizeObserver | null = null;

function updateIndicator() {
  const activeIndex = props.tabs.findIndex((tab) => tab.value === props.modelValue);
  const button = tabButtons.value[activeIndex];
  if (!button || !button.offsetWidth) {
    indicator.value = { ...indicator.value, visible: false };
    return;
  }
  indicator.value = {
    left: button.offsetLeft + 8,
    width: Math.max(0, button.offsetWidth - 16),
    visible: true,
  };
}

watch(() => props.modelValue, () => { void nextTick(updateIndicator); });
watch(() => props.tabs, () => { void nextTick(updateIndicator); }, { deep: true });

onMounted(() => {
  updateIndicator();
  if (typeof ResizeObserver === 'function' && tablist.value) {
    resizeObserver = new ResizeObserver(() => updateIndicator());
    resizeObserver.observe(tablist.value);
  }
  window.addEventListener('resize', updateIndicator);
});
onBeforeUnmount(() => {
  resizeObserver?.disconnect();
  window.removeEventListener('resize', updateIndicator);
});

function tabId(value: string) {
  return `${id}-tab-${value.replace(/[^a-zA-Z0-9_-]/g, '-')}`;
}

function select(value: string) {
  emit('update:modelValue', value);
  nextTick(() => focusWithoutScroll(document.getElementById(tabId(value))));
}

function selectRelative(currentValue: string, delta: number) {
  const index = enabledTabs.value.findIndex((tab) => tab.value === currentValue);
  if (!enabledTabs.value.length) return;
  const next = enabledTabs.value[(index + delta + enabledTabs.value.length) % enabledTabs.value.length];
  if (next) select(next.value);
}

function onKeydown(event: KeyboardEvent, value: string) {
  if (event.key === 'ArrowRight' || event.key === 'ArrowDown') selectRelative(value, 1);
  else if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') selectRelative(value, -1);
  else if (event.key === 'Home') select(enabledTabs.value[0]?.value ?? value);
  else if (event.key === 'End') select(enabledTabs.value.at(-1)?.value ?? value);
  else return;
  event.preventDefault();
}
</script>

<template>
  <div class="grid min-h-0 grid-cols-safe grid-rows-page">
    <!--
      WinUI 3 TabView header: 透明 tab 行 + 底部 1px divider + 单个共享 2px accent 下划线
      （250ms point-to-point，reduced-motion 由 main.css 降级）。
      选中态只由“文字主色 + 下划线”表达，不使用填充或 accent 文字。
    -->
    <div ref="tablist" class="relative flex w-fit max-w-full gap-1 overflow-x-auto overflow-y-hidden border-b border-divider" role="tablist">
      <button
        v-for="tab in tabs"
        :key="tab.value"
        ref="tabButtons"
        type="button"
        role="tab"
        :id="tabId(tab.value)"
        :aria-selected="modelValue === tab.value"
        :aria-controls="`${id}-tabpanel`"
        :tabindex="modelValue === tab.value ? 0 : -1"
        :disabled="tab.disabled"
        class="motion-tab relative h-control rounded-control px-3 text-body font-medium text-secondary-foreground transition-state duration-fast hover:bg-hover aria-selected:text-foreground"
        :class="[focusRingInset, disabledText]"
        @click="$emit('update:modelValue', tab.value)"
        @keydown="onKeydown($event, tab.value)"
      >
        {{ tab.label }}
      </button>
      <span
        v-show="indicator.visible"
        class="pointer-events-none absolute bottom-0 h-0.5 rounded-full bg-primary transition-indicator"
        :style="{ left: `${indicator.left}px`, width: `${indicator.width}px` }"
        aria-hidden="true"
      />
    </div>
    <div :id="`${id}-tabpanel`" class="min-h-0 pt-4" role="tabpanel" :aria-labelledby="tabId(modelValue)" tabindex="0">
      <Transition v-if="!keepPanels" name="tab-panel" mode="out-in">
        <div :key="modelValue" class="h-full min-h-0">
          <slot />
        </div>
      </Transition>
      <div v-else class="h-full min-h-0">
        <slot />
      </div>
    </div>
  </div>
</template>
