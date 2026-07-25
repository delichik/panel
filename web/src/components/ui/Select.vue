<script setup lang="ts">
import { Check, ChevronDown } from '@lucide/vue';
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useAttrs, useId, watch } from 'vue';
import { cn } from './cn';

defineOptions({ inheritAttrs: false });

type SelectOption = { label: string; value: string; disabled?: boolean };

const props = defineProps<{
  modelValue?: string;
  options: SelectOption[];
  placeholder?: string;
  disabled?: boolean;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: string];
  change: [value: string];
}>();

const attrs = useAttrs();
const root = ref<HTMLElement | null>(null);
const button = ref<HTMLButtonElement | null>(null);
const open = ref(false);
const activeIndex = ref(-1);
const id = useId();

const selectedOption = computed(() => props.options.find((option) => option.value === props.modelValue));
const selectedLabel = computed(() => selectedOption.value?.label ?? props.placeholder ?? '');
const enabledOptions = computed(() => props.options.filter((option) => !option.disabled));
const isDisabled = computed(() => props.disabled);
const activeOption = computed(() => props.options[activeIndex.value]);
const activeOptionId = computed(() => (open.value && activeOption.value ? `${id}-option-${activeIndex.value}` : undefined));

const rootClasses = computed(() => cn('relative w-full min-w-0', attrs.class as string));
const buttonClasses = computed(() => cn(
  'motion-field flex h-9 w-full min-w-0 items-center justify-between gap-2 rounded-xl border border-input bg-background px-3 text-left text-sm text-foreground transition-colors',
  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 focus-visible:ring-offset-2 focus-visible:ring-offset-background',
  'disabled:cursor-not-allowed disabled:opacity-45',
  open.value && 'border-border-strong bg-popover shadow-[var(--panel-motion-shadow-raised)]',
));

const hiddenSelectAttrs = computed(() => ({
  name: typeof attrs.name === 'string' ? attrs.name : undefined,
  form: typeof attrs.form === 'string' ? attrs.form : undefined,
  required: attrs.required === '' || attrs.required === true || attrs.required === 'true' ? true : undefined,
}));

function firstEnabledIndex() {
  return props.options.findIndex((option) => !option.disabled);
}

function currentOrFirstEnabledIndex() {
  const currentIndex = props.options.findIndex((option) => option.value === props.modelValue && !option.disabled);
  return currentIndex >= 0 ? currentIndex : firstEnabledIndex();
}

function setActiveIndex(nextIndex: number) {
  if (!props.options.length) {
    activeIndex.value = -1;
    return;
  }

  const direction = nextIndex >= activeIndex.value ? 1 : -1;
  let cursor = nextIndex;
  for (let checked = 0; checked < props.options.length; checked += 1) {
    const normalized = (cursor + props.options.length) % props.options.length;
    if (!props.options[normalized]?.disabled) {
      activeIndex.value = normalized;
      return;
    }
    cursor += direction;
  }
  activeIndex.value = -1;
}

function scrollActiveIntoView() {
  nextTick(() => {
    if (activeIndex.value < 0) return;
    const item = document.getElementById(`${id}-option-${activeIndex.value}`);
    item?.scrollIntoView({ block: 'nearest' });
  });
}

function openList() {
  if (isDisabled.value || !props.options.length) return;
  open.value = true;
  activeIndex.value = currentOrFirstEnabledIndex();
  scrollActiveIntoView();
}

function closeList({ restoreFocus = false } = {}) {
  open.value = false;
  activeIndex.value = -1;
  if (restoreFocus) button.value?.focus();
}

function toggleList() {
  if (open.value) closeList();
  else openList();
}

function choose(option: SelectOption) {
  if (option.disabled || isDisabled.value) return;
  emit('update:modelValue', option.value);
  emit('change', option.value);
  closeList({ restoreFocus: true });
}

function chooseValue(value: string) {
  const option = props.options.find((item) => item.value === value);
  if (option) choose(option);
}

function setActiveFromPointer(option: SelectOption, index: number) {
  if (!option.disabled) activeIndex.value = index;
}

