<script setup lang="ts">
import { computed, nextTick, useId } from 'vue';

const props = defineProps<{
  modelValue: string;
  tabs: Array<{ label: string; value: string; disabled?: boolean }>;
}>();
const emit = defineEmits<{ 'update:modelValue': [value: string] }>();
const id = useId();
const enabledTabs = computed(() => props.tabs.filter((tab) => !tab.disabled));

function tabId(value: string) {
  return `${id}-tab-${value.replace(/[^a-zA-Z0-9_-]/g, '-')}`;
}

function select(value: string) {
  emit('update:modelValue', value);
  nextTick(() => document.getElementById(tabId(value))?.focus());
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
  <div class="grid min-h-0 grid-rows-[auto_minmax(0,1fr)]">
    <div class="inline-flex max-w-full w-fit overflow-x-auto rounded-xl border border-border bg-muted p-1" role="tablist">
      <button
        v-for="tab in tabs"
        :key="tab.value"
        type="button"
        role="tab"
        :id="tabId(tab.value)"
        :aria-selected="modelValue === tab.value"
        :aria-controls="`${id}-tabpanel`"
        :tabindex="modelValue === tab.value ? 0 : -1"
        :disabled="tab.disabled"
        class="motion-tab h-8 rounded-lg px-3 text-sm font-medium text-muted-foreground transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 disabled:pointer-events-none disabled:opacity-45 aria-selected:bg-background aria-selected:text-foreground aria-selected:shadow-sm"
        @click="$emit('update:modelValue', tab.value)"
        @keydown="onKeydown($event, tab.value)"
      >
        {{ tab.label }}
      </button>
    </div>
    <div :id="`${id}-tabpanel`" class="min-h-0 pt-4" role="tabpanel" :aria-labelledby="tabId(modelValue)" tabindex="0">
      <Transition name="tab-panel" mode="out-in">
        <div :key="modelValue" class="h-full min-h-0">
          <slot />
        </div>
      </Transition>
    </div>
  </div>
</template>