function moveActive(delta: number) {
  if (!enabledOptions.value.length) return;
  if (!open.value) {
    openList();
    return;
  }
  const start = activeIndex.value >= 0 ? activeIndex.value : currentOrFirstEnabledIndex();
  setActiveIndex(start + delta);
  scrollActiveIntoView();
}

function onKeydown(event: KeyboardEvent) {
  if (isDisabled.value) return;
  if (event.key === 'ArrowDown') {
    event.preventDefault();
    moveActive(1);
    return;
  }
  if (event.key === 'ArrowUp') {
    event.preventDefault();
    moveActive(-1);
    return;
  }
  if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault();
    if (!open.value) {
      openList();
      return;
    }
    if (activeOption.value) choose(activeOption.value);
    return;
  }
  if (event.key === 'Escape' && open.value) {
    event.preventDefault();
    closeList({ restoreFocus: true });
  }
  if (event.key === 'Tab') closeList();
}

function onDocumentPointerDown(event: PointerEvent) {
  if (root.value && !root.value.contains(event.target as Node)) closeList();
}

watch(() => props.modelValue, () => {
  if (open.value) activeIndex.value = currentOrFirstEnabledIndex();
});

onMounted(() => document.addEventListener('pointerdown', onDocumentPointerDown));
onBeforeUnmount(() => document.removeEventListener('pointerdown', onDocumentPointerDown));
</script>

<template>
  <div ref="root" :class="rootClasses">
    <select
      v-bind="hiddenSelectAttrs"
      class="pointer-events-none absolute size-px opacity-0"
      tabindex="-1"
      aria-hidden="true"
      :disabled="isDisabled"
      :value="modelValue"
      @change="chooseValue(($event.target as HTMLSelectElement).value)"
    >
      <option v-if="placeholder" value="" disabled>{{ placeholder }}</option>
      <option v-for="option in options" :key="option.value" :value="option.value" :disabled="option.disabled">
        {{ option.label }}
      </option>
    </select>

    <button
      :id="attrs.id as string | undefined"
      ref="button"
      type="button"
      role="combobox"
      aria-haspopup="listbox"
      :aria-controls="`${id}-listbox`"
      :aria-expanded="open"
      :aria-activedescendant="activeOptionId"
      :disabled="isDisabled"
      :class="buttonClasses"
      @click="toggleList"
      @keydown="onKeydown"
    >
      <span class="min-w-0 flex-1 truncate" :class="selectedOption ? 'text-foreground' : 'text-muted-foreground'">
        {{ selectedLabel }}
      </span>
      <ChevronDown class="size-4 shrink-0 text-muted-foreground transition-transform duration-150 ease-out" :class="open ? 'rotate-180' : undefined" aria-hidden="true" />
    </button>

    <div
      v-if="open"
      :id="`${id}-listbox`"
      class="motion-popover absolute left-0 right-0 z-50 mt-2 max-h-64 overflow-y-auto overflow-x-hidden rounded-2xl border border-border bg-popover p-1 text-popover-foreground shadow-xl"
      role="listbox"
      :aria-labelledby="attrs.id as string | undefined"
      @keydown="onKeydown"
    >
      <button
        v-for="(option, index) in options"
        :id="`${id}-option-${index}`"
        :key="option.value"
        type="button"
        role="option"
        :aria-selected="modelValue === option.value"
        :disabled="option.disabled"
        class="motion-menu-item flex w-full min-w-0 items-center gap-2 rounded-xl px-3 py-2 text-left text-sm transition-colors focus-visible:outline-none disabled:pointer-events-none disabled:opacity-40"
        :class="[
          modelValue === option.value ? 'bg-accent text-foreground' : 'text-foreground/72 hover:bg-accent hover:text-foreground',
          activeIndex === index && modelValue !== option.value ? 'bg-accent text-foreground' : undefined,
        ]"
        @mouseenter="setActiveFromPointer(option, index)"
        @click="choose(option)"
      >
        <span class="min-w-0 flex-1 truncate">{{ option.label }}</span>
        <Check v-if="modelValue === option.value" class="size-4 shrink-0 text-foreground" aria-hidden="true" />
      </button>
    </div>
  </div>
</template>
